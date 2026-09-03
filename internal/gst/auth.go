package gst

import (
	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/auth"
)

// principalStoreID returns the authenticated principal's store, falling back
// to the client-supplied store id only when no session is present. Authenticated
// callers can never select another store.
func principalStoreID(c *gin.Context, fallback string) string {
	if p := auth.PrincipalFromContext(c.Request.Context()); p != nil && p.StoreID != "" {
		return p.StoreID
	}
	return fallback
}