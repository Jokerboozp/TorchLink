<script setup>
import { computed, reactive, ref } from 'vue'
import { Plus } from '@lucide/vue'
import { can } from '../../permissions.js'
import { notifyError } from '../../api.js'
import { UiMessage } from '../../ui/feedback.js'
import { createClientId } from '../../clientId.js'
import { qualityWrite } from '../../quality/api.js'
import { profileDraft, profilePayload, qualityTime, recordBody, compactNumber } from '../../quality/helpers.js'
const props = defineProps({ profiles: { type: Array, default: () => [] }, devices: { type: Array, default: () => [] }, deviceId: { type: String, default: '' } })
const emit = defineEmits(['refresh'])
const rows = computed(() => props.profiles.map(recordBody).filter(row => !props.deviceId || row.deviceIds?.includes(props.deviceId)))
const editable = computed(() => can('POST /api/v1/data-quality/profiles'))
const shareable = computed(() => can('POST /api/v1/data-quality/profiles/publish'))
const dialog = ref(false), saving = ref(false), formError = ref('')
const form = reactive(profileDraft())
const deviceName = id => props.devices.find(row => row.id === id)?.name || id
const products = computed(() => [...new Map(props.devices.filter(row => row.productId).map(row => [row.productId, { id: row.productId, name: row.productName || row.productId }])).values()])
let originalScope = 'personal', originalVersion = 0
function edit(row) {
  Object.assign(form, profileDraft(row))
  originalScope = form.scope; originalVersion = form.expectedVersion
  if (!shareable.value && form.scope === 'shared') { form.scope = 'personal'; form.expectedVersion = 0 }
  if (props.deviceId) { form.deviceIds = [props.deviceId]; form.targetType = 'DEVICE'; form.productId = '' }
  dialog.value = true; formError.value = ''
}
function scopeChanged() { form.expectedVersion = form.scope === originalScope ? originalVersion : 0 }
function targetChanged() {
  if (form.targetType === 'TENANT') form.deviceIds = props.devices.map(row => row.id)
  else if (form.targetType === 'PRODUCT' && form.productId) form.deviceIds = props.devices.filter(row => row.productId === form.productId).map(row => row.id)
  else if (form.targetType === 'DEVICE') form.productId = ''
}
async function save() {
  if (saving.value) return
  formError.value = ''; saving.value = true
  try {
    const payload = profilePayload(form)
    payload.resourceId ||= createClientId()
    await qualityWrite('profiles', '', '', payload)
    dialog.value = false; emit('refresh'); UiMessage.success('新的质量配置版本已保存')
  } catch (error) { formError.value = error?.status === 409 ? '配置已被更新，请刷新后基于最新版本保存。' : error.message; notifyError(error) }
  finally { saving.value = false }
}
</script>
<template>
 <section class="quality-stack">
  <div class="quality-toolbar"><p class="quality-hint">周期、量程和 epsilon 由产品资料或现场确认。保存新版本保留历史分析结果。</p><ui-button v-if="editable" type="primary" size="small" @click="edit()"><Plus/>新建配置</ui-button></div>
  <div class="quality-table-scroll"><ui-table :data="rows" empty-text="暂无质量配置，先填写上报模式和已确认的物理信息。">
   <ui-table-column label="属性 / 版本" min-width="160"><template #default="{row}"><strong>{{row.attributeId}}</strong><p class="quality-meta">版本 {{row.revisionVersion}} · {{row.scope==='SHARED'||row.scope==='shared'?'共享':'个人'}}</p></template></ui-table-column>
   <ui-table-column label="设备" min-width="200"><template #default="{row}">{{(row.deviceIds || []).map(deviceName).join('、')}}</template></ui-table-column>
   <ui-table-column label="上报策略" min-width="180"><template #default="{row}">{{row.mode==='event'?'事件上报':`周期 ${compactNumber(row.periodMs/1000)} 秒`}}<p v-if="row.mode==='periodic'" class="quality-meta">容忍 {{compactNumber(row.toleranceMs/1000)}} 秒 · 固定锚点 {{qualityTime(row.scheduleAnchor)}}</p></template></ui-table-column>
   <ui-table-column label="物理依据" min-width="160"><template #default="{row}">{{row.valueType}} · {{row.unit || '无单位'}}<p class="quality-meta">{{row.unitConfirmed?'单位已确认':'单位未确认'}} · {{row.rangeConfirmed?`量程 ${compactNumber(row.minimum)}～${compactNumber(row.maximum)}`:'量程未评估'}}</p></template></ui-table-column>
   <ui-table-column label="生效区间" min-width="190"><template #default="{row}">{{qualityTime(row.effectiveFrom)}}<p class="quality-meta">至 {{row.effectiveTo?qualityTime(row.effectiveTo):'下一配置版本'}}</p></template></ui-table-column>
   <ui-table-column v-if="editable" label="操作" width="120"><template #default="{row}"><ui-button :data-quality-revision="row.revisionId" text size="small" @click="edit(row)">保存新版本</ui-button></template></ui-table-column>
  </ui-table></div>
 </section>
 <ui-dialog v-model="dialog" :title="form.expectedVersion?'保存新的配置版本':'新建质量配置'" width="min(920px,94vw)" :close-on-click-modal="false" @close="dialog=false">
  <div class="quality-dialog-body">
   <ui-alert v-if="formError" :title="formError" type="error" :closable="false"/>
   <ui-form label-position="top" :disabled="saving">
    <div class="quality-grid">
     <ui-form-item label="设备范围" required><ui-select v-model="form.deviceIds" multiple filterable :disabled="!!deviceId" placeholder="明确选择获授权设备"><ui-option v-for="row in devices" :key="row.id" :value="row.id" :label="row.name || row.id"/></ui-select></ui-form-item>
     <ui-form-item label="属性标识" required><ui-input v-model="form.attributeId" placeholder="填写物模型属性标识，例如 pressure"/></ui-form-item>
     <ui-form-item label="配置用途"><ui-select v-model="form.scope" @change="scopeChanged"><ui-option value="personal" label="个人分析参数"/><ui-option v-if="shareable" value="shared" label="共享产品 / 测点配置"/></ui-select></ui-form-item>
     <ui-form-item v-if="!deviceId" label="配置目标"><ui-select v-model="form.targetType" @change="targetChanged"><ui-option value="DEVICE" label="指定设备 / 测点"/><ui-option value="PRODUCT" label="产品测点"/><ui-option v-if="shareable" value="TENANT" label="租户测点"/></ui-select></ui-form-item>
     <ui-form-item v-if="form.targetType==='PRODUCT'&&!deviceId" label="目标产品" required><ui-select v-model="form.productId" filterable @change="targetChanged"><ui-option v-for="row in products" :key="row.id" :value="row.id" :label="row.name"/></ui-select></ui-form-item>
     <ui-form-item label="生效时间" required><ui-date-time v-model="form.effectiveFrom"/></ui-form-item>
     <ui-form-item label="失效时间（选填）"><ui-date-time v-model="form.effectiveTo" clearable/></ui-form-item>
     <ui-form-item label="上报模式" required><ui-select v-model="form.mode"><ui-option value="periodic" label="周期上报"/><ui-option value="event" label="事件上报"/></ui-select></ui-form-item>
    </div>
    <div v-if="form.mode==='periodic'" class="quality-grid">
     <ui-form-item label="固定上报锚点" required><ui-date-time v-model="form.scheduleAnchor"/></ui-form-item>
     <ui-form-item label="上报周期（秒）" required><ui-input-number v-model="form.periodSeconds" :min="0.001" :step="1" placeholder="按资料填写"/></ui-form-item>
     <ui-form-item label="周期容忍（秒）" required><ui-input-number v-model="form.toleranceSeconds" :min="0" :step="0.1" placeholder="严格小于半周期"/></ui-form-item>
    </div>
    <p v-else class="quality-hint">事件测点不计算固定周期缺报率。心跳须另设周期属性。</p>
    <div class="quality-grid">
     <ui-form-item label="物模型类型"><ui-select v-model="form.valueType"><ui-option v-for="[value,label] in [['number','数值'],['integer','整数'],['boolean','布尔'],['string','字符串 / 状态'],['object','对象'],['array','数组']]" :key="value" :value="value" :label="label"/></ui-select></ui-form-item>
     <ui-form-item label="物理单位"><ui-input v-model="form.unit" placeholder="保留 MPa、°C 等标准写法"/></ui-form-item>
     <ui-form-item label="类型要求"><div class="quality-checks"><ui-checkbox v-model="form.required">必填属性</ui-checkbox><ui-checkbox v-model="form.unitConfirmed">单位已确认</ui-checkbox></div></ui-form-item>
     <ui-form-item label="量程下限"><ui-input-number v-model="form.minimum" clearable/></ui-form-item>
     <ui-form-item label="量程上限"><ui-input-number v-model="form.maximum" clearable/></ui-form-item>
     <ui-form-item label="量程依据"><ui-checkbox v-model="form.rangeConfirmed">单位和量程已核实</ui-checkbox></ui-form-item>
     <ui-form-item label="精度 epsilon"><ui-input-number v-model="form.epsilon" :min="0" clearable placeholder="未确认时留空"/></ui-form-item>
     <ui-form-item label="允许稳定时长（秒）"><ui-input-number v-model="form.stableDurationSeconds" :min="0" clearable/></ui-form-item>
     <ui-form-item label="变化率阈值（单位 / 秒）"><ui-input-number v-model="form.maxRate" :min="0" clearable/></ui-form-item>
     <ui-form-item label="连续样本最大间隔（秒）"><ui-input-number v-model="form.maxSequenceGapSeconds" :min="0" clearable placeholder="事件测点须单独填写"/></ui-form-item>
     <ui-form-item label="未来时间容忍（秒）"><ui-input-number v-model="form.futureToleranceSeconds" :min="0" clearable/></ui-form-item>
     <ui-form-item label="设备时间依据"><ui-checkbox v-model="form.clockCalibrated">已确认时钟校准</ui-checkbox></ui-form-item>
    </div>
    <h3 class="quality-subtitle">历史比较与小幅漂移</h3>
    <p class="quality-hint">基线须人工确认，并匹配单位、工况、协议及配置版本。统计保护下限为 30 条样本。</p>
    <div class="quality-grid">
     <ui-form-item label="统计最小样本数"><ui-input-number v-model="form.minimumSamples" :min="30" :precision="0"/></ui-form-item>
     <ui-form-item label="历史偏离方法"><ui-select v-model="form.deviationMethod" clearable><ui-option value="mad" label="中位数 / MAD"/><ui-option value="quantile" label="确认基线分位范围"/></ui-select></ui-form-item>
     <ui-form-item v-if="form.deviationMethod==='mad'" label="MAD 倍数"><ui-input-number v-model="form.madMultiplier" :min="0" clearable/></ui-form-item>
     <ui-form-item v-if="form.deviationMethod==='mad'" label="MAD 为零时的绝对偏差门槛"><ui-input-number v-model="form.absoluteDeviation" :min="0" clearable placeholder="或使用已确认 epsilon"/></ui-form-item>
     <ui-form-item v-if="form.deviationMethod==='quantile'" label="下分位"><ui-input-number v-model="form.quantileLower" :min="0" :max="1" :step="0.05"/></ui-form-item>
     <ui-form-item v-if="form.deviationMethod==='quantile'" label="上分位"><ui-input-number v-model="form.quantileUpper" :min="0" :max="1" :step="0.05"/></ui-form-item>
     <ui-form-item label="CUSUM 漂移线索"><ui-checkbox v-model="form.cusumEnabled">配置双侧 CUSUM</ui-checkbox></ui-form-item>
     <template v-if="form.cusumEnabled"><ui-form-item label="CUSUM 参数版本" required><ui-input v-model="form.cusumVersion"/></ui-form-item><ui-form-item label="CUSUM 容差（测量单位）" required><ui-input-number v-model="form.cusumAllowance" :min="0"/></ui-form-item><ui-form-item label="CUSUM 门槛（测量单位）" required><ui-input-number v-model="form.cusumThreshold" :min="0"/></ui-form-item></template>
    </div>
   </ui-form>
  </div>
  <template #footer><ui-button :disabled="saving" @click="dialog=false">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存配置版本</ui-button></template>
 </ui-dialog>
</template>
