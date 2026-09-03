package gst

import (
	"errors"
	"fmt"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

// validationError carries a single GSTR-1 validation failure. The message is
// intentionally actionable so a user can correct the underlying data before
// filing.
func validationError(format string, args ...interface{}) error {
	return errors.New(fmt.Sprintf(format, args...))
}

// ValidateGSTR1 checks a built GSTR-1 for structural and compliance problems
// before it is exported/filed:
//
//   - the supplier GSTIN is present and checksum-valid, and the return period
//     is populated
//   - every B2B entry carries a checksum-valid recipient GSTIN
//   - monetary values are non-negative per item detail
//   - an invoice does not appear in more than one of B2B/B2CL
//   - HSN summary and document series are present whenever supplies are
//     reported
//
// It returns one aggregated error message listing all failures so the caller
// can present them to the user in a single pass.
func ValidateGSTR1(g *GSTR1) error {
	var problems []string

	if g.Gstin == "" {
		problems = append(problems, "supplier GSTIN is missing")
	} else if !tax.ValidateGSTIN(g.Gstin) {
		problems = append(problems, "supplier GSTIN is invalid: "+g.Gstin)
	}

	if g.Fp == "" {
		problems = append(problems, "return period (fp) is missing")
	}

	hasSupplies := len(g.B2B)+len(g.B2CL)+len(g.B2CS) > 0

	// A zero-turnover month is a legitimate filing; only require gt>0 when
	// supplies are actually reported.
	if hasSupplies && g.Gt <= 0 {
		problems = append(problems, "total turnover (gt) is not populated")
	}

	// B2B: recipient GSTIN required and checksum-valid per entry group.
	invoices := make(map[string]string) // inum -> section, for duplicate check
	for i, entry := range g.B2B {
		if entry.Ctin == "" {
			problems = append(problems, fmt.Sprintf("b2b[%d] is missing recipient GSTIN", i))
		} else if !tax.ValidateGSTIN(entry.Ctin) {
			problems = append(problems, fmt.Sprintf("b2b[%d] has invalid recipient GSTIN %s", i, entry.Ctin))
		}
		for _, inv := range entry.Inv {
			if inv.Val <= 0 {
				problems = append(problems, fmt.Sprintf("b2b invoice %s has zero/missing value", inv.Inum))
			}
			if _, dup := invoices[inv.Inum]; dup {
				problems = append(problems, fmt.Sprintf("duplicate B2B invoice %s", inv.Inum))
			} else {
				invoices[inv.Inum] = "B2B"
			}
			checkItmDets(inv.Itms, "b2b "+inv.Inum, &problems)
		}
	}

	for i, entry := range g.B2CL {
		if entry.Pos == "" {
			problems = append(problems, fmt.Sprintf("b2cl[%d] is missing place of supply", i))
		}
		for _, inv := range entry.Inv {
			if inv.Val <= 0 {
				problems = append(problems, fmt.Sprintf("b2cl invoice %s has zero/missing value", inv.Inum))
			}
			if _, dup := invoices[inv.Inum]; dup {
				problems = append(problems, fmt.Sprintf("duplicate invoice %s in B2B and B2CL", inv.Inum))
			} else {
				invoices[inv.Inum] = "B2CL"
			}
			checkItmDets(inv.Itms, "b2cl "+inv.Inum, &problems)
		}
	}

	for i, it := range g.B2CS {
		if it.Pos == "" {
			problems = append(problems, fmt.Sprintf("b2cs[%d] missing place of supply", i))
		}
		if it.Txval < 0 {
			problems = append(problems, fmt.Sprintf("b2cs[%d] invalid taxable value", i))
		}
		if it.SplyTy != "INTRA" && it.SplyTy != "INTER" {
			problems = append(problems, fmt.Sprintf("b2cs[%d] supply type must be INTRA or INTER, got %q", i, it.SplyTy))
		}
	}

	// HSN section must not be empty when supplies were reported.
	if hasSupplies && len(g.Hsn.Data) == 0 {
		problems = append(problems, "HSN summary is empty though supplies were reported")
	}
	for _, hs := range g.Hsn.Data {
		if hs.HSNCode == "" {
			problems = append(problems, "HSN row missing HSN code")
		}
		if !tax.ValidateUQC(hs.UQC) {
			problems = append(problems, fmt.Sprintf("HSN %s uses invalid UQC %q", hs.HSNCode, hs.UQC))
		}
	}

	// Document series must exist whenever supplies are reported.
	if hasSupplies && len(g.DocIssue.DocDet) == 0 {
		problems = append(problems, "document series (Table 13) missing though supplies were reported")
	}

	if len(problems) > 0 {
		msg := "GSTR-1 validation failed:"
		for _, p := range problems {
			msg += "\n - " + p
		}
		return errors.New(msg)
	}
	return nil
}

func checkItmDets(items []B2BLineItem, label string, problems *[]string) {
	for _, it := range items {
		if it.ItmDet.Txval < 0 || it.ItmDet.Rt < 0 {
			*problems = append(*problems, fmt.Sprintf("%s has an invalid item detail (negative rt/txval)", label))
		}
	}
}
