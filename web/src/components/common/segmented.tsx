import { cn } from 'cn'

/**
 * A segmented control that looks like one.
 *
 * The previous version gave the selected segment a 12%-opacity tint and the
 * unselected ones nothing at all, so at a glance it read as three words of
 * body text rather than as a control — you could not tell it was clickable,
 * which of the three was active, or what it changed.
 *
 * Three rules from the guidelines it was breaking, all at once:
 *
 * - *Compact control semantics* (critical): an interactive chip needs a native
 *   role, an accessible name, and a selected state that visibly matches the
 *   label. `aria-checked` was right; the pixels were not.
 * - *Input affordance*: controls must look interactive. Borderless segments on
 *   a borderless track look like prose.
 * - *Input labels*: every input needs a visible label, not a placeholder doing
 *   the job.
 *
 * So: a recessed track, an opaque raised thumb on the active segment, real
 * weight on the active label, and a hover state on the inactive ones. The
 * label lives outside, supplied by the caller.
 */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  size = 'sm',
  className,
}: {
  value: T
  onChange: (next: T) => void
  options: readonly T[] | readonly { value: T; label: string }[]
  /** The accessible group name. Render a visible one beside it too. */
  label: string
  size?: 'sm' | 'md'
  className?: string
}) {
  const items = options.map((o) => (typeof o === 'string' ? { value: o as T, label: o } : o))

  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn(
        // Recessed track: the inset shadow is what says "the thing on top of
        // me is raised", which is the whole illusion.
        'inline-flex shrink-0 items-center rounded-md border border-border bg-well p-0.5',
        'shadow-[inset_0_1px_2px_rgba(0,0,0,0.05)]',
        size === 'sm' ? 'h-8' : 'h-9',
        className,
      )}
    >
      {items.map((o) => {
        const on = o.value === value
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.value)}
            className={cn(
              'relative h-full rounded capitalize transition-all duration-150',
              'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
              size === 'sm' ? 'px-2.5 text-xs' : 'px-3 text-sm',
              on
                ? // Opaque, raised, and heavier. Not a tint.
                  'bg-card font-semibold text-foreground shadow-[0_1px_2px_rgba(0,0,0,0.12),0_0_0_1px_var(--border)]'
                : 'font-medium text-muted-foreground hover:bg-card/60 hover:text-foreground',
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
