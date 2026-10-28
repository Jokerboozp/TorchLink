<script setup>
import { computed, ref, watch } from 'vue'
import ExternalAuthForm from './ExternalAuthForm.vue'
import {
  blankAuth,
  cloneExternal,
  eventTargets,
  externalKinds,
  fieldsFromForm,
  fieldsToForm,
  jsonObject,
  jsonValue,
  mappingFields,
  validateEndpoint,
  validateSource,
  fieldError
} from '../externalData'
const props = defineProps({
  value: { type: Object, required: true },
  kind: String,
  sources: { type: Array, default: () => [] },
  saving: Boolean,
  permission: String
})
const emit = defineEmits(['save', 'cancel'])
const form = ref({})
const fields = ref([])
const step = ref(1)
const error = ref('')
// 字段校验失败时提示同时显示在对应输入框下方。
const fieldErrors = ref({})
const hosts = ref('')
const headers = ref('{}')
const query = ref('{}')
const requestBody = ref('{}')
const tokenBody = ref('{}')
const responseBody = ref('{}')
const mappingExtra = ref('{}')
const overrideAuth = ref(false)
const steps = ['接口与请求', '字段转换', '运行设置']
const sourceOptions = computed(() =>
  props.sources.some(source => source.id === form.value.sourceId) || !form.value.sourceId
    ? props.sources
    : [{ id: form.value.sourceId, name: form.value.sourceId }, ...props.sources]
)
watch(
  () => props.value,
  value => {
    form.value = cloneExternal(value)
    error.value = ''
    step.value = 1
    hosts.value = (value.allowedHosts || []).join('\n')
    fields.value = fieldsToForm(value.mapping?.fields)
    headers.value = JSON.stringify(value.headers || {}, null, 2)
    query.value = JSON.stringify(value.query || {}, null, 2)
    requestBody.value = JSON.stringify(value.requestBody || {}, null, 2)
    tokenBody.value = JSON.stringify(value.auth?.tokenBody || {}, null, 2)
    responseBody.value = JSON.stringify(value.responseBody ?? { success: true }, null, 2)
    const { fields: ignored, itemsPath, ...extra } = value.mapping || {}
    mappingExtra.value = JSON.stringify(extra, null, 2)
    overrideAuth.value = Boolean(value.auth)
  },
  { immediate: true }
)
function setOverride(value) {
  overrideAuth.value = value
  form.value.auth = value ? blankAuth() : null
  tokenBody.value = '{}'
}
function addField() {
  fields.value.push({ target: '', path: '', type: 'string', required: false, valueText: '', defaultText: '', valuesText: '{}' })
}
function changeKind(kind) {
  if (!props.value.id && JSON.stringify(fields.value) === JSON.stringify(fieldsToForm(mappingFields(form.value.kind))))
    fields.value = fieldsToForm(mappingFields(kind))
  form.value.kind = kind
}
function submit() {
  error.value = ''
  fieldErrors.value = {}
  try {
    let value = cloneExternal(form.value)
    if (props.kind === 'sources') {
      value.allowedHosts = hosts.value.split(/[\n,]/)
      if (value.auth.type === 'token') value.auth.tokenBody = jsonObject(tokenBody.value, 'Token 登录请求体')
      value = validateSource(value)
    } else if (props.kind === 'endpoints') {
      value.headers = jsonObject(headers.value, '请求头')
      value.query = jsonObject(query.value, '查询参数')
      value.requestBody = jsonObject(requestBody.value, '请求体')
      value.responseBody = jsonValue(responseBody.value, '推送响应内容')
      value.mapping = {
        ...jsonObject(mappingExtra.value, '高级转换规则'),
        itemsPath: value.mapping.itemsPath,
        fields: fieldsFromForm(fields.value)
      }
      if (!overrideAuth.value) value.auth = null
      else if (value.auth.type === 'token') value.auth.tokenBody = jsonObject(tokenBody.value, 'Token 登录请求体')
      if (value.mode === 'push' && (value.auth || props.sources.find(source => source.id === value.sourceId)?.auth)?.type === 'token')
        throw new Error('登录获取 Token 仅适用于拉取，请为推送接口单独配置默认认证、签名或密钥')
      value = validateEndpoint(value)
    } else {
      value.externalId = value.externalId.trim()
      value.targetId = value.targetId.trim()
      if (!value.sourceId) throw fieldError('sourceId', '请选择数据来源')
      if (!value.externalId) throw fieldError('externalId', '请填写外部系统编号')
      if (!value.targetId) throw fieldError('targetId', '请填写平台编号')
    }
    emit('save', value)
  } catch (e) {
    error.value = e.message
    if (e.field) fieldErrors.value = { [e.field]: e.message }
  }
}
</script>

<template>
  <ui-form :model="form" label-position="top" class="external-editor">
    <template v-if="kind === 'sources'">
      <div class="form-grid">
        <ui-form-item label="来源名称" :error="fieldErrors.name"
          ><ui-input v-model="form.name" placeholder="例如 视频分析平台"
        /></ui-form-item>
        <ui-form-item label="执行用户" :error="fieldErrors.username"
          ><ui-input v-model="form.username" :disabled="!!form.id" placeholder="平台登录用户名"
        /></ui-form-item>
      </div>
      <p class="hint">数据按执行用户的权限和设备范围接入。新来源默认停用，完成接口测试和编号绑定后启用。</p>
      <ui-form-item label="允许访问的主机和端口（每行一个）"
        ><ui-input v-model="hosts" type="textarea" :rows="3" placeholder="api.example.com:443&#10;10.0.0.10:80"
      /></ui-form-item>
      <ui-form-item label="来源请求最小间隔（毫秒，所有接口合计，0 使用默认 1000）" :error="fieldErrors.requestIntervalMillis"
        ><ui-input-number v-model="form.requestIntervalMillis" :min="0" :max="3600000" :step="100"
      /></ui-form-item>
      <p class="hint">
        同一来源所有接口共同遵守此间隔；非零可填 100 至 3600000 毫秒。手动、自动拉取、请求预览与 Token
        登录都计入；对方限流时按其等待时间延后。
      </p>
      <ExternalAuthForm v-model="form.auth" />
      <ui-form-item v-if="form.auth.type === 'token'" label="Token 登录请求体（JSON）"
        ><ui-input v-model="tokenBody" type="textarea" :rows="4"
      /></ui-form-item>
      <!-- v-pre 与 v-if 不能同在一个元素上，否则条件不生效 -->
      <p v-if="form.auth.type === 'token'" class="hint">
        <span v-pre>凭据字段使用 {{ secret }} 引用认证密钥，例如 {"password":"{{ secret }}"}。</span>
      </p>
      <ui-form-item label="说明"><ui-input v-model="form.description" type="textarea" :rows="2" /></ui-form-item>
      <ui-switch v-model="form.enabled" active-text="启用来源" />
    </template>

    <template v-else-if="kind === 'bindings'">
      <p class="hint">将外部系统编号对应到已登记的平台资料。摄像头还需要关联执行用户有权访问的设备。</p>
      <ui-form-item label="数据来源" :error="fieldErrors.sourceId"
        ><ui-select v-model="form.sourceId" :disabled="!!form.id" filterable allow-create
          ><ui-option v-for="source in sourceOptions" :key="source.id" :value="source.id" :label="source.name" /></ui-select
      ></ui-form-item>
      <ui-form-item label="关联类型"
        ><ui-select v-model="form.kind"><ui-option value="device" label="设备" /><ui-option value="camera" label="摄像头" /></ui-select
      ></ui-form-item>
      <ui-form-item label="外部系统编号" :error="fieldErrors.externalId"
        ><ui-input v-model="form.externalId" placeholder="对方报文中的设备或摄像头编号"
      /></ui-form-item>
      <ui-form-item :label="form.kind === 'camera' ? '平台摄像头编号' : '平台设备编号'" :error="fieldErrors.targetId"
        ><ui-input v-model="form.targetId" placeholder="从设备管理或摄像头资料中复制编号"
      /></ui-form-item>
    </template>

    <template v-else>
      <nav class="editor-steps" aria-label="配置步骤">
        <button
          v-for="(label, index) in steps"
          :key="label"
          type="button"
          :aria-current="step === index + 1 ? 'step' : undefined"
          :class="{ active: step === index + 1 }"
          @click="step = index + 1"
        >
          {{ index + 1 }}. {{ label }}
        </button>
      </nav>
      <section v-show="step === 1">
        <div class="form-grid">
          <ui-form-item label="数据来源" :error="fieldErrors.sourceId"
            ><ui-select v-model="form.sourceId" :disabled="!!form.id" filterable allow-create
              ><ui-option v-for="source in sourceOptions" :key="source.id" :value="source.id" :label="source.name" /></ui-select
          ></ui-form-item>
          <ui-form-item label="接口名称" :error="fieldErrors.name"
            ><ui-input v-model="form.name" placeholder="例如 火焰识别告警"
          /></ui-form-item>
          <ui-form-item label="接入方式"
            ><ui-select v-model="form.mode"
              ><ui-option value="push" label="对方主动推送" /><ui-option value="pull" label="平台主动拉取" /></ui-select
          ></ui-form-item>
          <ui-form-item label="数据用途"
            ><ui-select :model-value="form.kind" @update:model-value="changeKind"
              ><ui-option v-for="(label, kind) in externalKinds" :key="kind" :value="kind" :label="label" /></ui-select
          ></ui-form-item>
        </div>
        <template v-if="form.mode === 'pull'">
          <ui-form-item label="请求地址" :error="fieldErrors.url"
            ><ui-input v-model="form.url" placeholder="https://api.example.com/events"
          /></ui-form-item>
          <ui-form-item label="请求方法"
            ><ui-select v-model="form.method"
              ><ui-option value="GET" label="GET" /><ui-option value="POST" label="POST" /><ui-option value="PUT" label="PUT" /></ui-select
          ></ui-form-item>
          <details>
            <summary>请求参数与认证</summary>
            <p v-pre class="hint">
              参数值可使用 {{ from }}、{{ to }}（毫秒）、{{ page }}、{{ pageSize }}、{{ cursor }}。认证密钥填写在认证设置中。
            </p>
            <ui-form-item label="请求头（JSON 对象）"><ui-input v-model="headers" type="textarea" :rows="3" /></ui-form-item>
            <ui-form-item label="查询参数（JSON 对象）"><ui-input v-model="query" type="textarea" :rows="3" /></ui-form-item>
            <ui-form-item v-if="['POST', 'PUT'].includes(form.method)" label="请求体（JSON 对象）"
              ><ui-input v-model="requestBody" type="textarea" :rows="4"
            /></ui-form-item>
            <ui-checkbox :model-value="overrideAuth" @update:model-value="setOverride"
              >为此接口单独配置认证（否则沿用来源认证）</ui-checkbox
            >
            <ExternalAuthForm v-if="overrideAuth" v-model="form.auth" />
            <ui-form-item v-if="overrideAuth && form.auth.type === 'token'" label="Token 登录请求体（JSON）"
              ><ui-input v-model="tokenBody" type="textarea" :rows="4"
            /></ui-form-item>
          </details>
        </template>
        <template v-else>
          <p class="hint">保存后，在接口列表打开“接收设置”，复制地址并按认证方式交给对方配置回调。接口启用后接收正式数据。</p>
          <ui-checkbox :model-value="overrideAuth" @update:model-value="setOverride"
            >为此推送接口单独配置认证（否则沿用来源认证）</ui-checkbox
          >
          <ExternalAuthForm v-if="overrideAuth" v-model="form.auth" />
        </template>
      </section>
      <section v-show="step === 2">
        <ui-form-item label="数据列表路径"
          ><ui-input v-model="form.mapping.itemsPath" placeholder="例如 data.items；单条对象留空"
        /></ui-form-item>
        <p class="hint">路径相对于每条数据。必须提供稳定事件编号、外部对象编号及发生时间；状态和等级可通过枚举转换。</p>
        <article v-for="(field, index) in fields" :key="index" class="field-rule">
          <div class="field-heading">
            <b>字段 {{ index + 1 }}</b
            ><ui-button text type="danger" @click="fields.splice(index, 1)">移除</ui-button>
          </div>
          <div class="form-grid">
            <ui-form-item label="目标字段"
              ><ui-select v-model="field.target" filterable allow-create placeholder="选择字段或输入 data.属性名"
                ><ui-option v-for="(label, target) in eventTargets" :key="target" :value="target" :label="label" /></ui-select
            ></ui-form-item>
            <ui-form-item label="数据类型"
              ><ui-select v-model="field.type"
                ><ui-option value="string" label="文本" /><ui-option value="number" label="数值" /><ui-option
                  value="boolean"
                  label="布尔值" /><ui-option value="timestamp" label="时间" /><ui-option value="json" label="JSON 对象" /></ui-select
            ></ui-form-item>
            <ui-form-item v-if="!field.constant" label="原文字段路径"
              ><ui-input v-model="field.path" placeholder="例如 event.cameraId"
            /></ui-form-item>
            <ui-form-item v-else label="固定值（JSON 值）"
              ><ui-input v-model="field.valueText" placeholder='例如 "FIRE" 或 true'
            /></ui-form-item>
            <ui-form-item v-if="field.type === 'timestamp'" label="时间格式"
              ><ui-select v-model="field.timeFormat" filterable allow-create
                ><ui-option value="milliseconds" label="毫秒时间戳" /><ui-option value="seconds" label="秒时间戳" /><ui-option
                  value="RFC3339"
                  label="RFC3339" /><ui-option value="2006-01-02 15:04:05" label="年-月-日 时:分:秒" /></ui-select
            ></ui-form-item>
          </div>
          <div class="inline-options">
            <ui-checkbox v-model="field.required">必填</ui-checkbox><ui-checkbox v-model="field.constant">使用固定值</ui-checkbox>
          </div>
          <details>
            <summary>默认值、枚举和时区</summary>
            <div class="form-grid">
              <ui-form-item label="默认值（JSON 值）"><ui-input v-model="field.defaultText" placeholder='例如 "HIGH"' /></ui-form-item>
              <ui-form-item v-if="field.type === 'timestamp'" label="时区"
                ><ui-input v-model="field.timezone" placeholder="Asia/Shanghai"
              /></ui-form-item>
            </div>
            <ui-form-item label="枚举映射（JSON 对象）"
              ><ui-input v-model="field.valuesText" type="textarea" :rows="2" placeholder='例如 {"1":"HIGH","2":"MEDIUM"}'
            /></ui-form-item>
          </details>
        </article>
        <ui-button @click="addField">添加字段</ui-button>
        <details>
          <summary>高级转换规则</summary>
          <p class="hint">可配置 successPath、successValue、filters 和 idFields。字段规则由上面的表单管理。</p>
          <ui-input v-model="mappingExtra" type="textarea" :rows="6" />
        </details>
      </section>
      <section v-show="step === 3">
        <template v-if="form.mode === 'pull'">
          <div class="form-grid">
            <ui-form-item label="自动拉取间隔（至少 10 秒，0 为仅手动）" :error="fieldErrors.intervalSeconds"
              ><ui-input-number v-model="form.intervalSeconds" :min="0"
            /></ui-form-item>
            <ui-form-item label="重叠回查时间（秒）"><ui-input-number v-model="form.overlapSeconds" :min="0" /></ui-form-item>
            <ui-form-item label="初始拉取时间"><ui-date-time v-model="form.startAt" clearable /></ui-form-item>
            <ui-form-item label="请求超时（秒）"><ui-input-number v-model="form.timeoutSeconds" :min="1" :max="120" /></ui-form-item>
            <ui-form-item label="最大尝试次数"><ui-input-number v-model="form.maxAttempts" :min="1" :max="20" /></ui-form-item>
            <ui-form-item label="分页方式"
              ><ui-select v-model="form.pagination.mode"
                ><ui-option value="none" label="不分页" /><ui-option value="page" label="页码" /><ui-option
                  value="offset"
                  label="偏移量" /><ui-option value="cursor" label="游标" /></ui-select
            ></ui-form-item>
          </div>
          <p class="hint">每次出站请求同时遵守来源与本接口的最小间隔。分页拉取也会按间隔等待，任务进度保留。</p>
          <div v-if="form.pagination.mode !== 'none'" class="form-grid">
            <ui-form-item label="页码 / 游标参数名"
              ><ui-input v-model="form.pagination.parameter" placeholder="page / cursor / offset"
            /></ui-form-item>
            <ui-form-item label="每页条数参数名"><ui-input v-model="form.pagination.sizeParameter" placeholder="pageSize" /></ui-form-item>
            <ui-form-item label="每页条数"><ui-input-number v-model="form.pagination.pageSize" :min="1" :max="1000" /></ui-form-item>
            <ui-form-item label="起始页码 / 偏移量"><ui-input-number v-model="form.pagination.start" :min="0" /></ui-form-item>
            <ui-form-item label="下一页游标路径"
              ><ui-input v-model="form.pagination.nextPath" placeholder="data.nextCursor"
            /></ui-form-item>
            <ui-form-item label="总条数路径"><ui-input v-model="form.pagination.totalPath" placeholder="data.total" /></ui-form-item>
            <ui-form-item label="单次任务最多页数"
              ><ui-input-number v-model="form.pagination.maxPages" :min="1" :max="10000"
            /></ui-form-item>
          </div>
        </template>
        <template v-else
          ><ui-form-item label="接收成功响应状态码"><ui-input-number v-model="form.responseStatus" :min="200" :max="299" /></ui-form-item
          ><ui-form-item label="接收成功响应内容（JSON）"><ui-input v-model="responseBody" type="textarea" :rows="4" /></ui-form-item
        ></template>
        <ui-switch v-model="form.enabled" active-text="启用接口" />
        <p class="hint">建议先保存为停用状态，测试字段转换，完成编号绑定后再启用。</p>
      </section>
    </template>
    <p v-if="error" class="editor-error" role="alert">{{ error }}</p>
    <footer class="editor-footer">
      <ui-button :disabled="saving" @click="emit('cancel')">取消</ui-button
      ><ui-button v-if="kind === 'endpoints' && step > 1" @click="step--">上一步</ui-button
      ><ui-button v-if="kind === 'endpoints' && step < 3" type="primary" @click="step++">下一步</ui-button
      ><ui-button v-else v-permission="permission" type="primary" :loading="saving" @click="submit">保存配置</ui-button>
    </footer>
  </ui-form>
</template>

<style scoped>
.hint {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.7;
  margin: 8px 0 16px;
}
.editor-steps {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}
.editor-steps button {
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: 6px;
  padding: 8px 12px;
  color: var(--text);
  cursor: pointer;
}
.editor-steps .active {
  color: var(--primary-text);
  background: var(--primary-soft);
  border-color: var(--primary);
}
.field-rule {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  margin: 12px 0;
}
.field-heading,
.inline-options {
  display: flex;
  gap: 16px;
  align-items: center;
}
.field-heading {
  justify-content: space-between;
  margin-bottom: 8px;
}
.field-rule :deep(.n-form-item) {
  margin-bottom: 0;
}
.editor-footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
  position: sticky;
  /* 覆盖弹窗正文的底部内边距，滚动内容不会从按钮下方露出。 */
  bottom: calc(-1 * var(--n-padding-bottom, 0px));
  background: var(--surface);
  padding: 16px 0 var(--n-padding-bottom, 0px);
  margin: 16px 0 calc(-1 * var(--n-padding-bottom, 0px));
  border-top: 1px solid var(--border);
}
.editor-error {
  color: var(--danger-text);
}
details {
  padding: 12px 0;
}
summary {
  cursor: pointer;
  color: var(--text-muted);
  margin-bottom: 12px;
}
.external-editor :deep(.n-input-number),
.external-editor :deep(.n-date-picker) {
  width: 100%;
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: 1fr !important;
  }
  .editor-steps button {
    font-size: 12px;
    padding: 7px;
  }
}
</style>
