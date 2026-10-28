<script setup>
import { takeNavigation } from '../router/paths'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { onMounted, reactive, ref, watch } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, isAbort, notifyError, parseJSON, pretty } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmDelete } from '../deleteAction'
import { alarmLevels, alarmType, alarmTypes, label, tagType } from '../labels'
import { Plus, RefreshCw, Wand2 } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import ProductFilterSelect from '../components/ProductFilterSelect.vue'
import { errorMessage } from '../presentation'
import { usePageState } from '../composables/usePageState.js'
import { confirmClose, trackDialogForm } from '../composables/unsavedGuard.js'
import { usePagedList } from '../composables/usePagedList'

const rules = ref([])
const dialog = ref(false)
const draftDialog = ref(false)
const readonly = ref(false)
const loading = ref(false)
const prompt = ref('')
const draft = ref(null)
const draftPresentation = ref(null)
const drafting = ref(false)
const draftError = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
// 页码与每页条数在刷新或切换菜单后恢复。
usePageState('rules', { page, pageSize })
const fieldDescriptions = [
  { field: 'name', meaning: '规则名称，只用于识别和审计。', example: '高温烟雾复合告警' },
  {
    field: 'description',
    meaning: '用中文解释这条规则为什么存在、命中后意味着什么。结构化数据不使用注释字段。',
    example: '温度过高且烟雾信号同时出现'
  },
  { field: 'productId', meaning: '可选的物模型产品标识；填写后只对该产品的设备计算。', example: 'smoke-detector-v1' },
  { field: 'alarmType', meaning: '命中后生成的告警类型。', example: 'FIRE_RISK' },
  { field: 'level', meaning: '告警等级：CRITICAL / HIGH / MEDIUM / LOW / INFO。', example: 'HIGH' },
  { field: 'match', meaning: 'all=全部条件满足；any=任一条件满足。', example: 'all' },
  { field: 'conditions', meaning: '触发条件数组；按 match 字段组合。', example: '[{"field":"temperature","operator":">","value":80}]' },
  {
    field: 'conditions[].field',
    meaning: '标准消息字段；不写前缀时优先读取 properties，也可写 properties./tags./event.。',
    example: 'temperature'
  },
  { field: 'conditions[].operator', meaning: '比较方式，如 eq、gt、gte、lt、contains、in、exists。', example: '>' },
  { field: 'conditions[].value', meaning: '和设备上报值比较的目标值，类型要和物模型一致。', example: '80' },
  { field: 'durationSeconds', meaning: '条件连续满足多少秒后触发；0 表示立即触发。', example: '30' },
  { field: 'recovery', meaning: '恢复条件数组，满足后关闭规则告警。', example: 'temperature < 70' },
  { field: 'recovery[].field', meaning: '恢复判断读取的标准消息字段，字段路径规则与触发条件相同。', example: 'temperature' },
  { field: 'recovery[].operator', meaning: '恢复判断使用的比较方式。', example: 'lt' },
  { field: 'recovery[].value', meaning: '恢复判断的目标值，类型应与设备上报值一致。', example: '70' },
  {
    field: 'actions',
    meaning: '告警后的前端联动数组，只允许打开已登记摄像头或平台页面。',
    example: '[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]'
  },
  { field: 'actions[].type', meaning: '联动类型：OPEN_CAMERA 或 OPEN_PAGE。', example: 'OPEN_CAMERA' },
  { field: 'actions[].cameraId', meaning: 'OPEN_CAMERA 要打开的摄像头标识，服务端会校验租户归属。', example: 'camera-001' },
  { field: 'actions[].page', meaning: 'OPEN_PAGE 要打开的平台页面代码，不能填写外部 URL。', example: 'alarms' },
  {
    field: 'expression',
    meaning: '可选规则引擎表达式；填写后运行时优先使用它，智能草稿默认不启用。',
    example: 'Properties["temperature"] > 80'
  },
  { field: 'enabled', meaning: '是否参与实时告警计算；智能生成的规则默认关闭。', example: 'false' }
]
const blank = () => ({
  id: '',
  name: '',
  description: '',
  alarmType: 'FIRE_RISK',
  level: 'HIGH',
  productId: '',
  match: 'all',
  expression: '',
  genginePlaceholder: '',
  conditions: pretty([{ field: 'temperature', operator: '>', value: 80 }]),
  recovery: '[]',
  actions: '[]',
  durationSeconds: 0,
  enabled: true
})
const form = reactive(blank())
// 编辑弹窗关闭前检查未保存的修改；只读查看不会产生修改。
const formGuard = trackDialogForm(dialog, () => form)
async function closeEditor() {
  if (await confirmClose(formGuard.dirty())) dialog.value = false
}
const fieldKinds = { property: '属性', event: '事件', eventField: '事件字段' }
const productFields = ref([])
const fieldPick = ref('')
const fieldsLoader = useListLoader()
// The selected product's thing-model fields help write conditions; the
// conditions themselves stay editable JSON.
watch(
  () => [dialog.value, form.productId],
  async ([visible, productId]) => {
    productFields.value = []
    fieldPick.value = ''
    if (!visible || !productId) return fieldsLoader.cancel()
    try {
      const result = await fieldsLoader.run(signal => api(`/api/v1/rules/fields?productId=${encodeURIComponent(productId)}`, { signal }))
      productFields.value = result.items || []
    } catch (error) {
      if (!isAbort(error)) productFields.value = []
    }
  }
)
function fieldLabel(item) {
  const extra = [
    item.unit,
    item.alarmHigh != null ? `高阈值 ${item.alarmHigh}` : '',
    item.alarmLow != null ? `低阈值 ${item.alarmLow}` : ''
  ]
    .filter(Boolean)
    .join(' · ')
  return `${item.name || item.field}（${fieldKinds[item.kind] || item.kind} ${item.field}${extra ? ` · ${extra}` : ''}）`
}
function insertField(key) {
  const item = productFields.value.find(x => `${x.kind}:${x.field}` === key)
  fieldPick.value = ''
  if (!item) return
  let conditions
  try {
    conditions = parseJSON(form.conditions || '[]', '触发条件')
  } catch (error) {
    notifyError(error)
    return
  }
  if (!Array.isArray(conditions)) conditions = []
  const field = item.kind === 'property' ? item.field : `event.${item.field}`
  const numeric = ['number', 'integer'].includes(item.dataType)
  const condition =
    item.kind === 'event'
      ? { field, operator: 'exists' }
      : numeric && item.alarmLow != null && item.alarmHigh == null
        ? { field, operator: '<', value: item.alarmLow }
        : numeric
          ? { field, operator: '>', value: item.alarmHigh ?? item.max ?? 0 }
          : item.dataType === 'boolean'
            ? { field, operator: 'eq', value: true }
            : { field, operator: 'eq', value: '' }
  form.conditions = pretty([...conditions, condition])
}

const loader = useListLoader(loading)
const loadError = ref('')
async function load() {
  try {
    // 规则表单的产品下拉按关键字远程检索，列表不预先加载全部设备模板。
    const rulesData = await loader.run(signal => api(`/api/v1/rules?page=${page.value}&pageSize=${pageSize.value}`, { signal }))
    rules.value = rulesData.items || []
    total.value = Number(rulesData.total ?? rules.value.length)
    loadError.value = ''
  } catch (error) {
    if (!isAbort(error)) loadError.value = error?.status === 401 ? '' : errorMessage(error) || '告警规则读取失败'
  }
}

const { changePage, changePageSize } = usePagedList(() => load(), { page, pageSize })

function open(value, presentation = null) {
  Object.assign(
    form,
    blank(),
    value
      ? {
          ...value,
          conditions: pretty(value.conditions || (value.expression ? [] : [{ field: 'temperature', operator: '>', value: 80 }])),
          recovery: pretty(value.recovery || []),
          actions: pretty(value.actions || []),
          expression: value.expression || '',
          genginePlaceholder: presentation?.genginePlaceholder || value.genginePlaceholder || ''
        }
      : {}
  )
  readonly.value = false
  dialog.value = true
}

function view(value) {
  open(value)
  readonly.value = true
}

function startEdit() {
  readonly.value = false
}

async function save() {
  try {
    const value = {
      ...form,
      expression: form.expression.trim(),
      conditions: parseJSON(form.conditions || '[]', '触发条件'),
      recovery: parseJSON(form.recovery || '[]', '恢复条件'),
      actions: parseJSON(form.actions || '[]', '联动动作'),
      durationSeconds: Number(form.durationSeconds) || 0
    }
    if (!Array.isArray(value.conditions) || !Array.isArray(value.recovery) || !Array.isArray(value.actions))
      throw new Error('条件、恢复条件和联动动作必须是结构化数据数组')
    if (!value.expression && !value.conditions.length) throw new Error('规则引擎表达式与条件结构化数据至少填写一种')
    const id = value.id
    delete value.id
    delete value.genginePlaceholder
    await api(id ? `/api/v1/rules/${encodeURIComponent(id)}` : '/api/v1/rules', {
      method: id ? 'PUT' : 'POST',
      body: JSON.stringify(value)
    })
    UiMessage.success('规则已保存')
    dialog.value = false
    await load()
  } catch (error) {
    notifyError(error)
  }
}

function remove(id) {
  const rule = rules.value.find(item => item.id === id)
  return confirmDelete({
    label: rule?.name || id,
    path: `/api/v1/rules/${encodeURIComponent(id)}`,
    warning: '删除后规则将不再参与告警计算，历史告警仍会保留。',
    onDeleted: load
  })
}

function openDraft() {
  draftError.value = ''
  draftDialog.value = true
  draftPresentation.value = null
}

async function createDraft() {
  if (!prompt.value.trim()) {
    draftError.value = '请输入规则要求'
    return
  }
  drafting.value = true
  draftError.value = ''
  draft.value = null
  try {
    const data = await api('/api/v1/ai/rule-draft', { method: 'POST', body: JSON.stringify({ text: prompt.value.trim() }) })
    draft.value = data.draft
    draftPresentation.value = data.presentation || null
  } catch (e) {
    draftError.value = e?.message || String(e)
    notifyError(e)
  } finally {
    drafting.value = false
  }
}

function useDraft() {
  open({ ...draft.value, id: '', enabled: false }, draftPresentation.value)
  draftDialog.value = false
}

function conditionText(item) {
  if (item.expression) return item.expression
  if (Array.isArray(item.conditions) && item.conditions.length)
    return `${item.conditions.length} 个条件 · ${item.match === 'any' ? '任一满足' : '全部满足'}`
  return '未配置条件'
}

function actionText(item) {
  if (!Array.isArray(item.actions) || !item.actions.length) return '无联动动作'
  return item.actions.map(action => action.type || '动作').join('、')
}

onMounted(async () => {
  await load()
  const detail = takeNavigation()
  if (detail.ruleDraft) open({ ...detail.ruleDraft, ...(detail.persisted ? {} : { id: '' }), enabled: false })
})
function rowActions(row) {
  return [
    { key: 'view', label: '详情', onClick: () => view(row) },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/rules/:id', onClick: () => open(row) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/rules/:id', onClick: () => remove(row.id) }
  ]
}
</script>

<template>
  <FilterBar>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-permission="'POST /api/v1/ai/rule-draft'" @click="openDraft"><Wand2 />智能生成规则草稿</ui-button>
      <ui-button v-permission="'POST /api/v1/rules'" type="primary" @click="open()"><Plus />手动添加规则</ui-button>
    </template>
  </FilterBar>

  <DataTableCard
    :title="`告警规则 · ${total} 条`"
    :error="loadError"
    @retry="load()"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="changePage"
    @update:page-size="changePageSize"
  >
    <ui-table v-loading="loading" :data="rules">
      <ui-table-column label="规则" min-width="230">
        <template #default="{ row }"
          ><b>{{ row.name }}</b
          ><small class="subline">{{ row.id }}</small></template
        >
      </ui-table-column>
      <ui-table-column label="告警类型" min-width="135"
        ><template #default="{ row }">{{ alarmType(row.alarmType) }}</template></ui-table-column
      >
      <ui-table-column label="等级" width="100" align="center"
        ><template #default="{ row }"
          ><ui-tag :type="tagType(row.level)" round>{{ label(alarmLevels, row.level, '未设置') }}</ui-tag></template
        ></ui-table-column
      >
      <ui-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><StatusDot :tone="row.enabled ? 'success' : 'info'" :label="row.enabled ? '已启用' : '草稿'" /></template
      ></ui-table-column>
      <ui-table-column label="触发条件" min-width="220" show-overflow-tooltip
        ><template #default="{ row }">{{ conditionText(row) }}</template></ui-table-column
      >
      <ui-table-column label="联动动作" min-width="160" show-overflow-tooltip
        ><template #default="{ row }">{{ actionText(row) }}</template></ui-table-column
      >
      <ui-table-column label="操作" width="176" fixed="right" align="right"
        ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
      ></ui-table-column>
      <template #empty><ui-empty description="暂无规则，可手动添加或使用智能生成草稿" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog v-model="draftDialog" title="智能规则草稿" width="min(720px, 94vw)">
    <ui-input v-model="prompt" type="textarea" :rows="6" placeholder="例如：东区烟感温度超过八十度且检测到烟雾，触发高级别火警。" />
    <ui-button v-permission="'POST /api/v1/ai/rule-draft'" class="top-gap" type="primary" :loading="drafting" @click="createDraft"
      >生成草稿</ui-button
    >
    <ui-alert
      v-if="draftError"
      class="top-gap"
      type="error"
      :closable="false"
      show-icon
      title="规则草稿生成失败"
      :description="draftError"
    />
    <ui-card v-if="draft" shadow="never" class="inner-card top-gap">
      <ui-descriptions :column="1">
        <ui-descriptions-item label="规则名称">{{ draft.name || '未命名' }}</ui-descriptions-item>
        <ui-descriptions-item label="规则含义">{{ draft.description || '智能未提供说明，请在编辑页补充。' }}</ui-descriptions-item>
        <ui-descriptions-item label="告警类型">{{ alarmType(draft.alarmType) }}</ui-descriptions-item>
        <ui-descriptions-item label="告警等级">{{ label(alarmLevels, draft.level, '未设置') }}</ui-descriptions-item>
        <ui-descriptions-item label="启用状态">待人工确认</ui-descriptions-item>
      </ui-descriptions>
      <ui-alert
        class="top-gap"
        title="结构化数据不支持标准注释"
        description="可执行结构化数据保持纯净；字段含义、条件运算符和规则引擎替代写法在下面单独展示，避免把说明误当成运行字段。"
        type="info"
        :closable="false"
        show-icon
      />
      <ui-form label-position="top" class="top-gap">
        <ui-form-item label="智能生成的规则配置"
          ><ui-input :model-value="draftPresentation?.json || pretty(draft)" type="textarea" :rows="12" readonly
        /></ui-form-item>
        <ui-form-item label="可选规则引擎表达式（默认注释展示，不会自动启用）"
          ><ui-input
            :model-value="draftPresentation?.genginePlaceholder || '// 载入编辑器后查看等价 Gengine 表达式'"
            type="textarea"
            :rows="5"
            readonly
        /></ui-form-item>
      </ui-form>
      <div class="rule-help-title">字段说明</div>
      <ui-table :data="draftPresentation?.fieldDescriptions || fieldDescriptions" size="small" border class="top-gap">
        <ui-table-column prop="field" label="字段" width="210" />
        <ui-table-column prop="meaning" label="含义" min-width="300" />
        <ui-table-column prop="example" label="示例" min-width="180" />
      </ui-table>
      <ui-button @click="useDraft">载入草稿并编辑</ui-button>
    </ui-card>
    <template #footer><ui-button @click="draftDialog = false">关闭</ui-button></template>
  </ui-dialog>

  <ui-dialog
    :model-value="dialog"
    :title="readonly ? `规则详情 · ${form.name}` : form.id ? `编辑规则 · ${form.name}` : '手动添加规则'"
    width="min(760px, 94vw)"
    destroy-on-close
    @update:model-value="value => value || closeEditor()"
  >
    <ui-form :model="form" label-position="top" :disabled="readonly">
      <section class="rule-editor-section">
        <div class="rule-editor-heading">
          <h3>规则基本信息</h3>
          <p>确定规则名称、告警结果及适用设备范围。</p>
        </div>
        <ui-form-item label="规则名称"><ui-input v-model="form.name" /></ui-form-item>
        <ui-form-item label="规则说明"
          ><ui-input
            v-model="form.description"
            type="textarea"
            :rows="2"
            placeholder="说明这条规则的触发含义和现场处置目的，便于后续复核。"
        /></ui-form-item>
        <div class="form-grid">
          <ui-form-item label="告警类型"
            ><ui-select v-model="form.alarmType"
              ><ui-option v-for="(text, key) in alarmTypes" :key="key" :label="text" :value="key" /></ui-select
          ></ui-form-item>
          <ui-form-item label="告警等级"
            ><ui-select v-model="form.level"
              ><ui-option v-for="(text, key) in alarmLevels" :key="key" :label="text" :value="key" /></ui-select
          ></ui-form-item>
          <ui-form-item label="所属产品（可选）"
            ><ProductFilterSelect v-model="form.productId" class="ui-select" :custom="false" placeholder="请选择" aria-label="所属产品"
          /></ui-form-item>
          <ui-form-item label="条件关系"
            ><ui-select v-model="form.match"><ui-option label="全部满足" value="all" /><ui-option label="任一满足" value="any" /></ui-select
          ></ui-form-item>
        </div>
      </section>
      <section class="rule-editor-section">
        <div class="rule-editor-heading">
          <h3>触发与恢复条件</h3>
          <p>按设备上报的数据填写条件；表达式填写后优先于结构化触发条件执行。</p>
        </div>
        <ui-alert
          title="当前默认使用结构化数据条件"
          description="智能草稿会同时生成规则引擎，但只以注释形式放在下面的占位文本中；只有人工把表达式填入后，运行时才会优先执行规则引擎。"
          type="info"
          :closable="false"
          show-icon
          class="rule-help-alert"
        />
        <ui-form-item label="规则引擎表达式（可选，填入后优先执行）"
          ><ui-input
            v-model="form.expression"
            type="textarea"
            :rows="4"
            :placeholder="form.genginePlaceholder || '例如：Properties[temperature] > 80 && Properties[smoke] == true'"
        /></ui-form-item>
        <ui-form-item v-if="!readonly && productFields.length" label="从设备模板物模型插入字段"
          ><ui-select v-model="fieldPick" filterable placeholder="选择属性或事件，追加一条触发条件" @update:model-value="insertField"
            ><ui-option
              v-for="item in productFields"
              :key="`${item.kind}:${item.field}`"
              :label="fieldLabel(item)"
              :value="`${item.kind}:${item.field}`" /></ui-select
        ></ui-form-item>
        <ui-form-item label="触发条件结构化数据"
          ><ui-input v-model="form.conditions" type="textarea" :rows="6" placeholder='[{"field":"temperature","operator":">","value":80}]'
        /></ui-form-item>
        <ui-form-item label="恢复条件结构化数据"
          ><ui-input v-model="form.recovery" type="textarea" :rows="4" placeholder='[{"field":"temperature","operator":"<","value":70}]'
        /></ui-form-item>
      </section>
      <section class="rule-editor-section">
        <div class="rule-editor-heading">
          <h3>联动动作与生效</h3>
          <p>设置命中后的平台动作，以及规则保存后的运行状态。</p>
        </div>
        <ui-form-item label="联动动作结构化数据"
          ><div class="rule-action-field">
            <ui-input
              v-model="form.actions"
              type="textarea"
              :rows="4"
              placeholder='[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]'
            /><small>支持定位已登记摄像头或打开平台页面，保存前会校验目标是否有效。</small>
          </div></ui-form-item
        >
        <div class="form-grid">
          <ui-form-item label="持续秒数"><ui-input-number v-model="form.durationSeconds" :min="0" /></ui-form-item
          ><ui-form-item label="保存后状态"
            ><ui-switch v-model="form.enabled" active-text="立即启用" inactive-text="保存为草稿"
          /></ui-form-item>
        </div>
      </section>
      <details class="rule-field-reference">
        <summary>查看结构化数据字段说明 <small>填写 JSON 时参考</small></summary>
        <div class="rule-reference-scroll">
          <ui-table :data="fieldDescriptions" size="small" border class="top-gap">
            <ui-table-column prop="field" label="字段" width="210" />
            <ui-table-column prop="meaning" label="含义" min-width="300" />
            <ui-table-column prop="example" label="示例" min-width="180" />
          </ui-table>
        </div>
        <div class="rule-reference-cards">
          <article v-for="item in fieldDescriptions" :key="item.field">
            <strong>{{ item.field }}</strong>
            <p>{{ item.meaning }}</p>
            <small>示例：{{ item.example }}</small>
          </article>
        </div>
      </details>
    </ui-form>
    <template #footer
      ><ui-button v-permission="'PUT /api/v1/rules/:id'" v-if="readonly" type="primary" @click="startEdit">编辑</ui-button
      ><ui-button @click="closeEditor">{{ readonly ? '关闭' : '取消' }}</ui-button
      ><ui-button v-permission="['POST /api/v1/rules', 'PUT /api/v1/rules/:id']" v-if="!readonly" type="primary" @click="save"
        >保存规则</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped>
.rule-editor-section {
  padding: 16px 17px;
  margin-bottom: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.rule-editor-heading {
  margin-bottom: 13px;
}
.rule-editor-heading h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
}
.rule-editor-heading p {
  margin: 5px 0 0;
  color: var(--text);
  font-size: 12px;
  line-height: 1.6;
}
.rule-editor-section :deep(.n-form-item) {
  min-width: 0;
}
.rule-editor-section :deep(.n-form-item:last-child) {
  margin-bottom: 0;
}
.rule-action-field {
  display: grid;
  width: 100%;
  gap: 7px;
}
.rule-action-field small {
  color: var(--text);
  font-size: 12px;
  line-height: 1.5;
}
.rule-field-reference {
  padding: 13px 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.rule-field-reference summary {
  cursor: pointer;
  color: var(--text);
  font-size: 13px;
  font-weight: 700;
}
.rule-field-reference summary small {
  margin-left: 8px;
  color: var(--text);
  font-weight: 400;
}
.rule-field-reference :deep(.n-data-table) {
  max-width: 100%;
}
.rule-reference-scroll {
  max-width: 100%;
  overflow-x: auto;
}
.rule-reference-cards {
  display: none;
}
@media (max-width: 640px) {
  .rule-editor-section {
    padding: 13px;
  }
  .rule-field-reference {
    padding: 12px;
  }
  .rule-field-reference summary small {
    display: block;
    margin: 3px 0 0;
  }
  .rule-reference-scroll {
    display: none;
  }
  .rule-reference-cards {
    display: grid;
    gap: 8px;
    margin-top: 12px;
  }
  .rule-reference-cards article {
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: 7px;
    background: var(--surface);
  }
  .rule-reference-cards strong {
    display: block;
    color: var(--text);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .rule-reference-cards p {
    margin: 5px 0;
    color: var(--text);
    font-size: 12px;
    line-height: 1.55;
  }
  .rule-reference-cards small {
    display: block;
    color: var(--text);
    font-size: 11px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
}
.rule-help-alert {
  margin: 2px 0 14px;
}
</style>
