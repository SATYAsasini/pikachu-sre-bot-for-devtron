import { Outlet } from '@tanstack/react-router'
import { ReactLenis } from 'lenis/react'
import { TopBar } from '@/components/layout/top-bar'
import { SideNav } from '@/components/layout/side-nav'
import { DotGrid } from '@/components/fx/dot-grid'

export function AppShell() {

  return (
    // Lenis smooths the wheel without touching keyboard or anchor jumps, and
    // it is disabled outright for anyone who asked for reduced motion.
    <ReactLenis
      root
      options={{
        duration: 0.9,
        smoothWheel: !window.matchMedia('(prefers-reduced-motion: reduce)').matches,
        wheelMultiplier: 1,
        touchMultiplier: 1.6,
      }}
    >
      <div className="relative flex min-h-svh overflow-x-clip bg-background">
        <DotGrid />
        <a
          href="#main"
          className="skip-link rounded-md border border-border bg-card px-3 py-2 text-xs font-medium shadow-lg focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          Skip to content
        </a>

        {/* Destinations down the left; the bar across the top keeps only the
            controls that change what you are looking at. */}
        <SideNav />

        <div className="flex min-w-0 flex-1 flex-col">
          <TopBar />
          <main
            id="main"
            tabIndex={-1}
            className="mx-auto w-full max-w-[1500px] min-w-0 flex-1 px-4 py-4 pb-28 focus:outline-none sm:px-6"
          >
            <Outlet />
          </main>
        </div>
      </div>
    </ReactLenis>
  )
}
