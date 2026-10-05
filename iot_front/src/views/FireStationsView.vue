<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmDelete } from '../deleteAction'
import { UiMessage } from '../ui/feedback.js'
import { errorMessage } from '../presentation'
import { can } from '../permissions'
import { stationTypes, dispatchTypes, statusLabel, statusTone, dateTimeLabel } from '../fireSafety'
import {
  managementPayload,
  localDateTimeInput,
  inputTimestamp,
  dispatchPayload,
  fireQuery,
  requiredFieldErrors
} from '../fireSafetyManagement'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
defineEmits(['navigate'])

const tabs = {
  stations: { label: '消防站台账', path: 'fire-stations' },
  personnel: { label: '人员', path: 'fire-personnel' },
  equipment: { label: '器材', path: 'fire-equipment' },
  dispatches: { label: '出勤记录', path: 'fire-dispatches' }
}
const tab = ref('stations'),
  rows = ref([]),
  page = ref(1),
  pageSize = ref(20),
  total = ref(0),
  loading = ref(false),
  loadError = ref('')
const filters = reactive({ q: '', stationId: '', status: '' })
const options = reactive({ stations: [], personnel: [], equipment: [] }),
  optionsError = ref('')
const statistics = ref(null),
  statisticsError = ref(''),
  statisticsLoading = ref(false)
const range = reactive({ from: '', to: '' })
const dialog = ref(false),
  detail = ref(null),
  editKind = ref('stations'),
  form = reactive({}),
  saving = ref(false),
  saveError = ref(''),
  fieldErrors = ref({})
const returning = ref(null),
  returnForm = reactive({ returnedAtInput: '', summary: '' }),
  returnSaving = ref(false),
  returnError = ref('')
const deleteSaving = ref(false)
const equipmentStates = [
  { value: 'ready', label: '可用' },
  { value: 'maintenance', label: '维护中' },
  { value: 'retired', label: '已报废' }
]
const filteredPersonnel = computed(() => options.personnel.filter(item => item.stationId === form.stationId && item.enabled))
const filteredEquipment = computed(() => options.equipment.filter(item => item.stationId === form.stationId && item.status === 'ready'))
const labels = kind => tabs[kind]?.label || ''
const blank = kind =>
  ({
    stations: {
      id: '',
      code: '',
      name: '',
      type: 'micro',
      address: '',
      longitude: 0,
      latitude: 0,
      contact: '',
      phone: '',
      enabled: true,
      notes: ''
    },
    personnel: { id: '', name: '', phone: '', stationId: filters.stationId, position: '', enabled: true, notes: '' },
    equipment: { id: '', stationId: filters.stationId, name: '', category: '', quantity: 1, unit: '件', status: 'ready', notes: '' },
    dispatches: {
      id: '',
      stationId: filters.stationId,
      title: '',
      type: 'fire',
      location: '',
      personnelIds: [],
      equipment: [],
      startedAtInput: localDateTimeInput()
    }
  })[kind]

const loader = useListLoader(loading),
  optionsLoader = useListLoader(),
  statisticsLoader = useListLoader(statisticsLoading)
function rangeQuery() {
  const result = {}
  if (range.from) result.fromAt = inputTimestamp(`${range.from}T00:00`, '开始日期')
  if (range.to) {
    const end = new Date(`${range.to}T00:00`)
    end.setDate(end.getDate() + 1)
    result.toAt = end.getTime()
  }
  if (result.fromAt && result.toAt && result.fromAt >= result.toAt) throw new Error('结束日期不能早于开始日期')
  return result
}
async function load() {
  const current = tab.value
  if (!tabs[current]) {
    loader.cancel()
    return
  }
  loadError.value = ''
  try {
    const result = await loader.run(signal => {
      const query = fireQuery(
        { ...filters, ...(current === 'dispatches' ? rangeQuery() : {}) },
        { page: page.value, pageSize: pageSize.value }
      )
      return api(`/api/v1/${tabs[current].path}?${query}`, { signal })
    })
    rows.value = result.items || []
    total.value = Number(result.total || 0)
  } catch (error) {
    if (isAbort(error)) return
    rows.value = []
    total.value = 0
    loadError.value = errorMessage(error)
  }
}
async function loadOptions() {
  optionsError.value = ''
  try {
    const result = await optionsLoader.run(signal => api('/api/v1/fire-safety/options', { signal }))
    Object.assign(options, { stations: result.stations || [], personnel: result.personnel || [], equipment: result.equipment || [] })
  } catch (error) {
    if (!isAbort(error)) optionsError.value = errorMessage(error)
  }
}
async function loadStatistics() {
  statisticsError.value = ''
  try {
    statistics.value = await statisticsLoader.run(signal =>
      api(`/api/v1/fire-stations/statistics?${fireQuery({ stationId: filters.stationId, ...rangeQuery() })}`, { signal })
    )
  } catch (error) {
    if (isAbort(error)) return
    statistics.value = null
    statisticsError.value = errorMessage(error)
  }
}
async function refresh() {
  await Promise.all([load(), loadOptions(), loadStatistics()])
}
function applyFilters() {
  page.value = 1
  load()
  loadStatistics()
}
watch(tab, () => {
  rows.value = []
  page.value = 1
  filters.status = ''
  load()
})
function changePage(value) {
  page.value = value
  load()
}
function changePageSize(value) {
  pageSize.value = value
  page.value = 1
  load()
}
function stationName(id) {
  return options.stations.find(item => item.id === id)?.name || '已移除消防站'
}
function personName(id) {
  return options.personnel.find(item => item.id === id)?.name || '已移除人员'
}
function equipmentName(id) {
  return options.equipment.find(item => item.id === id)?.name || '已移除器材'
}
function enumName(items, value) {
  return items.find(item => item.value === value)?.label || value
}
function open(kind, row) {
  editKind.value = kind
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form, blank(kind), row ? JSON.parse(JSON.stringify(row)) : {})
  saveError.value = ''
  fieldErrors.value = {}
  dialog.value = true
  loadOptions()
}
function changeDispatchStation() {
  form.personnelIds = []
  form.equipment = []
}
async function save() {
  if (saving.value || !can(`${form.id ? 'PUT' : 'POST'} /api/v1/${tabs[editKind.value].path}${form.id ? '/:id' : ''}`)) return
  saveError.value = ''
  saving.value = true
  try {
    const kind = editKind.value
    fieldErrors.value = requiredFieldErrors(kind, form)
    if (Object.keys(fieldErrors.value).length) return
    const value = kind === 'dispatches' ? dispatchPayload(form) : managementPayload(kind, form)
    await api(`/api/v1/${tabs[kind].path}${form.id ? `/${encodeURIComponent(form.id)}` : ''}`, {
      method: form.id ? 'PUT' : 'POST',
      body: JSON.stringify(value)
    })
    UiMessage.success(kind === 'dispatches' ? '出勤已登记' : '资料已保存')
    dialog.value = false
    if (detail.value?.id === form.id) detail.value = null
    await refresh()
  } catch (error) {
    saveError.value = errorMessage(error)
  } finally {
    saving.value = false
  }
}
async function remove(kind, row) {
  if (deleteSaving.value || !can(`DELETE /api/v1/${tabs[kind].path}/:id`)) return
  deleteSaving.value = true
  try {
    await confirmDelete({
      label: row.name || row.code,
      path: `/api/v1/${tabs[kind].path}/${encodeURIComponent(row.id)}?version=${row.version}`,
      warning: '已关联业务记录的资料不能删除。',
      blockedHint: '已关联业务记录或资料已被他人修改，请刷新后重试。',
      onDeleted: async () => {
        if (rows.value.length === 1 && page.value > 1) page.value--
        await refresh()
      }
    })
  } finally {
    deleteSaving.value = false
  }
}
function openReturn(row) {
  returning.value = row
  Object.assign(returnForm, { returnedAtInput: localDateTimeInput(), summary: '' })
  returnError.value = ''
}
async function saveReturn() {
  if (returnSaving.value || !can('POST /api/v1/fire-dispatches/:id/return')) return
  returnSaving.value = true
  returnError.value = ''
  try {
    if (!returnForm.summary.trim()) throw new Error('请填写归队总结')
    await api(`/api/v1/fire-dispatches/${encodeURIComponent(returning.value.id)}/return`, {
      method: 'POST',
      body: JSON.stringify({
        version: returning.value.version,
        returnedAt: inputTimestamp(returnForm.returnedAtInput, '归队时间'),
        summary: returnForm.summary.trim()
      })
    })
    UiMessage.success('已登记归队')
    returning.value = null
    detail.value = null
    await refresh()
  } catch (error) {
    returnError.value = errorMessage(error)
  } finally {
    returnSaving.value = false
  }
}
function rowActions(row) {
  const path = `/api/v1/${tabs[tab.value].path}`
  if (tab.value === 'dispatches')
    return [
      { key: 'detail', label: '详情', onClick: () => (detail.value = row) },
      {
        key: 'return',
        label: '归队',
        hidden: row.status !== 'dispatched',
        permission: 'POST /api/v1/fire-dispatches/:id/return',
        onClick: () => openReturn(row)
      }
    ]
  return [
    { key: 'edit', label: '编辑', permission: `PUT ${path}/:id`, onClick: () => open(tab.value, row) },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      disabled: deleteSaving.value,
      permission: `DELETE ${path}/:id`,
      onClick: () => remove(tab.value, row)
    }
  ]
}
onMounted(refresh)
</script>

<template>
  <div class="fire-page">
    <div v-if="optionsError" class="fire-error" role="alert">
      <span>{{ optionsError }}</span
      ><ui-button size="small" @click="loadOptions">重新加载选项</ui-button>
    </div>
    <ui-tabs v-model="tab">
      <ui-tab-pane v-for="(item, key) in tabs" :key="key" :name="key" :label="item.label" />
    </ui-tabs>
    <FilterBar>
      <ui-input v-model="filters.q" clearable placeholder="搜索编号、名称或位置" aria-label="搜索关键字" @keyup.enter="applyFilters" />
      <ui-select v-model="filters.stationId" clearable filterable placeholder="全部消防站" aria-label="消防站筛选" @change="applyFilters"
        ><ui-option v-for="item in options.stations" :key="item.id" :value="item.id" :label="item.name"
      /></ui-select>
      <ui-select
        v-if="tab === 'equipment' || tab === 'dispatches'"
        v-model="filters.status"
        clearable
        placeholder="全部状态"
        aria-label="状态筛选"
        @change="applyFilters"
        ><ui-option
          v-for="item in tab === 'equipment'
            ? equipmentStates
            : [
                { value: 'dispatched', label: '已出动' },
                { value: 'returned', label: '已归队' }
              ]"
          :key="item.value"
          :value="item.value"
          :label="item.label"
      /></ui-select>
      <ui-button @click="applyFilters">查询</ui-button>
      <template #actions
        ><ui-button :loading="loading || statisticsLoading" @click="refresh"><RefreshCw />刷新</ui-button
        ><ui-button
          v-if="tabs[tab]"
          v-permission="`POST /api/v1/${tabs[tab].path}`"
          type="primary"
          :disabled="Boolean(optionsError) && tab !== 'stations'"
          @click="open(tab)"
          ><Plus />{{ tab === 'dispatches' ? '登记出勤' : `新增${tab === 'stations' ? '消防站' : labels(tab)}` }}</ui-button
        ></template
      >
    </FilterBar>
    <div v-if="tab === 'dispatches'" class="fire-summary-filter">
      <label>开始日期<input v-model="range.from" type="date" class="fire-date" @change="applyFilters" /></label
      ><label>结束日期<input v-model="range.to" type="date" class="fire-date" @change="applyFilters" /></label>
    </div>
    <div v-if="statisticsError" class="fire-error" role="alert">
      <span>{{ statisticsError }}</span
      ><ui-button size="small" @click="loadStatistics">重新加载统计</ui-button>
    </div>
    <details v-if="statistics?.byStation?.length" class="fire-station-stats">
      <summary>各消防站统计 · {{ statistics.byStation.length }} 个</summary>
      <ui-table :data="statistics?.byStation || []" :loading="statisticsLoading" empty-text="暂无消防站统计"
        ><ui-table-column prop="name" label="消防站" min-width="180" /><ui-table-column
          prop="personnel"
          label="人员"
          width="100" /><ui-table-column prop="equipment" label="在用器材数量" min-width="130" /><ui-table-column
          prop="activeDispatches"
          label="未归队"
          width="100" /><ui-table-column prop="dispatches" label="出勤次数" width="100" /><ui-table-column
          prop="returnedDispatches"
          label="已归队次数"
          min-width="120"
      /></ui-table>
      <p class="fire-hint" style="padding: 0 16px">
        日期范围按出勤开始时间统计出勤次数；未归队为当前全部未归队记录。器材数量包含维护中的器材。
      </p>
    </details>
    <DataTableCard
      :title="`${labels(tab)} · ${total} 条`"
      :page="page"
      :page-size="pageSize"
      :total="total"
      :error="loadError"
      @retry="load"
      @update:page="changePage"
      @update:page-size="changePageSize"
    >
      <ui-table :data="rows" :loading="loading" :empty-text="`暂无${labels(tab)}，可使用上方按钮创建`">
        <template v-if="tab === 'stations'">
          <ui-table-column label="消防站" min-width="200"
            ><template #default="{ row }"
              ><strong>{{ row.name }}</strong
              ><small class="fire-subline">{{ row.code }} · {{ enumName(stationTypes, row.type) }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="地址 / 位置" min-width="230"
            ><template #default="{ row }"
              >{{ row.address || '未填写地址'
              }}<small class="fire-subline">经度 {{ row.longitude }} · 纬度 {{ row.latitude }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="联系信息" min-width="180"
            ><template #default="{ row }"
              >{{ row.contact || '—' }}<small class="fire-subline">{{ row.phone || '—' }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="状态" width="100"
            ><template #default="{ row }"
              ><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template
          ></ui-table-column>
        </template>
        <template v-if="tab === 'personnel'">
          <ui-table-column prop="name" label="姓名" min-width="140" /><ui-table-column label="所属消防站" min-width="180"
            ><template #default="{ row }">{{ stationName(row.stationId) }}</template></ui-table-column
          ><ui-table-column prop="position" label="岗位" min-width="140" /><ui-table-column
            prop="phone"
            label="联系电话"
            min-width="160"
          /><ui-table-column label="状态" width="100"
            ><template #default="{ row }"
              ><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template
          ></ui-table-column>
        </template>
        <template v-if="tab === 'equipment'">
          <ui-table-column label="器材" min-width="180"
            ><template #default="{ row }"
              ><strong>{{ row.name }}</strong
              ><small class="fire-subline">{{ row.category }}</small></template
            ></ui-table-column
          ><ui-table-column label="所属消防站" min-width="180"
            ><template #default="{ row }">{{ stationName(row.stationId) }}</template></ui-table-column
          ><ui-table-column label="数量" min-width="110"
            ><template #default="{ row }">{{ row.quantity }} {{ row.unit }}</template></ui-table-column
          ><ui-table-column label="状态" width="100"
            ><template #default="{ row }"
              ><StatusDot :tone="statusTone(row.status)" :label="statusLabel(row.status)" /></template></ui-table-column
          ><ui-table-column prop="notes" label="备注" min-width="180" show-overflow-tooltip />
        </template>
        <template v-if="tab === 'dispatches'">
          <ui-table-column label="出勤" min-width="200"
            ><template #default="{ row }"
              ><button class="fire-name" type="button" @click="detail = row">{{ row.title }}</button
              ><small class="fire-subline">{{ enumName(dispatchTypes, row.type) }} · {{ stationName(row.stationId) }}</small></template
            ></ui-table-column
          ><ui-table-column prop="location" label="地点" min-width="160" /><ui-table-column label="人员 / 器材" min-width="180"
            ><template #default="{ row }"
              >{{ (row.personnelIds || []).map(personName).join('、')
              }}<small class="fire-subline">{{
                (row.equipment || []).map(item => `${equipmentName(item.equipmentId)} × ${item.quantity}`).join('、') || '未携带器材'
              }}</small></template
            ></ui-table-column
          ><ui-table-column label="时间" min-width="190"
            ><template #default="{ row }"
              >{{ dateTimeLabel(row.startedAt)
              }}<small class="fire-subline">归队：{{ row.returnedAt ? dateTimeLabel(row.returnedAt) : '尚未归队' }}</small></template
            ></ui-table-column
          ><ui-table-column label="状态" width="100"
            ><template #default="{ row }"><StatusDot :tone="statusTone(row.status)" :label="statusLabel(row.status)" /></template
          ></ui-table-column>
        </template>
        <ui-table-column label="操作" width="140" fixed="right" align="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog
      v-model="dialog"
      :title="`${form.id ? '编辑' : '新增'}${editKind === 'stations' ? '消防站' : labels(editKind)}`"
      width="min(720px,94vw)"
      :close-on-click-modal="!saving"
      :show-close="!saving"
      :close-on-press-escape="!saving"
    >
      <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="fire-section" />
      <ui-form :model="form" label-position="top">
        <template v-if="editKind === 'stations'">
          <div class="fire-form-grid">
            <ui-form-item label="消防站编号 *" :error="fieldErrors.code"
              ><ui-input :disabled="saving" v-model="form.code" maxlength="64" /></ui-form-item
            ><ui-form-item label="消防站名称 *" :error="fieldErrors.name"
              ><ui-input :disabled="saving" v-model="form.name" maxlength="100" /></ui-form-item
            ><ui-form-item label="消防站类型"
              ><ui-select :disabled="saving" v-model="form.type"
                ><ui-option
                  v-for="item in stationTypes"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value" /></ui-select></ui-form-item
            ><ui-form-item label="联系人"><ui-input :disabled="saving" v-model="form.contact" /></ui-form-item>
          </div>
          <ui-form-item label="地址"><ui-input :disabled="saving" v-model="form.address" /></ui-form-item>
          <div class="fire-form-grid">
            <ui-form-item label="经度"
              ><ui-input-number :disabled="saving" v-model="form.longitude" :min="-180" :max="180" :precision="6" /></ui-form-item
            ><ui-form-item label="纬度"
              ><ui-input-number :disabled="saving" v-model="form.latitude" :min="-90" :max="90" :precision="6" /></ui-form-item
            ><ui-form-item label="联系电话"><ui-input :disabled="saving" v-model="form.phone" /></ui-form-item
            ><ui-form-item label="状态"><ui-switch :disabled="saving" v-model="form.enabled" active-text="启用" /></ui-form-item>
          </div>
        </template>
        <template v-else>
          <ui-form-item label="所属消防站 *" :error="fieldErrors.stationId"
            ><ui-select
              :disabled="saving"
              v-model="form.stationId"
              filterable
              @change="editKind === 'dispatches' && changeDispatchStation()"
              ><ui-option
                v-for="item in options.stations"
                :key="item.id"
                :label="`${item.name}${!item.enabled ? '（已停用）' : ''}`"
                :value="item.id"
                :disabled="!item.enabled && item.id !== form.stationId" /></ui-select
          ></ui-form-item>
          <div v-if="editKind === 'personnel'" class="fire-form-grid">
            <ui-form-item label="姓名 *" :error="fieldErrors.name"><ui-input :disabled="saving" v-model="form.name" /></ui-form-item
            ><ui-form-item label="岗位"><ui-input :disabled="saving" v-model="form.position" /></ui-form-item
            ><ui-form-item label="联系电话"><ui-input :disabled="saving" v-model="form.phone" /></ui-form-item
            ><ui-form-item label="状态"><ui-switch :disabled="saving" v-model="form.enabled" active-text="启用" /></ui-form-item>
          </div>
          <div v-if="editKind === 'equipment'" class="fire-form-grid">
            <ui-form-item label="器材名称 *" :error="fieldErrors.name"><ui-input :disabled="saving" v-model="form.name" /></ui-form-item
            ><ui-form-item label="器材类别 *" :error="fieldErrors.category"
              ><ui-input :disabled="saving" v-model="form.category" placeholder="例如：防护、通信、救援" /></ui-form-item
            ><ui-form-item label="数量"
              ><ui-input-number :disabled="saving" v-model="form.quantity" :min="0" :max="1000000" :precision="0" /></ui-form-item
            ><ui-form-item label="数量单位 *" :error="fieldErrors.unit"><ui-input :disabled="saving" v-model="form.unit" /></ui-form-item
            ><ui-form-item label="状态"
              ><ui-select :disabled="saving" v-model="form.status"
                ><ui-option v-for="item in equipmentStates" :key="item.value" :value="item.value" :label="item.label" /></ui-select
            ></ui-form-item>
          </div>
          <template v-if="editKind === 'dispatches'">
            <div class="fire-form-grid">
              <ui-form-item label="出勤标题 *" :error="fieldErrors.title"><ui-input :disabled="saving" v-model="form.title" /></ui-form-item
              ><ui-form-item label="出勤类型"
                ><ui-select :disabled="saving" v-model="form.type"
                  ><ui-option v-for="item in dispatchTypes" :key="item.value" :label="item.label" :value="item.value" /></ui-select
              ></ui-form-item>
            </div>
            <ui-form-item label="出勤地点 *" :error="fieldErrors.location"
              ><ui-input :disabled="saving" v-model="form.location" /></ui-form-item
            ><ui-form-item label="出勤时间 *" :error="fieldErrors.startedAtInput"
              ><input :disabled="saving" v-model="form.startedAtInput" type="datetime-local" class="fire-date" /></ui-form-item
            ><ui-form-item label="出勤人员 *" :error="fieldErrors.personnelIds"
              ><ui-select
                v-model="form.personnelIds"
                multiple
                filterable
                :disabled="saving || !form.stationId"
                placeholder="选择本消防站人员"
                ><ui-option v-for="item in filteredPersonnel" :key="item.id" :label="item.name" :value="item.id" /></ui-select
            ></ui-form-item>
            <section class="fire-section">
              <h3>出勤器材</h3>
              <p class="fire-hint">选择本消防站可用器材及携带数量，提交时核对实际剩余数量。</p>
              <div v-for="(item, index) in form.equipment" :key="index" class="fire-row">
                <ui-select v-model="item.equipmentId" :disabled="saving" filterable placeholder="选择器材"
                  ><ui-option
                    v-for="option in filteredEquipment"
                    :key="option.id"
                    :label="`${option.name}（台账 ${option.quantity}）`"
                    :value="option.id" /></ui-select
                ><ui-input-number v-model="item.quantity" :disabled="saving" :min="1" :max="1000000" :precision="0" /><ui-button
                  size="small"
                  :disabled="saving"
                  @click="form.equipment.splice(index, 1)"
                  >移除</ui-button
                >
              </div>
              <ui-button
                size="small"
                :disabled="saving || !form.stationId || !filteredEquipment.length"
                @click="form.equipment.push({ equipmentId: '', quantity: 1 })"
                >添加器材</ui-button
              >
            </section>
          </template>
        </template>
        <ui-form-item v-if="editKind !== 'dispatches'" label="备注"
          ><ui-input :disabled="saving" v-model="form.notes" type="textarea" :rows="3" maxlength="2000"
        /></ui-form-item>
      </ui-form>
      <template #footer
        ><ui-button :disabled="saving" @click="dialog = false">取消</ui-button
        ><ui-button
          v-permission="`${form.id ? 'PUT' : 'POST'} /api/v1/${tabs[editKind].path}${form.id ? '/:id' : ''}`"
          type="primary"
          :loading="saving"
          @click="save"
          >{{ editKind === 'dispatches' ? '登记出勤' : '保存' }}</ui-button
        ></template
      >
    </ui-dialog>
    <ui-dialog
      :model-value="Boolean(returning)"
      title="登记归队"
      width="min(560px,94vw)"
      :close-on-click-modal="!returnSaving"
      :show-close="!returnSaving"
      :close-on-press-escape="!returnSaving"
      @update:model-value="
        value => {
          if (!value && !returnSaving) returning = null
        }
      "
    >
      <ui-alert v-if="returnError" type="error" :title="returnError" :closable="false" class="fire-section" />
      <p>{{ returning?.title }}</p>
      <ui-form label-position="top"
        ><ui-form-item label="归队时间 *"
          ><input :disabled="returnSaving" v-model="returnForm.returnedAtInput" type="datetime-local" class="fire-date" /></ui-form-item
        ><ui-form-item label="归队总结 *"
          ><ui-input
            :disabled="returnSaving"
            v-model="returnForm.summary"
            type="textarea"
            :rows="4"
            maxlength="4000" /></ui-form-item></ui-form
      ><template #footer
        ><ui-button :disabled="returnSaving" @click="returning = null">取消</ui-button
        ><ui-button v-permission="'POST /api/v1/fire-dispatches/:id/return'" type="primary" :loading="returnSaving" @click="saveReturn"
          >确认归队</ui-button
        ></template
      >
    </ui-dialog>
    <ui-drawer :model-value="Boolean(detail)" :title="detail?.title || '出勤详情'" size="min(660px,100vw)" @close="detail = null">
      <template v-if="detail"
        ><StatusDot :tone="statusTone(detail.status)" :label="statusLabel(detail.status)" />
        <dl class="fire-details">
          <div>
            <dt>消防站</dt>
            <dd>{{ stationName(detail.stationId) }}</dd>
          </div>
          <div>
            <dt>类型</dt>
            <dd>{{ enumName(dispatchTypes, detail.type) }}</dd>
          </div>
          <div>
            <dt>地点</dt>
            <dd>{{ detail.location }}</dd>
          </div>
          <div>
            <dt>出勤时间</dt>
            <dd>{{ dateTimeLabel(detail.startedAt) }}</dd>
          </div>
          <div>
            <dt>出勤人员</dt>
            <dd>{{ (detail.personnelIds || []).map(personName).join('、') }}</dd>
          </div>
          <div>
            <dt>携带器材</dt>
            <dd>{{ (detail.equipment || []).map(item => `${equipmentName(item.equipmentId)} × ${item.quantity}`).join('、') || '无' }}</dd>
          </div>
          <div>
            <dt>登记人</dt>
            <dd>{{ detail.createdBy }}</dd>
          </div>
          <div>
            <dt>归队时间</dt>
            <dd>{{ detail.returnedAt ? dateTimeLabel(detail.returnedAt) : '尚未归队' }}</dd>
          </div>
          <div>
            <dt>归队登记人</dt>
            <dd>{{ detail.returnedBy || '—' }}</dd>
          </div>
          <div>
            <dt>归队总结</dt>
            <dd>{{ detail.summary || '—' }}</dd>
          </div>
        </dl>
        <ui-button
          v-if="detail.status === 'dispatched'"
          v-permission="'POST /api/v1/fire-dispatches/:id/return'"
          type="primary"
          @click="openReturn(detail)"
          >登记归队</ui-button
        ></template
      >
    </ui-drawer>
  </div>
</template>

<style scoped src="../fireSafetyManagement.css"></style>
