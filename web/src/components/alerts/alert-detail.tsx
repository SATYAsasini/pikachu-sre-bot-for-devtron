import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AiTrigger } from '@/components/agent/ai-trigger'
import { Chip, Dot } from '@/components/common/status'
import { Text } from '@/components/common/text'
import { relativeTime } from '@/lib/format'
import { severityTone, stateTone } from '@/lib/tone'
import type { Alert } from '@/lib/types'

/**
 * The whole alert, expanded.
 *
 * Everything the card had no room for: every label, every annotation, the
 * rule expression where the backend reported one, and the fingerprint as a
 * copyable value rather than nineteen digits of noise on the list.
 */
export function AlertDetail({
  alert,
  open,
  onOpenChange,
  onDebug,
}: {
  alert: Alert | null
  open: boolean
  onOpenChange: (v: boolean) => void
  onDebug: (alert: Alert) => void
}) {
  if (!alert) return null
  const labels = Object.entries(alert.labels ?? {}).sort(([a], [b]) => a.localeCompare(b))
  const annotations = Object.entries(alert.annotations ?? {}).filter(([, v]) => v)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <div className="flex items-start gap-2">
            <Dot tone={stateTone(alert.state)} pulse={alert.state === 'firing'} className="mt-1.5" />
            <div className="min-w-0">
              <DialogTitle className="flex flex-wrap items-center gap-2 font-display text-sm">
                <span className="truncate">{alert.name}</span>
                <Chip tone={severityTone(alert.severity)}>{alert.severity}</Chip>
                <Chip tone="neutral" mono>
                  {alert.source}
                </Chip>
              </DialogTitle>
              <DialogDescription className="text-xs">
                Firing {relativeTime(alert.startsAt)}
                {alert.namespace ? ` · ${alert.namespace}` : ''}
                {alert.resource ? ` / ${alert.resource}` : ''}
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <div className="max-h-[55vh] space-y-3 overflow-y-auto pr-1">
          {annotations.length > 0 && (
            <section className="space-y-1">
              <Text tone="label" as="h3">
                Annotations
              </Text>
              <dl className="space-y-1">
                {annotations.map(([k, v]) => (
                  <div key={k} className="rounded-md border border-border bg-well px-2.5 py-1.5">
                    <dt className="font-mono text-[0.625rem] text-muted-foreground">{k}</dt>
                    <dd className="mt-0.5 text-xs leading-relaxed break-words">{v}</dd>
                  </div>
                ))}
              </dl>
            </section>
          )}

          {alert.expression && (
            <section className="space-y-1">
              <Text tone="label" as="h3">
                Rule expression
              </Text>
              <pre className="overflow-x-auto rounded-md border border-border bg-well p-2.5 font-mono text-[0.6875rem] leading-relaxed">
                <code>{alert.expression}</code>
              </pre>
            </section>
          )}

          <section className="space-y-1">
            <Text tone="label" as="h3">
              Labels <span className="normal-case">({labels.length})</span>
            </Text>
            <dl className="grid gap-px overflow-hidden rounded-md border border-border bg-border sm:grid-cols-2">
              {labels.map(([k, v]) => (
                <div key={k} className="bg-card px-2.5 py-1">
                  <dt className="font-mono text-[0.625rem] text-muted-foreground">{k}</dt>
                  <dd className="font-mono text-[0.6875rem] break-all">{v}</dd>
                </div>
              ))}
            </dl>
          </section>

        </div>

        <div className="flex items-center justify-between gap-2 border-t border-border pt-3">
          <Text tone="fine">Read-only. Debugging changes nothing in your cluster.</Text>
          <AiTrigger size="sm" onClick={() => onDebug(alert)} />
        </div>
      </DialogContent>
    </Dialog>
  )
}
