package repository_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

func creditFixture(t *testing.T) fixture {
	t.Helper()
	return seedFixture(t, 500, 1000)
}

func TestCreditSaleWritesLedgerEntry(t *testing.T) {
	reset(t)
	fx := creditFixture(t)
	ctx := context.Background()

	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &fx.CustomerID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("credit sale: %v", err)
	}

	entries, err := custRepo.Ledger(ctx, fx.CustomerID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d want 1", len(entries))
	}
	e := entries[0]
	if e.EntryType != "CREDIT_SALE" || e.Amount != 60 || e.BalanceAfter != 60 {
		t.Errorf("entry wrong: %+v", e)
	}
	if !strings.Contains(e.Notes, "Invoice ") {
		t.Errorf("notes should reference invoice: %q", e.Notes)
	}

	cust, _ := custRepo.GetByID(ctx, fx.CustomerID)
	if cust.CurrentBalance != res.Invoice.TotalAmount {
		t.Errorf("balance %.2f != invoice total %.2f", cust.CurrentBalance, res.Invoice.TotalAmount)
	}
}

func TestCashSaleWritesNoLedgerEntry(t *testing.T) {
	reset(t)
	fx := creditFixture(t)

	if _, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
	}); err != nil {
		t.Fatal(err)
	}

	entries, _ := custRepo.Ledger(context.Background(), fx.CustomerID, 0)
	if len(entries) != 0 {
		t.Errorf("cash sale must not touch ledger, got %d entries", len(entries))
	}
}

func TestPaymentFlowFullAndPartial(t *testing.T) {
	reset(t)
	fx := creditFixture(t)
	ctx := context.Background()
	cid := fx.CustomerID

	for i := 0; i < 2; i++ {
		if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
			PaymentType: models.PaymentCredit,
			CustomerID:  &cid,
			Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
		}); err != nil {
			t.Fatalf("sale %d: %v", i, err)
		}
	}

	cust, _ := custRepo.GetByID(ctx, cid)
	if cust.CurrentBalance != 60 {
		t.Fatalf("balance = %.2f want 60.00", cust.CurrentBalance)
	}

	if _, _, err := custRepo.RecordPayment(ctx, cid, 25, "part payment cash"); err != nil {
		t.Fatalf("partial payment: %v", err)
	}
	cust, _ = custRepo.GetByID(ctx, cid)
	if cust.CurrentBalance != 35 {
		t.Errorf("balance after part payment = %.2f want 35.00", cust.CurrentBalance)
	}

	if _, _, err := custRepo.RecordPayment(ctx, cid, 35, "settled in full"); err != nil {
		t.Fatalf("full payment: %v", err)
	}
	cust, _ = custRepo.GetByID(ctx, cid)
	if cust.CurrentBalance != 0 {
		t.Errorf("balance after full settlement = %.2f want 0.00", cust.CurrentBalance)
	}

	entries, err := custRepo.Ledger(ctx, cid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("ledger entries = %d want 4 (2 sales + 2 payments)", len(entries))
	}
	if entries[0].EntryType != "PAYMENT" || entries[0].Amount != -35 || entries[0].BalanceAfter != 0 {
		t.Errorf("newest entry wrong: %+v", entries[0])
	}
	if entries[1].BalanceAfter != 35 || entries[2].BalanceAfter != 60 || entries[3].BalanceAfter != 30 {
		t.Errorf("running balances broken: %+v", entries)
	}
}

func TestOverpaymentRejectedAtomically(t *testing.T) {
	reset(t)
	fx := creditFixture(t)
	ctx := context.Background()
	cid := fx.CustomerID

	if _, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &cid,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
	}); err != nil {
		t.Fatal(err)
	}

	before, _ := custRepo.GetByID(ctx, cid)
	_, _, err := custRepo.RecordPayment(ctx, cid, before.CurrentBalance+1, "overpay attempt")
	if err == nil || !strings.Contains(err.Error(), "exceeds outstanding") {
		t.Fatalf("want exceeds-outstanding error, got %v", err)
	}

	after, _ := custRepo.GetByID(ctx, cid)
	if after.CurrentBalance != before.CurrentBalance {
		t.Errorf("rejected overpayment mutated balance: %.2f → %.2f",
			before.CurrentBalance, after.CurrentBalance)
	}
	entries, _ := custRepo.Ledger(ctx, cid, 0)
	if len(entries) != 1 {
		t.Errorf("rejected overpayment wrote ledger rows: %d", len(entries))
	}
}

func TestInvalidPaymentsRejected(t *testing.T) {
	reset(t)
	fx := creditFixture(t)

	for _, amount := range []float64{0, -10} {
		_, _, err := custRepo.RecordPayment(context.Background(), fx.CustomerID, amount, "")
		if err == nil {
			t.Errorf("amount %.2f must be rejected", amount)
		}
	}

	_, _, err := custRepo.RecordPayment(context.Background(),
		"00000000-0000-0000-0000-000000000000", 5, "")
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("unknown customer should be ErrNotFound, got %v", err)
	}
}
