import { useCallback } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { errorMessage } from '@/lib/api'
import { useCreateRun } from '@/lib/queries'
import { hasCluster, useScope } from '@/lib/scope'
import type { Alert, RunOptions } from '@/lib/types'

/**
 * Creating a run is the one write this UI performs, and it happens from two
 * places (an alert row, or the free-text box). Both go through here so the
 * scope, the toast and the navigation behave identically.
 */
export function useStartRun() {
  const navigate = useNavigate()
  const { scope } = useScope()
  const create = useCreateRun()

  const start = useCallback(
    (trigger: { alert?: Alert; ask?: string; options?: RunOptions }) => {
      if (!hasCluster(scope)) {
        toast.error('No cluster selected', { description: 'Runs need somewhere to look. Pick a cluster first.' })
        return
      }

      create.mutate(
        {
          clusterId: scope.clusterId,
          clusterName: scope.clusterName,
          environmentId: scope.environmentId,
          namespace: scope.namespace,
          appName: scope.appName,
          appType: scope.appType,
          alert: trigger.alert ?? null,
          ask: trigger.ask ?? '',
          options: trigger.options,
        },
        {
          onSuccess: (run) => {
            toast.success('Run started', {
              description: trigger.alert ? `Investigating ${trigger.alert.name}` : 'Working on your question',
            })
            void navigate({ to: '/runs/$runId', params: { runId: run.id } })
          },
          onError: (e) => toast.error('Could not start the run', { description: errorMessage(e) }),
        },
      )
    },
    [create, navigate, scope],
  )

  return { start, pending: create.isPending }
}
