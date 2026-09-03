package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

func registerOwner(t *testing.T, phone string) (storeID, userID string) {
	t.Helper()
	ctx := context.Background()
	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name:         "Shop Owner",
		Phone:        phone,
		PasswordHash: fakePHC,
		BusinessName: "Shop Biz",
		TradeName:    "SB",
		StoreName:    "Main",
		StoreAddress: "Pune",
		StorePhone:   phone,
	}, auth.HashSessionToken("sd-"+phone), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res.Principal.StoreID, res.User.ID
}

// rawStore reads the raw stores row columns for a store.
func rawStore(t *testing.T, storeID string) (name, address, phone, dlNum string, dlExpiry *string, isActive bool, maxEmp int, createdStr string) {
	t.Helper()
	ctx := context.Background()
	var dlExp *time.Time
	err := pool.QueryRow(ctx, `
		SELECT name, address, phone, drug_license_number, drug_license_expiry,
		       is_active, max_employees, created_at::text
		FROM stores WHERE id = $1`, storeID).
		Scan(&name, &address, &phone, &dlNum, &dlExp, &isActive, &maxEmp, &createdStr)
	if err != nil {
		t.Fatalf("raw store: %v", err)
	}
	if dlExp != nil {
		d := dlExp.Format("2006-01-02")
		dlExpiry = &d
	}
	return
}

func TestRegisterWithOptionalInfoPersists(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	gstin := "27AAPBC1234F1ZV"
	pan := "AAAAA0000A"
	expiry := "2025-12-31"
	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name:               "Opt Owner",
		Phone:              "9820000041",
		PasswordHash:       fakePHC,
		BusinessName:       "Opt Biz",
		GSTIN:              &gstin,
		PAN:                &pan,
		StoreName:          "Opt Store",
		StoreAddress:       "Mumbai",
		StorePhone:         "9820000041",
		DrugLicenseNumber:  "DL/12345",
		DrugLicenseExpiry:  &expiry,
	}, auth.HashSessionToken("opt-1"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	st, err := authRepo.GetStoreDetails(ctx, res.Principal.StoreID)
	if err != nil {
		t.Fatalf("get store details: %v", err)
	}
	if st.Phone != "9820000041" {
		t.Errorf("phone = %q want 9820000041", st.Phone)
	}
	if st.DrugLicenseNumber != "DL/12345" {
		t.Errorf("dl number = %q want DL/12345", st.DrugLicenseNumber)
	}
	if st.DrugLicenseExpiry == nil || st.DrugLicenseExpiry.String() != "2025-12-31" {
		t.Errorf("dl expiry = %v want 2025-12-31", st.DrugLicenseExpiry)
	}
	if st.OwnerName != "Opt Owner" {
		t.Errorf("owner name = %q want Opt Owner", st.OwnerName)
	}
	if st.GSTIN == nil || *st.GSTIN != gstin {
		t.Errorf("gstin = %v want %s", st.GSTIN, gstin)
	}
	if st.PAN == nil || *st.PAN != pan {
		t.Errorf("pan = %v want %s", st.PAN, pan)
	}
	if st.GSTRegistrationID == nil || *st.GSTRegistrationID == "" {
		t.Error("store should have a gst_registration_id")
	}

	// Raw SELECT double-check.
	var rGstin, rPan *string
	if err := pool.QueryRow(ctx, `
		SELECT gr.gstin, gr.pan FROM stores s
		JOIN gst_registrations gr ON gr.id = s.gst_registration_id
		WHERE s.id = $1`, res.Principal.StoreID).Scan(&rGstin, &rPan); err != nil {
		t.Fatalf("raw registration read: %v", err)
	}
	if rGstin == nil || *rGstin != gstin || rPan == nil || *rPan != pan {
		t.Errorf("raw registration = gstin=%v pan=%v", rGstin, rPan)
	}
}

func TestSettingsUpdatePersistsAndAudits(t *testing.T) {
	resetAuth(t)
	storeID, _ := registerOwner(t, "9820000042")
	ctx := context.Background()

	out, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name:         "Renamed Store",
		Address:      "Bengaluru",
		Phone:        "9820000043",
		OwnerName:    "New Owner",
		MaxEmployees: 3,
	})
	if err != nil {
		t.Fatalf("update store details: %v", err)
	}
	if out.Name != "Renamed Store" || out.Address != "Bengaluru" || out.Phone != "9820000043" {
		t.Errorf("updated store = %+v", out)
	}
	if out.OwnerName != "New Owner" {
		t.Errorf("owner name = %q want New Owner", out.OwnerName)
	}
	if out.MaxEmployees != 3 {
		t.Errorf("max_employees = %d want 3", out.MaxEmployees)
	}

	// Owner user row reflects the new name.
	var userName string
	if err := pool.QueryRow(ctx, `SELECT name FROM users WHERE id = (
		SELECT user_id FROM store_memberships WHERE store_id = $1 AND role = 'STORE_OWNER')`, storeID).Scan(&userName); err != nil {
		t.Fatalf("read owner name: %v", err)
	}
	if userName != "New Owner" {
		t.Errorf("db owner name = %q want New Owner", userName)
	}
}

func TestAddOptionalFieldsAfterRegister(t *testing.T) {
	resetAuth(t)
	storeID, _ := registerOwner(t, "9820000044")
	ctx := context.Background()

	gstin := "27AAPBC1234F1ZV"
	pan := "AAAAA0000A"
	expiry := "2030-01-15"
	out, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name:              "Main",
		Address:           "Pune",
		Phone:             "9820000044",
		OwnerName:         "Shop Owner",
		MaxEmployees:      2,
		GSTIN:             &gstin,
		PAN:               &pan,
		DrugLicenseNumber: "DL/999",
		DrugLicenseExpiry: &expiry,
	})
	if err != nil {
		t.Fatalf("add optional: %v", err)
	}
	if out.GSTIN == nil || *out.GSTIN != gstin {
		t.Errorf("gstin = %v want %s", out.GSTIN, gstin)
	}
	if out.PAN == nil || *out.PAN != pan {
		t.Errorf("pan = %v want %s", out.PAN, pan)
	}
	if out.DrugLicenseNumber != "DL/999" {
		t.Errorf("dl number = %q", out.DrugLicenseNumber)
	}
	if out.GSTRegistrationID == nil || *out.GSTRegistrationID == "" {
		t.Errorf("gst_registration_id should be set, got %v", out.GSTRegistrationID)
	}
}

func TestClearOptionalFields(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	// Register WITH optional fields so the store owns a registration we can
	// then clear (this sidesteps the separately-reported "add to minimal
	// store" defect and exercises only the clear path).
	gstin := "27AAPBC1234F1ZV"
	pan := "AAAAA0000A"
	expiry := "2030-01-15"
	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "Clear Owner", Phone: "9820000045", PasswordHash: fakePHC,
		BusinessName: "Clear Biz",
		GSTIN:        &gstin, PAN: &pan,
		StoreName: "Main", StoreAddress: "Mumbai", StorePhone: "9820000045",
		DrugLicenseNumber: "DL/999", DrugLicenseExpiry: &expiry,
	}, auth.HashSessionToken("clear-1"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	storeID := res.Principal.StoreID

	// Confirm the registration is present before clearing.
	if st, err := authRepo.GetStoreDetails(ctx, storeID); err != nil || st.GSTIN == nil || *st.GSTIN != gstin {
		t.Fatalf("precondition: expected gstin set, got %v (%v)", st, err)
	}

	empty := ""
	out, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name: "Main", Address: "Mumbai", Phone: "9820000045", OwnerName: "Clear Owner", MaxEmployees: 2,
		GSTIN: &empty, PAN: &empty, DrugLicenseNumber: "", DrugLicenseExpiry: &empty,
	})
	if err != nil {
		t.Fatalf("clear optional: %v", err)
	}
	if out.GSTIN != nil && *out.GSTIN != "" {
		t.Errorf("gstin not cleared: %v", out.GSTIN)
	}
	if out.PAN != nil && *out.PAN != "" {
		t.Errorf("pan not cleared: %v", out.PAN)
	}
	if out.DrugLicenseNumber != "" {
		t.Errorf("dl number not cleared: %q", out.DrugLicenseNumber)
	}

	// Values actually cleared in the DB.
	var rGstin, rPan *string
	if err := pool.QueryRow(ctx, `
		SELECT gr.gstin, gr.pan FROM stores s
		JOIN gst_registrations gr ON gr.id = s.gst_registration_id
		WHERE s.id = $1`, storeID).Scan(&rGstin, &rPan); err != nil {
		t.Fatalf("raw read after clear: %v", err)
	}
	if rGstin != nil && *rGstin != "" {
		t.Errorf("db gstin not cleared: %v", rGstin)
	}
	if rPan != nil && *rPan != "" {
		t.Errorf("db pan not cleared: %v", rPan)
	}
}

func TestMandatoryValidationNoDBChange(t *testing.T) {
	resetAuth(t)
	storeID, _ := registerOwner(t, "9820000046")
	ctx := context.Background()

	if _, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name: "Main", Address: "Pune", Phone: "9820000046", OwnerName: "Shop Owner", MaxEmployees: 2,
	}); err != nil {
		t.Fatalf("baseline update: %v", err)
	}
	beforeName, beforeAddr, beforePhone, _, _, _, _, _ := rawStore(t, storeID)

	cases := map[string]repository.StoreUpdate{
		"empty-name":    {Name: "", Address: "Pune", Phone: "9820000046", OwnerName: "Shop Owner", MaxEmployees: 2},
		"empty-address": {Name: "Main", Address: "", Phone: "9820000046", OwnerName: "Shop Owner", MaxEmployees: 2},
		"empty-phone":   {Name: "Main", Address: "Pune", Phone: "", OwnerName: "Shop Owner", MaxEmployees: 2},
		"empty-owner":   {Name: "Main", Address: "Pune", Phone: "9820000046", OwnerName: "", MaxEmployees: 2},
	}
	for name, upd := range cases {
		if _, err := authRepo.UpdateStoreDetails(ctx, storeID, upd); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
		gotName, gotAddr, gotPhone, _, _, _, _, _ := rawStore(t, storeID)
		if gotName != beforeName || gotAddr != beforeAddr || gotPhone != beforePhone {
			t.Errorf("%s: DB changed after rejected update: name=%q addr=%q phone=%q", name, gotName, gotAddr, gotPhone)
		}
	}
}

func TestOptionalEmptyIsAllowed(t *testing.T) {
	resetAuth(t)
	storeID, _ := registerOwner(t, "9820000047")
	ctx := context.Background()

	empty := ""
	out, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name: "Main", Address: "Pune", Phone: "9820000047", OwnerName: "Shop Owner", MaxEmployees: 2,
		GSTIN: &empty, PAN: &empty, DrugLicenseNumber: "", DrugLicenseExpiry: &empty,
	})
	if err != nil {
		t.Fatalf("optional empty allowed: %v", err)
	}
	if out.Name != "Main" || out.Address != "Pune" || out.Phone != "9820000047" || out.OwnerName != "Shop Owner" {
		t.Errorf("mandatory not preserved: %+v", out)
	}
}

func TestStoreIsolationUpdateOnlyAffectsTarget(t *testing.T) {
	resetAuth(t)
	storeA, _ := registerOwner(t, "9820000048")
	ctx := context.Background()

	// Seed a second store with a raw INSERT (mirrors TestClientStoreIDIsIgnored).
	storeB := "ffffffff-2222-3333-4444-555555555555"
	if _, err := pool.Exec(ctx, `
		INSERT INTO stores (id, name, address, phone, is_active, max_employees)
		VALUES ($1, 'Store B', 'Goa', '9820000099', true, 2)
		ON CONFLICT (id) DO NOTHING`, storeB); err != nil {
		t.Fatalf("seed store B: %v", err)
	}

	if _, err := authRepo.UpdateStoreDetails(ctx, storeA, repository.StoreUpdate{
		Name: "Store A Renamed", Address: "Mumbai", Phone: "9820000049", OwnerName: "Owner A", MaxEmployees: 2,
	}); err != nil {
		t.Fatalf("update store A: %v", err)
	}

	nameB, addrB, phoneB, _, _, _, _, _ := rawStore(t, storeB)
	if nameB != "Store B" || addrB != "Goa" || phoneB != "9820000099" {
		t.Errorf("store B mutated: name=%q addr=%q phone=%q", nameB, addrB, phoneB)
	}
}

func TestExistingDataRegressionUntouchedFields(t *testing.T) {
	resetAuth(t)
	storeID, _ := registerOwner(t, "9820000050")
	ctx := context.Background()

	// Force known values for the fields we want to guarantee stay intact.
	if _, err := pool.Exec(ctx, `
		UPDATE stores SET is_active = true, max_employees = 4, created_at = '2020-01-02'::timestamp
		WHERE id = $1`, storeID); err != nil {
		t.Fatalf("set regression values: %v", err)
	}
	var beforeCreated string
	if err := pool.QueryRow(ctx, `SELECT created_at::text FROM stores WHERE id = $1`, storeID).Scan(&beforeCreated); err != nil {
		t.Fatalf("read created_at: %v", err)
	}

	if _, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name: "Reg Store", Address: "Delhi", Phone: "9820000051", OwnerName: "Reg Owner", MaxEmployees: 4,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	_, _, _, _, _, isActive, maxEmp, createdAfter := rawStore(t, storeID)
	if !isActive {
		t.Error("is_active flipped to false")
	}
	if maxEmp != 4 {
		t.Errorf("max_employees = %d want 4 (unchanged)", maxEmp)
	}
	if createdAfter != beforeCreated {
		t.Errorf("created_at changed: %q -> %q", beforeCreated, createdAfter)
	}
}

func TestHistoricalInvoiceSnapshotPreservedAfterStoreUpdate(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	_, batchID := seedGSTMedicine(t)
	seedState27Store(t, ctx)
	paymentType := models.PaymentCash
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: paymentType,
		Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("checkout: %v", err)
	}
	invID := res.Invoice.ID

	var rate, cgst, sgst float64
	if err := pool.QueryRow(ctx,
		`SELECT sii.gst_rate, sii.cgst_amount, sii.sgst_amount FROM sales_invoice_items sii WHERE sii.invoice_id = $1`, invID).
		Scan(&rate, &cgst, &sgst); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	if _, err := authRepo.UpdateStoreDetails(ctx, testutil.StoreID, repository.StoreUpdate{
		Name: "Hist Store", Address: "Pune", Phone: "9820000052", OwnerName: "Hist Owner", MaxEmployees: 2,
	}); err != nil {
		t.Fatalf("update store: %v", err)
	}

	var pRate, pCgst, pSgst float64
	if err := pool.QueryRow(ctx,
		`SELECT sii.gst_rate, sii.cgst_amount, sii.sgst_amount FROM sales_invoice_items sii WHERE sii.invoice_id = $1`, invID).
		Scan(&pRate, &pCgst, &pSgst); err != nil {
		t.Fatalf("re-read snapshot: %v", err)
	}
	if pRate != rate || pCgst != cgst || pSgst != sgst {
		t.Errorf("historical invoice snapshot mutated after store update: got %.2f/%.2f/%.2f want %.2f/%.2f/%.2f",
			pRate, pCgst, pSgst, rate, cgst, sgst)
	}
}

func TestReloadPersistenceAfterUpdate(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	gstin := "27AAPBC1234F1ZV"
	pan := "AAAAA0000A"
	expiry := "2031-06-30"
	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "Reload Owner", Phone: "9820000053", PasswordHash: fakePHC,
		BusinessName: "Reload Biz",
		GSTIN:        &gstin, PAN: &pan,
		StoreName:          "Reload Store",
		StoreAddress:       "Kolkata",
		StorePhone:         "9820000053",
		DrugLicenseNumber:  "DL/777",
		DrugLicenseExpiry:  &expiry,
	}, auth.HashSessionToken("reload-1"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	storeID := res.Principal.StoreID

	// A settings update that preserves the full optional values, then a fresh
	// Read back — every value must survive the round-trip intact.
	if _, err := authRepo.UpdateStoreDetails(ctx, storeID, repository.StoreUpdate{
		Name: "Reload Store", Address: "Kolkata", Phone: "9820000054", OwnerName: "Reload Owner", MaxEmployees: 5,
		GSTIN: &gstin, PAN: &pan, DrugLicenseNumber: "DL/777", DrugLicenseExpiry: &expiry,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	st, err := authRepo.GetStoreDetails(ctx, storeID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if st.Name != "Reload Store" || st.Address != "Kolkata" || st.Phone != "9820000054" ||
		st.OwnerName != "Reload Owner" || st.MaxEmployees != 5 {
		t.Errorf("reloaded store = %+v", st)
	}
	if st.DrugLicenseNumber != "DL/777" || st.DrugLicenseExpiry == nil || st.DrugLicenseExpiry.String() != "2031-06-30" {
		t.Errorf("reloaded dl = %q/%v", st.DrugLicenseNumber, st.DrugLicenseExpiry)
	}
	if st.GSTIN == nil || *st.GSTIN != gstin || st.PAN == nil || *st.PAN != pan {
		t.Errorf("reloaded gstin/pan = %v/%v", st.GSTIN, st.PAN)
	}

	// Raw SELECT as a final authoritative check.
	var rName, rAddr, rPhone, rDl string
	var rDlE *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT name, address, phone, drug_license_number, drug_license_expiry
		FROM stores WHERE id = $1`, storeID).Scan(&rName, &rAddr, &rPhone, &rDl, &rDlE); err != nil {
		t.Fatalf("raw reload: %v", err)
	}
	if rName != "Reload Store" || rAddr != "Kolkata" || rPhone != "9820000054" || rDl != "DL/777" {
		t.Errorf("raw reload mismatch: %q/%q/%q/%q", rName, rAddr, rPhone, rDl)
	}
	if rDlE == nil || rDlE.Format("2006-01-02") != "2031-06-30" {
		t.Errorf("raw dl expiry = %v", rDlE)
	}
}
