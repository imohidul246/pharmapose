package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// SalesInvoiceRow is one line in the searchable sales-invoice history.
type SalesInvoiceRow struct {
	models.SalesInvoice
	CustomerName string `json:"customer_name"`
	ItemCount    int    `json:"item_count"`
	UnitsSold    int    `json:"units_sold"`
}

type SalesInvoiceItemDetail struct {
	models.SalesInvoiceItem
	MedicineName string `json:"medicine_name"`
	BatchNumber  string `json:"batch_number"`
}

type SalesInvoiceDetail struct {
	Invoice      models.SalesInvoice      `json:"invoice"`
	CustomerName string                   `json:"customer_name"`
	Items        []SalesInvoiceItemDetail `json:"items"`
}

// PurchaseInvoiceRow is one line in the searchable purchase-invoice history.
type PurchaseInvoiceRow struct {
	models.PurchaseOrder
	ItemCount      int `json:"item_count"`
	UnitsPurchased int `json:"units_purchased"`
}

type PurchaseInvoiceDetail struct {
	Invoice models.PurchaseOrder       `json:"invoice"`
	Items   []models.PurchaseOrderItem `json:"items"`
}

const invoiceSearchLimit = 200

// ListSalesInvoices returns sales invoices inside [start, end), optionally
// narrowed by a partial invoice-number match (substring of the numeric no).
func (r *SaleRepo) ListInvoices(ctx context.Context, storeID string, start, end time.Time, invoiceQuery string) ([]SalesInvoiceRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT si.id::text, si.invoice_no, si.customer_id::text, si.payment_type::text,
		       si.total_amount::float8, si.discount_total::float8, si.created_at,
		       COALESCE(c.name, ''),
		       COUNT(sii.id)::int,
		       COALESCE(SUM(sii.quantity), 0)::int,
		       si.sale_type,
		       si.supply_type,
		       si.grand_total::float8,
		       si.tax_total::float8
		FROM sales_invoices si
		LEFT JOIN customers c ON c.id = si.customer_id AND c.store_id = si.store_id
		LEFT JOIN sales_invoice_items sii ON sii.invoice_id = si.id
		WHERE si.store_id = $1 AND si.invoice_date >= $2 AND si.invoice_date < $3
		  AND ($4 = '' OR si.invoice_no LIKE '%' || $4 || '%')
		GROUP BY si.id, c.name
		ORDER BY si.created_at DESC, si.invoice_no DESC
		LIMIT $5`, storeID, start, end, invoiceQuery, invoiceSearchLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SalesInvoiceRow, 0)
	for rows.Next() {
		var (
			row         SalesInvoiceRow
			paymentType string
		)
		if err := rows.Scan(&row.ID, &row.InvoiceNo, &row.CustomerID, &paymentType,
			&row.TotalAmount, &row.DiscountTotal, &row.CreatedAt,
			&row.CustomerName, &row.ItemCount, &row.UnitsSold,
			&row.SaleType, &row.SupplyType, &row.GrandTotal, &row.TaxTotal); err != nil {
			return nil, err
		}
		row.PaymentType = models.PaymentType(paymentType)
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetSalesInvoice loads one sales invoice plus its enriched line items.
func (r *SaleRepo) GetInvoice(ctx context.Context, storeID, id string) (*SalesInvoiceDetail, error) {
	var (
		d           SalesInvoiceDetail
		customerID  *string
		paymentType string
	)
	err := r.db.QueryRow(ctx, `
		SELECT si.id::text, si.invoice_no, si.customer_id::text, si.payment_type::text,
		       si.total_amount::float8, si.discount_total::float8, si.created_at,
		       COALESCE(c.name, ''),
		       si.supply_type,
		       si.gross_amount::float8, si.taxable_amount::float8,
		       si.cgst_total::float8, si.sgst_total::float8, si.igst_total::float8,
		       si.cess_total::float8, si.tax_total::float8, si.round_off::float8,
		       si.grand_total::float8, si.price_includes_tax,
		       si.invoice_date, si.financial_year,
		       si.sale_type, si.buyer_name, si.buyer_gstin, si.buyer_address
		FROM sales_invoices si
		LEFT JOIN customers c ON c.id = si.customer_id AND c.store_id = si.store_id
		WHERE si.id = $1 AND si.store_id = $2`, id, storeID).
		Scan(&d.Invoice.ID, &d.Invoice.InvoiceNo, &customerID, &paymentType,
			&d.Invoice.TotalAmount, &d.Invoice.DiscountTotal, &d.Invoice.CreatedAt,
			&d.CustomerName,
			&d.Invoice.SupplyType,
			&d.Invoice.GrossAmount, &d.Invoice.TaxableAmount,
			&d.Invoice.CGSTTotal, &d.Invoice.SGSTTotal, &d.Invoice.IGSTTotal,
			&d.Invoice.CessTotal, &d.Invoice.TaxTotal, &d.Invoice.RoundOff,
			&d.Invoice.GrandTotal, &d.Invoice.PriceIncludesTax,
			&d.Invoice.InvoiceDate, &d.Invoice.FinancialYear,
			&d.Invoice.SaleType, &d.Invoice.BuyerName, &d.Invoice.BuyerGSTIN, &d.Invoice.BuyerAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Invoice.CustomerID = customerID
	d.Invoice.PaymentType = models.PaymentType(paymentType)
	d.Invoice.StoreID = &storeID

	rows, err := r.db.Query(ctx, `
		SELECT sii.id::text, sii.invoice_id::text, sii.medicine_id::text, sii.batch_id::text,
		       sii.quantity, sii.unit_sale_price::float8, sii.subtotal::float8,
		       sii.discount_type, sii.discount_value::float8, sii.discount_amount::float8,
		       m.name, b.batch_number,
		       sii.mrp::float8, sii.bonus_quantity,
		       sii.hsn_code,
		       sii.gross_amount::float8, sii.taxable_value::float8, sii.gst_rate::float8,
		       sii.cgst_rate::float8, sii.cgst_amount::float8,
		       sii.sgst_rate::float8, sii.sgst_amount::float8,
		       sii.igst_rate::float8, sii.igst_amount::float8,
		       sii.cess_rate::float8, sii.cess_amount::float8, sii.line_total::float8
		FROM sales_invoice_items sii
		JOIN medicines m ON m.id = sii.medicine_id
		JOIN batches b ON b.id = sii.batch_id
		WHERE sii.invoice_id = $1
		ORDER BY m.name, sii.id`, d.Invoice.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	d.Items = make([]SalesInvoiceItemDetail, 0)
	for rows.Next() {
		var it SalesInvoiceItemDetail
		if err := rows.Scan(&it.ID, &it.InvoiceID, &it.MedicineID, &it.BatchID,
			&it.Quantity, &it.UnitSalePrice, &it.Subtotal,
			&it.DiscountType, &it.DiscountValue, &it.DiscountAmount,
			&it.MedicineName, &it.BatchNumber,
			&it.MRP, &it.BonusQuantity,
			&it.HSNCode,
			&it.GrossAmount, &it.TaxableValue, &it.GSTRate,
			&it.CGSTRate, &it.CGSTAmount,
			&it.SGSTRate, &it.SGSTAmount,
			&it.IGSTRate, &it.IGSTAmount,
			&it.CessRate, &it.CessAmount, &it.LineTotal); err != nil {
			return nil, err
		}
		d.Items = append(d.Items, it)
	}
	return &d, rows.Err()
}

// GetInvoiceByNo resolves a sales invoice for a customer by its printed number
// (the format referenced from ledger-entry notes) and returns the full detail.
// The newest match wins; bare numeric numbers also match the pre-GST
// "INV/000xx" formatting used to backfill older records.
func (r *SaleRepo) GetInvoiceByNo(ctx context.Context, storeID, customerID, invoiceNo string) (*SalesInvoiceDetail, error) {
	var id string
	err := r.db.QueryRow(ctx, `
		SELECT si.id::text
		FROM sales_invoices si
		WHERE si.customer_id = $1 AND si.store_id = $3
		  AND (si.invoice_no = $2
		       OR ($2 ~ '^[0-9]+$' AND si.invoice_no = 'INV/' || LPAD($2, 5, '0')))
		ORDER BY si.created_at DESC
		LIMIT 1`, customerID, invoiceNo, storeID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetInvoice(ctx, storeID, id)
}

// ListPurchaseInvoices returns purchase invoices inside [start, end),
// optionally narrowed by a partial invoice-number match.
func (r *PurchaseRepo) ListInvoices(ctx context.Context, storeID string, start, end time.Time, invoiceQuery string) ([]PurchaseInvoiceRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT po.id::text, po.invoice_no, po.supplier_name,
		       po.total_amount::float8, po.discount_total::float8, po.created_at,
		       COUNT(poi.id)::int,
		       COALESCE(SUM(poi.quantity), 0)::int,
		       po.supply_type,
		       po.tax_total::float8,
		       po.grand_total::float8
		FROM purchase_orders po
		LEFT JOIN purchase_order_items poi ON poi.purchase_id = po.id
		WHERE po.store_id = $1 AND po.invoice_date >= $2 AND po.invoice_date < $3
		  AND ($4 = '' OR po.invoice_no ILIKE '%' || $4 || '%')
		GROUP BY po.id
		ORDER BY po.created_at DESC
		LIMIT $5`, storeID, start, end, invoiceQuery, invoiceSearchLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PurchaseInvoiceRow, 0)
	for rows.Next() {
		var row PurchaseInvoiceRow
		if err := rows.Scan(&row.ID, &row.InvoiceNo, &row.SupplierName,
			&row.TotalAmount, &row.DiscountTotal, &row.CreatedAt, &row.ItemCount, &row.UnitsPurchased,
			&row.SupplyType, &row.TaxTotal, &row.GrandTotal); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetPurchaseInvoice loads one purchase order plus its line items with names.
func (r *PurchaseRepo) GetInvoice(ctx context.Context, storeID, id string) (*PurchaseInvoiceDetail, error) {
	var d PurchaseInvoiceDetail
	err := r.db.QueryRow(ctx, `
		SELECT id::text, invoice_no, supplier_name, total_amount::float8, discount_total::float8, created_at,
		       supplier_id::text, supplier_gstin, supplier_state_code,
		       store_id::text, supply_type,
		       gross_amount::float8, taxable_amount::float8,
		       cgst_total::float8, sgst_total::float8, igst_total::float8,
		       cess_total::float8, tax_total::float8, grand_total::float8,
		       price_includes_tax, invoice_date, financial_year
		FROM purchase_orders WHERE id = $1 AND store_id = $2`, id, storeID).
		Scan(&d.Invoice.ID, &d.Invoice.InvoiceNo, &d.Invoice.SupplierName,
			&d.Invoice.TotalAmount, &d.Invoice.DiscountTotal, &d.Invoice.CreatedAt,
			&d.Invoice.SupplierID, &d.Invoice.SupplierGSTIN, &d.Invoice.SupplierStateCode,
			&d.Invoice.StoreID, &d.Invoice.SupplyType,
			&d.Invoice.GrossAmount, &d.Invoice.TaxableAmount,
			&d.Invoice.CGSTTotal, &d.Invoice.SGSTTotal, &d.Invoice.IGSTTotal,
			&d.Invoice.CessTotal, &d.Invoice.TaxTotal, &d.Invoice.GrandTotal,
			&d.Invoice.PriceIncludesTax, &d.Invoice.InvoiceDate, &d.Invoice.FinancialYear)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT poi.id::text, poi.purchase_id::text, poi.medicine_id::text,
		       poi.batch_number, poi.expiry_date, poi.quantity, poi.bonus_quantity,
		       poi.purchase_price::float8, poi.sale_price::float8,
		       poi.discount_type, poi.discount_value::float8, poi.discount_amount::float8,
		       m.name,
		       poi.hsn_code,
		       poi.gross_amount::float8, poi.taxable_value::float8, poi.gst_rate::float8,
		       poi.cgst_rate::float8, poi.cgst_amount::float8,
		       poi.sgst_rate::float8, poi.sgst_amount::float8,
		       poi.igst_rate::float8, poi.igst_amount::float8,
		       poi.cess_rate::float8, poi.cess_amount::float8, poi.line_total::float8
		FROM purchase_order_items poi
		JOIN medicines m ON m.id = poi.medicine_id
		WHERE poi.purchase_id = $1
		ORDER BY m.name, poi.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	d.Items = make([]models.PurchaseOrderItem, 0)
	for rows.Next() {
		var it models.PurchaseOrderItem
		var expiry time.Time
		if err := rows.Scan(&it.ID, &it.PurchaseID, &it.MedicineID,
			&it.BatchNumber, &expiry, &it.Quantity, &it.BonusQuantity,
			&it.PurchasePrice, &it.SalePrice,
			&it.DiscountType, &it.DiscountValue, &it.DiscountAmount,
			&it.MedicineName,
			&it.HSNCode,
			&it.GrossAmount, &it.TaxableValue, &it.GSTRate,
			&it.CGSTRate, &it.CGSTAmount,
			&it.SGSTRate, &it.SGSTAmount,
			&it.IGSTRate, &it.IGSTAmount,
			&it.CessRate, &it.CessAmount, &it.LineTotal); err != nil {
			return nil, err
		}
		it.ExpiryDate = models.NewDate(expiry)
		d.Items = append(d.Items, it)
	}
	return &d, rows.Err()
}
