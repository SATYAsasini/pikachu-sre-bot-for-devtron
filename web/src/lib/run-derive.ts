import { intelligencePhase, isInterrupted, isTerminal, type Run, type RunEvent, type ToolError } from '@/lib/types'

/**
 * `skipped` and `not_run` are different things, and conflating them was a bug
 * people could see: a run that died at the first model call showed its second
 * stage as "not needed", which reads as a decision we made rather than as a
 * stage that never got the chance. `skipped` is only ever set from an explicit
 * `agent_skipped` event.
 */
export type StageState = 'waiting' | 'running' | 'done' | 'failed' | 'skipped' | 'not_run'

export interface Stage {
  /** Why this stage never ran, when it was skipped on purpose. */
  skippedWhy?: string
  key: 'gather' | 'sre'
  /** Ordinal shown in the UI; the three outputs are always read in this order. */
  index: 1 | 2 | 3
  title: string
  /** One line explaining what this stage is, for people seeing it the first time. */
  subtitle: string
  state: StageState
}

function sawEvent(events: readonly RunEvent[], type: string, agent?: string): boolean {
  return events.some((e) => e.type === type && (agent === undefined || e.agent === agent))
}

/**
 * Works out where each of the three stacked outputs stands.
 *
 * The run object is authoritative for completion (verdict/report go non-null),
 * the event stream is authoritative for "in progress". Neither alone is enough:
 * a page loaded mid-run has events but no verdict, and a page loaded after the
 * run has a verdict but may never have seen the events.
 */
/**
 * Two phases, because there are two agents.
 *
 * This used to derive three: Devtron's first pass, a judge, and the SRE deep
 * dive. The judge is gone — it and the SRE are one model call now — and with
 * it goes the failure mode where a run that died early showed two stages that
 * had never started and a third labelled "not needed".
 *
 * What is left maps to who does the work: Devtron gathers, we reason.
 */
export function deriveStages(run: Run | undefined, events: readonly RunEvent[]): Stage[] {
  const terminal = run ? isTerminal(run.status) : false

  // `intelligence` is null until Devtron's first pass starts, then arrives
  // with analysis:"" while it streams, then fills. Four states, not two.
  const phase = intelligencePhase(run)
  const gather: StageState = !run
    ? 'waiting'
    : phase === 'failed'
      ? 'failed'
      : phase === 'complete'
        ? 'done'
        : phase === 'streaming' || sawEvent(events, 'intelligence_start')
          ? terminal
            ? 'failed'
            : 'running'
          : terminal
            ? 'not_run'
            : 'waiting'

  const sre: StageState = !run
    ? 'waiting'
    : run.report
      ? 'done'
      : sawEvent(events, 'agent_start', 'sre')
        ? terminal
          ? 'failed'
          : 'running'
        : terminal
          ? 'not_run'
          : 'waiting'

  return [
    {
      key: 'gather',
      index: 1,
      title: 'Gather',
      subtitle: 'Cluster facts, monitoring discovery and Devtron\u2019s first pass',
      state: gather,
    },
    {
      key: 'sre',
      index: 2,
      title: 'SRE',
      subtitle: 'Grade that pass against the facts, then write remediation',
      state: sre,
    },
  ]
}

export function thinkingTrail(events: readonly RunEvent[]): string[] {
  return events
    .filter((e) => e.type === 'intelligence_thinking')
    .map((e) => {
      const p = e.payload ?? {}
      // `content` is what the worker actually writes. It was missing from
      // this list, so every line fell through to JSON.stringify and the UI
      // rendered the payload wrapper — {"content":"Gathering data…"} — instead
      // of the thought.
      const text = p.content ?? p.text ?? p.thought ?? p.message
      return typeof text === 'string' ? text.trim() : ''
    })
    .filter((s) => s.length > 0)
}

export interface EventView {
  tone: 'neutral' | 'accent' | 'good' | 'warn' | 'bad'
  label: string
  detail: string
}

function asString(v: unknown): string {
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return ''
}

/** Reads the structured `toolError` off a tool_result payload, if present. */
export function toolError(e: RunEvent): ToolError | null {
  const raw = (e.payload ?? {}).toolError
  if (typeof raw !== 'object' || raw === null) return null
  const rec = raw as Record<string, unknown>
  if (typeof rec.message !== 'string') return null
  return {
    kind: typeof rec.kind === 'string' ? rec.kind : 'platform',
    code: typeof rec.code === 'string' ? rec.code : '',
    message: rec.message,
    retryable: rec.retryable === true,
  }
}

/** Turns a raw ledger event into the one dense line the timeline shows. */
/** Milliseconds between two ledger entries. */
export function gapMs(prev: RunEvent | undefined, e: RunEvent): number {
  if (!prev) return 0
  return Math.max(0, new Date(e.at).getTime() - new Date(prev.at).getTime())
}

/**
 * The step a row belongs to, so the timeline can be grouped by who was
 * working rather than read as one undifferentiated stream.
 */
export function actorOf(e: RunEvent): string {
  if (e.agent) return e.agent
  if (e.type.startsWith('intelligence')) return 'intelligence'
  return 'run'
}

/** The full payload, pretty-printed, for the expanded row. */
export function payloadText(e: RunEvent): string {
  try {
    return JSON.stringify(e.payload ?? {}, null, 2)
  } catch {
    return ''
  }
}

/** Running token total at each point, so cost is visible as it accrues. */
export function tokenSeries(events: readonly RunEvent[]): Map<number, number> {
  const out = new Map<number, number>()
  let total = 0
  for (const e of events) {
    if (e.type === 'model_call') {
      const t = Number((e.payload as { tokens?: unknown } | undefined)?.tokens ?? 0)
      if (Number.isFinite(t) && t > total) total = t
    }
    out.set(e.seq, total)
  }
  return out
}

export function describeEvent(e: RunEvent): EventView {
  const p = e.payload ?? {}
  switch (e.type) {
    case 'run_queued':
      return { tone: 'neutral', label: 'queued', detail: 'Run accepted' }
    case 'status':
      return { tone: 'accent', label: 'status', detail: asString(p.status) || 'changed' }
    case 'intelligence_start':
      return { tone: 'accent', label: 'intelligence', detail: `first pass started${p.requestId ? ` · ${asString(p.requestId)}` : ''}` }
    case 'intelligence_thinking':
      return { tone: 'neutral', label: 'thinking', detail: asString(p.text) || asString(p.thought) || '…' }
    case 'intelligence_analysis':
      return { tone: 'good', label: 'analysis', detail: p.chars ? `${asString(p.chars)} chars of analysis` : 'analysis received' }
    case 'agent_start':
      return { tone: 'accent', label: `${e.agent ?? 'agent'} start`, detail: 'agent took over' }
    case 'agent_end':
      return { tone: 'good', label: `${e.agent ?? 'agent'} end`, detail: asString(p.verdict) || (p.agrees === undefined ? 'finished' : p.agrees ? 'agrees with the first pass' : 'disagrees with the first pass') }
    case 'model_call':
      return { tone: 'neutral', label: 'model', detail: p.tokens ? `${asString(p.tokens)} tokens` : 'model call' }
    case 'tool_call': {
      const tool = asString(p.tool) || 'tool'
      const args = p.args && typeof p.args === 'object' ? JSON.stringify(p.args) : ''
      return { tone: 'accent', label: tool, detail: args }
    }
    case 'tool_result': {
      const tool = asString(p.tool) || 'tool'
      // A failed tool call is the whole story of why a claim went unverified,
      // so the structured message is the row, not a generic "failed".
      const err = toolError(e)
      if (err) {
        return { tone: 'bad', label: `${tool} ✗`, detail: `${err.kind}: ${err.message}${err.retryable ? ' (retryable)' : ''}` }
      }
      const ok = p.ok !== false
      return { tone: ok ? 'good' : 'bad', label: `${tool} →`, detail: asString(p.summary) || asString(p.error) || (ok ? 'ok' : 'failed') }
    }
    case 'finding':
      return { tone: 'good', label: 'finding', detail: `${asString(p.detail)}${p.ref ? ` [${asString(p.ref)}]` : ''}` }
    case 'budget':
      return { tone: 'warn', label: 'budget', detail: `${asString(p.toolCalls)}/${asString(p.maxToolCalls)} tools · ${asString(p.modelTokens)}/${asString(p.maxModelTokens)} tokens` }
    case 'error':
      return { tone: 'bad', label: 'error', detail: asString(p.message) || 'something broke' }
    default:
      return { tone: 'neutral', label: e.type, detail: Object.keys(p).length ? JSON.stringify(p) : '' }
  }
}

/** Timeline filter buckets. Deliberately three, not thirteen. */
export const TIMELINE_FILTERS = ['all', 'tools', 'agents'] as const
export type TimelineFilter = (typeof TIMELINE_FILTERS)[number]

export function filterEvents(events: readonly RunEvent[], filter: TimelineFilter): RunEvent[] {
  if (filter === 'all') return [...events]
  if (filter === 'tools') return events.filter((e) => e.type === 'tool_call' || e.type === 'tool_result' || e.type === 'finding')
  return events.filter((e) => e.type.startsWith('agent_') || e.type.startsWith('intelligence_') || e.type === 'status' || e.type === 'error')
}

/** One honest line explaining a terminal status. Empty for the happy path. */
export function statusExplanation(run: Pick<Run, 'status' | 'error'>): string {
  if (isInterrupted(run)) {
    return 'The agent restarted while this run was in flight. Nothing was concluded \u2014 start it again and it will pick up from a clean slate.'
  }
  switch (run.status) {
    case 'failed':
      return run.error || 'The run stopped before it could reach a conclusion.'
    case 'budget_exceeded':
      return (
        run.error ||
        'The run hit its tool or token ceiling and stopped rather than overspend. Whatever it had reached by then is below, and it is incomplete.'
      )
    case 'canceled':
      return 'Canceled. Whatever is below is exactly as far as it got.'
    default:
      return ''
  }
}
