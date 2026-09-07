<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref } from 'vue'
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
  FlaskConical,
  LayoutDashboard,
  Library,
  LogOut,
  MessageCircle,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Settings2,
  Search,
  Upload,
  Video
} from '@lucide/vue'
import Avatar from './components/ui/Avatar.vue'
import Button from './components/ui/Button.vue'
import Input from './components/ui/Input.vue'
import Label from './components/ui/Label.vue'
import GlobalAlertPopup from './components/GlobalAlertPopup.vue'
import { api, notifyError, session } from './api'
import { startRealtime, stopRealtime } from './realtime'

const DashboardView = defineAsyncComponent(() => import('./views/DashboardView.vue'))
const DevicesView = defineAsyncComponent(() => import('./views/DevicesView.vue'))
const ProductsView = defineAsyncComponent(() => import('./views/ProductsView.vue'))
const ProtocolsView = defineAsyncComponent(() => import('./views/ProtocolsView.vue'))
const IntegrationView = defineAsyncComponent(() => import('./views/IntegrationView.vue'))
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

const authenticated = ref(Boolean(session.token))
const active = ref('dashboard')
const collapsed = ref(false)
const launcherVisible = ref(false)
const pageSearch = ref('')
const contentArea = ref(null)
const pageKey = ref(0)
const loginLoading = ref(false)
const globalAlertPopup = ref(null)
const loginForm = ref({ tenantId: 'tenant_001', username: 'admin', password: '' })
const identity = ref({ tenant: session.tenant, user: session.user, role: session.role })
const currentTenant = computed(() => identity.value.tenant || loginForm.value.tenantId || '—')
const currentUser = computed(() => identity.value.user || loginForm.value.username || '账户')
const currentRole = computed(() => ({ admin: '管理员', operator: '运维人员', viewer: '访客' }[identity.value.role] || identity.value.role || '平台用户'))

const pages = {
  dashboard: { title: '运行总览', sub: '城市级消防感知与告警态势', icon: LayoutDashboard, component: DashboardView },
  devices: { title: '设备管理', sub: '注册、启停、凭证和实时状态统一管理', icon: Cpu, component: DevicesView },
  products: { title: '产品管理', sub: '产品模型与协议包绑定', icon: Boxes, component: ProductsView },
  protocols: { title: '设备接入', sub: 'Go 协议包、TCP/UDP 接入与版本热更新', icon: Network, component: ProtocolsView },
  integration: { title:'接入指南', sub: '真实设备 HTTP / MQTT 参数与数据联调', icon: Upload, component: IntegrationView },
  testDevice: { title:'测试设备', sub: '模板化发送数据、事件、报警和恢复报文', icon: FlaskConical, component: TestDeviceView },
  cameras: { title: '摄像头映射', sub: '视频平台摄像头、空间位置与物联设备关联', icon: Video, component: CameraMappingsView },
  alarms: { title: '告警中心', sub: '告警确认、恢复与闭环处置', icon: Bell, component: AlarmsView },
  inspection: { title:'智能巡检', sub: '设备健康、数据新鲜度与活动告警分析', icon: ChartNoAxesCombined, component: HealthInspectionView },
  raw: { title: '原始报文', sub: '证据链检索、审计与回放', icon: FileText, component: RawView },
  rules: { title: '告警规则', sub: '可审计的动态规则与 AI 草稿', icon: Settings2, component: RulesView },
  knowledge: { title: 'Agent 知识库', sub: '文档直接归属 Agent，并使用持久化向量索引', icon: Library, component: KnowledgeView },
  aiProviders: { title: 'AI 模型管理', sub: '模型服务、接口密钥和 AI 业务统一管理', icon: Cpu, component: AiProvidersView },
  ai: { title: 'AI 工作流', sub: '受控工具与知识库问答', icon: MessageCircle, component: AiView },
  backups: { title: '备份中心', sub: '设备原始数据与解析数据的每日备份', icon: Database, component: BackupsView }
}
const current = computed(() => pages[active.value])
const menuGroups = [
  { label: '控制中心', items: ['dashboard'] },
  { label: '设备接入', items: ['products', 'protocols', 'devices', 'integration', 'testDevice', 'cameras'] },
  { label: '监测与处置', items: ['alarms', 'inspection', 'raw', 'rules'] },
  { label: '智能助手', items: ['aiProviders', 'ai', 'knowledge'] },
  { label: '系统维护', items: ['backups'] }
]
const relatedPages = {
  dashboard: ['devices', 'alarms', 'inspection'],
  products: ['protocols', 'devices'], protocols: ['products', 'integration'],
  devices: ['integration', 'raw'], integration: ['testDevice', 'raw'],
  testDevice: ['raw', 'alarms'], cameras: ['devices'],
  alarms: ['rules', 'inspection'], inspection: ['devices', 'alarms'],
  raw: ['protocols', 'devices'], rules: ['alarms', 'ai'],
  knowledge: ['ai', 'aiProviders'], aiProviders: ['ai', 'knowledge'], ai: ['aiProviders', 'knowledge', 'rules'], backups: ['raw']
}
const filteredGroups = computed(() => menuGroups.map(group => ({
  ...group,
  items: group.items.filter(name => `${group.label} ${pages[name].title} ${pages[name].sub}`.toLowerCase().includes(pageSearch.value.trim().toLowerCase()))
})).filter(group => group.items.length))

function openLauncher() {
  pageSearch.value = ''
  launcherVisible.value = true
}

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
  if (!pages[name]) return
  launcherVisible.value = false
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
    const allowedPages = new Set(['dashboard', 'devices', 'products', 'protocols', 'integration', 'testDevice', 'cameras', 'alarms', 'inspection', 'raw', 'rules', 'knowledge', 'aiProviders', 'ai', 'backups'])
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
      <section class="login-hero">
        <div class="hero-brand"><span>IoT</span><strong>消防智联平台</strong></div>
        <div>
          <span class="eyebrow">FIRE SAFETY · IOT PLATFORM</span>
          <h1>连接设备，洞察现场，<br />驱动消防业务。</h1>
          <p>统一管理产品、设备、规则与视频资源，让城市消防物联数据在一个平台内完成接入、监控和智能研判。</p>
        </div>
        <div class="hero-tags"><span>统一设备模型</span><span>实时规则引擎</span><span>AI 辅助研判</span></div>
      </section>

      <section class="login-panel">
        <form class="login-form" @submit.prevent="login">
          <span class="form-kicker">欢迎回来</span>
          <h2>登录管理平台</h2>
          <p>使用您的平台账号继续</p>
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
        <div class="brand"><span>IoT</span><div v-show="!collapsed"><strong>消防智联</strong><small>物联网管理平台</small></div></div>
        <div class="menu-scroll">
          <div class="menu-scroll-inner">
            <template v-for="group in menuGroups" :key="group.label">
              <div v-show="!collapsed" class="menu-group">{{ group.label }}</div>
              <button v-for="name in group.items" :key="name" type="button" class="menu-item" :class="{ active: active === name }" :aria-label="pages[name].title" :title="pages[name].title" :aria-current="active === name ? 'page' : undefined" @click="openPage(name)">
                <component :is="pages[name].icon" />
                <span v-show="!collapsed">{{ pages[name].title }}</span>
              </button>
            </template>
          </div>
        </div>
      </aside>

      <main class="app-main">
        <header class="topbar">
          <div class="title-area">
            <button class="collapse-button" :aria-label="collapsed ? '展开菜单' : '折叠菜单'" :aria-expanded="!collapsed" @click="collapsed = !collapsed"><component :is="collapsed ? PanelLeftOpen : PanelLeftClose" /></button>
            <div><span>首页 / {{ current.title }}</span><h2>{{ current.title }}</h2><p>{{ current.sub }}</p></div>
          </div>
          <div class="top-actions">
            <button class="launcher-trigger" type="button" @click="openLauncher"><Search /><span>全部功能</span></button>
            <button class="alert-settings-trigger" type="button" aria-label="告警提醒设置" @click="openAlertSettings"><Settings2 /><span>告警提醒</span></button>
            <span class="tenant-pill">{{ currentTenant }}</span>
            <el-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <Avatar>{{ currentUser.slice(0, 1) }}</Avatar>
                <span class="account-copy"><strong>{{ currentUser }}</strong><small>{{ currentRole }}</small></span>
                <ChevronDown class="account-chevron" />
              </button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item disabled>{{ currentTenant }}</el-dropdown-item>
                  <el-dropdown-item divided command="logout"><LogOut />退出登录</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </header>
        <section ref="contentArea" class="main-content">
          <div class="page-context">
            <div class="page-context-copy"><span class="page-context-icon"><component :is="current.icon" /></span><div><strong>{{ current.title }}</strong><p>{{ current.sub }}</p></div></div>
            <div class="related-actions" aria-label="相关功能"><span>相关功能</span><el-button v-for="name in relatedPages[active]" :key="name" plain @click="openPage(name)"><component :is="pages[name].icon" />{{ pages[name].title }}</el-button></div>
          </div>
          <component :is="current.component" :key="`${active}-${pageKey}`" @navigate="openPage" />
        </section>
      </main>
    </div>
    <el-dialog v-if="authenticated" v-model="launcherVisible" title="全部功能" width="min(820px, 94vw)" class="function-launcher">
      <el-input v-model="pageSearch" clearable placeholder="搜索功能，例如：设备、告警、备份" aria-label="搜索系统功能"><template #prefix><Search :size="16" /></template></el-input>
      <div v-for="group in filteredGroups" :key="group.label" class="launcher-group"><h3>{{ group.label }}</h3><div class="launcher-grid">
        <button v-for="name in group.items" :key="name" type="button" class="launcher-item" :class="{ active: active === name }" :aria-current="active === name ? 'page' : undefined" @click="openPage(name)"><component :is="pages[name].icon" /><span><strong>{{ pages[name].title }}</strong><small>{{ pages[name].sub }}</small></span></button>
      </div></div>
      <el-empty v-if="!filteredGroups.length" description="没有匹配的功能，请尝试其他关键词" :image-size="64" />
      <template #footer><el-button @click="launcherVisible = false">关闭</el-button></template>
    </el-dialog>
    <GlobalAlertPopup v-if="authenticated" ref="globalAlertPopup" @navigate="openPage" />
  </el-config-provider>
</template>
