<script setup>
import { useProfileTargets } from '../../useProfileTargets.js'
import DeviceSelect from '../DeviceSelect.vue'
import { computed, reactive, ref } from 'vue'
import { Plus, Trash2 } from '@lucide/vue'
import { can } from '../../permissions.js'
import { notifyError } from '../../api.js'
import { UiMessage } from '../../ui/feedback.js'
import { createClientId } from '../../clientId.js'
import { monitoringWrite } from '../../monitoring/api.js'
import { durationLabel, messageTypes, monitoringTime, profileDraft, profilePayload, recordBody } from '../../monitoring/helpers.js'
const props = defineProps({ profiles: { type: Array, default: () => [] }, devices: { type: Array, default: () => [] }, deviceId: { type: String, default: '' } })
const emit = defineEmits(['refresh'])
const rows = computed(() => props.profiles.map(recordBody).filter(row => !props.deviceId || row.deviceIds?.includes(props.deviceId)))
const editable = computed(() => can('menu:devices')&&can('POST /api/v1/monitoring-gaps/profiles'))
const shareable = computed(() => can('POST /api/v1/monitoring-gaps/profiles/publish'))
const dialog = ref(false), saving = ref(false), error = ref(''), form = reactive(profileDraft())
const deviceName = id => props.devices.find(row => row.id === id)?.name || id
const { products, targetLoading, progress, targetChanged, loadProducts, cancel } = useProfileTargets(form, dialog, error, () => props.devices)
let originalScope = 'personal', originalVersion = 0
function edit(row) {
 cancel()
 Object.assign(form, profileDraft(row)); originalScope = form.scope; originalVersion = form.expectedVersion
 if (!shareable.value && form.scope === 'shared') { form.scope = 'personal'; form.expectedVersion = 0 }
 if (props.deviceId) { form.deviceIds = [props.deviceId]; form.targetType = 'DEVICE'; form.productId = '' }
 error.value = ''; dialog.value = true
}

async function save() {
 if (saving.value || targetLoading.value) return
 saving.value = true; error.value = ''
 try { const payload = profilePayload(form); payload.resourceId ||= createClientId(); await monitoringWrite('profiles', '', '', payload); dialog.value = false; emit('refresh'); UiMessage.success('监测策略新版本已保存') }
 catch (cause) { error.value = cause.status === 409 ? '策略版本已变化，请刷新后基于最新版本保存。' : cause.message; notifyError(cause) }
 finally { saving.value = false }
}
</script>
<template>
 <section class="monitor-stack">
  <div class="monitor-toolbar"><p class="monitor-hint">关键属性、报文类型、时效与合并策略均明确配置。父设备上报和无关心跳不会刷新子设备测点。</p><ui-button v-if="editable" type="primary" size="small" @click="edit()"><Plus/>新增监测策略</ui-button></div>
  <div class="monitor-table-scroll"><ui-table :data="rows" empty-text="暂无监测策略，请先明确关键属性与可接受报文类型。">
   <ui-table-column label="设备 / 策略版本" min-width="210"><template #default="{row}">{{(row.deviceIds || []).map(deviceName).join('、')}}<p class="monitor-meta">版本 {{row.revisionVersion}} · {{String(row.scope).toUpperCase()==='SHARED'?'共享':'个人'}} · {{row.importance || '未设重要性标签'}}</p></template></ui-table-column>
   <ui-table-column label="关键属性 / 合并" min-width="240"><template #default="{row}">{{row.attributes?.map(a=>a.id).join('、')}}<p class="monitor-meta">{{row.merge==='ALL'?'全部关键属性有效（ALL）':'任一关键属性有效（ANY）'}} · {{row.messageTypes?.map(type=>messageTypes[type] || type).join('、')}}</p></template></ui-table-column>
   <ui-table-column label="周期 / 时效" min-width="200"><template #default="{row}">{{row.mode==='event'?'事件上报，不计算周期可用率':`周期 ${durationLabel(row.periodMs)} · 容忍 ${durationLabel(row.toleranceMs)}`}}<p v-if="row.longGapMs" class="monitor-meta">长缺口门槛 {{durationLabel(row.longGapMs)}}</p></template></ui-table-column>
   <ui-table-column label="生效区间" min-width="200"><template #default="{row}">{{monitoringTime(row.effectiveFrom)}}<p class="monitor-meta">至 {{row.effectiveTo?monitoringTime(row.effectiveTo):'未设置失效时间'}}</p></template></ui-table-column>
   <ui-table-column v-if="editable" label="操作" width="110"><template #default="{row}"><ui-button :data-monitor-profile="row.revisionId" text size="small" @click="edit(row)">保存新版本</ui-button></template></ui-table-column>
  </ui-table></div>
 </section>
 <ui-dialog v-model="dialog" :title="form.expectedVersion?'保存监测策略新版本':'新增监测策略'" width="min(900px,94vw)" :close-on-click-modal="false" @close="dialog=false"><div class="monitor-dialog-body"><ui-alert v-if="error" :title="error" type="error" :closable="false"/><p v-if="targetLoading" class="monitor-hint">正在读取明确设备范围：{{progress.read}} / {{progress.total ?? '待返回总数'}}</p><ui-form label-position="top" :disabled="saving"><div class="monitor-grid">
  <ui-form-item label="设备范围" required><DeviceSelect v-model="form.deviceIds" multiple :disabled="!!deviceId||targetLoading"/></ui-form-item>
  <ui-form-item label="策略用途"><ui-select v-model="form.scope" @change="form.expectedVersion=form.scope===originalScope?originalVersion:0"><ui-option value="personal" label="个人分析参数"/><ui-option v-if="shareable" value="shared" label="受权共享策略"/></ui-select></ui-form-item>
  <ui-form-item v-if="!deviceId" label="配置目标"><ui-select v-model="form.targetType" @change="targetChanged"><ui-option value="DEVICE" label="指定设备"/><ui-option value="PRODUCT" label="产品测点"/><ui-option v-if="shareable" value="TENANT" label="租户测点"/></ui-select></ui-form-item>
  <ui-form-item v-if="form.targetType==='PRODUCT'&&!deviceId" label="目标产品" required><ui-select v-model="form.productId" filterable @update:show="loadProducts" @change="targetChanged"><ui-option v-for="row in products" :key="row.id" :value="row.id" :label="row.name"/></ui-select></ui-form-item>
  <ui-form-item label="生效时间" required><ui-date-time v-model="form.effectiveFrom"/></ui-form-item><ui-form-item label="失效时间（选填）"><ui-date-time v-model="form.effectiveTo" clearable/></ui-form-item>
  <ui-form-item label="人工重要性标签"><ui-input v-model="form.importance" placeholder="按资料或现场依据填写，未确认可留空"/></ui-form-item>
  <ui-form-item label="上报模式"><ui-select v-model="form.mode"><ui-option value="periodic" label="周期上报"/><ui-option value="event" label="事件上报"/></ui-select></ui-form-item>
  <ui-form-item v-if="form.mode==='periodic'" label="预期周期（秒）" required><ui-input-number v-model="form.periodSeconds" :min="0.001" clearable/></ui-form-item><ui-form-item v-if="form.mode==='periodic'" label="时效容忍（秒）" required><ui-input-number v-model="form.toleranceSeconds" :min="0" clearable/></ui-form-item>
  <ui-form-item label="长缺口门槛（秒，选填）"><ui-input-number v-model="form.longGapSeconds" :min="0.001" clearable/></ui-form-item><ui-form-item label="频繁中断次数门槛（选填）"><ui-input-number v-model="form.frequentGapCount" :min="1" :precision="0" clearable/></ui-form-item>
  <ui-form-item label="关键属性合并"><ui-select v-model="form.merge"><ui-option value="ALL" label="全部有效（ALL）"/><ui-option value="ANY" label="任一有效（ANY）"/></ui-select></ui-form-item>
 </div><ui-form-item label="可接受报文类型" required><ui-select v-model="form.messageTypes" multiple><ui-option v-for="[value,label] in Object.entries(messageTypes)" :key="value" :value="value" :label="label"/></ui-select></ui-form-item><h3 class="monitor-subtitle">关键属性与有效值条件</h3><div v-for="(attribute,index) in form.attributes" :key="index" class="monitor-card monitor-grid">
  <ui-form-item :label="`属性标识 ${index+1}`" required><ui-input v-model="attribute.id" placeholder="填写实际物模型属性标识"/></ui-form-item><ui-form-item :label="`属性类型 ${index+1}`"><ui-select v-model="attribute.valueType" @change="attribute.minimum=null;attribute.maximum=null"><ui-option value="number" label="数值"/><ui-option value="integer" label="整数"/><ui-option value="boolean" label="布尔"/><ui-option value="string" label="字符串 / 状态"/></ui-select></ui-form-item>
  <template v-if="['number','integer'].includes(attribute.valueType)"><ui-form-item :label="`有效值下限 ${index+1}（选填）`"><ui-input-number v-model="attribute.minimum" clearable/></ui-form-item><ui-form-item :label="`有效值上限 ${index+1}（选填）`"><ui-input-number v-model="attribute.maximum" clearable/></ui-form-item></template><ui-button v-if="form.attributes.length>1" text size="small" @click="form.attributes.splice(index,1)"><Trash2/>移除属性</ui-button>
 </div><ui-button :disabled="form.attributes.length>=50" text size="small" @click="form.attributes.push({id:'',valueType:'number',minimum:null,maximum:null})"><Plus/>添加关键属性</ui-button><p class="monitor-hint">类型不符、关键属性缺失和时效已过的样本不会刷新有效数据。事件测点不计算固定周期下的可用率。</p></ui-form></div><template #footer><ui-button :disabled="saving" @click="dialog=false">取消</ui-button><ui-button type="primary" :loading="saving" :disabled="targetLoading" @click="save">保存策略版本</ui-button></template></ui-dialog>
</template>
