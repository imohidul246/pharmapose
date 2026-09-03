package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/repository"
	"github.com/mohi/pms-marg-inspired/internal/testutil"
)

// nowForTest is the as-of timestamp for current-config reads within a test.
func nowForTest(t *testing.T) time.Time {
	t.Helper()
	return time.Now()
}

// hsnCodeOrNil renders a config's HSN code for readable failure messages.
func hsnCodeOrNil(cfg *models.MedicineTaxConfig) string {
	if cfg == nil {
		return "<nil>"
	}
	return cfg.HSNCode
}

// hsnAndRateFor upserts an HSN code (creating it if needed) and ensures it has
// an active tax rate, returning the HSN record and the active rate. Uses
// dedicated HSN codes (>= 9970) so tests never mutate the shared 3004/2106/9983
// reference data seeded by the migrations.
func hsnAndRateFor(t *testing.T, code string, gst float64) (*models.HSNCode, *models.TaxRate) {
	t.Helper()
	ctx := context.Background()
	tr := repository.NewTaxRepo(pool)

	hsn, err := tr.GetHSNByCode(ctx, testutil.StoreID, code)
	if err == models.ErrNotFound || hsn == nil {
		hsn, err = tr.CreateHSNCode(ctx, testutil.StoreID, code, "Regression test HSN")
		if err != nil && err != models.ErrDuplicate {
			t.Fatalf("create hsn %s: %v", code, err)
		}
		hsn, err = tr.GetHSNByCode(ctx, testutil.StoreID, code)
		if err != nil {
			t.Fatalf("re-fetch hsn %s: %v", code, err)
		}
	} else if err != nil {
		t.Fatalf("get hsn %s: %v", code, err)
	}
	// Scrub prior rates for this HSN so the test is fully self-contained and a
	// same-day rate upsert never trips chk_tr_effective across runs.
	if _, err := pool.Exec(ctx,
		`DELETE FROM tax_rates WHERE hsn_code_id = $1 AND store_id = $2`, hsn.ID, testutil.StoreID); err != nil {
		t.Fatalf("clear rates for %s: %v", code, err)
	}
	rate, err := tr.UpsertTaxRate(ctx, testutil.StoreID, hsn.ID, gst, gst/2, gst/2, gst, 0)
	if err != nil {
		t.Fatalf("upsert rate for %s: %v", code, err)
	}
	return hsn, rate
}

// TestMedicineTaxConfigReassignmentReturnsNewHSN is the regression test for the
// HSN reassignment bug (Operation B). A medicine is first associated with HSN
// A then reassigned to HSN B on the same day, so the old (closed) config and the
// new (active) config share the same effective_from and both satisfy the as-of
// read filter. GetMedicineTaxConfig must resolve the ACTIVE (new) HSN, not the
// old one. This failed before the read-path ORDER BY fix.
func TestMedicineTaxConfigReassignmentReturnsNewHSN(t *testing.T) {
	reset(t)
	ctx := context.Background()

	hsnA, rateA := hsnAndRateFor(t, "9971", 12)
	hsnB, rateB := hsnAndRateFor(t, "9972", 18)

	if hsnA.ID == hsnB.ID || hsnA.Code == hsnB.Code {
		t.Fatalf("test HSNs must differ: %+v vs %+v", hsnA, hsnB)
	}

	med := &models.Medicine{Name: "Reassign Med", SaltComposition: "Rx", Manufacturer: "ReassignPharma", MinReorderLevel: 1}
	if err := medRepo.Create(ctx, testutil.StoreID, med); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	tr := repository.NewTaxRepo(pool)
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnA.ID, rateA.ID, false); err != nil {
		t.Fatalf("link config A: %v", err)
	}

	// Reassign (same-day) to HSN B. This closes the A config with
	// effective_to = CURRENT_DATE + 1 and inserts a B config with
	// effective_from = CURRENT_DATE — old and new tie on effective_from.
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnB.ID, rateB.ID, false); err != nil {
		t.Fatalf("reassign config B: %v", err)
	}

	cfg, err := tr.GetMedicineTaxConfigByMedicine(ctx, testutil.StoreID, med.ID)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg == nil {
		t.Fatal("config is nil after reassignment")
	}
	if cfg.HSNCode != hsnB.Code {
		t.Errorf("current config HSN = %q, want %q (reassigned HSN must win over the closed old config)", cfg.HSNCode, hsnB.Code)
	}
	if cfg.EffectiveTo != nil {
		t.Errorf("resolved config must be the ACTIVE row (effective_to NULL), got effective_to=%v", cfg.EffectiveTo)
	}
	if cfg.TaxRate == nil || cfg.TaxRate.GSTRate != 18 {
		t.Errorf("resolved config must carry the new HSN's rate (18%%), got %+v", cfg.TaxRate)
	}
}

// TestMedicineTaxConfigReassignmentResolvesFromDB ensures the database row used
// by /api/medicines/:id/tax-config (GetMedicineTaxConfig) reflects the new HSN
// and no stale old-HSN config is returned for the same as-of date.
func TestMedicineTaxConfigReassignmentResolvesFromDB(t *testing.T) {
	reset(t)
	ctx := context.Background()

	hsnCodeA, rateA := hsnAndRateFor(t, "9973", 5)
	hsnCodeB, rateB := hsnAndRateFor(t, "9974", 28)

	med := &models.Medicine{Name: "Reassign DB Med", SaltComposition: "Rx", Manufacturer: "DBPharma", MinReorderLevel: 1}
	if err := medRepo.Create(ctx, testutil.StoreID, med); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	tr := repository.NewTaxRepo(pool)
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnCodeA.ID, rateA.ID, false); err != nil {
		t.Fatalf("assign A: %v", err)
	}
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnCodeB.ID, rateB.ID, false); err != nil {
		t.Fatalf("assign B: %v", err)
	}

	cfg, err := tr.GetMedicineTaxConfig(ctx, testutil.StoreID, med.ID, nowForTest(t))
	if err != nil {
		t.Fatalf("get config (as-of today): %v", err)
	}
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if cfg.HSNCode != hsnCodeB.Code {
		t.Errorf("as-of config HSN = %q, want %q", cfg.HSNCode, hsnCodeB.Code)
	}
	if cfg.EffectiveTo != nil {
		t.Errorf("resolved config should be active, got effective_to=%v", cfg.EffectiveTo)
	}
}

// TestMedicineTaxConfigReassignmentIsStoreScoped ensures reassigning a
// medicine's HSN writes a config scoped to the store and never leaks into any
// other store.
func TestMedicineTaxConfigReassignmentIsStoreScoped(t *testing.T) {
	reset(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		`INSERT INTO stores (id, name, address, max_employees)
		 VALUES ('00000000-0000-0000-0000-0000000000AA', 'Second Store', '', 2)
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("seed second store: %v", err)
	}
	otherStore := "00000000-0000-0000-0000-0000000000AA"

	tr := repository.NewTaxRepo(pool)
	hsnA, rateA := hsnAndRateFor(t, "9975", 12)
	hsnB, rateB := hsnAndRateFor(t, "9976", 18)

	med := &models.Medicine{Name: "Isolation Med", SaltComposition: "Rx", Manufacturer: "IsoPharma", MinReorderLevel: 1}
	if err := medRepo.Create(ctx, testutil.StoreID, med); err != nil {
		t.Fatalf("create medicine: %v", err)
	}

	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnA.ID, rateA.ID, false); err != nil {
		t.Fatalf("store A assign A: %v", err)
	}
	// Reassign store A to HSN B.
	if _, err := tr.UpsertMedicineTaxConfig(ctx, testutil.StoreID, med.ID, hsnB.ID, rateB.ID, true); err != nil {
		t.Fatalf("store A reassign B: %v", err)
	}

	cfgA, err := tr.GetMedicineTaxConfigByMedicine(ctx, testutil.StoreID, med.ID)
	if err != nil {
		t.Fatalf("get store A config: %v", err)
	}
	if cfgA == nil || cfgA.HSNCode != hsnB.Code {
		t.Errorf("store A config HSN = %q, want %q (reassigned)", hsnCodeOrNil(cfgA), hsnB.Code)
	}
	if cfgA == nil || !cfgA.PriceIncludesTax {
		t.Errorf("store A config should reflect the reassigned price_includes_tax=true")
	}

	// The reassignment must not create any active config in another store.
	var otherCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medicine_tax_config WHERE store_id = $1 AND effective_to IS NULL`, otherStore).
		Scan(&otherCount); err != nil {
		t.Fatal(err)
	}
	if otherCount != 0 {
		t.Errorf("store A reassignment leaked into store B: %d active configs in store B", otherCount)
	}

	// Store A's reassignment left exactly one active config row for the medicine.
	var activeCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM medicine_tax_config
		 WHERE medicine_id = $1 AND store_id = $2 AND effective_to IS NULL`, med.ID, testutil.StoreID).
		Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 ACTIVE config after reassignment, got %d", activeCount)
	}
}
