import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { CreateRunRequest, MonitoringPick, MonitoringStack, Run, RunStatus } from '@/lib/types'

export const qk = {
  health: ['health'] as const,
  config: ['config'] as const,
  harness: ['harness'] as const,
  rules: (clusterId?: number) => ['rules', clusterId ?? 0] as const,
  incidents: (clusterId?: number, state?: string) => ['incidents', clusterId ?? 0, state ?? 'open'] as const,
  incident: (id: string) => ['incident', id] as const,
  runAlert: (runId: string) => ['run-alert', runId] as const,
  clusters: ['clusters'] as const,
  sweep: ['clusters', 'sweep'] as const,
  environments: (clusterId?: number) => ['environments', clusterId ?? 'all'] as const,
  monitoring: (clusterId: number) => ['monitoring', clusterId] as const,
  monitoringProbe: (clusterId: number) => ['monitoring', clusterId, 'probe-all'] as const,
  apps: (environmentId?: number, search?: string) => ['apps', environmentId ?? 'all', search ?? ''] as const,
  helmApps: (clusterId?: number, environmentId?: number, search?: string) =>
    ['helm-apps', clusterId ?? 'all', environmentId ?? 'all', search ?? ''] as const,
  alerts: (p: Record<string, unknown>) => ['alerts', p] as const,
  runs: (p: Record<string, unknown>) => ['runs', p] as const,
  run: (id: string) => ['run', id] as const,
  runEvents: (id: string) => ['run-events', id] as const,
  knowledge: (q?: string, cls?: string) => ['knowledge', q ?? '', cls ?? ''] as const,
  settings: ['settings'] as const,
}

export function useHealth() {
  return useQuery({ queryKey: qk.health, queryFn: api.health, refetchInterval: 30_000, retry: false, staleTime: 15_000 })
}

export function useConfig() {
  return useQuery({ queryKey: qk.config, queryFn: api.config, staleTime: Infinity, retry: false })
}

/** Only clusters a run can actually succeed against. What the picker offers. */
export function useClusters() {
  return useQuery({ queryKey: qk.clusters, queryFn: () => api.clusters(), staleTime: 60_000 })
}

export function useEnvironments(clusterId?: number, enabled?: boolean) {
  return useQuery({
    queryKey: qk.environments(clusterId),
    queryFn: () => api.environments(clusterId),
    enabled: enabled ?? clusterId !== undefined,
    staleTime: 60_000,
  })
}

export function useMonitoring(clusterId?: number) {
  return useQuery({
    queryKey: qk.monitoring(clusterId ?? -1),
    queryFn: () => api.monitoring(clusterId as number),
    enabled: clusterId !== undefined,
    staleTime: 60_000,
    retry: false,
  })
}

/**
 * Every candidate measured, not just enough of them to answer.
 *
 * Separate query key from useMonitoring on purpose: this one is slow and only
 * the cluster's monitoring screen wants it, and folding it into the cheap one
 * would make every page that shows "what can the agent see here" pay for a
 * full probe of a dozen exporters.
 */
export function useMonitoringProbe(clusterId?: number, enabled = true) {
  return useQuery({
    queryKey: qk.monitoringProbe(clusterId ?? -1),
    queryFn: () => api.monitoring(clusterId as number, true),
    enabled: enabled && clusterId !== undefined,
    staleTime: 60_000,
    retry: false,
  })
}

/**
 * Pin the endpoints this cluster uses, or hand it back to discovery.
 *
 * Both write paths return the freshly re-probed stack, so the answer that
 * lands in the cache is one measured under the choice that was just made
 * rather than the one it replaced.
 */
export function useChooseMonitoring(clusterId?: number, clusterName?: string) {
  const qc = useQueryClient()
  const settle = (stack: MonitoringStack) => {
    if (clusterId === undefined) return
    qc.setQueryData(qk.monitoringProbe(clusterId), stack)
    qc.setQueryData(qk.monitoring(clusterId), stack)
    // The alert list is downstream of which source it was read from.
    void qc.invalidateQueries({ queryKey: ['alerts'] })
    void qc.invalidateQueries({ queryKey: ['rules'] })
  }
  return {
    choose: useMutation({
      mutationFn: (body: { metrics: MonitoringPick | null; alerts: MonitoringPick | null }) =>
        api.chooseMonitoring(clusterId as number, { ...body, clusterName }),
      onSuccess: settle,
    }),
    reset: useMutation({
      mutationFn: () => api.resetMonitoring(clusterId as number),
      onSuccess: settle,
    }),
  }
}

export function useApps(environmentId?: number, search?: string, enabled = true) {
  return useQuery({
    queryKey: qk.apps(environmentId, search),
    queryFn: () => api.apps({ environmentId, search }),
    enabled: enabled && environmentId !== undefined,
    staleTime: 30_000,
  })
}

export function useHelmApps(clusterId?: number, environmentId?: number, search?: string, enabled = true) {
  return useQuery({
    queryKey: qk.helmApps(clusterId, environmentId, search),
    queryFn: () => api.helmApps({ clusterId, environmentId, search }),
    enabled: enabled && clusterId !== undefined,
    staleTime: 30_000,
  })
}

export interface AlertsParams {
  clusterId?: number
  namespace?: string
  severity?: string
  nameLike?: string
  includePending?: boolean
  limit?: number
}

export function useAlerts(params: AlertsParams, enabled = true) {
  return useQuery({
    queryKey: qk.alerts(params as Record<string, unknown>),
    queryFn: () => api.alerts(params),
    enabled: enabled && params.clusterId !== undefined,
    refetchInterval: 30_000,
    retry: false,
  })
}

export function useRuns(params: { status?: RunStatus; clusterId?: number; limit?: number } = {}) {
  return useQuery({
    queryKey: qk.runs(params as Record<string, unknown>),
    queryFn: () => api.runs(params),
    refetchInterval: 15_000,
  })
}

/** Polls only while the run is live; a finished run is immutable. */
export function useRun(id: string, live: boolean) {
  return useQuery({
    queryKey: qk.run(id),
    queryFn: () => api.run(id),
    enabled: id !== '',
    refetchInterval: live ? 4_000 : false,
  })
}

/** The REST ledger, loaded once so the stream can resume after its last seq. */
export function useRunLedger(id: string) {
  return useQuery({
    queryKey: qk.runEvents(id),
    queryFn: () => api.runEvents(id, 0),
    enabled: id !== '',
    staleTime: Infinity,
    gcTime: 5 * 60_000,
  })
}

export function useCreateRun() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateRunRequest) => api.createRun(body),
    onSuccess: (run: Run) => {
      qc.setQueryData(qk.run(run.id), run)
      void qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })
}

export function useCancelRun() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.cancelRun(id),
    onSuccess: (run: Run) => {
      qc.setQueryData(qk.run(run.id), run)
      void qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })
}

export function useSettings() {
  return useQuery({ queryKey: qk.settings, queryFn: api.settings, staleTime: 30_000, retry: false })
}

export function useSaveSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.saveSettings,
    onSuccess: () => {
      // Everything downstream was derived from the old installation.
      void qc.invalidateQueries()
    },
  })
}

export function useTestSettings() {
  return useMutation({ mutationFn: api.testSettings })
}

/**
 * Every cluster, including the ones a run cannot succeed against.
 *
 * The setup screen needs the unusable ones — showing which clusters failed and
 * why is the whole point of that step — while the picker must not offer them.
 * Same endpoint, one flag.
 */
export function useAllClusters() {
  return useQuery({
    queryKey: [...qk.clusters, 'all'] as const,
    queryFn: () => api.clusters(true),
    staleTime: 30_000,
    retry: false,
  })
}

export function useRefreshClusters() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: api.refreshClusters,
    onSuccess: (rows) => {
      qc.setQueryData([...qk.clusters, 'all'], rows)
      // The picker is the usable subset of this, so it changes too.
      void qc.invalidateQueries({ queryKey: qk.clusters })
      void qc.invalidateQueries({ queryKey: qk.sweep })
    },
  })
}

/**
 * How far the sweep has got.
 *
 * The sweep runs in the background now — probes get a fair timeout and are
 * deliberately not piled onto the orchestrator, which means a large install
 * takes a minute or two. Polled while one is running so the list can be
 * watched filling in, and left alone otherwise.
 */
export function useSweepProgress(watch: boolean) {
  const qc = useQueryClient()
  return useQuery({
    queryKey: qk.sweep,
    queryFn: async () => {
      const p = await api.sweepProgress()
      // Each poll is also the moment to pick up whatever has been measured
      // since the last one.
      void qc.invalidateQueries({ queryKey: qk.clusters })
      return p
    },
    enabled: watch,
    refetchInterval: (q) => (q.state.data?.sweeping ? 2000 : false),
    staleTime: 0,
    retry: false,
  })
}
