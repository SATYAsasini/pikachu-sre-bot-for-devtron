import { Ban, CheckCircle2, CircleDashed, Gauge, Loader2, PlugZap, ShieldAlert, XCircle } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Chip, type Tone } from '@/components/common/status'
import { isInterrupted, type Run, type RunStatus } from '@/lib/types'

interface StatusLook {
  tone: Tone
  label: string
  icon: LucideIcon
  spin?: boolean
}

const LOOKS: Record<RunStatus, StatusLook> = {
  queued: { tone: 'neutral', label: 'queued', icon: CircleDashed },
  running: { tone: 'warn', label: 'running', icon: Loader2, spin: true },
  succeeded: { tone: 'ok', label: 'succeeded', icon: CheckCircle2 },
  failed: { tone: 'bad', label: 'failed', icon: XCircle },
  canceled: { tone: 'unknown', label: 'canceled', icon: Ban },
  budget_exceeded: { tone: 'warn', label: 'budget spent', icon: Gauge },
  // Devtron answered and we did not. Amber, not red: a complete first pass is
  // real work, and "failed" throws it away.
  partial: { tone: 'warn', label: 'unverified', icon: ShieldAlert },
}

/**
 * A run cut short by a process restart is an interruption, not a verdict. It
 * gets warm-toned "interrupted" wording rather than the red failure badge,
 * because nothing about the investigation actually went wrong.
 */
const INTERRUPTED: StatusLook = { tone: 'warn', label: 'interrupted', icon: PlugZap }

function statusLook(run: Pick<Run, 'status' | 'error'>): StatusLook {
  return isInterrupted(run) ? INTERRUPTED : LOOKS[run.status] ?? LOOKS.queued
}

export function RunStatusChip({ run, className }: { run: Pick<Run, 'status' | 'error'>; className?: string }) {
  const look = statusLook(run)
  const Icon = look.icon
  return (
    <Chip tone={look.tone} className={className} icon={<Icon aria-hidden className={look.spin ? 'size-3 animate-spin' : 'size-3'} />}>
      {look.label}
    </Chip>
  )
}
