import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { EMPTY_SCOPE, ScopeContext, readScope, writeScope, type Scope } from '@/lib/scope'
import { useClusters } from '@/lib/queries'

export function ScopeProvider({ children }: { children: ReactNode }) {
  const [stored, setStored] = useState<Scope>(readScope)
  const clusters = useClusters()

  /**
   * A first visit used to land on "Pick a cluster and I will take a look" on
   * every screen, including the ones whose entire content is per cluster.
   * The list on offer is already only the clusters that can actually be read,
   * so there was nobody being protected from anything by making them pick one
   * of them by hand.
   *
   * Derived during render rather than written by an effect: the default is a
   * function of the cluster list, not a second copy of it that has to be kept
   * in step. Nothing is persisted until the user picks something themselves,
   * so this keeps following the list rather than pinning the first answer.
   */
  const scope = useMemo<Scope>(() => {
    if (stored.clusterId !== undefined) return stored
    const first = clusters.data?.[0]
    return first ? { ...stored, clusterId: first.id, clusterName: first.clusterName } : stored
  }, [stored, clusters.data])

  const setScope = useCallback((next: Scope) => {
    setStored(next)
    writeScope(next)
  }, [])

  // Patches apply to the effective scope, not the stored one, so narrowing to
  // a namespace while the cluster is still the derived default does not throw
  // the cluster away.
  const patchScope = useCallback(
    (patch: Partial<Scope>) => {
      const next = { ...scope, ...patch }
      setStored(next)
      writeScope(next)
    },
    [scope],
  )

  /**
   * Clearing narrows back to the cluster, not to nothing.
   *
   * Every screen in this app is per cluster, so "no cluster" is not a state
   * worth offering — it is the empty state this release exists to stop people
   * landing in. What is worth clearing is the environment, namespace and app
   * the scope was narrowed to.
   */
  const clearScope = useCallback(() => {
    const next: Scope =
      scope.clusterId === undefined
        ? EMPTY_SCOPE
        : { clusterId: scope.clusterId, clusterName: scope.clusterName }
    setStored(next)
    writeScope(next)
  }, [scope])

  const value = useMemo(() => ({ scope, setScope, patchScope, clearScope }), [scope, setScope, patchScope, clearScope])

  return <ScopeContext.Provider value={value}>{children}</ScopeContext.Provider>
}
