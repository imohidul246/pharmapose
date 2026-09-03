package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /api/sync/tax — store-scoped HSN + tax configuration snapshot for the
// frontend offline cache.
func (d Deps) getTaxConfigSync(c *gin.Context) {
	snapshot, err := d.TaxRepo.ListStoreTaxSnapshot(c.Request.Context(), storeIDFor(c))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"synced_at": timeNow(), "hsn_codes": snapshot.HSNCodes, "tax_configs": snapshot.TaxConfigs})
}

// GET /api/hsn — list all HSN codes for the current store.
func (d Deps) listHSNCodes(c *gin.Context) {
	codes, err := d.TaxRepo.ListHSNCodes(c.Request.Context(), storeIDFor(c))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"hsn_codes": codes})
}

// POST /api/hsn — create a new HSN code.
func (d Deps) createHSNCode(c *gin.Context) {
	var in struct {
		Code        string `json:"code" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	hsn, err := d.TaxRepo.CreateHSNCode(c.Request.Context(), storeIDFor(c), in.Code, in.Description)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusCreated, hsn)
}

// PUT /api/hsn/:id/tax-rate — upsert the active tax rate for an HSN code.
func (d Deps) upsertTaxRate(c *gin.Context) {
	var in struct {
		GSTRate  float64 `json:"gst_rate" binding:"required"`
		CGSTRate float64 `json:"cgst_rate" binding:"required"`
		SGSTRate float64 `json:"sgst_rate" binding:"required"`
		IGSTRate float64 `json:"igst_rate" binding:"required"`
		CessRate float64 `json:"cess_rate"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	tr, err := d.TaxRepo.UpsertTaxRate(c.Request.Context(), storeIDFor(c), c.Param("id"),
		in.GSTRate, in.CGSTRate, in.SGSTRate, in.IGSTRate, in.CessRate)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, tr)
}

// PUT /api/medicines/:id/tax-config — assign tax config to a medicine.
// Requires the medicine to belong to the current store.
func (d Deps) upsertMedicineTaxConfig(c *gin.Context) {
	if _, err := d.MedicineRepo.GetByID(c.Request.Context(), storeIDFor(c), c.Param("id")); err != nil {
		mapRepoError(c, err)
		return
	}
	var in struct {
		HSNCodeID        string `json:"hsn_code_id" binding:"required"`
		TaxRateID        string `json:"tax_rate_id" binding:"required"`
		PriceIncludesTax bool   `json:"price_includes_tax"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	cfg, err := d.TaxRepo.UpsertMedicineTaxConfig(c.Request.Context(), storeIDFor(c),
		c.Param("id"), in.HSNCodeID, in.TaxRateID, in.PriceIncludesTax)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// GET /api/medicines/:id/tax-config — get active tax config for a medicine.
func (d Deps) getMedicineTaxConfig(c *gin.Context) {
	cfg, err := d.TaxRepo.GetMedicineTaxConfigByMedicine(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if cfg == nil {
		c.JSON(http.StatusOK, nil)
		return
	}
	c.JSON(http.StatusOK, cfg)
}
