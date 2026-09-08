<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, apiAll, notifyError, parseJSON, pretty } from '../api'
import { alarmLevels, alarmType, alarmTypes, label, tagType } from '../labels'

const rules = ref([])
const products = ref([])
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
const fieldDescriptions = [
  { field:'name', meaning:'规则名称，只用于识别和审计。', example:'高温烟雾复合告警' },
  { field:'description', meaning:'用中文解释这条规则为什么存在、命中后意味着什么。JSON 不使用注释字段。', example:'温度过高且烟雾信号同时出现' },
  { field:'productId', meaning:'可选的物模型产品 ID；填写后只对该产品的设备计算。', example:'smoke-detector-v1' },
  { field:'alarmType', meaning:'命中后生成的告警类型。', example:'FIRE_RISK' },
  { field:'level', meaning:'告警等级：CRITICAL / HIGH / MEDIUM / LOW / INFO。', example:'HIGH' },
  { field:'match', meaning:'all=全部条件满足；any=任一条件满足。', example:'all' },
  { field:'conditions', meaning:'触发条件数组；按 match 字段组合。', example:'[{"field":"temperature","operator":">","value":80}]' },
  { field:'conditions[].field', meaning:'标准消息字段；不写前缀时优先读取 properties，也可写 properties./tags./event.。', example:'temperature' },
  { field:'conditions[].operator', meaning:'比较方式，如 eq、gt、gte、lt、contains、in、exists。', example:'>' },
  { field:'conditions[].value', meaning:'和设备上报值比较的目标值，类型要和物模型一致。', example:'80' },
  { field:'durationSeconds', meaning:'条件连续满足多少秒后触发；0 表示立即触发。', example:'30' },
  { field:'recovery', meaning:'恢复条件数组，满足后关闭规则告警。', example:'temperature < 70' },
  { field:'recovery[].field', meaning:'恢复判断读取的标准消息字段，字段路径规则与触发条件相同。', example:'temperature' },
  { field:'recovery[].operator', meaning:'恢复判断使用的比较方式。', example:'lt' },
  { field:'recovery[].value', meaning:'恢复判断的目标值，类型应与设备上报值一致。', example:'70' },
  { field:'actions', meaning:'告警后的前端联动数组，只允许打开已登记摄像头或平台页面。', example:'[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]' },
  { field:'actions[].type', meaning:'联动类型：OPEN_CAMERA 或 OPEN_PAGE。', example:'OPEN_CAMERA' },
  { field:'actions[].cameraId', meaning:'OPEN_CAMERA 要打开的摄像头 ID，服务端会校验租户归属。', example:'camera-001' },
  { field:'actions[].page', meaning:'OPEN_PAGE 要打开的平台页面代码，不能填写外部 URL。', example:'alarms' },
  { field:'expression', meaning:'可选 Gengine 表达式；填写后运行时优先使用它，AI 草稿默认不启用。', example:'Properties["temperature"] > 80' },
  { field:'enabled', meaning:'是否参与实时告警计算；AI 生成的规则默认关闭。', example:'false' }
]
const blank = () => ({ id:'', name:'', description:'', alarmType:'FIRE_RISK', level:'HIGH', productId:'', match:'all', expression:'', genginePlaceholder:'', conditions:pretty([{ field:'temperature', operator:'>', value:80 }]), recovery:'[]', actions:'[]', durationSeconds:0, enabled:true })
const form = reactive(blank())

let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [rulesData, productData] = await Promise.all([
      api(`/api/v1/rules?page=${page.value}&pageSize=${pageSize.value}`),
      apiAll('/api/v1/products')
    ])
    if (version !== loadVersion) return
    rules.value = rulesData.items || []
    total.value = Number(rulesData.total ?? rulesData.count ?? rules.value.length)
    products.value = productData.items || []
  } catch (error) {
    if (version === loadVersion) notifyError(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

function changePage(value) {
  page.value = value
  load()
}

function changePageSize(value) {
  pageSize.value = value
  page.value = 1
  load()
}

function open(value, presentation = null) {
  Object.assign(form, blank(), value ? { ...value, conditions:pretty(value.conditions || (value.expression ? [] : [{ field:'temperature', operator:'>', value:80 }])), recovery:pretty(value.recovery || []), actions:pretty(value.actions || []), expression:value.expression || '', genginePlaceholder:presentation?.genginePlaceholder || value.genginePlaceholder || '' } : {})
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
    const value = { ...form, expression:form.expression.trim(), conditions:parseJSON(form.conditions || '[]', '触发条件'), recovery:parseJSON(form.recovery || '[]', '恢复条件'), actions:parseJSON(form.actions || '[]', '联动动作'), durationSeconds:Number(form.durationSeconds) || 0 }
    if (!Array.isArray(value.conditions) || !Array.isArray(value.recovery) || !Array.isArray(value.actions)) throw new Error('条件、恢复条件和联动动作必须是 JSON 数组')
    if (!value.expression && !value.conditions.length) throw new Error('Gengine 表达式与条件 JSON 至少填写一种')
    const id = value.id
    delete value.id
    delete value.genginePlaceholder
    await api(id ? `/api/v1/rules/${encodeURIComponent(id)}` : '/api/v1/rules', { method:id ? 'PUT' : 'POST', body:JSON.stringify(value) })
    ElMessage.success('规则已保存')
    dialog.value = false
    await load()
  } catch (error) {
    notifyError(error)
  }
}

async function remove(id) {
  try {
    await ElMessageBox.confirm('删除后规则将不再参与告警计算，历史告警仍会保留。', '删除规则', { type:'warning' })
    await api(`/api/v1/rules/${encodeURIComponent(id)}`, { method:'DELETE' })
    ElMessage.success('规则已删除')
    await load()
  } catch (error) {
    if (error !== 'cancel') notifyError(error)
  }
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
    const data = await api('/api/v1/ai/rule-draft', { method:'POST', body:JSON.stringify({ text:prompt.value.trim() }) })
    draft.value = data.draft
    draftPresentation.value = data.presentation || null
  } catch(e) {
    draftError.value=e?.message||String(e)
    notifyError(e)
  } finally {
    drafting.value = false
  }
}

function useDraft() {
  open({ ...draft.value, id:'', enabled:false }, draftPresentation.value)
  draftDialog.value = false
}

function conditionText(item) {
  if (item.expression) return item.expression
  if (Array.isArray(item.conditions) && item.conditions.length) return `${item.conditions.length} 个条件 · ${item.match === 'any' ? '任一满足' : '全部满足'}`
  return '未配置条件'
}

function actionText(item) {
  if (!Array.isArray(item.actions) || !item.actions.length) return '无联动动作'
  return item.actions.map(action => action.type || '动作').join('、')
}

onMounted(async () => {
  await load()
  const raw = sessionStorage.getItem('iot:navigation-detail')
  if (!raw) return
  try {
    const detail = JSON.parse(raw)
    if (detail.ruleDraft) {
      sessionStorage.removeItem('iot:navigation-detail')
      open({ ...detail.ruleDraft, ...(detail.persisted ? {} : { id:'' }), enabled:false })
    }
  } catch {
    // ignore invalid navigation detail
  }
})
</script>

<template>
  <div class="page-toolbar">
    <el-button type="primary" @click="open()">手动添加规则</el-button>
    <el-button @click="openDraft">AI 生成规则草稿</el-button>
    <el-button :loading="loading" @click="load">刷新</el-button>
    <span>共 {{ total }} 条规则，详情、编辑和删除操作位于列表右侧。</span>
  </div>

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="rules" stripe>
      <el-table-column label="规则" min-width="230">
        <template #default="{ row }"><b>{{ row.name }}</b><small class="subline">{{ row.id }}</small></template>
      </el-table-column>
      <el-table-column label="告警类型" min-width="135"><template #default="{ row }">{{ alarmType(row.alarmType) }}</template></el-table-column>
      <el-table-column label="等级" width="100" align="center"><template #default="{ row }"><el-tag :type="tagType(row.level)" round>{{ label(alarmLevels, row.level, '未设置') }}</el-tag></template></el-table-column>
      <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'" round>{{ row.enabled ? '已启用' : '草稿' }}</el-tag></template></el-table-column>
      <el-table-column label="触发条件" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ conditionText(row) }}</template></el-table-column>
      <el-table-column label="联动动作" min-width="160" show-overflow-tooltip><template #default="{ row }">{{ actionText(row) }}</template></el-table-column>
      <el-table-column label="操作" width="280" fixed="right" align="center">
        <template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="view(row)">详情</el-button><el-button plain type="primary" @click="open(row)">编辑</el-button><el-button plain type="danger" @click="remove(row.id)">删除</el-button></div></template>
      </el-table-column>
      <template #empty><el-empty description="暂无规则，可手动添加或使用 AI 生成草稿" /></template>
    </el-table>
    <div class="list-pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>

  <el-dialog v-model="draftDialog" title="AI 规则草稿" width="min(720px, 94vw)">
    <el-input v-model="prompt" type="textarea" :rows="6" placeholder="例如：A 区烟感温度超过 80 度且烟雾为 true，触发高级别火警。" />
    <el-button class="top-gap" type="primary" :loading="drafting" @click="createDraft">生成草稿</el-button>
    <el-alert v-if="draftError" class="top-gap" type="error" :closable="false" show-icon title="规则草稿生成失败" :description="draftError" />
    <el-card v-if="draft" shadow="never" class="inner-card top-gap">
      <el-descriptions :column="1">
        <el-descriptions-item label="规则名称">{{ draft.name || '未命名' }}</el-descriptions-item>
        <el-descriptions-item label="规则含义">{{ draft.description || 'AI 未提供说明，请在编辑页补充。' }}</el-descriptions-item>
        <el-descriptions-item label="告警类型">{{ alarmType(draft.alarmType) }}</el-descriptions-item>
        <el-descriptions-item label="告警等级">{{ label(alarmLevels, draft.level, '未设置') }}</el-descriptions-item>
        <el-descriptions-item label="启用状态">待人工确认</el-descriptions-item>
      </el-descriptions>
      <el-alert class="top-gap" title="JSON 不支持标准注释" description="可执行 JSON 保持纯净；字段含义、条件运算符和 Gengine 替代写法在下面单独展示，避免把说明误当成运行字段。" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="top-gap">
        <el-form-item label="AI 生成的 JSON 规则"><el-input :model-value="draftPresentation?.json || pretty(draft)" type="textarea" :rows="12" readonly /></el-form-item>
        <el-form-item label="可选 Gengine 表达式（默认注释展示，不会自动启用）"><el-input :model-value="draftPresentation?.genginePlaceholder || '// 载入编辑器后查看等价 Gengine 表达式'" type="textarea" :rows="5" readonly /></el-form-item>
      </el-form>
      <div class="rule-help-title">字段说明</div>
      <el-table :data="draftPresentation?.fieldDescriptions || fieldDescriptions" size="small" border class="top-gap">
        <el-table-column prop="field" label="字段" width="210" />
        <el-table-column prop="meaning" label="含义" min-width="300" />
        <el-table-column prop="example" label="示例" min-width="180" />
      </el-table>
      <el-button @click="useDraft">载入草稿并编辑</el-button>
    </el-card>
    <template #footer><el-button @click="draftDialog=false">关闭</el-button></template>
  </el-dialog>

  <el-dialog v-model="dialog" :title="readonly ? `规则详情 · ${form.name}` : (form.id ? `编辑规则 · ${form.name}` : '手动添加规则')" width="min(760px, 94vw)">
    <el-form :model="form" label-position="top" :disabled="readonly">
      <el-form-item label="规则名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="规则说明"><el-input v-model="form.description" type="textarea" :rows="2" placeholder="说明这条规则的触发含义和现场处置目的，便于后续复核。" /></el-form-item>
      <div class="form-grid">
        <el-form-item label="告警类型"><el-select v-model="form.alarmType"><el-option v-for="(text,key) in alarmTypes" :key="key" :label="text" :value="key" /></el-select></el-form-item>
        <el-form-item label="告警等级"><el-select v-model="form.level"><el-option v-for="(text,key) in alarmLevels" :key="key" :label="text" :value="key" /></el-select></el-form-item>
        <el-form-item label="所属产品（可选）"><el-select v-model="form.productId" clearable><el-option v-for="x in products" :key="x.id" :label="x.name" :value="x.id" /></el-select></el-form-item>
        <el-form-item label="条件关系"><el-select v-model="form.match"><el-option label="全部满足" value="all" /><el-option label="任一满足" value="any" /></el-select></el-form-item>
      </div>
      <el-alert title="当前默认使用 JSON 条件" description="AI 草稿会同时生成 Gengine，但只以注释形式放在下面的占位文本中；只有人工把表达式填入后，运行时才会优先执行 Gengine。" type="info" :closable="false" show-icon class="rule-help-alert" />
      <el-form-item label="Gengine 表达式（可选，填入后优先执行）"><el-input v-model="form.expression" type="textarea" :rows="4" :placeholder="form.genginePlaceholder || '例如：Properties[temperature] > 80 && Properties[smoke] == true'" /></el-form-item>
      <el-form-item label="触发条件 JSON"><el-input v-model="form.conditions" type="textarea" :rows="6" placeholder='[{"field":"temperature","operator":">","value":80}]' /></el-form-item>
      <el-form-item label="恢复条件 JSON"><el-input v-model="form.recovery" type="textarea" :rows="4" placeholder='[{"field":"temperature","operator":"<","value":70}]' /></el-form-item>
      <el-form-item label="联动动作 JSON"><el-input v-model="form.actions" type="textarea" :rows="4" placeholder='[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]' /><small>仅支持 OPEN_CAMERA（已登记摄像头）和 OPEN_PAGE（平台页面），保存前会由服务端校验。</small></el-form-item>
      <div class="form-grid"><el-form-item label="持续秒数"><el-input-number v-model="form.durationSeconds" :min="0" /></el-form-item><el-form-item label="保存后状态"><el-switch v-model="form.enabled" active-text="立即启用" inactive-text="保存为草稿" /></el-form-item></div>
      <div class="rule-help-title">字段说明</div>
      <el-table :data="fieldDescriptions" size="small" border class="top-gap">
        <el-table-column prop="field" label="字段" width="210" />
        <el-table-column prop="meaning" label="含义" min-width="300" />
        <el-table-column prop="example" label="示例" min-width="180" />
      </el-table>
    </el-form>
    <template #footer><el-button v-if="readonly" type="primary" @click="startEdit">编辑</el-button><el-button @click="dialog=false">关闭</el-button><el-button v-if="!readonly" type="primary" @click="save">保存规则</el-button></template>
  </el-dialog>
</template>

<style scoped>
.rule-help-title { margin-top: 16px; color: var(--ink); font-size: 13px; font-weight: 700; }
.rule-help-alert { margin: 2px 0 14px; }
:deep(.el-table) { width: 100%; }
</style>
