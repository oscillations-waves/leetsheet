# Job Worker Service — Design Document

## Overview

A prototype service that exposes an API to run arbitrary Linux processes. Clients can
start, stop, query status, and stream output of any executable on the host machine.

---

## Scope

**In scope (Level 5):**
- Worker library: start/stop/status/output streaming
- gRPC API with mTLS authentication and simple RBAC authorization
- Output streaming from the start of execution to N concurrent clients
- Process group termination (kill child processes on stop)
- cgroups v2 resource control: CPU, memory, disk I/O per job
- CLI client

**Out of scope:**
- Job persistence across server restarts (in-memory store)
- Job queuing / scheduling (jobs start immediately)
- Multi-node distribution
- User isolation (jobs run as the server process's user)

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  CLI (workerctl)                                     │
│  cobra + gRPC client stub                           │
└────────────────────┬────────────────────────────────┘
                     │ gRPC / mTLS (TLS 1.3)
┌────────────────────▼────────────────────────────────┐
│  gRPC Server                                         │
│  ├── UnaryInterceptor:  authz role check            │
│  ├── StreamInterceptor: authz role check            │
│  └── WorkerServiceServer impl                       │
│           │                                         │
│  ┌────────▼──────────────────────────────────────┐  │
│  │  Worker Library (internal/worker)             │  │
│  │  ├── Job registry  (sync.Map)                 │  │
│  │  ├── OutputStore   (append-only + sync.Cond)  │  │
│  │  └── cgroup v2     (/sys/fs/cgroup/…)         │  │
│  └───────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

---

## API (gRPC)

```protobuf
service WorkerService {
  rpc Start(StartRequest)             returns (StartResponse);
  rpc Stop(StopRequest)               returns (StopResponse);
  rpc Status(StatusRequest)           returns (StatusResponse);
  rpc StreamOutput(StreamOutputRequest) returns (stream OutputChunk);
}
```

### Messages

| RPC | Request fields | Response fields |
|---|---|---|
| Start | `command string`, `args []string`, `limits ResourceLimits` | `job_id string` |
| Stop  | `job_id string` | `ok bool` |
| Status | `job_id string` | `job_id`, `status`, `exit_code`, `started_at`, `ended_at` |
| StreamOutput | `job_id string` | stream of `OutputChunk { data bytes }` |

### Status enum
`PENDING → RUNNING → COMPLETED | FAILED | STOPPED`

---

## Security

### mTLS

Both server and client present X.509 certificates signed by the same CA.

**TLS configuration:**

```go
&tls.Config{
    MinVersion: tls.VersionTLS13,
    // TLS 1.3 cipher suites in Go are non-configurable; the runtime
    // automatically uses the three AEAD suites:
    //   TLS_AES_128_GCM_SHA256
    //   TLS_AES_256_GCM_SHA384
    //   TLS_CHACHA20_POLY1305_SHA256
    // No weak CBC or RC4 suites are negotiable.
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caPool,
    Certificates: []tls.Certificate{serverCert},
}
```

**Certificate generation (for testing):**
```bash
make certs
# produces: ca.crt, server.crt/key, admin.crt/key, viewer.crt/key
```

Certificates use:
- **Key algorithm:** ECDSA P-256 (256-bit security, compact signatures)
- **Signature hash:** SHA-256
- **Validity:** 1 year (production: use short-lived certs + automated rotation)

### Authorization

Roles are derived from the client certificate's **Subject Common Name (CN)**:

| CN | Role | Permitted RPCs |
|---|---|---|
| `admin` | Admin | Start, Stop, Status, StreamOutput |
| `viewer` | Viewer | Status, StreamOutput |
| anything else | — | Rejected with `codes.PermissionDenied` |

Authorization is enforced in gRPC interceptors (unary + streaming), never in handler
code, so no RPC can accidentally bypass the check.

---

## Output Streaming

### Design: append-only buffer + `sync.Cond`

The `OutputStore` is an append-only `[]byte` buffer. Each streaming client holds its own
cursor (`offset int`). When a client's offset equals `len(buf)` and the process is still
running, the client **blocks on `sync.Cond.Wait()`**. When new output arrives (or the
process exits), the writer calls `cond.Broadcast()` to wake all blocked readers.

```
OutputStore.buf:  [H e l l o \n W o r l d \n]
                   0              6           12

Reader A (offset=0):  gets all data immediately
Reader B (offset=12): blocks until more data or process exits
```

**Why not a ring buffer?**  
The challenge requires output *from start of execution*. A ring buffer evicts old data,
breaking late-joining clients. The append-only store grows with process output; acceptable
for typical process lifetimes. Production would cap at a configurable max size and return
a `ResourceExhausted` error to late joiners.

**Why not a channel fan-out?**  
A slow reader would stall the writer or drop messages. `sync.Cond` + per-reader cursors
gives each reader its own independent pace: a slow reader blocks itself but never blocks
the writer or other readers.

---

## Process Lifecycle

### Start

```
1. Generate UUID job ID
2. Create cgroup: /sys/fs/cgroup/job-worker/<id>/
3. Set resource limits (cpu.max, memory.max, io.max)
4. exec.Cmd with:
     SysProcAttr.Setpgid = true   ← new process group
     SysProcAttr.CgroupFD = cgroupFD  ← or write PID post-fork
     Stdout = outputStore (io.Writer)
     Stderr = outputStore (io.Writer)
5. cmd.Start()
6. goroutine: cmd.Wait() → update status, signal OutputStore.done
```

### Stop

```
1. Look up job by ID
2. If not running → return error
3. syscall.Kill(-pgid, syscall.SIGKILL)
   ↑ negative PID = kill the entire process group
     (catches child processes spawned by the job)
4. Wait for cmd.Wait() goroutine to update status → STOPPED
5. Cleanup cgroup directory
```

### cgroups v2

The server creates and manages cgroups entirely via the `/sys/fs/cgroup` filesystem.
No shell scripts, no external binaries, no libcgroup.

```
/sys/fs/cgroup/job-worker/
└── <job-id>/
    ├── cgroup.procs   ← write PID here to move process into cgroup
    ├── cpu.max        ← "50000 100000" = 50% of one CPU
    ├── memory.max     ← "268435456" = 256 MiB
    └── io.max         ← "8:0 rbps=10485760 wbps=10485760" = 10 MB/s r+w
```

Cleanup: `rmdir` the cgroup directory after the process exits (must be empty of tasks first).

**Prerequisite:** the server must run with sufficient privileges to write to
`/sys/fs/cgroup/job-worker/` (CAP_SYS_ADMIN or the cgroup directory delegated to
the server's user via `cgroup.subtree_control`).

---

## CLI UX

```bash
# Global flags (all commands)
workerctl --server localhost:8443 \
          --ca    certs/ca.crt   \
          --cert  certs/admin.crt \
          --key   certs/admin.key \
          <subcommand>

# Start a job (prints job ID)
$ workerctl start -- ls -la /tmp
abc123-def4-5678

# Start with resource limits
$ workerctl start --cpu 50 --mem 256 --iobps 10485760 -- stress --cpu 2 --timeout 30

# Check status
$ workerctl status abc123-def4-5678
ID:        abc123-def4-5678
Command:   ls -la /tmp
Status:    completed
Exit Code: 0
Started:   2024-01-10T10:00:00Z
Ended:     2024-01-10T10:00:01Z

# Stream output (blocks until process exits, then returns)
$ workerctl output abc123-def4-5678
total 24
drwxrwxrwt 8 root root 160 Jan 10 10:00 .
drwxr-xr-x 1 root root  60 Jan 10 09:00 ..

# Stop a running job
$ workerctl stop abc123-def4-5678
stopped

# Non-zero exit when job fails (useful for scripting)
$ workerctl status <failed-job-id>; echo $?
...
Status: failed
Exit Code: 1
1
```

---

## Alternatives Considered

| Decision | Alternative | Why rejected |
|---|---|---|
| gRPC server-streaming for output | REST long-poll or SSE | gRPC required by Level 3+; server-streaming is natural fit for binary data |
| append-only OutputStore | Ring buffer | Can't serve output-from-start to late joiners |
| `sync.Cond` for blocking readers | Channel fan-out | Slow readers block writers/other readers; Cond gives independent pacing |
| ECDSA P-256 certs | RSA-2048 | Smaller keys, faster handshake, equivalent security |
| Process group kill | SIGTERM then SIGKILL on PID only | Child processes (grandchildren) escape a single-PID kill |
| cgroups v2 direct FS writes | libcgroup / systemd | No external deps; direct writes are straightforward and testable |
| CN-based RBAC | JWT claims | mTLS already authenticates identity; extracting from cert avoids a second token layer |

---

## Testing Strategy

Key components with test coverage:

| Component | Happy path | Unhappy path |
|---|---|---|
| `OutputStore` | Concurrent readers get full output | Reader context cancelled mid-stream |
| `Worker.Start` | Process runs, output captured | Invalid command returns error |
| `Worker.Stop` | Job stops; child processes killed | Stop non-existent job |
| `Worker.Status` | Correct status transitions | Unknown job ID |
| `auth/authz` | Admin allowed on all RPCs | Viewer rejected on Start/Stop |
| `auth/tls` | Valid cert passes verification | Expired / wrong CA cert rejected |
| cgroup | Limits written correctly | Permission denied (non-root) |
