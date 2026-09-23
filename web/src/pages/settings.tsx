import { useNavigate, useSearch } from '@tanstack/react-router'
import { Boxes, Cable, CheckCircle2 } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from 'cn'
import { Panel, PanelBody, PanelHeader, Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Code } from '@/components/common/mono'
import { Heading, Text } from '@/components/common/text'
import { RowSkeleton } from '@/components/common/skeletons'
import { SetupFlow } from '@/components/setup/setup-flow'
import { HarnessView } from '@/components/setup/harness-view'
import { TokenPermissions } from '@/components/setup/token-permissions'
import { useReadiness } from '@/lib/readiness'
import type { SettingsSection } from '@/lib/settings-sections'

const SECTIONS: {
  key: SettingsSection
  label: string
  icon: LucideIcon
  blurb: string
}[] = [
  {
    key: 'configuration',
    label: 'Configuration',
    icon: Cable,
    blurb: 'Host, token, cluster reach and the model credential.',
  },
  {
    key: 'about',
    label: 'About the agent',
    icon: Boxes,
    blurb:
      'The agent tree, its tools and its prompts, exactly as the binary holds them.',
  },
]

/**
 * Settings, as two things rather than one scroll.
 *
 * It used to be a single 2xl-wide column: setup at the top, the architecture
 * below it, and on a wide screen two thirds of the page was empty margin while
 * the YAML was squeezed into the remaining third. Configuration and reference
 * are different jobs — one you do once, the other you come back to read — so
 * they are separate destinations behind a rail, and the content uses the same
 * width as the rest of the product.
 */
export function SettingsPage() {
  const { ready, loading, steps } = useReadiness()
  const navigate = useNavigate()
  // The section is URL state, not component state: 'How this agent works' on
  // the landing page links straight to it, and a page you can only reach by
  // clicking twice is a page nobody reads.
  const { section = 'configuration', panel } = useSearch({ from: '/settings' })
  const setSection = (next: SettingsSection) =>
    void navigate({
      to: '/settings',
      search: next === 'about' ? { section: next, panel } : { section: next },
    })
  const doneCount = steps.filter((s) => s.state === 'done').length
  const active = SECTIONS.find((s) => s.key === section) ?? SECTIONS[0]

  return (
    <div className="w-full">
      <div className="grid gap-6 lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-8">
        {/* Sticky, because the About pane is long and losing the way back to
            Configuration halfway down it is annoying. */}
        <nav
          aria-label="Settings sections"
          className="lg:sticky lg:top-[4.5rem] lg:self-start"
        >
          <ul className="flex gap-1 lg:flex-col">
            {SECTIONS.map(({ key, label, icon: Icon }) => {
              const on = key === section
              return (
                <li key={key} className="min-w-0 flex-1 lg:flex-none">
                  <button
                    type="button"
                    onClick={() => setSection(key)}
                    aria-current={on ? 'page' : undefined}
                    className={cn(
                      'flex w-full items-center gap-2.5 rounded-lg border px-3 py-2.5 text-left text-sm font-medium transition-colors',
                      'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                      on
                        ? 'border-accent-strong/40 bg-accent-strong/10 text-foreground'
                        : 'border-transparent text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                    )}
                  >
                    <Icon aria-hidden className="size-4 shrink-0" />
                    <span className="min-w-0 flex-1 truncate">{label}</span>
                    {key === 'configuration' && !loading ? (
                      <span className="shrink-0">
                        {ready ? (
                          <Chip
                            tone="ok"
                            icon={
                              <CheckCircle2 aria-hidden className="size-3" />
                            }
                          >
                            ready
                          </Chip>
                        ) : (
                          <Chip tone="warn">
                            {doneCount}/{steps.length}
                          </Chip>
                        )}
                      </span>
                    ) : null}
                  </button>
                </li>
              )
            })}
          </ul>
        </nav>

        <div className="min-w-0">
          <header className="mb-5 border-b border-border pb-3">
            <Heading level={1} className="text-lg">
              {active.label}
            </Heading>
            <Text tone="muted" className="mt-1">
              {active.blurb}
            </Text>
          </header>

          {section === 'configuration' ? (
            <ConfigurationSection loading={loading} ready={ready} />
          ) : (
            <HarnessView />
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Setup as an ordered journey.
 *
 * This was a flat form that let an operator save a host they had never tested
 * and then land on an empty dashboard with no idea which of three missing
 * prerequisites was at fault. It is the sequence itself, and it stays the
 * sequence after setup — completed steps collapse to a line of fact you can
 * reopen.
 */
function ConfigurationSection({
  loading,
  ready,
}: {
  loading: boolean
  ready: boolean
}) {
  return (
    <div className="space-y-5">
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_21rem]">
        <Panel className="min-w-0">
          <PanelHeader
            title={ready ? 'Three prerequisites' : 'Finish setting up'}
            description={
              ready
                ? 'All three hold. Reopen any step to change it.'
                : 'Three things have to be true before an investigation can succeed. They are in order.'
            }
          />
          <PanelBody>
            {loading ? <RowSkeleton rows={3} /> : <SetupFlow />}
          </PanelBody>
        </Panel>

        {/* The notes that used to sit under the panel as one grey paragraph.
          Beside it they fill the width instead of leaving it. */}
        <aside className="space-y-3">
          <TokenPermissions />

          <Well>
            <Text tone="label">Precedence</Text>
            <Text tone="muted" className="mt-1">
              What you save here overrides <Code>SRE_DEVTRON_URL</Code> and{' '}
              <Code>SRE_DEVTRON_TOKEN</Code> and takes effect immediately — runs
              already in flight keep the credentials they started with.
            </Text>
          </Well>
          <Well>
            <Text tone="label">Changing the host</Text>
            <Text tone="muted" className="mt-1">
              Discards every cluster measurement. Reach measured against one
              installation says nothing about another, and a stale answer is
              worse than none.
            </Text>
          </Well>
          <Well>
            <Text tone="label">One token, three placements</Text>
            <Text tone="muted" className="mt-1">
              <Code>token:</Code> for the orchestrator,{' '}
              <Code>Authorization: Bearer</Code> for the Kubernetes proxy,{' '}
              <Code>Cookie: argocd.token</Code> for Intelligence. Getting this
              wrong is the usual 401.
            </Text>
          </Well>
        </aside>
      </div>
    </div>
  )
}
