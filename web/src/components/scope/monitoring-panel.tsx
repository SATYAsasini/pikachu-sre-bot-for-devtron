import { BellRing, Gauge, Info, SlidersHorizontal } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { Panel, PanelBody, PanelHeader } from '@/components/common/panel'
import { Chip, Unknown } from '@/components/common/status'
import { Mono } from '@/components/common/mono'
import { ErrorState } from '@/components/common/error-state'
import { TextSkeleton } from '@/components/common/skeletons'
import { relativeTime } from '@/lib/format'
import { useMonitoring } from '@/lib/queries'
import { monitoringHalf, type MonitoringEndpoint, type MonitoringStack } from '@/lib/types'

/**
 * What the agent can actually see in this cluster.
 *
 * This panel exists because "no metrics" and "metrics we could not reach" are
 * different facts, and the second one is the dangerous one. A missing half is
 * never drawn as a zero or as a green tick.
 */
export function MonitoringPanel({ clusterId }: { clusterId?: number }) {
  const q = useMonitoring(clusterId)

  return (
    <Panel>
      <PanelHeader
        icon={<Gauge aria-hidden className="size-3.5" />}
        title="What the agent can see here"
        description="Discovered per cluster. Where more than one answers, you choose."
        actions={q.data ? <span className="text-[0.6875rem] text-muted-foreground">{relativeTime(q.data.discoveredAt)}</span> : null}
      />
      <PanelBody className="space-y-2">
        {clusterId === undefined ? (
          <p className="py-1 text-xs text-muted-foreground">Pick a cluster and we will go looking.</p>
        ) : q.isLoading ? (
          <TextSkeleton lines={3} />
        ) : q.isError ? (
          <ErrorState compact error={q.error} onRetry={() => void q.refetch()} />
        ) : q.data ? (
          <>
            <EndpointRow kind="metrics" icon={<Gauge aria-hidden className="size-3.5" />} endpoint={q.data.metrics} />
            <EndpointRow kind="alerts" icon={<BellRing aria-hidden className="size-3.5" />} endpoint={q.data.alerts} />
            <Alternatives stack={q.data} />
            {(q.data.notes ?? []).length > 0 ? (
              <ul className="mt-2 space-y-1 border-t border-border pt-2">
                {(q.data.notes ?? []).map((note, i) => (
                  <li key={i} className="flex items-start gap-1.5 text-[0.6875rem] leading-relaxed text-muted-foreground">
                    <Info aria-hidden className="mt-0.5 size-3 shrink-0 text-warn" />
                    <span>{note}</span>
                  </li>
                ))}
              </ul>
            ) : null}
          </>
        ) : null}
      </PanelBody>
    </Panel>
  )
}

/**
 * A cluster with two Alertmanagers is not a cluster with one.
 *
 * Discovery picks between them by name heuristic and says nothing about it,
 * which is fine right up until it picks the wrong one. Whenever there is more
 * than one way this could have gone, say so and offer the screen where it can
 * be settled.
 */
function Alternatives({ stack }: { stack: MonitoringStack }) {
  // Candidates, not answers. A normal walk stops probing a half as soon as it
  // has one, so counting only the reachable ones here would report "just the
  // one" on exactly the clusters where the others were never tried.
  const found = (half: 'metrics' | 'alerts') =>
    (stack.candidates ?? []).filter((c) => !c.scrapeTarget && monitoringHalf(c) === half).length

  const counts = [
    ['alert source', found('alerts')],
    ['metrics backend', found('metrics')],
  ] as const
  const plural = counts.filter(([, n]) => n > 1)
  if (plural.length === 0 && !stack.partial) return null

  return (
    <div className="mt-1 space-y-1 border-t border-border pt-2">
      {stack.partial ? (
        <p className="text-[0.6875rem] leading-relaxed text-warn">
          Discovery did not finish, so this is what was measured before time ran out — not the whole picture.
        </p>
      ) : null}
      {plural.map(([what, n]) => (
        <p key={what} className="text-[0.6875rem] leading-relaxed text-muted-foreground">
          {n} candidate {what}s in this cluster. One was chosen for you.
        </p>
      ))}
      <Link
        to="/clusters"
        className="inline-flex items-center gap-1 text-[0.6875rem] font-medium text-foreground underline decoration-dotted underline-offset-4"
      >
        <SlidersHorizontal aria-hidden className="size-3" />
        Choose which to use
      </Link>
    </div>
  )
}

function EndpointRow({
  kind,
  icon,
  endpoint,
}: {
  kind: 'metrics' | 'alerts'
  icon: React.ReactNode
  endpoint: MonitoringEndpoint | null | undefined
}) {
  const label = kind === 'metrics' ? 'Metrics' : 'Alert source'

  if (!endpoint) {
    return (
      <div className="flex items-center gap-2 text-xs">
        <span className="text-muted-foreground">{icon}</span>
        <span className="w-24 shrink-0 font-medium">{label}</span>
        <Unknown
          why={
            kind === 'metrics'
              ? 'No metrics backend was discovered. Coverage is unknown, not zero — the agent will work from Kubernetes facts alone and say so.'
              : 'No alert source was discovered. Firing alerts cannot be listed for this cluster; that is not the same as there being none.'
          }
        >
          none discovered
        </Unknown>
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <span className="text-muted-foreground">{icon}</span>
      <span className="w-24 shrink-0 font-medium">{label}</span>
      <Chip tone={endpoint.reachable ? 'ok' : 'bad'}>{endpoint.reachable ? 'reachable' : 'unreachable'}</Chip>
      <Chip tone="neutral" mono>
        {endpoint.flavor}
      </Chip>
      {endpoint.chosen ? <Chip tone="accent">pinned</Chip> : null}
      <Mono
        className="max-w-[16rem] flex-1"
        value={`${endpoint.service.namespace}/${endpoint.service.name}${endpoint.service.port ? `:${endpoint.service.port}` : ''}${endpoint.apiBase}`}
        title="Discovered service"
      />
      {endpoint.detail ? (
        <span className="w-full text-[0.6875rem] leading-relaxed text-bad">{endpoint.detail}</span>
      ) : null}
    </div>
  )
}
