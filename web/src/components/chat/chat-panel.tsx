import { useEffect, useRef, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { MessageSquareText, PlayCircle, X } from 'lucide-react'
import { Textarea } from '@/components/ui/textarea'
import { Markdown } from '@/components/common/markdown'
import { Text } from '@/components/common/text'
import { AgentFrames } from '@/components/agent/agent-frames'
import { AiTrigger } from '@/components/agent/ai-trigger'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { ThinkingStream } from '@/components/run/thinking-stream'
import { useChat, type RunProposal } from '@/lib/use-chat'
import { useStartRun } from '@/lib/use-start-run'
import { Chip } from '@/components/common/status'
import { duration } from '@/lib/format'
import type { RunOptions, SourceMode } from '@/lib/types'

/**
 * A conversation, not an investigation.
 *
 * Typing a question used to mint a run: an id, a ledger, a verdict and a row
 * in history that said "Free-text question — rkwn" and could not be deleted.
 * A run is an auditable record of something that was investigated; a question
 * is not that, and forcing one into the shape of the other made history
 * useless and the question awkward.
 *
 * So this is its own surface. It streams, it holds context across turns, it
 * survives a reload, and closing it erases it — which is true rather than
 * merely claimed, because the browser was the only thing holding it.
 *
 * Alerts still create runs. That is the line: something the cluster raised is
 * worth a record, something a person wondered aloud is not.
 */
export function ChatPanel({
  open,
  onClose,
  clusterId,
  clusterName,
  seed,
}: {
  open: boolean
  onClose: () => void
  clusterId?: number
  clusterName?: string
  /** The question that opened the panel, sent once on open. */
  seed?: string
}) {
  const still = useReducedMotion()
  const { turns, thinking, busy, send, clear } = useChat(clusterId, clusterName)
  const [draft, setDraft] = useState('')
  const scroller = useRef<HTMLDivElement>(null)
  const sent = useRef<string | null>(null)

  // The opening question is delivered once. A ref rather than state because a
  // re-render must not be able to ask it twice.
  useEffect(() => {
    if (!open || !seed || sent.current === seed) return
    sent.current = seed
    void send(seed)
    // `send` changes identity whenever the thread does, and depending on it
    // here would re-fire the seed after every answer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, seed])

  useEffect(() => {
    if (!open) sent.current = null
  }, [open])

  useEffect(() => {
    const el = scroller.current
    if (el) el.scrollTop = el.scrollHeight
  }, [turns.length, thinking.length])

  // Escape closes, which is the gesture people reach for before the button.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  const dismiss = () => {
    // Closing is the erase. Nothing to confirm: there is no copy anywhere else
    // and the panel says so before you press it.
    clear()
    onClose()
  }

  return (
    <AnimatePresence>
      {open ? (
        <>
          <motion.div
            key="scrim"
            initial={still ? false : { opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            onClick={dismiss}
            className="fixed inset-0 z-50 bg-background/60 backdrop-blur-sm"
            aria-hidden
          />

          <motion.aside
            key="panel"
            role="dialog"
            aria-label="Chat with the agent"
            initial={still ? false : { x: '100%' }}
            animate={{ x: 0 }}
            exit={{ x: '100%' }}
            transition={still ? { duration: 0 } : { duration: 0.42, ease: [0.22, 1, 0.36, 1] }}
            className="fixed inset-y-0 right-0 z-50 flex w-full max-w-xl flex-col border-l border-border bg-card shadow-2xl"
          >
            <header className="flex shrink-0 items-center gap-2.5 border-b border-border px-4 py-3">
              <AgentFrames mood={busy ? 'working' : 'idle'} size={40} aware={busy} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <MessageSquareText aria-hidden className="size-3.5 shrink-0 text-accent-strong" />
                  <span className="font-display text-sm font-semibold">Ask the agent</span>
                </div>
                <Text tone="fine" className="mt-0.5">
                  Nothing here is recorded. Close it and the conversation is gone.
                  {clusterName ? ` Looking at ${clusterName}.` : ''}
                </Text>
              </div>
              <button
                type="button"
                onClick={dismiss}
                aria-label="Close and erase this conversation"
                className="grid size-8 shrink-0 place-items-center rounded-lg border border-border text-muted-foreground transition-colors hover:border-bad/40 hover:text-bad focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
              >
                <X aria-hidden className="size-4" />
              </button>
            </header>

            <div ref={scroller} data-lenis-prevent className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-4">
              {turns.length === 0 && !busy ? (
                <div className="space-y-3 py-6">
                  <div className="text-center">
                    <Text tone="muted">Ask anything about {clusterName ?? 'this cluster'} — or about me.</Text>
                    <Text tone="fine" className="mt-1">
                      A conversation, not an investigation. It leaves no run in your history.
                    </Text>
                  </div>
                  {/* Four routes answer this panel, and nobody would guess the
                      last three from an empty box. */}
                  <ul className="mx-auto max-w-sm space-y-1">
                    {[
                      'Why is the scheduler reported down?',
                      'What did my recent runs find?',
                      'What permissions does your API token need?',
                      'Investigate the pgvector crash loop',
                    ].map((q) => (
                      <li key={q}>
                        <button
                          type="button"
                          onClick={() => void send(q)}
                          className="w-full rounded-lg border border-border bg-card px-2.5 py-1.5 text-left text-[0.6875rem] text-muted-foreground transition-colors hover:border-accent-strong/40 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                        >
                          {q}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}

              {turns.map((t, i) => (
                <Turn key={i} turn={t} />
              ))}

              {/* Only once there is something to show. An empty trail used to
                  render the run page's "Devtron counted N steps but did not
                  keep the text" line, which is meaningless in a conversation
                  and read as an answer. */}
              {busy ? (
                thinking.length > 0 ? (
                  <ThinkingStream lines={thinking} total={thinking.length} live />
                ) : (
                  <div className="flex items-center gap-2 rounded-xl border border-border bg-well px-3 py-2">
                    <span aria-hidden className="relative flex size-1.5 shrink-0">
                      <span className="absolute inline-flex size-full animate-ping rounded-full bg-accent-strong opacity-70" />
                      <span className="relative inline-flex size-1.5 rounded-full bg-accent-strong" />
                    </span>
                    <Text tone="fine" as="span">
                      Thinking…
                    </Text>
                  </div>
                )
              ) : null}
            </div>

            <footer className="shrink-0 border-t border-border p-3">
              <div className="relative">
                <Textarea
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key !== 'Enter' || e.nativeEvent.isComposing || e.shiftKey) return
                    e.preventDefault()
                    if (busy || draft.trim() === '') return
                    void send(draft)
                    setDraft('')
                  }}
                  placeholder="Ask a follow-up…"
                  rows={2}
                  className="resize-none pr-3 text-sm"
                />
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-3">
                <Text tone="fine" as="span">
                  <kbd className="rounded border border-border bg-well px-1 font-mono">↵</kbd> to send ·{' '}
                  <kbd className="rounded border border-border bg-well px-1 font-mono">⇧</kbd>
                  <kbd className="ml-0.5 rounded border border-border bg-well px-1 font-mono">↵</kbd> new line
                </Text>
                <AiTrigger
                  size="sm"
                  busy={busy}
                  disabled={draft.trim() === ''}
                  onClick={() => {
                    void send(draft)
                    setDraft('')
                  }}
                  className="ml-auto"
                >
                  {busy ? 'Thinking…' : 'Ask'}
                </AiTrigger>
              </div>
            </footer>
          </motion.aside>
        </>
      ) : null}
    </AnimatePresence>
  )
}

function Turn({ turn }: { turn: ReturnType<typeof useChat>['turns'][number] }) {
  if (turn.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="max-w-[85%] rounded-xl rounded-br-sm border border-accent-strong/30 bg-accent-strong/10 px-3 py-2">
          <p className="text-xs leading-relaxed whitespace-pre-wrap">{turn.text}</p>
        </div>
      </div>
    )
  }

  if (turn.error) {
    return (
      <div className="rounded-xl border border-bad/30 bg-bad/5 px-3 py-2">
        <Text tone="label">Could not answer</Text>
        <p className="mt-0.5 text-xs leading-relaxed text-bad">{turn.error}</p>
      </div>
    )
  }

  const src = turn.source ?? 'intelligence'

  return (
    <div className="space-y-2">
      <div className="rounded-xl rounded-bl-sm border border-border bg-well px-3 py-2">
        <Markdown tight>{turn.text}</Markdown>

        {/* Where the answer came from. Four routes answer this panel, and an
            answer about the agent's own history is a different kind of claim
            from one Devtron made about a cluster — so it says which. */}
        <div className="mt-1.5 flex flex-wrap items-center gap-1.5 border-t border-border pt-1.5">
          {src === 'intelligence' ? (
            <>
              <DevtronGlyph aria-hidden className="h-2.5 w-auto shrink-0" />
              <Text tone="fine" as="span">
                Devtron Intelligence
                {turn.steps ? ` · ${turn.steps} steps` : ''}
                {turn.ms ? ` · ${duration(turn.ms)}` : ''}
              </Text>
            </>
          ) : (
            <Text tone="fine" as="span">
              {src === 'runs'
                ? 'Read from your run history'
                : src === 'platform'
                  ? 'About this agent — no cluster was queried'
                  : 'Prepared, not started'}
            </Text>
          )}
        </div>
      </div>

      {turn.proposal ? <Proposal proposal={turn.proposal} /> : null}
    </div>
  )
}

/**
 * A run the agent prepared and did not start.
 *
 * Starting one costs a Devtron call, two models and a tool budget, and it
 * leaves a permanent record. Doing that because somebody used the word
 * "investigate" in a sentence would be presumptuous, so the parameters are
 * shown and the decision stays with the reader.
 */
/** The proposal's knobs, narrowed to the unions the API expects. */
function proposalOptions(p: RunProposal): RunOptions {
  const depth = (['auto', 'quick', 'deep'] as const).find((d) => d === p.options?.depth) ?? 'auto'
  const source = (v: string | undefined): SourceMode =>
    (['auto', 'on', 'off'] as const).find((s) => s === v) ?? 'auto'
  return { depth, metrics: source(p.options?.metrics), logs: source(p.options?.logs) }
}

function Proposal({ proposal }: { proposal: RunProposal }) {
  const { start, pending } = useStartRun()
  const [fired, setFired] = useState(false)

  const rows: [string, string][] = [
    ['Cluster', proposal.clusterName || String(proposal.clusterId)],
    ...(proposal.namespace ? ([['Namespace', proposal.namespace]] as [string, string][]) : []),
    ['Question', proposal.ask],
    ['Depth', proposal.options?.depth ?? 'auto'],
    ['Metrics', proposal.options?.metrics ?? 'auto'],
    ['Logs', proposal.options?.logs ?? 'auto'],
  ]

  return (
    <div className="overflow-hidden rounded-xl border-2 border-accent-strong/40 bg-accent-strong/5 shadow-card">
      <div className="flex items-center gap-1.5 border-b border-accent-strong/25 px-3 py-2">
        <PlayCircle aria-hidden className="size-3.5 shrink-0 text-accent-strong" />
        <span className="text-xs font-semibold">Run ready to trigger</span>
        <Chip tone="neutral" className="ml-auto shrink-0">
          leaves a record
        </Chip>
      </div>

      <dl className="divide-y divide-border/60">
        {rows.map(([k, v]) => (
          <div key={k} className="flex gap-2 px-3 py-1.5">
            <dt className="w-20 shrink-0 text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">
              {k}
            </dt>
            <dd className="min-w-0 flex-1 text-[0.6875rem] leading-relaxed">{v}</dd>
          </div>
        ))}
      </dl>

      <div className="flex items-center gap-2 border-t border-accent-strong/25 px-3 py-2">
        <Text tone="fine" as="span">
          {fired ? 'Started — opening the run…' : 'Unlike this chat, a run is recorded.'}
        </Text>
        <AiTrigger
          size="sm"
          busy={pending || fired}
          onClick={() => {
            setFired(true)
            // The card lists depth, metrics and logs, so the run has to
            // actually use them — passing only the question quietly ignored
            // every parameter it had just shown.
            start({ ask: proposal.ask, options: proposalOptions(proposal) })
          }}
          className="ml-auto"
        >
          {fired ? 'Starting…' : 'Trigger run'}
        </AiTrigger>
      </div>
    </div>
  )
}
