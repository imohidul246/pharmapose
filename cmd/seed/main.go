// Command seed resets the database and populates realistic pharmacy demo
// data: GST-registered store, medicines with effective-dated tax config in
// multiple physical batches, purchase inwards, customers and a month of retail
// and B2B sales so the GSTR-1 reports render with real values.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/auth"
	"github.com/mohi/pms-marg-inspired/internal/database"
	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type createdBatch struct {
	ID        string
	SalePrice float64
}

type seedBatch struct {
	Number        string
	ExpiresInDays int
	PurchasePrice float64
	SalePrice     float64
	Stock         int
}

type seedMedicine struct {
	Name, Salt, Manufacturer, UQC, HSN string
	GSTRate, CGSTRate, SGSTRate        float64
	PriceIncludesTax                   bool
	MinReorder                         int
	Batches                            []seedBatch
}

const (
	storeName    = "Main Store"
	fyPrefix     = "SEED"
	sellerState  = "27"
	sellerRegion = "Maharashtra"
)

// seedDate is the fixed anchor date for all seeding. Every sale, purchase,
// batch expiry and invoice is derived from this single constant so the seed
// is fully deterministic and reproducible (run twice -> identical dataset).
var seedDate = time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)

// taxAnchor is the fixed effective_from for every seeded tax rate and
// medicine tax config. It predates every seeded sale (which occur in the
// 30 days before seedDate) so no seeded invoice falls outside the tax window.
var taxAnchor = seedDate.AddDate(0, 0, -400)

// seedGSTINs are checksum-valid GSTINs (ISO 7064 MOD 37,36). They are demo
// fixtures, not real registrations.
const (
	sellerGSTIN    = "27AAAAA1111A1ZW"
	supplier1GSTIN = "27AAECS9876F1ZS" // MedPlus Distributors
	supplier2GSTIN = "27AAHFS2345K1ZY" // HealthFirst Pharma
	supplier3GSTIN = "06AACDD3456G1ZP" // Delhi Distributors Pvt Ltd (inter-state)
	customer1GSTIN = "27AAPBC1234F1ZV" // Anita Desai Clinic
	customer2GSTIN = "27AACCC5678K1Z7" // CityCare Hospital
	customer3GSTIN = "06AADCM4321P1ZC" // Delhi MedCare Supply (inter-state)
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/pms?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

resetDatabase(ctx, pool)

	// 1. GST shell: business -> registration -> store, then the team.
	storeID := seedGSTShell(ctx, pool)
	ownerUserID, employeeUserID, err := seedTeam(ctx, pool, storeID)
	if err != nil {
		log.Fatalf("seed team: %v", err)
	}

	supplierRepo := repository.NewSupplierRepo(pool, storeID)
	medRepo := repository.NewMedicineRepo(pool, storeID)
	purchaseRepo := repository.NewPurchaseRepo(pool)
	customerRepo := repository.NewCustomerRepo(pool, storeID)
	saleRepo := repository.NewSaleRepo(pool)
	taxRepo := repository.NewTaxRepo(pool)

	// 2. Suppliers (intra-state so purchase inwards mirror sales tax).
	suppliers := []models.Supplier{
		{LegalName: "MedPlus Distributors", TradeName: "MedPlus", GSTIN: strPtr(supplier1GSTIN),
			PAN: strPtr("AAECS9876F"), Address: "Goregaon, Mumbai", State: sellerRegion, StateCode: sellerState,
			Phone: "+91-98200-11223", Email: "sales@medplus.example"},
		{LegalName: "HealthFirst Pharma Supplies", TradeName: "HealthFirst", GSTIN: strPtr(supplier2GSTIN),
			PAN: strPtr("AAHFS2345K"), Address: "Andheri East, Mumbai", State: sellerRegion, StateCode: sellerState,
			Phone: "+91-99300-44556", Email: "orders@healthfirst.example"},
		{LegalName: "Delhi Distributors Pvt Ltd", TradeName: "DelhiDist", GSTIN: strPtr(supplier3GSTIN),
			PAN: strPtr("AACDD3456G"), Address: "Karol Bagh, New Delhi", State: "Delhi", StateCode: "06",
			Phone: "+91-11-4765-8901", Email: "orders@delhidist.example"},
	}
	supplierIDs := make([]string, 0, len(suppliers))
	for i := range suppliers {
		if err := supplierRepo.Create(ctx, &suppliers[i]); err != nil {
			log.Fatalf("create supplier %s: %v", suppliers[i].LegalName, err)
		}
		supplierIDs = append(supplierIDs, suppliers[i].ID)
	}
	fmt.Printf("seeded %d suppliers\n", len(supplierIDs))

	// 3. Medicines + HSN/tax config + purchase inwards (batches).
	medicines := []seedMedicine{
		{Name: "Calpol 500mg Tablet", Salt: "Paracetamol 500mg", Manufacturer: "GSK", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 50, Batches: []seedBatch{
				{Number: "CPL-2401", ExpiresInDays: 45, PurchasePrice: 12.50, SalePrice: 18.00, Stock: 300},
				{Number: "CPL-2409", ExpiresInDays: 400, PurchasePrice: 13.00, SalePrice: 19.00, Stock: 350},
			}},
		{Name: "Panadol Extra Tablet", Salt: "Paracetamol 500mg + Caffeine 65mg", Manufacturer: "GSK", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 40, Batches: []seedBatch{
				{Number: "PDX-1180", ExpiresInDays: 80, PurchasePrice: 22.00, SalePrice: 32.00, Stock: 250},
				{Number: "PDX-1185", ExpiresInDays: 260, PurchasePrice: 22.50, SalePrice: 32.50, Stock: 200},
			}},
		{Name: "Dolo 650 Tablet", Salt: "Paracetamol 650mg", Manufacturer: "Micro Labs", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 60, Batches: []seedBatch{
				{Number: "DL650-77", ExpiresInDays: 500, PurchasePrice: 17.25, SalePrice: 25.00, Stock: 400},
				{Number: "DL650-81", ExpiresInDays: 150, PurchasePrice: 16.80, SalePrice: 24.50, Stock: 300},
			}},
		{Name: "Augmentin 625 Duo Tablet", Salt: "Amoxicillin 500mg + Clavulanic Acid 125mg", Manufacturer: "GSK", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 30, Batches: []seedBatch{
				{Number: "AUG-3341", ExpiresInDays: 210, PurchasePrice: 148.50, SalePrice: 195.00, Stock: 120},
			}},
		{Name: "Azithral 500 Tablet", Salt: "Azithromycin 500mg", Manufacturer: "Alembic", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 25, Batches: []seedBatch{
				{Number: "AZT-9912", ExpiresInDays: 320, PurchasePrice: 89.00, SalePrice: 119.50, Stock: 150},
			}},
		{Name: "Shelcal 500 Tablet", Salt: "Calcium Carbonate 500mg + Vitamin D3", Manufacturer: "Torrent", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 40, Batches: []seedBatch{
				{Number: "SHC-556", ExpiresInDays: 640, PurchasePrice: 58.00, SalePrice: 78.00, Stock: 260},
			}},
		{Name: "Telma 40 Tablet", Salt: "Telmisartan 40mg", Manufacturer: "Glenmark", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 20, Batches: []seedBatch{
				{Number: "TL40-2201", ExpiresInDays: 95, PurchasePrice: 72.00, SalePrice: 96.00, Stock: 180},
			}},
		{Name: "Metformin SR 500 Tablet", Salt: "Metformin Hydrochloride 500mg", Manufacturer: "USV", UQC: "TBS",
			HSN: "3004", GSTRate: 12, CGSTRate: 6, SGSTRate: 6, PriceIncludesTax: true, MinReorder: 80, Batches: []seedBatch{
				{Number: "MF500-14", ExpiresInDays: 430, PurchasePrice: 21.00, SalePrice: 31.00, Stock: 320},
			}},
		{Name: "Zincovit Tablet", Salt: "Multivitamin + Minerals", Manufacturer: "Apex", UQC: "TBS",
			HSN: "21069099", GSTRate: 18, CGSTRate: 9, SGSTRate: 9, PriceIncludesTax: true, MinReorder: 35, Batches: []seedBatch{
				{Number: "ZCV-330", ExpiresInDays: 540, PurchasePrice: 42.00, SalePrice: 58.00, Stock: 220},
			}},
		{Name: "Electral ORS Sachet", Salt: "Oral Rehydration Salts", Manufacturer: "FDC", UQC: "BOX",
			HSN: "30049086", GSTRate: 5, CGSTRate: 2.5, SGSTRate: 2.5, PriceIncludesTax: true, MinReorder: 60, Batches: []seedBatch{
				{Number: "ERT-88", ExpiresInDays: 700, PurchasePrice: 9.50, SalePrice: 14.00, Stock: 500},
			}},
	}

	var createdBatches []createdBatch

	for i, sm := range medicines {
		m := &models.Medicine{Name: sm.Name, SaltComposition: sm.Salt,
			Manufacturer: sm.Manufacturer, MinReorderLevel: sm.MinReorder,
			UQC: sm.UQC, Packing: "Strip"}
		if err := medRepo.Create(ctx, m); err != nil {
			log.Fatalf("create medicine %s: %v", sm.Name, err)
		}
		if err := assignTaxConfig(ctx, taxRepo, storeID, m.ID, sm); err != nil {
			log.Fatalf("tax config %s: %v", sm.Name, err)
		}

		items := make([]repository.PurchaseItemInput, 0, len(sm.Batches))
		for _, sb := range sm.Batches {
			items = append(items, repository.PurchaseItemInput{
				MedicineID:    m.ID,
				BatchNumber:   sb.Number,
				ExpiryDate:    models.NewDate(seedDate.AddDate(0, 0, sb.ExpiresInDays)),
				Quantity:      sb.Stock,
				PurchasePrice: sb.PurchasePrice,
				SalePrice:     sb.SalePrice,
			})
		}
		in := &repository.PurchaseInput{
			InvoiceNo:     fmt.Sprintf("%s-PINV-%03d", fyPrefix, i+1),
			InvoiceDate:   strPtr(seedDate.AddDate(0, 0, -35).Format("2006-01-02")),
			SupplierName:  suppliers[i%len(suppliers)].LegalName,
			SupplierID:    &supplierIDs[i%len(suppliers)],
			SupplierGSTIN: suppliers[i%len(suppliers)].GSTIN,
			SupplierState: strPtr(suppliers[i%len(suppliers)].StateCode),
			StoreID:       &storeID,
			PlaceOfSupply: strPtr(suppliers[i%len(suppliers)].StateCode),
			ITCEligible:   boolPtr(true),
			Items:         items,
		}
		if _, _, err := purchaseRepo.CreateInward(ctx, in); err != nil {
			log.Fatalf("inward for %s: %v", m.Name, err)
		}
		for _, sb := range sm.Batches {
			b, err := medRepo.FindBatchByNumber(ctx, m.ID, sb.Number)
			if err != nil {
				log.Fatalf("find batch %s/%s: %v", sm.Name, sb.Number, err)
			}
			createdBatches = append(createdBatches, createdBatch{ID: b.ID, SalePrice: sb.SalePrice})
		}
		fmt.Printf("seeded %-24s HSN %s @ %g%% (%d batches)\n", sm.Name, sm.HSN, sm.GSTRate, len(sm.Batches))
	}

	// 4. Customers: walk-ins, registered buyers (B2B), and an out-of-state buyer.
	customers := []models.Customer{
		{Name: "Ravi Sharma", Phone: "+91-98765-43210", CreditLimit: 5000, CustomerType: "B2C", State: strPtr(sellerRegion), StateCode: strPtr(sellerState)},
		{Name: "Sunil Kumar", Phone: "+91-99223-34455", CreditLimit: 10000, CustomerType: "B2C", State: strPtr(sellerRegion), StateCode: strPtr(sellerState)},
		{Name: "Meena Joshi", Phone: "+91-98111-12122", CreditLimit: 8000, CustomerType: "B2C", State: strPtr(sellerRegion), StateCode: strPtr(sellerState)},
		{Name: "Anita Desai Clinic", Phone: "+91-98111-22334", CreditLimit: 25000, CustomerType: "B2B",
			GSTIN: strPtr(customer1GSTIN), State: strPtr(sellerRegion), StateCode: strPtr(sellerState)},
		{Name: "CityCare Hospital", Phone: "+91-90909-12123", CreditLimit: 100000, CustomerType: "B2B",
			GSTIN: strPtr(customer2GSTIN), State: strPtr(sellerRegion), StateCode: strPtr(sellerState)},
		{Name: "Delhi MedCare Supply Co", Phone: "+91-98113-99887", CreditLimit: 150000, CustomerType: "B2B",
			GSTIN: strPtr(customer3GSTIN), State: strPtr("Delhi"), StateCode: strPtr("06")},
	}

	var retailCustomerIDs []string
	var b2bCustomers []models.Customer
	for i := range customers {
		if err := customerRepo.Create(ctx, &customers[i]); err != nil {
			log.Fatalf("create customer %s: %v", customers[i].Name, err)
		}
		if customers[i].CustomerType == "B2B" {
			b2bCustomers = append(b2bCustomers, customers[i])
			fmt.Printf("seeded B2B customer  %s (%s)\n", customers[i].Name, derefStr(customers[i].GSTIN))
		} else {
			retailCustomerIDs = append(retailCustomerIDs, customers[i].ID)
		}
	}
	fmt.Printf("seeded %d customers (%d retail, %d B2B)\n",
		len(customers), len(retailCustomerIDs), len(b2bCustomers))

	// 5. A month of sales: retail B2CS + occasional wholesale B2B (intra and inter-state).
	rng := rand.New(rand.NewSource(42))
	days := 30
	retailSales := 0
	b2bIntra := 0
	b2bInter := 0

	// B2B wholesale schedule: (daysAgo, registered customer index, inter-state?)
	b2bSchedule := []struct {
		day     int
		custIdx int
		inter   bool
	}{
		{28, 0, false}, // Anita Desai Clinic, intra-state
		{22, 1, false}, // CityCare Hospital, intra-state
		{17, 2, true},  // Delhi MedCare, inter-state
		{11, 1, false}, // CityCare Hospital, intra-state
		{5, 0, false},  // Anita Desai Clinic, intra-state
	}
	b2bIdx := 0

	for day := days; day >= 1; day-- {
		when := seedDate.AddDate(0, 0, -day)

		for sale := 0; sale < 3+rng.Intn(4); sale++ {
			batch := createdBatches[rng.Intn(len(createdBatches))]
			qty := 1 + rng.Intn(5)
			in := &repository.CheckoutInput{
				PaymentType:   models.PaymentCash,
				StoreID:       &storeID,
				PlaceOfSupply: strPtr(sellerState),
				Items:         []repository.CheckoutItemInput{{BatchID: batch.ID, Quantity: qty}},
			}
			if rng.Intn(3) == 0 {
				in.PaymentType = models.PaymentCredit
				cid := retailCustomerIDs[rng.Intn(len(retailCustomerIDs))]
				in.CustomerID = &cid
			}
			if _, err := saleRepo.CheckoutAt(ctx, in, when); err != nil {
				log.Printf("retail sale skipped (day %d): %v", day, err)
				continue
			}
			retailSales++
		}

		if b2bIdx < len(b2bSchedule) && b2bSchedule[b2bIdx].day == day {
			bs := b2bSchedule[b2bIdx]
			cust := b2bCustomers[bs.custIdx]
			batch := createdBatches[rng.Intn(len(createdBatches))]
			pos := sellerState
			if bs.inter {
				pos = "06"
			}
			wholesale := round2(batch.SalePrice * 0.88)
			in := &repository.CheckoutInput{
				PaymentType:   models.PaymentCredit,
				CustomerID:    &cust.ID,
				SaleType:      "B2B",
				StoreID:       &storeID,
				PlaceOfSupply: &pos,
				BuyerName:     strPtr(cust.Name),
				BuyerGSTIN:    cust.GSTIN,
				BuyerAddress:  strPtr("Registered place of business"),
				Items: []repository.CheckoutItemInput{{
					BatchID:   batch.ID,
					Quantity:  20 + rng.Intn(10),
					SellPrice: f64Ptr(wholesale),
				}},
			}
			if _, err := saleRepo.CheckoutAt(ctx, in, when); err != nil {
				log.Printf("b2b sale skipped (day %d): %v", day, err)
			} else if bs.inter {
				b2bInter++
			} else {
				b2bIntra++
			}
			b2bIdx++
		}
	}
	fmt.Printf("seeded %d retail sales, %d intra-state B2B, %d inter-state B2B\n", retailSales, b2bIntra, b2bInter)

	// 6. Pending approval workflows: one purchase request and one stock audit
	// (both submitted by the employee, both awaiting owner sign-off).
	seedPendingRequests(ctx, pool, storeID, ownerUserID, employeeUserID, createdBatches, supplierIDs, suppliers)

	printSummary(ctx, pool)
}

// resetDatabase removes every transactional/demo row so the seed is idempotent.
// Master data seeded by migrations (hsn_codes, tax_rates) is preserved.
func resetDatabase(ctx context.Context, pool *pgxpool.Pool) {
	_, err := pool.Exec(ctx, `
		TRUNCATE customer_ledger, reconciliation_items, reconciliation_journals,
		         sales_credit_note_items, sales_credit_notes,
		         sales_invoice_items, sales_invoices,
		         purchase_order_items, purchase_orders,
		         gstr2b_imports, gstr2b_import_batches,
		         medicine_tax_config,
		         invoice_sequences,
		         purchase_requests, stock_audit_request_items, stock_audit_requests,
		         sessions, store_memberships, users, audit_logs,
		         gst_registrations, stores, businesses,
		         suppliers,
		         batches, customers, medicines CASCADE`)
	if err != nil {
		log.Fatalf("reset database: %v", err)
	}
	fmt.Println("removed all previous seed data")
}

// seedGSTShell creates the business, GST registration and active store.
func seedGSTShell(ctx context.Context, pool *pgxpool.Pool) string {
	var businessID string
	err := pool.QueryRow(ctx,
		`INSERT INTO businesses (legal_name, trade_name) VALUES ($1, $2) RETURNING id::text`,
		"PharmaPOS Demo Pharmacy", "PharmaPOS").Scan(&businessID)
	if err != nil {
		log.Fatalf("create business: %v", err)
	}

	var regID string
	err = pool.QueryRow(ctx, `
		INSERT INTO gst_registrations
			(business_id, gstin, legal_name, trade_name, pan, state_code, state_name, address, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true)
		RETURNING id::text`,
		businessID, sellerGSTIN, "PharmaPOS Demo Pharmacy", "PharmaPOS",
		"AAAAA0000A", sellerState, sellerRegion, "12 Marine Lines, Mumbai, Maharashtra 400020").
		Scan(&regID)
	if err != nil {
		log.Fatalf("create gst registration: %v", err)
	}

	var storeID string
	err = pool.QueryRow(ctx, `
		INSERT INTO stores (gst_registration_id, name, address, is_active)
		VALUES ($1, $2, $3, true) RETURNING id::text`,
		regID, storeName, "12 Marine Lines, Mumbai, Maharashtra 400020").Scan(&storeID)
	if err != nil {
		log.Fatalf("create store: %v", err)
	}
	fmt.Printf("seeded GST setup: store %s (GSTIN %s, state %s)\n", storeName, sellerGSTIN, sellerState)
	return storeID
}

// assignTaxConfig links a medicine to an HSN code with an explicit tax rate
// taken from the seed data itself.
//
// Every medicine carries its own classification -> rate mapping, so the rate
// applied is never guessed from the HSN prefix or from a shared default. This
// makes the ORS (5%, HSN 30049086) and multivitamin (18%, HSN 21069099) lines
// report the rates their products actually carry, distinct from the 12%
// general medicament bucket.
//
// The declared CGST+SGST split is validated to equal the total GST rate, and
// the config is linked with the medicine's price_includes_tax flag.
func assignTaxConfig(ctx context.Context, taxRepo *repository.TaxRepo, storeID, medicineID string, sm seedMedicine) error {
	if !tax.ValidateUQC(sm.UQC) {
		return fmt.Errorf("medicine %s uses invalid UQC %q", sm.Name, sm.UQC)
	}
	if sm.HSN == "" {
		return fmt.Errorf("medicine %s has no HSN", sm.Name)
	}
	if sm.CGSTRate+sm.SGSTRate != sm.GSTRate {
		return fmt.Errorf("medicine %s tax split %g+%g != total %g",
			sm.Name, sm.CGSTRate, sm.SGSTRate, sm.GSTRate)
	}

	// Ensure the exact HSN code row exists (create for precise sub-classifications).
	hsn, err := taxRepo.GetHSNByCode(ctx, storeID, sm.HSN)
	if errors.Is(err, models.ErrNotFound) {
		hsn, err = taxRepo.CreateHSNCode(ctx, storeID, sm.HSN, sm.Name)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Ensure there is an active tax rate with the declared rate. A medicine's
	// config always points at a rate whose values match the seed classification.
	tr, err := taxRepo.GetActiveTaxRate(ctx, storeID, hsn.ID)
	if err != nil {
		return err
	}
	if tr == nil || tr.GSTRate != sm.GSTRate || tr.CGSTRate != sm.CGSTRate || tr.SGSTRate != sm.SGSTRate {
		tr, err = taxRepo.UpsertTaxRate(ctx, storeID, hsn.ID, sm.GSTRate, sm.CGSTRate, sm.SGSTRate, sm.GSTRate, 0)
		if err != nil {
			return err
		}
	}

	cfg, err := taxRepo.UpsertMedicineTaxConfig(ctx, storeID, medicineID, hsn.ID, tr.ID, sm.PriceIncludesTax)
	if err != nil {
		return err
	}

	// Back-date the effective_from for both the tax rate and the medicine's
	// config so every seeded sale (seeded between seedDate-30d and seedDate)
	// falls within the config window. The repository defaults effective_from
	// to CURRENT_DATE, which would otherwise drop tax from any sale before
	// today. Using a fixed deterministic anchor keeps the seed reproducible.
	if err := taxRepo.BackdateTaxConfig(ctx, cfg.ID, tr.ID, taxAnchor); err != nil {
		return err
	}
	return nil
}

// seedTeam creates the owner (dev creds listed in the summary below) and the
// two employee seats the store ships with. The owner password is fixed so the
// demo can be logged into from the README; change it in production.
func seedTeam(ctx context.Context, pool *pgxpool.Pool, storeID string) (string, string, error) {
	authRepo := repository.NewAuthRepo(pool)

	ownerHash, err := auth.HashPassword("owner@123")
	if err != nil {
		return "", "", err
	}
	var ownerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (name, phone, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
		"Store Owner", "9820000000", ownerHash).Scan(&ownerID); err != nil {
		return "", "", err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO store_memberships (store_id, user_id, role, is_active)
		VALUES ($1, $2, 'STORE_OWNER', true)`, storeID, ownerID); err != nil {
		return "", "", err
	}

	employees := []struct{ name, phone string }{
		{"Ramesh Kumar", "9820000001"},
		{"Suresh Patil", "9820000002"},
	}
	empIDs := make([]string, 0, len(employees))
	for _, e := range employees {
		hash, err := auth.HashPassword("emp@1234")
		if err != nil {
			return "", "", err
		}
		u, err := authRepo.CreateEmployee(ctx, storeID, e.name, e.phone, hash)
		if err != nil {
			return "", "", fmt.Errorf("invite %s: %w", e.name, err)
		}
		empIDs = append(empIDs, u.ID)
	}
	fmt.Printf("seeded team: owner 9820000000/owner@123, employees 9820000001+9820000002/emp@1234\n")
	return ownerID, empIDs[0], nil
}

// seedPendingRequests plants one purchase request and one stock audit request,
// both PENDING so the demo owner has an approval to perform. The physical count
// is deliberately a couple units off the live stock so approving the audit
// produces a visible reconciliation adjustment.
func seedPendingRequests(ctx context.Context, pool *pgxpool.Pool, storeID, ownerID, employeeID string,
	createdBatches []createdBatch, supplierIDs []string, suppliers []models.Supplier) {

	purchReqRepo := repository.NewPurchaseRequestRepo(pool)
	auditReqRepo := repository.NewStockAuditRequestRepo(pool)

	// Purchase request: restock of the first two seeded batches.
	if len(createdBatches) >= 2 {
		items := make([]repository.PurchaseItemInput, 0, 2)
		for _, b := range createdBatches[:2] {
			var medicineID, batchNo string
			var e time.Time
			if err := pool.QueryRow(ctx,
				`SELECT medicine_id::text, batch_number, expiry_date FROM batches WHERE id = $1`, b.ID).
				Scan(&medicineID, &batchNo, &e); err != nil {
				log.Printf("purchase request batch lookup: %v", err)
			} else {
				items = append(items, repository.PurchaseItemInput{
					MedicineID:    medicineID,
					BatchNumber:   batchNo,
					ExpiryDate:    models.NewDate(e),
					Quantity:      150,
					PurchasePrice: b.SalePrice * 0.72,
					SalePrice:     b.SalePrice,
				})
			}
		}
		if len(items) == 2 {
			in := &repository.PurchaseInput{
				InvoiceNo:     "REQ-1001",
				SupplierName:  suppliers[0].LegalName,
				SupplierID:    &supplierIDs[0],
				SupplierGSTIN: suppliers[0].GSTIN,
				SupplierState: strPtr(suppliers[0].StateCode),
				StoreID:       &storeID,
				CreatedBy:     &employeeID,
				PlaceOfSupply: strPtr(suppliers[0].StateCode),
				ITCEligible:   boolPtr(true),
				Items:         items,
			}
			if req, err := purchReqRepo.Create(ctx, storeID, employeeID, in); err != nil {
				log.Printf("pending purchase request: %v", err)
			} else {
				fmt.Printf("seeded pending purchase request %s (submitted by employee)\n", req.ID[:8])
			}
		}
	}

	// Stock audit request: count of the first batch is physically +3 units.
	if len(createdBatches) >= 1 {
		var currentStock int
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(current_stock, 0) FROM batches WHERE id = $1`, createdBatches[0].ID).
			Scan(&currentStock); err != nil {
			log.Printf("audit batch lookup: %v", err)
			currentStock = 0
		}
		var medID string
		_ = pool.QueryRow(ctx, `SELECT medicine_id::text FROM batches WHERE id = $1`, createdBatches[0].ID).Scan(&medID)
		req, items, err := auditReqRepo.Create(ctx, storeID, employeeID, "monthly physical count",
			[]repository.AuditItemInput{{
				MedicineID:       medID,
				BatchID:          createdBatches[0].ID,
				PhysicalQuantity: currentStock + 3,
				Reason:           "floor count",
			}})
		if err != nil {
			log.Printf("pending stock audit request: %v", err)
		} else {
			fmt.Printf("seeded pending stock audit request %s (%d items)\n", req.ID[:8], len(items))
		}
	}
}

// printSummary reports what was seeded so report numbers can be sanity-checked.
func printSummary(ctx context.Context, pool *pgxpool.Pool) {
	var meds, batches, customers, invoices, items int
	var taxable, cgst, sgst, igst float64
	var withTax, b2bInvoices int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM medicines`).Scan(&meds)
	if err == nil {
		err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM batches`).Scan(&batches)
	}
	if err == nil {
		err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM customers`).Scan(&customers)
	}
	if err == nil {
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*),
			       COUNT(*) FILTER (WHERE COALESCE(cgst_total, 0) > 0 OR COALESCE(sgst_total, 0) > 0 OR COALESCE(igst_total, 0) > 0),
			       COUNT(*) FILTER (WHERE customer_gstin IS NOT NULL AND customer_gstin != '')
			FROM sales_invoices`).Scan(&invoices, &withTax, &b2bInvoices)
	}
	if err == nil {
		err = pool.QueryRow(ctx, `
			SELECT COUNT(*),
			       COALESCE(SUM(taxable_value), 0),
			       COALESCE(SUM(cgst_amount), 0),
			       COALESCE(SUM(sgst_amount), 0),
			       COALESCE(SUM(igst_amount), 0)
			FROM sales_invoice_items`).Scan(&items, &taxable, &cgst, &sgst, &igst)
	}
	if err != nil {
		log.Printf("summary query: %v", err)
		return
	}
	fmt.Println("---- seed summary ----")
	fmt.Printf("medicines=%d batches=%d customers=%d\n", meds, batches, customers)
	fmt.Printf("sales_invoices=%d (taxed=%d, B2B=%d)\n", invoices, withTax, b2bInvoices)
	fmt.Printf("sale line items=%d taxable=%.2f CGST=%.2f SGST=%.2f IGST=%.2f\n", items, taxable, cgst, sgst, igst)
}

// ---- small helpers ----

func strPtr(s string) *string   { return &s }
func f64Ptr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool      { return &b }
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func round2(v float64) float64 { return float64(int(v*100+0.5)) / 100 }
