package repository_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// valid GSTINs (ISO 7064 MOD 37,36 checksum verified):
//   27AAPBC1234F1ZV — Maharashtra
//   29AAPBC1234F1ZR — Karnataka
var (
	gstinMH = "27AAPBC1234F1ZV"
	gstinKA = "29AAPBC1234F1ZR"
)

func uniquePhone(prefix string) string {
	return fmt.Sprintf("+9198%s%06d", prefix, counter())
}

var searchCounter int

func counter() int {
	searchCounter++
	return searchCounter
}

func seedCustomer(t *testing.T, c *models.Customer) *models.Customer {
	t.Helper()
	if c.CustomerType == "" {
		c.CustomerType = "B2C"
	}
	if err := custRepo.Create(context.Background(), testutil.StoreID, c); err != nil {
		t.Fatalf("create customer %q: %v", c.Name, err)
	}
	return c
}

func TestCustomerSearchByName(t *testing.T) {
	reset(t)
	ctx := context.Background()

	seedCustomer(t, &models.Customer{Name: "Widget Wholesale", Phone: uniquePhone("10"), CustomerType: "B2B"})
	seedCustomer(t, &models.Customer{Name: "Gadget General Store", Phone: uniquePhone("20"), CustomerType: "B2C"})
	seedCustomer(t, &models.Customer{Name: "Sunrise Medico", Phone: uniquePhone("30"), CustomerType: "B2C"})

	got, err := custRepo.ListFiltered(ctx, testutil.StoreID, "widget", "", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Widget Wholesale" {
		t.Errorf("search 'widget' = %+v want [Widget Wholesale]", got)
	}

	got, err = custRepo.ListFiltered(ctx, testutil.StoreID, "MEDICO", "", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Sunrise Medico" {
		t.Errorf("search 'MEDICO' (case-insensitive) = %+v", got)
	}
}

func TestCustomerSearchByPhone(t *testing.T) {
	reset(t)
	ctx := context.Background()

	phone := uniquePhone("55")
	seedCustomer(t, &models.Customer{Name: "Phone Seeker", Phone: phone, CustomerType: "B2C"})

	// Search by a distinctive digit run within the phone number.
	got, err := custRepo.ListFiltered(ctx, testutil.StoreID, "55", "", 20)
	if err != nil {
		t.Fatalf("search by phone: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Phone Seeker" {
		t.Errorf("search by phone = %+v want [Phone Seeker]", got)
	}
}

func TestCustomerSearchByGSTIN(t *testing.T) {
	reset(t)
	ctx := context.Background()

	// Same GSTIN is legal on two rows (multi-branch); both must be returned.
	seedCustomer(t, &models.Customer{Name: "Mumbai Trading Co", Phone: uniquePhone("60"), CustomerType: "B2B", GSTIN: &gstinMH})
	seedCustomer(t, &models.Customer{Name: "Mumbai Trading Branch", Phone: uniquePhone("61"), CustomerType: "B2B", GSTIN: &gstinMH})
	seedCustomer(t, &models.Customer{Name: "Bangalore Wholesale", Phone: uniquePhone("62"), CustomerType: "B2B", GSTIN: &gstinKA})

	got, err := custRepo.ListFiltered(ctx, testutil.StoreID, "AAPBC1234F1Z", "", 20)
	if err != nil {
		t.Fatalf("search by gstin: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("search by gstin = %d rows want 3: %+v", len(got), got)
	}

	got, err = custRepo.ListFiltered(ctx, testutil.StoreID, gstinKA, "", 20)
	if err != nil {
		t.Fatalf("search by full gstin: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Bangalore Wholesale" {
		t.Errorf("search full gstin = %+v want [Bangalore Wholesale]", got)
	}
}

func TestCustomerSearchTypeFilter(t *testing.T) {
	reset(t)
	ctx := context.Background()

	seedCustomer(t, &models.Customer{Name: "Alpha Retail", Phone: uniquePhone("70"), CustomerType: "B2C"})
	seedCustomer(t, &models.Customer{Name: "Alpha Wholesale", Phone: uniquePhone("71"), CustomerType: "B2B"})

	got, err := custRepo.ListFiltered(ctx, testutil.StoreID, "Alpha", "B2B", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Alpha Wholesale" {
		t.Errorf("type-filtered search = %+v want [Alpha Wholesale]", got)
	}

	got, err = custRepo.ListFiltered(ctx, testutil.StoreID, "Alpha", "B2C", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Alpha Retail" {
		t.Errorf("type-filtered search = %+v want [Alpha Retail]", got)
	}
}

func TestCustomerSearchTypeBehindBackwardCompatible(t *testing.T) {
	reset(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		seedCustomer(t, &models.Customer{Name: fmt.Sprintf("Backward %02d", i), Phone: uniquePhone("80"), CustomerType: "B2C"})
	}

	val, err := custRepo.ListFiltered(ctx, testutil.StoreID, "", "B2C", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(val) != 5 {
		t.Errorf("type-only search returned %d rows want 5", len(val))
	}
}

func TestCustomerValidateRejectsInvalidGSTINAndStateCode(t *testing.T) {
	bad := "27AABCU9603R1ZM" // pattern-valid but checksum-invalid
	err := repository.ValidateCustomer(&models.Customer{
		Name: "Bad GSTIN Co", Phone: "9876543210", CustomerType: "B2B", GSTIN: &bad,
	})
	if err == nil || err.Error() != "invalid GSTIN" {
		t.Errorf("invalid GSTIN must be rejected with 'invalid GSTIN', got %v", err)
	}

	badState := "9"
	err = repository.ValidateCustomer(&models.Customer{
		Name: "Bad State Co", Phone: "9876543211", StateCode: &badState,
	})
	if err == nil || err.Error() != "invalid state_code" {
		t.Errorf("bad state_code must be rejected, got %v", err)
	}

	state := "29"
	c := &models.Customer{Name: "Valid KA", Phone: "9876543212", CustomerType: "B2B", GSTIN: &gstinKA, StateCode: &state}
	if err := repository.ValidateCustomer(c); err != nil {
		t.Errorf("valid customer rejected: %v", err)
	}
}

func TestCustomerValidateDefaultsCustomerTypeToB2C(t *testing.T) {
	reset(t)
	c := &models.Customer{Name: "No Type", Phone: uniquePhone("90")}
	if err := repository.ValidateCustomer(c); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if c.CustomerType != "B2C" {
		t.Errorf("customer_type defaulted to %q want B2C", c.CustomerType)
	}
	created := seedCustomer(t, c)
	if created.CustomerType != "B2C" {
		t.Errorf("persisted customer_type = %q want B2C", created.CustomerType)
	}
}

// A newly created customer must be immediately usable for a credit sale with
// a proper ledger trail — no refresh or reconciliation step required.
func TestCreditSaleToNewlyCreatedCustomerWritesLedger(t *testing.T) {
	reset(t)
	ctx := context.Background()

	state := "27"
	c := seedCustomer(t, &models.Customer{Name: "Fresh Credit Customer", Phone: uniquePhone("91"), CreditLimit: 10000, CustomerType: "B2C", StateCode: &state})
	fx := seedFixture(t, 200, 10000)
	_ = fx.CustomerID // existing fixture customer; the credit sale below uses the new one

	res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
		StoreID:     sid(testutil.StoreID),
		PaymentType: models.PaymentCredit,
		CustomerID:  &c.ID,
		Items:       []repository.CheckoutItemInput{{BatchID: fx.BatchIDs[0], Quantity: 5}},
	})
	if err != nil {
		t.Fatalf("credit sale to new customer: %v", err)
	}

	cust, _ := custRepo.GetByID(ctx, testutil.StoreID, c.ID)
	if cust.CurrentBalance != res.Invoice.TotalAmount {
		t.Errorf("balance %.2f != invoice %.2f", cust.CurrentBalance, res.Invoice.TotalAmount)
	}
	entries, err := custRepo.Ledger(ctx, testutil.StoreID, c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].EntryType != "CREDIT_SALE" {
		t.Fatalf("ledger entries = %+v want single CREDIT_SALE", entries)
	}
}

// The customer's state_code must drive the retail supply type once cached,
// matching the GST rule scope: a Maharashtra store selling on credit to a
// Karnataka customer is an inter-state supply (IGST).
func TestCustomerStateCodeDrivesSupplyType(t *testing.T) {
	reset(t)
	ctx := context.Background()
	_, batchID := seedGSTMedicine(t)
	sid := seedState27Store(t, ctx)

	intra := seedCustomer(t, &models.Customer{Name: "Mumbai Loyal", Phone: uniquePhone("92"), CreditLimit: 10000, CustomerType: "B2C", StateCode: strPtr("27")})
	inter := seedCustomer(t, &models.Customer{Name: "Bengaluru Buyer", Phone: uniquePhone("93"), CreditLimit: 10000, CustomerType: "B2C", StateCode: strPtr("29")})

	run := func(c *models.Customer, want string) {
		t.Helper()
		res, err := saleRepo.Checkout(ctx, &repository.CheckoutInput{
			PaymentType: models.PaymentCredit,
			CustomerID:  &c.ID,
			StoreID:     &sid,
			Items:       []repository.CheckoutItemInput{{BatchID: batchID, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("checkout to %s: %v", c.Name, err)
		}
		if res.Invoice.SupplyType == nil || *res.Invoice.SupplyType != want {
			t.Errorf("supply type for %s = %v want %s", c.Name, res.Invoice.SupplyType, want)
		}
		if res.Invoice.CustomerStateCode == nil || *res.Invoice.CustomerStateCode != *c.StateCode {
			t.Errorf("customer_state_code for %s = %v want %s", c.Name, res.Invoice.CustomerStateCode, *c.StateCode)
		}
	}

	run(intra, "INTRA_STATE")
	run(inter, "INTER_STATE")
}