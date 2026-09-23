import type { ReactNode } from 'react'
import { cn } from 'cn'
import type { LucideIcon } from 'lucide-react'

/**
 * Empty states earn their space by saying something. Never a shrug icon and
 * the word "No data".
 */
export function EmptyState({
  icon: Icon,
  title,
  line,
  action,
  tone = 'neutral',
  className,
}: {
  icon: LucideIcon
  title: string
  /** The witty, honest second line. Say what happened, not just that nothing did. */
  line: ReactNode
  action?: ReactNode
  tone?: 'neutral' | 'ok' | 'warn'
  className?: string
}) {
  const ring = tone === 'ok' ? 'border-ok/30 text-ok' : tone === 'warn' ? 'border-warn/30 text-warn' : 'border-border text-muted-foreground'
  return (
    <div className={cn('flex flex-col items-center justify-center gap-2 px-4 py-10 text-center', className)}>
      <span className={cn('flex size-9 items-center justify-center rounded-lg border', ring)}>
        <Icon aria-hidden className="size-4" />
      </span>
      <div className="text-sm font-medium">{title}</div>
      <p className="max-w-sm text-xs leading-relaxed text-muted-foreground">{line}</p>
      {action ? <div className="mt-1.5">{action}</div> : null}
    </div>
  )
}
