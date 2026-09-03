package gst

import (
	"strings"
	"testing"
)

func validGSTR1() *GSTR1 {
	return &GSTR1{
		Gstin:  "27AAAAA1111A1ZW",
		Fp:     "082026",
		Gt:     1680,
		CurtGt: 1680,
		B2B: []B2BEntry{{
			Ctin: "27AAPBC1234F1ZV",
			Inv: []B2BInvoice{{
				Inum:   "B2B/26-27/00001",
				Idt:    "12-08-2026",
				Val:    1680,
				Pos:    "27",
				Rchrg:  "N",
				InvTyp: "R",
				Itms: []B2BLineItem{{
					Num:    1,
					ItmDet: ItmDet{Rt: 12, Txval: 1500, Camt: 90, Samt: 90},
				}},
			}},
		}},
		Hsn: HSNSection{Data: []HSNSummary{{
			Num: 1, HSNCode: "3004", Desc: "Medicaments", UQC: "TBS",
			Qty: 10, Txval: 1500, Rt: 12, Camt: 90, Samt: 90,
		}}},
		DocIssue: DocIssue{DocDet: []DocDetail{{
			DocNum: 1,
			DocTyp: "Invoices for outward supply",
			Docs:   []DocRange{{Num: 1, From: "B2B/26-27/00001", To: "B2B/26-27/00001", TotNum: 1, NetIssue: 1}},
		}}},
	}
}

func TestValidateGSTR1_ValidPasses(t *testing.T) {
	if err := ValidateGSTR1(validGSTR1()); err != nil {
		t.Fatalf("expected valid GSTR-1 to pass, got: %v", err)
	}
}

func TestValidateGSTR1_MissingSupplierGSTIN(t *testing.T) {
	g := validGSTR1()
	g.Gstin = ""
	if err := ValidateGSTR1(g); err == nil {
		t.Fatal("expected error for missing supplier GSTIN")
	}
}

func TestValidateGSTR1_InvalidSupplierGSTIN(t *testing.T) {
	g := validGSTR1()
	g.Gstin = "27AAAAA0000A1Z5" // bad checksum
	if err := ValidateGSTR1(g); err == nil {
		t.Fatal("expected error for invalid supplier GSTIN")
	}
}

func TestValidateGSTR1_MissingPeriod(t *testing.T) {
	g := validGSTR1()
	g.Fp = ""
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "return period") {
		t.Fatalf("expected return period error, got: %v", err)
	}
}

func TestValidateGSTR1_MissingRecipientGSTIN(t *testing.T) {
	g := validGSTR1()
	g.B2B[0].Ctin = ""
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "missing recipient GSTIN") {
		t.Fatalf("expected recipient GSTIN error, got: %v", err)
	}
}

func TestValidateGSTR1_InvalidRecipientGSTIN(t *testing.T) {
	g := validGSTR1()
	g.B2B[0].Ctin = "27AABCU9603R1ZM" // pattern-valid but bad checksum
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "invalid recipient GSTIN") {
		t.Fatalf("expected invalid recipient GSTIN error, got: %v", err)
	}
}

func TestValidateGSTR1_InvalidUQC(t *testing.T) {
	g := validGSTR1()
	g.Hsn.Data[0].UQC = "TAB" // not a valid GSTN UQC
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "invalid UQC") {
		t.Fatalf("expected invalid UQC error, got: %v", err)
	}
}

func TestValidateGSTR1_DuplicateInvoice(t *testing.T) {
	g := validGSTR1()
	g.B2B[0].Inv = append(g.B2B[0].Inv, g.B2B[0].Inv[0])
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got: %v", err)
	}
}

func TestValidateGSTR1_MissingDocSeries(t *testing.T) {
	g := validGSTR1()
	g.DocIssue.DocDet = nil
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "document series") {
		t.Fatalf("expected doc series error, got: %v", err)
	}
}

func TestValidateGSTR1_ZeroGtWithSupplies(t *testing.T) {
	g := validGSTR1()
	g.Gt = 0
	if err := ValidateGSTR1(g); err == nil || !strings.Contains(err.Error(), "gt") {
		t.Fatalf("expected gt error, got: %v", err)
	}
}

func TestValidateGSTR1_EmptyReturnPasses(t *testing.T) {
	// A zero-turnover month is a legitimate filing: no supplies, no HSN, no
	// document series, gt absent — but the GSTIN and period must still be set.
	g := validGSTR1()
	g.Gt = 0
	g.B2B = nil
	g.B2CL = nil
	g.B2CS = nil
	g.Hsn.Data = nil
	g.DocIssue.DocDet = nil
	if err := ValidateGSTR1(g); err != nil {
		t.Fatalf("expected empty return to pass, got: %v", err)
	}
}

func TestSplitSeries(t *testing.T) {
	cases := []struct {
		in, prefix, serial string
	}{
		{"INV/26-27/00042", "INV/26-27/", "00042"},
		{"B2B/26-27/00007", "B2B/26-27/", "00007"},
		{"INV-120", "INV-", "120"},
		{"flat-no-number", "flat-no-number", "flat-no-number"},
	}
	for _, c := range cases {
		p, s := splitSeries(c.in)
		if p != c.prefix || s != c.serial {
			t.Errorf("splitSeries(%q) = (%q,%q) want (%q,%q)", c.in, p, s, c.prefix, c.serial)
		}
	}
}
