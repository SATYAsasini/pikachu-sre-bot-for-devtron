import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/lib/theme-context'

/**
 * Light or dark. Two states, because every click has to change something.
 *
 * This was a three-way cycle: light → dark → system. On a machine whose OS is
 * in dark mode, `system` *resolves to* dark — so the dark → system click
 * repainted nothing. The state changed, the label changed, the page did not,
 * and a button that looks identical after you press it is indistinguishable
 * from a broken one. It was reported as broken twice for exactly that reason.
 *
 * So the control is now a straight toggle against the *resolved* theme, and
 * pressing it always flips the page. `system` is still a valid stored value
 * from before; the first press resolves it to whatever is actually on screen
 * and moves to the opposite.
 *
 * It is also a plain `<button>` with no wrapper. Earlier versions put it
 * inside a tooltip trigger with `asChild`, and every layer that composes a ref
 * onto the real element is another place a click can be lost.
 */
export function ThemeToggle() {
  const { resolvedTheme, toggleTheme } = useTheme()
  const dark = resolvedTheme === 'dark'
  const Icon = dark ? Moon : Sun

  return (
    <button
      type="button"
      onClick={toggleTheme}
      title={dark ? 'Dark. Click for light.' : 'Light. Click for dark.'}
      aria-label={`Theme: ${dark ? 'dark' : 'light'}. Click to switch.`}
      className={[
        'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border bg-card px-2',
        'text-xs font-medium text-muted-foreground transition-colors',
        'hover:border-accent-strong/40 hover:text-foreground',
        'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
      ].join(' ')}
    >
      <Icon aria-hidden className="size-3.5 shrink-0" />
      <span className="hidden sm:inline">{dark ? 'Dark' : 'Light'}</span>
    </button>
  )
}
