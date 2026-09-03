package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

func cookieFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie in %#v", auth.CookieName, rec.Header())
	return ""
}

func registerUser(t *testing.T, phone string) (rawToken, userID string) {
	t.Helper()
	rec := doJSON(t, http.MethodPost, "/api/auth/register", map[string]interface{}{
		"name":          "Bootstrap Owner",
		"phone":         phone,
		"password":      "tenantpass123",
		"business_name": "Bootstrap Biz",
		"store_name":    "Branch",
		"store_address": "Pune",
		"store_phone":   phone,
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("register = %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Principal responsePrincipal `json:"principal"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("register body: %v", err)
	}
	return cookieFrom(t, rec), out.Principal.ID
}

func TestRegisterLoginLogoutMeCycle(t *testing.T) {
	rawToken, _ := registerUser(t, "9820000100")

	me := doJSONAs(t, http.MethodGet, "/api/auth/me", nil, rawToken)
	if me.Code != http.StatusOK {
		t.Fatalf("me = %d body %s", me.Code, me.Body.String())
	}
	var got struct {
		Principal responsePrincipal `json:"principal"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Principal.Role != "STORE_OWNER" || got.Principal.StoreID == "" {
		t.Errorf("principal = %+v want owner + non-empty store", got.Principal)
	}

	logout := doJSONAs(t, http.MethodPost, "/api/auth/logout", nil, rawToken)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout = %d", logout.Code)
	}
	if after := doJSONAs(t, http.MethodGet, "/api/auth/me", nil, rawToken); after.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d want 401", after.Code)
	}

	bad := doJSON(t, http.MethodPost, "/api/auth/login", map[string]interface{}{
		"phone": "9820000100", "password": "wrongpass",
	})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d want 401", bad.Code)
	}

	ok := doJSON(t, http.MethodPost, "/api/auth/login", map[string]interface{}{
		"phone": "9820000100", "password": "tenantpass123",
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("login = %d body %s", ok.Code, ok.Body.String())
	}
	fresh := cookieFrom(t, ok)
	after := doJSONAs(t, http.MethodGet, "/api/auth/me", nil, fresh)
	if after.Code != http.StatusOK {
		t.Fatalf("me after login = %d", after.Code)
	}
}

func TestChangePasswordRotatesAndKeepsCurrentSession(t *testing.T) {
	rawToken, _ := registerUser(t, "9820000101")

	bad := doJSONAs(t, http.MethodPost, "/api/auth/change-password", map[string]interface{}{
		"current_password": "nope",
		"new_password":     "newpass999",
	}, rawToken)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("wrong current = %d want 400", bad.Code)
	}

	ok := doJSONAs(t, http.MethodPost, "/api/auth/change-password", map[string]interface{}{
		"current_password": "tenantpass123",
		"new_password":     "rotated999",
	}, rawToken)
	if ok.Code != http.StatusOK {
		t.Fatalf("change-password = %d body %s", ok.Code, ok.Body.String())
	}

	if me := doJSONAs(t, http.MethodGet, "/api/auth/me", nil, rawToken); me.Code != http.StatusOK {
		t.Fatalf("me after rotation = %d", me.Code)
	}

	oldLogin := doJSON(t, http.MethodPost, "/api/auth/login", map[string]interface{}{
		"phone": "9820000101", "password": "tenantpass123",
	})
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password login = %d want 401", oldLogin.Code)
	}
	newLogin := doJSON(t, http.MethodPost, "/api/auth/login", map[string]interface{}{
		"phone": "9820000101", "password": "rotated999",
	})
	if newLogin.Code != http.StatusOK {
		t.Fatalf("new password login = %d", newLogin.Code)
	}
}

func TestRoleGatingOwnerVsEmployee(t *testing.T) {
	if rec := doJSON(t, http.MethodGet, "/api/employees", nil); rec.Code != http.StatusOK {
		t.Fatalf("owner employees = %d", rec.Code)
	}
	if rec := doJSON(t, http.MethodGet, "/api/store", nil); rec.Code != http.StatusOK {
		t.Fatalf("owner store = %d", rec.Code)
	}

	emp := func(method, path string, body interface{}) *httptest.ResponseRecorder {
		return doJSONAs(t, method, path, body, employeeRawToken)
	}
	for _, tc := range []struct {
		name, method, path string
		body               interface{}
	}{
		{"employees", http.MethodGet, "/api/employees", nil},
		{"store", http.MethodGet, "/api/store", nil},
		{"direct purchase", http.MethodPost, "/api/purchases", map[string]interface{}{
			"invoice_no": "EMP-GUARD", "supplier_name": "X",
			"items": []map[string]interface{}{{"batch_number": "B", "quantity": 1, "purchase_price": 1, "sale_price": 2, "expiry_date": "2030-01-01"}},
		}},
		{"reconcile", http.MethodPost, "/api/inventory/reconcile", map[string]interface{}{"entries": []interface{}{}}},
		{"purchase approve", http.MethodPost, "/api/purchase-requests/00000000-0000-0000-0000-00000000ffff/approve", nil},
		{"audit approve", http.MethodPost, "/api/stock-audit-requests/00000000-0000-0000-0000-00000000ffff/approve", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := emp(tc.method, tc.path, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s as employee = %d want 403, body %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}

	if rec := emp(http.MethodGet, "/api/purchase-requests", nil); rec.Code != http.StatusOK {
		t.Fatalf("employee list purchase-requests = %d want 200", rec.Code)
	}
	// Counter staff can bill and read invoices (sales is employee-capable).
	if rec := emp(http.MethodGet, "/api/sales/invoices", nil); rec.Code != http.StatusOK {
		t.Fatalf("employee sales invoices = %d want 200", rec.Code)
	}
	if rec := emp(http.MethodPost, "/api/sales/checkout", map[string]interface{}{
		"payment_type": "CASH",
		"items":        []interface{}{},
	}); rec.Code == http.StatusForbidden {
		t.Fatal("employee checkout must not be forbidden by role")
	}

	anon := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		testRouter.ServeHTTP(rec, req)
		return rec
	}
	if rec := anon(http.MethodGet, "/api/medicines"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/medicines = %d want 401", rec.Code)
	}
	if rec := anon(http.MethodGet, "/api/auth/me"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/auth/me = %d want 401", rec.Code)
	}
}

func TestCSRFBlocksForeignOrigin(t *testing.T) {
	body := map[string]interface{}{"name": "CSRF Tester", "phone": "+919999000200", "customer_type": "B2C"}
	raw, _ := json.Marshal(body)

	post := func(origin string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/customers", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ownerRawToken})
		rec := httptest.NewRecorder()
		testRouter.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("https://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin POST = %d want 403, body %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/medicines", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: ownerRawToken})
	rec := httptest.NewRecorder()
	testRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin GET = %d want 403 (CORS denies the origin)", rec.Code)
	}

	if rec := post("http://localhost:5173"); rec.Code != http.StatusCreated {
		t.Fatalf("dev-origin POST = %d want 201, body %s", rec.Code, rec.Body.String())
	}
}

func TestClientStoreIDIsIgnored(t *testing.T) {
	ctx := context.Background()

	rogue := "11111111-2222-3333-4444-555555555555"
	if _, err := testPoolDB.Exec(ctx, `
		INSERT INTO stores (id, name, address, is_active, max_employees)
		VALUES ($1, 'Rogue', 'Elsewhere', true, 2)
		ON CONFLICT (id) DO NOTHING`, rogue); err != nil {
		t.Fatalf("seed rogue store: %v", err)
	}

	med := doJSON(t, http.MethodPost, "/api/medicines", map[string]interface{}{
		"name":              "Isolation Med",
		"salt_composition":  "Paracetamol",
		"manufacturer":      "IsoPharma",
		"min_reorder_level": 5,
	})
	if med.Code != http.StatusCreated {
		t.Fatalf("create medicine = %d body %s", med.Code, med.Body.String())
	}
	var m struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(med.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	var medStore string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT store_id::text FROM medicines WHERE id = $1`, m.ID).Scan(&medStore); err != nil {
		t.Fatalf("medicine row: %v", err)
	}
	if medStore != testutil.StoreID {
		t.Errorf("medicine in store %q want tenant store", medStore)
	}

	purchaseBody := map[string]interface{}{
		"store_id":      rogue, // client spoof attempt
		"invoice_no":    "ISO-IN-1",
		"supplier_name": "Iso Supplier",
		"items": []map[string]interface{}{{
			"medicine_id": m.ID, "batch_number": "ISO-B1", "expiry_date": "2031-01-01",
			"quantity": 10, "purchase_price": 10, "sale_price": 15,
		}},
	}

	rec := doJSON(t, http.MethodPost, "/api/purchases", purchaseBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("purchase = %d body %s", rec.Code, rec.Body.String())
	}
	var srow string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT store_id::text FROM purchase_orders WHERE invoice_no = 'ISO-IN-1'`).Scan(&srow); err != nil {
		t.Fatalf("find purchase order: %v", err)
	}
	if srow != testutil.StoreID {
		t.Errorf("purchase in store %q want tenant store (rogue %q ignored)", srow, rogue)
	}
	var batchStore string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT store_id::text FROM batches WHERE batch_number = 'ISO-B1' AND medicine_id = $1`, m.ID).Scan(&batchStore); err != nil {
		t.Fatalf("batch store: %v", err)
	}
	if batchStore != testutil.StoreID {
		t.Errorf("batch in store %q want tenant store", batchStore)
	}

	reqRec := doJSONAs(t, http.MethodPost, "/api/purchase-requests", purchaseBody, employeeRawToken)
	if reqRec.Code != http.StatusCreated {
		t.Fatalf("purchase-request = %d body %s", reqRec.Code, reqRec.Body.String())
	}
	var reqStore string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT store_id::text FROM purchase_requests WHERE purchase_snapshot::text LIKE '%ISO-B1%'`).Scan(&reqStore); err != nil {
		t.Fatalf("find request: %v", err)
	}
	if reqStore != testutil.StoreID {
		t.Errorf("request in store %q want tenant store (rogue ignored)", reqStore)
	}
}