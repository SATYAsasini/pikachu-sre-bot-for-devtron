import type { ReactNode } from 'react'
import { cn } from 'cn'

/**
 * The one container in the app. A bordered surface with a compact header.
 * There is no Card-with-huge-padding here on purpose: density is the point.
 */
export function Panel({ className, children, ...rest }: React.ComponentProps<'section'>) {
  return (
    <section
      className={cn('overflow-hidden rounded-xl border border-border bg-card shadow-card', className)}
      {...rest}
    >
      {children}
    </section>
  )
}

export function PanelHeader({
  title,
  eyebrow,
  description,
  actions,
  icon,
  className,
}: {
  title: ReactNode
  /** Tiny uppercase kicker, e.g. the stage number. */
  eyebrow?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  icon?: ReactNode
  className?: string
}) {
  return (
    <header className={cn('flex items-start justify-between gap-3 border-b border-border px-3 py-2', className)}>
      <div className="flex min-w-0 items-start gap-2">
        {icon ? <span className="mt-0.5 shrink-0 text-muted-foreground">{icon}</span> : null}
        <div className="min-w-0">
          {eyebrow ? <div className="text-[0.625rem] font-semibold uppercase tracking-widest text-muted-foreground">{eyebrow}</div> : null}
          <h2 className="truncate text-sm font-semibold tracking-tight">{title}</h2>
          {description ? <p className="mt-0.5 text-xs text-muted-foreground">{description}</p> : null}
        </div>
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-1.5">{actions}</div> : null}
    </header>
  )
}

export function PanelBody({ className, children, ...rest }: React.ComponentProps<'div'>) {
  return (
    <div className={cn('px-3 py-2.5', className)} {...rest}>
      {children}
    </div>
  )
}

/** Label + value pair, used everywhere in headers and detail grids. */
export function Field({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('min-w-0', className)}>
      <div className="text-[0.625rem] font-medium uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className="mt-0.5 min-w-0 text-xs">{children}</div>
    </div>
  )
}

/** A recessed well for quoted facts: evidence detail, verify/rollback steps. */
export function Well({ className, children, ...rest }: React.ComponentProps<'div'>) {
  return (
    <div className={cn('rounded-lg border border-border bg-well px-2.5 py-2 text-xs', className)} {...rest}>
      {children}
    </div>
  )
}
