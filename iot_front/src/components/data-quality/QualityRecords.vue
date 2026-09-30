<script setup>
import { computed, reactive, ref } from 'vue'
import { Plus, Upload } from '@lucide/vue'
import { can } from '../../permissions.js'
import { notifyError } from '../../api.js'
import { UiMessage } from '../../ui/feedback.js'
import { createClientId } from '../../clientId.js'
import { qualityAttachment, qualityUpload, qualityWrite } from '../../quality/api.js'
import { compactNumber, qualityTime, recordBody } from '../../quality/helpers.js'
const props = defineProps({ kind: String, items: { type: Array, default: () => [] }, devices: { type: Array, default: () => [] }, profiles: { type: Array, default: () => [] }, deviceId: { type: String, default: '' } })
const emit = defineEmits(['refresh'])
const baseline = computed(() => props.kind === 'baselines')
const rows = computed(() => props.items.map(recordBody).filter(row => !props.deviceId || row.deviceId === props.deviceId))
const options = computed(() => props.profiles.map(recordBody).filter(row => row.deviceIds?.includes(form.deviceId)))
const editable = computed(() => can(`POST /api/v1/data-quality/${props.kind}`))
const shareable = computed(() => can('POST /api/v1/data-quality/profiles/publish'))
const dialog = ref(false), saving = ref(false), uploading = ref(false), formError = ref(''), confirmId = ref('')
const form = reactive({}), attachedNames = ref({})
const deviceName = id => props.devices.find(row => row.id === id)?.name || id
function edit(row = null) {
  const value = row ? recordBody(row) : {}
  Object.assign(form, { resourceId: value.resourceId || createClientId(), expectedVersion: value.revisionVersion || 0, scope: String(value.scope || 'personal').toLowerCase(), deviceId: props.deviceId || value.deviceId || '', attributeId: value.attributeId || '', profileRevisionId: value.profileRevisionId || '', range: value.start ? [value.start, value.end] : null, validFrom: value.validFrom || null, validUntil: value.validUntil || null, operatingCondition: value.operatingCondition || '', protocolVersion: value.protocolVersion || '', configurationVersion: value.configurationVersion || '', calibratedAt: value.calibratedAt || null, basis: value.basis || '', implementedBy: value.implementedBy || '', minimum: value.minimum ?? null, maximum: value.maximum ?? null, epsilon: value.epsilon ?? null, attachments: [...(value.attachments || [])] })
  formError.value = ''; dialog.value = true
}
function selectProfile() { const selected = options.value.find(row => row.revisionId === form.profileRevisionId); if (selected) form.attributeId = selected.attributeId }
async function upload(event) {
  const files = [...(event.target.files || [])]; event.target.value = ''
  if (!form.deviceId) { formError.value = '上传附件前请选择设备'; return }
  uploading.value = true; formError.value = ''
  try {
    for (const file of files) {
      const value = await qualityUpload(file, [form.deviceId], form.scope)
      const metadata = value.body || value
      const id = metadata.id || value.id
      if (!id) throw new Error('附件上传响应缺少标识')
      form.attachments.push(id); attachedNames.value[id] = metadata.name || file.name
    }
  } catch (error) { formError.value = error.message; notifyError(error) }
  finally { uploading.value = false }
}
async function save() {
  if (saving.value || uploading.value) return
  saving.value = true; formError.value = ''
  try {
    if (!form.deviceId || !form.attributeId.trim()) throw new Error('请选择设备并填写属性标识')
    let body
    if (baseline.value) {
      if (!form.profileRevisionId || !form.range?.[0] || !(form.range[1] > form.range[0]) || !form.validFrom || !(form.validUntil > form.validFrom) || form.validFrom < form.range[1] || !form.operatingCondition.trim() || !form.protocolVersion.trim() || !form.configurationVersion.trim()) throw new Error('请填写固定配置、基线区间、有效期及同工况/协议/配置版本依据。有效期不能早于基线结束。')
      body = { deviceId: form.deviceId, attributeId: form.attributeId.trim(), profileRevisionId: form.profileRevisionId, start: form.range[0], end: form.range[1], validFrom: form.validFrom, validUntil: form.validUntil, operatingCondition: form.operatingCondition.trim(), protocolVersion: form.protocolVersion.trim(), configurationVersion: form.configurationVersion.trim() }
    } else {
      if (!form.calibratedAt || !form.basis.trim() || !form.implementedBy.trim()) throw new Error('请填写实际校准日期、校准依据和实施人')
      if (form.minimum != null && form.maximum != null && form.minimum > form.maximum) throw new Error('量程下限不能大于上限')
      if (form.epsilon != null && form.epsilon <= 0) throw new Error('精度 epsilon 必须大于 0')
      body = { deviceId: form.deviceId, attributeId: form.attributeId.trim(), calibratedAt: form.calibratedAt, basis: form.basis.trim(), implementedBy: form.implementedBy.trim(), minimum: form.minimum ?? undefined, maximum: form.maximum ?? undefined, epsilon: form.epsilon ?? undefined, attachments: form.attachments }
    }
    await qualityWrite(props.kind, '', '', { resourceId: form.resourceId, expectedVersion: form.expectedVersion, deviceIds: [form.deviceId], scope: form.scope, body })
    dialog.value = false; emit('refresh'); UiMessage.success(baseline.value ? '基线已计算保存，人工确认后方可用于历史比较' : '校准记录已保存')
  } catch (error) { formError.value = error.status === 409 ? '记录版本已变化，请刷新后重新填写。' : error.message; notifyError(error) }
  finally { saving.value = false }
}
async function confirm(row) {
  if (confirmId.value) return
  confirmId.value = row.revisionId
  try { await qualityWrite('baselines', row.revisionId, 'confirm', { expectedVersion: row.revisionVersion }); emit('refresh'); UiMessage.success('已确认该固定版本基线') }
  catch (error) { notifyError(error); if (error.status === 409) emit('refresh') }
  finally { confirmId.value = '' }
}
</script>
<template>
 <section class="quality-stack">
  <div class="quality-toolbar"><p class="quality-hint">{{baseline?'从真实历史测量形成同工况基线。未确认、失效或少于统计下限的基线不参与自动判定。':'保存真实校准依据与附件。校准记录不直接改写原始测量，阈值变化另存质量配置版本。'}}</p><ui-button v-if="editable" type="primary" size="small" @click="edit()"><Plus/>{{baseline?'建立基线':'新增校准记录'}}</ui-button></div>
  <div class="quality-table-scroll"><ui-table :data="rows" :empty-text="baseline?'暂无已保存基线':'暂无校准记录'">
   <ui-table-column label="设备 / 属性" min-width="170"><template #default="{row}"><strong>{{deviceName(row.deviceId)}}</strong><p class="quality-meta">{{row.attributeId}} · 记录版本 {{row.revisionVersion}}</p></template></ui-table-column>
   <ui-table-column v-if="baseline" label="真实样本统计" min-width="180"><template #default="{row}">{{row.sampleCount || 0}} 条 · {{row.unit || '无单位'}}<p class="quality-meta">中位数 {{compactNumber(row.median)}} · MAD {{compactNumber(row.mad)}}</p></template></ui-table-column>
   <ui-table-column v-if="baseline" label="工况 / 版本" min-width="200"><template #default="{row}">{{row.operatingCondition}}<p class="quality-meta">协议 {{row.protocolVersion || '未知'}} · 配置 {{row.configurationVersion || '未知'}}</p></template></ui-table-column>
   <ui-table-column v-if="baseline" label="确认 / 有效期" min-width="200"><template #default="{row}"><ui-tag :type="row.confirmedBy?'success':'warning'">{{row.confirmedBy?'已人工确认':'尚未确认'}}</ui-tag><p class="quality-meta">{{qualityTime(row.validFrom)}} 至 {{qualityTime(row.validUntil)}}</p><p v-if="row.confirmedBy" class="quality-meta">{{row.confirmedBy}} · {{qualityTime(row.confirmedAt)}}</p></template></ui-table-column>
   <ui-table-column v-else label="日期 / 实施人" min-width="170"><template #default="{row}">{{qualityTime(row.calibratedAt)}}<p class="quality-meta">{{row.implementedBy}}</p></template></ui-table-column>
   <ui-table-column v-if="!baseline" label="校准依据" min-width="250"><template #default="{row}">{{row.basis}}<p v-if="row.minimum!=null||row.maximum!=null||row.epsilon!=null" class="quality-meta">量程 {{compactNumber(row.minimum)}}～{{compactNumber(row.maximum)}} · epsilon {{compactNumber(row.epsilon)}}</p><div class="quality-tag-list"><ui-button v-for="id in can('GET /api/v1/data-quality/calibrations/attachments/:id')?row.attachments || []:[]" :key="id" text size="small" @click="qualityAttachment(id,attachedNames[id]).catch(notifyError)">校准附件</ui-button></div></template></ui-table-column>
   <ui-table-column v-if="baseline&&can('POST /api/v1/data-quality/baselines/:id/confirm')" label="操作" width="130"><template #default="{row}"><ui-button v-if="!row.confirmedBy" text size="small" :data-quality-baseline="row.revisionId" :loading="confirmId===row.revisionId" @click="confirm(row)">确认基线依据</ui-button><span v-else class="quality-meta">已冻结确认</span></template></ui-table-column>
  </ui-table></div>
 </section>
 <ui-dialog v-model="dialog" :title="baseline?'从历史测量建立基线':'新增校准记录'" width="min(780px,94vw)" :close-on-click-modal="false" @close="dialog=false"><div class="quality-dialog-body"><ui-alert v-if="formError" :title="formError" type="error" :closable="false"/><ui-form label-position="top" :disabled="saving||uploading"><div class="quality-grid">
  <ui-form-item label="设备" required><ui-select v-model="form.deviceId" filterable :disabled="!!deviceId" @change="form.profileRevisionId='';form.attachments=[]"><ui-option v-for="row in devices" :key="row.id" :value="row.id" :label="row.name || row.id"/></ui-select></ui-form-item>
  <ui-form-item label="资料用途"><ui-select v-model="form.scope" @change="form.attachments=[]"><ui-option value="personal" label="个人分析资料"/><ui-option v-if="shareable" value="shared" label="受权共享资料"/></ui-select></ui-form-item>
  <ui-form-item v-if="baseline" label="固定质量配置版本" required><ui-select v-model="form.profileRevisionId" filterable @change="selectProfile"><ui-option v-for="row in options" :key="row.revisionId" :value="row.revisionId" :label="`${row.attributeId} · 版本 ${row.revisionVersion} · ${qualityTime(row.createdAt)}`"/></ui-select></ui-form-item>
  <ui-form-item label="属性标识" required><ui-input v-model="form.attributeId" :readonly="baseline"/></ui-form-item>
  <template v-if="baseline"><ui-form-item label="人工确认工况" required><ui-input v-model="form.operatingCondition" placeholder="须有依据，例如稳定正常供压"/></ui-form-item><ui-form-item label="协议版本" required><ui-input v-model="form.protocolVersion"/></ui-form-item><ui-form-item label="点表 / 配置版本" required><ui-input v-model="form.configurationVersion"/></ui-form-item></template>
  <template v-else><ui-form-item label="实际校准日期" required><ui-date-time v-model="form.calibratedAt" disable-future/></ui-form-item><ui-form-item label="实施人" required><ui-input v-model="form.implementedBy"/></ui-form-item></template>
 </div><template v-if="baseline"><ui-form-item label="固定基线样本区间" required><ui-date-range v-model="form.range" disable-future/></ui-form-item><div class="quality-grid"><ui-form-item label="有效期开始" required><ui-date-time v-model="form.validFrom"/></ui-form-item><ui-form-item label="有效期结束" required><ui-date-time v-model="form.validUntil"/></ui-form-item></div><p class="quality-hint">平台计算样本数、中位数、MAD 和分位值，不接收上传统计值。保存后另行人工确认。</p></template><template v-else><ui-form-item label="校准依据" required><ui-input v-model="form.basis" type="textarea" :rows="3" placeholder="记录标准、报告编号、样本与测量结果"/></ui-form-item><div class="quality-grid"><ui-form-item label="校准后量程下限（选填）"><ui-input-number v-model="form.minimum" clearable/></ui-form-item><ui-form-item label="校准后量程上限（选填）"><ui-input-number v-model="form.maximum" clearable/></ui-form-item><ui-form-item label="校准后精度 epsilon（选填）"><ui-input-number v-model="form.epsilon" :min="0" clearable/></ui-form-item></div><ui-form-item label="校准附件"><div class="quality-stack"><label v-if="can('POST /api/v1/data-quality/calibrations/attachments')" class="quality-file-label"><Upload/>{{uploading?'正在上传':'选择附件'}}<input type="file" multiple :disabled="uploading||saving||!form.deviceId" accept=".pdf,.png,.jpg,.jpeg,.webp,.txt,.csv" @change="upload"/></label><div v-for="id in form.attachments || []" :key="id" class="quality-toolbar"><span class="quality-meta">{{attachedNames[id] || '已保存附件'}}</span><ui-button text size="small" @click="form.attachments=form.attachments.filter(value=>value!==id)">移除关联</ui-button></div></div></ui-form-item></template></ui-form></div><template #footer><ui-button :disabled="saving||uploading" @click="dialog=false">取消</ui-button><ui-button type="primary" :disabled="uploading" :loading="saving" @click="save">{{baseline?'计算并保存基线':'保存校准记录'}}</ui-button></template></ui-dialog>
</template>
<style scoped>.quality-file-label{display:flex;gap:7px;align-items:center;font-size:13px;cursor:pointer;color:var(--text)}.quality-file-label svg{width:16px;height:16px}.quality-file-label input{max-width:220px;font:inherit;font-size:12px}.quality-file-label input::file-selector-button{padding:5px;border:1px solid var(--border);border-radius:5px;background:var(--surface);color:var(--text)}</style>
