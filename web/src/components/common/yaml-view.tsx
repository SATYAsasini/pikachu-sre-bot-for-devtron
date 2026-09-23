import { useMemo, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from 'cn'

/**
 * A read-only YAML document, gutter and all.
 *
 * This is a shape to read, not fields to fill, so it is a code viewer rather
 * than a form — the same call the Devtron dashboard makes for its own SSO
 * config. Line numbers matter more than they look: they are how someone says
 * "line 34 is wrong" about a document nobody can edit here.
 *
 * Highlighting is a regex per line rather than a syntax-highlighter
 * dependency. YAML we generate ourselves has four token classes — comment,
 * key, list marker, scalar — and a 40KB library to colour four things would be
 * the largest thing on the page.
 */
export function YamlView({
  text,
  json,
  className,
  /** Shown in the toolbar, e.g. the file this mirrors. */
  label,
  maxHeight = '32rem',
}: {
  text: string
  /** When given, a YAML/JSON switch appears and this is the other view. */
  json?: string
  className?: string
  label?: string
  maxHeight?: string
}) {
  // YAML reads better; JSON is what you paste into something else. Offering
  // both costs one toggle and settles the argument.
  const [format, setFormat] = useState<'yaml' | 'json'>('yaml')
  const body = json !== undefined && format === 'json' ? json : text
  const lines = useMemo(() => body.split('\n'), [body])
  const [copied, setCopied] = useState(false)

  const copy = () => {
    void (async () => {
      try {
        await navigator.clipboard.writeText(body)
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1400)
      } catch {
        toast.error('Clipboard said no', { description: 'Your browser blocked it. The text is still selectable.' })
      }
    })()
  }

  return (
    <div className={cn('overflow-hidden rounded-xl border border-border bg-well shadow-card', className)}>
      <div className="flex items-center gap-2 border-b border-border bg-card px-2.5 py-1.5">
        <span className="font-mono text-[0.6875rem] text-muted-foreground">{label ?? 'yaml'}</span>

        {json !== undefined ? (
          <div role="radiogroup" aria-label="Format" className="ml-2 inline-flex items-center rounded-md border border-border p-0.5">
            {(['yaml', 'json'] as const).map((f) => (
              <button
                key={f}
                type="button"
                role="radio"
                aria-checked={format === f}
                onClick={() => setFormat(f)}
                className={cn(
                  'rounded px-1.5 py-0.5 font-mono text-[0.625rem] uppercase transition-colors',
                  'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                  format === f ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:text-foreground',
                )}
              >
                {f}
              </button>
            ))}
          </div>
        ) : null}

        <span className="tabular ml-auto text-[0.6875rem] text-muted-foreground">{lines.length} lines</span>
        <button
          type="button"
          onClick={copy}
          className="inline-flex h-6 items-center gap-1 rounded border border-border bg-well px-1.5 text-[0.6875rem] text-muted-foreground transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          {copied ? <Check aria-hidden className="size-3 text-ok" /> : <Copy aria-hidden className="size-3" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>

      <div
        // Lenis owns the wheel at the root, so a nested scroller
        // never sees it. This is the library's opt-out.
        data-lenis-prevent
        className="overflow-auto"
        style={{ maxHeight }}
      >
        <table className="w-full border-collapse font-mono text-[0.6875rem] leading-[1.6]">
          <tbody>
            {lines.map((line, i) => (
              <tr key={i} className="hover:bg-accent/30">
                {/* select-none so copying a block does not drag the gutter
                    numbers into the paste. */}
                <td className="w-10 shrink-0 border-r border-border px-2 text-right align-top text-muted-foreground/50 select-none">
                  {i + 1}
                </td>
                <td className="px-2.5 align-top whitespace-pre-wrap">
                  <Line text={line} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

/** Comment, key, list marker, scalar. That is the whole grammar we emit. */
function Line({ text }: { text: string }) {
  if (text.trim() === '') return <span>{' '}</span>
  if (text.trimStart().startsWith('#')) {
    return <span className="text-muted-foreground/60 italic">{text}</span>
  }

  const m = /^(\s*)(-\s+)?([A-Za-z0-9_.\-/]+):(\s*)(.*)$/.exec(text)
  if (!m) {
    // A block-scalar body line, or a bare list item.
    const li = /^(\s*)(- )(.*)$/.exec(text)
    if (li) {
      return (
        <>
          <span>{li[1]}</span>
          <span className="text-muted-foreground">{li[2]}</span>
          <span className="text-foreground">{li[3]}</span>
        </>
      )
    }
    return <span className="text-foreground/75">{text}</span>
  }

  const [, indent, dash, key, gap, value] = m
  return (
    <>
      <span>{indent}</span>
      {dash ? <span className="text-muted-foreground">{dash}</span> : null}
      <span className="text-accent-strong">{key}</span>
      <span className="text-muted-foreground">:</span>
      <span>{gap}</span>
      <span className={cn(value.startsWith('|') || value.startsWith('>') ? 'text-warn' : 'text-foreground')}>
        {value}
      </span>
    </>
  )
}
