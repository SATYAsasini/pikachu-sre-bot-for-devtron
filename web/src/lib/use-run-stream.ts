import { useCallback, useEffect, useRef, useState } from 'react'
import { runStreamUrl } from '@/lib/api'
import { isFixtureMode, openFixtureStream } from '@/lib/fixtures'
import { EVENT_TYPES, type RunEvent } from '@/lib/types'

export type StreamStatus = 'idle' | 'connecting' | 'live' | 'reconnecting' | 'done' | 'error'

export interface RunStreamState {
  /** Events from the stream only, ascending by seq, de-duplicated. */
  events: RunEvent[]
  status: StreamStatus
  /** True once the server sent `event: done`. */
  done: boolean
  lastSeq: number
  /** Drops the buffer and reconnects from `afterSeq`. */
  reset: () => void
}

function parseEvent(raw: string): RunEvent | null {
  try {
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return null
    const rec = parsed as Record<string, unknown>
    if (typeof rec.seq !== 'number' || typeof rec.type !== 'string') return null
    return {
      seq: rec.seq,
      at: typeof rec.at === 'string' ? rec.at : new Date().toISOString(),
      type: rec.type,
      agent: typeof rec.agent === 'string' ? rec.agent : undefined,
      payload: typeof rec.payload === 'object' && rec.payload !== null && !Array.isArray(rec.payload) ? (rec.payload as Record<string, unknown>) : {},
    }
  } catch {
    return null
  }
}

/**
 * Subscribes to `GET /v1/runs/{id}/stream`.
 *
 * The caller loads the ledger over REST first and passes the highest seq it
 * already holds as `afterSeq`; the connection opens with `?after=<seq>` so
 * nothing is replayed. `afterSeq` is read at connect time only, so arriving
 * events never tear the socket down. The stream ends on `event: done`.
 *
 * In fixture mode this drives a scripted replay instead of an EventSource, so
 * the run detail page can be built with no backend at all.
 */
export function useRunStream(runId: string, afterSeq: number, enabled: boolean): RunStreamState {
  const [events, setEvents] = useState<RunEvent[]>([])
  const [conn, setConn] = useState<StreamStatus>('connecting')
  const [done, setDone] = useState(false)
  const [lastSeq, setLastSeq] = useState(afterSeq)
  const [generation, setGeneration] = useState(0)

  const afterRef = useRef(afterSeq)
  useEffect(() => {
    afterRef.current = afterSeq
  }, [afterSeq])

  const seenRef = useRef<Set<number>>(new Set())

  const reset = useCallback(() => {
    seenRef.current = new Set()
    setEvents([])
    setDone(false)
    setConn('connecting')
    setLastSeq(afterRef.current)
    setGeneration((g) => g + 1)
  }, [])

  const active = enabled && runId !== ''

  useEffect(() => {
    if (!active) return

    const ingest = (ev: RunEvent) => {
      if (seenRef.current.has(ev.seq)) return
      seenRef.current.add(ev.seq)
      setEvents((prev) => (prev.length === 0 || prev[prev.length - 1].seq < ev.seq ? [...prev, ev] : [...prev, ev].sort((a, b) => a.seq - b.seq)))
      setLastSeq((s) => Math.max(s, ev.seq))
    }

    const finish = () => {
      setDone(true)
      setConn('done')
    }

    if (isFixtureMode()) {
      return openFixtureStream(runId, afterRef.current, {
        onOpen: () => setConn('live'),
        onEvent: ingest,
        onDone: finish,
      })
    }

    const source = new EventSource(runStreamUrl(runId, afterRef.current))
    let closed = false

    const onMessage = (e: Event) => {
      if (!(e instanceof MessageEvent) || typeof e.data !== 'string') return
      const ev = parseEvent(e.data)
      if (ev) ingest(ev)
    }

    const onDone = () => {
      closed = true
      source.close()
      finish()
    }

    // EventSource fires a plain Event named "error" for transport failures and
    // a MessageEvent named "error" when the server sends `event: error`.
    const onError = (e: Event) => {
      if (e instanceof MessageEvent) {
        onMessage(e)
        return
      }
      if (closed) return
      setConn(source.readyState === EventSource.CLOSED ? 'error' : 'reconnecting')
    }

    source.addEventListener('message', onMessage)
    for (const t of EVENT_TYPES) {
      if (t === 'error') continue
      source.addEventListener(t, onMessage)
    }
    source.addEventListener('open', () => setConn('live'))
    source.addEventListener('error', onError)
    source.addEventListener('done', onDone)

    return () => {
      closed = true
      source.close()
    }
  }, [runId, active, generation])

  return { events, status: active ? conn : 'idle', done, lastSeq, reset }
}

/** Merges the REST ledger with streamed events, de-duplicating by seq. */
export function mergeEvents(ledger: readonly RunEvent[], live: readonly RunEvent[]): RunEvent[] {
  if (live.length === 0) return [...ledger]
  const bySeq = new Map<number, RunEvent>()
  for (const e of ledger) bySeq.set(e.seq, e)
  for (const e of live) bySeq.set(e.seq, e)
  return [...bySeq.values()].sort((a, b) => a.seq - b.seq)
}
