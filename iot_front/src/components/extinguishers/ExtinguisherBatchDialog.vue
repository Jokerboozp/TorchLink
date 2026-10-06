<script setup>
// 批量创建巡检：按消防站和到期情况选出灭火器，统一指定巡检人员和截止时间；已有未结任务或已报废的资产由服务端跳过。
import { computed, reactive, ref, watch } from 'vue'
import { api, isAbort } from '../../api'
import { useListLoader } from '../../composables/useListLoader'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { errorMessage } from '../../presentation'
import { fireQuery, inputTimestamp, localDateTimeInput } from '../../fireSafetyManagement'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 默认消防站；为空时取第一个启用的消防站。
  stationId: { type: String, default: '' },
  // 判断“即将到期”的提前天数，与页面筛选一致。
  remindDays: { type: Number, default: 30 },
  // /api/v1/fire-safety/options 的结果。
  options: { type: Object, required: true }
})
// saved：创建成功（弹窗保持打开以显示跳过的资产）。
const emit = defineEmits(['saved'])
const batchForm = reactive({ stationId: '', due: 'overdue', assigneeId: '', dueAtInput: '', notes: '' }),
  batchAssets = ref([]),
  batchSelected = ref([]),
  batchLoading = ref(false),
  batchSaving = ref(false),
  batchError = ref(''),
  batchResult = ref(null)
const batchAssignees = computed(() => props.options.personnel.filter(item => item.stationId === batchForm.stationId && item.enabled))
const batchLoader = useListLoader(batchLoading)
async function loadBatchAssets() {
  batchAssets.value = []
  batchSelected.value = []
  batchError.value = ''
  if (!batchForm.stationId) return batchLoader.cancel()
  try {
    const result = await batchLoader.run(signal =>
      api(
        `/api/v1/extinguishers?${fireQuery({ stationId: batchForm.stationId, due: batchForm.due, remindDays: props.remindDays }, { page: 1, pageSize: 100 })}`,
        { signal }
      )
    )
    batchAssets.value = (result.items || []).filter(item => item.status !== 'retired' && !item.openInspection)
    batchSelected.value = batchAssets.value.map(item => item.id)
  } catch (error) {
    if (!isAbort(error)) batchError.value = errorMessage(error)
  }
}
function reset() {
  Object.assign(batchForm, {
    stationId: props.stationId || props.options.stations.find(item => item.enabled)?.id || '',
    due: 'overdue',
    assigneeId: '',
    dueAtInput: localDateTimeInput(Date.now() + 86400000),
    notes: ''
  })
  batchResult.value = null
  loadBatchAssets()
}
watch(visible, open => open && reset())
function changeBatchStation() {
  batchForm.assigneeId = ''
  loadBatchAssets()
}
function toggleBatchAsset(id, checked) {
  batchSelected.value = checked ? [...new Set([...batchSelected.value, id])] : batchSelected.value.filter(item => item !== id)
}
// 候选灭火器随消防站自动勾选，只把人员、截止时间和说明视为填写内容。
const guard = trackDialogForm(visible, () => [batchForm.assigneeId, batchForm.dueAtInput, batchForm.notes])
async function saveBatch() {
  if (batchSaving.value || !can('POST /api/v1/extinguisher-inspections/batch')) return
  batchSaving.value = true
  batchError.value = ''
  try {
    if (!batchSelected.value.length || !batchForm.assigneeId) throw new Error('请选择灭火器和巡检人员')
    const result = await api('/api/v1/extinguisher-inspections/batch', {
      method: 'POST',
      body: JSON.stringify({
        extinguisherIds: batchSelected.value,
        assigneeId: batchForm.assigneeId,
        dueAt: inputTimestamp(batchForm.dueAtInput, '巡检截止时间'),
        notes: batchForm.notes.trim()
      })
    })
    batchResult.value = result
    guard.reset()
    UiMessage.success(`已创建 ${result.created} 个巡检任务`)
    emit('saved')
    await loadBatchAssets()
  } catch (error) {
    // 冲突说明候选已过期（他人已建任务或已报废），刷新候选后再显示原因。
    if (error?.status === 409) await loadBatchAssets()
    batchError.value = errorMessage(error)
  } finally {
    batchSaving.value = false
  }
}
async function close() {
  if (batchSaving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    title="批量创建巡检任务"
    width="min(760px,94vw)"
    :show-close="!batchSaving"
    :close-on-press-escape="!batchSaving"
    @update:model-value="value => value || close()"
  >
    <ui-alert v-if="batchError" type="error" :title="batchError" :closable="false" class="fire-section" />
    <ui-form label-position="top"
      ><div class="fire-form-grid">
        <ui-form-item label="消防站 *"
          ><ui-select v-model="batchForm.stationId" :disabled="batchSaving" filterable @change="changeBatchStation"
            ><ui-option
              v-for="item in options.stations"
              :key="item.id"
              :label="item.name"
              :value="item.id"
              :disabled="!item.enabled" /></ui-select
        ></ui-form-item>
        <ui-form-item label="到期情况"
          ><ui-select v-model="batchForm.due" :disabled="batchSaving" @change="loadBatchAssets"
            ><ui-option value="overdue" label="已逾期" /><ui-option value="soon" label="即将到期" /><ui-option
              value=""
              label="全部在用" /></ui-select
        ></ui-form-item>
        <ui-form-item label="巡检人员 *"
          ><ui-select
            v-model="batchForm.assigneeId"
            :disabled="batchSaving || !batchForm.stationId"
            filterable
            placeholder="选择本消防站人员"
            ><ui-option v-for="item in batchAssignees" :key="item.id" :label="item.name" :value="item.id" /></ui-select
        ></ui-form-item>
        <ui-form-item label="巡检截止时间 *"
          ><input v-model="batchForm.dueAtInput" :disabled="batchSaving" type="datetime-local" class="fire-date"
        /></ui-form-item>
      </div>
      <section class="fire-section">
        <h3>选择灭火器 · 已选 {{ batchSelected.length }} / {{ batchAssets.length }}</h3>
        <p class="fire-hint">只列出没有未结巡检任务的在用灭火器（最多 100 个）。</p>
        <p v-if="batchLoading" class="fire-hint">正在读取灭火器…</p>
        <div v-else class="fire-batch-assets">
          <label v-for="item in batchAssets" :key="item.id" class="fire-batch-asset"
            ><ui-checkbox
              :model-value="batchSelected.includes(item.id)"
              :disabled="batchSaving"
              @update:model-value="checked => toggleBatchAsset(item.id, checked)"
            /><span
              ><strong>{{ item.code }}</strong
              ><small class="fire-subline">{{ item.location }}</small></span
            ></label
          >
          <p v-if="!batchAssets.length" class="fire-hint">没有符合条件的灭火器。</p>
        </div>
      </section>
      <ui-form-item label="备注"
        ><ui-input v-model="batchForm.notes" :disabled="batchSaving" type="textarea" :rows="2" maxlength="2000" /></ui-form-item
    ></ui-form>
    <ui-alert
      v-if="batchResult?.skipped?.length"
      type="warning"
      :closable="false"
      :title="`已跳过 ${batchResult.skipped.length} 个：${batchResult.skipped.map(item => `${item.code}（${item.reason}）`).join('、')}`"
    />
    <template #footer
      ><ui-button :disabled="batchSaving" @click="close">关闭</ui-button
      ><ui-button
        v-permission="'POST /api/v1/extinguisher-inspections/batch'"
        type="primary"
        :loading="batchSaving"
        :disabled="!batchSelected.length || !batchForm.assigneeId"
        @click="saveBatch"
        >创建 {{ batchSelected.length }} 个任务</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
