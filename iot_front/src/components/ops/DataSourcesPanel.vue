<script setup>
// Grafana 数据源管理：平台只创建和编辑 Prometheus / Loki 数据源；部署配置（provisioning）
// 管理的数据源只读。密码与请求头的值从不回显。
import { computed, onMounted, ref } from 'vue'
import { Plus, RefreshCw, Trash2 } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import StatusDot from '../layout/StatusDot.vue'
import SecretField from './SecretField.vue'

const emit = defineEmits(['changed'])
const items = ref([])
const loading = ref(false)
const error = ref('')
const testing = ref('')
const results = ref({})
const dialogVisible = ref(false)
const form = ref(null)
const saving = ref(false)
const formError = ref('')
const canView = computed(() => can('GET /api/v1/ops/datasources/:uid'))
const canCreate = computed(() => can('POST /api/v1/ops/datasources'))
const canEdit = computed(() => can('PUT /api/v1/ops/datasources/:uid'))
const canDelete = computed(() => can('DELETE /api/v1/ops/datasources/:uid'))
const canTest = computed(() => can('POST /api/v1/ops/datasources/:uid/test'))

async function load() {
  loading.value = true
  try { items.value = (await opsGet('/api/v1/ops/datasources')).items || []; error.value = '' } catch (e) { error.value = opsErrorText(e) } finally { loading.value = false }
}
async function test(ds) {
  testing.value = ds.uid
  try {
    const result = await opsSend('POST', `/api/v1/ops/datasources/${encodeURIComponent(ds.uid)}/test`)
    results.value = { ...results.value, [ds.uid]: result }
  } catch (e) { results.value = { ...results.value, [ds.uid]: { status: 'error', message: opsErrorText(e) } } } finally { testing.value = '' }
}
function headersOf(ds) {
  return Object.entries(ds.jsonData || {}).filter(([key]) => key.startsWith('httpHeaderName')).sort(([a], [b]) => Number(a.slice(14)) - Number(b.slice(14))).map(([key, name]) => ({ name, value: { set: Boolean(ds.secureFields?.[`httpHeaderValue${key.slice(14)}`]) } }))
}
async function edit(ds) {
  formError.value = ''
  if (!ds) {
    form.value = { uid: '', type: 'prometheus', name: '', url: '', isDefault: false, basicAuth: false, basicAuthUser: '', basicAuthPassword: { set: false, mode: 'replace' }, headers: [], timeInterval: '', queryTimeout: '', httpMethod: 'POST', maxLines: null, tlsSkipVerify: false }
    dialogVisible.value = true
    return
  }
  try {
    const full = await opsGet(`/api/v1/ops/datasources/${encodeURIComponent(ds.uid)}`)
    const j = full.jsonData || {}
    form.value = { uid: full.uid, type: full.type, name: full.name, url: full.url || '', isDefault: full.isDefault, readOnly: full.readOnly, basicAuth: Boolean(full.basicAuth), basicAuthUser: full.basicAuthUser || '', basicAuthPassword: { set: Boolean(full.secureFields?.basicAuthPassword) }, headers: headersOf(full), timeInterval: j.timeInterval || '', queryTimeout: j.queryTimeout || '', httpMethod: j.httpMethod || 'POST', maxLines: j.maxLines ? Number(j.maxLines) : null, tlsSkipVerify: Boolean(j.tlsSkipVerify) }
    dialogVisible.value = true
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
async function save() {
  const f = form.value
  saving.value = true
  formError.value = ''
  const body = { ...f, maxLines: Number(f.maxLines) || 0, headers: f.headers.map(h => ({ name: h.name.trim(), value: { mode: h.value.mode || (h.value.set ? 'keep' : 'replace'), value: h.value.value || '' } })), basicAuthPassword: { mode: f.basicAuthPassword.mode || (f.basicAuthPassword.set ? 'keep' : 'replace'), value: f.basicAuthPassword.value || '' } }
  try {
    if (f.uid) await opsSend('PUT', `/api/v1/ops/datasources/${encodeURIComponent(f.uid)}`, body)
    else await opsSend('POST', '/api/v1/ops/datasources', body)
    UiMessage.success('数据源已保存')
    dialogVisible.value = false
    await load()
    emit('changed')
  } catch (e) { formError.value = opsErrorText(e) } finally { saving.value = false }
}
async function remove(ds) {
  try { await UiMessageBox.confirm(`删除数据源“${ds.name}”？使用它的仪表盘面板将无法查询。`, '删除数据源', { confirmButtonText: '删除' }) } catch { return }
  try {
    await opsSend('DELETE', `/api/v1/ops/datasources/${encodeURIComponent(ds.uid)}`)
    UiMessage.success('数据源已删除')
    await load()
    emit('changed')
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
onMounted(load)
defineExpose({ load })
</script>

<template>
  <div class="ds-panel">
    <div class="ds-panel__toolbar">
      <p>平台通过 Grafana 查询数据源；这里只能创建 Prometheus 与 Loki 数据源，其他类型只读显示。</p>
      <div>
        <ui-button size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-if="canCreate" size="small" type="primary" @click="edit(null)"><Plus />新建数据源</ui-button>
      </div>
    </div>
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <ui-table :data="items" size="small" row-key="uid" empty-text="暂无数据源">
      <ui-table-column label="名称" min-width="180"><template #default="{ row }"><strong>{{ row.name }}</strong><ui-tag v-if="row.isDefault" size="small" class="tag-gap">默认</ui-tag></template></ui-table-column>
      <ui-table-column label="类型" width="110"><template #default="{ row }">{{ row.type }}</template></ui-table-column>
      <ui-table-column v-if="canView" label="地址" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.url || '—' }}</template></ui-table-column>
      <ui-table-column label="管理方式" width="120"><template #default="{ row }"><ui-tag size="small" :type="row.readOnly ? 'info' : undefined">{{ row.readOnly ? '部署配置' : '平台 / Grafana' }}</ui-tag></template></ui-table-column>
      <ui-table-column label="平台支持" width="100"><template #default="{ row }"><StatusDot :tone="row.supported ? 'success' : 'neutral'" :label="row.supported ? '支持' : '不支持'" /></template></ui-table-column>
      <ui-table-column label="连接测试" min-width="200">
        <template #default="{ row }">
          <span v-if="results[row.uid]" :class="results[row.uid].status === 'OK' ? 'ok' : 'bad'">{{ results[row.uid].status === 'OK' ? '正常' : '失败' }}：{{ results[row.uid].message }}</span>
          <span v-else class="muted">—</span>
        </template>
      </ui-table-column>
      <ui-table-column label="操作" width="170">
        <template #default="{ row }">
          <div class="table-actions table-actions--start">
            <ui-button v-if="canTest" text size="small" :loading="testing === row.uid" @click="test(row)">测试</ui-button>
            <ui-button v-if="canView && canEdit && row.supported && !row.readOnly" text size="small" @click="edit(row)">编辑</ui-button>
            <ui-button v-if="canDelete && !row.readOnly" text size="small" type="danger" @click="remove(row)">删除</ui-button>
          </div>
        </template>
      </ui-table-column>
    </ui-table>

    <ui-dialog v-model="dialogVisible" :title="form?.uid ? '编辑数据源' : '新建数据源'" width="min(640px, 96vw)">
      <div v-if="form" class="ds-form">
        <div class="two">
          <label>类型<ui-select v-model="form.type" :disabled="Boolean(form.uid)" aria-label="类型"><ui-option value="prometheus" label="Prometheus" /><ui-option value="loki" label="Loki" /></ui-select></label>
          <label>名称<ui-input v-model="form.name" maxlength="100" /></label>
        </div>
        <label>地址<ui-input v-model="form.url" :placeholder="form.type === 'loki' ? 'http://loki:3100' : 'http://prometheus:9090'" /><small>Grafana 服务器访问的地址，不能包含账号密码</small></label>
        <ui-checkbox v-model="form.isDefault">设为默认数据源</ui-checkbox>
        <ui-checkbox v-model="form.basicAuth">使用 Basic 认证</ui-checkbox>
        <div v-if="form.basicAuth" class="two">
          <label>用户名<ui-input v-model="form.basicAuthUser" autocomplete="off" /></label>
          <label>密码<SecretField v-model="form.basicAuthPassword" :allow-clear="false" label="密码" /></label>
        </div>
        <div class="headers">
          <span>自定义请求头</span>
          <div v-for="(header, index) in form.headers" :key="index" class="header-row">
            <ui-input v-model="header.name" size="small" placeholder="名称，例如 X-Scope-OrgID" aria-label="请求头名称" />
            <SecretField v-model="header.value" label="请求头值" />
            <ui-button text size="small" aria-label="删除请求头" @click="form.headers.splice(index, 1)"><Trash2 /></ui-button>
          </div>
          <ui-button v-if="form.headers.length < 10" size="small" text type="primary" @click="form.headers.push({ name: '', value: { set: false, mode: 'replace' } })"><Plus />添加请求头</ui-button>
          <small>调整已有请求头的顺序后需要重新填写值。</small>
        </div>
        <div class="two">
          <label v-if="form.type === 'prometheus'">采集间隔<ui-input v-model="form.timeInterval" placeholder="例如 15s，用于 $__rate_interval" /></label>
          <label>查询超时<ui-input v-model="form.queryTimeout" placeholder="例如 60s" /></label>
          <label v-if="form.type === 'prometheus'">HTTP 方法<ui-select v-model="form.httpMethod" aria-label="HTTP 方法"><ui-option value="POST" label="POST" /><ui-option value="GET" label="GET" /></ui-select></label>
          <label v-if="form.type === 'loki'">最大行数<ui-input-number v-model="form.maxLines" :min="0" :max="50000" placeholder="1000" /></label>
        </div>
        <ui-checkbox v-model="form.tlsSkipVerify">跳过 TLS 证书校验（仅用于内网自签名证书）</ui-checkbox>
        <ui-alert v-if="formError" type="error" :title="formError" :closable="false" />
      </div>
      <template #footer><ui-button @click="dialogVisible = false">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.ds-panel { display: grid; gap: var(--space-3); min-width: 0; }
.ds-panel__toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.ds-panel__toolbar p { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.ds-panel__toolbar > div { display: flex; gap: var(--space-2); }
.tag-gap { margin-left: var(--space-2); }
.ok { color: var(--success-text); font-size: var(--font-size-xs); }
.bad { color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.muted { color: var(--text-muted); }
.ds-form { display: grid; gap: var(--space-3); }
.ds-form label, .headers { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.ds-form small { color: var(--text-muted); font-size: var(--font-size-xs); }
.two { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); }
.header-row { display: grid; grid-template-columns: minmax(120px, 0.8fr) minmax(0, 1.4fr) auto; align-items: center; gap: var(--space-2); }
@media (max-width: 767px) {
  .two, .header-row { grid-template-columns: minmax(0, 1fr); }
}
</style>
