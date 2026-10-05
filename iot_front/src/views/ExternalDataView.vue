<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { formatTime, notifyError, pretty, session } from '../api'
import { can, permissionState } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback'
import { externalApi, externalBase } from '../externalDataApi'
import {
  blankEndpoint,
  blankSource,
  cloneExternal,
  externalKinds,
  externalStatuses,
  externalTabs,
  externalViews,
  jsonSample,
  requestFence,
  timeWindow
} from '../externalData'
import ExternalDataEditor from '../components/ExternalDataEditor.vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { usePageState } from '../composables/usePageState.js'
defineEmits(['navigate'])

// 三个视图：接入配置（来源及其接口）、编号对应、运行记录（接收记录 / 拉取任务）。
const view = ref('config')
const runKind = ref('records')
const tab = computed(() => (view.value === 'config' ? 'sources' : view.value === 'bindings' ? 'bindings' : runKind.value))
const items = ref([])
const sources = ref([])
const endpoints = ref([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const sourceId = ref('')
const endpointId = ref('')
const status = ref('')
const loading = ref(false)
const error = ref('')
const busy = ref(false)
const editorOpen = ref(false)
const editorValue = ref(null)
const editorKind = ref('sources')
const detailOpen = ref(false)
const detail = ref(null)
const detailLoading = ref(false)
const detailError = ref('')
const retryCurrentMapping = ref(false)
const operationOpen = ref(false)
const operationEndpoint = ref(null)
const operation = ref('test')
const preview = ref(null)
const previewPage = ref(1)
const previewError = ref('')
const previewLoading = ref(false)
const sample = ref(
  JSON.stringify({ id: 'event-001', deviceId: 'device-001', timestamp: Date.now(), alarmType: 'FIRE', content: '检测到火焰' }, null, 2)
)
const range = ref([Date.now() - 3600000, Date.now()])
// 视图、筛选与页码在刷新或切换菜单后恢复；须在监听视图和筛选变化之前恢复，避免恢复时被当作切换而清空。
usePageState('external-data', { view, runKind, sourceId, endpointId, status, page, pageSize })
const keyOpen = ref(false)
const keyResult = ref(null)
const receiveEndpoint = ref(null)
const fence = requestFence(() => [session.tenant, session.user, session.token, permissionState.accessVersion].join('|'))
let poll = 0,
  listPending = 0
const configTab = computed(() => view.value !== 'runs')
const runKinds = computed(() => ['records', 'jobs'].filter(kind => can(`GET ${externalBase}/${kind}`)))
const visibleTabs = computed(() =>
  Object.entries(externalViews).filter(([key]) =>
    key === 'config'
      ? can(`GET ${externalBase}/sources`)
      : key === 'bindings'
        ? can(`GET ${externalBase}/bindings`)
        : runKinds.value.length > 0
  )
)
const sourceEndpoints = id => endpoints.value.filter(item => item.sourceId === id)
const filteredEndpoints = computed(() => endpoints.value.filter(item => !sourceId.value || item.sourceId === sourceId.value))
const editorPermission = computed(
  () => `${editorValue.value?.id ? 'PUT' : 'POST'} ${externalBase}/${editorKind.value}${editorValue.value?.id ? '/:id' : ''}`
)
const operationTitle = computed(
  () => ({ test: '样例转换测试', 'test-fetch': '真实请求预览', pull: '手动拉取 / 历史补拉' })[operation.value]
)
const previewItems = computed(() => (preview.value?.items || []).slice((previewPage.value - 1) * 20, previewPage.value * 20))
const listQuery = () => ({
  page: page.value,
  pageSize: pageSize.value,
  sourceId: tab.value !== 'sources' ? sourceId.value : '',
  endpointId: !configTab.value ? endpointId.value : '',
  status: !configTab.value ? status.value : ''
})
const endpointKind = id => endpoints.value.find(endpoint => endpoint.id === id)?.kind
const sourceName = id => sources.value.find(source => source.id === id)?.name || id || '—'
const endpointName = id => endpoints.value.find(endpoint => endpoint.id === id)?.name || id || '—'
const receiveAuth = row => row?.auth || sources.value.find(source => source.id === row?.sourceId)?.auth || { type: 'unknown' }
const receiveHint = computed(() => {
  const auth = receiveAuth(receiveEndpoint.value)
  if (!auth.type || auth.type === 'none') return '请求头 X-API-Key 填写此接口的接收密钥，也可使用 Authorization: Bearer。'
  if (auth.type === 'bearer') return '请求头 Authorization: Bearer 填写已配置的认证密钥。'
  if (auth.type === 'api_key')
    return auth.query ? `查询参数 ${auth.query} 填写已配置的认证密钥。` : `请求头 ${auth.header || 'X-API-Key'} 填写已配置的认证密钥。`
  if (auth.type === 'basic') return `使用 HTTP Basic 认证，用户名 ${auth.username || '见来源配置'}，密码使用已配置的认证密钥。`
  if (auth.type === 'hmac')
    return `使用 HMAC-SHA256，签名内容为 Unix 秒时间戳直接拼接原文，请求头 ${auth.timestampHeader || 'X-Timestamp'} 和 ${auth.header || 'X-Signature'} 分别传入时间戳与十六进制签名。`
  if (auth.type === 'token') return '当前登录获取 Token 仅适用于拉取，请先为推送接口单独配置认证。'
  return '此接口沿用数据来源认证，请在来源配置中核对认证方式。'
})
const stateText = value => externalStatuses[value] || value || '—'
function stateTone(value) {
  return ['FAILED', 'CONFLICT', 'BLOCKED', 'WAITING_BINDING'].includes(value)
    ? 'danger'
    : ['PROCESSED', 'DONE', 'SUCCESS', 'SUCCEEDED', 'COMPLETED'].includes(value)
      ? 'success'
      : ['RUNNING', 'PENDING', 'RETRY'].includes(value)
        ? 'warning'
        : 'neutral'
}
function allowed(method, kind, suffix = '') {
  return can(`${method} ${externalBase}/${kind}${suffix}`)
}
async function load(quiet = false) {
  if (quiet && listPending) return
  if (!allowed('GET', tab.value)) {
    items.value = []
    total.value = 0
    return
  }
  const current = fence.begin('list')
  listPending++
  if (!quiet) loading.value = true
  error.value = ''
  try {
    const result = await externalApi.list(tab.value, listQuery())
    if (!current()) return
    items.value = result.items || []
    total.value = Number(result.total ?? result.count ?? 0)
  } catch (e) {
    if (current()) error.value = e.message || '读取失败，请重试'
  } finally {
    listPending--
    if (current()) loading.value = false
  }
}
async function loadChoices() {
  const current = fence.begin('choices')
  const results = await Promise.allSettled(
    ['sources', 'endpoints'].map(kind =>
      allowed('GET', kind) ? externalApi.list(kind, { page: 1, pageSize: 100 }) : Promise.resolve({ items: [] })
    )
  )
  if (!current()) return
  if (results[0].status === 'fulfilled') sources.value = results[0].value.items || []
  if (results[1].status === 'fulfilled') endpoints.value = results[1].value.items || []
}
function resetList() {
  page.value = 1
  items.value = []
  total.value = 0
  load()
}
function changePage(value) {
  page.value = value
  load()
}
function changeSize(value) {
  pageSize.value = value
  page.value = 1
  load()
}
watch(tab, () => {
  fence.invalidate('list')
  sourceId.value = ''
  endpointId.value = ''
  status.value = ''
  resetList()
})
watch(sourceId, () => {
  endpointId.value = ''
  resetList()
})
watch([endpointId, status], resetList)
watch(detailOpen, value => {
  if (!value) {
    fence.invalidate('detail')
    detail.value = null
  }
})
watch(operationOpen, value => {
  if (!value) {
    fence.invalidate('preview')
    preview.value = null
    previewLoading.value = false
  }
})
watch(editorOpen, value => {
  if (!value) {
    fence.invalidate('save')
    bindingRecord.value = null
  }
})
watch(keyOpen, value => {
  if (!value) keyResult.value = null
})
watch(
  () => permissionState.accessVersion,
  () => {
    fence.invalidate('list')
    fence.invalidate('detail')
    fence.invalidate('preview')
    fence.invalidate('choices')
    fence.invalidate('mutation')
    items.value = []
    sources.value = []
    endpoints.value = []
    detail.value = null
    preview.value = null
    keyResult.value = null
    editorOpen.value = false
    detailOpen.value = false
    operationOpen.value = false
    keyOpen.value = false
    if (!visibleTabs.value.some(([key]) => key === view.value)) view.value = visibleTabs.value[0]?.[0] || 'config'
    if (!runKinds.value.includes(runKind.value)) runKind.value = runKinds.value[0] || 'records'
    load()
    loadChoices()
  }
)
function edit(row = null, kind = tab.value, defaults = {}) {
  if (!allowed(row ? 'PUT' : 'POST', kind, row ? '/:id' : '')) return
  editorKind.value = kind
  editorValue.value = row
    ? cloneExternal(row)
    : kind === 'sources'
      ? blankSource(session.user)
      : kind === 'endpoints'
        ? blankEndpoint(defaults.sourceId || '')
        : { id: '', revision: 0, sourceId: sourceId.value, externalId: '', kind: 'device', targetId: '', ...defaults }
  editorOpen.value = true
}
// 待关联的接收记录：按记录的来源与对象编号预填编号对应，保存后自动重试该记录。
const bindingRecord = ref(null)
function bindRecord(row) {
  const objectId = row.body?.event?.objectId
  if (!objectId || !allowed('POST', 'bindings')) return
  bindingRecord.value = row
  edit(null, 'bindings', {
    sourceId: row.sourceId,
    externalId: objectId,
    kind: endpointKind(row.endpointId) === 'video_alarm' ? 'camera' : 'device'
  })
}
async function save(value) {
  if (busy.value || !can(editorPermission.value)) return
  const current = fence.begin('save')
  busy.value = true
  const record = editorKind.value === 'bindings' ? bindingRecord.value : null
  try {
    await externalApi.save(editorKind.value, value)
    if (!current()) return
    editorOpen.value = false
    if (record && allowed('POST', 'records', '/:id/retry')) {
      await externalApi.action('records', record.id, 'retry', { revision: record.revision, useCurrentMapping: false })
      if (!current()) return
      UiMessage.success('编号已关联，记录已重新提交处理')
    } else UiMessage.success('配置已保存')
    await Promise.all([load(), loadChoices()])
  } catch (e) {
    if (current()) notifyError(e)
  } finally {
    busy.value = false
  }
}
async function mutate(action, success) {
  if (busy.value) return
  const current = fence.begin('mutation')
  busy.value = true
  try {
    const result = await action()
    if (!current()) return
    if (success) UiMessage.success(success)
    await Promise.all([load(), loadChoices()])
    return current() ? result : undefined
  } catch (e) {
    if (current()) notifyError(e)
  } finally {
    busy.value = false
  }
}
async function toggle(row, kind = tab.value) {
  if (!allowed('PUT', kind, '/:id')) return
  await mutate(() => externalApi.save(kind, { ...row, enabled: !row.enabled }), row.enabled ? '已停用' : '已启用')
}
async function remove(row, kind = tab.value) {
  if (!allowed('DELETE', kind, '/:id')) return
  const current = fence.begin('confirmation')
  try {
    await UiMessageBox.confirm(`删除“${row.name || row.externalId}”？存在关联配置或任务时需先解除关联。`, '删除确认', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  if (!current() || !allowed('DELETE', kind, '/:id')) return
  await mutate(() => externalApi.remove(kind, row), '已删除')
}
function openOperation(row, mode) {
  if (!allowed('POST', 'endpoints', `/:id/${mode}`)) return
  operationEndpoint.value = row
  operation.value = mode
  preview.value = null
  previewPage.value = 1
  previewError.value = ''
  previewLoading.value = false
  range.value = [Date.now() - 3600000, Date.now()]
  operationOpen.value = true
  fence.invalidate('preview')
}
async function executeOperation() {
  if (previewLoading.value || !allowed('POST', 'endpoints', `/:id/${operation.value}`)) return
  const current = fence.begin('preview')
  previewLoading.value = true
  previewError.value = ''
  preview.value = null
  previewPage.value = 1
  try {
    const body = operation.value === 'test' ? { sample: jsonSample(sample.value) } : timeWindow(range.value)
    const result = await externalApi.action('endpoints', operationEndpoint.value.id, operation.value, body)
    if (!current()) return
    preview.value = result
    if (operation.value === 'pull') {
      UiMessage.success('拉取任务已提交')
      operationOpen.value = false
      view.value = 'runs'
      runKind.value = 'jobs'
      await load()
    }
  } catch (e) {
    if (current()) previewError.value = e.message
  } finally {
    if (current()) previewLoading.value = false
  }
}
async function rotateKey(row) {
  if (!allowed('POST', 'endpoints', '/:id/rotate-key')) return
  const current = fence.begin('confirmation')
  if (row.pushKeySet) {
    try {
      await UiMessageBox.confirm('生成新密钥后旧密钥立即失效，请同步更新对方回调配置。', '轮换接收密钥', {
        confirmButtonText: '生成新密钥',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
  }
  if (!current() || !allowed('POST', 'endpoints', '/:id/rotate-key')) return
  const result = await mutate(() => externalApi.action('endpoints', row.id, 'rotate-key', { revision: row.revision }))
  if (result) {
    receiveEndpoint.value = { ...row, pushKeySet: true }
    keyResult.value = { ...result, receiveUrl: new URL(result.receiveUrl, window.location.origin).href }
    keyOpen.value = true
  }
}
function showReceive(row) {
  receiveEndpoint.value = row
  keyResult.value = {
    receiveUrl: new URL(`/api/external/v1/${encodeURIComponent(session.tenant)}/${encodeURIComponent(row.id)}`, window.location.origin).href
  }
  keyOpen.value = true
}
async function showDetail(row) {
  if (!allowed('GET', 'records', '/:id')) return
  const current = fence.begin('detail')
  detail.value = null
  detailError.value = ''
  detailLoading.value = true
  detailOpen.value = true
  retryCurrentMapping.value = false
  try {
    const result = await externalApi.detail(row.id)
    if (current()) detail.value = result
  } catch (e) {
    if (current()) detailError.value = e.message
  } finally {
    if (current()) detailLoading.value = false
  }
}
async function retry(row, kind = tab.value, useCurrentMapping = false) {
  if (!allowed('POST', kind, '/:id/retry')) return
  const current = fence.begin('confirmation')
  if (useCurrentMapping) {
    try {
      await UiMessageBox.confirm('将使用接口当前的字段规则重新转换此条原文。', '按当前规则重试', {
        confirmButtonText: '重新处理',
        cancelButtonText: '取消'
      })
    } catch {
      return
    }
  }
  if (!current() || !allowed('POST', kind, '/:id/retry')) return
  const result = await mutate(
    () => externalApi.action(kind, row.id, 'retry', { revision: row.revision, ...(kind === 'records' ? { useCurrentMapping } : {}) }),
    '已提交重试'
  )
  if (result && detailOpen.value) showDetail(row)
}
function endpointActions(row) {
  const out = [
    { key: 'edit', label: '编辑', permission: `PUT ${externalBase}/endpoints/:id`, onClick: () => edit(row, 'endpoints') },
    {
      key: 'toggle',
      label: row.enabled ? '停用' : '启用',
      permission: `PUT ${externalBase}/endpoints/:id`,
      onClick: () => toggle(row, 'endpoints')
    },
    { key: 'test', label: '样例测试', permission: `POST ${externalBase}/endpoints/:id/test`, onClick: () => openOperation(row, 'test') }
  ]
  if (row.mode === 'pull')
    out.push(
      {
        key: 'preview',
        label: '请求预览',
        permission: `POST ${externalBase}/endpoints/:id/test-fetch`,
        onClick: () => openOperation(row, 'test-fetch')
      },
      { key: 'pull', label: '立即拉取', permission: `POST ${externalBase}/endpoints/:id/pull`, onClick: () => openOperation(row, 'pull') }
    )
  else out.push({ key: 'key', label: '接收设置', permission: `GET ${externalBase}/endpoints`, onClick: () => showReceive(row) })
  out.push({
    key: 'delete',
    label: '删除',
    type: 'danger',
    permission: `DELETE ${externalBase}/endpoints/:id`,
    onClick: () => remove(row, 'endpoints')
  })
  return out.map(action => ({ ...action, disabled: busy.value }))
}
function rowActions(row) {
  if (configTab.value) {
    const out = [{ key: 'edit', label: '编辑', permission: `PUT ${externalBase}/${tab.value}/:id`, onClick: () => edit(row) }]
    if (tab.value === 'sources')
      (out.unshift({
        key: 'endpoint',
        label: '新增接口',
        permission: `POST ${externalBase}/endpoints`,
        onClick: () => edit(null, 'endpoints', { sourceId: row.id })
      }),
        out.push({
          key: 'toggle',
          label: row.enabled ? '停用' : '启用',
          permission: `PUT ${externalBase}/sources/:id`,
          onClick: () => toggle(row)
        }))
    out.push({
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: `DELETE ${externalBase}/${tab.value}/:id`,
      onClick: () => remove(row)
    })
    return out.map(action => ({ ...action, disabled: busy.value }))
  }
  const actions = []
  if (tab.value === 'records')
    actions.push({ key: 'detail', label: '查看详情', permission: `GET ${externalBase}/records/:id`, onClick: () => showDetail(row) })
  if (tab.value === 'records' && row.status === 'WAITING_BINDING' && row.body?.event?.objectId)
    actions.push({
      key: 'bind',
      label: '关联并重试',
      permission: `POST ${externalBase}/bindings`,
      disabled: busy.value,
      onClick: () => bindRecord(row)
    })
  else if (['FAILED', 'BLOCKED', 'WAITING_BINDING', 'CONFLICT'].includes(row.status))
    actions.push({
      key: 'retry',
      label: '重试',
      permission: `POST ${externalBase}/${tab.value}/:id/retry`,
      disabled: busy.value,
      onClick: () => retry(row)
    })
  return actions
}
async function copy(value) {
  try {
    await navigator.clipboard.writeText(value)
    UiMessage.success('已复制')
  } catch {
    UiMessage.warning('自动复制失败，请选中文本复制')
  }
}
onMounted(() => {
  // 恢复的视图仍有权限时保留，否则回到第一个可用视图。
  if (!visibleTabs.value.some(([key]) => key === view.value)) view.value = visibleTabs.value[0]?.[0] || 'config'
  if (!runKinds.value.includes(runKind.value)) runKind.value = runKinds.value[0] || 'records'
  load()
  loadChoices()
  poll = setInterval(() => {
    if (!configTab.value && !loading.value && !busy.value && !document.hidden) load(true)
  }, 5000)
})
onBeforeUnmount(() => {
  clearInterval(poll)
  fence.dispose()
})
</script>

<template>
  <div class="external-data-view">
    <p class="intro">
      配置一次，持续接收外部系统的数据。先登记来源并在来源下添加接口，测试转换后关联编号，最后启用；待关联的接收记录可直接关联并重试。
    </p>
    <nav class="external-tabs" aria-label="外部数据管理">
      <button
        v-for="[key, label] in visibleTabs"
        :key="key"
        :class="{ active: view === key }"
        :aria-current="view === key ? 'page' : undefined"
        @click="view = key"
      >
        {{ label }}
      </button>
    </nav>
    <FilterBar>
      <ui-radio-group v-if="view === 'runs' && runKinds.length > 1" v-model="runKind" aria-label="记录类型"
        ><ui-radio-button value="records">接收记录</ui-radio-button><ui-radio-button value="jobs">拉取任务</ui-radio-button></ui-radio-group
      >
      <ui-select v-if="tab !== 'sources'" v-model="sourceId" clearable filterable allow-create placeholder="全部来源" class="filter-select"
        ><ui-option v-for="source in sources" :key="source.id" :label="source.name" :value="source.id"
      /></ui-select>
      <ui-select v-if="!configTab" v-model="endpointId" clearable filterable allow-create placeholder="全部接口" class="filter-select"
        ><ui-option v-for="endpoint in filteredEndpoints" :key="endpoint.id" :label="endpoint.name" :value="endpoint.id"
      /></ui-select>
      <ui-select v-if="!configTab" v-model="status" clearable placeholder="全部状态" class="filter-select"
        ><ui-option v-for="(label, value) in externalStatuses" :key="value" :value="value" :label="label"
      /></ui-select>
      <template #actions
        ><ui-button
          :loading="loading"
          @click="
            () => {
              load()
              loadChoices()
            }
          "
          ><RefreshCw />刷新</ui-button
        ><ui-button v-if="configTab" v-permission="`POST ${externalBase}/${tab}`" type="primary" @click="edit()"
          ><Plus />{{ tab === 'sources' ? '新增来源' : '新增编号对应' }}</ui-button
        ></template
      >
    </FilterBar>
    <DataTableCard
      :title="externalTabs[tab]"
      :page="page"
      :page-size="pageSize"
      :total="total"
      :error="error"
      @retry="load"
      @update:page="changePage"
      @update:page-size="changeSize"
    >
      <ui-table v-loading="loading" :data="items">
        <template v-if="tab === 'sources'">
          <ui-table-column type="expand"
            ><template #default="{ row }"
              ><div class="source-endpoints">
                <ui-table :data="sourceEndpoints(row.id)" size="small">
                  <ui-table-column label="接口" min-width="170"
                    ><template #default="{ row: endpoint }"
                      ><b>{{ endpoint.name }}</b
                      ><small class="subline">{{ externalKinds[endpoint.kind] || endpoint.kind }}</small></template
                    ></ui-table-column
                  >
                  <ui-table-column label="接入方式" min-width="170"
                    ><template #default="{ row: endpoint }"
                      >{{ endpoint.mode === 'push' ? '对方推送' : '主动拉取'
                      }}<small class="subline">{{
                        endpoint.mode === 'push'
                          ? !receiveAuth(endpoint).type || receiveAuth(endpoint).type === 'none'
                            ? endpoint.pushKeySet
                              ? '已生成接收密钥'
                              : '待生成接收密钥'
                            : '使用来源 / 接口认证'
                          : endpoint.intervalSeconds
                            ? `每 ${endpoint.intervalSeconds} 秒拉取`
                            : '仅手动拉取'
                      }}</small></template
                    ></ui-table-column
                  >
                  <ui-table-column label="最近接收与处理" min-width="200"
                    ><template #default="{ row: endpoint }"
                      ><template v-if="endpoint.runtime"
                        >接收 {{ formatTime(endpoint.runtime.lastReceivedAt)
                        }}<small class="subline"
                          >待处理 {{ endpoint.runtime.pendingRecords ?? 0 }} · 失败 {{ endpoint.runtime.failedRecords ?? 0 }}</small
                        ><small v-if="endpoint.runtime.lastError" class="failure wrap-text">{{
                          endpoint.runtime.lastError
                        }}</small></template
                      ><span v-else>—</span></template
                    ></ui-table-column
                  >
                  <ui-table-column label="状态" width="100"
                    ><template #default="{ row: endpoint }"
                      ><StatusDot
                        :tone="endpoint.enabled ? 'success' : 'neutral'"
                        :label="endpoint.enabled ? '已启用' : '已停用'" /></template
                  ></ui-table-column>
                  <ui-table-column label="操作" width="240" align="right"
                    ><template #default="{ row: endpoint }"><RowActions :actions="endpointActions(endpoint)" /></template
                  ></ui-table-column>
                  <template #empty><ui-empty description="此来源尚无接口，点击“新增接口”添加推送或拉取接口" /></template>
                </ui-table></div></template
          ></ui-table-column>
          <ui-table-column label="数据来源" min-width="190"
            ><template #default="{ row }"
              ><b>{{ row.name }}</b
              ><small class="subline">{{ row.description || row.id }}</small></template
            ></ui-table-column
          >
          <ui-table-column prop="username" label="执行用户" min-width="130" />
          <ui-table-column label="允许访问" min-width="220"
            ><template #default="{ row }"
              ><span class="wrap-text">{{ row.allowedHosts?.join('、') || '未配置拉取主机' }}</span></template
            ></ui-table-column
          >
          <ui-table-column label="接口" width="90"
            ><template #default="{ row }">{{ sourceEndpoints(row.id).length }} 个</template></ui-table-column
          >
          <ui-table-column label="状态" width="105"
            ><template #default="{ row }"
              ><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template
          ></ui-table-column>
        </template>
        <template v-else-if="tab === 'bindings'">
          <ui-table-column label="来源" min-width="160"
            ><template #default="{ row }">{{ sourceName(row.sourceId) }}</template></ui-table-column
          >
          <ui-table-column prop="externalId" label="外部编号" min-width="180" />
          <ui-table-column label="关联类型" width="110"
            ><template #default="{ row }">{{ row.kind === 'camera' ? '摄像头' : '设备' }}</template></ui-table-column
          >
          <ui-table-column prop="targetId" label="平台编号" min-width="180" />
        </template>
        <template v-else>
          <ui-table-column label="接口 / 时间" min-width="190"
            ><template #default="{ row }"
              ><b>{{ endpointName(row.endpointId) }}</b
              ><small class="subline">{{ formatTime(row.createdAt) }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="状态" width="130"
            ><template #default="{ row }"><StatusDot :tone="stateTone(row.status)" :label="stateText(row.status)" /></template
          ></ui-table-column>
          <ui-table-column v-if="tab === 'records'" label="外部事件 / 对象" min-width="190"
            ><template #default="{ row }"
              >{{ row.body?.event?.id || '待转换' }}<small class="subline">{{ row.body?.event?.objectId || '—' }}</small></template
            ></ui-table-column
          >
          <ui-table-column v-else label="范围与实际进度" min-width="270"
            ><template #default="{ row }"
              >{{ row.body?.manual ? '手动拉取' : '自动拉取' }} · 已读取 {{ row.body?.pages || 0 }} 页 /
              {{ row.body?.received || 0 }} 条<small v-if="row.body?.processed != null || row.body?.failed != null" class="subline"
                >已处理 {{ row.body?.processed ?? '—' }} · 待处理 {{ row.body?.pending ?? '—' }} · 失败 {{ row.body?.failed ?? '—' }}</small
              ><small class="subline">{{ formatTime(row.body?.from) }} — {{ formatTime(row.body?.to) }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="尝试次数" width="105"
            ><template #default="{ row }">{{ row.body?.attempts || 0 }}</template></ui-table-column
          >
          <ui-table-column label="处理结果" min-width="230"
            ><template #default="{ row }"
              ><span class="wrap-text">{{ row.body?.error || row.body?.alarmId || row.body?.messageId || '—' }}</span></template
            ></ui-table-column
          >
        </template>
        <ui-table-column v-if="tab === 'sources'" label="最近接收与处理" min-width="225"
          ><template #default="{ row }"
            ><template v-if="row.runtime"
              ><span>接收 {{ formatTime(row.runtime.lastReceivedAt) }}</span
              ><small class="subline">处理 {{ formatTime(row.runtime.lastProcessedAt) }}</small
              ><small class="subline">待处理 {{ row.runtime.pendingRecords ?? 0 }} · 失败 {{ row.runtime.failedRecords ?? 0 }}</small
              ><small v-if="row.runtime.lastError" class="failure wrap-text" :title="formatTime(row.runtime.lastErrorAt)">{{
                row.runtime.lastError
              }}</small></template
            ><span v-else>—</span></template
          ></ui-table-column
        >
        <ui-table-column label="操作" :width="tab === 'sources' ? 240 : 175" align="right" fixed="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column>
        <template #empty><ui-empty :description="loading ? '正在读取' : '暂无数据'" /></template>
      </ui-table>
    </DataTableCard>

    <ui-dialog
      v-model="editorOpen"
      :title="`${editorValue?.id ? '编辑' : '新增'}${externalTabs[editorKind]}`"
      width="min(880px, 94vw)"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      destroy-on-close
      ><ExternalDataEditor
        v-if="editorValue"
        :value="editorValue"
        :kind="editorKind"
        :sources="sources"
        :saving="busy"
        :permission="editorPermission"
        @save="save"
        @cancel="editorOpen = false"
    /></ui-dialog>
    <ui-dialog v-model="operationOpen" :title="`${operationTitle} · ${operationEndpoint?.name || ''}`" width="min(900px, 94vw)">
      <p class="intro">
        {{
          operation === 'pull' ? '此操作创建正式拉取任务，接收到的数据会进入告警或设备业务。' : '仅预览请求与字段转换结果，不写入正式业务。'
        }}
      </p>
      <ui-form label-position="top"
        ><ui-form-item v-if="operation === 'test'" label="对方报文样例（JSON 对象或数组）"
          ><ui-input v-model="sample" type="textarea" :rows="9" /></ui-form-item
        ><ui-form-item v-else label="拉取时间范围"><ui-date-range v-model="range" /></ui-form-item
      ></ui-form>
      <p v-if="previewError" role="alert" class="failure">{{ previewError }}</p>
      <section v-if="preview" class="preview-result">
        <h3>预览 {{ preview.items?.length || 0 }} 条数据</h3>
        <article v-for="(item, index) in previewItems" :key="index" class="preview-item">
          <StatusDot
            :tone="item.error ? 'danger' : item.filtered ? 'neutral' : 'success'"
            :label="item.error ? '转换失败' : item.filtered ? '已按规则过滤' : '转换成功'"
          />
          <p v-if="item.error" class="failure">{{ item.error }}</p>
          <div class="raw-grid">
            <section>
              <h3>接收原文</h3>
              <pre>{{ pretty(item.raw) }}</pre>
            </section>
            <section>
              <h3>转换结果</h3>
              <pre>{{ pretty(item.event) }}</pre>
            </section>
          </div>
        </article>
        <ui-pagination v-if="preview.items?.length > 20" v-model:current-page="previewPage" :page-size="20" :total="preview.items.length" />
        <ui-empty v-if="!preview.items?.length" description="当前样例或时间范围未提取到数据，请核对列表路径和过滤规则" />
        <details v-if="preview.raw">
          <summary>完整响应原文</summary>
          <pre>{{ pretty(preview.raw) }}</pre>
        </details>
      </section>
      <template #footer
        ><ui-button @click="operationOpen = false">关闭</ui-button
        ><ui-button
          v-permission="`POST ${externalBase}/endpoints/:id/${operation}`"
          type="primary"
          :loading="previewLoading"
          @click="executeOperation"
          >{{ operation === 'pull' ? '开始正式拉取' : '运行测试' }}</ui-button
        ></template
      >
    </ui-dialog>
    <ui-dialog v-model="detailOpen" title="接收记录详情" width="min(1120px, 96vw)">
      <p v-if="detailLoading">正在读取详情…</p>
      <p v-if="detailError" class="failure" role="alert">{{ detailError }}</p>
      <template v-if="detail"
        ><div class="detail-meta">
          <StatusDot :label="stateText(detail.entry?.status)" :tone="stateTone(detail.entry?.status)" /><span
            >接收时间 {{ formatTime(detail.entry?.createdAt) }}</span
          ><span>规则修订 {{ detail.entry?.body?.configRevision }}</span>
        </div>
        <p v-if="detail.entry?.body?.error" class="failure">{{ detail.entry.body.error }}</p>
        <div class="raw-grid">
          <section>
            <h3>接收原文</h3>
            <pre>{{ pretty(detail.entry?.body?.raw) }}</pre>
          </section>
          <section>
            <h3>转换结果</h3>
            <pre>{{ pretty(detail.entry?.body?.event || {}) }}</pre>
          </section>
        </div>
        <details>
          <summary>关联结果与接收信息</summary>
          <pre>{{
            pretty({
              deviceId: detail.entry?.body?.deviceId,
              cameraId: detail.entry?.body?.cameraId,
              messageId: detail.entry?.body?.messageId,
              alarmId: detail.entry?.body?.alarmId,
              receipt: detail.receipt
            })
          }}</pre>
        </details>
        <div
          v-if="
            allowed('POST', 'records', '/:id/retry') && ['FAILED', 'BLOCKED', 'WAITING_BINDING', 'CONFLICT'].includes(detail.entry?.status)
          "
          class="retry-row"
        >
          <ui-checkbox v-model="retryCurrentMapping">使用当前字段规则重新转换</ui-checkbox
          ><ui-button :loading="busy" @click="retry(detail.entry, 'records', retryCurrentMapping)">重试此记录</ui-button>
        </div>
      </template>
    </ui-dialog>
    <ui-dialog v-model="keyOpen" title="推送接收设置" width="min(680px, 94vw)" :close-on-click-modal="!busy" :show-close="!busy">
      <template v-if="keyResult"
        ><p class="intro">将此地址与认证方式交给对方配置回调。接收地址使用当前访问域名。</p>
        <h3>接收地址 · POST</h3>
        <pre>{{ keyResult.receiveUrl }}</pre>
        <ui-button @click="copy(keyResult.receiveUrl)">复制地址</ui-button>
        <h3>认证方式</h3>
        <p class="intro">{{ receiveHint }}</p>
        <template v-if="keyResult.key"
          ><p class="intro">新密钥仅在本次显示，请复制保存；关闭窗口后无法再次查看。</p>
          <pre>{{ keyResult.key }}</pre>
          <ui-button @click="copy(keyResult.key)">复制密钥</ui-button></template
        >
        <ui-button
          v-else-if="!receiveAuth(receiveEndpoint).type || receiveAuth(receiveEndpoint).type === 'none'"
          v-permission="`POST ${externalBase}/endpoints/:id/rotate-key`"
          :loading="busy"
          @click="rotateKey(receiveEndpoint)"
          >{{ receiveEndpoint?.pushKeySet ? '轮换接收密钥' : '生成接收密钥' }}</ui-button
        >
      </template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.external-data-view {
  min-width: 0;
}
.intro {
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1.7;
  margin: 0 0 16px;
}
.external-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 16px;
  overflow: auto;
}
.external-tabs button {
  background: none;
  border: 0;
  border-bottom: 2px solid transparent;
  white-space: nowrap;
  padding: 10px 16px;
  color: var(--text-muted);
  cursor: pointer;
  font: inherit;
  font-size: 13px;
}
.external-tabs .active {
  border-bottom-color: var(--primary);
  color: var(--primary-text);
}
.filter-select {
  width: 190px;
}
.source-endpoints {
  padding: 4px 12px 12px 48px;
  min-width: 0;
}
.subline {
  display: block;
  color: var(--text-muted);
  font-size: 12px;
  margin-top: 4px;
}
.wrap-text {
  overflow-wrap: anywhere;
}
.failure {
  color: var(--danger-text);
  overflow-wrap: anywhere;
}
.raw-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
}
.raw-grid section,
.preview-result {
  min-width: 0;
}
pre {
  color: var(--text);
  max-height: 380px;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--surface-muted, var(--surface));
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.6;
}
h3 {
  font-size: 13px;
  margin: 16px 0 8px;
}
.detail-meta,
.retry-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
  font-size: 12px;
}
.retry-row {
  justify-content: flex-end;
  margin-top: 18px;
}
summary {
  cursor: pointer;
  color: var(--text-muted);
  font-size: 13px;
  margin-top: 16px;
}
@media (max-width: 640px) {
  .raw-grid {
    grid-template-columns: 1fr;
  }
  .filter-select {
    width: 100%;
  }
  .external-tabs button {
    padding: 9px 12px;
  }
  .intro {
    font-size: 12px;
  }
}
</style>
