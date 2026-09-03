package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohi/pms-marg-inspired/internal/models"
	"github.com/mohi/pms-marg-inspired/internal/tax"
)

type CustomerRepo struct {
	db    *pgxpool.Pool
	store *storeIDRef
}

func NewCustomerRepo(db *pgxpool.Pool, storeID string) *CustomerRepo {
	return &CustomerRepo{db: db, store: newStoreIDRef(db, storeID)}
}

func (r *CustomerRepo) storeID(ctx context.Context) (string, error) {
	return r.store.get(ctx)
}

const customerColumns = `id, name, phone, credit_limit::float8, current_balance::float8,
	gstin, customer_type, billing_address, shipping_address, state, state_code,
	created_at, updated_at`

var phonePattern = regexp.MustCompile(`^[0-9+\-\s]{5,20}$`)

var stateCodePattern = regexp.MustCompile(`^[0-9]{2}$`)

func ValidateCustomer(c *models.Customer) error {
	if c.Name == "" {
		return errors.New("customer name is required")
	}
	if !phonePattern.MatchString(c.Phone) {
		return errors.New("phone must be 5-20 characters of digits/spaces/+/-")
	}
	if c.CreditLimit < 0 {
		return errors.New("credit_limit must be >= 0")
	}
	if c.GSTIN != nil && *c.GSTIN != "" && !tax.ValidateGSTIN(*c.GSTIN) {
		return errors.New("invalid GSTIN")
	}
	if c.StateCode != nil && *c.StateCode != "" && !stateCodePattern.MatchString(*c.StateCode) {
		return errors.New("invalid state_code")
	}
	if c.CustomerType == "" {
		c.CustomerType = "B2C"
	}
	return nil
}

type PaymentInput struct {
	Amount float64 `json:"amount"`
	Notes  string  `json:"notes"`
}

func scanCustomer(row pgx.Row) (*models.Customer, error) {
	var c models.Customer
	err := row.Scan(&c.ID, &c.Name, &c.Phone, &c.CreditLimit,
		&c.CurrentBalance, &c.GSTIN, &c.CustomerType,
		&c.BillingAddress, &c.ShippingAddress, &c.State, &c.StateCode,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CustomerRepo) Create(ctx context.Context, c *models.Customer) error {
	sid, err := r.storeID(ctx)
	if err != nil {
		return err
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO customers (name, phone, credit_limit, gstin, customer_type, billing_address, shipping_address, state, state_code, store_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+customerColumns,
		c.Name, c.Phone, c.CreditLimit, c.GSTIN,
		c.CustomerType, c.BillingAddress, c.ShippingAddress, c.State, c.StateCode, sid,
	).Scan(&c.ID, &c.Name, &c.Phone, &c.CreditLimit,
		&c.CurrentBalance, &c.GSTIN, &c.CustomerType,
		&c.BillingAddress, &c.ShippingAddress, &c.State, &c.StateCode,
		&c.CreatedAt, &c.UpdatedAt)
}

func (r *CustomerRepo) GetByID(ctx context.Context, id string) (*models.Customer, error) {
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	c, err := scanCustomer(r.db.QueryRow(ctx,
		`SELECT `+customerColumns+` FROM customers WHERE id = $1 AND store_id = $2`, id, sid))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	return c, err
}

func (r *CustomerRepo) List(ctx context.Context) ([]models.Customer, error) {
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+customerColumns+` FROM customers WHERE store_id = $1 ORDER BY name`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Customer, 0)
	for rows.Next() {
		var c models.Customer
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.CreditLimit,
			&c.CurrentBalance, &c.GSTIN, &c.CustomerType,
			&c.BillingAddress, &c.ShippingAddress, &c.State, &c.StateCode,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListFiltered returns a paginated customer subset matching an optional
// search term (name/phone/GSTIN, case-insensitive) and customer type.
func (r *CustomerRepo) ListFiltered(ctx context.Context, search, customerType string, limit int) ([]models.Customer, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + customerColumns + ` FROM customers`
	conds := make([]string, 0, 3)
	args := make([]interface{}, 0, 4)
	args = append(args, sid)
	conds = append(conds, `store_id = $1`)
	if search != "" {
		args = append(args, "%"+search+"%")
		conds = append(conds, fmt.Sprintf(`(name ILIKE $%d OR phone ILIKE $%d OR gstin ILIKE $%d)`, len(args), len(args), len(args)))
	}
	if customerType != "" {
		args = append(args, customerType)
		conds = append(conds, fmt.Sprintf(`customer_type = $%d`, len(args)))
	}
	query += ` WHERE ` + strings.Join(conds, " AND ")
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY name LIMIT $%d`, len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Customer, 0)
	for rows.Next() {
		var c models.Customer
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.CreditLimit,
			&c.CurrentBalance, &c.GSTIN, &c.CustomerType,
			&c.BillingAddress, &c.ShippingAddress, &c.State, &c.StateCode,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *CustomerRepo) Update(ctx context.Context, c *models.Customer) error {
	sid, err := r.storeID(ctx)
	if err != nil {
		return err
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE customers
		SET name = $2, phone = $3, credit_limit = $4,
		    gstin = $5, customer_type = $6, billing_address = $7, shipping_address = $8,
		    state = $9, state_code = $10, updated_at = now()
		WHERE id = $1 AND store_id = $11`,
		c.ID, c.Name, c.Phone, c.CreditLimit,
		c.GSTIN, c.CustomerType, c.BillingAddress, c.ShippingAddress, c.State, c.StateCode, sid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// RecordPayment accepts a full or part payment against the customer's
// outstanding balance. Overpayments are rejected so the ledger always
// reconciles with real credit. The balance mutation and its audit entry commit
// atomically, and the row lock serializes concurrent payments per customer.
func (r *CustomerRepo) RecordPayment(ctx context.Context, customerID string, amount float64, notes string) (*models.Customer, *models.CustomerLedgerEntry, error) {
	if amount <= 0 {
		return nil, nil, models.NewValidationError("payment amount must be positive")
	}

	var (
		cust  *models.Customer
		entry *models.CustomerLedgerEntry
	)
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, nil, err
	}
	err = pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var outstanding float64
		err := tx.QueryRow(ctx,
			`SELECT current_balance::float8 FROM customers WHERE id = $1 AND store_id = $2 FOR UPDATE`,
			customerID, sid).Scan(&outstanding)
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrNotFound
		}
		if err != nil {
			return err
		}
		if amount > outstanding+0.005 {
			return fmt.Errorf("%w: payment %.2f exceeds outstanding balance %.2f",
				models.ErrOverpayment, amount, outstanding)
		}

		var newBalance float64
		if err := tx.QueryRow(ctx,
			`UPDATE customers SET current_balance = current_balance - $2, updated_at = now()
			 WHERE id = $1 AND store_id = $3 RETURNING current_balance::float8`,
			customerID, amount, sid).Scan(&newBalance); err != nil {
			return err
		}

		entry = &models.CustomerLedgerEntry{
			CustomerID:   customerID,
			EntryType:    "PAYMENT",
			Amount:       -round2(amount),
			BalanceAfter: round2(newBalance),
			Notes:        notes,
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO customer_ledger (customer_id, entry_type, amount, balance_after, notes)
			VALUES ($1, 'PAYMENT', $2, $3, $4)
			RETURNING id::text, created_at`,
			customerID, entry.Amount, entry.BalanceAfter, notes,
		).Scan(&entry.ID, &entry.CreatedAt)
		if err != nil {
			return err
		}

		cust, err = scanCustomer(tx.QueryRow(ctx,
			`SELECT `+customerColumns+` FROM customers WHERE id = $1 AND store_id = $2`, customerID, sid))
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return cust, entry, nil
}

// Ledger returns the customer's balance history, newest first.
func (r *CustomerRepo) Ledger(ctx context.Context, customerID string, limit int) ([]models.CustomerLedgerEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	sid, err := r.storeID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT cl.id::text, cl.customer_id::text, cl.entry_type, cl.amount::float8, cl.balance_after::float8, cl.notes, cl.created_at
		FROM customer_ledger cl
		JOIN customers c ON c.id = cl.customer_id AND c.store_id = $2
		WHERE cl.customer_id = $1
		ORDER BY cl.created_at DESC, cl.id DESC
		LIMIT $3`, customerID, sid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.CustomerLedgerEntry, 0)
	for rows.Next() {
		var e models.CustomerLedgerEntry
		if err := rows.Scan(&e.ID, &e.CustomerID, &e.EntryType, &e.Amount,
			&e.BalanceAfter, &e.Notes, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
