import { cn } from 'cn'

/**
 * A faint dot lattice behind the page.
 *
 * The Inspira/Magic UI "dot pattern" idea, done as a single CSS
 * radial-gradient rather than an SVG of hundreds of circles. It gives the
 * surface somewhere to sit without adding a coloured wash, which is what
 * keeps this reading as an instrument rather than a marketing page.
 */
export function DotGrid({ className }: { className?: string }) {
  return (
    <div
      aria-hidden
      className={cn(
        'pointer-events-none fixed inset-0 -z-10',
        '[background-image:radial-gradient(var(--dot)_1px,transparent_1px)]',
        '[background-size:22px_22px]',
        // Fade it out downward so the dots never compete with content.
        '[mask-image:linear-gradient(to_bottom,black,black_28rem,transparent)]',
        className,
      )}
    />
  )
}
