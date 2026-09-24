import { ScopeSelect } from '@/components/layout/scope-select'
import { NAV } from '@/lib/nav'
import { Link, useRouterState } from '@tanstack/react-router'
import { FlaskConical, X } from 'lucide-react'
import { cn } from 'cn'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { Chip, Dot } from '@/components/common/status'
import { ThemeToggle } from '@/components/layout/theme-toggle'
import { useConfig, useHealth } from '@/lib/queries'
import { hasCluster, useScope } from '@/lib/scope'
import { isFixtureMode, setFixtureMode } from '@/lib/fixtures'

/**
 * What is left of the chrome once navigation moved to the left.
 *
 * This bar now holds only the things that change what you are looking at —
 * which cluster, is the agent up, light or dark — plus the nav itself on
 * narrow screens where a sidebar would eat half the viewport. Keeping
 * destinations and controls in one row was the reason neither read as either.
 */
export function TopBar() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-background/85 backdrop-blur supports-[backdrop-filter]:bg-background/70">
      <div className="flex h-12 items-center gap-1 px-3 sm:px-4">
        {/* The sidebar carries these from lg up. Below that it is gone, so
            they come back here as icons. */}
        <Link
          to="/"
          aria-label="Pikachu SRE bot for Devtron, home"
          className="mr-1 flex shrink-0 items-center gap-2 rounded focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none lg:hidden"
        >
          <img
            src="/mark.png"
            alt=""
            aria-hidden
            width={32}
            height={32}
            className="size-8 shrink-0 rounded-full object-cover ring-1 ring-border"
          />
        </Link>

        <nav className="flex items-center gap-0.5 lg:hidden" aria-label="Main">
          {NAV.map(({ to, label, icon: Icon, exact }) => {
            const active = exact ? pathname === to : pathname.startsWith(to)
            return (
              <Link
                key={to}
                to={to}
                aria-current={active ? 'page' : undefined}
                title={label}
                className={cn(
                  'inline-flex h-8 items-center gap-1.5 rounded-md border px-2 text-xs font-medium transition-colors',
                  'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                  active
                    ? 'border-accent-strong/40 bg-accent-strong/12 text-foreground'
                    : 'border-transparent text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                )}
              >
                <Icon aria-hidden className="size-4" />
                <span className="sr-only sm:not-sr-only">{label}</span>
              </Link>
            )
          })}
        </nav>

        {/* Scope lives in the chrome so every page has one. It is the most
            consequential control here, so it leads. */}
        <div className="ml-1 hidden md:block">
          <ScopeSelect />
        </div>

        <div className="ml-auto flex min-w-0 items-center gap-1.5">
          <ScopeChip />
          <FixtureChip />
          <ApiStatus />
          <ThemeToggle />
        </div>
      </div>
    </header>
  )
}

/** The only piece of persistent state in the chrome: where runs will be created. */
function ScopeChip() {
  const { scope, clearScope } = useScope()
  if (!hasCluster(scope)) return null

  const parts = [scope.clusterName, scope.namespace ?? scope.environmentName, scope.appName].filter(Boolean) as string[]
  // The cluster is not clearable — every screen needs one — so the button
  // only appears when there is a narrowing to undo.
  const narrowed = parts.length > 1

  return (
    <div className="hidden min-w-0 items-center md:flex">
      <Tooltip>
        <TooltipTrigger asChild>
          <Link
            to="/"
            className={cn(
              'inline-flex h-6 min-w-0 items-center gap-1 border border-border bg-well pr-1 pl-1.5 font-mono text-[0.6875rem] text-muted-foreground transition-colors hover:text-foreground',
              narrowed ? 'rounded-l border-r-0' : 'rounded',
            )}
          >
            {parts.map((p, i) => (
              <span key={`${p}-${i}`} className="flex min-w-0 items-center gap-1">
                {i > 0 ? <span className="text-border">/</span> : null}
                <span className="max-w-[10rem] truncate">{p}</span>
              </span>
            ))}
          </Link>
        </TooltipTrigger>
        <TooltipContent>Runs are created in this scope. Click to change it.</TooltipContent>
      </Tooltip>
      {narrowed ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={clearScope}
              aria-label="Widen to the whole cluster"
              className="inline-flex h-6 items-center rounded-r border border-border bg-well px-1 text-muted-foreground transition-colors hover:text-bad"
            >
              <X aria-hidden className="size-3" />
            </button>
          </TooltipTrigger>
          <TooltipContent>Widen back to the whole cluster</TooltipContent>
        </Tooltip>
      ) : null}
    </div>
  )
}

/** Loud on purpose: nobody should mistake fixture data for their cluster. */
function FixtureChip() {
  if (!isFixtureMode()) return null
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" onClick={() => setFixtureMode(false)} aria-label="Fixture mode is on. Switch back to the live API.">
          <Chip tone="warn" icon={<FlaskConical aria-hidden className="size-3" />} mono>
            FIXTURES
          </Chip>
        </button>
      </TooltipTrigger>
      <TooltipContent>Nothing here is real. Click to go back to the live API.</TooltipContent>
    </Tooltip>
  )
}

/** Health of our own backend, not of the cluster. One dot, one tooltip. */
function ApiStatus() {
  const health = useHealth()
  const config = useConfig()

  const tone = health.isLoading ? 'unknown' : health.isError ? 'bad' : health.data?.status === 'ok' ? 'ok' : 'warn'
  const text = health.isLoading
    ? 'Checking the agent…'
    : health.isError
      ? 'The agent is not answering /v1/healthz. Everything on this page will be stale or empty.'
      : 'Agent is up.'

  const models = config.data?.models

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="API status" className="cursor-help">
          <Dot tone={tone} pulse={tone === 'ok'} />
        </Button>
      </TooltipTrigger>
      <TooltipContent className="max-w-80">
        <div>{text}</div>
        {config.data ? (
          <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 opacity-80">
            <dt>Devtron</dt>
            <dd className="font-mono break-all">{config.data.devtronUrl || '—'}</dd>
            {/* Opaque strings: shown, never branched on. */}
            <dt>Provider</dt>
            <dd className="font-mono">{models?.provider || '—'}</dd>
            <dt>Fast</dt>
            <dd className="font-mono break-all">{models?.fast || '—'}</dd>
            <dt>Strong</dt>
            <dd className="font-mono break-all">{models?.strong || '—'}</dd>
          </dl>
        ) : null}
      </TooltipContent>
    </Tooltip>
  )
}
