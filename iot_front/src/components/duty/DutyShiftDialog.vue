<script setup>
// 班次模板：名称与建议时段；结束时间不晚于开始时间时按跨日处理。
import { reactive, ref, watch } from 'vue'
import { api, notifyError } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { shiftRange, toDateInput } from '../../fireSafety'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
// target：打开时编辑的班次；为空时新增。
const props = defineProps({ target: { type: Object, default: null } })
const emit = defineEmits(['saved'])
const saving = ref(false)
const shift = reactive({ id: '', version: 0, name: '', startTime: '08:00', endTime: '17:00' })
const errorText = error => error?.message || '加载失败，请重试'

function reset() {
  Object.assign(shift, { id: '', version: 0, name: '', startTime: '08:00', endTime: '17:00' }, props.target || {})
}
watch(visible, open => open && reset())
async function saveShift() {
  if (saving.value || !can(shift.id ? 'PUT /api/v1/duty/shifts/:id' : 'POST /api/v1/duty/shifts')) return
  if (!shift.name.trim()) return UiMessage.warning('请填写班次名称')
  try {
    shiftRange(toDateInput(), shift.startTime, shift.endTime)
  } catch (error) {
    return UiMessage.warning(errorText(error))
  }
  saving.value = true
  try {
    await api(shift.id ? `/api/v1/duty/shifts/${encodeURIComponent(shift.id)}` : '/api/v1/duty/shifts', {
      method: shift.id ? 'PUT' : 'POST',
      body: JSON.stringify({ ...shift, name: shift.name.trim() })
    })
    visible.value = false
    UiMessage.success('班次模板已保存')
    emit('saved')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => shift)
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    :title="shift.id ? '编辑班次模板' : '新增班次模板'"
    width="min(560px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || close()"
  >
    <ui-form label-position="top" :disabled="saving"
      ><ui-form-item label="班次名称" required
        ><ui-input v-model="shift.name" :disabled="saving" placeholder="例如白班、夜班"
      /></ui-form-item>
      <div class="duty-form-grid">
        <ui-form-item label="开始时间" required><ui-time-picker v-model="shift.startTime" :disabled="saving" /></ui-form-item
        ><ui-form-item label="结束时间" required><ui-time-picker v-model="shift.endTime" :disabled="saving" /></ui-form-item>
      </div>
      <p class="duty-hint">结束时间不晚于开始时间时按跨日班次处理。修改模板不改变已有排班的实际时段。</p></ui-form
    >
    <template #footer
      ><ui-button :disabled="saving" @click="close">取消</ui-button
      ><ui-button
        v-permission="shift.id ? 'PUT /api/v1/duty/shifts/:id' : 'POST /api/v1/duty/shifts'"
        type="primary"
        :loading="saving"
        @click="saveShift"
        >保存班次</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="./duty-form.css"></style>
