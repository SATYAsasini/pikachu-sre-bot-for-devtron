import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { AlertTriangle, ArrowRight, Boxes, History, Settings2, Sparkles } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'motion/react'
import { cn } from 'cn'
import { useHoverZone } from '@/lib/use-hover-zone'
import { Panel, PanelBody, PanelHeader } from '@/components/common/panel'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { RowSkeleton } from '@/components/common/skeletons'
import { Chip } from '@/components/common/status'
import { Heading, Text } from '@/components/common/text'
import { Textarea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { AlertCard } from '@/components/alerts/alert-card'
import { AlertDetail } from '@/components/alerts/alert-detail'
import { DebugDialog } from '@/components/alerts/debug-dialog'
import { MonitoringPanel } from '@/components/scope/monitoring-panel'
import { AgentFrames } from '@/components/agent/agent-frames'
import { AiTrigger } from '@/components/agent/ai-trigger'
import type { Mood } from '@/components/agent/mood'
import { useAlerts, useMonitoring, useRuns } from '@/lib/queries'
import { alertKey, priorRunsByAlert } from '@/lib/prior-runs'
import { FilterBar } from '@/components/common/filter-bar'
import { Listing } from '@/components/common/listing'
import { ChatPanel } from '@/components/chat/chat-panel'
import { usePaged } from '@/lib/use-paged'
import { Segmented } from '@/components/common/segmented'
import { hasCluster, useScope } from '@/lib/scope'
import { useStartRun } from '@/lib/use-start-run'
import { useReadiness } from '@/lib/readiness'
import { SEARCH_EXPLAINS, SEARCH_HINT, SEARCH_MODES, matchesSearch, type SearchMode } from '@/lib/alert-meta'
import { relativeTime } from '@/lib/format'
import type { Alert, RunOptions } from '@/lib/types'

const ASK_EXAMPLES = [
  'Why did the last rollout of payments-api sit in Progressing for twenty minutes?',
  'Consumer lag on the settlement topic climbs every evening after 18:00.',
  'Checkout p99 doubled after Tuesday’s deploy and nobody knows why.',
]

/**
 * One screen: ask, or pick what is on fire.
 *
 * Investigate and Alerts used to be separate pages, which meant the first one
 * had a picker and no content while the second had content and no picker.
 * They are the same task — decide what to look at, then look — so they are one
 * page, and the cluster lives in the top bar where both halves can see it.
 */
export function NewRunPage() {
  const readiness = useReadiness()
  const { scope } = useScope()
  const ready = hasCluster(scope)
  const { start, pending } = useStartRun()

  const monitoring = useMonitoring(scope.clusterId)
  const alerts = useAlerts({ clusterId: scope.clusterId, limit: 200 }, ready)
  // A wider slice than the "Recent" strip needs, because it also answers
  // "has this alert already been investigated?" for every row in the list.
  // One query serves both rather than each alert row fetching for itself.
  const recent = useRuns({ limit: 100 })
  const prior = useMemo(() => priorRunsByAlert(recent.data ?? []), [recent.data])

  const [mode, setMode] = useState<SearchMode>('alert')
  const [query, setQuery] = useState('')
  const [detail, setDetail] = useState<Alert | null>(null)
  const [tuning, setTuning] = useState<Alert | null>(null)
  // A typed question opens a conversation. An alert still mints a run — the
  // cluster raised it, so it is worth a record; a passing thought is not.
  const [chatSeed, setChatSeed] = useState<string | null>(null)

  const list = useMemo(
    () => (alerts.data ?? []).filter((a) => matchesSearch(a, mode, query)),
    [alerts.data, mode, query],
  )

  // Twenty rows is a screen. A hundred is a document you scroll past looking
  // for where the page ends.
  const paged = usePaged(list, 20)

  // One number for the greeting. Severity spelling varies by alert source, so
  // it is matched loosely rather than compared to a constant.
  const critical = useMemo(
    () => (alerts.data ?? []).filter((a) => /crit|page|p0/i.test(a.severity ?? '')).length,
    [alerts.data],
  )

  // Setup is not finished: say which step, rather than showing an empty page.
  if (!readiness.loading && !readiness.ready) {
    const blocking = readiness.steps[readiness.activeIndex]
    return (
      <div className="mx-auto w-full max-w-2xl px-1 py-6">
        <Panel>
          <PanelBody className="space-y-3 py-8 text-center">
            <AgentFrames mood="idle" size={96} className="mx-auto" />
            <Heading level={1}>Not quite ready</Heading>
            <Text tone="muted" className="mx-auto max-w-sm">
              {blocking?.status} {blocking?.action}
            </Text>
            <Link
              to="/settings"
              className="inline-flex h-8 items-center rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              Step {readiness.activeIndex + 1}: {blocking?.title}
            </Link>
          </PanelBody>
        </Panel>
      </div>
    )
  }

  return (
    <div className="w-full space-y-4 pb-10">
      <AskHero
        busy={pending}
        hasCluster={ready}
        onSubmit={(ask) => setChatSeed(ask)}
        examples={ASK_EXAMPLES}
        clusterName={scope.clusterName}
        alertCount={alerts.data?.length}
        criticalCount={critical}
        loadingAlerts={alerts.isPending}
      />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_17rem]">
        {/* What is on fire, and one click to look at it. */}
        <Listing
          reserve="21rem"
          page={paged.page}
          pages={paged.pages}
          from={paged.from}
          to={paged.to}
          total={paged.total}
          noun="alert"
          onPage={paged.setPage}
          header={
            <PanelHeader
              title="Firing now"
              description={ready ? undefined : 'Pick a cluster in the bar above.'}
              icon={<AlertTriangle aria-hidden className="size-3.5" />}
              actions={
                ready && list.length > 0 ? (
                  <Chip tone={list.length > 20 ? 'warn' : 'neutral'}>{list.length}</Chip>
                ) : null
              }
            />
          }
          toolbar={
            ready && (alerts.data?.length ?? 0) > 0 ? (
              <FilterBar
              label="Filter by"
              scope={
                <Segmented
                  value={mode}
                  onChange={setMode}
                  options={SEARCH_MODES}
                  label="Which field to filter on"
                />
              }
              value={query}
              onChange={setQuery}
              placeholder={SEARCH_HINT[mode]}
              hint={SEARCH_EXPLAINS[mode]}
                count={list.length}
                total={alerts.data?.length}
                noun="alert"
              />
            ) : null
          }
        >
          {!ready ? (
              <EmptyState
                icon={ArrowRight}
                title="No cluster selected"
                line="Choose one in the bar above and whatever is alerting shows up here."
              />
            ) : alerts.isPending ? (
              <RowSkeleton rows={5} />
            ) : alerts.isError ? (
              <ErrorState error={alerts.error} onRetry={() => void alerts.refetch()} />
            ) : list.length === 0 ? (
              <EmptyState
                icon={AlertTriangle}
                title={query ? 'Nothing matches' : 'Nothing is firing'}
                line={
                  query
                    ? 'Try the resource or label mode — the alert you mean is often named nothing like you remember.'
                    : 'An alert source answered and had nothing for us. Enjoy it, or ask a question above.'
                }
              />
            ) : (
              /* One column, and each alert its own card rather than a row in a
                 divided list. Hairlines between rows made a hundred alerts
                 read as one long document — and an alert is the unit this
                 whole product is built around, so each one gets a boundary,
                 a surface and a shadow of its own. */
              <ul className="space-y-2">
                {paged.slice.map((a, i) => (
                  <li key={`${a.fingerprint || a.name}-${i}`} className="relative">
                    <AlertCard alert={a} onExpand={setDetail} active={detail === a} prior={prior.get(alertKey(a))} />
                    {/* One click investigates with sensible defaults; the
                        second button is for when you want to change them. */}
                    <div className="absolute top-1.5 right-1.5 flex gap-1 opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100 [li:hover_&]:opacity-100">
                      <Button
                        size="xs"
                        disabled={pending}
                        onClick={(e) => {
                          e.stopPropagation()
                          start({ alert: a })
                        }}
                      >
                        <Sparkles aria-hidden className="size-3" />
                        Debug
                      </Button>
                      <Button
                        size="xs"
                        variant="outline"
                        aria-label="Debug with options"
                        onClick={(e) => {
                          e.stopPropagation()
                          setTuning(a)
                        }}
                      >
                        <Settings2 aria-hidden className="size-3" />
                      </Button>
                    </div>
                  </li>
                ))}
            </ul>
          )}
        </Listing>

        {/* Context, kept narrow so it never competes with the list. */}
        <aside className="space-y-4">
          <AboutCallout />

          {ready && <MonitoringPanel clusterId={scope.clusterId as number} />}

          <Panel>
            <PanelHeader title="Recent" icon={<History aria-hidden className="size-3.5" />} />
            <PanelBody className="p-2">
              {recent.isPending ? (
                <RowSkeleton rows={3} />
              ) : (recent.data?.length ?? 0) === 0 ? (
                <Text tone="fine">Nothing investigated yet.</Text>
              ) : (
                <ul className="space-y-1">
                  {/* The query is wide so the alert list can use it; this
                      strip still shows three. */}
                  {(recent.data ?? []).slice(0, 3).map((r) => (
                    <li key={r.id}>
                      <Link
                        to="/runs/$runId"
                        params={{ runId: r.id }}
                        className="block rounded-md border border-transparent px-2 py-1.5 transition-colors hover:border-border hover:bg-well focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                      >
                        <span className="flex items-baseline gap-1.5">
                          <Chip tone={r.status === 'succeeded' ? 'ok' : r.status === 'running' ? 'warn' : 'neutral'}>
                            {r.status}
                          </Chip>
                          <span className="truncate text-[0.6875rem]">
                            {r.trigger.kind === 'alert' ? r.scope.clusterName : r.trigger.ask || r.scope.clusterName}
                          </span>
                        </span>
                        <span className="mt-0.5 block text-[0.625rem] text-muted-foreground">
                          {relativeTime(r.createdAt)}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </PanelBody>
          </Panel>
        </aside>
      </div>

      <AlertDetail
        alert={detail}
        open={detail !== null}
        onOpenChange={(v) => !v && setDetail(null)}
        onDebug={(a) => {
          setDetail(null)
          setTuning(a)
        }}
      />

      <ChatPanel
        open={chatSeed !== null}
        onClose={() => setChatSeed(null)}
        clusterId={scope.clusterId}
        clusterName={scope.clusterName}
        seed={chatSeed ?? undefined}
      />

      <DebugDialog
        alert={tuning}
        clusterName={scope.clusterName}
        open={tuning !== null}
        onOpenChange={(v) => !v && setTuning(null)}
        busy={pending}
        metricsAvailable={Boolean(monitoring.data?.metrics?.reachable)}
        logsAvailable={Boolean(monitoring.data?.alerts?.reachable)}
        onTrigger={(options: RunOptions) => {
          const a = tuning
          setTuning(null)
          if (a) start({ alert: a, options })
        }}
      />
    </div>
  )
}

/**
 * One curve for every part of the hero transition.
 *
 * The greeting, the mascot and the field all move at once; if any of them uses
 * a different duration the card appears to come apart mid-flight. Slower than
 * feels necessary on paper — a layout change this large reads as a glitch at
 * 300ms and as a deliberate motion at 700.
 */
const EASE = (still: boolean) =>
  still ? { duration: 0 } : ({ duration: 0.7, ease: [0.22, 1, 0.36, 1] } as const)

/**
 * The hero, in two states.
 *
 * Landing on a dense dashboard cold is disorienting: a hundred alert rows and
 * a text field tell you nothing about where you are or whether anything is
 * wrong. So the page opens as a greeting — one sentence, one number, the
 * mascot at full size — and collapses into the working layout the moment you
 * do anything: scroll, focus the field, or type.
 *
 * The collapse is one-way within a visit. Re-expanding under someone who has
 * started reading would be worse than never expanding at all.
 */
function AskHero({
  onSubmit,
  busy,
  hasCluster: ready,
  examples,
  clusterName,
  alertCount,
  criticalCount,
  loadingAlerts,
}: {
  onSubmit: (ask: string) => void
  busy?: boolean
  hasCluster: boolean
  examples: string[]
  clusterName?: string
  alertCount?: number
  criticalCount?: number
  loadingAlerts?: boolean
}) {
  const [value, setValue] = useState('')
  const [focused, setFocused] = useState(false)
  // Pointer position against a frozen rectangle, not mouseenter/mouseleave.
  // See use-hover-zone: the element shrinks when hovered, so the DOM events
  // fight themselves and no amount of delay fixes it.
  const [zoneRef, hovered] = useHoverZone<HTMLElement>(28)
  // Scrolling is the one thing that latches. Everything else is reversible:
  // approach the hero and it makes room to work, step away and the greeting
  // comes back. Once someone has scrolled into the alert list, though,
  // re-expanding the hero above them would shove the page under their cursor.
  const [scrolled, setScrolled] = useState(false)
  const typing = useTyping(value)
  const still = useReducedMotion()
  const [example] = useState(() => examples[Math.floor(Math.random() * examples.length)])
  const canSend = value.trim().length > 3 && ready && !busy
  const mood: Mood = busy ? 'working' : typing ? 'typing' : focused || hovered ? 'looking' : 'idle'

  // Any scroll at all means "I am here to work", so the greeting gets out of
  // the way. A threshold rather than zero, so a trackpad twitch does not do it.
  useEffect(() => {
    if (scrolled) return
    const onScroll = () => {
      if (window.scrollY > 24) setScrolled(true)
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [scrolled])

  // Derived, not an effect: the layout follows the pointer and the caret on
  // the same render rather than one after, so the transition starts with the
  // gesture instead of behind it.
  const big = !scrolled && !hovered && !focused && value === '' && !still

  return (
    <section
      ref={zoneRef}
      className={cn(
        'relative overflow-hidden rounded-2xl border bg-card shadow-card transition-colors',
        focused ? 'border-accent-strong/40' : 'border-border',
      )}
    >
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(30rem_16rem_at_10%_-25%,color-mix(in_oklab,var(--accent-strong)_9%,transparent),transparent)]"
      />

      <motion.div
        layout
        transition={EASE(Boolean(still))}
        className={cn('relative flex items-stretch gap-4', big ? 'p-6 sm:p-8' : 'p-4')}
      >
        {/* The mascot is the anchor of both states, so it is one element that
            resizes rather than two that swap — the character never blinks out
            of existence mid-transition. */}
        <motion.div
          layout
          transition={EASE(Boolean(still))}
          // Centred in the card, not stood on its floor. Bottom-aligned it
          // left a pocket of dead space above its ears in both states, which is
          // what made the hero look unfinished; centred, the character reads as
          // sitting in the panel rather than propped at the edge of it.
          className="hidden shrink-0 flex-col items-center justify-center sm:flex"
        >
          <AgentFrames mood={mood} size={big ? 168 : 104} aware={hovered || focused || typing} />
        </motion.div>

        <div className="flex min-w-0 flex-1 flex-col justify-center">
          <AnimatePresence mode="popLayout" initial={false}>
            {big ? (
              <motion.div
                key="welcome"
                layout
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, transition: { duration: 0.2 } }}
                transition={{ duration: 0.45, ease: [0.22, 1, 0.36, 1], delay: 0.05 }}
                className="mb-4"
              >
                <Greeting
                  ready={ready}
                  loading={loadingAlerts}
                  clusterName={clusterName}
                  alertCount={alertCount}
                  criticalCount={criticalCount}
                />
              </motion.div>
            ) : (
              <motion.div
                key="compact"
                layout
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.35, ease: [0.22, 1, 0.36, 1], delay: 0.05 }}
              >
                <Heading level={1}>Ask, or pick something that is on fire.</Heading>
                <Text tone="muted" className="mt-0.5">
                  One question, one investigation. Nothing is remembered between asks — each run stands on its
                  own evidence.
                </Text>
              </motion.div>
            )}
          </AnimatePresence>

          <motion.div layout className={cn('relative', big ? 'mt-0' : 'mt-3')}>
            <Textarea
              value={value}
              onChange={(e) => setValue(e.target.value)}
              onFocus={() => setFocused(true)}
              onBlur={() => setFocused(false)}
              onKeyDown={(e) => {
                if (e.key !== 'Enter') return
                // Mid-composition Enter commits the candidate in a Japanese,
                // Chinese or Korean IME. Sending the message there would eat
                // the word the person was still choosing.
                if (e.nativeEvent.isComposing) return
                // Shift+Enter is the newline, everything else sends — the
                // convention every chat input uses. Requiring Cmd+Enter was
                // the reason pressing Enter appeared to do nothing at all.
                if (e.shiftKey) return
                e.preventDefault()
                if (!canSend) return
                onSubmit(value.trim())
                setValue('')
              }}
              placeholder={example}
              rows={big ? 1 : 2}
              className={cn(
                'resize-none border-border bg-background transition-[height,font-size]',
                big ? 'min-h-12 py-3 text-base' : 'text-sm',
              )}
            />
          </motion.div>

          {/* The trigger reads its own label rather than hiding it in a
              tooltip. It needs a row of its own to do that: inside the field
              it was wide enough to sit on the border, and as an orb it said
              nothing until you hovered it. */}
          <motion.div layout className="mt-3 flex flex-wrap items-center gap-3 pb-0.5">
            <Text tone="fine" as="span">
              {ready ? (
                <>
                  <kbd className="rounded border border-border bg-well px-1 font-mono">↵</kbd> to send ·{' '}
                  <kbd className="rounded border border-border bg-well px-1 font-mono">⇧</kbd>
                  <kbd className="ml-0.5 rounded border border-border bg-well px-1 font-mono">↵</kbd> for a new line
                </>
              ) : (
                'Pick a cluster in the bar above — a run needs somewhere to look.'
              )}
            </Text>
            <AiTrigger
              size="sm"
              disabled={!canSend}
              onClick={() => {
                onSubmit(value.trim())
                setValue('')
              }}
              className="ml-auto"
            >
              Trigger debug with AI
            </AiTrigger>
          </motion.div>
        </div>
      </motion.div>
    </section>
  )
}

/**
 * The welcome line.
 *
 * Two numbers at most. The temptation is to summarise everything the page
 * knows, and the result is a paragraph nobody reads — "how much is on fire,
 * and where" is the whole of what someone arriving needs.
 */
function Greeting({
  ready,
  loading,
  clusterName,
  alertCount,
  criticalCount,
}: {
  ready: boolean
  loading?: boolean
  clusterName?: string
  alertCount?: number
  criticalCount?: number
}) {
  if (!ready) {
    return (
      <>
        <p className="font-display text-2xl leading-tight font-semibold tracking-tight sm:text-3xl">
          Pick a cluster and I will take a look.
        </p>
        <Text tone="muted" className="mt-1.5">
          Choose one in the bar above. I only offer the ones I can actually read.
        </Text>
      </>
    )
  }

  if (loading || alertCount === undefined) {
    return (
      <>
        <p className="font-display text-2xl leading-tight font-semibold tracking-tight sm:text-3xl">
          Counting what is on fire…
        </p>
        <Text tone="muted" className="mt-1.5">
          Reading the alert source on {clusterName ?? 'this cluster'}.
        </Text>
      </>
    )
  }

  const quiet = alertCount === 0

  return (
    <>
      <p className="font-display text-2xl leading-tight font-semibold tracking-tight sm:text-3xl">
        {quiet ? (
          <>Nothing is firing on {clusterName}.</>
        ) : (
          <>
            <span className="tabular text-accent-strong">{alertCount}</span> alert
            {alertCount === 1 ? '' : 's'} firing on {clusterName}.
          </>
        )}
      </p>
      <Text tone="muted" className="mt-1.5">
        {quiet
          ? 'Quiet is a good state. Ask me anything anyway — I can look at something that is not alerting.'
          : criticalCount
            ? `${criticalCount} of them critical. Pick one below, or ask me something.`
            : 'None critical. Pick one below, or ask me something.'}
      </Text>
    </>
  )
}

/**
 * The way in to the About page.
 *
 * That page is the point of this build — what it took to make the agent, and
 * what a builder platform would have to standardise — and it was reachable
 * only by opening Settings and finding a second tab. Anything two clicks deep
 * with no signpost is a page nobody reads, so it gets a card that says what is
 * behind it and goes straight there.
 */
function AboutCallout() {
  return (
    <Link
      to="/settings"
      search={{ section: 'about' as const, panel: 'overview' }}
      className={cn(
        'group block overflow-hidden rounded-xl border-2 border-accent-strong/35 bg-accent-strong/8 p-3 shadow-card',
        'transition-all hover:-translate-y-px hover:border-accent-strong/60 hover:shadow-raised',
        'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
      )}
    >
      <span className="flex items-center gap-2">
        <Boxes aria-hidden className="size-4 shrink-0 text-accent-strong" />
        <span className="text-xs font-semibold">How this agent works</span>
        <ArrowRight
          aria-hidden
          className="ml-auto size-3.5 shrink-0 text-accent-strong transition-transform group-hover:translate-x-0.5"
        />
      </span>
      <Text tone="fine" className="mt-1.5">
        The two agents and their prompts, the whole thing as one YAML file, a diagram of who owns each step, and
        what an agent-builder platform would have to normalise before this could be assembled rather than
        written.
      </Text>
      <span className="mt-2 flex flex-wrap gap-1">
        {['Agents', 'Configuration', 'Data flow', 'APIs → tools', 'Findings'].map((t) => (
          <span key={t} className="rounded border border-accent-strong/25 bg-card px-1.5 py-0.5 text-[0.625rem] text-muted-foreground">
            {t}
          </span>
        ))}
      </span>
    </Link>
  )
}

/** True while keystrokes are still arriving. */
function useTyping(value: string): boolean {
  const [typing, setTyping] = useState(false)
  const first = useRef(true)
  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    setTyping(true)
    const t = window.setTimeout(() => setTyping(false), 700)
    return () => window.clearTimeout(t)
  }, [value])
  return typing
}
