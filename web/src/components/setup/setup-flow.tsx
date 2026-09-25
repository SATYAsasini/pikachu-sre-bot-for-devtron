import { useState } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { Check, KeyRound, Link2, Loader2, Radar, RotateCw, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Code, Mono } from '@/components/common/mono'
import { Chip } from '@/components/common/status'
import { errorMessage } from '@/lib/api'
import {
  useAllClusters,
  useRefreshClusters,
  useSaveSettings,
  useSettings,
  useSweepProgress,
  useTestSettings,
} from '@/lib/queries'
import { useReadiness, type SetupStep } from '@/lib/readiness'
import type { Reach } from '@/lib/types'

/**
 * The setup journey, as a sequence rather than a form.
 *
 * Three prerequisites have to be true before an investigation can succeed,
 * and they are genuinely ordered: you cannot measure which clusters answer
 * until a host and token exist. Presenting them as three fields on one page
 * hides that ordering and lets an operator "finish" configuration while still
 * being two steps from a working product.
 *
 * So: one step open at a time, each gated on the one before, and each
 * collapsing to a single line of fact once it passes.
 */
export function SetupFlow() {
  const { steps, activeIndex, ready } = useReadiness()
  const [opened, setOpened] = useState<number | null>(null)
  const open = opened ?? activeIndex

  return (
    <ol className="space-y-2">
      {steps.map((step, i) => (
        <StepCard
          key={step.key}
          step={step}
          index={i}
          expanded={open === i && !(ready && opened === null)}
          onToggle={() => setOpened(open === i ? -1 : i)}
        />
      ))}
    </ol>
  )
}

const STEP_ICON = { connect: Link2, reach: Radar, model: KeyRound } as const

function StepCard({
  step,
  index,
  expanded,
  onToggle,
}: {
  step: SetupStep
  index: number
  expanded: boolean
  onToggle: () => void
}) {
  const still = useReducedMotion()
  const Icon = STEP_ICON[step.key]
  const done = step.state === 'done'
  const blocked = step.state === 'blocked'
  const pending = step.state === 'pending'

  return (
    <li
      className={cn(
        'overflow-hidden rounded-lg border transition-colors',
        done && 'border-ok/25 bg-card',
        blocked && 'border-bad/30 bg-card',
        !done && !blocked && !pending && 'border-accent-strong/35 bg-card',
        pending && 'border-border bg-card opacity-60',
      )}
    >
      <button
        type="button"
        onClick={onToggle}
        disabled={pending}
        aria-expanded={expanded}
        className="flex w-full items-start gap-3 px-3 py-2.5 text-left focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none disabled:cursor-not-allowed"
      >
        <span
          className={cn(
            'mt-0.5 grid size-6 shrink-0 place-items-center rounded-full border text-[0.6875rem] font-semibold',
            done ? 'border-ok/30 bg-ok/10 text-ok' : blocked ? 'border-bad/30 bg-bad/10 text-bad' : 'border-border bg-well text-muted-foreground',
          )}
        >
          {done ? <Check aria-hidden className="size-3.5" /> : index + 1}
        </span>

        <span className="min-w-0 flex-1">
          <span className="flex flex-wrap items-center gap-2">
            <span className="font-display text-sm font-semibold">{step.title}</span>
            {done && <Chip tone="ok">ready</Chip>}
            {blocked && <Chip tone="bad">blocked</Chip>}
          </span>
          <span className="mt-0.5 block text-xs text-muted-foreground">{done ? step.status : step.purpose}</span>
        </span>

        <Icon aria-hidden className={cn('mt-0.5 size-4 shrink-0', done ? 'text-ok' : 'text-muted-foreground')} />
      </button>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            initial={still ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={still ? undefined : { height: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
          >
            <div className="border-t border-border px-3 py-3">
              {step.action && (
                <p className="mb-2.5 flex items-start gap-1.5 text-xs text-muted-foreground">
                  {blocked && <TriangleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0 text-bad" />}
                  {step.action}
                </p>
              )}
              {step.key === 'connect' && <ConnectStep />}
              {step.key === 'reach' && <ReachStep />}
              {step.key === 'model' && <ModelStep />}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </li>
  )
}

/** Step 1. Save is gated on a passing test — configuration that has never been
 *  proven to work is the thing this whole flow exists to prevent. */
function ConnectStep() {
  const settings = useSettings()
  const save = useSaveSettings()
  const test = useTestSettings()

  const [url, setUrl] = useState('')
  const [token, setToken] = useState('')
  const [touched, setTouched] = useState(false)

  // Seeded during render rather than in an effect. An effect would paint the
  // field empty once and then fill it, which reads as a flash on every visit
  // to a page whose whole job is to show what is already configured.
  const seeded = settings.data?.devtronUrl ?? ''
  if (!touched && seeded !== '' && url === '' && seeded !== url) setUrl(seeded)

  const urlError =
    touched && url.trim() !== '' && !/^https?:\/\/.+/.test(url.trim())
      ? 'Include the scheme, for example https://devtron.your-company.com'
      : ''
  const hasToken = token.trim().length > 0 || Boolean(settings.data?.tokenSet)
  const canTest = url.trim().length > 0 && !urlError && hasToken && !test.isPending
  const proven = test.data?.ok === true

  return (
    <div className="space-y-3">
      <div className="space-y-1.5">
        <Label htmlFor="setup-url" className="text-xs">
          Devtron host
        </Label>
        <Input
          id="setup-url"
          value={url}
          onChange={(e) => {
            setUrl(e.target.value)
            setTouched(true)
            test.reset()
          }}
          placeholder="https://devtron.your-company.com"
          spellCheck={false}
          autoComplete="off"
          aria-invalid={Boolean(urlError)}
          aria-describedby={urlError ? 'setup-url-error' : undefined}
          className="font-mono text-xs"
        />
        {urlError && (
          <p id="setup-url-error" role="alert" className="text-xs text-bad">
            {urlError}
          </p>
        )}
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="setup-token" className="text-xs">
          View-only API token
        </Label>
        <Input
          id="setup-token"
          type="password"
          value={token}
          onChange={(e) => {
            setToken(e.target.value)
            setTouched(true)
            test.reset()
          }}
          placeholder={settings.data?.tokenSet ? `stored, ending ${settings.data.tokenHint ?? '••••'} — blank keeps it` : 'paste the token'}
          spellCheck={false}
          autoComplete="off"
          className="font-mono text-xs"
        />
        <p className="text-[0.6875rem] text-muted-foreground">
          Devtron&apos;s own RBAC decides what this reads, and every call it makes is a GET.
        </p>
      </div>

      {test.data && (
        <div
          role="status"
          className={cn(
            'rounded-md border px-2.5 py-2 text-xs',
            test.data.ok ? 'border-ok/25 bg-ok/5' : 'border-bad/25 bg-bad/5',
          )}
        >
          <p className="leading-relaxed">{test.data.message}</p>
          {test.data.names?.length ? (
            <p className="mt-1 flex flex-wrap gap-1">
              {test.data.names.map((n) => (
                <Mono key={n} value={n} className="rounded border border-border bg-card px-1 py-px text-[0.6875rem]" />
              ))}
            </p>
          ) : null}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant={proven ? 'default' : 'outline'}
          onClick={() =>
            test.mutate(
              { devtronUrl: url.trim(), devtronToken: token.trim() },
              { onError: (e) => toast.error('Could not test', { description: errorMessage(e) }) },
            )
          }
          disabled={!canTest}
        >
          {test.isPending ? <Loader2 aria-hidden className="size-3.5 animate-spin" /> : null}
          Test connection
        </Button>
        <Button
          size="sm"
          disabled={!proven || save.isPending}
          onClick={() =>
            save.mutate(
              { devtronUrl: url.trim(), devtronToken: token.trim() },
              {
                onSuccess: () => {
                  setToken('')
                  setTouched(false)
                  toast.success('Connected', { description: 'Now measuring which clusters answer.' })
                },
                onError: (e) => toast.error('Could not save', { description: errorMessage(e) }),
              },
            )
          }
        >
          {save.isPending ? <Loader2 aria-hidden className="size-3.5 animate-spin" /> : null}
          Save and continue
        </Button>
        {!proven && (
          <span className="text-[0.6875rem] text-muted-foreground">Test has to pass before this can be saved.</span>
        )}
      </div>
    </div>
  )
}

const REACH_TONE: Record<Reach, 'ok' | 'warn' | 'bad' | 'unknown'> = {
  usable: 'ok',
  empty: 'warn',
  forbidden: 'bad',
  error: 'bad',
  unreachable: 'bad',
  unknown: 'unknown',
}

/** Step 2. Measure, then show exactly what was measured. */
function ReachStep() {
  const caps = useAllClusters()
  const refresh = useRefreshClusters()
  // Watch once a sweep has been asked for, and keep watching if one is
  // already running when this screen opens.
  const [watch, setWatch] = useState(false)
  const sweep = useSweepProgress(watch || refresh.isSuccess)
  const rows = caps.data ?? []
  const running = sweep.data?.sweeping ?? false
  const probed = sweep.data?.probed ?? 0
  const total = sweep.data?.total ?? 0

  return (
    <div className="space-y-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          onClick={() => {
            setWatch(true)
            refresh.mutate()
          }}
          disabled={refresh.isPending || running}
        >
          {refresh.isPending || running ? (
            <Loader2 aria-hidden className="size-3.5 animate-spin" />
          ) : (
            <RotateCw aria-hidden className="size-3.5" />
          )}
          {rows.length > 0 ? 'Measure again' : 'Measure now'}
        </Button>
        {/* Progress, not a spinner. Each cluster is published the moment it
            is measured, so there is something true to say the whole time. */}
        {running && (
          <span className="text-[0.6875rem] text-muted-foreground">
            Measured {probed} of {total}. A cluster that cannot be reached takes the full timeout to give up —
            the rest are already in the list below.
          </span>
        )}
        {!running && sweep.data && probed > 0 && (
          <span className="text-[0.6875rem] text-muted-foreground">All {total} measured.</span>
        )}
      </div>

      {rows.length > 0 ? (
        <ul className="divide-y divide-border rounded-md border border-border">
          {/* Usable first: the ones that matter should not be below eight
              timeouts, and the list is truncated. */}
          {[...rows]
            .sort((a, b) => Number(b.investigable ?? false) - Number(a.investigable ?? false))
            .slice(0, running ? rows.length : 8)
            .map((c) => (
              <li key={c.id} className="flex items-center gap-2 px-2.5 py-1.5">
                <Chip tone={REACH_TONE[c.reach ?? 'unknown']}>{c.reach ?? 'unknown'}</Chip>
                <span className="min-w-0 flex-1 truncate font-mono text-xs">{c.clusterName}</span>
                <span className="tabular shrink-0 text-[0.6875rem] text-muted-foreground">{c.latencyMs ?? 0}ms</span>
              </li>
            ))}
          {!running && rows.length > 8 && (
            <li className="px-2.5 py-1.5 text-[0.6875rem] text-muted-foreground">…and {rows.length - 8} more.</li>
          )}
        </ul>
      ) : (
        <p className="text-xs text-muted-foreground">
          Nothing measured yet. Until then the picker shows everything Devtron lists, warts and all.
        </p>
      )}
    </div>
  )
}

/** Step 3. Deliberately not editable here. */
function ModelStep() {
  return (
    <div className="space-y-2 text-xs leading-relaxed text-muted-foreground">
      <p>
        Model keys stay in the deployment environment rather than a database, so a UI session can never read one
        back out. Set it and restart:
      </p>
      <pre className="overflow-x-auto rounded-md border border-border bg-well p-2.5 font-mono text-[0.6875rem] text-foreground">
        <code>{`# .env\nANTHROPIC_API_KEY=sk-ant-...\n# or SRE_GEMINI_API_KEY=...`}</code>
      </pre>
      <p>
        The provider is detected from whichever key is present. Either agent can be pointed at any model id with{' '}
        <Code>SRE_MODELS_FAST</Code> and <Code>SRE_MODELS_STRONG</Code>.
      </p>
    </div>
  )
}
