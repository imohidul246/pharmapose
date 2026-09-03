package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// POST /api/sales/checkout — real-time billing with stock + credit enforcement.
func (d Deps) checkout(c *gin.Context) {
	var in repository.CheckoutInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	if p := currentPrincipal(c); p != nil {
		in.StoreID = &p.StoreID
	}

	result, err := d.SaleRepo.Checkout(c.Request.Context(), &in)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

// POST /api/purchases — supplier inward that upserts physical batches.
func (d Deps) createPurchase(c *gin.Context) {
	var in repository.PurchaseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	if p := currentPrincipal(c); p != nil {
		in.StoreID = &p.StoreID
		in.CreatedBy = &p.UserID
	}

	po, items, err := d.PurchaseRepo.CreateInward(c.Request.Context(), &in)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"purchase_order": po, "items": items})
}

func (d Deps) listPurchases(c *gin.Context) {
	start, end, err := bindDateRange(c, 30)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	report, err := d.ReportRepo.Purchases(c.Request.Context(), storeIDFor(c), start, end)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, report)
}
