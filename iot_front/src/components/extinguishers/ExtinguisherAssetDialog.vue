<script setup>
// 灭火器资料：资产信息、放置位置与维护计划；必填项逐项标在对应字段下方。
import { reactive, ref, watch } from 'vue'
import { api } from '../../api'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { errorMessage } from '../../presentation'
import { extinguisherTypes } from '../../fireSafety'
import { managementPayload, requiredFieldErrors } from '../../fireSafetyManagement'
import { confirmClose, trackDialogForm } from '../../composables/unsavedGuard.js'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 打开时编辑的灭火器；为空时新增。
  target: { type: Object, default: null },
  // 新增时默认的消防站。
  stationId: { type: String, default: '' },
  // /api/v1/fire-safety/options 的结果。
  options: { type: Object, required: true }
})
// saved(灭火器编号)：保存成功，页面据此关闭过期的详情并刷新。
const emit = defineEmits(['saved'])
const assetStates = [
  { value: 'active', label: '在用' },
  { value: 'maintenance', label: '维护中' },
  { value: 'retired', label: '已报废' }
]
const saving = ref(false),
  saveError = ref(''),
  form = reactive({}),
  fieldErrors = ref({})
const blank = () => ({
  id: '',
  code: '',
  stationId: props.stationId,
  location: '',
  type: 'dry_powder',
  specification: '',
  manufacturer: '',
  serialNumber: '',
  manufacturedOn: '',
  serviceDueOn: '',
  retireOn: '',
  inspectionCycleDays: 30,
  status: 'active',
  notes: ''
})
function reset() {
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form, blank(), props.target ? JSON.parse(JSON.stringify(props.target)) : {})
  saveError.value = ''
  fieldErrors.value = {}
}
watch(visible, open => open && reset())
async function saveAsset() {
  if (saving.value || !can(form.id ? 'PUT /api/v1/extinguishers/:id' : 'POST /api/v1/extinguishers')) return
  saving.value = true
  saveError.value = ''
  try {
    fieldErrors.value = requiredFieldErrors('extinguishers', form)
    if (!Number.isInteger(form.inspectionCycleDays) || form.inspectionCycleDays < 1)
      fieldErrors.value.inspectionCycleDays = '巡检周期须为正整数天数'
    if (Object.keys(fieldErrors.value).length) return
    await api(`/api/v1/extinguishers${form.id ? `/${encodeURIComponent(form.id)}` : ''}`, {
      method: form.id ? 'PUT' : 'POST',
      body: JSON.stringify(managementPayload('extinguishers', form))
    })
    UiMessage.success('灭火器资料已保存')
    visible.value = false
    emit('saved', form.id)
  } catch (error) {
    saveError.value = errorMessage(error)
  } finally {
    saving.value = false
  }
}
const guard = trackDialogForm(visible, () => form)
async function close() {
  if (saving.value) return
  if (await confirmClose(guard.dirty())) visible.value = false
}
</script>

<template>
  <ui-dialog
    :model-value="visible"
    :title="form.id ? '编辑灭火器' : '新增灭火器'"
    width="min(760px,94vw)"
    :show-close="!saving"
    :close-on-press-escape="!saving"
    @update:model-value="value => value || close()"
  >
    <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="fire-section" />
    <ui-form :model="form" label-position="top">
      <section class="fire-section">
        <h3>资产与放置位置</h3>
        <div class="fire-form-grid">
          <ui-form-item label="灭火器编号 *" :error="fieldErrors.code"><ui-input v-model="form.code" :disabled="saving" /></ui-form-item
          ><ui-form-item label="所属消防站 *" :error="fieldErrors.stationId"
            ><ui-select v-model="form.stationId" :disabled="saving" filterable
              ><ui-option
                v-for="item in options.stations"
                :key="item.id"
                :label="`${item.name}${!item.enabled ? '（已停用）' : ''}`"
                :value="item.id"
                :disabled="!item.enabled && item.id !== form.stationId" /></ui-select></ui-form-item
          ><ui-form-item label="放置位置 *" :error="fieldErrors.location"
            ><ui-input v-model="form.location" :disabled="saving" placeholder="建筑、楼层或具体点位" /></ui-form-item
          ><ui-form-item label="灭火器类型"
            ><ui-select v-model="form.type" :disabled="saving"
              ><ui-option
                v-for="item in extinguisherTypes"
                :key="item.value"
                :value="item.value"
                :label="item.label" /></ui-select></ui-form-item
          ><ui-form-item label="规格"><ui-input v-model="form.specification" :disabled="saving" placeholder="例如：4 kg" /></ui-form-item
          ><ui-form-item label="状态"
            ><ui-select v-model="form.status" :disabled="saving"
              ><ui-option v-for="item in assetStates" :key="item.value" :value="item.value" :label="item.label" /></ui-select
          ></ui-form-item>
        </div>
      </section>
      <section class="fire-section">
        <h3>生产与维护计划</h3>
        <div class="fire-form-grid">
          <ui-form-item label="生产厂家"><ui-input v-model="form.manufacturer" :disabled="saving" /></ui-form-item
          ><ui-form-item label="出厂序列号"><ui-input v-model="form.serialNumber" :disabled="saving" /></ui-form-item
          ><ui-form-item label="生产日期"
            ><input v-model="form.manufacturedOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item
          ><ui-form-item label="维护到期日期"
            ><input v-model="form.serviceDueOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item
          ><ui-form-item label="计划报废日期"
            ><input v-model="form.retireOn" type="date" class="fire-date" :disabled="saving" /></ui-form-item
          ><ui-form-item label="巡检周期（天）" :error="fieldErrors.inspectionCycleDays"
            ><ui-input-number v-model="form.inspectionCycleDays" :disabled="saving" :min="1" :max="3650" :precision="0"
          /></ui-form-item>
        </div>
      </section>
      <ui-form-item label="备注"
        ><ui-input v-model="form.notes" :disabled="saving" type="textarea" :rows="3" maxlength="2000"
      /></ui-form-item> </ui-form
    ><template #footer
      ><ui-button :disabled="saving" @click="close">取消</ui-button
      ><ui-button
        v-permission="form.id ? 'PUT /api/v1/extinguishers/:id' : 'POST /api/v1/extinguishers'"
        type="primary"
        :loading="saving"
        @click="saveAsset"
        >保存</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
