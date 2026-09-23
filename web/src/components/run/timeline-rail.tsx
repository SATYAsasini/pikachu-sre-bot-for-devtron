import { useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Pin, PinOff, Terminal } from 'lucide-react'
import { cn } from 'cn'
import { Timeline } from '@/components/run/timeline'
import { describeEvent } from '@/lib/run-derive'
import type { RunEvent } from '@/lib/types'

const DOT_TONE = {
  neutral: 'bg-muted-foreground/40',
  accent: 'bg-foreground/50',
  good: 'bg-ok',
  warn: 'bg-warn',
  bad: 'bg-bad',
} as const

/**
 * The tool call log, out of the way until you want it.
 *
 * It used to hold a permanent 380px column next to the answer — eighty rows of
 * `prom_query {"expr":…}` competing with the thing you actually came to read.
 * The log matters when you are auditing how a conclusion was reached, and not
 * at all when you are acting on it, so it collapses to a spine of coloured
 * ticks and opens on approach.
 *
 * Pinning exists because "appears on hover" is hostile if you are genuinely
 * reading it.
 */
export function TimelineRail({
  events,
  loading = false,
  live,
}: {
  events: RunEvent[]
  loading?: boolean
  live?: boolean
}) {
  const still = useReducedMotion()
  const [hovered, setHovered] = useState(false)
  const [pinned, setPinned] = useState(false)
  const open = hovered || pinned

  const failed = events.filter((e) => describeEvent(e).tone === 'bad').length
  const tools = events.filter((e) => e.type === 'tool_call').length

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocusCapture={() => setHovered(true)}
      onBlurCapture={() => setHovered(false)}
      className="relative shrink-0"
      style={{ width: pinned ? 380 : 40 }}
    >
      {/* The spine: one tick per event, coloured by outcome. Enough to see
          that something failed without reading anything. */}
      <button
        type="button"
        onClick={() => setPinned((v) => !v)}
        aria-expanded={open}
        aria-label={pinned ? 'Unpin the tool log' : `Tool log: ${tools} calls, ${failed} failed. Pin open.`}
        className={cn(
          'sticky top-32 flex w-10 flex-col items-center gap-1.5 rounded-md border py-2 transition-colors',
          'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
          open ? 'border-border bg-card' : 'border-transparent hover:border-border hover:bg-card/60',
        )}
      >
        <Terminal aria-hidden className="size-3.5 text-muted-foreground" />
        <span className="tabular text-[0.625rem] text-muted-foreground">{tools}</span>
        {failed > 0 && <span className="tabular text-[0.625rem] font-medium text-bad">{failed}✗</span>}

        <span className="flex w-full flex-col items-center gap-px px-2 pt-1">
          {events.slice(-40).map((e) => (
            <span
              key={e.seq}
              className={cn('h-px w-full rounded-full', DOT_TONE[describeEvent(e).tone])}
            />
          ))}
        </span>

        {live && <span className="mt-1 size-1.5 animate-pulse rounded-full bg-ok" aria-hidden />}
        {pinned ? (
          <PinOff aria-hidden className="mt-1 size-3 text-muted-foreground" />
        ) : (
          <Pin aria-hidden className="mt-1 size-3 text-muted-foreground/40" />
        )}
      </button>

      {/* The log itself, floated over the page so opening it never reflows
          what you were reading. */}
      <AnimatePresence>
        {open && !pinned && (
          <motion.div
            initial={still ? false : { opacity: 0, x: 12 }}
            animate={{ opacity: 1, x: 0 }}
            exit={still ? undefined : { opacity: 0, x: 12, transition: { duration: 0.12 } }}
            transition={{ duration: 0.18, ease: [0.16, 1, 0.3, 1] }}
            className="absolute top-32 right-0 z-30 w-[24rem] shadow-xl"
          >
            <Timeline events={events} loading={loading} live={live ?? false} />
          </motion.div>
        )}
      </AnimatePresence>

      {pinned && (
        <div className="sticky top-32 -mt-[8.5rem] pl-11">
          <Timeline events={events} loading={loading} live={live ?? false} />
        </div>
      )}
    </div>
  )
}
