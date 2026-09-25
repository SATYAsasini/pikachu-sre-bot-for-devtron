/**
 * Typed mirror of the frozen /v1 API contract.
 *
 * Every field here exists on the wire. Nothing is invented, nothing is
 * renamed: when the backend and this file disagree, this file is wrong.
 */

/* ------------------------------------------------------------------ meta */

export interface Health {
  status: string
}

/**
 * Model identity is opaque. The provider is auto-detected from whichever
 * credential is present and either agent can be pointed at any model id, so
 * these three strings are displayed and never branched on.
 */
export interface ModelConfig {
  provider: string
  fast: string
  strong: string
}

export interface AppConfig {
  devtronUrl: string
  models: ModelConfig
  features: Record<string, unknown>
  /** What the backend is still missing before a run can succeed. */
  ready?: {
    devtron: boolean
    model: boolean
    modelBlockedBy: string
  }
}

/** Error envelope returned by any endpoint on failure. */
export interface ApiErrorBody {
  error: {
    code: string
    message: string
  }
}

/* -------------------------------------------------------------- topology */

/**
 * A cluster, with what was actually measured about it.
 *
 * The measurement used to live on a separate `/capabilities` endpoint with its
 * own shape, so the picker and the setup screen could — and did — disagree
 * about how many clusters were usable. One row, one truth.
 */
export interface Cluster {
  id: number
  clusterName: string
  serverUrl: string
  isVirtualCluster: boolean
  /** Non-empty means Devtron could not reach the cluster. */
  errorInConnecting: string
  /** Absent until a sweep has run. */
  reach?: Reach
  reachWhy?: string
  probedAt?: string
  latencyMs?: number
  detail?: string
  /** Whether a run pointed here can succeed. Only `usable` qualifies. */
  investigable?: boolean
  /** An operator has pinned which monitoring endpoints this cluster uses. */
  monitoringPinned?: boolean
  /** What this token can see in the cluster, from the reach check. */
  namespaces?: string[] | null
  /** The verdict is Devtron's own connection status, not something measured. */
  fromDevtron?: boolean
}

/** How far the cluster capability sweep has got. */
export interface SweepProgress {
  sweeping: boolean
  probed: number
  total: number
  startedAt?: string
}

export interface Environment {
  environmentId: number
  environmentName: string
  namespace: string
  clusterId: number
  clusterName: string
  isVirtualCluster: boolean
}

export const MONITORING_FLAVORS = [
  'prometheus',
  'victoriametrics',
  'thanos',
  'mimir',
  'alertmanager',
  'vmalert',
] as const
export type MonitoringFlavor = (typeof MONITORING_FLAVORS)[number]

export interface MonitoringService {
  namespace: string
  name: string
  port: string
}

export interface MonitoringEndpoint {
  flavor: MonitoringFlavor | string
  service: MonitoringService
  apiBase: string
  reachable: boolean
  detail: string
  /**
   * Whether this candidate was actually tried. A walk stops as soon as a half
   * is satisfied, so `reachable: false` covers two very different answers and
   * only this tells them apart. Never draw an unprobed row as a failure.
   */
  probed?: boolean
  /** An exporter that matched the name filter but serves no query API. */
  scrapeTarget?: boolean
  /** Pinned by the operator, as opposed to landed on by the heuristic. */
  chosen?: boolean
  ports?: string[] | null
}

/** Namespace and name are all it takes to name a Service. */
export interface MonitoringPick {
  namespace: string
  name: string
}

export interface MonitoringChoice {
  metrics?: MonitoringPick | null
  alerts?: MonitoringPick | null
}

/**
 * Either half may be absent. A missing half is a real, displayable state —
 * "we could not find one", which is not the same as "there is none" and very
 * much not the same as "everything is fine".
 */
export interface MonitoringStack {
  clusterId: number
  clusterName: string
  metrics?: MonitoringEndpoint | null
  alerts?: MonitoringEndpoint | null
  /**
   * Everything that looked like part of a monitoring stack, answered or not.
   * The server has always sent this; the UI never had it in the type, so the
   * one screen that needs it — choosing between two alert sources — could not
   * be built.
   */
  candidates?: MonitoringEndpoint[] | null
  chosen?: MonitoringChoice | null
  discoveredAt: string
  /** The walk did not finish. Not cached, and not to be read as an absence. */
  partial?: boolean
  notes?: string[] | null
}

/** The half of the stack an endpoint belongs to. */
export type MonitoringHalf = 'metrics' | 'alerts'

export const ALERT_FLAVORS: readonly string[] = ['alertmanager', 'vmalert']

export function monitoringHalf(e: MonitoringEndpoint): MonitoringHalf {
  return ALERT_FLAVORS.includes(e.flavor) ? 'alerts' : 'metrics'
}

export function samePick(a: MonitoringPick | null | undefined, e: MonitoringEndpoint): boolean {
  return !!a && a.namespace === e.service.namespace && a.name === e.service.name
}

/* ------------------------------------------------------------------ apps */

export interface DevtronAppEnvironment {
  environmentId: number
  environmentName: string
  namespace: string
  clusterName: string
  status: string
  appStatus: string
  lastDeployedTime: string
}

export interface DevtronApp {
  appId: number
  appName: string
  projectId: number
  environments?: DevtronAppEnvironment[] | null
}

export interface HelmAppEnvironmentDetail {
  environmentId: number
  environmentName: string
  namespace: string
  clusterId: number
  clusterName: string
}

export interface HelmApp {
  /** Composite id, e.g. "1|payments|redis". */
  appId: string
  appName: string
  /** What lets us recognise the product behind the release. */
  chartName: string
  chartVersion: string
  appStatus: string
  projectId: number
  lastDeployedAt: string
  environmentDetail: HelmAppEnvironmentDetail
}

/* ---------------------------------------------------------------- alerts */

/**
 * One firing or pending alert.
 *
 * Optional where the server is `omitempty`, which is almost everywhere. This
 * used to declare them all required, and the type was simply wrong: an alert
 * with no severity arrives with no severity key, and code that trusted the
 * type crashed the page on the first one. Anything not marked required here
 * can genuinely be absent.
 */
export interface Alert {
  name: string
  state: string
  source: string
  severity?: string
  summary?: string
  description?: string
  labels?: Record<string, string> | null
  annotations?: Record<string, string> | null
  startsAt?: string
  fingerprint?: string
  expression?: string
  namespace?: string
  kind?: string
  resource?: string
}

/**
 * What `/v1/alerts` answers with.
 *
 * `unavailable` is the whole point of the envelope: an empty list because
 * nothing is wrong and an empty list because nobody could be asked are
 * different facts, and rendering the second as the first is how a monitoring
 * tool tells you a cluster is healthy while it is on fire.
 */
export interface AlertsResult {
  alerts: Alert[]
  /** Set when no alert source could be reached. The list is then meaningless. */
  unavailable?: string
  /** What discovery noticed on the way, reachable or not. */
  notes?: string[]
}

/* ------------------------------------------------------------------ runs */

export const RUN_STATUSES = [
  'queued',
  'running',
  'succeeded',
  'failed',
  'canceled',
  'budget_exceeded',
  'partial',
] as const
export type RunStatus = (typeof RUN_STATUSES)[number]

export type AppType = 'devtron' | 'helm' | ''

export interface RunScope {
  clusterId: number
  clusterName: string
  environmentId?: number
  environmentName?: string
  namespace?: string
  appName?: string
  appType?: AppType
}

export interface RunTrigger {
  kind: 'alert' | 'ask' | string
  alert?: Alert | null
  ask: string
}

export interface IntelligenceResult {
  requestId: string
  analysis: string
  thinkingCount: number
  durationMs: number
  /** Non-empty means Devtron's first pass errored; the run continues on facts alone. */
  failed: string
}

export const CLAIM_STATUSES = ['supported', 'contradicted', 'unverifiable'] as const
export type ClaimStatus = (typeof CLAIM_STATUSES)[number] | string

export interface VerdictClaim {
  claim: string
  status: ClaimStatus
  why: string
}

export const COMPONENT_LAYERS = [
  'k8s_workload',
  'k8s_infra',
  'devtron_cd',
  'devtron_platform',
  'known_app',
  'unknown',
] as const
export type ComponentLayer = (typeof COMPONENT_LAYERS)[number] | string

export interface IdentifiedComponent {
  layer: ComponentLayer
  kind: string
  name: string
  namespace: string
  /** Null/empty when nothing was recognised. */
  id?: string | null
  displayName?: string | null
  matchWhy?: string[] | null
}

export const VERDICT_KINDS = [
  'supported',
  'partly_supported',
  'unsupported',
  'insufficient',
] as const
export type VerdictKind = (typeof VERDICT_KINDS)[number] | string

export interface Verdict {
  verdict: VerdictKind
  confidence: number
  claims?: VerdictClaim[] | null
  component?: IdentifiedComponent | null
  gaps?: string[] | null
  nextChecks?: string[] | null
}

export interface Evidence {
  source: string
  detail: string
  ref: string
}

export const RISK_LEVELS = ['low', 'medium', 'high'] as const
export type RiskLevel = (typeof RISK_LEVELS)[number] | string

export interface Remediation {
  action: string
  why: string
  risk: RiskLevel
  verify: string
  rollback: string
}

export interface Report {
  agrees: boolean
  correctedRootCause: string
  confidence: number
  evidence?: Evidence[] | null
  remediation?: Remediation[] | null
  sreNotes: string
  unknowns?: string[] | null
}

export interface RunUsage {
  toolCalls: number
  modelCalls: number
  modelTokens: number
  maxToolCalls: number
  maxModelTokens: number
}

export interface Run {
  id: string
  status: RunStatus
  createdAt: string
  startedAt?: string | null
  finishedAt?: string | null
  durationMs: number
  scope: RunScope
  trigger: RunTrigger
  intelligence?: IntelligenceResult | null
  verdict?: Verdict | null
  report?: Report | null
  usage: RunUsage
  error: string
}

export interface CreateRunRequest {
  clusterId: number
  clusterName: string
  environmentId?: number
  namespace?: string
  appName?: string
  appType?: AppType
  alert?: Alert | null
  ask?: string
  /** Per-run knobs. Never prompts. */
  options?: RunOptions
}

/* ---------------------------------------------------------------- events */

export const EVENT_TYPES = [
  'run_queued',
  'status',
  'intelligence_start',
  'intelligence_thinking',
  'intelligence_analysis',
  'agent_start',
  'agent_end',
  'model_call',
  'tool_call',
  'tool_result',
  'finding',
  'budget',
  'error',
] as const
export type EventType = (typeof EVENT_TYPES)[number]

export type AgentName = 'sre' | 'intelligence' | string

/**
 * Structured failure on a `tool_result` event.
 *
 * `kind` says whose fault it is: `input` is the agent's own malformed call,
 * `cluster` is the target refusing or timing out, `platform` is us or Devtron.
 * The distinction matters to a reader deciding whether to trust the verdict.
 */
export interface ToolError {
  kind: 'input' | 'cluster' | 'platform' | string
  code: string
  message: string
  retryable: boolean
}

export interface RunEvent {
  seq: number
  at: string
  type: EventType | string
  agent?: AgentName
  payload?: Record<string, unknown> | null
}

/* ------------------------------------------------------------- knowledge */

export interface KnowledgeMetric {
  name: string
  type: string
  labels?: string[] | null
  help: string
  means: string
  query: string
}

export interface KnowledgeComponent {
  /** Primary key, e.g. "app.redis". */
  id: string
  /** ADK skill directory name, e.g. "redis". The stable slug for deep links. */
  skill: string
  name: string
  class: 'devtron' | 'app' | string
  summary: string
  metrics?: KnowledgeMetric[] | null
  repo: string
  /** Markdown; always non-empty on the single-component endpoint. */
  doc?: string
}

/* ----------------------------------------------------------------- utils */

export function isTerminal(status: RunStatus): boolean {
  return status === 'succeeded' || status === 'failed' || status === 'canceled' || status === 'budget_exceeded'
}

export function isLive(status: RunStatus): boolean {
  return status === 'queued' || status === 'running'
}

/** The exact error the backend sets when the process restarted mid-run. */
export const RESTART_INTERRUPTED_ERROR = 'the agent restarted while this run was in flight'

/**
 * An interruption is not an investigation failure. The run never reached a
 * conclusion because the process went away underneath it, so it gets calmer
 * wording and a re-run action instead of the red failure treatment.
 */
export function isInterrupted(run: Pick<Run, 'status' | 'error'>): boolean {
  return run.status === 'failed' && run.error.trim() === RESTART_INTERRUPTED_ERROR
}

/** Panel ① has four distinct states; `intelligence` is null until the pass starts. */
export type IntelligencePhase = 'not_started' | 'streaming' | 'complete' | 'failed'

export function intelligencePhase(run: Pick<Run, 'intelligence'> | undefined): IntelligencePhase {
  const i = run?.intelligence
  if (!i) return 'not_started'
  if (i.failed) return 'failed'
  return i.analysis ? 'complete' : 'streaming'
}

/** Operator-editable Devtron connection. The token is never returned. */
export interface Settings {
  devtronUrl: string
  tokenSet: boolean
  tokenHint?: string
  source: 'database' | 'environment'
  updatedAt?: string
  updatedBy?: string
  intelligencePath: string
}

export interface ProbeResult {
  ok: boolean
  message: string
  clusters: number
  names?: string[]
}

export interface SettingsBody {
  devtronUrl: string
  /** Empty keeps the stored token. */
  devtronToken: string
}

/** One cluster's measured reach, from the capability sweep. */
export type Reach = 'usable' | 'empty' | 'forbidden' | 'error' | 'unreachable' | 'unknown'

export interface KindAccess {
  kind: string
  allowed: boolean
  count: number
  detail?: string
}



export type Depth = 'auto' | 'quick' | 'deep'
export type SourceMode = 'auto' | 'on' | 'off'

/** Per-run knobs an operator sets before triggering. Never prompts. */
export interface RunOptions {
  depth?: Depth
  maxToolCalls?: number
  metrics?: SourceMode
  logs?: SourceMode
}

/* --------------------------------------------------------------- harness */

/**
 * The agent tree the binary actually builds.
 *
 * Served by `/v1/harness`, generated from the same constants the pipeline is
 * assembled from. It is not a description maintained alongside the code — it
 * is the code, read back.
 */
export interface HarnessTool {
  name: string
  package?: string
  description?: string
}

export interface HarnessAgent {
  name: string
  order: number
  model: string
  role: string
  /** The judge is bound none, on purpose. */
  tools: HarnessTool[] | string[]
  toolsets?: string[]
  outputKey: string
  reads: string[]
  /** The full system prompt, exactly as the binary embedded it. */
  instruction?: string
  /** Present only on a stage that can be skipped. */
  skippedWhen?: string
}

export interface HarnessStep {
  step: string
  kind: 'deterministic' | 'devtron' | 'agent' | string
  what: string
}

export interface HarnessSurface {
  package: string
  api: string
  auth: string
  tools: string[]
  /** What had to be done to turn that API into something a model can call. */
  conversion: string[]
}

export interface HarnessLayer {
  layer: string
  what: string
  /** Whether this could plausibly be declared rather than written. */
  declarative: boolean
}

export interface HarnessNormalise {
  item: string
  why: string
}

export interface HarnessEntrypoint {
  name: string
  trigger: string
  path: string
  creates: string
  why: string
  stages: string
}

export interface Harness {
  adk: {
    module: string
    root: { name: string; type: string }
    session: string
    uses: string[]
  }
  agents: HarnessAgent[]
  budget: { maxToolCalls: number; maxModelTokens: number; timeoutSeconds: number }
  entrypoints: HarnessEntrypoint[]
  pipeline: HarnessStep[]
  surfaces: HarnessSurface[]
  orchestration: HarnessLayer[]
  normalise: HarnessNormalise[]
  guarantees: string[]
}

/* ----------------------------------------------------------------- rules */

export const PRIORITIES = ['P0', 'P1', 'P2'] as const
export type Priority = (typeof PRIORITIES)[number]

/**
 * One condition against an alert payload.
 *
 * Every field is optional and they are ANDed, so an empty match matches
 * everything — which is what makes a bare `{}` a usable catch-all at the end
 * of a priority list.
 */
export interface RuleMatch {
  name?: string
  severity?: string[]
  namespace?: string[]
  kind?: string[]
  labels?: Record<string, string>
  labelsRegex?: Record<string, string>
}

export interface Rule {
  name?: string
  match: RuleMatch
  priority?: Priority
  enabled: boolean
  /**
   * Makes a rule with no conditions match everything.
   *
   * An empty match used to do this implicitly, and "Add rule" creates an
   * empty rule — so the instant anyone clicked it, every alert was claimed by
   * a half-written rule. A catch-all is a real thing to want at the bottom of
   * a priority list, but it has to be asked for.
   */
  catchAll?: boolean
}

export const CHANNELS = ['slack', 'discord', 'webhook'] as const
export type Channel = (typeof CHANNELS)[number]

/**
 * Where a cluster's findings go.
 *
 * One event only — a finished investigation. Every monitoring tool can
 * already say something broke; what none of them say is what it was and what
 * to do, which is why that is the only thing worth a notification.
 */
export interface NotifyConfig {
  enabled: boolean
  channel: Channel
  /** Only ever sent *to* the server. The server never sends it back. */
  url?: string
  /** True when a webhook is configured. */
  urlSet?: boolean
  /** The last few characters, to recognise which one it is. */
  urlHint?: string
  /** Explicit, because an empty url on save means "unchanged". */
  clearUrl?: boolean
  /** Suppress anything less urgent. Empty means everything. */
  minPriority?: Priority | ''
}

export interface RulesConfig {
  clusterId: number
  show: Rule[]
  mute: Rule[]
  auto: Rule[]
  autoEnabled: boolean
  priority: Rule[]
  notify: NotifyConfig
}

export interface RuleDecision {
  show: boolean
  priority: Priority
  auto: boolean
  /** The rule that set the priority, so it can be pointed at. */
  why?: string
  /** The rule that hid it. */
  mutedBy?: string
}

export interface RulePreview {
  rows: { alert: Alert; decision: RuleDecision }[]
  total: number
  shown: number
  muted: number
  auto: number
  byPriority: Record<Priority, number>
  unavailable?: string
}

/* ------------------------------------------------------------- incidents */

export const ALERT_STATES = ['firing', 'acknowledged', 'resolved'] as const
export type AlertState = (typeof ALERT_STATES)[number]

/** What an investigation concluded, flattened for a dashboard row. */
export interface Finding {
  runId: string
  rootCause?: string
  action?: string
  risk?: string
  agrees: boolean
  /** Devtron answered but nobody checked it. */
  unverified?: boolean
  confidence?: number
  at: string
}

export interface RunRef {
  runId: string
  status: RunStatus
  createdAt: string
}

/**
 * An alert we took responsibility for.
 *
 * Distinct from the live feed, which is a question asked of the cluster and
 * forgotten. This has an identity that survives flapping, a state, notes, and
 * the investigations run against it.
 */
export interface TrackedAlert {
  id: string
  /** The handle people say out loud: "#42 is still open". */
  seq: number
  clusterId: number
  clusterName: string
  dedupKey: string

  name: string
  severity?: string
  namespace?: string
  kind?: string
  resource?: string
  summary?: string
  labels?: Record<string, string>

  priority: Priority
  state: AlertState
  origin: 'manual' | 'rule'

  firstSeen: string
  lastSeen: string
  /** Why this table exists: one row however many times it fired. */
  seenCount: number

  ackedBy?: string
  ackedAt?: string
  resolvedAt?: string
  notes?: string
  updatedAt: string

  runs?: RunRef[]
  latest?: Finding
}

export interface AlertLogEntry {
  id: number
  at: string
  kind: string
  detail?: string
  actor?: string
}
