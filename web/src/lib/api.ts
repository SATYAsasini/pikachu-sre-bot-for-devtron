import type {
  AlertsResult,
  AppConfig,
  Cluster,
  CreateRunRequest,
  DevtronApp,
  Environment,
  Health,
  HelmApp,
  Harness,
  RulePreview,
  RulesConfig,
  KnowledgeComponent,
  MonitoringStack,
  ProbeResult,
  Run,
  RunEvent,
  RunStatus,
  Settings,
  SettingsBody,
} from '@/lib/types'
import { fixtures, isFixtureMode } from '@/lib/fixtures'

/**
 * Base path for the API. Defaults to /v1, which the Vite dev server proxies
 * to http://localhost:8090 (see vite.config.ts).
 */
export const API_BASE: string = (import.meta.env.VITE_API_BASE ?? '/v1').replace(/\/+$/, '')

/** Error carrying the contract's `{error:{code,message}}` envelope when present. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export type QueryParams = Record<string, string | number | boolean | undefined | null>

function buildUrl(path: string, params?: QueryParams): string {
  const url = `${API_BASE}${path.startsWith('/') ? path : `/${path}`}`
  if (!params) return url
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    qs.set(k, String(v))
  }
  const s = qs.toString()
  return s ? `${url}?${s}` : url
}

interface RequestOptions {
  method?: string
  json?: unknown
  params?: QueryParams
  signal?: AbortSignal
}

function readErrorEnvelope(body: unknown): { code: string; message: string } | null {
  if (typeof body !== 'object' || body === null) return null
  const err = (body as { error?: unknown }).error
  if (typeof err !== 'object' || err === null) return null
  const rec = err as Record<string, unknown>
  if (typeof rec.message !== 'string') return null
  return { code: typeof rec.code === 'string' ? rec.code : 'unknown', message: rec.message }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', json, params, signal } = options
  const headers = new Headers({ Accept: 'application/json' })
  let body: string | undefined
  if (json !== undefined) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(json)
  }

  let res: Response
  try {
    res = await fetch(buildUrl(path, params), { method, headers, body, signal })
  } catch (cause) {
    if (signal?.aborted) throw cause
    throw new ApiError(0, 'network_unreachable', 'The API did not answer. Is the agent running on :8090?')
  }

  const text = await res.text()
  let parsed: unknown
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }

  if (!res.ok) {
    const env = readErrorEnvelope(parsed)
    throw new ApiError(res.status, env?.code ?? `http_${res.status}`, env?.message ?? `HTTP ${res.status} ${res.statusText}`)
  }
  return parsed as T
}

/** Arrays come back as `null` from Go when empty; normalise once, here. */
function list<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

/* --------------------------------------------------------------- endpoints */

export const api = {
  health: (): Promise<Health> => (isFixtureMode() ? fixtures.health() : request<Health>('/healthz')),

  config: (): Promise<AppConfig> => (isFixtureMode() ? fixtures.config() : request<AppConfig>('/config')),

  /** `all` includes clusters a run cannot succeed against; the picker omits it. */
  clusters: async (all = false): Promise<Cluster[]> =>
    isFixtureMode()
      ? fixtures.clusters()
      : list(await request<Cluster[] | null>('/clusters', { params: all ? { all: 'true' } : {} })),

  environments: async (clusterId?: number): Promise<Environment[]> => {
    if (isFixtureMode()) return fixtures.environments(clusterId)
    const path = clusterId === undefined ? '/environments' : `/clusters/${clusterId}/environments`
    return list(await request<Environment[] | null>(path))
  },

  monitoring: (clusterId: number): Promise<MonitoringStack> =>
    isFixtureMode() ? fixtures.monitoring(clusterId) : request<MonitoringStack>(`/clusters/${clusterId}/monitoring`),

  apps: async (params: { environmentId?: number; search?: string; status?: string } = {}): Promise<DevtronApp[]> =>
    isFixtureMode() ? fixtures.apps(params) : list(await request<DevtronApp[] | null>('/apps', { params })),

  helmApps: async (
    params: { clusterId?: number; environmentId?: number; search?: string } = {},
  ): Promise<HelmApp[]> =>
    isFixtureMode() ? fixtures.helmApps(params) : list(await request<HelmApp[] | null>('/helm-apps', { params })),

  alerts: async (
    params: {
      clusterId?: number
      namespace?: string
      severity?: string
      nameLike?: string
      includePending?: boolean
      limit?: number
    } = {},
  ): Promise<AlertsResult> => {
    if (isFixtureMode()) return { alerts: await fixtures.alerts(params) }
    const res = await request<AlertsResult | null>('/alerts', { params })
    return { alerts: list(res?.alerts), unavailable: res?.unavailable, notes: res?.notes }
  },

  createRun: (body: CreateRunRequest): Promise<Run> =>
    isFixtureMode() ? fixtures.createRun(body) : request<Run>('/runs', { method: 'POST', json: body }),

  runs: async (params: { status?: RunStatus; clusterId?: number; limit?: number } = {}): Promise<Run[]> =>
    isFixtureMode() ? fixtures.runs(params) : list(await request<Run[] | null>('/runs', { params })),

  run: (id: string): Promise<Run> =>
    isFixtureMode() ? fixtures.run(id) : request<Run>(`/runs/${encodeURIComponent(id)}`),

  runEvents: async (id: string, after = 0, limit?: number): Promise<RunEvent[]> =>
    isFixtureMode()
      ? fixtures.runEvents(id, after)
      : list(await request<RunEvent[] | null>(`/runs/${encodeURIComponent(id)}/events`, { params: { after, limit } })),

  cancelRun: (id: string): Promise<Run> =>
    isFixtureMode()
      ? fixtures.cancelRun(id)
      : request<Run>(`/runs/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),

  knowledge: async (params: { q?: string; class?: 'devtron' | 'app' } = {}): Promise<KnowledgeComponent[]> =>
    isFixtureMode()
      ? fixtures.knowledge(params)
      : list(await request<KnowledgeComponent[] | null>('/knowledge', { params })),

  harness: (): Promise<Harness> => request<Harness>('/harness'),

  rules: (clusterId: number): Promise<RulesConfig> => request<RulesConfig>('/rules', { params: { clusterId } }),

  saveRules: (body: RulesConfig & { clusterName?: string }): Promise<RulesConfig> =>
    request<RulesConfig>('/rules', { method: 'PUT', json: body }),

  /** Runs a proposed rule set against what is firing right now. */
  previewRules: (body: RulesConfig & { clusterName?: string }): Promise<RulePreview> =>
    request<RulePreview>('/rules/preview', { method: 'POST', json: body }),

  /** Re-measures every cluster and returns the same rows as `clusters`. */
  refreshClusters: async (): Promise<Cluster[]> =>
    isFixtureMode()
      ? fixtures.clusters()
      : list(await request<Cluster[] | null>('/clusters/refresh', { method: 'POST', params: { all: 'true' } })),

  settings: (): Promise<Settings> =>
    isFixtureMode() ? fixtures.settings() : request<Settings>('/settings'),

  saveSettings: (body: SettingsBody): Promise<Settings> =>
    isFixtureMode() ? fixtures.saveSettings(body) : request<Settings>('/settings', { method: 'PUT', json: body }),

  testSettings: (body: SettingsBody): Promise<ProbeResult> =>
    isFixtureMode() ? fixtures.testSettings(body) : request<ProbeResult>('/settings/test', { method: 'POST', json: body }),

  knowledgeComponent: (id: string): Promise<KnowledgeComponent> =>
    isFixtureMode()
      ? fixtures.knowledgeComponent(id)
      : request<KnowledgeComponent>(`/knowledge/${encodeURIComponent(id)}`),
}

/** URL of the SSE endpoint for a run, resuming after `seq`. */
export function runStreamUrl(id: string, after: number): string {
  return buildUrl(`/runs/${encodeURIComponent(id)}/stream`, { after })
}

/** Human-readable one-liner for anything thrown by the client. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return String(err)
}

export function errorCode(err: unknown): string | null {
  return err instanceof ApiError ? err.code : null
}
