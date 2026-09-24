import type { Run, RunEvent } from '@/lib/types'

export type Mood = 'idle' | 'looking' | 'thinking' | 'working' | 'pleased' | 'concerned' | 'asleep'

export interface AgentState {
  mood: Mood
  /** Which agent is on shift: intelligence, judge, sre, or none. */
  actor: string
  /** One short line, in the agent's own voice. */
  says: string
}

const IDLE_LINES = [
  'Nothing on. Point me at a cluster.',
  'All quiet. Suspiciously quiet.',
  'Standing by. Coffee acquired.',
  'No alerts open. Enjoy it while it lasts.',
]

/** Picks a stable idle line per mount so it does not flicker on re-render. */
export const idleLine = IDLE_LINES[Math.floor(Math.random() * IDLE_LINES.length)]

/**
 * Turns the run's live state into something with a face.
 *
 * This is cosmetic, but it is doing real work: which of the two agents is
 * currently spending your budget is genuinely useful to see at a glance, and
 * a status word in a table does not communicate "still going" the way a
 * moving thing does.
 */
export function agentStateOf(run: Run | undefined, events: readonly RunEvent[]): AgentState {
  if (!run) return { mood: 'idle', actor: '', says: idleLine }

  switch (run.status) {
    case 'succeeded':
      return { mood: 'pleased', actor: '', says: 'Done. Verdict is in.' }
    case 'failed':
      return { mood: 'concerned', actor: '', says: 'That did not go to plan.' }
    case 'canceled':
      return { mood: 'asleep', actor: '', says: 'Stopped, as asked.' }
    case 'budget_exceeded':
      return { mood: 'concerned', actor: '', says: 'Out of budget. Report may be thin.' }
    case 'partial':
      return { mood: 'concerned', actor: '', says: 'Devtron answered. I could not check it.' }
    case 'queued':
      return { mood: 'looking', actor: '', says: 'Queued. Warming up.' }
  }

  // Running: report whoever spoke most recently.
  const last = [...events].reverse().find((e) => e.agent || e.type.startsWith('intelligence'))
  const actor = last?.agent || (last?.type.startsWith('intelligence') ? 'intelligence' : '')
  const lastTool = [...events].reverse().find((e) => e.type === 'tool_call')

  if (last?.type === 'tool_call' || (lastTool && last?.type === 'tool_result')) {
    const tool = (lastTool?.payload as { tool?: string } | undefined)?.tool
    return { mood: 'working', actor, says: tool ? `Running ${tool}` : 'Checking something' }
  }
  if (actor === 'intelligence') {
    return { mood: 'thinking', actor, says: 'Devtron is taking the first pass' }
  }
  if (actor === 'sre') {
    return { mood: 'working', actor, says: 'Digging into what it missed' }
  }
  return { mood: 'thinking', actor, says: 'Gathering facts' }
}
