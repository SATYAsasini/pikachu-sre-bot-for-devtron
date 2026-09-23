import type { ReactNode } from 'react'
import { Search, X } from 'lucide-react'
import { cn } from 'cn'
import { Text } from '@/components/common/text'

/**
 * The list toolbar, in the shape cloud consoles settled on.
 *
 * Every dense-data console — GCP, AWS, the Devtron dashboard itself — converged
 * on the same arrangement for a filtered list, and it is worth copying rather
 * than reinventing:
 *
 * 1. A **visible label** saying what the control does. Guidelines call this
 *    High severity and it is the single cheapest fix here: "Filter by" removes
 *    the entire question of what those three words are for.
 * 2. The **scope selector first**, then the field, reading left to right as a
 *    sentence: filter by *resource* matching *pgvector*.
 * 3. One **bordered field** with a leading icon, so it is unmistakably an
 *    input, and a clear button that appears only when there is something to
 *    clear.
 * 4. A **live count** of what survived, because a filter with no feedback is a
 *    filter you do not trust.
 *
 * None of this is decoration. The previous toolbar failed three separate
 * guideline checks — input affordance, compact control semantics and input
 * labels — and the result was a control nobody could see.
 */
export function FilterBar({
  label = 'Filter by',
  scope,
  value,
  onChange,
  placeholder,
  hint,
  count,
  total,
  noun = 'result',
  className,
}: {
  /** Visible, not just an aria-label. */
  label?: string
  /** The scope selector, e.g. a <Segmented>. */
  scope?: ReactNode
  value: string
  onChange: (next: string) => void
  placeholder?: string
  /** One line under the bar explaining what the current scope searches. */
  hint?: string
  /** How many rows survived the filter. */
  count?: number
  total?: number
  noun?: string
  className?: string
}) {
  const filtering = value.trim() !== ''

  return (
    <div className={cn('border-b border-border px-3 py-2.5', className)}>
      <div className="flex flex-wrap items-center gap-2">
        <Text tone="label" as="span" className="shrink-0">
          {label}
        </Text>

        {scope}

        <div className="relative min-w-[14rem] flex-1">
          <Search
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <input
            type="search"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholder}
            aria-label={placeholder ?? 'Filter'}
            className={cn(
              'h-8 w-full rounded-md border border-border bg-card pr-8 pl-8 text-xs',
              'placeholder:text-muted-foreground/70',
              'transition-[border-color,box-shadow]',
              'focus:border-accent-strong/50 focus:ring-[3px] focus:ring-ring/30 focus:outline-none',
              // Safari draws its own clear button on type=search and it fights
              // ours.
              '[&::-webkit-search-cancel-button]:appearance-none',
            )}
          />
          {filtering ? (
            <button
              type="button"
              onClick={() => onChange('')}
              aria-label="Clear the filter"
              className="absolute top-1/2 right-1.5 grid size-5 -translate-y-1/2 place-items-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <X aria-hidden className="size-3" />
            </button>
          ) : null}
        </div>

        {/* A filter with no feedback is a filter nobody trusts. */}
        {count !== undefined ? (
          <Text tone="fine" as="span" className="tabular shrink-0">
            {filtering && total !== undefined
              ? `${count} of ${total}`
              : `${count} ${noun}${count === 1 ? '' : 's'}`}
          </Text>
        ) : null}
      </div>

      {hint ? (
        <Text tone="fine" className="mt-1.5">
          {hint}
        </Text>
      ) : null}
    </div>
  )
}
