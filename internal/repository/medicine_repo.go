package repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// LockedBatch is a tenant-verified batch row held under SELECT ... FOR UPDATE
// by LockBatchesForUpdate. It carries every column the locked writers need
// (checkout pricing + UQC snapshot, reconcile costing, audit naming) so all
// batch-mutating flows share one lock path and one snapshot shape.
type LockedBatch struct {
	ID            string
	MedicineID    string
	MedicineName  string
	BatchNumber   string
	SalePrice     float64
	PurchasePrice float64
	CurrentStock  int
	UQC           string
}

// LockBatchesForUpdate is the single, shared batch-locking utility for every
// transaction that mutates stock (checkout, reconciliations, stock audits, PO
// adjustments). Before touching the database it deduplicates batchIDs and
// sorts them in strict lexicographical order, then locks each row
// individually in that order:
//
//	sort.Slice(batchIDs, func(i, j int) bool { return batchIDs[i] < batchIDs[j] })
//	// SELECT ... FROM batches WHERE id = $1 AND store_id = $2 FOR UPDATE
//
// Locking in a canonical order — never in client payload order — is what
// eliminates lock-order-inversion deadlocks (PostgreSQL 40P01) when concurrent
// transactions touch overlapping batches in different orders.
//
// Tenant isolation is enforced per row (store_id = $2): a batch belonging to
// another store matches nothing and is simply absent from the returned map,
// so callers can never lock, read, or mutate foreign stock. Query failures
// abort with an error; absent IDs do not.
func LockBatchesForUpdate(ctx context.Context, tx pgx.Tx, storeID string, batchIDs []string) (map[string]LockedBatch, error) {
	seen := make(map[string]struct{}, len(batchIDs))
	unique := make([]string, 0, len(batchIDs))
	for _, id := range batchIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })

	locked := make(map[string]LockedBatch, len(unique))
	for _, id := range unique {
		var lb LockedBatch
		err := tx.QueryRow(ctx, `
			SELECT b.id::text, b.medicine_id::text, m.name, b.batch_number,
			       b.sale_price::float8, b.purchase_price::float8, b.current_stock,
			       COALESCE(m.uqc, 'OTH')
			FROM batches b
			JOIN medicines m ON m.id = b.medicine_id
			WHERE b.id = $1 AND b.store_id = $2
			FOR UPDATE OF b`, id, storeID).Scan(
			&lb.ID, &lb.MedicineID, &lb.MedicineName, &lb.BatchNumber,
			&lb.SalePrice, &lb.PurchasePrice, &lb.CurrentStock, &lb.UQC)
		if errors.Is(err, pgx.ErrNoRows) {
			// Unknown ID or foreign-tenant batch: absent by design (no
			// oracle). Callers map absence to their domain error.
			continue
		}
		if err != nil {
			return nil, err
		}
		locked[id] = lb
	}
	return locked, nil
}

type MedicineRepo struct {
	db *pgxpool.Pool
}

func NewMedicineRepo(db *pgxpool.Pool) *MedicineRepo {
	return &MedicineRepo{db: db}
}

const medicineColumns = `id, name, salt_composition, manufacturer, min_reorder_level, packing, uqc, created_at, updated_at`

func scanMedicine(row pgx.Row) (*models.Medicine, error) {
	var m models.Medicine
	err := row.Scan(&m.ID, &m.Name, &m.SaltComposition, &m.Manufacturer,
		&m.MinReorderLevel, &m.Packing, &m.UQC, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *MedicineRepo) Create(ctx context.Context, storeID string, m *models.Medicine) error {
	if err := requireStoreID(storeID); err != nil {
		return err
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO medicines (name, salt_composition, manufacturer, min_reorder_level, packing, uqc, store_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+medicineColumns,
		m.Name, m.SaltComposition, m.Manufacturer, m.MinReorderLevel, m.Packing, m.UQC, storeID,
	).Scan(&m.ID, &m.Name, &m.SaltComposition, &m.Manufacturer,
		&m.MinReorderLevel, &m.Packing, &m.UQC, &m.CreatedAt, &m.UpdatedAt)
}

func (r *MedicineRepo) GetByID(ctx context.Context, storeID, id string) (*models.Medicine, error) {
	if err := requireStoreID(storeID); err != nil {
		return nil, err
	}
	m, err := scanMedicine(r.db.QueryRow(ctx,
		`SELECT `+medicineColumns+` FROM medicines WHERE id = $1 AND store_id = $2 AND deleted_at IS NULL`, id, storeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	return m, err
}

func (r *MedicineRepo) List(ctx context.Context, storeID string) ([]models.Medicine, error) {
	if err := requireStoreID(storeID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+medicineColumns+` FROM medicines WHERE store_id = $1 AND deleted_at IS NULL ORDER BY name`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Medicine, 0)
	for rows.Next() {
		var m models.Medicine
		if err := rows.Scan(&m.ID, &m.Name, &m.SaltComposition, &m.Manufacturer,
			&m.MinReorderLevel, &m.Packing, &m.UQC, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *MedicineRepo) Update(ctx context.Context, storeID string, m *models.Medicine) error {
	if err := requireStoreID(storeID); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE medicines
		SET name = $2, salt_composition = $3, manufacturer = $4,
		    min_reorder_level = $5, packing = $6, uqc = $7, updated_at = now()
		WHERE id = $1 AND store_id = $8 AND deleted_at IS NULL`,
		m.ID, m.Name, m.SaltComposition, m.Manufacturer, m.MinReorderLevel, m.Packing, m.UQC, storeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

func (r *MedicineRepo) SoftDelete(ctx context.Context, storeID, id string) error {
	if err := requireStoreID(storeID); err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx,
		`UPDATE medicines SET deleted_at = $2, updated_at = now() WHERE id = $1 AND store_id = $3 AND deleted_at IS NULL`,
		id, time.Now(), storeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// FindBatchByNumber resolves the batch row for a medicine's batch number
// (used after upserts and by tooling that works in batch-number space).
func (r *MedicineRepo) FindBatchByNumber(ctx context.Context, storeID, medicineID, batchNumber string) (*models.Batch, error) {
	if err := requireStoreID(storeID); err != nil {
		return nil, err
	}
	var b models.Batch
	var expiry time.Time
	err := r.db.QueryRow(ctx, `
		SELECT id::text, medicine_id::text, batch_number, expiry_date,
		       purchase_price::float8, sale_price::float8, current_stock, created_at, updated_at
		FROM batches WHERE medicine_id = $1 AND batch_number = $2 AND store_id = $3`,
		medicineID, batchNumber, storeID,
	).Scan(&b.ID, &b.MedicineID, &b.BatchNumber, &expiry,
		&b.PurchasePrice, &b.SalePrice, &b.CurrentStock, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	b.ExpiryDate = models.NewDate(expiry)
	return &b, nil
}

// InventorySnapshot returns every live medicine paired with its unexpired batches.
// This powers the frontend IndexedDB cache; it must stay a single round-trip query.
func (r *MedicineRepo) InventorySnapshot(ctx context.Context, storeID string) ([]models.MedicineWithBatches, error) {
	if err := requireStoreID(storeID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT m.id, m.name, m.salt_composition, m.manufacturer, m.min_reorder_level, m.packing, m.uqc,
		       m.created_at, m.updated_at,
		       b.id, b.batch_number, b.expiry_date,
		       b.purchase_price::float8, b.sale_price::float8, b.current_stock,
		       b.created_at, b.updated_at
		FROM medicines m
		LEFT JOIN batches b
		       ON b.medicine_id = m.id AND b.expiry_date >= CURRENT_DATE AND b.current_stock > 0
		WHERE m.deleted_at IS NULL AND m.store_id = $1
		ORDER BY m.name ASC, b.expiry_date ASC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[string]*models.MedicineWithBatches)
	order := make([]string, 0)

	for rows.Next() {
		var (
			m        models.MedicineWithBatches
			batchID  *string
			bNumber  *string
			expiry   *time.Time
			purchase *float64
			sale     *float64
			stock    *int
			bCreated *time.Time
			bUpdated *time.Time
		)
		if err := rows.Scan(&m.ID, &m.Name, &m.SaltComposition, &m.Manufacturer,
			&m.MinReorderLevel, &m.Packing, &m.UQC, &m.CreatedAt, &m.UpdatedAt,
			&batchID, &bNumber, &expiry, &purchase, &sale, &stock, &bCreated, &bUpdated); err != nil {
			return nil, err
		}
		existing, ok := byID[m.ID]
		if !ok {
			mc := m
			mc.Batches = make([]models.Batch, 0)
			byID[m.ID] = &mc
			order = append(order, m.ID)
			existing = &mc
		}
		if batchID != nil {
			existing.Batches = append(existing.Batches, models.Batch{
				ID:            *batchID,
				MedicineID:    existing.ID,
				BatchNumber:   derefString(bNumber),
				ExpiryDate:    models.NewDate(derefTime(expiry)),
				PurchasePrice: derefFloat(purchase),
				SalePrice:     derefFloat(sale),
				CurrentStock:  derefInt(stock),
				CreatedAt:     derefTime(bCreated),
				UpdatedAt:     derefTime(bUpdated),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]models.MedicineWithBatches, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// GetDetail returns a full medicine profile with all batches (including expired)
// and aggregated sales/purchase statistics. This powers the medicine catalog page.
func (r *MedicineRepo) GetDetail(ctx context.Context, storeID, id string) (*models.MedicineDetail, error) {
	if err := requireStoreID(storeID); err != nil {
		return nil, err
	}
	m, err := r.GetByID(ctx, storeID, id)
	if err != nil {
		return nil, err
	}

	sid := storeID

	detail := &models.MedicineDetail{Medicine: *m}

	// Load ALL batches for this medicine (current and expired)
	batchRows, err := r.db.Query(ctx, `
		SELECT id::text, medicine_id::text, batch_number, expiry_date,
		       purchase_price::float8, sale_price::float8, current_stock,
		       created_at, updated_at
		FROM batches
		WHERE medicine_id = $1 AND store_id = $2
		ORDER BY expiry_date DESC`, id, sid)
	if err != nil {
		return nil, err
	}
	defer batchRows.Close()

	detail.Batches = make([]models.BatchDetail, 0)
	for batchRows.Next() {
		var b models.Batch
		var expiry time.Time
		if err := batchRows.Scan(&b.ID, &b.MedicineID, &b.BatchNumber, &expiry,
			&b.PurchasePrice, &b.SalePrice, &b.CurrentStock,
			&b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.ExpiryDate = models.NewDate(expiry)
		expired := expiry.Before(time.Now().UTC())
		detail.Batches = append(detail.Batches, models.BatchDetail{Batch: b, Expired: expired})
		if !expired {
			detail.TotalStock += b.CurrentStock
		}
	}
	if err := batchRows.Err(); err != nil {
		return nil, err
	}

	// Aggregated sales stats
	err = r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(sii.quantity), 0)::int,
		       COALESCE(SUM(sii.subtotal), 0)::float8,
		       COUNT(DISTINCT si.id)::int
		FROM sales_invoice_items sii
		JOIN sales_invoices si ON si.id = sii.invoice_id
		WHERE sii.medicine_id = $1 AND si.store_id = $2`, id, sid).Scan(
		&detail.SalesStats.UnitsSold,
		&detail.SalesStats.TotalRevenue,
		&detail.SalesStats.Invoices)
	if err != nil {
		return nil, err
	}

	// Aggregated purchase stats
	err = r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(poi.quantity + poi.bonus_quantity), 0)::int,
		       COALESCE(SUM(poi.quantity * poi.purchase_price - poi.discount_amount), 0)::float8,
		       COUNT(DISTINCT po.id)::int
		FROM purchase_order_items poi
		JOIN purchase_orders po ON po.id = poi.purchase_id
		WHERE poi.medicine_id = $1 AND po.store_id = $2`, id, sid).Scan(
		&detail.PurchaseStats.UnitsPurchased,
		&detail.PurchaseStats.TotalSpend,
		&detail.PurchaseStats.Orders)
	if err != nil {
		return nil, err
	}

	// Recent sales (last 20)
	saleRows, err := r.db.Query(ctx, `
		SELECT sii.invoice_id::text, si.invoice_no, sii.quantity,
		       sii.unit_sale_price::float8, sii.subtotal::float8,
		       (si.created_at AT TIME ZONE 'UTC')::date::text,
		       COALESCE(c.name, '')
		FROM sales_invoice_items sii
		JOIN sales_invoices si ON si.id = sii.invoice_id
		LEFT JOIN customers c ON c.id = si.customer_id AND c.store_id = si.store_id
		WHERE sii.medicine_id = $1 AND si.store_id = $2
		ORDER BY si.created_at DESC
		LIMIT 20`, id, sid)
	if err != nil {
		return nil, err
	}
	defer saleRows.Close()

	detail.RecentSales = make([]models.RecentSale, 0)
	for saleRows.Next() {
		var s models.RecentSale
		var createdStr string
		if err := saleRows.Scan(&s.InvoiceID, &s.InvoiceNo, &s.Quantity,
			&s.UnitPrice, &s.Subtotal, &createdStr, &s.CustomerName); err != nil {
			return nil, err
		}
		if t, err := time.Parse("2006-01-02", createdStr); err == nil {
			s.CreatedAt = models.NewDate(t)
		}
		detail.RecentSales = append(detail.RecentSales, s)
	}
	if err := saleRows.Err(); err != nil {
		return nil, err
	}

	// Recent purchases (last 20)
	purchaseRows, err := r.db.Query(ctx, `
		SELECT poi.purchase_id::text, po.invoice_no, po.supplier_name,
		       poi.quantity, poi.bonus_quantity, poi.purchase_price::float8,
		       (po.created_at AT TIME ZONE 'UTC')::date::text
		FROM purchase_order_items poi
		JOIN purchase_orders po ON po.id = poi.purchase_id
		WHERE poi.medicine_id = $1 AND po.store_id = $2
		ORDER BY po.created_at DESC
		LIMIT 20`, id, sid)
	if err != nil {
		return nil, err
	}
	defer purchaseRows.Close()

	detail.RecentPurchases = make([]models.RecentPurchase, 0)
	for purchaseRows.Next() {
		var p models.RecentPurchase
		var createdStr string
		if err := purchaseRows.Scan(&p.PurchaseID, &p.InvoiceNo, &p.SupplierName,
			&p.Quantity, &p.BonusQty, &p.PurchasePrice, &createdStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse("2006-01-02", createdStr); err == nil {
			p.CreatedAt = models.NewDate(t)
		}
		detail.RecentPurchases = append(detail.RecentPurchases, p)
	}
	if err := purchaseRows.Err(); err != nil {
		return nil, err
	}

	// Load tax configuration for this medicine, scoped to the medicine's store.
	taxRepo := NewTaxRepo(r.db)
	taxCfg, err := taxRepo.GetMedicineTaxConfigByMedicine(ctx, sid, id)
	if err != nil {
		return nil, err
	}
	detail.TaxConfig = taxCfg

	return detail, nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefTime(p *time.Time) time.Time {
	if p == nil {
		return time.Time{}
	}
	return *p
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
