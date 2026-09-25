import { Loader2, RotateCw } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Chip, type Tone } from '@/components/common/status'
import { Text } from '@/components/common/text'
import { useProbeCluster } from '@/lib/queries'
import type { Cluster, Reach } from '@/lib/types'

/**
 * What answered, what did not, and why — for every cluster at once.
 *
 * A flat list of forty rows reading "unreachable 20000ms" tells you nothing
 * you can act on: the eye cannot count it, and the reason is the same eleven
 * words repeated. So the clusters are grouped by what happened to them, each
 * group states its cause and its fix once, and the bar across the top gives
 * the shape of the install in a glance.
 *
 * The distinction that matters most is the one people get wrong: a timeout is
 * the orchestrator failing to reach a cluster, and no amount of token
 * permission will change it. A refusal is the opposite. Grouping them puts
 * that difference on screen instead of leaving it to be inferred.
 */

const ORDER: Reach[] = ['usable', 'empty', 'forbidden', 'error', 'unreachable', 'unknown']

const TONE: Record<Reach, Tone> = {
  usable: 'ok',
  empty: 'warn',
  forbidden: 'bad',
  error: 'bad',
  unreachable: 'bad',
  unknown: 'unknown',
}

/** The bar segments, in the same order, so colour and position agree. */
const BAR: Record<Reach, string> = {
  usable: 'bg-ok',
  empty: 'bg-warn',
  forbidden: 'bg-bad',
  error: 'bg-bad/70',
  unreachable: 'bg-bad/40',
  unknown: 'bg-muted-foreground/30',
}

const MEANING: Record<Reach, { what: string; why: string }> = {
  usable: {
    what: 'Answered and returned objects.',
    why: 'These are the clusters a run can be pointed at.',
  },
  empty: {
    what: 'Answered, but every kind came back with nothing — in any namespace Devtron knows about.',
    why: 'Either genuinely idle, or the token cannot see into them. Zero Nodes is the tell: every live cluster has some, so no nodes points at the token rather than an empty cluster.',
  },
  forbidden: {
    what: 'Devtron refused the read.',
    why: 'A permissions problem, not an outage. The token needs Kubernetes Resources → View on these clusters.',
  },
  error: {
    what: 'The orchestrator failed while serving them.',
    why: 'Its own error, usually HTTP 500 — returned fast, so nothing was waited on. Check the orchestrator logs for these cluster ids.',
  },
  unreachable: {
    what: 'Devtron cannot connect to them.',
    why: 'Nothing to do with the token — the credentials Devtron holds for these clusters no longer work, or the clusters are gone. Most of these are Devtron\'s own verdict, taken without spending a request; if you have just fixed one, probe it and see.',
  },
  unknown: {
    what: 'Not measured yet.',
    why: 'Either the sweep has not reached them, or it ended first. Not a verdict.',
  },
}

export function ReachBreakdown({ rows, running }: { rows: Cluster[]; running: boolean }) {
  if (rows.length === 0) {
    return (
      <Text tone="fine">
        Nothing measured yet. Until then the picker shows everything Devtron lists, warts and all.
      </Text>
    )
  }

  const groups = ORDER.map((reach) => ({
    reach,
    clusters: rows.filter((c) => (c.reach ?? 'unknown') === reach),
  })).filter((g) => g.clusters.length > 0)

  const total = rows.length

  return (
    <div className="space-y-2">
      {/* The shape of the install, to scale. */}
      <div className="flex h-2 w-full overflow-hidden rounded-full bg-border" role="img"
           aria-label={groups.map((g) => `${g.clusters.length} ${g.reach}`).join(', ')}>
        {groups.map((g) => (
          <div
            key={g.reach}
            className={cn('h-full', BAR[g.reach])}
            style={{ width: `${(g.clusters.length / total) * 100}%` }}
            title={`${g.clusters.length} ${g.reach}`}
          />
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        {groups.map((g) => (
          <Chip key={g.reach} tone={TONE[g.reach]}>
            {g.clusters.length} {g.reach}
          </Chip>
        ))}
        <span className="text-[0.6875rem] text-muted-foreground">
          of {total}
          {running ? ' so far' : ''}
        </span>
      </div>

      <div className="space-y-2">
        {groups.map((g) => (
          <ReachGroup key={g.reach} reach={g.reach} clusters={g.clusters} />
        ))}
      </div>
    </div>
  )
}

/**
 * One cluster, with what is known about it and a way to disagree.
 *
 * A verdict taken from Devtron rather than measured says so, because the two
 * are different claims: one is a cached opinion held by another service, the
 * other is a read we performed. The operator who has just repaired a cluster
 * is the person best placed to know the first is stale, so they get a button
 * rather than a wait.
 */
function ClusterLine({ cluster }: { cluster: Cluster }) {
  const probe = useProbeCluster()
  const namespaces = cluster.namespaces ?? []

  return (
    <li className="px-2.5 py-1">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem]">{cluster.clusterName}</span>
        {cluster.fromDevtron ? (
          <span
            className="shrink-0 text-[0.625rem] text-muted-foreground"
            title="Devtron's own connection status. No request was spent on this."
          >
            per Devtron
          </span>
        ) : (
          <span className="tabular shrink-0 text-[0.625rem] text-muted-foreground">{cluster.latencyMs ?? 0}ms</span>
        )}
        {cluster.investigable ? null : (
          <Button
            size="xs"
            variant="ghost"
            disabled={probe.isPending}
            onClick={() => probe.mutate(cluster.id)}
            title="Measure this cluster now, whatever Devtron thinks"
          >
            {probe.isPending ? (
              <Loader2 aria-hidden className="size-3 animate-spin" />
            ) : (
              <RotateCw aria-hidden className="size-3" />
            )}
            Probe
          </Button>
        )}
      </div>

      {namespaces.length > 0 && (
        <div className="mt-0.5 flex flex-wrap items-center gap-1">
          <span className="text-[0.625rem] text-muted-foreground">
            {namespaces.length} namespace{namespaces.length === 1 ? '' : 's'}:
          </span>
          {namespaces.slice(0, 6).map((n) => (
            <span key={n} className="rounded border border-border/70 bg-well px-1 font-mono text-[0.625rem]">
              {n}
            </span>
          ))}
          {namespaces.length > 6 && (
            <span className="text-[0.625rem] text-muted-foreground">+{namespaces.length - 6}</span>
          )}
        </div>
      )}

      {cluster.detail && !cluster.investigable && (
        <p className="mt-0.5 text-[0.625rem] leading-relaxed text-bad">{cluster.detail}</p>
      )}
    </li>
  )
}

function ReachGroup({ reach, clusters }: { reach: Reach; clusters: Cluster[] }) {
  const meaning = MEANING[reach]
  // The slowest one is the interesting one in a timeout group, and the
  // fastest in an error group. Either way, sort so the extremes are visible.
  const sorted = [...clusters].sort((a, b) => (b.latencyMs ?? 0) - (a.latencyMs ?? 0))
  // Every distinct reason in this group, which is usually one.
  const reasons = [...new Set(clusters.map((c) => c.detail).filter(Boolean))] as string[]

  return (
    <section className="overflow-hidden rounded-lg border border-border">
      <header className="flex flex-wrap items-baseline gap-2 border-b border-border bg-well px-2.5 py-1.5">
        <Chip tone={TONE[reach]}>{reach}</Chip>
        <span className="text-xs font-semibold">{clusters.length}</span>
        <span className="min-w-0 flex-1 text-[0.6875rem] leading-relaxed text-muted-foreground">{meaning.what}</span>
      </header>

      <p className="border-b border-border px-2.5 py-1.5 text-[0.6875rem] leading-relaxed text-muted-foreground">
        {meaning.why}
      </p>

      {reasons.length === 1 && (
        <p className="border-b border-border px-2.5 py-1.5 font-mono text-[0.625rem] leading-relaxed text-bad">
          {reasons[0]}
        </p>
      )}

      <ul data-lenis-prevent className="max-h-56 divide-y divide-border overflow-y-auto">
        {sorted.map((c) => (
          <ClusterLine key={c.id} cluster={c} />
        ))}
      </ul>
    </section>
  )
}
