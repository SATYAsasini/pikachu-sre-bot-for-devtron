import { useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { ArrowRight, CircleAlert, CircleCheck, CircleHelp, ShieldCheck, Undo2 } from 'lucide-react'
import { cn } from 'cn'
import { Markdown } from '@/components/common/markdown'
import { Chip, Confidence } from '@/components/common/status'
import { Heading, Text } from '@/components/common/text'
import type { Report, Verdict } from '@/lib/types'

/**
 * The answer, before the reasoning.
 *
 * A finished run produces a corrected root cause, five ranked remediations,
 * evidence, notes and unknowns — several screens of genuinely good material
 * that nobody reads, because the page opens with the first-pass analysis and
 * makes you scroll to find out what we actually concluded.
 *
 * So: one sentence of cause, one recommended action, and a confidence. Every
 * other word on the page is available one click away and closed by default.
 */
export function Conclusion({
  verdict,
  report,
  onJump,
}: {
  verdict: Verdict | null
  report: Report | null
  onJump: (stage: 'intelligence' | 'verdict' | 'report') => void
}) {
  const still = useReducedMotion()
  const [openStep, setOpenStep] = useState(false)

  if (!report && !verdict) return null

  const cause = report?.correctedRootCause?.trim() || ''
  const first = report?.remediation?.[0]
  const agrees = report?.agrees
  const nothing = !cause && !first

  const tone = nothing ? 'unknown' : agrees === false ? 'warn' : 'ok'
  const Icon = nothing ? CircleHelp : agrees === false ? CircleAlert : CircleCheck

  return (
    <motion.section
      initial={still ? false : { opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: [0.16, 1, 0.3, 1] }}
      aria-labelledby="conclusion-heading"
      className={cn(
        'overflow-hidden rounded-xl border-2 bg-card shadow-card',
        tone === 'ok' && 'border-ok/30',
        tone === 'warn' && 'border-warn/35',
        tone === 'unknown' && 'border-border',
      )}
    >
      <div className="flex items-start gap-2.5 px-3 py-2.5">
        <Icon
          aria-hidden
          className={cn(
            'mt-0.5 size-4 shrink-0',
            tone === 'ok' && 'text-ok',
            tone === 'warn' && 'text-warn',
            tone === 'unknown' && 'text-unknown',
          )}
        />
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Heading level={1} id="conclusion-heading" className="text-sm">
              {nothing ? 'No conclusion' : agrees === false ? 'The first pass was wrong' : 'Confirmed'}
            </Heading>
            {report?.confidence !== undefined && <Confidence value={report.confidence} />}
          </div>

          {nothing ? (
            <Text tone="muted">
              Nothing could be established. The detail below says why, which is worth reading before trusting any
              of it.
            </Text>
          ) : (
            <Markdown tight className="text-xs leading-relaxed">
              {cause || verdict?.claims?.[0]?.claim || ''}
            </Markdown>
          )}
        </div>
      </div>

      {/* The single next action. Four more are ranked below; this is the one
          you would do first. */}
      {first && (
        <div className="border-t border-border bg-well/60">
          <button
            type="button"
            onClick={() => setOpenStep((v) => !v)}
            aria-expanded={openStep}
            className="flex w-full items-start gap-2.5 px-3 py-2 text-left focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <span className="mt-px shrink-0">
              <Chip tone="accent">do first</Chip>
            </span>
            <span className="min-w-0 flex-1">
              <Markdown tight className="text-xs font-medium">
                {first.action}
              </Markdown>
            </span>
            <Chip tone={first.risk === 'high' ? 'bad' : first.risk === 'medium' ? 'warn' : 'ok'} className="mt-px shrink-0">
              {first.risk || 'unrated'}
            </Chip>
          </button>

          <AnimatePresence initial={false}>
            {openStep && (
              <motion.div
                initial={still ? false : { height: 0, opacity: 0 }}
                animate={{ height: 'auto', opacity: 1 }}
                exit={still ? undefined : { height: 0, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.16, 1, 0.3, 1] }}
              >
                <dl className="grid gap-px border-t border-border bg-border sm:grid-cols-2">
                  <div className="bg-card px-3 py-2">
                    <dt className="flex items-center gap-1 text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">
                      <ShieldCheck aria-hidden className="size-3 text-ok" /> How to verify
                    </dt>
                    <dd className="mt-0.5 text-xs leading-relaxed">{first.verify || 'Not given.'}</dd>
                  </div>
                  <div className="bg-card px-3 py-2">
                    <dt className="flex items-center gap-1 text-[0.625rem] font-medium tracking-wider text-muted-foreground uppercase">
                      <Undo2 aria-hidden className="size-3 text-warn" /> How to roll back
                    </dt>
                    <dd className="mt-0.5 text-xs leading-relaxed">{first.rollback || 'Not given.'}</dd>
                  </div>
                </dl>
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      )}

      {/* Where the reasoning lives, for anyone who wants it. */}
      <nav className="flex flex-wrap items-center gap-1 border-t border-border px-3 py-1.5" aria-label="Jump to reasoning">
        <Text tone="fine" as="span" className="mr-1">
          Working:
        </Text>
        {(
          [
            ['intelligence', 'first pass'],
            ['verdict', `${verdict?.claims?.length ?? 0} claims`],
            ['report', `${report?.remediation?.length ?? 0} steps · ${report?.evidence?.length ?? 0} evidence`],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            onClick={() => onJump(key)}
            className="inline-flex items-center gap-1 rounded border border-transparent px-1.5 py-0.5 text-[0.6875rem] text-muted-foreground transition-colors hover:border-border hover:bg-card hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            {label}
            <ArrowRight aria-hidden className="size-2.5" />
          </button>
        ))}
      </nav>
    </motion.section>
  )
}
