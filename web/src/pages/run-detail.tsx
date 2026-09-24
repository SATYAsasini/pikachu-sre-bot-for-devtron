import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Stethoscope } from 'lucide-react'
import { Panel } from '@/components/common/panel'
import { ErrorState } from '@/components/common/error-state'
import { PanelSkeleton } from '@/components/common/skeletons'
import { Skeleton } from '@/components/ui/skeleton'
import { RunHeader } from '@/components/run/run-header'
import { PhaseFlow } from '@/components/run/phase-flow'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { StageSection } from '@/components/run/stage-section'
import { Conclusion } from '@/components/run/conclusion'
import { TriggerCard } from '@/components/run/trigger-card'
import { IntelligencePanel } from '@/components/run/intelligence-panel'
import { VerdictPanel } from '@/components/run/verdict-panel'
import { ReportPanel } from '@/components/run/report-panel'
import { TimelineRail } from '@/components/run/timeline-rail'
import { KnowledgeSheet } from '@/components/run/knowledge-sheet'
import { qk, useCancelRun, useCreateRun, useRun, useRunLedger } from '@/lib/queries'
import { mergeEvents, useRunStream } from '@/lib/use-run-stream'
import { deriveStages, thinkingTrail, type Stage } from '@/lib/run-derive'
import { usePublishAgentState } from '@/lib/agent-presence'
import { Rise, Stagger } from '@/components/fx/reveal'
import { duration, humanise, pluralise } from '@/lib/format'
import { errorMessage } from '@/lib/api'
import { intelligencePhase, isLive, type Run } from '@/lib/types'

type StageKey = 'gather' | 'sre'

/**
 * The run detail page: the answer first, the working underneath.
 *
 * It used to open with Devtron's first pass and put our conclusion three
 * panels down, so the first thing anyone read was the analysis we were in the
 * middle of disproving. Now the conclusion is the top of the page, the alert
 * that caused the run sits directly under it, and each of the three stages is
 * one summarised line that opens if you want the working. The tool log —
 * eighty rows of `prom_query` — is a spine on the right that expands on
 * approach instead of holding a permanent column.
 */
export function RunDetailPage() {
  const { runId } = useParams({ from: '/runs/$runId' })
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [knowledgeId, setKnowledgeId] = useState<string | null>(null)

  const ledger = useRunLedger(runId)
  const ledgerEvents = useMemo(() => ledger.data ?? [], [ledger.data])
  const afterSeq = ledgerEvents.length > 0 ? ledgerEvents[ledgerEvents.length - 1].seq : 0

  // The run is polled as a slow safety net; the stream is what actually drives
  // the page. Polling stops the moment the run is terminal.
  const runQuery = useRun(runId, true)
  const run = runQuery.data
  const live = run ? isLive(run.status) : true

  const stream = useRunStream(runId, afterSeq, ledger.isSuccess && live)
  const events = useMemo(() => mergeEvents(ledgerEvents, stream.events), [ledgerEvents, stream.events])

  // Every arriving event may mean the run object gained a verdict or a report,
  // so refetch it rather than guessing from the payload.
  useEffect(() => {
    if (stream.events.length === 0) return
    void qc.invalidateQueries({ queryKey: qk.run(runId) })
  }, [stream.events.length, qc, runId])

  useEffect(() => {
    if (!stream.done) return
    void qc.invalidateQueries({ queryKey: qk.run(runId) })
    void qc.invalidateQueries({ queryKey: ['runs'] })
  }, [stream.done, qc, runId])

  const stages = useMemo(() => deriveStages(run, events), [run, events])

  // Feed the shell's companion so the agent in the corner reflects this run.
  usePublishAgentState(run, events)
  const thinking = useMemo(() => thinkingTrail(events), [events])

  const cancel = useCancelRun()
  const rerun = useCreateRun()

  const onCancel = () => {
    cancel.mutate(runId, {
      onSuccess: () => toast.success('Run canceled', { description: 'Whatever it had reached is still on the page.' }),
      onError: (e) => toast.error('Could not cancel', { description: errorMessage(e) }),
    })
  }

  const onRerun = run
    ? () => {
        rerun.mutate(
          {
            clusterId: run.scope.clusterId,
            clusterName: run.scope.clusterName,
            environmentId: run.scope.environmentId,
            namespace: run.scope.namespace,
            appName: run.scope.appName,
            appType: run.scope.appType,
            alert: run.trigger.alert ?? null,
            ask: run.trigger.alert ? '' : run.trigger.ask,
          },
          {
            onSuccess: (next) => {
              toast.success('Running it again', { description: 'Same scope, same question, clean slate.' })
              void navigate({ to: '/runs/$runId', params: { runId: next.id } })
            },
            onError: (e) => toast.error('Could not start the run', { description: errorMessage(e) }),
          },
        )
      }
    : undefined

  // Jumping from the conclusion has to open the stage as well as scroll to it,
  // or the click lands on a collapsed line and appears to do nothing.
  const [forced, setForced] = useState<StageKey | null>(null)
  const onJump = (key: StageKey) => {
    setForced(key)
    requestAnimationFrame(() => document.getElementById(`stage-${key}`)?.scrollIntoView({ block: 'start' }))
  }

  if (runQuery.isError) {
    return (
      <Panel>
        <ErrorState error={runQuery.error} onRetry={() => void runQuery.refetch()} />
      </Panel>
    )
  }

  const summaries = stageSummaries(run, thinking.length)

  return (
    <>
      {run ? (
        <RunHeader run={run} stream={stream.status} onCancel={onCancel} canceling={cancel.isPending} onRerun={onRerun} />
      ) : (
        <div className="sticky top-12 z-30 -mx-4 mb-3 space-y-2 border-b border-border bg-background/90 px-4 py-2.5 backdrop-blur sm:-mx-6 sm:px-6">
          <div className="flex items-center gap-3">
            <Skeleton className="h-5 w-24 rounded-full" />
            <Skeleton className="h-5 w-20" />
            <Skeleton className="h-4 w-56" />
          </div>
          <Skeleton className="h-3 w-80" />
        </div>
      )}

      {/* Who did what, across the top. Two parties, and the split between
          them is the product: Devtron gathers, we reason. */}
      <PhaseFlow stages={stages} run={run} events={events} />

      <div className="flex gap-3">
        <div className="min-w-0 flex-1 space-y-3">
          {runQuery.isLoading && !run ? (
            <>
              <PanelSkeleton lines={4} />
              <PanelSkeleton lines={3} />
            </>
          ) : (
            <Stagger className="space-y-3">
              {/* ① The answer. Present the moment either half of it exists. */}
              {run?.verdict || run?.report ? (
                <Rise>
                  <Conclusion verdict={run.verdict ?? null} report={run.report ?? null} onJump={onJump} />
                </Rise>
              ) : null}

              {/* ② Why the run exists at all, and what it turned out to be
                  about. Above the working, because you need it to read the
                  working. */}
              {run ? (
                <Rise>
                  <TriggerCard run={run} component={run.verdict?.component} onOpenKnowledge={setKnowledgeId} />
                </Rise>
              ) : null}

              {/* ③ The working, one section per party. */}
              <Rise>
                <div className="space-y-2">
                  <StageSection
                    id="stage-gather"
                    index={1}
                    title="What Devtron gathered"
                    icon={<DevtronGlyph aria-hidden className="h-3 w-auto" />}
                    state={stageState(stages, 'gather')}
                    summary={summaries.gather}
                    open={forced === 'gather' || openFor(stages, 'gather')}
                  >
                    <IntelligencePanel run={run} thinking={thinking} state={stageState(stages, 'gather')} />
                  </StageSection>

                  <StageSection
                    id="stage-sre"
                    index={2}
                    title="What we made of it"
                    icon={<Stethoscope aria-hidden className="size-3.5" />}
                    state={stageState(stages, 'sre')}
                    summary={summaries.sre}
                    open={forced === 'sre' || openFor(stages, 'sre')}
                  >
                    {/* One agent produced both halves, so they read as one
                        section rather than as two stages that might not have
                        run. */}
                    <div className="divide-y divide-border">
                      <VerdictPanel verdict={run?.verdict} state={stageState(stages, 'sre')} onOpenKnowledge={setKnowledgeId} />
                      <ReportPanel report={run?.report} state={stageState(stages, 'sre')} onOpenKnowledge={setKnowledgeId} />
                    </div>
                  </StageSection>
                </div>
              </Rise>
            </Stagger>
          )}
        </div>

        <TimelineRail events={events} loading={ledger.isLoading} live={live} />
      </div>

      <KnowledgeSheet componentId={knowledgeId} onClose={() => setKnowledgeId(null)} />
    </>
  )
}

function stageState(stages: Stage[], key: string) {
  return stages.find((s) => s.key === key)?.state ?? 'waiting'
}

/**
 * A stage is open while it is producing, and shut once it has produced.
 *
 * The exception is a failure, which stays open — nobody wants to hunt for the
 * reason a run died behind a disclosure triangle.
 */
function openFor(stages: Stage[], key: string): boolean {
  const state = stageState(stages, key)
  return state === 'running' || state === 'failed'
}

/**
 * The one line each stage reduces to.
 *
 * These carry what the stage headers used to: duration and request id for the
 * first pass, the verdict word and claim count for the judge, agreement and
 * step count for the deep-dive. If a line here is not worth reading, the
 * stage below it is not worth opening.
 */
/**
 * The one line each phase reduces to.
 *
 * If a line here is not worth reading, the section below it is not worth
 * opening.
 */
function stageSummaries(run: Run | undefined, thinkingCount: number) {
  const intel = run?.intelligence
  const phase = intelligencePhase(run)
  const verdict = run?.verdict
  const report = run?.report

  const gather =
    phase === 'failed'
      ? 'Devtron errored — we continued on facts alone'
      : phase === 'not_started'
        ? 'Not started'
        : [
            intel?.durationMs ? duration(intel.durationMs) : null,
            pluralise(intel?.thinkingCount ?? thinkingCount, 'thinking step'),
          ]
            .filter(Boolean)
            .join(' · ')

  const claims = verdict?.claims ?? []
  const contradicted = claims.filter((c) => c.status === 'contradicted').length

  const sre = !report
    ? verdict
      ? 'Graded, still writing remediation'
      : 'Nothing yet'
    : [
        verdict ? humanise(String(verdict.verdict)) : null,
        claims.length > 0 ? `${pluralise(claims.length, 'claim')} checked` : null,
        contradicted > 0 ? `${contradicted} contradicted` : null,
        pluralise((report.remediation ?? []).length, 'step'),
        `${pluralise((report.evidence ?? []).length, 'piece')} of evidence`,
      ]
        .filter(Boolean)
        .join(' · ')

  return { gather, sre }
}
