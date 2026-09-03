import type {
  AuthSession,
  AuthUser,
  CheckoutRequest,
  CheckoutResponse,
  CreatePurchaseRequest,
  Customer,
  ExpiringBatch,
  GSTR1Preview,
  GSTR2BBatch,
  GSTR2BDoc,
  GSTR2BReconciliation,
  GSTR3B,
  HSNCode,
  LedgerEntry,
  LowStockItem,
  Membership,
  MedicineDetail,
  MedicineTaxConfig,
  PlatformStoreInfo,
  Principal,
  ProfitLossReport,
  PurchaseInvoiceDetail,
  PurchaseInvoiceRow,
  PurchaseOrderInfo,
  PurchaseOrderItemInfo,
  PurchaseReport,
  PurchaseRequest,
  ReconcileRowInput,
  RequestStatus,
  SalesInvoiceDetail,
  SalesInvoiceRow,
  SalesReport,
  StockAuditRequest,
  StockAuditRequestItem,
  Store,
  SubscriptionPayment,
  SubscriptionPlanType,
  SubscriptionStatus,
  Supplier,
  TaxRate,
} from '../types'

export class UnauthorizedError extends Error {
  constructor() {
    super('session expired — sign in again')
    this.name = 'UnauthorizedError'
  }
}

export class SubscriptionExpiredError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'SubscriptionExpiredError'
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!res.ok) {
    let message = `request failed (${res.status})`
    let code = ''
    try {
      const body = await res.json()
      if (body && typeof body.error === 'string') {
        code = body.error
        message = body.error
      }
      // Prefer the human-readable message when the server sends one (e.g.
      // the subscription gate returns {error, message}).
      if (body && typeof body.message === 'string' && body.message.trim() !== '') {
        message = body.message
      }
      if (code === 'subscription_expired') {
        throw new SubscriptionExpiredError(message)
      }
    } catch (err) {
      if (err instanceof SubscriptionExpiredError) throw err
      /* keep default */
    }
    // A 401 only means the session died when the server says so. On login a 401
    // is just bad credentials ("invalid phone or password", "account is
    // disabled") and must surface that real reason, not a session-expiry.
    if (res.status === 401 && message === 'unauthorized') {
      window.dispatchEvent(new Event('pms:unauthorized'))
      throw new UnauthorizedError()
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

export interface CustomerInput {
  name: string
  phone: string
  credit_limit: number
  gstin?: string
  customer_type?: string
  state_code?: string
  state?: string
  billing_address?: string
  shipping_address?: string
}

export interface ShopStoreInput {
  name: string
  address: string
  phone: string
  owner_name: string
  max_employees: number
  gstin?: string | null
  pan?: string | null
  drug_license_number?: string
  drug_license_expiry?: string | null
}

export const api = {
  // ---- Auth ----

  me(): Promise<AuthSession> {
    return request<AuthSession>('/api/auth/me')
  },

  login(phone: string, password: string): Promise<{ principal: Principal }> {
    return request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ phone, password }),
    })
  },

  register(input: {
    name: string
    phone: string
    password: string
    business_name: string
    trade_name?: string
    gstin?: string
    store_name: string
    store_address: string
    store_phone: string
    pan?: string
    drug_license_number?: string
    drug_license_expiry?: string
  }): Promise<AuthSession> {
    return request('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  logout(): Promise<{ status: string }> {
    return request('/api/auth/logout', { method: 'POST' })
  },

  changePassword(currentPassword: string, newPassword: string): Promise<{ status: string }> {
    return request('/api/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    })
  },

  // ---- Purchase requests ----

  createPurchaseRequest(req: CreatePurchaseRequest): Promise<{ request: PurchaseRequest }> {
    return request('/api/purchase-requests', {
      method: 'POST',
      body: JSON.stringify(req),
    })
  },

  purchaseRequests(status?: RequestStatus | 'ALL'): Promise<{ requests: PurchaseRequest[] }> {
    const qs = status && status !== 'ALL' ? `?status=${status}` : ''
    return request(`/api/purchase-requests${qs}`)
  },

  approvePurchaseRequest(id: string): Promise<{ purchase_order: PurchaseOrderInfo; items: PurchaseOrderItemInfo[] }> {
    return request(`/api/purchase-requests/${id}/approve`, { method: 'POST' })
  },

  rejectPurchaseRequest(id: string, reason: string): Promise<{ request: PurchaseRequest }> {
    return request(`/api/purchase-requests/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    })
  },

  cancelPurchaseRequest(id: string): Promise<{ request: PurchaseRequest }> {
    return request(`/api/purchase-requests/${id}/cancel`, { method: 'POST' })
  },

  // ---- Stock audit requests ----

  createStockAuditRequest(
    notes: string,
    items: { medicine_id: string; batch_id: string; physical_quantity: number; reason: string }[],
  ): Promise<{ request: StockAuditRequest; items: StockAuditRequestItem[] }> {
    return request('/api/stock-audit-requests', {
      method: 'POST',
      body: JSON.stringify({ notes, items }),
    })
  },

  stockAuditRequests(status?: RequestStatus | 'ALL'): Promise<{ requests: StockAuditRequest[] }> {
    const qs = status && status !== 'ALL' ? `?status=${status}` : ''
    return request(`/api/stock-audit-requests${qs}`)
  },

  getStockAuditRequest(id: string): Promise<{ request: StockAuditRequest; items: StockAuditRequestItem[] }> {
    return request(`/api/stock-audit-requests/${id}`)
  },

  approveStockAuditRequest(id: string): Promise<{ journal: { id: string }; items: StockAuditRequestItem[] }> {
    return request(`/api/stock-audit-requests/${id}/approve`, { method: 'POST' })
  },

  rejectStockAuditRequest(id: string, reason: string): Promise<{ request: StockAuditRequest }> {
    return request(`/api/stock-audit-requests/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    })
  },

  cancelStockAuditRequest(id: string): Promise<{ request: StockAuditRequest }> {
    return request(`/api/stock-audit-requests/${id}/cancel`, { method: 'POST' })
  },

  // ---- Employees & store settings (owner) ----

  employees(): Promise<{ members: Membership[] }> {
    return request('/api/employees')
  },

  inviteEmployee(name: string, phone: string, password: string): Promise<{ user: AuthUser }> {
    return request('/api/employees', {
      method: 'POST',
      body: JSON.stringify({ name, phone, password }),
    })
  },

  deactivateEmployee(userId: string): Promise<{ status: string }> {
    return request(`/api/employees/${userId}`, { method: 'DELETE' })
  },

  store(): Promise<{ store: Store }> {
    return request('/api/store')
  },

  updateStore(input: ShopStoreInput): Promise<{ store: Store }> {
    return request('/api/store', {
      method: 'PUT',
      body: JSON.stringify(input),
    })
  },

  checkout(req: CheckoutRequest): Promise<CheckoutResponse> {
    return request<CheckoutResponse>('/api/sales/checkout', {
      method: 'POST',
      body: JSON.stringify(req),
    })
  },

  createPurchase(
    req: CreatePurchaseRequest,
  ): Promise<{ purchase_order: PurchaseOrderInfo; items: PurchaseOrderItemInfo[] }> {
    return request('/api/purchases', {
      method: 'POST',
      body: JSON.stringify(req),
    })
  },

  reconcile(
    items: ReconcileRowInput[],
    notes: string,
    verifiedBy?: string,
  ): Promise<{ journal: { id: string; notes: string }; items: unknown[] }> {
    return request('/api/inventory/reconcile', {
      method: 'POST',
      body: JSON.stringify({ items, notes, verified_by_user_id: verifiedBy }),
    })
  },

  customers(): Promise<{ customers: Customer[] }> {
    return request('/api/customers')
  },

  searchCustomers(opts: { q?: string; type?: string; limit?: number } = {}): Promise<{ customers: Customer[] }> {
    const params = new URLSearchParams()
    if (opts.q) params.set('search', opts.q)
    if (opts.type) params.set('type', opts.type)
    if (opts.limit !== undefined) params.set('limit', String(opts.limit))
    const qs = params.toString()
    return request(`/api/customers${qs ? `?${qs}` : ''}`)
  },

  createCustomer(c: CustomerInput): Promise<Customer> {
    return request('/api/customers', { method: 'POST', body: JSON.stringify(c) })
  },

  updateCustomer(id: string, c: CustomerInput): Promise<Customer> {
    return request(`/api/customers/${id}`, { method: 'PUT', body: JSON.stringify(c) })
  },

  ledger(id: string): Promise<{ customer: Customer; entries: LedgerEntry[] }> {
    return request(`/api/customers/${id}/ledger`)
  },

  recordPayment(
    id: string,
    amount: number,
    notes: string,
  ): Promise<{ customer: Customer; entry: LedgerEntry }> {
    return request(`/api/customers/${id}/payments`, {
      method: 'POST',
      body: JSON.stringify({ amount, notes }),
    })
  },

  salesReport(start: string, end: string): Promise<SalesReport> {
    return request(`/api/reports/sales?start_date=${start}&end_date=${end}`)
  },

  purchaseReport(start: string, end: string): Promise<PurchaseReport> {
    return request(`/api/reports/purchase?start_date=${start}&end_date=${end}`)
  },

  profitLoss(start: string, end: string): Promise<ProfitLossReport> {
    return request(`/api/reports/profit-loss?start_date=${start}&end_date=${end}`)
  },

  expiry(withinMonths: number): Promise<{ batches: ExpiringBatch[] }> {
    return request(`/api/reports/expiry?within_months=${withinMonths}`)
  },

  lowStock(): Promise<{ items: LowStockItem[] }> {
    return request('/api/reports/low-stock')
  },

  salesInvoices(start: string, end: string, q?: string): Promise<{ invoices: SalesInvoiceRow[] }> {
    const params = new URLSearchParams({ start_date: start, end_date: end })
    if (q) params.set('q', q)
    return request(`/api/sales/invoices?${params}`)
  },

  salesInvoice(id: string): Promise<SalesInvoiceDetail> {
    return request(`/api/sales/invoices/${id}`)
  },

  salesInvoiceByNo(customerId: string, invoiceNo: string): Promise<SalesInvoiceDetail> {
    const params = new URLSearchParams({ customer_id: customerId, invoice_no: invoiceNo })
    return request(`/api/sales/invoices/resolve?${params}`)
  },

  purchaseInvoices(start: string, end: string, q?: string): Promise<{ invoices: PurchaseInvoiceRow[] }> {
    const params = new URLSearchParams({ start_date: start, end_date: end })
    if (q) params.set('q', q)
    return request(`/api/purchases/invoices?${params}`)
  },

  purchaseInvoice(id: string): Promise<PurchaseInvoiceDetail> {
    return request(`/api/purchases/invoices/${id}`)
  },

  medicineDetail(id: string): Promise<MedicineDetail> {
    return request(`/api/medicines/${id}/detail`)
  },

  suppliers(): Promise<Supplier[]> {
    return request('/api/suppliers')
  },

  createSupplier(s: Omit<Supplier, 'id' | 'created_at' | 'updated_at'>): Promise<Supplier> {
    return request('/api/suppliers', { method: 'POST', body: JSON.stringify(s) })
  },

  updateSupplier(id: string, s: Partial<Supplier>): Promise<Supplier> {
    return request(`/api/suppliers/${id}`, { method: 'PUT', body: JSON.stringify(s) })
  },

  deleteSupplier(id: string): Promise<{ deleted: boolean }> {
    return request(`/api/suppliers/${id}`, { method: 'DELETE' })
  },

  listHSNCodes(): Promise<{ hsn_codes: HSNCode[] }> {
    return request('/api/hsn')
  },

  createHSNCode(code: string, description: string): Promise<HSNCode> {
    return request('/api/hsn', { method: 'POST', body: JSON.stringify({ code, description }) })
  },

  upsertTaxRate(hsnCodeId: string, rates: {
    gst_rate: number
    cgst_rate: number
    sgst_rate: number
    igst_rate: number
    cess_rate?: number
  }): Promise<TaxRate> {
    return request(`/api/hsn/${hsnCodeId}/tax-rate`, { method: 'PUT', body: JSON.stringify(rates) })
  },

  getMedicineTaxConfig(medicineId: string): Promise<MedicineTaxConfig | null> {
    return request(`/api/medicines/${medicineId}/tax-config`)
  },

  upsertMedicineTaxConfig(medicineId: string, config: {
    hsn_code_id: string
    tax_rate_id: string
    price_includes_tax: boolean
  }): Promise<MedicineTaxConfig> {
    return request(`/api/medicines/${medicineId}/tax-config`, { method: 'PUT', body: JSON.stringify(config) })
  },

  // ---- GST Returns ----

  // GSTR-1 is driven by a return period (a calendar month, YYYY-MM).

  gstr1Preview(period: string, storeId?: string): Promise<GSTR1Preview> {
    const params = new URLSearchParams({ period })
    if (storeId) params.set('store_id', storeId)
    return request(`/api/gst/gstr1/preview?${params}`)
  },

  downloadGSTR1JSON(period: string, storeId?: string): Promise<Blob> {
    const params = new URLSearchParams({ period })
    if (storeId) params.set('store_id', storeId)
    return fetch(`/api/gst/gstr1?${params}`).then(res => {
      if (!res.ok) throw new Error('Failed to download GSTR-1 JSON')
      return res.blob()
    })
  },

  downloadGSTR1CSV(period: string, storeId?: string): Promise<Blob> {
    const params = new URLSearchParams({ period })
    if (storeId) params.set('store_id', storeId)
    return fetch(`/api/gst/gstr1/excel?${params}`).then(res => {
      if (!res.ok) throw new Error('Failed to download GSTR-1 CSV')
      return res.blob()
    })
  },

  gstr3b(period: string, storeId?: string): Promise<GSTR3B> {
    const params = new URLSearchParams({ period })
    if (storeId) params.set('store_id', storeId)
    return request(`/api/gst/gstr3b?${params}`)
  },

  gstr2bBatches(): Promise<GSTR2BBatch[]> {
    return request('/api/gst/gstr2b/batches')
  },

  gstr2bBatch(id: string): Promise<{ batch: GSTR2BBatch; docs: GSTR2BDoc[] }> {
    return request(`/api/gst/gstr2b/batches/${id}`)
  },

  importGSTR2B(input: {
    period: string
    gstin?: string
    source?: string
    docs: Array<{
      supplier_gstin?: string
      doc_type?: string
      invoice_no: string
      invoice_date: string
      taxable_value: number
      igst?: number
      cgst?: number
      sgst?: number
      cess?: number
      total_value?: number
    }>
  }): Promise<GSTR2BReconciliation> {
    return request('/api/gst/gstr2b/import', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  downloadB2BInvoicePDF(invoiceId: string): Promise<Blob> {
    return fetch(`/api/sales/invoices/${invoiceId}/pdf`).then(res => {
      if (!res.ok) throw new Error('Failed to download invoice PDF')
      return res.blob()
    })
  },

  // ---- Platform administration (super-admin only) ----

  platformStores(): Promise<{ stores: PlatformStoreInfo[] }> {
    return request('/api/platform/stores')
  },

  platformRenew(
    storeId: string,
    input: { plan_type: SubscriptionPlanType; amount: number; notes?: string },
  ): Promise<{ payment: SubscriptionPayment }> {
    return request(`/api/platform/stores/${storeId}/renew`, {
      method: 'POST',
      body: JSON.stringify(input),
    })
  },

  platformSetStatus(storeId: string, status: SubscriptionStatus): Promise<{ status: string }> {
    return request(`/api/platform/stores/${storeId}/status`, {
      method: 'PUT',
      body: JSON.stringify({ status }),
    })
  },

  platformPayments(storeId: string): Promise<{ payments: SubscriptionPayment[] }> {
    return request(`/api/platform/stores/${storeId}/payments`)
  },
}
