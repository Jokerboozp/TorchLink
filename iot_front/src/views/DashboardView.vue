<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
import { alarmLevels, alarmType, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */
import { RefreshCw } from '@lucide/vue' /* 引入当前代码需要的依赖。 */
import DashboardTrend from '../components/DashboardTrend.vue' /* 引入当前代码需要的依赖。 */
import { deviceSegments, ringSegments, productBars } from '../dashboard.js' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */
const data = ref(null) /* 声明 data。 */
const alarms = ref([]) /* 声明 alarms。 */
const days = ref(7) /* 声明 days。 */
const detail = ref(null) /* 声明 detail。 */
const detailVisible = ref(false) /* 声明 detailVisible。 */
const loading = ref(false) /* 声明 loading。 */
const loadError = ref('') /* 声明 loadError。 */
const stats = computed(() => data.value || {}) /* 声明 stats。 */
const rate = computed(() => stats.value.devices ? Math.round(stats.value.online / stats.value.devices * 100) : 0) /* 声明 rate。 */
const states = computed(() => ringSegments(deviceSegments(stats.value.states))) /* 声明 states。 */
const products = computed(() => productBars(stats.value.products)) /* 声明 products。 */
const productMax = computed(() => Math.max(1,...products.value.map(item => item.count))) /* 声明 productMax。 */
const levels = computed(() => { /* 声明 levels。 */
  const known = Object.entries(alarmLevels).map(([key,name],index) => ({ key,name,count:stats.value.levels?.[key] || 0,color:['#e5484d','#ff9f0a','#e5b64c','#0071e3','#86868b'][index] })) /* 声明 known。 */
  const other = Object.entries(stats.value.levels || {}).filter(([key]) => !alarmLevels[key]).reduce((sum,[,n]) => sum+n,0) /* 声明 other。 */
  return other ? [...known,{key:'OTHER',name:'未设置',count:other,color:'#af52de'}] : known /* 返回当前处理结果。 */
}) /* 结束当前表达式或代码块。 */
let controller, revision = 0, timer, disposed = false /* 声明 controller。 */
async function load() { /* 定义 load 函数。 */
  const current = ++revision /* 声明 current。 */
  controller?.abort() /* 执行当前语句并推进处理流程。 */
  controller = new AbortController() /* 更新 controller 的值。 */
  const options = { signal:controller.signal } /* 声明 options。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [overview, active] = await Promise.all([api(`/api/v1/dashboard?days=${days.value}&offset=${-new Date().getTimezoneOffset()}`,options), api('/api/v1/alarms?status=ACTIVE&limit=6',options)]) /* 执行当前语句并推进处理流程。 */
    if (disposed || current !== revision) return /* 判断条件并选择处理分支。 */
    data.value = overview /* 更新 data.value 的值。 */
    alarms.value = active.items || [] /* 更新 alarms.value 的值。 */
    loadError.value = '' /* 更新 loadError.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (disposed || current !== revision || error?.name === 'AbortError') return /* 判断条件并选择处理分支。 */
    loadError.value = data.value ? '刷新失败，当前显示上次成功获取的数据。' : '统计读取失败，请重试。' /* 更新 loadError.value 的值。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { if (!disposed && current === revision) loading.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function showDetail(id) { try { detail.value = await api(`/api/v1/alarms/${encodeURIComponent(id)}`); if (!disposed) detailVisible.value = true } catch (e) { if (!disposed) notifyError(e) } } /* 定义 showDetail 函数。 */
const realtime = event => { /* 声明 realtime。 */
  const topic = event?.detail?.topic || '' /* 声明 topic。 */
  if (!topic.includes('/alarm/') && !topic.includes('/device/state/')) return /* 判断条件并选择处理分支。 */
  if (timer || disposed) return /* 判断条件并选择处理分支。 */
  timer = setTimeout(() => { timer = null; if (loading.value) realtime(event); else load() }, 5000) /* 更新 timer 的值。 */
} /* 结束当前表达式或代码块。 */
onMounted(() => { load(); window.addEventListener('iot:realtime', realtime) }) /* 执行当前语句并推进处理流程。 */
onBeforeUnmount(() => { disposed = true; controller?.abort(); clearTimeout(timer); window.removeEventListener('iot:realtime', realtime) }) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <div class="dashboard-page" :aria-busy="loading"> <!-- 渲染 div 界面元素。 -->
    <ui-alert v-if="loadError" class="dashboard-error" :title="loadError" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
    <div class="section-toolbar dashboard-toolbar"><span class="muted-text">{{ loading ? '正在更新…' : data ? `更新于 ${formatTime(data.updatedAt)}` : '尚未获取数据' }}</span><ui-button :loading="loading" @click="load"><RefreshCw :size="14" />刷新</ui-button></div> <!-- 渲染 div 界面元素。 -->
    <div class="stats-grid"> <!-- 渲染 div 界面元素。 -->
      <ui-card v-for="item in [{label:'设备总数',value:stats.devices,note:'已登记设备',tone:'primary'},{label:'在线设备',value:stats.online,note:`在线率 ${rate}%`,tone:'success'},{label:'活动告警',value:stats.activeAlarms,note:'待确认',tone:'warning'},{label:'高等级告警',value:stats.highAlarms,note:'紧急与高等级 · 活动中',tone:'danger'}]" :key="item.label" class="stat-card" shadow="never"> <!-- 渲染 ui-card 界面元素。 -->
        <span>{{ item.label }}</span><strong>{{ data ? item.value.toLocaleString() : '—' }}</strong><small :class="item.tone">{{ data ? item.note : '等待更新' }}</small><i :class="item.tone" /> <!-- 渲染 span 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
    </div> <!-- 结束当前界面区域。 -->
    <div class="dashboard-charts"> <!-- 渲染 div 界面元素。 -->
      <ui-card shadow="never" class="surface-card trend-card"><template #header><div class="card-header"><strong>告警趋势</strong><ui-radio-group v-model="days" size="small" aria-label="告警统计时段" @change="load"><ui-radio-button :value="7">近 7 天</ui-radio-button><ui-radio-button :value="30">近 30 天</ui-radio-button></ui-radio-group></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <DashboardTrend v-if="data" :items="data.trend" /><ui-skeleton v-else :rows="5" :loading="loading" animated><ui-empty description="尚未获取趋势数据" :image-size="76" /></ui-skeleton> <!-- 渲染 DashboardTrend 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
      <ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>设备状态</strong><ui-button v-permission="'menu:devices'" text @click="emit('navigate','devices')">管理设备</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <div v-if="data && stats.devices" class="device-chart"> <!-- 渲染 div 界面元素。 -->
          <div class="device-ring"><svg viewBox="0 0 180 180" role="img" :aria-label="`设备在线率 ${rate}%`"><circle cx="90" cy="90" r="72" fill="none" stroke="#f0f0f2" stroke-width="15" /><circle v-for="item in states.filter(item => item.count)" :key="item.key" cx="90" cy="90" r="72" fill="none" :stroke="item.color" stroke-width="15" pathLength="100" :stroke-dasharray="`${item.percent} ${100-item.percent}`" :stroke-dashoffset="-item.offset" transform="rotate(-90 90 90)"><title>{{ item.name }} {{ item.count }} 台</title></circle></svg><div><strong>{{ rate }}<small>%</small></strong><span>在线率</span></div></div> <!-- 渲染 div 界面元素。 -->
          <div class="chart-legend"><div v-for="item in states" :key="item.key"><i :style="{background:item.color}" /><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }}</b></div></div> <!-- 渲染 div 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <ui-empty v-else :description="data ? '暂无已登记设备' : loading ? '正在读取设备' : '尚未获取设备数据'" :image-size="76" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
    </div> <!-- 结束当前界面区域。 -->
    <div class="dashboard-distributions"> <!-- 渲染 div 界面元素。 -->
      <ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>产品设备分布</strong><ui-button v-permission="'menu:products'" text @click="emit('navigate','products')">管理产品</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <div v-if="products.length" class="horizontal-chart"><div v-for="item in products" :key="item.key" class="bar-row"><div><span :title="item.name">{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>台</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / productMax * 100}%`, background:item.color }" /></div></div></div> <!-- 渲染 div 界面元素。 -->
        <ui-empty v-else :description="data ? '暂无设备分布' : loading ? '正在读取产品' : '尚未获取产品数据'" :image-size="65" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
      <ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>活动告警等级</strong><span class="chart-meta">{{ data ? `${stats.activeAlarms.toLocaleString()} 条` : '—' }}</span></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <div v-if="data && stats.activeAlarms" class="horizontal-chart"><div v-for="item in levels" :key="item.key" class="bar-row"><div><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>条</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / stats.activeAlarms * 100}%`, background:item.color }" /></div></div></div> <!-- 渲染 div 界面元素。 -->
        <ui-empty v-else :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
    </div> <!-- 结束当前界面区域。 -->
    <ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>最新活动告警</strong><ui-button v-permission="'menu:alarms'" text @click="emit('navigate','alarms')">查看全部</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
      <ui-empty v-if="!alarms.length" :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" /> <!-- 渲染 ui-empty 界面元素。 -->
      <div v-for="alarm in alarms" :key="alarm.alarmId" class="alarm-row"><i /><div><strong>{{ alarm.deviceName || alarm.deviceId }} · {{ alarmType(alarm.alarmType) }}</strong><small>{{ alarm.details?.description || alarm.details?.message || alarmType(alarm.alarmType) }} · {{ formatTime(alarm.lastTriggeredAt) }}</small></div><ui-tag :type="tagType(alarm.alarmLevel)" effect="light" round>{{ label(alarmLevels,alarm.alarmLevel,'未设置') }}</ui-tag><ui-button text @click="showDetail(alarm.alarmId)">详情</ui-button></div> <!-- 渲染 div 界面元素。 -->
    </ui-card> <!-- 结束当前界面区域。 -->
  <ui-dialog v-model="detailVisible" title="告警详情" width="min(680px, 94vw)"><ui-descriptions v-if="detail" :column="1" border><ui-descriptions-item label="告警编号">{{ detail.alarmId }}</ui-descriptions-item><ui-descriptions-item label="设备">{{ detail.deviceName || detail.deviceId }}</ui-descriptions-item><ui-descriptions-item label="告警类型">{{ alarmType(detail.alarmType) }}</ui-descriptions-item><ui-descriptions-item label="发生时间">{{ formatTime(detail.lastTriggeredAt) }}</ui-descriptions-item></ui-descriptions><details class="technical-details"><summary>查看原始记录</summary><pre>{{ JSON.stringify(detail,null,2) }}</pre></details><template #footer><ui-button @click="detailVisible = false">关闭</ui-button><ui-button v-permission="'menu:alarms'" type="primary" @click="emit('navigate', 'alarms', { alarmId: detail?.alarmId })">前往告警处置</ui-button></template></ui-dialog> <!-- 渲染 ui-dialog 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->
</template>
