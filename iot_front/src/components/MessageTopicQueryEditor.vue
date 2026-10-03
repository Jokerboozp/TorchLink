<script setup>
import { computed } from 'vue'
import MessageTopicDevicePicker from './MessageTopicDevicePicker.vue'
import { queryOperators } from '../messageTopics.js'
const props = defineProps({ modelValue:{ type:Object, required:true }, datasets:{ type:Array, default:() => [] }, disabled:Boolean, previewing:Boolean, preview:{ type:Object, default:null }, previewError:{ type:String, default:'' }, canPreview:Boolean })
const emit = defineEmits(['preview', 'sql', 'form'])
const draft = computed(() => props.modelValue)
const datasets = computed(() => props.datasets.filter(item => item.mode === draft.value.mode))
const fields = computed(() => props.datasets.find(item => item.id === draft.value.dataset)?.fields || [])
function changeMode() {
  if (!datasets.value.some(item => item.id === draft.value.dataset)) {
    draft.value.dataset = datasets.value[0]?.id || ''
    draft.value.fields = []; draft.value.conditions = []; draft.value.allFields = true
  }
}
function changeDataset() { draft.value.fields = []; draft.value.conditions = []; draft.value.allFields = true }
function addField(path = '') { draft.value.fields.push({ path, output:path.split('.').at(-1) || '' }) }
</script>

<template>
  <div class="query-editor">
    <div class="query-heading"><strong>发送哪些数据</strong><ui-button v-if="draft.editor === 'sql'" size="small" text :disabled="disabled || previewing" @click="emit('form')">转换为表单</ui-button></div>
    <template v-if="draft.editor === 'form'">
      <ui-form-item label="业务数据" required><ui-select v-model="draft.dataset" aria-label="业务数据" :disabled="disabled" @change="changeDataset"><ui-option v-for="dataset in datasets" :key="dataset.id" :value="dataset.id" :label="dataset.name" /></ui-select></ui-form-item>
      <ui-form-item label="返回字段"><ui-checkbox v-model="draft.allFields" :disabled="disabled">全部业务字段</ui-checkbox></ui-form-item>
      <div v-if="!draft.allFields" class="query-fields"><div v-for="(field, index) in draft.fields" :key="index" class="query-field-row"><ui-input v-model="field.path" placeholder="字段路径，如 properties.temperature" :aria-label="`返回字段 ${index + 1}`" :disabled="disabled" /><ui-input v-model="field.output" placeholder="输出名称" :aria-label="`字段别名 ${index + 1}`" :disabled="disabled" /><ui-button :disabled="disabled" @click="draft.fields.splice(index, 1)">移除</ui-button></div><div class="query-field-add"><ui-select :model-value="null" placeholder="选择常用字段" aria-label="添加常用字段" :disabled="disabled" @update:model-value="addField"><ui-option v-for="field in fields" :key="field.path" :label="`${field.name} · ${field.path}`" :value="field.path" /></ui-select><ui-button :disabled="disabled" @click="addField()">添加字段</ui-button></div><p class="query-hint">来源字段支持点路径；例如将 properties.temperature 输出为 temperature。</p></div>
      <div class="query-heading"><strong>查询条件</strong><ui-button size="small" :disabled="disabled" @click="draft.conditions.push({ field:'', operator:'eq', value:'' })">添加条件</ui-button></div>
      <ui-radio-group v-if="draft.conditions.length" v-model="draft.logic" :disabled="disabled"><ui-radio value="and">满足全部条件</ui-radio><ui-radio value="or">满足任一条件</ui-radio></ui-radio-group>
      <p v-else class="query-hint">未设置条件，发送所选设备范围内的全部记录。</p>
      <div v-for="(condition, index) in draft.conditions" :key="index" class="query-condition-row"><ui-input v-model="condition.field" placeholder="字段，如 productId" :aria-label="`条件字段 ${index + 1}`" :disabled="disabled" /><ui-select v-model="condition.operator" :aria-label="`比较方式 ${index + 1}`" :disabled="disabled"><ui-option v-for="operator in queryOperators" :key="operator.value" :value="operator.value" :label="operator.label" /></ui-select><ui-input v-if="!['is_null', 'not_null'].includes(condition.operator)" v-model="condition.value" :placeholder="['in', 'not_in'].includes(condition.operator) ? '多个值以逗号分隔' : '比较值'" :aria-label="`比较值 ${index + 1}`" :disabled="disabled" /><span v-else /><ui-button :disabled="disabled" @click="draft.conditions.splice(index, 1)">移除</ui-button></div>
      <p v-if="draft.conditions.length" class="query-hint">数字、true / false 按对应类型比较；数字形式的文本请加双引号，例如 "001"。</p>
    </template>
    <template v-else><ui-form-item label="查询 SQL" required><ui-input v-model="draft.sql" type="textarea" :rows="7" aria-label="查询 SQL" :disabled="disabled" /></ui-form-item><p class="query-hint">支持 SELECT 字段 FROM 业务数据 WHERE 条件。仅查询平台业务数据，不执行数据库 SQL。包含嵌套条件的查询保留 SQL 编辑。</p></template>
    <ui-form-item label="设备范围"><ui-radio-group v-model="draft.deviceScope" :disabled="disabled"><ui-radio value="all">当前租户全部设备</ui-radio><ui-radio value="selected">指定设备</ui-radio></ui-radio-group></ui-form-item>
    <MessageTopicDevicePicker v-if="draft.deviceScope === 'selected'" v-model="draft.deviceIds" :disabled="disabled" label="查询设备选择" hint="仅发送所选设备的数据；主设备和子设备分别选择。" />
    <p class="query-hint">{{ draft.mode === 'interval' ? '按周期发送完整查询快照（items 数组）；没有匹配记录时发送空数组。' : '收到符合条件的新数据时发送，不补发订阅前的数据。' }}</p>
    <details class="query-advanced"><summary>高级设置</summary><ui-form-item label="发送方式"><ui-radio-group v-model="draft.mode" :disabled="disabled" @change="changeMode"><ui-radio value="realtime">数据产生时</ui-radio><ui-radio value="interval">定时快照</ui-radio></ui-radio-group></ui-form-item><ui-form-item v-if="draft.mode === 'interval'" label="查询周期（秒）"><ui-input v-model="draft.intervalSeconds" type="number" aria-label="查询周期" :disabled="disabled" /></ui-form-item><ui-button v-if="draft.editor === 'form'" :disabled="disabled" @click="emit('sql')">使用 SQL 编辑</ui-button><details class="query-field-reference"><summary>可用业务字段</summary><p v-for="field in fields" :key="field.path" class="query-hint"><code>{{ field.path }}</code> · {{ field.name }}{{ field.dynamic ? '（支持子字段）' : '' }}</p><p v-if="draft.editor === 'sql'" class="query-hint">业务数据：{{ props.datasets.map(item => `${item.name} (${item.id})`).join('、') }}</p></details></details>
    <div v-if="canPreview" class="query-preview"><div class="query-heading"><strong>数据预览</strong><ui-button :loading="previewing" :disabled="disabled" @click="emit('preview')">预览数据</ui-button></div><p class="query-hint">优先使用当前业务数据，只计算结果，不保存、不发送。</p><details><summary>使用自己的 JSON 样例</summary><ui-input v-model="draft.sample" type="textarea" :rows="4" placeholder="留空使用真实业务样本" aria-label="查询预览样例" :disabled="disabled" /></details><ui-alert v-if="previewError" :title="previewError" type="error" :closable="false" /><p v-if="preview && !preview.sampled" class="query-hint">暂无可用的业务样本，可填写 JSON 样例预览。</p><p v-else-if="preview && !preview.matched && draft.mode !== 'interval'" class="query-hint">样本未匹配当前查询条件，不会发送。</p><pre v-else-if="preview?.matched || (preview?.sampled && draft.mode === 'interval')" class="query-result">{{ preview.payload }}</pre></div>
  </div>
</template>

<style scoped>
.query-editor { display:grid; gap:var(--space-3); min-width:0; padding:var(--space-4) 0; border-top:1px solid var(--border); }
.query-heading { display:flex; align-items:center; justify-content:space-between; gap:var(--space-2); }
.query-fields, .query-preview { display:grid; gap:var(--space-2); min-width:0; }
.query-field-row { display:grid; grid-template-columns:minmax(0,1.2fr) minmax(0,1fr) auto; gap:var(--space-2); }
.query-field-add { display:flex; gap:var(--space-2); }
.query-field-add > :first-child { flex:1; min-width:0; }
.query-condition-row { display:grid; grid-template-columns:minmax(0,1fr) 130px minmax(0,1fr) auto; gap:var(--space-2); }
.query-hint { margin:0; color:var(--text-muted); font-size:var(--font-size-sm); line-height:1.6; overflow-wrap:anywhere; }
.query-advanced > *, .query-field-reference > * { margin-top:var(--space-3); }
summary { cursor:pointer; font-size:var(--font-size-sm); color:var(--text-muted); }
.query-result { margin:0; max-height:240px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; background:var(--surface-muted); border:1px solid var(--border); border-radius:var(--radius-md); padding:var(--space-3); }
@media (max-width:600px) { .query-field-row, .query-condition-row { grid-template-columns:minmax(0,1fr); } .query-condition-row { border-bottom:1px solid var(--border); padding-bottom:var(--space-3); } }
</style>
