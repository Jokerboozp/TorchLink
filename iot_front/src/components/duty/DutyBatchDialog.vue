<script setup>
// 批量排班：一次生成日期区间内的排班，服务端整体校验冲突，有冲突则不保存任何一条。
import { computed, reactive, ref, watch } from 'vue'
import { NDatePicker } from 'naive-ui'
import { api, notifyError } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { batchAssignments, toDateInput } from '../../fireSafety'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // /api/v1/fire-safety/options 的消防站、人员与班次。
  options: { type: Object, required: true },
  // 打开时的默认消防站与起止日期。
  stationId: { type: String, default: '' },
  date: { type: String, default: '' }
})
const emit = defineEmits(['saved'])
const saving = ref(false)
const errorText = error => error?.message || '加载失败，请重试'
const enabledStations = computed(() => props.options.stations.filter(row => row.enabled))
const batch = reactive({ stationId: '', shiftId: '', from: toDateInput(), to: toDateInput(), groups: [[]], notes: '' })
const batchPersonnel = computed(() => props.options.personnel.filter(row => row.enabled && row.stationId === batch.stationId))
const batchPreview = computed(() => {
  try {
    return { rows: batchAssignments({ ...batch, shift: props.options.shifts.find(row => row.id === batch.shiftId) }), error: '' }
  } catch (error) {
    return { rows: [], error: errorText(error) }
  }
})
function reset() {
  Object.assign(batch, {
    stationId: props.stationId,
    shiftId: props.options.shifts[0]?.id || '',
    from: props.date,
    to: props.date,
    groups: [[]],
    notes: ''
  })
}
watch(visible, open => open && reset())
function changeBatchStation() {
  batch.groups = batch.groups.map(group => group.filter(id => batchPersonnel.value.some(row => row.id === id)))
}
async function saveBatch() {
  if (saving.value || !can('POST /api/v1/duty/assignments/batch')) return
  if (batchPreview.value.error) return UiMessage.warning(batchPreview.value.error)
  saving.value = true
  try {
    const result = await api('/api/v1/duty/assignments/batch', {
      method: 'POST',
      body: JSON.stringify({ assignments: batchPreview.value.rows })
    })
    visible.value = false
    UiMessage.success(`已安排 ${result.created} 天排班`)
    emit('saved')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => batch)
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    title="批量排班"
    width="min(760px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || close()"
  >
    <ui-form label-position="top" :disabled="saving">
      <div class="duty-form-grid">
        <ui-form-item label="消防站" required
          ><ui-select v-model="batch.stationId" filterable @change="changeBatchStation"
            ><ui-option v-for="row in enabledStations" :key="row.id" :value="row.id" :label="row.name" /></ui-select
        ></ui-form-item>
        <ui-form-item label="班次模板" required
          ><ui-select v-model="batch.shiftId"
            ><ui-option
              v-for="row in options.shifts"
              :key="row.id"
              :value="row.id"
              :label="`${row.name} · ${row.startTime}—${row.endTime}`" /></ui-select
        ></ui-form-item>
        <ui-form-item label="开始日期" required
          ><NDatePicker v-model:formatted-value="batch.from" type="date" value-format="yyyy-MM-dd" format="yyyy-MM-dd" :clearable="false"
        /></ui-form-item>
        <ui-form-item label="结束日期" required
          ><NDatePicker v-model:formatted-value="batch.to" type="date" value-format="yyyy-MM-dd" format="yyyy-MM-dd" :clearable="false"
        /></ui-form-item>
      </div>
      <section class="duty-editor-section">
        <h3>值班人员轮换</h3>
        <p>每天安排一组人员，按组顺序逐日轮换；只有一组时每天相同。</p>
        <div v-for="(group, index) in batch.groups" :key="index" class="duty-batch-group">
          <span>第 {{ index + 1 }} 组</span
          ><ui-select
            v-model="batch.groups[index]"
            multiple
            filterable
            clearable
            :disabled="!batch.stationId"
            placeholder="选择本组值班人员"
            ><ui-option v-for="row in batchPersonnel" :key="row.id" :value="row.id" :label="row.name" /></ui-select
          ><ui-button v-if="batch.groups.length > 1" text type="danger" @click="batch.groups.splice(index, 1)">移除</ui-button>
        </div>
        <ui-button size="small" :disabled="batch.groups.length >= 7" @click="batch.groups.push([])">添加一组</ui-button>
      </section>
      <ui-form-item label="备注"><ui-input v-model="batch.notes" type="textarea" :rows="2" /></ui-form-item>
      <p class="duty-batch-summary" :class="{ 'duty-batch-error': batchPreview.error }">
        {{ batchPreview.error || `将生成 ${batchPreview.rows.length} 条排班；与已有排班或人员时段冲突时整批不保存，并列出冲突日期。` }}
      </p>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="close">取消</ui-button
      ><ui-button
        v-permission="'POST /api/v1/duty/assignments/batch'"
        type="primary"
        :loading="saving"
        :disabled="!!batchPreview.error"
        @click="saveBatch"
        >生成排班</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="./duty-form.css"></style>
<style scoped>
.duty-batch-group {
  display: grid;
  grid-template-columns: 64px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.duty-batch-group > span {
  color: var(--text-muted);
  font-size: 13px;
}
.duty-batch-summary {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.6;
}
.duty-batch-error {
  color: var(--danger-text);
}
</style>
