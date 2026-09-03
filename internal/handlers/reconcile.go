package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// POST /api/inventory/reconcile — physical audit correction (Phase 2.4).
func (d Deps) reconcile(c *gin.Context) {
	var in repository.ReconcileInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}

	journal, items, err := d.ReconcileRepo.Reconcile(c.Request.Context(), storeIDFor(c), &in)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, gin.H{"journal": journal, "items": items})
}

func (d Deps) listReconciliations(c *gin.Context) {
	limit := 0
	if v := c.Query("limit"); v != "" {
		n := 0
		for _, ch := range v {
			if ch < '0' || ch > '9' {
				respondBadRequest(c, errLimitInvalid)
				return
			}
			n = n*10 + int(ch-'0')
		}
		limit = n
	}

	journals, items, err := d.ReconcileRepo.ListJournals(c.Request.Context(), storeIDFor(c), limit)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"journals": journals, "items": items})
}
