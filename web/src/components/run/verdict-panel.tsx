import { BookOpen, CheckCircle2, CircleHelp, Gavel, HelpCircle, Target, XCircle } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from 'cn'
import { Disclose } from '@/components/common/disclose'
import { PanelBody, Well } from '@/components/common/panel'
import { Chip, type Tone } from '@/components/common/status'
import { TextSkeleton } from '@/components/common/skeletons'
import { EmptyState } from '@/components/common/empty-state'
import { humanise } from '@/lib/format'
import type { StageState } from '@/lib/run-derive'
import type { ClaimStatus, ComponentLayer, IdentifiedComponent, Verdict, VerdictKind } from '@/lib/types'

const VERDICT_LOOK: Record<string, { tone: Tone; label: string; line: string }> = {
  supported: { tone: 'ok', label: 'Supported', line: 'The first pass holds up against the facts we could check.' },
  partly_supported: { tone: 'warn', label: 'Partly supported', line: 'Some of it holds. Some of it does not. The breakdown is below.' },
  unsupported: { tone: 'bad', label: 'Unsupported', line: 'The facts contradict the first-pass analysis.' },
  insufficient: { tone: 'unknown', label: 'Insufficient evidence', line: 'Not enough could be checked to agree or disagree. That is a finding, not a failure.' },
}

const CLAIM_LOOK: Record<string, { tone: Tone; icon: LucideIcon; label: string }> = {
  supported: { tone: 'ok', icon: CheckCircle2, label: 'supported' },
  contradicted: { tone: 'bad', icon: XCircle, label: 'contradicted' },
  unverifiable: { tone: 'unknown', icon: CircleHelp, label: 'unverifiable' },
}

const LAYER_LABEL: Record<string, string> = {
  k8s_workload: 'Kubernetes workload',
  k8s_infra: 'Kubernetes infrastructure',
  devtron_cd: 'Devtron CD',
  devtron_platform: 'Devtron platform',
  known_app: 'Recognised application',
  unknown: 'Unrecognised',
}

function verdictLook(v: VerdictKind) {
  return VERDICT_LOOK[v] ?? { tone: 'unknown' as Tone, label: humanise(String(v)), line: '' }
}

function claimLook(s: ClaimStatus) {
  return CLAIM_LOOK[s] ?? { tone: 'unknown' as Tone, icon: CircleHelp, label: String(s) }
}

/**
 * Output ② — our verdict on the first pass.
 *
 * The headline verdict is deliberately one word with one sentence under it;
 * the claim-by-claim table is where the actual work shows, so it is not hidden
 * behind a tab or a toggle.
 */
export function VerdictPanel({
  verdict,
  state,
  onOpenKnowledge,
}: {
  verdict: Verdict | null | undefined
  state: StageState
  onOpenKnowledge: (componentId: string) => void
}) {
  const look = verdict ? verdictLook(verdict.verdict) : null
  const claims = verdict?.claims ?? []
  const counts = claims.reduce<Record<string, number>>((acc, c) => {
    acc[c.status] = (acc[c.status] ?? 0) + 1
    return acc
  }, {})

  return (
      <PanelBody className="space-y-3">
        {!verdict ? (
          state === 'running' ? (
            <div className="space-y-2 py-1">
              <p className="text-xs text-muted-foreground">The judge is checking the claims. This fills in as soon as it lands.</p>
              <TextSkeleton lines={4} />
            </div>
          ) : state === 'skipped' || state === 'failed' ? (
            <EmptyState
              icon={Gavel}
              title="No verdict was reached"
              line="The run ended before the judge could return one. There is nothing here to read into — it simply did not get that far."
            />
          ) : (
            <div className="space-y-2 py-1">
              <p className="text-xs text-muted-foreground">Queued behind the first pass.</p>
              <TextSkeleton lines={3} />
            </div>
          )
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <span
                className={cn(
                  'inline-flex items-center rounded-md border px-2 py-1 text-sm font-semibold tracking-tight',
                  look?.tone === 'ok' && 'border-ok/30 bg-ok/10 text-ok',
                  look?.tone === 'warn' && 'border-warn/30 bg-warn/10 text-warn',
                  look?.tone === 'bad' && 'border-bad/30 bg-bad/10 text-bad',
                  look?.tone === 'unknown' && 'border-unknown/30 bg-unknown/10 text-unknown',
                )}
              >
                {look?.label}
              </span>
              <p className="min-w-0 flex-1 text-xs text-muted-foreground">{look?.line}</p>
            </div>

            {claims.length > 0 ? (
              <div>
                <div className="mb-1.5 flex flex-wrap items-center gap-1.5">
                  <h3 className="text-[0.625rem] font-semibold uppercase tracking-widest text-muted-foreground">
                    Claims ({claims.length})
                  </h3>
                  {Object.entries(counts).map(([status, n]) => {
                    const cl = claimLook(status)
                    return (
                      <Chip key={status} tone={cl.tone} icon={<cl.icon aria-hidden className="size-3" />}>
                        {n}
                      </Chip>
                    )
                  })}
                </div>
                <ul className="space-y-0.5">
                  {claims.map((c, i) => {
                    const cl = claimLook(c.status)
                    const Icon = cl.icon
                    return (
                      <li key={i}>
                        <Disclose
                          tone={cl.tone}
                          detail={c.why}
                          summary={
                            <>
                              <Icon
                                aria-hidden
                                className={cn(
                                  'mt-0.5 size-3.5 shrink-0',
                                  cl.tone === 'ok' && 'text-ok',
                                  cl.tone === 'bad' && 'text-bad',
                                  cl.tone === 'unknown' && 'text-unknown',
                                )}
                              />
                              <div className="min-w-0 flex-1">
                                <p className="text-xs leading-relaxed font-medium">{c.claim}</p>
                              </div>
                              <Chip tone={cl.tone} className="mt-0.5">
                                {cl.label}
                              </Chip>
                            </>
                          }
                        />
                      </li>
                    )
                  })}
                </ul>
              </div>
            ) : null}

            {verdict.component ? <ComponentCard component={verdict.component} onOpenKnowledge={onOpenKnowledge} /> : null}

            <div className="grid gap-2 sm:grid-cols-2">
              <ListBlock
                title="Gaps"
                icon={HelpCircle}
                tone="warn"
                items={verdict.gaps ?? []}
                empty="No gaps were flagged. Every claim had something to check it against."
              />
              <ListBlock
                title="Next checks"
                icon={Target}
                tone="neutral"
                items={verdict.nextChecks ?? []}
                empty="No follow-up checks were suggested."
              />
            </div>
          </>
        )}
    </PanelBody>
  )
}

function ComponentCard({
  component,
  onOpenKnowledge,
}: {
  component: IdentifiedComponent
  onOpenKnowledge: (id: string) => void
}) {
  const recognised = Boolean(component.id)
  const layer: ComponentLayer = component.layer

  return (
    <div className="rounded-md border border-border bg-well px-2.5 py-2">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-[0.625rem] font-semibold uppercase tracking-widest text-muted-foreground">Identified component</h3>
        <Chip tone={recognised ? 'ok' : 'unknown'}>{LAYER_LABEL[layer] ?? humanise(String(layer))}</Chip>
      </div>

      <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="text-sm font-medium">{component.displayName || component.name || 'Not identified'}</span>
        <span className="font-mono text-xs text-muted-foreground">
          {component.kind}
          {component.namespace ? ` · ${component.namespace}/` : ' · '}
          {component.name}
        </span>
      </div>

      {recognised ? (
        <button
          type="button"
          onClick={() => onOpenKnowledge(component.id as string)}
          className="mt-1.5 inline-flex items-center gap-1.5 rounded border border-border bg-card px-1.5 py-1 text-[0.6875rem] font-medium transition-colors hover:border-input focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <BookOpen aria-hidden className="size-3" />
          What we know about {component.displayName || component.name}
          <span className="font-mono text-muted-foreground">{component.id}</span>
        </button>
      ) : (
        <p className="mt-1 text-[0.6875rem] leading-relaxed text-muted-foreground">
          Nothing in the knowledge base matched this. The agents worked from Kubernetes facts alone, which is fine — it just means no
          product-specific failure modes were brought to bear.
        </p>
      )}

      {(component.matchWhy ?? []).length > 0 ? (
        <div className="mt-1.5 flex flex-wrap items-center gap-1">
          <span className="text-[0.625rem] text-muted-foreground">matched on</span>
          {(component.matchWhy ?? []).map((why, i) => (
            <Chip key={i} tone="neutral" mono className="max-w-[16rem]">
              {why}
            </Chip>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function ListBlock({
  title,
  icon: Icon,
  items,
  empty,
  tone,
}: {
  title: string
  icon: LucideIcon
  items: string[]
  empty: string
  tone: Tone
}) {
  return (
    <Well>
      <h3 className="flex items-center gap-1.5 text-[0.625rem] font-semibold uppercase tracking-widest text-muted-foreground">
        <Icon aria-hidden className={cn('size-3', tone === 'warn' ? 'text-warn' : '')} />
        {title}
        {items.length > 0 ? <span className="tabular font-normal">{items.length}</span> : null}
      </h3>
      {items.length === 0 ? (
        <p className="mt-1 text-[0.6875rem] leading-relaxed text-muted-foreground">{empty}</p>
      ) : (
        <ul className="mt-1 space-y-1">
          {items.map((item, i) => (
            <li key={i} className="flex gap-1.5 text-xs leading-relaxed">
              <span className="text-muted-foreground">·</span>
              <span className="min-w-0">{item}</span>
            </li>
          ))}
        </ul>
      )}
    </Well>
  )
}
