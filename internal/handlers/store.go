package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/auth"
)

// storeIDFor returns the store scope for the current request. The
// authenticated principal's store is authoritative: a client-provided
// store_id is never trusted on a protected route. The client-supplied value
// (query param `store_id` or JSON body field) is only used as a fallback on
// bootstrapping/unauthenticated flows.
func storeIDFor(c *gin.Context) string {
	if p := auth.PrincipalFromContext(c.Request.Context()); p != nil && p.StoreID != "" {
		return p.StoreID
	}

	if v := c.Query("store_id"); v != "" {
		return v
	}

	if c.Request.Method == http.MethodGet || c.Request.Body == nil {
		return ""
	}

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))

	var body struct {
		StoreID string `json:"store_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	return body.StoreID
}

// currentPrincipal is a convenience accessor matching the middleware.
func currentPrincipal(c *gin.Context) *auth.Principal {
	return auth.PrincipalFromContext(c.Request.Context())
}