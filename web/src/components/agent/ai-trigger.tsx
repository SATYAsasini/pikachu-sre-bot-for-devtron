import type { ReactNode } from 'react'
import { cn } from 'cn'

/**
 * The one button that starts an investigation.
 *
 * Every AI trigger in the product renders this: the ask box, the alert detail,
 * the confirmation dialog. They were three different buttons before, and the
 * main one came out muted grey — the single most consequential control on the
 * page looked like a cancel.
 *
 * The treatment is a shallow 3D raise in the agent's own yellow, plus a bolt.
 * "3D" here means two inset shadows and a drop shadow, not a gradient mesh: it
 * has to survive at 28px tall next to a textarea, and it has to read on both
 * themes. Pressing it drops the raise by a pixel, which is the entire
 * interaction.
 *
 * The bolt shimmers once every few seconds rather than continuously. Motion
 * that never stops stops meaning anything, and this one is saying "this costs
 * a model call".
 */
export function AiTrigger({
  children = 'Trigger debug with AI',
  onClick,
  disabled,
  busy,
  size = 'md',
  shape = 'pill',
  type = 'button',
  className,
  title,
}: {
  children?: ReactNode
  onClick?: () => void
  disabled?: boolean
  /** A run is already starting. Keeps the label, stills the bolt. */
  busy?: boolean
  /** `xs` matches the 24px row buttons on the alert list. */
  size?: 'xs' | 'sm' | 'md'
  /**
   * `orb` is the round send control that lives inside the ask box. A labelled
   * pill there had to be wide enough to read, which pushed it onto the
   * textarea's own border — a round one sits properly inside the field, and
   * the label moves to the accessible name.
   */
  shape?: 'pill' | 'orb'
  type?: 'button' | 'submit'
  className?: string
  title?: string
}) {
  if (shape === 'orb') {
    return (
      <AiOrb
        onClick={onClick}
        disabled={disabled}
        busy={busy}
        type={type}
        className={className}
        label={typeof children === 'string' ? children : 'Trigger debug with AI'}
      />
    )
  }

  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled || busy}
      title={title}
      className={cn(
        'group relative inline-flex shrink-0 items-center gap-1.5 rounded-lg font-semibold whitespace-nowrap',
        'transition-[transform,box-shadow,filter] duration-150 ease-out',
        'focus-visible:ring-[3px] focus-visible:ring-bolt/50 focus-visible:outline-none',
        size === 'xs' ? 'h-6 gap-1 px-2 text-xs' : size === 'sm' ? 'h-7 px-2.5 text-xs' : 'h-9 px-3.5 text-sm',
        // The raise. A light top edge, a dark bottom edge and a cast shadow —
        // three declarations, no images.
        'bg-bolt text-bolt-ink',
        'shadow-[inset_0_1px_0_rgba(255,255,255,0.55),inset_0_-2px_0_rgba(120,80,0,0.28),0_2px_6px_rgba(120,80,0,0.28)]',
        'hover:-translate-y-px hover:brightness-[1.06]',
        'hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.6),inset_0_-2px_0_rgba(120,80,0,0.3),0_4px_12px_rgba(120,80,0,0.34)]',
        // Pressed: sit down onto the page and lose the cast shadow.
        'active:translate-y-px active:shadow-[inset_0_2px_4px_rgba(120,80,0,0.35)]',
        // Disabled keeps the shape and most of the colour: this is "type
        // something first", not "this is broken". opacity-45 plus saturate-50
        // on a yellow button produced a pale beige with grey text, which is
        // exactly what a dead control looks like.
        'disabled:pointer-events-none disabled:translate-y-0 disabled:bg-bolt/45 disabled:text-bolt-ink/70',
        'disabled:shadow-[inset_0_1px_0_rgba(255,255,255,0.35)]',
        className,
      )}
    >
      <Bolt busy={busy || disabled} className={size === 'md' ? 'size-4' : 'size-3.5'} />
      <span>{children}</span>

      {/* A single sweep of light across the face. Purely decorative, so it is
          hidden from assistive tech and stopped under reduced motion. */}
      <span
        aria-hidden
        className={cn(
          'pointer-events-none absolute inset-0 overflow-hidden rounded-lg',
          'motion-reduce:hidden',
        )}
      >
        <span className="absolute inset-y-0 -left-full w-1/2 -skew-x-12 bg-white/30 blur-[2px] group-hover:animate-[ai-sheen_0.9s_ease-out]" />
      </span>
    </button>
  )
}

/** The bolt, drawn rather than imported, so it can carry its own animation. */
function Bolt({ busy, className }: { busy?: boolean; className?: string }) {
  return (
    <svg
      aria-hidden
      viewBox="0 0 24 24"
      fill="none"
      className={cn('shrink-0', !busy && 'motion-safe:animate-[ai-spark_3.2s_ease-in-out_infinite]', className)}
    >
      {/* Filled, with a darker outline: at 16px a stroke-only bolt reads as a
          smudge against the yellow it sits on. */}
      <path
        d="M13.5 2 4 13.2h6.2L9.8 22 20 10.6h-6.6L13.5 2Z"
        fill="currentColor"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinejoin="round"
      />
    </svg>
  )
}

/**
 * The send control, as an orb.
 *
 * Two rings and a bolt. The outer ring is a conic gradient that only spins on
 * hover or while a run is starting — a permanently spinning thing next to a
 * text field reads as "loading" and is exhausting to type beside. On approach
 * it swirls up, the glow blooms and the bolt strikes; at rest it is a plain
 * yellow disc.
 *
 * The label is not drawn but it is still there: `aria-label` carries it, and a
 * tooltip is unnecessary because the field beside it says what it does.
 */
function AiOrb({
  onClick,
  disabled,
  busy,
  type,
  className,
  label,
}: {
  onClick?: () => void
  disabled?: boolean
  busy?: boolean
  type: 'button' | 'submit'
  className?: string
  label: string
}) {
  const live = busy || !disabled
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled || busy}
      aria-label={label}
      title={label}
      className={cn(
        'group relative grid size-10 shrink-0 place-items-center rounded-full',
        'transition-[transform,box-shadow] duration-200 ease-out',
        'focus-visible:ring-[3px] focus-visible:ring-bolt/50 focus-visible:outline-none',
        'bg-bolt text-bolt-ink',
        'shadow-[inset_0_1px_0_rgba(255,255,255,0.6),inset_0_-2px_0_rgba(120,80,0,0.3),0_2px_8px_rgba(120,80,0,0.3)]',
        'enabled:hover:scale-[1.07] enabled:hover:shadow-[inset_0_1px_0_rgba(255,255,255,0.65),0_0_0_4px_rgba(245,197,24,0.22),0_6px_18px_rgba(120,80,0,0.38)]',
        'enabled:active:scale-95',
        'disabled:pointer-events-none disabled:bg-bolt/45 disabled:text-bolt-ink/70 disabled:shadow-[inset_0_1px_0_rgba(255,255,255,0.35)]',
        className,
      )}
    >
      {/* The swirl. A conic gradient masked to a ring, spun only when the
          control is live and the pointer is on it. */}
      <span
        aria-hidden
        className={cn(
          'pointer-events-none absolute -inset-[3px] rounded-full opacity-0 transition-opacity duration-200',
          'motion-reduce:hidden',
          live && 'group-hover:opacity-100 group-focus-visible:opacity-100',
          busy && 'opacity-100',
        )}
        style={{
          background:
            'conic-gradient(from 0deg, transparent 0deg, var(--bolt) 70deg, #fff 110deg, var(--bolt) 150deg, transparent 220deg)',
          WebkitMask: 'radial-gradient(farthest-side, transparent calc(100% - 3px), #000 calc(100% - 3px))',
          mask: 'radial-gradient(farthest-side, transparent calc(100% - 3px), #000 calc(100% - 3px))',
          animation: 'ai-swirl 1.1s linear infinite',
        }}
      />

      <Bolt
        busy={busy || disabled}
        className={cn('relative size-[18px]', live && 'group-hover:animate-[ai-strike_0.5s_ease-out]')}
      />
    </button>
  )
}
