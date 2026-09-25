import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertTriangle, EyeOff, Save, Server, Sparkles, SlidersHorizontal } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Panel, PanelBody, PanelHeader, Well } from '@/components/common/panel'
import { Chip, type Tone } from '@/components/common/status'
import { Heading, Text } from '@/components/common/text'
import { EmptyState } from '@/components/common/empty-state'
import { RowSkeleton } from '@/components/common/skeletons'
import { RuleList } from '@/components/rules/rule-editor'
import { NotifyPanel } from '@/components/rules/notify-panel'
import { api, errorMessage } from '@/lib/api'
import { qk, useClusters } from '@/lib/queries'
import { useScope } from '@/lib/scope'
import { alertMeta } from '@/lib/alert-meta'
import { MonitoringPicker } from '@/components/scope/monitoring-picker'
import { ClusterAccess } from '@/components/scope/cluster-access'
import type { Cluster, Priority, RulesConfig } from '@/lib/types'

const PRIORITY_TONE: Record<Priority, Tone> = { P0: 'bad', P1: 'warn', P2: 'neutral' }

const EMPTY = (clusterId: number): RulesConfig => ({
  clusterId,
  show: [],
  mute: [],
  auto: [],
  autoEnabled: false,
  priority: [],
  notify: { enabled: false, channel: 'slack' },
})

/**
 * Nothing saved is not an error, and it is not an empty product.
 *
 * The server used to marshal nil slices as `null`, and this page did
 * `.length` on them — so the very first visit to a cluster with no rules
 * crashed with "Cannot read properties of null". Fixed on the server too, but
 * coalesced here as well: a page that falls over because a feature has never
 * been configured is the worst possible first impression of it.
 */
function safe(cfg: RulesConfig | undefined, clusterId: number): RulesConfig {
  const e = EMPTY(clusterId)
  if (!cfg) return e
  return {
    ...e,
    ...cfg,
    show: cfg.show ?? [],
    mute: cfg.mute ?? [],
    auto: cfg.auto ?? [],
    priority: cfg.priority ?? [],
    notify: cfg.notify ?? e.notify,
  }
}

/**
 * Per-cluster alert rules, with the consequences on screen.
 *
 * Writing a match against a payload you cannot see is guesswork, and the worst
 * outcome this feature has is a rule that quietly hides the one alert that
 * mattered. So the editor is half the page and the other half is **what these
 * rules would do to the alerts firing right now** — evaluated by the server,
 * with the same code that will run in production rather than a second
 * implementation in the browser that drifts from it.
 *
 * The preview updates as you type, before anything is saved. Saving is a
 * separate, deliberate act.
 */
type Tab = 'rules' | 'notifications' | 'monitoring' | 'access'

/**
 * Per-cluster configuration, one cluster at a time.
 *
 * This was a bare /rules page bound to whatever the top bar happened to be
 * scoped to, which made "where do I set this up for my other cluster?" an
 * unanswerable question. Clusters are the unit of configuration here — what
 * counts as P0 in production is rarely what counts as P0 in a sandbox, and
 * each one has its own channel — so the cluster is picked on the page rather
 * than inherited from elsewhere.
 */
export function ClustersPage() {
  const { scope, setScope } = useScope()
  const clusters = useClusters()
  const [tab, setTab] = useState<Tab>('rules')

  // Default to whatever the top bar is scoped to, then the first usable one.
  const list = clusters.data ?? []
  const selected = list.find((c) => c.id === scope.clusterId) ?? list[0]
  const clusterId = selected?.id ?? 0
  const clusterName = selected?.clusterName
  const ready = clusterId !== 0

  if (clusters.isPending) {
    return (
      <Panel>
        <PanelBody>
          <RowSkeleton rows={4} />
        </PanelBody>
      </Panel>
    )
  }
  if (list.length === 0) {
    return (
      <Panel>
        <PanelBody>
          <EmptyState
            icon={SlidersHorizontal}
            title="No readable clusters"
            line="Rules and notifications are per cluster, and none can be read right now. Settings shows what each one answered."
          />
        </PanelBody>
      </Panel>
    )
  }

  return (
    <div className="w-full space-y-4">
      <header className="border-b border-border pb-3">
        <Heading level={1} className="text-lg">
          Clusters
        </Heading>
        <Text tone="muted" className="mt-1">
          What each cluster shows, how urgent its alerts are, and where its findings go.
        </Text>
      </header>

      {/* One card per cluster. Selecting also moves the global scope, so the
          alert list you go back to is the one you just configured. */}
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {list.map((c) => {
          const on = c.id === clusterId
          return (
            <button
              key={c.id}
              type="button"
              onClick={() => setScope({ clusterId: c.id, clusterName: c.clusterName })}
              aria-current={on ? 'true' : undefined}
              className={cn(
                'rounded-xl border-2 bg-card px-3 py-2.5 text-left shadow-card transition-all',
                'hover:-translate-y-px hover:shadow-raised',
                'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                on ? 'border-accent-strong' : 'border-border hover:border-accent-strong/40',
              )}
            >
              <span className="flex items-center gap-1.5">
                <Server aria-hidden className={cn('size-3.5 shrink-0', on ? 'text-accent-strong' : 'text-muted-foreground')} />
                <span className="truncate font-mono text-xs font-semibold">{c.clusterName}</span>
                {on ? <Chip tone="accent" className="ml-auto shrink-0">editing</Chip> : null}
              </span>
              <ClusterSummary cluster={c} />
            </button>
          )
        })}
      </div>

      <div className="flex flex-wrap items-center gap-1.5 border-b border-border pb-2">
        {(
          [
            ['rules', 'Alert rules'],
            ['notifications', 'Notifications'],
            ['monitoring', 'Monitoring'],
            ['access', 'Access'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            aria-current={tab === key ? 'page' : undefined}
            className={cn(
              'rounded-lg border px-3 py-1.5 text-xs transition-all',
              'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
              tab === key
                ? 'border-accent-strong/45 bg-accent-strong/12 font-semibold text-foreground shadow-card'
                : 'border-border bg-card font-medium text-muted-foreground hover:text-foreground',
            )}
          >
            {label}
          </button>
        ))}
      </div>

      {!ready ? null : tab === 'monitoring' ? (
        <MonitoringPicker key={clusterId} clusterId={clusterId} clusterName={clusterName} />
      ) : tab === 'access' ? (
        <ClusterAccess key={clusterId} clusterId={clusterId} clusterName={clusterName} />
      ) : (
        <ClusterConfig key={clusterId} clusterId={clusterId} clusterName={clusterName} tab={tab} />
      )}
    </div>
  )
}

/** A one-line "is anything configured here" for the cluster cards. */
function ClusterSummary({ cluster }: { cluster: Cluster }) {
  const clusterId = cluster.id
  const q = useQuery({ queryKey: qk.rules(clusterId), queryFn: () => api.rules(clusterId), staleTime: 60_000 })
  const cfg = q.data
  if (!cfg) return <span className="mt-1 block text-[0.625rem] text-muted-foreground">…</span>

  const ruleCount = (cfg.priority?.length ?? 0) + (cfg.mute?.length ?? 0) + (cfg.show?.length ?? 0)
  return (
    <span className="mt-1 flex flex-wrap items-center gap-1">
      <span className="text-[0.625rem] text-muted-foreground">
        {ruleCount === 0 ? 'no rules yet' : `${ruleCount} rule${ruleCount === 1 ? '' : 's'}`}
      </span>
      {cfg.notify?.enabled ? (
        <Chip tone="ok">notifying</Chip>
      ) : (
        <span className="text-[0.625rem] text-muted-foreground/70">· no channel</span>
      )}
      {cfg.autoEnabled ? <Chip tone="warn">auto</Chip> : null}
      {cluster.monitoringPinned ? <Chip tone="accent">pinned stack</Chip> : null}
    </span>
  )
}

function ClusterConfig({
  clusterId,
  clusterName,
  tab,
}: {
  clusterId: number
  clusterName?: string
  tab: Exclude<Tab, 'monitoring' | 'access'>
}) {

  const saved = useQuery({
    queryKey: qk.rules(clusterId),
    queryFn: () => api.rules(clusterId),
    staleTime: 30_000,
  })

  // The draft is keyed by what it was seeded from. When the cluster changes or
  // the server returns different rules, the key changes and the draft is
  // discarded during render rather than corrected in an effect — an effect
  // would paint one frame of the previous cluster's rules first, which on this
  // page means briefly showing somebody the wrong mute list.
  const seed = saved.data ? safe(saved.data, clusterId) : EMPTY(clusterId)
  const seedKey = useMemo(() => JSON.stringify(seed), [seed])
  const [draft, setDraft] = useState<{ key: string; cfg: RulesConfig } | null>(null)
  const cfg = draft?.key === seedKey ? draft.cfg : (seed ?? EMPTY(clusterId))
  const setCfg = (next: RulesConfig) => setDraft({ key: seedKey, cfg: next })
  const dirty = useMemo(
    () => saved.data !== undefined && JSON.stringify(saved.data) !== JSON.stringify(cfg),
    [saved.data, cfg],
  )

  // Debounced so a preview is not fired on every keystroke.
  const [debounced, setDebounced] = useState(cfg)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(cfg), 350)
    return () => window.clearTimeout(t)
  }, [cfg])

  const preview = useQuery({
    queryKey: [...qk.rules(clusterId), 'preview', JSON.stringify(debounced)] as const,
    queryFn: () => api.previewRules({ ...debounced, clusterName }),
    staleTime: 15_000,
    retry: false,
  })

  const save = useMutation({
    mutationFn: () => api.saveRules({ ...cfg, clusterName }),
    onSuccess: () => {
      toast.success('Rules saved', { description: 'They apply to every alert list from now on.' })
      void saved.refetch()
    },
    onError: (e) => toast.error('Could not save', { description: errorMessage(e) }),
  })

  if (tab === 'notifications') {
    return (
      <div className="space-y-3">
        <SaveBar dirty={dirty} pending={save.isPending} onSave={() => save.mutate()} />
        <div className="max-w-2xl">
          <NotifyPanel
            clusterId={clusterId}
            clusterName={clusterName}
            notify={cfg.notify}
            onChange={(notify) => setCfg({ ...cfg, notify })}
          />
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <SaveBar dirty={dirty} pending={save.isPending} onSave={() => save.mutate()} />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <div className="min-w-0 space-y-3">
          <RuleList
            title="Priority"
            hint="First match wins, top to bottom. Anything no rule claims is P2 — unclassified is not urgent."
            rules={cfg.priority}
            showPriority
            onChange={(priority) => setCfg({ ...cfg, priority })}
          />

          <RuleList
            title="Mute"
            hint="Alerts matching these never appear, and never auto-trigger. Use it for known noise."
            rules={cfg.mute}
            onChange={(mute) => setCfg({ ...cfg, mute })}
          />

          <RuleList
            title="Show"
            hint="Leave empty to show everything. Add one rule and the list becomes opt-in — only matches appear."
            rules={cfg.show}
            onChange={(show) => setCfg({ ...cfg, show })}
          />

          <RuleList
            title="Auto-investigate"
            hint="Opens a run on its own. Each one spends a Devtron call, a model and a tool budget, so it is off until you switch it on."
            rules={cfg.auto}
            onChange={(auto) => setCfg({ ...cfg, auto })}
            action={
              <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-border bg-well px-2 py-1">
                <input
                  type="checkbox"
                  checked={cfg.autoEnabled}
                  onChange={(e) => setCfg({ ...cfg, autoEnabled: e.target.checked })}
                  className="size-3.5 accent-[var(--accent-strong)]"
                />
                <span className="text-[0.6875rem] font-medium">
                  {cfg.autoEnabled ? 'Enabled' : 'Off'}
                </span>
              </label>
            }
          />
        </div>

        <aside className="min-w-0 xl:sticky xl:top-[4.5rem] xl:self-start">
          <Panel>
            <PanelHeader
              title="What these rules do"
              description="Against the alerts firing right now, before you save."
              icon={<Sparkles aria-hidden className="size-3.5" />}
              actions={dirty ? <Chip tone="warn">unsaved</Chip> : null}
            />
            <PanelBody className="space-y-2.5">
              {preview.isPending ? (
                <RowSkeleton rows={4} />
              ) : preview.isError ? (
                <Text tone="fine">Could not preview: {errorMessage(preview.error)}</Text>
              ) : preview.data?.unavailable ? (
                <Well className="border-warn/30">
                  <Text tone="muted">
                    No alert source answered, so there is nothing to preview against. The rules will still be
                    saved and applied once it comes back.
                  </Text>
                </Well>
              ) : (
                <>
                  <div className="grid grid-cols-3 gap-px overflow-hidden rounded-lg border border-border bg-border">
                    <Stat label="Shown" value={preview.data?.shown ?? 0} />
                    <Stat label="Muted" value={preview.data?.muted ?? 0} tone={preview.data?.muted ? 'warn' : undefined} />
                    <Stat label="Auto" value={preview.data?.auto ?? 0} tone={preview.data?.auto ? 'bad' : undefined} />
                  </div>

                  <div className="flex flex-wrap gap-1.5">
                    {(['P0', 'P1', 'P2'] as const).map((p) => (
                      <Chip key={p} tone={PRIORITY_TONE[p]}>
                        {p} · {preview.data?.byPriority?.[p] ?? 0}
                      </Chip>
                    ))}
                  </div>

                  {(preview.data?.auto ?? 0) > 0 && cfg.autoEnabled ? (
                    <Well className="border-bad/35 bg-bad/5">
                      <Text tone="muted">
                        <strong>{preview.data?.auto} alerts would auto-trigger right now.</strong> Each is a
                        separate run. Narrow the match, or leave auto off until it is down to the handful you
                        actually want.
                      </Text>
                    </Well>
                  ) : null}

                  <PreviewList rows={preview.data?.rows ?? []} />
                </>
              )}
            </PanelBody>
          </Panel>
        </aside>
      </div>
    </div>
  )
}

/** Unsaved work needs somewhere obvious to go, on both tabs. */
function SaveBar({ dirty, pending, onSave }: { dirty: boolean; pending: boolean; onSave: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Text tone="fine" as="span">
        {dirty ? 'Unsaved changes — nothing applies until you save.' : 'Everything here is saved.'}
      </Text>
      <Button size="sm" onClick={onSave} disabled={!dirty || pending} className="ml-auto">
        <Save aria-hidden className="size-3.5" />
        {pending ? 'Saving…' : dirty ? 'Save' : 'Saved'}
      </Button>
    </div>
  )
}

function Stat({ label, value, tone }: { label: string; value: number; tone?: Tone }) {
  return (
    <div className="bg-card px-2.5 py-2 text-center">
      <div className="text-[0.625rem] tracking-wider text-muted-foreground uppercase">{label}</div>
      <div
        className={cn(
          'tabular mt-0.5 font-display text-lg font-semibold',
          tone === 'warn' && 'text-warn',
          tone === 'bad' && 'text-bad',
        )}
      >
        {value}
      </div>
    </div>
  )
}

/**
 * Every alert, including the hidden ones.
 *
 * The muted rows are the point — an operator needs to see what a rule is about
 * to suppress, not only what survives it. They are struck through and dimmed
 * with the rule that hid them named beside.
 */
function PreviewList({
  rows,
}: {
  rows: { alert: import('@/lib/types').Alert; decision: import('@/lib/types').RuleDecision }[]
}) {
  if (rows.length === 0) {
    return <Text tone="fine">Nothing firing on this cluster to preview against.</Text>
  }

  return (
    <ul data-lenis-prevent className="max-h-[28rem] space-y-1 overflow-y-auto">
      {rows.map(({ alert, decision }, i) => {
        const meta = alertMeta(alert, 1)[0]
        return (
          <li
            key={`${alert.name}-${i}`}
            className={cn(
              'rounded-lg border px-2 py-1.5',
              decision.show ? 'border-border bg-card' : 'border-dashed border-border bg-transparent opacity-55',
            )}
          >
            <div className="flex min-w-0 items-center gap-1.5">
              {decision.show ? (
                <Chip tone={PRIORITY_TONE[decision.priority]}>{decision.priority}</Chip>
              ) : (
                <EyeOff aria-hidden className="size-3 shrink-0 text-muted-foreground" />
              )}
              <span className={cn('truncate text-[0.6875rem] font-medium', !decision.show && 'line-through')}>
                {alert.name}
              </span>
              {decision.auto ? (
                <Chip tone="bad" className="ml-auto shrink-0">
                  auto
                </Chip>
              ) : null}
            </div>

            <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-1.5">
              {meta ? (
                <span className="truncate font-mono text-[0.625rem] text-muted-foreground">
                  {meta.key}={meta.value}
                </span>
              ) : null}
              {decision.mutedBy ? (
                <span className="text-[0.625rem] text-warn">muted by “{decision.mutedBy}”</span>
              ) : decision.why ? (
                <span className="text-[0.625rem] text-muted-foreground">via “{decision.why}”</span>
              ) : decision.show ? (
                <span className="text-[0.625rem] text-muted-foreground/70">no rule matched — default P2</span>
              ) : null}
            </div>
          </li>
        )
      })}
    </ul>
  )
}

/** Shown in the nav when a cluster has noisy alerts and no rules yet. */
export const RULES_ICON = AlertTriangle
