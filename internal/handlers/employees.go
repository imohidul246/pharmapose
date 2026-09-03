package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// GET /api/employees — owner-only member roster.
func (d *Deps) listEmployees(c *gin.Context) {
	p := currentPrincipal(c)
	members, err := d.AuthRepo.ListMembers(c.Request.Context(), p.StoreID)
	if err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}

// POST /api/employees — owner invites a staff member. Creating the user is the
// invite: the owner assigns the initial password and hands it to the employee
// out of band. The seat cap is enforced atomically in the repository.
func (d *Deps) inviteEmployee(c *gin.Context) {
	p := currentPrincipal(c)
	var in struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	if in.Name == "" || in.Phone == "" {
		respondError(c, http.StatusBadRequest, "name and phone are required")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	user, err := d.AuthRepo.CreateEmployee(c.Request.Context(), p.StoreID, in.Name, in.Phone, hash)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "employee.invite", "user", user.ID, gin.H{"member_id": user.ID}); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// DELETE /api/employees/:userID — owner deactivates a member. The seat is
// released and all of that member's sessions are revoked.
func (d *Deps) deactivateEmployee(c *gin.Context) {
	p := currentPrincipal(c)
	userID := c.Param("userID")
	if userID == "" {
		respondError(c, http.StatusBadRequest, "user id is required")
		return
	}
	if err := d.AuthRepo.SetMembershipActive(c.Request.Context(), p.StoreID, userID, false); err != nil {
		mapRepoError(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "employee.deactivate", "user", userID, gin.H{"member_id": userID}); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GET /api/store — owner view of the full shop profile (name, address, phone,
// owner name, GSTIN, PAN, drug license, seat limit).
func (d *Deps) getStore(c *gin.Context) {
	p := currentPrincipal(c)
	store, err := d.AuthRepo.GetStoreDetails(c.Request.Context(), p.StoreID)
	if err != nil {
		if err == models.ErrNotFound {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"store": store})
}

// PUT /api/store — owner updates shop/business details. Mandatory fields
// (name, owner name, phone, address) may change but never save empty; optional
// business info (GSTIN, PAN, drug license) may be filled, changed or cleared.
func (d *Deps) updateStore(c *gin.Context) {
	p := currentPrincipal(c)
	var in struct {
		Name               string  `json:"name"`
		Address            string  `json:"address"`
		Phone              string  `json:"phone"`
		OwnerName          string  `json:"owner_name"`
		MaxEmployees       int     `json:"max_employees"`
		GSTIN              *string `json:"gstin"`
		PAN                *string `json:"pan"`
		DrugLicenseNumber  string  `json:"drug_license_number"`
		DrugLicenseExpiry  *string `json:"drug_license_expiry"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	store, err := d.AuthRepo.UpdateStoreDetails(c.Request.Context(), p.StoreID, repository.StoreUpdate{
		Name:               in.Name,
		Address:            in.Address,
		Phone:              in.Phone,
		OwnerName:          in.OwnerName,
		MaxEmployees:       in.MaxEmployees,
		GSTIN:              in.GSTIN,
		PAN:                in.PAN,
		DrugLicenseNumber:  in.DrugLicenseNumber,
		DrugLicenseExpiry:  in.DrugLicenseExpiry,
	})
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "store.settings.update", "store", p.StoreID, gin.H{"max_employees": in.MaxEmployees}); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"store": store})
}