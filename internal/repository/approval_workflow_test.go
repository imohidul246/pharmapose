package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

var (
	prRepo *repository.PurchaseRequestRepo
	saRepo *repository.StockAuditRequestRepo
)

// workflowTenant registers an owner + employee under testutil.StoreID so the
// approval gates (requester != reviewer) work.
func workflowTenant(t *testing.T) (ownerID, employeeID, storeID string) {
	t.Helper()
	ctx := context.Background()
	var ownerIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (name, phone, password_hash)
		VALUES ('Flow Owner', '9820000090', $1) RETURNING id::text`, fakePHC).Scan(&ownerIDStr); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO store_memberships (store_id, user_id, role)
		VALUES ($1, $2, 'STORE_OWNER')`, testutil.StoreID, ownerIDStr); err != nil {
		t.Fatalf("membership owner: %v", err)
	}
	emp, err := authRepo.CreateEmployee(ctx, testutil.StoreID, "Flow Emp", "9820000091", fakePHC)
	if err != nil {
		t.Fatalf("create employee: %v", err)
	}
	return ownerIDStr, emp.ID, testutil.StoreID
}

func validPurchaseSnapshot(t *testing.T, medicineID string) *repository.PurchaseInput {
	t.Helper()
	return &repository.PurchaseInput{
		StoreID:      sid(testutil.StoreID),
		InvoiceNo:    "RQ-IN-1",
		SupplierName: "Request Supplier",
		Items: []repository.PurchaseItemInput{{
			MedicineID:    medicineID,
			BatchNumber:   "RQ-B1",
			ExpiryDate:    models.NewDate(time.Now().AddDate(2, 0, 0)),
			Quantity:      50,
			PurchasePrice: 10,
			SalePrice:     15,
		}},
	}
}

func TestPurchaseRequestWorkflow(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()
	ownerID, empID, storeID := workflowTenant(t)
	medID := seedOneMedicine(t, "Request Med "+uniqueSuffix())

	req, err := prRepo.Create(ctx, storeID, empID, validPurchaseSnapshot(t, medID))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if req.Status != "PENDING" || req.RequestedBy != empID {
		t.Fatalf("request = %+v want PENDING by employee", req)
	}

	// Self-approval is refused.
	if _, _, err := prRepo.Approve(ctx, storeID, req.ID, empID); !errors.Is(err, models.ErrRequesterApprover) {
		t.Fatalf("self-approve: want ErrRequesterApprover, got %v", err)
	}

	po, items, err := prRepo.Approve(ctx, storeID, req.ID, ownerID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 purchase item, got %d", len(items))
	}

	// The batch created by the approval carries the right quantity.
	batch, err := medRepo.FindBatchByNumber(ctx, testutil.StoreID, medID, "RQ-B1")
	if err != nil || batch.CurrentStock != 50 {
		t.Errorf("batch = %+v err=%v, want 50 units", batch, err)
	}

	// Status flipped and the PO is attached.
	got, err := prRepo.Get(ctx, storeID, req.ID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Status != "APPROVED" || got.PurchaseID == nil || *got.PurchaseID != po.ID {
		t.Errorf("post-approval request = %+v, want APPROVED linked to %s", got, po.ID)
	}

	// Re-approving an APPROVED request is refused.
	if _, _, err := prRepo.Approve(ctx, storeID, req.ID, ownerID); !errors.Is(err, models.ErrRequestNotPending) {
		t.Fatalf("double-approve: want ErrRequestNotPending, got %v", err)
	}
}

func seedOneMedicine(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	m := &models.Medicine{Name: name, SaltComposition: "Paracetamol", Manufacturer: "VM"}
	if err := medRepo.Create(ctx, testutil.StoreID, m); err != nil {
		t.Fatalf("create medicine: %v", err)
	}
	return m.ID
}

func TestPurchaseRequestRejectAndCancel(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()
	ownerID, empID, storeID := workflowTenant(t)
	medID := seedOneMedicine(t, "Reject Med "+uniqueSuffix())

	// Reject requires a reason.
	r1, err := prRepo.Create(ctx, storeID, empID, validPurchaseSnapshot(t, medID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := prRepo.Approve(ctx, storeID, r1.ID, empID); !errors.Is(err, models.ErrRequesterApprover) {
		t.Fatal("self-approve must be refused")
	}
	if _, err := prRepo.Reject(ctx, storeID, r1.ID, ownerID, ""); err == nil {
		t.Fatal("empty rejection reason must be refused")
	}
	rejected, err := prRepo.Reject(ctx, storeID, r1.ID, ownerID, "duplicate supply")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != "REJECTED" || rejected.RejectionReason != "duplicate supply" {
		t.Errorf("rejected = %+v", rejected)
	}
	if _, err := prRepo.Reject(ctx, storeID, r1.ID, ownerID, "again"); !errors.Is(err, models.ErrRequestNotPending) {
		t.Fatalf("re-reject: want ErrRequestNotPending, got %v", err)
	}

	// A non-requester cannot cancel; the requester can.
	r2, err := prRepo.Create(ctx, storeID, empID, validPurchaseSnapshot(t, medID))
	if err != nil {
		t.Fatalf("create r2: %v", err)
	}
	if _, err := prRepo.Cancel(ctx, storeID, r2.ID, ownerID); !errors.Is(err, models.ErrNotAMember) {
		t.Fatalf("foreign cancel: want ErrNotAMember, got %v", err)
	}
	cancelled, err := prRepo.Cancel(ctx, storeID, r2.ID, empID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != "CANCELLED" {
		t.Errorf("status = %q want CANCELLED", cancelled.Status)
	}

	// Cross-store scrutiny: a request from another tenant is invisible here.
	other, err := authRepo.Register(ctx, repository.RegisterInput{
		Name: "Other Owner", Phone: "9820000092", PasswordHash: fakePHC,
		BusinessName: "Other", StoreName: "Branch", StoreAddress: "Pune", StorePhone: "9820000092",
	}, "th-other", "ip", "ua")
	if err != nil {
		t.Fatalf("register other tenant: %v", err)
	}
	if _, err := prRepo.Get(ctx, other.Principal.StoreID, r1.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("cross-store read: want ErrNotFound, got %v", err)
	}
	if _, _, err := prRepo.Approve(ctx, other.Principal.StoreID, r1.ID, other.User.ID); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("cross-store approve: want ErrNotFound, got %v", err)
	}
}

func TestStockAuditRequestWorkflow(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()
	ownerID, empID, storeID := workflowTenant(t)
	fx := seedFixture(t, 100, 0)

	req, outed, err := saRepo.Create(ctx, storeID, empID, "yearly count",
		[]repository.AuditItemInput{{BatchID: fx.BatchIDs[0], PhysicalQuantity: 103, Reason: "shelf count"}})
	if err != nil {
		t.Fatalf("create audit: %v", err)
	}
	if len(outed) != 1 || outed[0].SystemQuantity != 100 {
		t.Fatalf("snapshot = %+v want system 100", outed)
	}
	if req.Status != "PENDING" {
		t.Fatalf("status = %q want PENDING", req.Status)
	}

	// Self-approval refused.
	if _, _, err := saRepo.Approve(ctx, storeID, req.ID, empID); !errors.Is(err, models.ErrRequesterApprover) {
		t.Fatalf("self-approve: want ErrRequesterApprover, got %v", err)
	}

	journal, items, err := saRepo.Approve(ctx, storeID, req.ID, ownerID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if len(items) != 1 || items[0].VarianceQuantity != 3 || items[0].PhysicalStock != 103 {
		t.Errorf("items = %+v want variance +3", items)
	}
	if journal.ID == "" {
		t.Error("journal id is empty")
	}

	// Live stock now reflects the counted quantity.
	batch, err := medRepo.FindBatchByNumber(ctx, testutil.StoreID, fx.MedicineID, "FIX-B1")
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	if batch.CurrentStock != 103 {
		t.Errorf("stock = %d want 103", batch.CurrentStock)
	}

	got, gotItems, err := saRepo.Get(ctx, storeID, req.ID)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if got.Status != "APPROVED" || got.JournalID == nil || *got.JournalID != journal.ID {
		t.Errorf("post-approval = %+v want APPROVED + journal", got)
	}
	if len(gotItems) != 1 || gotItems[0].MedicineName == "" {
		t.Errorf("items = %+v", gotItems)
	}

	if _, _, err := saRepo.Approve(ctx, storeID, req.ID, ownerID); !errors.Is(err, models.ErrRequestNotPending) {
		t.Fatalf("double-approve: want ErrRequestNotPending, got %v", err)
	}
}

func TestStockAuditRequestStaleAndValidation(t *testing.T) {
	resetAuth(t)
	ctx := context.Background()
	ownerID, empID, storeID := workflowTenant(t)

	// Validation: empty items, missing reason, negative physical.
	if _, _, err := saRepo.Create(ctx, storeID, empID, "n", nil); err == nil {
		t.Fatal("empty items must be rejected")
	}
	fx := seedFixture(t, 100, 0)
	if _, _, err := saRepo.Create(ctx, storeID, empID, "n",
		[]repository.AuditItemInput{{BatchID: fx.BatchIDs[0], PhysicalQuantity: 99, Reason: ""}}); err == nil {
		t.Fatal("missing reason must be rejected")
	}
	if _, _, err := saRepo.Create(ctx, storeID, empID, "n",
		[]repository.AuditItemInput{{BatchID: fx.BatchIDs[0], PhysicalQuantity: -1, Reason: "r"}}); err == nil {
		t.Fatal("negative physical must be rejected")
	}

	// Stock moved after the audit was submitted → approval refused as stale.
	req, _, err := saRepo.Create(ctx, storeID, empID, "q4",
		[]repository.AuditItemInput{{BatchID: fx.BatchIDs[0], PhysicalQuantity: 90, Reason: "sold"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 1}},
	}); err != nil {
		t.Fatalf("sell a unit: %v", err)
	}
	if _, _, err := saRepo.Approve(ctx, storeID, req.ID, ownerID); !errors.Is(err, models.ErrStaleStock) {
		t.Fatalf("stale approve: want ErrStaleStock, got %v", err)
	}

	// Reject/cancel flow works end to end.
	req2, _, err := saRepo.Create(ctx, storeID, empID, "q5",
		[]repository.AuditItemInput{{BatchID: fx.BatchIDs[0], PhysicalQuantity: 100, Reason: "shelf"}})
	if err != nil {
		t.Fatalf("create req2: %v", err)
	}
	// A non-requester cannot cancel while it is still pending.
	if _, err := saRepo.Cancel(ctx, storeID, req2.ID, ownerID); !errors.Is(err, models.ErrNotAMember) {
		t.Fatalf("foreign cancel: want ErrNotAMember, got %v", err)
	}
	// The requester can withdraw it.
	if _, err := saRepo.Cancel(ctx, storeID, req2.ID, empID); err != nil {
		t.Fatalf("cancel by requester: %v", err)
	}
	if _, err := saRepo.Reject(ctx, storeID, req2.ID, ownerID, "nope"); !errors.Is(err, models.ErrRequestNotPending) {
		t.Fatalf("reject cancelled: want ErrRequestNotPending, got %v", err)
	}
}

func uniqueSuffix() string {
	return "x" + time.Now().Format("150405.000000")
}