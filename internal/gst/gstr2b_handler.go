package gst

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// POST /api/gst/gstr2b/import
// body: {period, gstin/empty, source, store_id, docs:[{supplier_gstin, doc_type,
// invoice_no, invoice_date, taxable_value, igst, cgst, sgst, cess, total_value}]}
// Imports a GSTR-2B document set (GSTN's view of the pharmacy's supplier
// invoices) and reconciles each document against the purchase ledger.
func (h *Handler) ImportGSTR2B(c *gin.Context) {
	var in models.GSTR2BImportInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	rec, err := h.gstr2b.Import(c.Request.Context(), principalStoreID(c, storeID(in)), &in)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	c.JSON(http.StatusOK, rec)
}

// GET /api/gst/gstr2b/batches
func (h *Handler) ListGSTR2BBatches(c *gin.Context) {
	batches, err := h.gstr2b.ListBatches(c.Request.Context(), principalStoreID(c, c.Query("store_id")))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to list GSTR-2B imports: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, batches)
}

// GET /api/gst/gstr2b/batches/:id
func (h *Handler) GetGSTR2BBatch(c *gin.Context) {
	batchID := c.Param("id")
	if batchID == "" {
		respondError(c, http.StatusBadRequest, "batch id is required")
		return
	}
	ctx := c.Request.Context()
	store := principalStoreID(c, c.Query("store_id"))

	batch, err := h.gstr2b.GetBatch(ctx, store, batchID)
	if errors.Is(err, models.ErrNotFound) {
		respondError(c, http.StatusNotFound, "GSTR-2B import batch not found")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to load GSTR-2B batch: "+err.Error())
		return
	}

	docs, err := h.gstr2b.BatchDocs(ctx, store, batchID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "failed to load GSTR-2B documents: "+err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"batch": batch, "docs": docs})
}

// storeID extracts the store id from a GSTR-2B import input (client-supplied
// in the body), defaulting to an empty string when absent.
func storeID(in models.GSTR2BImportInput) string {
	if in.StoreID != nil {
		return *in.StoreID
	}
	return ""
}
