// GSTR-2B import parsing.
//
// The GST Portal's GSTR-2B download is an Excel workbook; this accepts the
// JSON or CSV a user exports from it and normalises the GSTN column names
// into the document shape the import endpoint expects.

export interface ParsedGSTR2BDoc {
  supplier_gstin?: string
  doc_type: string
  invoice_no: string
  invoice_date: string
  taxable_value: number
  igst: number
  cgst: number
  sgst: number
  cess: number
  total_value?: number
}

const FIELD_ALIASES: Record<keyof Omit<ParsedGSTR2BDoc, 'doc_type'> | 'doc_type', string[]> = {
  supplier_gstin: ['supplier gstin', 'gstin of supplier', 'gstin', 'supplier gstin no'],
  doc_type: ['doc type', 'transaction type', 'document type', 'type'],
  invoice_no: ['invoice no', 'invoice number', 'invoicenumber', 'bill no'],
  invoice_date: ['invoice date', 'doc date', 'date'],
  taxable_value: ['taxable value', 'taxablevalue', 'taxable amount', 'txval'],
  igst: ['igst', 'igst amount'],
  cgst: ['cgst', 'cgst amount'],
  sgst: ['sgst', 'sgst amount'],
  cess: ['cess', 'cess amount'],
  total_value: ['total value', 'totalvalue', 'total amount', 'total'],
}

type FieldAlias =
  | 'supplier_gstin'
  | 'doc_type'
  | 'invoice_no'
  | 'invoice_date'
  | 'taxable_value'
  | 'igst'
  | 'cgst'
  | 'sgst'
  | 'cess'
  | 'total_value'

function normKey(k: string): string {
  return k.trim().toLowerCase().replace(/[\s_-]+/g, ' ')
}

function resolveField(src: Record<string, unknown>, alias: FieldAlias): unknown {
  for (const key of Object.keys(src)) {
    if (FIELD_ALIASES[alias].includes(normKey(key))) return src[key]
    if (normKey(key) === alias.replace(/_/g, ' ')) return src[key]
  }
  return undefined
}

function normDate(s: unknown): string {
  const v = String(s ?? '').trim()
  if (!v) return ''
  const iso = v.match(/^(\d{4})-(\d{1,2})-(\d{1,2})/)
  if (iso) return `${iso[1]}-${iso[2].padStart(2, '0')}-${iso[3].padStart(2, '0')}`
  // GSTN exports use day-month-year (e.g. 18/08/2026).
  const dmy = v.match(/^(\d{1,2})[/-](\d{1,2})[/-](\d{4})/)
  if (dmy) return `${dmy[3]}-${dmy[2].padStart(2, '0')}-${dmy[1].padStart(2, '0')}`
  return v
}

function normDocType(s: unknown): string {
  const v = String(s ?? '').trim().toLowerCase()
  if (v === 'crn' || v.includes('credit')) return 'CRN'
  if (v === 'dbn' || v.includes('debit')) return 'DBN'
  return 'INV'
}

function normNum(s: unknown): number {
  const v = String(s ?? '').trim().replace(/[, ]/g, '')
  if (v === '') return 0
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function maybeNum(s: unknown): number | undefined {
  const n = normNum(s)
  return s === undefined || s === null || String(s).trim() === '' ? undefined : n
}

function normalizeEntry(src: Record<string, unknown>): ParsedGSTR2BDoc {
  return {
    supplier_gstin: String(resolveField(src, 'supplier_gstin') ?? '').trim() || undefined,
    doc_type: normDocType(resolveField(src, 'doc_type')),
    invoice_no: String(resolveField(src, 'invoice_no') ?? '').trim(),
    invoice_date: normDate(resolveField(src, 'invoice_date')),
    taxable_value: normNum(resolveField(src, 'taxable_value')),
    igst: normNum(resolveField(src, 'igst')),
    cgst: normNum(resolveField(src, 'cgst')),
    sgst: normNum(resolveField(src, 'sgst')),
    cess: normNum(resolveField(src, 'cess')),
    total_value: maybeNum(resolveField(src, 'total_value')),
  }
}

function splitCSVLine(line: string): string[] {
  const out: string[] = []
  let cur = ''
  let inQuotes = false
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (ch === '"') {
      if (inQuotes && line[i + 1] === '"') {
        cur += '"'
        i++
      } else {
        inQuotes = !inQuotes
      }
    } else if (ch === ',' && !inQuotes) {
      out.push(cur)
      cur = ''
    } else {
      cur += ch
    }
  }
  out.push(cur)
  return out
}

export interface GSTR2BParseResult {
  docs: ParsedGSTR2BDoc[]
  period?: string
}

// parseGSTR2BFile turns a JSON or CSV export (from the GST Portal / a
// spreadsheet export of it) into normalised documents ready to import.
export function parseGSTR2BFile(text: string, fileName: string): GSTR2BParseResult {
  const isCSV = /\.(csv|tsv)$/i.test(fileName)
  if (isCSV) {
    const blocks = text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean)
    if (blocks.length < 2) throw new Error('CSV needs a header row and at least one document row')
    const header = splitCSVLine(blocks[0]).map(normKey)
    const rows = blocks.slice(1).map((l) => {
      const cells = splitCSVLine(l)
      const src: Record<string, unknown> = {}
      header.forEach((h, i) => {
        src[h] = cells[i] ?? ''
      })
      return src
    })
    const docs = rows.map(normalizeEntry).filter((d) => d.invoice_no !== '')
    if (docs.length === 0) {
      throw new Error('No documents recognised in the CSV. Keep the GSTN column names (invoice_no, invoice_date, taxable_value…)')
    }
    return { docs }
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    if (/\.(txt|unknown)$/i.test(fileName)) {
      throw new Error(`${fileName} is not .json or .csv`)
    }
    throw new Error('The file is not valid JSON and not a .csv — upload the GSTR-2B export as JSON or CSV')
  }

  let entries: unknown[] = []
  let period: string | undefined
  if (Array.isArray(parsed)) {
    entries = parsed
  } else if (parsed && typeof parsed === 'object') {
    const obj = parsed as Record<string, unknown>
    if (typeof obj.period === 'string') period = obj.period
    entries = Array.isArray(obj.docs)
      ? (obj.docs as unknown[])
      : Array.isArray(obj.docdata)
        ? (obj.docdata as unknown[])
        : []
  }
  if (entries.length === 0) throw new Error('No document array (docs / docdata) found in the JSON')

  const docs = entries
    .map((e) => normalizeEntry((e ?? {}) as Record<string, unknown>))
    .filter((d) => d.invoice_no !== '')
  if (docs.length === 0) throw new Error('No documents recognised in the JSON')
  return { docs, period }
}