import { History, Radar, Server, Settings2 } from 'lucide-react'

/**
 * The product's destinations, in one place.
 *
 * Lives here rather than beside the sidebar because both the sidebar and the
 * narrow-screen top bar render it, and a constant exported from a component
 * file breaks fast refresh for that file.
 *
 * Three items for four screens. Alerts used to be a fourth and is gone: the
 * landing page already lists what is firing and starts a run from it in one
 * click, so a separate page was the same data with a navigation step in front.
 * Run detail is always reached from a list, so putting it here would only add
 * a dead link.
 */
export const NAV = [
  { to: '/', label: 'Investigate', icon: Radar, exact: true, blurb: 'Ask, or pick something that is on fire' },
  { to: '/runs', label: 'History', icon: History, exact: false, blurb: 'Every run this agent has made' },
  {
    to: '/clusters',
    label: 'Clusters',
    icon: Server,
    exact: false,
    blurb: 'Alert rules and where findings go, per cluster',
  },
  { to: '/settings', label: 'Settings', icon: Settings2, exact: false, blurb: 'Connection, reach, and how it works' },
] as const
