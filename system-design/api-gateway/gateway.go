// Package apigateway teaches multi-tenant SaaS API gateway design.
//
// Architecture: each inbound HTTP request flows through a middleware chain:
//
//   [Request]
//      │
//      ▼
//   [Observability - attach trace ID, start timer]
//      │
//      ▼
//   [AuthN - validate JWT or API key → extract TenantID + UserID]
//      │
//      ▼
//   [Tenant Resolution - load tenant config/tier]
//      │
//      ▼
//   [Rate Limiter - per-tenant sliding-window counter]
//      │
//      ▼
//   [AuthZ - RBAC check: can this user perform this action?]
//      │
//      ▼
//   [Upstream Proxy - forward to backend microservice]
//
// Key design decisions:
//   - Context carries identity: TenantID, UserID, TraceID — never pass as naked args.
//   - Rate limiting is per-tenant (not per-IP) because tenants share IPs in SaaS.
//   - AuthN and AuthZ are deliberately separate: you can authenticate an API key
//     without knowing its permissions; those come from a separate policy store.
//   - Observability is the FIRST middleware so every request — even auth failures —
//     gets recorded.
package apigateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ---- Context keys ----------------------------------------------------------
// Use unexported types for context keys to prevent collisions with other pkgs.

type ctxKey int

const (
	ctxTenantID ctxKey = iota
	ctxUserID
	ctxTraceID
	ctxClaims
)

func TenantFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxTenantID).(string)
	return v
}

func UserFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxUserID).(string)
	return v
}

func TraceFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxTraceID).(string)
	return v
}

// ---- Middleware type -------------------------------------------------------

// Middleware wraps an http.Handler, returning a new http.Handler.
// Chaining: Build(h, mw1, mw2, mw3) → mw1(mw2(mw3(h)))
// Execution order: mw1 first, h last.
type Middleware func(http.Handler) http.Handler

// Build chains middlewares around a final handler.
// Outermost middleware runs first (left-to-right in the slice).
func Build(h http.Handler, mws ...Middleware) http.Handler {
	// Apply in reverse so mws[0] is outermost.
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// ---- Gateway ---------------------------------------------------------------

// Gateway wires together all middleware and the upstream proxy.
//
// Design choice: Gateway owns the dependency graph. Each middleware is a pure
// function — it receives its dependencies via closure, not global state. This
// makes every layer independently testable.
type Gateway struct {
	obs     *ObservabilityMiddleware
	authn   *AuthNMiddleware
	limiter *RateLimiter
	authz   *AuthZMiddleware
	tenants TenantStore
	proxy   http.Handler
}

func NewGateway(
	obs *ObservabilityMiddleware,
	authn *AuthNMiddleware,
	limiter *RateLimiter,
	authz *AuthZMiddleware,
	tenants TenantStore,
	upstream http.Handler,
) *Gateway {
	return &Gateway{obs, authn, limiter, authz, tenants, upstream}
}

// Handler returns the fully wired http.Handler for the gateway.
func (g *Gateway) Handler() http.Handler {
	return Build(
		g.proxy,
		g.obs.Middleware,       // 1. attach trace, start timer
		g.authn.Middleware,     // 2. validate credential → TenantID, UserID
		g.tenantMiddleware(),   // 3. load tenant config (tier, plan)
		g.limiter.Middleware,   // 4. check rate limit for this tenant
		g.authz.Middleware,     // 5. RBAC — can user do this?
	)
}

// tenantMiddleware loads the Tenant record and adds it to context.
// Placed AFTER authn so TenantID is already in context.
func (g *Gateway) tenantMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := TenantFromCtx(r.Context())
			tenant, err := g.tenants.Get(r.Context(), tenantID)
			if err != nil {
				slog.Error("tenant not found", "tenant_id", tenantID, "trace", TraceFromCtx(r.Context()))
				http.Error(w, "tenant not found", http.StatusUnauthorized)
				return
			}
			// Attach full tenant to context so downstream middleware (rate limiter,
			// authz) can read the tier/plan without a second DB lookup.
			ctx := context.WithValue(r.Context(), ctxTenantID, tenant.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---- Error helpers ---------------------------------------------------------

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"code":%q,"message":%q},"ts":%d}`, code, msg, time.Now().UnixMilli())
}
