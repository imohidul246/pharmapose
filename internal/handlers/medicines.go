package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
)

// ---- Sync endpoints (Phase 2.2) ----

func (d Deps) getInventorySync(c *gin.Context) {
	snapshot, err := d.MedicineRepo.InventorySnapshot(c.Request.Context(), storeIDFor(c))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"synced_at": timeNow(), "medicines": snapshot})
}

func (d Deps) getCustomersSync(c *gin.Context) {
	customers, err := d.CustomerRepo.List(c.Request.Context(), storeIDFor(c))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"synced_at": timeNow(), "customers": customers})
}

// ---- Medicine CRUD ----

func (d Deps) listMedicines(c *gin.Context) {
	meds, err := d.MedicineRepo.List(c.Request.Context(), storeIDFor(c))
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"medicines": meds})
}

func (d Deps) getMedicine(c *gin.Context) {
	m, err := d.MedicineRepo.GetByID(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, m)
}

func (d Deps) getMedicineDetail(c *gin.Context) {
	detail, err := d.MedicineRepo.GetDetail(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (d Deps) createMedicine(c *gin.Context) {
	var m models.Medicine
	if err := c.ShouldBindJSON(&m); err != nil {
		respondBadRequest(c, err)
		return
	}
	if m.Name == "" {
		respondBadRequest(c, errNameRequired)
		return
	}
	if m.MinReorderLevel < 0 {
		m.MinReorderLevel = 0
	}
	if err := d.MedicineRepo.Create(c.Request.Context(), storeIDFor(c), &m); err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

func (d Deps) updateMedicine(c *gin.Context) {
	var m models.Medicine
	if err := c.ShouldBindJSON(&m); err != nil {
		respondBadRequest(c, err)
		return
	}
	m.ID = c.Param("id")
	if err := d.MedicineRepo.Update(c.Request.Context(), storeIDFor(c), &m); mapRepoError(c, err) {
		return
	}
	out, err := d.MedicineRepo.GetByID(c.Request.Context(), storeIDFor(c), m.ID)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, out)
}

func (d Deps) deleteMedicine(c *gin.Context) {
	err := d.MedicineRepo.SoftDelete(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ---- Customer CRUD ----

// GET /api/customers?search=&type=&limit= — backward compatible: with no
// params it returns the full list (existing behavior); with any param it
// performs a filtered name/phone/GSTIN search, optionally by customer type.
func (d Deps) listCustomers(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	customerType := strings.TrimSpace(c.Query("type"))
	limitQ := strings.TrimSpace(c.Query("limit"))

	if search == "" && customerType == "" && limitQ == "" {
		customers, err := d.CustomerRepo.List(c.Request.Context(), storeIDFor(c))
		if err != nil {
			mapRepoError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"customers": customers})
		return
	}

	limit := 20
	if limitQ != "" {
		n, err := strconv.Atoi(limitQ)
		if err != nil || n <= 0 || n > 200 {
			respondBadRequest(c, errors.New("limit must be between 1 and 200"))
			return
		}
		limit = n
	}
	if customerType != "" && customerType != "B2C" && customerType != "B2B" {
		respondBadRequest(c, errors.New("type must be B2C or B2B"))
		return
	}

	customers, err := d.CustomerRepo.ListFiltered(c.Request.Context(), storeIDFor(c), search, customerType, limit)
	if err != nil {
		mapRepoError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"customers": customers})
}

func (d Deps) getCustomer(c *gin.Context) {
	cust, err := d.CustomerRepo.GetByID(c.Request.Context(), storeIDFor(c), c.Param("id"))
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, cust)
}

func (d Deps) createCustomer(c *gin.Context) {
	var cust models.Customer
	if err := c.ShouldBindJSON(&cust); err != nil {
		respondBadRequest(c, err)
		return
	}
	if err := repository.ValidateCustomer(&cust); err != nil {
		respondBadRequest(c, err)
		return
	}
	if isDuplicateCustomerError(c, d.CustomerRepo.Create(c.Request.Context(), storeIDFor(c), &cust)) {
		return
	}
	c.JSON(http.StatusCreated, cust)
}

func (d Deps) updateCustomer(c *gin.Context) {
	var cust models.Customer
	if err := c.ShouldBindJSON(&cust); err != nil {
		respondBadRequest(c, err)
		return
	}
	cust.ID = c.Param("id")
	if err := repository.ValidateCustomer(&cust); err != nil {
		respondBadRequest(c, err)
		return
	}
	current, err := d.CustomerRepo.GetByID(c.Request.Context(), storeIDFor(c), cust.ID)
	if mapRepoError(c, err) {
		return
	}
	cust.CurrentBalance = current.CurrentBalance
	if isDuplicateCustomerError(c, d.CustomerRepo.Update(c.Request.Context(), storeIDFor(c), &cust)) {
		return
	}
	c.JSON(http.StatusOK, cust)
}

// isDuplicateCustomerError maps a unique-constraint (23505) failure on the
// customers table to a 409 Conflict with a friendly message and returns true;
// otherwise any other error is routed through mapRepoError. It returns false
// only when err is nil.
func isDuplicateCustomerError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		respondConflict(c, "a customer with this phone number already exists")
		return true
	}
	mapRepoError(c, err)
	return true
}

// GET /api/customers/:id/ledger — full credit audit trail.
func (d Deps) customerLedger(c *gin.Context) {
	id := c.Param("id")
	customer, err := d.CustomerRepo.GetByID(c.Request.Context(), storeIDFor(c), id)
	if mapRepoError(c, err) {
		return
	}
	entries, err := d.CustomerRepo.Ledger(c.Request.Context(), storeIDFor(c), id, 0)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer": customer, "entries": entries})
}

// POST /api/customers/:id/payments — collect full or part payment.
func (d Deps) recordPayment(c *gin.Context) {
	var in repository.PaymentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respondBadRequest(c, err)
		return
	}
	customer, entry, err := d.CustomerRepo.RecordPayment(
		c.Request.Context(), storeIDFor(c), c.Param("id"), in.Amount, in.Notes)
	if mapRepoError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer": customer, "entry": entry})
}
