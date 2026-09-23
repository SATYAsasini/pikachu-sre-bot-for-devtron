import { useState } from 'react'
import { Gauge, Layers, ScrollText, Server, Zap } from 'lucide-react'
import { cn } from 'cn'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { AiTrigger } from '@/components/agent/ai-trigger'
import { Segmented } from '@/components/common/segmented'
import { Chip } from '@/components/common/status'
import { Text } from '@/components/common/text'
import { AgentMark } from '@/components/agent/agent-mark'
import type { Alert, Depth, RunOptions, SourceMode } from '@/lib/types'

const DEPTHS: { value: Depth; label: string; blurb: string }[] = [
  { value: 'quick', label: 'Quick', blurb: 'Verify the first pass and stop. One fast model call.' },
  { value: 'auto', label: 'Auto', blurb: 'Go deeper only if the verdict leaves something open. Usually right.' },
  { value: 'deep', label: 'Deep', blurb: 'Always run the deep dive, even on a settled verdict. Slowest.' },
]

const SOURCES: { value: SourceMode; label: string }[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'on', label: 'Require' },
  { value: 'off', label: 'Skip' },
]

/**
 * Confirm what the agent is about to do, and let the operator tune it.
 *
 * Only run-shaped choices are here: which cluster, how far to go, which data
 * sources may be read. Prompts are deliberately absent — they are the product,
 * not configuration, and a per-run prompt box would make every result
 * unreproducible.
 */
export function DebugDialog({
  alert,
  clusterName,
  open,
  onOpenChange,
  onTrigger,
  busy,
  metricsAvailable,
  logsAvailable,
}: {
  alert: Alert | null
  clusterName?: string
  open: boolean
  onOpenChange: (v: boolean) => void
  onTrigger: (options: RunOptions) => void
  busy?: boolean
  metricsAvailable?: boolean
  logsAvailable?: boolean
}) {
  const [depth, setDepth] = useState<Depth>('auto')
  const [metrics, setMetrics] = useState<SourceMode>('auto')
  const [logs, setLogs] = useState<SourceMode>('auto')

  if (!alert) return null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <div className="flex items-start gap-2.5">
            <AgentMark mood="working" size={40} />
            <div className="min-w-0">
              <DialogTitle className="font-display text-sm">Debug this alert</DialogTitle>
              <DialogDescription className="text-xs">
                Read-only. Nothing in your cluster changes — the agent looks, then tells you what it would do.
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        {/* What is fixed for this run. */}
        <dl className="grid gap-px overflow-hidden rounded-md border border-border bg-border text-xs">
          <div className="flex items-baseline gap-2 bg-card px-2.5 py-1.5">
            <dt className="flex w-24 shrink-0 items-center gap-1.5 text-muted-foreground">
              <Zap aria-hidden className="size-3" /> Alert
            </dt>
            <dd className="min-w-0 flex-1 truncate font-medium">{alert.name}</dd>
            <Chip tone={alert.severity === 'critical' ? 'bad' : 'warn'}>{alert.severity}</Chip>
          </div>
          <div className="flex items-baseline gap-2 bg-card px-2.5 py-1.5">
            <dt className="flex w-24 shrink-0 items-center gap-1.5 text-muted-foreground">
              <Server aria-hidden className="size-3" /> Cluster
            </dt>
            <dd className="min-w-0 flex-1 truncate font-mono">{clusterName ?? '—'}</dd>
          </div>
          {(alert.namespace || alert.resource) && (
            <div className="flex items-baseline gap-2 bg-card px-2.5 py-1.5">
              <dt className="flex w-24 shrink-0 items-center gap-1.5 text-muted-foreground">
                <Layers aria-hidden className="size-3" /> Target
              </dt>
              <dd className="min-w-0 flex-1 truncate font-mono">
                {alert.namespace ?? '—'}
                {alert.resource ? ` / ${alert.resource}` : ''}
              </dd>
            </div>
          )}
        </dl>

        {/* How hard to try. */}
        <fieldset className="space-y-1.5">
          <legend className="text-[0.625rem] font-medium tracking-widest text-muted-foreground uppercase">
            How far to go
          </legend>
          <div className="grid gap-1.5 sm:grid-cols-3">
            {DEPTHS.map((d) => (
              <button
                key={d.value}
                type="button"
                onClick={() => setDepth(d.value)}
                aria-pressed={depth === d.value}
                className={cn(
                  'relative rounded-md border-2 px-2.5 py-2 text-left transition-all',
                  'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                  depth === d.value
                    ? 'border-accent-strong bg-accent-strong/10 shadow-sm'
                    : 'border-border bg-card hover:border-accent-strong/40 hover:bg-well',
                )}
              >
                <span className="flex items-center gap-1.5">
                  {/* A radio dot, because these are exclusive and a border
                      alone does not say so. */}
                  <span
                    aria-hidden
                    className={cn(
                      'grid size-3.5 shrink-0 place-items-center rounded-full border-2 transition-colors',
                      depth === d.value ? 'border-accent-strong' : 'border-muted-foreground/40',
                    )}
                  >
                    {depth === d.value ? <span className="size-1.5 rounded-full bg-accent-strong" /> : null}
                  </span>
                  <span className="text-xs font-semibold">{d.label}</span>
                </span>
                <span className="mt-1 block pl-5 text-[0.625rem] leading-snug text-muted-foreground">{d.blurb}</span>
              </button>
            ))}
          </div>
        </fieldset>

        {/* What it may read. */}
        <fieldset className="space-y-1.5">
          <legend className="text-[0.625rem] font-medium tracking-widest text-muted-foreground uppercase">
            Data sources
          </legend>
          <SourceRow
            icon={<Gauge aria-hidden className="size-3" />}
            label="Metrics"
            available={metricsAvailable}
            value={metrics}
            onChange={setMetrics}
          />
          <SourceRow
            icon={<ScrollText aria-hidden className="size-3" />}
            label="Logs & events"
            available={logsAvailable}
            value={logs}
            onChange={setLogs}
          />
        </fieldset>

        <DialogFooter className="gap-2 sm:justify-between">
          <Text tone="fine" className="hidden sm:block">
            Prompts are not editable — that is what keeps two runs comparable.
          </Text>
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <AiTrigger size="sm" busy={busy} onClick={() => onTrigger({ depth, metrics, logs })}>
              {busy ? 'Starting…' : 'Trigger debug with AI'}
            </AiTrigger>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SourceRow({
  icon,
  label,
  available,
  value,
  onChange,
}: {
  icon: React.ReactNode
  label: string
  available?: boolean
  value: SourceMode
  onChange: (v: SourceMode) => void
}) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-border bg-card px-2.5 py-1.5">
      <span className="flex min-w-0 flex-1 items-center gap-1.5 text-xs">
        <span className="text-muted-foreground">{icon}</span>
        {label}
        {available === false && (
          <Chip tone="unknown" className="ml-1">
            none found
          </Chip>
        )}
      </span>
      <Segmented value={value} onChange={onChange} options={SOURCES} label={label} />
    </div>
  )
}
