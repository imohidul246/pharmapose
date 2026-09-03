export interface Batch {
  id: string
  medicine_id: string
  batch_number: string
  expiry_date: string
  purchase_price: number
  sale_price: number
  current_stock: number
}

export interface MedicineWithBatches {
  id: string
  name: string
  salt_composition: string
  manufacturer: string
  min_reorder_level: number
  packing: string
  uqc: string
  batches: Batch[]
}

export interface Customer {
  id: string
  name: string
  phone: string
  credit_limit: number
  current_balance: number
  // GST fields (nullable for pre-GST records)
  gstin?: string | null
  customer_type?: string | null
  billing_address?: string | null
  shipping_address?: string | null
  state?: string | null
  state_code?: string | null
}

export type LedgerEntryType = 'CREDIT_SALE' | 'PAYMENT' | 'ADJUSTMENT'

export interface LedgerEntry {
  id: string
  customer_id: string
  entry_type: LedgerEntryType
  amount: number
  balance_after: number
  notes: string
  created_at: string
}

export type PaymentType = 'CASH' | 'CREDIT'

export type DiscountType = 'percent' | 'amount'

export interface LineDiscountInput {
  type: DiscountType
  value: number
}

export interface CheckoutItemInput {
  batch_id: string
  quantity: number
  discount?: LineDiscountInput
  sell_price?: number   // B2B: custom sell price
  bonus_quantity?: number  // B2B: free items
}

export interface CheckoutRequest {
  customer_id?: string
  payment_type: PaymentType
  items: CheckoutItemInput[]
  store_id?: string
  place_of_supply?: string
  sale_type?: string        // "RETAIL" (default) or "B2B"
  buyer_name?: string       // B2B buyer info
  buyer_gstin?: string
  buyer_address?: string
}

export interface InvoiceItem {
  id: string
  invoice_id: string
  medicine_id: string
  batch_id: string
  quantity: number
  unit_sale_price: number
  subtotal: number
  discount_type: string
  discount_value: number
  discount_amount: number
  // B2B fields
  mrp?: number | null
  bonus_quantity: number
  // GST tax snapshot fields (nullable for pre-GST records)
  hsn_code?: string | null
  gross_amount?: number | null
  taxable_value?: number | null
  gst_rate?: number | null
  cgst_rate?: number | null
  cgst_amount?: number | null
  sgst_rate?: number | null
  sgst_amount?: number | null
  igst_rate?: number | null
  igst_amount?: number | null
  cess_rate?: number | null
  cess_amount?: number | null
  line_total?: number | null
}

export interface CheckoutResponse {
  invoice: {
    id: string
    invoice_no: string
    customer_id?: string | null
    payment_type: PaymentType
    total_amount: number
    discount_total: number
    invoice_date: string
    financial_year: string
    created_at: string
    // B2B fields
    sale_type?: string
    buyer_name?: string | null
    buyer_gstin?: string | null
    buyer_address?: string | null
    // GST fields
    supply_type?: string | null
    gross_amount?: number | null
    taxable_amount?: number | null
    cgst_total?: number | null
    sgst_total?: number | null
    igst_total?: number | null
    cess_total?: number | null
    tax_total?: number | null
    round_off?: number | null
    grand_total?: number | null
    price_includes_tax?: boolean | null
  }
  items: InvoiceItem[]
}

export interface SyncInventoryResponse {
  synced_at: string
  medicines: MedicineWithBatches[]
}

// An HSN code bundled with its currently-active tax rate. This is the shape
// served by GET /api/sync/tax so the HSN dropdown and tax auto-fill can be
// built from a single offline-cached read.
export interface HSNWithRate extends HSNCode {
  gst_rate: number
  cgst_rate: number
  sgst_rate: number
  igst_rate: number
  cess_rate: number
}

export interface SyncTaxResponse {
  synced_at: string
  hsn_codes: HSNWithRate[]
  tax_configs: MedicineTaxConfig[]
}

export interface ReconcileRowInput {
  batch_id: string
  physical_count: number
  reason: string
}

export interface ReconcileResultItem {
  id: string
  journal_id: string
  medicine_id: string
  batch_id: string
  system_stock: number
  physical_stock: number
  variance_quantity: number
  cost_impact: number
  batch_number?: string
  medicine_name?: string
}

export interface SalesBreakdown {
  payment_type: PaymentType
  invoices: number
  total: number
  units_sold: number
}

export interface DailySales {
  day: string
  payment_type: PaymentType
  invoices: number
  total: number
}

export interface SalesReport {
  breakdown: SalesBreakdown[]
  daily: DailySales[]
  net_sales: number
  net_units: number
}

export interface SupplierPurchase {
  supplier_name: string
  orders: number
  items: number
  total: number
}

export interface PurchaseReport {
  order_count: number
  item_count: number
  total_spend: number
  suppliers: SupplierPurchase[]
}

export interface ProfitLossLine {
  medicine_id: string
  medicine_name: string
  units_sold: number
  revenue: number
  cost: number
  profit: number
  margin_pct: number
}

export interface ProfitLossReport {
  lines: ProfitLossLine[]
  total_revenue: number
  total_cost: number
  total_profit: number
  margin_pct: number
}

export interface ExpiringBatch {
  batch_id: string
  medicine_id: string
  medicine_name: string
  manufacturer: string
  batch_number: string
  expiry_date: string
  current_stock: number
  purchase_price: number
  sale_price: number
  stock_value: number
  expired: boolean
}

export interface LowStockItem {
  medicine_id: string
  medicine_name: string
  manufacturer: string
  min_reorder_level: number
  total_stock: number
  shortfall: number
}

export interface Supplier {
  id: string
  legal_name: string
  trade_name?: string | null
  gstin?: string | null
  pan?: string | null
  address?: string | null
  state?: string | null
  state_code?: string | null
  phone?: string | null
  email?: string | null
  created_at: string
  updated_at: string
}

export type PurchaseLineInput =
  | {
      medicine_id: string
      batch_number: string
      expiry_date: string
      quantity: number
      bonus_quantity: number
      purchase_price: number
      sale_price: number
      discount_type: string
      discount_value: number
    }
  | {
      medicine_id?: undefined
      medicine_name: string
      salt_composition: string
      manufacturer: string
      packing: string
      min_reorder_level: number
      hsn_code?: string
      price_includes_tax?: boolean
      batch_number: string
      expiry_date: string
      quantity: number
      bonus_quantity: number
      purchase_price: number
      sale_price: number
      discount_type: string
      discount_value: number
    }

export interface CreatePurchaseRequest {
  invoice_no?: string
  supplier_name: string
  supplier_id?: string
  supplier_gstin?: string
  supplier_state?: string
  store_id?: string
  place_of_supply?: string
  discount_total: number
  items: PurchaseLineInput[]
}

export interface PurchaseOrderInfo {
  id: string
  invoice_no: string
  supplier_name: string
  total_amount: number
  discount_total: number
  created_at: string
  // GST fields
  supplier_id?: string | null
  supplier_gstin?: string | null
  supplier_state_code?: string | null
  store_id?: string | null
  supply_type?: string | null
  gross_amount?: number | null
  taxable_amount?: number | null
  cgst_total?: number | null
  sgst_total?: number | null
  igst_total?: number | null
  cess_total?: number | null
  tax_total?: number | null
  grand_total?: number | null
  price_includes_tax?: boolean | null
}

export interface PurchaseOrderItemInfo {
  id: string
  purchase_id: string
  medicine_id: string
  batch_number: string
  expiry_date: string
  quantity: number
  bonus_quantity: number
  purchase_price: number
  sale_price: number
  discount_type: string
  discount_value: number
  discount_amount: number
  // GST tax snapshot fields
  hsn_code?: string | null
  gross_amount?: number | null
  taxable_value?: number | null
  gst_rate?: number | null
  cgst_rate?: number | null
  cgst_amount?: number | null
  sgst_rate?: number | null
  sgst_amount?: number | null
  igst_rate?: number | null
  igst_amount?: number | null
  cess_rate?: number | null
  cess_amount?: number | null
  line_total?: number | null
}

export interface SalesInvoiceRow {
  id: string
  invoice_no: string
  customer_id?: string | null
  customer_name: string
  payment_type: PaymentType
  total_amount: number
  discount_total: number
  item_count: number
  units_sold: number
  created_at: string
  // B2B
  sale_type?: string
  // GST fields
  supply_type?: string | null
  grand_total?: number | null
  tax_total?: number | null
}

export interface SalesInvoiceItemDetail extends InvoiceItem {
  medicine_name: string
  batch_number: string
}

export interface SalesInvoiceDetail {
  invoice: {
    id: string
    invoice_no: string
    customer_id?: string | null
    payment_type: PaymentType
    total_amount: number
    discount_total: number
    invoice_date: string
    financial_year: string
    created_at: string
    // B2B fields
    sale_type?: string
    buyer_name?: string | null
    buyer_gstin?: string | null
    buyer_address?: string | null
    // GST fields
    supply_type?: string | null
    gross_amount?: number | null
    taxable_amount?: number | null
    cgst_total?: number | null
    sgst_total?: number | null
    igst_total?: number | null
    cess_total?: number | null
    tax_total?: number | null
    round_off?: number | null
    grand_total?: number | null
    price_includes_tax?: boolean | null
  }
  customer_name: string
  items: SalesInvoiceItemDetail[]
}

export interface PurchaseInvoiceRow {
  id: string
  invoice_no: string
  supplier_name: string
  total_amount: number
  discount_total: number
  item_count: number
  units_purchased: number
  created_at: string
  // GST fields
  supply_type?: string | null
  tax_total?: number | null
  grand_total?: number | null
}

export interface PurchaseInvoiceDetail {
  invoice: PurchaseOrderInfo
  items: (PurchaseOrderItemInfo & { medicine_name: string })[]
}

export interface BatchDetail extends Batch {
  expired: boolean
}

export interface MedicineSalesStats {
  units_sold: number
  total_revenue: number
  invoices: number
}

export interface MedicinePurchaseStats {
  units_purchased: number
  total_spend: number
  orders: number
}

export interface RecentSale {
  invoice_id: string
  invoice_no: string
  quantity: number
  unit_sale_price: number
  subtotal: number
  created_at: string
  customer_name: string
}

export interface RecentPurchase {
  purchase_id: string
  invoice_no: string
  supplier_name: string
  quantity: number
  bonus_quantity: number
  purchase_price: number
  created_at: string
}

export interface MedicineDetail {
  id: string
  name: string
  salt_composition: string
  manufacturer: string
  min_reorder_level: number
  packing: string
  uqc: string
  created_at: string
  updated_at: string
  batches: BatchDetail[]
  total_stock: number
  sales_stats: MedicineSalesStats
  purchase_stats: MedicinePurchaseStats
  recent_sales: RecentSale[]
  recent_purchases: RecentPurchase[]
  tax_config?: MedicineTaxConfig | null
}

export interface HSNCode {
  id: string
  code: string
  description: string
  created_at: string
}

export interface TaxRate {
  id: string
  hsn_code_id: string
  gst_rate: number
  cgst_rate: number
  sgst_rate: number
  igst_rate: number
  cess_rate: number
  effective_from: string
  effective_to?: string | null
  created_at: string
}

export interface MedicineTaxConfig {
  id: string
  medicine_id: string
  hsn_code_id: string
  tax_rate_id: string
  price_includes_tax: boolean
  effective_from: string
  effective_to?: string | null
  created_at: string
  hsn_code?: string
  tax_rate?: TaxRate
}

// ---- GST Report Types ----

export interface GSTR1Preview {
  taxable_value: number
  cgst_total: number
  sgst_total: number
  igst_total: number
  b2b_count: number
  b2c_count: number
}

export interface GST3BLineTotals {
  taxable_value: number
  igst: number
  cgst: number
  sgst: number
  cess: number
  total: number
}

export interface GST3BNetLiability {
  liability: GST3BLineTotals
  itc_credit: GST3BLineTotals
  payable: GST3BLineTotals
}

export interface GSTR3B {
  gstin: string
  period: string
  financial_year: string
  gstn_period_code: string
  filing_date: string
  state_code: string
  '3_1_a_outward_taxable_supplies': GST3BLineTotals
  '3_1_b_reverse_charge': GST3BLineTotals
  '3_1_c_zero_rated': GST3BLineTotals
  '3_1_d_exempt_nil_rated': GST3BLineTotals
  '4_a_eligible_itc': GST3BLineTotals
  '4_b_ineligible_itc': GST3BLineTotals
  '6_1_net_liability': GST3BNetLiability
  itc_at_risk: number
  unmatched_docs: number
}

export interface GSTR2BBatch {
  id: string
  store_id?: string | null
  gstin: string
  period: string
  file_name: string
  doc_count: number
  matched_count: number
  unmatched_count: number
  status: string
  created_at: string
}

export interface GSTR2BDoc {
  id: string
  import_batch_id: string
  store_id?: string | null
  supplier_gstin: string
  doc_type: string
  period: string
  invoice_no: string
  invoice_date: string
  taxable_value: number
  igst_amount: number
  cgst_amount: number
  sgst_amount: number
  cess_amount: number
  total_value: number
  match_status: string
  matched_purchase_id?: string | null
  matched_difference?: number | null
  notes: string
  created_at: string
}

export interface GSTR2BReconciliation {
  batch_id: string
  period: string
  gstin: string
  total_docs: number
  matched: number
  unmatched: number
  amount_mismatch: number
  matched_taxable_value: number
  unmatched_taxable_value: number
}

export interface Store {
  id: string
  gst_registration_id?: string | null
  name: string
  address: string
  phone: string
  drug_license_number: string
  drug_license_expiry: string | null
  is_active: boolean
  max_employees: number
  created_at: string
  updated_at: string
  owner_name?: string
  gstin?: string | null
  pan?: string | null
  state_code?: string | null
}

// ---- Auth & roles ----

export type Role = 'STORE_OWNER' | 'EMPLOYEE'

export type Permission =
  | 'sales:create'
  | 'sales:view'
  | 'customers:create'
  | 'customers:view'
  | 'purchases:create'
  | 'purchases:view'
  | 'stock_audit:create'
  | 'stock:view'
  | 'khata:view'

export interface Principal {
  id: string
  name: string
  role: Role
  store_id: string
  permissions: Permission[]
}

export interface AuthUser {
  id: string
  name: string
  phone: string
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface AuthSession {
  user: AuthUser
  principal: Principal
}

// ---- Employees & store settings ----

export interface Membership {
  id: string
  store_id: string
  user_id: string
  role: Role
  is_active: boolean
  created_at: string
  updated_at: string
  user_name?: string
  user_phone?: string
  user_active?: boolean
}

// ---- Purchase requests (employee-submitted, owner-approved) ----

export type RequestStatus = 'PENDING' | 'APPROVED' | 'REJECTED' | 'CANCELLED'

export interface PurchaseRequest {
  id: string
  store_id: string
  requested_by: string
  status: RequestStatus
  purchase_snapshot?: string
  purchase_id?: string | null
  reviewed_by?: string | null
  reviewed_at?: string | null
  rejection_reason?: string
  created_at: string
  updated_at: string
  requester_name?: string
  reviewer_name?: string
}

export interface PurchaseSnapshotItem {
  medicine_id?: string
  medicine_name?: string
  salt_composition?: string
  manufacturer?: string
  packing?: string
  min_reorder_level?: number
  hsn_code?: string
  price_includes_tax?: boolean
  batch_number: string
  expiry_date: string
  quantity: number
  bonus_quantity: number
  purchase_price: number
  sale_price: number
  discount_type: string
  discount_value: number
}

export interface PurchaseSnapshot {
  invoice_no?: string
  supplier_name: string
  supplier_gstin?: string
  supplier_state?: string
  place_of_supply?: string
  discount_total: number
  reverse_charge?: boolean
  itc_eligible?: boolean
  itc_amount?: number
  items: PurchaseSnapshotItem[]
}

export interface StockAuditRequestItem {
  id: string
  request_id: string
  medicine_id: string
  medicine_name?: string
  batch_id: string
  batch_number: string
  system_quantity: number
  physical_quantity: number
  reason: string
}

export interface StockAuditRequest {
  id: string
  store_id: string
  requested_by: string
  status: RequestStatus
  notes: string
  journal_id?: string | null
  reviewed_by?: string | null
  reviewed_at?: string | null
  rejection_reason?: string
  created_at: string
  updated_at: string
  requester_name?: string
  reviewer_name?: string
}

export const UQC_OPTIONS = [
  { value: 'NOS', label: 'Numbers' },
  { value: 'TAB', label: 'Tablets' },
  { value: 'BTL', label: 'Bottles' },
  { value: 'BOX', label: 'Boxes' },
  { value: 'KGS', label: 'Kilograms' },
  { value: 'GMS', label: 'Grams' },
  { value: 'LTR', label: 'Litres' },
] as const
