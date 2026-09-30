<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { Download, RefreshCw, Sparkles } from '@lucide/vue'
import { can } from '../../permissions.js'
import { notifyError } from '../../api.js'
import { UiMessage } from '../../ui/feedback.js'
import { createClientId } from '../../clientId.js'
import { qualityAll, qualityExport, qualityRead, qualityWrite } from '../../quality/api.js'
import { compactNumber, evidenceChart, evidenceRows, findingKinds, findingRows, metricRows, qualityChartSamples, qualityTime, ratioLabel, recordBody, reviewStates, runIsActive, stateLabel, stateTone, timeMs } from '../../quality/helpers.js'
import TimeSeriesChart from '../ops/TimeSeriesChart.vue'
const props = defineProps({ run: Object, devices: { type: Array, default: () => [] }, profiles: { type: Array, default: () => [] }, baselines: { type: Array, default: () => [] }, deviceId: { type: String, default: '' } })
const emit = defineEmits(['navigate', 'refresh'])
const loading = ref(false), error = ref(''), snapshot = ref(null), metrics = ref([]), findings = ref([]), evidence = ref([]), reviews = ref([]), selectedMetricId = ref('')
const downloading = ref(false), aiBusy = ref(false), aiError = ref(''), aiJobs = ref([]), aiId = ref('')
const useKnowledge = ref(false)
const productFilter = ref(''), attributeFilter = ref(''), kindFilter = ref(''), reviewFilter = ref(''), page = ref(1)
const findingDialog = ref(false), selectedFinding = ref(null), reviewHistory = ref([]), reviewSaving = ref(false), reviewError = ref('')
const review = reactive({ result: '', explanation: '', correctsId: '', idempotencyKey: '' })
const series = ref(null), seriesLoading = ref(false), seriesError = ref(''), clock = ref('eventAt')
const resultRoot = ref(null)
let generation = 0, seriesGeneration = 0, detailGeneration = 0, aiTimer = 0, disposed = false
const deviceName = id => props.devices.find(row => row.id === id)?.name || id || '设备'
const productID = id => props.devices.find(row => row.id === id)?.productId || ''
const productName = id => props.devices.find(row => row.productId === id)?.productName || id
const visibleMetrics = computed(() => metrics.value.filter(row => !props.deviceId || row.deviceId === props.deviceId))
const selectedMetric = computed(() => visibleMetrics.value.find(row => row.id === selectedMetricId.value) || visibleMetrics.value[0])
const selectedProfile = computed(() => props.profiles.map(recordBody).find(row => row.revisionId === selectedMetric.value?.profileId))
const profileVersionLabel = metric => props.profiles.map(recordBody).find(row => row.revisionId === metric?.profileId)?.revisionVersion || '历史版本'
const latestReview = findingId => reviews.value.filter(row => row.resourceId === findingId).sort((a, b) => b.reviewedAt - a.reviewedAt || String(b.id).localeCompare(String(a.id)))[0]
const filteredFindings = computed(() => findings.value.filter(row => (!props.deviceId || row.deviceId === props.deviceId) && (!productFilter.value || productID(row.deviceId) === productFilter.value) && (!attributeFilter.value || row.attributeId === attributeFilter.value) && (!kindFilter.value || row.kind === kindFilter.value) && (!reviewFilter.value || (latestReview(row.id)?.result || 'UNREVIEWED') === reviewFilter.value)))
const pageFindings = computed(() => filteredFindings.value.slice((page.value - 1) * 20, page.value * 20))
const productOptions = computed(() => [...new Set(visibleMetrics.value.map(row => productID(row.deviceId)).filter(Boolean))])
const attributeOptions = computed(() => [...new Set(visibleMetrics.value.map(row => row.attributeId).filter(Boolean))])
const activeAI = computed(() => aiJobs.value.find(row => row.id === aiId.value) || aiJobs.value[0])
const aiContent = computed(() => activeAI.value?.interpretation || {})
const baseline = computed(() => {
  const finding = selectedFinding.value
  if (finding?.baselineId) return props.baselines.map(recordBody).find(row => [row.id, row.revisionId, row.resourceId].includes(finding.baselineId))
  const m = selectedMetric.value
  return props.baselines.map(recordBody).find(row => props.run?.parameters?.baselineRevisionIds?.includes(row.revisionId) && row.deviceId === m?.deviceId && row.attributeId === m?.attributeId && row.profileVersion === m?.profileVersion && row.confirmedBy)
})
const ratioCards = computed(() => {
  const m = selectedMetric.value
  if (!m) return []
  return [['格式问题', m.format], ['量程越界', m.range], ['事件时间缺报', m.eventCompleteness?.missing], ['平台接收缺报', m.receptionCompleteness?.missing], ['事件乱序', m.time?.outOfOrder], ['时间回退', m.time?.rollback], ['异常未来时间', m.time?.future], ['长期稳定线索', m.sequence?.stable], ['变化率线索', m.sequence?.rate], ['历史基线偏离', m.sequence?.deviation], ['CUSUM 漂移线索', m.sequence?.drift], ['最后解析失败', m.parse?.lastFailure], ['全部解析尝试失败', m.parse?.attemptFailure]].map(([label, value]) => ({ label, value }))
})
const selectedEvidence = computed(() => {
  if (selectedFinding.value) return evidence.value.filter(row => selectedFinding.value.evidenceIds?.includes(row.id) || selectedFinding.value.evidenceIds?.includes(row.sourceId))
  const m = selectedMetric.value
  return evidence.value.filter(row => row.deviceId === m?.deviceId && (!row.attributeId || row.attributeId === m?.attributeId))
})
const chart = computed(() => {
  const m = selectedMetric.value
  const samples = qualityChartSamples(series.value?.items || [], clock.value, m?.window)
  const threshold = ['range_below', 'range_above'].includes(selectedFinding.value?.kind) ? selectedFinding.value.threshold : undefined
  const data = evidenceChart(samples, baseline.value?.median, threshold)
  if (selectedFinding.value?.kind === 'historical_deviation' && baseline.value) {
    const profile = props.profiles.map(recordBody).find(row => row.revisionId === baseline.value.profileRevisionId)
    const lower = profile?.deviationMethod === 'quantile' ? baseline.value.lowerValue : baseline.value.median - selectedFinding.value.threshold
    const upper = profile?.deviationMethod === 'quantile' ? baseline.value.upperValue : baseline.value.median + selectedFinding.value.threshold
    if (Number.isFinite(lower) && Number.isFinite(upper)) data.series.push({ name: '历史比较下界', values: data.times.map(() => lower) }, { name: '历史比较上界', values: data.times.map(() => upper) })
  }
  return data
})
const chartRange = computed(() => {
  const from = timeMs(selectedMetric.value?.window?.start) || props.run?.start, to = timeMs(selectedMetric.value?.window?.end) || props.run?.end
  if (clock.value === 'eventAt' || !chart.value.times.length) return { from, to }
  return { from: Math.min(from, chart.value.times[0]), to: Math.max(to, chart.value.times.at(-1) + 1) }
})
const detailTitle = computed(() => findingKinds[selectedFinding.value?.kind] || '质量发现')
const showAI = computed(() => can('POST /api/v1/data-quality/runs/:id/ai-jobs'))
function stopAI() { clearTimeout(aiTimer); aiTimer = 0 }
async function loadAI() {
  const id = props.run?.id, token = generation
  if (!id) return
  try {
    const value = await qualityRead('runs', id, 'ai-jobs')
    if (token !== generation || disposed) return
    aiJobs.value = (value.items || []).slice().sort((a, b) => b.createdAt - a.createdAt || String(b.id).localeCompare(String(a.id)))
    if (!aiJobs.value.some(row => row.id === aiId.value)) aiId.value = aiJobs.value[0]?.id || ''
    stopAI()
    if (aiJobs.value.some(row => ['QUEUED', 'RUNNING'].includes(row.status))) aiTimer = setTimeout(loadAI, 2200)
  } catch (cause) { if (token === generation && !disposed) { aiError.value = cause.message; stopAI() } }
}
async function runAI(regenerate = false) {
  if (aiBusy.value || !props.run?.snapshotId) return
  const id = props.run.id, token = generation
  aiBusy.value = true; aiError.value = ''
  try {
    const current = await qualityRead('runs', id)
    if (token !== generation || disposed) return
    const value = await qualityWrite('runs', id, 'ai-jobs', { expectedVersion: current.version, idempotencyKey: createClientId(), useKnowledge: useKnowledge.value, reinterpret: regenerate })
    if (token !== generation || disposed) return
    aiId.value = value.id || ''; await loadAI()
  } catch (cause) { if (token === generation && !disposed) aiError.value = cause.message }
  finally { if (token === generation) aiBusy.value = false }
}
async function load() {
  const id = props.run?.id, token = ++generation
  stopAI(); seriesGeneration++; detailGeneration++; findingDialog.value = false; selectedFinding.value = null; reviewHistory.value = []; series.value = null; seriesError.value = ''; seriesLoading.value = false; selectedMetricId.value = ''; loading.value = false; aiBusy.value = false; reviewSaving.value = false
  error.value = ''; metrics.value = []; findings.value = []; evidence.value = []; reviews.value = []; snapshot.value = null; aiJobs.value = []; aiId.value = ''; aiError.value = ''
  if (!id || !props.run.snapshotId) return
  loading.value = true
  try {
    const values = await Promise.all([qualityRead('runs', id, 'snapshot'), qualityAll('runs', id, 'metrics'), qualityAll('runs', id, 'findings'), qualityAll('runs', id, 'evidence'), qualityAll('runs', id, 'reviews')])
    if (token !== generation || disposed) return
    snapshot.value = values[0]; metrics.value = metricRows(values[1].items); findings.value = findingRows(values[2].items); evidence.value = evidenceRows(values[3].items); reviews.value = values[4].items || []
    selectedMetricId.value = visibleMetrics.value[0]?.id || ''; page.value = 1
    await Promise.all([loadSeries(), loadAI()])
  } catch (cause) { if (token === generation && !disposed) { error.value = cause.message; if (cause.status === 403) { metrics.value = []; findings.value = []; evidence.value = []; snapshot.value = null } } }
  finally { if (token === generation) loading.value = false }
}
async function loadSeries() {
  const m = selectedMetric.value, id = props.run?.id, token = ++seriesGeneration
  series.value = null; seriesError.value = ''; seriesLoading.value = false
  if (!m || !id) return
  seriesLoading.value = true
  try {
    const value = await qualityRead('runs', id, 'series', { deviceId: m.deviceId, attributeId: m.attributeId, limit: 2000 })
    if (token === seriesGeneration && !disposed) series.value = value
  } catch (cause) { if (token === seriesGeneration && !disposed) seriesError.value = cause.message }
  finally { if (token === seriesGeneration) seriesLoading.value = false }
}
async function openFinding(row) {
  const token = ++detailGeneration
  selectedFinding.value = row; selectedMetricId.value = row.metricId; findingDialog.value = true; reviewHistory.value = []; reviewError.value = ''
  Object.assign(review, { result: '', explanation: '', correctsId: '', idempotencyKey: createClientId() })
  void loadSeries()
  try { const value = await qualityAll('findings', row.id, 'reviews', { runId: props.run.id }); if (token === detailGeneration && !disposed) reviewHistory.value = value.items || [] }
  catch (cause) { if (token === detailGeneration && !disposed) reviewError.value = cause.message }
}
async function saveReview() {
  if (reviewSaving.value || !selectedFinding.value) return
  const id = selectedFinding.value.id, runId = props.run.id, token = detailGeneration
  reviewSaving.value = true; reviewError.value = ''
  try {
    if (!review.result || !review.explanation.trim()) throw new Error('请选择核实结果并填写实际依据')
    const current = await qualityRead('runs', runId)
    if (token !== detailGeneration || disposed) return
    const value = await qualityWrite('findings', id, 'reviews', { runId, expectedRunVersion: current.version, result: review.result, explanation: review.explanation.trim(), correctsId: review.correctsId, idempotencyKey: review.idempotencyKey })
    if (token !== detailGeneration || disposed) return
    reviews.value = [...reviews.value.filter(row => row.id !== value.id), value]; reviewHistory.value = [...reviewHistory.value, value]; Object.assign(review, { result: '', explanation: '', correctsId: '', idempotencyKey: createClientId() }); UiMessage.success('核实记录已保存')
  } catch (cause) { if (token === detailGeneration && !disposed) reviewError.value = cause.status === 409 ? '任务或核实版本已变化，请重新加载并核对后保存。' : cause.message }
  finally { if (token === detailGeneration) reviewSaving.value = false }
}
async function exportResult() { if (downloading.value) return; downloading.value = true; try { await qualityExport(props.run.id) } catch (cause) { notifyError(cause) } finally { downloading.value = false } }
function raw(row) { if (row.rawMessageId) emit('navigate', 'raw', { deviceId: row.deviceId || selectedMetric.value?.deviceId, messageId: row.rawMessageId }) }
function factLabel(id) { const f = findings.value.find(row => row.id === id); if (f) return findingKinds[f.kind] || '质量发现'; const m = metrics.value.find(row => row.id === id || row.outputId === id); return m ? `${m.attributeId}指标` : evidence.value.some(row => row.id === id) ? '代表来源证据' : id === snapshot.value?.id + '/summary' ? '快照汇总' : '事实依据' }
function selectFact(id) {
  const f = findings.value.find(row => row.id === id)
  if (f) return openFinding(f)
  const m = metrics.value.find(row => row.id === id || row.outputId === id)
  if (m) { selectedMetricId.value = m.id; selectMetric(); resultRoot.value?.querySelector('.quality-ratio-grid')?.scrollIntoView({ block: 'start' }); return }
  const ev = evidence.value.find(row => row.id === id)
  if (ev) {
    const associated = findings.value.find(row => row.evidenceIds?.includes(ev.id))
    if (associated) return openFinding(associated)
    const metric = metrics.value.find(row => row.deviceId === ev.deviceId && (!ev.attributeId || ev.attributeId === row.attributeId))
    detailGeneration++; reviewHistory.value = []; selectedMetricId.value = metric?.id || selectedMetricId.value
    selectedFinding.value = { id: 'reference:' + ev.id, synthetic: true, metricId: selectedMetricId.value, deviceId: ev.deviceId, attributeId: metric?.attributeId, window: metric?.window, evidenceIds: [ev.id], level: 'evidence', explanation: '该记录是固定快照中的代表来源证据。请结合指标、版本与当前来源覆盖核对。' }
    findingDialog.value = true; void loadSeries(); return
  }
  resultRoot.value?.querySelector('.quality-facts-snapshot')?.scrollIntoView({ block: 'start' })
}
function selectMetric() { selectedFinding.value = null; loadSeries() }
function explanation(item) { return typeof item === 'string' ? item : item.explanation || item.text || item.description || item.summary || item.title || '' }
watch(() => [props.run?.id, props.run?.snapshotId], load, { immediate: true })
watch(selectedMetricId, () => { if (selectedFinding.value && selectedFinding.value.metricId !== selectedMetricId.value) selectedFinding.value = null })
watch([productFilter, attributeFilter, kindFilter, reviewFilter], () => { page.value = 1 })
onBeforeUnmount(() => { disposed = true; generation++; seriesGeneration++; detailGeneration++; stopAI() })
</script>
<template>
 <section ref="resultRoot" class="quality-stack quality-results">
  <div class="quality-toolbar"><h3 class="quality-subtitle">固定事实结果</h3><div class="quality-actions"><ui-button size="small" :loading="loading" @click="load"><RefreshCw/>刷新结果</ui-button><ui-button v-if="run?.snapshotId&&can('GET /api/v1/data-quality/runs/:id/export')" size="small" :loading="downloading" @click="exportResult"><Download/>导出报告</ui-button></div></div>
  <ui-alert v-if="error" :title="error" type="error" :closable="false"><ui-button size="small" @click="load">重新读取</ui-button></ui-alert>
  <ui-skeleton v-if="loading&&!metrics.length" :rows="5" animated/>
  <ui-empty v-else-if="!run?.snapshotId" :description="runIsActive(run)?'任务正在形成固定事实，请稍后查看。':'任务没有可用的事实快照，请检查任务状态。'"/>
  <template v-else-if="snapshot">
   <div class="quality-card quality-facts-snapshot"><div class="quality-toolbar"><strong>快照版本 {{snapshot.version}} · 数据截至 {{qualityTime(snapshot.dataCutoff)}}</strong><ui-tag :type="run.status==='PARTIAL'?'warning':'info'">{{stateLabel(run.status)}}</ui-tag></div><p class="quality-meta">已授权设备 {{snapshot.deviceIds?.length || 0}} 个 · 固定输入 {{snapshot.inputHashes?.length || 0}} 块</p><ui-alert v-for="item in snapshot.limitations || []" :key="item" :title="item" type="warning" :closable="false"/><div v-if="snapshot.missingSources?.length" class="quality-meta">缺失来源：{{snapshot.missingSources.join('、')}}</div><div v-if="snapshot.uncomputableMetrics?.length" class="quality-meta">不可计算：{{snapshot.uncomputableMetrics.join('、')}}</div><div class="quality-table-scroll"><table class="quality-native-table"><thead><tr><th>来源</th><th>实际覆盖</th><th>读取时点</th><th>完整性 / 说明</th></tr></thead><tbody><tr v-for="source in snapshot.sources || []" :key="source.source+source.start"><td>{{source.source}}</td><td>{{qualityTime(source.start)}} ～ {{qualityTime(source.end)}}</td><td>{{qualityTime(source.readAt)}}</td><td>{{source.complete?'已覆盖':'部分 / 未覆盖'}} {{source.reason}}</td></tr></tbody></table></div></div>
   <div v-if="visibleMetrics.length" class="quality-stack">
    <ui-select v-model="selectedMetricId" filterable placeholder="选择设备、属性与质量配置段" @change="selectMetric"><ui-option v-for="m in visibleMetrics" :key="m.id" :value="m.id" :label="`${deviceName(m.deviceId)} · ${m.attributeId} · 配置 ${profileVersionLabel(m)} · ${qualityTime(m.window?.start)}`"/></ui-select>
    <div class="quality-ratio-grid"><article v-for="card in ratioCards" :key="card.label"><span class="quality-meta">{{card.label}}</span><strong>{{ratioLabel(card.value)}}</strong><small class="quality-meta">{{card.value?.numerator || 0}} / {{card.value?.denominator || 0}}<template v-if="card.value?.state"> · {{stateLabel(card.value.state)}}</template></small><p v-for="reason in card.value?.reasons || []" :key="reason" class="quality-meta">{{reason}}</p></article></div>
    <div class="quality-card"><h3>有效样本与序列分段</h3><p v-if="selectedProfile" class="quality-meta">固定配置版本 {{selectedProfile.revisionVersion}} · {{selectedProfile.mode==='event'?'事件上报，不建立周期槽':'周期上报'}} · 单位 {{selectedProfile.unit || '无单位'}} · epsilon {{compactNumber(selectedProfile.epsilon)}}</p><p class="quality-meta">主窗口检查 {{selectedMetric.inspectedSamples}} 条 · 类型有效 {{selectedMetric.validSamples}} 条<template v-if="selectedProfile?.mode!=='event'"> · 事件槽 {{selectedMetric.eventCompleteness?.covered || 0}} / {{selectedMetric.eventCompleteness?.expected || 0}} · 同槽重复 {{selectedMetric.eventCompleteness?.duplicates || 0}}</template></p><p class="quality-meta">同时间不比较 {{selectedMetric.sequence?.sameTime || 0}} 对 · 长间隔 {{selectedMetric.sequence?.longGaps || 0}} 处 · 单位 / 协议 / 配置 / 工况切分 {{selectedMetric.sequence?.versionBreaks || 0}} 处</p><p v-if="selectedMetric.sequence?.representationLimitation" class="quality-hint">{{selectedMetric.sequence.representationLimitation}}</p><p v-if="selectedMetric.originalFindingCount!=null" class="quality-meta">该属性共 {{selectedMetric.originalFindingCount}} 条发现，展示 {{selectedMetric.representedFindingCount}} 条 · 代表证据 {{selectedMetric.representedEvidenceCount}} / {{selectedMetric.originalEvidenceCount}} 条。{{selectedMetric.representationPolicy}}</p><p v-for="item in selectedMetric.time?.limitations || []" :key="item" class="quality-hint">{{item}}</p><p v-if="selectedMetric.time?.differenceSeconds" class="quality-meta">接收时间减事件时间：中位数 {{compactNumber(selectedMetric.time.differenceSeconds.median)}} 秒 · 最小 {{compactNumber(selectedMetric.time.differenceSeconds.min)}} 秒 · 最大 {{compactNumber(selectedMetric.time.differenceSeconds.max)}} 秒</p><div class="quality-table-scroll"><table class="quality-native-table"><thead><tr><th>区间</th><th>单位 / 协议 / 配置 / 工况</th><th>有效样本</th><th>中位数 / MAD / P05 / P95</th></tr></thead><tbody><tr v-for="(segment,index) in selectedMetric.sequence?.segments || []" :key="index"><td>{{qualityTime(segment.window.start)}} ～ {{qualityTime(segment.window.end)}}</td><td>{{segment.unit || '无单位'}} · {{segment.protocolVersion || '未知'}} · {{segment.configurationVersion || '未知'}} · {{segment.operatingCondition || '未知工况'}}</td><td>{{segment.sampleCount}}</td><td>{{compactNumber(segment.statistics?.median)}} / {{compactNumber(segment.statistics?.mad)}} / {{compactNumber(segment.statistics?.p05)}} / {{compactNumber(segment.statistics?.p95)}}</td></tr></tbody></table></div></div>
    <div v-if="selectedMetric.parse" class="quality-card"><h3>接入与解析阶段</h3><p class="quality-meta">观察原文 {{selectedMetric.parse.observedRaw}} 条 · 已归档 {{selectedMetric.parse.archived}} 条 · 已尝试解析 {{selectedMetric.parse.attempted}} 条 · 未尝试 {{selectedMetric.parse.notAttempted}} 条</p><p class="quality-meta">最后失败 {{selectedMetric.parse.lastFailed}} 条 · 最后成功 {{selectedMetric.parse.lastSucceeded}} 条 · 最后状态未知 {{selectedMetric.parse.unknownLast}} 条 · 成功标准消息 {{selectedMetric.parse.successfulStandardMessages}} 条</p><p v-for="reason in selectedMetric.parse.reasons || []" :key="reason" class="quality-hint">{{reason}}</p></div><div class="quality-card"><div class="quality-toolbar"><h3>固定输入原值曲线</h3><ui-select v-model="clock" class="quality-clock"><ui-option value="eventAt" label="设备事件时间"/><ui-option value="receivedAt" label="平台接收时间"/><ui-option value="availableAt" label="首次可查询时间"/></ui-select></div><ui-skeleton v-if="seriesLoading" :rows="3" animated/><ui-alert v-else-if="seriesError" :title="seriesError" type="warning" :closable="false"><ui-button size="small" @click="loadSeries">重新读取曲线</ui-button></ui-alert><template v-else><TimeSeriesChart :times="chart.times" :series="chart.series" :time-range="chartRange" :fill="false" unit="short" :height="240" empty-text="当前时间口径没有可绘制样本；原值和未知时间可在曲线数据中核对。"/><p class="quality-hint">{{clock==='eventAt'?'事件时间表示设备声明的历史发生时刻；补报不会补足过去平台接收连续性。':clock==='receivedAt'?'平台接收时间按真实收包时刻展示。':'首次可查询时间来自成功提交记录；未知时间不补成接收时间。'}}</p><p v-if="chart.collapsedCount" class="quality-hint">同一时间戳存在 {{chart.collapsedCount}} 条额外原值，曲线保留按消息标识稳定排序的代表值；全部样本仍用于指标。</p><p v-if="series" class="quality-meta">固定输入 {{series.originalCount || 0}} 条 · 返回 {{series.returnedCount ?? series.items?.length ?? 0}} 条{{series.downsampled?' · 曲线已抽样':''}} · {{series.complete?'当前来源已覆盖':'来源不完整，固定指标仍保留'}}</p><p v-for="item in series?.limitations || []" :key="item" class="quality-hint">{{item}}</p><ui-collapse v-if="series?.items?.length"><ui-collapse-item title="查看曲线数据与三种时间（最多 20 个代表点）" name="samples"><div class="quality-table-scroll"><table class="quality-native-table"><thead><tr><th>原值</th><th>设备事件时间</th><th>平台接收时间</th><th>首次可查询时间</th><th>协议 / 配置</th></tr></thead><tbody><tr v-for="row in series.items.slice(0,20)" :key="row.id"><td>{{typeof row.value==='object'?JSON.stringify(row.value):row.value ?? '—'}}</td><td>{{qualityTime(row.eventAt)}}</td><td>{{qualityTime(row.receivedAt)}}</td><td>{{qualityTime(row.availableAt)}}</td><td>{{row.protocolVersion || '未知'}} · {{row.configurationVersion || '未知'}}</td></tr></tbody></table></div></ui-collapse-item></ui-collapse></template></div>
   </div>
   <ui-empty v-else description="当前快照没有可计算的测点指标，请核对配置与来源覆盖。"/>
   <div class="quality-card quality-stack"><h3>发现与人工核实</h3><div class="quality-filter-row"><ui-select v-if="!deviceId" v-model="productFilter" clearable placeholder="全部产品"><ui-option v-for="id in productOptions" :key="id" :value="id" :label="productName(id)"/></ui-select><ui-select v-model="attributeFilter" clearable placeholder="全部属性"><ui-option v-for="id in attributeOptions" :key="id" :value="id" :label="id"/></ui-select><ui-select v-model="kindFilter" clearable placeholder="全部问题类型"><ui-option v-for="[value,label] in Object.entries(findingKinds)" :key="value" :value="value" :label="label"/></ui-select><ui-select v-model="reviewFilter" clearable placeholder="全部核实状态"><ui-option value="UNREVIEWED" label="尚未核实"/><ui-option v-for="[value,label] in Object.entries(reviewStates)" :key="value" :value="value" :label="label"/></ui-select></div><div class="quality-table-scroll"><ui-table :data="pageFindings" empty-text="本页无质量发现。请同时查看未知维度与资料缺口。"><ui-table-column label="设备 / 属性" min-width="170"><template #default="{row}">{{deviceName(row.deviceId)}}<p class="quality-meta">{{row.attributeId}}</p></template></ui-table-column><ui-table-column label="发现 / 依据" min-width="300"><template #default="{row}"><strong>{{findingKinds[row.kind] || row.kind}}</strong><p class="quality-meta">{{row.explanation}}</p></template></ui-table-column><ui-table-column label="提示 / 核实" min-width="170"><template #default="{row}"><ui-tag :type="stateTone(row.level)">{{row.level==='issue'?'有质量问题':'待核实线索'}}</ui-tag><p class="quality-meta">{{reviewStates[latestReview(row.id)?.result] || '尚未核实'}}</p></template></ui-table-column><ui-table-column label="操作" width="100"><template #default="{row}"><ui-button text size="small" @click="openFinding(row)">依据与核实</ui-button></template></ui-table-column></ui-table></div><ui-pagination :current-page="page" :page-size="20" :total="filteredFindings.length" @current-change="page=$event"/></div>
   <div class="quality-card quality-stack"><div class="quality-toolbar"><h3>AI 解读</h3><div class="quality-actions"><ui-button v-if="showAI" size="small" :loading="aiBusy" :disabled="aiJobs.some(job=>['QUEUED','RUNNING'].includes(job.status))" @click="runAI(!!activeAI)"><Sparkles/>{{activeAI?'重新解读':'开始解读'}}</ui-button></div></div><p class="quality-hint">用户显式启动。解读绑定当前固定快照，事实、人工核实和报告导出独立保存。</p><ui-checkbox v-if="showAI" v-model="useKnowledge">使用本工作流已绑定且获授权的知识</ui-checkbox><ui-alert v-if="aiError" :title="aiError" type="warning" :closable="false"/><ui-select v-if="aiJobs.length>1" v-model="aiId"><ui-option v-for="job in aiJobs" :key="job.id" :value="job.id" :label="`${qualityTime(job.createdAt)} · ${stateLabel(job.status)}`"/></ui-select><template v-if="activeAI"><ui-tag :type="stateTone(activeAI.status)">{{stateLabel(activeAI.status)}}</ui-tag><p v-if="activeAI.error" class="quality-hint">{{activeAI.error}}。固定事实结果仍可核实和导出。</p><p class="quality-meta">快照 {{activeAI.snapshotVersion}} · {{activeAI.model || '模型由工作流配置'}} · 已提供事实 {{activeAI.factIds?.length || 0}} 条</p><p v-if="activeAI.coverage" class="quality-meta">{{activeAI.coverage.summary || activeAI.coverage.description || `AI 实际阅读范围以 ${activeAI.factIds?.length || 0} 条事实及受控分页记录为准。`}}</p><template v-if="activeAI.status==='SUCCEEDED'"><p>{{aiContent.summary}}</p><article v-for="(item,index) in aiContent.interpretations || []" :key="'interpretation'+index" class="quality-card"><strong v-if="item.title">{{item.title}}</strong><p>{{explanation(item)}}</p><div class="quality-tag-list"><ui-button v-for="id in item.factIds || []" :key="id" text size="small" @click="selectFact(id)">{{factLabel(id)}}</ui-button></div></article><h4 v-if="aiContent.suggestedVerification?.length">建议核实步骤</h4><article v-for="(item,index) in aiContent.suggestedVerification || []" :key="'verification'+index"><p>{{index+1}}. {{explanation(item)}}</p><div class="quality-tag-list"><ui-button v-for="id in item.factIds || []" :key="id" text size="small" @click="selectFact(id)">{{factLabel(id)}}</ui-button></div></article><p v-for="(item,index) in aiContent.limitations || []" :key="'limitation'+index" class="quality-hint">{{explanation(item)}}</p></template></template><p v-else class="quality-meta">尚未生成解读。</p></div>
  </template>
 </section>
 <ui-dialog v-model="findingDialog" :title="detailTitle" width="min(1000px,94vw)" :close-on-click-modal="false" @close="findingDialog=false;detailGeneration++"><div class="quality-details-body"><template v-if="selectedFinding"><div class="quality-card"><strong>{{deviceName(selectedFinding.deviceId)}} · {{selectedFinding.attributeId}}</strong><p>{{selectedFinding.explanation}}</p><p class="quality-meta">{{qualityTime(selectedFinding.window?.start)}} 至 {{qualityTime(selectedFinding.window?.end)}} · {{selectedFinding.level==='issue'?'有质量问题':selectedFinding.synthetic?'代表来源证据':'待核实线索'}}</p><p v-if="selectedFinding.threshold!=null" class="quality-meta">阈值 {{compactNumber(selectedFinding.threshold)}} · 代表指标 {{compactNumber(selectedFinding.value)}}</p><p v-if="baseline" class="quality-meta">确认基线：{{baseline.operatingCondition}} · 样本 {{baseline.sampleCount}} 条 · 中位数 {{compactNumber(baseline.median)}} · MAD {{compactNumber(baseline.mad)}} · 确认人 {{baseline.confirmedBy}}</p></div><div class="quality-card"><div class="quality-toolbar"><h3>原值、基线与阈值</h3><ui-select v-model="clock" class="quality-clock"><ui-option value="eventAt" label="设备事件时间"/><ui-option value="receivedAt" label="平台接收时间"/><ui-option value="availableAt" label="首次可查询时间"/></ui-select></div><ui-skeleton v-if="seriesLoading" :rows="3" animated/><ui-alert v-else-if="seriesError" :title="seriesError" type="warning" :closable="false"/><TimeSeriesChart v-else :times="chart.times" :series="chart.series" :time-range="chartRange" :fill="false" unit="short" :height="220" empty-text="来源证据不足或没有数值样本，固定指标仍保留。"/><p v-if="chart.collapsedCount" class="quality-hint">同一时间戳存在多条原值，曲线展示稳定代表值，统计分母保留全部样本。</p><p class="quality-hint">{{series?.downsampled?'曲线采用明确抽样点，统计分母来自全部冻结样本。':'曲线按固定输入身份重新读取；设备事件时间和平台接收时间分别评价。'}}</p><p v-for="item in series?.limitations || []" :key="item" class="quality-hint">{{item}}</p></div><div class="quality-card"><h3>代表证据</h3><p class="quality-hint">代表证据用于核对异常依据，完整原值曲线在固定事实结果中单独读取。</p><div class="quality-table-scroll"><table class="quality-native-table"><thead><tr><th>设备事件时间</th><th>平台接收时间 / 首次可查询</th><th>原值</th><th>协议 / 配置版本</th><th>原文</th></tr></thead><tbody><tr v-for="row in selectedEvidence" :key="row.id"><td>{{qualityTime(row.eventAt || row.occurredAt)}}</td><td>{{qualityTime(row.receivedAt)}}<p class="quality-meta">可查询 {{qualityTime(row.availableAt)}}</p></td><td>{{typeof row.value==='object'?JSON.stringify(row.value):row.value ?? '—'}}</td><td>{{row.protocolVersion || '未知'}}<p class="quality-meta">{{row.configurationVersion || row.resourceVersion || '未知'}}</p></td><td><ui-button v-if="row.rawMessageId&&can('menu:raw')" text size="small" :disabled="['expired','missing','unavailable'].includes(row.availability)" @click="raw(row)">查看原文</ui-button><p class="quality-meta">{{row.availability==='available'?'原文可核对':row.availability==='expired'?'原文证据已过期':row.availability==='available_at_freeze'?'冻结时可用，当前未核实':'原文可用性未确认'}}</p></td></tr><tr v-if="!selectedEvidence.length"><td colspan="5">该发现没有可读取的代表原文证据；固定指标保留，证据不足须另行核实。</td></tr></tbody></table></div></div><div v-if="!selectedFinding.synthetic" class="quality-card quality-stack"><h3>人工核实记录</h3><ui-alert v-if="reviewError" :title="reviewError" type="error" :closable="false"/><article v-for="item in reviewHistory" :key="item.id"><ui-tag type="info">{{reviewStates[item.result] || item.result}}</ui-tag><p>{{item.explanation}}</p><p class="quality-meta">{{item.reviewer}} · {{qualityTime(item.reviewedAt)}}<span v-if="item.correctsId"> · 更正既有记录</span></p></article><p v-if="!reviewHistory.length" class="quality-meta">尚无核实记录。</p><ui-form v-if="can('POST /api/v1/data-quality/findings/:id/reviews')" label-position="top" :disabled="reviewSaving"><ui-form-item label="核实结果" required><ui-select v-model="review.result"><ui-option v-for="[value,label] in Object.entries(reviewStates)" :key="value" :value="value" :label="label"/></ui-select></ui-form-item><ui-form-item v-if="reviewHistory.length" label="更正既有记录（选填）"><ui-select v-model="review.correctsId" clearable><ui-option v-for="item in reviewHistory" :key="item.id" :value="item.id" :label="`${reviewStates[item.result] || item.result} · ${qualityTime(item.reviewedAt)}`"/></ui-select></ui-form-item><ui-form-item label="实际核实依据" required><ui-input v-model="review.explanation" type="textarea" :rows="3" placeholder="记录现场检查、正常工况解释、处理结果或继续观察依据"/></ui-form-item><ui-button type="primary" :loading="reviewSaving" @click="saveReview">保存核实记录</ui-button></ui-form></div></template></div><template #footer><ui-button :disabled="reviewSaving" @click="findingDialog=false">关闭</ui-button></template></ui-dialog>
</template>
<style scoped>.quality-clock{width:190px;max-width:100%}.quality-results p{overflow-wrap:anywhere;white-space:pre-wrap}.quality-results .quality-native-table{min-width:650px}.quality-details-body .quality-native-table{min-width:720px}.quality-results h4{font-size:13px;margin:0}.quality-details-body p{overflow-wrap:anywhere;white-space:pre-wrap}.quality-results .quality-ratio-grid article>small{display:block}@media(max-width:640px){.quality-clock{width:100%}.quality-results .quality-toolbar>.quality-clock{flex-basis:100%}}</style>
