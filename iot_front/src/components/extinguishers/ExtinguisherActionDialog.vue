<script setup>
// 巡检任务处理：提交巡检结果、整改、复核或取消；整改提交人不能复核自己的整改。
import { computed, reactive, ref, toRef, watch } from 'vue'
import { api, session } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { errorMessage } from '../../presentation'
import { dateTimeLabel } from '../../fireSafety'
import { canReviewInspection, extinguisherLabels, inspectionPayload } from '../../fireSafetyManagement'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // inspect、rectify、review 或 cancel。
  kind: { type: String, default: '' },
  // 打开前重新读取的任务详情。
  task: { type: Object, default: null },
  // /api/v1/fire-safety/options 的结果。
  options: { type: Object, required: true }
})
// saved(任务)：提交成功后的最新任务。
const emit = defineEmits(['saved'])
const { assetName, personName } = extinguisherLabels(props.options)
const action = computed(() => props.kind),
  actionTask = toRef(props, 'task')
const actionForm = reactive({ checks: [], findings: '', action: '', approved: true, note: '', reason: '' }),
  actionSaving = ref(false),
  actionError = ref('')
const actionTitles = { inspect: '提交巡检结果', rectify: '提交整改', review: '复核整改', cancel: '取消巡检任务' }
function reset() {
  actionError.value = ''
  Object.assign(actionForm, {
    checks: props.options.inspectionChecks.map(name => ({ name, passed: null, standard: true })),
    findings: '',
    action: '',
    approved: true,
    note: '',
    reason: ''
  })
}
watch(visible, open => open && reset())
async function saveAction() {
  if (actionSaving.value || !can(`POST /api/v1/extinguisher-inspections/:id/${action.value}`)) return
  actionSaving.value = true
  actionError.value = ''
  try {
    const version = actionTask.value.version
    let payload
    if (action.value === 'inspect') payload = inspectionPayload(actionForm, version)
    if (action.value === 'rectify') {
      if (!actionForm.action.trim()) throw new Error('请填写整改措施及完成情况')
      payload = { version, action: actionForm.action.trim() }
    }
    if (action.value === 'review') {
      if (!canReviewInspection(actionTask.value, session.user)) throw new Error('整改提交人不能复核自己的整改')
      if (!actionForm.note.trim()) throw new Error('请填写复核意见')
      payload = { version, approved: actionForm.approved, note: actionForm.note.trim() }
    }
    if (action.value === 'cancel') {
      if (!actionForm.reason.trim()) throw new Error('请填写取消原因')
      payload = { version, reason: actionForm.reason.trim() }
    }
    const result = await api(`/api/v1/extinguisher-inspections/${encodeURIComponent(actionTask.value.id)}/${action.value}`, {
      method: 'POST',
      body: JSON.stringify(payload)
    })
    UiMessage.success('任务已更新')
    visible.value = false
    emit('saved', result)
  } catch (error) {
    actionError.value = errorMessage(error)
  } finally {
    actionSaving.value = false
  }
}
const guard = trackDialogForm(visible, () => actionForm)
async function close() {
  if (actionSaving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    :title="actionTitles[action] || ''"
    width="min(680px,94vw)"
    :show-close="!actionSaving"
    :close-on-press-escape="!actionSaving"
    @update:model-value="value => value || close()"
  >
    <ui-alert v-if="actionError" type="error" :title="actionError" :closable="false" class="fire-section" />
    <p class="fire-hint">灭火器 {{ assetName(actionTask?.extinguisherId) }} · {{ personName(actionTask?.assigneeId) }}</p>
    <ui-form label-position="top">
      <template v-if="action === 'inspect'"
        ><p class="fire-hint">按实际检查结果逐项选择。标准项目不能修改或移除，可追加其他检查项目。</p>
        <div v-for="(item, index) in actionForm.checks" :key="index" class="fire-row">
          <ui-input
            v-model="item.name"
            :disabled="actionSaving || item.standard"
            placeholder="检查项目名称"
            :aria-label="`检查项目 ${index + 1}`"
          /><ui-select v-model="item.passed" :disabled="actionSaving" placeholder="请选择结果" :aria-label="`检查结果 ${index + 1}`"
            ><ui-option :value="true" label="合格" /><ui-option :value="false" label="不合格" /></ui-select
          ><ui-button v-if="!item.standard" size="small" :disabled="actionSaving" @click="actionForm.checks.splice(index, 1)"
            >移除</ui-button
          >
        </div>
        <ui-button size="small" :disabled="actionSaving" @click="actionForm.checks.push({ name: '', passed: null })">添加检查项目</ui-button
        ><ui-form-item label="发现的问题（不合格时必填）" style="margin-top: 16px"
          ><ui-input v-model="actionForm.findings" :disabled="actionSaving" type="textarea" :rows="4" maxlength="4000" /></ui-form-item
      ></template>
      <template v-if="action === 'rectify'"
        ><ui-alert
          type="warning"
          :title="actionTask?.findings || '请根据巡检问题完成整改'"
          :closable="false"
          class="fire-section" /><ui-form-item label="整改措施与完成情况 *"
          ><ui-input v-model="actionForm.action" :disabled="actionSaving" type="textarea" :rows="5" maxlength="4000" /></ui-form-item
      ></template>
      <template v-if="action === 'review'"
        ><div class="fire-history">
          <strong>本次整改</strong>
          <p>{{ actionTask?.rectifications?.at(-1)?.action }}</p>
          <small class="fire-hint"
            >提交人 {{ actionTask?.rectifications?.at(-1)?.submittedBy }} ·
            {{ dateTimeLabel(actionTask?.rectifications?.at(-1)?.submittedAt) }}</small
          >
        </div>
        <ui-form-item label="复核结果"
          ><ui-select v-model="actionForm.approved" :disabled="actionSaving"
            ><ui-option :value="true" label="通过，关闭任务" /><ui-option :value="false" label="驳回，继续整改" /></ui-select></ui-form-item
        ><ui-form-item label="复核意见 *"
          ><ui-input v-model="actionForm.note" :disabled="actionSaving" type="textarea" :rows="4" maxlength="4000" /></ui-form-item
      ></template>
      <ui-form-item v-if="action === 'cancel'" label="取消原因 *"
        ><ui-input v-model="actionForm.reason" :disabled="actionSaving" type="textarea" :rows="4" maxlength="2000"
      /></ui-form-item> </ui-form
    ><template #footer
      ><ui-button :disabled="actionSaving" @click="close">返回</ui-button
      ><ui-button
        v-permission="`POST /api/v1/extinguisher-inspections/:id/${action}`"
        type="primary"
        :loading="actionSaving"
        @click="saveAction"
        >提交</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
