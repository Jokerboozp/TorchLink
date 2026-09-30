<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Activity, Plus, RefreshCw } from '@lucide/vue'
import { api, apiAll, notifyError, session } from '../api.js'
import { can, permissionState } from '../permissions.js'
import { createClientId } from '../clientId.js'
import { qualityCatalog, qualityRead, qualityWrite } from '../quality/api.js'
import { qualityNavigationTarget, qualityStage, qualityTime, recordBody, resolveQualityRun, runIsActive, stateLabel, stateTone } from '../quality/helpers.js'
import QualityProfiles from '../components/data-quality/QualityProfiles.vue'
import QualityRecords from '../components/data-quality/QualityRecords.vue'
import QualityResults from '../components/data-quality/QualityResults.vue'
const props = defineProps({ deviceId: { type: String, default: '' } })
const emit = defineEmits(['navigate'])
const tab = ref('runs'), loading = ref(false), error = ref(''), saving = ref(false), runDialog = ref(false), runError = ref('')
const devices = ref([]), profiles = ref([]), baselines = ref([]), calibrations = ref([]), runs = ref([]), selectedRun = ref(null)
const runPage = ref(1), runTotal = ref(0)
const form = reactive({ deviceIds: [], profileRevisionIds: [], baselineRevisionIds: [], range: null, idempotencyKey: '' })
const initialDevice = ref('')
const requestedRun = ref('')
const viewKey = ref(0)
const filteredDevice = computed(() => props.deviceId || initialDevice.value)
const runOptions = computed(() => profiles.value.map(recordBody).filter(row => !form.deviceIds.length || form.deviceIds.some(id => row.deviceIds?.includes(id))))
const baselineOptions = computed(() => baselines.value.map(recordBody).filter(row => row.confirmedBy && form.deviceIds.includes(row.deviceId) && form.profileRevisionIds.includes(row.profileRevisionId)))
const deviceName = id => devices.value.find(row => row.id === id)?.name || id
let generation = 0, runGeneration = 0, listGeneration = 0, timer = 0, disposed = false
const cacheKey = () => `iot:quality-run:${session.tenant}:${session.user}:${props.deviceId || 'management'}:${permissionState.accessVersion || ''}`
function remember(id) { try { if (id) sessionStorage.setItem(cacheKey(), id); else sessionStorage.removeItem(cacheKey()) } catch { /* Private browsing still supports server persistence. */ } }
function stopPolling() { clearTimeout(timer); timer = 0 }
function resetVisible() {
  generation++; runGeneration++; listGeneration++; viewKey.value++; stopPolling(); selectedRun.value = null; runDialog.value = false; runPage.value = 1
  profiles.value = []; baselines.value = []; calibrations.value = []; devices.value = []; runs.value = []; runTotal.value = 0
  form.deviceIds = []; form.profileRevisionIds = []; form.baselineRevisionIds = []
}
function schedule() { stopPolling(); if (!disposed && selectedRun.value && runIsActive(selectedRun.value)) timer = setTimeout(refreshRun, 1800) }
async function refreshRun() {
  const id = selectedRun.value?.id, token = ++runGeneration
  if (!id) return
  try {
    const value = await qualityRead('runs', id)
    if (token !== runGeneration || disposed) return
    if (props.deviceId && !value.deviceIds?.includes(props.deviceId)) throw new Error('该任务不属于当前设备')
    selectedRun.value = value; schedule()
    if (!runIsActive(value)) await loadRuns()
  } catch (cause) { if (token === runGeneration && !disposed) { error.value = cause.message; stopPolling(); if (cause.status === 403) { selectedRun.value = null; remember('') } } }
}
async function openRun(run) { selectedRun.value = run; remember(run.id); error.value = ''; await refreshRun() }
async function loadRuns() {
  const token = ++listGeneration, view = generation
  const value = await qualityRead('runs', '', '', { limit: 20, offset: (runPage.value - 1) * 20, deviceId: filteredDevice.value })
  if (disposed || token !== listGeneration || view !== generation) return
  runs.value = (value.items || []).filter(row => !filteredDevice.value || row.deviceIds?.includes(filteredDevice.value)); runTotal.value = value.total || 0
}
async function load() {
  const token = ++generation; loading.value = true; error.value = ''
  try {
    const jobs = [qualityCatalog('profiles'), qualityCatalog('baselines'), qualityCatalog('calibrations'), props.deviceId ? api(`/api/v1/device-registry/${encodeURIComponent(props.deviceId)}/connection`).then(value => ({ items: [value.device] })) : apiAll('/api/v1/device-registry')]
    const [profileData, baselineData, calibrationData, deviceData] = await Promise.all(jobs)
    if (token !== generation || disposed) return
    const newestFirst = (a, b) => b.createdAt - a.createdAt || String(b.id).localeCompare(String(a.id))
    profiles.value = (profileData.items || []).slice().sort(newestFirst); baselines.value = (baselineData.items || []).slice().sort(newestFirst); calibrations.value = (calibrationData.items || []).slice().sort(newestFirst)
    devices.value = (deviceData.items || []).map(row => row.device ? { ...row.device, productName: row.product?.name || row.productName } : row).filter(row => row?.id)
    await loadRuns()
    let restored = ''
    try { restored = sessionStorage.getItem(cacheKey()) || '' } catch { /* Server lists remain available. */ }
    if (requestedRun.value) {
      const row = await resolveQualityRun({ requestedRunId: requestedRun.value, cachedRunId: restored, read: id => qualityRead('runs', id) })
      if (token !== generation || disposed) return
      if (props.deviceId && !row.deviceIds?.includes(props.deviceId)) throw new Error('关联数据质量任务不属于当前设备')
      selectedRun.value = row; remember(row.id); schedule(); requestedRun.value = ''
    }
    else if (!selectedRun.value && restored) await openRun({ id: restored })
    else if (selectedRun.value) await refreshRun()
  } catch (cause) { if (token === generation && !disposed) { error.value = requestedRun.value && [403,404].includes(cause.status) ? '关联的固定数据质量任务已不存在或超出当前授权范围。' : cause.message; if (requestedRun.value) { selectedRun.value = null; remember('') } } }
  finally { if (token === generation) loading.value = false }
}
function newRun(previous = null) {
  const now = Date.now()
  form.deviceIds = props.deviceId ? [props.deviceId] : previous?.deviceIds?.slice() || (initialDevice.value ? [initialDevice.value] : [])
  form.profileRevisionIds = previous?.parameters?.profileRevisionIds?.slice() || []
  form.baselineRevisionIds = previous?.parameters?.baselineRevisionIds?.slice() || []
  form.range = previous ? [previous.start, previous.end] : [now - 3600e3, now]
  form.previousRunId = previous?.id || ''; form.idempotencyKey = createClientId(); runError.value = ''; runDialog.value = true
}
async function createRun() {
  if (saving.value) return
  const token = generation
  saving.value = true; runError.value = ''
  try {
    if (!form.deviceIds.length || !form.profileRevisionIds.length || !form.range?.[0] || !(form.range[1] > form.range[0])) throw new Error('请选择设备、配置版本及有效时间区间')
    const revisionIds = [...new Set(form.profileRevisionIds)].sort()
    const selected = profiles.value.filter(row => revisionIds.includes(row.id)).map(recordBody)
    const value = await qualityWrite('runs', '', '', { deviceIds: form.deviceIds, start: form.range[0], end: form.range[1], configurationVersion: revisionIds.join(','), parameters: { attributeIds: [...new Set(selected.map(row => row.attributeId))], profileRevisionIds: revisionIds, baselineRevisionIds: form.baselineRevisionIds }, previousRunId: form.previousRunId, idempotencyKey: form.idempotencyKey })
    if (token !== generation || disposed) return
    runDialog.value = false; await openRun(value); await loadRuns()
  } catch (cause) { runError.value = cause.message; notifyError(cause) }
  finally { saving.value = false }
}
async function stopRun() {
  if (!selectedRun.value || saving.value) return
  const token = generation, id = selectedRun.value.id, version = selectedRun.value.version
  saving.value = true
  try { const value = await qualityWrite('runs', id, 'stop', { expectedVersion: version }); if (token !== generation || disposed) return; selectedRun.value = value; stopPolling(); await loadRuns() }
  catch (cause) { notifyError(cause); if (cause.status === 409) await refreshRun() }
  finally { saving.value = false }
}
watch(runPage, () => loadRuns().catch(notifyError))
watch(() => props.deviceId, () => { resetVisible(); load() })
watch(() => permissionState.accessVersion, () => { resetVisible(); load() })
onMounted(() => {
  if (!props.deviceId) try { const detail = qualityNavigationTarget(JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}')); initialDevice.value = detail.deviceId; requestedRun.value = detail.runId; sessionStorage.removeItem('iot:navigation-detail') } catch { /* Invalid initial selection is ignored. */ }
  load()
})
onBeforeUnmount(() => { disposed = true; generation++; runGeneration++; listGeneration++; stopPolling() })
</script>
<template>
 <div class="data-quality-page" :class="{'quality-embedded':!!deviceId}">
  <header class="quality-toolbar"><div><h1><Activity/>数据质量<span v-if="deviceId" class="quality-heading-device"> · {{deviceName(deviceId)}}</span></h1><p class="quality-hint">分维度核对测量、时间和解析依据，人工核实变化线索。</p></div><div class="quality-actions"><ui-button size="small" :loading="loading" @click="load"><RefreshCw/>刷新</ui-button><ui-button v-if="can('POST /api/v1/data-quality/runs')" type="primary" size="small" @click="newRun()"><Plus/>新建分析</ui-button><ui-button v-if="deviceId" text size="small" @click="emit('navigate','dataQuality',{deviceId})">管理页</ui-button></div></header>
  <ui-alert v-if="error" :title="error" type="error" :closable="false"><ui-button size="small" @click="load">重新加载</ui-button></ui-alert>
  <ui-tabs v-model="tab"><ui-tab-pane name="runs" label="分析任务"/><ui-tab-pane name="profiles" label="质量配置"/><ui-tab-pane name="baselines" label="确认基线"/><ui-tab-pane name="calibrations" label="校准记录"/></ui-tabs>
  <ui-skeleton v-if="loading&&!profiles.length&&!runs.length" :rows="5" animated/>
  <QualityProfiles v-else-if="tab==='profiles'" :key="viewKey+'-profiles'" :profiles="profiles" :devices="devices" :device-id="deviceId" @refresh="load"/>
  <QualityRecords v-else-if="tab==='baselines'||tab==='calibrations'" :key="viewKey+'-'+tab" :kind="tab" :items="tab==='baselines'?baselines:calibrations" :profiles="profiles" :devices="devices" :device-id="deviceId" @refresh="load"/>
  <section v-else-if="tab==='runs'" class="quality-stack">
   <ui-alert v-if="selectedRun" :type="selectedRun.status==='PARTIAL'?'warning':selectedRun.status==='FAILED'?'error':'info'" :closable="false"><div class="quality-toolbar"><div><strong>{{stateLabel(selectedRun.status)}}</strong><p class="quality-meta">{{qualityStage(selectedRun.stage)}} · 实际处理 {{selectedRun.processed || 0}} 条 · {{qualityTime(selectedRun.start)}} 至 {{qualityTime(selectedRun.end)}}</p><p v-if="selectedRun.error" class="quality-meta">{{selectedRun.error}}</p></div><div class="quality-actions"><ui-button v-if="runIsActive(selectedRun)&&can('POST /api/v1/data-quality/runs/:id/stop')" size="small" :loading="saving" @click="stopRun">停止任务</ui-button><ui-button v-else-if="can('POST /api/v1/data-quality/runs')" size="small" @click="newRun(selectedRun)">重算新任务</ui-button></div></div></ui-alert>
   <div class="quality-table-scroll"><ui-table :data="runs" empty-text="暂无分析任务。填写质量配置后，明确选择设备和时间区间。"><ui-table-column label="分析区间" min-width="240"><template #default="{row}">{{qualityTime(row.start)}}<p class="quality-meta">至 {{qualityTime(row.end)}}</p></template></ui-table-column><ui-table-column label="设备" min-width="200"><template #default="{row}">{{(row.deviceIds || []).map(deviceName).join('、')}}</template></ui-table-column><ui-table-column label="状态 / 实际数量" min-width="160"><template #default="{row}"><ui-tag :type="stateTone(row.status)">{{stateLabel(row.status)}}</ui-tag><p class="quality-meta">{{qualityStage(row.stage)}} · {{row.processed || 0}} 条</p></template></ui-table-column><ui-table-column label="操作" width="100"><template #default="{row}"><ui-button text size="small" @click="openRun(row)">{{runIsActive(row)?'查看进度':'查看结果'}}</ui-button></template></ui-table-column></ui-table></div>
   <ui-pagination :current-page="runPage" :page-size="20" :total="runTotal" @current-change="runPage=$event"/>
   <QualityResults v-if="selectedRun" :run="selectedRun" :devices="devices" :profiles="profiles" :baselines="baselines" :device-id="deviceId" @refresh="refreshRun" @navigate="(name,detail)=>emit('navigate',name,detail)"/>
  </section>
 </div>
 <ui-dialog v-model="runDialog" title="新建数据质量分析" width="min(720px,94vw)" :close-on-click-modal="false" @close="runDialog=false"><div class="quality-dialog-body"><ui-alert v-if="runError" :title="runError" type="error" :closable="false"/><ui-form label-position="top" :disabled="saving"><ui-form-item v-if="!deviceId" label="明确设备范围" required><ui-select v-model="form.deviceIds" multiple filterable @change="form.profileRevisionIds=[];form.baselineRevisionIds=[]"><ui-option v-for="row in devices" :key="row.id" :value="row.id" :label="row.name || row.id"/></ui-select></ui-form-item><p v-else class="quality-hint">当前设备：{{deviceName(deviceId)}}</p><ui-form-item label="固定质量配置版本" required><ui-select v-model="form.profileRevisionIds" multiple filterable placeholder="选择属性及已保存的配置版本" @change="form.baselineRevisionIds=[]"><ui-option v-for="row in runOptions" :key="row.revisionId" :value="row.revisionId" :label="`${row.attributeId} · 版本 ${row.revisionVersion} · ${row.mode==='event'?'事件':'周期'} · ${row.deviceIds.map(deviceName).join('、')} · ${qualityTime(row.createdAt)}`"/></ui-select></ui-form-item><ui-form-item label="已确认基线（选填）"><ui-select v-model="form.baselineRevisionIds" multiple filterable placeholder="仅列出匹配设备、配置和人工确认的基线"><ui-option v-for="row in baselineOptions" :key="row.revisionId" :value="row.revisionId" :label="`${row.attributeId} · ${row.operatingCondition} · ${row.sampleCount} 条`"/></ui-select></ui-form-item><ui-form-item label="分析时间区间" required><ui-date-range v-model="form.range" disable-future/></ui-form-item><p class="quality-hint">运行后冻结输入与指标。补到数据或修改配置后重算，生成独立新任务。</p></ui-form></div><template #footer><ui-button :disabled="saving" @click="runDialog=false">取消</ui-button><ui-button type="primary" :loading="saving" @click="createRun">开始事实计算</ui-button></template></ui-dialog>
</template>
<style>
.data-quality-page{display:grid;grid-template-columns:minmax(0,1fr);gap:16px;min-width:0}.data-quality-page h1{display:flex;align-items:center;gap:8px;margin:0;font-size:20px}.data-quality-page h1>svg{width:21px;height:21px}.quality-heading-device{font-size:14px;color:var(--text-muted);overflow-wrap:anywhere}.quality-toolbar,.quality-actions{display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap;min-width:0}.quality-toolbar>div{min-width:0}.quality-actions{justify-content:flex-end}.quality-stack{display:grid;gap:14px;grid-template-columns:minmax(0,1fr);min-width:0}.quality-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:0 14px}.quality-grid .ui-form-item,.quality-grid .ui-select{min-width:0}.quality-dialog-body,.quality-details-body{display:grid;gap:14px;max-height:68vh;overflow:auto;min-width:0;padding:0 4px 8px 0}.quality-hint,.quality-meta{font-size:12px;line-height:1.6;color:var(--text-muted);margin:5px 0;overflow-wrap:anywhere}.quality-table-scroll{overflow-x:auto;min-width:0}.quality-table-scroll .n-data-table{min-width:650px}.quality-card{background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:14px;min-width:0}.quality-card h3,.quality-subtitle{margin:0 0 10px;font-size:14px}.quality-checks{display:flex;gap:10px;flex-wrap:wrap}.quality-tag-list{display:flex;gap:6px;flex-wrap:wrap}.quality-ratio-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px}.quality-ratio-grid article{padding:12px;border:1px solid var(--border);border-radius:6px;min-width:0}.quality-ratio-grid strong{display:block;margin:5px 0;font-size:17px}.quality-filter-row{display:flex;gap:8px;flex-wrap:wrap;min-width:0}.quality-filter-row>*{min-width:140px;flex:1}.quality-native-table{width:100%;border-collapse:collapse;font-size:12px}.quality-native-table th,.quality-native-table td{text-align:left;border-bottom:1px solid var(--border);padding:8px;overflow-wrap:anywhere}.quality-native-table th{color:var(--text-muted)}@media(max-width:640px){.quality-grid{grid-template-columns:minmax(0,1fr)}.quality-ratio-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.quality-dialog-body,.quality-details-body{max-height:65vh}.data-quality-page h1{font-size:18px;flex-wrap:wrap}.quality-actions{justify-content:flex-start}.quality-card{padding:11px}.quality-toolbar{align-items:flex-start}.quality-embedded .quality-ratio-grid{grid-template-columns:minmax(0,1fr)}}
</style>
