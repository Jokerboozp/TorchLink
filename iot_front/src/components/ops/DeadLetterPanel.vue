<script setup>
// 死信消息：处理失败并转入 iot.dlq.<消费组> 的消息。依赖故障时消息会暂停等待恢复，
// 只有消息本身无法处理或等待超过上限才进入这里；核对原因后可重新投递到原处理主题。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { isAbort, latest, opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { formatTime } from '../../format'
import { confirmed } from '../../deleteAction'

const groupNames = { parser: '解析', processor: '业务处理', state: '设备状态', 'device-alarm-notifications': '告警邮件通知' }
const groups = ref([])
const loading = ref(false)
const error = ref('')
const replaying = ref('')
const total = computed(() => groups.value.reduce((sum, g) => sum + (g.total || 0), 0))
const rows = computed(() => groups.value.flatMap(g => g.items.map(item => ({ ...item, groupName: groupNames[g.group] || g.group }))))
const groupErrors = computed(() => groups.value.filter(g => g.error).map(g => `${groupNames[g.group] || g.group}：${g.error}`))

// 总览刷新与本区刷新共用；同一时间只保留最新请求，旧请求不改动列表和加载状态。
const runner = latest()
let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  loading.value = true
  try {
    const data = await runner.run(signal => opsGet('/api/v1/ops/overview/dead-letters', { limit: 20 }, signal))
    if (seq !== loadSeq) return
    groups.value = data.groups || []
    error.value = ''
  } catch (e) {
    if (seq === loadSeq && !isAbort(e)) error.value = opsErrorText(e)
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function replay(row) {
  if (
    !(await confirmed('将这条消息重新投递到原处理主题？处理按消息幂等，重复投递不会重复生成告警；死信记录保留。', '重新投递', {
      confirmButtonText: '重新投递'
    }))
  )
    return
  const key = `${row.group}/${row.partition}/${row.offset}`
  replaying.value = key
  try {
    await opsSend('POST', `/api/v1/ops/overview/dead-letters/${encodeURIComponent(row.group)}/${row.partition}/${row.offset}/replay`)
    UiMessage.success('已重新投递，处理结果以原始报文、告警页为准')
  } catch (e) {
    UiMessage.error(opsErrorText(e))
  } finally {
    replaying.value = ''
  }
}

const time = value => formatTime(value)
onMounted(load)
onBeforeUnmount(() => runner.cancel())
defineExpose({ reload: load })
</script>

<template>
  <section class="kpi-section" aria-label="死信消息">
    <div class="section-heading">
      <h2>死信消息</h2>
      <span>处理失败、未进入正常链路的消息；共保留 {{ total }} 条</span>
      <ui-button text size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
    </div>
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <p v-for="item in groupErrors" :key="item" class="ops-muted">{{ item }}</p>
    <ui-table
      :data="rows"
      size="small"
      :row-key="row => `${row.group}/${row.partition}/${row.offset}`"
      :empty-text="loading ? '正在读取…' : '没有死信消息'"
    >
      <ui-table-column label="时间" width="170"
        ><template #default="{ row }">{{ time(row.time) }}</template></ui-table-column
      >
      <ui-table-column label="环节" width="110" prop="groupName" />
      <ui-table-column label="失败原因" min-width="260"
        ><template #default="{ row }"
          ><span class="dlq-error">{{ row.error || '—' }}</span></template
        ></ui-table-column
      >
      <ui-table-column label="消息内容" min-width="240">
        <template #default="{ row }"
          ><code class="dlq-payload" :title="row.payload">{{ row.payload }}</code
          ><small v-if="row.truncated" class="ops-muted">（已截断）</small></template
        >
      </ui-table-column>
      <ui-table-column label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <ui-button
            v-if="can('POST /api/v1/ops/overview/dead-letters/:group/:partition/:offset/replay')"
            text
            size="small"
            :loading="replaying === `${row.group}/${row.partition}/${row.offset}`"
            @click="replay(row)"
            >重新投递</ui-button
          >
        </template>
      </ui-table-column>
    </ui-table>
  </section>
</template>

<style scoped>
.kpi-section {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
.section-heading {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.section-heading h2 {
  margin: 0;
  color: var(--text-strong);
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-semibold);
}
.section-heading span,
.ops-muted {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.dlq-error {
  overflow-wrap: anywhere;
}
.dlq-payload {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--font-size-xs);
}
</style>
