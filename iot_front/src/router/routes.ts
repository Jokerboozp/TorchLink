import type { RouteRecordRaw } from 'vue-router'
import { pathOf } from './paths.ts'

declare module 'vue-router' {
  interface RouteMeta {
    /** The menu page the route shows; empty for addresses that match no page. */
    page: string
    /** Rule linkage (OPEN_PAGE actions) may open this page. */
    linkable?: boolean
  }
}

// Every menu page, in menu order; App.vue holds their components and icons.
// linkable pages may be opened by rule linkage.
export const PAGES: { name: string; linkable?: boolean }[] = [
  { name: 'dashboard', linkable: true },
  { name: 'alarms', linkable: true },
  { name: 'notifications' },
  { name: 'inspection', linkable: true },
  { name: 'raw', linkable: true },
  { name: 'rules', linkable: true },
  { name: 'devices', linkable: true },
  { name: 'products', linkable: true },
  { name: 'profiles', linkable: true },
  { name: 'protocols', linkable: true },
  { name: 'cameras', linkable: true },
  { name: 'externalData', linkable: true },
  { name: 'messageTopics' },
  { name: 'integration', linkable: true },
  { name: 'ai', linkable: true },
  { name: 'knowledge', linkable: true },
  { name: 'aiProviders', linkable: true },
  { name: 'sites' },
  { name: 'duty' },
  { name: 'extinguishers' },
  { name: 'fireStations' },
  { name: 'opsOverview' },
  { name: 'opsMetrics' },
  { name: 'opsLogs' },
  { name: 'opsDashboards' },
  { name: 'opsAlerts' },
  { name: 'opsCapacity' },
  { name: 'backups', linkable: true },
  { name: 'access' }
]

/** Pages that rule linkage may open. */
export const linkablePages = new Set(PAGES.filter(page => page.linkable).map(page => page.name))

// App.vue renders the page itself (lazy components, page keys, header), so
// routes carry no component; the empty render keeps the router satisfied.
const empty = { render: () => null }

export const routes: RouteRecordRaw[] = [
  ...PAGES.map(({ name, linkable }) => ({ path: pathOf(name), name, component: empty, meta: { page: name, linkable } })),
  { path: '/alarms/:alarmId', name: 'alarmDetail', component: empty, meta: { page: 'alarms', linkable: true } },
  // The root and unknown addresses resolve to the first permitted page in App's guard.
  { path: '/:pathMatch(.*)*', name: 'unknown', component: empty, meta: { page: '' } }
]
