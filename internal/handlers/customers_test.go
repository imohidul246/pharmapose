package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/gst"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

var (
	testRouter *gin.Engine
	testPoolDB *pgxpool.Pool

	// ownerRawToken is the raw session cookie for a seeded STORE_OWNER account
	// (and the employee twin) so handler tests can authenticate their requests.
	ownerRawToken   string
	employeeRawToken string
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	testPoolDB, err = testutil.ConnectTestDB(ctx, "handlers")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect handlers test db: %v\n", err)
		os.Exit(1)
	}
	_, err = testPoolDB.Exec(ctx, `
		TRUNCATE sessions, store_memberships, users, audit_logs,
		         purchase_requests, stock_audit_request_items, stock_audit_requests,
		         customer_ledger, sales_credit_notes, sales_invoice_items, sales_invoices,
		         purchase_order_items, purchase_orders,
		         gstr2b_imports, gstr2b_import_batches, medicine_tax_config,
		         gst_registrations, stores, businesses, suppliers, batches, customers, medicines CASCADE`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: %v\n", err)
		os.Exit(1)
	}
	if err := testutil.SeedStore(ctx, testPoolDB); err != nil {
		fmt.Fprintf(os.Stderr, "seed store: %v\n", err)
		os.Exit(1)
	}
	authRepo := repository.NewAuthRepo(testPoolDB)
	ownerRawToken, err = seedTestSession(ctx, testPoolDB, authRepo, testutil.StoreID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed owner session: %v\n", err)
		os.Exit(1)
	}
	employeeRawToken, err = seedTestEmployeeSession(ctx, testPoolDB, authRepo, testutil.StoreID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed employee session: %v\n", err)
		os.Exit(1)
	}

	testRouter = NewRouter(Deps{
		AuthRepo:              authRepo,
		PurchaseRequestRepo:   repository.NewPurchaseRequestRepo(testPoolDB),
		StockAuditRequestRepo: repository.NewStockAuditRequestRepo(testPoolDB),
		CookieOptions:         auth.CookieOptions{Secure: false},
		MedicineRepo:          repository.NewMedicineRepo(testPoolDB),
		CustomerRepo:          repository.NewCustomerRepo(testPoolDB),
		SaleRepo:              repository.NewSaleRepo(testPoolDB),
		PurchaseRepo:          repository.NewPurchaseRepo(testPoolDB),
		ReconcileRepo:         repository.NewReconcileRepo(testPoolDB),
		ReportRepo:            repository.NewReportRepo(testPoolDB),
		SupplierRepo:          repository.NewSupplierRepo(testPoolDB),
		TaxRepo:               repository.NewTaxRepo(testPoolDB),
		GSTHandler:            gst.NewHandler(testPoolDB),
	})

	code := m.Run()
	testPoolDB.Close()
	os.Exit(code)
}

// seedTestSession creates an owner (Store Owner) with a live session and
// returns the raw cookie value to attach to requests.
func seedTestSession(ctx context.Context, pool *pgxpool.Pool, authRepo *repository.AuthRepo, storeID string) (string, error) {
	hash, err := auth.HashPassword("owner@123")
	if err != nil {
		return "", err
	}
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (name, phone, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
		"Store Owner", "9820000000", hash).Scan(&userID); err != nil {
		return "", err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO store_memberships (store_id, user_id, role, is_active)
		VALUES ($1, $2, 'STORE_OWNER', true)`, storeID, userID); err != nil {
		return "", err
	}
	return newSession(ctx, pool, userID)
}

func seedTestEmployeeSession(ctx context.Context, pool *pgxpool.Pool, authRepo *repository.AuthRepo, storeID string) (string, error) {
	hash, err := auth.HashPassword("emp@1234")
	if err != nil {
		return "", err
	}
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (name, phone, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
		"Ramesh Kumar", "9820000001", hash).Scan(&userID); err != nil {
		return "", err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO store_memberships (store_id, user_id, role, is_active)
		VALUES ($1, $2, 'EMPLOYEE', true)`, storeID, userID); err != nil {
		return "", err
	}
	return newSession(ctx, pool, userID)
}

func newSession(ctx context.Context, pool *pgxpool.Pool, userID string) (string, error) {
	raw, err := auth.NewSessionToken()
	if err != nil {
		return "", err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO sessions (user_id, token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, '127.0.0.1', 'test', now() + interval '7 days')`,
		userID, auth.HashSessionToken(raw))
	return raw, err
}

func cleanupCustomers(t *testing.T) {
	t.Helper()
	_, err := testPoolDB.Exec(context.Background(), `TRUNCATE customers, customer_ledger CASCADE`)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func doJSON(t *testing.T, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONAs(t, method, path, body, ownerRawToken)
}

func doJSONAs(t *testing.T, method, path string, body interface{}, rawToken string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		raw, _ := json.Marshal(body)
		buf.Write(raw)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if rawToken != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: rawToken})
	}
	rec := httptest.NewRecorder()
	testRouter.ServeHTTP(rec, req)
	return rec
}

func errMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return out.Error
}

func TestCreateCustomerDuplicatePhoneConflict(t *testing.T) {
	cleanupCustomers(t)

	body := map[string]interface{}{
		"name": "Dup Tester", "phone": "+919999000001", "customer_type": "B2C",
	}
	first := doJSON(t, http.MethodPost, "/api/customers", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create = %d want 201, body %s", first.Code, first.Body.String())
	}

	dup := doJSON(t, http.MethodPost, "/api/customers", body)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate phone = %d want 409, not 500", dup.Code)
	}
	if msg := errMessage(t, dup); msg == "" || msg == "internal server error" {
		t.Fatalf("duplicate phone must surface a friendly message, got %q", msg)
	}
}

func TestCreateCustomerInvalidGSTINBadRequest(t *testing.T) {
	cleanupCustomers(t)

	bad := "27AABCU9603R1ZM" // pattern-valid but checksum-invalid
	body := map[string]interface{}{
		"name": "Bad GSTIN", "phone": "+919999000002", "customer_type": "B2B", "gstin": bad,
	}
	rec := doJSON(t, http.MethodPost, "/api/customers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid GSTIN create = %d want 400, body %s", rec.Code, rec.Body.String())
	}
	if msg := errMessage(t, rec); msg != "invalid GSTIN" {
		t.Fatalf("want 'invalid GSTIN', got %q", msg)
	}
}

func TestListCustomersSearch(t *testing.T) {
	cleanupCustomers(t)

	stateToC := "27"
	stateKA := "29"
	for _, c := range []map[string]interface{}{
		{"name": "Aashirwad Medico", "phone": "+919999000011", "customer_type": "B2C", "state_code": stateToC},
		{"name": "Karnataka Chemists", "phone": "+919999000012", "customer_type": "B2B", "gstin": "29AAPBC1234F1ZR", "state_code": stateKA},
		{"name": "Sunrise Pharma", "phone": "+919999000013", "customer_type": "B2C", "state_code": stateToC},
	} {
		if rec := doJSON(t, http.MethodPost, "/api/customers", c); rec.Code != http.StatusCreated {
			t.Fatalf("seed create = %d body %s", rec.Code, rec.Body.String())
		}
	}

	// No params -> full list (backward compatible).
	all := doJSON(t, http.MethodGet, "/api/customers", nil)
	if all.Code != http.StatusOK {
		t.Fatalf("list all = %d", all.Code)
	}
	var full struct {
		Customers []models.Customer `json:"customers"`
	}
	if err := json.Unmarshal(all.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if len(full.Customers) != 3 {
		t.Errorf("no-param list = %d want 3", len(full.Customers))
	}

	// Search narrows results; type filter further narrows.
	searched := doJSON(t, http.MethodGet, "/api/customers?search=Karnataka", nil)
	if searched.Code != http.StatusOK {
		t.Fatalf("search = %d", searched.Code)
	}
	var sres struct {
		Customers []models.Customer `json:"customers"`
	}
	if err := json.Unmarshal(searched.Body.Bytes(), &sres); err != nil {
		t.Fatal(err)
	}
	if len(sres.Customers) != 1 || sres.Customers[0].Name != "Karnataka Chemists" {
		t.Errorf("search 'Karnataka' = %+v want [Karnataka Chemists]", sres.Customers)
	}

	typed := doJSON(t, http.MethodGet, "/api/customers?search=99900001&type=B2C", nil)
	var tres struct {
		Customers []models.Customer `json:"customers"`
	}
	if err := json.Unmarshal(typed.Body.Bytes(), &tres); err != nil {
		t.Fatal(err)
	}
	if len(tres.Customers) != 2 {
		t.Errorf("phone+type search = %d want 2 (Aashirwad, Sunrise)", len(tres.Customers))
	}

	badType := doJSON(t, http.MethodGet, "/api/customers?type=WHOLESALE", nil)
	if badType.Code != http.StatusBadRequest {
		t.Errorf("invalid type filter = %d want 400", badType.Code)
	}
}
