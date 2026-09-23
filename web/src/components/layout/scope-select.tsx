import { Check, ChevronsUpDown, Server } from 'lucide-react'
import { cn } from 'cn'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useClusters } from '@/lib/queries'
import { useScope } from '@/lib/scope'

/**
 * The cluster, chosen once, in the bar.
 *
 * It used to live on the landing page only, which meant Alerts and History
 * could render with no cluster set and nothing to show — a page that looks
 * broken but is merely unscoped, offering a button that sends you somewhere
 * else to fix it. Scope is a property of the whole session, so it belongs in
 * the chrome where every page can see and change it.
 */
export function ScopeSelect() {
  const clusters = useClusters()
  const { scope, setScope } = useScope()
  const list = clusters.data ?? []
  const current = list.find((c) => c.id === scope.clusterId)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          'inline-flex h-7 max-w-[13rem] items-center gap-1.5 rounded-md border px-2 text-xs transition-colors',
          'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
          current
            ? 'border-border bg-card text-foreground hover:bg-accent/60'
            : 'border-warn/40 bg-warn/10 text-warn hover:bg-warn/15',
        )}
        aria-label={current ? `Cluster: ${current.clusterName}. Change.` : 'Choose a cluster'}
      >
        <Server aria-hidden className="size-3.5 shrink-0" />
        <span className="truncate font-mono">{current ? current.clusterName : 'no cluster'}</span>
        <ChevronsUpDown aria-hidden className="size-3 shrink-0 opacity-60" />
      </DropdownMenuTrigger>

      <DropdownMenuContent align="start" className="min-w-56">
        <DropdownMenuLabel className="text-[0.625rem] tracking-widest uppercase">
          {clusters.isPending ? 'Loading…' : `${list.length} ready to investigate`}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {list.length === 0 && !clusters.isPending ? (
          <DropdownMenuItem disabled className="text-xs">
            Nothing reachable — check Settings
          </DropdownMenuItem>
        ) : (
          list.map((c) => (
            <DropdownMenuItem
              key={c.id}
              onSelect={() => setScope({ clusterId: c.id, clusterName: c.clusterName })}
              className="text-xs"
            >
              <span className="flex-1 truncate font-mono">{c.clusterName}</span>
              {c.id === scope.clusterId && <Check aria-hidden className="size-3.5 text-ok" />}
            </DropdownMenuItem>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
