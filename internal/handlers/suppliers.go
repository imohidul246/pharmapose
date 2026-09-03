package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

func (d Deps) listSuppliers(c *gin.Context) {
	suppliers, err := d.SupplierRepo.List(c.Request.Context(), storeIDFor(c))
	if err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, suppliers)
}

func (d Deps) createSupplier(c *gin.Context) {
	var s models.Supplier
	if err := c.ShouldBindJSON(&s); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := d.SupplierRepo.Create(c.Request.Context(), storeIDFor(c), &s); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			respondError(c, http.StatusNotFound, "not found")
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, s)
}

func (d Deps) getSupplier(c *gin.Context) {
	id := c.Param("id")
	s, err := d.SupplierRepo.GetByID(c.Request.Context(), storeIDFor(c), id)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			respondError(c, http.StatusNotFound, "supplier not found")
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

func (d Deps) updateSupplier(c *gin.Context) {
	id := c.Param("id")
	var s models.Supplier
	if err := c.ShouldBindJSON(&s); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	s.ID = id
	if err := d.SupplierRepo.Update(c.Request.Context(), storeIDFor(c), &s); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			respondError(c, http.StatusNotFound, "supplier not found")
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

func (d Deps) deleteSupplier(c *gin.Context) {
	id := c.Param("id")
	if err := d.SupplierRepo.Delete(c.Request.Context(), storeIDFor(c), id); err != nil {
		if errors.Is(err, models.ErrNotFound) {
			respondError(c, http.StatusNotFound, "supplier not found")
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
