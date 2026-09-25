<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  Bell,
  Bot,
  Boxes,
  BrainCircuit,
  Cable,
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  Cpu,
  Database,
  FileText,
  FlaskConical,
  LayoutDashboard,
  Library,
  LogOut,
  Menu,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Settings2,
  ShieldCheck,
  SlidersHorizontal,
  Video,
  X
} from '@lucide/vue'
import { UiMessage } from './ui/feedback.js'
import GlobalAlertPopup from './components/GlobalAlertPopup.vue'
import { api, notifyError, session } from './api'
import { pageGuide } from './pageGuide'
import { can, permissionState, refreshPermissions, resetPermissions } from './permissions'
import { startRealtime, stopRealtime } from './realtime'
import { useMediaQuery } from './composables/useMediaQuery'

const DashboardView = defineAsyncComponent(() => import('./views/DashboardView.vue'))
const DevicesView = defineAsyncComponent(() => import('./views/DevicesView.vue'))
const ProductsView = defineAsyncComponent(() => import('./views/ProductsView.vue'))
const ProtocolsView = defineAsyncComponent(() => import('./views/ProtocolsView.vue'))
const TestDeviceView = defineAsyncComponent(() => import('./views/TestDeviceView.vue'))
const CameraMappingsView = defineAsyncComponent(() => import('./views/CameraMappingsView.vue'))
const AlarmsView = defineAsyncComponent(() => import('./views/AlarmsView.vue'))
const HealthInspectionView = defineAsyncComponent(() => import('./views/HealthInspectionView.vue'))
const RawView = defineAsyncComponent(() => import('./views/RawView.vue'))
const RulesView = defineAsyncComponent(() => import('./views/RulesView.vue'))
const KnowledgeView = defineAsyncComponent(() => import('./views/KnowledgeView.vue'))
const AiView = defineAsyncComponent(() => import('./views/AiView.vue'))
const AiProvidersView = defineAsyncComponent(() => import('./views/AiProvidersView.vue'))
const BackupsView = defineAsyncComponent(() => import('./views/BackupsView.vue'))
const AccessView = defineAsyncComponent(() => import('./views/AccessView.vue'))

const authenticated = ref(Boolean(session.token))
const active = ref('dashboard')
const readStoredCollapse = () => { try { return localStorage.getItem('iot:sidebar-collapsed') === 'true' } catch { return false } }
const collapsed = ref(readStoredCollapse())
watch(collapsed, value => { try { localStorage.setItem('iot:sidebar-collapsed', String(value)) } catch { /* 浏览器禁用存储时只影响本次折叠状态。 */ } })
const narrow = useMediaQuery('(max-width: 767px)')
const navOpen = ref(false)
watch(narrow, value => { if (!value) navOpen.value = false })
const contentArea = ref(null)
const pageKey = ref(0)
const loginLoading = ref(false)
const globalAlertPopup = ref(null)
const loginForm = ref({ tenantId: 'tenant_001', username: 'admin', password: '' })
const identity = ref({ tenant: session.tenant, user: session.user, role: session.role })
const currentUser = computed(() => identity.value.user || loginForm.value.username || '账户')
const currentRole = computed(() => ({ admin: '管理员', operator: '运维人员', viewer: '访客' }[identity.value.role] || '平台用户'))

// layout=full 的页面自带标题区并占满内容高度；header=false 的页面暂时保留自己的介绍区。
const pages = {
  dashboard: { ...pageGuide.dashboard, icon: LayoutDashboard, component: DashboardView },
  alarms: { ...pageGuide.alarms, icon: Bell, component: AlarmsView },
  inspection: { ...pageGuide.inspection, icon: ClipboardCheck, component: HealthInspectionView, header: false },
  raw: { ...pageGuide.raw, icon: FileText, component: RawView },
  rules: { ...pageGuide.rules, icon: SlidersHorizontal, component: RulesView },
  devices: { ...pageGuide.devices, icon: Cpu, component: DevicesView },
  products: { ...pageGuide.products, icon: Boxes, component: ProductsView },
  profiles: { ...pageGuide.profiles, icon: Cable, component: ProtocolsView, props: { section: 'profiles' } },
  protocols: { ...pageGuide.protocols, icon: Network, component: ProtocolsView, props: { section: 'protocols' } },
  cameras: { ...pageGuide.cameras, icon: Video, component: CameraMappingsView },
  integration: { ...pageGuide.integration, icon: FlaskConical, component: TestDeviceView },
  ai: { ...pageGuide.ai, icon: Bot, component: AiView, layout: 'full' },
  knowledge: { ...pageGuide.knowledge, icon: Library, component: KnowledgeView, header: false },
  aiProviders: { ...pageGuide.aiProviders, icon: BrainCircuit, component: AiProvidersView, header: false },
  backups: { ...pageGuide.backups, icon: Database, component: BackupsView },
  access: { ...pageGuide.access, icon: ShieldCheck, component: AccessView }
}
const menuGroups = [
  { label: '运行监控', items: ['dashboard', 'alarms', 'inspection', 'raw', 'rules'] },
  { label: '设备与接入', items: ['devices', 'products', 'profiles', 'protocols', 'cameras', 'integration'] },
  { label: '智能助手', items: ['ai', 'knowledge', 'aiProviders'] },
  { label: '系统', items: ['backups', 'access'] }
]
const current = computed(() => pages[active.value] || { title: '暂无可用功能' })
const currentGroup = computed(() => menuGroups.find(group => group.items.includes(active.value))?.label || '')
const showHeader = computed(() => current.value.layout !== 'full' && current.value.header !== false && Boolean(current.value.component))
// 接入点在设备模板详情中管理；没有模板菜单权限的账号仍保留独立入口。
const navigable = name => can('menu:' + name) && !(name === 'profiles' && can('menu:products'))
const visibleGroups = computed(() => menuGroups.map(group => ({ ...group, items: group.items.filter(navigable) })).filter(group => group.items.length))
const firstAllowedPage = () => visibleGroups.value[0]?.items[0] || ''

watch(() => permissionState.accessVersion + '\n' + permissionState.items.join('\n'), (value, old) => {
  if (!authenticated.value || value === old) return
  if (!can('menu:' + active.value)) active.value = firstAllowedPage()
  pageKey.value++
})

async function syncIdentity() {
  if (!authenticated.value) return
  try {
    await refreshPermissions()
    if (!can('menu:' + active.value)) active.value = firstAllowedPage()
  } catch (error) { notifyError(error) }
}

async function login() {
  loginLoading.value = true
  try {
    const data = await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify(loginForm.value) })
    session.save(data, loginForm.value.username)
    identity.value = { tenant: data.tenantId || '', user: loginForm.value.username, role: data.role || '' }
    authenticated.value = true
    permissionState.accessVersion = data.accessVersion || ''
    permissionState.items = data.permissions || []
    permissionState.ready = true
    active.value = firstAllowedPage()
    loginForm.value.password = ''
    if (can(['menu:devices', 'menu:alarms', 'menu:dashboard', 'menu:raw'])) connect()
  } catch (error) {
    notifyError(error)
  } finally {
    loginLoading.value = false
  }
}

function logout() {
  stopRealtime()
  session.clear()
  resetPermissions()
  identity.value = { tenant: '', user: '', role: '' }
  authenticated.value = false
}

function handleAccountCommand(command) {
  if (command === 'logout') logout()
}

function openPage(name, detail) {
  // 旧的导航事件仍可能使用 testDevice，统一落到模拟设备测试页面。
  if (name === 'testDevice') name = 'integration'
  if (name === 'profiles' && can('menu:products')) { name = 'products'; detail = detail && { ...detail, tab: 'access' } }
  if (!pages[name] || !can('menu:' + name)) return
  navOpen.value = false
  if (active.value === name && !detail) return
  sessionStorage.removeItem('iot:navigation-detail')
  active.value = name
  pageKey.value++
  if (detail) sessionStorage.setItem('iot:navigation-detail', JSON.stringify(detail))
  contentArea.value?.scrollTo({ top: 0 })
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

function handleUIAction(payload) {
  try {
    const event = JSON.parse(payload)
    const action = event?.action || {}
    if (action.type === 'OPEN_CAMERA' && typeof action.cameraId === 'string' && action.cameraId) {
      openPage('cameras', { cameraId: action.cameraId, actionId: event.id })
      UiMessage.info(`规则联动：已定位摄像头信息 ${action.cameraId}`)
      return
    }
    const allowedPages = new Set(['dashboard', 'devices', 'products', 'protocols', 'profiles', 'integration', 'testDevice', 'cameras', 'alarms', 'inspection', 'raw', 'rules', 'knowledge', 'aiProviders', 'ai', 'backups'])
    if (action.type === 'OPEN_PAGE' && allowedPages.has(action.page)) {
      openPage(action.page)
      UiMessage.warning('规则联动：已打开相关业务页面')
    }
  } catch {
    // 忽略格式错误或不支持的联动动作。
  }
}

function connect() {
  startRealtime((topic, payload) => {
    if (topic.includes('/ui-action/')) handleUIAction(payload)
    window.dispatchEvent(new CustomEvent('iot:realtime', { detail: { topic, payload } }))
  })
}

function unauthorized() {
  logout()
  UiMessage.error('登录已过期，请重新登录')
}

onMounted(async () => {
  window.addEventListener('iot:unauthorized', unauthorized)
  window.addEventListener('focus', syncIdentity)
  window.addEventListener('keydown', closeNavigationOnEscape)
  await syncIdentity()
  if (authenticated.value && can(['menu:devices', 'menu:alarms', 'menu:dashboard', 'menu:raw'])) connect()
})

onBeforeUnmount(() => {
  window.removeEventListener('iot:unauthorized', unauthorized)
  window.removeEventListener('focus', syncIdentity)
  window.removeEventListener('keydown', closeNavigationOnEscape)
  stopRealtime()
})
</script>

<template>
  <ui-config-provider>
    <div v-if="!authenticated" class="login-page">
      <section class="login-brand-panel" aria-hidden="true">
        <div class="login-brand-panel__top">
          <img class="login-brand-panel__logo" src="/torchlink-sidebar.svg" alt="" />
          <span class="login-chip">消防物联网管理平台</span>
        </div>
        <div class="login-brand-panel__copy">
          <p class="login-eyebrow">智慧消防 · 全域感知</p>
          <h1>连接现场设备<br /><em>守护每一处安全</em></h1>
          <p>从设备接入、实时监测到告警处置，在一个工作台掌握现场运行情况。</p>
        </div>
        <ul class="login-features">
          <li><Network /><div><strong>多协议接入</strong><span>标准 HTTP / MQTT、TCP / UDP 与 Modbus 设备统一登记</span></div></li>
          <li><Bell /><div><strong>实时告警</strong><span>设备上报告警与规则告警在同一处确认和处置</span></div></li>
          <li><ClipboardCheck /><div><strong>智能巡检</strong><span>在线情况、上报时效与活动告警一次汇总</span></div></li>
        </ul>
      </section>
      <section class="login-form-panel">
        <form class="login-form" @submit.prevent="login">
          <img class="login-form__logo" src="/torchlink-login.svg" alt="炬联 TorchLink" />
          <h2>欢迎使用炬联</h2>
          <p class="login-form__lead">登录账户，进入消防物联网工作台</p>
          <div class="login-fields">
            <div class="login-field">
              <label for="tenant-id">租户</label>
              <ui-input id="tenant-id" v-model="loginForm.tenantId" size="large" autocomplete="organization" />
            </div>
            <div class="login-field">
              <label for="username">用户名</label>
              <ui-input id="username" v-model="loginForm.username" size="large" autocomplete="username" placeholder="请输入用户名" required />
            </div>
            <div class="login-field">
              <label for="password">密码</label>
              <ui-input id="password" v-model="loginForm.password" size="large" type="password" autocomplete="current-password" placeholder="请输入密码" required />
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
          <img src="/torchlink-sidebar.svg" alt="炬联 TorchLink" />
          <button v-if="narrow" type="button" class="app-sidebar__close" aria-label="关闭菜单" @click="navOpen = false"><X /></button>
        </div>
        <nav class="app-sidebar__nav" aria-label="主导航">
          <section v-for="group in visibleGroups" :key="group.label" class="nav-group">
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
              @click="openPage(name)"
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
            <button v-if="can('menu:alarms')" class="topbar-button" type="button" aria-label="告警提醒设置" @click="openAlertSettings"><Settings2 /><span>告警提醒</span></button>
            <ui-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <span class="account__avatar" aria-hidden="true">{{ currentRole.slice(0, 1) }}</span>
                <span class="account__copy"><strong>{{ currentUser === 'admin' ? '管理员' : currentUser }}</strong><small>{{ currentRole }}</small></span>
                <ChevronDown class="account__chevron" />
              </button>
              <template #dropdown>
                <ui-dropdown-menu>
                  <ui-dropdown-item command="logout"><LogOut />退出登录</ui-dropdown-item>
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
          <component :is="current.component" v-if="permissionState.ready && current.component" :key="`${active}-${pageKey}`" v-bind="current.props || {}" @navigate="openPage" />
          <ui-empty v-else-if="permissionState.ready" description="尚未分配菜单权限，请联系管理员" />
        </main>
      </div>
    </div>
    <GlobalAlertPopup v-if="authenticated && permissionState.ready && can('menu:alarms')" ref="globalAlertPopup" @navigate="openPage" />
  </ui-config-provider>
</template>
