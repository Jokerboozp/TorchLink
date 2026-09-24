<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, session } from '../api' /* 引入当前代码需要的依赖。 */
import { businessStatuses, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */
import MarkdownContent from '../components/MarkdownContent.vue' /* 引入当前代码需要的依赖。 */
import { loadHealthInspection, saveHealthInspection } from '../healthInspectionState' /* 引入当前代码需要的依赖。 */

const inspectionStorage = typeof window !== 'undefined' ? window.sessionStorage : null /* 声明 inspectionStorage。 */
const report = ref(loadHealthInspection(inspectionStorage, session)) /* 声明 report。 */
const progress = ref(null) /* 声明 progress。 */
const loading = ref(false) /* 声明 loading。 */
const downloading = ref(false) /* 声明 downloading。 */
const error = ref('') /* 声明 error。 */
const progressPercentage = computed(() => Math.max(0, Math.min(100, Number(progress.value?.progress || 0)))) /* 声明 progressPercentage。 */
const progressStatus = computed(() => progress.value?.status === 'failed' ? 'exception' : progress.value?.status === 'succeeded' ? 'success' : undefined) /* 声明 progressStatus。 */
const inspectionRunning = computed(() => loading.value || progress.value?.status === 'running') /* 声明 inspectionRunning。 */
const counts = computed(() => report.value?.counts || {}) /* 声明 counts。 */
let progressTimer = 0 /* 声明 progressTimer。 */
let viewToken = 0 /* 声明 viewToken。 */

function stopProgressPolling() { /* 定义 stopProgressPolling 函数。 */
  if (progressTimer) clearTimeout(progressTimer) /* 判断条件并选择处理分支。 */
  progressTimer = 0 /* 更新 progressTimer 的值。 */
} /* 结束当前表达式或代码块。 */

function scheduleProgressPolling(token) { /* 定义 scheduleProgressPolling 函数。 */
  stopProgressPolling() /* 执行当前语句并推进处理流程。 */
  progressTimer = setTimeout(() => pollProgress(token, true), 1000) /* 更新 progressTimer 的值。 */
} /* 结束当前表达式或代码块。 */

function formatRemaining(value) { /* 定义 formatRemaining 函数。 */
  const milliseconds = Number(value || 0) /* 声明 milliseconds。 */
  if (milliseconds <= 0) return '即将完成' /* 判断条件并选择处理分支。 */
  const seconds = Math.ceil(milliseconds / 1000) /* 声明 seconds。 */
  if (seconds < 60) return `约 ${seconds} 秒` /* 判断条件并选择处理分支。 */
  const minutes = Math.floor(seconds / 60) /* 声明 minutes。 */
  const remainder = seconds % 60 /* 声明 remainder。 */
  return remainder ? `约 ${minutes} 分 ${remainder} 秒` : `约 ${minutes} 分钟` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function handleProgress(value, token, announce = false) { /* 定义 handleProgress 函数。 */
  if (token !== viewToken || !value) return /* 判断条件并选择处理分支。 */
  const previous = progress.value /* 声明 previous。 */
  progress.value = value /* 更新 progress.value 的值。 */
  if (value.status === 'running') { /* 判断条件并选择处理分支。 */
    error.value = '' /* 更新 error.value 的值。 */
    loading.value = true /* 更新 loading.value 的值。 */
    scheduleProgressPolling(token) /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  loading.value = false /* 更新 loading.value 的值。 */
  stopProgressPolling() /* 执行当前语句并推进处理流程。 */
  if (value.status === 'succeeded' && value.report) { /* 判断条件并选择处理分支。 */
    report.value = value.report /* 更新 report.value 的值。 */
    saveHealthInspection(inspectionStorage, session, value.report) /* 执行当前语句并推进处理流程。 */
    if (announce && previous?.status === 'running') UiMessage.success('设备健康巡检已完成') /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  if (value.status === 'failed') { /* 判断条件并选择处理分支。 */
    error.value = value.error || '智能巡检失败，请稍后重试' /* 更新 error.value 的值。 */
    if (announce) notifyError(new Error(error.value)) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function pollProgress(token = viewToken, announce = true) { /* 定义 pollProgress 函数。 */
  if (token !== viewToken) return /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = await api('/api/v1/ai/health-inspection/progress') /* 声明 value。 */
    handleProgress(value, token, announce) /* 执行当前语句并推进处理流程。 */
  } catch (exception) { /* 结束当前表达式或代码块。 */
    if (token !== viewToken) return /* 判断条件并选择处理分支。 */
    stopProgressPolling() /* 执行当前语句并推进处理流程。 */
    loading.value = false /* 更新 loading.value 的值。 */
    if (exception?.status === 404) { /* 判断条件并选择处理分支。 */
      progress.value = null /* 更新 progress.value 的值。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    error.value = exception.message || '智能巡检进度读取失败' /* 更新 error.value 的值。 */
    if (progress.value?.status === 'running') { /* 判断条件并选择处理分支。 */
      loading.value = true /* 更新 loading.value 的值。 */
      scheduleProgressPolling(token) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function loadProgress() { /* 定义 loadProgress 函数。 */
  error.value = '' /* 更新 error.value 的值。 */
  await pollProgress(viewToken, false) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

async function run() { /* 定义 run 函数。 */
  if (inspectionRunning.value) return /* 判断条件并选择处理分支。 */
  error.value = '' /* 更新 error.value 的值。 */
  progress.value = null /* 更新 progress.value 的值。 */
  loading.value = true /* 更新 loading.value 的值。 */
  stopProgressPolling() /* 执行当前语句并推进处理流程。 */
  const token = viewToken /* 声明 token。 */
  try { /* 执行当前语句并推进处理流程。 */
    const job = await api('/api/v1/ai/health-inspection/run', { method:'POST', body:'{}' }) /* 声明 job。 */
    handleProgress(job, token) /* 执行当前语句并推进处理流程。 */
  } catch (exception) { /* 结束当前表达式或代码块。 */
    if (token !== viewToken) return /* 判断条件并选择处理分支。 */
    loading.value = false /* 更新 loading.value 的值。 */
    error.value = exception.message || '智能巡检启动失败' /* 更新 error.value 的值。 */
    notifyError(exception) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function downloadPDF() { /* 定义 downloadPDF 函数。 */
  if (!report.value || downloading.value || inspectionRunning.value) return /* 判断条件并选择处理分支。 */
  downloading.value = true /* 更新 downloading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const stamp = new Date(Number(report.value.generatedAt || Date.now())).toISOString().replace(/[:.]/g, '-') /* 声明 stamp。 */
    await download('/api/v1/ai/health-inspection/pdf', `智能巡检结果_${stamp}.pdf`, { method:'POST', body:'{}' }) /* 等待异步操作完成。 */
    UiMessage.success('巡检结果文档已下载') /* 执行当前语句并推进处理流程。 */
  } catch (exception) { /* 结束当前表达式或代码块。 */
    notifyError(exception) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    downloading.value = false /* 更新 downloading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(loadProgress) /* 执行当前语句并推进处理流程。 */
onBeforeUnmount(() => { /* 执行当前语句并推进处理流程。 */
  viewToken += 1 /* 更新 viewToken 的值。 */
  stopProgressPolling() /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
</script>

<template>
  <div class="inspection-page"> <!-- 渲染 div 界面元素。 -->
    <header class="inspection-hero">
      <div class="inspection-hero-copy"><h2>设备健康巡检</h2><p>汇总设备运行情况，生成需要关注的设备与处置建议。</p><div class="inspection-scope"><span>在线状态</span><span>数据新鲜度</span><span>活动告警</span></div></div>
      <div class="inspection-hero-side"><small>手动启动检查，不会执行设备控制</small><div class="inspection-hero-actions"><ui-button v-permission="'POST /api/v1/ai/health-inspection/pdf'" v-if="report" :disabled="inspectionRunning" :loading="downloading" @click="downloadPDF">下载文档</ui-button><ui-button v-permission="'POST /api/v1/ai/health-inspection/run'" type="primary" :disabled="inspectionRunning" :loading="loading" @click="run">{{ inspectionRunning ? '巡检进行中' : '立即巡检' }}</ui-button></div></div>
    </header>

    <ui-alert v-if="error" class="top-gap" :title="error" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->

    <ui-card v-if="progress" shadow="never" class="surface-card inspection-progress top-gap"> <!-- 渲染 ui-card 界面元素。 -->
      <div class="inspection-progress-heading"><div><strong>{{ progress.message }}</strong><small v-if="progress.status === 'running'">任务在后台继续执行，切换页面后会自动恢复</small><small v-else-if="progress.status === 'succeeded'">本次巡检结果已保存，可下载文档</small></div><strong>{{ progressPercentage }}%</strong></div> <!-- 渲染 div 界面元素。 -->
      <ui-progress :percentage="progressPercentage" :status="progressStatus" :stroke-width="10" :show-text="false" /> <!-- 渲染 ui-progress 界面元素。 -->
      <div class="inspection-progress-meta"><span v-if="progress.status === 'running'">预计剩余 {{ formatRemaining(progress.estimatedRemainingMs) }}</span><span v-else-if="progress.status === 'failed'">请检查模型服务和设备数据后重试</span><span v-else>已完成</span><span v-if="progress.updatedAt">更新时间：{{ formatTime(progress.updatedAt) }}</span></div> <!-- 渲染 div 界面元素。 -->
    </ui-card> <!-- 结束当前界面区域。 -->

    <ui-card shadow="never" class="surface-card top-gap"> <!-- 渲染 ui-card 界面元素。 -->
      <ui-skeleton v-if="loading && !report" :rows="5" animated /> <!-- 渲染 ui-skeleton 界面元素。 -->
      <template v-else-if="report">
        <ui-alert :title="report.summary" type="info" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
        <div class="inspection-counts top-gap"><div><span>设备总数</span><strong>{{ counts.total || 0 }}</strong></div><div class="healthy"><span>状态正常</span><strong>{{ counts.healthy || 0 }}</strong></div><div class="attention"><span>需关注</span><strong>{{ counts.attention || 0 }}</strong></div><div class="critical"><span>高风险</span><strong>{{ counts.critical || 0 }}</strong></div><div class="offline"><span>离线/疑似离线</span><strong>{{ counts.offline || 0 }}</strong></div><div><span>活动告警</span><strong>{{ counts.activeAlarms || 0 }}</strong></div></div> <!-- 渲染 div 界面元素。 -->
        <ui-card v-if="report.aiAdvice" shadow="never" class="inner-card top-gap"><template #header><strong>智能巡检建议</strong></template><MarkdownContent class="report-text" :source="report.aiAdvice" /></ui-card> <!-- 渲染 ui-card 界面元素。 -->
        <ui-alert v-for="warning in report.warnings || []" :key="warning" class="top-gap" :title="warning" type="warning" :closable="false" /> <!-- 渲染 ui-alert 界面元素。 -->
        <ui-table class="top-gap" :data="report.items || []" stripe><ui-table-column label="设备" min-width="190"><template #default="{row}"><b>{{ row.deviceName || row.deviceId }}</b><small class="subline">{{ row.deviceId }} · {{ row.productId }}</small></template></ui-table-column><ui-table-column label="业务状态" width="130"><template #default="{row}"><ui-tag :type="tagType(row.businessStatus)" round>{{ label(businessStatuses, row.businessStatus, row.businessStatus) }}</ui-tag></template></ui-table-column><ui-table-column label="最近上报" min-width="170"><template #default="{row}">{{ formatTime(row.lastSeenAt) }}</template></ui-table-column><ui-table-column label="活动告警" width="100"><template #default="{row}">{{ row.activeAlarmCount }}</template></ui-table-column><ui-table-column label="巡检结论" min-width="280"><template #default="{row}"><ui-tag :type="tagType(row.severity)" size="small" round>{{ row.severity }}</ui-tag><span class="inspection-findings">{{ (row.findings || []).join('；') }}</span></template></ui-table-column></ui-table> <!-- 渲染 ui-table 界面元素。 -->
      </template>
      <div v-else class="inspection-empty"><strong>尚无巡检结果</strong><p>点击上方“立即巡检”开始检查，结果会显示在这里。</p></div>
    </ui-card>
  </div>
</template>

<style scoped>
.inspection-page { min-width:0; }
.inspection-hero { box-sizing:border-box; width:100%; min-width:0; padding:19px 22px; display:flex; align-items:center; justify-content:space-between; gap:24px; background:#fff; border:1px solid #e1e9f2; border-left:4px solid #1554ad; border-radius:10px; }
.inspection-hero-copy { min-width:0; }
.inspection-hero-copy h2 { margin:0; color:#1f2d40; font-size:17px; line-height:1.4; }
.inspection-hero-copy p { margin:5px 0 10px; color:#52657d; font-size:13px; line-height:1.6; }
.inspection-scope { display:flex; flex-wrap:wrap; gap:7px; }
.inspection-scope span { padding:3px 9px; color:#1554ad; background:#edf4ff; border-radius:5px; font-size:12px; }
.inspection-hero-side { flex:none; display:grid; justify-items:end; gap:10px; }
.inspection-hero-side>small { color:#607086; font-size:12px; }
.inspection-hero-actions { display:flex; align-items:center; gap:8px; }
.inspection-hero-actions :deep(.n-button) { min-width:112px; }
.inspection-hero-actions :deep(.n-button--primary-type) { min-width:132px; height:38px; }
.inspection-empty { min-height:122px; display:grid; align-content:center; justify-items:center; gap:6px; text-align:center; }
.inspection-empty strong { color:#24364d; font-size:14px; }
.inspection-empty p { margin:0; color:#607086; font-size:13px; line-height:1.5; }
.inspection-progress { border-color:#d6e8ff; background:#f8fbff; } /* 定义当前元素的样式规则。 */
.inspection-progress-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:12px; } /* 定义当前元素的样式规则。 */
.inspection-progress-heading>div { display:grid; gap:4px; } /* 定义当前元素的样式规则。 */
.inspection-progress-heading strong { color:#1554ad; font-size:14px; } /* 定义当前元素的样式规则。 */
.inspection-progress-heading>strong { font-size:20px; } /* 定义当前元素的样式规则。 */
.inspection-progress-heading small,.inspection-progress-meta { color:#64748b; font-size:12px; line-height:1.5; } /* 定义当前元素的样式规则。 */
.inspection-progress-meta { display:flex; justify-content:space-between; gap:12px; margin-top:8px; } /* 定义当前元素的样式规则。 */
.inspection-findings { display:inline-block; margin-left:8px; color:#646c73; line-height:1.6; } /* 定义当前元素的样式规则。 */
@media (max-width:760px) { .inspection-hero { align-items:stretch; flex-direction:column; gap:18px; padding:17px; }.inspection-hero-side { justify-items:stretch; }.inspection-hero-actions :deep(.n-button) { flex:1; min-width:0; } }
@media (max-width:560px) { .inspection-progress-meta { display:grid; gap:2px; } } /* 按屏幕条件调整样式。 */
</style>
