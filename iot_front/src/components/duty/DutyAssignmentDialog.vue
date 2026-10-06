<script setup>
// 排班新增与详情：选择消防站、值班人员与班次时段；保存后由页面刷新日历与列表。
import { computed, reactive, ref, watch } from 'vue'
import { NDatePicker } from 'naive-ui'
import { api, notifyError } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { shiftRange, toDateInput } from '../../fireSafety'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 打开时查看或编辑的排班；为空时新增。
  target: { type: Object, default: null },
  // 新增排班的值班日期与默认消防站。
  date: { type: String, default: '' },
  stationId: { type: String, default: '' },
  // /api/v1/fire-safety/options 的消防站、人员与班次。
  options: { type: Object, required: true },
  // 选项仍在加载或加载失败时不能保存。
  unavailable: { type: Boolean, default: false }
})
// saved：保存成功；swap：从详情发起换班申请。
const emit = defineEmits(['saved', 'swap'])
const saving = ref(false)
const blankAssignment = () => ({ id: '', version: 0, stationId: '', shiftId: '', personnelIds: [], startAt: null, endAt: null, notes: '' })
const assignment = reactive(blankAssignment()),
  assignmentDate = ref(toDateInput())
const assignmentEditable = computed(() => can(assignment.id ? 'PUT /api/v1/duty/assignments/:id' : 'POST /api/v1/duty/assignments'))
const stationChoices = computed(() => props.options.stations.filter(row => row.enabled || row.id === assignment.stationId))
const personnelChoices = computed(() =>
  props.options.personnel.filter(row => row.stationId === assignment.stationId && (row.enabled || assignment.personnelIds.includes(row.id)))
)
const errorText = error => error?.message || '加载失败，请重试'

function reset() {
  const row = props.target
  Object.assign(
    assignment,
    blankAssignment(),
    row
      ? { ...row, personnelIds: [...(row.personnelIds || [])] }
      : { stationId: props.stationId, shiftId: props.options.shifts[0]?.id || '' }
  )
  assignmentDate.value = row ? toDateInput(row.startAt) : props.date
  if (!row) applyShift()
}
watch(visible, open => open && reset())
function applyShift() {
  const selected = props.options.shifts.find(row => row.id === assignment.shiftId)
  if (!selected || !assignmentDate.value) return
  try {
    ;[assignment.startAt, assignment.endAt] = shiftRange(assignmentDate.value, selected.startTime, selected.endTime)
  } catch (error) {
    UiMessage.warning(errorText(error))
  }
}
function changeAssignmentStation() {
  assignment.personnelIds = assignment.personnelIds.filter(id =>
    props.options.personnel.some(row => row.id === id && row.stationId === assignment.stationId)
  )
}
async function saveAssignment() {
  if (saving.value || !assignmentEditable.value) return
  if (!assignment.stationId || !assignment.shiftId || !assignment.personnelIds.length)
    return UiMessage.warning('请选择消防站、班次和至少一名值班人员')
  if (!assignment.startAt || !assignment.endAt || assignment.endAt <= assignment.startAt) return UiMessage.warning('结束时间须晚于开始时间')
  saving.value = true
  try {
    const body = { ...assignment, personnelIds: [...assignment.personnelIds] }
    await api(assignment.id ? `/api/v1/duty/assignments/${encodeURIComponent(assignment.id)}` : '/api/v1/duty/assignments', {
      method: assignment.id ? 'PUT' : 'POST',
      body: JSON.stringify(body)
    })
    visible.value = false
    UiMessage.success('排班已保存')
    emit('saved')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => [assignment, assignmentDate.value])
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    :title="assignment.id ? '排班详情' : '新增排班'"
    width="min(760px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || close()"
  >
    <ui-form label-position="top" :disabled="saving || !assignmentEditable">
      <section class="duty-editor-section">
        <h3>站点与人员</h3>
        <div class="duty-form-grid">
          <ui-form-item label="消防站" required
            ><ui-select
              v-model="assignment.stationId"
              :disabled="saving || !assignmentEditable"
              filterable
              @change="changeAssignmentStation"
              ><ui-option
                v-for="row in stationChoices"
                :key="row.id"
                :value="row.id"
                :label="row.name"
                :disabled="!row.enabled" /></ui-select></ui-form-item
          ><ui-form-item label="值班人员" required
            ><ui-select
              v-model="assignment.personnelIds"
              multiple
              filterable
              clearable
              :disabled="saving || !assignmentEditable || !assignment.stationId"
              placeholder="可安排多名人员"
              ><ui-option
                v-for="row in personnelChoices"
                :key="row.id"
                :value="row.id"
                :label="row.name"
                :disabled="!row.enabled" /></ui-select
          ></ui-form-item>
        </div>
      </section>
      <section class="duty-editor-section">
        <h3>班次与实际时段</h3>
        <p>选择日期与模板带入建议时间，可调整实际起止时间。</p>
        <div class="duty-form-grid">
          <ui-form-item label="值班日期"
            ><NDatePicker
              v-model:formatted-value="assignmentDate"
              type="date"
              value-format="yyyy-MM-dd"
              format="yyyy-MM-dd"
              :clearable="false"
              :disabled="saving || !assignmentEditable"
              @update:formatted-value="applyShift" /></ui-form-item
          ><ui-form-item label="班次模板" required
            ><ui-select v-model="assignment.shiftId" :disabled="saving || !assignmentEditable" @change="applyShift"
              ><ui-option
                v-for="row in options.shifts"
                :key="row.id"
                :value="row.id"
                :label="`${row.name} · ${row.startTime}—${row.endTime}`" /></ui-select></ui-form-item
          ><ui-form-item label="实际开始时间" required
            ><ui-date-time v-model="assignment.startAt" :disabled="saving || !assignmentEditable" /></ui-form-item
          ><ui-form-item label="实际结束时间" required
            ><ui-date-time v-model="assignment.endAt" :disabled="saving || !assignmentEditable"
          /></ui-form-item>
        </div>
      </section>
      <ui-form-item label="备注"
        ><ui-input v-model="assignment.notes" :disabled="saving || !assignmentEditable" type="textarea" :rows="3"
      /></ui-form-item>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="close">关闭</ui-button
      ><ui-button
        v-if="assignment.id"
        v-permission="'POST /api/v1/duty/swaps'"
        :disabled="saving || assignment.endAt <= Date.now()"
        @click="emit('swap', assignment)"
        >申请换班</ui-button
      ><ui-button
        v-permission="assignment.id ? 'PUT /api/v1/duty/assignments/:id' : 'POST /api/v1/duty/assignments'"
        type="primary"
        :loading="saving"
        :disabled="unavailable"
        @click="saveAssignment"
        >保存排班</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="./duty-form.css"></style>
