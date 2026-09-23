<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import {
  Bell,
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
import GlobalAlertPopup from './components/GlobalAlertPopup.vue'
import { api, notifyError, session } from './api'
import { pageGuide } from './pageGuide'
import { can, permissionState, refreshPermissions, resetPermissions } from './permissions'
import { startRealtime, stopRealtime } from './realtime'

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
const collapsed = ref(localStorage.getItem('iot:sidebar-collapsed') === 'true')
watch(collapsed, value => localStorage.setItem('iot:sidebar-collapsed', String(value)))
const closedGroups = ref([])
function toggleGroup(name) { closedGroups.value = closedGroups.value.includes(name) ? closedGroups.value.filter(item => item !== name) : [...closedGroups.value, name] }
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
  integration: { ...pageGuide.integration, icon: Upload, component: TestDeviceView },
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
const current = computed(() => pages[active.value] || {title:'暂无可用功能'})
const menuGroups = [
  { label: '控制中心', items: ['dashboard'] },
  { label: '设备接入', items: ['protocols', 'products', 'devices', 'profiles', 'integration', 'cameras'] },
  { label: '监测与处置', items: ['alarms', 'inspection', 'raw', 'rules'] },
  { label: '智能助手', items: ['aiProviders', 'ai', 'knowledge'] },
  { label: '系统维护', items: ['backups','access'] }
]
pages.access = {title:'用户与权限',icon:Settings2,component:AccessView}
const visibleGroups = computed(() => menuGroups.map(group=>({...group,items:group.items.filter(name=>can('menu:'+name))})).filter(group=>group.items.length))
watch(() => permissionState.items.join('\n'), (value, old) => {
 if (!authenticated.value || value === old) return
 if (!can('menu:' + active.value)) active.value = visibleGroups.value[0]?.items[0] || ''
 pageKey.value++
})
async function syncIdentity(){
 if(!authenticated.value)return
 try{await refreshPermissions();if(!can('menu:'+active.value))active.value=visibleGroups.value[0]?.items[0]||''}catch(error){notifyError(error)}
}
const currentGroup = computed(() => menuGroups.find(group => group.items.includes(active.value))?.label || '工作台')
async function login() {
  loginLoading.value = true
  try {
    const data = await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify(loginForm.value) })
    session.save(data, loginForm.value.username)
    identity.value = { tenant: data.tenantId || '', user: loginForm.value.username, role: data.role || '' }
    authenticated.value = true
    permissionState.items=data.permissions || [];permissionState.ready=true
    active.value = visibleGroups.value[0]?.items[0] || ''
    loginForm.value.password=''
    if(can(['menu:devices','menu:alarms','menu:dashboard','menu:raw']))connect()
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
  // 历史导航事件仍使用 testDevice，统一落到当前的接入测试菜单。
  if (name === 'testDevice') {
    name = 'integration'
  }
  if (!pages[name] || !can('menu:'+name)) return
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

onMounted(async () => {
  window.addEventListener('iot:unauthorized', unauthorized)
  window.addEventListener('focus',syncIdentity)
  await syncIdentity()
  if (authenticated.value && can(['menu:devices','menu:alarms','menu:dashboard','menu:raw'])) connect()
})

onBeforeUnmount(() => {
  window.removeEventListener('iot:unauthorized', unauthorized)
  window.removeEventListener('focus',syncIdentity)
  stopRealtime()
})
</script>

<template>
  <el-config-provider :locale="zhCn" size="small">
    <div v-if="!authenticated" class="login-page">
      <section class="login-intro"><div class="login-brand"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></div><span class="login-eyebrow">消防物联网管理平台</span><h1>连接每一台设备<br />守护每一处安全</h1><p>从设备接入、实时监测到告警处置，<br />在一个工作台掌握现场运行情况。</p><div class="login-capabilities"><span><Network />多协议接入</span><span><Bell />实时告警</span><span><ChartNoAxesCombined />智能巡检</span></div><div class="login-grid-art" aria-hidden="true"><span></span><span></span><span></span><i></i></div></section>
      <section class="login-panel">
        <form class="login-form" @submit.prevent="login">
          <span class="login-mark"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></span>
          <h2>欢迎回来</h2>
          <p>登录你的账户，进入炬联工作台</p>
          <div class="login-fields">
            <div class="login-field">
              <label for="tenant-id">租户</label>
              <el-input id="tenant-id" v-model="loginForm.tenantId" size="large" autocomplete="organization" />
            </div>
            <div class="login-field">
              <label for="username">用户名</label>
              <el-input id="username" v-model="loginForm.username" size="large" autocomplete="username" placeholder="请输入用户名" required />
            </div>
            <div class="login-field">
              <label for="password">密码</label>
              <el-input id="password" v-model="loginForm.password" size="large" type="password" autocomplete="current-password" placeholder="请输入密码" required />
            </div>
          </div>
          <el-button native-type="submit" type="primary" size="large" class="login-submit" :loading="loginLoading">进入平台</el-button>
          <p class="login-help">账户由管理员分配 · 按授权访问设备和功能</p>
        </form>
      </section>
    </div>

    <div v-else class="app-shell">
      <aside class="app-aside" :class="{ 'is-collapsed': collapsed }">
        <div class="brand"><span class="brand-logo"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></span></div>
        <nav class="menu-scroll" aria-label="主导航">
          <div class="menu-scroll-inner">
            <template v-for="group in visibleGroups" :key="group.label">
              <button v-show="!collapsed" class="menu-group menu-group-toggle" :aria-expanded="!closedGroups.includes(group.label)" @click="toggleGroup(group.label)">{{ group.label }}<ChevronDown :class="{closed:closedGroups.includes(group.label)}" /></button>
              <button v-for="name in group.items" v-show="collapsed || !closedGroups.includes(group.label)" :key="name" type="button" class="menu-item" :class="{ active: active === name }" :aria-label="pages[name].title" :title="pages[name].title" :aria-current="active === name ? 'page' : undefined" @click="openPage(name)">
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
            <button v-if="can('menu:alarms')" class="alert-settings-trigger" type="button" aria-label="告警提醒设置" @click="openAlertSettings"><Settings2 /><span>告警提醒</span></button>
            <el-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <span class="account-avatar" aria-hidden="true">{{ currentRole.slice(0, 1) }}</span>
                <span class="account-copy"><strong>{{ currentUser === 'admin' ? '管理员' : currentUser }}</strong><small>{{ currentRole }}</small></span>
                <ChevronDown class="account-chevron" />
              </button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="logout"><LogOut />退出登录</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </header>
        <section ref="contentArea" class="main-content" :class="{ 'main-content--ai': active === 'ai' }">
          <div class="page-context"><h1>{{ current.title }}</h1></div>
          <component v-if="permissionState.ready && current.component" :is="current.component" :key="`${active}-${pageKey}`" v-bind="current.props || {}" @navigate="openPage" />
          <el-empty v-else-if="permissionState.ready" description="尚未分配菜单权限，请联系管理员" />
        </section>
      </main>
    </div>
    <GlobalAlertPopup v-if="authenticated && permissionState.ready && can('menu:alarms')" ref="globalAlertPopup" @navigate="openPage" />
  </el-config-provider>
</template>
