import { useState } from 'react'
import { MousePointerClick } from 'lucide-react'
import { cn } from 'cn'
import { Heading, Text } from '@/components/common/text'
import type { Harness, HarnessTool } from '@/lib/types'

/**
 * Where the work happens, and who owns it.
 *
 * The question this diagram answers is the one the whole page is about: of
 * everything a run does, how much came from outside and how much we had to
 * build? So it is drawn as three bands rather than as a flowchart — systems we
 * call, the harness we wrote, and the two agents — with the pipeline running
 * left to right through the middle of them.
 *
 * It is generated from the same `/v1/harness` payload as the YAML above, so a
 * step added to the pipeline appears here without anyone redrawing anything.
 * Hovering a step lights only the edges that step actually uses, which is the
 * fastest way to answer "what does `facts` talk to".
 *
 * SVG with a viewBox rather than divs: the connectors are curves between
 * arbitrary points, and that is the one thing CSS is genuinely bad at.
 */

const W = 1240
const H = 560

/** Band centre-lines. */
const Y_OUT = 92
const Y_STEP = 300
const Y_HARNESS = 470

const STEP_W = 168
const STEP_H = 74

/** Which external system each kind of step reaches for. */
const EXTERNAL: Record<string, string[]> = {
  deterministic: ['devtron'],
  devtron: ['devtron'],
  agent: ['model'],
}

interface Ext {
  id: string
  label: string
  sub: string
  tone: 'devtron' | 'model' | 'monitoring'
}

export function FlowDiagram({ h }: { h: Harness }) {
  const [hot, setHot] = useState<string | null>(null)

  const steps = h.pipeline
  const gap = (W - 80 - steps.length * STEP_W) / Math.max(1, steps.length - 1)
  const stepX = (i: number) => 40 + i * (STEP_W + gap)

  // The SRE agent is the only step that reaches the monitoring stack, because
  // it is the only one holding tools. Derived rather than asserted.
  const toolNames = new Set(
    h.agents.flatMap((a) =>
      ((a.tools ?? []) as HarnessTool[])
        .filter((t): t is HarnessTool => typeof t === 'object' && t !== null)
        .map((t) => t.package ?? t.name.split('.')[0]),
    ),
  )
  const hasMonitoring = toolNames.has('prom') || toolNames.has('alerts')
  const toolAgent = h.agents.find((a) => ((a.tools ?? []) as unknown[]).length > 0)?.name ?? ''

  const externals: Ext[] = [
    { id: 'devtron', label: 'Devtron orchestrator', sub: 'clusters · apps · k8s proxy · /intelligence', tone: 'devtron' },
    ...(hasMonitoring
      ? [
          {
            id: 'monitoring',
            label: 'Monitoring stack',
            sub: 'Prometheus or VictoriaMetrics · Alertmanager or vmalert',
            tone: 'monitoring' as const,
          },
        ]
      : []),
    { id: 'model', label: 'Model provider', sub: h.agents.map((a) => a.model).filter(Boolean).join(' · '), tone: 'model' },
  ]
  const extW = (W - 80 - (externals.length - 1) * 28) / externals.length
  const extX = (i: number) => 40 + i * (extW + 28)

  const edges: { from: string; to: string; kind: 'ext' | 'harness' }[] = []
  for (const s of steps) {
    for (const e of EXTERNAL[s.kind] ?? []) edges.push({ from: s.step, to: e, kind: 'ext' })
    if (s.step === toolAgent && hasMonitoring) {
      edges.push({ from: s.step, to: 'monitoring', kind: 'ext' })
      edges.push({ from: s.step, to: 'devtron', kind: 'ext' })
    }
  }

  const layers = h.orchestration
  const layW = (W - 80 - (layers.length - 1) * 10) / layers.length
  const layX = (i: number) => 40 + i * (layW + 10)

  const dim = (id: string) => hot !== null && hot !== id

  return (
    <section>
      <Heading level={2}>Where the work happens</Heading>
      <Text tone="muted" className="mt-1 max-w-3xl">
        The same six steps as the YAML, drawn against who owns them. Everything in the top band is somebody
        else's system; everything in the bottom band is harness we had to write.
      </Text>

      {/* The inspector. It is the same widget as the diagram, not a note
          above it — one card, one border, the readout welded to the top of the
          thing it reads.

          The previous version was muted text on a dashed grey border, which is
          the visual language of a disclaimer: it disguised the one genuinely
          interactive thing on the page as small print. Its job is to make
          someone want to touch the diagram, so it is lit in the product's
          action colour, the cursor icon moves until it has been used, and the
          copy is an instruction in foreground weight rather than a caption. */}
      <div className="mt-3 overflow-hidden rounded-xl border border-border bg-card shadow-card">
        <div
          className={cn(
            'flex min-h-11 items-center gap-2.5 border-b px-3 py-2 transition-colors duration-300',
            hot ? 'border-accent-strong/30 bg-accent-strong/10' : 'border-border bg-bolt/12',
          )}
        >
          <span
            className={cn(
              'grid size-7 shrink-0 place-items-center rounded-full transition-colors duration-300',
              hot ? 'bg-accent-strong/20 text-accent-strong' : 'bg-bolt/35 text-bolt-ink',
            )}
          >
            <MousePointerClick
              aria-hidden
              className={cn('size-3.5', !hot && 'motion-safe:animate-[inspect-nudge_2.4s_ease-in-out_infinite]')}
            />
          </span>

          {hot ? (
            <span className="min-w-0 text-xs leading-snug">
              <span className="font-mono font-semibold text-foreground">{hot}</span>
              <span className="text-muted-foreground"> — {steps.find((s) => s.step === hot)?.what}</span>
            </span>
          ) : (
            <span className="min-w-0 text-xs leading-snug">
              <span className="font-semibold text-foreground">Point at any box to inspect it.</span>{' '}
              <span className="text-muted-foreground">
                Steps light up only the systems they actually call — it is the fastest way to see how much of
                this run is ours.
              </span>
            </span>
          )}

          {/* A live dot while nothing has been touched: this panel is waiting
              for input, and saying so costs one element. */}
          {!hot ? (
            <span aria-hidden className="relative ml-auto hidden size-2 shrink-0 sm:flex">
              <span className="absolute inline-flex size-full animate-ping rounded-full bg-bolt opacity-70" />
              <span className="relative inline-flex size-2 rounded-full bg-bolt" />
            </span>
          ) : null}
        </div>

        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="h-auto w-full"
          role="img"
          aria-label="Data flow: external systems, the pipeline, and the harness layers around it"
        >
          <defs>
            <marker id="fd-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
              <path d="M0 0 L8 4 L0 8 z" fill="currentColor" />
            </marker>
          </defs>

          <BandLabel y={Y_OUT - 58} text="Outside — systems we call, and do not control" />
          <BandLabel y={Y_STEP - 96} text="The run — one pass, left to right" />
          <BandLabel y={Y_HARNESS - 62} text="Harness — everything we had to write around it" />

          {/* Edges first, so boxes paint over their ends. */}
          <g fill="none" strokeWidth={1.5}>
            {edges.map((e, i) => {
              const si = steps.findIndex((s) => s.step === e.from)
              const ei = externals.findIndex((x) => x.id === e.to)
              if (si < 0 || ei < 0) return null
              const x1 = stepX(si) + STEP_W / 2
              const y1 = Y_STEP - STEP_H / 2
              const x2 = extX(ei) + extW / 2
              const y2 = Y_OUT + 30
              const my = (y1 + y2) / 2
              const on = hot === null || hot === e.from
              return (
                <path
                  key={`${e.from}-${e.to}-${i}`}
                  d={`M${x1},${y1} C${x1},${my} ${x2},${my} ${x2},${y2}`}
                  className={cn(
                    'transition-opacity duration-200',
                    e.to === 'model' ? 'stroke-bad/55' : e.to === 'monitoring' ? 'stroke-sky/55' : 'stroke-accent-strong/55',
                    on ? 'opacity-100' : 'opacity-10',
                  )}
                  strokeDasharray="4 3"
                />
              )
            })}
          </g>

          {/* The spine: every step feeds the next. */}
          <g className="text-border" fill="none" stroke="currentColor" strokeWidth={1.5}>
            {steps.slice(0, -1).map((s, i) => (
              <line
                key={s.step}
                x1={stepX(i) + STEP_W}
                y1={Y_STEP}
                x2={stepX(i + 1)}
                y2={Y_STEP}
                markerEnd="url(#fd-arrow)"
              />
            ))}
          </g>

          {/* Outside. */}
          {externals.map((x, i) => (
            <g key={x.id} className={cn('cursor-help transition-opacity duration-200', dim(x.id) && 'opacity-30')}>
              <title>{`${x.label} — ${x.sub}`}</title>
              <rect
                x={extX(i)}
                y={Y_OUT - 30}
                width={extW}
                height={60}
                rx={8}
                className={cn(
                  'fill-well stroke-[1.5]',
                  x.tone === 'model' ? 'stroke-bad/45' : x.tone === 'monitoring' ? 'stroke-sky/45' : 'stroke-accent-strong/45',
                )}
              />
              <text x={extX(i) + extW / 2} y={Y_OUT - 6} textAnchor="middle" className="fill-foreground text-[15px] font-semibold">
                {x.label}
              </text>
              <text x={extX(i) + extW / 2} y={Y_OUT + 16} textAnchor="middle" className="fill-muted-foreground text-[12px]">
                {clip(x.sub, 62)}
              </text>
            </g>
          ))}

          {/* The pipeline. */}
          {steps.map((s, i) => {
            const on = hot === null || hot === s.step
            return (
              <g
                key={s.step}
                onMouseEnter={() => setHot(s.step)}
                onMouseLeave={() => setHot(null)}
                className={cn('cursor-help transition-opacity duration-200', on ? 'opacity-100' : 'opacity-35')}
              >
                <rect
                  x={stepX(i)}
                  y={Y_STEP - STEP_H / 2}
                  width={STEP_W}
                  height={STEP_H}
                  rx={8}
                  className={cn(
                    'stroke-[1.5]',
                    s.kind === 'agent'
                      ? 'fill-ok/10 stroke-ok/50'
                      : s.kind === 'devtron'
                        ? 'fill-accent-strong/10 stroke-accent-strong/50'
                        : 'fill-well stroke-border',
                  )}
                />
                <text x={stepX(i) + 12} y={Y_STEP - 14} className="fill-muted-foreground font-mono text-[11px]">
                  {String(i + 1).padStart(2, '0')}
                </text>
                <text x={stepX(i) + 12} y={Y_STEP + 6} className="fill-foreground font-mono text-[14px] font-semibold">
                  {clip(s.step, 16)}
                </text>
                <text x={stepX(i) + 12} y={Y_STEP + 24} className="fill-muted-foreground text-[11px]">
                  {s.kind}
                </text>
                <title>{s.what}</title>
              </g>
            )
          })}

          {/* The harness band, drawn as a bracket under the whole run: these
              layers apply to every step, which is the point of them. */}
          <path
            d={`M40,${Y_STEP + STEP_H / 2 + 22} L40,${Y_HARNESS - 46} L${W - 40},${Y_HARNESS - 46} L${W - 40},${Y_STEP + STEP_H / 2 + 22}`}
            fill="none"
            className="stroke-border"
            strokeWidth={1.5}
            strokeDasharray="4 4"
          />
          {layers.map((l, i) => (
            <g key={l.layer} className="cursor-help">
              <rect
                x={layX(i)}
                y={Y_HARNESS - 36}
                width={layW}
                height={56}
                rx={8}
                className={cn('stroke-[1.5]', l.declarative ? 'fill-ok/8 stroke-ok/35' : 'fill-warn/8 stroke-warn/35')}
              />
              <text x={layX(i) + layW / 2} y={Y_HARNESS - 14} textAnchor="middle" className="fill-foreground font-mono text-[12px] font-semibold">
                {clip(l.layer, 14)}
              </text>
              <text x={layX(i) + layW / 2} y={Y_HARNESS + 4} textAnchor="middle" className="fill-muted-foreground text-[10.5px]">
                {l.declarative ? 'declarable' : 'hand-written'}
              </text>
              <title>{l.what}</title>
            </g>
          ))}
        </svg>

        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-border px-3 py-2">
          <Key className="bg-accent-strong/50" label="Devtron" />
          <Key className="bg-sky/50" label="Monitoring" />
          <Key className="bg-bad/50" label="Model provider" />
          <Key className="bg-ok/50" label="Declarable today" />
          <Key className="bg-warn/50" label="Had to be written" />
        </div>
      </div>
    </section>
  )
}

function BandLabel({ y, text }: { y: number; text: string }) {
  return (
    <text x={40} y={y} className="fill-muted-foreground text-[11px] font-semibold tracking-[0.12em] uppercase">
      {text}
    </text>
  )
}

function Key({ className, label }: { className: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-muted-foreground">
      <span className={cn('size-2 rounded-sm', className)} />
      {label}
    </span>
  )
}

/** SVG has no ellipsis, so it is done here. */
function clip(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s
}
