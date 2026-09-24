import type { Alert, Run } from '@/lib/types'

/**
 * Has this alert already been investigated?
 *
 * Firing alerts do not go away because someone looked at them, so the same
 * row sits on the landing page after a run has already answered it — and the
 * obvious thing to do with a row that looks unhandled is to hit Debug. That
 * spends a Devtron first pass, two models and a tool budget re-deriving a
 * conclusion that is already on disk.
 *
 * Alerts are matched to runs on name, namespace and resource rather than on
 * the alert's fingerprint: vmalert reuses fingerprints across distinct alerts,
 * which is the bug that put duplicate React keys on this same list.
 */
export function alertKey(a: Pick<Alert, 'name' | 'namespace' | 'resource'>): string {
  return `${a.name}\u0000${a.namespace ?? ''}\u0000${a.resource ?? ''}`
}

export interface PriorRun {
  run: Run
  /** One paragraph: the corrected cause, or the first claim if there is no report. */
  answer: string
  /** The first thing to do, when one was proposed. */
  action: string | null
  /** How many of the three stages actually produced something. */
  stagesRan: number
  /** True when the deep dive was deliberately skipped rather than failing. */
  settledEarly: boolean
}

/**
 * Index runs by the alert that triggered them, newest first.
 *
 * Only runs that reached a conclusion are indexed. A failed or canceled run
 * is not an answer, and offering it as one would suppress the retry that the
 * operator actually needs.
 */
export function priorRunsByAlert(runs: readonly Run[]): Map<string, PriorRun> {
  const out = new Map<string, PriorRun>()

  for (const run of runs) {
    const alert = run.trigger?.alert
    if (!alert?.name) continue
    if (run.status !== 'succeeded') continue

    const key = alertKey(alert)
    // The list arrives newest first, so the first hit for a key wins.
    if (out.has(key)) continue

    const summary = summarise(run)
    if (!summary) continue
    out.set(key, summary)
  }

  return out
}

function summarise(run: Run): PriorRun | null {
  const report = run.report ?? null
  const verdict = run.verdict ?? null

  const answer =
    report?.correctedRootCause?.trim() ||
    report?.sreNotes?.trim() ||
    verdict?.claims?.[0]?.claim?.trim() ||
    ''
  if (!answer) return null

  const action = report?.remediation?.[0]?.action?.trim() || null

  // A report with no cause and nothing to do is a run that found the first
  // pass sound and had no remediation to add. Worth marking, because it looks
  // identical to an empty one otherwise.
  const settledEarly = Boolean(report) && !report?.correctedRootCause?.trim() && (report?.remediation ?? []).length === 0

  // Two phases now, not three: Devtron gathers, we reason.
  const stagesRan = [Boolean(run.intelligence?.analysis), Boolean(report)].filter(Boolean).length

  return { run, answer, action, stagesRan, settledEarly }
}
