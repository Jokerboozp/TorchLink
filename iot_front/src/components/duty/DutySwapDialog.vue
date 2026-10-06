<script setup>
// 换班申请：在同一消防站的启用人员中选择接班人，审批通过后才更新排班。
import { computed, reactive, ref, watch } from 'vue'
import { api, notifyError } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { dateTimeLabel, optionLabels } from '../../fireSafety'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 申请换班的排班。
  target: { type: Object, default: null },
  // /api/v1/fire-safety/options 的消防站、人员与班次。
  options: { type: Object, required: true }
})
const emit = defineEmits(['saved'])
const saving = ref(false)
const { stationName } = optionLabels(props.options)
const swap = reactive({ assignmentId: '', fromPersonnelId: '', toPersonnelId: '', reason: '' })
const swapFromChoices = computed(() => props.options.personnel.filter(row => props.target?.personnelIds?.includes(row.id)))
const swapToChoices = computed(() =>
  props.options.personnel.filter(
    row => row.enabled && row.stationId === props.target?.stationId && !props.target?.personnelIds?.includes(row.id)
  )
)

function reset() {
  const row = props.target
  Object.assign(swap, { assignmentId: row?.id || '', fromPersonnelId: row?.personnelIds?.[0] || '', toPersonnelId: '', reason: '' })
}
watch(visible, open => open && reset())
async function saveSwap() {
  if (saving.value || !can('POST /api/v1/duty/swaps')) return
  if (!swap.fromPersonnelId || !swap.toPersonnelId || !swap.reason.trim())
    return UiMessage.warning('请选择原值班人员、接班人员并填写换班原因')
  saving.value = true
  try {
    await api('/api/v1/duty/swaps', {
      method: 'POST',
      body: JSON.stringify({
        assignmentId: swap.assignmentId,
        fromPersonnelId: swap.fromPersonnelId,
        toPersonnelId: swap.toPersonnelId,
        reason: swap.reason.trim()
      })
    })
    visible.value = false
    UiMessage.success('换班申请已提交，审批通过后更新排班')
    emit('saved')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => swap)
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    title="申请换班"
    width="min(620px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || close()"
  >
    <p v-if="target" class="duty-hint">
      {{ stationName(target.stationId) }} · {{ dateTimeLabel(target.startAt) }} — {{ dateTimeLabel(target.endAt) }}
    </p>
    <ui-form label-position="top" :disabled="saving"
      ><div class="duty-form-grid">
        <ui-form-item label="原值班人员" required
          ><ui-select v-model="swap.fromPersonnelId" :disabled="saving"
            ><ui-option v-for="row in swapFromChoices" :key="row.id" :value="row.id" :label="row.name" /></ui-select></ui-form-item
        ><ui-form-item label="接班人员" required
          ><ui-select v-model="swap.toPersonnelId" :disabled="saving" filterable placeholder="同一消防站的启用人员"
            ><ui-option v-for="row in swapToChoices" :key="row.id" :value="row.id" :label="row.name" /></ui-select
        ></ui-form-item>
      </div>
      <ui-form-item label="换班原因" required><ui-input v-model="swap.reason" :disabled="saving" type="textarea" :rows="3" /></ui-form-item>
      <p class="duty-hint">审批通过后更新值班人员。</p></ui-form
    >
    <template #footer
      ><ui-button :disabled="saving" @click="close">取消</ui-button
      ><ui-button v-permission="'POST /api/v1/duty/swaps'" type="primary" :loading="saving" @click="saveSwap">提交申请</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped src="./duty-form.css"></style>
