<script setup>
import { computed, reactive, ref } from 'vue'
import { Plus } from '@lucide/vue'
import { can } from '../../permissions.js'
import { notifyError } from '../../api.js'
import { UiMessage } from '../../ui/feedback.js'
import { createClientId } from '../../clientId.js'
import { monitoringWrite } from '../../monitoring/api.js'
import { monitoringTime, recordBody } from '../../monitoring/helpers.js'
const props = defineProps({ items: { type: Array, default: () => [] }, devices: { type: Array, default: () => [] }, deviceId: { type: String, default: '' } })
const emit = defineEmits(['refresh'])
const labels = { RUNNING: '运行观察', STOPPED: '计划停运', MAINTENANCE: '计划检修' }
const rows = computed(() => props.items.map(recordBody).filter(row => !props.deviceId || row.deviceId === props.deviceId))
const editable = computed(() => can('menu:devices')&&can('POST /api/v1/monitoring-gaps/observations')), shareable = computed(() => can('POST /api/v1/monitoring-gaps/profiles/publish'))
const dialog = ref(false), saving = ref(false), confirming = ref(''), error = ref(''), form = reactive({})
const deviceName = id => props.devices.find(row => row.id === id)?.name || id
let originalScope = 'personal', originalVersion = 0
function edit(row) {
 const value = row ? recordBody(row) : {}
 Object.assign(form, { resourceId: value.resourceId || createClientId(), expectedVersion: value.revisionVersion || 0, scope: String(value.scope || 'personal').toLowerCase(), deviceId: props.deviceId || value.deviceId || '', type: value.type || 'RUNNING', range: value.start ? [value.start,value.end] : null, basis: value.basis || '', reason: value.reason || '' })
 originalScope = form.scope; originalVersion = form.expectedVersion
 if (form.scope === 'shared' && !shareable.value) { form.scope = 'personal'; form.expectedVersion = 0 }
 error.value = ''; dialog.value = true
}
async function save() {
 if (saving.value) return
 saving.value = true; error.value = ''
 try {
  if (!form.deviceId || !form.range?.[0] || !(form.range[1] > form.range[0]) || !form.basis.trim() || !form.reason.trim()) throw new Error('请选择设备、时间区间并填写实际原因和观察依据')
  await monitoringWrite('observations', '', '', { resourceId: form.resourceId, expectedVersion: form.expectedVersion, scope: form.scope, deviceIds: [form.deviceId], body: { deviceId: form.deviceId, start: form.range[0], end: form.range[1], type: form.type, basis: form.basis.trim(), reason: form.reason.trim() } })
  dialog.value = false; emit('refresh'); UiMessage.success('观察窗口草稿已保存，确认后方可作为固定分析依据')
 } catch (cause) { error.value = cause.status === 409 ? '观察记录版本已变化，请刷新后保存。' : cause.message; notifyError(cause) }
 finally { saving.value = false }
}
async function confirm(row) {
 if (confirming.value) return
 confirming.value = row.revisionId
 try { await monitoringWrite('observations', row.revisionId, 'confirm', { expectedVersion: row.revisionVersion }); emit('refresh'); UiMessage.success('已确认观察依据，新分析可选择此固定版本') }
 catch (cause) { notifyError(cause); if (cause.status === 409) emit('refresh') }
 finally { confirming.value = '' }
}
</script>
<template>
 <section class="monitor-stack"><div class="monitor-toolbar"><p class="monitor-hint">仅人工确认的停运 / 检修区间可从计划观察时长排除。草稿和正常运行窗口不排除数据缺口。</p><ui-button v-if="editable" type="primary" size="small" @click="edit()"><Plus/>新增观察窗口</ui-button></div><div class="monitor-table-scroll"><ui-table :data="rows" empty-text="暂无观察窗口。没有停运依据时保留完整计划观察区间。">
  <ui-table-column label="设备 / 类型" min-width="170"><template #default="{row}">{{deviceName(row.deviceId)}}<p class="monitor-meta">{{labels[row.type] || row.type}} · 版本 {{row.revisionVersion}}</p></template></ui-table-column>
  <ui-table-column label="实际区间" min-width="220"><template #default="{row}">{{monitoringTime(row.start)}}<p class="monitor-meta">至 {{monitoringTime(row.end)}}</p></template></ui-table-column>
  <ui-table-column label="原因 / 依据" min-width="260"><template #default="{row}">{{row.reason}}<p class="monitor-meta">{{row.basis}}</p></template></ui-table-column>
  <ui-table-column label="人工确认" min-width="170"><template #default="{row}"><ui-tag :type="row.status==='CONFIRMED'?'success':'warning'">{{row.status==='CONFIRMED'?'已确认':'草稿'}}</ui-tag><p v-if="row.confirmedBy" class="monitor-meta">{{row.confirmedBy}} · {{monitoringTime(row.confirmedAt)}}</p></template></ui-table-column>
  <ui-table-column v-if="editable||can('POST /api/v1/monitoring-gaps/observations/:id/confirm')" label="操作" width="150"><template #default="{row}"><ui-button v-if="row.status!=='CONFIRMED'&&can('POST /api/v1/monitoring-gaps/observations/:id/confirm')" :data-monitor-observation="row.revisionId" text size="small" :loading="confirming===row.revisionId" @click="confirm(row)">确认观察依据</ui-button><ui-button v-if="editable" text size="small" @click="edit(row)">保存新版本</ui-button></template></ui-table-column>
 </ui-table></div></section>
 <ui-dialog v-model="dialog" title="保存观察窗口草稿" width="min(720px,94vw)" :close-on-click-modal="false" @close="dialog=false"><div class="monitor-dialog-body"><ui-alert v-if="error" :title="error" type="error" :closable="false"/><ui-form label-position="top" :disabled="saving"><div class="monitor-grid"><ui-form-item label="设备" required><ui-select v-model="form.deviceId" :disabled="!!deviceId" filterable><ui-option v-for="row in devices" :key="row.id" :value="row.id" :label="row.name || row.id"/></ui-select></ui-form-item><ui-form-item label="观察类型"><ui-select v-model="form.type"><ui-option v-for="[value,label] in Object.entries(labels)" :key="value" :value="value" :label="label"/></ui-select></ui-form-item><ui-form-item label="资料用途"><ui-select v-model="form.scope" @change="form.expectedVersion=form.scope===originalScope?originalVersion:0"><ui-option value="personal" label="个人分析资料"/><ui-option v-if="shareable" value="shared" label="受权共享资料"/></ui-select></ui-form-item></div><ui-form-item label="实际观察区间" required><ui-date-range v-model="form.range"/></ui-form-item><ui-form-item label="实际原因" required><ui-input v-model="form.reason" placeholder="填写已知运行、停运或检修原因"/></ui-form-item><ui-form-item label="观察依据" required><ui-input v-model="form.basis" type="textarea" :rows="3" placeholder="填写计划、记录编号及人工核实依据"/></ui-form-item><p class="monitor-hint">保存不改变设备状态。确认和分析版本独立保留，既有任务仍采用其冻结的观察依据。</p></ui-form></div><template #footer><ui-button :disabled="saving" @click="dialog=false">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存观察草稿</ui-button></template></ui-dialog>
</template>
