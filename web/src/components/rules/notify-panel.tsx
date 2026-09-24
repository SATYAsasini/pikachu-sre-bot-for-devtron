import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { BellRing, Check, Send, X } from 'lucide-react'
import { cn } from 'cn'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Segmented } from '@/components/common/segmented'
import { Text } from '@/components/common/text'
import { api, errorMessage } from '@/lib/api'
import { CHANNELS, PRIORITIES, type Channel, type NotifyConfig, type Priority } from '@/lib/types'

/**
 * Where a cluster's findings go.
 *
 * One event, deliberately. Every monitoring tool already tells you something
 * broke — that is the notification people have muted. What none of them send
 * is "we investigated it, here is the root cause and the first thing to do",
 * arriving while somebody is still reading the alert. Opened, acknowledged
 * and resolved events were designed and then cut; they are cheap to add and
 * they are how a channel becomes noise.
 *
 * The webhook URL never comes back from the server, so the field shows a hint
 * and an empty save means "unchanged". Clearing is an explicit button, because
 * editing a priority rule must not silently disconnect the channel.
 */
export function NotifyPanel({
  clusterId,
  clusterName,
  notify,
  onChange,
}: {
  clusterId: number
  clusterName?: string
  notify: NotifyConfig
  onChange: (next: NotifyConfig) => void
}) {
  const [editing, setEditing] = useState(false)
  const set = (patch: Partial<NotifyConfig>) => onChange({ ...notify, ...patch })

  const test = useMutation({
    mutationFn: () => api.testNotify({ clusterId, clusterName, notify }),
  })

  const configured = notify.urlSet || (notify.url ?? '') !== ''

  return (
    <section className="space-y-3">
      <div className="rounded-xl border border-border bg-card p-3 shadow-card">
        <header className="flex flex-wrap items-center gap-2">
          <BellRing aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <h3 className="font-display text-xs font-semibold tracking-tight">Send findings to a channel</h3>
            <Text tone="fine" className="mt-0.5">
              One message per finished investigation: the root cause and the first thing to do.
            </Text>
          </div>
          <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-border bg-well px-2 py-1">
            <input
              type="checkbox"
              checked={notify.enabled}
              onChange={(e) => set({ enabled: e.target.checked })}
              className="size-3.5 accent-[var(--accent-strong)]"
            />
            <span className="text-[0.6875rem] font-medium">{notify.enabled ? 'Enabled' : 'Off'}</span>
          </label>
        </header>

        <div className="mt-3 space-y-2.5">
          <Field label="Channel">
            <Segmented
              value={notify.channel || 'slack'}
              onChange={(c) => set({ channel: c as Channel })}
              options={CHANNELS}
              label="Which channel to post to"
            />
          </Field>

          <Field label="Webhook URL">
            {configured && !editing ? (
              <div className="flex flex-wrap items-center gap-2">
                <Chip tone="ok" icon={<Check aria-hidden className="size-3" />}>
                  configured {notify.urlHint ?? ''}
                </Chip>
                <Button size="xs" variant="outline" onClick={() => setEditing(true)}>
                  Replace
                </Button>
                <Button
                  size="xs"
                  variant="ghost"
                  className="text-muted-foreground hover:text-bad"
                  onClick={() => set({ url: '', urlSet: false, urlHint: '', clearUrl: true })}
                >
                  <X aria-hidden className="size-3" />
                  Clear
                </Button>
              </div>
            ) : (
              <Input
                value={notify.url ?? ''}
                onChange={(e) => set({ url: e.target.value, clearUrl: false })}
                placeholder={placeholderFor(notify.channel)}
                aria-label="Webhook URL"
                className="h-7 font-mono text-[0.6875rem]"
              />
            )}
          </Field>

          <Field label="Only at or above">
            <Segmented
              value={notify.minPriority || 'all'}
              onChange={(p) => set({ minPriority: p === 'all' ? '' : (p as Priority) })}
              options={[{ value: 'all', label: 'All' }, ...PRIORITIES.map((p) => ({ value: p, label: p }))]}
              label="Minimum priority to notify"
            />
          </Field>
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-2.5">
          {/* A pasted webhook is wrong surprisingly often — a trailing space,
              the wrong workspace, a revoked token. Finding that out during an
              incident, when the message that matters does not arrive, is the
              worst possible time. */}
          <Button size="xs" variant="outline" onClick={() => test.mutate()} disabled={test.isPending || !configured}>
            <Send aria-hidden className="size-3" />
            {test.isPending ? 'Sending…' : 'Send a test'}
          </Button>

          {test.data?.ok ? (
            <Chip tone="ok" icon={<Check aria-hidden className="size-3" />}>
              delivered
            </Chip>
          ) : test.data?.error ? (
            <Text tone="fine" as="span" className="text-bad">
              {test.data.error}
            </Text>
          ) : test.isError ? (
            <Text tone="fine" as="span" className="text-bad">
              {errorMessage(test.error)}
            </Text>
          ) : null}
        </div>
      </div>

      <Well>
        <Text tone="label">What arrives</Text>
        <pre className="mt-1.5 overflow-x-auto rounded-md border border-border bg-card p-2 font-mono text-[0.625rem] leading-relaxed whitespace-pre-wrap">
          {`[P0] KubePodCrashLooping on pgvector-6cccdcb6f6 — the first pass was wrong

pgvector was applied with a bare \`kubectl create deployment\`: no env, no
volumes, no probes, so the container exits before initdb completes.

Do first: capture the crash text before changing anything (risk: low)`}
        </pre>
        <Text tone="fine" className="mt-1.5">
          Nothing is sent when an alert opens, is acknowledged or resolves. Those are the notifications people
          mute, and a muted channel is worse than none.
        </Text>
      </Well>
    </section>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className={cn('flex flex-wrap items-center gap-2')}>
      <span className="w-32 shrink-0 text-[0.625rem] tracking-wider text-muted-foreground uppercase">{label}</span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

function placeholderFor(c?: Channel): string {
  switch (c) {
    case 'discord':
      return 'https://discord.com/api/webhooks/…'
    case 'webhook':
      return 'https://your-service.internal/hooks/sre'
    default:
      return 'https://hooks.slack.com/services/…'
  }
}
