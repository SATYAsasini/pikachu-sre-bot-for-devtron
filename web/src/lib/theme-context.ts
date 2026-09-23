import { createContext, useContext } from 'react'

export type Theme = 'light' | 'dark' | 'system'

export const THEME_STORAGE_KEY = 'devtron.sre.theme'

export interface ThemeContextValue {
  theme: Theme
  /** The theme actually applied to <html>, after resolving "system". */
  resolvedTheme: 'light' | 'dark'
  setTheme: (theme: Theme) => void
  toggleTheme: () => void
}

export const ThemeContext = createContext<ThemeContextValue | null>(null)

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within <ThemeProvider>')
  return ctx
}
