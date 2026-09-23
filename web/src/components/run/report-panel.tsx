import { BookOpen, ChevronDown, CircleAlert, FlaskConical, ShieldCheck, Stethoscope, Undo2, Wrench } from 'lucide-react'
import { useState } from 'react'
import { cn } from 'cn'
import { PanelBody, Well } from '@/components/common/panel'
import { Chip, type Tone } from '@/components/common/status'
import { Markdown } from '@/components/common/markdown'
import { TextSkeleton } from '@/components/common/skeletons'
import { EmptyState } from '@/components/common/empty-state'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { StageState } from '@/lib/run-derive'
import type { Evidence, Remediation, Report, RiskLevel } from '@/lib/types'

const RISK_TONE: Record<string, Tone> = { low: 'ok', medium: 'warn', high: 'bad' }

const RISK_WHY: Record<string, string> = {
  low: 'Reversible in place, no data movement, no restart of anything you cannot restart.',
  medium: 'Touches capacity or configuration that other things depend on. Do it deliberately.',
  high: 'Structural. Has a blast radius beyond the component in question.',
}

function riskTone(risk: RiskLevel): Tone {
  return RISK_TONE[String(risk).toLowerCase()] ?? 'unknown'
}

/**
 * Output ③ — the SRE deep-dive.
 *
 * Remediation is ranked, and every option carries its own risk, how to verify
 * it worked and how to undo it. An option without those three is not advice,
 * it is a guess, so the card renders the absence honestly rather than hiding
 * the empty row.
 */
export function ReportPanel({
  report,
  state,
  onOpenKnowledge,
}: {
  report: Report | null | undefined
  state: StageState
  onOpenKnowledge: (componentId: string) => void
}) {
  const remediation = report?.remediation ?? []
  const evidence = report?.evidence ?? []

  return (
      <PanelBody className="space-y-3">
        {!report ? (
          state === 'running' ? (
            <div className="space-y-2 py-1">
              <p className="text-xs text-muted-foreground">The SRE agent is gathering evidence. Remediation appears once it has something to stand on.</p>
              <TextSkeleton lines={5} />
            </div>
          ) : state === 'skipped' || state === 'failed' ? (
            <EmptyState
              icon={Stethoscope}
              title="No deep-dive was produced"
              line="The run stopped before the SRE agent finished. Whatever is in the two panels above is everything this run reached."
            />
          ) : (
            <div className="space-y-2 py-1">
              <p className="text-xs text-muted-foreground">Queued behind the verdict.</p>
              <TextSkeleton lines={3} />
            </div>
          )
        ) : (
          <>
            <div>
              <h3 className="text-[0.625rem] font-semibold uppercase tracking-widest text-muted-foreground">
                {report.agrees ? 'Root cause' : 'Corrected root cause'}
              </h3>
              <div className="mt-1 rounded-md border border-border bg-well px-2.5 py-2">
                <Markdown tight>{report.correctedRootCause || '_No root cause was stated._'}</Markdown>
              </div>
            </div>

            <div>
              <h3 className="mb-1.5 flex items-center gap-1.5 text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase">
                <Wrench aria-hidden className="size-3" />
                Remediation
                {remediation.length > 0 ? <span className="tabular font-normal">{remediation.length} ranked</span> : null}
              </h3>
              {remediation.length === 0 ? (
                <Well className="text-muted-foreground">
                  No remediation was proposed. Either the root cause sits outside what this agent can act on, or it is not confident enough to
                  recommend a change — which is the correct answer more often than it looks.
                </Well>
              ) : (
                <ol className="space-y-2">
                  {remediation.map((r, i) => (
                    <RemediationCard key={i} rank={i + 1} remediation={r} defaultOpen={i === 0} />
                  ))}
                </ol>
              )}
            </div>

            {/* Everything that is not the answer or the action.
                It is all true and all cited, and none of it is what someone
                mid-incident opens this page for, so it is one line until
                asked for. */}
            <Working report={report} evidence={evidence} onOpenKnowledge={onOpenKnowledge} />
          </>
        )}
    </PanelBody>
  )
}

/**
 * The working, shut.
 *
 * The deep-dive used to render the root cause, ten paragraphs of evidence,
 * the remediation, the SRE notes and the unknowns, in that order — so the
 * action you came for sat in the middle of a wall and the notes nobody reads
 * came last. Now the panel is two things: what is wrong, and what to do. The
 * rest is behind one control.
 */
function Working({
  report,
  evidence,
  onOpenKnowledge,
}: {
  report: Report
  evidence: Evidence[]
  onOpenKnowledge: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const unknowns = report.unknowns ?? []
  if (evidence.length === 0 && unknowns.length === 0 && !report.sreNotes) return null

  const parts = [
    evidence.length > 0 ? `${evidence.length} evidence` : null,
    unknowns.length > 0 ? `${unknowns.length} unknown` : null,
    report.sreNotes ? 'notes' : null,
  ].filter(Boolean)

  return (
    <div className="border-t border-border pt-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="inline-flex items-center gap-1 rounded text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <ChevronDown aria-hidden className={cn('size-3 transition-transform', !open && '-rotate-90')} />
        Working
        <span className="font-normal normal-case">· {parts.join(' · ')}</span>
      </button>

      {open ? (
        <div className="mt-2 space-y-3">
          {evidence.length > 0 ? <EvidenceList evidence={evidence} onOpenKnowledge={onOpenKnowledge} /> : null}

          {report.sreNotes ? (
            <Well>
              <h3 className="flex items-center gap-1.5 text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase">
                <ShieldCheck aria-hidden className="size-3" />
                SRE notes
              </h3>
              <div className="mt-1">
                <Markdown tight>{report.sreNotes}</Markdown>
              </div>
            </Well>
          ) : null}

          {unknowns.length > 0 ? (
            <Well className="border-unknown/30">
              <h3 className="flex items-center gap-1.5 text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase">
                <CircleAlert aria-hidden className="size-3 text-unknown" />
                Still unknown
                <span className="tabular font-normal">{unknowns.length}</span>
              </h3>
              <ul className="mt-1 space-y-1">
                {unknowns.map((u, i) => (
                  <li key={i} className="flex gap-1.5 text-xs leading-relaxed">
                    <span className="text-unknown">·</span>
                    <span className="min-w-0">{u}</span>
                  </li>
                ))}
              </ul>
            </Well>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/**
 * The evidence, shut.
 *
 * Every row is a paragraph, and ten of them between the root cause and the
 * remediation is the single biggest wall on this page — you scroll past all of
 * it to reach the thing you came to do. It is the audit trail, which matters
 * when you are checking the conclusion and not when you are acting on it, so
 * it is one line until asked for.
 */
function EvidenceList({ evidence, onOpenKnowledge }: { evidence: Evidence[]; onOpenKnowledge: (id: string) => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <h3>
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="mb-1.5 flex items-center gap-1.5 rounded text-[0.625rem] font-semibold tracking-widest text-muted-foreground uppercase transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <ChevronDown aria-hidden className={cn('size-3 transition-transform', !open && '-rotate-90')} />
          <FlaskConical aria-hidden className="size-3" />
          Evidence
          <span className="tabular font-normal">{evidence.length}</span>
        </button>
      </h3>
      <ul className={cn('divide-y divide-border rounded-md border border-border', !open && 'hidden')}>
        {evidence.map((e, i) => {
          const fromKnowledge = e.source === 'knowledge'
          return (
            <li key={i} className="flex items-start gap-2 px-2.5 py-1.5">
              <Chip tone="neutral" mono className="mt-0.5 shrink-0">
                {e.source}
              </Chip>
              <span className="min-w-0 flex-1 text-xs leading-relaxed">{e.detail}</span>
              {e.ref ? (
                fromKnowledge ? (
                  <button
                    type="button"
                    onClick={() => onOpenKnowledge(e.ref)}
                    className="mt-0.5 inline-flex shrink-0 items-center gap-1 rounded border border-border bg-well px-1 font-mono text-[0.625rem] text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                  >
                    <BookOpen aria-hidden className="size-2.5" />
                    {e.ref}
                  </button>
                ) : (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="mt-0.5 shrink-0 cursor-help rounded border border-border bg-well px-1 font-mono text-[0.625rem] text-muted-foreground">
                        {e.ref}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>Ledger reference. Find it by sequence in the timeline.</TooltipContent>
                  </Tooltip>
                )
              ) : null}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function RemediationCard({ rank, remediation, defaultOpen }: { rank: number; remediation: Remediation; defaultOpen?: boolean }) {
  const tone = riskTone(remediation.risk)
  const [open, setOpen] = useState(Boolean(defaultOpen))

  // The action and its risk are always visible, because that is the decision.
  // Verification and rollback are the execution detail, and they only matter
  // once you have chosen this option — so they wait until you open it.
  return (
    <li
      className={cn(
        'overflow-hidden rounded-md border transition-colors',
        open ? 'border-border bg-card' : 'border-border/60 bg-card hover:border-border hover:bg-well/60',
      )}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-start gap-2 px-2.5 py-2 text-left focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <span className="tabular mt-0.5 flex size-5 shrink-0 items-center justify-center rounded border border-border bg-well text-[0.625rem] font-semibold text-muted-foreground">
          {rank}
        </span>
        <div className="min-w-0 flex-1">
          <Markdown tight className="font-medium">
            {remediation.action}
          </Markdown>
          {remediation.why ? (
            <p className={cn('mt-0.5 text-xs leading-relaxed text-muted-foreground', !open && 'line-clamp-1')}>
              {remediation.why}
            </p>
          ) : null}
        </div>
        <span className="mt-0.5 flex shrink-0 items-center gap-1.5">
          <Chip tone={tone}>risk: {remediation.risk || 'unrated'}</Chip>
          <ChevronDown
            aria-hidden
            className={cn('size-3.5 text-muted-foreground transition-transform', open && 'rotate-180')}
          />
        </span>
      </button>

      <div
        className={cn(
          'grid transition-[grid-template-rows] duration-200 ease-out',
          open ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]',
        )}
      >
        <div className="overflow-hidden">
          <dl className="grid gap-px border-t border-border bg-border sm:grid-cols-2">
            <div className="bg-card px-2.5 py-1.5">
              <dt className="flex items-center gap-1 text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">
                <ShieldCheck aria-hidden className="size-3 text-ok" />
                How to verify
              </dt>
              <dd className="mt-0.5 text-xs leading-relaxed">
                {remediation.verify || <span className="text-unknown">No verification step was given — do not apply this blind.</span>}
              </dd>
            </div>
            <div className="bg-card px-2.5 py-1.5">
              <dt className="flex items-center gap-1 text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">
                <Undo2 aria-hidden className="size-3 text-warn" />
                How to roll back
              </dt>
              <dd className="mt-0.5 text-xs leading-relaxed">
                {remediation.rollback || <span className="text-unknown">No rollback was given. Assume this one is not trivially reversible.</span>}
              </dd>
            </div>
            <div className="bg-card px-2.5 py-1.5 sm:col-span-2">
              <dt className="text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">Why this risk level</dt>
              <dd className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
                {RISK_WHY[String(remediation.risk).toLowerCase()] ?? 'No risk level was given for this option.'}
              </dd>
            </div>
          </dl>
        </div>
      </div>
    </li>
  )
}
