<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, formatTime, notifyError } from '../api'
import { alarmLevels, alarmType, label, tagType } from '../labels'
import { RefreshCw } from '@lucide/vue'
import DashboardTrend from '../components/DashboardTrend.vue'
import { deviceSegments, ringSegments, productBars } from '../dashboard.js'

const emit = defineEmits(['navigate'])
const data = ref(null)
const alarms = ref([])
const days = ref(7)
const detail = ref(null)
const detailVisible = ref(false)
const loading = ref(false)
const loadError = ref('')
const stats = computed(() => data.value || {})
const rate = computed(() => stats.value.devices ? Math.round(stats.value.online / stats.value.devices * 100) : 0)
const states = computed(() => ringSegments(deviceSegments(stats.value.states)))
const products = computed(() => productBars(stats.value.products))
const productMax = computed(() => Math.max(1,...products.value.map(item => item.count)))
const levels = computed(() => {
  const known = Object.entries(alarmLevels).map(([key,name],index) => ({ key,name,count:stats.value.levels?.[key] || 0,color:['#e5484d','#ff9f0a','#e5b64c','#0071e3','#86868b'][index] }))
  const other = Object.entries(stats.value.levels || {}).filter(([key]) => !alarmLevels[key]).reduce((sum,[,n]) => sum+n,0)
  return other ? [...known,{key:'OTHER',name:'未设置',count:other,color:'#af52de'}] : known
})
let controller, revision = 0, timer, disposed = false
async function load() {
  const current = ++revision
  controller?.abort()
  controller = new AbortController()
  const options = { signal:controller.signal }
  loading.value = true
  try {
    const [overview, active] = await Promise.all([api(`/api/v1/dashboard?days=${days.value}&offset=${-new Date().getTimezoneOffset()}`,options), api('/api/v1/alarms?status=ACTIVE&limit=6',options)])
    if (disposed || current !== revision) return
    data.value = overview
    alarms.value = active.items || []
    loadError.value = ''
  } catch (error) {
    if (disposed || current !== revision || error?.name === 'AbortError') return
    loadError.value = data.value ? '刷新失败，当前显示上次成功获取的数据。' : '统计读取失败，请重试。'
    notifyError(error)
  } finally { if (!disposed && current === revision) loading.value = false }
}
async function showDetail(id) { try { detail.value = await api(`/api/v1/alarms/${encodeURIComponent(id)}`); if (!disposed) detailVisible.value = true } catch (e) { if (!disposed) notifyError(e) } }
const realtime = () => {
  if (timer || disposed) return
  timer = setTimeout(() => { timer = null; if (loading.value) realtime(); else load() }, 5000)
}
onMounted(() => { load(); window.addEventListener('iot:realtime', realtime) })
onBeforeUnmount(() => { disposed = true; controller?.abort(); clearTimeout(timer); window.removeEventListener('iot:realtime', realtime) })
</script>

<template>
  <div class="dashboard-page" :aria-busy="loading">
    <el-alert v-if="loadError" class="dashboard-error" :title="loadError" type="error" :closable="false" show-icon />
    <div class="section-toolbar dashboard-toolbar"><span class="muted-text">{{ loading ? '正在更新…' : data ? `更新于 ${formatTime(data.updatedAt)}` : '尚未获取数据' }}</span><el-button :loading="loading" @click="load"><RefreshCw :size="14" />刷新</el-button></div>
    <div class="stats-grid">
      <el-card v-for="item in [{label:'设备总数',value:stats.devices,note:'已登记设备',tone:'primary'},{label:'在线设备',value:stats.online,note:`在线率 ${rate}%`,tone:'success'},{label:'活动告警',value:stats.activeAlarms,note:'待确认',tone:'warning'},{label:'高等级告警',value:stats.highAlarms,note:'紧急与高等级 · 活动中',tone:'danger'}]" :key="item.label" class="stat-card" shadow="never">
        <span>{{ item.label }}</span><strong>{{ data ? item.value.toLocaleString() : '—' }}</strong><small :class="item.tone">{{ data ? item.note : '等待更新' }}</small><i :class="item.tone" />
      </el-card>
    </div>
    <div class="dashboard-charts">
      <el-card shadow="never" class="surface-card trend-card"><template #header><div class="card-header"><strong>告警趋势</strong><el-radio-group v-model="days" size="small" aria-label="告警统计时段" @change="load"><el-radio-button :value="7">近 7 天</el-radio-button><el-radio-button :value="30">近 30 天</el-radio-button></el-radio-group></div></template>
        <DashboardTrend v-if="data" :items="data.trend" /><el-skeleton v-else :rows="5" :loading="loading" animated><el-empty description="尚未获取趋势数据" :image-size="76" /></el-skeleton>
      </el-card>
      <el-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>设备状态</strong><el-button v-permission="'menu:devices'" text @click="emit('navigate','devices')">管理设备</el-button></div></template>
        <div v-if="data && stats.devices" class="device-chart">
          <div class="device-ring"><svg viewBox="0 0 180 180" role="img" :aria-label="`设备在线率 ${rate}%`"><circle cx="90" cy="90" r="72" fill="none" stroke="#f0f0f2" stroke-width="15" /><circle v-for="item in states.filter(item => item.count)" :key="item.key" cx="90" cy="90" r="72" fill="none" :stroke="item.color" stroke-width="15" pathLength="100" :stroke-dasharray="`${item.percent} ${100-item.percent}`" :stroke-dashoffset="-item.offset" transform="rotate(-90 90 90)"><title>{{ item.name }} {{ item.count }} 台</title></circle></svg><div><strong>{{ rate }}<small>%</small></strong><span>在线率</span></div></div>
          <div class="chart-legend"><div v-for="item in states" :key="item.key"><i :style="{background:item.color}" /><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }}</b></div></div>
        </div>
        <el-empty v-else :description="data ? '暂无已登记设备' : loading ? '正在读取设备' : '尚未获取设备数据'" :image-size="76" />
      </el-card>
    </div>
    <div class="dashboard-distributions">
      <el-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>产品设备分布</strong><el-button v-permission="'menu:products'" text @click="emit('navigate','products')">管理产品</el-button></div></template>
        <div v-if="products.length" class="horizontal-chart"><div v-for="item in products" :key="item.key" class="bar-row"><div><span :title="item.name">{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>台</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / productMax * 100}%`, background:item.color }" /></div></div></div>
        <el-empty v-else :description="data ? '暂无设备分布' : loading ? '正在读取产品' : '尚未获取产品数据'" :image-size="65" />
      </el-card>
      <el-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>活动告警等级</strong><span class="chart-meta">{{ data ? `${stats.activeAlarms.toLocaleString()} 条` : '—' }}</span></div></template>
        <div v-if="data && stats.activeAlarms" class="horizontal-chart"><div v-for="item in levels" :key="item.key" class="bar-row"><div><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>条</small></b></div><div class="bar-track"><i :style="{ width:`${item.count / stats.activeAlarms * 100}%`, background:item.color }" /></div></div></div>
        <el-empty v-else :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" />
      </el-card>
    </div>
    <el-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>最新活动告警</strong><el-button v-permission="'menu:alarms'" text @click="emit('navigate','alarms')">查看全部</el-button></div></template>
      <el-empty v-if="!alarms.length" :description="data ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" :image-size="65" />
      <div v-for="alarm in alarms" :key="alarm.alarmId" class="alarm-row"><i /><div><strong>{{ alarm.deviceName || alarm.deviceId }} · {{ alarmType(alarm.alarmType) }}</strong><small>{{ alarm.details?.description || alarm.details?.message || alarmType(alarm.alarmType) }} · {{ formatTime(alarm.lastTriggeredAt) }}</small></div><el-tag :type="tagType(alarm.alarmLevel)" effect="light" round>{{ label(alarmLevels,alarm.alarmLevel,'未设置') }}</el-tag><el-button text @click="showDetail(alarm.alarmId)">详情</el-button></div>
    </el-card>
  <el-dialog v-model="detailVisible" title="告警详情" width="min(680px, 94vw)"><el-descriptions v-if="detail" :column="1" border><el-descriptions-item label="告警编号">{{ detail.alarmId }}</el-descriptions-item><el-descriptions-item label="设备">{{ detail.deviceName || detail.deviceId }}</el-descriptions-item><el-descriptions-item label="告警类型">{{ alarmType(detail.alarmType) }}</el-descriptions-item><el-descriptions-item label="发生时间">{{ formatTime(detail.lastTriggeredAt) }}</el-descriptions-item></el-descriptions><details class="technical-details"><summary>查看原始记录</summary><pre>{{ JSON.stringify(detail,null,2) }}</pre></details><template #footer><el-button @click="detailVisible = false">关闭</el-button><el-button v-permission="'menu:alarms'" type="primary" @click="emit('navigate', 'alarms', { alarmId: detail?.alarmId })">前往告警处置</el-button></template></el-dialog>
  </div>
</template>
