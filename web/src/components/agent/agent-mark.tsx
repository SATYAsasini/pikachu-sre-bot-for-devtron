import { cn } from 'cn'
import type { Mood } from '@/components/agent/mood'

/**
 * The agent at small sizes: the artwork, in a ring.
 *
 * This replaces a hand-drawn pixel sprite. The sprite was built out of theme
 * tokens so it would repaint for day and night, and the cost of that was a
 * palette pale enough to work on both — at 22px in a rail it read as a white
 * blob rather than as a character. One mark, at every size, is both more
 * recognisable and one file instead of three hundred rects.
 *
 * Mood is carried by the ring rather than by the face, because a 22px face
 * cannot carry it and a coloured ring can be read at a glance from across the
 * page.
 */

const RING: Record<Mood, string> = {
  idle: 'ring-border',
  looking: 'ring-border',
  asleep: 'ring-border opacity-45 saturate-50',
  typing: 'ring-accent-strong/50',
  thinking: 'ring-accent-strong/50',
  working: 'ring-accent-strong/60',
  pleased: 'ring-ok/60',
  concerned: 'ring-bad/60',
}

const BUSY: Mood[] = ['typing', 'thinking', 'working']

export function AgentMark({
  mood = 'idle',
  size = 24,
  className,
}: {
  mood?: Mood
  size?: number
  className?: string
}) {
  const busy = BUSY.includes(mood)

  return (
    <span
      className={cn('relative inline-flex shrink-0', className)}
      style={{ width: size, height: size }}
      role="img"
      aria-label={`Agent, ${mood}`}
    >
      {/* A halo only while something is actually in flight. Motion that is
          always on stops meaning anything. */}
      {busy && (
        <span
          aria-hidden
          className="absolute inset-0 animate-ping rounded-full bg-accent-strong/25 motion-reduce:hidden"
        />
      )}
      <img
        src="/mark.png"
        alt=""
        aria-hidden
        width={size}
        height={size}
        className={cn('relative size-full rounded-full object-cover ring-1', RING[mood])}
      />
    </span>
  )
}
