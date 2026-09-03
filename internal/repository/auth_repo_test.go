package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

var authRepo *repository.AuthRepo

// resetAuth truncates auth + store tables (users, sessions, memberships,
// audit trail) so this file's tests are independent of the rest of the suite.
func resetAuth(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		TRUNCATE customer_ledger, reconciliation_items, reconciliation_journals,
		         sales_credit_notes, sales_invoice_items, sales_invoices,
		         purchase_order_items, purchase_orders,
		         stock_audit_request_items, stock_audit_requests,
		         purchase_requests,
		         gstr2b_imports, gstr2b_import_batches, medicine_tax_config,
		         gst_registrations, stores, businesses, suppliers,
		         batches, customers, medicines,
		         audit_logs, sessions, store_memberships, users CASCADE`)
	if err != nil {
		t.Fatalf("resetAuth: %v", err)
	}
	if err := testutil.SeedStore(context.Background(), pool); err != nil {
		t.Fatalf("resetAuth seed store: %v", err)
	}
}

const fakePHC = "phc-argon2id-test-hash"

func validGSTIN() string { return "27AAPBC1234F1ZV" }

func TestRegisterCreatesTenantAndSession(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	rawToken := "register-token-abc123"
	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name:         "Owner One",
		Phone:        "+91 98200 00000",
		PasswordHash: fakePHC,
		BusinessName: "Owner One Biz",
		TradeName:    "OO",
		GSTIN:        strPtr(validGSTIN()),
		StoreName:    "Main Store",
		StoreAddress: "Mumbai",
		StorePhone:   "9820000000",
	}, auth.HashSessionToken(rawToken), "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if res.User.Phone != "919820000000" {
		t.Errorf("normalized phone = %q want 919820000000", res.User.Phone)
	}
	if res.Principal.Role != auth.RoleStoreOwner {
		t.Errorf("role = %q want STORE_OWNER", res.Principal.Role)
	}
	if res.Principal.StoreID == "" {
		t.Error("principal store id is empty")
	}
	if !res.ExpiresAt.After(time.Now()) {
		t.Error("expires_at should be in the future")
	}

	// The registration session resolves to a live owner principal.
	p, err := authRepo.ValidateSession(ctx, auth.HashSessionToken(rawToken))
	if err != nil {
		t.Fatalf("validate session: %v", err)
	}
	if p.UserID != res.User.ID || p.Role != auth.RoleStoreOwner {
		t.Errorf("principal = %+v, want owner for user %s", p, res.User.ID)
	}
	if p.StoreID != res.Principal.StoreID {
		t.Errorf("store mismatch: %q vs %q", p.StoreID, res.Principal.StoreID)
	}

	// The seeded store now carries the GST registration id.
	store, err := authRepo.GetStore(ctx, p.StoreID)
	if err != nil {
		t.Fatalf("get store: %v", err)
	}
	if store.GSTRegistrationID == nil || *store.GSTRegistrationID == "" {
		t.Error("store should be linked to the registration")
	}
}

func TestRegisterValidationAndDuplicates(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	base := repository.RegisterInput{
		Name:         "Dupe Owner",
		Phone:        "9820000001",
		PasswordHash: fakePHC,
		BusinessName: "Dupe Biz",
		StoreName:    "Main",
		StoreAddress: "Mumbai",
		StorePhone:   "9820000001",
	}

	// Missing store name is rejected.
	if _, err := authRepo.Register(ctx, func() repository.RegisterInput {
		in := base
		in.StoreName = ""
		return in
	}(), "th1", "ip", "ua"); err == nil {
		t.Error("empty store_name must be rejected")
	}

	// Empty password hash is rejected.
	if _, err := authRepo.Register(ctx, func() repository.RegisterInput {
		in := base
		in.PasswordHash = ""
		return in
	}(), "th1", "ip", "ua"); err == nil {
		t.Error("empty password_hash must be rejected")
	}

	// Structurally invalid GSTIN is rejected.
	if _, err := authRepo.Register(ctx, func() repository.RegisterInput {
		in := base
		in.GSTIN = strPtr("not-a-gstin")
		return in
	}(), "th1", "ip", "ua"); err == nil {
		t.Error("invalid gstin must be rejected")
	}

	if _, err := authRepo.Register(ctx, base, "th2", "ip", "ua"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// Same phone must be rejected with the friendly conflict message.
	if _, err := authRepo.Register(ctx, base, "th3", "ip", "ua"); err == nil {
		t.Fatal("duplicate phone must be rejected")
	}
}

func TestSessionLifecycleAndDeactivation(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	owner, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "Session Owner", Phone: "9820000002", PasswordHash: fakePHC,
		BusinessName: "Sess Biz", StoreName: "Main", StoreAddress: "Mumbai", StorePhone: "9820000002",
	}, auth.HashSessionToken("sess-1"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	emp, err := authRepo.CreateEmployee(ctx, owner.Principal.StoreID, "Session Emp", "9820000022", fakePHC)
	if err != nil {
		t.Fatalf("create employee: %v", err)
	}

	tokenOK := auth.HashSessionToken("live-token")
	if _, err := authRepo.CreateSession(ctx, emp.ID, tokenOK, "ip", "ua"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, tokenOK); err != nil {
		t.Fatalf("live token must validate: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, auth.HashSessionToken("ghost")); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("unknown token: want ErrUnauthorized, got %v", err)
	}

	// An expired session is rejected.
	expired := auth.HashSessionToken("expired-token")
	if _, err := authRepo.CreateSession(ctx, emp.ID, expired, "ip", "ua"); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at = now() - interval '1 day'
		WHERE token_hash = $1`, expired); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, expired); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("expired token: want ErrUnauthorized, got %v", err)
	}

	// Deactivating the employee's membership kills all sessions instantly.
	if err := authRepo.SetMembershipActive(ctx, owner.Principal.StoreID, emp.ID, false); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, tokenOK); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("deactivated user token: want ErrUnauthorized, got %v", err)
	}

	// Re-activation lets a brand-new session in again.
	if err := authRepo.SetMembershipActive(ctx, owner.Principal.StoreID, emp.ID, true); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	renewed := auth.HashSessionToken("renewed-token")
	if _, err := authRepo.CreateSession(ctx, emp.ID, renewed, "ip", "ua"); err != nil {
		t.Fatalf("recreate session: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, renewed); err != nil {
		t.Fatalf("reactivated token must validate: %v", err)
	}

	// DeleteSessionsForUser abolishes every session at once.
	if _, err := authRepo.DeleteSessionsForUser(ctx, emp.ID); err != nil {
		t.Fatalf("delete sessions: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, renewed); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("cleared token: want ErrUnauthorized, got %v", err)
	}
}

func TestOwnerCannotBeDeactivated(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "Owner", Phone: "9820000003", PasswordHash: fakePHC,
		BusinessName: "B", StoreName: "Main", StoreAddress: "Mumbai", StorePhone: "9820000003",
	}, auth.HashSessionToken("owner-t"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := authRepo.SetMembershipActive(ctx, res.Principal.StoreID, res.User.ID, false); !errors.Is(err, models.ErrCannotDisableOwner) {
		t.Fatalf("deactivate owner: want ErrCannotDisableOwner, got %v", err)
	}
}

func TestSeatCapEnforced(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	// testutil.SeedStore ships with max_employees = 2.
	if n, err := authRepo.CountActiveEmployees(ctx, testutil.StoreID); err != nil || n != 0 {
		t.Fatalf("expected 0 active employees, got %d (%v)", n, err)
	}

	for i, name := range []string{"Emp One", "Emp Two"} {
		phone := ""
		switch i {
		case 0:
			phone = "9820000010"
		case 1:
			phone = "9820000011"
		}
		if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, name, phone, fakePHC); err != nil {
			t.Fatalf("create employee %d: %v", i, err)
		}
	}

	if n, _ := authRepo.CountActiveEmployees(ctx, testutil.StoreID); n != 2 {
		t.Fatalf("expected 2 active employees, got %d", n)
	}
	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Emp Three", "9820000012", fakePHC); !errors.Is(err, models.ErrEmployeeLimitReached) {
		t.Fatalf("third employee: want ErrEmployeeLimitReached, got %v", err)
	}

	// Seat freed by deactivation can be re-filled.
	members, err := authRepo.ListMembers(ctx, testutil.StoreID)
	if err != nil || len(members) != 2 {
		t.Fatalf("list members: len=%d err=%v", len(members), err)
	}
	if err := authRepo.SetMembershipActive(ctx, testutil.StoreID, members[0].UserID, false); err != nil {
		t.Fatalf("deactivate emp one: %v", err)
	}
	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Emp Four", "9820000013", fakePHC); err != nil {
		t.Fatalf("fill freed seat: %v", err)
	}
	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Emp Five", "9820000014", fakePHC); !errors.Is(err, models.ErrEmployeeLimitReached) {
		t.Fatalf("over cap after refill: want ErrEmployeeLimitReached, got %v", err)
	}

	// Duplicate phone on an employee invite is rejected.
	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Six", "9820000013", fakePHC); err == nil {
		t.Fatal("duplicate employee phone must be rejected")
	}
}

func TestStoreSettingsSeatResize(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	if _, err := authRepo.UpdateStoreSettings(ctx, testutil.StoreID, "Main", "Mumbai", -1); err == nil {
		t.Fatal("negative max_employees must be rejected")
	}
	if _, err := authRepo.UpdateStoreSettings(ctx, testutil.StoreID, "", "Mumbai", 5); err == nil {
		t.Fatal("empty store name must be rejected")
	}

	store, err := authRepo.UpdateStoreSettings(ctx, testutil.StoreID, "Main", "Mumbai", 5)
	if err != nil {
		t.Fatalf("raise cap: %v", err)
	}
	if store.MaxEmployees != 5 {
		t.Errorf("max_employees = %d want 5", store.MaxEmployees)
	}

	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "A", "9820000020", fakePHC); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "B", "9820000021", fakePHC); err != nil {
		t.Fatalf("create B: %v", err)
	}
	// Dropping below currently-occupied seats is refused.
	if _, err := authRepo.UpdateStoreSettings(ctx, testutil.StoreID, "Main", "Mumbai", 1); err == nil {
		t.Fatal("cap below active seats must be rejected")
	}
}

func TestChangePasswordRotatesHashAndKeepsSession(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	res, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "PW Owner", Phone: "9820000030", PasswordHash: fakePHC,
		BusinessName: "B", StoreName: "Main", StoreAddress: "Mumbai", StorePhone: "9820000030",
	}, auth.HashSessionToken("keep-this"), "ip", "ua")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	other := auth.HashSessionToken("revoke-me")
	if _, err := authRepo.CreateSession(ctx, res.User.ID, other, "ip", "ua"); err != nil {
		t.Fatalf("create other session: %v", err)
	}

	if err := authRepo.ChangePassword(ctx, res.User.ID, "phc-argon2id-new", auth.HashSessionToken("keep-this")); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if hash, err := authRepo.GetPasswordHash(ctx, res.User.ID); err != nil || hash != "phc-argon2id-new" {
		t.Fatalf("hash = %q err=%v want new hash", hash, err)
	}
	if _, err := authRepo.ValidateSession(ctx, auth.HashSessionToken("keep-this")); err != nil {
		t.Fatalf("current session must survive: %v", err)
	}
	if _, err := authRepo.ValidateSession(ctx, other); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("other session: want ErrUnauthorized, got %v", err)
	}
}

func TestListMembersAndNotAMember(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()

	emp, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Ramesh", "9820000040", fakePHC)
	if err != nil {
		t.Fatalf("create employee: %v", err)
	}
	if err := authRepo.SetMembershipActive(ctx, testutil.StoreID, "00000000-0000-0000-0000-00000000ffff", false); !errors.Is(err, models.ErrNotAMember) {
		t.Fatalf("foreign user: want ErrNotAMember, got %v", err)
	}

	members, err := authRepo.ListMembers(ctx, testutil.StoreID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected exactly 1 member, got %d", len(members))
	}
	if members[0].UserID != emp.ID || members[0].Role != "EMPLOYEE" {
		t.Errorf("member = %+v, want Ramesh EMPLOYEE", members[0])
	}
}