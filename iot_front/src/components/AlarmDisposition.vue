<script setup>
// 告警核实：记录现场核实结论（真实火警、误报、测试、检修、设备故障）、到场时间与处置说明。
// 火灾类和紧急告警关闭前必须核实；关闭后结论不再修改。
import { reactive, ref, watch } from 'vue'
import { api, formatTime } from '../api'
import { UiMessage } from '../ui/feedback.js'
import { dispositionResults, requiresVerification } from '../labels'

const props = defineProps({ alarm: { type: Object, required: true } })
const emit = defineEmits(['updated'])
const form = reactive({ result: '', notes: '', arrivedAt: '' })
const saving = ref(false),
  error = ref(''),
  editing = ref(false)
const toInput = ms => {
  if (!ms) return ''
  const d = new Date(ms)
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset())
  return d.toISOString().slice(0, 16)
}
watch(
  () => props.alarm,
  alarm => {
    const d = alarm?.disposition
    Object.assign(form, { result: d?.result || '', notes: d?.notes || '', arrivedAt: toInput(d?.arrivedAt) })
    editing.value = !d && alarm?.status !== 'CLOSED'
    error.value = ''
  },
  { immediate: true }
)

async function save() {
  if (!form.result) {
    error.value = '请选择核实结论'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const body = { result: form.result, notes: form.notes, arrivedAt: form.arrivedAt ? new Date(form.arrivedAt).getTime() : 0 }
    const updated = await api(`/api/v1/alarms/${encodeURIComponent(props.alarm.alarmId)}/disposition`, {
      method: 'POST',
      body: JSON.stringify(body)
    })
    editing.value = false
    UiMessage.success('核实结论已保存')
    emit('updated', updated)
  } catch (e) {
    error.value = e.message || '保存失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ui-card shadow="never" class="top-gap">
    <template #header>
      <div class="card-header">
        <strong>核实处置</strong>
        <ui-button
          v-if="alarm.disposition && !editing && alarm.status !== 'CLOSED'"
          v-permission="'POST /api/v1/alarms/:id/disposition'"
          text
          size="small"
          @click="editing = true"
          >修改</ui-button
        >
      </div>
    </template>
    <ui-alert
      v-if="!alarm.disposition && requiresVerification(alarm) && alarm.status !== 'CLOSED'"
      type="warning"
      :closable="false"
      title="火灾类或紧急告警须先填写核实结论才能关闭"
      class="disposition-gap"
    />
    <ui-descriptions v-if="alarm.disposition && !editing" :column="1" border>
      <ui-descriptions-item label="核实结论">{{
        dispositionResults[alarm.disposition.result] || alarm.disposition.result
      }}</ui-descriptions-item>
      <ui-descriptions-item label="核实人 / 时间"
        >{{ alarm.disposition.handler }} · {{ formatTime(alarm.disposition.verifiedAt) }}</ui-descriptions-item
      >
      <ui-descriptions-item v-if="alarm.disposition.arrivedAt" label="到场时间">{{
        formatTime(alarm.disposition.arrivedAt)
      }}</ui-descriptions-item>
      <ui-descriptions-item v-if="alarm.disposition.notes" label="处置说明">{{ alarm.disposition.notes }}</ui-descriptions-item>
    </ui-descriptions>
    <ui-empty v-else-if="!editing" description="未填写核实结论" :image-size="48" />
    <ui-form v-else label-position="top" :disabled="saving">
      <ui-alert v-if="error" type="error" :title="error" :closable="false" class="disposition-gap" />
      <ui-form-item label="核实结论" required>
        <ui-radio-group v-model="form.result"
          ><ui-radio-button v-for="(text, value) in dispositionResults" :key="value" :value="value">{{
            text
          }}</ui-radio-button></ui-radio-group
        >
      </ui-form-item>
      <ui-form-item label="到场时间（可选）"
        ><input v-model="form.arrivedAt" type="datetime-local" class="disposition-time"
      /></ui-form-item>
      <ui-form-item label="处置说明"
        ><ui-input v-model="form.notes" type="textarea" :rows="3" maxlength="2000" placeholder="现场情况、处置措施、误报原因等"
      /></ui-form-item>
      <ui-button v-permission="'POST /api/v1/alarms/:id/disposition'" type="primary" :loading="saving" @click="save"
        >保存核实结论</ui-button
      >
      <ui-button v-if="alarm.disposition" :disabled="saving" @click="editing = false">取消</ui-button>
    </ui-form>
  </ui-card>
</template>

<style scoped>
.disposition-gap {
  margin-bottom: var(--space-3);
}
.disposition-time {
  height: 32px;
  padding: 0 var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: var(--text-strong);
}
</style>
