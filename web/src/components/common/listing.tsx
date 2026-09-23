import type { ReactNode } from 'react'
import { cn } from 'cn'
import { Panel } from '@/components/common/panel'
import { Pagination } from '@/components/common/pagination'

/**
 * One layout for every list in the product.
 *
 * The rule it encodes: **the chrome stays, the rows scroll.** A panel that
 * scrolls as a whole takes its own header, filter bar and pager off-screen the
 * moment you look at row twenty — so you lose the count you were reading, the
 * filter you were about to change, and the way to the next page, all at once.
 * Here the header and the pager are pinned to the panel and only the rows move.
 *
 * The panel is also bounded to the viewport rather than growing with its
 * content, which is what stops any page from feeling like it runs off the
 * bottom of the world.
 */
export function Listing({
  header,
  toolbar,
  children,
  page,
  pages,
  from,
  to,
  total,
  noun,
  onPage,
  /** Space above the list: the top bar, page header and any gutters. */
  reserve = '19rem',
  className,
}: {
  header: ReactNode
  toolbar?: ReactNode
  children: ReactNode
  page: number
  pages: number
  from: number
  to: number
  total: number
  noun: string
  onPage: (next: number) => void
  reserve?: string
  className?: string
}) {
  return (
    <Panel className={cn('flex flex-col', className)} style={{ maxHeight: `calc(100svh - ${reserve})` }}>
      <div className="shrink-0">
        {header}
        {toolbar}
      </div>

      {/* The only part that moves. data-lenis-prevent because Lenis owns the
          wheel at the document root and a nested scroller never sees it. */}
      <div data-lenis-prevent className="min-h-0 flex-1 overflow-y-auto p-2">
        {children}
      </div>

      <div className="shrink-0">
        <Pagination page={page} pages={pages} from={from} to={to} total={total} noun={noun} onPage={onPage} />
      </div>
    </Panel>
  )
}
