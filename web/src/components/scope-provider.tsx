import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { EMPTY_SCOPE, ScopeContext, readScope, writeScope, type Scope } from '@/lib/scope'

export function ScopeProvider({ children }: { children: ReactNode }) {
  const [scope, setScopeState] = useState<Scope>(readScope)

  const setScope = useCallback((next: Scope) => {
    setScopeState(next)
    writeScope(next)
  }, [])

  const patchScope = useCallback((patch: Partial<Scope>) => {
    setScopeState((prev) => {
      const next = { ...prev, ...patch }
      writeScope(next)
      return next
    })
  }, [])

  const clearScope = useCallback(() => {
    setScopeState(EMPTY_SCOPE)
    writeScope(EMPTY_SCOPE)
  }, [])

  const value = useMemo(() => ({ scope, setScope, patchScope, clearScope }), [scope, setScope, patchScope, clearScope])

  return <ScopeContext.Provider value={value}>{children}</ScopeContext.Provider>
}
