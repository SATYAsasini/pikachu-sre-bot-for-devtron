import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Check, ChevronDown, Hand, RotateCcw, ShieldAlert } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Chip, Dot, type Tone } from '@/components/common/status'
import { Text } from '@/components/common/text'
import { Markdown } from '@/components/common/markdown'
import { api, errorMessage } from '@/lib/api'
import { qk } from '@/lib/queries'
import { relativeTime } from '@/lib/format'
import { alertMeta } from '@/lib/alert-meta'
import type { AlertLogEntry, AlertState, Priority, TrackedAlert } from '@/lib/types'

const PRIORITY_TONE: Record<Priority, Tone> = { P0: 'bad', P1: 'warn', P2: 'neutral' }
const STATE_TONE: Record<AlertState, Tone> = { firing: 'bad', acknowledged: 'warn', resolved: 'ok' }

/**
 * One alert we own.
 *
 * The row leads with what it turned out to be, not with what fired. An alert
 * whose investigation concluded "this is a monitoring defect, not an outage"
 * is a different object from one nobody has looked at, and a list that shows
 * them identically is a list you have to open every row of.
 *
 * `seenCount` is on the row for the same reason it exists at all: "firing
 * 6,048 times over 23 days" and "fired once ten minutes ago" demand different
 * responses, and without dedup they look the same.
 */
export function IncidentRow({ alert }: { alert: TrackedAlert }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)

  const update = useMutation({
    mutationFn: (body: { state?: AlertState; notes?: string }) => api.updateIncident(alert.id, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['incidents'] })
      void qc.invalidateQueries({ queryKey: qk.incident(alert.id) })
    },
  })

  const meta = alertMeta(
    {
      ...alert,
      state: alert.state,
      startsAt: alert.firstSeen,
      fingerprint: '',
      source: '',
      expression: '',
      description: '',
      annotations: null,
      summary: alert.summary ?? '',
      labels: alert.labels ?? null,
      severity: alert.severity ?? '',
      namespace: alert.namespace ?? '',
      kind: alert.kind ?? '',
      resource: alert.resource ?? '',
      name: alert.name,
    },
    3,
  )

  const finding = alert.latest
  const resolved = alert.state === 'resolved'

  return (
    <li
      className={cn(
        'overflow-hidden rounded-xl border bg-card shadow-card transition-all',
        resolved ? 'border-border opacity-80' : alert.priority === 'P0' ? 'border-bad/35' : 'border-border',
      )}
    >
      <div className="flex min-w-0 items-start gap-2.5 px-3 py-2.5">
        <Dot tone={STATE_TONE[alert.state]} pulse={alert.state === 'firing'} className="mt-1.5 shrink-0" />

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Chip tone={PRIORITY_TONE[alert.priority]}>{alert.priority}</Chip>
            {/* The handle. Everything else about this row can be described
                two ways; the number cannot. */}
            <span className="shrink-0 font-mono text-[0.625rem] text-muted-foreground">#{alert.seq}</span>
            <span className={cn('truncate text-xs font-semibold', resolved && 'line-through opacity-70')}>
              {alert.name}
            </span>
            <Chip tone={STATE_TONE[alert.state]}>{alert.state}</Chip>
            {/* Where it came from. An alert a rule claimed by itself and one
                a person clicked Debug on carry different expectations about
                who is already looking. */}
            <span title={alert.origin === 'rule' ? 'An alert rule on this cluster claimed it' : 'Somebody claimed it'}>
              <Chip tone={alert.origin === 'rule' ? 'accent' : 'neutral'}>
                {alert.origin === 'rule' ? 'by rule' : 'by hand'}
              </Chip>
            </span>
          </div>

          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
            {meta.map((m) => (
              <span
                key={m.key}
                className="inline-flex max-w-[12rem] items-baseline gap-1 rounded border border-border/70 bg-well px-1 font-mono text-[0.625rem]"
              >
                <span className="shrink-0 opacity-55">{m.key}</span>
                <span className="truncate">{m.value}</span>
              </span>
            ))}
            {/* The count is the whole reason this is an entity and not a feed
                row. 6,048 sightings and one sighting need different answers. */}
            <span className="text-[0.625rem] text-muted-foreground">
              {alert.seenCount > 1 ? `seen ${alert.seenCount.toLocaleString()}× · ` : ''}
              since {relativeTime(alert.firstSeen)}
            </span>
          </div>

          {/* The conclusion, on the row. Opening something to find out whether
              it was ever answered defeats the point of the dashboard. */}
          {finding ? (
            <div
              className={cn(
                'mt-2 rounded-lg border px-2.5 py-1.5',
                finding.unverified ? 'border-warn/30 bg-warn/5' : 'border-ok/25 bg-ok/5',
              )}
            >
              <div className="flex flex-wrap items-center gap-1.5">
                {finding.unverified ? (
                  <ShieldAlert aria-hidden className="size-3 shrink-0 text-warn" />
                ) : (
                  <Check aria-hidden className="size-3 shrink-0 text-ok" />
                )}
                <span
                  className={cn(
                    'text-[0.625rem] font-semibold tracking-widest uppercase',
                    finding.unverified ? 'text-warn' : 'text-ok',
                  )}
                >
                  {finding.unverified ? 'Unverified first pass' : finding.agrees ? 'Root cause' : 'Corrected cause'}
                </span>
                <Link
                  to="/runs/$runId"
                  params={{ runId: finding.runId }}
                  className="ml-auto inline-flex items-center gap-0.5 text-[0.625rem] text-muted-foreground hover:text-foreground"
                >
                  open the run
                  <ArrowRight aria-hidden className="size-2.5" />
                </Link>
              </div>
              <p className="mt-0.5 line-clamp-2 text-[0.6875rem] leading-relaxed">{finding.rootCause}</p>
              {finding.action ? (
                <div className="mt-1 flex items-start gap-1.5">
                  <Chip tone="accent" className="mt-px shrink-0">
                    do first
                  </Chip>
                  <span className="line-clamp-1 min-w-0 text-[0.6875rem] text-muted-foreground">{finding.action}</span>
                </div>
              ) : null}
            </div>
          ) : (
            <Text tone="fine" className="mt-1.5">
              Not investigated yet.
            </Text>
          )}
        </div>

        <div className="flex shrink-0 items-center gap-1">
          {alert.state === 'firing' ? (
            <Button
              size="xs"
              variant="outline"
              onClick={() => update.mutate({ state: 'acknowledged' })}
              disabled={update.isPending}
              title="I am on this"
            >
              <Hand aria-hidden className="size-3" />
              Ack
            </Button>
          ) : null}
          {!resolved ? (
            <Button
              size="xs"
              variant="outline"
              onClick={() => update.mutate({ state: 'resolved' })}
              disabled={update.isPending}
            >
              <Check aria-hidden className="size-3" />
              Resolve
            </Button>
          ) : (
            <Button
              size="xs"
              variant="ghost"
              onClick={() => update.mutate({ state: 'firing' })}
              disabled={update.isPending}
              title="Reopen"
            >
              <RotateCcw aria-hidden className="size-3" />
            </Button>
          )}
          <Button size="icon-xs" variant="ghost" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
            <ChevronDown aria-hidden className={cn('transition-transform', open && 'rotate-180')} />
          </Button>
        </div>
      </div>

      {open ? <Detail alert={alert} onNotes={(notes) => update.mutate({ notes })} saving={update.isPending} error={update.isError ? errorMessage(update.error) : undefined} /> : null}
    </li>
  )
}

/** Notes, the full finding, every run against this alert, and what happened to it. */
function Detail({
  alert,
  onNotes,
  saving,
  error,
}: {
  alert: TrackedAlert
  onNotes: (notes: string) => void
  saving: boolean
  error?: string
}) {
  const [notes, setNotes] = useState(alert.notes ?? '')
  const dirty = notes !== (alert.notes ?? '')

  // The list read leaves out the runs and the timeline — both are joins, and
  // paying for them on every row to show them on one is how a dashboard with
  // fifty alerts takes four seconds. They are fetched when the row is opened.
  const full = useQuery({
    queryKey: qk.incident(alert.id),
    queryFn: () => api.incident(alert.id),
    staleTime: 15_000,
  })
  const runs = full.data?.alert.runs ?? alert.runs ?? []
  const timeline = full.data?.timeline ?? []

  return (
    <div className="space-y-3 border-t border-border bg-well/50 px-3 py-2.5">
      {alert.summary ? <Text tone="muted">{alert.summary}</Text> : null}

      {alert.latest?.rootCause ? (
        <div>
          <Text tone="label">The finding, in full</Text>
          <div className="mt-1 rounded-lg border border-border bg-card px-2.5 py-2">
            <Markdown tight>{alert.latest.rootCause}</Markdown>
          </div>
        </div>
      ) : null}

      <div>
        <Text tone="label">Notes</Text>
        {/* Free text, because half of incident handling is context that fits
            no schema: who was deploying, what was tried, why it was left. */}
        <textarea
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          rows={2}
          placeholder="What you tried, what you know, what the next person needs."
          className="mt-1 w-full resize-none rounded-lg border border-border bg-card px-2 py-1.5 text-xs focus:border-accent-strong/50 focus:ring-[3px] focus:ring-ring/30 focus:outline-none"
        />
        <div className="mt-1 flex items-center gap-2">
          <Button size="xs" onClick={() => onNotes(notes)} disabled={!dirty || saving}>
            {saving ? 'Saving…' : 'Save notes'}
          </Button>
          {error ? (
            <Text tone="fine" as="span" className="text-bad">
              {error}
            </Text>
          ) : null}
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <Text tone="label">Investigations</Text>
          {runs.length === 0 ? (
            <Text tone="fine" className="mt-1">
              {full.isPending ? 'Loading…' : 'Nobody has looked at this one yet.'}
            </Text>
          ) : (
            <ul className="mt-1 space-y-1">
              {runs.map((r) => (
                <li key={r.runId}>
                  <Link
                    to="/runs/$runId"
                    params={{ runId: r.runId }}
                    className="flex items-center gap-2 rounded-md border border-border bg-card px-2 py-1 transition-colors hover:border-accent-strong/40"
                  >
                    <Chip tone={r.status === 'succeeded' ? 'ok' : r.status === 'partial' ? 'warn' : 'neutral'}>
                      {r.status}
                    </Chip>
                    <span className="font-mono text-[0.625rem] text-muted-foreground">{r.runId.slice(0, 8)}</span>
                    <span className="ml-auto text-[0.625rem] text-muted-foreground">{relativeTime(r.createdAt)}</span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <Text tone="label">What happened</Text>
          {/* Including the non-events. "We did not notify because this was a
              duplicate" is the question people actually ask afterwards. */}
          {timeline.length === 0 ? (
            <Text tone="fine" className="mt-1">
              {full.isPending ? 'Loading…' : 'Nothing recorded.'}
            </Text>
          ) : (
            <ol
              data-lenis-prevent
              className="mt-1 max-h-40 space-y-1 overflow-y-auto rounded-lg border border-border bg-card px-2 py-1.5"
            >
              {timeline.slice(-20).reverse().map((e) => (
                <TimelineLine key={e.id} entry={e} />
              ))}
            </ol>
          )}
        </div>
      </div>
    </div>
  )
}

const LOG_LABEL: Record<string, string> = {
  tracked: 'became ours',
  seen: 'fired again',
  run_started: 'investigation opened',
  run_finished: 'investigation finished',
  run_suppressed: 'second run skipped',
  acknowledged: 'acknowledged',
  resolved: 'resolved',
  reopened: 'reopened',
  noted: 'notes changed',
  notified: 'notified',
  notify_skipped: 'notification skipped',
}

function TimelineLine({ entry }: { entry: AlertLogEntry }) {
  return (
    <li className="flex min-w-0 items-baseline gap-1.5 text-[0.6875rem]">
      <span className="shrink-0 font-medium">{LOG_LABEL[entry.kind] ?? entry.kind}</span>
      {entry.detail ? <span className="truncate text-muted-foreground">{entry.detail}</span> : null}
      <span className="ml-auto shrink-0 text-[0.625rem] text-muted-foreground">{relativeTime(entry.at)}</span>
    </li>
  )
}
