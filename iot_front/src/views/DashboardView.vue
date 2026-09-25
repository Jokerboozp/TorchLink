<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
import { alarmLevels, alarmType, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */
import { RefreshCw } from '@lucide/vue' /* 引入当前代码需要的依赖。 */
import DashboardTrend from '../components/DashboardTrend.vue' /* 引入当前代码需要的依赖。 */
import { deviceSegments, ringSegments, productBars } from '../dashboard.js'
import StatusDot from '../components/layout/StatusDot.vue' /* 引入当前代码需要的依赖。 */

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
  const known = Object.entries(alarmLevels).map(([key,name],index) => ({ key,name,count:stats.value.levels?.[key] || 0,color:['var(--danger)','var(--flame)','var(--warning)','var(--info)','var(--gray-500)'][index] })) /* 声明 known。 */
  const other = Object.entries(stats.value.levels || {}).filter(([key]) => !alarmLevels[key]).reduce((sum,[,n]) => sum+n,0) /* 声明 other。 */
  return other ? [...known,{key:'OTHER',name:'未设置',count:other,color:'var(--gray-300)'}] : known /* 返回当前处理结果。 */
}) /* 结束当前表达式或代码块。 */
const kpis = computed(() => [
  { label:'设备总数', value:stats.value.devices, note:'已登记设备' },
  { label:'在线设备', value:stats.value.online, note:`在线率 ${rate.value}%` },
  { label:'活动告警', value:stats.value.activeAlarms, note:'待确认或处置', tone:stats.value.activeAlarms ? 'warning' : '' },
  { label:'高等级告警', value:stats.value.highAlarms, note:stats.value.highAlarms ? '需优先处置' : '无紧急与高等级告警', tone:stats.value.highAlarms ? 'danger' : '' }
])
// 相邻环段之间留出与背景同色的 2px 间隔。
const ring = computed(() => {
  const shown = states.value.filter(item => item.count)
  const gap = shown.length > 1 ? 0.5 : 0
  return shown.map(item => ({ ...item, dash:`${Math.max(item.percent - gap, 0.1)} ${100 - Math.max(item.percent - gap, 0.1)}` }))
})
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
    <div class="dashboard-toolbar"><span>{{ loading ? '正在更新…' : data ? `更新于 ${formatTime(data.updatedAt)}` : '尚未获取数据' }}</span><ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button></div>
    <div class="stats-grid">
      <section v-for="item in kpis" :key="item.label" class="stat-card">
        <span>{{ item.label }}</span><strong>{{ data ? (item.value ?? 0).toLocaleString() : '—' }}</strong>
        <small v-if="!data">等待更新</small>
        <StatusDot v-else-if="item.tone" :tone="item.tone" :label="item.note" />
        <small v-else>{{ item.note }}</small>
      </section>
    </div>
    <div class="dashboard-charts"> <!-- 渲染 div 界面元素。 -->
      <ui-card shadow="never" class="surface-card trend-card"><template #header><div class="card-header"><strong>告警趋势</strong><ui-radio-group v-model="days" size="small" class="segmented-choice-group" aria-label="告警统计时段" @change="load"><ui-radio-button :value="7">近 7 天</ui-radio-button><ui-radio-button :value="30">近 30 天</ui-radio-button></ui-radio-group></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <DashboardTrend v-if="data" :items="data.trend" /><ui-skeleton v-else :rows="5" :loading="loading" animated><ui-empty description="尚未获取趋势数据" :image-size="76" /></ui-skeleton> <!-- 渲染 DashboardTrend 界面元素。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
      <ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>设备状态</strong><ui-button v-permission="'menu:devices'" text @click="emit('navigate','devices')">管理设备</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <div v-if="data && stats.devices" class="device-chart"> <!-- 渲染 div 界面元素。 -->
          <div class="device-ring"><svg viewBox="0 0 180 180" role="img" :aria-label="`设备在线率 ${rate}%`"><circle cx="90" cy="90" r="72" fill="none" stroke="var(--surface-hover)" stroke-width="14" /><circle v-for="item in ring" :key="item.key" cx="90" cy="90" r="72" fill="none" :stroke="item.color" stroke-width="14" pathLength="100" :stroke-dasharray="item.dash" :stroke-dashoffset="-item.offset" transform="rotate(-90 90 90)"><title>{{ item.name }} {{ item.count }} 台</title></circle></svg><div><strong>{{ rate }}<small>%</small></strong><span>在线率</span></div></div> <!-- 渲染 div 界面元素。 -->
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

<style scoped>
.dashboard-page :deep(.n-radio-group[aria-label="告警统计时段"] .n-radio-button) { min-width: 74px; min-height: 28px; }
.dashboard-page { display: grid; gap: var(--space-4); min-width: 0; }
.dashboard-toolbar { display: flex; align-items: center; justify-content: flex-end; gap: var(--space-3); color: var(--text-muted); font-size: var(--font-size-sm); }
.stats-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-4); }
.stat-card { display: grid; align-content: space-between; gap: var(--space-2); min-height: 120px; padding: var(--space-4) var(--space-5); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); box-shadow: var(--shadow-xs); }
.stat-card > span { color: var(--text-secondary); font-size: var(--font-size-sm); }
.stat-card > strong { color: var(--text-strong); font-size: 30px; font-weight: var(--font-weight-semibold); line-height: 1; font-variant-numeric: tabular-nums; }
.stat-card > small, .stat-card .status-dot { color: var(--text-secondary); font-size: var(--font-size-xs); }
.dashboard-charts { display: grid; grid-template-columns: minmax(0, 1.7fr) minmax(300px, 1fr); gap: var(--space-4); }
.dashboard-distributions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-4); }
.dashboard-page :deep(.surface-card) { min-width: 0; }
.dashboard-page :deep(.card-header) { min-height: 30px; }
.chart-meta { color: var(--text-muted); font-size: var(--font-size-xs); }
.device-chart { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: var(--space-5); min-height: 256px; }
.device-ring { position: relative; flex-shrink: 0; width: 174px; height: 174px; }
.device-ring svg { width: 100%; height: 100%; }
.device-ring circle { transition: stroke-dasharray 0.4s ease; }
.device-ring > div { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; pointer-events: none; }
.device-ring strong { color: var(--text-strong); font-size: 32px; font-weight: var(--font-weight-semibold); }
.device-ring small { margin-left: 2px; font-size: var(--font-size-lg); }
.device-ring span { margin-top: var(--space-1); color: var(--text-muted); font-size: var(--font-size-xs); }
.chart-legend { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3) var(--space-5); width: 100%; }
.chart-legend > div { display: flex; align-items: center; gap: 7px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.chart-legend i { flex-shrink: 0; width: 8px; height: 8px; border-radius: 50%; }
.chart-legend b { margin-left: auto; color: var(--text); font-weight: var(--font-weight-medium); font-variant-numeric: tabular-nums; }
.horizontal-chart { display: flex; flex-direction: column; gap: var(--space-4); min-height: 238px; }
.bar-row > div:first-child { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); margin-bottom: var(--space-2); font-size: var(--font-size-sm); }
.bar-row span { overflow: hidden; color: var(--text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.bar-row b { color: var(--text); font-weight: var(--font-weight-medium); white-space: nowrap; font-variant-numeric: tabular-nums; }
.bar-row small { color: var(--text-muted); font-size: var(--font-size-xs); font-weight: 400; }
.bar-track { height: 8px; overflow: hidden; background: var(--surface-hover); border-radius: var(--radius-full); }
.bar-track i { display: block; height: 100%; border-radius: inherit; transition: width 0.4s ease; }
.alarm-row { display: grid; grid-template-columns: 8px minmax(0, 1fr) auto auto; align-items: center; gap: var(--space-3); min-height: 62px; border-bottom: 1px solid var(--border); }
.alarm-row:last-child { border-bottom: 0; }
.alarm-row > i { width: 7px; height: 7px; background: var(--danger); border-radius: 50%; }
.alarm-row > div { min-width: 0; overflow-wrap: anywhere; }
.alarm-row strong { color: var(--text-strong); font-size: var(--font-size-sm); }
.alarm-row small { display: block; color: var(--text-muted); font-size: var(--font-size-xs); }
@media (min-width: 1440px) {
  .device-chart { flex-direction: row; gap: var(--space-6); }
  .chart-legend { grid-template-columns: 1fr; }
}
@media (max-width: 1120px) {
  .stats-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 1100px) {
  .dashboard-charts { grid-template-columns: minmax(0, 1fr); }
  .device-chart { flex-direction: row; gap: 40px; min-height: 210px; }
  .chart-legend { max-width: 320px; }
}
@media (max-width: 767px) {
  .stats-grid { gap: var(--space-2); }
  .stat-card { min-height: 100px; padding: var(--space-3) var(--space-4); }
  .stat-card > strong { font-size: 26px; }
  .dashboard-distributions { grid-template-columns: minmax(0, 1fr); }
  .device-chart { flex-direction: column; gap: var(--space-5); }
  .horizontal-chart { min-height: 0; }
  .alarm-row { grid-template-columns: 8px minmax(0, 1fr) auto; }
  .alarm-row > .ui-button { grid-column: 2 / -1; justify-self: end; margin-bottom: 10px; }
}
@media (max-width: 390px) {
  .stats-grid { grid-template-columns: 1fr; }
}
</style>
