package apigateway

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ---- Tenant model ----------------------------------------------------------

// Plan describes what a tenant pays for and directly drives rate-limit quotas,
// feature flags, and SLO targets. Keeping plan data here (not in the auth
// token) means you can upgrade/downgrade a tenant without re-issuing tokens.
type Plan string

const (
	PlanFree       Plan = "free"       // 100 req/min
	PlanPro        Plan = "pro"        // 1 000 req/min
	PlanEnterprise Plan = "enterprise" // 10 000 req/min, custom SLO
)

// Tenant is the central identity in a multi-tenant system.
//
// Design note: TenantID should be a stable, opaque identifier (UUID v4) — never
// expose sequential integers (IDOR risk). The slug is human-readable but must
// not be treated as a security principal.
type Tenant struct {
	ID        string // opaque UUID, e.g. "t_01HX..."
	Slug      string // human-readable, e.g. "acme-corp"
	Plan      Plan
	Enabled   bool
	CreatedAt time.Time

	// RateLimitOverride lets enterprise customers negotiate custom quotas.
	// Zero means "use the plan default".
	RateLimitOverride int // requests per minute; 0 = use plan default
}

// RateLimit returns the effective req/min quota for this tenant.
func (t *Tenant) RateLimit() int {
	if t.RateLimitOverride > 0 {
		return t.RateLimitOverride
	}
	switch t.Plan {
	case PlanEnterprise:
		return 10_000
	case PlanPro:
		return 1_000
	default: // free
		return 100
	}
}

// ---- TenantStore interface -------------------------------------------------

// TenantStore is the read interface the gateway uses. The concrete
// implementation could be a Postgres query, a Redis cache, or both layered
// together (cache-aside pattern). Programming to an interface means the
// gateway is not coupled to any specific storage technology.
type TenantStore interface {
	Get(ctx context.Context, tenantID string) (*Tenant, error)
}

// ---- In-memory store (for teaching / unit tests) --------------------------

// MemTenantStore is a thread-safe in-memory TenantStore. In production you
// would replace this with a cached Postgres implementation.
//
// Cache-aside pattern in production:
//  1. Check Redis (TTL ~60s).
//  2. On miss, query Postgres.
//  3. Write result back to Redis.
//
// This keeps the gateway stateless (no local cache to invalidate) while still
// being fast. The gateway itself only holds an interface reference.
type MemTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*Tenant
}

func NewMemTenantStore(seeds ...*Tenant) *MemTenantStore {
	m := &MemTenantStore{tenants: make(map[string]*Tenant)}
	for _, t := range seeds {
		m.tenants[t.ID] = t
	}
	return m
}

var ErrTenantNotFound = errors.New("tenant not found")

func (s *MemTenantStore) Get(_ context.Context, tenantID string) (*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantID]
	if !ok {
		return nil, ErrTenantNotFound
	}
	if !t.Enabled {
		return nil, errors.New("tenant disabled")
	}
	return t, nil
}

func (s *MemTenantStore) Upsert(t *Tenant) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenants[t.ID] = t
}
