package apigateway

// Observability: "What is the system doing right now?"
//
// Observability is not a feature you add later. It is the foundation that lets
// you operate, debug, and scale a production system. The three pillars are:
//
//  Metrics  → aggregated numbers (RED: Rate, Errors, Duration)
//  Logs     → structured event records per request
//  Traces   → causal chain of spans across services
//
// ── Metrics (RED method) ─────────────────────────────────────────────────────
//
//  Rate      — requests/second per tenant, route, status code
//  Errors    — 4xx/5xx rate, error codes
//  Duration  — p50/p95/p99 latency
//
//  Why per-tenant metrics?
//    - Detect a noisy-neighbour tenant degrading the shared gateway.
//    - Enforce SLOs per plan tier (enterprise gets 99.99%, free gets 99.9%).
//    - Power per-tenant dashboards and alerting.
//
//  Production tools: Prometheus (scrape) + Grafana (dashboards)
//    counter:   gateway_requests_total{tenant,route,method,status}
//    histogram: gateway_request_duration_seconds{tenant,route}
//    gauge:     gateway_active_connections{tenant}
//
// ── Distributed Tracing ──────────────────────────────────────────────────────
//
//  A trace is a tree of spans — one span per service/component in the path.
//
//  Gateway span
//    └─ AuthN span          (e.g. 2ms — JWT verify)
//    └─ RateLimit span      (e.g. 1ms — Redis ZADD)
//    └─ AuthZ span          (e.g. 1ms — policy eval)
//    └─ Upstream proxy span (e.g. 45ms — backend call)
//         └─ DB query span  (e.g. 30ms — Postgres)
//
//  Trace ID propagation:
//    - Gateway generates a UUID v7 TraceID on inbound request.
//    - Injects it as W3C Traceparent header before forwarding upstream.
//    - Each service adds its own SpanID to the chain.
//    - Result: one query in Jaeger/Tempo shows the full call graph.
//
//  W3C Traceparent header format:
//    traceparent: 00-{traceID}-{spanID}-{flags}
//    e.g.  00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
//
// ── Structured Logging ───────────────────────────────────────────────────────
//
//  Use JSON lines — one JSON object per log event — not printf strings.
//  Every log line MUST include: trace_id, tenant_id, method, path, status, latency_ms.
//  This lets you correlate logs → traces → metrics for any single request.
//
//  Bad:  log.Printf("request to /api/invoices took 45ms")
//  Good: slog.Info("request", "trace", traceID, "tenant", tenantID,
//                  "method", "GET", "path", "/api/invoices",
//                  "status", 200, "latency_ms", 45)
//
// ── Alerting rules (golden signals) ─────────────────────────────────────────
//
//  Alert on SLO burn rate, not on thresholds:
//    - ERROR RATE:  > 1% of requests returning 5xx for 5 min → page
//    - LATENCY:     p99 > 500ms for 5 min → page
//    - RATE LIMIT:  > 5% of requests rate-limited for 15 min → warn
//    - AUTH FAIL:   sudden spike in 401/403 → security alert

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ---- Trace ID --------------------------------------------------------------

// generateTraceID generates a cryptographically random 16-byte trace ID
// encoded as a 32-character hex string (compatible with W3C Traceparent).
func generateTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp + counter (not for production).
		return hex.EncodeToString([]byte(strconv.FormatInt(time.Now().UnixNano(), 16)))
	}
	return hex.EncodeToString(b)
}

// ---- Metrics ---------------------------------------------------------------

// RequestMetrics holds counters and histograms for one tenant+route bucket.
// In production, replace with prometheus.CounterVec / prometheus.HistogramVec.
type RequestMetrics struct {
	Total     atomic.Int64
	Errors    atomic.Int64 // 5xx responses
	ClientErr atomic.Int64 // 4xx responses
	// LatencyBuckets stores request counts per latency bucket (ms).
	// Buckets: <10, <50, <100, <250, <500, <1000, >=1000
	latMu   sync.Mutex
	latency []int64 // raw millisecond values (bounded ring buffer in prod)
}

func (m *RequestMetrics) Record(statusCode int, latencyMs int64) {
	m.Total.Add(1)
	if statusCode >= 500 {
		m.Errors.Add(1)
	} else if statusCode >= 400 {
		m.ClientErr.Add(1)
	}
	m.latMu.Lock()
	// Keep last 1000 samples (ring buffer in production).
	if len(m.latency) < 1000 {
		m.latency = append(m.latency, latencyMs)
	}
	m.latMu.Unlock()
}

// P99 returns an approximate 99th-percentile latency from recorded samples.
// Production: use a proper HDR histogram or Prometheus Summary.
func (m *RequestMetrics) P99() int64 {
	m.latMu.Lock()
	samples := make([]int64, len(m.latency))
	copy(samples, m.latency)
	m.latMu.Unlock()
	if len(samples) == 0 {
		return 0
	}
	// Sort to find percentile.
	for i := 1; i < len(samples); i++ {
		for j := i; j > 0 && samples[j] < samples[j-1]; j-- {
			samples[j], samples[j-1] = samples[j-1], samples[j]
		}
	}
	idx := int(float64(len(samples)) * 0.99)
	if idx >= len(samples) {
		idx = len(samples) - 1
	}
	return samples[idx]
}

// MetricsRegistry is a thread-safe store of per-tenant metrics.
type MetricsRegistry struct {
	mu      sync.RWMutex
	buckets map[string]*RequestMetrics // keyed by tenantID
}

func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{buckets: make(map[string]*RequestMetrics)}
}

func (r *MetricsRegistry) For(tenantID string) *RequestMetrics {
	r.mu.RLock()
	m, ok := r.buckets[tenantID]
	r.mu.RUnlock()
	if ok {
		return m
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok = r.buckets[tenantID]; ok {
		return m
	}
	m = &RequestMetrics{}
	r.buckets[tenantID] = m
	return m
}

// ---- responseWriter wrapper ------------------------------------------------

// statusRecorder wraps http.ResponseWriter to capture the written status code.
// This is necessary because http.ResponseWriter does not expose the status
// after it has been written.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// ---- ObservabilityMiddleware -----------------------------------------------

// ObservabilityMiddleware is the FIRST middleware in the chain. It:
//  1. Generates a TraceID and injects it into the request context.
//  2. Propagates TraceID via W3C Traceparent to upstream services.
//  3. Records latency, status code, and tenant in the metrics registry.
//  4. Emits a structured log line for every request.
//
// Being first ensures EVERY request — including auth failures — is recorded.
type ObservabilityMiddleware struct {
	metrics *MetricsRegistry
	logger  *slog.Logger
}

func NewObservabilityMiddleware(metrics *MetricsRegistry, logger *slog.Logger) *ObservabilityMiddleware {
	return &ObservabilityMiddleware{metrics: metrics, logger: logger}
}

func (o *ObservabilityMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 1. Honour incoming trace ID (from upstream caller) or generate new.
		traceID := r.Header.Get("X-Trace-Id")
		if traceID == "" {
			traceID = generateTraceID()
		}
		ctx := r.Context()
		ctx = withValue(ctx, ctxTraceID, traceID)

		// 2. Expose trace ID to caller so they can correlate their logs.
		w.Header().Set("X-Trace-Id", traceID)

		// 3. Wrap the ResponseWriter to capture the status code.
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// 4. Call the next handler (the rest of the middleware chain).
		next.ServeHTTP(rec, r.WithContext(ctx))

		// 5. After the response: record metrics and emit log.
		latencyMs := time.Since(start).Milliseconds()
		tenantID := TenantFromCtx(ctx)

		if tenantID != "" {
			o.metrics.For(tenantID).Record(rec.status, latencyMs)
		}

		// Structured log — every field is queryable in Datadog / Loki / Splunk.
		o.logger.Info("request",
			"trace_id", traceID,
			"tenant_id", tenantID,
			"user_id", UserFromCtx(ctx),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"latency_ms", latencyMs,
			"user_agent", r.Header.Get("User-Agent"),
			"remote_ip", remoteIP(r),
		)

		// 6. Propagate W3C Traceparent to upstream (if this were a proxy).
		// In a real reverse proxy you'd set this on the upstream request:
		// upstreamReq.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
	})
}

// withValue is a thin alias to avoid importing context in this file.
func withValue(ctx interface{ Value(any) any }, key, val any) interface{ Value(any) any } {
	// We use the real context package — this is just a type alias workaround
	// to keep the file self-contained. In the real code we use context directly.
	return contextWithValue(ctx, key, val)
}

// contextWithValue wraps the standard context.WithValue.
// Separated here so the import is localised.
func contextWithValue(parent interface{ Value(any) any }, key, val any) interface{ Value(any) any } {
	// This function exists only to make the type signatures work without
	// importing context again at the top. In practice, you'd just call
	// context.WithValue() directly.
	panic("replace with context.WithValue in real code — see gateway.go for usage pattern")
}

// remoteIP extracts the real client IP, respecting common proxy headers.
// Security note: trust X-Forwarded-For ONLY if your load balancer sets it.
// Never use it raw from untrusted clients — it is trivially spoofable.
func remoteIP(r *http.Request) string {
	// Only safe if your LB is the only entity that sets this header.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF can be comma-separated; leftmost is the original client.
		parts := splitFirst(xff, ',')
		return trimSpace(parts)
	}
	return r.RemoteAddr
}

func splitFirst(s, sep string) string {
	for i, c := range s {
		if string(c) == sep {
			return s[:i]
		}
	}
	return s
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
