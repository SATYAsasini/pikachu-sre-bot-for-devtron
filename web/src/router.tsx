import { createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { AppShell } from '@/components/layout/app-shell'
import { NewRunPage } from '@/pages/new-run'
import { RunDetailPage } from '@/pages/run-detail'
import { RunHistoryPage, type RunsSearch } from '@/pages/runs'
import { SettingsPage } from '@/pages/settings'
import { SETTINGS_SECTIONS, type SettingsSearch, type SettingsSection } from '@/lib/settings-sections'
import { NotFoundPage } from '@/pages/not-found'
import { RUN_STATUSES, type RunStatus } from '@/lib/types'

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

function num(v: unknown): number | undefined {
  const n = typeof v === 'number' ? v : typeof v === 'string' && v !== '' ? Number(v) : NaN
  return Number.isFinite(n) ? n : undefined
}

function oneOf<T extends string>(v: unknown, values: readonly T[]): T | undefined {
  return typeof v === 'string' && (values as readonly string[]).includes(v) ? (v as T) : undefined
}

const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: NotFoundPage,
})

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: NewRunPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
  // The section lives in the URL so it can be linked to directly — the whole
  // point of the About page is being sent to it.
  validateSearch: (search: Record<string, unknown>): SettingsSearch => ({
    section: oneOf<SettingsSection>(search.section, SETTINGS_SECTIONS),
    panel: str(search.panel),
  }),
})

const runsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runs',
  component: RunHistoryPage,
  validateSearch: (search: Record<string, unknown>): RunsSearch => ({
    status: oneOf<RunStatus>(search.status, RUN_STATUSES),
    clusterId: num(search.clusterId),
  }),
})

const runDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/runs/$runId',
  component: RunDetailPage,
})

const routeTree = rootRoute.addChildren([indexRoute, runsRoute, runDetailRoute, settingsRoute])

export const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  defaultNotFoundComponent: NotFoundPage,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
