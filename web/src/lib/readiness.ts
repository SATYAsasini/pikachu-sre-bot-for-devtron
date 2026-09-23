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
            : `None of the ${total} listed clusters returned anything readable.`,
      action:
        !connected || usable > 0
          ? undefined
          : probed
            ? 'Every cluster timed out or errored. Check the orchestrator, or widen the token’s RBAC.'
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
