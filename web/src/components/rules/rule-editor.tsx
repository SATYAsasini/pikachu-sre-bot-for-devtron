import { Plus, Trash2 } from 'lucide-react'
import { cn } from 'cn'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Segmented } from '@/components/common/segmented'
import { Text } from '@/components/common/text'
import { PRIORITIES, type Priority, type Rule, type RuleMatch } from '@/lib/types'

/**
 * One rule, edited in place.
 *
 * The matcher has six fields and most rules use one of them, so a form with
 * six labelled inputs would be five empty boxes and a lot of scrolling. This
 * is one row of chips-with-inputs that stay out of the way until filled, and
 * the whole rule reads left to right as the sentence it is: *when severity is
 * critical and namespace is prod → P0*.
 *
 * Nothing here validates a regex. The server compiles it, and an unparseable
 * pattern matches nothing rather than everything — a typo must never silently
 * widen a rule, and the preview beside this editor shows exactly that.
 */
export function RuleRow({
  rule,
  showPriority,
  onChange,
  onRemove,
}: {
  rule: Rule
  /** Priority lists carry a level; show/mute/auto do not. */
  showPriority?: boolean
  onChange: (next: Rule) => void
  onRemove: () => void
}) {
  const set = (patch: Partial<Rule>) => onChange({ ...rule, ...patch })
  const setMatch = (patch: Partial<RuleMatch>) => set({ match: { ...rule.match, ...patch } })

  const empty =
    !rule.match.name &&
    !rule.match.severity?.length &&
    !rule.match.namespace?.length &&
    !Object.keys(rule.match.labels ?? {}).length

  return (
    <li
      className={cn(
        'rounded-xl border bg-card p-2.5 transition-colors',
        rule.enabled ? 'border-border' : 'border-dashed border-border opacity-60',
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        {/* Off rather than deleted: switching a rule off should not lose what
            it said. */}
        <label className="inline-flex shrink-0 cursor-pointer items-center gap-1.5">
          <input
            type="checkbox"
            checked={rule.enabled}
            onChange={(e) => set({ enabled: e.target.checked })}
            className="size-3.5 accent-[var(--accent-strong)]"
          />
          <span className="sr-only">Rule enabled</span>
        </label>

        <Input
          value={rule.name ?? ''}
          onChange={(e) => set({ name: e.target.value })}
          placeholder="Name this rule"
          aria-label="Rule name"
          className="h-7 w-44 text-xs"
        />

        {showPriority ? (
          <Segmented
            value={rule.priority ?? 'P2'}
            onChange={(p) => set({ priority: p as Priority })}
            options={PRIORITIES}
            label="Priority for this rule"
          />
        ) : null}

        <Button
          size="icon-xs"
          variant="ghost"
          onClick={onRemove}
          aria-label="Remove this rule"
          className="ml-auto text-muted-foreground hover:text-bad"
        >
          <Trash2 aria-hidden />
        </Button>
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <Text tone="label" as="span" className="shrink-0">
          when
        </Text>

        <Clause label="name has">
          <Input
            value={rule.match.name ?? ''}
            onChange={(e) => setMatch({ name: e.target.value })}
            placeholder="KubePod…"
            aria-label="Alert name contains"
            className="h-6 w-36 border-0 bg-transparent px-1 text-[0.6875rem] shadow-none focus-visible:ring-0"
          />
        </Clause>

        <Clause label="severity">
          <CsvInput
            value={rule.match.severity}
            onChange={(v) => setMatch({ severity: v })}
            placeholder="critical, warning"
            aria={'Severity is one of'}
          />
        </Clause>

        <Clause label="namespace">
          <CsvInput
            value={rule.match.namespace}
            onChange={(v) => setMatch({ namespace: v })}
            placeholder="prod, payments"
            aria="Namespace is one of"
          />
        </Clause>

        <Clause label="label">
          <PairInput value={rule.match.labels} onChange={(v) => setMatch({ labels: v })} />
        </Clause>
      </div>

      {/* A rule with no conditions does nothing until it is asked to. It used
          to match everything implicitly, which meant clicking "Add rule"
          relabelled every alert on the cluster before a single condition had
          been typed. */}
      {empty ? (
        <div className="mt-1.5 flex flex-wrap items-center gap-2">
          <label className="inline-flex cursor-pointer items-center gap-1.5">
            <input
              type="checkbox"
              checked={rule.catchAll ?? false}
              onChange={(e) => set({ catchAll: e.target.checked })}
              className="size-3.5 accent-[var(--accent-strong)]"
            />
            <span className="text-[0.6875rem] font-medium">Match every alert</span>
          </label>
          <Text tone="fine" as="span">
            {rule.catchAll
              ? 'This claims every alert. Keep it last — anything above it wins.'
              : 'No conditions yet, so this rule does nothing.'}
          </Text>
        </div>
      ) : null}
    </li>
  )
}

/** A labelled slot that only looks like a control once it holds something. */
function Clause({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border border-border bg-well py-0.5 pl-1.5">
      <span className="text-[0.625rem] tracking-wide text-muted-foreground">{label}</span>
      {children}
    </span>
  )
}

/** Comma-separated values, because an OR of three strings is not worth a chip UI. */
function CsvInput({
  value,
  onChange,
  placeholder,
  aria,
}: {
  value?: string[]
  onChange: (next: string[] | undefined) => void
  placeholder: string
  aria: string
}) {
  return (
    <Input
      value={(value ?? []).join(', ')}
      onChange={(e) => {
        const parts = e.target.value
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
        onChange(parts.length > 0 ? parts : undefined)
      }}
      placeholder={placeholder}
      aria-label={aria}
      className="h-6 w-40 border-0 bg-transparent px-1 text-[0.6875rem] shadow-none focus-visible:ring-0"
    />
  )
}

/** One `key=value` pair. Multiple labels are rare enough not to earn a list. */
function PairInput({
  value,
  onChange,
}: {
  value?: Record<string, string>
  onChange: (next: Record<string, string> | undefined) => void
}) {
  const [k = '', v = ''] = Object.entries(value ?? {})[0] ?? []
  return (
    <Input
      value={k ? `${k}=${v}` : ''}
      onChange={(e) => {
        const raw = e.target.value.trim()
        if (!raw.includes('=')) return onChange(undefined)
        const [key, ...rest] = raw.split('=')
        const val = rest.join('=').trim()
        onChange(key.trim() && val ? { [key.trim()]: val } : undefined)
      }}
      placeholder="team=payments"
      aria-label="Label equals"
      className="h-6 w-40 border-0 bg-transparent px-1 font-mono text-[0.6875rem] shadow-none focus-visible:ring-0"
    />
  )
}

/** A named list of rules with an add button. */
export function RuleList({
  title,
  hint,
  rules,
  showPriority,
  onChange,
  action,
}: {
  title: string
  hint: string
  rules: Rule[]
  showPriority?: boolean
  onChange: (next: Rule[]) => void
  action?: React.ReactNode
}) {
  // Disabled on creation. An enabled blank rule is a rule that acts before
  // anyone has said what it should act on.
  const add = () =>
    onChange([...rules, { name: '', match: {}, enabled: false, ...(showPriority ? { priority: 'P1' as const } : {}) }])

  return (
    <section className="rounded-xl border border-border bg-card p-3 shadow-card">
      <header className="mb-2 flex flex-wrap items-center gap-2">
        <div className="min-w-0 flex-1">
          <h3 className="font-display text-xs font-semibold tracking-tight">{title}</h3>
          <Text tone="fine" className="mt-0.5">
            {hint}
          </Text>
        </div>
        {action}
        <Button size="xs" variant="outline" onClick={add}>
          <Plus aria-hidden className="size-3" />
          Add rule
        </Button>
      </header>

      {rules.length === 0 ? (
        <Text tone="fine">None yet.</Text>
      ) : (
        <ol className="space-y-1.5">
          {rules.map((r, i) => (
            <RuleRow
              key={i}
              rule={r}
              showPriority={showPriority}
              onChange={(next) => onChange(rules.map((x, j) => (j === i ? next : x)))}
              onRemove={() => onChange(rules.filter((_, j) => j !== i))}
            />
          ))}
        </ol>
      )}
    </section>
  )
}
