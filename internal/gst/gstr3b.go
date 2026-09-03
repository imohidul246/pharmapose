package gst

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

// TaxTotals is a set of monetary GST totals for one table/section of a return.
// Every value is computed in the database from the immutable tax snapshots
// persisted at transaction time; nothing is re-derived from current masters.
type TaxTotals struct {
	TaxableValue float64 `json:"taxable_value"`
	IGST         float64 `json:"igst"`
	CGST         float64 `json:"cgst"`
	SGST         float64 `json:"sgst"`
	Cess         float64 `json:"cess"`
	Total        float64 `json:"total"`
}

// taxTotalsFromHeads assembles a TaxTotals row, computing Total.
// Rounding is banker's (round-half-even) via the tax engine.
func taxTotalsFromHeads(taxable, igst, cgst, sgst, cess float64) TaxTotals {
	return TaxTotals{
		TaxableValue: roundMoney(taxable),
		IGST:         roundMoney(igst),
		CGST:         roundMoney(cgst),
		SGST:         roundMoney(sgst),
		Cess:         roundMoney(cess),
		Total:        roundMoney(igst + cgst + sgst + cess),
	}
}

// NetLiabilityTotals reports the tax liability for the period, the ITC credit
// available to set off against it, and the resulting payable per tax head.
type NetLiabilityTotals struct {
	Liability TaxTotals `json:"liability"`
	ITCCredit TaxTotals `json:"itc_credit"`
	Payable   TaxTotals `json:"payable"`
}

// GSTR3B is the Section 3B monthly summary for one GSTIN and period. It is a
// read-model aggregated from sales invoices, sales credit notes and purchase
// orders (the GST snapshot columns on each), which are the authoritative
// transaction-layer records.
type GSTR3B struct {
	GSTIN          string `json:"gstin"`
	Period         string `json:"period"`           // 'YYYY-MM'
	FinancialYear  string `json:"financial_year"`   // '2026-27'
	GSTNPeriodCode string `json:"gstn_period_code"` // '082026'
	FilingDate     string `json:"filing_date"`      // 'YYYY-MM-DD'
	StateCode      string `json:"state_code"`

	// Table 3.1 Outward taxable supplies (3.1(a) net of credit notes, 3.1(b)
	// reverse charge outward, 3.1(c) zero rated, 3.1(d)/(e) nil/exempt).
	OutwardTaxableSupply TaxTotals `json:"3_1_a_outward_taxable_supplies"`
	ReverseChargeSupply  TaxTotals `json:"3_1_b_reverse_charge"`
	ZeroRatedSupply      TaxTotals `json:"3_1_c_zero_rated"`
	ExemptSupply         TaxTotals `json:"3_1_d_exempt_nil_rated"`

	// Table 4 Input tax credit: what is eligible (claimed) vs explicitly
	// ineligible. ITC is never assumed from purchase GST — only purchases
	// flagged itc_eligible contribute.
	EligibleITC   TaxTotals `json:"4_a_eligible_itc"`
	IneligibleITC TaxTotals `json:"4_b_ineligible_itc"`

	// Section 6.1 net liability after set-off.
	NetLiability NetLiabilityTotals `json:"6_1_net_liability"`

	// ITCAtRisk is the recorded ITC on eligible purchases whose invoices are
	// absent from the imported GSTR-2B for the period. Zero when no GSTR-2B
	// has been imported, i.e. no comparison is possible yet.
	ITCAtRisk     float64 `json:"itc_at_risk"`
	UnmatchedDocs int     `json:"unmatched_docs"`
}

// GSTR3BRequest selects the data to include in a GSTR-3B return.
type GSTR3BRequest struct {
	StoreID   string
	Period    string // 'YYYY-MM'
	GSTIN     string
	StateCode string
}

type GSTR3BService struct {
	db *pgxpool.Pool
}

func NewGSTR3BService(db *pgxpool.Pool) *GSTR3BService {
	return &GSTR3BService{db: db}
}

// Build computes the GSTR-3B for the request's month. All amounts come from
// the persisted line/invoice GST snapshots aggregated by SQL ROUND, so the
// result is auditable and never re-derives tax from current master data.
func (s *GSTR3BService) Build(ctx context.Context, req GSTR3BRequest) (*GSTR3B, error) {
	start, end, err := periodRange(req.Period)
	if err != nil {
		return nil, modelsNewValidationError("period must be 'YYYY-MM'")
	}

	st := &GSTR3B{
		GSTIN:          req.GSTIN,
		Period:         req.Period,
		FinancialYear:  fiscalYearFor(start),
		GSTNPeriodCode: gstnPeriodCode(start),
		FilingDate:     end.AddDate(0, -1, 0).Format("2006-01-02"),
		StateCode:      req.StateCode,
	}

	sales, err := s.salesTotals(ctx, start, end, req.StoreID)
	if err != nil {
		return nil, err
	}
	creditNotes, err := s.creditNoteTotals(ctx, start, end, req.StoreID)
	if err != nil {
		return nil, err
	}
	st.OutwardTaxableSupply = taxTotalsFromHeads(
		sales.TaxableValue-creditNotes.TaxableValue,
		sales.IGST-creditNotes.IGST,
		sales.CGST-creditNotes.CGST,
		sales.SGST-creditNotes.SGST,
		sales.Cess-creditNotes.Cess,
	)

	st.ReverseChargeSupply, err = s.reverseChargeTotals(ctx, start, end, req.StoreID)
	if err != nil {
		return nil, err
	}

	st.EligibleITC, err = s.itcTotals(ctx, start, end, req.StoreID, true)
	if err != nil {
		return nil, err
	}
	st.IneligibleITC, err = s.itcTotals(ctx, start, end, req.StoreID, false)
	if err != nil {
		return nil, err
	}

	st.NetLiability = computeNetLiability(st.OutwardTaxableSupply, st.ReverseChargeSupply, st.EligibleITC)

	st.ITCAtRisk, st.UnmatchedDocs, err = s.itcAtRisk(ctx, start, end, req.StoreID, req.Period)
	if err != nil {
		return nil, err
	}

	return st, nil
}

// salesTotals aggregates outward supplies (Table 3.1(a)) from sales invoices.
func (s *GSTR3BService) salesTotals(ctx context.Context, start, end time.Time, storeID string) (TaxTotals, error) {
	var taxable, igst, cgst, sgst, cess float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(COALESCE(si.taxable_amount, si.total_amount)), 0)::float8,
			COALESCE(SUM(COALESCE(si.igst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(si.cgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(si.sgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(si.cess_total, 0)), 0)::float8
		FROM sales_invoices si
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)`,
		start, end, storeID).
		Scan(&taxable, &igst, &cgst, &sgst, &cess)
	return taxTotalsFromHeads(taxable, igst, cgst, sgst, cess), err
}

// creditNoteTotals aggregates credit notes issued in the period; they reduce
// the outward supplies of the same period in Table 3.1(a).
func (s *GSTR3BService) creditNoteTotals(ctx context.Context, start, end time.Time, storeID string) (TaxTotals, error) {
	var taxable, igst, cgst, sgst, cess float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(scn.taxable_amount), 0)::float8,
			COALESCE(SUM(scn.igst_total), 0)::float8,
			COALESCE(SUM(scn.cgst_total), 0)::float8,
			COALESCE(SUM(scn.sgst_total), 0)::float8,
			COALESCE(SUM(scn.cess_total), 0)::float8
		FROM sales_credit_notes scn
		WHERE scn.note_date >= $1 AND scn.note_date < $2
		  AND ($3 = '' OR scn.store_id::text = $3)`,
		start, end, storeID).
		Scan(&taxable, &igst, &cgst, &sgst, &cess)
	return taxTotalsFromHeads(taxable, igst, cgst, sgst, cess), err
}

// reverseChargeTotals aggregates reverse-charge inward supplies, which are
// reported as the pharmacy's own outward liability in Table 3.1(b).
func (s *GSTR3BService) reverseChargeTotals(ctx context.Context, start, end time.Time, storeID string) (TaxTotals, error) {
	var taxable, igst, cgst, sgst, cess float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(COALESCE(po.taxable_amount, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.igst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.cgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.sgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.cess_total, 0)), 0)::float8
		FROM purchase_orders po
		WHERE po.invoice_date >= $1 AND po.invoice_date < $2
		  AND po.reverse_charge = true
		  AND ($3 = '' OR po.store_id::text = $3)`,
		start, end, storeID).
		Scan(&taxable, &igst, &cgst, &sgst, &cess)
	return taxTotalsFromHeads(taxable, igst, cgst, sgst, cess), err
}

// itcTotals aggregates Table 4 input tax credit for purchases flagged with the
// given itc_eligible value. Claimed ITC is the recorded itc_amount, split by
// tax head from the purchase totals.
func (s *GSTR3BService) itcTotals(ctx context.Context, start, end time.Time, storeID string, eligible bool) (TaxTotals, error) {
	var taxable, igst, cgst, sgst, cess float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(COALESCE(po.itc_amount, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.igst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.cgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.sgst_total, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.cess_total, 0)), 0)::float8
		FROM purchase_orders po
		WHERE po.invoice_date >= $1 AND po.invoice_date < $2
		  AND po.itc_eligible = $3
		  AND ($4 = '' OR po.store_id::text = $4)`,
		start, end, eligible, storeID).
		Scan(&taxable, &igst, &cgst, &sgst, &cess)
	return taxTotalsFromHeads(taxable, igst, cgst, sgst, cess), err
}

// itcAtRisk finds eligible purchases in the period that have no counterpart in
// the imported GSTR-2B for that period. When no GSTR-2B has been imported the
// comparison is skipped (no assumption can be made), so the result is empty.
func (s *GSTR3BService) itcAtRisk(ctx context.Context, start, end time.Time, storeID, period string) (float64, int, error) {
	// Step 1: discover whether a 2B import exists for the period.
	var importsExist bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM gstr2b_import_batches WHERE period = $1)`, period).
		Scan(&importsExist); err != nil {
		return 0, 0, err
	}
	if !importsExist {
		return 0, 0, nil
	}

	var count int
	var taxable, itc float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COALESCE(SUM(COALESCE(po.taxable_amount, 0)), 0)::float8,
			COALESCE(SUM(COALESCE(po.itc_amount, po.igst_total + po.cgst_total + po.sgst_total + po.cess_total)), 0)::float8
		FROM purchase_orders po
		WHERE po.invoice_date >= $1 AND po.invoice_date < $2
		  AND po.itc_eligible = true
		  AND ($3 = '' OR po.store_id::text = $3)
		  AND NOT EXISTS (
			SELECT 1 FROM gstr2b_imports gi
			WHERE gi.period = $4
			  AND gi.doc_type = 'INV'
			  AND gi.supplier_gstin = COALESCE(po.supplier_gstin, '')
			  AND gi.invoice_no = po.invoice_no
		  )`,
		start, end, storeID, period).
		Scan(&count, &taxable, &itc)
	if err != nil {
		return 0, 0, err
	}
	return itc, count, nil
}

// computeNetLiability applies the statutory ITC set-off order: IGST credit
// first against IGST, then CGST then SGST; then CGST credit against CGST and
// IGST; SGST credit against SGST and IGST. Cess credit offsets cess liability.
func computeNetLiability(outward, rc, itc TaxTotals) NetLiabilityTotals {
	liability := TaxTotals{
		IGST: outward.IGST + rc.IGST,
		CGST: outward.CGST + rc.CGST,
		SGST: outward.SGST + rc.SGST,
		Cess: outward.Cess + rc.Cess,
	}
	liability.TaxableValue = roundMoney(outward.TaxableValue + rc.TaxableValue)
	liability.Total = roundMoney(liability.IGST + liability.CGST + liability.SGST + liability.Cess)

	igstLiab := liability.IGST
	cgstLiab := liability.CGST
	sgstLiab := liability.SGST

	igstCr := itc.IGST
	cgstCr := itc.CGST
	sgstCr := itc.SGST

	// IGST credit: IGST liability first, surplus then CGST then SGST.
	surplusIGST := igstCr - igstLiab
	if surplusIGST < 0 {
		igstLiab = -surplusIGST
		surplusIGST = 0
	} else {
		igstLiab = 0
	}
	if surplusIGST > 0 {
		used := minf(surplusIGST, cgstLiab)
		cgstLiab -= used
		surplusIGST -= used
	}
	if surplusIGST > 0 {
		sgstLiab = maxf(0, sgstLiab-surplusIGST)
	}

	// CGST credit: CGST liability first, surplus then IGST.
	surplusCGST := cgstCr - cgstLiab
	if surplusCGST < 0 {
		cgstLiab = -surplusCGST
		surplusCGST = 0
	} else {
		cgstLiab = 0
	}
	if surplusCGST > 0 {
		igstLiab = maxf(0, igstLiab-surplusCGST)
	}

	// SGST credit: SGST liability first, surplus then IGST.
	surplusSGST := sgstCr - sgstLiab
	if surplusSGST < 0 {
		sgstLiab = -surplusSGST
		surplusSGST = 0
	} else {
		sgstLiab = 0
	}
	if surplusSGST > 0 {
		igstLiab = maxf(0, igstLiab-surplusSGST)
	}

	payable := TaxTotals{
		IGST: roundMoney(igstLiab),
		CGST: roundMoney(cgstLiab),
		SGST: roundMoney(sgstLiab),
		Cess: roundMoney(maxf(0, liability.Cess-itc.Cess)),
	}
	payable.Total = roundMoney(payable.IGST + payable.CGST + payable.SGST + payable.Cess)

	return NetLiabilityTotals{
		Liability: liability,
		ITCCredit: itc,
		Payable:   payable,
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// roundMoney rounds to 2 decimals with banker's rounding via the tax engine.
func roundMoney(v float64) float64 {
	return tax.RoundMoney(decimal.NewFromFloat(v)).InexactFloat64()
}

// modelsNewValidationError creates a validation-style error, matching the
// models.NewValidationError contract without importing models.
func modelsNewValidationError(msg string) error {
	return errors.New(msg)
}
