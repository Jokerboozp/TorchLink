<script setup>
import { onMounted, reactive, ref, toRef, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, isAbort, notifyError, session } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmDelete } from '../deleteAction'
import { errorMessage } from '../presentation'
import { can } from '../permissions'
import { inspectionStates, statusLabel, statusTone, dateTimeLabel, dateLabel } from '../fireSafety'
import { canReviewInspection, extinguisherLabels, fireQuery } from '../fireSafetyManagement'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import ExtinguisherAssetDialog from '../components/extinguishers/ExtinguisherAssetDialog.vue'
import ExtinguisherTaskDialog from '../components/extinguishers/ExtinguisherTaskDialog.vue'
import ExtinguisherActionDialog from '../components/extinguishers/ExtinguisherActionDialog.vue'
import ExtinguisherBatchDialog from '../components/extinguishers/ExtinguisherBatchDialog.vue'
import ExtinguisherAssetDrawer from '../components/extinguishers/ExtinguisherAssetDrawer.vue'
import ExtinguisherTaskDrawer from '../components/extinguishers/ExtinguisherTaskDrawer.vue'
import { usePageState } from '../composables/usePageState.js'
import { usePagedList } from '../composables/usePagedList'
defineEmits(['navigate'])

const tab = ref('assets'),
  rows = ref([]),
  page = ref(1),
  pageSize = ref(20),
  total = ref(0),
  loading = ref(false),
  loadError = ref('')
const filters = reactive({ q: '', stationId: '', status: '', due: '', remindDays: 30 })
// 页签、筛选与页码在刷新或切换菜单后恢复；须在监听页签切换之前恢复，避免恢复时被当作切换而清空筛选。
usePageState('extinguishers', {
  tab,
  q: toRef(filters, 'q'),
  stationId: toRef(filters, 'stationId'),
  status: toRef(filters, 'status'),
  due: toRef(filters, 'due'),
  remindDays: toRef(filters, 'remindDays'),
  page,
  pageSize
})
const options = reactive({ stations: [], personnel: [], extinguishers: [], inspectionChecks: [] }),
  optionsError = ref('')
const statistics = ref(null),
  statisticsError = ref('')
// 弹窗与抽屉：弹窗各自持有表单与保存逻辑，抽屉的打开状态就是详情对象本身。
const assetOpen = ref(false),
  assetTarget = ref(null),
  taskOpen = ref(false),
  taskAsset = ref(null),
  actionOpen = ref(false),
  actionKind = ref(''),
  actionTask = ref(null),
  batchOpen = ref(false)
const assetDetail = ref(null),
  taskDetail = ref(null),
  opening = ref(false),
  deleting = ref(false)
const assetStates = [
  { value: 'active', label: '在用' },
  { value: 'maintenance', label: '维护中' },
  { value: 'retired', label: '已报废' }
]
const reminderKinds = { service: '维护到期', retire: '报废到期', inspection: '巡检到期' }
const { stationName, personName, assetName, typeName } = extinguisherLabels(options)
const loader = useListLoader(loading),
  optionsLoader = useListLoader(),
  statisticsLoader = useListLoader()
function query() {
  return {
    q: filters.q,
    stationId: filters.stationId,
    status: filters.status,
    ...(tab.value === 'assets' ? { due: filters.due, remindDays: filters.remindDays } : {})
  }
}
async function load() {
  loadError.value = ''
  try {
    const result = await loader.run(signal =>
      api(
        `/api/v1/${tab.value === 'assets' ? 'extinguishers' : 'extinguisher-inspections'}?${fireQuery(query(), { page: page.value, pageSize: pageSize.value })}`,
        { signal }
      )
    )
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
    Object.assign(options, {
      stations: result.stations || [],
      personnel: result.personnel || [],
      extinguishers: result.extinguishers || [],
      inspectionChecks: result.inspectionChecks || []
    })
  } catch (error) {
    if (!isAbort(error)) optionsError.value = errorMessage(error)
  }
}
async function loadStatistics() {
  statisticsError.value = ''
  const filtersForStatistics = tab.value === 'assets' ? query() : { stationId: filters.stationId, remindDays: filters.remindDays }
  try {
    statistics.value = await statisticsLoader.run(signal =>
      api(`/api/v1/extinguishers/statistics?${fireQuery(filtersForStatistics)}`, { signal })
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
  page.value = 1
  rows.value = []
  filters.status = ''
  filters.due = ''
  applyFilters()
})
const { changePage, changePageSize } = usePagedList(() => load(), { page, pageSize })
function openAsset(row) {
  assetTarget.value = row || null
  assetOpen.value = true
  loadOptions()
}
async function assetSaved(id) {
  if (assetDetail.value?.id === id) assetDetail.value = null
  await refresh()
}
async function removeAsset(row) {
  if (deleting.value || !can('DELETE /api/v1/extinguishers/:id')) return
  deleting.value = true
  try {
    await confirmDelete({
      label: `灭火器 ${row.code}`,
      path: `/api/v1/extinguishers/${encodeURIComponent(row.id)}?version=${row.version}`,
      warning: '已有巡检记录的灭火器不能删除，可改为报废状态保留记录。',
      blockedHint: '已有巡检记录或资料已被他人修改，请刷新后改为报废状态保留记录。',
      onDeleted: async () => {
        if (rows.value.length === 1 && page.value > 1) page.value--
        await refresh()
      }
    })
  } finally {
    deleting.value = false
  }
}
async function openAssetDetail(row) {
  if (opening.value) return
  opening.value = true
  try {
    assetDetail.value = await api(`/api/v1/extinguishers/${encodeURIComponent(row.id)}?remindDays=${filters.remindDays}`)
  } catch (error) {
    notifyError(error)
  } finally {
    opening.value = false
  }
}
function openCreateTask(asset) {
  taskAsset.value = asset || null
  taskOpen.value = true
  loadOptions()
}
async function taskSaved() {
  tab.value = 'inspections'
  await refresh()
}
async function openTask(row, kind = '') {
  if (opening.value) return
  opening.value = true
  try {
    const task = await api(`/api/v1/extinguisher-inspections/${encodeURIComponent(row.id)}`)
    if (!kind) {
      taskDetail.value = task
      return
    }
    if (kind === 'review' && !canReviewInspection(task, session.user))
      throw new Error('整改提交人不能复核自己的整改，请由其他有权限的用户复核')
    actionTask.value = task
    actionKind.value = kind
    actionOpen.value = true
  } catch (error) {
    notifyError(error)
  } finally {
    opening.value = false
  }
}
async function actionSaved(result) {
  if (taskDetail.value?.id === result.id) taskDetail.value = result
  await refresh()
}
function openBatch() {
  batchOpen.value = true
  loadOptions()
}
function assetActions(row) {
  return [
    { key: 'detail', label: '详情', disabled: opening.value, onClick: () => openAssetDetail(row) },
    {
      key: 'task',
      label: '创建巡检',
      permission: 'POST /api/v1/extinguisher-inspections',
      hidden: row.status === 'retired' || Boolean(row.openInspection),
      onClick: () => openCreateTask(row)
    },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/extinguishers/:id', onClick: () => openAsset(row) },
    {
      key: 'delete',
      label: '删除',
      permission: 'DELETE /api/v1/extinguishers/:id',
      type: 'danger',
      disabled: deleting.value,
      onClick: () => removeAsset(row)
    }
  ]
}
function taskActions(row) {
  return [
    { key: 'detail', label: '详情', disabled: opening.value, onClick: () => openTask(row) },
    {
      key: 'inspect',
      label: '巡检',
      permission: 'POST /api/v1/extinguisher-inspections/:id/inspect',
      hidden: row.status !== 'pending',
      disabled: opening.value,
      onClick: () => openTask(row, 'inspect')
    },
    {
      key: 'rectify',
      label: '整改',
      permission: 'POST /api/v1/extinguisher-inspections/:id/rectify',
      hidden: row.status !== 'rectifying',
      disabled: opening.value,
      onClick: () => openTask(row, 'rectify')
    },
    {
      key: 'review',
      label: '复核',
      permission: 'POST /api/v1/extinguisher-inspections/:id/review',
      hidden: row.status !== 'reviewing',
      disabled: opening.value || !canReviewInspection(row, session.user),
      onClick: () => openTask(row, 'review')
    },
    {
      key: 'cancel',
      label: '取消',
      permission: 'POST /api/v1/extinguisher-inspections/:id/cancel',
      hidden: ['completed', 'cancelled'].includes(row.status),
      disabled: opening.value,
      type: 'danger',
      onClick: () => openTask(row, 'cancel')
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
    <ui-tabs v-model="tab"><ui-tab-pane name="assets" label="灭火器台账" /><ui-tab-pane name="inspections" label="巡检与整改" /></ui-tabs>
    <FilterBar>
      <ui-input
        v-model="filters.q"
        clearable
        :placeholder="tab === 'assets' ? '搜索编号、型号或位置' : '搜索巡检备注或问题'"
        aria-label="搜索关键字"
        @keyup.enter="applyFilters"
      />
      <ui-select v-model="filters.stationId" clearable filterable placeholder="全部消防站" aria-label="消防站筛选" @change="applyFilters"
        ><ui-option v-for="item in options.stations" :key="item.id" :value="item.id" :label="item.name"
      /></ui-select>
      <ui-select v-model="filters.status" clearable placeholder="全部状态" aria-label="状态筛选" @change="applyFilters"
        ><ui-option
          v-for="item in tab === 'assets' ? assetStates : inspectionStates"
          :key="item.value"
          :value="item.value"
          :label="item.label"
      /></ui-select>
      <ui-select
        v-if="tab === 'assets'"
        v-model="filters.due"
        clearable
        placeholder="全部到期情况"
        aria-label="到期筛选"
        @change="applyFilters"
        ><ui-option value="overdue" label="已逾期" /><ui-option value="soon" label="即将到期" /></ui-select
      ><ui-button @click="applyFilters">查询</ui-button>
      <template #actions
        ><ui-button :loading="loading" @click="refresh"><RefreshCw />刷新</ui-button
        ><ui-button v-permission="'POST /api/v1/extinguisher-inspections/batch'" :disabled="Boolean(optionsError)" @click="openBatch"
          >批量创建巡检</ui-button
        ><ui-button
          v-if="tab === 'assets'"
          v-permission="'POST /api/v1/extinguishers'"
          type="primary"
          :disabled="Boolean(optionsError)"
          @click="openAsset()"
          ><Plus />新增灭火器</ui-button
        ><ui-button
          v-else
          v-permission="'POST /api/v1/extinguisher-inspections'"
          type="primary"
          :disabled="Boolean(optionsError)"
          @click="openCreateTask()"
          ><Plus />创建巡检任务</ui-button
        ></template
      >
    </FilterBar>
    <div v-if="tab === 'assets'" class="fire-row">
      <span class="fire-hint" style="margin: 0">提前提醒天数</span
      ><ui-input-number v-model="filters.remindDays" :min="0" :max="365" :precision="0" @change="applyFilters" />
    </div>
    <div v-if="statisticsError" class="fire-error" role="alert">
      <span>{{ statisticsError }}</span
      ><ui-button size="small" @click="loadStatistics">重新加载统计</ui-button>
    </div>
    <div v-if="statistics" class="fire-stats" aria-label="灭火器概况">
      <div
        v-for="(label, key) in tab === 'assets'
          ? { total: '灭火器', active: '在用', maintenance: '维护中', retired: '已报废', overdue: '已逾期', soon: '即将到期' }
          : { pendingInspections: '待巡检', overdueInspections: '逾期任务', rectifying: '待整改', reviewing: '待复核' }"
        :key="key"
        class="fire-stat"
        :class="{ 'fire-stat--danger': ['overdue', 'overdueInspections', 'rectifying'].includes(key) }"
      >
        <span>{{ label }}</span
        ><strong>{{ statistics[key] ?? '—' }}</strong>
      </div>
    </div>
    <p v-if="tab === 'inspections' && statistics" class="fire-hint">概况统计所选消防站的全部巡检任务；下方任务列表按关键字和状态筛选。</p>
    <DataTableCard
      :title="`${tab === 'assets' ? '灭火器' : '巡检任务'} · ${total} 条`"
      :page="page"
      :page-size="pageSize"
      :total="total"
      :error="loadError"
      @retry="load"
      @update:page="changePage"
      @update:page-size="changePageSize"
    >
      <ui-table
        :data="rows"
        :loading="loading"
        :empty-text="tab === 'assets' ? '暂无灭火器，请添加现场台账' : '暂无巡检任务，请创建巡检任务'"
      >
        <template v-if="tab === 'assets'">
          <ui-table-column label="灭火器" min-width="180"
            ><template #default="{ row }"
              ><button class="fire-name" type="button" @click="openAssetDetail(row)">{{ row.code }}</button
              ><small class="fire-subline">{{ typeName(row.type) }} · {{ row.specification || '规格未填' }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="所属消防站 / 位置" min-width="200"
            ><template #default="{ row }"
              >{{ stationName(row.stationId) }}<small class="fire-subline">{{ row.location }}</small></template
            ></ui-table-column
          >
          <ui-table-column label="到期提醒" min-width="230"
            ><template #default="{ row }"
              ><span v-for="item in row.reminders || []" :key="item.kind" class="fire-reminder" :class="`fire-reminder--${item.status}`"
                >{{ reminderKinds[item.kind] }} · {{ dateLabel(item.dueOn)
                }}{{ item.status === 'overdue' ? '（已逾期）' : item.status === 'soon' ? '（即将到期）' : '' }}</span
              ><span v-if="!row.reminders?.length">—</span></template
            ></ui-table-column
          >
          <ui-table-column label="最近巡检" min-width="170"
            ><template #default="{ row }"
              >{{ row.lastInspectedAt ? dateTimeLabel(row.lastInspectedAt) : '尚未巡检'
              }}<small class="fire-subline">周期 {{ row.inspectionCycleDays }} 天</small></template
            ></ui-table-column
          >
          <ui-table-column label="状态" width="120"
            ><template #default="{ row }"
              ><StatusDot :tone="statusTone(row.status)" :label="statusLabel(row.status)" /><small
                v-if="row.openInspection"
                class="fire-subline"
                :class="{ 'fire-reminder--overdue': row.openInspection.status === 'rectifying' }"
                >{{ inspectionStates.find(item => item.value === row.openInspection.status)?.label }}</small
              ></template
            ></ui-table-column
          >
        </template>
        <template v-else>
          <ui-table-column label="巡检资产" min-width="180"
            ><template #default="{ row }"
              ><button class="fire-name" type="button" @click="openTask(row)">{{ assetName(row.extinguisherId) }}</button
              ><small class="fire-subline">{{
                stationName(options.extinguishers.find(item => item.id === row.extinguisherId)?.stationId)
              }}</small></template
            ></ui-table-column
          ><ui-table-column label="巡检人员" min-width="130"
            ><template #default="{ row }">{{ personName(row.assigneeId) }}</template></ui-table-column
          ><ui-table-column label="截止时间" min-width="170"
            ><template #default="{ row }"
              ><span :class="{ 'fire-reminder--overdue': !['completed', 'cancelled'].includes(row.status) && row.dueAt < Date.now() }">{{
                dateTimeLabel(row.dueAt)
              }}</span></template
            ></ui-table-column
          ><ui-table-column label="状态 / 结果" min-width="120"
            ><template #default="{ row }"
              ><StatusDot
                :tone="statusTone(row.status)"
                :label="inspectionStates.find(item => item.value === row.status)?.label || row.status"
              /><small v-if="row.result" class="fire-subline">{{ statusLabel(row.result) }}</small></template
            ></ui-table-column
          ><ui-table-column label="问题 / 备注" min-width="220" show-overflow-tooltip
            ><template #default="{ row }">{{ row.findings || row.notes || '—' }}</template></ui-table-column
          >
        </template>
        <ui-table-column label="操作" :width="tab === 'assets' ? 190 : 170" align="right" fixed="right"
          ><template #default="{ row }"><RowActions :actions="tab === 'assets' ? assetActions(row) : taskActions(row)" /></template
        ></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ExtinguisherAssetDialog
      v-model="assetOpen"
      :target="assetTarget"
      :station-id="filters.stationId"
      :options="options"
      @saved="assetSaved"
    />
    <ExtinguisherBatchDialog
      v-model="batchOpen"
      :station-id="filters.stationId"
      :remind-days="filters.remindDays"
      :options="options"
      @saved="refresh"
    />
    <ExtinguisherTaskDialog v-model="taskOpen" :asset="taskAsset" :station-id="filters.stationId" :options="options" @saved="taskSaved" />
    <ExtinguisherActionDialog v-model="actionOpen" :kind="actionKind" :task="actionTask" :options="options" @saved="actionSaved" />
    <ExtinguisherAssetDrawer v-model="assetDetail" :options="options" @edit="openAsset" @task="openCreateTask" />
    <ExtinguisherTaskDrawer
      v-model="taskDetail"
      :actions="taskDetail ? taskActions(taskDetail).filter(item => item.key !== 'detail') : []"
      :options="options"
    />
  </div>
</template>

<style scoped src="../fireSafetyManagement.css"></style>
