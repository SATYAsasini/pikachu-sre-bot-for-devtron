/**
 * The two halves of Settings, and the shape of its URL.
 *
 * Lives here rather than in the page because the router validates the search
 * params and the landing page links into them — a constant exported from a
 * component file breaks fast refresh for that file.
 */
export const SETTINGS_SECTIONS = ['configuration', 'about'] as const
export type SettingsSection = (typeof SETTINGS_SECTIONS)[number]

export interface SettingsSearch {
  section?: SettingsSection
  /** Which pane of the About section to open. Read by HarnessView. */
  panel?: string
}
