package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type ReportRepo struct {
	db *pgxpool.Pool
}

func NewReportRepo(db *pgxpool.Pool) *ReportRepo { return &ReportRepo{db: db} }

type SalesBreakdown struct {
	PaymentType string  `json:"payment_type"`
	Invoices    int     `json:"invoices"`
	Total       float64 `json:"total"`
	UnitsSold   int     `json:"units_sold"`
}

type DailySales struct {
	Day         string  `json:"day"`
	PaymentType string  `json:"payment_type"`
	Invoices    int     `json:"invoices"`
	Total       float64 `json:"total"`
}

type SalesSummary struct {
	Start     time.Time        `json:"start_date"`
	End       time.Time        `json:"end_date"`
	Breakdown []SalesBreakdown `json:"breakdown"`
	Daily     []DailySales     `json:"daily"`
	NetSales  float64          `json:"net_sales"`
	NetUnits  int              `json:"net_units"`
}

func (r *ReportRepo) Sales(ctx context.Context, storeID string, start, end time.Time) (*SalesSummary, error) {
	out := &SalesSummary{Start: start, End: end, Breakdown: []SalesBreakdown{}, Daily: []DailySales{}}

	rows, err := r.db.Query(ctx, `
		SELECT si.payment_type::text,
		       COUNT(DISTINCT si.id)::int,
		       COALESCE(SUM(si.total_amount), 0)::float8,
		       COALESCE((SELECT SUM(sii.quantity)
		                 FROM sales_invoice_items sii
		                 JOIN sales_invoices s2 ON s2.id = sii.invoice_id
		                 WHERE s2.created_at >= $1 AND s2.created_at < $2
		                   AND s2.store_id = $3
		                   AND s2.payment_type = si.payment_type), 0)::int
		FROM sales_invoices si
		WHERE si.created_at >= $1 AND si.created_at < $2 AND si.store_id = $3
		GROUP BY si.payment_type
		ORDER BY si.payment_type`, start, end, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b SalesBreakdown
		if err := rows.Scan(&b.PaymentType, &b.Invoices, &b.Total, &b.UnitsSold); err != nil {
			return nil, err
		}
		out.Breakdown = append(out.Breakdown, b)
		out.NetSales += b.Total
		out.NetUnits += b.UnitsSold
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dailyRows, err := r.db.Query(ctx, `
		SELECT (si.created_at AT TIME ZONE 'UTC')::date::text,
		       si.payment_type::text,
		       COUNT(*)::int,
		       COALESCE(SUM(si.total_amount), 0)::float8
		FROM sales_invoices si
		WHERE si.created_at >= $1 AND si.created_at < $2 AND si.store_id = $3
		GROUP BY 1, 2
		ORDER BY 1, 2`, start, end, storeID)
	if err != nil {
		return nil, err
	}
	defer dailyRows.Close()
	for dailyRows.Next() {
		var d DailySales
		if err := dailyRows.Scan(&d.Day, &d.PaymentType, &d.Invoices, &d.Total); err != nil {
			return nil, err
		}
		out.Daily = append(out.Daily, d)
	}
	return out, dailyRows.Err()
}

type SupplierPurchase struct {
	SupplierName string  `json:"supplier_name"`
	Orders       int     `json:"orders"`
	Items        int     `json:"items"`
	Total        float64 `json:"total"`
}

type PurchaseSummary struct {
	Start      time.Time          `json:"start_date"`
	End        time.Time          `json:"end_date"`
	OrderCount int                `json:"order_count"`
	ItemCount  int                `json:"item_count"`
	TotalSpend float64            `json:"total_spend"`
	Suppliers  []SupplierPurchase `json:"suppliers"`
}

func (r *ReportRepo) Purchases(ctx context.Context, storeID string, start, end time.Time) (*PurchaseSummary, error) {
	out := &PurchaseSummary{Start: start, End: end, Suppliers: []SupplierPurchase{}}

	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(COUNT(*), 0)::int,
		       COALESCE(SUM(total_amount), 0)::float8
		FROM purchase_orders WHERE created_at >= $1 AND created_at < $2 AND store_id = $3`,
		start, end, storeID).Scan(&out.OrderCount, &out.TotalSpend)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, `
		SELECT s.supplier_name,
		       s.orders::int,
		       COALESCE(i.items, 0)::int,
		       s.spend::float8
		FROM (
			SELECT supplier_name,
			       COUNT(*) AS orders,
			       SUM(total_amount) AS spend
			FROM purchase_orders
			WHERE created_at >= $1 AND created_at < $2 AND store_id = $3
			GROUP BY supplier_name
		) s
		LEFT JOIN (
			SELECT po.supplier_name,
			       COUNT(poi.id) AS items
			FROM purchase_order_items poi
			JOIN purchase_orders po ON po.id = poi.purchase_id
			WHERE po.created_at >= $1 AND po.created_at < $2 AND po.store_id = $3
			GROUP BY po.supplier_name
		) i ON i.supplier_name = s.supplier_name
		ORDER BY spend DESC`, start, end, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	itemCount := 0
	for rows.Next() {
		var s SupplierPurchase
		if err := rows.Scan(&s.SupplierName, &s.Orders, &s.Items, &s.Total); err != nil {
			return nil, err
		}
		out.Suppliers = append(out.Suppliers, s)
		itemCount += s.Items
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.ItemCount = itemCount
	return out, nil
}

type ProfitLossLine struct {
	MedicineID   string  `json:"medicine_id"`
	MedicineName string  `json:"medicine_name"`
	UnitsSold    int     `json:"units_sold"`
	Revenue      float64 `json:"revenue"`
	Cost         float64 `json:"cost"`
	Profit       float64 `json:"profit"`
	MarginPct    float64 `json:"margin_pct"`
}

type ProfitLossReport struct {
	Start     time.Time        `json:"start_date"`
	End       time.Time        `json:"end_date"`
	Lines     []ProfitLossLine `json:"lines"`
	Revenue   float64          `json:"total_revenue"`
	Cost      float64          `json:"total_cost"`
	Profit    float64          `json:"total_profit"`
	MarginPct float64          `json:"margin_pct"`
}

// ProfitLoss matches sold units against the purchase price recorded on the exact
// batch node that was sold, so margin reflects historical batch cost.
func (r *ReportRepo) ProfitLoss(ctx context.Context, storeID string, start, end time.Time) (*ProfitLossReport, error) {
	rows, err := r.db.Query(ctx, `
		SELECT m.id::text,
		       m.name,
		       SUM(sii.quantity)::int,
		       SUM(sii.subtotal)::float8 AS revenue,
		       SUM(sii.quantity * b.purchase_price)::float8 AS cost
		FROM sales_invoice_items sii
		JOIN sales_invoices si ON si.id = sii.invoice_id
		JOIN batches b ON b.id = sii.batch_id
		JOIN medicines m ON m.id = sii.medicine_id
		WHERE si.created_at >= $1 AND si.created_at < $2 AND si.store_id = $3
		GROUP BY m.id, m.name
		ORDER BY revenue DESC`, start, end, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := &ProfitLossReport{Start: start, End: end, Lines: []ProfitLossLine{}}
	for rows.Next() {
		var l ProfitLossLine
		if err := rows.Scan(&l.MedicineID, &l.MedicineName, &l.UnitsSold, &l.Revenue, &l.Cost); err != nil {
			return nil, err
		}
		l.Profit = tax.RoundMoney(decimal.NewFromFloat(l.Revenue).Sub(decimal.NewFromFloat(l.Cost))).InexactFloat64()
		if l.Revenue > 0 {
			l.MarginPct = tax.RoundMoney(decimal.NewFromFloat(l.Profit).Div(decimal.NewFromFloat(l.Revenue)).Mul(decimal.NewFromInt(100))).InexactFloat64()
		}
		out.Lines = append(out.Lines, l)
		out.Revenue += l.Revenue
		out.Cost += l.Cost
		out.Profit += l.Profit
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.Revenue = tax.RoundMoney(decimal.NewFromFloat(out.Revenue)).InexactFloat64()
	out.Cost = tax.RoundMoney(decimal.NewFromFloat(out.Cost)).InexactFloat64()
	out.Profit = tax.RoundMoney(decimal.NewFromFloat(out.Profit)).InexactFloat64()
	if out.Revenue > 0 {
		out.MarginPct = tax.RoundMoney(decimal.NewFromFloat(out.Profit).Div(decimal.NewFromFloat(out.Revenue)).Mul(decimal.NewFromInt(100))).InexactFloat64()
	}
	return out, nil
}

type ExpiringBatch struct {
	BatchID       string  `json:"batch_id"`
	MedicineID    string  `json:"medicine_id"`
	MedicineName  string  `json:"medicine_name"`
	Manufacturer  string  `json:"manufacturer"`
	BatchNumber   string  `json:"batch_number"`
	ExpiryDate    string  `json:"expiry_date"`
	CurrentStock  int     `json:"current_stock"`
	PurchasePrice float64 `json:"purchase_price"`
	SalePrice     float64 `json:"sale_price"`
	StockValue    float64 `json:"stock_value"`
	Expired       bool    `json:"expired"`
}

func (r *ReportRepo) Expiry(ctx context.Context, storeID string, withinMonths int) ([]ExpiringBatch, error) {
	rows, err := r.db.Query(ctx, `
		SELECT b.id::text, m.id::text, m.name, m.manufacturer,
		       b.batch_number, b.expiry_date::text,
		       b.current_stock, b.purchase_price::float8, b.sale_price::float8,
		       (b.current_stock * b.purchase_price)::float8,
		       (b.expiry_date < CURRENT_DATE)
		FROM batches b
		JOIN medicines m ON m.id = b.medicine_id AND m.deleted_at IS NULL
		WHERE b.current_stock > 0
		  AND b.store_id = $2
		  AND b.expiry_date <= CURRENT_DATE + make_interval(months => $1)
		ORDER BY b.expiry_date ASC`, withinMonths, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ExpiringBatch, 0)
	for rows.Next() {
		var b ExpiringBatch
		if err := rows.Scan(&b.BatchID, &b.MedicineID, &b.MedicineName, &b.Manufacturer,
			&b.BatchNumber, &b.ExpiryDate,
			&b.CurrentStock, &b.PurchasePrice, &b.SalePrice,
			&b.StockValue, &b.Expired); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

type LowStockItem struct {
	MedicineID      string `json:"medicine_id"`
	MedicineName    string `json:"medicine_name"`
	Manufacturer    string `json:"manufacturer"`
	MinReorderLevel int    `json:"min_reorder_level"`
	TotalStock      int    `json:"total_stock"`
	Shortfall       int    `json:"shortfall"`
}

func (r *ReportRepo) LowStock(ctx context.Context, storeID string) ([]LowStockItem, error) {
	rows, err := r.db.Query(ctx, `
		SELECT m.id::text, m.name, m.manufacturer, m.min_reorder_level,
		       COALESCE(SUM(b.current_stock), 0)::int AS total_stock
		FROM medicines m
		LEFT JOIN batches b
		       ON b.medicine_id = m.id AND b.expiry_date >= CURRENT_DATE
		WHERE m.deleted_at IS NULL AND m.min_reorder_level > 0 AND m.store_id = $1
		GROUP BY m.id, m.name, m.manufacturer, m.min_reorder_level
		HAVING COALESCE(SUM(b.current_stock), 0) < m.min_reorder_level
		ORDER BY COALESCE(SUM(b.current_stock), 0) - m.min_reorder_level ASC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]LowStockItem, 0)
	for rows.Next() {
		var it LowStockItem
		if err := rows.Scan(&it.MedicineID, &it.MedicineName, &it.Manufacturer,
			&it.MinReorderLevel, &it.TotalStock); err != nil {
			return nil, err
		}
		it.Shortfall = it.MinReorderLevel - it.TotalStock
		out = append(out, it)
	}
	return out, rows.Err()
}
