import { useState, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { BellRing, BookOpen, ChevronRight, MessageSquareText } from 'lucide-react'
import { cn } from 'cn'
import { Chip, type Tone } from '@/components/common/status'
import { Code, Mono, Truncated } from '@/components/common/mono'
import { Text } from '@/components/common/text'
import { Well } from '@/components/common/panel'
import { alertMeta, hiddenMetaCount } from '@/lib/alert-meta'
import { absoluteTime, humanise, relativeTime } from '@/lib/format'
import { api } from '@/lib/api'
import { qk } from '@/lib/queries'
import type { IdentifiedComponent, Run, TrackedAlert } from '@/lib/types'

const SEVERITY_TONE: Record<string, Tone> = {
  critical: 'bad',
  error: 'bad',
  page: 'bad',
  warning: 'warn',
  warn: 'warn',
  info: 'neutral',
  none: 'neutral',
}

const LAYER_LABEL: Record<string, string> = {
  k8s_workload: 'Kubernetes workload',
  k8s_infra: 'Kubernetes infrastructure',
  devtron_cd: 'Devtron CD',
  devtron_platform: 'Devtron platform',
  known_app: 'Recognised application',
  unknown: 'Unrecognised',
}

/** Devtron's own pieces get their knowledge pack shown inline, not on request. */
const DEVTRON_LAYERS = new Set(['devtron_cd', 'devtron_platform'])

/**
 * Why this run exists.
 *
 * A run opened from history is otherwise context-free: you get a verdict about
 * a pod you have never heard of and no way to tell what fired, when, or which
 * rule decided it mattered. This is the provenance strip — the alert exactly as
 * the alert source published it, or the question that was asked, plus what the
 * subject actually turned out to be.
 *
 * When the subject is one of Devtron's own components, its knowledge pack is
 * summarised here rather than left behind a click: "this is Kubelink, it talks
 * to Helm on the orchestrator's behalf" is context you need *before* reading a
 * verdict about it, not after.
 */
export function TriggerCard({
  run,
  component,
  onOpenKnowledge,
}: {
  run: Run
  /** From the verdict, once the agent has identified what this is about. */
  component: IdentifiedComponent | null | undefined
  onOpenKnowledge: (componentId: string) => void
}) {
  const alert = run.trigger.alert ?? null

  // Which tracked alert this run belongs to. A run is an investigation *of*
  // something; opening one from history and finding no way back to the alert
  // it answered is how the run list stopped being navigable.
  const owner = useQuery({
    queryKey: qk.runAlert(run.id),
    queryFn: () => api.runAlert(run.id),
    enabled: Boolean(alert),
    retry: false,
    staleTime: 60_000,
  })

  return (
    <section
      aria-label="What triggered this run"
      className="overflow-hidden rounded-xl border border-border bg-card shadow-card"
    >
      {owner.data?.alert ? <OwnerStrip alert={owner.data.alert} /> : null}
      {alert ? <AlertTrigger alert={alert} /> : <AskTrigger ask={run.trigger.ask} />}
      <SubjectStrip component={component} onOpenKnowledge={onOpenKnowledge} />
    </section>
  )
}

/**
 * The alert this run answers, above everything else on the page.
 *
 * The number is the point: an investigation is worth having only if it can be
 * referred to later, and "#42" is how somebody refers to it in a channel.
 */
function OwnerStrip({ alert }: { alert: TrackedAlert }) {
  const tone: Tone = alert.state === 'resolved' ? 'ok' : alert.state === 'acknowledged' ? 'warn' : 'bad'
  return (
    <Link
      to="/"
      className="flex min-w-0 flex-wrap items-center gap-1.5 border-b border-border bg-well px-3 py-1.5 transition-colors hover:bg-well/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <Text tone="label" as="span">
        Alert
      </Text>
      <span className="font-mono text-[0.6875rem] font-semibold">#{alert.seq}</span>
      <Chip tone={alert.priority === 'P0' ? 'bad' : alert.priority === 'P1' ? 'warn' : 'neutral'}>
        {alert.priority}
      </Chip>
      <Chip tone={tone}>{alert.state}</Chip>
      {alert.seenCount > 1 ? (
        <span className="text-[0.625rem] text-muted-foreground">seen {alert.seenCount.toLocaleString()}×</span>
      ) : null}
      <span className="ml-auto inline-flex items-center gap-0.5 text-[0.625rem] text-muted-foreground">
        back to the dashboard
        <ChevronRight aria-hidden className="size-2.5" />
      </span>
    </Link>
  )
}

function AlertTrigger({ alert }: { alert: NonNullable<Run['trigger']['alert']> }) {
  const meta = alertMeta(alert, 5)
  const hidden = hiddenMetaCount(alert, meta.length)
  const tone = SEVERITY_TONE[alert.severity?.toLowerCase()] ?? 'neutral'
  const labels = Object.entries(alert.labels ?? {})
  const annotations = Object.entries(alert.annotations ?? {})

  return (
    <div className="px-3 py-2.5">
      <div className="flex min-w-0 items-start gap-2">
        <BellRing
          aria-hidden
          className={cn(
            'mt-0.5 size-3.5 shrink-0',
            tone === 'bad' ? 'text-bad' : tone === 'warn' ? 'text-warn' : 'text-muted-foreground',
          )}
        />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Text tone="label" as="span">
              Triggered by
            </Text>
            <span className="truncate text-xs font-semibold">{alert.name}</span>
            {alert.severity ? <Chip tone={tone}>{alert.severity}</Chip> : null}
            {alert.state ? <Chip tone="neutral">{alert.state}</Chip> : null}
            {alert.source ? <Chip tone="neutral">{alert.source}</Chip> : null}
            {alert.startsAt ? (
              <span className="tabular text-[0.6875rem] text-muted-foreground" title={absoluteTime(alert.startsAt)}>
                firing {relativeTime(alert.startsAt)}
              </span>
            ) : null}
          </div>

          {alert.summary ? (
            <Text tone="muted" className="mt-1">
              <Truncated text={alert.summary} lines={2} />
            </Text>
          ) : null}

          {/* Identity before plumbing: the most specific label first, so two
              near-identical alerts are still told apart on this one line. */}
          {meta.length > 0 && (
            <div className="mt-1.5 flex flex-wrap items-center gap-1">
              {meta.map((c) => (
                <Chip key={c.key} tone={c.lead ? 'accent' : 'neutral'} mono className="max-w-[16rem]">
                  {c.key}={c.value}
                </Chip>
              ))}
              {alert.namespace && !meta.some((c) => c.key === 'namespace') ? (
                <Chip tone="neutral" mono>
                  namespace={alert.namespace}
                </Chip>
              ) : null}
              {hidden > 0 ? <Text tone="fine" as="span">+{hidden} more</Text> : null}
            </div>
          )}
        </div>
      </div>

      {/* Everything the alert source actually sent, for anyone reconciling
          this page against their own Alertmanager. */}
      {(alert.expression || labels.length > 0 || annotations.length > 0 || alert.description) && (
        <Payload count={labels.length + annotations.length}>
          {alert.description && alert.description !== alert.summary ? (
            <Text tone="muted">{alert.description}</Text>
          ) : null}

          {alert.expression ? (
            <div>
              <Text tone="label">Rule expression</Text>
              <Well className="mt-0.5 font-mono text-[0.6875rem] break-all">{alert.expression}</Well>
            </div>
          ) : null}

          {labels.length > 0 ? <Pairs title="Labels" pairs={labels} mono /> : null}
          {annotations.length > 0 ? <Pairs title="Annotations" pairs={annotations} /> : null}
        </Payload>
      )}
    </div>
  )
}

/**
 * The raw alert, behind a click rather than a hover.
 *
 * Hover-disclosure is right for a one-line "why" and wrong for twenty rows of
 * labels: a block that tall appearing under the pointer shifts whatever you
 * were about to click.
 */
function Payload({ count, children }: { count: number; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="mt-2 border-t border-border pt-1.5">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="inline-flex items-center gap-1 rounded text-[0.6875rem] text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <ChevronRight aria-hidden className={cn('size-3 transition-transform', open && 'rotate-90')} />
        Full alert payload
        {count > 0 ? <span className="tabular opacity-70">· {count} fields</span> : null}
      </button>
      <div
        className={cn(
          'grid transition-[grid-template-rows,opacity] duration-200 ease-out',
          open ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
        )}
      >
        <div className="overflow-hidden">
          <div className="space-y-2 pt-1.5">{children}</div>
        </div>
      </div>
    </div>
  )
}

function AskTrigger({ ask }: { ask: string }) {
  return (
    <div className="flex items-start gap-2 px-3 py-2.5">
      <MessageSquareText aria-hidden className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <Text tone="label">Asked</Text>
        <Text className="mt-0.5">{ask.trim() || 'No question was recorded for this run.'}</Text>
      </div>
    </div>
  )
}

function Pairs({ title, pairs, mono }: { title: string; pairs: [string, string][]; mono?: boolean }) {
  return (
    <div>
      <Text tone="label">{title}</Text>
      <dl className="mt-0.5 grid gap-x-3 gap-y-0.5 sm:grid-cols-2">
        {pairs.map(([k, v]) => (
          <div key={k} className="flex min-w-0 items-baseline gap-1.5">
            <dt className="shrink-0 font-mono text-[0.6875rem] text-muted-foreground">{k}</dt>
            <dd className={cn('min-w-0 flex-1 text-[0.6875rem]', mono && 'font-mono')}>
              <Truncated text={v} />
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

/**
 * What the run turned out to be about.
 *
 * Absent until the agent names it, and it says so rather than reserving an
 * empty row — an identification that has not happened yet is different from
 * one that came back `unknown`.
 */
function SubjectStrip({
  component,
  onOpenKnowledge,
}: {
  component: IdentifiedComponent | null | undefined
  onOpenKnowledge: (componentId: string) => void
}) {
  const id = component?.id?.trim() || ''
  const isDevtron = DEVTRON_LAYERS.has(String(component?.layer))

  // Only Devtron's own components get their pack pulled in. For a random
  // workload there is nothing to say that the manifest does not already say.
  const pack = useQuery({
    queryKey: [...qk.knowledge(), 'component', id],
    queryFn: () => api.knowledgeComponent(id),
    enabled: id !== '' && isDevtron,
    staleTime: 5 * 60_000,
  })

  if (!component) return null

  const layer = LAYER_LABEL[String(component.layer)] ?? humanise(String(component.layer))
  const name = component.displayName?.trim() || component.name

  return (
    <div className="border-t border-border bg-well/50 px-3 py-2">
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <Text tone="label" as="span">
          Subject
        </Text>
        {component.kind ? <Chip tone="neutral">{component.kind}</Chip> : null}
        <span className="truncate font-mono text-xs">{name || 'unnamed'}</span>
        {component.namespace ? <Mono value={component.namespace} title="Namespace" className="max-w-[12rem]" /> : null}
        <Chip tone={isDevtron ? 'accent' : 'neutral'}>{layer}</Chip>
        {id && isDevtron ? (
          <button
            type="button"
            onClick={() => onOpenKnowledge(id)}
            className="inline-flex items-center gap-1 rounded border border-border bg-card px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <BookOpen aria-hidden className="size-3" />
            Knowledge pack
          </button>
        ) : null}
      </div>

      {/* A line about the Devtron component itself, read from its source at
          build time rather than recalled by a model. */}
      {pack.data?.summary ? (
        <Text tone="muted" className="mt-1">
          {pack.data.summary}
          {pack.data.repo ? (
            <>
              {' '}
              <Code>{pack.data.repo}</Code>
            </>
          ) : null}
        </Text>
      ) : null}

      {(component.matchWhy ?? []).length > 0 ? (
        <Text tone="fine" className="mt-1">
          Identified by {(component.matchWhy ?? []).join(', ')}.
        </Text>
      ) : null}
    </div>
  )
}
