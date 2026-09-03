package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (d Deps) salesReport(c *gin.Context) {
	start, end, err := bindDateRange(c, 30)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	report, err := d.ReportRepo.Sales(c.Request.Context(), storeIDFor(c), start, end)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, report)
}

func (d Deps) purchaseReport(c *gin.Context) {
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

func (d Deps) profitLossReport(c *gin.Context) {
	start, end, err := bindDateRange(c, 30)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	report, err := d.ReportRepo.ProfitLoss(c.Request.Context(), storeIDFor(c), start, end)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, report)
}

func (d Deps) expiryReport(c *gin.Context) {
	months := 6
	if v := c.Query("within_months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 120 {
			respondBadRequest(c, errors.New("within_months must be a positive integer (e.g. 3, 6, 12)"))
			return
		}
		months = n
	}
	batches, err := d.ReportRepo.Expiry(c.Request.Context(), storeIDFor(c), months)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"within_months": months, "batches": batches})
}

func (d Deps) lowStockReport(c *gin.Context) {
	items, err := d.ReportRepo.LowStock(c.Request.Context(), storeIDFor(c))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
