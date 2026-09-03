package gst

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GET /api/gst/gstr3b?period=2026-08&store_id=
// Computes the GSTR-3B monthly summary for the given return period. All
// figures are derived from the transaction-layer GST snapshots; nothing is
// recomputed from current master data.
func (h *Handler) GetGSTR3B(c *gin.Context) {
	period := c.Query("period")
	if period == "" {
		respondError(c, http.StatusBadRequest, "period is required (YYYY-MM)")
		return
	}
	if _, err := time.Parse("2006-01", period); err != nil {
		respondError(c, http.StatusBadRequest, "period must be YYYY-MM")
		return
	}

	storeID := principalStoreID(c, c.Query("store_id"))
	ctx := c.Request.Context()

	gstin, stateCode, err := h.fetchStoreGSTContext(ctx, storeID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not resolve GST registration for store")
		return
	}

	g3b, err := h.gstr3b.Build(ctx, GSTR3BRequest{
		StoreID:   storeID,
		Period:    period,
		GSTIN:     gstin,
		StateCode: stateCode,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to build GSTR-3B: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, g3b)
}
