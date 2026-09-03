package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// parsePagination parses ?page= and ?limit= with defaults limit=100, page=1.
// Limits are clamped to [1,200]; pages <1 become 1. It returns limit, offset, page.
func parsePagination(c *gin.Context) (limit, offset, page int) {
	limit = 100
	page = 1
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			page = n
		}
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	if page < 1 {
		page = 1
	}
	offset = (page - 1) * limit
	return limit, offset, page
}

// POST /api/purchase-requests — an employee (or owner) submits a proposed
// purchase inward for review. Stock is not mutated here; the snapshot is
// approved (and posted) by the owner later.
func (d *Deps) createPurchaseRequest(c *gin.Context) {
	p := currentPrincipal(c)
	var in repository.PurchaseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	in.StoreID = &p.StoreID
	in.CreatedBy = &p.UserID

	req, err := d.PurchaseRequestRepo.Create(c.Request.Context(), p.StoreID, p.UserID, &in)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"request": req})
}

// GET /api/purchase-requests?status=PENDING&page=1&limit=100
func (d *Deps) listPurchaseRequests(c *gin.Context) {
	p := currentPrincipal(c)
	limit, offset, page := parsePagination(c)
	reqs, total, err := d.PurchaseRequestRepo.List(c.Request.Context(), p.StoreID, c.Query("status"), limit, offset)
	if err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"requests": reqs, "total_count": total, "limit": limit, "page": page})
}

// GET /api/purchase-requests/:id
func (d *Deps) getPurchaseRequest(c *gin.Context) {
	p := currentPrincipal(c)
	req, err := d.PurchaseRequestRepo.Get(c.Request.Context(), p.StoreID, c.Param("id"))
	if err != nil {
		if err == models.ErrNotFound {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}

// POST /api/purchase-requests/:id/approve — owner posts the snapshotted
// inward and marks the request approved, atomically.
func (d *Deps) approvePurchaseRequest(c *gin.Context) {
	p := currentPrincipal(c)
	if !assertOwner(c, p) {
		return
	}
	po, items, err := d.PurchaseRequestRepo.Approve(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "purchase_request.approve", "purchase_request", c.Param("id"), nil); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"purchase_order": po, "items": items})
}

// POST /api/purchase-requests/:id/reject — owner rejects the request.
func (d *Deps) rejectPurchaseRequest(c *gin.Context) {
	p := currentPrincipal(c)
	if !assertOwner(c, p) {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in)
	req, err := d.PurchaseRequestRepo.Reject(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID, in.Reason)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}

// POST /api/purchase-requests/:id/cancel — the requester withdraws their own
// pending request.
func (d *Deps) cancelPurchaseRequest(c *gin.Context) {
	p := currentPrincipal(c)
	req, err := d.PurchaseRequestRepo.Cancel(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}

// POST /api/stock-audit-requests — an employee counts physical stock against
// batches; the owner's approval applies the reconciliation.
func (d *Deps) createStockAuditRequest(c *gin.Context) {
	p := currentPrincipal(c)
	var in struct {
		Notes string                         `json:"notes"`
		Items []repository.AuditItemInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	req, items, err := d.StockAuditRequestRepo.Create(c.Request.Context(), p.StoreID, p.UserID, in.Notes, in.Items)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"request": req, "items": items})
}

// GET /api/stock-audit-requests?status=PENDING&page=1&limit=100
func (d *Deps) listStockAuditRequests(c *gin.Context) {
	p := currentPrincipal(c)
	limit, offset, page := parsePagination(c)
	reqs, total, err := d.StockAuditRequestRepo.List(c.Request.Context(), p.StoreID, c.Query("status"), limit, offset)
	if err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"requests": reqs, "total_count": total, "limit": limit, "page": page})
}

// GET /api/stock-audit-requests/:id
func (d *Deps) getStockAuditRequest(c *gin.Context) {
	p := currentPrincipal(c)
	req, items, err := d.StockAuditRequestRepo.Get(c.Request.Context(), p.StoreID, c.Param("id"))
	if err != nil {
		if err == models.ErrNotFound {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req, "items": items})
}

// POST /api/stock-audit-requests/:id/approve — owner applies the counted
// reconciliation. Rejected with 409 if live stock moved since the count
// (ErrStaleStock).
func (d *Deps) approveStockAuditRequest(c *gin.Context) {
	p := currentPrincipal(c)
	if !assertOwner(c, p) {
		return
	}
	journal, items, err := d.StockAuditRequestRepo.Approve(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "stock_audit.approve", "stock_audit_request", c.Param("id"), nil); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"journal": journal, "items": items})
}

// POST /api/stock-audit-requests/:id/reject — owner rejects the audit.
func (d *Deps) rejectStockAuditRequest(c *gin.Context) {
	p := currentPrincipal(c)
	if !assertOwner(c, p) {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&in)
	req, err := d.StockAuditRequestRepo.Reject(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID, in.Reason)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}

// POST /api/stock-audit-requests/:id/cancel — the requester withdraws their
// own pending request.
func (d *Deps) cancelStockAuditRequest(c *gin.Context) {
	p := currentPrincipal(c)
	req, err := d.StockAuditRequestRepo.Cancel(c.Request.Context(), p.StoreID, c.Param("id"), p.UserID)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}