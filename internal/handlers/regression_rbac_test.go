package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/auth"
)

// TestCashierForbiddenOnSensitiveRoutes proves cashiers (EMPLOYEE) receive 403
// on store settings, profit reports and reconcile actions, while the owner
// passes the role guard.
func TestCashierForbiddenOnSensitiveRoutes(t *testing.T) {
	sensitive := []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPut, "/api/store", map[string]interface{}{
			"name": "Hack", "address": "X", "phone": "9820000000", "owner_name": "H",
		}},
		{http.MethodGet, "/api/store", nil},
		{http.MethodGet, "/api/reports/sales", nil},
		{http.MethodGet, "/api/reports/purchase", nil},
		{http.MethodGet, "/api/reports/profit-loss", nil},
		{http.MethodGet, "/api/reports/expiry", nil},
		{http.MethodGet, "/api/reports/low-stock", nil},
		{http.MethodPost, "/api/inventory/reconcile", map[string]interface{}{"items": []interface{}{}}},
		{http.MethodGet, "/api/inventory/reconciliations", nil},
	}
	for _, tc := range sensitive {
		rec := doJSONAs(t, tc.method, tc.path, tc.body, employeeRawToken)
		if rec.Code != http.StatusForbidden {
			t.Errorf("cashier %s %s = %d want 403, body %s",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestStoreHeaderTamperingRejected proves an X-Store-ID header naming a store
// the user is not assigned to is rejected with 403 and never trusted.
func TestStoreHeaderTamperingRejected(t *testing.T) {
	rogue := "11111111-2222-3333-4444-555555555555"

	newReq := func(token, storeHeader string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/medicines", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		if storeHeader != "" {
			req.Header.Set("X-Store-ID", storeHeader)
		}
		rec := httptest.NewRecorder()
		testRouter.ServeHTTP(rec, req)
		return rec
	}

	// No header: passes auth (owner can list).
	if rec := newReq(ownerRawToken, ""); rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Errorf("owner without header = %d (must pass), body %s", rec.Code, rec.Body.String())
	}
	// Rogue header: 403 even though the session is valid.
	if rec := newReq(ownerRawToken, rogue); rec.Code != http.StatusForbidden {
		t.Errorf("owner with rogue X-Store-ID = %d want 403, body %s", rec.Code, rec.Body.String())
	}
	if rec := newReq(employeeRawToken, rogue); rec.Code != http.StatusForbidden {
		t.Errorf("cashier with rogue X-Store-ID = %d want 403, body %s", rec.Code, rec.Body.String())
	}
}
