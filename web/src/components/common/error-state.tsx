import { AlertTriangle, RotateCw } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Chip } from '@/components/common/status'
import { errorCode, errorMessage } from '@/lib/api'

/**
 * Renders whatever the client threw, including the contract's
 * `{error:{code,message}}` envelope. The code is shown, because "which code"
 * is the first thing anyone debugging this asks.
 */
export function ErrorState({
  error,
  onRetry,
  compact,
  className,
}: {
  error: unknown
  onRetry?: () => void
  compact?: boolean
  className?: string
}) {
  const code = errorCode(error)
  const message = errorMessage(error)

  if (compact) {
    return (
      <div className={cn('flex items-center gap-2 px-3 py-2 text-xs text-bad', className)}>
        <AlertTriangle aria-hidden className="size-3.5 shrink-0" />
        <span className="min-w-0 truncate">{message}</span>
        {onRetry ? (
          <Button size="xs" variant="ghost" onClick={onRetry} className="ml-auto shrink-0">
            <RotateCw aria-hidden />
            Retry
          </Button>
        ) : null}
      </div>
    )
  }

  return (
    <div className={cn('flex flex-col items-center gap-2.5 px-4 py-9 text-center', className)}>
      <span className="flex size-9 items-center justify-center rounded-lg border border-bad/30 text-bad">
        <AlertTriangle aria-hidden className="size-4" />
      </span>
      <div className="text-sm font-medium">That request did not land</div>
      <p className="max-w-md text-xs leading-relaxed text-muted-foreground">{message}</p>
      {code ? <Chip tone="bad" mono>{code}</Chip> : null}
      {onRetry ? (
        <Button size="sm" variant="outline" onClick={onRetry} className="mt-1">
          <RotateCw aria-hidden />
          Try again
        </Button>
      ) : null}
    </div>
  )
}
