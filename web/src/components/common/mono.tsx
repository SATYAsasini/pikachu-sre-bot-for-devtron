import type { ReactNode } from 'react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from 'cn'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

/**
 * A machine-generated value: run id, fingerprint, PromQL, metric name.
 * Always monospace, always truncated rather than wrapped, always disclosable
 * in full on hover — long hashes must never push a layout around.
 */
/**
 * A monospace value inside a sentence.
 *
 * Mono is block-and-truncate because it exists for table cells; using it in
 * prose pushes every reference onto its own line. This is the inline one.
 */
export function Code({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <code
      className={cn(
        'rounded border border-border bg-well px-1 py-px font-mono text-[0.9em] whitespace-nowrap',
        className,
      )}
    >
      {children}
    </code>
  )
}

export function Mono({
  value,
  className,
  title,
}: {
  value: string
  className?: string
  /** Extra line above the value in the tooltip. */
  title?: string
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn('block max-w-full truncate font-mono text-xs text-muted-foreground', className)}>{value}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-[min(32rem,90vw)]">
        {title ? <div className="mb-0.5 font-medium">{title}</div> : null}
        <span className="font-mono break-all">{value}</span>
      </TooltipContent>
    </Tooltip>
  )
}

/**
 * Monospace value with a copy affordance. The whole chip is the button, so the
 * hit target matches what the eye reads as one thing.
 */
export function CopyValue({
  value,
  display,
  label = 'value',
  className,
}: {
  value: string
  /** What is shown; defaults to the value itself (e.g. a shortened id). */
  display?: string
  /** Used in the toast: "Run id copied". */
  label?: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const copy = useCallback(() => {
    void (async () => {
      try {
        await navigator.clipboard.writeText(value)
        setCopied(true)
        toast.success(`${label.charAt(0).toUpperCase()}${label.slice(1)} copied`, {
          description: value.length > 64 ? `${value.slice(0, 61)}…` : value,
        })
        window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => setCopied(false), 1400)
      } catch {
        toast.error('Clipboard said no', { description: 'Your browser blocked it. The text is still selectable.' })
      }
    })()
  }, [value, label])

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={copy}
          className={cn(
            'group inline-flex h-6 max-w-full items-center gap-1.5 rounded border border-border bg-well px-1.5 font-mono text-xs text-muted-foreground transition-colors hover:border-input hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
            className,
          )}
        >
          <span className="truncate">{display ?? value}</span>
          {copied ? (
            <Check aria-hidden className="size-3 shrink-0 text-ok" />
          ) : (
            <Copy aria-hidden className="size-3 shrink-0 opacity-50 transition-opacity group-hover:opacity-100" />
          )}
          <span className="sr-only">Copy {label}</span>
        </button>
      </TooltipTrigger>
      <TooltipContent className="max-w-[min(32rem,90vw)]">
        <div className="font-mono break-all">{value}</div>
        <div className="mt-0.5 opacity-70">Click to copy</div>
      </TooltipContent>
    </Tooltip>
  )
}

/** Truncating text with the full string on hover. For summaries and asks. */
export function Truncated({ text, className, lines = 1 }: { text: string; className?: string; lines?: 1 | 2 }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className={cn(lines === 1 ? 'block truncate' : 'line-clamp-2', className)}>{text}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-[min(32rem,90vw)]">{text}</TooltipContent>
    </Tooltip>
  )
}
