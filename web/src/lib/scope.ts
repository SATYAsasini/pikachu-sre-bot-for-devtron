import { createContext, useContext } from 'react'
import type { AppType } from '@/lib/types'

/**
 * The scope a run will be created in. Chosen once on the New run screen and
 * carried to Alerts, so the user never picks a cluster twice.
 */
export interface Scope {
  clusterId?: number
  clusterName?: string
  environmentId?: number
  environmentName?: string
  namespace?: string
  appName?: string
  appType?: AppType
  /** Helm releases carry their chart name; it is what identifies the product. */
  chartName?: string
}

export const SCOPE_STORAGE_KEY = 'devtron.sre.scope'

export const EMPTY_SCOPE: Scope = {}

export function readScope(): Scope {
  try {
    const raw = localStorage.getItem(SCOPE_STORAGE_KEY)
    if (!raw) return EMPTY_SCOPE
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null) return EMPTY_SCOPE
    return parsed as Scope
  } catch {
    return EMPTY_SCOPE
  }
}

export function writeScope(scope: Scope): void {
  try {
    localStorage.setItem(SCOPE_STORAGE_KEY, JSON.stringify(scope))
  } catch {
    /* storage unavailable; scope simply will not survive a reload */
  }
}

export function hasCluster(scope: Scope): scope is Scope & { clusterId: number; clusterName: string } {
  return typeof scope.clusterId === 'number' && typeof scope.clusterName === 'string'
}

/** "tenant-acme-prod / payments / redis" — for the top bar and run headers. */
export function scopeLabel(scope: Scope): string {
  const parts = [scope.clusterName, scope.namespace ?? scope.environmentName, scope.appName].filter(Boolean)
  return parts.length ? parts.join(' / ') : 'No scope selected'
}

export interface ScopeContextValue {
  scope: Scope
  setScope: (next: Scope) => void
  patchScope: (patch: Partial<Scope>) => void
  clearScope: () => void
}

export const ScopeContext = createContext<ScopeContextValue | null>(null)

export function useScope(): ScopeContextValue {
  const ctx = useContext(ScopeContext)
  if (!ctx) throw new Error('useScope must be used within <ScopeProvider>')
  return ctx
}
