package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /api/sales/invoices?start_date=&end_date=&q= — searchable sales history.
func (d Deps) listSalesInvoices(c *gin.Context) {
	start, end, err := bindDateRange(c, 30)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	invoices, err := d.SaleRepo.ListInvoices(c.Request.Context(), storeIDFor(c), start, end, c.Query("q"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"invoices": invoices})
}

// GET /api/sales/invoices/:id — full detail for one sales invoice.
func (d Deps) getSalesInvoice(c *gin.Context) {
	detail, err := d.SaleRepo.GetInvoice(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GET /api/sales/invoices/resolve?customer_id=&invoice_no= — resolve a sales
// invoice by the number printed on it (ledger notes only carry the number).
func (d Deps) getSalesInvoiceByNo(c *gin.Context) {
	detail, err := d.SaleRepo.GetInvoiceByNo(
		c.Request.Context(), storeIDFor(c), c.Query("customer_id"), c.Query("invoice_no"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GET /api/purchases/invoices?start_date=&end_date=&q= — searchable inward history.
func (d Deps) listPurchaseInvoices(c *gin.Context) {
	start, end, err := bindDateRange(c, 30)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	invoices, err := d.PurchaseRepo.ListInvoices(c.Request.Context(), storeIDFor(c), start, end, c.Query("q"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"invoices": invoices})
}

// GET /api/purchases/invoices/:id — full detail for one purchase invoice.
func (d Deps) getPurchaseInvoice(c *gin.Context) {
	detail, err := d.PurchaseRepo.GetInvoice(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, detail)
}
