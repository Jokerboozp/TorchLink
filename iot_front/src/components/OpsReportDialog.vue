<script setup>
// 运维报告：由 ops-assistant 工作流按所选时段汇总授权范围内的设备状态与告警。
// 业务运行可能要等待 Harness 空位，关闭弹窗会取消请求。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import { copyText } from '../clipboard'
import { UiMessage } from '../ui/feedback.js'
import MarkdownContent from './MarkdownContent.vue'

const visible = defineModel({ type: Boolean, default: false })
const periods = [
  { key: 'today', label: '今日' },
  { key: 'week', label: '近 7 天', days: 7 },
  { key: 'month', label: '近 30 天', days: 30 }
]
const period = ref('today')
const loading = ref(false)
const report = ref('')
const error = ref('')
const selected = computed(() => periods.find(item => item.key === period.value) || periods[0])
let controller = null

function range(item, now = Date.now()) {
  if (!item.days) {
    const start = new Date(now)
    start.setHours(0, 0, 0, 0)
    return { start: start.getTime(), end: now }
  }
  return { start: now - item.days * 24 * 60 * 60 * 1000, end: now }
}

async function generate() {
  if (loading.value) return
  controller = new AbortController()
  loading.value = true
  error.value = ''
  report.value = ''
  try {
    const result = await api('/api/v1/ai/reports', {
      method: 'POST',
      body: JSON.stringify({ period: selected.value.label, ...range(selected.value) }),
      signal: controller.signal
    })
    report.value = result.report || ''
    if (!report.value) error.value = '模型没有返回报告内容，请稍后重试'
  } catch (e) {
    if (e?.name !== 'AbortError') error.value = e?.message || '运维报告生成失败'
  } finally {
    loading.value = false
    controller = null
  }
}

async function copy() {
  if (await copyText(report.value)) UiMessage.success('报告已复制')
  else UiMessage.warning('浏览器不允许复制，请手动选择文本')
}

watch(visible, open => {
  if (open) return
  controller?.abort()
})
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <ui-dialog v-model="visible" title="运维报告" width="min(760px, 94vw)" destroy-on-close>
    <div class="ops-report-toolbar">
      <ui-radio-group v-model="period" class="segmented-choice-group" aria-label="报告时段" :disabled="loading">
        <ui-radio-button v-for="item in periods" :key="item.key" :value="item.key">{{ item.label }}</ui-radio-button>
      </ui-radio-group>
      <ui-button v-permission="'POST /api/v1/ai/reports'" type="primary" :loading="loading" @click="generate">
        {{ report ? '重新生成' : '生成报告' }}
      </ui-button>
    </div>
    <p class="ops-report-note">汇总当前账户有权查看的设备状态与告警；仅供辅助判断，处置仍以现场核实为准。</p>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" />
    <p v-else-if="loading" class="ops-report-note" role="status">正在生成报告，模型服务繁忙时可能需要几分钟…</p>
    <MarkdownContent v-else-if="report" class="ops-report-content" :source="report" />
    <ui-empty v-else description="选择时段后生成报告" />
    <template #footer>
      <ui-button @click="visible = false">关闭</ui-button>
      <ui-button :disabled="!report" @click="copy">复制报告</ui-button>
    </template>
  </ui-dialog>
</template>

<style scoped>
.ops-report-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.ops-report-note {
  margin: var(--space-3) 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.ops-report-content {
  max-height: 60vh;
  overflow: auto;
}
</style>
