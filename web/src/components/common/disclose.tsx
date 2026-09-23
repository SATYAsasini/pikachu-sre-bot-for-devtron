import { useState, type ReactNode } from 'react'
import { cn } from 'cn'

/**
 * A row that keeps its reasoning out of the way until you want it.
 *
 * The run detail page carries a lot of true, useful detail — why a claim was
 * contradicted, how to verify a fix, what to roll back. Showing all of it at
 * once turns the page into a wall and the reader skims. Showing none of it
 * costs a click per item.
 *
 * So: the detail is present but clamped to a single line, it opens on hover,
 * and a click pins it open. Nothing is hidden behind a control you have to
 * discover, and nothing shouts.
 */
export function Disclose({
  summary,
  detail,
  className,
  defaultOpen = false,
  tone,
}: {
  summary: ReactNode
  detail?: ReactNode
  className?: string
  defaultOpen?: boolean
  tone?: 'ok' | 'warn' | 'bad' | 'unknown' | 'sky' | 'neutral' | 'accent'
}) {
  const [pinned, setPinned] = useState(defaultOpen)
  const [hovered, setHovered] = useState(false)
  const open = pinned || hovered
  const hasDetail = Boolean(detail)

  return (
    <div
      className={cn(
        'group/disclose rounded-md border border-transparent px-2 py-1.5 transition-colors',
        hasDetail && 'hover:border-border hover:bg-well',
        pinned && 'border-border bg-well',
        tone === 'bad' && 'hover:border-bad/25',
        tone === 'ok' && 'hover:border-ok/25',
        tone === 'warn' && 'hover:border-warn/25',
        className,
      )}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <button
        type="button"
        disabled={!hasDetail}
        onClick={() => setPinned((v) => !v)}
        onFocus={() => setHovered(true)}
        onBlur={() => setHovered(false)}
        aria-expanded={hasDetail ? open : undefined}
        className={cn(
          'flex w-full items-start gap-2 text-left focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
          hasDetail ? 'cursor-pointer' : 'cursor-default',
        )}
      >
        {summary}
      </button>

      {hasDetail && (
        <div
          className={cn(
            'grid transition-[grid-template-rows,opacity] duration-200 ease-out',
            open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
          )}
        >
          <div className="overflow-hidden">
            <div className="pt-1 text-xs leading-relaxed text-muted-foreground">{detail}</div>
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * The one-line trace of what is behind a Disclose, shown while it is shut.
 * Keeping a clamped preview means the page still reads as informative when
 * nothing is hovered, which a bare chevron does not.
 */
export function DiscloseHint({ children }: { children: ReactNode }) {
  return (
    <span className="line-clamp-1 text-xs text-muted-foreground/80 group-hover/disclose:opacity-0 transition-opacity">
      {children}
    </span>
  )
}
