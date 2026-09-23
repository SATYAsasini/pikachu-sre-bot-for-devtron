import { useEffect, useRef, useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { MessageSquareText, X } from 'lucide-react'
import { Textarea } from '@/components/ui/textarea'
import { Markdown } from '@/components/common/markdown'
import { Text } from '@/components/common/text'
import { AgentFrames } from '@/components/agent/agent-frames'
import { AiTrigger } from '@/components/agent/ai-trigger'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { ThinkingStream } from '@/components/run/thinking-stream'
import { useChat } from '@/lib/use-chat'
import { duration } from '@/lib/format'

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
                <div className="py-8 text-center">
                  <Text tone="muted">Ask anything about {clusterName ?? 'this cluster'}.</Text>
                  <Text tone="fine" className="mt-1">
                    This is a conversation, not an investigation — it leaves no run in your history.
                  </Text>
                </div>
              ) : null}

              {turns.map((t, i) => (
                <Turn key={i} turn={t} />
              ))}

              {busy ? (
                <div className="space-y-2">
                  <ThinkingStream lines={thinking} total={Math.max(thinking.length, 1)} live />
                </div>
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

  return (
    <div className="rounded-xl rounded-bl-sm border border-border bg-well px-3 py-2">
      <Markdown tight>{turn.text}</Markdown>
      <div className="mt-1.5 flex flex-wrap items-center gap-2 border-t border-border pt-1.5">
        <DevtronGlyph aria-hidden className="h-2.5 w-auto shrink-0" />
        <Text tone="fine" as="span">
          Answered by Devtron Intelligence
          {turn.steps ? ` · ${turn.steps} steps` : ''}
          {turn.ms ? ` · ${duration(turn.ms)}` : ''}
        </Text>
      </div>
    </div>
  )
}
