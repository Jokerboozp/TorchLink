<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import {
  Bell,
  Flame,
  Boxes,
  ChartNoAxesCombined,
  ChevronDown,
  Cpu,
  Database,
  FileText,
  LayoutDashboard,
  Library,
  LogOut,
  MessageCircle,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Settings2,
  Upload,
  Video
} from '@lucide/vue'
import Avatar from './components/ui/Avatar.vue'
import Button from './components/ui/Button.vue'
import Input from './components/ui/Input.vue'
import Label from './components/ui/Label.vue'
import GlobalAlertPopup from './components/GlobalAlertPopup.vue'
import { api, notifyError, session } from './api'
import { pageGuide } from './pageGuide'
import { startRealtime, stopRealtime } from './realtime'

const DashboardView = defineAsyncComponent(() => import('./views/DashboardView.vue'))
const DevicesView = defineAsyncComponent(() => import('./views/DevicesView.vue'))
const ProductsView = defineAsyncComponent(() => import('./views/ProductsView.vue'))
const ProtocolsView = defineAsyncComponent(() => import('./views/ProtocolsView.vue'))
const DeviceAccessView = defineAsyncComponent(() => import('./views/DeviceAccessView.vue'))
const CameraMappingsView = defineAsyncComponent(() => import('./views/CameraMappingsView.vue'))
const AlarmsView = defineAsyncComponent(() => import('./views/AlarmsView.vue'))
const HealthInspectionView = defineAsyncComponent(() => import('./views/HealthInspectionView.vue'))
const RawView = defineAsyncComponent(() => import('./views/RawView.vue'))
const RulesView = defineAsyncComponent(() => import('./views/RulesView.vue'))
const KnowledgeView = defineAsyncComponent(() => import('./views/KnowledgeView.vue'))
const AiView = defineAsyncComponent(() => import('./views/AiView.vue'))
const AiProvidersView = defineAsyncComponent(() => import('./views/AiProvidersView.vue'))
const BackupsView = defineAsyncComponent(() => import('./views/BackupsView.vue'))

const authenticated = ref(Boolean(session.token))
const active = ref('dashboard')
const collapsed = ref(false)
const contentArea = ref(null)
const pageKey = ref(0)
const loginLoading = ref(false)
const globalAlertPopup = ref(null)
const loginForm = ref({ tenantId: 'tenant_001', username: 'admin', password: '' })
const identity = ref({ tenant: session.tenant, user: session.user, role: session.role })
const currentTenant = computed(() => identity.value.tenant || loginForm.value.tenantId || '—')
const currentUser = computed(() => identity.value.user || loginForm.value.username || '账户')
const currentRole = computed(() => ({ admin: '管理员', operator: '运维人员', viewer: '访客' }[identity.value.role] || '平台用户'))

const pages = {
  dashboard: { ...pageGuide.dashboard, icon: LayoutDashboard, component: DashboardView },
  devices: { ...pageGuide.devices, icon: Cpu, component: DevicesView },
  products: { ...pageGuide.products, icon: Boxes, component: ProductsView },
  protocols: { ...pageGuide.protocols, icon: Network, component: ProtocolsView, props: { section: 'protocols' } },
  profiles: { ...pageGuide.profiles, icon: Settings2, component: ProtocolsView, props: { section: 'profiles' } },
  integration: { ...pageGuide.integration, icon: Upload, component: DeviceAccessView },
  cameras: { ...pageGuide.cameras, icon: Video, component: CameraMappingsView },
  alarms: { ...pageGuide.alarms, icon: Bell, component: AlarmsView },
  inspection: { ...pageGuide.inspection, icon: ChartNoAxesCombined, component: HealthInspectionView },
  raw: { ...pageGuide.raw, icon: FileText, component: RawView },
  rules: { ...pageGuide.rules, icon: Settings2, component: RulesView },
  knowledge: { ...pageGuide.knowledge, icon: Library, component: KnowledgeView },
  aiProviders: { ...pageGuide.aiProviders, icon: Cpu, component: AiProvidersView },
  ai: { ...pageGuide.ai, icon: MessageCircle, component: AiView },
  backups: { ...pageGuide.backups, icon: Database, component: BackupsView }
}
const current = computed(() => pages[active.value])
const menuGroups = [
  { label: '控制中心', items: ['dashboard'] },
  { label: '设备接入', items: ['protocols', 'products', 'devices', 'profiles', 'integration', 'cameras'] },
  { label: '监测与处置', items: ['alarms', 'inspection', 'raw', 'rules'] },
  { label: '智能助手', items: ['aiProviders', 'ai', 'knowledge'] },
  { label: '系统维护', items: ['backups'] }
]
const currentGroup = computed(() => menuGroups.find(group => group.items.includes(active.value))?.label || '工作台')
async function login() {
  loginLoading.value = true
  try {
    const data = await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify(loginForm.value) })
    session.save(data, loginForm.value.username)
    identity.value = { tenant: data.tenantId || '', user: loginForm.value.username, role: data.role || '' }
    authenticated.value = true
    active.value = 'dashboard'
    connect()
  } catch (error) {
    notifyError(error)
  } finally {
    loginLoading.value = false
  }
}

function logout() {
  stopRealtime()
  session.clear()
  identity.value = { tenant: '', user: '', role: '' }
  authenticated.value = false
}

function handleAccountCommand(command) {
  if (command === 'logout') logout()
}

function openPage(name, detail) {
  if (name === 'testDevice') {
    name = 'integration'
    detail = { ...detail, tab: 'testDevice' }
  }
  if (!pages[name]) return
  if (active.value === name && !detail) return
  sessionStorage.removeItem('iot:navigation-detail')
  active.value = name
  pageKey.value++
  if (detail) sessionStorage.setItem('iot:navigation-detail', JSON.stringify(detail))
  contentArea.value?.scrollTo({ top: 0 })
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
      ElMessage.info(`规则联动：已定位摄像头信息 ${action.cameraId}`)
      return
    }
    const allowedPages = new Set(['dashboard', 'devices', 'products', 'protocols', 'profiles', 'integration', 'testDevice', 'cameras', 'alarms', 'inspection', 'raw', 'rules', 'knowledge', 'aiProviders', 'ai', 'backups'])
    if (action.type === 'OPEN_PAGE' && allowedPages.has(action.page)) {
      openPage(action.page)
      ElMessage.warning('规则联动：已打开相关业务页面')
    }
  } catch {
    // Ignore malformed or unsupported UI actions.
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
  ElMessage.error('登录已过期，请重新登录')
}

onMounted(() => {
  window.addEventListener('iot:unauthorized', unauthorized)
  if (authenticated.value) connect()
})

onBeforeUnmount(() => {
  window.removeEventListener('iot:unauthorized', unauthorized)
  stopRealtime()
})
</script>

<template>
  <el-config-provider :locale="zhCn" size="small">
    <div v-if="!authenticated" class="login-page">
      <section class="login-panel">
        <form class="login-form" @submit.prevent="login">
          <span class="login-mark"><Flame :size="32" /></span>
          <h2>登录炬联</h2>
          <p>消防物联网管理平台</p>
          <div class="login-fields">
            <div class="login-field">
              <Label for="tenant-id">租户</Label>
              <Input id="tenant-id" v-model="loginForm.tenantId" autocomplete="organization" />
            </div>
            <div class="login-field">
              <Label for="username">用户名</Label>
              <Input id="username" v-model="loginForm.username" autocomplete="username" />
            </div>
            <div class="login-field">
              <Label for="password">密码</Label>
              <Input id="password" v-model="loginForm.password" type="password" autocomplete="current-password" />
            </div>
          </div>
          <Button type="submit" class="login-submit" :loading="loginLoading">进入平台</Button>
        </form>
      </section>
    </div>

    <div v-else class="app-shell">
      <aside class="app-aside" :class="{ 'is-collapsed': collapsed }">
        <div class="brand"><span><Flame :size="23" aria-hidden="true" /></span><div v-show="!collapsed"><strong>炬联</strong></div></div>
        <nav class="menu-scroll" aria-label="主导航">
          <div class="menu-scroll-inner">
            <template v-for="group in menuGroups" :key="group.label">
              <div v-show="!collapsed" class="menu-group">{{ group.label }}</div>
              <button v-for="name in group.items" :key="name" type="button" class="menu-item" :class="{ active: active === name }" :aria-label="pages[name].title" :title="pages[name].title" :aria-current="active === name ? 'page' : undefined" @click="openPage(name)">
                <component :is="pages[name].icon" />
                <span v-show="!collapsed">{{ pages[name].title }}</span>
              </button>
            </template>
          </div>
        </nav>
      </aside>

      <main class="app-main">
        <header class="topbar">
          <div class="title-area">
            <button class="collapse-button" :aria-label="collapsed ? '展开菜单' : '折叠菜单'" :aria-expanded="!collapsed" @click="collapsed = !collapsed"><component :is="collapsed ? PanelLeftOpen : PanelLeftClose" /></button>
            <span class="workspace-label">{{ currentGroup }}</span>
          </div>
          <div class="top-actions">
            <button class="alert-settings-trigger" type="button" aria-label="告警提醒设置" @click="openAlertSettings"><Settings2 /><span>告警提醒</span></button>
            <el-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <Avatar>{{ currentRole.slice(0, 1) }}</Avatar>
                <span class="account-copy"><strong>{{ currentUser === 'admin' ? '管理员' : currentUser }}</strong><small>{{ currentRole }}</small></span>
                <ChevronDown class="account-chevron" />
              </button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item disabled>账户：{{ currentUser }} · 租户：{{ currentTenant }}</el-dropdown-item>
                  <el-dropdown-item divided command="logout"><LogOut />退出登录</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </header>
        <section ref="contentArea" class="main-content" :class="{ 'main-content--ai': active === 'ai' }">
          <div class="page-context"><h1>{{ current.title }}</h1></div>
          <component :is="current.component" :key="`${active}-${pageKey}`" v-bind="current.props || {}" @navigate="openPage" />
        </section>
      </main>
    </div>
    <GlobalAlertPopup v-if="authenticated" ref="globalAlertPopup" @navigate="openPage" />
  </el-config-provider>
</template>
