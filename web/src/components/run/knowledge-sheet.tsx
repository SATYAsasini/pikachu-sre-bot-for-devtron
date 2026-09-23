import { useQuery } from '@tanstack/react-query'
import { BookOpen } from 'lucide-react'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Panel, PanelBody, PanelHeader, Well } from '@/components/common/panel'
import { Chip } from '@/components/common/status'
import { Mono } from '@/components/common/mono'
import { Markdown } from '@/components/common/markdown'
import { ErrorState } from '@/components/common/error-state'
import { TextSkeleton } from '@/components/common/skeletons'
import { api } from '@/lib/api'
import { qk } from '@/lib/queries'

/**
 * The knowledge pack behind a recognised component, as a side sheet.
 *
 * There are twenty of these in total, so a browsable screen would be a screen
 * nobody visits. It surfaces where it is actually wanted: off the identified
 * component in the verdict, and off any evidence row sourced from knowledge.
 *
 * The metrics table leads with `means` — what a change in the number tells an
 * SRE — because that is the insight. The metric name and type are the footnote.
 */
export function KnowledgeSheet({ componentId, onClose }: { componentId: string | null; onClose: () => void }) {
  const q = useQuery({
    queryKey: [...qk.knowledge(), 'component', componentId ?? ''],
    queryFn: () => api.knowledgeComponent(componentId as string),
    enabled: componentId !== null,
    staleTime: 5 * 60_000,
  })

  return (
    <Sheet open={componentId !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" data-lenis-prevent className="w-full gap-0 overflow-y-auto sm:max-w-xl">
        <SheetHeader className="border-b border-border">
          <SheetTitle className="flex items-center gap-2 text-sm">
            <BookOpen aria-hidden className="size-4 text-muted-foreground" />
            {q.data?.name ?? 'Knowledge pack'}
            {q.data ? <Chip tone="neutral">{q.data.class}</Chip> : null}
          </SheetTitle>
          <SheetDescription className="text-xs">
            {q.data?.summary ?? 'What we know about this component, read from source rather than invented.'}
          </SheetDescription>
          {q.data ? (
            <div className="flex flex-wrap items-center gap-2 pt-1">
              <Mono value={q.data.id} title="Component id" className="max-w-[12rem]" />
              {q.data.skill ? <Chip tone="neutral" mono>skill: {q.data.skill}</Chip> : null}
              {q.data.repo ? <Mono value={q.data.repo} title="Source repository" className="max-w-[16rem]" /> : null}
            </div>
          ) : null}
        </SheetHeader>

        <div className="space-y-3 p-4">
          {q.isLoading ? <TextSkeleton lines={8} /> : null}
          {q.isError ? <ErrorState error={q.error} onRetry={() => void q.refetch()} /> : null}

          {q.data ? (
            <>
              {(q.data.metrics ?? []).length > 0 ? (
                <Panel>
                  <PanelHeader title="Metrics that matter" description="What a move in each number actually tells you." />
                  <PanelBody className="space-y-2">
                    {(q.data.metrics ?? []).map((m) => (
                      <div key={m.name} className="border-b border-border pb-2 last:border-0 last:pb-0">
                        <p className="text-xs leading-relaxed">{m.means || m.help || 'No interpretation was recorded for this metric.'}</p>
                        <div className="mt-1 flex flex-wrap items-center gap-1.5">
                          <Mono value={m.name} className="max-w-[18rem]" title="Metric" />
                          {m.type ? <Chip tone="neutral">{m.type}</Chip> : null}
                          {(m.labels ?? []).map((l) => (
                            <Chip key={l} tone="neutral" mono>
                              {l}
                            </Chip>
                          ))}
                        </div>
                        {m.query ? (
                          <Well className="mt-1.5 font-mono text-[0.6875rem] break-all">{m.query}</Well>
                        ) : null}
                      </div>
                    ))}
                  </PanelBody>
                </Panel>
              ) : null}

              {q.data.doc ? (
                <Panel>
                  <PanelHeader title="Failure modes" />
                  <PanelBody>
                    <Markdown>{q.data.doc}</Markdown>
                  </PanelBody>
                </Panel>
              ) : null}
            </>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}
