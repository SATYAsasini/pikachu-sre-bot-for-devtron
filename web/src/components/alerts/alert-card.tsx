import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { AlertTriangle, ChevronDown, CircleCheck, Maximize2 } from 'lucide-react'
import { cn } from 'cn'
import { Chip, Dot } from '@/components/common/status'
import { relativeTime } from '@/lib/format'
import { isFiring, isResolved, severityTone, stateTone } from '@/lib/tone'
import { alertMeta, hiddenMetaCount } from '@/lib/alert-meta'
import type { Alert } from '@/lib/types'
import type { PriorRun } from '@/lib/prior-runs'

/**
 * One alert, compact.
 *
 * Small on purpose: a hundred of these have to fit on a page where the point
 * is to scan for the one that matters. Everything beyond the name, its
 * severity and the labels that distinguish it lives behind the expand, so a
 * long list stays a list rather than becoming a document.
 */
export function AlertCard({
  alert,
  onExpand,
  active,
  prior,
}: {
  alert: Alert
  onExpand: (alert: Alert) => void
  active?: boolean
  /** A finished run that already answered this alert, if there is one. */
  prior?: PriorRun
}) {
  const meta = alertMeta(alert, 2)
  const hidden = hiddenMetaCount(alert, meta.length)

  return (
    <div
      className={cn(
        'overflow-hidden rounded-xl border bg-card shadow-card transition-all',
        'hover:-translate-y-px hover:shadow-raised',
        active
          ? 'border-accent-strong/50 ring-[3px] ring-accent-strong/15'
          : prior
            ? 'border-ok/35'
            : 'border-border hover:border-accent-strong/35',
      )}
    >
    <button
      type="button"
      onClick={() => onExpand(alert)}
      aria-label={`${alert.name}. Open details.`}
      className={cn(
        'group relative flex w-full items-start gap-2.5 px-3 py-2.5 text-left transition-colors',
        'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
        active && 'bg-accent-strong/5',
      )}
    >
      {/* One dot, three meanings, in the order an operator cares about:
          red still firing, orange on its way, green either resolved by the
          cluster or already answered by us. "Debugged" counts as green because
          the question this list answers is "what still needs me". */}
      <Dot
        tone={prior || isResolved(alert.state) ? 'ok' : stateTone(alert.state)}
        pulse={isFiring(alert.state) && !prior}
        className="mt-1.5 shrink-0"
      />

      <span className="min-w-0 flex-1">
        <span className="flex items-baseline gap-1.5">
          <span className="truncate text-xs font-medium">{alert.name}</span>
          <Chip tone={severityTone(alert.severity)} className="shrink-0">
            {alert.severity}
          </Chip>
        </span>

        <span className="mt-1 flex flex-wrap items-center gap-1">
          {meta.length === 0 ? (
            <span className="text-[0.625rem] text-muted-foreground/70">no labels</span>
          ) : (
            meta.map((m) => (
              <span
                key={m.key}
                className="inline-flex max-w-[11rem] items-baseline gap-1 rounded border border-border/70 bg-well px-1 font-mono text-[0.625rem]"
              >
                <span className="shrink-0 opacity-55">{m.key}</span>
                <span className="truncate">{m.value}</span>
              </span>
            ))
          )}
          {hidden > 0 && <span className="text-[0.625rem] text-muted-foreground/60">+{hidden}</span>}
        </span>

        <span className="mt-1 block text-[0.625rem] text-muted-foreground/70">
          {relativeTime(alert.startsAt)}
        </span>
      </span>

      {/* The expand affordance appears on approach rather than sitting there
          on every one of a hundred rows. */}
      <Maximize2
        aria-hidden
        className="mt-0.5 size-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
      />
    </button>

      {prior ? <Answered prior={prior} /> : null}
    </div>
  )
}

/**
 * This one has already been answered.
 *
 * A firing alert does not stop firing because somebody looked at it, so the
 * row stays on this page looking exactly as unhandled as its neighbours — and
 * the obvious thing to do with an unhandled row is hit Debug, which spends a
 * Devtron first pass and two models re-deriving a conclusion already on disk.
 *
 * One paragraph of what it was, the first action if one was proposed, and how
 * far the run got. Enough to decide whether to open it or run it again, and
 * not so much that it competes with the alert above it.
 */
function Answered({ prior }: { prior: PriorRun }) {
  const [open, setOpen] = useState(false)

  return (
    <div className="border-t border-ok/20 bg-ok/5">
      {/* Shut by default. The root cause is a paragraph, and a paragraph on
          every answered row turns a list of alerts back into an essay — which
          is the thing this list exists not to be. */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left transition-colors hover:bg-ok/10 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <CircleCheck aria-hidden className="size-3 shrink-0 text-ok" />
        <span className="text-[0.625rem] font-semibold tracking-widest text-ok uppercase">Already investigated</span>
        <span className="text-[0.625rem] text-muted-foreground">
          {relativeTime(prior.run.finishedAt ?? prior.run.createdAt)}
        </span>
        <span className="ml-auto shrink-0 text-[0.625rem] text-muted-foreground">
          {prior.settledEarly ? `${prior.stagesRan} of 3 · settled early` : `${prior.stagesRan} of 3 stages`}
        </span>
        <ChevronDown
          aria-hidden
          className={cn('size-3 shrink-0 text-muted-foreground transition-transform', open && 'rotate-180')}
        />
      </button>

      {open ? (
        <div className="space-y-1.5 px-3 pb-2">
          <p className="text-[0.6875rem] leading-relaxed">{prior.answer}</p>

          {prior.action ? (
            <div className="flex items-start gap-1.5">
              <Chip tone="accent" className="mt-px shrink-0">
                do first
              </Chip>
              <span className="min-w-0 flex-1 text-[0.6875rem] text-muted-foreground">{prior.action}</span>
            </div>
          ) : null}

          <Link
            to="/runs/$runId"
            params={{ runId: prior.run.id }}
            className="inline-flex items-center gap-1 text-[0.6875rem] font-medium text-accent-strong hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            Open the full run →
          </Link>
        </div>
      ) : null}
    </div>
  )
}

/** Header for a group of alerts that share a rule name. */
export function AlertGroupLabel({ name, count }: { name: string; count: number }) {
  return (
    <div className="col-span-full mt-1 flex items-baseline gap-2 first:mt-0">
      <AlertTriangle aria-hidden className="size-3 shrink-0 text-muted-foreground" />
      <span className="truncate text-[0.6875rem] font-medium">{name}</span>
      <span className="text-[0.625rem] text-muted-foreground">×{count}</span>
      <span className="h-px flex-1 bg-border" />
    </div>
  )
}
