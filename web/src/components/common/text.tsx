import type { ElementType, ReactNode } from 'react'
import { cn } from 'cn'

/**
 * Centralised typography.
 *
 * Every size, weight and colour in the product comes from here rather than
 * from ad-hoc class strings at each call site. That is what makes a global
 * change — tightening density, swapping the display face — a one-file edit
 * instead of a grep across forty components.
 */

const HEADING = {
  1: 'font-display text-base font-semibold tracking-tight',
  2: 'font-display text-sm font-semibold tracking-tight',
  3: 'font-display text-xs font-semibold tracking-tight',
} as const

/** A real heading element. `level` sets both the tag and the scale, so the
 *  document outline can never drift from what the page looks like. */
export function Heading({
  level = 2,
  children,
  className,
  id,
}: {
  level?: 1 | 2 | 3
  children: ReactNode
  className?: string
  id?: string
}) {
  const Tag = (`h${level}`) as ElementType
  return (
    <Tag id={id} className={cn(HEADING[level], className)}>
      {children}
    </Tag>
  )
}

const TEXT = {
  body: 'text-xs leading-relaxed text-foreground',
  muted: 'text-xs leading-relaxed text-muted-foreground',
  fine: 'text-[0.6875rem] leading-relaxed text-muted-foreground',
  label: 'text-[0.625rem] font-medium tracking-widest text-muted-foreground uppercase',
  data: 'font-mono text-xs text-foreground',
} as const

export type TextTone = keyof typeof TEXT

export function Text({
  tone = 'body',
  as = 'p',
  children,
  className,
}: {
  tone?: TextTone
  as?: ElementType
  children: ReactNode
  className?: string
}) {
  const Tag = as
  return <Tag className={cn(TEXT[tone], className)}>{children}</Tag>
}

/** Visible only to screen readers. For the per-page h1 where the design
 *  already carries the title visually in the header. */
export function SrOnly({ children, as = 'span' }: { children: ReactNode; as?: ElementType }) {
  const Tag = as
  return <Tag className="sr-only">{children}</Tag>
}
