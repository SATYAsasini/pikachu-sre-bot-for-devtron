import { useAllClusters, useConfig, useSettings } from '@/lib/queries'

export type StepState = 'done' | 'current' | 'blocked' | 'pending'

export interface SetupStep {
  key: 'connect' | 'reach' | 'model'
  title: string
  /** What this step is for, in one line. */
  purpose: string
  state: StepState
  /** What is true right now. Shown whether done or not. */
  status: string
  /** Only when not done: the next concrete action. */
  action?: string
}

export interface Readiness {
  loading: boolean
  ready: boolean
  steps: SetupStep[]
  /** Index of the step the user should be working on. */
  activeIndex: number
}

/**
 * The three things that must be true before an investigation can succeed,
 * in the order they have to happen.
 *
 * They are genuinely sequential — you cannot measure which clusters answer
 * until a host and token exist — so the UI presents them as a sequence rather
 * than as a settings page with three unrelated fields.
 */
export function useReadiness(): Readiness {
  const config = useConfig()
  const settings = useSettings()
  const caps = useAllClusters()

  const loading = config.isPending || settings.isPending

  const connected = Boolean(settings.data?.devtronUrl && settings.data?.tokenSet)
  // Counted from the rows rather than read off a separate report, which is
  // how the picker and this screen used to disagree.
  const usable = (caps.data ?? []).filter((c) => c.investigable).length
  const probed = (caps.data ?? []).some((c) => c.reach !== undefined)
  const total = (caps.data ?? []).length
  const modelOk = config.data?.ready?.model ?? true
  const modelWhy = config.data?.ready?.modelBlockedBy ?? ''

  // Why there is nothing usable, taken from what was actually measured.
  // Telling somebody to check the orchestrator when every cluster answered
  // in under a second sends them to the wrong place entirely.
  const rows = caps.data ?? []
  const count = (r: string) => rows.filter((c) => c.reach === r).length
  const empty = count('empty')
  const dead = count('unreachable') + count('error')
  const refused = count('forbidden')
  const whyBlocked =
    empty > 0 && dead === 0 && refused === 0
      ? empty === total
        ? 'Every cluster answered but returned nothing — no pods, events, services, deployments or nodes, in any namespace Devtron knows about. Either they are genuinely idle, or this token cannot see into them. Widen the token to Kubernetes Resources → View with Resource name "All resources".'
        : 'The clusters that answered returned nothing readable. Check the token has Kubernetes Resources → View on them.'
      : refused > 0 && dead === 0
        ? 'Devtron refused the reads. This is RBAC, not an outage — the token needs Kubernetes Resources → View on these clusters.'
        : empty > 0
          ? 'Some clusters could not be reached and the rest returned nothing. Check the orchestrator first, then the token’s RBAC.'
          : 'Every cluster timed out or errored. That is the orchestrator failing to reach them, not a permissions problem.'

  const steps: SetupStep[] = [
    {
      key: 'connect',
      title: 'Connect Devtron',
      purpose: 'Every cluster is reached through Devtron. No kubeconfig, anywhere.',
      state: connected ? 'done' : 'current',
      status: connected
        ? `${settings.data?.devtronUrl} · token ${settings.data?.tokenHint ?? 'set'}`
        : 'No host or token yet.',
      action: connected ? undefined : 'Enter the host and a view-only API token, then test it.',
    },
    {
      key: 'reach',
      title: 'Measure what answers',
      purpose: 'Devtron lists clusters it can no longer reach. We check before offering them.',
      state: !connected ? 'pending' : usable > 0 ? 'done' : probed ? 'blocked' : 'current',
      status: !connected
        ? 'Waiting on a connection.'
        : !probed
          ? 'Not measured yet.'
          : usable > 0
            ? `${usable} of ${total} clusters can be investigated.`
            : `None of the ${total} listed ${total === 1 ? 'cluster' : 'clusters'} returned anything readable.` +
              (empty > 0 ? ` ${empty} answered but had nothing in ${empty === 1 ? 'it' : 'them'}.` : '') +
              (dead > 0 ? ` ${dead} could not be reached.` : ''),
      action:
        !connected || usable > 0
          ? undefined
          : probed
            ? whyBlocked
            : 'Run the probe to find out which clusters this token can actually read.',
    },
    {
      key: 'model',
      title: 'Model credential',
      purpose: 'The two agents need a model. This one lives in the environment, not here.',
      state: modelOk ? 'done' : connected ? 'current' : 'pending',
      status: modelOk
        ? `${config.data?.models.provider} · ${config.data?.models.strong}`
        : modelWhy || 'No credential set.',
      action: modelOk ? undefined : 'Set the key in .env and restart the backend. It is not editable from the UI on purpose.',
    },
  ]

  const activeIndex = Math.max(
    0,
    steps.findIndex((s) => s.state === 'current' || s.state === 'blocked'),
  )
  return { loading, ready: steps.every((s) => s.state === 'done'), steps, activeIndex }
}
