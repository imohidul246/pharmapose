import type { ReactNode } from 'react'

export default function Modal({
  title,
  children,
  onClose,
  wide,
}: {
  title: string
  children: ReactNode
  onClose: () => void
  wide?: boolean
}) {
  return (
    <div
      className="fixed inset-0 z-30 flex items-center justify-center bg-pine-950/55 p-4 backdrop-blur-[2px]"
      onClick={onClose}
    >
      <div
        tabIndex={-1}
        className={(wide ? 'max-w-2xl' : 'max-w-md') + ' w-full space-y-3 rounded-2xl bg-white p-5 shadow-2xl outline-none'}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-dashed border-line pb-3">
          <h3 className="font-display text-base font-bold tracking-tight">{title}</h3>
          <button
            onClick={onClose}
            aria-label="Close dialog"
            className="rounded-md p-1 text-inksoft/70 transition-colors hover:bg-mint-50 hover:text-ink"
          >
            <svg viewBox="0 0 14 14" className="h-3.5 w-3.5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round">
              <path d="M2 2l10 10M12 2L2 12" />
            </svg>
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}