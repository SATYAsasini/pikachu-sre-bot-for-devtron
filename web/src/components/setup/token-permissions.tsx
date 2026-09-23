import { KeyRound, Lock, ShieldCheck } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Code } from '@/components/common/mono'
import { Text } from '@/components/common/text'

/**
 * Exactly what the API token has to be allowed to do.
 *
 * Anyone minting a token in Devtron has to decide what to tick, and "give it
 * access" is not an answer they can act on — so this lists the real calls, the
 * real Kubernetes kinds, and the one thing that matters more than any of it:
 * every single call is a read.
 *
 * The two POSTs are called out on purpose. `app/list/v2` and
 * `k8s/resource/list` are reads that take a body, and someone reviewing a
 * permission grant will reasonably assume a POST writes something.
 */

const SCOPES: { area: string; permission: string; calls: string[] }[] = [
  {
    area: 'Clusters & environments',
    permission: 'View on every cluster you want investigated',
    calls: [
      'GET /orchestrator/cluster/autocomplete',
      'GET /orchestrator/env/autocomplete/helm',
    ],
  },
  {
    area: 'Devtron apps',
    permission: 'View on the apps in those environments',
    calls: [
      'GET /orchestrator/app/autocomplete',
      'POST /orchestrator/app/list/v2',
    ],
  },
  {
    area: 'Helm apps',
    permission: 'View on Helm releases in those environments',
    calls: ['GET /orchestrator/env/autocomplete/helm'],
  },
  {
    area: 'Kubernetes resources',
    permission:
      'View on the kinds listed below, in those clusters and namespaces',
    calls: ['POST /orchestrator/k8s/resource/list'],
  },
  {
    area: 'Monitoring (Prometheus, Alertmanager, vmalert)',
    permission: 'Reach the monitoring Services through the cluster proxy',
    calls: ['GET /orchestrator/k8s/proxy/cluster/{id}/…'],
  },
  {
    area: 'Devtron Intelligence',
    permission: 'Access to the Athena proxy',
    calls: ['POST /proxy/athena/intelligence'],
  },
]

/** Every kind the agent ever asks for. Nothing outside this list is requested. */
const KINDS = [
  'Pod',
  'Deployment',
  'StatefulSet',
  'DaemonSet',
  'ReplicaSet',
  'Job',
  'Service',
  'Ingress',
  'Event',
  'Node',
  'PersistentVolumeClaim',
  'ConfigMap',
  'ServiceMonitor',
  'PodMonitor',
  'VMServiceScrape',
  'VMPodScrape',
]

/**
 * A line, not a screen.
 *
 * Spelled out in full this is six scopes, sixteen kinds and three caveats —
 * true, and needed exactly once, by whoever is minting the token. Left open on
 * the page it was the largest thing in Settings and pushed the actual setup
 * steps off the top. So it collapses to one row that states the only fact most
 * readers want — it is read-only — and opens the detail on demand.
 */
export function TokenPermissions() {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <button
          type="button"
          className={[
            'flex w-full items-center gap-2 rounded-lg border border-border bg-card px-2.5 py-2 text-left',
            'transition-colors hover:border-accent-strong/40',
            'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
          ].join(' ')}
        >
          <KeyRound
            aria-hidden
            className="size-3.5 shrink-0 text-muted-foreground"
          />
          <span className="min-w-0 flex-1">
            <span className="block text-xs font-semibold">
              What the token needs
            </span>
            <span className="mt-0.5 block text-[0.625rem] text-muted-foreground">
              Six scopes, {KINDS.length} Kubernetes kinds
            </span>
          </span>
          <Chip
            tone="ok"
            icon={<ShieldCheck aria-hidden className="size-3" />}
            className="shrink-0"
          >
            read-only
          </Chip>
        </button>
      </DialogTrigger>

      <DialogContent
        className="max-h-[85svh] max-w-3xl overflow-y-auto"
        data-lenis-prevent
      >
        <DialogHeader>
          <DialogTitle className="text-sm">What the token needs</DialogTitle>
          <DialogDescription className="text-xs">
            All read. The agent has no write tools and none can be added.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <ul className="divide-y divide-border overflow-hidden rounded-lg border border-border">
            {SCOPES.map((s) => (
              <li key={s.area} className="px-2.5 py-2">
                <div className="flex flex-wrap items-baseline gap-x-2">
                  <span className="text-xs font-semibold">{s.area}</span>
                  <span className="text-xs text-muted-foreground">
                    {s.permission}
                  </span>
                </div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {s.calls.map((c) => (
                    <span
                      key={c}
                      className="rounded border border-border bg-well px-1 font-mono text-[0.625rem] text-muted-foreground"
                    >
                      {c}
                    </span>
                  ))}
                </div>
              </li>
            ))}
          </ul>

          <div>
            <Text tone="label">Kubernetes kinds it reads</Text>
            <div className="mt-1 flex flex-wrap gap-1">
              {KINDS.map((k) => (
                <span
                  key={k}
                  className="rounded border border-border bg-well px-1.5 py-0.5 font-mono text-[0.625rem]"
                >
                  {k}
                </span>
              ))}
            </div>
            <Text tone="fine" className="mt-1.5">
              Nothing outside this list is ever requested. The last four are
              only read to work out whether a workload is being scraped at all.
            </Text>
          </div>

          <Well className="space-y-1.5">
            <div className="flex items-center gap-1.5">
              <Lock aria-hidden className="size-3 shrink-0 text-ok" />
              <Text tone="label" as="span">
                Never requested
              </Text>
            </div>
            <Text tone="muted">
              <Code>Secret</Code> is not in the list above and is not read
              anywhere in the codebase. Neither is any write, patch, delete,
              scale, restart or exec — the Kubernetes proxy is used{' '}
              <em>GET-only</em>, and the two <Code>POST</Code>s above are reads
              that happen to take a body.
            </Text>
          </Well>

          <Well>
            <Text tone="label">One token, three placements</Text>
            <Text tone="muted" className="mt-1">
              The same token is sent three ways, and getting this wrong is the
              usual 401: <Code>token:</Code> for <Code>/orchestrator/*</Code>,{' '}
              <Code>Authorization: Bearer</Code> for{' '}
              <Code>/orchestrator/k8s/proxy/*</Code> only, and{' '}
              <Code>Cookie: argocd.token</Code> for{' '}
              <Code>/proxy/athena/intelligence</Code> only.
            </Text>
          </Well>

          <Text tone="fine">
            A narrower token still works — it simply sees fewer clusters.
            Settings measures what this token can actually read and the cluster
            picker only offers those, so an under-scoped token shows up as a
            short list rather than as a run that fails halfway.
          </Text>
        </div>
      </DialogContent>
    </Dialog>
  )
}
