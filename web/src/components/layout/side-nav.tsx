import { Link, useRouterState } from '@tanstack/react-router'
import { cn } from 'cn'
import { DevtronGlyph } from '@/components/layout/devtron-mark'
import { NAV } from '@/lib/nav'

/**
 * Navigation down the left, the way every console this product sits next to
 * does it.
 *
 * It was three items in the top bar, competing for the same row as the scope
 * picker, the health dot and the theme toggle — so the destinations and the
 * per-page controls looked like one undifferentiated strip of chrome. Down the
 * side they are unmistakably destinations, the top bar is left holding only
 * the things that change what you are looking at, and each item gets room for
 * a line saying what it is.
 *
 * The active item is marked three ways, not one: a filled background, a solid
 * accent bar on the leading edge, and heavier text. A tint alone reads as
 * hover on a page this pale.
 */
export function SideNav() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })

  return (
    <aside className="sticky top-0 hidden h-svh w-56 shrink-0 flex-col border-r border-border bg-card/40 lg:flex">
      <Link
        to="/"
        aria-label="Pikachu SRE bot for Devtron, home"
        className="group flex shrink-0 items-center gap-2.5 border-b border-border px-3 py-3 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        <img
          src="/mark.png"
          alt=""
          aria-hidden
          width={40}
          height={40}
          className={cn(
            'size-10 shrink-0 rounded-full object-cover ring-1 ring-border',
            'transition-[transform,box-shadow] duration-200 ease-out',
            'group-hover:-translate-y-px group-hover:ring-bolt/60 group-hover:shadow-[0_2px_10px_rgba(245,197,24,0.35)]',
          )}
        />
        <span className="flex min-w-0 flex-col leading-tight">
          <span className="font-display truncate text-sm font-semibold tracking-tight">Pikachu SRE</span>
          <span className="flex items-center gap-1 text-[0.625rem] tracking-wide text-muted-foreground">
            bot for
            <DevtronGlyph aria-hidden className="h-2.5 w-auto shrink-0" />
            Devtron
          </span>
        </span>
      </Link>

      <nav aria-label="Main" className="flex-1 space-y-0.5 p-2">
        {NAV.map(({ to, label, icon: Icon, exact, blurb }) => {
          const active = exact ? pathname === to : pathname.startsWith(to)
          return (
            <Link
              key={to}
              to={to}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'relative flex items-start gap-2.5 rounded-md px-2.5 py-2 transition-colors',
                'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none',
                active ? 'bg-accent-strong/12 text-foreground' : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
              )}
            >
              {/* The leading bar. Selection has to change more than hue. */}
              {active ? (
                <span aria-hidden className="absolute top-1.5 bottom-1.5 -left-2 w-1 rounded-r bg-accent-strong" />
              ) : null}
              <Icon aria-hidden className={cn('mt-0.5 size-4 shrink-0', active && 'text-accent-strong')} />
              <span className="min-w-0">
                <span className={cn('block truncate text-sm', active ? 'font-semibold' : 'font-medium')}>{label}</span>
                <span className="mt-0.5 block text-[0.625rem] leading-snug text-muted-foreground">{blurb}</span>
              </span>
            </Link>
          )
        })}
      </nav>

      {/* Devtron's mark sits at the foot rather than beside ours: this is an
          attribution, not a destination. The glyph rather than the full
          lockup, because the lockup already spells "devtron" and the line
          beside it says the word again. */}
      <div className="flex shrink-0 items-center gap-2 border-t border-border px-3 py-2.5">
        <DevtronGlyph aria-hidden className="h-4 w-auto shrink-0 opacity-80" />
        <span className="text-[0.625rem] text-muted-foreground">Built for Devtron</span>
      </div>
    </aside>
  )
}
