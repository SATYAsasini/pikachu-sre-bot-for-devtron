/**
 * Dev fixture mode.
 *
 * Lets the UI be built and demoed before the Go backend is up. It is off by
 * default and only reachable in three explicit ways:
 *
 *   - `VITE_FIXTURES=1` in .env.local
 *   - `?fixtures=1` in the URL (sticks in localStorage; `?fixtures=0` clears it)
 *   - the "FIXTURES" chip in the top bar
 *
 * This is deliberately a thin shim behind the real typed client in api.ts, not
 * a parallel implementation: every call still goes through the same signature,
 * so deleting this file would leave the app correct but offline-unfriendly.
 */

import type {
  Alert,
  AppConfig,
  Cluster,
  CreateRunRequest,
  DevtronApp,
  Environment,
  Health,
  HelmApp,
  KnowledgeComponent,
  MonitoringStack,
  ProbeResult,
  Run,
  RunEvent,
  RunStatus,
  Settings,
  SettingsBody,
} from '@/lib/types'

const FIXTURE_KEY = 'devtron.sre.fixtures'

function readFlag(): boolean {
  if (import.meta.env.VITE_FIXTURES === '1') return true
  try {
    const url = new URL(window.location.href)
    const q = url.searchParams.get('fixtures')
    if (q === '1') {
      localStorage.setItem(FIXTURE_KEY, '1')
      return true
    }
    if (q === '0') {
      localStorage.removeItem(FIXTURE_KEY)
      return false
    }
    return localStorage.getItem(FIXTURE_KEY) === '1'
  } catch {
    return false
  }
}

let cached: boolean | null = null

export function isFixtureMode(): boolean {
  if (cached === null) cached = readFlag()
  return cached
}

export function setFixtureMode(on: boolean): void {
  try {
    if (on) localStorage.setItem(FIXTURE_KEY, '1')
    else localStorage.removeItem(FIXTURE_KEY)
  } catch {
    /* storage unavailable; the reload below simply will not stick */
  }
  cached = on
  window.location.reload()
}

/* -------------------------------------------------------------- latency */

const delay = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))
async function slow<T>(value: T, ms = 320): Promise<T> {
  await delay(ms)
  return value
}

/* ----------------------------------------------------------------- data */

// The measurement rides on the row now, so the fixture carries it too —
// including the unusable ones, which is what the setup screen exists to show.
const now = () => new Date().toISOString()
const CLUSTERS: Cluster[] = [
  { id: 1, clusterName: 'tenant-acme-prod', serverUrl: 'https://acme-prod.k8s.internal', isVirtualCluster: false, errorInConnecting: '',
    reach: 'usable', reachWhy: 'Reachable, and returning workloads.', latencyMs: 768, probedAt: now(), investigable: true },
  { id: 2, clusterName: 'tenant-acme-stage', serverUrl: 'https://acme-stage.k8s.internal', isVirtualCluster: false, errorInConnecting: '',
    reach: 'usable', reachWhy: 'Reachable, and returning workloads.', latencyMs: 704, probedAt: now(), investigable: true },
  { id: 3, clusterName: 'edge-eu-west', serverUrl: 'https://edge-eu-west.k8s.internal', isVirtualCluster: false, errorInConnecting: 'dial tcp 10.4.0.11:6443: i/o timeout',
    reach: 'unreachable', reachWhy: 'The orchestrator timed out talking to this cluster. Nothing can be read from it.',
    detail: 'timed out; the orchestrator could not reach the cluster', latencyMs: 8000, probedAt: now(), investigable: false },
  { id: 4, clusterName: 'virtual-sandbox', serverUrl: '', isVirtualCluster: true, errorInConnecting: '',
    reach: 'empty', reachWhy: 'Reachable, but nothing came back. Either the cluster is empty or this token cannot see into it — those look identical from here.',
    latencyMs: 190, probedAt: now(), investigable: false },
]

const ENVIRONMENTS: Environment[] = [
  { environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', clusterId: 1, clusterName: 'tenant-acme-prod', isVirtualCluster: false },
  { environmentId: 6, environmentName: 'prod-checkout', namespace: 'checkout', clusterId: 1, clusterName: 'tenant-acme-prod', isVirtualCluster: false },
  { environmentId: 7, environmentName: 'prod-platform', namespace: 'platform', clusterId: 1, clusterName: 'tenant-acme-prod', isVirtualCluster: false },
  { environmentId: 11, environmentName: 'stage-ns', namespace: 'payments', clusterId: 2, clusterName: 'tenant-acme-stage', isVirtualCluster: false },
  { environmentId: 12, environmentName: 'stage-checkout', namespace: 'checkout', clusterId: 2, clusterName: 'tenant-acme-stage', isVirtualCluster: false },
  { environmentId: 21, environmentName: 'edge', namespace: 'edge', clusterId: 3, clusterName: 'edge-eu-west', isVirtualCluster: false },
]

const MONITORING: Record<number, MonitoringStack> = {
  1: {
    clusterId: 1,
    clusterName: 'tenant-acme-prod',
    metrics: { flavor: 'victoriametrics', service: { namespace: 'monitoring', name: 'vmsingle-vm', port: '' }, apiBase: '/prometheus', reachable: true, detail: '' },
    alerts: { flavor: 'vmalert', service: { namespace: 'monitoring', name: 'vmalert-vm', port: '' }, apiBase: '', reachable: true, detail: '' },
    discoveredAt: '2026-09-23T06:10:00Z',
    notes: [],
  },
  2: {
    clusterId: 2,
    clusterName: 'tenant-acme-stage',
    metrics: { flavor: 'prometheus', service: { namespace: 'monitoring', name: 'prometheus-k8s', port: '9090' }, apiBase: '', reachable: false, detail: 'HTTP 503 from /api/v1/query: service has no endpoints' },
    alerts: null,
    discoveredAt: '2026-09-23T06:09:12Z',
    notes: [
      'prometheus-k8s answered 503; metric coverage is unknown, not zero',
      'no alert source answered; firing alerts cannot be listed for this cluster',
    ],
  },
  3: {
    clusterId: 3,
    clusterName: 'edge-eu-west',
    metrics: null,
    alerts: null,
    discoveredAt: '2026-09-23T05:55:00Z',
    notes: ['cluster is unreachable through the orchestrator, so nothing could be discovered'],
  },
  4: { clusterId: 4, clusterName: 'virtual-sandbox', metrics: null, alerts: null, discoveredAt: '2026-09-23T06:00:00Z', notes: ['virtual cluster: no monitoring to discover'] },
}

const APPS: DevtronApp[] = [
  {
    appId: 42,
    appName: 'payments-api',
    projectId: 3,
    environments: [
      { environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', clusterName: 'tenant-acme-prod', status: 'Succeeded', appStatus: 'Degraded', lastDeployedTime: '2026-09-22T18:04:11Z' },
      { environmentId: 11, environmentName: 'stage-ns', namespace: 'payments', clusterName: 'tenant-acme-stage', status: 'Succeeded', appStatus: 'Healthy', lastDeployedTime: '2026-09-21T09:12:00Z' },
    ],
  },
  {
    appId: 43,
    appName: 'ledger-worker',
    projectId: 3,
    environments: [{ environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', clusterName: 'tenant-acme-prod', status: 'Succeeded', appStatus: 'Healthy', lastDeployedTime: '2026-09-20T11:30:00Z' }],
  },
  {
    appId: 51,
    appName: 'checkout-web',
    projectId: 4,
    environments: [{ environmentId: 6, environmentName: 'prod-checkout', namespace: 'checkout', clusterName: 'tenant-acme-prod', status: 'Failed', appStatus: 'Degraded', lastDeployedTime: '2026-09-23T04:51:02Z' }],
  },
  {
    appId: 61,
    appName: 'notification-svc',
    projectId: 4,
    environments: [{ environmentId: 7, environmentName: 'prod-platform', namespace: 'platform', clusterName: 'tenant-acme-prod', status: 'Succeeded', appStatus: 'Healthy', lastDeployedTime: '2026-09-19T15:00:00Z' }],
  },
]

const HELM_APPS: HelmApp[] = [
  { appId: '1|payments|redis', appName: 'redis', chartName: 'redis', chartVersion: '18.1.2', appStatus: 'Healthy', projectId: 0, lastDeployedAt: '2026-09-20T10:00:00Z', environmentDetail: { environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', clusterId: 1, clusterName: 'tenant-acme-prod' } },
  { appId: '1|payments|postgres-primary', appName: 'postgres-primary', chartName: 'postgresql', chartVersion: '15.5.1', appStatus: 'Degraded', projectId: 0, lastDeployedAt: '2026-09-11T08:20:00Z', environmentDetail: { environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', clusterId: 1, clusterName: 'tenant-acme-prod' } },
  { appId: '1|platform|kafka', appName: 'kafka', chartName: 'kafka', chartVersion: '26.0.0', appStatus: 'Healthy', projectId: 0, lastDeployedAt: '2026-08-30T12:00:00Z', environmentDetail: { environmentId: 7, environmentName: 'prod-platform', namespace: 'platform', clusterId: 1, clusterName: 'tenant-acme-prod' } },
]

const ALERTS: Alert[] = [
  {
    name: 'KubePodCrashLooping',
    state: 'firing',
    severity: 'critical',
    summary: 'Pod payments/payments-api-7d9f is crash looping',
    description: 'Pod has restarted 14 times in the last 10 minutes',
    labels: { alertname: 'KubePodCrashLooping', namespace: 'payments', pod: 'payments-api-7d9f', severity: 'critical' },
    annotations: { summary: 'Pod payments/payments-api-7d9f is crash looping' },
    startsAt: '2026-09-23T05:41:00Z',
    fingerprint: 'a1b2c3d4e5f6',
    source: 'alertmanager',
    expression: '',
    namespace: 'payments',
    kind: 'Pod',
    resource: 'payments-api-7d9f',
  },
  {
    name: 'RedisMemoryHigh',
    state: 'firing',
    severity: 'warning',
    summary: 'Redis memory usage above 92% in payments',
    description: 'redis_memory_used_bytes / container limit has exceeded 0.92 for 15m',
    labels: { alertname: 'RedisMemoryHigh', namespace: 'payments', service: 'redis-master', severity: 'warning' },
    annotations: {},
    startsAt: '2026-09-23T05:12:00Z',
    fingerprint: 'bb1199ffee00',
    source: 'vmalert',
    expression: 'redis_memory_used_bytes / on(pod) kube_pod_container_resource_limits > 0.92',
    namespace: 'payments',
    kind: 'StatefulSet',
    resource: 'redis-master',
  },
  {
    name: 'TargetDown',
    state: 'firing',
    severity: 'warning',
    summary: '1 of 4 ledger-worker targets are down',
    description: 'Scrape of ledger-worker has failed for 10m',
    labels: { alertname: 'TargetDown', namespace: 'payments', job: 'ledger-worker', severity: 'warning' },
    annotations: {},
    startsAt: '2026-09-23T04:02:00Z',
    fingerprint: '77aa33cc11dd',
    source: 'vmalert',
    expression: 'up{job="ledger-worker"} == 0',
    namespace: 'payments',
    kind: 'Deployment',
    resource: 'ledger-worker',
  },
  {
    name: 'CheckoutLatencyHigh',
    state: 'pending',
    severity: 'critical',
    summary: 'p99 checkout latency 2.4s (SLO 800ms)',
    description: 'histogram_quantile(0.99, ...) has been above the objective for 4m',
    labels: { alertname: 'CheckoutLatencyHigh', namespace: 'checkout', severity: 'critical' },
    annotations: {},
    startsAt: '2026-09-23T06:05:00Z',
    fingerprint: 'ff00aa22bb33',
    source: 'vmalert',
    expression: 'histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{app="checkout-web"}[5m]))) > 0.8',
    namespace: 'checkout',
    kind: 'Deployment',
    resource: 'checkout-web',
  },
  {
    name: 'PersistentVolumeFillingUp',
    state: 'firing',
    severity: 'info',
    summary: 'PVC data-postgres-primary-0 is 81% full',
    description: 'At the current write rate the volume fills in about 6 days',
    labels: { alertname: 'PersistentVolumeFillingUp', namespace: 'payments', persistentvolumeclaim: 'data-postgres-primary-0', severity: 'info' },
    annotations: {},
    startsAt: '2026-09-22T22:15:00Z',
    fingerprint: '5a5a5a5a5a5a',
    source: 'alertmanager',
    expression: '',
    namespace: 'payments',
    kind: 'PersistentVolumeClaim',
    resource: 'data-postgres-primary-0',
  },
]

const KNOWLEDGE: KnowledgeComponent[] = [
  {
    id: 'app.redis',
    skill: 'redis',
    name: 'Redis',
    class: 'app',
    summary: 'In-memory store. Nearly every Redis incident is memory, eviction or persistence.',
    metrics: [
      { name: 'redis_memory_used_bytes', type: 'gauge', labels: ['instance'], help: 'Resident memory held by the Redis process', means: 'Only meaningful as a ratio against maxmemory. On its own it tells you nothing about pressure.', query: 'redis_memory_used_bytes / redis_memory_max_bytes' },
      { name: 'redis_memory_max_bytes', type: 'gauge', labels: ['instance'], help: 'Configured maxmemory', means: 'Zero means maxmemory was never set, so Redis will never evict and the kernel will do the killing instead.', query: 'redis_memory_max_bytes == 0' },
      { name: 'redis_evicted_keys_total', type: 'counter', labels: ['instance'], help: 'Keys evicted since start', means: 'Flat at zero under memory pressure is the tell that eviction is not configured. Climbing is healthy, not alarming.', query: 'rate(redis_evicted_keys_total[5m])' },
      { name: 'redis_keyspace_hits_total', type: 'counter', labels: ['instance'], help: 'Lookups that found a key', means: 'Watch the hit ratio after enabling eviction. A collapsing ratio means the cache is undersized, not misconfigured.', query: '' },
    ],
    repo: 'github.com/oliver006/redis_exporter',
    doc: '# Redis\n\n## Top failure modes\n\n1. **maxmemory unset in a container.** The kernel OOM-kills before Redis ever evicts.\n2. **AOF rewrite storms** during heavy writes.\n',
  },
  {
    id: 'devtron.cd',
    skill: 'devtron_cd',
    name: 'Devtron CD',
    class: 'devtron',
    summary: 'Deployment pipeline. Failures are usually image pull, hook timeout or a stuck sync.',
    metrics: [
      { name: 'cd_pipeline_duration_seconds', type: 'histogram', labels: ['pipeline'], help: 'Wall time of a CD pipeline', means: 'A long tail with a normal median almost always means a hook is waiting on something, not that the deploy is slow.', query: '' },
    ],
    repo: 'github.com/devtron-labs/devtron',
    doc: '# Devtron CD\n\nPipelines fail loudest at the pre-sync hook.\n',
  },
]

/* ----------------------------------------------------------------- runs */

const SAMPLE_ANALYSIS = `## Root cause

The container in \`payments/redis-master-0\` **exits with code 137** shortly after
the working set grows past ~500Mi. The kernel cgroup OOM killer is the one doing
the killing, not Redis itself.

### What I looked at

- Pod restart history over the last hour
- \`kubectl describe\` on the StatefulSet
- The last 200 lines of container logs

### Suggested fix

Increase the memory limit on the StatefulSet from \`512Mi\` to \`2Gi\`.

> Restarts began at 05:38 and have continued at roughly 4-minute intervals.
`

const SAMPLE_INTELLIGENCE = {
  requestId: 'oneshot-3f9a2b',
  analysis: SAMPLE_ANALYSIS,
  thinkingCount: 7,
  durationMs: 38000,
  failed: '',
}

const SAMPLE_VERDICT = {
  verdict: 'partly_supported' as const,
  confidence: 0.72,
  claims: [
    { claim: 'The container is OOMKilled', status: 'supported', why: 'Last terminated state reports reason OOMKilled with exit code 137' },
    { claim: 'Redis itself is evicting keys under pressure', status: 'contradicted', why: 'redis_evicted_keys_total is flat at 0; nothing has ever been evicted' },
    { claim: 'Restarts began at 05:38', status: 'supported', why: "Matches the pod's restart timestamps" },
    { claim: 'Raising the memory limit resolves the incident', status: 'unverifiable', why: 'No data on the steady-state working set; a longer window is required' },
  ],
  component: {
    layer: 'known_app',
    kind: 'StatefulSet',
    name: 'redis-master',
    namespace: 'payments',
    id: 'app.redis',
    displayName: 'Redis',
    matchWhy: ['helm chart redis', 'label app.kubernetes.io/name=redis'],
  },
  gaps: ['No metrics were consulted by the first pass; memory pressure was inferred from the exit code alone'],
  nextChecks: ['Query redis_memory_used_bytes / redis_memory_max_bytes over the last hour', 'Check whether maxmemory-policy is set on the running config'],
}

const SAMPLE_REPORT = {
  agrees: false,
  correctedRootCause:
    'Redis has no `maxmemory` configured, so the container hit its pod memory limit and was killed by the kernel rather than evicting keys. Raising the limit alone moves the cliff; it does not remove it.',
  confidence: 0.86,
  evidence: [
    { source: 'prom.query', detail: 'redis_memory_max_bytes == 0 for all instances', ref: 'ev:12' },
    { source: 'k8s.get', detail: 'container limit 512Mi; last state terminated reason OOMKilled', ref: 'ev:9' },
    { source: 'prom.query', detail: 'redis_evicted_keys_total has been flat at 0 for 7 days', ref: 'ev:14' },
    { source: 'knowledge', detail: 'Unset maxmemory in a container is the root cause behind most Redis OOMKills', ref: 'app.redis' },
  ],
  remediation: [
    {
      action: 'Set `maxmemory` to ~80% of the container limit and `maxmemory-policy` to `allkeys-lru`',
      why: 'Lets Redis evict instead of being killed by the kernel',
      risk: 'low',
      verify: 'redis_evicted_keys_total begins incrementing and restarts stop',
      rollback: 'Remove the two config keys and restart',
    },
    {
      action: 'Raise the pod memory limit to 1Gi',
      why: 'Buys headroom if the working set is genuinely larger than the current limit',
      risk: 'medium',
      verify: 'redis_memory_used_bytes plateaus below the new limit',
      rollback: 'Revert the limit in the Helm values and redeploy',
    },
    {
      action: 'Move the session cache off the shared Redis onto its own release',
      why: 'Removes the noisy-neighbour coupling between sessions and the rate limiter',
      risk: 'high',
      verify: 'Both releases stay under 60% of their limits for a full peak window',
      rollback: 'Repoint the session client at the original endpoint',
    },
  ],
  sreNotes:
    'Watch the keyspace hit ratio after enabling eviction; a collapsing hit rate means the cache is undersized rather than misconfigured.',
  unknowns: ['Whether the working set genuinely exceeds 512Mi, which needs a longer observation window'],
}

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString()
}

const SEED_RUNS: Run[] = [
  {
    id: '7e30c411-aa62-4c98-bb01-12f3d4a5b6c7',
    status: 'failed',
    createdAt: iso(48),
    startedAt: iso(48),
    finishedAt: iso(47),
    durationMs: 19_400,
    scope: { clusterId: 1, clusterName: 'tenant-acme-prod', environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', appName: 'ledger-worker', appType: 'devtron' },
    trigger: { kind: 'alert', alert: ALERTS[2], ask: 'Alert TargetDown is firing for payments/ledger-worker.' },
    intelligence: { ...SAMPLE_INTELLIGENCE, requestId: 'oneshot-7e30c4', analysis: '## Root cause\n\nOne of four replicas is failing its readiness probe after the 04:00 rollout.\n', thinkingCount: 2 },
    verdict: null,
    report: null,
    usage: { toolCalls: 3, modelCalls: 1, modelTokens: 11_200, maxToolCalls: 25, maxModelTokens: 400_000 },
    error: 'the agent restarted while this run was in flight',
  },
  {
    id: '9f1c2f7e-2b11-4d0a-9f1c-2f7e2b114d0a',
    status: 'succeeded',
    createdAt: iso(22),
    startedAt: iso(22),
    finishedAt: iso(20),
    durationMs: 112430,
    scope: { clusterId: 1, clusterName: 'tenant-acme-prod', environmentId: 5, environmentName: 'prod-ns', namespace: 'payments', appName: 'redis', appType: 'helm' },
    trigger: { kind: 'alert', alert: ALERTS[1], ask: 'Alert RedisMemoryHigh is firing for payments/redis-master.' },
    intelligence: SAMPLE_INTELLIGENCE,
    verdict: SAMPLE_VERDICT,
    report: SAMPLE_REPORT,
    usage: { toolCalls: 14, modelCalls: 6, modelTokens: 118_402, maxToolCalls: 25, maxModelTokens: 400_000 },
    error: '',
  },
  {
    id: '3a77bd01-55c9-4a2e-b0d1-0f9a7c2e1188',
    status: 'budget_exceeded',
    createdAt: iso(95),
    startedAt: iso(95),
    finishedAt: iso(91),
    durationMs: 241_000,
    scope: { clusterId: 1, clusterName: 'tenant-acme-prod', environmentId: 6, environmentName: 'prod-checkout', namespace: 'checkout', appName: 'checkout-web', appType: 'devtron' },
    trigger: { kind: 'alert', alert: ALERTS[3], ask: 'Alert CheckoutLatencyHigh is pending for checkout/checkout-web.' },
    intelligence: { ...SAMPLE_INTELLIGENCE, requestId: 'oneshot-77bd01', analysis: '## Root cause\n\nUpstream `ledger-worker` is saturating its connection pool, so checkout requests queue behind it.\n', thinkingCount: 4 },
    verdict: { ...SAMPLE_VERDICT, verdict: 'insufficient', confidence: 0.31, claims: SAMPLE_VERDICT.claims.slice(0, 2), component: { layer: 'k8s_workload', kind: 'Deployment', name: 'checkout-web', namespace: 'checkout', id: null, displayName: null, matchWhy: [] }, gaps: ['Tool budget ran out before traces could be read'], nextChecks: [] },
    report: null,
    usage: { toolCalls: 25, modelCalls: 11, modelTokens: 402_118, maxToolCalls: 25, maxModelTokens: 400_000 },
    error: 'tool call budget exhausted after 25 calls',
  },
  {
    id: 'c2b4ef90-8d3a-41b7-9c55-aa01de773100',
    status: 'failed',
    createdAt: iso(180),
    startedAt: iso(180),
    finishedAt: iso(179),
    durationMs: 8_120,
    scope: { clusterId: 2, clusterName: 'tenant-acme-stage', environmentId: 11, environmentName: 'stage-ns', namespace: 'payments', appName: 'payments-api', appType: 'devtron' },
    trigger: { kind: 'ask', alert: null, ask: 'Why did the last rollout of payments-api in stage sit in Progressing for 20 minutes?' },
    intelligence: { requestId: 'oneshot-c2b4ef', analysis: '', thinkingCount: 0, durationMs: 4100, failed: 'devtron /athena/intelligence: HTTP 401: token expired' },
    verdict: null,
    report: null,
    usage: { toolCalls: 2, modelCalls: 1, modelTokens: 3_902, maxToolCalls: 25, maxModelTokens: 400_000 },
    error: 'devtron_unreachable: /athena/intelligence: HTTP 401: token expired',
  },
  {
    id: '4d8e1a63-90cc-4f21-8a11-b7d2f0e44551',
    status: 'canceled',
    createdAt: iso(320),
    startedAt: iso(320),
    finishedAt: iso(319),
    durationMs: 31_000,
    scope: { clusterId: 1, clusterName: 'tenant-acme-prod', environmentId: 7, environmentName: 'prod-platform', namespace: 'platform', appName: 'kafka', appType: 'helm' },
    trigger: { kind: 'ask', alert: null, ask: 'Consumer lag on the settlement topic keeps climbing after 18:00 every day.' },
    intelligence: { ...SAMPLE_INTELLIGENCE, requestId: 'oneshot-4d8e1a', analysis: '## Root cause\n\nThe nightly reconciliation job co-schedules onto the same nodes as the brokers.\n', thinkingCount: 3 },
    verdict: null,
    report: null,
    usage: { toolCalls: 5, modelCalls: 2, modelTokens: 22_100, maxToolCalls: 25, maxModelTokens: 400_000 },
    error: '',
  },
]

/** Mutable in-memory store, so created runs behave like real ones for a session. */
const runStore = new Map<string, Run>(SEED_RUNS.map((r) => [r.id, r]))
const eventStore = new Map<string, RunEvent[]>()

/** Events for the seeded, already-finished runs. */
function seedEvents(run: Run): RunEvent[] {
  const base = new Date(run.createdAt).getTime()
  const at = (s: number) => new Date(base + s * 1000).toISOString()
  const out: RunEvent[] = [
    { seq: 1, at: at(0), type: 'run_queued', payload: { scope: run.scope } },
    { seq: 2, at: at(1), type: 'status', payload: { status: 'running' } },
    { seq: 3, at: at(1), type: 'intelligence_start', agent: 'intelligence', payload: { requestId: run.intelligence?.requestId ?? '' } },
  ]
  let seq = 4
  const thinking = run.intelligence?.thinkingCount ?? 0
  for (let i = 0; i < thinking; i++) {
    out.push({ seq: seq++, at: at(3 + i * 4), type: 'intelligence_thinking', agent: 'intelligence', payload: { text: THINKING_LINES[i % THINKING_LINES.length] } })
  }
  if (run.intelligence?.failed) {
    out.push({ seq: seq++, at: at(6), type: 'error', agent: 'intelligence', payload: { message: run.intelligence.failed } })
  } else {
    out.push({ seq: seq++, at: at(40), type: 'intelligence_analysis', agent: 'intelligence', payload: { chars: run.intelligence?.analysis.length ?? 0 } })
  }
  if (run.verdict) {
    out.push({ seq: seq++, at: at(41), type: 'agent_start', agent: 'sre', payload: {} })
    out.push({ seq: seq++, at: at(44), type: 'tool_call', agent: 'sre', payload: { tool: 'k8s.get', args: { kind: 'Pod', namespace: run.scope.namespace } } })
    out.push({ seq: seq++, at: at(46), type: 'tool_result', agent: 'sre', payload: { tool: 'k8s.get', ok: true, summary: '3 pods, 1 with restartCount=14' } })
    out.push({ seq: seq++, at: at(52), type: 'agent_end', agent: 'sre', payload: { verdict: run.verdict.verdict } })
  }
  if (run.report) {
    out.push({ seq: seq++, at: at(53), type: 'agent_start', agent: 'sre', payload: {} })
    out.push({ seq: seq++, at: at(58), type: 'tool_call', agent: 'sre', payload: { tool: 'prom.query', args: { expr: 'redis_memory_max_bytes' } } })
    out.push({ seq: seq++, at: at(60), type: 'tool_result', agent: 'sre', payload: { tool: 'prom.query', ok: true, summary: '0 for all 3 instances' } })
    out.push({ seq: seq++, at: at(64), type: 'finding', agent: 'sre', payload: { ref: 'ev:12', detail: 'maxmemory is unset' } })
    out.push({ seq: seq++, at: at(96), type: 'agent_end', agent: 'sre', payload: { agrees: run.report.agrees } })
  }
  if (run.error) out.push({ seq: seq++, at: at(97), type: 'error', payload: { message: run.error } })
  out.push({ seq, at: at(98), type: 'status', payload: { status: run.status } })
  return out
}

const THINKING_LINES = [
  'Checking whether the pod has a terminated container state',
  'Restart count is 14 in 10 minutes, so this is a tight crash loop',
  'Exit code 137 usually means the kernel killed it',
  'Looking for a memory limit on the container spec',
  'Limit is 512Mi, which is small for a cache of this shape',
  'No OOMKilled string in the logs themselves, only in the pod status',
  'Settling on memory pressure as the most likely cause',
]

/* ------------------------------------------------------- scripted stream */

interface ScriptStep {
  afterMs: number
  event: Omit<RunEvent, 'seq' | 'at'>
  /** Applied to the stored run when this step fires. */
  patch?: (run: Run) => void
}

function script(): ScriptStep[] {
  const steps: ScriptStep[] = [
    { afterMs: 250, event: { type: 'run_queued', payload: {} } },
    { afterMs: 400, event: { type: 'status', payload: { status: 'running' } }, patch: (r) => { r.status = 'running'; r.startedAt = new Date().toISOString() } },
    { afterMs: 700, event: { type: 'intelligence_start', agent: 'intelligence', payload: { requestId: 'oneshot-live01' } } },
  ]
  THINKING_LINES.forEach((text, i) => {
    steps.push({ afterMs: 1400 + i * 900, event: { type: 'intelligence_thinking', agent: 'intelligence', payload: { text } } })
  })
  steps.push({
    afterMs: 8200,
    event: { type: 'intelligence_analysis', agent: 'intelligence', payload: { chars: SAMPLE_ANALYSIS.length } },
    patch: (r) => { r.intelligence = { ...SAMPLE_INTELLIGENCE, requestId: 'oneshot-live01' } },
  })
  steps.push({ afterMs: 8600, event: { type: 'agent_start', agent: 'sre', payload: {} } })
  steps.push({ afterMs: 9200, event: { type: 'model_call', agent: 'sre', payload: { tokens: 8_140 } }, patch: (r) => { r.usage = { ...r.usage, modelCalls: r.usage.modelCalls + 1, modelTokens: r.usage.modelTokens + 8_140 } } })
  steps.push({ afterMs: 10_000, event: { type: 'tool_call', agent: 'sre', payload: { tool: 'k8s.get', args: { kind: 'Pod', namespace: 'payments' } } }, patch: (r) => { r.usage = { ...r.usage, toolCalls: r.usage.toolCalls + 1 } } })
  steps.push({ afterMs: 11_100, event: { type: 'tool_result', agent: 'sre', payload: { tool: 'k8s.get', ok: true, summary: 'redis-master-0 restartCount=14, lastState.terminated.reason=OOMKilled' } } })
  steps.push({ afterMs: 12_000, event: { type: 'tool_call', agent: 'sre', payload: { tool: 'prom.query', args: { expr: 'redis_evicted_keys_total' } } }, patch: (r) => { r.usage = { ...r.usage, toolCalls: r.usage.toolCalls + 1 } } })
  steps.push({ afterMs: 13_200, event: { type: 'tool_result', agent: 'sre', payload: { tool: 'prom.query', ok: true, summary: '0 across all instances for 7d' } } })
  steps.push({
    afterMs: 14_500,
    event: { type: 'agent_end', agent: 'sre', payload: { verdict: 'partly_supported' } },
    patch: (r) => { r.verdict = SAMPLE_VERDICT },
  })
  steps.push({ afterMs: 15_000, event: { type: 'agent_start', agent: 'sre', payload: {} } })
  steps.push({ afterMs: 15_900, event: { type: 'tool_call', agent: 'sre', payload: { tool: 'prom.query', args: { expr: 'redis_memory_max_bytes' } } }, patch: (r) => { r.usage = { ...r.usage, toolCalls: r.usage.toolCalls + 1 } } })
  steps.push({ afterMs: 17_000, event: { type: 'tool_result', agent: 'sre', payload: { tool: 'prom.query', ok: true, summary: '0 for all 3 instances — maxmemory is unset' } } })
  steps.push({ afterMs: 18_100, event: { type: 'finding', agent: 'sre', payload: { ref: 'ev:12', detail: 'maxmemory is unset, so Redis can never evict' } } })
  steps.push({ afterMs: 19_000, event: { type: 'tool_call', agent: 'sre', payload: { tool: 'knowledge.get', args: { id: 'app.redis' } } }, patch: (r) => { r.usage = { ...r.usage, toolCalls: r.usage.toolCalls + 1 } } })
  steps.push({ afterMs: 20_100, event: { type: 'tool_result', agent: 'sre', payload: { tool: 'knowledge.get', ok: true, summary: 'Redis knowledge pack, 4 failure modes' } } })
  steps.push({ afterMs: 21_000, event: { type: 'budget', payload: { toolCalls: 5, maxToolCalls: 25, modelTokens: 96_000, maxModelTokens: 400_000 } } })
  steps.push({
    afterMs: 23_000,
    event: { type: 'agent_end', agent: 'sre', payload: { agrees: false } },
    patch: (r) => { r.report = SAMPLE_REPORT },
  })
  steps.push({
    afterMs: 23_600,
    event: { type: 'status', payload: { status: 'succeeded' } },
    patch: (r) => {
      r.status = 'succeeded'
      r.finishedAt = new Date().toISOString()
      r.durationMs = r.startedAt ? Date.now() - new Date(r.startedAt).getTime() : 23_600
    },
  })
  return steps
}

const liveScripts = new Map<string, ScriptStep[]>()

export interface FixtureStreamHandlers {
  /** Fired asynchronously once the replay is armed, mirroring EventSource's `open`. */
  onOpen: () => void
  onEvent: (event: RunEvent) => void
  onDone: () => void
}

/** Replays the scripted run for `id`. Returns an unsubscribe function. */
export function openFixtureStream(id: string, after: number, handlers: FixtureStreamHandlers): () => void {
  const run = runStore.get(id)
  const steps = liveScripts.get(id)
  const timers: number[] = []

  timers.push(window.setTimeout(handlers.onOpen, 0))

  if (!steps || !run) {
    // Finished run: replay the ledger immediately, then close.
    const evts = (eventStore.get(id) ?? (run ? seedEvents(run) : [])).filter((e) => e.seq > after)
    if (run) eventStore.set(id, eventStore.get(id) ?? seedEvents(run))
    timers.push(
      window.setTimeout(() => {
        for (const e of evts) handlers.onEvent(e)
        handlers.onDone()
      }, 120),
    )
    return () => timers.forEach(window.clearTimeout)
  }

  const existing = eventStore.get(id) ?? []
  let seq = existing.length
  const startedAt = Date.now()

  for (const step of steps) {
    timers.push(
      window.setTimeout(
        () => {
          seq += 1
          const event: RunEvent = { seq, at: new Date().toISOString(), ...step.event }
          const cur = runStore.get(id)
          if (cur && step.patch) step.patch(cur)
          eventStore.set(id, [...(eventStore.get(id) ?? []), event])
          if (seq > after) handlers.onEvent(event)
        },
        Math.max(0, step.afterMs - (Date.now() - startedAt)),
      ),
    )
  }
  const last = steps[steps.length - 1]
  timers.push(
    window.setTimeout(() => {
      liveScripts.delete(id)
      handlers.onDone()
    }, last.afterMs + 300),
  )

  return () => timers.forEach(window.clearTimeout)
}

/* ------------------------------------------------------------ the shim */

export const fixtures = {
  health: () => slow<Health>({ status: 'ok' }, 120),
  config: () => slow<AppConfig>({ devtronUrl: 'https://devtron.example.com', models: { provider: 'anthropic', fast: 'claude-haiku-4-5', strong: 'claude-haiku-4-5' }, features: { fixtures: true } }, 120),
  clusters: (all = false) => slow(all ? CLUSTERS : CLUSTERS.filter((c) => c.investigable)),
  environments: (clusterId?: number) => slow(clusterId === undefined ? ENVIRONMENTS : ENVIRONMENTS.filter((e) => e.clusterId === clusterId)),
  monitoring: (clusterId: number) => slow(MONITORING[clusterId] ?? { clusterId, clusterName: '', metrics: null, alerts: null, discoveredAt: new Date().toISOString(), notes: ['no monitoring discovered for this cluster'] }),

  apps: (p: { environmentId?: number; search?: string }) =>
    slow(
      APPS.filter((a) => (p.environmentId === undefined || (a.environments ?? []).some((e) => e.environmentId === p.environmentId)))
        .filter((a) => !p.search || a.appName.toLowerCase().includes(p.search.toLowerCase())),
    ),

  helmApps: (p: { clusterId?: number; environmentId?: number; search?: string }) =>
    slow(
      HELM_APPS.filter((a) => (p.clusterId === undefined || a.environmentDetail.clusterId === p.clusterId))
        .filter((a) => (p.environmentId === undefined || a.environmentDetail.environmentId === p.environmentId))
        .filter((a) => !p.search || a.appName.toLowerCase().includes(p.search.toLowerCase())),
    ),

  alerts: (p: { clusterId?: number; namespace?: string; severity?: string; nameLike?: string; includePending?: boolean; limit?: number }) => {
    if (p.clusterId === 2) return slow<Alert[]>([], 260) // stage has no alert source; see MONITORING[2].notes
    if (p.clusterId === 3) return Promise.reject(new Error('cluster edge-eu-west is unreachable'))
    let out = ALERTS.slice()
    if (!p.includePending) out = out.filter((a) => a.state !== 'pending')
    if (p.namespace) out = out.filter((a) => a.namespace === p.namespace)
    if (p.severity) out = out.filter((a) => a.severity === p.severity)
    if (p.nameLike) out = out.filter((a) => a.name.toLowerCase().includes(p.nameLike!.toLowerCase()))
    if (p.limit) out = out.slice(0, p.limit)
    return slow(out, 420)
  },

  runs: (p: { status?: RunStatus; clusterId?: number; limit?: number }) => {
    let out = [...runStore.values()].sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    if (p.status) out = out.filter((r) => r.status === p.status)
    if (p.clusterId !== undefined) out = out.filter((r) => r.scope.clusterId === p.clusterId)
    if (p.limit) out = out.slice(0, p.limit)
    return slow(out)
  },

  run: (id: string) => {
    const r = runStore.get(id)
    if (!r) return Promise.reject(new Error(`run ${id} not found`))
    return slow({ ...r }, 180)
  },

  runEvents: (id: string, after: number) => {
    const run = runStore.get(id)
    let evts = eventStore.get(id)
    if (!evts && run && !liveScripts.has(id)) {
      evts = seedEvents(run)
      eventStore.set(id, evts)
    }
    return slow((evts ?? []).filter((e) => e.seq > after), 160)
  },

  createRun: (body: CreateRunRequest) => {
    const id = crypto.randomUUID()
    const env = ENVIRONMENTS.find((e) => e.environmentId === body.environmentId)
    const run: Run = {
      id,
      status: 'queued',
      createdAt: new Date().toISOString(),
      startedAt: null,
      finishedAt: null,
      durationMs: 0,
      scope: {
        clusterId: body.clusterId,
        clusterName: body.clusterName,
        environmentId: body.environmentId,
        environmentName: env?.environmentName,
        namespace: body.namespace,
        appName: body.appName,
        appType: body.appType,
      },
      trigger: body.alert
        ? { kind: 'alert', alert: body.alert, ask: `Alert ${body.alert.name} is ${body.alert.state} for ${body.alert.namespace}/${body.alert.resource}.` }
        : { kind: 'ask', alert: null, ask: body.ask ?? '' },
      intelligence: null,
      verdict: null,
      report: null,
      usage: { toolCalls: 0, modelCalls: 0, modelTokens: 0, maxToolCalls: 25, maxModelTokens: 400_000 },
      error: '',
    }
    runStore.set(id, run)
    eventStore.set(id, [])
    liveScripts.set(id, script())
    return slow(run, 400)
  },

  cancelRun: (id: string) => {
    const r = runStore.get(id)
    if (!r) return Promise.reject(new Error(`run ${id} not found`))
    liveScripts.delete(id)
    r.status = 'canceled'
    r.finishedAt = new Date().toISOString()
    return slow({ ...r }, 200)
  },

  knowledge: (p: { q?: string; class?: string }) =>
    slow(KNOWLEDGE.filter((k) => (!p.class || k.class === p.class)).filter((k) => !p.q || k.name.toLowerCase().includes(p.q.toLowerCase()))),

  knowledgeComponent: (id: string) => {
    const k = KNOWLEDGE.find((x) => x.id === id)
    if (!k) return Promise.reject(new Error(`knowledge component ${id} not found`))
    return slow(k, 180)
  },

  settings: async (): Promise<Settings> =>
    slow({
      devtronUrl: 'https://devtron.example.com',
      tokenSet: true,
      tokenHint: '…4f2a',
      source: 'environment' as const,
      intelligencePath: '/athena/intelligence',
    }),

  saveSettings: async (body: SettingsBody): Promise<Settings> =>
    slow({
      devtronUrl: body.devtronUrl,
      tokenSet: true,
      tokenHint: '…4f2a',
      source: 'database' as const,
      updatedAt: new Date().toISOString(),
      updatedBy: 'ui',
      intelligencePath: '/athena/intelligence',
    }),

  // The body is irrelevant: the fixture always probes clean. The parameter
  // stays so the signature matches the live client.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  testSettings: async (_body?: SettingsBody): Promise<ProbeResult> =>
    slow({
      ok: true,
      message: 'Connected. 3 cluster(s) visible to this token.',
      clusters: 3,
      names: ['tenant-acme-prod', 'tenant-acme-stage', 'platform-shared'],
    }),

}
