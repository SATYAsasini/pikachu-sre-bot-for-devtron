import { useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { motion } from 'motion/react'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, ChevronDown, Cpu, Database, FileText, Info, Plug, ShieldCheck, Wrench } from 'lucide-react'
import { cn } from 'cn'
import { Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Heading, Text } from '@/components/common/text'
import { YamlView } from '@/components/common/yaml-view'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { FlowDiagram } from '@/components/setup/flow-diagram'
import { TextSkeleton } from '@/components/common/skeletons'
import { ErrorState } from '@/components/common/error-state'
import { api } from '@/lib/api'
import { qk } from '@/lib/queries'
import type { Harness, HarnessAgent, HarnessTool } from '@/lib/types'

/**
 * What this thing actually is.
 *
 * Every name, number and prompt here comes from `/v1/harness`, which the
 * server generates from the same constants the pipeline is assembled from —
 * the agent names, the tool allowlist, the registry's own descriptions, the
 * configured models, the live budget, and the two prompt files the binary
 * embedded. Nothing is transcribed. A hand-written architecture page is wrong
 * within a week of the first tool being added, and a page nobody trusts is
 * worse than no page.
 */
export function HarnessView() {
  const q = useQuery({ queryKey: qk.harness, queryFn: api.harness, staleTime: 5 * 60_000 })

  if (q.isLoading) return <TextSkeleton lines={12} />
  if (q.isError) return <ErrorState error={q.error} onRetry={() => void q.refetch()} />
  if (!q.data) return null
  const h = q.data

  return <HarnessPanes h={h} />
}

/** The six things this page has to say, in the order they build on each other. */
const PANES = [
  { key: 'overview', label: 'Overview', hint: 'What this page is, and the shape of a run.' },
  { key: 'agents', label: 'Agents', hint: 'Both agents, their tools and their full system prompts.' },
  { key: 'config', label: 'Configuration', hint: 'The whole agent as one YAML or JSON file.' },
  { key: 'flow', label: 'Data flow', hint: 'Who owns each step. Hover one to see what it touches.' },
  { key: 'apis', label: 'APIs → tools', hint: 'Five surfaces, twelve tools, and what each conversion cost.' },
  { key: 'findings', label: 'Findings', hint: 'What a builder platform would have to normalise first.' },
] as const

type PaneKey = (typeof PANES)[number]['key']

/**
 * Nine stacked sections was a scroll, not a page.
 *
 * Everything here is reference material someone came looking for a specific
 * part of, so it is addressable: one pane at a time, the choice in the URL, and
 * a rail that says what each pane holds before you open it. Nothing is
 * removed — it is the same content, findable.
 */
function HarnessPanes({ h }: { h: Harness }) {
  const navigate = useNavigate()
  const { panel } = useSearch({ from: '/settings' })
  const active: PaneKey = (PANES.find((p) => p.key === panel)?.key ?? 'overview') as PaneKey
  const open = (key: PaneKey) => void navigate({ to: '/settings', search: { section: 'about', panel: key } })

  return (
    <div className="space-y-4">
      <Intro />

      <nav aria-label="About sections" className="flex flex-wrap gap-1.5">
        {PANES.map((p) => {
          const on = p.key === active
          return (
            <button
              key={p.key}
              type="button"
              onClick={() => open(p.key)}
              aria-current={on ? 'page' : undefined}
              title={p.hint}
              className={cn(
                'rounded-lg border px-3 py-1.5 text-xs transition-all',
                'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                on
                  ? 'border-accent-strong/45 bg-accent-strong/12 font-semibold text-foreground shadow-card'
                  : 'border-border bg-card font-medium text-muted-foreground hover:border-accent-strong/30 hover:text-foreground',
              )}
            >
              {p.label}
            </button>
          )
        })}
      </nav>

      <div className="flex items-center gap-2 rounded-lg border border-border bg-well px-3 py-2">
        <Info aria-hidden className="size-3.5 shrink-0 text-accent-strong" />
        <Text tone="muted" as="span">
          {PANES.find((p) => p.key === active)?.hint}
        </Text>
      </div>

      {/* Keyed so each pane animates in rather than swapping instantly, which
          otherwise reads as the page having jumped. */}
      <motion.div
        key={active}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25, ease: [0.16, 1, 0.3, 1] }}
        className="space-y-6"
      >
        {active === 'overview' && (
          <>
            <Overview h={h} />
            <Entrypoints h={h} />
            <Pipeline h={h} />
            <Guarantees h={h} />
          </>
        )}
        {active === 'agents' && <Agents h={h} />}
        {active === 'config' && <FullConfig h={h} />}
        {active === 'flow' && <FlowDiagram h={h} />}
        {active === 'apis' && <Surfaces h={h} />}
        {active === 'findings' && (
          <>
            <Orchestration h={h} />
            <Normalise h={h} />
          </>
        )}
      </motion.div>
    </div>
  )
}

/**
 * What this page is for.
 *
 * Not documentation of a product — this is a personal build, made to find out
 * what it actually takes. The page is the finding.
 */
function Intro() {
  return (
    <section className="rounded-lg border border-accent-strong/30 bg-accent-strong/5 px-4 py-3">
      <Heading level={2}>What this page is</Heading>
      <Text tone="muted" className="mt-1 max-w-3xl">
        This agent was built to learn what building one costs. The question underneath it was whether an agent
        like this could be assembled from a UI — declared, not written — and if not, what a platform would have
        to standardise first. What follows is the answer, read out of the running binary: the tree that <em>is</em>{' '}
        declarative, the orchestration that had to be written by hand, how each Devtron API became something a
        model can call, and the seven things a builder would need to normalise.
      </Text>
      <Text tone="fine" className="mt-2 max-w-3xl">
        The shape below is tuned for this one use case. A real product would widen it — but the gaps it exposes
        are not specific to this use case at all.
      </Text>
    </section>
  )
}

function Overview({ h }: { h: Harness }) {
  const facts: [string, string][] = [
    ['Root agent', `${h.adk.root.name} · ${h.adk.root.type}`],
    ['Runtime', h.adk.module],
    ['Session state', h.adk.session],
    ['Tool budget', `${h.budget.maxToolCalls} calls`],
    ['Token budget', h.budget.maxModelTokens.toLocaleString()],
    ['Run timeout', `${h.budget.timeoutSeconds}s`],
  ]
  return (
    <dl className="grid gap-px overflow-hidden rounded-lg border border-border bg-border sm:grid-cols-2 lg:grid-cols-3">
      {facts.map(([k, v]) => (
        <div key={k} className="bg-card px-3 py-2">
          <dt className="text-[0.625rem] font-medium tracking-widest text-muted-foreground uppercase">{k}</dt>
          <dd className="mt-0.5 truncate font-mono text-xs">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

const KIND_TONE = {
  deterministic: 'border-border bg-well',
  devtron: 'border-accent-strong/35 bg-accent-strong/5',
  agent: 'border-ok/30 bg-ok/5',
} as const

/**
 * The harness as a diagram.
 *
 * Boxes and rules rather than an image, so it restyles with the theme, reads
 * at any width and stays selectable text. The three deterministic steps are
 * deliberately quieter than the two agents: facts first, models second, and
 * that difference is the whole design.
 */
function Pipeline({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2} className="mb-2">
        The pipeline
      </Heading>
      <ol className="grid gap-1.5 sm:grid-cols-2 xl:grid-cols-3">
        {h.pipeline.map((s, i) => (
          <li
            key={s.step}
            className={cn(
              'flex items-start gap-2 rounded-md border px-2.5 py-2',
              KIND_TONE[s.kind as keyof typeof KIND_TONE] ?? KIND_TONE.deterministic,
            )}
          >
            <span className="tabular mt-px w-4 shrink-0 text-center text-[0.625rem] text-muted-foreground">
              {i + 1}
            </span>
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-1.5">
                {s.kind === 'devtron' ? <DevtronGlyph aria-hidden className="h-2.5 w-auto shrink-0" /> : null}
                <span className="font-mono text-xs font-medium">{s.step}</span>
                <Chip tone={s.kind === 'agent' ? 'ok' : s.kind === 'devtron' ? 'accent' : 'neutral'}>{s.kind}</Chip>
              </span>
              <Text tone="fine" className="mt-0.5">
                {s.what}
              </Text>
            </span>
          </li>
        ))}
      </ol>
      <Text tone="fine" className="mt-1.5">
        Three of these are deterministic and run before any model is called, which is what lets the single agent
        that follows spend its budget on judgement rather than on gathering.
      </Text>
    </section>
  )
}

function Agents({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2} className="mb-2">
        The agents
      </Heading>
      <div className="grid gap-2 lg:grid-cols-2">
        {h.agents.map((a) => (
          <AgentCard key={a.name} a={a} />
        ))}
      </div>
    </section>
  )
}

function AgentCard({ a }: { a: HarnessAgent }) {
  const [openPrompt, setOpenPrompt] = useState(false)
  const named = ((a.tools ?? []) as HarnessTool[]).filter(
    (t): t is HarnessTool => typeof t === 'object' && t !== null,
  )

  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-lg border border-border bg-card">
      <div className="flex flex-wrap items-center gap-1.5 border-b border-border px-3 py-2">
        <span className="tabular text-[0.625rem] text-muted-foreground">{a.order}</span>
        <span className="font-mono text-xs font-semibold">{a.name}</span>
        <Chip tone="neutral" mono>
          → {a.outputKey}
        </Chip>
        <span className="ml-auto flex items-center gap-1 text-[0.6875rem] text-muted-foreground">
          <Cpu aria-hidden className="size-3" />
          <span className="font-mono">{a.model || '—'}</span>
        </span>
      </div>

      <div className="flex-1 space-y-2 px-3 py-2">
        <Text tone="muted">{a.role}</Text>

        <div className="flex items-start gap-1.5">
          <Wrench aria-hidden className="mt-0.5 size-3 shrink-0 text-muted-foreground" />
          {named.length === 0 ? (
            <Text tone="fine">
              No tools, deliberately. Grading an argument against a fact pack it already holds does not need a
              cluster, and tools would turn a two-second step into a second investigation.
            </Text>
          ) : (
            <div className="flex min-w-0 flex-wrap gap-1">
              {named.map((t) => (
                <span
                  key={t.name}
                  title={t.description}
                  className="cursor-help rounded border border-border bg-well px-1 font-mono text-[0.625rem] text-muted-foreground"
                >
                  {t.name}
                </span>
              ))}
            </div>
          )}
        </div>

        {(a.toolsets ?? []).map((t) => (
          <div key={t} className="flex items-center gap-1.5">
            <Database aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            <Text tone="fine" as="span">
              {t}
            </Text>
          </div>
        ))}

        <div className="flex flex-wrap items-center gap-1">
          <Text tone="fine" as="span">
            reads
          </Text>
          {a.reads.map((r) => (
            <span key={r} className="rounded border border-border bg-well px-1 font-mono text-[0.625rem]">
              {r}
            </span>
          ))}
        </div>

        {a.skippedWhen ? <Text tone="fine">Skipped when {a.skippedWhen}.</Text> : null}
      </div>

      {/* The prompt is the product, so it is here in full rather than
          described. Shut by default because it is a thousand words. */}
      {a.instruction ? (
        <div className="border-t border-border">
          {/* The prompt is the most interesting thing on this card — it is the
              agent, in the end — and it was hidden behind grey uppercase
              micro-text that read as a footnote. It is now an invitation:
              product colour, a real label, and the line count as the reason to
              click rather than as a measurement. */}
          <button
            type="button"
            onClick={() => setOpenPrompt((v) => !v)}
            aria-expanded={openPrompt}
            className={cn(
              'flex w-full items-center gap-2 px-3 py-2 text-left text-xs transition-colors',
              'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
              openPrompt ? 'bg-accent-strong/10' : 'bg-bolt/10 hover:bg-bolt/20',
            )}
          >
            <FileText aria-hidden className={cn('size-3.5 shrink-0', openPrompt ? 'text-accent-strong' : 'text-bolt-ink')} />
            <span className="font-semibold">{openPrompt ? 'Hide the system prompt' : 'Read the system prompt'}</span>
            <span className="tabular ml-auto text-[0.6875rem] text-muted-foreground">
              {a.instruction.split('\n').length} lines
            </span>
            <ChevronDown
              aria-hidden
              className={cn('size-3.5 shrink-0 text-muted-foreground transition-transform', openPrompt && 'rotate-180')}
            />
          </button>
          {openPrompt ? (
            <pre
              data-lenis-prevent
              className="max-h-96 overflow-auto border-t border-border bg-well px-3 py-2 font-mono text-[0.6875rem] leading-relaxed whitespace-pre-wrap"
            >
              <code>{a.instruction}</code>
            </pre>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/** The whole tree as one document, prompts included. */
function FullConfig({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2}>The whole agent, as a file</Heading>
      <Text tone="muted" className="mt-1 mb-2 max-w-3xl">
        This is the whole agent as data — tree, models, tools, budgets and both system prompts. It is generated
        from the running binary, so it is what is executing rather than a description of it. Everything here
        could be an input to a builder instead of an output of one; that is the point.
      </Text>
      <YamlView text={toYaml(h)} json={JSON.stringify(h, null, 2)} label="harness" maxHeight="40rem" />
    </section>
  )
}

/**
 * Serialise the harness to YAML by hand.
 *
 * A YAML library would be a dependency for one read-only view of a shape we
 * control completely, and the hand-written version orders keys the way a
 * reader wants them rather than the way a struct declares them.
 */
function toYaml(h: Harness): string {
  const q = (s: string) => (/[:#]|^\s|\s$/.test(s) ? JSON.stringify(s) : s)
  const out: string[] = []

  out.push('# Generated from the running binary. Nothing here is maintained by hand.')
  out.push('')
  out.push('runtime:')
  out.push(`  module: ${h.adk.module}`)
  out.push(`  session: ${q(h.adk.session)}`)
  out.push('  uses:')
  for (const u of h.adk.uses) out.push(`    - ${u}`)
  out.push('')
  out.push('budget:')
  out.push(`  maxToolCalls: ${h.budget.maxToolCalls}`)
  out.push(`  maxModelTokens: ${h.budget.maxModelTokens}`)
  out.push(`  timeoutSeconds: ${h.budget.timeoutSeconds}`)
  out.push('')
  out.push('root:')
  out.push(`  name: ${h.adk.root.name}`)
  out.push(`  type: ${h.adk.root.type}`)
  out.push('  subAgents:')
  for (const a of h.agents) out.push(`    - ${a.name}`)
  out.push('')
  out.push('pipeline:')
  for (const s of h.pipeline) {
    out.push(`  - step: ${s.step}`)
    out.push(`    kind: ${s.kind}`)
    out.push(`    what: ${q(s.what)}`)
  }
  out.push('')
  out.push('agents:')
  for (const a of h.agents) {
    const named = ((a.tools ?? []) as HarnessTool[]).filter(
      (t): t is HarnessTool => typeof t === 'object' && t !== null,
    )
    out.push(`  - name: ${a.name}`)
    out.push(`    order: ${a.order}`)
    out.push(`    model: ${q(a.model || '(unset)')}`)
    out.push(`    role: ${q(a.role)}`)
    out.push(`    outputKey: ${a.outputKey}`)
    out.push(`    reads: [${a.reads.join(', ')}]`)
    if (named.length === 0) {
      out.push('    tools: []          # deliberate: this step must stay cheap')
    } else {
      out.push('    tools:')
      for (const t of named) {
        out.push(`      - name: ${t.name}`)
        if (t.description) out.push(`        description: ${q(oneLine(t.description))}`)
      }
    }
    for (const t of a.toolsets ?? []) out.push(`    toolset: ${q(t)}`)
    if (a.skippedWhen) out.push(`    skippedWhen: ${q(a.skippedWhen)}`)
    if (a.instruction) {
      // A literal block scalar: the prompt is markdown with its own colons,
      // dashes and code fences, and quoting it inline would be unreadable.
      out.push('    instruction: |')
      for (const line of a.instruction.replace(/\s+$/, '').split('\n')) {
        out.push(line === '' ? '' : `      ${line}`)
      }
    }
  }
  out.push('')
  out.push('guarantees:')
  for (const g of h.guarantees) out.push(`  - ${q(g)}`)
  return out.join('\n')
}

function oneLine(s: string): string {
  return s.replace(/\s+/g, ' ').trim()
}

/**
 * The two ways in, side by side.
 *
 * People assume a question and an alert do the same thing because they start
 * in the same box. They do not, and the difference — one leaves a record, one
 * leaves nothing — is the sort of thing that has to be stated rather than
 * inferred from a missing row in history.
 */
function Entrypoints({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2}>Two ways in</Heading>
      <Text tone="muted" className="mt-1 mb-2 max-w-3xl">
        The same box takes both, and they are not the same thing.
      </Text>
      <div className="grid gap-2 lg:grid-cols-2">
        {h.entrypoints.map((e, i) => (
          <div
            key={e.name}
            className={cn(
              'overflow-hidden rounded-xl border bg-card shadow-card',
              i === 0 ? 'border-ok/35' : 'border-accent-strong/35',
            )}
          >
            <div
              className={cn(
                'flex flex-wrap items-center gap-2 border-b px-3 py-2',
                i === 0 ? 'border-ok/25 bg-ok/5' : 'border-accent-strong/25 bg-accent-strong/8',
              )}
            >
              <span className="text-xs font-semibold">{e.name}</span>
              <span className="ml-auto rounded border border-border bg-card px-1.5 py-0.5 font-mono text-[0.625rem] text-muted-foreground">
                {e.path}
              </span>
            </div>
            <dl className="divide-y divide-border">
              {(
                [
                  ['Started by', e.trigger],
                  ['Creates', e.creates],
                  ['Runs', e.stages],
                  ['Why', e.why],
                ] as const
              ).map(([k, v]) => (
                <div key={k} className="px-3 py-1.5">
                  <dt className="text-[0.625rem] font-medium tracking-widest text-muted-foreground uppercase">{k}</dt>
                  <dd className="mt-0.5 text-xs leading-relaxed">{v}</dd>
                </div>
              ))}
            </dl>
          </div>
        ))}
      </div>
    </section>
  )
}

/**
 * Every API, and what it took to make it callable.
 *
 * A REST endpoint is not a tool. The distance between the two is the part of
 * this job nobody budgets for, so it is written down per surface rather than
 * summarised.
 */
function Surfaces({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2}>APIs, and what it took to make them tools</Heading>
      <Text tone="muted" className="mt-1 mb-2 max-w-3xl">
        Five surfaces became twelve tools. None of them converted cleanly.
      </Text>
      <div className="space-y-2">
        {h.surfaces.map((s) => (
          <div key={s.package} className="overflow-hidden rounded-lg border border-border bg-card">
            <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
              <Plug aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="font-mono text-xs font-semibold">{s.package}</span>
              <ArrowRight aria-hidden className="size-3 shrink-0 text-muted-foreground" />
              <div className="flex min-w-0 flex-wrap gap-1">
                {s.tools.map((t) => (
                  <span key={t} className="rounded border border-border bg-well px-1 font-mono text-[0.625rem] text-muted-foreground">
                    {t}
                  </span>
                ))}
              </div>
            </div>
            <div className="grid gap-px bg-border lg:grid-cols-[22rem_minmax(0,1fr)]">
              <div className="space-y-1.5 bg-card px-3 py-2">
                <div>
                  <Text tone="label">Endpoint</Text>
                  <Text tone="fine" className="mt-0.5 font-mono break-words">
                    {s.api}
                  </Text>
                </div>
                <div>
                  <Text tone="label">Auth</Text>
                  <Text tone="fine" className="mt-0.5">
                    {s.auth}
                  </Text>
                </div>
              </div>
              <ul className="space-y-1 bg-card px-3 py-2">
                {s.conversion.map((c) => (
                  <li key={c} className="flex gap-1.5 text-xs leading-relaxed">
                    <span className="text-accent-strong">·</span>
                    <span className="min-w-0">{c}</span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

/**
 * Declared versus written.
 *
 * The honest answer to "could this be built from a UI": the agent tree could.
 * Four of these seven layers could be declared with a little more orchestrating
 * vocabulary. Three could not — they are cluster-shaped, not agent-shaped.
 */
function Orchestration({ h }: { h: Harness }) {
  const declarative = h.orchestration.filter((l) => l.declarative)
  const written = h.orchestration.filter((l) => !l.declarative)

  return (
    <section>
      <Heading level={2}>What could be declared, and what had to be written</Heading>
      <Text tone="muted" className="mt-1 mb-2 max-w-3xl">
        The tree itself is already data — the YAML above is the whole of it. These are the layers around it, which
        is where an agent builder would actually have to reach.
      </Text>
      <div className="grid gap-2 lg:grid-cols-2">
        <LayerList
          title={`Could be declarative (${declarative.length})`}
          hint="A builder could express these as configuration today, given the vocabulary."
          tone="ok"
          layers={declarative}
        />
        <LayerList
          title={`Had to be written (${written.length})`}
          hint="These depend on what the cluster answers, not on what the agent is. No UI discovers them for you."
          tone="warn"
          layers={written}
        />
      </div>
    </section>
  )
}

function LayerList({
  title,
  hint,
  tone,
  layers,
}: {
  title: string
  hint: string
  tone: 'ok' | 'warn'
  layers: Harness['orchestration']
}) {
  return (
    <div className={cn('overflow-hidden rounded-lg border bg-card', tone === 'ok' ? 'border-ok/30' : 'border-warn/30')}>
      <div className={cn('border-b px-3 py-2', tone === 'ok' ? 'border-ok/25 bg-ok/5' : 'border-warn/25 bg-warn/5')}>
        <Heading level={3}>{title}</Heading>
        <Text tone="fine" className="mt-0.5">
          {hint}
        </Text>
      </div>
      <ul className="divide-y divide-border">
        {layers.map((l) => (
          <li key={l.layer} className="px-3 py-2">
            <span className="font-mono text-xs font-medium">{l.layer}</span>
            <Text tone="fine" className="mt-0.5">
              {l.what}
            </Text>
          </li>
        ))}
      </ul>
    </div>
  )
}

/** The finding the whole page exists to carry. */
function Normalise({ h }: { h: Harness }) {
  return (
    <section>
      <Heading level={2}>What an agent-builder platform would have to normalise</Heading>
      <Text tone="muted" className="mt-1 mb-2 max-w-3xl">
        Seven things. None of them are model problems — they are runtime contracts, and a prompt cannot supply
        any of them.
      </Text>
      <ol className="grid gap-2 lg:grid-cols-2 xl:grid-cols-3">
        {h.normalise.map((n, i) => (
          <li key={n.item} className="flex gap-2.5 rounded-lg border border-border bg-card px-3 py-2.5">
            <span className="tabular mt-px shrink-0 font-mono text-xs text-accent-strong">
              {String(i + 1).padStart(2, '0')}
            </span>
            <span className="min-w-0">
              <span className="block text-xs font-semibold">{n.item}</span>
              <Text tone="fine" className="mt-0.5">
                {n.why}
              </Text>
            </span>
          </li>
        ))}
      </ol>
    </section>
  )
}

function Guarantees({ h }: { h: Harness }) {
  return (
    <Well className="space-y-1">
      <h3 className="flex items-center gap-1.5 text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase">
        <ShieldCheck aria-hidden className="size-3 text-ok" />
        What holds, always
      </h3>
      <ul className="grid gap-0.5 sm:grid-cols-2">
        {h.guarantees.map((g) => (
          <li key={g} className="flex gap-1.5 text-xs leading-relaxed">
            <span className="text-ok">·</span>
            <span className="min-w-0">{g}</span>
          </li>
        ))}
      </ul>
    </Well>
  )
}
