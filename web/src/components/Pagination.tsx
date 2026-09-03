import { useEffect, useRef, useState } from 'react'

// usePaged slices any list into pages and resets to page 1 whenever the
// underlying list is replaced (new search, reload, mutation).
export function usePaged<T>(items: T[], pageSize: number) {
  const [page, setPage] = useState(1)
  const prevRef = useRef(items)

  useEffect(() => {
    if (prevRef.current !== items) {
      prevRef.current = items
      setPage(1)
    }
  }, [items])

  const pageCount = Math.max(1, Math.ceil(items.length / pageSize))
  const safePage = Math.min(page, pageCount)
  const start = (safePage - 1) * pageSize
  const slice = items.slice(start, start + pageSize)

  return { page: safePage, pageCount, setPage, slice, total: items.length, start }
}

function pageNumberList(page: number, pageCount: number): (number | 'ellipsis')[] {
  if (pageCount <= 7) {
    return Array.from({ length: pageCount }, (_, i) => i + 1)
  }
  const pages = new Set<number>([1, pageCount])
  for (let i = Math.max(2, page - 1); i <= Math.min(pageCount - 1, page + 1); i++) {
    pages.add(i)
  }
  const out: (number | 'ellipsis')[] = []
  let prev = 0
  for (const p of [...pages].sort((a, b) => a - b)) {
    if (p - prev > 1) out.push('ellipsis')
    out.push(p)
    prev = p
  }
  return out
}

export default function Pagination({
  page,
  pageCount,
  total,
  start,
  pageSize,
  onPage,
  pageNumbers = false,
}: {
  page: number
  pageCount: number
  total: number
  start: number
  pageSize: number
  onPage: (page: number) => void
  pageNumbers?: boolean
}) {
  if (pageCount <= 1) return null

  const showing = `${start + 1}–${Math.min(start + pageSize, total)} of ${total}`
  const btn =
    'rounded-md border border-line px-2.5 py-1 text-xs font-semibold text-inksoft transition-colors hover:bg-mint-50 disabled:opacity-40 disabled:hover:bg-transparent'
  const numBtn = (active: boolean) =>
    'min-w-[30px] rounded-md border px-2 py-1 text-center text-xs font-semibold tabular-nums transition-colors ' +
    (active
      ? 'border-pine-700 bg-pine-700 text-white'
      : 'border-line text-inksoft hover:bg-mint-50')

  return (
    <nav
      aria-label="Pagination"
      className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-line-soft px-4 py-2.5"
    >
      <p className="text-xs tabular-nums text-inksoft">
        Showing <span className="font-mono font-semibold">{showing}</span>
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <button onClick={() => onPage(page - 1)} disabled={page <= 1} className={btn}>
          ← Previous
        </button>
        {pageNumbers && (
          <span className="flex items-center gap-1">
            {pageNumberList(page, pageCount).map((n, i) =>
              n === 'ellipsis' ? (
                <span key={`ellipsis-${i}`} className="px-0.5 text-xs text-inksoft/60">
                  …
                </span>
              ) : (
                <button
                  key={n}
                  onClick={() => onPage(n)}
                  disabled={n === page}
                  aria-current={n === page ? 'page' : undefined}
                  className={numBtn(n === page)}
                >
                  {n}
                </button>
              )
            )}
          </span>
        )}
        <span className="text-xs font-semibold tabular-nums text-inksoft">
          Page {page} of {pageCount}
        </span>
        <button onClick={() => onPage(page + 1)} disabled={page >= pageCount} className={btn}>
          Next →
        </button>
      </div>
    </nav>
  )
}
