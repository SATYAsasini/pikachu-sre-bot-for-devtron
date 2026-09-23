import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { History, RotateCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { PanelHeader } from '@/components/common/panel'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { RowSkeleton } from '@/components/common/skeletons'
import { Chip } from '@/components/common/status'
import { Listing } from '@/components/common/listing'
import { usePaged } from '@/lib/use-paged'
import { RunRow } from '@/components/runs/run-row'
import { useClusters, useRuns } from '@/lib/queries'
import { RUN_STATUSES, type RunStatus } from '@/lib/types'

const ANY = '__any__'

export interface RunsSearch {
  status?: RunStatus
  clusterId?: number
}

/** Screen 4 — everything that has been asked, newest first. */
export function RunHistoryPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs' })
  const clusters = useClusters()
  const runs = useRuns({ status: search.status, clusterId: search.clusterId, limit: 200 })

  const setSearch = (patch: Partial<RunsSearch>) => {
    void navigate({ to: '/runs', search: (prev: RunsSearch) => ({ ...prev, ...patch }), replace: true })
  }

  const list = runs.data ?? []
  // Twenty-five rows is a screen; the query fetches up to 200.
  const paged = usePaged(list, 25)
  const filtered = search.status !== undefined || search.clusterId !== undefined

  return (
    <div className="w-full">
      <Listing
        reserve="9rem"
        page={paged.page}
        pages={paged.pages}
        from={paged.from}
        to={paged.to}
        total={paged.total}
        noun="run"
        onPage={paged.setPage}
        header={
          <PanelHeader
            title="Run history"
            description="Every investigation, and what it concluded."
            icon={<History aria-hidden className="size-3.5" />}
            actions={
              <>
                {list.length > 0 ? <Chip tone="neutral">{list.length}</Chip> : null}
                <Button size="icon-xs" variant="ghost" onClick={() => void runs.refetch()} aria-label="Refresh runs">
                  <RotateCw aria-hidden className={runs.isFetching ? 'animate-spin' : undefined} />
                </Button>
              </>
            }
          />
        }
        toolbar={
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
          <Select
            value={search.status ?? ANY}
            onValueChange={(v) => setSearch({ status: v === ANY ? undefined : (v as RunStatus) })}
          >
            <SelectTrigger size="sm" className="text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              {RUN_STATUSES.map((s) => (
                <SelectItem key={s} value={s}>
                  {s.replace('_', ' ')}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select
            value={search.clusterId !== undefined ? String(search.clusterId) : ANY}
            onValueChange={(v) => setSearch({ clusterId: v === ANY ? undefined : Number(v) })}
          >
            <SelectTrigger size="sm" className="text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any cluster</SelectItem>
              {(clusters.data ?? []).map((c) => (
                <SelectItem key={c.id} value={String(c.id)}>
                  <span className="font-mono text-xs">{c.clusterName}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

            {filtered ? (
              <Button size="sm" variant="ghost" className="text-xs" onClick={() => setSearch({ status: undefined, clusterId: undefined })}>
                Clear
              </Button>
            ) : null}
          </div>
        }
      >
        {runs.isLoading ? (
          <RowSkeleton rows={6} />
        ) : runs.isError ? (
          <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={History}
            title={filtered ? 'Nothing matches those filters' : 'No runs yet'}
            line={
              filtered
                ? 'Clear the filters and see what is actually in here.'
                : 'Nothing has been investigated. The history stays empty until something breaks, which is the good outcome.'
            }
            action={
              <Button asChild size="sm" variant={filtered ? 'outline' : 'default'}>
                <Link to={filtered ? '/runs' : '/'}>{filtered ? 'Clear filters' : 'Start a run'}</Link>
              </Button>
            }
          />
        ) : (
          // Same rule as the alert list: every run is its own card, because a
          // divided list of thirty reads as one document.
          <ul className="space-y-2">
            {paged.slice.map((run) => (
              <li key={run.id} className="overflow-hidden rounded-xl border border-border bg-card shadow-card transition-all hover:-translate-y-px hover:border-accent-strong/35 hover:shadow-raised">
                <RunRow run={run} />
              </li>
            ))}
          </ul>
        )}
      </Listing>
    </div>
  )
}
