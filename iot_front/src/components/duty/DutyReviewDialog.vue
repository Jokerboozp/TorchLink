<script setup>
// 换班审批：申请人不能审批自己的申请，审批通过后服务端更新排班人员。
import { reactive, ref, watch } from 'vue'
import { api, notifyError, session } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { dateTimeLabel, optionLabels } from '../../fireSafety'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 待审批的换班申请。
  target: { type: Object, default: null },
  // 申请关联的排班；不可用时提示刷新核对。
  assignment: { type: Object, default: null },
  // /api/v1/fire-safety/options 的消防站、人员与班次。
  options: { type: Object, required: true }
})
const emit = defineEmits(['saved'])
const saving = ref(false)
const { stationName, personName, shiftName } = optionLabels(props.options)
const review = reactive({ approved: true, note: '' })
const canReview = row => row?.status === 'pending' && row.requestedBy !== session.user && can('POST /api/v1/duty/swaps/:id/review')

function reset() {
  Object.assign(review, { approved: true, note: '' })
}
watch(visible, open => open && reset())
async function saveReview() {
  const row = props.target
  if (saving.value || !row || !canReview(row)) return
  saving.value = true
  try {
    await api(`/api/v1/duty/swaps/${encodeURIComponent(row.id)}/review`, {
      method: 'POST',
      body: JSON.stringify({ version: row.version, approved: review.approved, note: review.note.trim() })
    })
    visible.value = false
    UiMessage.success(review.approved ? '换班已通过，排班人员已更新' : '换班申请已驳回')
    emit('saved')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => review)
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    title="审批换班申请"
    width="min(620px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || close()"
  >
    <ui-form v-if="target" label-position="top" :disabled="saving">
      <ui-descriptions v-if="assignment" :column="1" border class="duty-review-context">
        <ui-descriptions-item label="消防站">{{ stationName(assignment.stationId) }}</ui-descriptions-item>
        <ui-descriptions-item label="班次">{{ shiftName(assignment.shiftId) }}</ui-descriptions-item>
        <ui-descriptions-item label="实际值班时间"
          >{{ dateTimeLabel(assignment.startAt) }} — {{ dateTimeLabel(assignment.endAt) }}</ui-descriptions-item
        >
      </ui-descriptions>
      <ui-alert v-else type="warning" :closable="false" class="duty-review-context">关联排班不可用，请刷新后核对。</ui-alert>
      <p>{{ personName(target.fromPersonnelId) }} → {{ personName(target.toPersonnelId) }}</p>
      <p class="duty-hint">申请人：{{ target.requestedBy }}<br />原因：{{ target.reason }}</p>
      <ui-form-item label="审批结果"
        ><ui-radio-group v-model="review.approved" :disabled="saving"
          ><ui-radio-button :value="true">通过</ui-radio-button><ui-radio-button :value="false">驳回</ui-radio-button></ui-radio-group
        ></ui-form-item
      >
      <ui-form-item label="审批意见"><ui-input v-model="review.note" :disabled="saving" type="textarea" :rows="3" /></ui-form-item>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="close">取消</ui-button
      ><ui-button v-permission="'POST /api/v1/duty/swaps/:id/review'" type="primary" :loading="saving" @click="saveReview"
        >提交审批</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="./duty-form.css"></style>
<style scoped>
.duty-review-context {
  margin-bottom: 16px;
}
</style>
