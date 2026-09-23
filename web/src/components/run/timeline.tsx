import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownToLine, Terminal } from 'lucide-react'
import { cn } from 'cn'
import { Panel, PanelHeader } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { EmptyState } from '@/components/common/empty-state'
import { TimelineSkeleton } from '@/components/common/skeletons'
import { clockTime } from '@/lib/format'
import {
  TIMELINE_FILTERS,
  describeEvent,
  filterEvents,
  gapMs,
  payloadText,
  tokenSeries,
  type TimelineFilter,
} from '@/lib/run-derive'
import type { RunEvent } from '@/lib/types'

const AGENT_TONE: Record<string, string> = {
  judge: 'text-warn',
  sre: 'text-ok',
  intelligence: 'text-unknown',
}

const DOT_TONE = {
  neutral: 'bg-muted-foreground/50',
  accent: 'bg-foreground/60',
  good: 'bg-ok',
  warn: 'bg-warn',
  bad: 'bg-bad',
} as const

interface Row {
  /** The last event of the run, which is the one worth showing. */
  e: RunEvent
  /** How many identical events it stands for. 1 for an ordinary row. */
  repeat: number
  /** The event the run started at, for the elapsed-time column. */
  first: RunEvent
}

/**
 * Consecutive identical events, folded into one row.
 *
 * Devtron's first pass emits one `intelligence_thinking` per thought and most
 * of them carry no text, so a real run pushes twenty-five byte-identical rows
 * through the log and everything either side of them scrolls out of reach.
 * They are still counted — `×25` is the useful part — but they take one line.
 */
function collapseRuns(events: RunEvent[]): Row[] {
  const rows: Row[] = []
  for (const e of events) {
    const prev = rows[rows.length - 1]
    const same =
      prev !== undefined &&
      prev.e.type === e.type &&
      prev.e.agent === e.agent &&
      describeEvent(prev.e).detail === describeEvent(e).detail
    if (same) {
      prev.e = e
      prev.repeat += 1
    } else {
      rows.push({ e, repeat: 1, first: e })
    }
  }
  return rows
}

/**
 * The ledger, live.
 *
 * Deliberately a single scrolling column of one-line rows rather than an
 * expandable tree: this is the thing you watch out of the corner of your eye
 * while reading the panels, and it has to stay scannable at a glance.
 *
 * Auto-scroll releases the moment you scroll up, and a button brings it back —
 * nothing is more annoying than a log that yanks you away mid-read.
 */
export function Timeline({ events, loading, live }: { events: RunEvent[]; loading: boolean; live: boolean }) {
  const [expanded, setExpanded] = useState<number | null>(null)
  const tokenTotals = useMemo(() => tokenSeries(events), [events])
  const [filter, setFilter] = useState<TimelineFilter>('all')
  const [pinned, setPinned] = useState(true)
  const scroller = useRef<HTMLDivElement>(null)

  const shown = useMemo(() => collapseRuns(filterEvents(events, filter)), [events, filter])

  useLayoutEffect(() => {
    if (!pinned) return
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [shown.length, pinned])

  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const onScroll = () => {
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24
      setPinned(atBottom)
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  return (
    <Panel className="flex max-h-[calc(100svh-8rem)] flex-col lg:sticky lg:top-28">
      <PanelHeader
        title="Timeline"
        icon={<Terminal aria-hidden className="size-3.5" />}
        actions={
          <div className="flex items-center gap-0.5 rounded-md border border-border p-0.5">
            {TIMELINE_FILTERS.map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setFilter(f)}
                className={cn(
                  'rounded px-1.5 py-0.5 text-[0.625rem] font-medium capitalize transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                  filter === f ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {f}
              </button>
            ))}
          </div>
        }
      />

      <div ref={scroller} data-lenis-prevent className="relative min-h-0 flex-1 overflow-y-auto">
        {loading && events.length === 0 ? (
          <TimelineSkeleton />
        ) : shown.length === 0 ? (
          <EmptyState
            icon={Terminal}
            title={events.length === 0 ? 'Nothing on the wire yet' : 'Nothing matches that filter'}
            line={
              events.length === 0
                ? live
                  ? 'The run has not emitted an event yet. It will start any second now.'
                  : 'This run finished without writing anything to the ledger, which is unusual enough to be worth reporting.'
                : 'Try "all" — the interesting ones hide in the other bucket surprisingly often.'
            }
          />
        ) : (
          <ol className="py-1">
            {shown.map((row, i) => {
              const e = row.e
              const view = describeEvent(e)
              const gap = gapMs(shown[i - 1]?.e, row.first)
              const tokens = tokenTotals.get(e.seq) ?? 0
              const prevTokens = i > 0 ? (tokenTotals.get(shown[i - 1].e.seq) ?? 0) : 0
              const spent = tokens - prevTokens
              const open = expanded === e.seq
              return (
                <li key={e.seq} className="border-b border-border/40 last:border-0">
                  <button
                    type="button"
                    onClick={() => setExpanded(open ? null : e.seq)}
                    aria-expanded={open}
                    className="flex w-full items-baseline gap-2 px-2.5 py-[3px] text-left font-mono text-[0.6875rem] leading-relaxed hover:bg-accent/40 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                  >
                    <span className="tabular shrink-0 text-muted-foreground/70">{clockTime(e.at)}</span>
                    {/* Elapsed since the previous step. A run's cost is mostly
                        a few slow steps, and this is where they show up. */}
                    <span className="tabular w-11 shrink-0 text-right text-muted-foreground/60">
                      {gap >= 1000 ? `+${(gap / 1000).toFixed(1)}s` : gap > 0 ? `+${gap}ms` : ''}
                    </span>
                    <span className={cn('mt-[0.35rem] size-1.5 shrink-0 rounded-full', DOT_TONE[view.tone])} />
                    {/* Fixed and truncating. "intelligence" is thirteen
                        characters and used to run straight over the label
                        beside it. */}
                    <span
                      className={cn(
                        'w-[4.5rem] shrink-0 truncate',
                        e.agent ? (AGENT_TONE[e.agent] ?? 'text-muted-foreground') : '',
                      )}
                      title={e.agent || undefined}
                    >
                      {e.agent}
                    </span>
                    <span className="max-w-[9rem] shrink-0 truncate font-medium">{view.label}</span>
                    {row.repeat > 1 ? (
                      <span className="tabular shrink-0 rounded bg-muted px-1 text-[0.625rem] text-muted-foreground">
                        ×{row.repeat}
                      </span>
                    ) : null}
                    {view.detail ? (
                      <span className={cn('min-w-0 flex-1 text-muted-foreground', open ? 'whitespace-pre-wrap' : 'truncate')}>
                        {view.detail}
                      </span>
                    ) : (
                      <span className="flex-1" />
                    )}
                    {spent > 0 && (
                      <span className="tabular shrink-0 text-accent-strong/80">+{spent.toLocaleString()}t</span>
                    )}
                  </button>

                  {/* The whole payload, on demand. Truncated JSON in a tooltip
                      was unreadable for anything with real arguments. */}
                  {open && (
                    <pre className="overflow-x-auto border-t border-border/40 bg-well px-2.5 py-2 font-mono text-[0.625rem] leading-relaxed text-muted-foreground">
                      <code>{payloadText(e)}</code>
                    </pre>
                  )}
                </li>
              )
            })}
          </ol>
        )}
      </div>

      <footer className="flex items-center justify-between gap-2 border-t border-border px-2.5 py-1.5">
        <span className="text-[0.625rem] text-muted-foreground">
          {shown.length === events.length
            ? `${events.length} events`
            : `${shown.length} rows · ${events.length} events`}
        </span>
        {pinned ? (
          live ? (
            <Chip tone="ok">following</Chip>
          ) : null
        ) : (
          <button
            type="button"
            onClick={() => {
              setPinned(true)
              const el = scroller.current
              if (el) el.scrollTop = el.scrollHeight
            }}
            className="inline-flex items-center gap-1 rounded border border-border bg-well px-1.5 py-0.5 text-[0.625rem] font-medium transition-colors hover:border-input focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <ArrowDownToLine aria-hidden className="size-3" />
            Jump to latest
          </button>
        )}
      </footer>
    </Panel>
  )
}
