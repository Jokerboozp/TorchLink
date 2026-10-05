<script setup>
import { computed, onMounted, reactive, ref, toRef } from 'vue'
import { NDatePicker } from 'naive-ui'
import { ChevronLeft, ChevronRight, Plus, RefreshCw } from '@lucide/vue'
import { api, notifyError, session } from '../api'
import { can } from '../permissions'
import { confirmDelete } from '../deleteAction'
import { UiMessage } from '../ui/feedback.js'
import {
  assignmentsForDay,
  batchAssignments,
  calendarDays,
  dateLabel,
  dateTimeLabel,
  monthRange,
  moveCalendar,
  shiftRange,
  statusLabel,
  statusTone,
  toDateInput,
  weekRange
} from '../fireSafety'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { usePageState } from '../composables/usePageState.js'
import { confirmClose, trackDialogForm } from '../composables/unsavedGuard.js'

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
const versions = { assignments: 0, shifts: 0, swaps: 0 }
let optionsVersion = 0,
  calendarVersion = 0
const calendarRows = ref([]),
  calendarLoading = ref(false),
  calendarError = ref('')
const calendarTotal = ref(0),
  calendarNextPage = ref(1),
  calendarComplete = ref(false)
const calendarRange = computed(() => (calendarMode.value === 'week' ? weekRange(anchor.value) : monthRange(anchor.value)))
const days = computed(() => calendarDays(calendarRange.value).map(day => ({ ...day, rows: assignmentsForDay(calendarRows.value, day) })))
const calendarTitle = computed(() =>
  calendarMode.value === 'week'
    ? `${dateLabel(toDateInput(calendarRange.value[0]))} — ${dateLabel(toDateInput(calendarRange.value[1] - 1))}`
    : `${anchor.value.slice(0, 4)}年${Number(anchor.value.slice(5, 7))}月`
)
const leadingDays = computed(() => (calendarMode.value === 'month' ? (days.value[0]?.weekday + 6) % 7 : 0))
const calendarPeople = computed(() => new Set(calendarRows.value.flatMap(row => row.personnelIds || [])).size)
const enabledStations = computed(() => options.stations.filter(row => row.enabled))
const enabledPersonnel = computed(() => options.personnel.filter(row => row.enabled))
const missingSetup = computed(() => !enabledStations.value.length || !enabledPersonnel.value.length)
const saving = ref(false),
  dialog = ref('')
const blankAssignment = () => ({ id: '', version: 0, stationId: '', shiftId: '', personnelIds: [], startAt: null, endAt: null, notes: '' })
const assignment = reactive(blankAssignment()),
  assignmentDate = ref(toDateInput())
const shift = reactive({ id: '', version: 0, name: '', startTime: '08:00', endTime: '17:00' })
const swap = reactive({ assignmentId: '', fromPersonnelId: '', toPersonnelId: '', reason: '' })
const swapAssignment = ref(null),
  reviewTarget = ref(null)
const review = reactive({ approved: true, note: '' })
const assignmentEditable = computed(() => can(assignment.id ? 'PUT /api/v1/duty/assignments/:id' : 'POST /api/v1/duty/assignments'))
const stationChoices = computed(() => options.stations.filter(row => row.enabled || row.id === assignment.stationId))
const personnelChoices = computed(() =>
  options.personnel.filter(row => row.stationId === assignment.stationId && (row.enabled || assignment.personnelIds.includes(row.id)))
)
const swapFromChoices = computed(() => options.personnel.filter(row => swapAssignment.value?.personnelIds?.includes(row.id)))
const swapToChoices = computed(() =>
  enabledPersonnel.value.filter(
    row => row.stationId === swapAssignment.value?.stationId && !swapAssignment.value?.personnelIds?.includes(row.id)
  )
)
const canReview = row => row.status === 'pending' && row.requestedBy !== session.user && can('POST /api/v1/duty/swaps/:id/review')
const stationName = id => options.stations.find(row => row.id === id)?.name || id || '—'
const personName = id => options.personnel.find(row => row.id === id)?.name || id || '—'
const shiftName = id => options.shifts.find(row => row.id === id)?.name || id || '—'
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
const timeLabel = value => new Date(value).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })
const errorText = error => error?.message || '加载失败，请重试'

async function loadOptions() {
  const version = ++optionsVersion
  optionsLoading.value = true
  optionsError.value = ''
  try {
    const data = await api('/api/v1/fire-safety/options')
    if (version !== optionsVersion) return
    for (const key of ['stations', 'personnel', 'shifts']) options[key] = data[key] || []
  } catch (error) {
    if (version === optionsVersion) optionsError.value = errorText(error)
  } finally {
    if (version === optionsVersion) optionsLoading.value = false
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
  const version = ++versions[kind]
  loading[kind] = true
  errors[kind] = ''
  try {
    const data = await api(`/api/v1/duty/${kind}?${listQuery(kind)}`)
    if (version !== versions[kind]) return
    const rows = data.items || []
    const count = Number(data.total ?? rows.length)
    if (page[kind] > 1 && !rows.length && count < (page[kind] - 1) * pageSize[kind] + 1) {
      page[kind] = Math.max(1, Math.ceil(count / pageSize[kind]))
      return await loadList(kind)
    }
    ;({ assignments, shifts, swaps })[kind].value = rows
    total[kind] = count
  } catch (error) {
    if (version === versions[kind]) errors[kind] = errorText(error)
  } finally {
    if (version === versions[kind]) loading[kind] = false
  }
}

// A calendar has its own bounded date query; list pagination never hides its
// later days. Large months load in batches and remain explicitly incomplete.
async function loadCalendar({ append = false } = {}) {
  if (append && calendarLoading.value) return
  const version = ++calendarVersion
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
  calendarLoading.value = true
  calendarError.value = ''
  try {
    for (let batch = 0; batch < 10; batch++) {
      const query = new URLSearchParams({ page: String(nextPage), pageSize: '100', fromAt: String(fromAt), toAt: String(toAt) })
      if (stationId) query.set('stationId', stationId)
      if (keyword) query.set('q', keyword)
      const data = await api(`/api/v1/duty/assignments?${query}`)
      if (version !== calendarVersion) return
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
  } catch (error) {
    if (version === calendarVersion) calendarError.value = errorText(error)
  } finally {
    if (version === calendarVersion) calendarLoading.value = false
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

function openAssignment(row = null, date = anchor.value) {
  Object.assign(
    assignment,
    blankAssignment(),
    row
      ? { ...row, personnelIds: [...(row.personnelIds || [])] }
      : { stationId: filters.stationId || enabledStations.value[0]?.id || '', shiftId: options.shifts[0]?.id || '' }
  )
  assignmentDate.value = row ? toDateInput(row.startAt) : date
  if (!row) applyShift()
  dialog.value = 'assignment'
}
function applyShift() {
  const selected = options.shifts.find(row => row.id === assignment.shiftId)
  if (!selected || !assignmentDate.value) return
  try {
    ;[assignment.startAt, assignment.endAt] = shiftRange(assignmentDate.value, selected.startTime, selected.endTime)
  } catch (error) {
    UiMessage.warning(errorText(error))
  }
}
// 批量排班：一次生成日期区间内的排班，服务端整体校验冲突，有冲突则不保存任何一条。
const batch = reactive({ stationId: '', shiftId: '', from: toDateInput(), to: toDateInput(), groups: [[]], notes: '' })
const batchPersonnel = computed(() => enabledPersonnel.value.filter(row => row.stationId === batch.stationId))
const batchPreview = computed(() => {
  try {
    return { rows: batchAssignments({ ...batch, shift: options.shifts.find(row => row.id === batch.shiftId) }), error: '' }
  } catch (error) {
    return { rows: [], error: errorText(error) }
  }
})
function openBatch() {
  Object.assign(batch, {
    stationId: filters.stationId || enabledStations.value[0]?.id || '',
    shiftId: options.shifts[0]?.id || '',
    from: anchor.value,
    to: anchor.value,
    groups: [[]],
    notes: ''
  })
  dialog.value = 'batch'
}
function changeBatchStation() {
  batch.groups = batch.groups.map(group => group.filter(id => batchPersonnel.value.some(row => row.id === id)))
}
async function saveBatch() {
  if (saving.value || !can('POST /api/v1/duty/assignments/batch')) return
  if (batchPreview.value.error) return UiMessage.warning(batchPreview.value.error)
  saving.value = true
  try {
    const result = await api('/api/v1/duty/assignments/batch', {
      method: 'POST',
      body: JSON.stringify({ assignments: batchPreview.value.rows })
    })
    dialog.value = ''
    UiMessage.success(`已安排 ${result.created} 天排班`)
    await refreshAssignments()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
function changeAssignmentStation() {
  assignment.personnelIds = assignment.personnelIds.filter(id =>
    options.personnel.some(row => row.id === id && row.stationId === assignment.stationId)
  )
}
async function saveAssignment() {
  if (saving.value || !assignmentEditable.value) return
  if (!assignment.stationId || !assignment.shiftId || !assignment.personnelIds.length)
    return UiMessage.warning('请选择消防站、班次和至少一名值班人员')
  if (!assignment.startAt || !assignment.endAt || assignment.endAt <= assignment.startAt) return UiMessage.warning('结束时间须晚于开始时间')
  saving.value = true
  try {
    const body = { ...assignment, personnelIds: [...assignment.personnelIds] }
    await api(assignment.id ? `/api/v1/duty/assignments/${encodeURIComponent(assignment.id)}` : '/api/v1/duty/assignments', {
      method: assignment.id ? 'PUT' : 'POST',
      body: JSON.stringify(body)
    })
    dialog.value = ''
    UiMessage.success('排班已保存')
    await refreshAssignments()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
function openShift(row = null) {
  Object.assign(shift, { id: '', version: 0, name: '', startTime: '08:00', endTime: '17:00' }, row || {})
  dialog.value = 'shift'
}
async function saveShift() {
  if (saving.value || !can(shift.id ? 'PUT /api/v1/duty/shifts/:id' : 'POST /api/v1/duty/shifts')) return
  if (!shift.name.trim()) return UiMessage.warning('请填写班次名称')
  try {
    shiftRange(toDateInput(), shift.startTime, shift.endTime)
  } catch (error) {
    return UiMessage.warning(errorText(error))
  }
  saving.value = true
  try {
    await api(shift.id ? `/api/v1/duty/shifts/${encodeURIComponent(shift.id)}` : '/api/v1/duty/shifts', {
      method: shift.id ? 'PUT' : 'POST',
      body: JSON.stringify({ ...shift, name: shift.name.trim() })
    })
    dialog.value = ''
    UiMessage.success('班次模板已保存')
    await Promise.all([loadOptions(), loadList('shifts')])
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
async function remove(kind, row) {
  if (saving.value) return
  saving.value = true
  try {
    await confirmDelete({
      label: kind === 'shifts' ? row.name : `${stationName(row.stationId)} ${dateTimeLabel(row.startAt)}`,
      path: `/api/v1/duty/${kind}/${encodeURIComponent(row.id)}?version=${row.version}`,
      blockedHint: '记录已更新或仍有排班、待审批换班关联，请刷新后处理。',
      onDeleted: kind === 'shifts' ? () => Promise.all([loadOptions(), loadList('shifts')]) : refreshAssignments
    })
  } finally {
    saving.value = false
  }
}
function openSwap(row) {
  swapAssignment.value = { ...row, personnelIds: [...(row.personnelIds || [])] }
  Object.assign(swap, { assignmentId: row.id, fromPersonnelId: row.personnelIds?.[0] || '', toPersonnelId: '', reason: '' })
  dialog.value = 'swap'
}
async function saveSwap() {
  if (saving.value || !can('POST /api/v1/duty/swaps')) return
  if (!swap.fromPersonnelId || !swap.toPersonnelId || !swap.reason.trim())
    return UiMessage.warning('请选择原值班人员、接班人员并填写换班原因')
  saving.value = true
  try {
    await api('/api/v1/duty/swaps', {
      method: 'POST',
      body: JSON.stringify({
        assignmentId: swap.assignmentId,
        fromPersonnelId: swap.fromPersonnelId,
        toPersonnelId: swap.toPersonnelId,
        reason: swap.reason.trim()
      })
    })
    dialog.value = ''
    UiMessage.success('换班申请已提交，审批通过后更新排班')
    await loadList('swaps')
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}
function openReview(row) {
  if (!canReview(row)) return
  reviewTarget.value = row
  Object.assign(review, { approved: true, note: '' })
  dialog.value = 'review'
}
async function saveReview() {
  const row = reviewTarget.value
  if (saving.value || !row || !canReview(row)) return
  saving.value = true
  try {
    await api(`/api/v1/duty/swaps/${encodeURIComponent(row.id)}/review`, {
      method: 'POST',
      body: JSON.stringify({ version: row.version, approved: review.approved, note: review.note.trim() })
    })
    dialog.value = ''
    UiMessage.success(review.approved ? '换班已通过，排班人员已更新' : '换班申请已驳回')
    await refreshAssignments()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
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
// 弹窗关闭前检查未保存的修改（排班、批量排班、班次、换班申请与审批共用同一检查）。
// 以弹窗类型为准：从排班详情切到换班申请时重新记录快照。
const dialogGuard = trackDialogForm(
  computed(() => dialog.value),
  () =>
    ({
      assignment: [assignment, assignmentDate.value],
      batch,
      shift,
      swap,
      review
    })[dialog.value]
)
async function closeDialog() {
  if (saving.value) return
  if (await confirmClose(dialogGuard.dirty())) dialog.value = ''
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

  <section v-if="tab === 'calendar'" class="duty-calendar" aria-label="排班日历">
    <header class="duty-calendar__toolbar">
      <div class="duty-calendar__period">
        <ui-button size="small" aria-label="上一周期" @click="movePeriod(-1)"><ChevronLeft /></ui-button>
        <h2>{{ calendarTitle }}</h2>
        <ui-button size="small" aria-label="下一周期" @click="movePeriod(1)"><ChevronRight /></ui-button>
      </div>
      <div class="duty-calendar__controls">
        <NDatePicker
          v-model:formatted-value="anchor"
          size="small"
          type="date"
          value-format="yyyy-MM-dd"
          format="yyyy-MM-dd"
          :clearable="false"
          @update:formatted-value="changeCalendar"
        /><ui-button size="small" @click="today">今天</ui-button
        ><ui-radio-group v-model="calendarMode" size="small" @change="changeCalendar"
          ><ui-radio-button value="month">月</ui-radio-button><ui-radio-button value="week">周</ui-radio-button></ui-radio-group
        >
      </div>
    </header>
    <div class="duty-calendar__summary">
      <span
        >{{ calendarMode === 'month' ? '当月' : '当周' }}{{ filters.stationId || filters.q ? '筛选范围' : '' }}：已加载
        {{ calendarRows.length }} / {{ calendarTotal }} 个排班 · {{ calendarPeople }} 名人员</span
      ><StatusDot
        :tone="calendarComplete ? 'success' : 'warning'"
        :label="calendarLoading ? '加载中' : calendarComplete ? '已加载全部排班' : '尚未加载完整'"
      />
    </div>
    <div v-if="calendarError" class="duty-notice" role="alert">
      <span>{{ calendarError }}</span
      ><ui-button size="small" @click="loadCalendar">重新加载</ui-button>
    </div>
    <div class="duty-calendar__scroll" :aria-busy="calendarLoading">
      <div class="duty-calendar__weekdays">
        <span v-for="name in ['周一', '周二', '周三', '周四', '周五', '周六', '周日']" :key="name">{{ name }}</span>
      </div>
      <div class="duty-calendar__grid" :class="{ 'duty-calendar__grid--week': calendarMode === 'week' }">
        <div v-for="n in leadingDays" :key="`empty-${n}`" class="duty-calendar__blank" aria-hidden="true" />
        <section
          v-for="day in days"
          :key="day.date"
          class="duty-calendar__day"
          :class="{ 'duty-calendar__day--today': day.date === toDateInput() }"
          :aria-label="dateLabel(day.date)"
        >
          <div class="duty-calendar__day-head">
            <strong>{{ day.day }}</strong
            ><ui-button
              v-permission="'POST /api/v1/duty/assignments'"
              size="small"
              text
              :aria-label="`${dateLabel(day.date)}新增排班`"
              :disabled="missingSetup || !options.shifts.length || !!optionsError || optionsLoading"
              @click="openAssignment(null, day.date)"
              ><Plus
            /></ui-button>
          </div>
          <button v-for="row in day.rows" :key="row.id" class="duty-calendar__assignment" type="button" @click="openAssignment(row)">
            <strong>{{ shiftName(row.shiftId) }}</strong
            ><span>{{ stationName(row.stationId) }}</span
            ><span
              >{{ row.startAt < day.startAt ? '前日 ' : '' }}{{ timeLabel(row.startAt) }} — {{ row.endAt > day.endAt ? '次日 ' : ''
              }}{{ timeLabel(row.endAt) }}</span
            ><small>{{ personnelNames(row) }}</small>
          </button>
          <span v-if="!day.rows.length" class="duty-calendar__empty">{{ calendarComplete ? '未排班' : '待核对' }}</span>
        </section>
      </div>
    </div>
    <footer v-if="!calendarComplete" class="duty-calendar__footer">
      <span>当前日历及人数只包含已加载记录，完整加载后可核对覆盖情况。</span
      ><ui-button :loading="calendarLoading" @click="loadCalendar({ append: true })">继续加载排班</ui-button>
    </footer>
  </section>

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

  <ui-dialog
    :model-value="dialog === 'assignment'"
    :title="assignment.id ? '排班详情' : '新增排班'"
    width="min(760px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || closeDialog()"
  >
    <ui-form label-position="top" :disabled="saving || !assignmentEditable">
      <section class="duty-editor-section">
        <h3>站点与人员</h3>
        <div class="duty-form-grid">
          <ui-form-item label="消防站" required
            ><ui-select
              v-model="assignment.stationId"
              :disabled="saving || !assignmentEditable"
              filterable
              @change="changeAssignmentStation"
              ><ui-option
                v-for="row in stationChoices"
                :key="row.id"
                :value="row.id"
                :label="row.name"
                :disabled="!row.enabled" /></ui-select></ui-form-item
          ><ui-form-item label="值班人员" required
            ><ui-select
              v-model="assignment.personnelIds"
              multiple
              filterable
              clearable
              :disabled="saving || !assignmentEditable || !assignment.stationId"
              placeholder="可安排多名人员"
              ><ui-option
                v-for="row in personnelChoices"
                :key="row.id"
                :value="row.id"
                :label="row.name"
                :disabled="!row.enabled" /></ui-select
          ></ui-form-item>
        </div>
      </section>
      <section class="duty-editor-section">
        <h3>班次与实际时段</h3>
        <p>选择日期与模板带入建议时间，可调整实际起止时间。</p>
        <div class="duty-form-grid">
          <ui-form-item label="值班日期"
            ><NDatePicker
              v-model:formatted-value="assignmentDate"
              type="date"
              value-format="yyyy-MM-dd"
              format="yyyy-MM-dd"
              :clearable="false"
              :disabled="saving || !assignmentEditable"
              @update:formatted-value="applyShift" /></ui-form-item
          ><ui-form-item label="班次模板" required
            ><ui-select v-model="assignment.shiftId" :disabled="saving || !assignmentEditable" @change="applyShift"
              ><ui-option
                v-for="row in options.shifts"
                :key="row.id"
                :value="row.id"
                :label="`${row.name} · ${row.startTime}—${row.endTime}`" /></ui-select></ui-form-item
          ><ui-form-item label="实际开始时间" required
            ><ui-date-time v-model="assignment.startAt" :disabled="saving || !assignmentEditable" /></ui-form-item
          ><ui-form-item label="实际结束时间" required
            ><ui-date-time v-model="assignment.endAt" :disabled="saving || !assignmentEditable"
          /></ui-form-item>
        </div>
      </section>
      <ui-form-item label="备注"
        ><ui-input v-model="assignment.notes" :disabled="saving || !assignmentEditable" type="textarea" :rows="3"
      /></ui-form-item>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="closeDialog">关闭</ui-button
      ><ui-button
        v-if="assignment.id"
        v-permission="'POST /api/v1/duty/swaps'"
        :disabled="saving || assignment.endAt <= Date.now()"
        @click="openSwap(assignment)"
        >申请换班</ui-button
      ><ui-button
        v-permission="assignment.id ? 'PUT /api/v1/duty/assignments/:id' : 'POST /api/v1/duty/assignments'"
        type="primary"
        :loading="saving"
        :disabled="optionsLoading || !!optionsError"
        @click="saveAssignment"
        >保存排班</ui-button
      ></template
    >
  </ui-dialog>
  <ui-dialog
    :model-value="dialog === 'batch'"
    title="批量排班"
    width="min(760px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || closeDialog()"
  >
    <ui-form label-position="top" :disabled="saving">
      <div class="duty-form-grid">
        <ui-form-item label="消防站" required
          ><ui-select v-model="batch.stationId" filterable @change="changeBatchStation"
            ><ui-option v-for="row in enabledStations" :key="row.id" :value="row.id" :label="row.name" /></ui-select
        ></ui-form-item>
        <ui-form-item label="班次模板" required
          ><ui-select v-model="batch.shiftId"
            ><ui-option
              v-for="row in options.shifts"
              :key="row.id"
              :value="row.id"
              :label="`${row.name} · ${row.startTime}—${row.endTime}`" /></ui-select
        ></ui-form-item>
        <ui-form-item label="开始日期" required
          ><NDatePicker v-model:formatted-value="batch.from" type="date" value-format="yyyy-MM-dd" format="yyyy-MM-dd" :clearable="false"
        /></ui-form-item>
        <ui-form-item label="结束日期" required
          ><NDatePicker v-model:formatted-value="batch.to" type="date" value-format="yyyy-MM-dd" format="yyyy-MM-dd" :clearable="false"
        /></ui-form-item>
      </div>
      <section class="duty-editor-section">
        <h3>值班人员轮换</h3>
        <p>每天安排一组人员，按组顺序逐日轮换；只有一组时每天相同。</p>
        <div v-for="(group, index) in batch.groups" :key="index" class="duty-batch-group">
          <span>第 {{ index + 1 }} 组</span
          ><ui-select
            v-model="batch.groups[index]"
            multiple
            filterable
            clearable
            :disabled="!batch.stationId"
            placeholder="选择本组值班人员"
            ><ui-option v-for="row in batchPersonnel" :key="row.id" :value="row.id" :label="row.name" /></ui-select
          ><ui-button v-if="batch.groups.length > 1" text type="danger" @click="batch.groups.splice(index, 1)">移除</ui-button>
        </div>
        <ui-button size="small" :disabled="batch.groups.length >= 7" @click="batch.groups.push([])">添加一组</ui-button>
      </section>
      <ui-form-item label="备注"><ui-input v-model="batch.notes" type="textarea" :rows="2" /></ui-form-item>
      <p class="duty-batch-summary" :class="{ 'duty-batch-error': batchPreview.error }">
        {{ batchPreview.error || `将生成 ${batchPreview.rows.length} 条排班；与已有排班或人员时段冲突时整批不保存，并列出冲突日期。` }}
      </p>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="closeDialog">取消</ui-button
      ><ui-button
        v-permission="'POST /api/v1/duty/assignments/batch'"
        type="primary"
        :loading="saving"
        :disabled="!!batchPreview.error"
        @click="saveBatch"
        >生成排班</ui-button
      ></template
    >
  </ui-dialog>
  <ui-dialog
    :model-value="dialog === 'shift'"
    :title="shift.id ? '编辑班次模板' : '新增班次模板'"
    width="min(560px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || closeDialog()"
  >
    <ui-form label-position="top" :disabled="saving"
      ><ui-form-item label="班次名称" required
        ><ui-input v-model="shift.name" :disabled="saving" placeholder="例如白班、夜班"
      /></ui-form-item>
      <div class="duty-form-grid">
        <ui-form-item label="开始时间" required><ui-time-picker v-model="shift.startTime" :disabled="saving" /></ui-form-item
        ><ui-form-item label="结束时间" required><ui-time-picker v-model="shift.endTime" :disabled="saving" /></ui-form-item>
      </div>
      <p class="duty-hint">结束时间不晚于开始时间时按跨日班次处理。修改模板不改变已有排班的实际时段。</p></ui-form
    >
    <template #footer
      ><ui-button :disabled="saving" @click="closeDialog">取消</ui-button
      ><ui-button
        v-permission="shift.id ? 'PUT /api/v1/duty/shifts/:id' : 'POST /api/v1/duty/shifts'"
        type="primary"
        :loading="saving"
        @click="saveShift"
        >保存班次</ui-button
      ></template
    >
  </ui-dialog>
  <ui-dialog
    :model-value="dialog === 'swap'"
    title="申请换班"
    width="min(620px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || closeDialog()"
  >
    <p v-if="swapAssignment" class="duty-hint">
      {{ stationName(swapAssignment.stationId) }} · {{ dateTimeLabel(swapAssignment.startAt) }} — {{ dateTimeLabel(swapAssignment.endAt) }}
    </p>
    <ui-form label-position="top" :disabled="saving"
      ><div class="duty-form-grid">
        <ui-form-item label="原值班人员" required
          ><ui-select v-model="swap.fromPersonnelId" :disabled="saving"
            ><ui-option v-for="row in swapFromChoices" :key="row.id" :value="row.id" :label="row.name" /></ui-select></ui-form-item
        ><ui-form-item label="接班人员" required
          ><ui-select v-model="swap.toPersonnelId" :disabled="saving" filterable placeholder="同一消防站的启用人员"
            ><ui-option v-for="row in swapToChoices" :key="row.id" :value="row.id" :label="row.name" /></ui-select
        ></ui-form-item>
      </div>
      <ui-form-item label="换班原因" required><ui-input v-model="swap.reason" :disabled="saving" type="textarea" :rows="3" /></ui-form-item>
      <p class="duty-hint">审批通过后更新值班人员。</p></ui-form
    >
    <template #footer
      ><ui-button :disabled="saving" @click="closeDialog">取消</ui-button
      ><ui-button v-permission="'POST /api/v1/duty/swaps'" type="primary" :loading="saving" @click="saveSwap">提交申请</ui-button></template
    >
  </ui-dialog>
  <ui-dialog
    :model-value="dialog === 'review'"
    title="审批换班申请"
    width="min(620px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="value => value || closeDialog()"
  >
    <ui-form v-if="reviewTarget" label-position="top" :disabled="saving">
      <ui-descriptions v-if="reviewAssignment" :column="1" border class="duty-review-context">
        <ui-descriptions-item label="消防站">{{ stationName(reviewAssignment.stationId) }}</ui-descriptions-item>
        <ui-descriptions-item label="班次">{{ shiftName(reviewAssignment.shiftId) }}</ui-descriptions-item>
        <ui-descriptions-item label="实际值班时间"
          >{{ dateTimeLabel(reviewAssignment.startAt) }} — {{ dateTimeLabel(reviewAssignment.endAt) }}</ui-descriptions-item
        >
      </ui-descriptions>
      <ui-alert v-else type="warning" :closable="false" class="duty-review-context">关联排班不可用，请刷新后核对。</ui-alert>
      <p>{{ personName(reviewTarget.fromPersonnelId) }} → {{ personName(reviewTarget.toPersonnelId) }}</p>
      <p class="duty-hint">申请人：{{ reviewTarget.requestedBy }}<br />原因：{{ reviewTarget.reason }}</p>
      <ui-form-item label="审批结果"
        ><ui-radio-group v-model="review.approved" :disabled="saving"
          ><ui-radio-button :value="true">通过</ui-radio-button><ui-radio-button :value="false">驳回</ui-radio-button></ui-radio-group
        ></ui-form-item
      >
      <ui-form-item label="审批意见"><ui-input v-model="review.note" :disabled="saving" type="textarea" :rows="3" /></ui-form-item>
    </ui-form>
    <template #footer
      ><ui-button :disabled="saving" @click="closeDialog">取消</ui-button
      ><ui-button v-permission="'POST /api/v1/duty/swaps/:id/review'" type="primary" :loading="saving" @click="saveReview"
        >提交审批</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped>
.duty-batch-group {
  display: grid;
  grid-template-columns: 64px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.duty-batch-group > span {
  color: var(--text-muted);
  font-size: 13px;
}
.duty-batch-summary {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.6;
}
.duty-batch-error {
  color: var(--danger-text);
}
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
.duty-calendar {
  min-width: 0;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}
.duty-calendar__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 16px;
}
.duty-calendar__period,
.duty-calendar__controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.duty-calendar__period h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.duty-calendar__controls :deep(.n-date-picker) {
  width: 150px;
}
.duty-calendar__summary {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 12px;
}
.duty-calendar__scroll {
  overflow-x: auto;
}
.duty-calendar__weekdays,
.duty-calendar__grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  min-width: 840px;
}
.duty-calendar__weekdays {
  color: var(--text-muted);
  background: var(--surface-muted);
  border-top: 1px solid var(--border);
  font-size: 12px;
  text-align: center;
}
.duty-calendar__weekdays span {
  padding: 9px;
}
.duty-calendar__day,
.duty-calendar__blank {
  min-height: 155px;
  padding: 8px;
  border-top: 1px solid var(--border);
  border-right: 1px solid var(--border);
}
.duty-calendar__day:nth-child(7n),
.duty-calendar__blank:nth-child(7n) {
  border-right: 0;
}
.duty-calendar__grid--week .duty-calendar__day {
  min-height: 280px;
}
.duty-calendar__blank {
  background: var(--surface-muted);
}
.duty-calendar__day-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 24px;
  margin-bottom: 8px;
}
.duty-calendar__day-head strong {
  display: grid;
  place-items: center;
  min-width: 24px;
  height: 24px;
  font-size: 12px;
}
.duty-calendar__day--today .duty-calendar__day-head strong {
  color: var(--primary-text);
  background: var(--primary-soft);
  border-radius: 50%;
}
.duty-calendar__assignment {
  display: flex;
  flex-direction: column;
  gap: 3px;
  width: 100%;
  margin: 0 0 7px;
  padding: 8px;
  color: var(--text);
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: 5px;
  font: inherit;
  font-size: 11px;
  text-align: left;
  cursor: pointer;
  overflow-wrap: anywhere;
}
.duty-calendar__assignment:hover {
  background: var(--primary-soft);
  border-color: var(--primary);
}
.duty-calendar__assignment strong {
  font-size: 12px;
}
.duty-calendar__assignment small {
  font-size: 11px;
  color: var(--text-muted);
}
.duty-calendar__empty {
  font-size: 11px;
  color: var(--text-muted);
}
.duty-calendar__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 12px;
}
.duty-review-context {
  margin-bottom: 16px;
}
.duty-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
.duty-form-grid > * {
  min-width: 0;
}
.duty-form-grid :deep(.n-date-picker),
.duty-form-grid :deep(.ui-date-time),
.duty-form-grid :deep(.ui-time-picker) {
  width: 100%;
}
.duty-editor-section {
  margin-bottom: 16px;
}
.duty-editor-section + .duty-editor-section {
  padding-top: 16px;
  border-top: 1px solid var(--border);
}
.duty-editor-section h3 {
  margin: 0 0 12px;
  font-size: 14px;
}
.duty-editor-section p,
.duty-hint {
  margin: 0 0 12px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
@media (max-width: 767px) {
  .duty-calendar__toolbar,
  .duty-calendar__summary {
    align-items: flex-start;
    flex-direction: column;
  }
  .duty-calendar__controls {
    width: 100%;
  }
  .duty-calendar__controls :deep(.n-date-picker) {
    flex: 1;
    min-width: 130px;
  }
  .duty-calendar__period h2 {
    font-size: 13px;
  }
  .duty-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
