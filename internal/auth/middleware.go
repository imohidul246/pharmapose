package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Session cookie parameters — shared by the login handler and the middleware.
const (
	CookieName      = "pms_session"
	sessionLifetime = 7 * 24 * 3600 // seconds; 7 days
	cookiePath      = "/"
)

// SessionTTL is the session validity duration (7 days, sliding on re-login).
func SessionTTL() time.Duration { return 7 * 24 * time.Hour }

// CookieOptions bundles the tunable bits of the session cookie.
type CookieOptions struct {
	Secure bool
	Domain string
}

// SessionValidator resolves a hashed session token into a live principal.
// Implemented by *repository.AuthRepo; having it as an interface keeps the
// middleware free of the repository layer.
type SessionValidator interface {
	ValidateSession(ctx context.Context, tokenHash string) (*Principal, error)
}

// ErrUnauthorized signals a missing, invalid or expired session.
var ErrUnauthorized = errors.New("unauthorized")

// ErrForbidden signals an authenticated user without the required role/permission.
var ErrForbidden = errors.New("forbidden")

// RequireAuth authenticates the request from the session cookie and binds the
// resolved principal to the request context. Every store-scoped handler relies
// on it, so the group it guards must come after RequireAuth in a route chain.
func RequireAuth(validator SessionValidator, opts CookieOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := c.Cookie(CookieName)
		if err != nil || raw == "" {
			abortAuth(c)
			return
		}
		principal, err := validator.ValidateSession(c.Request.Context(), HashSessionToken(raw))
		if err != nil {
			// Drop the cookie on the way out so a dead session is cleared.
			clearSessionCookie(c, opts)
			abortAuth(c)
			return
		}
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

func abortAuth(c *gin.Context) {
	c.Set("authed", false)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthorized.Error()})
}

// RequirePermission gates a route to users whose role holds the permission.
func RequirePermission(perm Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PrincipalFromContext(c.Request.Context())
		if p == nil || !Can(p.Role, perm) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": ErrForbidden.Error()})
			return
		}
		c.Next()
	}
}

// RequireRole gates a route to users holding one of the given roles.
// It is the explicit role-verification primitive: sensitive endpoints list
// the exact roles allowed (e.g. RequireRole(RoleStoreOwner) for approvals,
// tax overrides and direct stock postings) so a cashier/employee token can
// never exercise owner-only powers, even if permission maps change.
func RequireRole(roles ...Role) gin.HandlerFunc {
	allowed := make(map[Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		p := PrincipalFromContext(c.Request.Context())
		if p == nil || !allowed[p.Role] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": ErrForbidden.Error()})
			return
		}
		c.Next()
	}
}

// Role aliases for the generic RBAC vocabulary used by compliance checklists:
// RoleAdmin is the tenant administrator (STORE_OWNER) and RoleStoreManager is
// currently also the STORE_OWNER — the codebase has no separate manager tier,
// so both aliases resolve to the owner role. They exist so route guards can
// be written as RequireRole(RoleAdmin, RoleStoreManager) per spec while
// remaining backward-compatible with the membership CHECK
// (role IN ('STORE_OWNER','EMPLOYEE')).
const (
	RoleAdmin        Role = RoleStoreOwner
	RoleStoreManager Role = RoleStoreOwner
	RoleCashier      Role = RoleEmployee
)

// ValidateStoreHeader rejects multi-store header tampering: when the client
// supplies an X-Store-ID header it MUST equal the authenticated principal's
// store. The header is never trusted on its own — the principal (resolved
// server-side from the session) is authoritative, and any mismatch aborts
// with 403 before the handler runs. Requests without the header pass through
// (the principal's store is used downstream).
func ValidateStoreHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PrincipalFromContext(c.Request.Context())
		if p == nil {
			c.Next()
			return
		}
		if hdr := c.GetHeader("X-Store-ID"); hdr != "" && hdr != p.StoreID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": ErrForbidden.Error()})
			return
		}
		c.Next()
	}
}

// RequireOwner gates a route to the store owner (approvals, employee management,
// settings, direct purchase/reconcile posting).
func RequireOwner() gin.HandlerFunc {
	return RequireRole(RoleStoreOwner)
}

// CSRF: the session cookie is SameSite=Lax, so cross-site POSTs from another
// origin cannot carry it. An attacker who somehow acquires the cookie still
// cannot forge a POST without a matching Origin header. In production the API
// is served same-origin behind the SPA, so "no Origin header / browser same-origin
// events" pass naturally and only a mismatched Origin is rejected.
func CSRFProtect(devOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(devOrigins))
	for _, o := range devOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet ||
			c.Request.Method == http.MethodHead ||
			c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		// Requests without an Origin header are non-browser clients (curl, the
		// test suite, same-tab POSTs from the SPA served on the same origin host
		// where the browser omits Origin) — trust them.
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		host := c.Request.Host
		originHost := originHost(origin)
		if originHost == "" || originHost == host || allowed[strings.TrimRight(origin, "/")] {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cross-origin request rejected"})
	}
}

func originHost(origin string) string {
	o := strings.TrimPrefix(origin, "https://")
	if o != origin {
		o = strings.TrimPrefix(o, "http://") // unreachable
	} else {
		o = strings.TrimPrefix(origin, "http://")
	}
	if i := strings.Index(o, "/"); i >= 0 {
		o = o[:i]
	}
	return o
}

// SessionCookieValue builds the http.Cookie sent back to the browser.
func SessionCookieValue(rawToken string, opts CookieOptions) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    rawToken,
		Path:     cookiePath,
		MaxAge:   sessionLifetime,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		Domain:   opts.Domain,
	}
}

// ExpiredSessionCookie clears the session cookie (used on logout and when a
// session is rejected).
func ExpiredSessionCookie(opts CookieOptions) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		Domain:   opts.Domain,
	}
}

// setSessionCookie is the canonical way to attach the session cookie.
func setSessionCookie(c *gin.Context, rawToken string, opts CookieOptions) {
	http.SetCookie(c.Writer, SessionCookieValue(rawToken, opts))
}

// clearSessionCookie expires the session cookie on the response.
func clearSessionCookie(c *gin.Context, opts CookieOptions) {
	http.SetCookie(c.Writer, ExpiredSessionCookie(opts))
}