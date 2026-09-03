package handlers

import (
	"net/http"
	"testing"
)

const fakeUUID = "00000000-0000-0000-0000-000000000000"

// TestEmployeeCannotApproveOrPostSensitive proves the owner-only boundary:
// an EMPLOYEE (cashier) token is rejected with 403 on every approval and
// direct-posting route, while the owner passes the guard (reaching the
// handler, which then 404s on the fake ID instead of 403ing).
func TestEmployeeCannotApproveOrPostSensitive(t *testing.T) {
	ownerOnly := []struct {
		method string
		path   string
		body   interface{}
	}{
		{http.MethodPost, "/api/purchase-requests/" + fakeUUID + "/approve", nil},
		{http.MethodPost, "/api/purchase-requests/" + fakeUUID + "/reject", map[string]interface{}{"reason": "x"}},
		{http.MethodPost, "/api/stock-audit-requests/" + fakeUUID + "/approve", nil},
		{http.MethodPost, "/api/stock-audit-requests/" + fakeUUID + "/reject", map[string]interface{}{"reason": "x"}},
		{http.MethodPost, "/api/inventory/reconcile", map[string]interface{}{"items": []interface{}{}}},
		{http.MethodPost, "/api/purchases", map[string]interface{}{"supplier_name": "x"}},
		{http.MethodPut, "/api/medicines/" + fakeUUID + "/tax-config", map[string]interface{}{}},
		{http.MethodPost, "/api/hsn", map[string]interface{}{"code": "9999"}},
		{http.MethodPut, "/api/hsn/" + fakeUUID + "/tax-rate", map[string]interface{}{}},
		{http.MethodGet, "/api/employees", nil},
	}

	for _, tc := range ownerOnly {
		rec := doJSONAs(t, tc.method, tc.path, tc.body, employeeRawToken)
		if rec.Code != http.StatusForbidden {
			t.Errorf("employee %s %s = %d want 403, body %s",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}

		owner := doJSONAs(t, tc.method, tc.path, tc.body, ownerRawToken)
		if owner.Code == http.StatusForbidden {
			t.Errorf("owner %s %s = 403 (owner must pass the role guard), body %s",
				tc.method, tc.path, owner.Body.String())
		}
	}
}

// TestEmployeeRetainsAllowedAccess guards against over-blocking: routes the
// employee role legitimately holds must keep working after the guard rollout.
func TestEmployeeRetainsAllowedAccess(t *testing.T) {
	allowed := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/medicines"},
		{http.MethodGet, "/api/customers"},
		{http.MethodGet, "/api/sales/invoices"},
		{http.MethodPost, "/api/sales/checkout"},
	}
	for _, tc := range allowed {
		var body interface{}
		if tc.method == http.MethodPost {
			body = map[string]interface{}{"payment_type": "CASH", "items": []interface{}{}}
		}
		rec := doJSONAs(t, tc.method, tc.path, body, employeeRawToken)
		if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Errorf("employee %s %s = %d (must remain accessible), body %s",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// Unauthenticated requests still 401 on a guarded route.
	anon := doJSONAs(t, http.MethodGet, "/api/medicines", nil, "")
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET /api/medicines = %d want 401", anon.Code)
	}
}
