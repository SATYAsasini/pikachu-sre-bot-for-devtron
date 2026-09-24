import { useCallback } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { api, errorMessage } from '@/lib/api'
import { useCreateRun } from '@/lib/queries'
import { hasCluster, useScope } from '@/lib/scope'
import type { Alert, RunOptions } from '@/lib/types'

/**
 * Creating a run is the one write this UI performs, and it happens from two
 * places (an alert row, or the free-text box). Both go through here so the
 * scope, the toast and the navigation behave identically.
 *
 * An alert takes the longer road. Debugging one is the moment we take
 * responsibility for it, so it is tracked first and the run is attached to
 * it: the dashboard is a list of alerts with answers, and a run that exists
 * without the alert it answered leaves nothing on that list. The server also
 * refuses to start a second investigation into an alert already being looked
 * at, and returns the run in flight instead.
 *
 * A typed question is not an alert and mints a plain run — there is no entity
 * to own, and no second person who needs to know it was handled.
 */
export function useStartRun() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { scope } = useScope()
  const create = useCreateRun()

  const track = useMutation({ mutationFn: api.trackAlert })

  const start = useCallback(
    (trigger: {
      alert?: Alert
      ask?: string
      options?: RunOptions
      /**
       * Where to run, when it is not wherever the top bar is pointing. An
       * alert already on the dashboard carries its own cluster, and running
       * it against the currently scoped one would file the result under the
       * wrong entity.
       */
       cluster?: { id: number; name?: string }
    }) => {
      const clusterId = trigger.cluster?.id ?? (hasCluster(scope) ? scope.clusterId : 0)
      const clusterName = trigger.cluster ? trigger.cluster.name : scope.clusterName
      if (!clusterId) {
        toast.error('No cluster selected', { description: 'Runs need somewhere to look. Pick a cluster first.' })
        return
      }
      // Narrowing only carries over when the run is going to the cluster the
      // narrowing was chosen in.
      const inScope = clusterId === scope.clusterId

      if (trigger.alert) {
        track.mutate(
          {
            clusterId,
            clusterName,
            alert: trigger.alert,
            investigate: true,
            options: trigger.options,
            environmentId: inScope ? scope.environmentId : undefined,
            namespace: inScope ? scope.namespace : undefined,
            appName: inScope ? scope.appName : undefined,
            appType: inScope ? scope.appType : undefined,
          },
          {
            onSuccess: (res) => {
              void qc.invalidateQueries({ queryKey: ['incidents'] })
              void qc.invalidateQueries({ queryKey: ['runs'] })
              if (!res.runId) {
                toast.success('Alert tracked', { description: 'It is on the dashboard, waiting for someone.' })
                return
              }
              toast.success(res.alreadyRunning ? 'Already being investigated' : 'Run started', {
                description: res.alreadyRunning
                  ? `Opening the run already looking at ${trigger.alert?.name}`
                  : `Investigating ${trigger.alert?.name}`,
              })
              void navigate({ to: '/runs/$runId', params: { runId: res.runId } })
            },
            onError: (e) => toast.error('Could not start the run', { description: errorMessage(e) }),
          },
        )
        return
      }

      create.mutate(
        {
          clusterId,
          clusterName: clusterName ?? '',
          environmentId: inScope ? scope.environmentId : undefined,
          namespace: inScope ? scope.namespace : undefined,
          appName: inScope ? scope.appName : undefined,
          appType: inScope ? scope.appType : undefined,
          alert: null,
          ask: trigger.ask ?? '',
          options: trigger.options,
        },
        {
          onSuccess: (run) => {
            toast.success('Run started', { description: 'Working on your question' })
            void navigate({ to: '/runs/$runId', params: { runId: run.id } })
          },
          onError: (e) => toast.error('Could not start the run', { description: errorMessage(e) }),
        },
      )
    },
    [create, navigate, qc, scope, track],
  )

  return { start, pending: create.isPending || track.isPending }
}
