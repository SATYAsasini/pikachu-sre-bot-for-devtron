/** Small, dependency-free formatters. Everything here is display-only. */

/** 41230 -> "41.2s", 112430 -> "1m 52s", 900 -> "0.9s" */
export function duration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms) || ms < 0) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(1)}s`
  const m = Math.floor(s / 60)
  const rest = Math.round(s % 60)
  if (m < 60) return `${m}m ${rest}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

/** "just now", "4m ago", "2h ago", "3d ago". Future stamps read "in 4m". */
export function relativeTime(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const diff = now - t
  const future = diff < 0
  const abs = Math.abs(diff)
  const s = Math.round(abs / 1000)
  if (s < 45) return future ? 'in a moment' : 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return future ? `in ${m}m` : `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 24) return future ? `in ${h}h` : `${h}h ago`
  const d = Math.round(h / 24)
  if (d < 30) return future ? `in ${d}d` : `${d}d ago`
  return new Date(t).toLocaleDateString()
}

export function absoluteTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return iso
  return new Date(t).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

/** Wall-clock time only, for dense event rows. */
export function clockTime(iso: string | null | undefined): string {
  if (!iso) return '--:--:--'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '--:--:--'
  return new Date(t).toLocaleTimeString(undefined, { hour12: false })
}

/** 118402 -> "118.4k", 402118 -> "402.1k", 902 -> "902" */
export function compactNumber(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return '—'
  if (Math.abs(n) < 1000) return String(n)
  if (Math.abs(n) < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(2)}M`
}

/** 0.72 -> "72%". Null confidence is unknown, not zero. */
export function percent(v: number | null | undefined, digits = 0): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—'
  return `${(v * 100).toFixed(digits)}%`
}

/** First 8 chars of an id, for dense tables. The full value stays copyable. */
export function shortId(id: string, len = 8): string {
  return id.length <= len ? id : id.slice(0, len)
}

/** Turns snake_case and kebab-case into Sentence case. */
export function humanise(s: string): string {
  if (!s) return ''
  const words = s.replace(/[_-]+/g, ' ').trim()
  return words.charAt(0).toUpperCase() + words.slice(1)
}

export function pluralise(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}
