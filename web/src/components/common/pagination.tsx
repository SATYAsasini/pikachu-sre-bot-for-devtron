import { ChevronLeft, ChevronRight } from 'lucide-react'
import { cn } from 'cn'
import { Text } from '@/components/common/text'

export function Pagination({
  page,
  pages,
  from,
  to,
  total,
  noun = 'item',
  onPage,
  className,
}: {
  page: number
  pages: number
  from: number
  to: number
  total: number
  noun?: string
  onPage: (next: number) => void
  className?: string
}) {
  if (pages <= 1) return null

  return (
    <nav
      aria-label="Pagination"
      className={cn('flex flex-wrap items-center justify-between gap-2 border-t border-border px-3 py-2', className)}
    >
      <Text tone="fine" as="span" className="tabular">
        {from}–{to} of {total} {noun}
        {total === 1 ? '' : 's'}
      </Text>

      <div className="flex items-center gap-1">
        <Step label="Previous page" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <ChevronLeft aria-hidden className="size-3.5" />
        </Step>

        {pageNumbers(page, pages).map((n, i) =>
          n === null ? (
            <span key={`gap-${i}`} aria-hidden className="px-1 text-xs text-muted-foreground">
              …
            </span>
          ) : (
            <button
              key={n}
              type="button"
              onClick={() => onPage(n)}
              aria-current={n === page ? 'page' : undefined}
              aria-label={`Page ${n}`}
              className={cn(
                'tabular h-7 min-w-7 rounded-md border px-2 text-xs transition-colors',
                'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                n === page
                  ? 'border-accent-strong/45 bg-accent-strong/12 font-semibold text-foreground'
                  : 'border-border bg-card text-muted-foreground hover:text-foreground',
              )}
            >
              {n}
            </button>
          ),
        )}

        <Step label="Next page" disabled={page >= pages} onClick={() => onPage(page + 1)}>
          <ChevronRight aria-hidden className="size-3.5" />
        </Step>
      </div>
    </nav>
  )
}

function Step({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string
  disabled: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className="grid size-7 place-items-center rounded-md border border-border bg-card text-muted-foreground transition-colors hover:text-foreground disabled:pointer-events-none disabled:opacity-40 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      {children}
    </button>
  )
}

/**
 * First, last, and a window around the current page, with gaps marked.
 *
 * Rendering every page number is fine at five pages and absurd at fifty, and
 * the row must not change width as you move through it — that is why the
 * window is fixed rather than growing near the ends.
 */
function pageNumbers(page: number, pages: number): (number | null)[] {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1)

  const out: (number | null)[] = [1]
  const lo = Math.max(2, Math.min(page - 1, pages - 4))
  const hi = Math.min(pages - 1, Math.max(page + 1, 5))

  if (lo > 2) out.push(null)
  for (let n = lo; n <= hi; n++) out.push(n)
  if (hi < pages - 1) out.push(null)
  out.push(pages)
  return out
}
