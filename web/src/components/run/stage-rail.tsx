import { motion, useReducedMotion } from 'motion/react'
import { MinusCircle } from 'lucide-react'
import { cn } from 'cn'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { BorderBeam } from '@/components/fx/border-beam'
import { AgentFrames } from '@/components/agent/agent-frames'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import type { Mood } from '@/components/agent/mood'
import type { Stage, StageState } from '@/lib/run-derive'

const STATE_MOOD: Record<StageState, Mood> = {
  waiting: 'asleep',
  running: 'working',
  done: 'pleased',
  failed: 'concerned',
  skipped: 'asleep',
  not_run: 'asleep',
}

const STATE_WORD: Record<StageState, string> = {
  waiting: 'not started',
  running: 'working',
  done: 'complete',
  failed: 'errored',
  // A decision we made, versus a stage the run never reached. Saying "not
  // needed" for the second one claims credit for an outcome we did not choose.
  skipped: 'not needed',
  not_run: 'did not run',
}

/**
 * Where the run stands, across the top.
 *
 * It used to run down the left, which was right when the left was empty. Now
 * that navigation lives there, two vertical rails side by side read as one
 * confusing column — and a run's own progress is the first thing anyone looks
 * for on this page, so it belongs above the content rather than beside it.
 *
 * Laid out as a row of equal cards with a connector between them, so the three
 * still read as a sequence rather than as three independent badges.
 */
export function StageRail({ stages }: { stages: Stage[] }) {
  const still = useReducedMotion()

  return (
    <nav aria-label="Run stages" className="mb-3">
      <ol className="grid gap-2 sm:grid-cols-3">
        {stages.map((stage, i) => {
          const state = stage.state
          const last = i === stages.length - 1
          return (
            <li key={stage.key} className="relative min-w-0">
              {/* Connector, so the three read as one sequence. Horizontal on a
                  row, and gone once the cards stack. */}
              {!last && (
                <span
                  aria-hidden
                  className={cn(
                    'absolute top-1/2 -right-2 hidden h-px w-2 sm:block',
                    state === 'done' ? 'bg-ok/40' : 'bg-border',
                  )}
                />
              )}

              <Tooltip>
                <TooltipTrigger asChild>
                  <a
                    href={`#stage-${stage.key}`}
                    aria-current={state === 'running' ? 'step' : undefined}
                    className={cn(
                      'relative flex min-w-0 items-center gap-2.5 rounded-lg border px-3 py-2 transition-colors',
                      'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                      state === 'done' && 'border-ok/30 bg-ok/5',
                      state === 'running' && 'border-accent-strong/40 bg-accent-strong/5',
                      state === 'failed' && 'border-bad/30 bg-bad/5',
                      state === 'skipped' && 'border-dashed border-border bg-transparent',
                      state === 'not_run' && 'border-dashed border-border bg-transparent opacity-70',
                      state === 'waiting' && 'border-border bg-card',
                    )}
                  >
                    {state === 'running' && <BorderBeam duration={3.4} size={120} />}

                    <span className="relative z-10 shrink-0">
                      {state === 'skipped' || state === 'not_run' ? (
                        <MinusCircle
                          aria-hidden
                          className={cn('size-8', state === 'skipped' ? 'text-unknown' : 'text-muted-foreground/50')}
                        />
                      ) : (
                        <AgentFrames mood={STATE_MOOD[state]} size={36} />
                      )}
                      {/* This stage is Devtron's own first pass, not ours.
                          Saying so with their mark is clearer than saying it
                          in the title, which is already doing other work. */}
                      {stage.key === 'intelligence' ? (
                        <span className="absolute -right-1 -bottom-0.5 grid size-4 place-items-center rounded-full border border-border bg-card shadow-sm">
                          <DevtronGlyph aria-hidden className="h-2 w-auto" />
                        </span>
                      ) : null}
                    </span>

                    <span className="relative z-10 min-w-0 flex-1">
                      <span className="flex items-baseline gap-1.5">
                        <span className="tabular text-[0.625rem] text-muted-foreground">{stage.index}</span>
                        <span
                          className={cn(
                            'truncate text-xs font-semibold',
                            state === 'skipped' || state === 'not_run' ? 'text-muted-foreground' : 'text-foreground',
                          )}
                        >
                          {stage.title}
                        </span>
                      </span>
                      <motion.span
                        key={state}
                        initial={still ? false : { opacity: 0, y: -3 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.2 }}
                        className={cn(
                          'mt-px block truncate text-[0.6875rem]',
                          state === 'done' && 'text-ok',
                          state === 'running' && 'text-accent-strong',
                          state === 'failed' && 'text-bad',
                          (state === 'skipped' || state === 'not_run' || state === 'waiting') &&
                            'text-muted-foreground',
                        )}
                      >
                        {STATE_WORD[state]}
                      </motion.span>
                    </span>
                  </a>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="max-w-72">
                  {stage.skippedWhy ?? stage.subtitle}
                </TooltipContent>
              </Tooltip>
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
