import { useEffect, useRef, useState } from 'react'
import { useReducedMotion } from 'motion/react'
import { cn } from 'cn'
import type { Mood } from '@/components/agent/mood'

/**
 * The hand-drawn agent, played as frames.
 *
 * Extracted from the screen recording in /asset. It is soft-shaded
 * illustration, so tracing it to SVG would produce thousands of paths that
 * look worse and weigh more than the source — the honest treatment is a short
 * frame loop. Frames are served as static files rather than bundled, so they
 * never enter the JS payload and the browser caches them independently.
 *
 * Three things were wrong before, and all three looked like the same bug:
 *
 * 1. The studio background was keyed as `#EDDDC3`. The recording's actual
 *    background is `#F3E1C5`, so the key never fully matched and left a pale
 *    rectangle around the character; raising the tolerance far enough to
 *    cover the mismatch started eating the yellow, which is how the sprite
 *    turned white. Keying the real colour needs almost no tolerance at all.
 * 2. Frames after the first were `loading="lazy"`. The loop advanced to a
 *    frame the browser had not fetched yet and painted nothing — once per
 *    cycle, forever. Every frame is now fetched and decoded up front, and the
 *    loop does not start until they are all ready.
 * 3. Frames cross-faded over 100ms on a 140ms interval, so two drawings were
 *    on screen almost all the time. Animation frames cut; they do not
 *    dissolve.
 *
 * The cycle is one blink — open, closed, open — sampled so the last frame
 * flows back into the first. It loops without a seam and without a restart.
 */

/**
 * The drawings, by what they are rather than by order.
 *
 * Only two of the eight extracted frames have the eyes closed; the other six
 * are near-identical open-eyed poses. Playing all eight on one interval — which
 * is what this did — produced a blink every 0.88 seconds with six frames of
 * visible stutter between. That is what read as mechanical: nothing alive
 * blinks at 68 per minute, and nothing alive holds six subtly different poses
 * on the way there.
 *
 * So the frames are split. One is the rest pose, held. Four are the blink,
 * played as a burst and then dropped. The rest are unused — they were
 * duplicates of the rest pose, and shipping them was the bug.
 */
const REST = 'a1_01'

/** Open → closing → closed → opening. Played once, at speed. */
const BLINK = ['a1_02', 'a1_03', 'a1_04', 'a1_05']

/** Every drawing that needs preloading. */
const ALL = [REST, ...BLINK]

/**
 * How long each blink frame is held.
 *
 * There is deliberately no cross-fade, and adding one made this worse rather
 * than better. Two stacked RGBA drawings at 50% opacity do not composite to
 * one opaque drawing — the transparent background shows through both, so the
 * whole character washes out for the length of the fade and then comes back.
 * That dip is read as a flicker, and it is unavoidable when fading between
 * images that have their own alpha.
 *
 * So frames cut, and the hold carries the smoothness instead. 170ms is roughly
 * six frames a second, which is how hand-drawn animation has always been
 * timed; the original 62ms was the strobe.
 */
const BLINK_MS = 170

/** Gap between blinks, in ms. Randomised, because a metronome is the tell. */
const GAP_IDLE: [number, number] = [4000, 7500]
// Working blinks more often than resting, but not much — at 1.4s it read as
// twitching rather than as concentration.
const GAP_BUSY: [number, number] = [2400, 4200]

const SRC = (name: string) => `/agent/${name}.png`

/**
 * Moods that mean work is actually happening. Everything else holds still: a
 * stage that has not started must not look like one that is running, and at
 * 30px motion is the only difference anyone will notice.
 */
const BUSY: Mood[] = ['thinking', 'working', 'typing', 'looking']

export function AgentFrames({
  mood,
  size = 96,
  aware,
  className,
}: {
  mood: Mood
  size?: number
  /**
   * Something is happening nearby — the pointer is over the hero, or someone
   * is typing. The character does not change drawing (there is only one loop),
   * so awareness is expressed around it instead: the ground brightens, a ring
   * of arcs sweeps, and a few sparks lift.
   */
  aware?: boolean
  className?: string
}) {
  const still = useReducedMotion()
  const animate = BUSY.includes(mood) && !still
  const [ready, setReady] = useState(false)
  /** null while resting; otherwise the index into BLINK being shown. */
  const [blink, setBlink] = useState<number | null>(null)
  const live = useRef(true)

  // Decode every frame before the first swap. Without this the blink advances
  // to a frame the browser has not fetched and paints nothing.
  useEffect(() => {
    live.current = true
    let left = ALL.length
    for (const f of ALL) {
      const img = new Image()
      img.onload = img.onerror = () => {
        left -= 1
        if (left === 0 && live.current) setReady(true)
      }
      img.src = SRC(f)
    }
    return () => {
      live.current = false
    }
  }, [])

  // One self-rescheduling chain rather than a repeating interval: wait a
  // randomised gap, run the four blink frames, then wait again. It is also a
  // tenth of the timers the old loop ran.
  useEffect(() => {
    if (still || !ready) return
    let timer = 0
    let alive = true

    const [lo, hi] = animate ? GAP_BUSY : GAP_IDLE
    const gap = () => lo + Math.random() * (hi - lo)

    const step = (i: number) => {
      if (!alive) return
      if (i >= BLINK.length) {
        setBlink(null)
        timer = window.setTimeout(() => step(0), gap())
        return
      }
      setBlink(i)
      timer = window.setTimeout(() => step(i + 1), BLINK_MS)
    }

    timer = window.setTimeout(() => step(0), gap())
    return () => {
      alive = false
      window.clearTimeout(timer)
      setBlink(null)
    }
  }, [animate, ready, still])

  const shown = blink === null ? REST : BLINK[blink]

  const lit = (aware || animate) && !still

  return (
    <div
      className={cn('relative shrink-0 select-none', className)}
      style={{ width: size, height: size * 1.12 }}
      role="img"
      aria-label={`Agent, ${mood}`}
    >
      {/* A contact shadow. The desk used to be dissolved by a hard linear
          mask, which cut the drawing off in a straight line across the bottom
          — the single thing that made it look broken rather than placed. An
          ellipse under the feet does the opposite job: it puts the character
          on a surface instead of trimming it off one. */}
      <span
        aria-hidden
        className={cn(
          'absolute bottom-0 left-1/2 -translate-x-1/2 rounded-[50%] transition-all duration-500',
          lit ? 'bg-bolt/25 blur-[7px]' : 'bg-foreground/12 blur-[6px]',
        )}
        style={{ width: size * 0.66, height: size * 0.1 }}
      />

      {/* Awareness, drawn around the character rather than on it: two arcs
          sweeping in opposite directions and three sparks lifting off. It is
          off entirely at rest, so when it appears it means something. */}
      <svg
        aria-hidden
        viewBox="0 0 100 100"
        className={cn(
          'pointer-events-none absolute inset-x-0 top-0 transition-opacity duration-500 motion-reduce:hidden',
          lit ? 'opacity-100' : 'opacity-0',
        )}
        style={{ height: size }}
      >
        <g className="origin-center" style={{ animation: 'agent-ring 11s linear infinite' }}>
          <circle
            cx="50"
            cy="52"
            r="45"
            fill="none"
            className="stroke-bolt/45"
            strokeWidth="1.2"
            strokeLinecap="round"
            strokeDasharray="26 140"
          />
        </g>
        <g className="origin-center" style={{ animation: 'agent-ring 8s linear infinite reverse' }}>
          <circle
            cx="50"
            cy="52"
            r="45"
            fill="none"
            className="stroke-accent-strong/35"
            strokeWidth="1"
            strokeLinecap="round"
            strokeDasharray="12 160"
          />
        </g>
        {[
          { x: 14, y: 40, d: '0s' },
          { x: 86, y: 30, d: '1.1s' },
          { x: 80, y: 68, d: '2.2s' },
        ].map((p) => (
          <circle
            key={p.d}
            cx={p.x}
            cy={p.y}
            r="1.8"
            className="fill-bolt"
            style={{ animation: `agent-spark 3.4s ease-in-out ${p.d} infinite` }}
          />
        ))}
      </svg>

      {/* The drawing itself, unmasked. Only the very bottom of the desk is
          feathered, and gently — enough to meet the shadow, not enough to be
          seen as a cut. */}
      {/* A slow float. Individually imperceptible; without it the character
          is a still image for four seconds at a stretch, which is the other
          half of looking mechanical. */}
      <div
        className={cn('absolute inset-x-0 top-0', !still && 'motion-safe:animate-[agent-breathe_6.5s_ease-in-out_infinite]')}
        style={{
          height: size,
          WebkitMaskImage: 'radial-gradient(118% 100% at 50% 34%, #000 74%, transparent 100%)',
          maskImage: 'radial-gradient(118% 100% at 50% 34%, #000 74%, transparent 100%)',
        }}
      >
        {ALL.map((f) => (
          <img
            key={f}
            src={SRC(f)}
            alt=""
            aria-hidden
            width={size}
            height={size}
            // Every frame stays mounted and visibility is a hard switch. No
            // transition: two drawings must never be on screen at once.
            className={cn('absolute inset-0 size-full object-contain', f === shown ? 'opacity-100' : 'opacity-0')}
            loading="eager"
            decoding="async"
          />
        ))}
      </div>
    </div>
  )
}
