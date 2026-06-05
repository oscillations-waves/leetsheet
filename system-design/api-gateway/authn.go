package apigateway

// Authentication (AuthN): "Who are you?"
//
// The gateway supports two credential types:
//
//   1. JWT Bearer token  — issued by an Identity Provider (IdP) such as Auth0,
//      Cognito, or a self-hosted OIDC server. Carries claims including sub
//      (user ID), tid (tenant ID), and roles[].
//
//   2. API key           — a long random secret, e.g. "sk_live_…". Mapped in a
//      store to a TenantID + UserID. Used for server-to-server automation.
//
// ── JWT validation deep-dive ─────────────────────────────────────────────────
//
//  A JWT has three parts: header.payload.signature (base64url encoded).
//
//  header:  {"alg":"RS256","kid":"key-001"}
//  payload: {"sub":"user_xyz","tid":"tenant_abc","roles":["admin"],"exp":…}
//  sig:     RSA/HMAC over header+payload
//
//  Verification steps (in order — skip any and you have a security hole):
//    1. Parse and decode header + payload.
//    2. Look up the public key by `kid` from the IdP's JWKS endpoint.
//    3. Verify signature using that key.
//    4. Check `exp` claim: token must not be expired.
//    5. Check `aud` claim: token must be issued for THIS service.
//    6. Check `iss` claim: must match your trusted IdP URL.
//    7. Extract `tid` (tenant ID) and `sub` (user ID) into context.
//
//  JWKS caching: fetch the IdP's JWKS once, cache for ~5 min. On a `kid` miss,
//  re-fetch once (key rotation). Never skip verification for a "valid-looking"
//  token — that is the top JWT security mistake.
//
// ── API key design ────────────────────────────────────────────────────────────
//
//  Format: sk_{env}_{random32bytes_base58}
//    - Prefix  "sk_live_" / "sk_test_" makes the type obvious.
//    - 32 random bytes = 256-bit entropy — practically unguessable.
//
//  Storage: NEVER store the raw key. Store SHA-256(key) in the DB.
//    - On presentation: hash the incoming key, look up the hash.
//    - If stolen from DB, attacker cannot reverse-engineer the key.
//    - Similar to how password hashing works.
//
//  Lookup: key hash → TenantID + UserID + scopes.
//  Invalidation: delete the hash row; the key is immediately revoked.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---- JWT claims ------------------------------------------------------------

// Claims is the minimal set of JWT claims the gateway cares about.
// A production implementation uses a proper OIDC library (e.g. go-jose).
type Claims struct {
	Subject  string   `json:"sub"` // user ID
	TenantID string   `json:"tid"` // tenant ID — custom claim
	Roles    []string `json:"roles"`
	Expiry   int64    `json:"exp"`
	Issuer   string   `json:"iss"`
	Audience string   `json:"aud"`
}

func (c *Claims) Expired() bool {
	return time.Now().Unix() > c.Expiry
}

// ---- JWT verifier ----------------------------------------------------------

// JWTVerifier validates JWTs using a symmetric HMAC-SHA256 key for teaching.
// In production, use RSA/ECDSA (asymmetric) so the gateway only holds the
// PUBLIC key — the private key never leaves the IdP.
//
// Why asymmetric in production?
//   - Any service holding the symmetric key can FORGE tokens.
//   - With RSA, services verify but cannot create.
type JWTVerifier struct {
	secret []byte // HMAC key (symmetric, for demo only)
	issuer string
	aud    string
}

func NewJWTVerifier(secret []byte, issuer, audience string) *JWTVerifier {
	return &JWTVerifier{secret: secret, issuer: issuer, aud: audience}
}

// Verify parses and validates a raw JWT string, returning Claims on success.
func (v *JWTVerifier) Verify(rawToken string) (*Claims, error) {
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed jwt: expected 3 parts")
	}

	// 1. Verify signature: HMAC-SHA256(header + "." + payload, secret)
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("invalid jwt signature")
	}

	// 2. Decode payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed jwt payload")
	}
	var c Claims
	if err := json.Unmarshal(payloadJSON, &c); err != nil {
		return nil, errors.New("malformed jwt claims")
	}

	// 3-6. Validate standard claims.
	if c.Expired() {
		return nil, errors.New("jwt expired")
	}
	if c.Issuer != v.issuer {
		return nil, errors.New("jwt issuer mismatch")
	}
	if c.Audience != v.aud {
		return nil, errors.New("jwt audience mismatch")
	}
	if c.TenantID == "" {
		return nil, errors.New("jwt missing tid claim")
	}

	return &c, nil
}

// ---- API key store ---------------------------------------------------------

// APIKeyRecord maps a hashed API key → tenant + user identity.
type APIKeyRecord struct {
	TenantID string
	UserID   string
	Roles    []string
}

// APIKeyStore is a thread-safe in-memory store for API keys.
// Production: Redis or Postgres, keys indexed by SHA-256 hash.
type APIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*APIKeyRecord // SHA-256 hex hash → record
}

func NewAPIKeyStore() *APIKeyStore {
	return &APIKeyStore{keys: make(map[string]*APIKeyRecord)}
}

// Register stores a new API key. The raw key is hashed; the raw value is NOT
// retained — callers must present the raw key on every request.
func (s *APIKeyStore) Register(rawKey string, rec *APIKeyRecord) {
	hash := hashKey(rawKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[hash] = rec
}

func (s *APIKeyStore) Lookup(rawKey string) (*APIKeyRecord, bool) {
	hash := hashKey(rawKey)
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.keys[hash]
	return rec, ok
}

func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

// ---- AuthN middleware -------------------------------------------------------

// AuthNMiddleware detects credential type, validates, and injects identity into
// the request context. Downstream middleware reads identity via helpers in
// gateway.go (TenantFromCtx, UserFromCtx).
//
// Credential detection order:
//  1. Authorization: Bearer <jwt>
//  2. Authorization: ApiKey <key>
//  3. X-API-Key: <key>   (legacy header, some clients prefer this)
type AuthNMiddleware struct {
	jwt    *JWTVerifier
	apiKey *APIKeyStore
}

func NewAuthNMiddleware(jwt *JWTVerifier, apiKey *APIKeyStore) *AuthNMiddleware {
	return &AuthNMiddleware{jwt: jwt, apiKey: apiKey}
}

func (a *AuthNMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := a.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *AuthNMiddleware) authenticate(r *http.Request) (context.Context, error) {
	authHeader := r.Header.Get("Authorization")
	switch {
	case strings.HasPrefix(authHeader, "Bearer "):
		return a.verifyJWT(r.Context(), strings.TrimPrefix(authHeader, "Bearer "))

	case strings.HasPrefix(authHeader, "ApiKey "):
		return a.verifyAPIKey(r.Context(), strings.TrimPrefix(authHeader, "ApiKey "))

	default:
		// Fallback: legacy X-API-Key header.
		if key := r.Header.Get("X-API-Key"); key != "" {
			return a.verifyAPIKey(r.Context(), key)
		}
		return nil, errors.New("no credentials provided")
	}
}

func (a *AuthNMiddleware) verifyJWT(ctx context.Context, rawToken string) (context.Context, error) {
	claims, err := a.jwt.Verify(rawToken)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, ctxTenantID, claims.TenantID)
	ctx = context.WithValue(ctx, ctxUserID, claims.Subject)
	ctx = context.WithValue(ctx, ctxClaims, claims)
	return ctx, nil
}

func (a *AuthNMiddleware) verifyAPIKey(ctx context.Context, rawKey string) (context.Context, error) {
	rec, ok := a.apiKey.Lookup(rawKey)
	if !ok {
		return nil, errors.New("invalid api key")
	}
	// Wrap roles into a Claims-compatible struct so authz sees a uniform type.
	ctx = context.WithValue(ctx, ctxTenantID, rec.TenantID)
	ctx = context.WithValue(ctx, ctxUserID, rec.UserID)
	ctx = context.WithValue(ctx, ctxClaims, &Claims{
		TenantID: rec.TenantID,
		Subject:  rec.UserID,
		Roles:    rec.Roles,
	})
	return ctx, nil
}
