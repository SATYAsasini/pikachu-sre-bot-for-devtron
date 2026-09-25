import { cn } from 'cn'
import { Chip, type Tone } from '@/components/common/status'
import { Text } from '@/components/common/text'
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
    what: 'The orchestrator could not reach them before the deadline.',
    why: 'Nothing to do with the token — the credentials Devtron holds for these clusters no longer work, or the clusters are gone. Raise SRE_RUN_PROBE_TIMEOUT_SECONDS if you believe they are merely slow.',
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

      {reasons.length > 0 && (
        <ul className="space-y-0.5 border-b border-border px-2.5 py-1.5">
          {reasons.slice(0, 3).map((r) => (
            <li key={r} className="font-mono text-[0.625rem] leading-relaxed text-bad">
              {r}
            </li>
          ))}
        </ul>
      )}

      <ul data-lenis-prevent className="max-h-48 divide-y divide-border overflow-y-auto">
        {sorted.map((c) => (
          <li key={c.id} className="flex items-center gap-2 px-2.5 py-1">
            <span className="min-w-0 flex-1 truncate font-mono text-[0.6875rem]">{c.clusterName}</span>
            <span className="tabular shrink-0 text-[0.625rem] text-muted-foreground">
              {c.reach === undefined ? '—' : `${c.latencyMs ?? 0}ms`}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}
