import { useEffect, useState } from 'react'

/**
 * A ticking clock, for live durations. Pass `active: false` on finished runs so
 * a page full of history is not re-rendering once a second for nothing.
 */
export function useNow(intervalMs = 1000, active = true): number {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!active) return
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs, active])

  return now
}
