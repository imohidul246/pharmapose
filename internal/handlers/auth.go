package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// responsePrincipal is the shape of GET /api/auth/me and the login/register
// responses: everything the SPA needs to render, in one round trip.
type responsePrincipal struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	StoreID     string   `json:"store_id"`
	Permissions []string `json:"permissions"`
}

func toResponsePrincipal(p *auth.Principal) responsePrincipal {
	perms := make([]string, 0, len(auth.EmployeePermissions()))
	for perm := range auth.EmployeePermissions() {
		if auth.Can(p.Role, perm) {
			perms = append(perms, string(perm))
		}
	}
	return responsePrincipal{ID: p.UserID, Name: p.Name, Role: string(p.Role), StoreID: p.StoreID, Permissions: perms}
}

// POST /api/auth/register — bootstraps a brand-new tenant (owner login +
// business + optional GST registration + store + session). Registering is only
// meaningful before any store exists; afterwards the server is bound to the
// seeded store (single-tenant installs).
func (d *Deps) register(c *gin.Context) {
	var body struct {
		Name        string  `json:"name"`
		Phone       string  `json:"phone"`
		Password    string  `json:"password"`
		BusinessName string `json:"business_name"`
		TradeName   string  `json:"trade_name"`
		GSTIN       *string `json:"gstin"`
		PAN         *string `json:"pan"`
		StoreName   string  `json:"store_name"`
		StoreAddress string `json:"store_address"`
		StorePhone  string  `json:"store_phone"`
		DrugLicenseNumber string `json:"drug_license_number"`
		DrugLicenseExpiry *string `json:"drug_license_expiry"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondBadRequest(c, err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		respondBadRequest(c, err)
		return
	}

	rawToken, err := auth.NewSessionToken()
	if err != nil {
		respondInternal(c, err)
		return
	}

	in := repository.RegisterInput{
		Name:         body.Name,
		Phone:        body.Phone,
		PasswordHash: hash,
		BusinessName: body.BusinessName,
		TradeName:    body.TradeName,
		GSTIN:        body.GSTIN,
		PAN:          body.PAN,
		StoreName:    body.StoreName,
		StoreAddress: body.StoreAddress,
		StorePhone:   body.StorePhone,
		DrugLicenseNumber: body.DrugLicenseNumber,
		DrugLicenseExpiry: body.DrugLicenseExpiry,
	}
	res, err := d.AuthRepo.Register(c.Request.Context(), in, auth.HashSessionToken(rawToken), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if errors.Is(err, models.ErrEmployeeLimitReached) {
			respondConflict(c, err.Error())
			return
		}
		respondBadRequest(c, err)
		return
	}

	http.SetCookie(c.Writer, auth.SessionCookieValue(rawToken, d.CookieOptions))
	c.JSON(http.StatusOK, gin.H{"user": res.User, "principal": toResponsePrincipal(res.Principal)})
}

// POST /api/auth/login
func (d *Deps) login(c *gin.Context) {
	var in struct {
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	user, hash, err := d.AuthRepo.FindUserByPhone(c.Request.Context(), in.Phone)
	if err != nil {
		respondInternal(c, err)
		return
	}
	if user == nil {
		respondError(c, http.StatusUnauthorized, "invalid phone or password")
		return
	}
	ok, err := auth.VerifyPassword(in.Password, hash)
	if err != nil || !ok {
		respondError(c, http.StatusUnauthorized, "invalid phone or password")
		return
	}
	if !user.IsActive {
		respondError(c, http.StatusUnauthorized, "account is disabled")
		return
	}

	rawToken, err := auth.NewSessionToken()
	if err != nil {
		respondInternal(c, err)
		return
	}
	tokenHash := auth.HashSessionToken(rawToken)
	if _, err := d.AuthRepo.CreateSession(c.Request.Context(), user.ID, tokenHash, c.ClientIP(), c.Request.UserAgent()); err != nil {
		respondInternal(c, err)
		return
	}

	http.SetCookie(c.Writer, auth.SessionCookieValue(rawToken, d.CookieOptions))
	d.emitPrincipal(c, tokenHash)
}

// GET /api/auth/me
func (d *Deps) me(c *gin.Context) {
	p := currentPrincipal(c)
	if p == nil {
		respondError(c, http.StatusUnauthorized, auth.ErrUnauthorized.Error())
		return
	}
	user, err := d.AuthRepo.GetUser(c.Request.Context(), p.UserID)
	if err != nil {
		respondInternal(c, err)
		return
	}
	if user == nil {
		respondError(c, http.StatusUnauthorized, auth.ErrUnauthorized.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "principal": toResponsePrincipal(p)})
}

// POST /api/auth/logout
func (d *Deps) logout(c *gin.Context) {
	raw, err := c.Cookie(auth.CookieName)
	if err == nil && raw != "" {
		_ = d.AuthRepo.DeleteSession(c.Request.Context(), auth.HashSessionToken(raw))
	}
	http.SetCookie(c.Writer, auth.ExpiredSessionCookie(d.CookieOptions))
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// POST /api/auth/change-password — verifies the current password, rotates to a
// new hash and revokes every other session without interrupting this one.
func (d *Deps) changePassword(c *gin.Context) {
	p := currentPrincipal(c)
	if p == nil {
		respondError(c, http.StatusUnauthorized, auth.ErrUnauthorized.Error())
		return
	}
	var in struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}

	storedHash, err := d.AuthRepo.GetPasswordHash(c.Request.Context(), p.UserID)
	if err != nil {
		respondInternal(c, err)
		return
	}
	ok, err := auth.VerifyPassword(in.CurrentPassword, storedHash)
	if err != nil || !ok {
		respondError(c, http.StatusBadRequest, "current password is incorrect")
		return
	}
	newHash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		respondBadRequest(c, err)
		return
	}
	raw, _ := c.Cookie(auth.CookieName)
	if err := d.AuthRepo.ChangePassword(c.Request.Context(), p.UserID, newHash, auth.HashSessionToken(raw)); err != nil {
		respondInternal(c, err)
		return
	}
	if err := d.AuthRepo.InsertAudit(c.Request.Context(), p.StoreID, p.UserID, "auth.change_password", "user", p.UserID, nil); err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// emitPrincipal resolves the just-created session into the response principal.
func (d *Deps) emitPrincipal(c *gin.Context, tokenHash string) {
	p, err := d.AuthRepo.ValidateSession(c.Request.Context(), tokenHash)
	if err != nil {
		respondInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"principal": toResponsePrincipal(p)})
}