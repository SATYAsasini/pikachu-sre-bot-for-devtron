import { motion, useReducedMotion } from 'motion/react'
import { AlertTriangle, Boxes, Database, Gauge, ShieldCheck, Wrench } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from 'cn'
import { AgentFrames } from '@/components/agent/agent-frames'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { Text } from '@/components/common/text'
import { duration, pluralise } from '@/lib/format'
import type { Stage, StageState } from '@/lib/run-derive'
import type { Run, RunEvent } from '@/lib/types'

/**
 * A run, as the two things it actually is.
 *
 * The page used to open with three equal boxes — Devtron, a judge, a deep
 * dive — which was three because the implementation had three agents, not
 * because a reader needs three. Two of them regularly showed "not started"
 * next to a finished run, which reads as breakage.
 *
 * There are two parties now and the split is the interesting part of the
 * product: **Devtron gathers** infrastructure state, **we reason** about it.
 * So the flow shows who owns each half and what each one actually produced —
 * counts, not adjectives. A phase that is running says what it is doing; a
 * phase that finished says what it found.
 */
export function PhaseFlow({
  stages,
  run,
  events,
}: {
  stages: Stage[]
  run: Run | undefined
  events: readonly RunEvent[]
}) {
  const still = useReducedMotion()
  const gather = stages.find((s) => s.key === 'gather')
  const sre = stages.find((s) => s.key === 'sre')
  if (!gather || !sre) return null

  const intel = run?.intelligence
  const verdict = run?.verdict
  const report = run?.report
  const toolCalls = events.filter((e) => e.type === 'tool_call').length

  return (
    <nav aria-label="Run phases" className="mb-3">
      <div className="grid gap-2 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
        <Phase
          stage={gather}
          owner="Devtron"
          ownerMark={<DevtronGlyph aria-hidden className="h-3.5 w-auto shrink-0" />}
          role="Collects the infrastructure state"
          facts={[
            {
              icon: Boxes,
              label: 'Cluster facts',
              value: run ? 'gathered before any model ran' : '—',
              on: Boolean(run),
            },
            {
              icon: Gauge,
              label: 'Monitoring',
              value: 'discovered per cluster',
              on: Boolean(run),
            },
            {
              icon: AlertTriangle,
              label: 'First pass',
              value: intel?.durationMs
                ? `${duration(intel.durationMs)} · ${pluralise(intel.thinkingCount ?? 0, 'step')}`
                : gather.state === 'running'
                  ? 'thinking…'
                  : '—',
              on: Boolean(intel?.analysis) || gather.state === 'running',
            },
          ]}
        />

        {/* The handoff. One direction, and it is the whole product. */}
        <div className="flex items-center justify-center lg:flex-col lg:px-1">
          <span aria-hidden className="hidden h-full w-px bg-border lg:block" />
          <motion.span
            aria-hidden
            initial={still ? false : { opacity: 0.35 }}
            animate={sre.state === 'running' ? { opacity: [0.35, 1, 0.35] } : { opacity: 0.55 }}
            transition={{ duration: 1.8, repeat: sre.state === 'running' ? Infinity : 0 }}
            className="rounded-full border border-border bg-card px-2 py-0.5 text-[0.5625rem] tracking-widest text-muted-foreground uppercase lg:my-1"
          >
            hands off
          </motion.span>
          <span aria-hidden className="hidden h-full w-px bg-border lg:block" />
        </div>

        <Phase
          stage={sre}
          owner="Pikachu SRE"
          ownerMark={<AgentFrames mood={sre.state === 'running' ? 'working' : 'idle'} size={26} />}
          role="Grades it, then says what to do"
          facts={[
            {
              icon: ShieldCheck,
              label: 'Claims graded',
              value: verdict
                ? `${(verdict.claims ?? []).length} checked · ${(verdict.claims ?? []).filter((c) => c.status === 'contradicted').length} contradicted`
                : sre.state === 'running'
                  ? 'reading the first pass…'
                  : '—',
              on: Boolean(verdict),
            },
            {
              icon: Database,
              label: 'Evidence',
              value: report
                ? `${(report.evidence ?? []).length} cited · ${pluralise(toolCalls, 'tool call')}`
                : sre.state === 'running'
                  ? `${pluralise(toolCalls, 'tool call')} so far`
                  : '—',
              on: toolCalls > 0,
            },
            {
              icon: Wrench,
              label: 'Remediation',
              value: report
                ? pluralise((report.remediation ?? []).length, 'step')
                : sre.state === 'running'
                  ? 'not yet'
                  : '—',
              on: Boolean(report),
            },
          ]}
        />
      </div>
    </nav>
  )
}

const RING: Record<StageState, string> = {
  waiting: 'border-border bg-card',
  running: 'border-accent-strong/45 bg-accent-strong/5',
  done: 'border-ok/35 bg-ok/5',
  failed: 'border-bad/35 bg-bad/5',
  skipped: 'border-dashed border-border bg-transparent',
  not_run: 'border-dashed border-border bg-transparent opacity-70',
}

const WORD: Record<StageState, string> = {
  waiting: 'queued',
  running: 'working',
  done: 'complete',
  failed: 'errored',
  skipped: 'not needed',
  not_run: 'did not run',
}

interface Fact {
  icon: LucideIcon
  label: string
  value: string
  /** Dimmed until it has actually happened. */
  on: boolean
}

function Phase({
  stage,
  owner,
  ownerMark,
  role,
  facts,
}: {
  stage: Stage
  owner: string
  ownerMark: React.ReactNode
  role: string
  facts: Fact[]
}) {
  const still = useReducedMotion()

  return (
    <a
      href={`#stage-${stage.key}`}
      className={cn(
        'group relative block overflow-hidden rounded-xl border-2 shadow-card transition-all',
        'hover:-translate-y-px hover:shadow-raised',
        'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
        RING[stage.state],
      )}
    >
      <div className="flex items-center gap-2.5 border-b border-border/70 px-3 py-2">
        <span className="shrink-0">{ownerMark}</span>
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-1.5">
            <span className="tabular text-[0.625rem] text-muted-foreground">{stage.index}</span>
            <span className="truncate text-xs font-semibold">{stage.title}</span>
            <span className="truncate text-[0.6875rem] text-muted-foreground">· {owner}</span>
          </span>
          <Text tone="fine" className="mt-px truncate">
            {role}
          </Text>
        </span>
        <motion.span
          key={stage.state}
          initial={still ? false : { opacity: 0, y: -3 }}
          animate={{ opacity: 1, y: 0 }}
          className={cn(
            'shrink-0 text-[0.625rem] font-medium',
            stage.state === 'done' && 'text-ok',
            stage.state === 'running' && 'text-accent-strong',
            stage.state === 'failed' && 'text-bad',
            (stage.state === 'waiting' || stage.state === 'not_run' || stage.state === 'skipped') &&
              'text-muted-foreground',
          )}
        >
          {WORD[stage.state]}
        </motion.span>
      </div>

      {/* What this half actually produced. Counts rather than adjectives:
          "4 cited · 7 tool calls" is checkable, "thorough" is not. */}
      <dl className="divide-y divide-border/50">
        {facts.map((f) => (
          <div key={f.label} className={cn('flex items-center gap-2 px-3 py-1.5', !f.on && 'opacity-45')}>
            <f.icon aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            <dt className="w-24 shrink-0 text-[0.625rem] tracking-wider text-muted-foreground uppercase">
              {f.label}
            </dt>
            <dd className="min-w-0 flex-1 truncate text-[0.6875rem]">{f.value}</dd>
          </div>
        ))}
      </dl>

      {stage.state === 'running' ? (
        <motion.span
          aria-hidden
          className="absolute inset-x-0 bottom-0 h-0.5 origin-left bg-accent-strong/60"
          initial={{ scaleX: 0 }}
          animate={{ scaleX: [0, 1, 0] }}
          transition={{ duration: 2.4, repeat: Infinity, ease: 'easeInOut' }}
        />
      ) : null}
    </a>
  )
}
