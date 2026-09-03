package models

import (
	"errors"
	"fmt"
	"time"
)

// ---- GST / Business entities ----

type Supplier struct {
	ID        string    `json:"id"`
	LegalName string    `json:"legal_name"`
	TradeName string    `json:"trade_name"`
	GSTIN     *string   `json:"gstin"`
	PAN       *string   `json:"pan"`
	Address   string    `json:"address"`
	State     string    `json:"state"`
	StateCode string    `json:"state_code"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Business struct {
	ID        string    `json:"id"`
	LegalName string    `json:"legal_name"`
	TradeName string    `json:"trade_name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GSTRegistration struct {
	ID         string    `json:"id"`
	BusinessID string    `json:"business_id"`
	GSTIN      *string   `json:"gstin"`
	LegalName  string    `json:"legal_name"`
	TradeName  string    `json:"trade_name"`
	PAN        *string   `json:"pan"`
	StateCode  string    `json:"state_code"`
	StateName  string    `json:"state_name"`
	Address    string    `json:"address"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Store struct {
	ID                    string     `json:"id"`
	GSTRegistrationID     *string    `json:"gst_registration_id"`
	Name                  string     `json:"name"`
	Address               string     `json:"address"`
	Phone                 string     `json:"phone"`
	DrugLicenseNumber     string     `json:"drug_license_number"`
	DrugLicenseExpiry     *Date      `json:"drug_license_expiry"`
	IsActive              bool       `json:"is_active"`
	MaxEmployees          int        `json:"max_employees"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	SubscriptionValidUntil *time.Time `json:"subscription_valid_until"`
	SubscriptionStatus    string     `json:"subscription_status"`

	// OwnerName/GSTIN/PAN/StateCode are joined read-only fields surfaced by the
	// shop details endpoint; they are not columns on stores and are never
	// written here. StateCode is the GST registration's state (derived from the
	// GSTIN) and is authoritative for intra/inter-state billing.
	OwnerName string  `json:"owner_name,omitempty"`
	GSTIN     *string `json:"gstin,omitempty"`
	PAN       *string `json:"pan,omitempty"`
	StateCode *string `json:"state_code,omitempty"`
}

type HSNCode struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// HSNWithRate is an HSN code bundled with its currently-active tax rate. It is
// the shape consumed by the HSN sync snapshot so the frontend can build the HSN
// dropdown and auto-fill tax fields from one cache read.
type HSNWithRate struct {
	HSNCode
	GSTRate  float64 `json:"gst_rate"`
	CGSTRate float64 `json:"cgst_rate"`
	SGSTRate float64 `json:"sgst_rate"`
	IGSTRate float64 `json:"igst_rate"`
	CessRate float64 `json:"cess_rate"`
}

type TaxRate struct {
	ID            string     `json:"id"`
	HSNCodeID     string     `json:"hsn_code_id"`
	GSTRate       float64    `json:"gst_rate"`
	CGSTRate      float64    `json:"cgst_rate"`
	SGSTRate      float64    `json:"sgst_rate"`
	IGSTRate      float64    `json:"igst_rate"`
	CessRate      float64    `json:"cess_rate"`
	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to"`
	CreatedAt     time.Time  `json:"created_at"`
}

type MedicineTaxConfig struct {
	ID               string     `json:"id"`
	MedicineID       string     `json:"medicine_id"`
	HSNCodeID        string     `json:"hsn_code_id"`
	TaxRateID        string     `json:"tax_rate_id"`
	PriceIncludesTax bool       `json:"price_includes_tax"`
	EffectiveFrom    time.Time  `json:"effective_from"`
	EffectiveTo      *time.Time `json:"effective_to"`
	CreatedAt        time.Time  `json:"created_at"`

	// Joined fields (populated on read, not stored)
	HSNCode string   `json:"hsn_code,omitempty"`
	TaxRate *TaxRate `json:"tax_rate,omitempty"`
}

// GSTR2BImportBatch is a single uploaded GSTR-2B file (GSTN's view of the
// pharmacy's supplier invoices for one tax period) and its reconciliation state.
type GSTR2BImportBatch struct {
	ID             string    `json:"id"`
	StoreID        *string   `json:"store_id"`
	GSTIN          string    `json:"gstin"`
	Period         string    `json:"period"`
	FileName       string    `json:"file_name"`
	DocCount       int       `json:"doc_count"`
	MatchedCount   int       `json:"matched_count"`
	UnmatchedCount int       `json:"unmatched_count"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// GSTR2BImport is one document in an imported GSTR-2B batch, as reported by the
// supplier, plus the result of reconciling it against purchase_orders.
type GSTR2BImport struct {
	ID                string    `json:"id"`
	ImportBatchID     string    `json:"import_batch_id"`
	StoreID           *string   `json:"store_id"`
	SupplierGSTIN     string    `json:"supplier_gstin"`
	DocType           string    `json:"doc_type"`
	Period            string    `json:"period"`
	InvoiceNo         string    `json:"invoice_no"`
	InvoiceDate       Date      `json:"invoice_date"`
	TaxableValue      float64   `json:"taxable_value"`
	IGSTAmount        float64   `json:"igst_amount"`
	CGSTAmount        float64   `json:"cgst_amount"`
	SGSTAmount        float64   `json:"sgst_amount"`
	CessAmount        float64   `json:"cess_amount"`
	TotalValue        float64   `json:"total_value"`
	MatchStatus       string    `json:"match_status"`
	MatchedPurchaseID *string   `json:"matched_purchase_id"`
	MatchedDifference *float64  `json:"matched_difference"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
}

// GSTR2BReconciliation is the aggregate outcome of matching one batch.
type GSTR2BReconciliation struct {
	BatchID          string  `json:"batch_id"`
	Period           string  `json:"period"`
	GSTIN            string  `json:"gstin"`
	TotalDocs        int     `json:"total_docs"`
	Matched          int     `json:"matched"`
	Unmatched        int     `json:"unmatched"`
	AmountMismatch   int     `json:"amount_mismatch"`
	MatchedTaxable   float64 `json:"matched_taxable_value"`
	UnmatchedTaxable float64 `json:"unmatched_taxable_value"`
}

// GSTR2BImportInput is the ingestion payload for a GSTR-2B import.
// It accepts either the compact docdata format seen in GSTN downloads or a
// flat list; both are normalised into documents.
type GSTR2BImportInput struct {
	StoreID *string          `json:"store_id"`
	Period  string           `json:"period"`
	GSTIN   string           `json:"gstin"`
	Source  string           `json:"source,omitempty"`
	Docs    []GSTR2BDocInput `json:"docs"`
}

// GSTR2BDocInput is a single GSTR-2B document from the import payload.
type GSTR2BDocInput struct {
	SupplierGSTIN string  `json:"supplier_gstin"`
	DocType       string  `json:"doc_type"` // INV / CRN / DBN
	InvoiceNo     string  `json:"invoice_no"`
	InvoiceDate   string  `json:"invoice_date"` // YYYY-MM-DD
	TaxableValue  float64 `json:"taxable_value"`
	IGST          float64 `json:"igst"`
	CGST          float64 `json:"cgst"`
	SGST          float64 `json:"sgst"`
	Cess          float64 `json:"cess"`
	TotalValue    float64 `json:"total_value"`
}

// ---- Core Entities ----

type Medicine struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	SaltComposition string    `json:"salt_composition"`
	Manufacturer    string    `json:"manufacturer"`
	MinReorderLevel int       `json:"min_reorder_level"`
	Packing         string    `json:"packing"`
	UQC             string    `json:"uqc"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Batch is the Marg-core inventory unit: a distinct physical batch of a medicine.
type Batch struct {
	ID            string    `json:"id"`
	MedicineID    string    `json:"medicine_id"`
	BatchNumber   string    `json:"batch_number"`
	ExpiryDate    Date      `json:"expiry_date"`
	PurchasePrice float64   `json:"purchase_price"`
	SalePrice     float64   `json:"sale_price"`
	CurrentStock  int       `json:"current_stock"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// MedicineWithBatches is the unified sync-payload object consumed by the frontend cache.
type MedicineWithBatches struct {
	Medicine
	Batches []Batch `json:"batches"`
}

type Customer struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Phone           string    `json:"phone"`
	CreditLimit     float64   `json:"credit_limit"`
	CurrentBalance  float64   `json:"current_balance"`
	GSTIN           *string   `json:"gstin"`
	CustomerType    string    `json:"customer_type"`
	BillingAddress  *string   `json:"billing_address"`
	ShippingAddress *string   `json:"shipping_address"`
	State           *string   `json:"state"`
	StateCode       *string   `json:"state_code"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PaymentType string

const (
	PaymentCash   PaymentType = "CASH"
	PaymentCredit PaymentType = "CREDIT"
)

func (p PaymentType) Valid() bool {
	return p == PaymentCash || p == PaymentCredit
}

type SalesInvoice struct {
	ID            string      `json:"id"`
	InvoiceNo     string      `json:"invoice_no"`
	CustomerID    *string     `json:"customer_id"`
	PaymentType   PaymentType `json:"payment_type"`
	TotalAmount   float64     `json:"total_amount"`
	DiscountTotal float64     `json:"discount_total"`
	InvoiceDate   Date        `json:"invoice_date"`
	FinancialYear string      `json:"financial_year"`
	CreatedAt     time.Time   `json:"created_at"`

	// B2B fields
	SaleType     string  `json:"sale_type"`
	BuyerName    *string `json:"buyer_name"`
	BuyerGSTIN   *string `json:"buyer_gstin"`
	BuyerAddress *string `json:"buyer_address"`

	// GST fields (nullable for pre-GST historical records)
	StoreID           *string  `json:"store_id"`
	GSTRegistrationID *string  `json:"gst_registration_id"`
	CustomerGSTIN     *string  `json:"customer_gstin"`
	CustomerStateCode *string  `json:"customer_state_code"`
	SupplyType        *string  `json:"supply_type"`
	GrossAmount       *float64 `json:"gross_amount"`
	TaxableAmount     *float64 `json:"taxable_amount"`
	CGSTTotal         *float64 `json:"cgst_total"`
	SGSTTotal         *float64 `json:"sgst_total"`
	IGSTTotal         *float64 `json:"igst_total"`
	CessTotal         *float64 `json:"cess_total"`
	TaxTotal          *float64 `json:"tax_total"`
	RoundOff          *float64 `json:"round_off"`
	GrandTotal        *float64 `json:"grand_total"`
	PriceIncludesTax  *bool    `json:"price_includes_tax"`
}

type SalesInvoiceItem struct {
	ID             string  `json:"id"`
	InvoiceID      string  `json:"invoice_id"`
	MedicineID     string  `json:"medicine_id"`
	BatchID        string  `json:"batch_id"`
	Quantity       int     `json:"quantity"`
	UnitSalePrice  float64 `json:"unit_sale_price"`
	Subtotal       float64 `json:"subtotal"`
	DiscountType   string  `json:"discount_type"`
	DiscountValue  float64 `json:"discount_value"`
	DiscountAmount float64 `json:"discount_amount"`

	// B2B fields
	MRP           *float64 `json:"mrp,omitempty"`
	BonusQuantity int      `json:"bonus_quantity"`

	// GST tax snapshot fields (nullable for pre-GST records)
	HSNCode      *string  `json:"hsn_code"`
	UQC          string   `json:"uqc"`
	GrossAmount  *float64 `json:"gross_amount"`
	TaxableValue *float64 `json:"taxable_value"`
	GSTRate      *float64 `json:"gst_rate"`
	CGSTRate     *float64 `json:"cgst_rate"`
	CGSTAmount   *float64 `json:"cgst_amount"`
	SGSTRate     *float64 `json:"sgst_rate"`
	SGSTAmount   *float64 `json:"sgst_amount"`
	IGSTRate     *float64 `json:"igst_rate"`
	IGSTAmount   *float64 `json:"igst_amount"`
	CessRate     *float64 `json:"cess_rate"`
	CessAmount   *float64 `json:"cess_amount"`
	LineTotal    *float64 `json:"line_total"`
}

type PurchaseOrder struct {
	ID            string    `json:"id"`
	InvoiceNo     string    `json:"invoice_no"`
	SupplierName  string    `json:"supplier_name"`
	TotalAmount   float64   `json:"total_amount"`
	DiscountTotal float64   `json:"discount_total"`
	InvoiceDate   Date      `json:"invoice_date"`
	FinancialYear string    `json:"financial_year"`
	CreatedAt     time.Time `json:"created_at"`

	// GST fields (nullable for pre-GST historical records)
	SupplierID        *string  `json:"supplier_id"`
	SupplierGSTIN     *string  `json:"supplier_gstin"`
	SupplierStateCode *string  `json:"supplier_state_code"`
	StoreID           *string  `json:"store_id"`
	GSTRegistrationID *string  `json:"gst_registration_id"`
	SupplyType        *string  `json:"supply_type"`
	PlaceOfSupply     *string  `json:"place_of_supply"`
	ReverseCharge     bool     `json:"reverse_charge"`
	ITCEligible       bool     `json:"itc_eligible"`
	ITCAmount         *float64 `json:"itc_amount"`
	GrossAmount       *float64 `json:"gross_amount"`
	TaxableAmount     *float64 `json:"taxable_amount"`
	CGSTTotal         *float64 `json:"cgst_total"`
	SGSTTotal         *float64 `json:"sgst_total"`
	IGSTTotal         *float64 `json:"igst_total"`
	CessTotal         *float64 `json:"cess_total"`
	TaxTotal          *float64 `json:"tax_total"`
	GrandTotal        *float64 `json:"grand_total"`
	PriceIncludesTax  *bool    `json:"price_includes_tax"`
}

type PurchaseOrderItem struct {
	ID             string  `json:"id"`
	PurchaseID     string  `json:"purchase_id"`
	MedicineID     string  `json:"medicine_id"`
	BatchNumber    string  `json:"batch_number"`
	ExpiryDate     Date    `json:"expiry_date"`
	Quantity       int     `json:"quantity"`
	BonusQuantity  int     `json:"bonus_quantity"`
	PurchasePrice  float64 `json:"purchase_price"`
	SalePrice      float64 `json:"sale_price"`
	DiscountType   string  `json:"discount_type"`
	DiscountValue  float64 `json:"discount_value"`
	DiscountAmount float64 `json:"discount_amount"`

	MedicineName string `json:"medicine_name,omitempty"`

	// GST tax snapshot fields (nullable for pre-GST records)
	HSNCode      *string  `json:"hsn_code"`
	UQC          string   `json:"uqc"`
	GrossAmount  *float64 `json:"gross_amount"`
	TaxableValue *float64 `json:"taxable_value"`
	GSTRate      *float64 `json:"gst_rate"`
	CGSTRate     *float64 `json:"cgst_rate"`
	CGSTAmount   *float64 `json:"cgst_amount"`
	SGSTRate     *float64 `json:"sgst_rate"`
	SGSTAmount   *float64 `json:"sgst_amount"`
	IGSTRate     *float64 `json:"igst_rate"`
	IGSTAmount   *float64 `json:"igst_amount"`
	CessRate     *float64 `json:"cess_rate"`
	CessAmount   *float64 `json:"cess_amount"`
	LineTotal    *float64 `json:"line_total"`
}

type ReconciliationJournal struct {
	ID               string    `json:"id"`
	VerifiedByUserID *string   `json:"verified_by_user_id"`
	Notes            string    `json:"notes"`
	CreatedAt        time.Time `json:"created_at"`
	ItemCount        int       `json:"item_count,omitempty"`
}

type CustomerLedgerEntry struct {
	ID           string    `json:"id"`
	CustomerID   string    `json:"customer_id"`
	EntryType    string    `json:"entry_type"`
	Amount       float64   `json:"amount"`
	BalanceAfter float64   `json:"balance_after"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
}

type SalesCreditNote struct {
	ID                  string    `json:"id"`
	InvoiceID           string    `json:"invoice_id"`
	NoteNo              string    `json:"note_no"`
	NoteDate            Date      `json:"note_date"`
	Reason              string    `json:"reason"`
	OriginalInvoiceNo   *string   `json:"original_invoice_no"`
	OriginalInvoiceDate *Date     `json:"original_invoice_date"`
	StoreID             *string   `json:"store_id"`
	FinancialYear       string    `json:"financial_year"`
	CustomerGSTIN       *string   `json:"customer_gstin"`
	SupplyType          *string   `json:"supply_type"`
	GrossAmount         float64   `json:"gross_amount"`
	TaxableAmount       float64   `json:"taxable_amount"`
	CGSTTotal           float64   `json:"cgst_total"`
	SGSTTotal           float64   `json:"sgst_total"`
	IGSTTotal           float64   `json:"igst_total"`
	CessTotal           float64   `json:"cess_total"`
	TaxTotal            float64   `json:"tax_total"`
	GrandTotal          float64   `json:"grand_total"`
	CreatedAt           time.Time `json:"created_at"`
}

type SalesCreditNoteItem struct {
	ID            string  `json:"id"`
	CreditNoteID  string  `json:"credit_note_id"`
	InvoiceItemID *string `json:"invoice_item_id"`
	MedicineID    string  `json:"medicine_id"`
	BatchID       string  `json:"batch_id"`
	Quantity      int     `json:"quantity"`
	// BonusQuantity tracks the free units being returned alongside Quantity so
	// inventory restock restores the FULL physical quantity (billed + bonus).
	BonusQuantity int     `json:"bonus_quantity"`
	HSNCode       *string `json:"hsn_code"`
	TaxableValue  float64 `json:"taxable_value"`
	GSTRate       float64 `json:"gst_rate"`
	CGSTAmount    float64 `json:"cgst_amount"`
	SGSTAmount    float64 `json:"sgst_amount"`
	IGSTAmount    float64 `json:"igst_amount"`
	CessAmount    float64 `json:"cess_amount"`
	LineTotal     float64 `json:"line_total"`
}

type ReconciliationItem struct {
	ID               string  `json:"id"`
	JournalID        string  `json:"journal_id"`
	MedicineID       string  `json:"medicine_id"`
	BatchID          string  `json:"batch_id"`
	SystemStock      int     `json:"system_stock"`
	PhysicalStock    int     `json:"physical_stock"`
	VarianceQuantity int     `json:"variance_quantity"`
	CostImpact       float64 `json:"cost_impact"`

	BatchNumber  string `json:"batch_number,omitempty"`
	MedicineName string `json:"medicine_name,omitempty"`
}

// ---- Auth / Membership ----

type User struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Phone           string    `json:"phone"`
	IsActive        bool      `json:"is_active"`
	IsPlatformAdmin bool      `json:"is_platform_admin"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// StoreSubscriptionPayment is one offline cash receipt in the subscription
// ledger. Recording a payment extends the store's validity window.
type StoreSubscriptionPayment struct {
	ID         string    `json:"id"`
	StoreID    string    `json:"store_id"`
	PlanType   string    `json:"plan_type"` // '1_MONTH' | '6_MONTHS' | '1_YEAR'
	Amount     float64   `json:"amount"`
	ValidFrom  time.Time `json:"valid_from"`
	ValidUntil time.Time `json:"valid_until"`
	Notes      string    `json:"notes,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// PlatformStoreInfo is the per-store row served to the platform admin: store
// identity + owner contact + subscription metrics with computed days remaining.
// DaysRemaining is nil when the store has no validity window (grace).
type PlatformStoreInfo struct {
	StoreID                string     `json:"store_id"`
	StoreName              string     `json:"store_name"`
	StoreAddress           string     `json:"store_address"`
	StorePhone             string     `json:"store_phone"`
	IsActive               bool       `json:"is_active"`
	OwnerName              string     `json:"owner_name,omitempty"`
	OwnerPhone             string     `json:"owner_phone,omitempty"`
	SubscriptionValidUntil *time.Time `json:"subscription_valid_until"`
	SubscriptionStatus     string     `json:"subscription_status"`
	DaysRemaining          *int       `json:"days_remaining"`
	CreatedAt              time.Time  `json:"created_at"`
}

// Subscription plan catalogue for offline cash collection.
const (
	Plan1Month  = "1_MONTH"
	Plan6Months = "6_MONTHS"
	Plan1Year   = "1_YEAR"
)

// PlanDays maps a plan type to its validity extension in days.
func PlanDays(planType string) (int, bool) {
	switch planType {
	case Plan1Month:
		return 30, true
	case Plan6Months:
		return 180, true
	case Plan1Year:
		return 365, true
	default:
		return 0, false
	}
}

// PlanAmount maps a plan type to its offline cash price in INR.
func PlanAmount(planType string) (float64, bool) {
	switch planType {
	case Plan1Month:
		return 250.00, true
	case Plan6Months:
		return 1350.00, true
	case Plan1Year:
		return 2500.00, true
	default:
		return 0, false
	}
}

// DaysRemainingUntil computes whole days from now until validUntil (negative
// when expired). Returns nil when validUntil is nil (no window = grace).
func DaysRemainingUntil(validUntil *time.Time, now time.Time) *int {
	if validUntil == nil {
		return nil
	}
	d := int(validUntil.Sub(now).Hours() / 24)
	return &d
}

// IsSubscriptionActive reports whether a store subscription permits logins:
// status must be ACTIVE and the validity window (when set) must not be past.
// A nil validUntil means grace (no expiry enforced yet) so bootstrap, legacy
// rows and test seeds never lock out.
func IsSubscriptionActive(status string, validUntil *time.Time, now time.Time) bool {
	if status != "" && status != "ACTIVE" {
		return false
	}
	if validUntil != nil && validUntil.Before(now) {
		return false
	}
	return true
}

// Membership links a user to a store with a role and an active flag.
// is_active=false means the employee is deactivated (their sessions are dead)
// but the row is never physically deleted, preserving the audit trail.
type Membership struct {
	ID        string    `json:"id"`
	StoreID   string    `json:"store_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined fields (populated on list reads, not stored)
	UserName   string `json:"user_name,omitempty"`
	UserPhone  string `json:"user_phone,omitempty"`
	UserActive bool   `json:"user_active,omitempty"`
}

// PurchaseRequest is an employee-submitted purchase application awaiting owner
// approval. The full PurchaseInput payload is snapshotted; nothing mutates
// inventory until approval.
type PurchaseRequest struct {
	ID               string    `json:"id"`
	StoreID          string    `json:"store_id"`
	RequestedBy      string    `json:"requested_by"`
	Status           string    `json:"status"` // PENDING / APPROVED / REJECTED / CANCELLED
	PurchaseSnapshot []byte    `json:"purchase_snapshot,omitempty"`
	PurchaseID       *string   `json:"purchase_id"`
	ReviewedBy       *string   `json:"reviewed_by"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
	RejectionReason  string    `json:"rejection_reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Joined fields
	RequesterName string `json:"requester_name,omitempty"`
	ReviewerName  string `json:"reviewer_name,omitempty"`
}

// StockAuditRequest is an employee-submitted physical-count audit awaiting
// owner approval. Each item snapshots the system quantity at submission so a
// stale audit (stock moved since) can be rejected rather than overwrite stock.
type StockAuditRequest struct {
	ID              string    `json:"id"`
	StoreID         string    `json:"store_id"`
	RequestedBy     string    `json:"requested_by"`
	Status          string    `json:"status"` // PENDING / APPROVED / REJECTED / CANCELLED
	Notes           string    `json:"notes"`
	JournalID       *string   `json:"journal_id"`
	ReviewedBy      *string   `json:"reviewed_by"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	RequesterName string `json:"requester_name,omitempty"`
	ReviewerName  string `json:"reviewer_name,omitempty"`
}

// StockAuditRequestItem is one counted batch within a stock audit request.
type StockAuditRequestItem struct {
	ID               string `json:"id"`
	RequestID        string `json:"request_id"`
	MedicineID       string `json:"medicine_id"`
	MedicineName     string `json:"medicine_name,omitempty"`
	BatchID          string `json:"batch_id"`
	BatchNumber      string `json:"batch_number"`
	SystemQuantity   int    `json:"system_quantity"`
	PhysicalQuantity int    `json:"physical_quantity"`
	Reason           string `json:"reason"`
}

// AuditLog is an immutable tracerow for sensitive actions (approvals, employee
// changes, settings edits).
type AuditLog struct {
	ID       string  `json:"id"`
	StoreID  *string `json:"store_id"`
	UserID   *string `json:"user_id"`
	Action   string  `json:"action"`
	Entity   string  `json:"entity"`
	EntityID string  `json:"entity_id"`
	Details  string  `json:"details,omitempty"`
}

// ---- Medicine detail (catalog / medicine profile page) ----

type BatchDetail struct {
	Batch
	Expired bool `json:"expired"`
}

type MedicineSalesStats struct {
	UnitsSold    int     `json:"units_sold"`
	TotalRevenue float64 `json:"total_revenue"`
	Invoices     int     `json:"invoices"`
}

type MedicinePurchaseStats struct {
	UnitsPurchased int     `json:"units_purchased"`
	TotalSpend     float64 `json:"total_spend"`
	Orders         int     `json:"orders"`
}

type RecentSale struct {
	InvoiceID    string  `json:"invoice_id"`
	InvoiceNo    string  `json:"invoice_no"`
	Quantity     int     `json:"quantity"`
	UnitPrice    float64 `json:"unit_sale_price"`
	Subtotal     float64 `json:"subtotal"`
	CreatedAt    Date    `json:"created_at"`
	CustomerName string  `json:"customer_name"`
}

type RecentPurchase struct {
	PurchaseID    string  `json:"purchase_id"`
	InvoiceNo     string  `json:"invoice_no"`
	SupplierName  string  `json:"supplier_name"`
	Quantity      int     `json:"quantity"`
	BonusQty      int     `json:"bonus_quantity"`
	PurchasePrice float64 `json:"purchase_price"`
	CreatedAt     Date    `json:"created_at"`
}

type MedicineDetail struct {
	Medicine
	Batches         []BatchDetail         `json:"batches"`
	TotalStock      int                   `json:"total_stock"`
	SalesStats      MedicineSalesStats    `json:"sales_stats"`
	PurchaseStats   MedicinePurchaseStats `json:"purchase_stats"`
	RecentSales     []RecentSale          `json:"recent_sales"`
	RecentPurchases []RecentPurchase      `json:"recent_purchases"`
	TaxConfig       *MedicineTaxConfig    `json:"tax_config,omitempty"`
}

// ---- Domain error sentinels mapped to HTTP status codes by the handler layer ----

var ErrNotFound = errors.New("record not found")

// ErrDuplicate signals a uniqueness/conflict violation (e.g. an HSN code that
// already exists for the current store). The handler layer surfaces it as 409.
var ErrDuplicate = errors.New("duplicate record")

// ErrOverpayment signals a payment attempt larger than the outstanding balance.
var ErrOverpayment = errors.New("payment exceeds outstanding balance")

// ValidationError signals a client-caused business-rule violation that the
// handler layer must surface as 400 Bad Request rather than 500.
type ValidationError struct{ msg string }

func NewValidationError(msg string) error { return &ValidationError{msg: msg} }

func (e *ValidationError) Error() string { return e.msg }

type InsufficientStockError struct {
	BatchID        string
	RequestedQty   int
	AvailableStock int
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for batch %s: requested %d, available %d",
		e.BatchID, e.RequestedQty, e.AvailableStock)
}

type CreditLimitExceededError struct {
	CustomerID   string
	CustomerName string
	Outstanding  float64
	InvoiceTotal float64
	CreditLimit  float64
}

func (e *CreditLimitExceededError) Error() string {
	return fmt.Sprintf(
		"credit limit exceeded for customer %s (%s): outstanding %.2f + invoice %.2f exceeds limit %.2f",
		e.CustomerName, e.CustomerID, e.Outstanding, e.InvoiceTotal, e.CreditLimit)
}

// ---- Approval workflow sentinels ----

var (
	// ErrEmployeeLimitReached is returned when adding an employee would exceed
	// the store's max_employees.
	ErrEmployeeLimitReached = errors.New("employee limit reached")
	// ErrRequestNotPending is returned when an approval/rejection/cancel targets
	// a request that already left the PENDING state.
	ErrRequestNotPending = errors.New("request is no longer pending")
	// ErrRequesterApprover is returned when a user tries to approve their own request.
	ErrRequesterApprover = errors.New("you cannot review your own request")
	// ErrStaleStock is returned when a stock audit no longer matches live stock.
	ErrStaleStock = errors.New("stock has changed since this audit was submitted; please re-validate")
	// ErrCannotDisableOwner guards against deactivating the store owner.
	ErrCannotDisableOwner = errors.New("cannot deactivate the store owner")
	// ErrNotAMember guards against operations on users who do not belong to the store.
	ErrNotAMember = errors.New("user does not belong to this store")
)
