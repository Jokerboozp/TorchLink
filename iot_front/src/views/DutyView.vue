<script setup>
import { computed, onMounted, reactive, ref, toRef } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, isAbort, session } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { can } from '../permissions'
import { confirmDelete } from '../deleteAction'
import { dateTimeLabel, monthRange, moveCalendar, optionLabels, statusLabel, statusTone, toDateInput, weekRange } from '../fireSafety'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import DutyCalendar from '../components/duty/DutyCalendar.vue'
import DutyAssignmentDialog from '../components/duty/DutyAssignmentDialog.vue'
import DutyBatchDialog from '../components/duty/DutyBatchDialog.vue'
import DutyShiftDialog from '../components/duty/DutyShiftDialog.vue'
import DutySwapDialog from '../components/duty/DutySwapDialog.vue'
import DutyReviewDialog from '../components/duty/DutyReviewDialog.vue'
import { usePageState } from '../composables/usePageState.js'

const emit = defineEmits(['navigate'])
const tab = ref('calendar'),
  calendarMode = ref('month'),
  anchor = ref(toDateInput())
const options = reactive({ stations: [], personnel: [], shifts: [] })
const optionsLoading = ref(false),
  optionsError = ref('')
const filters = reactive({ q: '', stationId: '', status: '', range: null })
const assignments = ref([]),
  shifts = ref([]),
  swaps = ref([])
const page = reactive({ assignments: 1, shifts: 1, swaps: 1 })
const pageSize = reactive({ assignments: 20, shifts: 20, swaps: 20 })
const total = reactive({ assignments: 0, shifts: 0, swaps: 0 })
const loading = reactive({ assignments: false, shifts: false, swaps: false })
const errors = reactive({ assignments: '', shifts: '', swaps: '' })
// 页签、日历视图、筛选与各列表页码在刷新或切换菜单后恢复。
usePageState('duty', {
  tab,
  calendarMode,
  anchor,
  q: toRef(filters, 'q'),
  stationId: toRef(filters, 'stationId'),
  status: toRef(filters, 'status'),
  range: toRef(filters, 'range'),
  assignmentsPage: toRef(page, 'assignments'),
  assignmentsPageSize: toRef(pageSize, 'assignments'),
  shiftsPage: toRef(page, 'shifts'),
  shiftsPageSize: toRef(pageSize, 'shifts'),
  swapsPage: toRef(page, 'swaps'),
  swapsPageSize: toRef(pageSize, 'swaps')
})
const calendarRows = ref([]),
  calendarLoading = ref(false),
  calendarError = ref('')
const listLoaders = Object.fromEntries(Object.keys(loading).map(kind => [kind, useListLoader(toRef(loading, kind))]))
const optionsLoader = useListLoader(optionsLoading)
const calendarLoader = useListLoader(calendarLoading)
const calendarTotal = ref(0),
  calendarNextPage = ref(1),
  calendarComplete = ref(false)
const calendarRange = computed(() => (calendarMode.value === 'week' ? weekRange(anchor.value) : monthRange(anchor.value)))
const enabledStations = computed(() => options.stations.filter(row => row.enabled))
const enabledPersonnel = computed(() => options.personnel.filter(row => row.enabled))
const missingSetup = computed(() => !enabledStations.value.length || !enabledPersonnel.value.length)
// 新增排班与批量排班默认使用筛选中的消防站。
const defaultStationId = computed(() => filters.stationId || enabledStations.value[0]?.id || '')
const saving = ref(false),
  dialog = ref('')
// 每个弹窗由 dialog 决定是否打开；关闭由弹窗在检查未保存修改后发起。
const dialogModel = name =>
  computed({
    get: () => dialog.value === name,
    set: open => {
      if (open) dialog.value = name
      else if (dialog.value === name) dialog.value = ''
    }
  })
const assignmentOpen = dialogModel('assignment'),
  batchOpen = dialogModel('batch'),
  shiftOpen = dialogModel('shift'),
  swapOpen = dialogModel('swap'),
  reviewOpen = dialogModel('review')
const assignmentTarget = ref(null),
  assignmentDay = ref(''),
  shiftTarget = ref(null),
  swapTarget = ref(null),
  reviewTarget = ref(null)
const canReview = row => row.status === 'pending' && row.requestedBy !== session.user && can('POST /api/v1/duty/swaps/:id/review')
const { stationName, personName, shiftName } = optionLabels(options)
const personnelNames = row => (row.personnelIds || []).map(personName).join('、') || '未分配'
const knownAssignment = id => [...calendarRows.value, ...assignments.value].find(row => row.id === id)
const swapContext = row => (row?.assignment && row.assignment.id === row.assignmentId ? row.assignment : knownAssignment(row?.assignmentId))
const reviewAssignment = computed(() => swapContext(reviewTarget.value))
const assignmentLabel = swapRow => {
  const row = swapContext(swapRow)
  return row
    ? `${stationName(row.stationId)} · ${shiftName(row.shiftId)} · ${dateTimeLabel(row.startAt)} — ${dateTimeLabel(row.endAt)}`
    : '关联排班不可用，请刷新核对'
}
const errorText = error => error?.message || '加载失败，请重试'

async function loadOptions() {
  optionsError.value = ''
  try {
    const data = await optionsLoader.run(signal => api('/api/v1/fire-safety/options', { signal }))
    for (const key of ['stations', 'personnel', 'shifts']) options[key] = data[key] || []
  } catch (error) {
    if (!isAbort(error)) optionsError.value = errorText(error)
  }
}

function listQuery(kind) {
  const query = new URLSearchParams({ page: String(page[kind]), pageSize: String(pageSize[kind]) })
  if (filters.q.trim()) query.set('q', filters.q.trim())
  if (kind !== 'shifts' && filters.stationId) query.set('stationId', filters.stationId)
  if (kind === 'swaps' && filters.status) query.set('status', filters.status)
  if (kind === 'assignments' && filters.range?.length === 2) {
    query.set('fromAt', String(filters.range[0]))
    query.set('toAt', String(filters.range[1]))
  }
  return query
}
async function loadList(kind) {
  errors[kind] = ''
  try {
    const data = await listLoaders[kind].run(signal => api(`/api/v1/duty/${kind}?${listQuery(kind)}`, { signal }))
    const rows = data.items || []
    const count = Number(data.total ?? rows.length)
    if (page[kind] > 1 && !rows.length && count < (page[kind] - 1) * pageSize[kind] + 1) {
      page[kind] = Math.max(1, Math.ceil(count / pageSize[kind]))
      return await loadList(kind)
    }
    ;({ assignments, shifts, swaps })[kind].value = rows
    total[kind] = count
  } catch (error) {
    if (!isAbort(error)) errors[kind] = errorText(error)
  }
}

// A calendar has its own bounded date query; list pagination never hides its
// later days. Large months load in batches and remain explicitly incomplete.
async function loadCalendar({ append = false } = {}) {
  if (append && calendarLoading.value) return
  const [fromAt, toAt] = calendarRange.value
  const stationId = filters.stationId,
    keyword = filters.q.trim()
  const collected = append ? [...calendarRows.value] : []
  let nextPage = append ? calendarNextPage.value : 1
  if (!append) {
    calendarRows.value = []
    calendarTotal.value = 0
    calendarComplete.value = false
  }
  calendarError.value = ''
  try {
    await calendarLoader.run(async signal => {
      for (let batch = 0; batch < 10; batch++) {
        const query = new URLSearchParams({ page: String(nextPage), pageSize: '100', fromAt: String(fromAt), toAt: String(toAt) })
        if (stationId) query.set('stationId', stationId)
        if (keyword) query.set('q', keyword)
        const data = await api(`/api/v1/duty/assignments?${query}`, { signal })
        // A newer calendar query replaced this one.
        if (signal.aborted) return
        const rows = data.items || []
        collected.push(...rows)
        calendarRows.value = [...new Map(collected.map(row => [row.id, row])).values()]
        calendarTotal.value = Number(data.total ?? calendarRows.value.length)
        nextPage++
        calendarNextPage.value = nextPage
        calendarComplete.value = calendarRows.value.length >= calendarTotal.value
        if (calendarComplete.value) break
        if (!rows.length) {
          calendarError.value = '排班数据在加载期间发生变化，请刷新日历重新核对。'
          break
        }
      }
    })
  } catch (error) {
    if (!isAbort(error)) calendarError.value = errorText(error)
  }
}
function changeTab(value) {
  if (value === 'calendar') loadCalendar()
  else loadList(value)
}
function changePage(kind, value) {
  page[kind] = value
  loadList(kind)
}
function changePageSize(kind, value) {
  pageSize[kind] = value
  page[kind] = 1
  loadList(kind)
}
function applyFilters() {
  for (const key of ['assignments', 'shifts', 'swaps']) page[key] = 1
  if (tab.value === 'calendar') loadCalendar()
  else loadList(tab.value)
}
function changeCalendar() {
  if (anchor.value) loadCalendar()
}
function movePeriod(direction) {
  anchor.value = moveCalendar(anchor.value, calendarMode.value, direction)
  loadCalendar()
}
function today() {
  anchor.value = toDateInput()
  loadCalendar()
}
async function refresh() {
  await Promise.all([loadOptions(), tab.value === 'calendar' ? loadCalendar() : loadList(tab.value)])
}
async function refreshAssignments() {
  await Promise.all([loadCalendar(), loadList('assignments'), loadList('swaps')])
}

async function refreshShifts() {
  await Promise.all([loadOptions(), loadList('shifts')])
}

function openAssignment(row = null, date = anchor.value) {
  assignmentTarget.value = row
  assignmentDay.value = date
  dialog.value = 'assignment'
}
function openBatch() {
  dialog.value = 'batch'
}
function openShift(row = null) {
  shiftTarget.value = row
  dialog.value = 'shift'
}
async function remove(kind, row) {
  if (saving.value) return
  saving.value = true
  try {
    await confirmDelete({
      label: kind === 'shifts' ? row.name : `${stationName(row.stationId)} ${dateTimeLabel(row.startAt)}`,
      path: `/api/v1/duty/${kind}/${encodeURIComponent(row.id)}?version=${row.version}`,
      blockedHint: '记录已更新或仍有排班、待审批换班关联，请刷新后处理。',
      onDeleted: kind === 'shifts' ? refreshShifts : refreshAssignments
    })
  } finally {
    saving.value = false
  }
}
function openSwap(row) {
  swapTarget.value = { ...row, personnelIds: [...(row.personnelIds || [])] }
  dialog.value = 'swap'
}
function openReview(row) {
  if (!canReview(row)) return
  reviewTarget.value = row
  dialog.value = 'review'
}
function assignmentActions(row) {
  return [
    { key: 'detail', label: '详情', onClick: () => openAssignment(row) },
    {
      key: 'swap',
      label: '换班',
      permission: 'POST /api/v1/duty/swaps',
      disabled: saving.value || row.endAt <= Date.now(),
      onClick: () => openSwap(row)
    },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/duty/assignments/:id',
      disabled: saving.value,
      onClick: () => remove('assignments', row)
    }
  ]
}
function shiftActions(row) {
  return [
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/duty/shifts/:id', onClick: () => openShift(row) },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/duty/shifts/:id',
      disabled: saving.value,
      onClick: () => remove('shifts', row)
    }
  ]
}
onMounted(() =>
  Promise.all([
    loadOptions(),
    loadCalendar(),
    loadList('assignments'),
    ...(tab.value === 'shifts' || tab.value === 'swaps' ? [loadList(tab.value)] : [])
  ])
)
</script>

<template>
  <ui-tabs v-model="tab" @tab-change="changeTab">
    <ui-tab-pane name="calendar" label="排班日历" />
    <ui-tab-pane name="assignments" label="排班列表" />
    <ui-tab-pane name="shifts" label="班次模板" />
    <ui-tab-pane name="swaps" label="换班申请" />
  </ui-tabs>
  <FilterBar>
    <ui-input
      v-model="filters.q"
      clearable
      placeholder="搜索名称或备注"
      aria-label="搜索排班"
      @keyup.enter="applyFilters"
      @clear="applyFilters"
    />
    <ui-select v-if="tab !== 'shifts'" v-model="filters.stationId" clearable filterable placeholder="全部消防站" @change="applyFilters"
      ><ui-option v-for="row in options.stations" :key="row.id" :value="row.id" :label="row.name"
    /></ui-select>
    <ui-select v-if="tab === 'swaps'" v-model="filters.status" clearable placeholder="全部申请状态" @change="applyFilters"
      ><ui-option value="pending" label="待审批" /><ui-option value="approved" label="已通过" /><ui-option value="rejected" label="已驳回"
    /></ui-select>
    <ui-date-range v-if="tab === 'assignments'" v-model="filters.range" clearable @change="applyFilters" />
    <ui-button @click="applyFilters">查询</ui-button>
    <template #actions>
      <ui-button :loading="optionsLoading || (tab === 'calendar' ? calendarLoading : loading[tab])" @click="refresh"
        ><RefreshCw />刷新</ui-button
      >
      <ui-button v-if="tab === 'shifts'" v-permission="'POST /api/v1/duty/shifts'" type="primary" @click="openShift()"
        ><Plus />新增班次</ui-button
      >
      <ui-button
        v-if="tab !== 'shifts' && tab !== 'swaps'"
        v-permission="'POST /api/v1/duty/assignments/batch'"
        :disabled="optionsLoading || !!optionsError || missingSetup || !options.shifts.length"
        @click="openBatch"
        >批量排班</ui-button
      >
      <ui-button
        v-if="tab !== 'shifts'"
        v-permission="'POST /api/v1/duty/assignments'"
        type="primary"
        :disabled="optionsLoading || !!optionsError || missingSetup || !options.shifts.length"
        @click="openAssignment()"
        ><Plus />新增排班</ui-button
      >
    </template>
  </FilterBar>
  <div v-if="optionsError" class="duty-notice" role="alert">
    <span>{{ optionsError }}</span
    ><ui-button size="small" @click="loadOptions">重试加载人员和班次</ui-button>
  </div>
  <div v-else-if="!optionsLoading && missingSetup" class="duty-notice">
    <span>先登记启用的消防站和人员，再安排值班。</span
    ><ui-button v-if="can('menu:fireStations')" size="small" @click="emit('navigate', 'fireStations')">前往消防站管理</ui-button>
  </div>
  <div v-else-if="!optionsLoading && !options.shifts.length" class="duty-notice">
    <span>尚无班次模板，请先添加班次。</span
    ><ui-button v-permission="'POST /api/v1/duty/shifts'" size="small" @click="openShift()">新增班次模板</ui-button>
  </div>

  <DutyCalendar
    v-if="tab === 'calendar'"
    v-model:anchor="anchor"
    v-model:mode="calendarMode"
    :rows="calendarRows"
    :range="calendarRange"
    :total="calendarTotal"
    :complete="calendarComplete"
    :loading="calendarLoading"
    :filtered="Boolean(filters.stationId || filters.q)"
    :create-disabled="missingSetup || !options.shifts.length || !!optionsError || optionsLoading"
    :station-name="stationName"
    :shift-name="shiftName"
    :personnel-names="personnelNames"
    @move="movePeriod"
    @today="today"
    @change="changeCalendar"
    @more="loadCalendar({ append: true })"
    @create="date => openAssignment(null, date)"
    @open="row => openAssignment(row)"
  >
    <div v-if="calendarError" class="duty-notice" role="alert">
      <span>{{ calendarError }}</span
      ><ui-button size="small" @click="loadCalendar">重新加载</ui-button>
    </div>
  </DutyCalendar>

  <DataTableCard
    v-if="tab === 'assignments'"
    :title="`排班 · ${total.assignments} 个`"
    :page="page.assignments"
    :page-size="pageSize.assignments"
    :total="total.assignments"
    :error="errors.assignments"
    @retry="loadList('assignments')"
    @update:page="changePage('assignments', $event)"
    @update:page-size="changePageSize('assignments', $event)"
  >
    <ui-table :data="assignments" :loading="loading.assignments" empty-text="暂无排班">
      <ui-table-column label="消防站 / 班次" min-width="200"
        ><template #default="{ row }"
          ><strong>{{ stationName(row.stationId) }}</strong
          ><small class="subline">{{ shiftName(row.shiftId) }}</small></template
        ></ui-table-column
      >
      <ui-table-column label="值班时间" min-width="290"
        ><template #default="{ row }">{{ dateTimeLabel(row.startAt) }} — {{ dateTimeLabel(row.endAt) }}</template></ui-table-column
      >
      <ui-table-column label="值班人员" min-width="200"
        ><template #default="{ row }">{{ personnelNames(row) }}</template></ui-table-column
      >
      <ui-table-column prop="notes" label="备注" min-width="180" show-overflow-tooltip />
      <ui-table-column label="操作" width="170" fixed="right" align="right"
        ><template #default="{ row }"><RowActions :actions="assignmentActions(row)" /></template
      ></ui-table-column>
    </ui-table>
  </DataTableCard>
  <DataTableCard
    v-if="tab === 'shifts'"
    :title="`班次模板 · ${total.shifts} 个`"
    :page="page.shifts"
    :page-size="pageSize.shifts"
    :total="total.shifts"
    :error="errors.shifts"
    @retry="loadList('shifts')"
    @update:page="changePage('shifts', $event)"
    @update:page-size="changePageSize('shifts', $event)"
  >
    <ui-table :data="shifts" :loading="loading.shifts" empty-text="暂无班次模板">
      <ui-table-column prop="name" label="班次名称" min-width="180" />
      <ui-table-column label="建议时段" min-width="220"
        ><template #default="{ row }"
          >{{ row.startTime }} — {{ row.endTime }} <ui-tag v-if="row.endTime <= row.startTime" type="info">跨日</ui-tag></template
        ></ui-table-column
      >
      <ui-table-column label="操作" width="140" fixed="right" align="right"
        ><template #default="{ row }"><RowActions :actions="shiftActions(row)" /></template
      ></ui-table-column>
    </ui-table>
  </DataTableCard>
  <DataTableCard
    v-if="tab === 'swaps'"
    :title="`换班申请 · ${total.swaps} 个`"
    :page="page.swaps"
    :page-size="pageSize.swaps"
    :total="total.swaps"
    :error="errors.swaps"
    @retry="loadList('swaps')"
    @update:page="changePage('swaps', $event)"
    @update:page-size="changePageSize('swaps', $event)"
  >
    <ui-table :data="swaps" :loading="loading.swaps" empty-text="暂无换班申请，可从排班详情或列表发起">
      <ui-table-column label="关联排班" min-width="310"
        ><template #default="{ row }">{{ assignmentLabel(row) }}</template></ui-table-column
      >
      <ui-table-column label="换班人员" min-width="200"
        ><template #default="{ row }"
          >{{ personName(row.fromPersonnelId) }} → {{ personName(row.toPersonnelId) }}</template
        ></ui-table-column
      >
      <ui-table-column prop="reason" label="申请原因" min-width="200" show-overflow-tooltip />
      <ui-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><StatusDot :label="row.status === 'pending' ? '待审批' : statusLabel(row.status)" :tone="statusTone(row.status)" /></template
      ></ui-table-column>
      <ui-table-column label="申请人 / 时间" min-width="180"
        ><template #default="{ row }"
          >{{ row.requestedBy }}<small class="subline">{{ dateTimeLabel(row.createdAt) }}</small></template
        ></ui-table-column
      >
      <ui-table-column label="审批记录" min-width="200"
        ><template #default="{ row }"
          ><span v-if="row.reviewedBy"
            >{{ row.reviewedBy }} · {{ row.reviewNote || '无备注' }}<small class="subline">{{ dateTimeLabel(row.reviewedAt) }}</small></span
          ><span v-else>{{ row.requestedBy === session.user ? '由其他有审批权限的用户处理' : '等待审批' }}</span></template
        ></ui-table-column
      >
      <ui-table-column label="操作" width="100" fixed="right" align="right"
        ><template #default="{ row }"
          ><ui-button
            v-if="canReview(row)"
            v-permission="'POST /api/v1/duty/swaps/:id/review'"
            size="small"
            text
            type="primary"
            @click="openReview(row)"
            >审批</ui-button
          ></template
        ></ui-table-column
      >
    </ui-table>
  </DataTableCard>

  <DutyAssignmentDialog
    v-model="assignmentOpen"
    :target="assignmentTarget"
    :date="assignmentDay"
    :station-id="defaultStationId"
    :options="options"
    :unavailable="optionsLoading || !!optionsError"
    @saved="refreshAssignments"
    @swap="openSwap"
  />
  <DutyBatchDialog v-model="batchOpen" :options="options" :station-id="defaultStationId" :date="anchor" @saved="refreshAssignments" />
  <DutyShiftDialog v-model="shiftOpen" :target="shiftTarget" @saved="refreshShifts" />
  <DutySwapDialog v-model="swapOpen" :target="swapTarget" :options="options" @saved="loadList('swaps')" />
  <DutyReviewDialog
    v-model="reviewOpen"
    :target="reviewTarget"
    :assignment="reviewAssignment"
    :options="options"
    @saved="refreshAssignments"
  />
</template>

<style scoped>
.duty-notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 12px 16px;
  margin-bottom: 16px;
  color: var(--text-muted);
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 13px;
}
</style>
