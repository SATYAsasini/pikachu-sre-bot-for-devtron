import { Link } from '@tanstack/react-router'
import { Bell, ChevronRight, MessageSquareText } from 'lucide-react'
import { Chip } from '@/components/common/status'
import { Truncated } from '@/components/common/mono'
import { RunStatusChip } from '@/components/run/run-status'
import { duration, relativeTime, shortId } from '@/lib/format'
import { verdictTone } from '@/lib/tone'
import type { Run } from '@/lib/types'

/**
 * One run, one row. The three outputs are summarised as three signals — status,
 * verdict, agreement — because that is what someone scanning history is after.
 */
export function RunRow({ run }: { run: Run }) {
  const alert = run.trigger.alert
  const verdict = run.verdict
  const scope = [run.scope.clusterName, run.scope.namespace ?? run.scope.environmentName, run.scope.appName].filter(Boolean).join(' / ')

  return (
    <li>
      <Link
        to="/runs/$runId"
        params={{ runId: run.id }}
        className="flex items-start gap-3 px-3 py-2.5 transition-colors hover:bg-accent/50 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <div className="mt-0.5 shrink-0">
          <RunStatusChip run={run} />
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            {alert ? <Bell aria-hidden className="size-3 shrink-0 text-muted-foreground" /> : <MessageSquareText aria-hidden className="size-3 shrink-0 text-muted-foreground" />}
            <span className="truncate text-sm font-medium">{alert ? alert.name : 'Free-text question'}</span>
            {alert ? <Chip tone="neutral">{alert.severity}</Chip> : null}
          </div>
          <Truncated text={alert?.summary || run.trigger.ask || '—'} className="mt-0.5 text-xs text-muted-foreground" />
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[0.625rem] text-muted-foreground">
            <span className="truncate">{scope || '—'}</span>
            <span className="opacity-50">·</span>
            <span>{shortId(run.id, 8)}</span>
          </div>
        </div>

        <div className="flex shrink-0 flex-col items-end gap-1">
          <div className="flex items-center gap-1.5">
            {verdict ? <Chip tone={verdictTone(verdict.verdict)}>{verdict.verdict.replace('_', ' ')}</Chip> : null}
            {run.report ? <Chip tone={run.report.agrees ? 'ok' : 'warn'}>{run.report.agrees ? 'agrees' : 'disagrees'}</Chip> : null}
          </div>
          <div className="flex items-center gap-1.5 text-[0.625rem] text-muted-foreground">
            <span className="tabular">{duration(run.durationMs)}</span>
            <span className="opacity-50">·</span>
            <span>{relativeTime(run.createdAt)}</span>
            <ChevronRight aria-hidden className="size-3" />
          </div>
        </div>
      </Link>
    </li>
  )
}
