<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { ElMessage } from 'element-plus' /* 引入当前代码需要的依赖。 */
import { api, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { canAcknowledgeAlarm, canCloseAlarm } from '../alarmActions' /* 引入当前代码需要的依赖。 */
import { alarmNavigation, alarmQuery } from '../alarmNavigation' /* 引入当前代码需要的依赖。 */
import { alarmLevel, alarmLevels, alarmSources, alarmStatuses, alarmType, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */

const filters = reactive({ status:'', level:'', deviceId:'' }) /* 声明 filters。 */
const items = ref([]) /* 声明 items。 */
const loading = ref(false) /* 声明 loading。 */
const detail = ref(null) /* 声明 detail。 */
const detailVisible = ref(false) /* 声明 detailVisible。 */
const analysis = ref(null) /* 声明 analysis。 */
const analysisLoading = ref(false) /* 声明 analysisLoading。 */
const analysisProgress = ref(null) /* 声明 analysisProgress。 */
const actionPending = reactive({}) /* 声明 actionPending。 */
const page = ref(1) /* 声明 page。 */
const pageSize = ref(20) /* 声明 pageSize。 */
const total = ref(0) /* 声明 total。 */
let analysisPollTimer = 0 /* 声明 analysisPollTimer。 */
let analysisViewToken = 0 /* 声明 analysisViewToken。 */

const progressPercent = computed(() => Math.max(0, Math.min(100, Number(analysisProgress.value?.progress || 0)))) /* 声明 progressPercent。 */
const progressStatus = computed(() => analysisProgress.value?.status === 'failed' ? 'exception' : analysisProgress.value?.status === 'succeeded' ? 'success' : undefined) /* 声明 progressStatus。 */

async function load(resetPage = false) { /* 定义 load 函数。 */
  if (resetPage) page.value = 1 /* 判断条件并选择处理分支。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const q = alarmQuery(filters, page.value, pageSize.value) /* 声明 q。 */
    const d = await api('/api/v1/alarms?' + q) /* 声明 d。 */
    items.value = d.items || [] /* 更新 items.value 的值。 */
    total.value = Number(d.total ?? d.count ?? items.value.length) /* 更新 total.value 的值。 */
  } catch (e) { /* 结束当前表达式或代码块。 */
    notifyError(e) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function changePage(value) { page.value = value; load() } /* 定义 changePage 函数。 */
function changePageSize(value) { pageSize.value = value; page.value = 1; load() } /* 定义 changePageSize 函数。 */

function stopAnalysisPolling() { /* 定义 stopAnalysisPolling 函数。 */
  if (analysisPollTimer) window.clearTimeout(analysisPollTimer) /* 判断条件并选择处理分支。 */
  analysisPollTimer = 0 /* 更新 analysisPollTimer 的值。 */
} /* 结束当前表达式或代码块。 */

function handleDetailClosed() { /* 定义 handleDetailClosed 函数。 */
  analysisViewToken += 1 /* 更新 analysisViewToken 的值。 */
  stopAnalysisPolling() /* 执行当前语句并推进处理流程。 */
  analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
  analysisProgress.value = null /* 更新 analysisProgress.value 的值。 */
} /* 结束当前表达式或代码块。 */

function formatRemaining(ms) { /* 定义 formatRemaining 函数。 */
  const seconds = Math.ceil(Number(ms || 0) / 1000) /* 声明 seconds。 */
  if (seconds <= 0) return '即将完成' /* 判断条件并选择处理分支。 */
  if (seconds < 60) return `预计还需约 ${seconds} 秒` /* 判断条件并选择处理分支。 */
  return `预计还需约 ${Math.ceil(seconds / 60)} 分钟` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function pollAnalysis(jobId, alarmId, viewToken = analysisViewToken) { /* 定义 pollAnalysis 函数。 */
  if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const progress = await api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(alarmId)}/progress/${encodeURIComponent(jobId)}`) /* 声明 progress。 */
    if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
    analysisProgress.value = progress /* 更新 analysisProgress.value 的值。 */
    if (progress.status === 'succeeded') { /* 判断条件并选择处理分支。 */
      analysis.value = progress.analysis || null /* 更新 analysis.value 的值。 */
      analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
      ElMessage.success('智能研判已完成') /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    if (progress.status === 'failed') { /* 判断条件并选择处理分支。 */
      analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
      notifyError(progress.error || '智能研判失败') /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    analysisPollTimer = window.setTimeout(() => { void pollAnalysis(jobId, alarmId, viewToken) }, 800) /* 更新 analysisPollTimer 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
    analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
    stopAnalysisPolling() /* 执行当前语句并推进处理流程。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function show(id) { /* 定义 show 函数。 */
  const viewToken = ++analysisViewToken /* 声明 viewToken。 */
  stopAnalysisPolling() /* 执行当前语句并推进处理流程。 */
  analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
  analysisProgress.value = null /* 更新 analysisProgress.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    detail.value = await api(`/api/v1/alarms/${encodeURIComponent(id)}`) /* 更新 detail.value 的值。 */
    analysis.value = null /* 更新 analysis.value 的值。 */
    detailVisible.value = true /* 更新 detailVisible.value 的值。 */
    const [savedResult, progressResult] = await Promise.allSettled([ /* 执行当前语句并推进处理流程。 */
      api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(id)}`), /* 执行当前语句并推进处理流程。 */
      api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(id)}/progress`) /* 执行当前语句并推进处理流程。 */
    ]) /* 结束当前表达式或代码块。 */
    if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
    analysis.value = savedResult.status === 'fulfilled' ? savedResult.value : null /* 更新 analysis.value 的值。 */
    if (progressResult.status !== 'fulfilled') return /* 判断条件并选择处理分支。 */
    analysisProgress.value = progressResult.value /* 更新 analysisProgress.value 的值。 */
    if (progressResult.value.status === 'running') { /* 判断条件并选择处理分支。 */
      analysisLoading.value = true /* 更新 analysisLoading.value 的值。 */
      void pollAnalysis(progressResult.value.jobId, id, viewToken) /* 执行当前语句并推进处理流程。 */
    } else if (progressResult.value.status === 'succeeded') { /* 结束当前表达式或代码块。 */
      analysis.value = progressResult.value.analysis || analysis.value /* 更新 analysis.value 的值。 */
      analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
    } else { /* 结束当前表达式或代码块。 */
      analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
    } /* 结束当前表达式或代码块。 */
  } catch (e) { /* 结束当前表达式或代码块。 */
    notifyError(e) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function runAnalysis() { /* 定义 runAnalysis 函数。 */
  if (!detail.value || analysisLoading.value) return /* 判断条件并选择处理分支。 */
  const viewToken = analysisViewToken /* 声明 viewToken。 */
  stopAnalysisPolling() /* 执行当前语句并推进处理流程。 */
  analysisLoading.value = true /* 更新 analysisLoading.value 的值。 */
  analysisProgress.value = { status:'running', stage:'preparing', message:'正在准备告警上下文', progress:5, estimatedRemainingMs:45000 } /* 更新 analysisProgress.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const job = await api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(detail.value.alarmId)}/run`, { method:'POST', body:'{}' }) /* 声明 job。 */
    if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
    analysisProgress.value = job /* 更新 analysisProgress.value 的值。 */
    if (job.status === 'succeeded') { /* 判断条件并选择处理分支。 */
      analysis.value = job.analysis || null /* 更新 analysis.value 的值。 */
      analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
      ElMessage.success('智能研判已完成') /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    await pollAnalysis(job.jobId, detail.value.alarmId, viewToken) /* 等待异步操作完成。 */
  } catch (e) { /* 结束当前表达式或代码块。 */
    if (viewToken !== analysisViewToken || !detailVisible.value) return /* 判断条件并选择处理分支。 */
    analysisLoading.value = false /* 更新 analysisLoading.value 的值。 */
    notifyError(e) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function action(id, value) { /* 定义 action 函数。 */
  if (actionPending[id]) return /* 判断条件并选择处理分支。 */
  actionPending[id] = value /* 更新 actionPending[id] 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const updated = await api(`/api/v1/alarms/${encodeURIComponent(id)}/actions`, { method:'POST', body:JSON.stringify({ action:value }) }) /* 声明 updated。 */
    const row = items.value.find(item => item.alarmId === id) /* 声明 row。 */
    if (row && updated?.status) row.status = updated.status /* 判断条件并选择处理分支。 */
    ElMessage.success('操作成功') /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (e) { /* 结束当前表达式或代码块。 */
    notifyError(e) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    delete actionPending[id] /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

let realtimeTimer = 0 /* 声明 realtimeTimer。 */
const realtime = event => { /* 声明 realtime。 */
  if (!event?.detail?.topic?.includes('/alarm/') || realtimeTimer) return /* 判断条件并选择处理分支。 */
  realtimeTimer = window.setTimeout(() => { realtimeTimer = 0; void load() }, 300) /* 更新 realtimeTimer 的值。 */
} /* 结束当前表达式或代码块。 */
onMounted(async () => { /* 执行当前语句并推进处理流程。 */
  const navigation = alarmNavigation(sessionStorage.getItem('iot:navigation-detail')) /* 声明 navigation。 */
  sessionStorage.removeItem('iot:navigation-detail') /* 执行当前语句并推进处理流程。 */
  filters.deviceId = navigation.deviceId /* 更新 filters.deviceId 的值。 */
  window.addEventListener('iot:realtime', realtime) /* 执行当前语句并推进处理流程。 */
  await load() /* 等待异步操作完成。 */
  if (navigation.alarmId) await show(navigation.alarmId) /* 判断条件并选择处理分支。 */
}) /* 结束当前表达式或代码块。 */
onBeforeUnmount(() => { /* 执行当前语句并推进处理流程。 */
  analysisViewToken += 1 /* 更新 analysisViewToken 的值。 */
  stopAnalysisPolling() /* 执行当前语句并推进处理流程。 */
  window.clearTimeout(realtimeTimer) /* 执行当前语句并推进处理流程。 */
  window.removeEventListener('iot:realtime', realtime) /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
</script>

<template>
  <div class="page-toolbar"> <!-- 渲染 div 界面元素。 -->
    <el-input v-model="filters.deviceId" clearable placeholder="按设备标识筛选" aria-label="设备标识筛选" @keyup.enter="load(true)" @clear="load(true)" /> <!-- 渲染 el-input 界面元素。 -->
    <el-select v-model="filters.status" clearable placeholder="全部状态" @change="load(true)"><el-option v-for="(text,key) in alarmStatuses" :key="key" :label="text" :value="key" /></el-select> <!-- 渲染 el-select 界面元素。 -->
    <el-select v-model="filters.level" clearable placeholder="全部等级" @change="load(true)"><el-option v-for="(text,key) in alarmLevels" :key="key" :label="text" :value="key" /></el-select> <!-- 渲染 el-select 界面元素。 -->
    <el-button type="primary" :loading="loading" @click="load()">刷新告警</el-button> <!-- 渲染 el-button 界面元素。 -->
    <el-button :disabled="!filters.status && !filters.level && !filters.deviceId" @click="filters.status = ''; filters.level = ''; filters.deviceId = ''; load(true)">重置筛选</el-button> <!-- 渲染 el-button 界面元素。 -->
    <span>共 {{total}} 条告警</span> <!-- 渲染 span 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->

  <el-card shadow="never" class="surface-card table-card"> <!-- 渲染 el-card 界面元素。 -->
    <el-table v-loading="loading" :data="items" stripe> <!-- 渲染 el-table 界面元素。 -->
      <el-table-column label="时间" min-width="170"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      <el-table-column label="设备 / 来源" min-width="190"><template #default="{row}"><b>{{row.deviceName||row.deviceId}}</b><small v-if="row.componentId" class="subline">{{row.componentName||row.componentId}} · {{row.componentLocation||row.componentId}}</small><small class="subline">{{label(alarmSources,row.source,'其他来源')}}</small></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      <el-table-column label="告警类型" min-width="150"><template #default="{row}">{{alarmType(row.alarmType)}}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      <el-table-column label="等级" width="90"><template #default="{row}"><el-tag :type="tagType(row.alarmLevel)" round>{{label(alarmLevels,row.alarmLevel,'未设置')}}</el-tag></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      <el-table-column label="状态" width="100"><template #default="{row}"><el-tag :type="tagType(row.status)" round>{{label(alarmStatuses,row.status)}}</el-tag></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      <el-table-column label="操作" fixed="right" width="310" align="center"><template #default="{row}"><div class="table-actions"><el-button plain type="primary" @click="show(row.alarmId)">查看详情</el-button><template v-if="['ACTIVE','ACKED'].includes(row.status)"><el-button v-permission="'POST /api/v1/alarms/:id/actions'" v-if="canAcknowledgeAlarm(row.status)" :loading="actionPending[row.alarmId] === 'ACKED'" :disabled="Boolean(actionPending[row.alarmId])" plain type="warning" @click="action(row.alarmId,'ACKED')">确认告警</el-button><el-button v-permission="'POST /api/v1/alarms/:id/actions'" v-if="canCloseAlarm(row.status)" :loading="actionPending[row.alarmId] === 'CLOSED'" :disabled="Boolean(actionPending[row.alarmId])" plain type="danger" @click="action(row.alarmId,'CLOSED')">关闭告警</el-button></template></div></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
    </el-table> <!-- 结束当前界面区域。 -->
    <div class="list-pagination"><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20,50,100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /></div> <!-- 渲染 div 界面元素。 -->
  </el-card> <!-- 结束当前界面区域。 -->

  <el-dialog v-model="detailVisible" title="告警详情" width="min(760px, 94vw)" @closed="handleDetailClosed"> <!-- 渲染 el-dialog 界面元素。 -->
    <el-descriptions v-if="detail" :column="1" border> <!-- 渲染 el-descriptions 界面元素。 -->
      <el-descriptions-item label="告警编号">{{detail.alarmId}}</el-descriptions-item><el-descriptions-item label="设备">{{detail.deviceName||detail.deviceId}}</el-descriptions-item><el-descriptions-item v-if="detail.componentId" label="部件">{{detail.componentName||detail.componentId}}（{{detail.componentId}}）</el-descriptions-item><el-descriptions-item v-if="detail.componentLocation" label="部件位置">{{detail.componentLocation}}</el-descriptions-item><el-descriptions-item label="告警类型">{{alarmType(detail.alarmType)}}</el-descriptions-item><el-descriptions-item label="等级 / 状态"><el-tag :type="tagType(detail.alarmLevel)">{{label(alarmLevels,detail.alarmLevel)}}</el-tag> {{label(alarmStatuses,detail.status)}}</el-descriptions-item><el-descriptions-item label="来源">{{label(alarmSources,detail.source,'其他来源')}}</el-descriptions-item><el-descriptions-item label="首次发生">{{formatTime(detail.firstTriggeredAt)}}</el-descriptions-item><el-descriptions-item label="最后发生">{{formatTime(detail.lastTriggeredAt)}}</el-descriptions-item><el-descriptions-item label="触发次数">{{detail.triggerCount}}</el-descriptions-item> <!-- 渲染 el-descriptions-item 界面元素。 -->
    </el-descriptions> <!-- 结束当前界面区域。 -->
    <el-card shadow="never" class="top-gap"> <!-- 渲染 el-card 界面元素。 -->
      <template #header><div class="card-header"><strong>智能自动研判</strong><el-button v-permission="'POST /api/v1/ai/alarm-analysis'" size="small" type="primary" :loading="analysisLoading" :disabled="analysisLoading" @click="runAnalysis">{{analysisLoading ? '研判中…' : analysis ? '重新研判' : '立即研判'}}</el-button></div></template>
      <div v-if="analysisProgress" class="analysis-progress" aria-live="polite"> <!-- 渲染 div 界面元素。 -->
        <div class="analysis-progress-heading"><strong>{{analysisProgress.message || '智能正在处理'}}</strong><span>{{progressPercent}}%</span></div> <!-- 渲染 div 界面元素。 -->
        <el-progress :percentage="progressPercent" :status="progressStatus" :stroke-width="10" /> <!-- 渲染 el-progress 界面元素。 -->
        <small v-if="analysisProgress.status === 'running'">{{formatRemaining(analysisProgress.estimatedRemainingMs)}}</small> <!-- 渲染 small 界面元素。 -->
        <small v-else>{{analysisProgress.status === 'succeeded' ? '处理完成' : analysisProgress.error || '处理失败'}}</small> <!-- 渲染 small 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <el-empty v-if="!analysis && !analysisLoading" description="该告警暂无研判结果，可点击立即研判" :image-size="52" /> <!-- 渲染 el-empty 界面元素。 -->
      <div v-if="analysis" class="analysis-grid"><el-alert :title="analysis.summary||'智能未返回摘要'" :type="tagType(analysis.riskLevel)==='danger'?'error':'warning'" :closable="false" show-icon /><div><strong>风险等级：</strong>{{alarmLevel(analysis.riskLevel)}} <span class="subline">置信度 {{Number(analysis.confidence||0).toFixed(2)}}</span></div><div v-if="analysis.possibleReasons?.length"><strong>可能原因</strong><ul><li v-for="item in analysis.possibleReasons" :key="item">{{item}}</li></ul></div><div v-if="analysis.suggestions?.length"><strong>建议处置</strong><ul><li v-for="item in analysis.suggestions" :key="item">{{item}}</li></ul></div><small class="subline">模型：{{analysis.model||'—'}} · 生成时间：{{formatTime(analysis.createdAt)}}</small></div> <!-- 渲染 div 界面元素。 -->
    </el-card> <!-- 结束当前界面区域。 -->
    <pre>{{pretty(detail)}}</pre> <!-- 渲染 pre 界面元素。 -->
    <template #footer><el-button @click="detailVisible = false">关闭详情</el-button></template>
  </el-dialog> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.analysis-progress { margin: 12px 0; padding: 12px; background: #f5f9ff; border: 1px solid #d6e8ff; border-radius: 5px; } /* 定义当前元素的样式规则。 */
.analysis-progress-heading { display:flex; justify-content:space-between; gap:12px; margin-bottom:7px; color:#1554ad; font-size:13px; } /* 定义当前元素的样式规则。 */
.analysis-progress-heading span { color:#1677ff; font-weight:700; } /* 定义当前元素的样式规则。 */
.analysis-progress small { display:block; margin-top:7px; color:#697386; font-size:12px; } /* 定义当前元素的样式规则。 */
</style>
