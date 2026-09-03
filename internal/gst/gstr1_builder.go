package gst

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// b2clThreshold is the invoice-value threshold above which inter-state B2C
// supplies must be reported invoice-wise in Table 6B (B2CL). It was reduced
// from Rs 2,50,000 to Rs 1,00,000 effective 1 Aug 2024 (Notification
// 12/2024-CT, Rule 59(4)). Returns are filed here for the current regime.
const b2clThreshold = 100000

// gstr1Version identifies the GSTN schema this file is generated against.
const gstr1Version = "GST3.2.2"

type GSTR1Builder struct {
	db *pgxpool.Pool
}

func NewGSTR1Builder(db *pgxpool.Pool) *GSTR1Builder {
	return &GSTR1Builder{db: db}
}

// GSTR1Request describes the return being built. Either Period ("YYYY-MM")
// or an explicit StartDate/EndDate may be supplied; the period is used when
// present and StartDate/EndDate default to its month boundaries.
type GSTR1Request struct {
	StoreID   string
	GSTIN     string
	Period    string
	StartDate time.Time
	EndDate   time.Time
}

func (r GSTR1Request) normalize() (GSTR1Request, error) {
	if r.Period != "" {
		start, end, err := periodRange(r.Period)
		if err != nil {
			return r, fmt.Errorf("invalid period: %w", err)
		}
		r.StartDate, r.EndDate = start, end
	}
	return r, nil
}

// BuildGSTR1 aggregates sales, credit notes, and HSN data for the given
// period and store, returning a GST Portal Offline Tool compliant JSON.
func (b *GSTR1Builder) BuildGSTR1(ctx context.Context, req GSTR1Request) (*GSTR1, error) {
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}

	gt, err := b.totalTurnover(ctx, req)
	if err != nil {
		return nil, err
	}
	curGt, err := b.cumulativeTurnover(ctx, req)
	if err != nil {
		return nil, err
	}

	gstr := &GSTR1{
		Gstin:   req.GSTIN,
		Fp:      gstnPeriodCode(req.StartDate),
		Gt:      gt,
		CurtGt:  curGt,
		Version: gstr1Version,
	}

	var b2b []B2BEntry
	var b2cl []B2CLEntry
	var b2cs []B2CSItem
	var hsn []HSNSummary
	var cdnr []CDNREntry
	var cdnur []CDNURNote
	var docIssue DocIssue

	errCh := make(chan error, 7)

	go func() {
		var err error
		b2b, err = b.buildB2B(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		b2cl, err = b.buildB2CL(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		b2cs, err = b.buildB2CS(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		hsn, err = b.buildHSN(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		cdnr, err = b.buildCDNR(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		cdnur, err = b.buildCDNUR(ctx, req)
		errCh <- err
	}()
	go func() {
		var err error
		docIssue, err = b.buildDocIssue(ctx, req)
		errCh <- err
	}()

	for i := 0; i < 7; i++ {
		if err := <-errCh; err != nil {
			return nil, err
		}
	}

	gstr.B2B = b2b
	gstr.B2CL = b2cl
	gstr.B2CS = b2cs
	gstr.Hsn.Data = hsn
	gstr.Cdnr = cdnr
	gstr.Cdnur = cdnur
	gstr.DocIssue = docIssue

	return gstr, nil
}

// totalTurnover returns the sum of grand totals for the period (Table 3 "gt").
func (b *GSTR1Builder) totalTurnover(ctx context.Context, req GSTR1Request) (float64, error) {
	var gt float64
	err := b.db.QueryRow(ctx, `
		SELECT COALESCE(ROUND(SUM(COALESCE(si.grand_total, si.total_amount)), 2), 0)
		FROM sales_invoices si
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)`,
		req.StartDate, req.EndDate, req.StoreID).Scan(&gt)
	if err != nil {
		return 0, fmt.Errorf("gt query: %w", err)
	}
	return gt, nil
}

// cumulativeTurnover returns the financial-year-to-date turnover (Table 3
// "cur_gt") up to and including the report period.
func (b *GSTR1Builder) cumulativeTurnover(ctx context.Context, req GSTR1Request) (float64, error) {
	var gt float64
	err := b.db.QueryRow(ctx, `
		SELECT COALESCE(ROUND(SUM(COALESCE(si.grand_total, si.total_amount)), 2), 0)
		FROM sales_invoices si
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)`,
		fyStart(req.StartDate), req.EndDate, req.StoreID).Scan(&gt)
	if err != nil {
		return 0, fmt.Errorf("cur_gt query: %w", err)
	}
	return gt, nil
}

// fyStart returns 1 April of the GST financial year containing t.
func fyStart(t time.Time) time.Time {
	if t.Month() >= time.April {
		return time.Date(t.Year(), time.April, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year()-1, time.April, 1, 0, 0, 0, 0, time.UTC)
}

// PreviewSummary returns aggregate stats for the preview card.
type PreviewSummary struct {
	TaxableValue float64 `json:"taxable_value"`
	CGSTTotal    float64 `json:"cgst_total"`
	SGSTTotal    float64 `json:"sgst_total"`
	IGSTTotal    float64 `json:"igst_total"`
	B2BCount     int     `json:"b2b_count"`
	B2CCount     int     `json:"b2c_count"`
}

func (b *GSTR1Builder) PreviewSummary(ctx context.Context, req GSTR1Request) (*PreviewSummary, error) {
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}
	s := &PreviewSummary{}

	err = b.db.QueryRow(ctx, `
		SELECT
			COALESCE(ROUND(SUM(si.taxable_amount), 2), 0),
			COALESCE(ROUND(SUM(si.cgst_total), 2), 0),
			COALESCE(ROUND(SUM(si.sgst_total), 2), 0),
			COALESCE(ROUND(SUM(si.igst_total), 2), 0),
			COUNT(DISTINCT si.id) FILTER (WHERE si.customer_gstin IS NOT NULL AND si.customer_gstin != ''),
			COUNT(DISTINCT si.id) FILTER (WHERE si.customer_gstin IS NULL OR si.customer_gstin = '')
		FROM sales_invoices si
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)`,
		req.StartDate, req.EndDate, req.StoreID,
	).Scan(&s.TaxableValue, &s.CGSTTotal, &s.SGSTTotal, &s.IGSTTotal, &s.B2BCount, &s.B2CCount)
	if err != nil {
		return nil, fmt.Errorf("preview summary: %w", err)
	}

	return s, nil
}

// ---- B2B / B2CL ----

// buildB2B constructs the B2B section (Table 4): registered sales grouped by
// recipient GSTIN (ctin). The recipient GSTIN is validated by the portal.
func (b *GSTR1Builder) buildB2B(ctx context.Context, req GSTR1Request) ([]B2BEntry, error) {
	rows, err := b.db.Query(ctx, `
		SELECT si.id::text, si.invoice_no, si.invoice_date::text,
		       COALESCE(si.grand_total, si.total_amount)::float8,
		       COALESCE(si.customer_state_code, ''),
		       COALESCE(si.customer_gstin, ''),
		       COALESCE(sii.taxable_value, sii.subtotal)::float8,
		       COALESCE(sii.gst_rate, 0)::float8,
		       COALESCE(sii.cgst_amount, 0)::float8,
		       COALESCE(sii.sgst_amount, 0)::float8,
		       COALESCE(sii.igst_amount, 0)::float8,
		       COALESCE(sii.cess_amount, 0)::float8
		FROM sales_invoices si
		JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND si.customer_gstin IS NOT NULL AND si.customer_gstin != ''
		  AND ($3 = '' OR si.store_id::text = $3)
		ORDER BY si.invoice_date, si.invoice_no, sii.id`,
		req.StartDate, req.EndDate, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("b2b query: %w", err)
	}
	defer rows.Close()

	order := make([]B2BInvoice, 0)
	byID := make(map[string]int)
	gstins := make([]string, 0)
	for rows.Next() {
		var (
			id, invNo, invDate     string
			invVal                 float64
			pos, gstin             string
			taxableVal, rate       float64
			cgst, sgst, igst, cess float64
		)
		if err := rows.Scan(&id, &invNo, &invDate, &invVal,
			&pos, &gstin, &taxableVal, &rate, &cgst, &sgst, &igst, &cess); err != nil {
			return nil, fmt.Errorf("b2b scan: %w", err)
		}
		if pos == "" && len(gstin) >= 2 {
			pos = gstin[:2]
		}
		idx, ok := byID[id]
		if !ok {
			idx = len(order)
			byID[id] = idx
			gstins = append(gstins, gstin)
			order = append(order, B2BInvoice{
				Inum:   invNo,
				Idt:    formatGSTDate(invDate),
				Val:    invVal,
				Pos:    pos,
				Rchrg:  "N",
				InvTyp: "R",
				Itms:   make([]B2BLineItem, 0, 4),
			})
		}
		inv := &order[idx]
		inv.Itms = append(inv.Itms, B2BLineItem{
			Num:    len(inv.Itms) + 1,
			ItmDet: itmDet(taxableVal, rate, cgst, sgst, igst, cess),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ctinOrder := make([]string, 0)
	byCtin := make(map[string][]B2BInvoice)
	for i, inv := range order {
		gstin := gstins[i]
		if _, ok := byCtin[gstin]; !ok {
			ctinOrder = append(ctinOrder, gstin)
		}
		byCtin[gstin] = append(byCtin[gstin], inv)
	}

	out := make([]B2BEntry, 0, len(ctinOrder))
	for _, ctin := range ctinOrder {
		out = append(out, B2BEntry{Ctin: ctin, Inv: byCtin[ctin]})
	}
	return out, nil
}

// buildB2CL constructs the B2CL section (Table 6B): unregistered inter-state
// sales above the current threshold, grouped by place of supply.
func (b *GSTR1Builder) buildB2CL(ctx context.Context, req GSTR1Request) ([]B2CLEntry, error) {
	rows, err := b.db.Query(ctx, `
		SELECT si.id::text, si.invoice_no, si.invoice_date::text,
		       COALESCE(si.grand_total, si.total_amount)::float8,
		       COALESCE(si.customer_state_code, ''),
		       COALESCE(sii.taxable_value, sii.subtotal)::float8,
		       COALESCE(sii.gst_rate, 0)::float8,
		       COALESCE(sii.cgst_amount, 0)::float8,
		       COALESCE(sii.sgst_amount, 0)::float8,
		       COALESCE(sii.igst_amount, 0)::float8,
		       COALESCE(sii.cess_amount, 0)::float8
		FROM sales_invoices si
		JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND (si.customer_gstin IS NULL OR si.customer_gstin = '')
		  AND si.supply_type = 'INTER_STATE'
		  AND COALESCE(si.grand_total, si.total_amount) > $3
		  AND ($4 = '' OR si.store_id::text = $4)
		ORDER BY si.invoice_date, si.invoice_no, sii.id`,
		req.StartDate, req.EndDate, b2clThreshold, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("b2cl query: %w", err)
	}
	defer rows.Close()

	order := make([]B2BInvoice, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var (
			id, invNo, invDate     string
			invVal                 float64
			pos                    string
			taxableVal, rate       float64
			cgst, sgst, igst, cess float64
		)
		if err := rows.Scan(&id, &invNo, &invDate, &invVal,
			&pos, &taxableVal, &rate, &cgst, &sgst, &igst, &cess); err != nil {
			return nil, fmt.Errorf("b2cl scan: %w", err)
		}
		idx, ok := byID[id]
		if !ok {
			idx = len(order)
			byID[id] = idx
			order = append(order, B2BInvoice{
				Inum:   invNo,
				Idt:    formatGSTDate(invDate),
				Val:    invVal,
				Pos:    pos,
				Rchrg:  "N",
				InvTyp: "R",
				Itms:   make([]B2BLineItem, 0, 4),
			})
		}
		inv := &order[idx]
		inv.Itms = append(inv.Itms, B2BLineItem{
			Num:    len(inv.Itms) + 1,
			ItmDet: itmDet(taxableVal, rate, cgst, sgst, igst, cess),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	posOrder := make([]string, 0)
	byPos := make(map[string][]B2BInvoice)
	for _, inv := range order {
		if _, ok := byPos[inv.Pos]; !ok {
			posOrder = append(posOrder, inv.Pos)
		}
		byPos[inv.Pos] = append(byPos[inv.Pos], inv)
	}

	out := make([]B2CLEntry, 0, len(posOrder))
	for _, pos := range posOrder {
		out = append(out, B2CLEntry{Pos: pos, Inv: byPos[pos]})
	}
	return out, nil
}

// ---- B2CS ----

// buildB2CS constructs the B2CS section (Table 7): unregistered intra-state
// sales and inter-state sales at or below the B2CL threshold, consolidated
// per (place of supply, rate, supply type).
func (b *GSTR1Builder) buildB2CS(ctx context.Context, req GSTR1Request) ([]B2CSItem, error) {
	rows, err := b.db.Query(ctx, `
		SELECT COALESCE(NULLIF(si.customer_state_code, ''), gr.state_code, '00') AS pos,
		       COALESCE(sii.gst_rate, 0)::float8 AS rate,
		       CASE WHEN si.supply_type = 'INTER_STATE' THEN 'INTER' ELSE 'INTRA' END AS sply_ty,
		       ROUND(SUM(COALESCE(sii.taxable_value, sii.subtotal)), 2)::float8 AS taxable_val,
		       ROUND(SUM(COALESCE(sii.cgst_amount, 0)), 2)::float8 AS cgst,
		       ROUND(SUM(COALESCE(sii.sgst_amount, 0)), 2)::float8 AS sgst,
		       ROUND(SUM(COALESCE(sii.igst_amount, 0)), 2)::float8 AS igst,
		       ROUND(SUM(COALESCE(sii.cess_amount, 0)), 2)::float8 AS cess
		FROM sales_invoices si
		JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		LEFT JOIN stores st ON st.id = si.store_id
		LEFT JOIN gst_registrations gr ON gr.id = st.gst_registration_id
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND (si.customer_gstin IS NULL OR si.customer_gstin = '')
		  AND (
		       si.supply_type IS NULL OR si.supply_type = 'INTRA_STATE'
		       OR (si.supply_type = 'INTER_STATE' AND COALESCE(si.grand_total, si.total_amount) <= $3)
		      )
		  AND ($4 = '' OR si.store_id::text = $4)
		GROUP BY pos, rate, sply_ty
		ORDER BY pos, rate, sply_ty`,
		req.StartDate, req.EndDate, b2clThreshold, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("b2cs query: %w", err)
	}
	defer rows.Close()

	out := make([]B2CSItem, 0)
	for rows.Next() {
		var it B2CSItem
		var taxable, cgst, sgst, igst, cess float64
		if err := rows.Scan(&it.Pos, &it.Rt, &it.SplyTy, &taxable, &cgst, &sgst, &igst, &cess); err != nil {
			return nil, fmt.Errorf("b2cs scan: %w", err)
		}
		it.Txval = taxable
		it.Camt = cgst
		it.Samt = sgst
		it.Iamt = igst
		it.Csamt = cess
		out = append(out, it)
	}
	return out, rows.Err()
}

// ---- HSN (Table 12) ----

// buildHSN constructs the single Table 12 HSN summary across all outward
// supplies, grouped by hsn + uqc + rate (a single HSN can carry multiple
// GST rates) and aggregated with SQL ROUND.
//
// The UQC is read DIRECTLY from sales_invoice_items.uqc — the snapshot taken
// at the moment of sale — never by joining back to medicines. A later edit of
// the medicine master must not rewrite already-filed HSN history.
func (b *GSTR1Builder) buildHSN(ctx context.Context, req GSTR1Request) ([]HSNSummary, error) {
	rows, err := b.db.Query(ctx, `
		SELECT COALESCE(sii.hsn_code, 'UNKNOWN') AS hsn,
		       COALESCE(hc.description, '') AS descr,
		       COALESCE(NULLIF(sii.uqc, ''), 'OTH') AS uqc,
		       COALESCE(sii.gst_rate, 0)::float8 AS rate,
		       SUM(sii.quantity + COALESCE(sii.bonus_quantity, 0))::float8 AS total_qty,
		       ROUND(SUM(COALESCE(sii.taxable_value, sii.subtotal)), 2)::float8 AS total_taxable,
		       ROUND(SUM(COALESCE(sii.cgst_amount, 0)), 2)::float8 AS total_cgst,
		       ROUND(SUM(COALESCE(sii.sgst_amount, 0)), 2)::float8 AS total_sgst,
		       ROUND(SUM(COALESCE(sii.igst_amount, 0)), 2)::float8 AS total_igst,
		       ROUND(SUM(COALESCE(sii.cess_amount, 0)), 2)::float8 AS total_cess
		FROM sales_invoices si
		JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		LEFT JOIN hsn_codes hc ON hc.code = sii.hsn_code
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)
		GROUP BY sii.hsn_code, hc.description, sii.uqc, sii.gst_rate
		ORDER BY sii.hsn_code, sii.uqc, sii.gst_rate`,
		req.StartDate, req.EndDate, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("hsn query: %w", err)
	}
	defer rows.Close()

	out := make([]HSNSummary, 0)
	i := 1
	for rows.Next() {
		var h HSNSummary
		if err := rows.Scan(&h.HSNCode, &h.Desc, &h.UQC, &h.Rt,
			&h.Qty, &h.Txval, &h.Camt, &h.Samt, &h.Iamt, &h.Csamt); err != nil {
			return nil, fmt.Errorf("hsn scan: %w", err)
		}
		h.Num = i
		out = append(out, h)
		i++
	}
	return out, rows.Err()
}

// ---- CDNR / CDNUR (Tables 9A / 9B) ----

// buildCDNR constructs Table 9A: credit notes issued to registered recipients
// (customers with a GSTIN), grouped by recipient ctin.
func (b *GSTR1Builder) buildCDNR(ctx context.Context, req GSTR1Request) ([]CDNREntry, error) {
	rows, err := b.creditNoteRows(ctx, req, true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ctinOrder := make([]string, 0)
	byCtin := make(map[string][]creditNoteRow)
	for rows.Next() {
		n, err := scanCreditNoteRow(rows)
		if err != nil {
			return nil, err
		}
		if _, ok := byCtin[n.Ctin]; !ok {
			ctinOrder = append(ctinOrder, n.Ctin)
		}
		byCtin[n.Ctin] = append(byCtin[n.Ctin], n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemsByNote, err := b.noteItemGroups(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]CDNREntry, 0, len(ctinOrder))
	for _, ctin := range ctinOrder {
		notes := byCtin[ctin]
		entry := CDNREntry{Ctin: ctin, Nt: make([]CDNREntryNote, 0, len(notes))}
		for _, n := range notes {
			n.Entry.Itms = itemsByNote[n.NoteID]
			entry.Nt = append(entry.Nt, n.Entry)
		}
		out = append(out, entry)
	}
	return out, nil
}

// buildCDNUR constructs Table 9B: credit notes issued to unregistered
// recipients.
func (b *GSTR1Builder) buildCDNUR(ctx context.Context, req GSTR1Request) ([]CDNURNote, error) {
	rows, err := b.creditNoteRows(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	order := make([]creditNoteRow, 0)
	for rows.Next() {
		n, err := scanCreditNoteRow(rows)
		if err != nil {
			return nil, err
		}
		order = append(order, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemsByNote, err := b.noteItemGroups(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make([]CDNURNote, 0, len(order))
	for _, n := range order {
		note := n.Entry
		note.Itms = itemsByNote[n.NoteID]
		out = append(out, CDNURNote{
			Typ:    "CN",
			NtNum:  note.NtNum,
			NtDt:   note.NtDt,
			Ntty:   note.Ntty,
			Val:    note.Val,
			Pos:    note.Pos,
			Rchrg:  note.Rchrg,
			Inum:   note.Inum,
			Idt:    note.Idt,
			Rsn:    note.Rsn,
			Pgst:   note.Pgst,
			InvTyp: note.InvTyp,
			Itms:   note.Itms,
		})
	}
	return out, nil
}

// creditNoteRows yields credit-note rows, enriched with the recipient ctin
// when "registered" is true, filtered to the report period and store.
func (b *GSTR1Builder) creditNoteRows(ctx context.Context, req GSTR1Request, registered bool) (pgx.Rows, error) {
	filter := "scn.customer_gstin IS NULL OR scn.customer_gstin = ''"
	if registered {
		filter = "scn.customer_gstin IS NOT NULL AND scn.customer_gstin != ''"
	}

	rows, err := b.db.Query(ctx, `
		SELECT scn.id::text, scn.note_no::text, scn.note_date::text,
		       scn.gross_amount::float8,
		       COALESCE(si.customer_state_code, ''),
		       COALESCE(scn.customer_gstin, ''),
		       COALESCE(scn.original_invoice_no, ''),
		       COALESCE(scn.original_invoice_date::text, ''),
		       COALESCE(scn.reason, '')
		FROM sales_credit_notes scn
		JOIN sales_invoices si ON si.id = scn.invoice_id
		WHERE scn.note_date >= $1 AND scn.note_date < $2
		  AND (`+filter+`)
		  AND ($3 = '' OR scn.store_id::text = $3)
		ORDER BY scn.note_date, scn.note_no`,
		req.StartDate, req.EndDate, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("credit note query: %w", err)
	}
	return rows, nil
}

// creditNoteRow is a scanned credit note plus its grouping keys.
type creditNoteRow struct {
	Entry  CDNREntryNote
	Ctin   string
	NoteID string
}

// scanCreditNoteRow reads one credit-note row into a creditNoteRow.
func scanCreditNoteRow(rows pgx.Rows) (creditNoteRow, error) {
	var (
		r           creditNoteRow
		origInvDate string
	)
	if err := rows.Scan(&r.NoteID, &r.Entry.NtNum, &r.Entry.NtDt, &r.Entry.Val,
		&r.Entry.Pos, &r.Ctin, &r.Entry.Inum, &origInvDate, &r.Entry.Rsn); err != nil {
		return r, fmt.Errorf("credit note scan: %w", err)
	}
	r.Entry.NtDt = formatGSTDate(r.Entry.NtDt)
	r.Entry.Idt = formatGSTDate(origInvDate)
	r.Entry.Ntty = "C"
	r.Entry.Rchrg = "N"
	r.Entry.InvTyp = "R"
	return r, nil
}

// noteItemGroups maps credit note ids to their per-rate item detail rows.
func (b *GSTR1Builder) noteItemGroups(ctx context.Context, req GSTR1Request) (map[string][]B2BLineItem, error) {
	rows, err := b.db.Query(ctx, `
		SELECT scni.credit_note_id::text,
		       COALESCE(scni.taxable_value, 0)::float8,
		       COALESCE(scni.gst_rate, 0)::float8,
		       COALESCE(scni.cgst_amount, 0)::float8,
		       COALESCE(scni.sgst_amount, 0)::float8,
		       COALESCE(scni.igst_amount, 0)::float8,
		       COALESCE(scni.cess_amount, 0)::float8
		FROM sales_credit_note_items scni
		JOIN sales_credit_notes scn ON scn.id = scni.credit_note_id
		WHERE scn.note_date >= $1 AND scn.note_date < $2
		  AND ($3 = '' OR scn.store_id::text = $3)
		ORDER BY scni.credit_note_id, scni.id`,
		req.StartDate, req.EndDate, req.StoreID)
	if err != nil {
		return nil, fmt.Errorf("credit note items query: %w", err)
	}
	defer rows.Close()

	byNote := make(map[string][]B2BLineItem)
	for rows.Next() {
		var noteID string
		var taxable, rate, cgst, sgst, igst, cess float64
		if err := rows.Scan(&noteID, &taxable, &rate, &cgst, &sgst, &igst, &cess); err != nil {
			return nil, fmt.Errorf("credit note item scan: %w", err)
		}
		items := byNote[noteID]
		items = append(items, B2BLineItem{
			Num:    len(items) + 1,
			ItmDet: itmDet(taxable, rate, cgst, sgst, igst, cess),
		})
		byNote[noteID] = items
	}
	return byNote, rows.Err()
}

// ---- Document series (Table 13) ----

// buildDocIssue constructs the Table 13 summary of documents issued:
// one doc_det entry per document type, with the numeric series ranges.
func (b *GSTR1Builder) buildDocIssue(ctx context.Context, req GSTR1Request) (DocIssue, error) {
	docIssue := DocIssue{DocDet: make([]DocDetail, 0)}
	num := 0

	// Outward invoices.
	rows, err := b.db.Query(ctx, `
		SELECT si.invoice_no
		FROM sales_invoices si
		WHERE si.invoice_date >= $1 AND si.invoice_date < $2
		  AND ($3 = '' OR si.store_id::text = $3)
		ORDER BY si.invoice_no`,
		req.StartDate, req.EndDate, req.StoreID)
	if err != nil {
		return DocIssue{}, fmt.Errorf("doc summary: %w", err)
	}

	type series struct {
		from, to string
		count    int
	}
	bySeries := make(map[string]*series)
	order := make([]string, 0)
	for rows.Next() {
		var invNo string
		if err := rows.Scan(&invNo); err != nil {
			rows.Close()
			return DocIssue{}, fmt.Errorf("doc summary scan: %w", err)
		}
		prefix, serial := splitSeries(invNo)
		s, ok := bySeries[prefix]
		if !ok {
			s = &series{from: serial, to: serial}
			bySeries[prefix] = s
			order = append(order, prefix)
		}
		s.count++
		if lessSerial(serial, s.from) {
			s.from = serial
		}
		if lessSerial(s.to, serial) {
			s.to = serial
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return DocIssue{}, err
	}

	if len(order) > 0 {
		num++
		invDet := DocDetail{DocNum: num, DocTyp: "Invoices for outward supply"}
		for _, prefix := range order {
			s := bySeries[prefix]
			invDet.Docs = append(invDet.Docs, DocRange{
				Num:      len(invDet.Docs) + 1,
				From:     serialWithPrefix(prefix, s.from),
				To:       serialWithPrefix(prefix, s.to),
				TotNum:   float64(s.count),
				Cancel:   0,
				NetIssue: float64(s.count),
			})
		}
		docIssue.DocDet = append(docIssue.DocDet, invDet)
	}

	// Credit notes.
	var cnTotal int
	err = b.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM sales_credit_notes scn
		WHERE scn.note_date >= $1 AND scn.note_date < $2
		  AND ($3 = '' OR scn.store_id::text = $3)`,
		req.StartDate, req.EndDate, req.StoreID).Scan(&cnTotal)
	if err != nil {
		return DocIssue{}, fmt.Errorf("doc summary cn count: %w", err)
	}
	if cnTotal > 0 {
		num++
		docIssue.DocDet = append(docIssue.DocDet, DocDetail{
			DocNum: num,
			DocTyp: "Credit Note",
			Docs: []DocRange{{
				Num:      1,
				From:     "1",
				To:       strconv.Itoa(cnTotal),
				TotNum:   float64(cnTotal),
				Cancel:   0,
				NetIssue: float64(cnTotal),
			}},
		})
	}

	return docIssue, nil
}

// serialWithPrefix renders a full document number from a series prefix and
// numeric serial. A flat series (no numeric suffix) is used as-is.
func serialWithPrefix(prefix, serial string) string {
	if prefix == serial {
		return serial
	}
	return prefix + serial
}

// splitSeries splits an invoice number like "INV/26-27/00042" into its
// prefix ("INV/26-27/") and trailing numeric serial ("00042"). If no trailing
// number is present the whole number is treated as a flat series.
func splitSeries(invNo string) (prefix, serial string) {
	i := len(invNo)
	for i > 0 && invNo[i-1] >= '0' && invNo[i-1] <= '9' {
		i--
	}
	if i == len(invNo) {
		return invNo, invNo
	}
	return invNo[:i], invNo[i:]
}

// lessSerial compares two numeric serial strings numerically.
func lessSerial(a, b string) bool {
	x, _ := strconv.ParseInt(a, 10, 64)
	y, _ := strconv.ParseInt(b, 10, 64)
	return x < y
}

// itmDet composes a per-rate item detail from a sale line's values.
func itmDet(taxable, rate, cgst, sgst, igst, cess float64) ItmDet {
	return ItmDet{
		Rt:    rate,
		Txval: taxable,
		Camt:  cgst,
		Samt:  sgst,
		Iamt:  igst,
		Csamt: cess,
	}
}

// ---- helpers ----

func formatGSTDate(dateStr string) string {
	if dateStr == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", dateStr[:10])
	if err != nil {
		return dateStr
	}
	return t.Format("02-01-2006")
}
