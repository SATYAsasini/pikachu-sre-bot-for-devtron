import { cn } from 'cn'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * Skeletons, not spinners. Each one traces the shape of what is loading, so
 * the layout never jumps when the data lands.
 */

export function TextSkeleton({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <div className={cn('space-y-2', className)}>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className="h-3" style={{ width: `${[100, 92, 80, 96, 64][i % 5]}%` }} />
      ))}
    </div>
  )
}

export function RowSkeleton({ rows = 5, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('divide-y divide-border', className)}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-3 px-3 py-2.5">
          <Skeleton className="size-2 rounded-full" />
          <Skeleton className="h-3 w-40" />
          <Skeleton className="h-3 flex-1" />
          <Skeleton className="h-3 w-16" />
        </div>
      ))}
    </div>
  )
}

export function PanelSkeleton({ lines = 4, className }: { lines?: number; className?: string }) {
  return (
    <div className={cn('rounded-lg border border-border bg-card', className)}>
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <Skeleton className="h-3.5 w-44" />
        <Skeleton className="h-4 w-16 rounded-full" />
      </div>
      <div className="px-3 py-3">
        <TextSkeleton lines={lines} />
      </div>
    </div>
  )
}

export function TimelineSkeleton({ rows = 7 }: { rows?: number }) {
  return (
    <div className="space-y-2 px-3 py-2.5">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-2">
          <Skeleton className="h-2.5 w-12" />
          <Skeleton className="size-1.5 rounded-full" />
          <Skeleton className="h-2.5 flex-1" style={{ maxWidth: `${[80, 60, 95, 70, 88, 55, 76][i % 7]}%` }} />
        </div>
      ))}
    </div>
  )
}
