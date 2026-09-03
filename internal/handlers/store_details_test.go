package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

type storeResp struct {
	Store struct {
		ID                string  `json:"id"`
		Name              string  `json:"name"`
		Address           string  `json:"address"`
		Phone             string  `json:"phone"`
		OwnerName         string  `json:"owner_name"`
		GSTIN             *string `json:"gstin"`
		PAN               *string `json:"pan"`
		DrugLicenseNumber string  `json:"drug_license_number"`
		DrugLicenseExpiry *string `json:"drug_license_expiry"`
		IsActive          bool    `json:"is_active"`
		MaxEmployees      int     `json:"max_employees"`
		GSTRegistrationID *string `json:"gst_registration_id"`
	} `json:"store"`
}

func getStoreAs(t *testing.T, rawToken string) storeResp {
	t.Helper()
	rec := doJSONAs(t, http.MethodGet, "/api/store", nil, rawToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get store = %d body %s", rec.Code, rec.Body.String())
	}
	var out storeResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode store: %v", err)
	}
	return out
}

func TestMinimalRegistrationShopProfile(t *testing.T) {
	rawToken, userID := registerUser(t, "9820000200")

	out := getStoreAs(t, rawToken)
	if out.Store.OwnerName != "Bootstrap Owner" {
		t.Errorf("owner_name = %q want Bootstrap Owner", out.Store.OwnerName)
	}
	if out.Store.Name != "Branch" || out.Store.Address != "Pune" || out.Store.Phone != "9820000200" {
		t.Errorf("minimal store = %+v", out.Store)
	}
	if out.Store.GSTIN != nil || out.Store.PAN != nil {
		t.Errorf("gstin/pan should be null, got %v/%v", out.Store.GSTIN, out.Store.PAN)
	}
	if out.Store.DrugLicenseNumber != "" {
		t.Errorf("dl number should be empty, got %q", out.Store.DrugLicenseNumber)
	}
	if out.Store.GSTRegistrationID != nil && *out.Store.GSTRegistrationID != "" {
		t.Errorf("gst_registration_id should be null, got %v", out.Store.GSTRegistrationID)
	}
	if userID == "" {
		t.Error("registerUser returned empty user id")
	}
}

func TestStoreUpdateMandatoryValidation(t *testing.T) {
	rawToken, _ := registerUser(t, "9820000202")
	sid := getStoreAs(t, rawToken).Store.ID

	if sid == "" {
		t.Fatal("empty store id")
	}

	for name, body := range map[string]map[string]interface{}{
		"empty-name":    {"name": "", "address": "Pune", "phone": "9820000202", "owner_name": "Bootstrap Owner"},
		"empty-address": {"name": "Branch", "address": "", "phone": "9820000202", "owner_name": "Bootstrap Owner"},
		"empty-phone":   {"name": "Branch", "address": "Pune", "phone": "", "owner_name": "Bootstrap Owner"},
		"empty-owner":   {"name": "Branch", "address": "Pune", "phone": "9820000202", "owner_name": ""},
	} {
		rec := doJSONAs(t, http.MethodPut, "/api/store", body, rawToken)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: PUT store = %d want 400, body %s", name, rec.Code, rec.Body.String())
		}
	}

	// No DB change after all failures.
	ctx := context.Background()
	var name, addr, phone string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT name, address, phone FROM stores WHERE id = $1`, sid).Scan(&name, &addr, &phone); err != nil {
		t.Fatalf("read store: %v", err)
	}
	if name != "Branch" || addr != "Pune" || phone != "9820000202" {
		t.Errorf("store mutated after rejected updates: %q/%q/%q", name, addr, phone)
	}
}

func TestStoreClearOptionalFields(t *testing.T) {
	// Register a tenant WITH the optional GST/PAN/DL fields present (so the
	// store owns a registration), then clear them via PUT. This exercises the
	// clear path directly.
	rec := doJSON(t, http.MethodPost, "/api/auth/register", map[string]interface{}{
		"name": "Clear Owner", "phone": "9820000203", "password": "tenantpass123",
		"business_name": "Clear Biz",
		"store_name":    "Branch", "store_address": "Pune", "store_phone": "9820000203",
		"gstin": "27AAPBC1234F1ZV", "pan": "AAAAA0000A",
		"drug_license_number": "DL/555", "drug_license_expiry": "2029-05-05",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register-with-optional = %d body %s", rec.Code, rec.Body.String())
	}
	rawToken := cookieFrom(t, rec)

	clear := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Branch", "address": "Pune", "phone": "9820000203", "owner_name": "Clear Owner",
		"gstin": "", "pan": "",
	}, rawToken)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear = %d body %s", clear.Code, clear.Body.String())
	}
	cleared := getStoreAs(t, rawToken)
	if cleared.Store.GSTIN != nil && *cleared.Store.GSTIN != "" {
		t.Errorf("gstin not cleared: %v", cleared.Store.GSTIN)
	}
	if cleared.Store.PAN != nil && *cleared.Store.PAN != "" {
		t.Errorf("pan not cleared: %v", cleared.Store.PAN)
	}
	if cleared.Store.DrugLicenseNumber != "" {
		t.Errorf("dl number not cleared: %q", cleared.Store.DrugLicenseNumber)
	}
}

func TestStoreUpdatePersistsAndAudits(t *testing.T) {
	rawToken, userID := registerUser(t, "9820000204")

	rec := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Updated Branch", "address": "Delhi", "phone": "9820000205",
		"owner_name": "Updated Owner", "max_employees": 3,
	}, rawToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d body %s", rec.Code, rec.Body.String())
	}

	after := getStoreAs(t, rawToken)
	if after.Store.Name != "Updated Branch" || after.Store.Address != "Delhi" ||
		after.Store.Phone != "9820000205" || after.Store.OwnerName != "Updated Owner" ||
		after.Store.MaxEmployees != 3 {
		t.Errorf("updated store = %+v", after.Store)
	}

	// Audit row written for the update.
	ctx := context.Background()
	var action string
	if err := testPoolDB.QueryRow(ctx,
		`SELECT action FROM audit_logs WHERE user_id = $1 AND action = 'store.settings.update' ORDER BY created_at DESC LIMIT 1`,
		userID).Scan(&action); err != nil {
		t.Fatalf("expected a store.settings.update audit row: %v", err)
	}
}

func TestStoreUnauthorizedUpdates(t *testing.T) {
	rawToken, _ := registerUser(t, "9820000206")
	sid := getStoreAs(t, rawToken).Store.ID

	// Employee cannot update.
	emp := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Emp Hack", "address": "X", "phone": "9820000206", "owner_name": "Bootstrap Owner",
	}, employeeRawToken)
	if emp.Code != http.StatusForbidden {
		t.Errorf("employee update = %d want 403, body %s", emp.Code, emp.Body.String())
	}

	// Anonymous cannot update (or read).
	anon := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Anon Hack", "address": "X", "phone": "9820000206", "owner_name": "Bootstrap Owner",
	}, "")
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("anonymous update = %d want 401, body %s", anon.Code, anon.Body.String())
	}
	anonGet := doJSONAs(t, http.MethodGet, "/api/store", nil, "")
	if anonGet.Code != http.StatusUnauthorized {
		t.Errorf("anonymous get = %d want 401", anonGet.Code)
	}

	// No DB change.
	ctx := context.Background()
	var name string
	if err := testPoolDB.QueryRow(ctx, `SELECT name FROM stores WHERE id = $1`, sid).Scan(&name); err != nil {
		t.Fatalf("read store: %v", err)
	}
	if name != "Branch" {
		t.Errorf("store name = %q want Branch (unchanged)", name)
	}
}

func TestStoreOptionalEmptyPreservesMandatory(t *testing.T) {
	rawToken, _ := registerUser(t, "9820000207")

	rec := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Branch", "address": "Pune", "phone": "9820000207", "owner_name": "Bootstrap Owner",
		"gstin": "", "pan": "",
	}, rawToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("optional empty = %d body %s", rec.Code, rec.Body.String())
	}
	after := getStoreAs(t, rawToken)
	if after.Store.Name != "Branch" || after.Store.Address != "Pune" || after.Store.Phone != "9820000207" {
		t.Errorf("mandatory not preserved: %+v", after.Store)
	}
}

func TestStoreReloadViaFreshRoundTrip(t *testing.T) {
	// Register a fresh tenant with full optional values present, update while
	// preserving them, then re-fetch via a brand-new GET round trip.
	rec := doJSON(t, http.MethodPost, "/api/auth/register", map[string]interface{}{
		"name": "Reload Owner", "phone": "9820000208", "password": "tenantpass123",
		"business_name": "Reload Biz",
		"store_name":    "Reload Branch", "store_address": "Kolkata", "store_phone": "9820000208",
		"gstin": "27AAPBC1234F1ZV", "pan": "AAAAA0000A",
		"drug_license_number": "DL/321", "drug_license_expiry": "2031-12-31",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register-with-optional = %d body %s", rec.Code, rec.Body.String())
	}
	rawToken := cookieFrom(t, rec)

	put := doJSONAs(t, http.MethodPut, "/api/store", map[string]interface{}{
		"name": "Reload Branch", "address": "Kolkata", "phone": "9820000209",
		"owner_name": "Reload Owner", "max_employees": 4,
		"gstin": "27AAPBC1234F1ZV", "pan": "AAAAA0000A",
		"drug_license_number": "DL/321", "drug_license_expiry": "2031-12-31",
	}, rawToken)
	if put.Code != http.StatusOK {
		t.Fatalf("put = %d body %s", put.Code, put.Body.String())
	}

	// A fresh GET (new HTTP round trip) reflects every persisted value.
	reload := getStoreAs(t, rawToken)
	if reload.Store.Name != "Reload Branch" || reload.Store.Address != "Kolkata" ||
		reload.Store.Phone != "9820000209" || reload.Store.OwnerName != "Reload Owner" {
		t.Errorf("reloaded store = %+v", reload.Store)
	}
	if reload.Store.GSTIN == nil || *reload.Store.GSTIN != "27AAPBC1234F1ZV" {
		t.Errorf("reloaded gstin = %v", reload.Store.GSTIN)
	}
	if reload.Store.PAN == nil || *reload.Store.PAN != "AAAAA0000A" {
		t.Errorf("reloaded pan = %v", reload.Store.PAN)
	}
	if reload.Store.DrugLicenseNumber != "DL/321" || reload.Store.DrugLicenseExpiry == nil ||
		*reload.Store.DrugLicenseExpiry != "2031-12-31" {
		t.Errorf("reloaded dl = %q/%v", reload.Store.DrugLicenseNumber, reload.Store.DrugLicenseExpiry)
	}
}
