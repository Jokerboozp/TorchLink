<script setup>
// 创建单个巡检任务：先选消防站，再选该站的在用灭火器和启用人员。
import { computed, reactive, ref, watch } from 'vue'
import { api } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { errorMessage } from '../../presentation'
import { inputTimestamp, localDateTimeInput } from '../../fireSafetyManagement'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 预先选定的灭火器；为空时只带入默认消防站。
  asset: { type: Object, default: null },
  stationId: { type: String, default: '' },
  // /api/v1/fire-safety/options 的结果。
  options: { type: Object, required: true }
})
const emit = defineEmits(['saved'])
const taskForm = reactive({ extinguisherId: '', assigneeId: '', dueAtInput: '', notes: '' }),
  taskStationId = ref(''),
  taskSaving = ref(false),
  taskError = ref('')
const taskAssets = computed(() =>
  props.options.extinguishers.filter(item => item.stationId === taskStationId.value && item.status !== 'retired')
)
const assignees = computed(() => {
  const asset = props.options.extinguishers.find(item => item.id === taskForm.extinguisherId)
  return props.options.personnel.filter(item => item.stationId === asset?.stationId && item.enabled)
})
function reset() {
  taskStationId.value = props.asset?.stationId || props.stationId
  Object.assign(taskForm, {
    extinguisherId: props.asset?.id || '',
    assigneeId: '',
    dueAtInput: localDateTimeInput(Date.now() + 86400000),
    notes: ''
  })
  taskError.value = ''
}
watch(visible, open => open && reset())
function changeTaskStation() {
  taskForm.extinguisherId = ''
  taskForm.assigneeId = ''
}
async function saveTask() {
  if (taskSaving.value || !can('POST /api/v1/extinguisher-inspections')) return
  taskSaving.value = true
  taskError.value = ''
  try {
    if (!taskForm.extinguisherId || !taskForm.assigneeId) throw new Error('请选择灭火器和巡检人员')
    await api('/api/v1/extinguisher-inspections', {
      method: 'POST',
      body: JSON.stringify({
        extinguisherId: taskForm.extinguisherId,
        assigneeId: taskForm.assigneeId,
        dueAt: inputTimestamp(taskForm.dueAtInput, '巡检截止时间'),
        notes: taskForm.notes.trim()
      })
    })
    UiMessage.success('巡检任务已创建')
    visible.value = false
    emit('saved')
  } catch (error) {
    taskError.value = errorMessage(error)
  } finally {
    taskSaving.value = false
  }
}
const guard = trackDialogForm(visible, () => [taskForm, taskStationId.value])
async function close() {
  if (taskSaving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    title="创建巡检任务"
    width="min(580px,94vw)"
    :show-close="!taskSaving"
    :close-on-press-escape="!taskSaving"
    @update:model-value="value => value || close()"
  >
    <ui-alert v-if="taskError" type="error" :title="taskError" :closable="false" class="fire-section" /><ui-form label-position="top"
      ><ui-form-item label="消防站"
        ><ui-select v-model="taskStationId" :disabled="taskSaving" filterable @change="changeTaskStation"
          ><ui-option
            v-for="item in options.stations"
            :key="item.id"
            :label="item.name"
            :value="item.id"
            :disabled="!item.enabled" /></ui-select></ui-form-item
      ><ui-form-item label="灭火器 *"
        ><ui-select
          v-model="taskForm.extinguisherId"
          :disabled="taskSaving || !taskStationId"
          filterable
          @change="taskForm.assigneeId = ''"
          placeholder="选择该消防站的灭火器"
          ><ui-option v-for="item in taskAssets" :key="item.id" :label="item.code" :value="item.id" /></ui-select></ui-form-item
      ><ui-form-item label="巡检人员 *"
        ><ui-select
          v-model="taskForm.assigneeId"
          :disabled="taskSaving || !taskForm.extinguisherId"
          filterable
          placeholder="选择本消防站人员"
          ><ui-option v-for="item in assignees" :key="item.id" :label="item.name" :value="item.id" /></ui-select></ui-form-item
      ><ui-form-item label="巡检截止时间 *"
        ><input v-model="taskForm.dueAtInput" :disabled="taskSaving" type="datetime-local" class="fire-date" /></ui-form-item
      ><ui-form-item label="任务说明"
        ><ui-input v-model="taskForm.notes" :disabled="taskSaving" type="textarea" :rows="3" maxlength="2000" /></ui-form-item></ui-form
    ><template #footer
      ><ui-button :disabled="taskSaving" @click="close">取消</ui-button
      ><ui-button v-permission="'POST /api/v1/extinguisher-inspections'" type="primary" :loading="taskSaving" @click="saveTask"
        >创建任务</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
