<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
import { alarmLevels, alarmType, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */
import { RefreshCw } from '@lucide/vue' /* 引入当前代码需要的依赖。 */
import DashboardTrend from '../components/DashboardTrend.vue' /* 引入当前代码需要的依赖。 */
import { deviceSegments, ringSegments, productBars, dashboardDistributions } from '../dashboard.js'
import DashboardDistribution from '../components/DashboardDistribution.vue'
import StatusDot from '../components/layout/StatusDot.vue' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */
const data = ref(null) /* 声明 data。 */
const alarms = ref([]) /* 声明 alarms。 */
const days = ref(7) /* 声明 days。 */
const detail = ref(null) /* 声明 detail。 */
const detailVisible = ref(false) /* 声明 detailVisible。 */
const loading = ref(false) /* 声明 loading。 */
const loadError = ref('') /* 声明 loadError。 */
const distributions = computed(() => dashboardDistributions(data.value || {}))
const alarmDistributions = computed(() => distributions.value.filter(chart => chart.unit === '条'))
const deviceDistributions = computed(() => distributions.value.filter(chart => chart.unit === '台'))
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
  <div class="dashboard-page" :aria-busy="loading">
    <ui-alert v-if="loadError" class="dashboard-error" :title="loadError" type="error" :closable="false" show-icon />
    <div class="dashboard-toolbar"><span>当前运行快照</span><div><span>{{ loading ? '正在更新…' : data ? `更新于 ${formatTime(data.updatedAt)}` : '尚未获取数据' }}</span><ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button></div></div>
    <div class="stats-grid">
      <section v-for="item in kpis" :key="item.label" class="stat-card" :class="item.tone && `stat-${item.tone}`">
        <span>{{ item.label }}</span><strong>{{ data ? (item.value ?? 0).toLocaleString() : '—' }}</strong>
        <small v-if="!data">等待更新</small>
        <StatusDot v-else-if="item.tone" :tone="item.tone" :label="item.note" />
        <small v-else>{{ item.note }}</small>
      </section>
    </div>
    <section class="dashboard-section" aria-labelledby="alarm-section-title">
      <div class="section-heading"><div><h2 id="alarm-section-title">告警分析</h2><span>发生趋势与处置进展</span></div><ui-radio-group v-model="days" size="small" class="segmented-choice-group" aria-label="告警统计时段" @change="load"><ui-radio-button :value="7">近 7 天</ui-radio-button><ui-radio-button :value="30">近 30 天</ui-radio-button></ui-radio-group></div>
      <div class="dashboard-primary"><ui-card shadow="never" class="surface-card trend-card"><template #header><div class="card-header"><strong>告警趋势</strong><span class="chart-meta">近 {{ stats.days || days }} 天</span></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <DashboardTrend v-if="data" :items="data.trend" /><ui-skeleton v-else :rows="5" :loading="loading" animated><ui-empty description="尚未获取趋势数据" :image-size="76" /></ui-skeleton> <!-- 渲染 DashboardTrend 界面元素。 -->
      </ui-card>
        <ui-card shadow="never" class="surface-card recent-alarms"><template #header><div class="card-header"><strong>最新活动告警</strong><ui-button v-permission="'menu:alarms'" text @click="emit('navigate','alarms')">查看全部</ui-button></div></template>
          <p class="chart-description">当前待处理告警 · 最近 6 条</p>
          <ui-empty v-if="!alarms.length" :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" />
          <div v-else class="alarm-list" tabindex="0" role="region" aria-label="最新活动告警列表">
            <article v-for="alarm in alarms" :key="alarm.alarmId" class="alarm-row">
              <div class="alarm-title"><strong :title="alarm.deviceName || alarm.deviceId">{{ alarm.deviceName || alarm.deviceId }}</strong><ui-tag :type="tagType(alarm.alarmLevel)" effect="light" round>{{ label(alarmLevels,alarm.alarmLevel,'未设置') }}</ui-tag></div>
              <p :title="alarm.details?.description || alarmType(alarm.alarmType)">{{ alarm.details?.description || alarmType(alarm.alarmType) }}</p>
              <div class="alarm-footer"><time>{{ formatTime(alarm.lastTriggeredAt) }}</time><ui-button text @click="showDetail(alarm.alarmId)">详情</ui-button></div>
            </article>
          </div>
        </ui-card>
      </div>
      <div class="dashboard-grid"><ui-card shadow="never" class="surface-card level-card"><template #header><div class="card-header"><strong>活动告警等级</strong><span class="chart-meta">{{ data ? `${stats.activeAlarms.toLocaleString()} 条` : '—' }}</span></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <p class="chart-description">当前活动告警的风险等级分布</p>
        <div v-if="data && stats.activeAlarms" class="horizontal-chart"><div v-for="item in levels" :key="item.key" class="bar-row"><div><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>条</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / stats.activeAlarms * 100}%`, background:item.color }" /></div></div></div> <!-- 渲染 div 界面元素。 -->
        <ui-empty v-else :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card>
        <ui-card v-for="chart in alarmDistributions" :key="chart.key" shadow="never" class="surface-card insight-card">
          <template #header><div class="card-header"><strong>{{ chart.title }}</strong><span class="chart-meta">{{ chart.unit === '台' ? '当前快照' : `近 ${stats.days || days} 天` }}</span></div></template>
          <p class="chart-description">{{ chart.subtitle }}</p>
          <DashboardDistribution :items="chart.items" :title="chart.title" :unit="chart.unit" :variant="chart.variant" :available="chart.available" :loading="loading" />
        </ui-card>
      </div>
    </section>
    <section class="dashboard-section" aria-labelledby="device-section-title">
      <div class="section-heading"><div><h2 id="device-section-title">设备概况</h2><span>在线、连接与数据活跃情况</span></div><span class="chart-meta">当前快照</span></div>
      <div class="dashboard-grid"><ui-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>设备状态</strong><ui-button v-permission="'menu:devices'" text @click="emit('navigate','devices')">管理设备</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <p class="chart-description">已登记设备的当前业务状态</p>
        <div v-if="data && stats.devices" class="device-chart"> <!-- 渲染 div 界面元素。 -->
          <div class="device-ring"><svg viewBox="0 0 180 180" role="img" :aria-label="`设备在线率 ${rate}%`"><circle cx="90" cy="90" r="72" fill="none" stroke="var(--surface-hover)" stroke-width="14" /><circle v-for="item in ring" :key="item.key" cx="90" cy="90" r="72" fill="none" :stroke="item.color" stroke-width="14" pathLength="100" :stroke-dasharray="item.dash" :stroke-dashoffset="-item.offset" transform="rotate(-90 90 90)"><title>{{ item.name }} {{ item.count }} 台</title></circle></svg><div><strong>{{ rate }}<small>%</small></strong><span>在线率</span></div></div> <!-- 渲染 div 界面元素。 -->
          <div class="chart-legend"><div v-for="item in states" :key="item.key"><i :style="{background:item.color}" /><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }}</b></div></div> <!-- 渲染 div 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <ui-empty v-else :description="data ? '暂无已登记设备' : loading ? '正在读取设备' : '尚未获取设备数据'" :image-size="76" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card>
        <ui-card v-for="chart in deviceDistributions" :key="chart.key" shadow="never" class="surface-card insight-card">
          <template #header><div class="card-header"><strong>{{ chart.title }}</strong><span class="chart-meta">{{ chart.unit === '台' ? '当前快照' : `近 ${stats.days || days} 天` }}</span></div></template>
          <p class="chart-description">{{ chart.subtitle }}</p>
          <DashboardDistribution :items="chart.items" :title="chart.title" :unit="chart.unit" :variant="chart.variant" :available="chart.available" :loading="loading" />
        </ui-card>
      </div>
      <ui-card shadow="never" class="surface-card product-card"><template #header><div class="card-header"><strong>产品设备分布</strong><ui-button v-permission="'menu:products'" text @click="emit('navigate','products')">管理产品</ui-button></div></template> <!-- 渲染 ui-card 界面元素。 -->
        <div v-if="products.length" class="horizontal-chart product-chart"><div v-for="item in products" :key="item.key" class="bar-row"><div><span :title="item.name">{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>台</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / productMax * 100}%`, background:item.color }" /></div></div></div> <!-- 渲染 div 界面元素。 -->
        <ui-empty v-else :description="data ? '暂无设备分布' : loading ? '正在读取产品' : '尚未获取产品数据'" :image-size="65" /> <!-- 渲染 ui-empty 界面元素。 -->
      </ui-card>
    </section>
      <ui-dialog v-model="detailVisible" title="告警详情" width="min(680px, 94vw)"><ui-descriptions v-if="detail" :column="1" border><ui-descriptions-item label="告警编号">{{ detail.alarmId }}</ui-descriptions-item><ui-descriptions-item label="设备">{{ detail.deviceName || detail.deviceId }}</ui-descriptions-item><ui-descriptions-item label="告警类型">{{ alarmType(detail.alarmType) }}</ui-descriptions-item><ui-descriptions-item label="发生时间">{{ formatTime(detail.lastTriggeredAt) }}</ui-descriptions-item></ui-descriptions><details class="technical-details"><summary>查看原始记录</summary><pre>{{ JSON.stringify(detail,null,2) }}</pre></details><template #footer><ui-button @click="detailVisible = false">关闭</ui-button><ui-button v-permission="'menu:alarms'" type="primary" @click="emit('navigate', 'alarms', { alarmId: detail?.alarmId })">前往告警处置</ui-button></template></ui-dialog>
  </div>
</template>

<style scoped>
.dashboard-page { display:grid; gap:16px; min-width:0; }
.dashboard-toolbar,.dashboard-toolbar > div { display:flex; align-items:center; justify-content:space-between; gap:12px; color:var(--text-muted); font-size:12px; }
.stats-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:16px; }
.stat-card { display:grid; align-content:center; gap:8px; padding:16px 20px; min-height:104px; background:var(--surface); border:1px solid var(--border); border-radius:var(--radius-lg); box-shadow:var(--shadow-xs); }
.stat-card > span { color:var(--text-secondary); font-size:13px; }
.stat-card > strong { color:var(--text-strong); font-size:30px; line-height:1; font-weight:600; font-variant-numeric:tabular-nums; }
.stat-card > small,.stat-card .status-dot { color:var(--text-secondary); font-size:12px; }
.stat-danger > strong { color:var(--danger); }
.stat-warning > strong { color:var(--warning); }
.dashboard-section { display:grid; gap:16px; min-width:0; margin-top:8px; }
.section-heading,.section-heading > div { display:flex; align-items:center; gap:12px; }
.section-heading { justify-content:space-between; min-height:32px; }
.section-heading h2 { margin:0; font-size:16px; font-weight:600; color:var(--text-strong); }
.section-heading > div > span { color:var(--text-muted); font-size:12px; }
.dashboard-page :deep(.n-radio-group[aria-label="告警统计时段"] .n-radio-button) { min-width:74px; min-height:28px; }
.dashboard-primary { display:grid; grid-template-columns:minmax(0,1.75fr) minmax(300px,1fr); gap:16px; }
.dashboard-grid { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:16px; }
.dashboard-page :deep(.surface-card) { min-width:0; }
.dashboard-page :deep(.surface-card .n-card-header) { padding:16px 18px 12px; }
.dashboard-page :deep(.surface-card .n-card-content) { padding:0 18px 18px; }
.dashboard-page :deep(.card-header) { min-height:28px; gap:8px; }
.chart-meta { color:var(--text-muted); font-size:12px; white-space:nowrap; }
.chart-description { min-height:36px; margin:0 0 12px; color:var(--text-muted); font-size:12px; line-height:1.5; }
.dashboard-primary .chart-description { min-height:0; }
.trend-card :deep(.trend-plot svg) { max-height:220px; min-height:160px; }
.trend-card :deep(.trend-caption) { margin-top:8px; }
.alarm-list { max-height:240px; overflow:auto; padding-right:4px; overscroll-behavior:contain; }
.alarm-list:focus-visible { outline:2px solid var(--primary); outline-offset:2px; }
.alarm-row { padding:12px 0; border-bottom:1px solid var(--border); }
.alarm-row:first-child { padding-top:0; }
.alarm-row:last-child { border-bottom:0; padding-bottom:0; }
.alarm-title,.alarm-footer { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.alarm-title strong { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:13px; font-weight:500; color:var(--text-strong); }
.alarm-title .ui-tag { flex-shrink:0; }
.alarm-row p { display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; overflow:hidden; overflow-wrap:anywhere; margin:6px 0; font-size:12px; color:var(--text-secondary); }
.alarm-footer time { color:var(--text-muted); font-size:12px; }
.device-chart { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:18px; min-height:240px; }
.device-ring { position:relative; flex-shrink:0; width:126px; height:126px; }
.device-ring svg { display:block; width:100%; height:100%; }
.device-ring circle { transition:stroke-dasharray .4s ease; }
.device-ring > div { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; pointer-events:none; }
.device-ring strong { color:var(--text-strong); font-size:28px; font-weight:600; }
.device-ring small { margin-left:2px; font-size:14px; }
.device-ring span { margin-top:4px; color:var(--text-muted); font-size:12px; }
.chart-legend { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px 16px; width:100%; }
.chart-legend > div { display:flex; align-items:center; gap:7px; color:var(--text-secondary); font-size:12px; }
.chart-legend i { flex-shrink:0; width:8px; height:8px; border-radius:50%; }
.chart-legend b { margin-left:auto; color:var(--text); font-weight:500; font-variant-numeric:tabular-nums; }
.horizontal-chart { display:flex; flex-direction:column; gap:18px; min-height:240px; justify-content:center; }
.bar-row > div:first-child { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:8px; font-size:13px; }
.bar-row span { overflow:hidden; color:var(--text-secondary); text-overflow:ellipsis; white-space:nowrap; }
.bar-row b { color:var(--text); font-weight:500; white-space:nowrap; font-variant-numeric:tabular-nums; }
.bar-row small { color:var(--text-muted); font-size:12px; font-weight:400; }
.bar-track { height:7px; overflow:hidden; background:var(--surface-hover); border-radius:var(--radius-full); }
.bar-track i { display:block; height:100%; border-radius:inherit; transition:width .4s ease; }
.product-chart { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:20px 28px; min-height:0; padding-top:4px; }
@media (min-width:1600px) {
  .device-chart { flex-direction:row; }
  .chart-legend { grid-template-columns:1fr; }
}
@media (max-width:1199px) {
  .dashboard-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .dashboard-grid > :last-child { grid-column:1 / -1; }
  .dashboard-grid > :last-child :deep(.distribution-chart) { min-height:160px; }
  .dashboard-grid > :last-child .chart-description { min-height:0; }
  .product-chart { grid-template-columns:repeat(2,minmax(0,1fr)); }
}
@media (max-width:1000px) {
  .dashboard-primary { grid-template-columns:minmax(0,1fr); }
  .alarm-list { max-height:220px; }
  .stats-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }
}
@media (max-width:767px) {
  .dashboard-page,.dashboard-section,.dashboard-primary,.dashboard-grid { gap:12px; }
  .dashboard-toolbar { flex-wrap:wrap; gap:6px; }
  .dashboard-toolbar > div { flex:1; justify-content:flex-end; }
  .dashboard-toolbar > span { display:none; }
  .stats-grid { gap:10px; }
  .stat-card { padding:14px; min-height:96px; }
  .stat-card > strong { font-size:26px; }
  .section-heading { flex-wrap:wrap; gap:8px; }
  .section-heading > div > span { display:none; }
  .dashboard-grid,.product-chart { grid-template-columns:minmax(0,1fr); }
  .chart-description { min-height:0; }
  .device-chart,.horizontal-chart { min-height:0; }
  .device-chart { padding:8px 0; }
}
</style>
