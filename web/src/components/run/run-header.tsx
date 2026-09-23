import { Link } from '@tanstack/react-router'
import { Ban, Bell, ChevronLeft, MessageSquareText, RotateCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Chip, Dot, Meter } from '@/components/common/status'
import { CopyValue, Truncated } from '@/components/common/mono'
import { RunStatusChip } from '@/components/run/run-status'
import { statusExplanation } from '@/lib/run-derive'
import { compactNumber, duration, relativeTime, shortId } from '@/lib/format'
import { useNow } from '@/lib/use-now'
import { isLive, type Run } from '@/lib/types'
import type { StreamStatus } from '@/lib/use-run-stream'
import { severityTone } from '@/lib/tone'

/**
 * Two dense lines under the top bar: what this run is, where it is, and what
 * it has spent. Sticky, because the status badge is the thing people glance
 * back at while the page streams.
 */
export function RunHeader({
  run,
  stream,
  onCancel,
  canceling,
  onRerun,
}: {
  run: Run
  stream: StreamStatus
  onCancel: () => void
  canceling: boolean
  onRerun?: () => void
}) {
  const live = isLive(run.status)
  const now = useNow(1000, live)
  const elapsed = live && run.startedAt ? now - Date.parse(run.startedAt) : run.durationMs
  const explanation = statusExplanation(run)
  const alert = run.trigger.alert

  const scopeParts = [run.scope.clusterName, run.scope.namespace ?? run.scope.environmentName, run.scope.appName].filter(Boolean) as string[]

  return (
    <div className="sticky top-12 z-30 -mx-4 mb-3 border-b border-border bg-background/90 px-4 py-2 backdrop-blur sm:-mx-6 sm:px-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <Button asChild variant="ghost" size="icon-sm" className="-ml-1 shrink-0">
          <Link to="/runs" aria-label="Back to run history">
            <ChevronLeft aria-hidden />
          </Link>
        </Button>

        <RunStatusChip run={run} />

        <CopyValue value={run.id} display={shortId(run.id, 8)} label="run id" />

        <nav aria-label="Run scope" className="flex min-w-0 items-center gap-1 font-mono text-xs text-muted-foreground">
          {scopeParts.map((p, i) => (
            <span key={`${p}-${i}`} className="flex min-w-0 items-center gap-1">
              {i > 0 ? <span className="text-border">/</span> : null}
              <span className="max-w-[12rem] truncate">{p}</span>
            </span>
          ))}
          {run.scope.appType ? <Chip tone="neutral" className="ml-1">{run.scope.appType}</Chip> : null}
        </nav>

        <div className="ml-auto flex shrink-0 items-center gap-2">
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="tabular cursor-help text-xs font-medium">{duration(elapsed)}</span>
            </TooltipTrigger>
            <TooltipContent>
              Started {relativeTime(run.startedAt ?? run.createdAt)}
              {run.finishedAt ? ` · finished ${relativeTime(run.finishedAt)}` : ''}
            </TooltipContent>
          </Tooltip>

          <StreamChip status={stream} live={live} />

          {live ? (
            // Stopping a run that is burning budget is urgent, so it looks
            // urgent and sits at a normal size rather than as a ghost chip.
            <Button size="sm" variant="destructive" onClick={onCancel} disabled={canceling}>
              <Ban aria-hidden />
              {canceling ? 'Stopping…' : 'Cancel run'}
            </Button>
          ) : onRerun ? (
            <Button size="xs" variant="outline" onClick={onRerun}>
              <RotateCw aria-hidden />
              Run again
            </Button>
          ) : null}
        </div>
      </div>

      <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 pl-1">
        <span className="flex min-w-0 max-w-full items-center gap-1.5 text-xs text-muted-foreground md:max-w-[52%]">
          {alert ? <Bell aria-hidden className="size-3 shrink-0" /> : <MessageSquareText aria-hidden className="size-3 shrink-0" />}
          {alert ? (
            <>
              <span className="shrink-0 font-medium text-foreground">{alert.name}</span>
              <Chip tone={severityTone(alert.severity)}>{alert.severity}</Chip>
              <Truncated text={alert.summary} className="min-w-0" />
            </>
          ) : (
            <Truncated text={run.trigger.ask || 'No prompt recorded'} className="min-w-0" />
          )}
        </span>

        <div className="ml-auto flex shrink-0 items-center gap-3">
          <Meter label="tools" value={run.usage.toolCalls} max={run.usage.maxToolCalls} />
          <Meter label="tokens" value={run.usage.modelTokens} max={run.usage.maxModelTokens} format={compactNumber} />
        </div>
      </div>

      {explanation ? (
        <p className="mt-1.5 rounded border border-border bg-well px-2 py-1 text-[0.6875rem] leading-relaxed text-muted-foreground">{explanation}</p>
      ) : null}
    </div>
  )
}

function StreamChip({ status, live }: { status: StreamStatus; live: boolean }) {
  if (!live && status !== 'reconnecting' && status !== 'error') return null

  const look =
    status === 'live'
      ? { tone: 'ok' as const, text: 'live', why: 'Connected. Events are arriving as they happen.' }
      : status === 'connecting'
        ? { tone: 'unknown' as const, text: 'connecting', why: 'Opening the event stream.' }
        : status === 'reconnecting'
          ? { tone: 'warn' as const, text: 'reconnecting', why: 'The stream dropped. It resumes from the last sequence number, so nothing is lost.' }
          : status === 'error'
            ? { tone: 'bad' as const, text: 'stream down', why: 'The event stream could not be re-established. The run itself may still be going; the page falls back to polling.' }
            : { tone: 'neutral' as const, text: 'ended', why: 'The server closed the stream.' }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help">
          <Chip tone={look.tone} icon={<Dot tone={look.tone} pulse={status === 'live'} />}>
            {look.text}
          </Chip>
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-72">{look.why}</TooltipContent>
    </Tooltip>
  )
}
