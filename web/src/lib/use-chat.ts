import { useCallback, useEffect, useRef, useState } from 'react'
import { API_BASE } from '@/lib/api'

export interface ChatTurn {
  role: 'user' | 'agent'
  text: string
  /** Only on an agent turn that failed. */
  error?: string
  /** Devtron's request id, worth keeping for tracing a bad answer. */
  requestId?: string
  steps?: number
  ms?: number
}

const KEY = 'sre.chat'

/**
 * A conversation the browser owns.
 *
 * The server keeps nothing: every turn posts the recent history back, and
 * `/v1/chat` answers and forgets. That is the honest implementation of "close
 * it and it is gone" — there is no copy to go stale, no row in history, and
 * no id to leak into a list someone reads six months later.
 *
 * It survives a reload because losing an answer to a mistyped ⌘R is
 * infuriating and costs nothing to prevent. It does not survive closing the
 * panel, because that is the gesture that means "done with this".
 *
 * sessionStorage rather than localStorage: a conversation about one cluster in
 * one sitting should not reappear in a tab opened next week.
 */
export function useChat(clusterId: number | undefined, clusterName: string | undefined) {
  const [turns, setTurns] = useState<ChatTurn[]>(() => load())
  const [thinking, setThinking] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const abort = useRef<AbortController | null>(null)

  useEffect(() => {
    try {
      sessionStorage.setItem(KEY, JSON.stringify(turns))
    } catch {
      // Private mode, or storage is full. The thread still works in memory;
      // it just will not survive a reload, which is the lesser loss.
    }
  }, [turns])

  const clear = useCallback(() => {
    abort.current?.abort()
    abort.current = null
    setTurns([])
    setThinking([])
    setBusy(false)
    try {
      sessionStorage.removeItem(KEY)
    } catch {
      /* nothing to clean up if it was never written */
    }
  }, [])

  const send = useCallback(
    async (ask: string) => {
      const text = ask.trim()
      if (!text || clusterId === undefined) return

      // Snapshot before the optimistic append, so the server sees the history
      // as it stood when the question was asked.
      const history = turns.map((t) => ({ role: t.role, text: t.text }))

      setTurns((prev) => [...prev, { role: 'user', text }])
      setThinking([])
      setBusy(true)

      const ctrl = new AbortController()
      abort.current = ctrl

      try {
        const res = await fetch(`${API_BASE}/chat`, {
          method: 'POST',
          headers: { 'content-type': 'application/json' },
          body: JSON.stringify({ clusterId, clusterName, ask: text, history }),
          signal: ctrl.signal,
        })
        if (!res.ok || !res.body) throw new Error(`the agent answered ${res.status}`)

        // Hand-rolled SSE: EventSource cannot POST, and the history has to go
        // in a body rather than a query string.
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })

          let cut = buffer.indexOf('\n\n')
          while (cut !== -1) {
            handleFrame(buffer.slice(0, cut), setTurns, setThinking)
            buffer = buffer.slice(cut + 2)
            cut = buffer.indexOf('\n\n')
          }
        }
      } catch (e) {
        if ((e as Error).name !== 'AbortError') {
          setTurns((prev) => [...prev, { role: 'agent', text: '', error: (e as Error).message }])
        }
      } finally {
        setBusy(false)
        setThinking([])
        abort.current = null
      }
    },
    [clusterId, clusterName, turns],
  )

  useEffect(() => () => abort.current?.abort(), [])

  return { turns, thinking, busy, send, clear }
}

function handleFrame(
  frame: string,
  setTurns: React.Dispatch<React.SetStateAction<ChatTurn[]>>,
  setThinking: React.Dispatch<React.SetStateAction<string[]>>,
) {
  let event = 'message'
  let data = ''
  for (const line of frame.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) data += line.slice(5).trim()
  }
  if (!data) return

  let payload: Record<string, unknown>
  try {
    payload = JSON.parse(data) as Record<string, unknown>
  } catch {
    return
  }

  if (event === 'thinking' && typeof payload.text === 'string') {
    setThinking((prev) => [...prev, payload.text as string])
    return
  }
  if (event === 'answer') {
    setTurns((prev) => [
      ...prev,
      {
        role: 'agent',
        text: String(payload.text ?? ''),
        requestId: payload.requestId as string | undefined,
        steps: payload.steps as number | undefined,
        ms: payload.ms as number | undefined,
      },
    ])
    return
  }
  if (event === 'error') {
    setTurns((prev) => [...prev, { role: 'agent', text: '', error: String(payload.error ?? 'unknown error') }])
  }
}

function load(): ChatTurn[] {
  try {
    const raw = sessionStorage.getItem(KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as ChatTurn[]) : []
  } catch {
    return []
  }
}
