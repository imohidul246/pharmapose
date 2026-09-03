package services_test

import (
	"sync"
	"testing"
	"time"

	"github.com/mohi/pms-marg-inspired/internal/services"
)

func TestFinancialYear_AprilOnwards(t *testing.T) {
	// April 1, 2026 → "2026-27"
	fy := services.FinancialYear(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC))
	if fy != "2026-27" {
		t.Errorf("FY for Apr 1 2026 = %q want 2026-27", fy)
	}
}

func TestFinancialYear_January(t *testing.T) {
	// January 15, 2027 → "2026-27" (still FY 2026-27)
	fy := services.FinancialYear(time.Date(2027, time.January, 15, 0, 0, 0, 0, time.UTC))
	if fy != "2026-27" {
		t.Errorf("FY for Jan 15 2027 = %q want 2026-27", fy)
	}
}

func TestFinancialYear_March(t *testing.T) {
	// March 31, 2026 → "2025-26"
	fy := services.FinancialYear(time.Date(2026, time.March, 31, 0, 0, 0, 0, time.UTC))
	if fy != "2025-26" {
		t.Errorf("FY for Mar 31 2026 = %q want 2025-26", fy)
	}
}

func TestFinancialYear_December(t *testing.T) {
	// December 25, 2025 → "2025-26"
	fy := services.FinancialYear(time.Date(2025, time.December, 25, 0, 0, 0, 0, time.UTC))
	if fy != "2025-26" {
		t.Errorf("FY for Dec 25 2025 = %q want 2025-26", fy)
	}
}

func TestFinancialYear_March31ToApril1(t *testing.T) {
	mar31 := services.FinancialYear(time.Date(2027, time.March, 31, 0, 0, 0, 0, time.UTC))
	apr1 := services.FinancialYear(time.Date(2027, time.April, 1, 0, 0, 0, 0, time.UTC))
	if mar31 != "2026-27" {
		t.Errorf("Mar 31 2027 FY = %q want 2026-27", mar31)
	}
	if apr1 != "2027-28" {
		t.Errorf("Apr 1 2027 FY = %q want 2027-28", apr1)
	}
}

// TestInvoiceSequenceFallbackConcurrency verifies that the in-memory fallback
// counter never produces duplicate invoice numbers even under heavy concurrency.
func TestInvoiceSequenceFallbackConcurrency(t *testing.T) {
	seq := services.NewInvoiceSequence(nil)
	const workers = 20
	const perWorker = 50

	var mu sync.Mutex
	seen := make(map[string]bool)
	var wg sync.WaitGroup
	errCh := make(chan string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				invoiceNo, _, _ := seq.NextInvoiceNumber(nil, nil, "", "INV/")
				mu.Lock()
				if seen[invoiceNo] {
					errCh <- invoiceNo
				}
				seen[invoiceNo] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for dup := range errCh {
		t.Errorf("duplicate invoice number generated: %s", dup)
	}

	total := workers * perWorker
	if len(seen) != total {
		t.Errorf("unique count = %d want %d", len(seen), total)
	}

	// Verify format: INV/YY-YY/NNNNN (15 chars)
	for inv := range seen {
		if len(inv) != 15 || inv[:4] != "INV/" || inv[9] != '/' {
			t.Errorf("unexpected format: %q", inv)
		}
	}
}
