import type { MedicineWithBatches } from '../types'

export interface SearchHit {
  medicine: MedicineWithBatches
  score: number
}

/**
 * High-speed local search across the cached inventory. Runs entirely against
 * IndexedDB-loaded data — no server round-trip.
 *
 * Every whitespace-separated token must match at least one of:
 *   - brand name
 *   - salt composition (generic)
 *   - manufacturer
 *
 * Typing "Paracetamol" therefore surfaces Calpol, Panadol and Dolo together;
 * typing "Calpol" surfaces the brand; "paracetamol gsk" narrows to both.
 *
 * An empty/blank query browses the whole catalog alphabetically so screens
 * have a usable list immediately after sync — no typing required.
 */
export function searchMedicines(
  medicines: MedicineWithBatches[],
  query: string,
  limit = 30,
): SearchHit[] {
  const tokens = normalize(query).split(/\s+/).filter(Boolean)
  if (tokens.length === 0) {
    const hits: SearchHit[] = medicines.map((m) => ({ medicine: m, score: 0 }))
    hits.sort((a, b) => a.medicine.name.localeCompare(b.medicine.name))
    return hits.slice(0, limit)
  }

  const hits: SearchHit[] = []
  for (const m of medicines) {
    const name = normalize(m.name)
    const salt = normalize(m.salt_composition)
    const maker = normalize(m.manufacturer)
    const packing = normalize(m.packing)

    let score = 0
    let allMatched = true
    for (const token of tokens) {
      const s = tokenScore(token, name, salt, maker, packing)
      if (s <= 0) {
        allMatched = false
        break
      }
      score += s
    }
    if (!allMatched) continue

    const totalStock = m.batches.reduce((acc, b) => acc + b.current_stock, 0)
    if (totalStock > 0) score += 5
    hits.push({ medicine: m, score })
  }

  hits.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score
    return a.medicine.name.localeCompare(b.medicine.name)
  })
  return hits.slice(0, limit)
}

function tokenScore(token: string, name: string, salt: string, maker: string, packing: string): number {
  if (name === token) return 100
  if (name.startsWith(token)) return 60
  if (salt === token) return 55
  if (name.includes(token)) return 40
  if (salt.startsWith(token)) return 35
  if (salt.includes(token)) return 25
  if (maker.includes(token)) return 15
  if (packing.includes(token)) return 12
  return 0
}

function normalize(s: string): string {
  return s.toLowerCase().trim()
}
