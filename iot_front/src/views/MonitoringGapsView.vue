<script setup>
import DeviceSelect from '../components/DeviceSelect.vue'
import { provideDeviceCatalog } from '../useDeviceCatalog.js'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Activity, Plus, RefreshCw } from '@lucide/vue'
import { notifyError, session } from '../api.js'
import { can, permissionState } from '../permissions.js'
import { createClientId } from '../clientId.js'
import { qualityAll } from '../quality/api.js'
import { monitoringAll, monitoringRead, monitoringWrite } from '../monitoring/api.js'
import { monitoringStage, monitoringTime, recordBody, runIsActive, stateLabel, stateTone } from '../monitoring/helpers.js'
import MonitoringProfiles from '../components/monitoring-gaps/MonitoringProfiles.vue'
import MonitoringObservations from '../components/monitoring-gaps/MonitoringObservations.vue'
import MonitoringResults from '../components/monitoring-gaps/MonitoringResults.vue'
const props = defineProps({ deviceId: { type: String, default: '' } })
const emit = defineEmits(['navigate'])
const tab = ref('runs'), loading = ref(false), error = ref(''), saving = ref(false), dialog = ref(false), formError = ref(''), viewKey = ref(0)
const devices = ref([]), profiles = ref([]), observations = ref([]), qualityRuns = ref([]), runs = ref([]), selectedRun = ref(null), page = ref(1), total = ref(0), initialDevice = ref('')
const deviceFilter = computed(() => props.deviceId || initialDevice.value)
const form = reactive({ deviceIds: [], profileRevisionIds: [], observationRevisionIds: [], qualityRunIds: [], range: null, commonEnabled: false, commonVersion: '', minimumGapCount: null, minimumOverlapSeconds: null, minimumJaccard: null, previousRunId: '', idempotencyKey: '' })
const deviceName = id => devices.value.find(row => row.id === id)?.name || id
const profileOptions = computed(() => profiles.value.map(recordBody).filter(row => form.deviceIds.some(id => row.deviceIds?.includes(id))))
const observationOptions = computed(() => observations.value.map(recordBody).filter(row => row.status === 'CONFIRMED' && form.deviceIds.includes(row.deviceId) && (!form.range || (row.start < form.range[1] && row.end > form.range[0]))))
const qualityOptions = computed(() => qualityRuns.value.filter(row => row.snapshotId && ['SUCCEEDED','PARTIAL'].includes(row.status) && row.deviceIds?.length && row.deviceIds.every(id => form.deviceIds.includes(id)) && (!form.range || (row.start < form.range[1] && row.end > form.range[0]))))
const deviceCatalog = provideDeviceCatalog(devices, () => [props.deviceId,initialDevice.value,...form.deviceIds,...(selectedRun.value?.deviceIds||[])])
let generation = 0, runGeneration = 0, listGeneration = 0, timer = 0, disposed = false
const cacheKey = () => `iot:monitoring-run:${session.tenant}:${session.user}:${props.deviceId || 'management'}:${permissionState.accessVersion || ''}`
function remember(id) { try { if (id) sessionStorage.setItem(cacheKey(), id); else sessionStorage.removeItem(cacheKey()) } catch { /* Server persistence remains available. */ } }
function stopPolling() { clearTimeout(timer); timer = 0 }
function reset() { generation++; runGeneration++; listGeneration++; viewKey.value++; stopPolling(); selectedRun.value = null; dialog.value = false; page.value = 1; runs.value = []; profiles.value = []; observations.value = []; qualityRuns.value = []; devices.value = []; total.value = 0; form.deviceIds = []; form.profileRevisionIds = []; form.observationRevisionIds = []; form.qualityRunIds = [] }
function schedule() { stopPolling(); if (!disposed && runIsActive(selectedRun.value)) timer = setTimeout(refreshRun, 1800) }
async function refreshRun() {
 const id = selectedRun.value?.id, token = ++runGeneration
 if (!id) return
 try { const row = await monitoringRead('runs', id); if (disposed || token !== runGeneration) return; if (props.deviceId && !row.deviceIds?.includes(props.deviceId)) throw new Error('该任务不属于当前设备'); selectedRun.value = row; schedule(); if (!runIsActive(row)) await loadRuns() }
 catch (cause) { if (token === runGeneration && !disposed) { error.value = cause.message; stopPolling(); if (cause.status === 403) { selectedRun.value = null; remember('') } } }
}
async function openRun(row) { selectedRun.value = row; remember(row.id); error.value = ''; await refreshRun() }
async function loadRuns() {
 const token = ++listGeneration, view = generation
 const value = await monitoringRead('runs', '', '', { deviceId: deviceFilter.value, limit: 20, offset: (page.value - 1) * 20 })
 if (disposed || token !== listGeneration || view !== generation) return
 runs.value = (value.items || []).filter(row => !deviceFilter.value || row.deviceIds?.includes(deviceFilter.value)); total.value = value.total || 0
}
async function load() {
 const token = ++generation; loading.value = true; error.value = ''
 try {
  if (!can('menu:devices')) throw new Error('监测连续性需要设备管理读取权限，以及管理员分配的设备范围。')
  const mainList = loadRuns().catch(cause => { if (token === generation && !disposed) error.value = cause.message })
  const [p,o,q] = await Promise.all([monitoringAll('profiles'), monitoringAll('observations'), can('menu:dataQuality') ? qualityAll('runs') : Promise.resolve({items:[]})])
  if (disposed || token !== generation) return
  const newest = (a,b) => b.createdAt-a.createdAt || String(b.id).localeCompare(String(a.id))
  profiles.value = (p.items || []).slice().sort(newest); observations.value = (o.items || []).slice().sort(newest); qualityRuns.value = (q.items || []).slice().sort(newest)
  await mainList
  let restored = ''; try { restored = sessionStorage.getItem(cacheKey()) || '' } catch { /* No client cache is required to read runs. */ }
  if (!selectedRun.value && restored) await openRun({id:restored}); else if (selectedRun.value) await refreshRun()
 } catch (cause) { if (token === generation && !disposed) error.value = cause.message }
 finally { if (token === generation) loading.value = false }
}
function newRun(previous) {
 const now = Date.now(), params = previous?.parameters || {}
 Object.assign(form, { deviceIds: props.deviceId ? [props.deviceId] : previous?.deviceIds?.slice() || (initialDevice.value ? [initialDevice.value] : []), profileRevisionIds: params.profileRevisionIds?.slice() || [], observationRevisionIds: params.observationRevisionIds?.slice() || [], qualityRunIds: params.qualityRunIds?.slice() || [], range: previous ? [previous.start,previous.end] : [now-3600000,now], commonEnabled: !!params.commonGapPolicy, commonVersion: params.commonGapPolicy?.version || '', minimumGapCount: params.commonGapPolicy?.minimumGapCount ?? null, minimumOverlapSeconds: params.commonGapPolicy?.minimumOverlapMs == null ? null : params.commonGapPolicy.minimumOverlapMs/1000, minimumJaccard: params.commonGapPolicy?.minimumJaccard ?? null, previousRunId: previous?.id || '', idempotencyKey: createClientId() })
 formError.value = ''; dialog.value = true
}
async function createRun() {
 if (saving.value) return
 const token = generation; saving.value = true; formError.value = ''
 try {
  if (!form.deviceIds.length || !form.profileRevisionIds.length || !form.range?.[0] || !(form.range[1]>form.range[0])) throw new Error('请选择明确设备、固定监测策略和有效分析区间')
  let policy
  if (form.commonEnabled) {
   if (!form.commonVersion.trim() || !Number.isSafeInteger(Number(form.minimumGapCount)) || !(form.minimumGapCount>=1) || !Number.isSafeInteger(Number(form.minimumOverlapSeconds)*1000) || !(form.minimumOverlapSeconds>0) || form.minimumJaccard==null || !Number.isFinite(Number(form.minimumJaccard)) || form.minimumJaccard<0 || form.minimumJaccard>1) throw new Error('请填写共同缺报策略版本、最少缺口数、重叠时长和0至1的Jaccard门槛')
   policy = {version:form.commonVersion.trim(),minimumGapCount:Number(form.minimumGapCount),minimumOverlapMs:Number(form.minimumOverlapSeconds)*1000,minimumJaccard:Number(form.minimumJaccard)}
  }
  const revisionIds = [...new Set(form.profileRevisionIds)].sort()
  const row = await monitoringWrite('runs','','',{deviceIds:form.deviceIds,start:form.range[0],end:form.range[1],configurationVersion:revisionIds.join(','),parameters:{profileRevisionIds:revisionIds,observationRevisionIds:form.observationRevisionIds,qualityRunIds:form.qualityRunIds,commonGapPolicy:policy},previousRunId:form.previousRunId,idempotencyKey:form.idempotencyKey})
  if (disposed || token!==generation) return
  dialog.value=false;await openRun(row);await loadRuns()
 } catch(cause) { if(token===generation&&!disposed){formError.value=cause.message;notifyError(cause)} }
 finally { saving.value=false }
}
async function stopRun() {
 if (!selectedRun.value || saving.value) return
 const id=selectedRun.value.id,version=selectedRun.value.version,token=generation;saving.value=true
 try { const row=await monitoringWrite('runs',id,'stop',{expectedVersion:version});if(token!==generation||disposed)return;selectedRun.value=row;stopPolling();await loadRuns() }
 catch(cause){if(token===generation&&!disposed){notifyError(cause);if(cause.status===409)await refreshRun()}}
 finally{saving.value=false}
}
watch(page,()=>loadRuns().catch(notifyError));watch(()=>props.deviceId,()=>{reset();load()});watch(()=>[permissionState.accessVersion,session.tenant,session.user],()=>{reset();load()})
onMounted(()=>{if(!props.deviceId)try{const detail=JSON.parse(sessionStorage.getItem('iot:navigation-detail')||'{}');initialDevice.value=detail.deviceId||'';sessionStorage.removeItem('iot:navigation-detail')}catch{/* Invalid navigation data is ignored. */}load()})
onBeforeUnmount(()=>{disposed=true;generation++;runGeneration++;listGeneration++;stopPolling()})
</script>
<template>
 <div class="monitoring-gaps-page" :class="{'monitor-embedded':!!deviceId}"><header class="monitor-toolbar"><div><h1><Activity/>监测连续性<span v-if="deviceId" class="monitor-heading-device"> · {{deviceName(deviceId)}}</span></h1><p class="monitor-hint">核对连接、关键属性数据与接入依赖的实际覆盖。</p></div><div class="monitor-actions"><ui-button size="small" :loading="loading" @click="load"><RefreshCw/>刷新</ui-button><ui-button v-if="can('menu:devices')&&can('POST /api/v1/monitoring-gaps/runs')" type="primary" size="small" @click="newRun()"><Plus/>新增连续性分析</ui-button><ui-button v-if="deviceId" text size="small" @click="emit('navigate','monitoringGaps',{deviceId})">管理页</ui-button></div></header>
  <ui-alert v-if="error" :title="error" type="error" :closable="false"><ui-button size="small" @click="load">重新加载</ui-button></ui-alert><ui-tabs v-model="tab"><ui-tab-pane name="runs" label="分析任务"/><ui-tab-pane name="profiles" label="监测策略"/><ui-tab-pane name="observations" label="观察窗口"/></ui-tabs><ui-skeleton v-if="loading&&!profiles.length&&!runs.length" :rows="5" animated/>
  <MonitoringProfiles v-else-if="tab==='profiles'" :key="viewKey+'-profiles'" :profiles="profiles" :devices="devices" :device-id="deviceId" @refresh="load"/>
  <MonitoringObservations v-else-if="tab==='observations'" :key="viewKey+'-observations'" :items="observations" :devices="devices" :device-id="deviceId" @refresh="load"/>
  <section v-else-if="tab==='runs'" class="monitor-stack"><ui-alert v-if="selectedRun" :type="selectedRun.status==='PARTIAL'?'warning':selectedRun.status==='FAILED'?'error':'info'" :closable="false"><div class="monitor-toolbar"><div><strong>{{stateLabel(selectedRun.status)}}</strong><p class="monitor-meta">{{monitoringStage(selectedRun.stage)}} · 实际处理 {{selectedRun.processed || 0}} 条 · {{monitoringTime(selectedRun.start)}} 至 {{monitoringTime(selectedRun.end)}}</p><p v-if="selectedRun.error" class="monitor-meta">{{selectedRun.error}}</p></div><div class="monitor-actions"><ui-button v-if="runIsActive(selectedRun)&&can('POST /api/v1/monitoring-gaps/runs/:id/stop')" size="small" :loading="saving" @click="stopRun">停止任务</ui-button><ui-button v-else-if="can('POST /api/v1/monitoring-gaps/runs')" size="small" @click="newRun(selectedRun)">重算新任务</ui-button></div></div></ui-alert>
   <div class="monitor-table-scroll"><ui-table :data="runs" empty-text="暂无分析任务，请先明确关键属性和监测策略。"><ui-table-column label="分析区间" min-width="240"><template #default="{row}">{{monitoringTime(row.start)}}<p class="monitor-meta">至 {{monitoringTime(row.end)}}</p></template></ui-table-column><ui-table-column label="获授权设备" min-width="200"><template #default="{row}">{{(row.deviceIds || []).map(deviceName).join('、')}}</template></ui-table-column><ui-table-column label="状态 / 实际数量" min-width="180"><template #default="{row}"><ui-tag :type="stateTone(row.status)">{{stateLabel(row.status)}}</ui-tag><p class="monitor-meta">{{monitoringStage(row.stage)}} · {{row.processed || 0}} 条</p></template></ui-table-column><ui-table-column label="操作" width="100"><template #default="{row}"><ui-button text size="small" @click="openRun(row)">{{runIsActive(row)?'查看进度':'查看结果'}}</ui-button></template></ui-table-column></ui-table></div><ui-pagination :current-page="page" :page-size="20" :total="total" @current-change="page=$event"/><MonitoringResults v-if="selectedRun" :run="selectedRun" :devices="devices" :profiles="profiles" :device-id="deviceId" @refresh="refreshRun" @navigate="(name,detail)=>emit('navigate',name,detail)"/>
  </section>
 </div>
 <ui-dialog v-model="dialog" title="新增监测连续性分析" width="min(800px,94vw)" :close-on-click-modal="false" @close="dialog=false"><div class="monitor-dialog-body"><ui-alert v-if="formError" :title="formError" type="error" :closable="false"/><ui-form label-position="top" :disabled="saving"><ui-form-item v-if="!deviceId" label="明确设备范围" required><DeviceSelect v-model="form.deviceIds" multiple @change="form.profileRevisionIds=[];form.observationRevisionIds=[];form.qualityRunIds=[]"/></ui-form-item><p v-else class="monitor-hint">当前设备：{{deviceName(deviceId)}}</p><ui-form-item label="固定监测策略版本" required><ui-select v-model="form.profileRevisionIds" multiple filterable><ui-option v-for="row in profileOptions" :key="row.revisionId" :value="row.revisionId" :label="`${row.attributes.map(a=>a.id).join('、')} · 版本 ${row.revisionVersion} · ${row.deviceIds.map(deviceName).join('、')} · ${monitoringTime(row.createdAt)}`"/></ui-select></ui-form-item><ui-form-item label="分析时间区间" required><ui-date-range v-model="form.range" disable-future/></ui-form-item><ui-form-item label="已确认观察窗口（选填）"><ui-select v-model="form.observationRevisionIds" multiple filterable><ui-option v-for="row in observationOptions" :key="row.revisionId" :value="row.revisionId" :label="`${deviceName(row.deviceId)} · ${row.type==='RUNNING'?'运行观察':row.type==='STOPPED'?'计划停运':'计划检修'} · ${monitoringTime(row.start)}`"/></ui-select></ui-form-item><ui-form-item v-if="can('menu:dataQuality')" label="固定数据质量任务（选填）"><ui-select v-model="form.qualityRunIds" multiple filterable><ui-option v-for="row in qualityOptions" :key="row.id" :value="row.id" :label="`${row.deviceIds.map(deviceName).join('、')} · ${monitoringTime(row.start)} · ${stateLabel(row.status)} · 创建于 ${monitoringTime(row.createdAt)}`"/></ui-select></ui-form-item><ui-checkbox v-model="form.commonEnabled">明确评价共同缺报</ui-checkbox><div v-if="form.commonEnabled" class="monitor-grid"><ui-form-item label="共同缺报策略版本" required><ui-input v-model="form.commonVersion"/></ui-form-item><ui-form-item label="每设备最少缺口数" required><ui-input-number v-model="form.minimumGapCount" :min="1" :precision="0"/></ui-form-item><ui-form-item label="最少重叠时长（秒）" required><ui-input-number v-model="form.minimumOverlapSeconds" :min="0.001"/></ui-form-item><ui-form-item label="最小 Jaccard" required><ui-input-number v-model="form.minimumJaccard" :min="0" :max="1" :step="0.05"/></ui-form-item></div><p class="monitor-hint">运行冻结策略、来源与关系版本。共同缺报只表示时间重叠现象，不能直接判断共同原因。</p></ui-form></div><template #footer><ui-button :disabled="saving" @click="dialog=false">取消</ui-button><ui-button type="primary" :loading="saving" @click="createRun">开始连续性计算</ui-button></template></ui-dialog>
</template>
<style>
.monitoring-gaps-page{display:grid;grid-template-columns:minmax(0,1fr);gap:16px;min-width:0}.monitoring-gaps-page h1{display:flex;align-items:center;gap:8px;font-size:20px;margin:0}.monitoring-gaps-page h1 svg{width:21px;height:21px}.monitor-heading-device{font-size:14px;color:var(--text-muted);overflow-wrap:anywhere}.monitor-toolbar,.monitor-actions{display:flex;align-items:center;gap:10px;justify-content:space-between;flex-wrap:wrap;min-width:0}.monitor-toolbar>div{min-width:0}.monitor-actions{justify-content:flex-end}.monitor-stack{display:grid;grid-template-columns:minmax(0,1fr);gap:14px;min-width:0}.monitor-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:0 14px}.monitor-grid .ui-form-item,.monitor-grid .ui-select{min-width:0}.monitor-dialog-body{display:grid;grid-template-columns:minmax(0,1fr);gap:14px;max-height:68vh;overflow:auto;min-width:0;padding:0 4px 8px 0}.monitor-hint,.monitor-meta{font-size:12px;line-height:1.6;color:var(--text-muted);margin:5px 0;overflow-wrap:anywhere;white-space:pre-wrap}.monitor-card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px;min-width:0}.monitor-card h3,.monitor-subtitle{font-size:14px;margin:0 0 10px}.monitor-table-scroll{overflow-x:auto;min-width:0}.monitor-table-scroll .n-data-table{min-width:660px}.monitor-native-table{width:100%;min-width:640px;border-collapse:collapse;font-size:12px}.monitor-native-table th,.monitor-native-table td{padding:8px;text-align:left;border-bottom:1px solid var(--border);overflow-wrap:anywhere}.monitor-native-table th{color:var(--text-muted)}.monitor-filter-row{display:flex;gap:8px;flex-wrap:wrap;min-width:0}.monitor-filter-row>*{min-width:145px;flex:1}.monitor-stat-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.monitor-stat-grid article{padding:12px;border:1px solid var(--border);border-radius:6px;min-width:0}.monitor-stat-grid strong{display:block;margin:6px 0;font-size:16px;overflow-wrap:anywhere}.monitor-group-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.monitor-tags{display:flex;flex-wrap:wrap;gap:6px}@media(max-width:640px){.monitor-grid,.monitor-group-grid{grid-template-columns:minmax(0,1fr)}.monitor-stat-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.monitor-dialog-body{max-height:65vh}.monitoring-gaps-page h1{font-size:18px;flex-wrap:wrap}.monitor-actions{justify-content:flex-start}.monitor-card{padding:11px}.monitor-toolbar{align-items:flex-start}}
</style>
