import { cn } from 'cn'

/**
 * A light that travels once around a container's border.
 *
 * Ported from the Inspira UI / Magic UI effect, which is Vue-only, so it is
 * reimplemented here rather than installed. It is pure CSS — an
 * `offset-path` walk around a rounded rectangle — so it costs nothing on the
 * main thread and degrades to nothing under reduced motion.
 *
 * Used for exactly one thing: the stage that is running right now. A border
 * that is in motion reads as "this is happening" from the corner of the eye,
 * which a coloured dot never manages.
 */
export function BorderBeam({
  duration = 5,
  size = 120,
  className,
  tone = 'accent',
}: {
  duration?: number
  size?: number
  className?: string
  tone?: 'accent' | 'ok' | 'warn'
}) {
  const color =
    tone === 'ok' ? 'var(--ok)' : tone === 'warn' ? 'var(--warn)' : 'var(--accent-strong)'

  return (
    <div
      aria-hidden
      className={cn(
        'pointer-events-none absolute inset-0 rounded-[inherit]',
        '[border:1px_solid_transparent] ![mask-clip:padding-box,border-box] ![mask-composite:intersect]',
        '[mask:linear-gradient(transparent,transparent),linear-gradient(#000,#000)]',
        className,
      )}
    >
      <div
        className="absolute aspect-square bg-[linear-gradient(to_left,var(--beam-color),transparent)] motion-reduce:hidden"
        style={
          {
            width: size,
            offsetPath: `rect(0 auto auto 0 round ${size}px)`,
            animation: `border-beam ${duration}s linear infinite`,
            '--beam-color': color,
          } as React.CSSProperties
        }
      />
    </div>
  )
}
