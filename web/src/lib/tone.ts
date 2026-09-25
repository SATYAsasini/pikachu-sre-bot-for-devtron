import type { Tone } from '@/components/common/status'

/**
 * Alert severity to one of the four semantic hues.
 *
 * Takes undefined, because the wire does. Every one of these fields is
 * `omitempty` on the server, so an alert with no severity arrives with no
 * severity *key* — and a bare `.toLowerCase()` on it threw during render and
 * took the entire page down to "Something went wrong!". A missing severity is
 * an ordinary thing for an alert to have; it is not a reason to lose the
 * screen.
 */
export function severityTone(severity?: string): Tone {
  switch ((severity ?? '').toLowerCase()) {
    case 'critical':
      return 'bad'
    case 'warning':
      return 'warn'
    case 'info':
      return 'neutral'
    default:
      return 'unknown'
  }
}

/**
 * Alert lifecycle state to one of the four semantic hues.
 *
 * `active` is not a synonym anyone chose — it is what vmalert calls firing,
 * while Alertmanager says `firing`. Only one of the two was handled, so every
 * alert from a VictoriaMetrics cluster fell through to grey and the status dot
 * said nothing at all.
 */
export function stateTone(state?: string): Tone {
  switch ((state ?? '').toLowerCase()) {
    case 'firing':
    case 'active':
      return 'bad'
    case 'pending':
      return 'warn'
    case 'resolved':
    case 'inactive':
      return 'ok'
    case 'suppressed':
      return 'neutral'
    default:
      return 'unknown'
  }
}

/** True when the alert is still going off, whichever backend named it. */
export function isFiring(state?: string): boolean {
  const s = (state ?? '').toLowerCase()
  return s === 'firing' || s === 'active'
}

/** True once the alert has stopped, whichever backend named it. */
export function isResolved(state?: string): boolean {
  const s = (state ?? '').toLowerCase()
  return s === 'resolved' || s === 'inactive'
}

/** Judge verdict to one of the four semantic hues. */
export function verdictTone(verdict?: string): Tone {
  switch (verdict) {
    case 'supported':
      return 'ok'
    case 'partly_supported':
      return 'warn'
    case 'unsupported':
      return 'bad'
    default:
      return 'unknown'
  }
}
