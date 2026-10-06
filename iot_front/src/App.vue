<script setup>
import { computed, defineAsyncComponent, h, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Activity,
  Bell,
  BellRing,
  Bot,
  Boxes,
  BrainCircuit,
  Building2,
  Cable,
  CalendarDays,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  Cpu,
  Database,
  FileText,
  FireExtinguisher,
  FlaskConical,
  Gauge,
  House,
  KeyRound,
  LayoutDashboard,
  LayoutGrid,
  Library,
  LineChart,
  LogOut,
  Megaphone,
  Menu,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  ScrollText,
  Search,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Video,
  X
} from '@lucide/vue'
import { UiMessage } from './ui/feedback.js'
import GlobalAlertPopup from './components/GlobalAlertPopup.vue'
import LivePlayerDialog from './components/LivePlayerDialog.vue'
import PasswordChangeDialog from './components/PasswordChangeDialog.vue'
import { liveUsable, loadLiveStatus, resetLiveState } from './liveVideo'
import { resetAIConversation } from './aiConversation'
import { clearAIHistory } from './aiHistory'
import { api, notifyError, session } from './api'
import { pageGuide } from './pageGuide'
import { linkablePages, router } from './router'
import { NAVIGATION_KEY, detailFromLocation, locationFor, pathOf } from './router/paths'
import { confirmDiscard, hasUnsaved } from './composables/unsavedGuard.js'
import PageLoadState from './components/layout/PageLoadState.vue'
import { can, permissionState } from './permissions'
import { storeToRefs } from 'pinia'
import { onSessionReset, useSessionStore } from './stores/session'
import { isDark, setThemeMode, themeMode } from './theme/mode.js'
import { realtimeStatus, retryRealtime, startRealtime, stopRealtime } from './realtime'
import { toRealtimeEvent } from './composables/useRealtime'
import { useMediaQuery } from './composables/useMediaQuery'

// 页面按需加载：慢网络显示加载提示，加载失败（如升级后旧文件已不存在）提示刷新。
const lazyView = loader =>
  defineAsyncComponent({
    loader,
    loadingComponent: PageLoadState,
    errorComponent: { render: () => h(PageLoadState, { failed: true }) },
    delay: 200,
    timeout: 30000
  })
const DashboardView = lazyView(() => import('./views/DashboardView.vue'))
const SitesView = lazyView(() => import('./views/SitesView.vue'))
const DevicesView = lazyView(() => import('./views/DevicesView.vue'))
const ProductsView = lazyView(() => import('./views/ProductsView.vue'))
const ProtocolsView = lazyView(() => import('./views/ProtocolsView.vue'))
const TestDeviceView = lazyView(() => import('./views/TestDeviceView.vue'))
const CameraMappingsView = lazyView(() => import('./views/CameraMappingsView.vue'))
const ExternalDataView = lazyView(() => import('./views/ExternalDataView.vue'))
const MessageTopicsView = lazyView(() => import('./views/MessageTopicsView.vue'))
const AlarmsView = lazyView(() => import('./views/AlarmsView.vue'))
const HealthInspectionView = lazyView(() => import('./views/HealthInspectionView.vue'))
const RawView = lazyView(() => import('./views/RawView.vue'))
const RulesView = lazyView(() => import('./views/RulesView.vue'))
const NotificationsView = lazyView(() => import('./views/NotificationsView.vue'))
const KnowledgeView = lazyView(() => import('./views/KnowledgeView.vue'))
const AiView = lazyView(() => import('./views/AiView.vue'))
const AiProvidersView = lazyView(() => import('./views/AiProvidersView.vue'))
const BackupsView = lazyView(() => import('./views/BackupsView.vue'))
const AccessView = lazyView(() => import('./views/AccessView.vue'))
const DutyView = lazyView(() => import('./views/DutyView.vue'))
const ExtinguishersView = lazyView(() => import('./views/ExtinguishersView.vue'))
const FireStationsView = lazyView(() => import('./views/FireStationsView.vue'))
const OpsOverviewView = lazyView(() => import('./views/OpsOverviewView.vue'))
const OpsMetricsView = lazyView(() => import('./views/OpsMetricsView.vue'))
const OpsLogsView = lazyView(() => import('./views/OpsLogsView.vue'))
const OpsDashboardsView = lazyView(() => import('./views/OpsDashboardsView.vue'))
const OpsAlertsView = lazyView(() => import('./views/OpsAlertsView.vue'))
const OpsCapacityView = lazyView(() => import('./views/OpsCapacityView.vue'))

const sessionStore = useSessionStore()
const { authenticated, identity, platformVersion } = storeToRefs(sessionStore)
// 退出登录时由会话仓库统一清理：直播状态缓存与后台运行的智能助手对话。
onSessionReset(resetLiveState)
onSessionReset(resetAIConversation)
const active = ref('dashboard')
const readStoredCollapse = () => {
  try {
    return localStorage.getItem('iot:sidebar-collapsed') === 'true'
  } catch {
    return false
  }
}
const collapsed = ref(readStoredCollapse())
watch(collapsed, value => {
  try {
    localStorage.setItem('iot:sidebar-collapsed', String(value))
  } catch {
    /* 浏览器禁用存储时只影响本次折叠状态。 */
  }
})
const narrow = useMediaQuery('(max-width: 767px)')
const navOpen = ref(false)
watch(narrow, value => {
  if (!value) navOpen.value = false
})
// 窄屏抽屉导航：打开时聚焦菜单搜索框，关闭时把焦点还给打开按钮，键盘与读屏用户不会丢失位置。
const menuSearch = ref(null)
const navToggle = ref(null)
watch(navOpen, async (open, wasOpen) => {
  if (!narrow.value || open === wasOpen) return
  await nextTick()
  if (open) menuSearch.value?.focus()
  else navToggle.value?.focus()
})
const contentArea = ref(null)
const pageKey = ref(0)
const loginLoading = ref(false)
const globalAlertPopup = ref(null)
// 登录表单只记住本浏览器上次使用的租户，不预填账户名。
const lastTenant = () => {
  try {
    return localStorage.getItem('iot:last-tenant') || ''
  } catch {
    return ''
  }
}
const loginForm = ref({ tenantId: lastTenant(), username: '', password: '' })
const currentUser = computed(() => identity.value.user || loginForm.value.username || '账户')
const currentRole = computed(() => ({ admin: '管理员', operator: '运维人员', viewer: '访客' })[identity.value.role] || '平台用户')

// layout=full 的页面占满内容高度；header=false 的页面使用自己的介绍区。
const pages = {
  dashboard: { ...pageGuide.dashboard, icon: LayoutDashboard, component: DashboardView },
  alarms: { ...pageGuide.alarms, icon: Bell, component: AlarmsView },
  inspection: { ...pageGuide.inspection, icon: ClipboardCheck, component: HealthInspectionView },
  raw: { ...pageGuide.raw, icon: FileText, component: RawView },
  rules: { ...pageGuide.rules, icon: SlidersHorizontal, component: RulesView },
  notifications: { ...pageGuide.notifications, icon: Megaphone, component: NotificationsView },
  devices: { ...pageGuide.devices, icon: Cpu, component: DevicesView },
  products: { ...pageGuide.products, icon: Boxes, component: ProductsView },
  profiles: { ...pageGuide.profiles, icon: Cable, component: ProtocolsView, props: { section: 'profiles' } },
  protocols: { ...pageGuide.protocols, icon: Network, component: ProtocolsView, props: { section: 'protocols' } },
  cameras: { ...pageGuide.cameras, icon: Video, component: CameraMappingsView },
  externalData: { ...pageGuide.externalData, icon: Cable, component: ExternalDataView },
  messageTopics: { ...pageGuide.messageTopics, icon: Network, component: MessageTopicsView },
  integration: { ...pageGuide.integration, icon: FlaskConical, component: TestDeviceView },
  ai: { ...pageGuide.ai, icon: Bot, component: AiView, layout: 'full' },
  knowledge: { ...pageGuide.knowledge, icon: Library, component: KnowledgeView },
  aiProviders: { ...pageGuide.aiProviders, icon: BrainCircuit, component: AiProvidersView },
  backups: { ...pageGuide.backups, icon: Database, component: BackupsView },
  access: { ...pageGuide.access, icon: ShieldCheck, component: AccessView },
  duty: { ...pageGuide.duty, icon: CalendarDays, component: DutyView },
  extinguishers: { ...pageGuide.extinguishers, icon: FireExtinguisher, component: ExtinguishersView },
  fireStations: { ...pageGuide.fireStations, icon: House, component: FireStationsView },
  sites: { ...pageGuide.sites, icon: Building2, component: SitesView },
  opsOverview: { ...pageGuide.opsOverview, icon: Gauge, component: OpsOverviewView },
  opsMetrics: { ...pageGuide.opsMetrics, icon: LineChart, component: OpsMetricsView },
  opsLogs: { ...pageGuide.opsLogs, icon: ScrollText, component: OpsLogsView },
  opsDashboards: { ...pageGuide.opsDashboards, icon: LayoutGrid, component: OpsDashboardsView },
  opsAlerts: { ...pageGuide.opsAlerts, icon: BellRing, component: OpsAlertsView },
  opsCapacity: { ...pageGuide.opsCapacity, icon: Activity, component: OpsCapacityView }
}
const menuGroups = [
  { label: '运行监控', items: ['dashboard', 'alarms', 'notifications', 'inspection', 'raw', 'rules'] },
  {
    label: '设备与接入',
    items: ['devices', 'products', 'profiles', 'protocols', 'cameras', 'externalData', 'messageTopics', 'integration']
  },
  { label: '智能助手', items: ['ai', 'knowledge', 'aiProviders'] },
  { label: '消防管理', items: ['sites', 'duty', 'extinguishers', 'fireStations'] },
  { label: '运维中心', items: ['opsOverview', 'opsMetrics', 'opsLogs', 'opsDashboards', 'opsAlerts', 'opsCapacity'] },
  { label: '系统', items: ['backups', 'access'] }
]
const current = computed(() => pages[active.value] || { title: '暂无可用功能' })
const currentGroup = computed(() => menuGroups.find(group => group.items.includes(active.value))?.label || '')
const showHeader = computed(() => current.value.layout !== 'full' && current.value.header !== false && Boolean(current.value.component))
// 接入点在设备模板详情中管理；没有模板菜单权限的账号仍保留独立入口。
// 容量测试是可选部署模块：未部署时菜单不出现（直接打开页面会显示启用方法）。
const capacityModuleOn = ref(true)
async function refreshModules() {
  if (!authenticated.value || !can('menu:opsCapacity')) return
  try {
    capacityModuleOn.value = (await api('/api/v1/ops/capacity/status')).enabled !== false
  } catch {
    capacityModuleOn.value = true
  }
}
const navigable = name =>
  can('menu:' + name) && !(name === 'profiles' && can('menu:products')) && !(name === 'opsCapacity' && !capacityModuleOn.value)
const visibleGroups = computed(() =>
  menuGroups.map(group => ({ ...group, items: group.items.filter(navigable) })).filter(group => group.items.length)
)
const firstAllowedPage = () => visibleGroups.value[0]?.items[0] || ''
// 侧栏菜单按名称或分组筛选；回车打开第一个匹配项。
const menuQuery = ref('')
const shownGroups = computed(() => {
  const query = menuQuery.value.trim().toLowerCase()
  if (!query) return visibleGroups.value
  return visibleGroups.value
    .map(group => ({
      ...group,
      items: group.label.toLowerCase().includes(query)
        ? group.items
        : group.items.filter(name => pages[name].title.toLowerCase().includes(query))
    }))
    .filter(group => group.items.length)
})
function openFirstMatch() {
  const name = shownGroups.value[0]?.items[0]
  if (!name) return
  menuQuery.value = ''
  openPage(name)
}

// 实时通道跟随权限：获得告警、设备等菜单时补连，全部失去时停止，避免授权后要重新登录才有告警提醒。
const realtimeAllowed = () => can(['menu:devices', 'menu:alarms', 'menu:dashboard', 'menu:raw'])
watch(realtimeAllowed, (allowed, before) => {
  if (!authenticated.value || allowed === before) return
  if (allowed) connect()
  else stopRealtime()
})

watch(
  () => permissionState.accessVersion + '\n' + permissionState.items.join('\n'),
  (value, old) => {
    if (!authenticated.value || value === old) return
    // 智能助手的回答可在其他页面后台生成；授权变化后停止旧授权下的运行。
    resetAIConversation()
    if (!can('menu:' + active.value)) {
      void router.replace(pathOf(firstAllowedPage() || 'dashboard'))
      return
    }
    // 仍可访问当前页面且有未保存修改时不重载页面，避免丢失填写内容；服务端仍按新权限校验每个请求。
    if (hasUnsaved()) {
      UiMessage.info('账户权限已更新，保存当前修改后重新打开页面即可按新权限显示')
      return
    }
    pageKey.value++
  }
)

// 回到窗口时同步权限；实时轮询已在后台更新权限，30 秒内的重复焦点不再请求。
let lastFocusSync = 0
function syncOnFocus() {
  if (Date.now() - lastFocusSync < 30000) return
  void syncIdentity()
}

async function syncIdentity() {
  if (!authenticated.value) return
  lastFocusSync = Date.now()
  try {
    await sessionStore.refresh()
    if (!routeApplied) applyRoute()
    else if (!can('menu:' + active.value)) void router.replace(pathOf(firstAllowedPage() || 'dashboard'))
  } catch (error) {
    notifyError(error)
  }
  refreshModules()
}

// 地址栏与当前页面由 vue-router 同步：打开页面写入历史记录，前进后退和深链接按地址切换页面。
// 守卫按菜单权限放行，未知或无权限的地址改到首个可用页面；页面仍由下方 <component> 渲染。
let routeApplied = false
// 权限就绪后按当前地址重新导航一次（登录前打开的深链接在登录后继续打开）。
function applyRoute() {
  routeApplied = true
  const { path, query, hash } = router.currentRoute.value
  void router.replace({ path, query, hash, force: true })
}
// openPage 发起的导航：携带的细节与是否已确认离开未保存页面；地址栏发起的导航（刷新、前进后退）从地址读取细节。
let pendingDetail
let navigationConfirmed = false
router.beforeEach(async (to, from) => {
  if (!authenticated.value || !permissionState.ready) return true
  const page = to.meta.page
  if (page === 'profiles' && can('menu:products')) return { path: pathOf('products'), query: { ...to.query, tab: 'access' }, replace: true }
  if (!page || !can('menu:' + page)) {
    const first = firstAllowedPage()
    return first ? { path: pathOf(first), replace: true } : true
  }
  // 前进后退离开有未保存修改的页面时先确认；取消则停留在当前地址。
  const confirmed = navigationConfirmed
  navigationConfirmed = false
  if (!confirmed && from.matched.length && hasUnsaved() && !(await confirmDiscard())) return false
  return true
})
router.afterEach((to, from, failure) => {
  const detail = pendingDetail === undefined ? detailFromLocation(to.params, to.query) : pendingDetail
  pendingDetail = undefined
  if (failure || !authenticated.value || !permissionState.ready) return
  const page = to.meta.page
  if (!page || !can('menu:' + page)) {
    active.value = firstAllowedPage()
    return
  }
  sessionStorage.removeItem(NAVIGATION_KEY)
  active.value = page
  pageKey.value++
  if (detail) sessionStorage.setItem(NAVIGATION_KEY, JSON.stringify(detail))
  contentArea.value?.scrollTo({ top: 0 })
})

// 管理员设置或重置密码后，首次登录只拿到改密凭据，修改成功后才建立会话。
const passwordDialog = ref(false)
const passwordChange = ref({ required: false, token: '', current: '' })
function startSession(data, username) {
  sessionStore.start(data, username)
  try {
    if (data.tenantId) localStorage.setItem('iot:last-tenant', data.tenantId)
  } catch {
    /* 无法保存时下次手动填写租户。 */
  }
  // 登录前打开的深链接（例如通知中的告警详情）在登录后继续打开。
  applyRoute()
  refreshModules()
  loginForm.value.password = ''
  if (can(['menu:devices', 'menu:alarms', 'menu:dashboard', 'menu:raw'])) connect()
}

async function login() {
  loginLoading.value = true
  try {
    const data = await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify(loginForm.value) })
    if (data.passwordChangeRequired) {
      passwordChange.value = { required: true, token: data.changeToken, current: loginForm.value.password }
      passwordDialog.value = true
      return
    }
    startSession(data, loginForm.value.username)
  } catch (error) {
    notifyError(error)
  } finally {
    loginLoading.value = false
  }
}

function passwordChanged(data) {
  const forced = passwordChange.value.required
  passwordChange.value = { required: false, token: '', current: '' }
  if (forced) {
    startSession(data, loginForm.value.username)
  } else {
    stopRealtime()
    startSession(data, identity.value.user)
  }
  UiMessage.success('密码已修改，其他登录已失效')
}

function logout() {
  stopRealtime()
  // 先关闭直播弹窗（卸载时释放播放会话），再清除身份与直播状态缓存。
  livePlayerVisible.value = false
  // 对话记录含设备与告警问答，退出后不留在本浏览器。
  try {
    // 用本页的身份：其他标签页退出时共享的会话键已被清空。
    clearAIHistory(localStorage, { tenant: identity.value.tenant || session.tenant, user: identity.value.user || session.user })
  } catch {
    /* 存储不可用时没有可清理的记录。 */
  }
  sessionStore.signOut()
}

const themeOptions = [
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
  { value: 'system', label: '跟随系统' }
]
function handleAccountCommand(command) {
  if (command?.startsWith('theme:')) setThemeMode(command.slice(6))
  if (command === 'logout') logout()
  if (command === 'password') {
    passwordChange.value = { required: false, token: '', current: '' }
    passwordDialog.value = true
  }
}

function openPage(name, detail, { force = false, confirmed = true } = {}) {
  if (name === 'profiles' && can('menu:products')) {
    name = 'products'
    detail = detail && { ...detail, tab: 'access' }
  }
  if (!pages[name] || !can('menu:' + name)) return
  navOpen.value = false
  // 已在当前页面且没有新的定位条件时不再新增历史记录，也不重新打开页面。
  if (active.value === name && !detail && !force) return
  pendingDetail = detail || null
  navigationConfirmed = confirmed
  void router.push({ ...locationFor(name, detail), force: true })
}

// 用户主动切换页面（菜单、页面内跳转、告警弹窗）：离开有未保存修改的页面前先确认。
// options.onDone 在确实切换后调用（例如全局告警弹窗据此关闭对应提示）。
async function navigate(name, detail, options) {
  if ((name !== active.value || detail) && !(await confirmDiscard())) return
  openPage(name, detail)
  options?.onDone?.()
}

function toggleNavigation() {
  if (narrow.value) navOpen.value = !navOpen.value
  else collapsed.value = !collapsed.value
}

function closeNavigationOnEscape(event) {
  if (event.key === 'Escape') navOpen.value = false
}

function openAlertSettings() {
  globalAlertPopup.value?.openSettings()
}

const livePlayerVisible = ref(false)
const livePlayerCamera = ref(null)

// 规则联动打开摄像头：直播可用且当前用户有权观看时直接播放，否则保留原有的资料定位。
async function openCameraAction(cameraId, actionId) {
  try {
    await loadLiveStatus()
    if (liveUsable()) {
      const camera = await api(`/api/v1/video/cameras/${encodeURIComponent(cameraId)}`)
      if (camera?.liveAvailable) {
        livePlayerCamera.value = camera
        livePlayerVisible.value = true
        UiMessage.info(`规则联动：正在打开摄像头直播 ${camera.cameraName || cameraId}`)
        return
      }
    }
  } catch {
    /* 无权观看或模块不可用时退回资料定位。 */
  }
  openPage('cameras', { cameraId, actionId })
  UiMessage.info(`规则联动：已定位摄像头信息 ${cameraId}`)
}

function handleUIAction(payload) {
  try {
    const event = JSON.parse(payload)
    const action = event?.action || {}
    if (action.type === 'OPEN_CAMERA' && typeof action.cameraId === 'string' && action.cameraId) {
      void openCameraAction(action.cameraId, event.id)
      return
    }
    if (action.type === 'OPEN_PAGE' && linkablePages.has(action.page)) {
      // 自动跳转不能打断正在填写的表单：有未保存修改时只提示，由用户自行打开。
      if (action.page !== active.value && hasUnsaved()) {
        UiMessage.warning(`规则联动请求打开「${pages[action.page]?.title || action.page}」，当前有未保存的修改，未自动跳转`)
        return
      }
      openPage(action.page)
      UiMessage.warning('规则联动：已打开相关业务页面')
    }
  } catch {
    // 忽略格式错误或不支持的联动动作。
  }
}

function connect() {
  startRealtime((topic, payload, meta) => {
    if (topic.includes('/ui-action/')) handleUIAction(payload)
    window.dispatchEvent(new CustomEvent('iot:realtime', { detail: toRealtimeEvent(topic, payload, meta?.added) }))
  })
}

// 并发请求可能同时返回 401，只在第一次退出并提示一次。
function unauthorized() {
  if (!authenticated.value) return
  logout()
  UiMessage.error('登录已过期，请重新登录')
}

// 其他标签页登录了另一身份或已退出时，本页同步，避免用新令牌显示旧身份。
function onStorage(event) {
  if (!['iot_token', 'iot_tenant', 'iot_user'].includes(event.key)) return
  if (!authenticated.value) {
    if (session.token) window.location.reload()
    return
  }
  if (!session.token) {
    logout()
    UiMessage.info('已在其他标签页退出登录')
    return
  }
  if (session.tenant !== identity.value.tenant || session.user !== identity.value.user) window.location.reload()
}

// 浏览器标签标题显示当前页面，多个标签页和历史记录可以区分。
watch(
  () => (authenticated.value ? current.value.title : ''),
  title => {
    if (typeof document !== 'undefined') document.title = title ? `${title} · 炬联 TorchLink` : '炬联 TorchLink · 消防物联网管理平台'
  },
  { immediate: true }
)

// 实时通道连续失败或已停止时在顶栏提示，避免误以为看到的是实时告警。
const realtimeNotice = computed(() => {
  if (!authenticated.value) return ''
  if (realtimeStatus.state === 'stopped') return '实时告警已停止，请刷新页面'
  if (realtimeStatus.state === 'retrying' && realtimeStatus.failures >= 2) {
    const last = realtimeStatus.lastOk ? new Date(realtimeStatus.lastOk).toLocaleTimeString('zh-CN', { hour12: false }) : ''
    return last ? `实时数据中断，最近更新 ${last}，正在重试` : '实时数据连接失败，正在重试'
  }
  return ''
})

onMounted(async () => {
  window.addEventListener('iot:unauthorized', unauthorized)
  window.addEventListener('storage', onStorage)
  window.addEventListener('focus', syncOnFocus)
  window.addEventListener('keydown', closeNavigationOnEscape)
  await syncIdentity()
  if (authenticated.value && can(['menu:devices', 'menu:alarms', 'menu:dashboard', 'menu:raw'])) connect()
})

onBeforeUnmount(() => {
  window.removeEventListener('iot:unauthorized', unauthorized)
  window.removeEventListener('storage', onStorage)
  window.removeEventListener('focus', syncOnFocus)
  window.removeEventListener('keydown', closeNavigationOnEscape)
  stopRealtime()
})
</script>

<template>
  <ui-config-provider>
    <div v-if="!authenticated" class="login-page">
      <section class="login-brand-panel" aria-hidden="true">
        <div class="login-brand-panel__top">
          <img class="login-brand-panel__logo" :src="isDark ? '/torchlink-sidebar-dark.svg' : '/torchlink-sidebar.svg'" alt="" />
          <span class="login-chip">消防物联网管理平台</span>
        </div>
        <div class="login-brand-panel__copy">
          <p class="login-eyebrow">智慧消防 · 全域感知</p>
          <h1>连接现场设备<br /><em>守护每一处安全</em></h1>
          <p>从设备接入、实时监测到告警处置，在一个工作台掌握现场运行情况。</p>
        </div>
        <ul class="login-features">
          <li>
            <Network />
            <div><strong>多协议接入</strong><span>标准 HTTP / MQTT、TCP / UDP 与 Modbus 设备统一登记</span></div>
          </li>
          <li>
            <Bell />
            <div><strong>实时告警</strong><span>设备上报告警与规则告警在同一处确认和处置</span></div>
          </li>
          <li>
            <ClipboardCheck />
            <div><strong>智能巡检</strong><span>在线情况、上报时效与活动告警一次汇总</span></div>
          </li>
        </ul>
      </section>
      <section class="login-form-panel">
        <form class="login-form" @submit.prevent="login">
          <img class="login-form__logo" :src="isDark ? '/torchlink-login-dark.svg' : '/torchlink-login.svg'" alt="炬联 TorchLink" />
          <h2>欢迎使用炬联</h2>
          <p class="login-form__lead">登录账户，进入消防物联网工作台</p>
          <div class="login-fields">
            <div class="login-field">
              <label for="tenant-id">租户</label>
              <ui-input
                id="tenant-id"
                v-model="loginForm.tenantId"
                size="large"
                autocomplete="organization"
                placeholder="请输入租户编号"
                required
              />
            </div>
            <div class="login-field">
              <label for="username">用户名</label>
              <ui-input
                id="username"
                v-model="loginForm.username"
                size="large"
                autocomplete="username"
                placeholder="请输入用户名"
                required
              />
            </div>
            <div class="login-field">
              <label for="password">密码</label>
              <ui-input
                id="password"
                v-model="loginForm.password"
                size="large"
                type="password"
                autocomplete="current-password"
                placeholder="请输入密码"
                required
              />
            </div>
          </div>
          <ui-button native-type="submit" type="primary" size="large" class="login-submit" :loading="loginLoading">进入平台</ui-button>
          <p class="login-help">账户由管理员分配，按授权访问设备和功能</p>
        </form>
      </section>
    </div>

    <div v-else class="app-shell" :class="{ 'is-collapsed': collapsed && !narrow, 'is-nav-open': navOpen }">
      <aside class="app-sidebar" :inert="narrow && !navOpen ? true : undefined">
        <div class="app-sidebar__brand">
          <img :src="isDark ? '/torchlink-sidebar-dark.svg' : '/torchlink-sidebar.svg'" alt="炬联 TorchLink" />
          <button v-if="narrow" type="button" class="app-sidebar__close" aria-label="关闭菜单" @click="navOpen = false"><X /></button>
        </div>
        <div v-if="!collapsed || narrow" class="app-sidebar__search">
          <Search aria-hidden="true" />
          <input
            ref="menuSearch"
            v-model="menuQuery"
            type="search"
            placeholder="搜索功能"
            aria-label="搜索功能菜单"
            @keydown.enter.prevent="openFirstMatch"
            @keydown.esc="menuQuery = ''"
          />
        </div>
        <nav class="app-sidebar__nav" aria-label="主导航">
          <p v-if="!shownGroups.length" class="nav-empty">没有匹配的功能</p>
          <section v-for="group in shownGroups" :key="group.label" class="nav-group">
            <p class="nav-group__label">{{ group.label }}</p>
            <button
              v-for="name in group.items"
              :key="name"
              type="button"
              class="nav-item"
              :class="{ 'is-active': active === name }"
              :aria-label="pages[name].title"
              :title="collapsed && !narrow ? pages[name].title : undefined"
              :aria-current="active === name ? 'page' : undefined"
              @click="navigate(name)"
            >
              <component :is="pages[name].icon" />
              <span>{{ pages[name].title }}</span>
            </button>
          </section>
        </nav>
      </aside>
      <div v-if="navOpen" class="app-sidebar-mask" @click="navOpen = false" />

      <div class="app-main">
        <header class="app-topbar">
          <div class="app-topbar__left">
            <button
              ref="navToggle"
              type="button"
              class="icon-button app-topbar__toggle"
              :aria-label="narrow ? '打开菜单' : collapsed ? '展开菜单' : '折叠菜单'"
              :aria-expanded="narrow ? navOpen : !collapsed"
              @click="toggleNavigation"
            >
              <component :is="narrow ? Menu : collapsed ? PanelLeftOpen : PanelLeftClose" />
            </button>
            <nav class="app-breadcrumb" aria-label="当前位置">
              <span v-if="currentGroup">{{ currentGroup }}</span>
              <ChevronRight v-if="currentGroup" />
              <strong>{{ current.title }}</strong>
            </nav>
          </div>
          <div class="app-topbar__actions">
            <button
              v-if="realtimeNotice"
              class="topbar-button realtime-notice"
              type="button"
              role="status"
              :title="`${realtimeNotice}，点击立即重试`"
              :aria-label="`${realtimeNotice}，点击立即重试`"
              @click="retryRealtime"
            >
              <span class="realtime-notice__dot" aria-hidden="true" /><span>{{ realtimeNotice }}</span>
            </button>
            <button v-if="can('menu:alarms')" class="topbar-button" type="button" aria-label="告警提醒设置" @click="openAlertSettings">
              <Settings2 /><span>告警提醒</span>
            </button>
            <ui-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <span class="account__avatar" aria-hidden="true">{{ currentRole.slice(0, 1) }}</span>
                <span class="account__copy"
                  ><strong>{{ currentUser === 'admin' ? '管理员' : currentUser }}</strong
                  ><small>{{ currentRole }}</small></span
                >
                <ChevronDown class="account__chevron" />
              </button>
              <template #dropdown>
                <ui-dropdown-menu>
                  <ui-dropdown-item v-if="identity.role !== 'admin'" command="password"><KeyRound />修改密码</ui-dropdown-item>
                  <ui-dropdown-item
                    v-for="option in themeOptions"
                    :key="option.value"
                    :command="`theme:${option.value}`"
                    :disabled="themeMode === option.value"
                    >外观：{{ option.label }}{{ themeMode === option.value ? '（当前）' : '' }}</ui-dropdown-item
                  >
                  <ui-dropdown-item command="logout"><LogOut />退出登录</ui-dropdown-item>
                  <ui-dropdown-item v-if="platformVersion" disabled command="version">平台版本 {{ platformVersion }}</ui-dropdown-item>
                </ui-dropdown-menu>
              </template>
            </ui-dropdown>
          </div>
        </header>
        <main ref="contentArea" class="app-content" :class="{ 'app-content--full': current.layout === 'full' }">
          <header v-if="showHeader" class="page-header">
            <h1>{{ current.title }}</h1>
            <p v-if="current.sub">{{ current.sub }}</p>
          </header>
          <component
            :is="current.component"
            v-if="permissionState.ready && current.component"
            :key="`${active}-${pageKey}`"
            v-bind="current.props || {}"
            @navigate="navigate"
          />
          <ui-empty v-else-if="permissionState.ready" description="尚未分配菜单权限，请联系管理员" />
        </main>
      </div>
    </div>
    <GlobalAlertPopup v-if="authenticated && permissionState.ready && can('menu:alarms')" ref="globalAlertPopup" @navigate="navigate" />
    <LivePlayerDialog v-if="authenticated" v-model="livePlayerVisible" :camera="livePlayerCamera" />
    <PasswordChangeDialog
      v-model="passwordDialog"
      :required="passwordChange.required"
      :change-token="passwordChange.token"
      :current-password="passwordChange.current"
      @changed="passwordChanged"
    />
  </ui-config-provider>
</template>
