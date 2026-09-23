import { AlertTriangle, Sparkles } from 'lucide-react'
import { PanelBody } from '@/components/common/panel'
import { ThinkingStream } from '@/components/run/thinking-stream'
import { Mono } from '@/components/common/mono'
import { Markdown } from '@/components/common/markdown'
import { TextSkeleton } from '@/components/common/skeletons'
import { EmptyState } from '@/components/common/empty-state'
import { intelligencePhase, type Run } from '@/lib/types'
import type { StageState } from '@/lib/run-derive'

/**
 * Output ① — what Devtron Intelligence said.
 *
 * Four states, because `intelligence` is null until the first pass starts,
 * arrives with an empty analysis while it streams, then fills — and may come
 * back with `failed` set, which is the state that matters most: everything
 * below it is then weaker, and saying so is the job.
 */
export function IntelligencePanel({
  run,
  thinking,
  state,
}: {
  run: Run | undefined
  /** Thinking lines pulled out of the ledger. */
  thinking: string[]
  state: StageState
}) {
  const phase = intelligencePhase(run)
  const intel = run?.intelligence
  const thinkingCount = intel?.thinkingCount ?? thinking.length

  return (
      <PanelBody className="space-y-3">
        {phase === 'failed' ? (
          <div className="flex items-start gap-2 rounded-md border border-warn/30 bg-warn/5 px-2.5 py-2">
            <AlertTriangle aria-hidden className="mt-0.5 size-3.5 shrink-0 text-warn" />
            <div className="min-w-0 text-xs">
              <div className="font-medium text-warn">Devtron's first pass errored</div>
              <p className="mt-0.5 leading-relaxed text-muted-foreground">
                There is no first-pass analysis to check. Our agents continued on deterministic facts alone, so treat the verdict below as weaker
                than usual — it had nothing to disagree with.
              </p>
              <Mono className="mt-1 text-bad" value={intel?.failed ?? ''} title="Reported error" />
            </div>
          </div>
        ) : null}

        {phase === 'not_started' ? (
          state === 'skipped' ? (
            <EmptyState
              icon={Sparkles}
              title="The first pass never ran"
              line="This run ended before Devtron Intelligence was asked anything. Nothing was lost — there was simply nothing to check."
            />
          ) : (
            <div className="space-y-2 py-1">
              <p className="text-xs text-muted-foreground">Waiting on Devtron's first pass…</p>
              <TextSkeleton lines={4} />
            </div>
          )
        ) : null}

        {phase === 'streaming' ? (
          <ThinkingStream lines={thinking} total={Math.max(thinkingCount, thinking.length)} live />
        ) : null}

        {phase === 'complete' && intel ? <Markdown>{intel.analysis}</Markdown> : null}

        {phase !== 'streaming' && thinkingCount > 0 ? (
          <ThinkingStream lines={thinking} total={thinkingCount} />
        ) : null}
    </PanelBody>
  )
}
