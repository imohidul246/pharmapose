package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// GET /api/platform/stores — list every tenant with subscription metrics.
// Platform admin only; deliberately crosses tenant boundaries.
func (d *Deps) listPlatformStores(c *gin.Context) {
	if d.PlatformRepo == nil {
		respondInternal(c, errPlatformUnavailable())
		return
	}
	stores, err := d.PlatformRepo.ListStores(c.Request.Context())
	if err != nil {
		respondInternal(c, err)
		return
	}
	if stores == nil {
		stores = []models.PlatformStoreInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"stores": stores})
}

// POST /api/platform/stores/:id/renew — record an offline cash payment and
// extend the store's validity. Body: { plan_type, amount, notes }.
// When amount is omitted (0) the catalogue price for the plan is used.
func (d *Deps) renewPlatformStore(c *gin.Context) {
	if d.PlatformRepo == nil {
		respondInternal(c, errPlatformUnavailable())
		return
	}
	storeID := c.Param("id")
	if storeID == "" {
		respondError(c, http.StatusBadRequest, "store id is required")
		return
	}
	var in struct {
		PlanType string  `json:"plan_type"`
		Amount   float64 `json:"amount"`
		Notes    string  `json:"notes"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	if in.PlanType == "" {
		respondError(c, http.StatusBadRequest, "plan_type is required")
		return
	}
	if _, ok := models.PlanDays(in.PlanType); !ok {
		respondError(c, http.StatusBadRequest, "plan_type must be one of 1_MONTH, 6_MONTHS, 1_YEAR")
		return
	}
	amount := in.Amount
	if amount == 0 {
		if catalogue, ok := models.PlanAmount(in.PlanType); ok {
			amount = catalogue
		}
	}
	payment, err := d.PlatformRepo.RecordPaymentAndExtend(c.Request.Context(), storeID, in.PlanType, amount, in.Notes)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"payment": payment})
}

// PUT /api/platform/stores/:id/status — suspend or re-activate a store.
// Body: { status: "ACTIVE" | "SUSPENDED" }.
func (d *Deps) updatePlatformStoreStatus(c *gin.Context) {
	if d.PlatformRepo == nil {
		respondInternal(c, errPlatformUnavailable())
		return
	}
	storeID := c.Param("id")
	if storeID == "" {
		respondError(c, http.StatusBadRequest, "store id is required")
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	if in.Status != "ACTIVE" && in.Status != "SUSPENDED" {
		respondError(c, http.StatusBadRequest, "status must be ACTIVE or SUSPENDED")
		return
	}
	if err := d.PlatformRepo.UpdateStoreStatus(c.Request.Context(), storeID, in.Status); err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GET /api/platform/stores/:id/payments — cash-payment ledger for one store.
func (d *Deps) listPlatformStorePayments(c *gin.Context) {
	if d.PlatformRepo == nil {
		respondInternal(c, errPlatformUnavailable())
		return
	}
	storeID := c.Param("id")
	if storeID == "" {
		respondError(c, http.StatusBadRequest, "store id is required")
		return
	}
	payments, err := d.PlatformRepo.ListPayments(c.Request.Context(), storeID)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	if payments == nil {
		payments = []models.StoreSubscriptionPayment{}
	}
	c.JSON(http.StatusOK, gin.H{"payments": payments})
}

func errPlatformUnavailable() error {
	return errPlatformRepoMissing
}

// errPlatformRepoMissing is returned when the router was built without a
// platform repo (should never happen in production wiring).
var errPlatformRepoMissing = platformUnavailableError{}

type platformUnavailableError struct{}

func (platformUnavailableError) Error() string { return "platform repository is not configured" }
