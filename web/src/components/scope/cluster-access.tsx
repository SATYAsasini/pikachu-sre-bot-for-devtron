import { useState } from 'react'
import { Boxes, Check, Layers, RefreshCw, X } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Panel, PanelBody, PanelHeader, Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Text } from '@/components/common/text'
import { ErrorState } from '@/components/common/error-state'
import { RowSkeleton } from '@/components/common/skeletons'
import { useClusterAccess } from '@/lib/queries'

/**
 * What this token can actually read in one cluster, and where.
 *
 * The reach check answers "may we look at all", and it has to be cheap
 * enough to run across every cluster an installation lists — so it asks one
 * question and stops. This is the other half: the namespaces that came back
 * from it, and what each Kubernetes kind yields inside whichever one you
 * pick. It costs five reads, which is precisely why it is here, for the one
 * cluster somebody opened, rather than in the sweep.
 *
 * A denied kind is worth as much as an allowed one. "The agent cannot read
 * Events on this cluster" explains an investigation that came back thin
 * better than any amount of looking at the run will.
 */
export function ClusterAccess({ clusterId, clusterName }: { clusterId: number; clusterName?: string }) {
  const [namespace, setNamespace] = useState<string>('')
  const q = useClusterAccess(clusterId, namespace || undefined)

  const namespaces = q.data?.namespaces ?? []
  const kinds = q.data?.kinds ?? []
  const readable = kinds.filter((k) => k.allowed).length

  return (
    <div className="grid gap-3 xl:grid-cols-[22rem_minmax(0,1fr)]">
      <Panel>
        <PanelHeader
          icon={<Layers aria-hidden className="size-3.5" />}
          title="Namespaces"
          description={`What this token can see in ${clusterName ?? 'this cluster'}.`}
          actions={namespaces.length > 0 ? <Chip tone="neutral">{namespaces.length}</Chip> : null}
        />
        <PanelBody className="space-y-1.5">
          {q.isPending ? (
            <RowSkeleton rows={4} />
          ) : namespaces.length === 0 ? (
            <Text tone="fine">
              None came back. Either the token cannot list namespaces here, or Devtron maps no environments to
              this cluster.
            </Text>
          ) : (
            <>
              <NamespaceRow
                label="All namespaces"
                hint="Read across the cluster, which a scoped token may be refused."
                selected={namespace === ''}
                onSelect={() => setNamespace('')}
              />
              <ul data-lenis-prevent className="max-h-80 space-y-1 overflow-y-auto">
                {namespaces.map((n) => (
                  <li key={n}>
                    <NamespaceRow label={n} mono selected={namespace === n} onSelect={() => setNamespace(n)} />
                  </li>
                ))}
              </ul>
            </>
          )}
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader
          icon={<Boxes aria-hidden className="size-3.5" />}
          title="What the agent may read"
          description={
            namespace ? `Measured inside ${namespace}.` : 'Measured across the cluster, not inside a namespace.'
          }
          actions={
            <div className="flex items-center gap-1.5">
              {kinds.length > 0 ? (
                <Chip tone={readable === kinds.length ? 'ok' : readable > 0 ? 'warn' : 'bad'}>
                  {readable} of {kinds.length}
                </Chip>
              ) : null}
              <Button size="xs" variant="outline" disabled={q.isFetching} onClick={() => void q.refetch()}>
                <RefreshCw aria-hidden className={cn('size-3', q.isFetching && 'animate-spin')} />
                {q.isFetching ? 'Reading…' : 'Re-read'}
              </Button>
            </div>
          }
        />
        <PanelBody className="space-y-1.5">
          {q.isPending ? (
            <RowSkeleton rows={5} />
          ) : q.isError ? (
            <ErrorState error={q.error} onRetry={() => void q.refetch()} />
          ) : (
            <>
              {readable === 0 && kinds.length > 0 ? (
                <Well className="border-bad/35 bg-bad/5">
                  <Text tone="muted">
                    Nothing is readable {namespace ? `in ${namespace}` : 'across this cluster'}. An investigation
                    pointed here would work from the alert text alone and say so. Try a namespace on the left, or
                    widen the token to Kubernetes Resources → View.
                  </Text>
                </Well>
              ) : null}

              <ul className="space-y-1">
                {kinds.map((k) => (
                  <li
                    key={k.kind}
                    className={cn(
                      'rounded-lg border px-2.5 py-1.5',
                      k.allowed ? 'border-border bg-card' : 'border-bad/25 bg-bad/5',
                    )}
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      {k.allowed ? (
                        <Check aria-hidden className="size-3 shrink-0 text-ok" />
                      ) : (
                        <X aria-hidden className="size-3 shrink-0 text-bad" />
                      )}
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{k.kind}</span>
                      {k.allowed ? (
                        <span className="tabular shrink-0 text-[0.6875rem] text-muted-foreground">
                          {k.count} {k.count === 1 ? 'object' : 'objects'}
                        </span>
                      ) : null}
                    </div>
                    {k.detail ? (
                      <p className="mt-0.5 text-[0.625rem] leading-relaxed text-bad">{k.detail}</p>
                    ) : null}
                  </li>
                ))}
              </ul>

              <Text tone="fine">
                These are the reads an investigation makes. A kind that is denied here is one the agent will say
                it could not check, rather than one it quietly skips.
              </Text>
            </>
          )}
        </PanelBody>
      </Panel>
    </div>
  )
}

function NamespaceRow({
  label,
  hint,
  mono,
  selected,
  onSelect,
}: {
  label: string
  hint?: string
  mono?: boolean
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? 'true' : undefined}
      className={cn(
        'w-full rounded-lg border px-2.5 py-1.5 text-left transition-colors',
        'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
        selected ? 'border-accent-strong bg-accent-strong/8' : 'border-border bg-card hover:border-accent-strong/40',
      )}
    >
      <span className={cn('block truncate text-xs', mono && 'font-mono')}>{label}</span>
      {hint ? <span className="mt-0.5 block text-[0.625rem] text-muted-foreground">{hint}</span> : null}
    </button>
  )
}
