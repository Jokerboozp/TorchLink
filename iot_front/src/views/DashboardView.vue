<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, formatTime, notifyError } from '../api'
import { alarmLevels, alarmType, label, tagType } from '../labels'
import { RefreshCw } from '@lucide/vue'

const emit = defineEmits(['navigate'])
const stats = ref({ devices:0, online:0, alarms:0, high:0, offline:0 })
const alarms = ref([])
const detail = ref(null)
const detailVisible = ref(false)
const loading = ref(false)
const loaded = ref(false)
const loadError = ref('')
const updatedAt = ref(0)
const rate = computed(() => stats.value.devices ? Math.round(stats.value.online / stats.value.devices * 100) : 0)
async function load() {
  if (loading.value) return
  loading.value = true
  try {
    const [devices, active, registry] = await Promise.all([api('/api/v1/devices'), api('/api/v1/alarms?status=ACTIVE&limit=8'), api('/api/v1/device-registry')])
    alarms.value = active.items || []
    loaded.value = true
    loadError.value = ''
    updatedAt.value = Date.now()
    stats.value = { devices:registry.total ?? registry.count ?? devices.total ?? 0, online:devices.online || 0, offline:devices.offline || 0, alarms:active.count || 0, high:alarms.value.filter(x => ['HIGH','CRITICAL'].includes(x.alarmLevel)).length }
  } catch (error) { loadError.value = '概况读取失败，请检查服务连接后重试。已有数据保留至下次刷新成功。'; notifyError(error) }
  finally { loading.value = false }
}
async function showDetail(id) { try { detail.value = await api(`/api/v1/alarms/${encodeURIComponent(id)}`); detailVisible.value = true } catch (e) { notifyError(e) } }
const realtime = () => load()
onMounted(() => { load(); window.addEventListener('iot:realtime', realtime) })
onBeforeUnmount(() => window.removeEventListener('iot:realtime', realtime))
</script>

<template>
  <el-alert v-if="loadError" class="dashboard-error" :title="loadError" type="error" :closable="false" show-icon />
  <div class="section-toolbar"><strong>实时概况 <small class="muted-text">{{ updatedAt ? `更新于 ${formatTime(updatedAt)}` : '等待首次更新' }}</small></strong><el-button :loading="loading" @click="load"><RefreshCw :size="14" />刷新概况</el-button></div>
  <div class="stats-grid">
    <el-card v-for="item in [{label:'设备总数',value:stats.devices,note:'当前租户',tone:'primary'},{label:'在线设备',value:stats.online,note:'实时状态',tone:'success'},{label:'活动告警',value:stats.alarms,note:'待处置',tone:'warning'},{label:'近期高等级告警',value:stats.high,note:'最近八条活动告警中',tone:'danger'}]" :key="item.label" class="stat-card" shadow="never">
      <span>{{ item.label }}</span><strong>{{ loaded ? item.value : '—' }}</strong><small :class="item.tone">{{ item.note }}</small><i :class="item.tone" />
    </el-card>
  </div>
  <div class="dashboard-grid">
    <el-card shadow="never" class="surface-card"><template #header><div class="card-header"><strong>最新告警</strong><el-button plain type="primary" @click="emit('navigate','alarms')">查看全部</el-button></div></template>
      <el-empty :description="loaded ? '暂无活动告警' : loading ? '正在读取告警' : '尚未获取告警数据'" v-if="!alarms.length" :image-size="80" />
      <div v-for="alarm in alarms" :key="alarm.alarmId" class="alarm-row"><i /><div><strong>{{ alarmType(alarm.alarmType) }}</strong><small>{{ alarm.deviceName || alarm.deviceId }} · {{ formatTime(alarm.lastTriggeredAt) }}</small></div><el-tag :type="tagType(alarm.alarmLevel)" effect="light" round>{{ label(alarmLevels,alarm.alarmLevel,'未设置') }}</el-tag><el-button plain type="primary" @click="showDetail(alarm.alarmId)">详情</el-button></div>
    </el-card>
    <el-card shadow="never" class="surface-card"><template #header><strong>设备在线概况</strong></template>
      <el-empty v-if="!loaded" description="尚未获取设备数据" :image-size="70" /><div v-else class="online-summary"><el-progress type="circle" :percentage="rate" :width="150" :stroke-width="12" color="#1677ff" /><div><p><i class="online" />在线 <b>{{ stats.online }}</b></p><p><i />离线/疑似 <b>{{ stats.offline }}</b></p></div></div>
    </el-card>
  </div>
  <el-dialog v-model="detailVisible" title="告警详情" width="min(680px, 94vw)"><el-descriptions v-if="detail" :column="1" border><el-descriptions-item label="告警编号">{{ detail.alarmId }}</el-descriptions-item><el-descriptions-item label="设备">{{ detail.deviceName || detail.deviceId }}</el-descriptions-item><el-descriptions-item label="告警类型">{{ alarmType(detail.alarmType) }}</el-descriptions-item><el-descriptions-item label="发生时间">{{ formatTime(detail.lastTriggeredAt) }}</el-descriptions-item></el-descriptions><details class="technical-details"><summary>查看原始记录</summary><pre>{{ JSON.stringify(detail,null,2) }}</pre></details><template #footer><el-button @click="detailVisible = false">关闭</el-button><el-button type="primary" @click="emit('navigate', 'alarms', { alarmId: detail?.alarmId })">前往告警处置</el-button></template></el-dialog>
</template>
