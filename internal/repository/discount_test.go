package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

func TestCheckoutDiscounts(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 500, 10000)
	ctx := context.Background()

	// 6 × ₹15 = ₹90, 10% line discount = ₹9, net ₹81.
	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  fx.BatchIDs[0],
			Quantity: 6,
			Discount: &repository.LineDiscount{Type: "percent", Value: 10},
		}},
	})
	if err != nil {
		t.Fatalf("percent discount checkout: %v", err)
	}
	if res.Invoice.DiscountTotal != 9 || res.Invoice.TotalAmount != 81 {
		t.Errorf("invoice: total=%.2f disc_total=%.2f want 81.00/9.00", res.Invoice.TotalAmount, res.Invoice.DiscountTotal)
	}

	// Flat rupee line discount: 3 × ₹15 = ₹45, discount ₹10, net ₹35.
	amt := &repository.LineDiscount{Type: "amount", Value: 10}
	res, err = saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  fx.BatchIDs[0],
			Quantity: 3,
			Discount: amt,
		}},
	})
	if err != nil {
		t.Fatalf("amount discount checkout: %v", err)
	}
	if res.Items[0].Subtotal != 35 || res.Invoice.TotalAmount != 35 {
		t.Errorf("amount line: subtotal=%.2f total=%.2f want 35.00", res.Items[0].Subtotal, res.Invoice.TotalAmount)
	}

	// Over-100% discount clamps the line to zero rather than going negative.
	hugePct := &repository.LineDiscount{Type: "percent", Value: 150}
	res, err = saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  fx.BatchIDs[0],
			Quantity: 2,
			Discount: hugePct,
		}},
	})
	if err != nil {
		t.Fatalf("over-discount checkout: %v", err)
	}
	if res.Invoice.TotalAmount != 0 || res.Items[0].Subtotal != 0 {
		t.Errorf("clamped total=%.2f want 0.00 (never negative)", res.Invoice.TotalAmount)
	}

	// Negative discount values are rejected outright.
	negative := &repository.LineDiscount{Type: "amount", Value: -5}
	_, err = saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{{
			BatchID:  fx.BatchIDs[0],
			Quantity: 1,
			Discount: negative,
		}},
	})
	if err == nil {
		t.Fatal("negative discount must be rejected")
	}
}

func TestCreditCheckUsesDiscountedTotal(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 28)

	cid := fx.CustomerID
	_, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &cid,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 2}},
	})
	if !assertCreditError(err) {
		t.Fatalf("gross ₹30 must breach ₹28 limit, got %v", err)
	}

	discounted := repository.CheckoutItemInput{
		BatchID: fx.BatchIDs[0], Quantity: 2,
		Discount: &repository.LineDiscount{Type: "percent", Value: 10},
	}
	res, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &cid,
		Items:       []repository.CheckoutItemInput{discounted},
	})
	if err != nil {
		t.Fatalf("discounted ₹27 must fit ₹28 limit: %v", err)
	}
	if res.Invoice.TotalAmount != 27 {
		t.Errorf("total=%.2f want 27.00", res.Invoice.TotalAmount)
	}

	cust, _ := custRepo.GetByID(context.Background(), cid)
	if cust.CurrentBalance != 27 {
		t.Errorf("balance=%.2f want 27.00", cust.CurrentBalance)
	}
}

func TestConflictingDuplicateLineDiscountsRejected(t *testing.T) {
	reset(t)
	fx := seedFixture(t, 100, 0)

	_, err := saleRepo.Checkout(context.Background(), &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCash,
		Items: []repository.CheckoutItemInput{
			{BatchID: fx.BatchIDs[0], Quantity: 1, Discount: &repository.LineDiscount{Type: "percent", Value: 10}},
			{BatchID: fx.BatchIDs[0], Quantity: 1, Discount: &repository.LineDiscount{Type: "amount", Value: 5}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting discount") {
		t.Fatalf("want conflicting-discount error, got %v", err)
	}
}