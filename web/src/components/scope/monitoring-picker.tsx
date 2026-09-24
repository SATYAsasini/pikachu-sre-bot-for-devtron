import { useMemo, useState } from 'react'
import { BellRing, Gauge, Info, RefreshCw, RotateCcw, Sparkles } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Panel, PanelBody, PanelHeader, Well } from '@/components/common/panel'
import { Chip, type Tone } from '@/components/common/status'
import { Mono } from '@/components/common/mono'
import { Text } from '@/components/common/text'
import { ErrorState } from '@/components/common/error-state'
import { RowSkeleton } from '@/components/common/skeletons'
import { errorMessage } from '@/lib/api'
import { useChooseMonitoring, useMonitoringProbe } from '@/lib/queries'
import { relativeTime } from '@/lib/format'
import {
  monitoringHalf,
  samePick,
  type MonitoringEndpoint,
  type MonitoringHalf,
  type MonitoringPick,
  type MonitoringStack,
} from '@/lib/types'

/**
 * Which monitoring endpoints this cluster uses.
 *
 * Discovery finds them all; it cannot know which one you meant. A cluster
 * running both vmalert and an Alertmanager gets picked between by a name
 * heuristic, and until this screen existed there was no way to see that a
 * choice had been made, let alone disagree with it.
 *
 * So the whole candidate list is on screen, including the ones that did not
 * answer and the exporters that were never going to. Choosing is one click
 * and takes effect immediately — a stack choice has one visible consequence
 * and hiding it behind a save button at the top of a different tab would mean
 * a picker that appears to do nothing.
 */
export function MonitoringPicker({ clusterId, clusterName }: { clusterId: number; clusterName?: string }) {
  const q = useMonitoringProbe(clusterId)
  const { choose, reset } = useChooseMonitoring(clusterId, clusterName)
  const [busy, setBusy] = useState<MonitoringHalf | 'reset' | null>(null)

  const stack = q.data
  const pinned = stack?.chosen ?? null
  const anyPinned = !!(pinned?.metrics || pinned?.alerts)

  const { metrics, alerts } = useMemo(() => split(stack), [stack])

  const apply = (half: MonitoringHalf, pick: MonitoringPick | null) => {
    setBusy(half)
    choose.mutate(
      {
        metrics: half === 'metrics' ? pick : (pinned?.metrics ?? null),
        alerts: half === 'alerts' ? pick : (pinned?.alerts ?? null),
      },
      {
        onSuccess: (next) => {
          const chosen = half === 'metrics' ? next.metrics : next.alerts
          toast.success(pick ? 'Endpoint pinned' : 'Back to discovery', {
            description: chosen
              ? `${label(half)} now reads ${chosen.service.namespace}/${chosen.service.name}.`
              : `Nothing answered for ${label(half).toLowerCase()} on this cluster.`,
          })
        },
        onError: (e) => toast.error('Could not save that choice', { description: errorMessage(e) }),
        onSettled: () => setBusy(null),
      },
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Text tone="fine" as="span">
          {anyPinned
            ? 'Pinned. Discovery still runs, but these are the endpoints in use.'
            : 'On discovery. The best-ranked endpoint that answers wins.'}
        </Text>
        <div className="ml-auto flex items-center gap-1.5">
          {anyPinned ? (
            <Button
              size="sm"
              variant="outline"
              disabled={busy !== null}
              onClick={() => {
                setBusy('reset')
                reset.mutate(undefined, {
                  onSuccess: () => toast.success('Back to discovery for both halves'),
                  onError: (e) => toast.error('Could not reset', { description: errorMessage(e) }),
                  onSettled: () => setBusy(null),
                })
              }}
            >
              <RotateCcw aria-hidden className="size-3.5" />
              {busy === 'reset' ? 'Resetting…' : 'Reset to discovery'}
            </Button>
          ) : null}
          <Button size="sm" variant="outline" disabled={q.isFetching} onClick={() => void q.refetch()}>
            <RefreshCw aria-hidden className={cn('size-3.5', q.isFetching && 'animate-spin')} />
            {q.isFetching ? 'Probing…' : 'Re-probe'}
          </Button>
        </div>
      </div>

      {q.isPending ? (
        <Panel>
          <PanelBody>
            <RowSkeleton rows={5} />
          </PanelBody>
        </Panel>
      ) : q.isError ? (
        <ErrorState error={q.error} onRetry={() => void q.refetch()} />
      ) : (
        <>
          {stack?.partial ? (
            <Well className="border-warn/40">
              <Text tone="muted">
                This probe did not finish in time, so the list below is incomplete. It has not been cached —
                re-probe and it will pick up where it can.
              </Text>
            </Well>
          ) : null}

          <div className="grid gap-3 xl:grid-cols-2">
            <HalfPanel
              half="alerts"
              icon={<BellRing aria-hidden className="size-3.5" />}
              candidates={alerts}
              inUse={stack?.alerts ?? null}
              pick={pinned?.alerts ?? null}
              busy={busy === 'alerts'}
              disabled={busy !== null}
              onPick={(p) => apply('alerts', p)}
            />
            <HalfPanel
              half="metrics"
              icon={<Gauge aria-hidden className="size-3.5" />}
              candidates={metrics}
              inUse={stack?.metrics ?? null}
              pick={pinned?.metrics ?? null}
              busy={busy === 'metrics'}
              disabled={busy !== null}
              onPick={(p) => apply('metrics', p)}
            />
          </div>

          {(stack?.notes ?? []).length > 0 ? (
            <Panel>
              <PanelBody>
                <ul className="space-y-1">
                  {(stack?.notes ?? []).map((note, i) => (
                    <li key={i} className="flex items-start gap-1.5 text-[0.6875rem] leading-relaxed text-muted-foreground">
                      <Info aria-hidden className="mt-0.5 size-3 shrink-0 text-warn" />
                      <span>{note}</span>
                    </li>
                  ))}
                </ul>
              </PanelBody>
            </Panel>
          ) : null}

          {stack ? (
            <Text tone="fine">
              {stack.candidates?.length ?? 0} candidate Services, last probed {relativeTime(stack.discoveredAt)}.
            </Text>
          ) : null}
        </>
      )}
    </div>
  )
}

function HalfPanel({
  half,
  icon,
  candidates,
  inUse,
  pick,
  busy,
  disabled,
  onPick,
}: {
  half: MonitoringHalf
  icon: React.ReactNode
  candidates: MonitoringEndpoint[]
  inUse: MonitoringEndpoint | null
  pick: MonitoringPick | null
  busy: boolean
  disabled: boolean
  onPick: (pick: MonitoringPick | null) => void
}) {
  const [showTargets, setShowTargets] = useState(false)
  const real = candidates.filter((c) => !c.scrapeTarget)
  const targets = candidates.filter((c) => c.scrapeTarget)
  const reachable = real.filter((c) => c.reachable).length
  const name = `monitoring-${half}`

  return (
    <Panel>
      <PanelHeader
        icon={icon}
        title={label(half)}
        description={
          half === 'alerts'
            ? 'Where firing alerts are read from. Alertmanager or vmalert.'
            : 'Where PromQL is run. Prometheus, VictoriaMetrics, Thanos or Mimir.'
        }
        actions={
          candidates.length > 0 ? (
            <Chip tone={reachable > 0 ? 'ok' : 'warn'}>
              {reachable} of {real.length} answering
            </Chip>
          ) : null
        }
      />
      <PanelBody className="space-y-1.5">
        {candidates.length === 0 ? (
          <Text tone="fine">No candidate Services of this kind were found in the cluster.</Text>
        ) : (
          <>
            <AutoRow
              name={name}
              checked={pick === null}
              disabled={disabled}
              busy={busy && pick === null}
              inUse={pick === null ? inUse : null}
              onSelect={() => onPick(null)}
            />
            {real.map((c) => (
              <CandidateRow
                key={`${c.service.namespace}/${c.service.name}`}
                name={name}
                endpoint={c}
                checked={samePick(pick, c)}
                current={!!inUse && sameService(inUse, c)}
                disabled={disabled}
                busy={busy && samePick(pick, c)}
                onSelect={() => onPick({ namespace: c.service.namespace, name: c.service.name })}
              />
            ))}

            {/* The exporters matched the name filter and nothing else. There
                are usually more of them than there are real endpoints, and
                leaving them expanded buries the two rows that matter. They
                stay reachable in one click, because the list of what counts
                as an exporter is a heuristic and can be wrong about an
                install. */}
            {targets.length > 0 ? (
              <div className="pt-1">
                <button
                  type="button"
                  onClick={() => setShowTargets((v) => !v)}
                  aria-expanded={showTargets}
                  className="text-[0.6875rem] text-muted-foreground underline decoration-dotted underline-offset-4 hover:text-foreground"
                >
                  {showTargets ? 'Hide' : 'Show'} {targets.length} exporter{targets.length === 1 ? '' : 's'}
                </button>
                {showTargets ? (
                  <div className="mt-1.5 space-y-1.5">
                    <Text tone="fine">
                      These serve /metrics for something else to scrape rather than a query API of their own,
                      so they are not probed by default. Pin one anyway if this install is different.
                    </Text>
                    {targets.map((c) => (
                      <CandidateRow
                        key={`${c.service.namespace}/${c.service.name}`}
                        name={name}
                        endpoint={c}
                        checked={samePick(pick, c)}
                        current={!!inUse && sameService(inUse, c)}
                        disabled={disabled}
                        busy={busy && samePick(pick, c)}
                        onSelect={() => onPick({ namespace: c.service.namespace, name: c.service.name })}
                      />
                    ))}
                  </div>
                ) : null}
              </div>
            ) : null}
          </>
        )}
      </PanelBody>
    </Panel>
  )
}

/** "Let discovery decide", and what that currently resolves to. */
function AutoRow({
  name,
  checked,
  disabled,
  busy,
  inUse,
  onSelect,
}: {
  name: string
  checked: boolean
  disabled: boolean
  busy: boolean
  inUse: MonitoringEndpoint | null
  onSelect: () => void
}) {
  return (
    <Row checked={checked} disabled={disabled} name={name} onSelect={onSelect}>
      <span className="flex min-w-0 items-center gap-1.5">
        <Sparkles aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="text-xs font-semibold">Auto</span>
        {busy ? <Chip tone="neutral">saving…</Chip> : null}
      </span>
      <span className="mt-0.5 block text-[0.6875rem] text-muted-foreground">
        {inUse
          ? `Currently ${inUse.service.namespace}/${inUse.service.name} — best-ranked endpoint that answers.`
          : 'Best-ranked endpoint that answers. Re-evaluated on every walk.'}
      </span>
    </Row>
  )
}

function CandidateRow({
  name,
  endpoint,
  checked,
  current,
  disabled,
  busy,
  onSelect,
}: {
  name: string
  endpoint: MonitoringEndpoint
  checked: boolean
  current: boolean
  disabled: boolean
  busy: boolean
  onSelect: () => void
}) {
  const { tone, text } = reachability(endpoint)
  const svc =
    `${endpoint.service.namespace}/${endpoint.service.name}` +
    (endpoint.service.port ? `:${endpoint.service.port}` : '') +
    (endpoint.apiBase || '')

  return (
    <Row checked={checked} disabled={disabled} name={name} onSelect={onSelect}>
      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Chip tone={tone}>{text}</Chip>
        <Chip tone="neutral" mono>
          {endpoint.flavor}
        </Chip>
        {current && !checked ? <Chip tone="accent">in use</Chip> : null}
        {endpoint.scrapeTarget ? <Chip tone="unknown">exporter</Chip> : null}
        {busy ? <Chip tone="neutral">saving…</Chip> : null}
      </span>
      <Mono className="mt-0.5" value={svc} title="Discovered service" />
      {endpoint.detail ? (
        <Mono className="mt-0.5 text-bad" value={endpoint.detail} title="Why it did not answer" />
      ) : null}
    </Row>
  )
}

/**
 * Unreachable rows stay selectable on purpose.
 *
 * Being told the Alertmanager you chose is down is the truth. Being quietly
 * moved onto a different one is how somebody ends up reading another team's
 * alerts and believing they are their own.
 */
function Row({
  checked,
  disabled,
  name,
  onSelect,
  children,
}: {
  checked: boolean
  disabled: boolean
  name: string
  onSelect: () => void
  children: React.ReactNode
}) {
  return (
    <label
      className={cn(
        'flex cursor-pointer items-start gap-2 rounded-lg border px-2.5 py-2 transition-colors',
        'focus-within:ring-[3px] focus-within:ring-ring/50',
        checked ? 'border-accent-strong bg-accent-strong/8' : 'border-border bg-card hover:border-accent-strong/40',
        disabled && 'pointer-events-none opacity-60',
      )}
    >
      <input
        type="radio"
        name={name}
        checked={checked}
        disabled={disabled}
        onChange={onSelect}
        className="mt-0.5 size-3.5 shrink-0 accent-[var(--accent-strong)]"
      />
      <span className="min-w-0 flex-1">{children}</span>
    </label>
  )
}

function reachability(e: MonitoringEndpoint): { tone: Tone; text: string } {
  if (e.reachable) return { tone: 'ok', text: 'answering' }
  if (e.probed === false) return { tone: 'unknown', text: 'not probed' }
  return { tone: 'bad', text: 'no answer' }
}

function sameService(a: MonitoringEndpoint, b: MonitoringEndpoint): boolean {
  return a.service.namespace === b.service.namespace && a.service.name === b.service.name
}

function label(half: MonitoringHalf): string {
  return half === 'alerts' ? 'Alert source' : 'Metrics'
}

function split(stack: MonitoringStack | undefined): {
  metrics: MonitoringEndpoint[]
  alerts: MonitoringEndpoint[]
} {
  const metrics: MonitoringEndpoint[] = []
  const alerts: MonitoringEndpoint[] = []
  for (const c of stack?.candidates ?? []) {
    ;(monitoringHalf(c) === 'alerts' ? alerts : metrics).push(c)
  }
  return { metrics, alerts }
}
