<script setup>
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from './ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { /* 引入当前代码需要的依赖。 */
  Bell, /* 执行当前语句并推进处理流程。 */
  Boxes, /* 执行当前语句并推进处理流程。 */
  ChartNoAxesCombined, /* 执行当前语句并推进处理流程。 */
  ChevronDown, /* 执行当前语句并推进处理流程。 */
  Cpu, /* 执行当前语句并推进处理流程。 */
  Database, /* 执行当前语句并推进处理流程。 */
  FileText, /* 执行当前语句并推进处理流程。 */
  LayoutDashboard, /* 执行当前语句并推进处理流程。 */
  Library, /* 执行当前语句并推进处理流程。 */
  LogOut, /* 执行当前语句并推进处理流程。 */
  MessageCircle, /* 执行当前语句并推进处理流程。 */
  Network, /* 执行当前语句并推进处理流程。 */
  PanelLeftClose, /* 执行当前语句并推进处理流程。 */
  PanelLeftOpen, /* 执行当前语句并推进处理流程。 */
  Settings2, /* 执行当前语句并推进处理流程。 */
  Upload, /* 执行当前语句并推进处理流程。 */
  Video, /* 执行当前语句并推进处理流程。 */
  X /* 关闭工作区标签页。 */
} from '@lucide/vue' /* 结束当前表达式或代码块。 */
import GlobalAlertPopup from './components/GlobalAlertPopup.vue' /* 引入当前代码需要的依赖。 */
import { api, notifyError, session } from './api' /* 引入当前代码需要的依赖。 */
import { pageGuide } from './pageGuide' /* 引入当前代码需要的依赖。 */
import { can, permissionState, refreshPermissions, resetPermissions } from './permissions' /* 引入当前代码需要的依赖。 */
import { startRealtime, stopRealtime } from './realtime' /* 引入当前代码需要的依赖。 */

const DashboardView = defineAsyncComponent(() => import('./views/DashboardView.vue')) /* 声明 DashboardView。 */
const DevicesView = defineAsyncComponent(() => import('./views/DevicesView.vue')) /* 声明 DevicesView。 */
const ProductsView = defineAsyncComponent(() => import('./views/ProductsView.vue')) /* 声明 ProductsView。 */
const ProtocolsView = defineAsyncComponent(() => import('./views/ProtocolsView.vue')) /* 声明 ProtocolsView。 */
const TestDeviceView = defineAsyncComponent(() => import('./views/TestDeviceView.vue')) /* 声明 TestDeviceView。 */
const CameraMappingsView = defineAsyncComponent(() => import('./views/CameraMappingsView.vue')) /* 声明 CameraMappingsView。 */
const AlarmsView = defineAsyncComponent(() => import('./views/AlarmsView.vue')) /* 声明 AlarmsView。 */
const HealthInspectionView = defineAsyncComponent(() => import('./views/HealthInspectionView.vue')) /* 声明 HealthInspectionView。 */
const RawView = defineAsyncComponent(() => import('./views/RawView.vue')) /* 声明 RawView。 */
const RulesView = defineAsyncComponent(() => import('./views/RulesView.vue')) /* 声明 RulesView。 */
const KnowledgeView = defineAsyncComponent(() => import('./views/KnowledgeView.vue')) /* 声明 KnowledgeView。 */
const AiView = defineAsyncComponent(() => import('./views/AiView.vue')) /* 声明 AiView。 */
const AiProvidersView = defineAsyncComponent(() => import('./views/AiProvidersView.vue')) /* 声明 AiProvidersView。 */
const BackupsView = defineAsyncComponent(() => import('./views/BackupsView.vue')) /* 声明 BackupsView。 */
const AccessView = defineAsyncComponent(() => import('./views/AccessView.vue')) /* 声明 AccessView。 */

const authenticated = ref(Boolean(session.token)) /* 声明 authenticated。 */
const active = ref('dashboard') /* 声明 active。 */
const openedTabs = ref(['dashboard']) /* 保存本次会话打开的工作区页面。 */
const collapsed = ref(localStorage.getItem('iot:sidebar-collapsed') === 'true') /* 声明 collapsed。 */
watch(collapsed, value => localStorage.setItem('iot:sidebar-collapsed', String(value))) /* 执行当前语句并推进处理流程。 */
const closedGroups = ref([]) /* 声明 closedGroups。 */
function toggleGroup(name) { closedGroups.value = closedGroups.value.includes(name) ? closedGroups.value.filter(item => item !== name) : [...closedGroups.value, name] } /* 定义 toggleGroup 函数。 */
const contentArea = ref(null) /* 声明 contentArea。 */
const pageKey = ref(0) /* 声明 pageKey。 */
const loginLoading = ref(false) /* 声明 loginLoading。 */
const globalAlertPopup = ref(null) /* 声明 globalAlertPopup。 */
const loginForm = ref({ tenantId: 'tenant_001', username: 'admin', password: '' }) /* 声明 loginForm。 */
const identity = ref({ tenant: session.tenant, user: session.user, role: session.role }) /* 声明 identity。 */
const currentTenant = computed(() => identity.value.tenant || loginForm.value.tenantId || '—') /* 声明 currentTenant。 */
const currentUser = computed(() => identity.value.user || loginForm.value.username || '账户') /* 声明 currentUser。 */
const currentRole = computed(() => ({ admin: '管理员', operator: '运维人员', viewer: '访客' }[identity.value.role] || '平台用户')) /* 声明 currentRole。 */

const pages = { /* 声明 pages。 */
  dashboard: { ...pageGuide.dashboard, icon: LayoutDashboard, component: DashboardView }, /* 执行当前语句并推进处理流程。 */
  devices: { ...pageGuide.devices, icon: Cpu, component: DevicesView }, /* 执行当前语句并推进处理流程。 */
  products: { ...pageGuide.products, icon: Boxes, component: ProductsView }, /* 执行当前语句并推进处理流程。 */
  protocols: { ...pageGuide.protocols, icon: Network, component: ProtocolsView, props: { section: 'protocols' } }, /* 执行当前语句并推进处理流程。 */
  profiles: { ...pageGuide.profiles, icon: Settings2, component: ProtocolsView, props: { section: 'profiles' } }, /* 执行当前语句并推进处理流程。 */
  integration: { ...pageGuide.integration, icon: Upload, component: TestDeviceView }, /* 执行当前语句并推进处理流程。 */
  cameras: { ...pageGuide.cameras, icon: Video, component: CameraMappingsView }, /* 执行当前语句并推进处理流程。 */
  alarms: { ...pageGuide.alarms, icon: Bell, component: AlarmsView }, /* 执行当前语句并推进处理流程。 */
  inspection: { ...pageGuide.inspection, icon: ChartNoAxesCombined, component: HealthInspectionView }, /* 执行当前语句并推进处理流程。 */
  raw: { ...pageGuide.raw, icon: FileText, component: RawView }, /* 执行当前语句并推进处理流程。 */
  rules: { ...pageGuide.rules, icon: Settings2, component: RulesView }, /* 执行当前语句并推进处理流程。 */
  knowledge: { ...pageGuide.knowledge, icon: Library, component: KnowledgeView }, /* 执行当前语句并推进处理流程。 */
  aiProviders: { ...pageGuide.aiProviders, icon: Cpu, component: AiProvidersView }, /* 执行当前语句并推进处理流程。 */
  ai: { ...pageGuide.ai, icon: MessageCircle, component: AiView }, /* 执行当前语句并推进处理流程。 */
  backups: { ...pageGuide.backups, icon: Database, component: BackupsView } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
const current = computed(() => pages[active.value] || {title:'暂无可用功能'}) /* 声明 current。 */
const menuGroups = [ /* 声明 menuGroups。 */
  { label: '控制中心', items: ['dashboard'] }, /* 执行当前语句并推进处理流程。 */
  { label: '设备接入', items: ['protocols', 'products', 'devices', 'profiles', 'integration', 'cameras'] }, /* 执行当前语句并推进处理流程。 */
  { label: '监测与处置', items: ['alarms', 'inspection', 'raw', 'rules'] }, /* 执行当前语句并推进处理流程。 */
  { label: '智能助手', items: ['aiProviders', 'ai', 'knowledge'] }, /* 执行当前语句并推进处理流程。 */
  { label: '系统维护', items: ['backups','access'] } /* 执行当前语句并推进处理流程。 */
] /* 结束当前表达式或代码块。 */
pages.access = {title:'用户与权限',icon:Settings2,component:AccessView} /* 更新 pages.access 的值。 */
const visibleGroups = computed(() => menuGroups.map(group=>({...group,items:group.items.filter(name=>can('menu:'+name))})).filter(group=>group.items.length)) /* 声明 visibleGroups。 */
watch(() => permissionState.items.join('\n'), (value, old) => { /* 执行当前语句并推进处理流程。 */
 if (!authenticated.value || value === old) return /* 判断条件并选择处理分支。 */
 if (!can('menu:' + active.value)) active.value = visibleGroups.value[0]?.items[0] || '' /* 判断条件并选择处理分支。 */
 openedTabs.value = openedTabs.value.filter(name => can('menu:' + name)) /* 权限收窄后移除无权访问的标签。 */
 if (active.value && !openedTabs.value.includes(active.value)) openedTabs.value.push(active.value) /* 保留当前可访问页面。 */
 pageKey.value++ /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
async function syncIdentity(){ /* 定义 syncIdentity 函数。 */
 if(!authenticated.value)return /* 判断条件并选择处理分支。 */
 try{await refreshPermissions();if(!can('menu:'+active.value))active.value=visibleGroups.value[0]?.items[0]||'';openedTabs.value=openedTabs.value.filter(name=>can('menu:'+name));if(active.value&&!openedTabs.value.includes(active.value))openedTabs.value.push(active.value)}catch(error){notifyError(error)} /* 同步权限后保持标签栏只含可访问页面。 */
} /* 结束当前表达式或代码块。 */
const currentGroup = computed(() => menuGroups.find(group => group.items.includes(active.value))?.label || '工作台') /* 声明 currentGroup。 */
async function login() { /* 定义 login 函数。 */
  loginLoading.value = true /* 更新 loginLoading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const data = await api('/api/v1/auth/login', { method: 'POST', body: JSON.stringify(loginForm.value) }) /* 声明 data。 */
    session.save(data, loginForm.value.username) /* 执行当前语句并推进处理流程。 */
    identity.value = { tenant: data.tenantId || '', user: loginForm.value.username, role: data.role || '' } /* 更新 identity.value 的值。 */
    authenticated.value = true /* 更新 authenticated.value 的值。 */
    permissionState.items=data.permissions || [];permissionState.ready=true /* 更新 permissionState.items 的值。 */
    active.value = visibleGroups.value[0]?.items[0] || '' /* 更新 active.value 的值。 */
    openedTabs.value = active.value ? [active.value] : [] /* 新登录会话从首个可访问页面开始。 */
    loginForm.value.password='' /* 更新 loginForm.value.password 的值。 */
    if(can(['menu:devices','menu:alarms','menu:dashboard','menu:raw']))connect() /* 判断条件并选择处理分支。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    loginLoading.value = false /* 更新 loginLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function logout() { /* 定义 logout 函数。 */
  stopRealtime() /* 执行当前语句并推进处理流程。 */
  session.clear() /* 执行当前语句并推进处理流程。 */
  resetPermissions() /* 执行当前语句并推进处理流程。 */
  identity.value = { tenant: '', user: '', role: '' } /* 更新 identity.value 的值。 */
  authenticated.value = false /* 更新 authenticated.value 的值。 */
} /* 结束当前表达式或代码块。 */

function handleAccountCommand(command) { /* 定义 handleAccountCommand 函数。 */
  if (command === 'logout') logout() /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */

function openPage(name, detail) { /* 定义 openPage 函数。 */
  // 历史导航事件仍使用 testDevice，统一落到当前的接入测试菜单。
  if (name === 'testDevice') { /* 判断条件并选择处理分支。 */
    name = 'integration' /* 更新 name 的值。 */
  } /* 结束当前表达式或代码块。 */
  if (!pages[name] || !can('menu:'+name)) return /* 判断条件并选择处理分支。 */
  if (active.value === name && !detail) return /* 判断条件并选择处理分支。 */
  sessionStorage.removeItem('iot:navigation-detail') /* 执行当前语句并推进处理流程。 */
  active.value = name /* 更新 active.value 的值。 */
  if (!openedTabs.value.includes(name)) openedTabs.value.push(name) /* 新页面加入工作区标签栏。 */
  pageKey.value++ /* 执行当前语句并推进处理流程。 */
  if (detail) sessionStorage.setItem('iot:navigation-detail', JSON.stringify(detail)) /* 判断条件并选择处理分支。 */
  contentArea.value?.scrollTo({ top: 0 }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function closeTab(name) { /* 关闭工作区标签并保持一个可见页面。 */
  if (openedTabs.value.length <= 1) return /* 最后一个页面保持打开。 */
  const index = openedTabs.value.indexOf(name) /* 记住被关闭标签的位置。 */
  openedTabs.value = openedTabs.value.filter(item => item !== name) /* 移除指定标签。 */
  if (active.value === name) openPage(openedTabs.value[Math.max(0, index - 1)]) /* 当前页关闭后定位到相邻页面。 */
} /* 结束标签关闭逻辑。 */

function openAlertSettings() { /* 定义 openAlertSettings 函数。 */
  globalAlertPopup.value?.openSettings() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function handleUIAction(payload) { /* 定义 handleUIAction 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    const event = JSON.parse(payload) /* 声明 event。 */
    const action = event?.action || {} /* 声明 action。 */
    if (action.type === 'OPEN_CAMERA' && typeof action.cameraId === 'string' && action.cameraId) { /* 判断条件并选择处理分支。 */
      openPage('cameras', { cameraId: action.cameraId, actionId: event.id }) /* 执行当前语句并推进处理流程。 */
      UiMessage.info(`规则联动：已定位摄像头信息 ${action.cameraId}`) /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    const allowedPages = new Set(['dashboard', 'devices', 'products', 'protocols', 'profiles', 'integration', 'testDevice', 'cameras', 'alarms', 'inspection', 'raw', 'rules', 'knowledge', 'aiProviders', 'ai', 'backups']) /* 声明 allowedPages。 */
    if (action.type === 'OPEN_PAGE' && allowedPages.has(action.page)) { /* 判断条件并选择处理分支。 */
      openPage(action.page) /* 执行当前语句并推进处理流程。 */
      UiMessage.warning('规则联动：已打开相关业务页面') /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } catch { /* 结束当前表达式或代码块。 */
    // Ignore malformed or unsupported UI actions.
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function connect() { /* 定义 connect 函数。 */
  startRealtime((topic, payload) => { /* 执行当前语句并推进处理流程。 */
    if (topic.includes('/ui-action/')) handleUIAction(payload) /* 判断条件并选择处理分支。 */
    window.dispatchEvent(new CustomEvent('iot:realtime', { detail: { topic, payload } })) /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function unauthorized() { /* 定义 unauthorized 函数。 */
  logout() /* 执行当前语句并推进处理流程。 */
  UiMessage.error('登录已过期，请重新登录') /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

onMounted(async () => { /* 执行当前语句并推进处理流程。 */
  window.addEventListener('iot:unauthorized', unauthorized) /* 执行当前语句并推进处理流程。 */
  window.addEventListener('focus',syncIdentity) /* 执行当前语句并推进处理流程。 */
  await syncIdentity() /* 等待异步操作完成。 */
  if (authenticated.value && can(['menu:devices','menu:alarms','menu:dashboard','menu:raw'])) connect() /* 判断条件并选择处理分支。 */
}) /* 结束当前表达式或代码块。 */

onBeforeUnmount(() => { /* 执行当前语句并推进处理流程。 */
  window.removeEventListener('iot:unauthorized', unauthorized) /* 执行当前语句并推进处理流程。 */
  window.removeEventListener('focus',syncIdentity) /* 执行当前语句并推进处理流程。 */
  stopRealtime() /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
</script>

<template>
  <ui-config-provider size="small"> <!-- 渲染 ui-config-provider 界面元素。 -->
    <div v-if="!authenticated" class="login-page"> <!-- 渲染 div 界面元素。 -->
      <section class="login-intro"><div class="login-brand"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></div><span class="login-eyebrow">消防物联网管理平台</span><h1>连接每一台设备<br />守护每一处安全</h1><p>从设备接入、实时监测到告警处置，<br />在一个工作台掌握现场运行情况。</p><div class="login-capabilities"><span><Network />多协议接入</span><span><Bell />实时告警</span><span><ChartNoAxesCombined />智能巡检</span></div><div class="login-grid-art" aria-hidden="true"><span></span><span></span><span></span><i></i></div></section> <!-- 渲染 section 界面元素。 -->
      <section class="login-panel"> <!-- 渲染 section 界面元素。 -->
        <form class="login-form" @submit.prevent="login"> <!-- 渲染 form 界面元素。 -->
          <span class="login-mark"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></span> <!-- 渲染 span 界面元素。 -->
          <h2>欢迎回来</h2> <!-- 渲染 h2 界面元素。 -->
          <p>登录你的账户，进入炬联工作台</p> <!-- 渲染 p 界面元素。 -->
          <div class="login-fields"> <!-- 渲染 div 界面元素。 -->
            <div class="login-field"> <!-- 渲染 div 界面元素。 -->
              <label for="tenant-id">租户</label> <!-- 渲染 label 界面元素。 -->
              <ui-input id="tenant-id" v-model="loginForm.tenantId" size="large" autocomplete="organization" /> <!-- 渲染 ui-input 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
            <div class="login-field"> <!-- 渲染 div 界面元素。 -->
              <label for="username">用户名</label> <!-- 渲染 label 界面元素。 -->
              <ui-input id="username" v-model="loginForm.username" size="large" autocomplete="username" placeholder="请输入用户名" required /> <!-- 渲染 ui-input 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
            <div class="login-field"> <!-- 渲染 div 界面元素。 -->
              <label for="password">密码</label> <!-- 渲染 label 界面元素。 -->
              <ui-input id="password" v-model="loginForm.password" size="large" type="password" autocomplete="current-password" placeholder="请输入密码" required /> <!-- 渲染 ui-input 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
          </div> <!-- 结束当前界面区域。 -->
          <ui-button native-type="submit" type="primary" size="large" class="login-submit" :loading="loginLoading">进入平台</ui-button> <!-- 渲染 ui-button 界面元素。 -->
          <p class="login-help">账户由管理员分配 · 按授权访问设备和功能</p> <!-- 渲染 p 界面元素。 -->
        </form> <!-- 结束当前界面区域。 -->
      </section> <!-- 结束当前界面区域。 -->
    </div> <!-- 结束当前界面区域。 -->

    <div v-else class="app-shell"> <!-- 渲染 div 界面元素。 -->
      <aside class="app-aside" :class="{ 'is-collapsed': collapsed }"> <!-- 渲染 aside 界面元素。 -->
        <div class="brand"><span class="brand-logo"><img src="/torchlink-logo.png" alt="炬联 TorchLink" /></span></div> <!-- 渲染 div 界面元素。 -->
        <nav class="menu-scroll" aria-label="主导航"> <!-- 渲染 nav 界面元素。 -->
          <div class="menu-scroll-inner"> <!-- 渲染 div 界面元素。 -->
            <template v-for="group in visibleGroups" :key="group.label">
              <button v-show="!collapsed" class="menu-group menu-group-toggle" :aria-expanded="!closedGroups.includes(group.label)" @click="toggleGroup(group.label)">{{ group.label }}<ChevronDown :class="{closed:closedGroups.includes(group.label)}" /></button> <!-- 渲染 button 界面元素。 -->
              <button v-for="name in group.items" v-show="collapsed || !closedGroups.includes(group.label)" :key="name" type="button" class="menu-item" :class="{ active: active === name }" :aria-label="pages[name].title" :title="pages[name].title" :aria-current="active === name ? 'page' : undefined" @click="openPage(name)"> <!-- 渲染 button 界面元素。 -->
                <component :is="pages[name].icon" /> <!-- 渲染 component 界面元素。 -->
                <span v-show="!collapsed">{{ pages[name].title }}</span> <!-- 渲染 span 界面元素。 -->
              </button> <!-- 结束当前界面区域。 -->
            </template>
          </div>
        </nav>
      </aside>

      <main class="app-main">
        <header class="topbar">
          <div class="title-area">
            <button class="collapse-button" :aria-label="collapsed ? '展开菜单' : '折叠菜单'" :aria-expanded="!collapsed" @click="collapsed = !collapsed"><component :is="collapsed ? PanelLeftOpen : PanelLeftClose" /></button>
            <div class="workspace-breadcrumb"><span>工作台</span><span class="breadcrumb-divider">/</span><span>{{ currentGroup }}</span><span class="breadcrumb-divider">/</span><strong>{{ current.title }}</strong></div> <!-- 显示当前任务在后台管理结构中的位置。 -->
          </div>
          <div class="top-actions">
            <button v-if="can('menu:alarms')" class="alert-settings-trigger" type="button" aria-label="告警提醒设置" @click="openAlertSettings"><Settings2 /><span>告警提醒</span></button>
            <ui-dropdown class="account-dropdown" trigger="click" @command="handleAccountCommand">
              <button class="account" type="button" aria-label="打开用户菜单">
                <span class="account-avatar" aria-hidden="true">{{ currentRole.slice(0, 1) }}</span>
                <span class="account-copy"><strong>{{ currentUser === 'admin' ? '管理员' : currentUser }}</strong><small>{{ currentRole }}</small></span>
                <ChevronDown class="account-chevron" />
              </button>
              <template #dropdown>
                <ui-dropdown-menu> <!-- 渲染 ui-dropdown-menu 界面元素。 -->
                  <ui-dropdown-item command="logout"><LogOut />退出登录</ui-dropdown-item> <!-- 渲染 ui-dropdown-item 界面元素。 -->
                </ui-dropdown-menu> <!-- 结束当前界面区域。 -->
              </template>
            </ui-dropdown>
          </div>
        </header>
        <div class="app-tabs" role="tablist" aria-label="已打开页面"> <!-- 工作区标签栏沿用参考模板的多页导航方式。 -->
          <div v-for="name in openedTabs" :key="name" class="app-tab" :class="{ active: active === name }"> <!-- 保留会话中打开的页面。 -->
            <button type="button" role="tab" :aria-selected="active === name" @click="openPage(name)"><component :is="pages[name].icon" /><span>{{ pages[name].title }}</span></button> <!-- 切换工作区页面。 -->
            <button v-if="openedTabs.length > 1" type="button" class="app-tab-close" :aria-label="`关闭${pages[name].title}`" @click="closeTab(name)"><X /></button> <!-- 关闭不再使用的标签。 -->
          </div> <!-- 结束单个工作区标签。 -->
        </div> <!-- 结束工作区标签栏。 -->
        <section ref="contentArea" class="main-content" :class="{ 'main-content--ai': active === 'ai' }">
          <div class="page-context"><h1>{{ current.title }}</h1></div>
          <component v-if="permissionState.ready && current.component" :is="current.component" :key="`${active}-${pageKey}`" v-bind="current.props || {}" @navigate="openPage" />
          <ui-empty v-else-if="permissionState.ready" description="尚未分配菜单权限，请联系管理员" />
        </section>
      </main>
    </div>
    <GlobalAlertPopup v-if="authenticated && permissionState.ready && can('menu:alarms')" ref="globalAlertPopup" @navigate="openPage" />
  </ui-config-provider>
</template>
