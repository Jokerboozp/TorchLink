<script setup>
// 告警详情中的通知记录：各级通知的渠道、接收人、发送状态与失败原因。
import { onBeforeUnmount, ref, watch } from 'vue'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { taskStatuses, taskTone } from '../notifications'
import StatusDot from './layout/StatusDot.vue'
import { formatTime } from '../format'

const props = defineProps({ alarmId: { type: String, required: true } })
const items = ref([]),
  enabled = ref(true),
  loading = ref(false),
  error = ref('')
const loader = useListLoader(loading)
async function load() {
  error.value = ''
  try {
    const data = await loader.run(signal => api(`/api/v1/alarms/${encodeURIComponent(props.alarmId)}/notifications`, { signal }))
    items.value = data.items || []
    enabled.value = data.enabled !== false
  } catch (e) {
    if (!isAbort(e)) error.value = e.message || '读取通知记录失败'
  }
}
const time = value => formatTime(value, '')
const stageText = item => (item.kind === 'recovery' ? '恢复通知' : item.stage === 0 ? '第 1 级' : `第 ${item.stage + 1} 级（升级）`)
watch(() => props.alarmId, load, { immediate: true })
onBeforeUnmount(loader.cancel)
defineExpose({ load })
</script>

<template>
  <ui-card shadow="never" class="top-gap">
    <template #header
      ><div class="card-header">
        <strong>通知记录</strong><ui-button text size="small" :loading="loading" @click="load">刷新</ui-button>
      </div></template
    >
    <p v-if="error" class="subline">{{ error }}</p>
    <ui-empty
      v-else-if="!items.length && !loading"
      :description="enabled ? '没有匹配的通知策略，未发送通知' : '告警通知服务未启用'"
      :image-size="52"
    />
    <ol v-else class="notify-timeline">
      <li v-for="item in items" :key="item.id">
        <StatusDot :tone="taskTone(item.status)" :label="taskStatuses[item.status] || item.status" />
        <span
          >{{ stageText(item) }} · {{ item.channelName || item.channelId
          }}<template v-if="item.policyName"> · {{ item.policyName }}</template></span
        >
        <small class="subline"
          >{{ item.status === 'SENT' ? `发送于 ${time(item.sentAt)}` : item.status === 'PENDING' ? `计划 ${time(item.nextAt)}` : ''
          }}<template v-if="item.recipients?.length"> · 接收人：{{ item.recipients.join('、') }}</template
          ><template v-if="item.lastError"> · {{ item.lastError }}</template></small
        >
      </li>
    </ol>
  </ui-card>
</template>

<style scoped>
.notify-timeline {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding-left: 18px;
}
.notify-timeline li {
  display: grid;
  gap: 2px;
}
</style>
