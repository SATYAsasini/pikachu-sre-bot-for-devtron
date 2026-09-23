import { BellRing, Gauge, Info } from 'lucide-react'
import { Panel, PanelBody, PanelHeader } from '@/components/common/panel'
import { Chip, Unknown } from '@/components/common/status'
import { Mono } from '@/components/common/mono'
import { ErrorState } from '@/components/common/error-state'
import { TextSkeleton } from '@/components/common/skeletons'
import { relativeTime } from '@/lib/format'
import { useMonitoring } from '@/lib/queries'
import type { MonitoringEndpoint } from '@/lib/types'

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
        description="Discovered per cluster, never configured."
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
      <Mono
        className="max-w-[16rem] flex-1"
        value={`${endpoint.service.namespace}/${endpoint.service.name}${endpoint.service.port ? `:${endpoint.service.port}` : ''}${endpoint.apiBase}`}
        title="Discovered service"
      />
      {endpoint.detail ? <Mono className="w-full text-bad" value={endpoint.detail} title="Why it failed" /> : null}
    </div>
  )
}
