package gst

import "testing"

// TestComputeNetLiability exercises the statutory ITC set-off order:
// IGST credit first against IGST, then CGST then SGST; CGST against CGST then
// IGST; SGST against SGST then IGST.
func TestComputeNetLiability(t *testing.T) {
	// Case 1: intra-state sale + generous CGST/SGST credit → payable zero.
	liab := computeNetLiability(
		TaxTotals{TaxableValue: 1500, CGST: 90, SGST: 90},
		TaxTotals{},
		TaxTotals{IGST: 0, CGST: 600, SGST: 600},
	)
	if liab.Liability.CGST != 90 || liab.Liability.SGST != 90 {
		t.Fatalf("liability = %+v, want CGST 90 SGST 90", liab.Liability)
	}
	if liab.Payable.CGST != 0 || liab.Payable.SGST != 0 {
		t.Fatalf("payable = %+v, want both zero", liab.Payable)
	}
	if liab.Payable.Total != 0 {
		t.Fatalf("payable total = %v, want 0", liab.Payable.Total)
	}

	// Case 2: IGST credit covers IGST and bleeds into CGST.
	liab = computeNetLiability(
		TaxTotals{TaxableValue: 2000, IGST: 360, CGST: 0, SGST: 0},
		TaxTotals{},
		TaxTotals{IGST: 400, CGST: 0, SGST: 0},
	)
	// IGST liability 360, credit 400 → surplus 40 → CGST (0 liability) unused.
	if liab.Payable.IGST != 0 {
		t.Fatalf("payable IGST = %v, want 0", liab.Payable.IGST)
	}

	// Case 3: CGST credit surplus offsets remaining IGST liability.
	liab = computeNetLiability(
		TaxTotals{TaxableValue: 2000, IGST: 360, CGST: 60, SGST: 60},
		TaxTotals{},
		TaxTotals{IGST: 0, CGST: 100, SGST: 100},
	)
	// IGST 360 has no IGST credit → stays. CGST 60 - 100 = surplus 40 → IGST. SGST 60-100 = surplus 40 → IGST.
	// IGST payable = 360 - 40 - 40 = 280.
	if liab.Payable.IGST != 280 {
		t.Fatalf("payable IGST = %v, want 280", liab.Payable.IGST)
	}

	// Case 4: reverse-charge outward adds to liability.
	liab = computeNetLiability(
		TaxTotals{TaxableValue: 1000, CGST: 60, SGST: 60},
		TaxTotals{TaxableValue: 1000, IGST: 180},
		TaxTotals{IGST: 180, CGST: 60, SGST: 60},
	)
	if liab.Liability.IGST != 180 || liab.Liability.CGST != 60 || liab.Liability.SGST != 60 {
		t.Fatalf("liability = %+v, want IGST 180 CGST 60 SGST 60", liab.Liability)
	}
	if liab.Payable.Total != 0 {
		t.Fatalf("payable total = %v, want fully covered: 0", liab.Payable.Total)
	}
}
