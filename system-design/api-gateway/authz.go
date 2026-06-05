package apigateway

// Authorization (AuthZ): "Are you allowed to do this?"
//
// AuthN answers "who are you?". AuthZ answers "what can you do?"
// They are deliberately separate because:
//   - A valid token proves identity, NOT permission.
//   - Permissions change frequently (role assignments, plan limits).
//   - Keeping them separate allows permission changes without re-issuing tokens.
//
// ── Models ───────────────────────────────────────────────────────────────────
//
//  RBAC (Role-Based Access Control)
//    Users are assigned roles. Roles have permissions.
//    e.g.: role "admin" → {read, write, delete}
//          role "viewer" → {read}
//    Simple, auditable, works for 90% of SaaS products.
//
//  ABAC (Attribute-Based Access Control)
//    Policies evaluate attributes: user.department == resource.owner.department
//    Flexible but complex. Use when RBAC cannot express the rules.
//    Tools: Open Policy Agent (OPA), Cedar (AWS).
//
//  ReBAC (Relationship-Based Access Control)
//    Permissions derived from graph relationships.
//    e.g.: user X owns document D, so X can edit D.
//    Used by Google Zanzibar (Docs/Drive), Airbnb, Notion.
//    Scales to billions of objects but adds architectural complexity.
//
// ── Multi-tenant RBAC design choices ─────────────────────────────────────────
//
//  1. Roles are SCOPED to a tenant. "admin" in Tenant A ≠ "admin" in Tenant B.
//  2. Permissions are expressed as "<resource>:<action>" strings.
//     e.g. "invoices:read", "users:write", "billing:admin"
//  3. A super-admin role exists at the platform level (not per-tenant) to
//     allow internal support/ops access. Keep this separate from tenant roles.
//  4. Store policies in a database, not in code. That way you can update them
//     without a deployment.
//
// ── Enforcement points ───────────────────────────────────────────────────────
//
//  Gateway  - coarse-grained: "can this tenant access /api/invoices at all?"
//  Service  - fine-grained:   "can user X read invoice #42 owned by org Y?"
//
//  The gateway does not have business-context (e.g. record ownership), so it
//  enforces at the resource-type + action level. Fine-grained enforcement lives
//  in the downstream service.

import (
	"net/http"
	"strings"
	"sync"
)

// ---- Permission model ------------------------------------------------------

// Permission is a "<resource>:<action>" string.
// Using strings keeps the model extensible without code changes.
type Permission string

const (
	PermRead    Permission = "*:read"
	PermWrite   Permission = "*:write"
	PermDelete  Permission = "*:delete"
	PermBilling Permission = "billing:admin"
)

// Role is a named set of permissions.
type Role struct {
	Name        string
	Permissions []Permission
}

// BuiltinRoles defines the platform default roles.
// In production, tenants may also define custom roles stored in the DB.
var BuiltinRoles = map[string]*Role{
	"viewer": {
		Name:        "viewer",
		Permissions: []Permission{PermRead},
	},
	"editor": {
		Name:        "editor",
		Permissions: []Permission{PermRead, PermWrite},
	},
	"admin": {
		Name:        "admin",
		Permissions: []Permission{PermRead, PermWrite, PermDelete, PermBilling},
	},
}

// ---- Policy store ----------------------------------------------------------

// PolicyStore maps (tenantID, roleID) → Role with its permissions.
// In production this is backed by Postgres / OPA. The interface allows you to
// swap implementations (e.g. for testing or for enterprise "custom roles").
type PolicyStore interface {
	RolePermissions(tenantID, roleID string) []Permission
}

// MemPolicyStore is a simple in-memory PolicyStore for teaching.
// Tenant-scoped roles override built-ins; falls back to BuiltinRoles.
type MemPolicyStore struct {
	mu    sync.RWMutex
	roles map[string]map[string]*Role // [tenantID][roleID]
}

func NewMemPolicyStore() *MemPolicyStore {
	return &MemPolicyStore{roles: make(map[string]map[string]*Role)}
}

func (s *MemPolicyStore) AddRole(tenantID string, role *Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roles[tenantID] == nil {
		s.roles[tenantID] = make(map[string]*Role)
	}
	s.roles[tenantID][role.Name] = role
}

func (s *MemPolicyStore) RolePermissions(tenantID, roleID string) []Permission {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Tenant-specific override first.
	if tenantRoles, ok := s.roles[tenantID]; ok {
		if r, ok := tenantRoles[roleID]; ok {
			return r.Permissions
		}
	}
	// Fall back to built-in roles.
	if r, ok := BuiltinRoles[roleID]; ok {
		return r.Permissions
	}
	return nil
}

// ---- Permission checker ----------------------------------------------------

// hasPermission checks if any of the given roles grant the required permission.
// It supports wildcard matching: "invoices:read" is granted by "*:read".
func hasPermission(store PolicyStore, tenantID string, roles []string, required Permission) bool {
	for _, roleID := range roles {
		for _, p := range store.RolePermissions(tenantID, roleID) {
			if matchPermission(p, required) {
				return true
			}
		}
	}
	return false
}

// matchPermission checks if granted matches required, supporting "*" wildcards.
//
//	granted="*:read",    required="invoices:read"  → true
//	granted="invoices:*", required="invoices:read" → true
//	granted="billing:admin", required="invoices:read" → false
func matchPermission(granted, required Permission) bool {
	if granted == required {
		return true
	}
	gp := strings.SplitN(string(granted), ":", 2)
	rp := strings.SplitN(string(required), ":", 2)
	if len(gp) != 2 || len(rp) != 2 {
		return false
	}
	resourceMatch := gp[0] == "*" || gp[0] == rp[0]
	actionMatch := gp[1] == "*" || gp[1] == rp[1]
	return resourceMatch && actionMatch
}

// ---- Route permission map --------------------------------------------------

// routePermission maps HTTP method + path prefix to required permission.
// In production, this might come from a config file or service registry.
//
// Pattern: METHOD /path/prefix → Permission
// Matching is longest-prefix; more specific rules win.
var routePermissions = []struct {
	method string
	prefix string
	perm   Permission
}{
	{"GET", "/api/", PermRead},
	{"POST", "/api/", PermWrite},
	{"PUT", "/api/", PermWrite},
	{"PATCH", "/api/", PermWrite},
	{"DELETE", "/api/", PermDelete},
	{"POST", "/api/billing/", PermBilling},
}

// requiredPermission returns the Permission needed for a given request.
// Returns empty string if no rule matches (allow-all for unprotected routes).
func requiredPermission(r *http.Request) Permission {
	var best struct {
		perm   Permission
		prefix string
	}
	for _, rule := range routePermissions {
		if rule.method != r.Method {
			continue
		}
		if strings.HasPrefix(r.URL.Path, rule.prefix) && len(rule.prefix) > len(best.prefix) {
			best.perm = rule.perm
			best.prefix = rule.prefix
		}
	}
	return best.perm
}

// ---- AuthZ middleware -------------------------------------------------------

// AuthZMiddleware enforces RBAC rules. Must run AFTER AuthN middleware has
// populated ctx with Claims (which carry the user's roles).
type AuthZMiddleware struct {
	policies PolicyStore
}

func NewAuthZMiddleware(policies PolicyStore) *AuthZMiddleware {
	return &AuthZMiddleware{policies: policies}
}

func (a *AuthZMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ctxClaims).(*Claims)
		if claims == nil {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "no identity in context")
			return
		}

		required := requiredPermission(r)
		if required == "" {
			// No permission rule for this route — allow (e.g. health checks).
			next.ServeHTTP(w, r)
			return
		}

		if !hasPermission(a.policies, claims.TenantID, claims.Roles, required) {
			writeError(w, http.StatusForbidden, "FORBIDDEN",
				"insufficient permissions for this operation")
			return
		}

		next.ServeHTTP(w, r)
	})
}
