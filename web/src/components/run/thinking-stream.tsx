import { useEffect, useRef } from 'react'
import { Brain } from 'lucide-react'
import { cn } from 'cn'
import { pluralise } from '@/lib/format'

/** How many lines the window holds. Enough to read, short enough to skim past. */
const WINDOW = 6

/**
 * Devtron's thinking, as a stream rather than a transcript.
 *
 * This was a collapsible list of all twenty-nine lines, which is a wall — and
 * a repetitive one, since half of them are "Gathering data from the cluster…".
 * Nobody reads a thinking trail line by line after the fact; they watch it
 * move while they wait, and afterwards they want to know it happened.
 *
 * So it is a fixed six-line window that scrolls itself as lines land, masked
 * top and bottom so text fades out rather than being cut. The count in the
 * header is the durable fact; the window is the texture.
 */
export function ThinkingStream({
  lines,
  total,
  live,
  className,
}: {
  lines: string[]
  /** What Devtron reported, which can exceed what the ledger kept. */
  total: number
  /** Still arriving. Drives the pulse and the auto-scroll. */
  live?: boolean
  className?: string
}) {
  const scroller = useRef<HTMLDivElement>(null)

  // Pin to the newest line. No "am I at the bottom" check: this window is six
  // lines tall and exists to show the latest, so it always follows.
  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines.length])

  if (total === 0) return null

  return (
    <div className={cn('overflow-hidden rounded-lg border border-border bg-well', className)}>
      <div className="flex items-center gap-1.5 border-b border-border px-2.5 py-1.5">
        <Brain aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="text-xs font-medium">Thinking trail</span>
        <span className="tabular text-[0.6875rem] text-muted-foreground">{pluralise(total, 'step')}</span>
        {live ? (
          <span aria-hidden className="relative ml-auto flex size-1.5 shrink-0">
            <span className="absolute inline-flex size-full animate-ping rounded-full bg-accent-strong opacity-70" />
            <span className="relative inline-flex size-1.5 rounded-full bg-accent-strong" />
          </span>
        ) : null}
      </div>

      {lines.length === 0 ? (
        <p className="px-2.5 py-2 text-[0.6875rem] leading-relaxed text-muted-foreground">
          Devtron counted {pluralise(total, 'step')} but did not keep the text of them for this run.
        </p>
      ) : (
        <div
          ref={scroller}
          // Lenis owns the wheel at the root; without this the window cannot
          // be scrolled back by hand.
          data-lenis-prevent
          aria-live={live ? 'polite' : 'off'}
          className="overflow-y-auto px-2.5 py-1.5"
          style={{
            // Six lines at the line-height below. A fixed height rather than
            // max-height so the block does not jump as lines arrive.
            height: `${WINDOW * 1.45}rem`,
            // Fade rather than cut. The top edge is what makes it read as a
            // stream with history above it instead of a clipped list.
            WebkitMaskImage: 'linear-gradient(to bottom, transparent 0%, #000 18%, #000 84%, transparent 100%)',
            maskImage: 'linear-gradient(to bottom, transparent 0%, #000 18%, #000 84%, transparent 100%)',
          }}
        >
          <ol>
            {lines.map((line, i) => (
              <li
                key={i}
                className={cn(
                  'truncate text-[0.6875rem] leading-[1.45rem] text-muted-foreground',
                  // The newest line is the one being read.
                  i === lines.length - 1 && 'text-foreground',
                )}
                title={line}
              >
                {line}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  )
}
