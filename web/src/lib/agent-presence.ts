import { useEffect, useSyncExternalStore } from 'react'
import { agentStateOf, idleLine, type AgentState } from '@/lib/agent-mood'
import type { Run, RunEvent } from '@/lib/types'

const IDLE: AgentState = { mood: 'idle', actor: '', says: idleLine }

let current: AgentState = IDLE
const listeners = new Set<() => void>()

function publish(next: AgentState) {
  if (next.mood === current.mood && next.says === current.says && next.actor === current.actor) return
  current = next
  for (const fn of listeners) fn()
}

/**
 * Publishes the live run into the shared agent presence.
 *
 * The companion lives in the shell so it survives navigation, but only the
 * run detail page knows what is happening. Rather than lift the whole run
 * into a context, the page pushes a three-field summary here.
 */
export function usePublishAgentState(run: Run | undefined, events: readonly RunEvent[]) {
  useEffect(() => {
    publish(agentStateOf(run, events))
  }, [run, events])

  // Going back to idle on unmount matters: a stale "still working" face on
  // the alerts screen would be a lie.
  useEffect(() => () => publish(IDLE), [])
}

/**
 * Subscribes the shell's companion to whatever the current page published.
 *
 * useSyncExternalStore rather than an effect plus setState: the store is
 * genuinely external, and the effect version read the value once at mount and
 * once again in the effect, so anything published between those two points
 * was silently dropped.
 */
export function useAgentState(): AgentState {
  return useSyncExternalStore(subscribe, () => current, () => current)
}

function subscribe(onChange: () => void): () => void {
  const fn = () => onChange()
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}
