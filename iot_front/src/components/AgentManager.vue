<script setup>
// 智能体管理：内置智能体只读查看；自定义智能体用表单新建、编辑、启停和删除。
// 字段上限与工具白名单以服务端校验为准，白名单由管理接口返回。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmDelete } from '../deleteAction'
import { can } from '../permissions'
import { toolName } from '../presentation'
import { UiMessage } from '../ui/feedback.js'
import DataTableCard from './layout/DataTableCard.vue'
import RowActions from './layout/RowActions.vue'

const emit = defineEmits(['changed'])

const items = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const loadError = ref('')
const allowedTools = ref([])
const builtinIds = ref([])
const loader = useListLoader(loading)

const blank = () => ({
  schemaVersion: 1,
  id: '',
  name: '',
  description: '',
  version: '1.0.0',
  enabled: true,
  persona: '',
  defaultModel: 'deepseek-flash',
  maxTokens: 4096,
  capabilities: [],
  allowedTools: []
})
const form = reactive(blank())
const editorVisible = ref(false)
const editingId = ref('')
const readonly = ref(false)
const saving = ref(false)
const formErrors = ref([])
// 首次保存后按输入实时更新字段提示，之前不打扰填写。
const fieldErrors = ref({})
const checked = ref(false)
const manifestText = ref('')
const manifestError = ref('')

const editorTitle = computed(() => (readonly.value ? '查看内置智能体' : editingId.value ? '编辑智能体' : '新建智能体'))
const toolOptions = computed(() => allowedTools.value.map(id => ({ value: id, label: toolName(id) })))

function isBuiltin(item) {
  return builtinIds.value.includes(item?.id)
}

async function load() {
  try {
    const result = await loader.run(signal => api(`/api/v1/ai/workflows/admin?page=${page.value}&pageSize=${pageSize.value}`, { signal }))
    items.value = Array.isArray(result?.items) ? result.items : []
    total.value = Number(result?.total ?? items.value.length)
    allowedTools.value = Array.isArray(result?.allowedTools) ? result.allowedTools : []
    builtinIds.value = Array.isArray(result?.builtinIds) ? result.builtinIds : []
    loadError.value = ''
  } catch (error) {
    if (!isAbort(error)) loadError.value = error?.message || '智能体清单读取失败'
  }
}

function fill(manifest) {
  Object.assign(form, blank(), JSON.parse(JSON.stringify(manifest)))
  form.capabilities = Array.isArray(form.capabilities) ? form.capabilities : []
  form.allowedTools = Array.isArray(form.allowedTools) ? form.allowedTools : []
  manifestText.value = JSON.stringify(manifestFromForm(), null, 2)
  manifestError.value = ''
  formErrors.value = []
  fieldErrors.value = {}
  checked.value = false
}

function openEditor(item = null, view = false) {
  readonly.value = view
  editingId.value = item && !view ? item.id : ''
  fill(item || blank())
  editorVisible.value = true
}

function manifestFromForm() {
  return {
    schemaVersion: 1,
    id: form.id.trim(),
    name: form.name.trim(),
    description: form.description.trim(),
    version: form.version.trim(),
    enabled: Boolean(form.enabled),
    persona: form.persona.trim(),
    defaultModel: form.defaultModel.trim(),
    maxTokens: Number(form.maxTokens),
    capabilities: form.capabilities.map(item => String(item).trim()).filter(Boolean),
    allowedTools: [...form.allowedTools]
  }
}

// 与服务端规则一致的提前检查，按字段返回提示，显示在对应输入框下方。
function validate(manifest) {
  const errors = {}
  const length = value => [...value].length
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(manifest.id))
    errors.id = '以字母或数字开头，只含字母、数字、点、下划线、冒号和连字符，最长 128 字符'
  else if (builtinIds.value.includes(manifest.id) && !editingId.value) errors.id = '与内置智能体重复'
  if (!manifest.name || length(manifest.name) > 128) errors.name = '必填，最长 128 字符'
  if (!manifest.description || length(manifest.description) > 1024) errors.description = '必填，最长 1024 字符'
  if (!manifest.version || length(manifest.version) > 64) errors.version = '必填，最长 64 字符'
  if (!manifest.persona || length(manifest.persona) > 16384) errors.persona = '必填，最长 16384 字符'
  if (!/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(manifest.defaultModel)) errors.defaultModel = '模型标识无效'
  if (!Number.isInteger(manifest.maxTokens) || manifest.maxTokens < 1 || manifest.maxTokens > 8192) errors.maxTokens = '需在 1–8192 之间'
  if (!manifest.capabilities.length || manifest.capabilities.length > 32) errors.capabilities = '需填写 1–32 项'
  else if (manifest.capabilities.some(item => length(item) > 64)) errors.capabilities = '每项最长 64 字符'
  else if (new Set(manifest.capabilities).size !== manifest.capabilities.length) errors.capabilities = '不能重复'
  if (!manifest.allowedTools.length || manifest.allowedTools.length > 6) errors.allowedTools = '需选择 1–6 项'
  return errors
}

watch(
  form,
  () => {
    if (checked.value) fieldErrors.value = validate(manifestFromForm())
  },
  { deep: true }
)

function applyManifestText() {
  try {
    const parsed = JSON.parse(manifestText.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object')
    if (editingId.value && parsed.id !== editingId.value) {
      manifestError.value = '编辑时不能修改智能体标识'
      return
    }
    fill(parsed)
  } catch {
    manifestError.value = 'JSON 格式不正确，请检查括号、引号和逗号'
  }
}

async function save() {
  if (saving.value || readonly.value) return
  const manifest = manifestFromForm()
  formErrors.value = []
  fieldErrors.value = validate(manifest)
  checked.value = true
  if (Object.keys(fieldErrors.value).length) return
  saving.value = true
  try {
    const saved = editingId.value
      ? await api(`/api/v1/ai/workflows/${encodeURIComponent(editingId.value)}`, { method: 'PUT', body: JSON.stringify(manifest) })
      : await api('/api/v1/ai/workflows', { method: 'POST', body: JSON.stringify(manifest) })
    editorVisible.value = false
    UiMessage.success(`${editingId.value ? '智能体已更新' : '智能体已创建'}：${saved?.name || manifest.name}`)
    await load()
    emit('changed', saved?.id || manifest.id)
  } catch (error) {
    formErrors.value = [error?.message || '保存失败']
  } finally {
    saving.value = false
  }
}

const toggling = ref('')
async function toggle(item) {
  if (toggling.value) return
  toggling.value = item.id
  try {
    await api(`/api/v1/ai/workflows/${encodeURIComponent(item.id)}`, {
      method: 'PUT',
      body: JSON.stringify({ ...item, enabled: item.enabled === false })
    })
    UiMessage.success(`${item.name}已${item.enabled === false ? '启用' : '停用'}`)
    await load()
    emit('changed')
  } catch (error) {
    loadError.value = error?.message || '状态更新失败'
  } finally {
    toggling.value = ''
  }
}

function remove(item) {
  return confirmDelete({
    label: item.name || item.id,
    path: `/api/v1/ai/workflows/${encodeURIComponent(item.id)}`,
    warning: '删除后不能再选择该智能体，已有对话记录保留在浏览器中。',
    onDeleted: async () => {
      await load()
      emit('changed')
    }
  })
}

function rowActions(item) {
  if (isBuiltin(item)) return [{ key: 'view', label: '查看', onClick: () => openEditor(item, true) }]
  return [
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/ai/workflows/:id', onClick: () => openEditor(item) },
    {
      key: 'toggle',
      label: item.enabled === false ? '启用' : '停用',
      permission: 'PUT /api/v1/ai/workflows/:id',
      loading: toggling.value === item.id,
      onClick: () => toggle(item)
    },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/ai/workflows/:id', onClick: () => remove(item) }
  ]
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

onMounted(load)
</script>

<template>
  <section class="agent-manager">
    <div class="agent-manager__intro">
      <p>
        智能体决定对话时的角色、可用工具与回答方式。内置智能体只读；自定义智能体只能使用平台提供的只读查询工具，规则草稿工具只保存待人工确认的禁用草稿。
      </p>
      <ui-button v-if="can('POST /api/v1/ai/workflows')" type="primary" @click="openEditor()">新建智能体</ui-button>
    </div>
    <DataTableCard
      :title="`智能体 · ${total} 个`"
      :error="loadError"
      :page="page"
      :page-size="pageSize"
      :total="total"
      @retry="load()"
      @update:page="changePage"
      @update:page-size="changePageSize"
    >
      <ui-table :data="items" :loading="loading" empty-text="暂无智能体">
        <ui-table-column label="智能体" min-width="220">
          <template #default="{ row }">
            <strong>{{ row.name || row.id }}</strong>
            <small class="agent-manager__id">{{ row.id }}{{ row.version ? ` · v${row.version}` : '' }}</small>
          </template>
        </ui-table-column>
        <ui-table-column label="说明" min-width="260" show-overflow-tooltip>
          <template #default="{ row }">{{ row.description || '—' }}</template>
        </ui-table-column>
        <ui-table-column label="可用工具" min-width="220">
          <template #default="{ row }">{{ (row.allowedTools || []).map(toolName).join('、') || '—' }}</template>
        </ui-table-column>
        <ui-table-column label="状态" width="120">
          <template #default="{ row }">
            <ui-tag size="small" :type="row.enabled === false ? 'info' : 'success'" effect="plain">{{
              row.enabled === false ? '已停用' : '已启用'
            }}</ui-tag>
            <ui-tag v-if="isBuiltin(row)" size="small" effect="plain">内置</ui-tag>
          </template>
        </ui-table-column>
        <ui-table-column label="操作" fixed="right" width="170" align="right">
          <template #default="{ row }"><RowActions :actions="rowActions(row)" /></template>
        </ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog v-model="editorVisible" :title="editorTitle" width="min(760px, 94vw)" :close-on-click-modal="false" destroy-on-close>
      <ui-alert v-if="formErrors.length" type="error" :closable="false" :title="formErrors.join('；')" />
      <ui-form label-position="top" class="agent-form" :disabled="readonly || saving">
        <div class="agent-form__grid">
          <ui-form-item label="名称" required :error="fieldErrors.name"
            ><ui-input v-model="form.name" maxlength="128" placeholder="例如 巡检值班助手"
          /></ui-form-item>
          <ui-form-item label="标识" required :error="fieldErrors.id">
            <ui-input v-model="form.id" maxlength="128" :disabled="Boolean(editingId) || readonly" placeholder="例如 duty-assistant" />
          </ui-form-item>
        </div>
        <ui-form-item label="说明" required :error="fieldErrors.description">
          <ui-input v-model="form.description" type="textarea" :rows="2" maxlength="1024" placeholder="这个智能体适合回答哪些问题" />
        </ui-form-item>
        <ui-form-item label="角色提示词" required :error="fieldErrors.persona">
          <ui-input
            v-model="form.persona"
            type="textarea"
            :rows="7"
            maxlength="16384"
            show-count
            placeholder="描述角色、回答原则以及何时调用哪个工具，例如：回答统计问题前必须调用“查询系统概况”；只依据工具结果回答，使用简洁中文。"
          />
        </ui-form-item>
        <ui-form-item label="可用工具（1–6 项）" required :error="fieldErrors.allowedTools">
          <ui-select v-model="form.allowedTools" multiple filterable placeholder="选择只读查询工具">
            <ui-option v-for="tool in toolOptions" :key="tool.value" :value="tool.value" :label="tool.label" />
          </ui-select>
        </ui-form-item>
        <ui-form-item label="能力标签（展示给使用者，1–32 项）" required :error="fieldErrors.capabilities">
          <ui-select
            v-model="form.capabilities"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="输入后按回车，例如 设备状态查询"
          />
        </ui-form-item>
        <div class="agent-form__grid agent-form__grid--three">
          <ui-form-item label="版本" :error="fieldErrors.version"><ui-input v-model="form.version" maxlength="64" /></ui-form-item>
          <ui-form-item label="单次最大输出词元" :error="fieldErrors.maxTokens"
            ><ui-input-number v-model="form.maxTokens" :min="1" :max="8192"
          /></ui-form-item>
          <ui-form-item label="启用"><ui-switch v-model="form.enabled" /></ui-form-item>
        </div>
        <ui-form-item label="默认模型标识" :error="fieldErrors.defaultModel">
          <ui-input v-model="form.defaultModel" maxlength="128" />
          <small class="agent-form__hint">实际运行使用“模型管理”中生效的模型，此处仅作为清单记录。</small>
        </ui-form-item>
        <ui-collapse>
          <ui-collapse-item title="高级：JSON 清单（导入或导出）" name="json">
            <ui-input v-model="manifestText" type="textarea" :rows="10" spellcheck="false" class="agent-form__json" />
            <ui-alert v-if="manifestError" :title="manifestError" type="warning" :closable="false" />
            <ui-button v-if="!readonly" size="small" class="agent-form__apply" @click="applyManifestText">应用到表单</ui-button>
          </ui-collapse-item>
        </ui-collapse>
      </ui-form>
      <template #footer>
        <ui-button @click="editorVisible = false">{{ readonly ? '关闭' : '取消' }}</ui-button>
        <ui-button
          v-if="!readonly"
          v-permission="['POST /api/v1/ai/workflows', 'PUT /api/v1/ai/workflows/:id']"
          type="primary"
          :loading="saving"
          @click="save"
          >{{ editingId ? '保存修改' : '创建智能体' }}</ui-button
        >
      </template>
    </ui-dialog>
  </section>
</template>

<style scoped>
.agent-manager {
  display: grid;
  gap: var(--space-4);
}
.agent-manager__intro {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
}
.agent-manager__intro p {
  margin: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}
.agent-manager__id {
  display: block;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.agent-form__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 0 var(--space-3);
}
.agent-form__grid--three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.agent-form__hint {
  display: block;
  margin-top: var(--space-1);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.agent-form__json :deep(textarea) {
  font-family: var(--font-mono, monospace);
  font-size: var(--font-size-xs);
}
.agent-form__apply {
  margin-top: var(--space-2);
}
@media (max-width: 767px) {
  .agent-manager__intro {
    flex-direction: column;
  }
  .agent-form__grid,
  .agent-form__grid--three {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
