import type { Alert } from '@/lib/types'

/**
 * Labels that never tell a reader anything, so they never earn a slot.
 * `alertname` and `severity` are already the headline; the rest are routing
 * plumbing.
 */
const NOISE = new Set([
  'alertname',
  'severity',
  'prometheus',
  'endpoint',
  'alertstate',
  'level',
  'replica',
  'cluster',
  'monitor',
  'origin_prometheus',
])

/**
 * Precedence for identifying labels — most specific first.
 *
 * The order matters more than it looks. Two `KubeSchedulerDown` alerts can be
 * byte-identical except for `job`, and a row that leads with namespace shows
 * them as duplicates. Leading with the most specific label that is actually
 * present is what makes two rows distinguishable at a glance.
 */
const PRECEDENCE = [
  'pod',
  'persistentvolumeclaim',
  'deployment',
  'statefulset',
  'daemonset',
  'container',
  'job_name',
  'job',
  'service',
  'ingress',
  'node',
  'instance',
  'namespace',
  'scrape_pool',
  'reason',
  'phase',
]

export interface MetaChip {
  key: string
  value: string
  /** True for the single most identifying label, which is emphasised. */
  lead?: boolean
}

/**
 * The metadata worth showing on one line, in precedence order.
 *
 * Returns nothing rather than placeholders: a row of em-dashes takes the same
 * space as real information and carries none.
 */
export function alertMeta(alert: Alert, limit = 4): MetaChip[] {
  const labels = alert.labels ?? {}
  const seen = new Set<string>()
  const out: MetaChip[] = []

  const push = (key: string, value: string | undefined) => {
    if (!value || seen.has(key) || out.length >= limit) return
    seen.add(key)
    out.push({ key, value })
  }

  // The derived resource wins outright when the backend identified one.
  if (alert.resource) {
    seen.add('resource')
    out.push({ key: (alert.kind || 'resource').toLowerCase(), value: alert.resource, lead: true })
    // Do not repeat it under its own label name.
    for (const [k, v] of Object.entries(labels)) if (v === alert.resource) seen.add(k)
  }

  for (const key of PRECEDENCE) push(key, labels[key])

  // Anything left that is not plumbing, so unusual labels still surface.
  for (const [k, v] of Object.entries(labels)) {
    if (!NOISE.has(k)) push(k, v)
  }

  if (out.length > 0 && !out.some((c) => c.lead)) out[0].lead = true
  return out
}

/** How many labels exist beyond the ones shown. */
export function hiddenMetaCount(alert: Alert, shown: number): number {
  const total = Object.keys(alert.labels ?? {}).filter((k) => !NOISE.has(k)).length
  return Math.max(0, total - shown)
}

export const SEARCH_MODES = ['alert', 'resource', 'label'] as const
export type SearchMode = (typeof SEARCH_MODES)[number]

export const SEARCH_HINT: Record<SearchMode, string> = {
  alert: 'Alert name, e.g. KubeScheduler',
  resource: 'Pod, node, service, job…',
  label: 'key=value, or just a value',
}

/**
 * What each mode actually searches, spelled out under the bar.
 *
 * "Alert / Resource / Label" is only obvious to whoever wrote it. The mode
 * changes which field is matched, and saying so removes the guesswork.
 */
export const SEARCH_EXPLAINS: Record<SearchMode, string> = {
  alert: 'Matching the alert name and its summary.',
  resource: 'Matching the object an alert points at — pod, node, service, job — and its namespace.',
  label: 'Matching Prometheus labels. Use key=value for an exact pair, or a bare word to match any value.',
}

/**
 * Client-side search. The list is already in memory and capped at a hundred,
 * so filtering here is instant and avoids a round trip per keystroke.
 */
export function matchesSearch(alert: Alert, mode: SearchMode, q: string): boolean {
  const needle = q.trim().toLowerCase()
  if (!needle) return true
  const labels = alert.labels ?? {}

  switch (mode) {
    case 'alert':
      return (
        alert.name.toLowerCase().includes(needle) ||
        (alert.summary ?? '').toLowerCase().includes(needle)
      )
    case 'resource':
      return (
        (alert.resource ?? '').toLowerCase().includes(needle) ||
        (alert.namespace ?? '').toLowerCase().includes(needle) ||
        PRECEDENCE.some((k) => (labels[k] ?? '').toLowerCase().includes(needle))
      )
    case 'label': {
      // key=value is an exact-ish pair match; a bare term matches any value.
      const [k, v] = needle.includes('=') ? needle.split('=', 2) : [null, needle]
      return Object.entries(labels).some(([lk, lv]) =>
        k === null
          ? lv.toLowerCase().includes(v) || lk.toLowerCase().includes(v)
          : lk.toLowerCase() === k.trim() && lv.toLowerCase().includes(v.trim()),
      )
    }
  }
}
