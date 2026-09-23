import type { ReactNode } from 'react'
import { cn } from 'cn'
import { CountingNumber } from '@/components/animate-ui/primitives/texts/counting-number'
import { HelpCircle } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { percent } from '@/lib/format'

export type Tone = 'ok' | 'warn' | 'bad' | 'unknown' | 'neutral' | 'accent'

const TONE_TEXT: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  bad: 'text-bad',
  unknown: 'text-unknown',
  neutral: 'text-muted-foreground',
  accent: 'text-foreground',
}

const TONE_CHIP: Record<Tone, string> = {
  ok: 'border-ok/30 bg-ok/10 text-ok',
  warn: 'border-warn/30 bg-warn/10 text-warn',
  bad: 'border-bad/30 bg-bad/10 text-bad',
  unknown: 'border-unknown/30 bg-unknown/10 text-unknown',
  neutral: 'border-border bg-muted text-muted-foreground',
  accent: 'border-border bg-well text-foreground',
}

const TONE_DOT: Record<Tone, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  bad: 'bg-bad',
  unknown: 'bg-unknown',
  neutral: 'bg-muted-foreground',
  accent: 'bg-foreground',
}

/** Small status dot. `pulse` marks something genuinely in flight. */
export function Dot({ tone, pulse, className }: { tone: Tone; pulse?: boolean; className?: string }) {
  return (
    <span className={cn('relative inline-flex size-2 shrink-0', className)}>
      {pulse ? <span className={cn('absolute inline-flex size-full animate-ping rounded-full opacity-60', TONE_DOT[tone])} /> : null}
      <span className={cn('relative inline-flex size-2 rounded-full', TONE_DOT[tone])} />
    </span>
  )
}

/** Dense, bordered status chip. The workhorse label of the whole UI. */
export function Chip({
  tone = 'neutral',
  icon,
  children,
  className,
  mono,
}: {
  tone?: Tone
  icon?: ReactNode
  children: ReactNode
  className?: string
  mono?: boolean
}) {
  return (
    <span
      className={cn(
        'inline-flex h-5 max-w-full shrink-0 items-center gap-1 overflow-hidden rounded border px-1.5 text-[0.6875rem] font-medium whitespace-nowrap',
        mono && 'font-mono',
        TONE_CHIP[tone],
        className,
      )}
    >
      {icon}
      <span className="truncate">{children}</span>
    </span>
  )
}

/**
 * A value we genuinely do not know.
 *
 * Showing 0 here would be a lie, and this product cares about the difference
 * between "nothing is wrong" and "we could not look".
 */
export function Unknown({ why, children = 'unknown' }: { why: string; children?: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex cursor-help items-center gap-1 text-unknown decoration-dotted underline-offset-4 [text-decoration-line:underline]">
          <HelpCircle aria-hidden className="size-3" />
          <span className="text-xs">{children}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-80">{why}</TooltipContent>
    </Tooltip>
  )
}

/**
 * Confidence as a bar plus the number. Confidence is a model's own opinion of
 * itself, so it is labelled as such rather than presented as a measurement.
 */
export function Confidence({ value, className }: { value: number | null | undefined; className?: string }) {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return <Unknown why="The agent did not report a confidence for this result.">confidence unknown</Unknown>
  }
  const pct = Math.max(0, Math.min(1, value))
  const tone: Tone = pct >= 0.75 ? 'ok' : pct >= 0.5 ? 'warn' : 'bad'
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn('inline-flex cursor-help items-center gap-2', className)}>
          <span className="h-1.5 w-16 overflow-hidden rounded-full bg-muted">
            <span className={cn('block h-full rounded-full transition-[width] duration-500', TONE_DOT[tone])} style={{ width: `${pct * 100}%` }} />
          </span>
          <span className={cn('tabular text-xs font-medium', TONE_TEXT[tone])}>{percent(pct)}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>The agent's own confidence in this result, not a measurement.</TooltipContent>
    </Tooltip>
  )
}

/** Budget / usage meter. Turns warm as the ceiling approaches. */
export function Meter({
  label,
  value,
  max,
  format = (n: number) => String(n),
}: {
  label: string
  value: number
  max: number
  format?: (n: number) => string
}) {
  const ratio = max > 0 ? Math.min(1, value / max) : 0
  const tone: Tone = ratio >= 1 ? 'bad' : ratio >= 0.8 ? 'warn' : 'neutral'
  // Only animate a raw count. Token counts are shown compactly (118.4k), and
  // rolling that digit by digit would be unreadable as well as wrong.
  const animated = format === undefined || format(value) === String(value)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex cursor-help items-center gap-1.5">
          <span className="text-[0.6875rem] text-muted-foreground">{label}</span>
          <span className="h-1 w-10 overflow-hidden rounded-full bg-muted">
            <span
              className={cn('block h-full rounded-full transition-[width] duration-500 ease-out', TONE_DOT[tone])}
              style={{ width: `${ratio * 100}%` }}
            />
          </span>
          <span className={cn('tabular text-[0.6875rem] font-medium', TONE_TEXT[tone])}>
            {animated ? (
              <CountingNumber number={value} transition={{ stiffness: 90, damping: 22 }} />
            ) : (
              format(value)
            )}
            /{format(max)}
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent>
        {label}: {format(value)} of a {format(max)} ceiling. The run stops rather than overspend.
      </TooltipContent>
    </Tooltip>
  )
}
