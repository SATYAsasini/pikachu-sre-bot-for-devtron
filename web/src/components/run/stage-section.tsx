import { useState, type ReactNode } from 'react'
import { ChevronRight, MinusCircle } from 'lucide-react'
import { cn } from 'cn'
import { Chip, type Tone } from '@/components/common/status'
import { Text } from '@/components/common/text'
import type { StageState } from '@/lib/run-derive'

const STATE_TONE: Record<StageState, Tone> = {
  waiting: 'neutral',
  running: 'warn',
  done: 'ok',
  failed: 'bad',
  skipped: 'unknown',
  not_run: 'neutral',
}

/**
 * One stage, one line — until you ask for more.
 *
 * The page used to mount all three full outputs stacked, which is roughly
 * nine screens of correct, well-structured material that nobody scrolled
 * through: the conclusion was at the bottom and the first thing you read was
 * a first pass we had already decided was wrong.
 *
 * So each stage is a single summarised line, and the working is one click
 * away. The stage that is currently producing stays open — while a run is in
 * flight, watching it is the point — and closes itself once the next stage
 * takes over, which means the page settles rather than growing.
 */
export function StageSection({
  id,
  index,
  title,
  summary,
  state,
  icon,
  open: openDefault,
  children,
}: {
  /** Anchor target for the rail on the left. */
  id: string
  index: number
  title: string
  /** The single line this stage reduces to when shut. */
  summary: ReactNode
  state: StageState
  icon: ReactNode
  /** Whether it starts open. Overridden the moment the reader clicks. */
  open: boolean
  children: ReactNode
}) {
  const [override, setOverride] = useState<boolean | null>(null)
  const open = override ?? openDefault
  const headingId = `${id}-heading`

  return (
    <section
      id={id}
      aria-labelledby={headingId}
      className={cn(
        'scroll-mt-32 overflow-hidden rounded-xl border bg-card shadow-card transition-colors',
        state === 'running' ? 'border-accent-strong/35' : 'border-border',
        (state === 'skipped' || state === 'not_run') && 'border-dashed bg-transparent',
      )}
    >
      <h2 id={headingId}>
        <button
          type="button"
          onClick={() => setOverride(!open)}
          aria-expanded={open}
          className={cn(
            'flex w-full items-center gap-2 px-3 py-2 text-left transition-colors',
            'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
            open ? 'border-b border-border' : 'hover:bg-well/60',
          )}
        >
          <ChevronRight
            aria-hidden
            className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')}
          />
          <span className="tabular shrink-0 text-[0.625rem] text-muted-foreground">{index}</span>
          <span className="shrink-0 text-muted-foreground">
            {state === 'skipped' || state === 'not_run' ? <MinusCircle aria-hidden className="size-3.5" /> : icon}
          </span>
          <span
            className={cn(
              'shrink-0 text-xs font-semibold tracking-tight',
              state === 'skipped' || state === 'not_run' ? 'text-muted-foreground' : 'text-foreground',
            )}
          >
            {title}
          </span>

          {/* The whole stage, in one clause. This is the line that has to be
              worth reading on its own. */}
          <span className="min-w-0 flex-1 truncate">
            <Text tone="fine" as="span">
              {summary}
            </Text>
          </span>

          {state === 'running' ? (
            <Chip tone={STATE_TONE[state]} className="shrink-0">
              running
            </Chip>
          ) : null}
        </button>
      </h2>

      {/* Unmounted when shut rather than hidden: these panels each run their
          own state, and three of them re-rendering off the stream for nobody
          to look at is work the page does not need to do. */}
      {open ? children : null}
    </section>
  )
}
