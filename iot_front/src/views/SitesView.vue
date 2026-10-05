<script setup>
// 单位建筑：维护单位、建筑、楼层与平面图，在平面图上标注设备或部件位置。
// 告警生成时记录位置快照；按单位授权的用户可见该单位下已标注的设备。
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, toRef, watch } from 'vue'
import { Plus, RefreshCw, Upload, Download, MapPin } from '@lucide/vue'
import { api, apiBlob, download, notifyError } from '../api'
import { confirmDelete } from '../deleteAction'
import { UiMessage } from '../ui/feedback.js'
import { errorMessage } from '../presentation'
import { can } from '../permissions'
import FilterBar from '../components/layout/FilterBar.vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import { clientPagination } from '../listPagination'
import { usePageState } from '../composables/usePageState.js'
import { confirmClose, trackDialogForm } from '../composables/unsavedGuard.js'
import RowActions from '../components/layout/RowActions.vue'
import { floorLabel, pointFraction, sitePayload, siteTree } from '../sites'
defineEmits(['navigate'])

const data = reactive({ units: [], buildings: [], floors: [], points: [] })
const loading = ref(false),
  loadError = ref('')
const selected = reactive({ kind: '', id: '' })
const kinds = {
  unit: { path: 'units', label: '单位' },
  building: { path: 'buildings', label: '建筑' },
  floor: { path: 'floors', label: '楼层' },
  point: { path: 'points', label: '设备点位' }
}
const tree = computed(() => siteTree(data))
const unit = computed(() => data.units.find(item => item.id === (selected.kind === 'unit' ? selected.id : building.value?.unitId)))
const building = computed(() =>
  data.buildings.find(item => item.id === (selected.kind === 'building' ? selected.id : floor.value?.buildingId))
)
const floor = computed(() => (selected.kind === 'floor' ? data.floors.find(item => item.id === selected.id) : null))
const visiblePoints = computed(() =>
  data.points.filter(point =>
    selected.kind === 'unit'
      ? point.unitId === selected.id
      : selected.kind === 'building'
        ? point.buildingId === selected.id
        : point.floorId === selected.id
  )
)
// 资料接口一次返回全部单位、建筑、楼层和点位；左侧树需要全部资料，点位表在前端分页。
// 选中的节点与点位表页码在刷新或切换菜单后恢复。
const pointPage = ref(1),
  pointPageSize = ref(20)
usePageState('sites', { kind: toRef(selected, 'kind'), id: toRef(selected, 'id'), page: pointPage, pageSize: pointPageSize })
const { paged: shownPoints, total: pointTotal } = clientPagination(visiblePoints, pointPage, pointPageSize)
const floorPoints = computed(() => (floor.value ? data.points.filter(point => point.floorId === floor.value.id) : []))
const placedOnPlan = computed(() => floorPoints.value.filter(point => point.x != null && point.y != null))

let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  loadError.value = ''
  try {
    const result = await api('/api/v1/sites')
    if (version !== loadVersion) return
    Object.assign(data, {
      units: result.units || [],
      buildings: result.buildings || [],
      floors: result.floors || [],
      points: result.points || []
    })
    if (selected.kind && !data[`${selected.kind}s`]?.some(item => item.id === selected.id)) Object.assign(selected, { kind: '', id: '' })
    if (!selected.kind && data.units.length) Object.assign(selected, { kind: 'unit', id: data.units[0].id })
  } catch (error) {
    if (version === loadVersion) loadError.value = errorMessage(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}
function select(kind, id) {
  if (selected.kind !== kind || selected.id !== id) pointPage.value = 1
  Object.assign(selected, { kind, id })
  placing.value = null
}

// 资料编辑
const dialog = reactive({ kind: '', visible: false, saving: false, error: '' })
const form = reactive({})
// 资料编辑弹窗关闭前检查未保存的修改。
const formGuard = trackDialogForm(toRef(dialog, 'visible'), () => form)
async function closeEditor() {
  if (dialog.saving) return
  if (await confirmClose(formGuard.dirty())) dialog.visible = false
}
const blank = kind =>
  ({
    unit: { name: '', code: '', address: '', contact: '', phone: '', notes: '' },
    building: { unitId: unit.value?.id || '', name: '', address: '', aboveFloors: 0, belowFloors: 0, notes: '' },
    floor: { buildingId: building.value?.id || '', name: '', level: 1 },
    point: {
      unitId: unit.value?.id || '',
      buildingId: building.value?.id || '',
      floorId: floor.value?.id || '',
      deviceId: '',
      componentId: '',
      name: ''
    }
  })[kind]
function openEditor(kind, row) {
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form, blank(kind), row ? JSON.parse(JSON.stringify(row)) : {})
  Object.assign(dialog, { kind, visible: true, error: '' })
  if (kind === 'point') {
    devices.value = form.deviceId ? [{ id: form.deviceId, name: row?.deviceName || form.deviceId }] : []
    searchDevices()
  }
}
const pointBuildings = computed(() => data.buildings.filter(item => item.unitId === form.unitId))
const pointFloors = computed(() => data.floors.filter(item => item.buildingId === form.buildingId))
async function save() {
  if (dialog.saving) return
  const kind = dialog.kind,
    path = `/api/v1/sites/${kinds[kind].path}`
  dialog.saving = true
  dialog.error = ''
  try {
    const body = sitePayload({ ...form })
    if (kind === 'point' && body.floorId !== (data.points.find(item => item.id === body.id)?.floorId ?? body.floorId)) {
      delete body.x
      delete body.y
    }
    const saved = await api(body.id ? `${path}/${encodeURIComponent(body.id)}` : path, {
      method: body.id ? 'PUT' : 'POST',
      body: JSON.stringify(body)
    })
    dialog.visible = false
    UiMessage.success(`${kinds[kind].label}已保存`)
    await load()
    if (!body.id && kind !== 'point') select(kind, saved.id)
    if (kind === 'point' && floor.value && saved.floorId === floor.value.id && planUrl.value && saved.x == null) {
      placing.value = saved
      UiMessage.info('在平面图上点击设备所在位置')
    }
  } catch (error) {
    dialog.error = errorMessage(error)
  } finally {
    dialog.saving = false
  }
}
function remove(kind, row) {
  return confirmDelete({
    label: `${kinds[kind].label} ${row.name || row.deviceName || row.deviceId}`,
    path: `/api/v1/sites/${kinds[kind].path}/${encodeURIComponent(row.id)}?version=${row.version}`,
    blockedHint: '仍有下级资料或资料已被他人修改，请刷新后先删除下级资料。',
    onDeleted: async () => {
      if (selected.kind === kind && selected.id === row.id) Object.assign(selected, { kind: '', id: '' })
      await load()
    }
  })
}

// 设备检索
const devices = ref([]),
  devicesLoading = ref(false)
let deviceSearchVersion = 0,
  deviceSearchTimer = 0
async function searchDevices(keyword = '') {
  const version = ++deviceSearchVersion
  devicesLoading.value = true
  try {
    const query = new URLSearchParams({ page: '1', pageSize: '50' })
    if (keyword.trim()) query.set('q', keyword.trim())
    const result = await api(`/api/v1/device-registry?${query}`)
    if (version !== deviceSearchVersion) return
    const found = (result.items || []).map(item => item.device || item).filter(item => item.id)
    if (form.deviceId && !found.some(item => item.id === form.deviceId)) found.unshift({ id: form.deviceId, name: form.deviceId })
    devices.value = found
  } catch (error) {
    if (version === deviceSearchVersion) notifyError(error)
  } finally {
    if (version === deviceSearchVersion) devicesLoading.value = false
  }
}
function onDeviceSearch(keyword) {
  clearTimeout(deviceSearchTimer)
  deviceSearchTimer = setTimeout(() => searchDevices(keyword), 300)
}

// 平面图
const planUrl = ref(''),
  planLoading = ref(false),
  planError = ref(''),
  uploading = ref(false),
  planInput = ref(null)
let planVersion = 0
function releasePlan() {
  if (planUrl.value) URL.revokeObjectURL(planUrl.value)
  planUrl.value = ''
}
async function loadPlan() {
  const version = ++planVersion
  releasePlan()
  planError.value = ''
  if (!floor.value?.plan) return
  planLoading.value = true
  try {
    const blob = await apiBlob(`/api/v1/sites/floors/${encodeURIComponent(floor.value.id)}/plan`)
    if (version === planVersion) planUrl.value = URL.createObjectURL(blob)
  } catch (error) {
    if (version === planVersion) planError.value = errorMessage(error)
  } finally {
    if (version === planVersion) planLoading.value = false
  }
}
watch(() => `${floor.value?.id || ''}:${floor.value?.plan?.sha256 || ''}`, loadPlan)
async function uploadPlan(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file || !floor.value) return
  uploading.value = true
  try {
    const body = new FormData()
    body.append('file', file)
    await api(`/api/v1/sites/floors/${encodeURIComponent(floor.value.id)}/plan?version=${floor.value.version}`, { method: 'PUT', body })
    UiMessage.success('平面图已上传')
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    uploading.value = false
  }
}

// 标注与拖动点位
const placing = ref(null),
  planBox = ref(null),
  dragging = reactive({ id: '', x: 0, y: 0 })
async function savePosition(point, x, y) {
  try {
    await api(`/api/v1/sites/points/${encodeURIComponent(point.id)}`, {
      method: 'PUT',
      body: JSON.stringify(sitePayload({ ...point, x, y }))
    })
    await load()
  } catch (error) {
    notifyError(error)
    await load()
  }
}
function placeAt(event) {
  if (!placing.value || !planBox.value) return
  const { x, y } = pointFraction(event, planBox.value.getBoundingClientRect())
  const point = placing.value
  placing.value = null
  void savePosition(point, x, y)
}
// 键盘标注：开始标注时平面图获得焦点，方向键移动十字光标（按住 Shift 移动更快），Enter 或空格确定，Esc 取消。
const cursor = reactive({ x: 0.5, y: 0.5 })
watch(placing, point => {
  if (!point) return
  Object.assign(cursor, { x: point.x ?? 0.5, y: point.y ?? 0.5 })
  void nextTick(() => planBox.value?.focus())
})
function placeByKey(event) {
  if (!placing.value) return
  const step = event.shiftKey ? 0.05 : 0.01
  const moves = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }
  const clamp = value => Math.round(Math.min(1, Math.max(0, value)) * 10000) / 10000
  if (moves[event.key]) {
    event.preventDefault()
    cursor.x = clamp(cursor.x + moves[event.key][0])
    cursor.y = clamp(cursor.y + moves[event.key][1])
  } else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    const point = placing.value
    placing.value = null
    void savePosition(point, cursor.x, cursor.y)
  } else if (event.key === 'Escape') {
    event.preventDefault()
    placing.value = null
  }
}
function startDrag(event, point) {
  if (!can('PUT /api/v1/sites/points/:id') || !planBox.value) return
  event.preventDefault()
  Object.assign(dragging, { id: point.id, x: point.x, y: point.y })
  const move = e => Object.assign(dragging, pointFraction(e, planBox.value.getBoundingClientRect()))
  const up = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
    const moved = Math.abs(dragging.x - point.x) > 0.002 || Math.abs(dragging.y - point.y) > 0.002
    const { x, y } = dragging
    dragging.id = ''
    if (moved) void savePosition(point, x, y)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}
const markerStyle = point => {
  const x = dragging.id === point.id ? dragging.x : point.x,
    y = dragging.id === point.id ? dragging.y : point.y
  return { left: `${x * 100}%`, top: `${y * 100}%` }
}

// 批量导入
const importing = reactive({ visible: false, running: false, file: null, result: null, error: '' })
function openImport() {
  Object.assign(importing, { visible: true, running: false, file: null, result: null, error: '' })
}
async function runImport() {
  if (!importing.file || importing.running) return
  importing.running = true
  importing.error = ''
  importing.result = null
  try {
    const body = new FormData()
    body.append('file', importing.file)
    importing.result = await api('/api/v1/sites/import', { method: 'POST', body })
    UiMessage.success('导入完成')
    await load()
  } catch (error) {
    importing.error = errorMessage(error)
    importing.result = error.details?.result || null
  } finally {
    importing.running = false
  }
}
const downloadTemplate = () => download('/api/v1/sites/import-template', '单位点位导入模板.csv').catch(notifyError)

function pointActions(point) {
  return [
    {
      key: 'place',
      label: '标注位置',
      hidden: !floor.value || point.floorId !== floor.value.id || !planUrl.value,
      permission: 'PUT /api/v1/sites/points/:id',
      onClick: () => {
        placing.value = point
        UiMessage.info('在平面图上点击设备所在位置')
      }
    },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/sites/points/:id', onClick: () => openEditor('point', point) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/sites/points/:id', onClick: () => remove('point', point) }
  ]
}
const locationText = point =>
  [data.buildings.find(item => item.id === point.buildingId)?.name, floorLabel(data.floors.find(item => item.id === point.floorId))]
    .filter(Boolean)
    .join(' · ') || '未指定建筑'

onMounted(load)
onBeforeUnmount(() => {
  planVersion++
  releasePlan()
  clearTimeout(deviceSearchTimer)
})
</script>

<template>
  <FilterBar>
    <span class="site-hint">先建单位与建筑楼层，再上传平面图并标注设备；告警会带上标注位置。</span>
    <template #actions>
      <ui-button @click="downloadTemplate"><Download />导入模板</ui-button>
      <ui-button v-permission="'POST /api/v1/sites/import'" @click="openImport"><Upload />批量导入</ui-button>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-permission="'POST /api/v1/sites/units'" type="primary" @click="openEditor('unit')"><Plus />新增单位</ui-button>
    </template>
  </FilterBar>
  <div v-if="loadError" class="site-error" role="alert">
    <span>{{ loadError }}</span
    ><ui-button size="small" @click="load">重新加载</ui-button>
  </div>
  <div class="site-layout">
    <nav class="site-tree" aria-label="单位建筑">
      <p v-if="!tree.length && !loading" class="site-empty">暂无单位，点击“新增单位”或批量导入。</p>
      <div v-for="u in tree" :key="u.id" class="site-tree-unit">
        <button type="button" :class="{ active: selected.kind === 'unit' && selected.id === u.id }" @click="select('unit', u.id)">
          <strong>{{ u.name }}</strong
          ><small>{{ u.deviceCount }} 台设备</small>
        </button>
        <div v-for="b in u.buildings" :key="b.id" class="site-tree-building">
          <button type="button" :class="{ active: selected.kind === 'building' && selected.id === b.id }" @click="select('building', b.id)">
            {{ b.name }}
          </button>
          <button
            v-for="f in b.floors"
            :key="f.id"
            type="button"
            class="site-tree-floor"
            :class="{ active: selected.kind === 'floor' && selected.id === f.id }"
            @click="select('floor', f.id)"
          >
            {{ floorLabel(f) }}<small v-if="f.plan">平面图</small>
          </button>
        </div>
      </div>
    </nav>

    <section class="site-detail">
      <ui-empty v-if="!selected.kind" description="选择左侧的单位、建筑或楼层" />
      <template v-else>
        <header class="site-detail-header">
          <div>
            <h2>
              {{
                selected.kind === 'unit'
                  ? unit?.name
                  : selected.kind === 'building'
                    ? building?.name
                    : `${building?.name || ''} · ${floorLabel(floor)}`
              }}
            </h2>
            <small v-if="selected.kind === 'unit'">{{
              [unit?.code, unit?.address, unit?.contact, unit?.phone].filter(Boolean).join(' · ') || '未填写编码、地址与联系人'
            }}</small>
            <small v-else-if="selected.kind === 'building'"
              >{{ unit?.name
              }}<template v-if="building?.aboveFloors || building?.belowFloors">
                · 地上 {{ building.aboveFloors }} 层 / 地下 {{ building.belowFloors }} 层</template
              ></small
            >
            <small v-else
              >{{ unit?.name }}<template v-if="floor?.plan"> · 平面图 {{ floor.plan.width }}×{{ floor.plan.height }}</template></small
            >
          </div>
          <div class="site-actions">
            <ui-button
              v-if="selected.kind === 'unit'"
              v-permission="'POST /api/v1/sites/buildings'"
              size="small"
              @click="openEditor('building')"
              >新增建筑</ui-button
            >
            <ui-button
              v-if="selected.kind === 'building'"
              v-permission="'POST /api/v1/sites/floors'"
              size="small"
              @click="openEditor('floor')"
              >新增楼层</ui-button
            >
            <template v-if="selected.kind === 'floor'">
              <input ref="planInput" type="file" accept="image/png,image/jpeg" hidden @change="uploadPlan" />
              <ui-button v-permission="'PUT /api/v1/sites/floors/:id/plan'" size="small" :loading="uploading" @click="planInput?.click()"
                ><Upload />{{ floor?.plan ? '更换平面图' : '上传平面图' }}</ui-button
              >
            </template>
            <ui-button v-permission="'POST /api/v1/sites/points'" size="small" type="primary" @click="openEditor('point')"
              ><MapPin />标注设备</ui-button
            >
            <ui-button
              v-permission="`PUT /api/v1/sites/${kinds[selected.kind].path}/:id`"
              size="small"
              @click="openEditor(selected.kind, selected.kind === 'unit' ? unit : selected.kind === 'building' ? building : floor)"
              >编辑</ui-button
            >
            <ui-button
              v-permission="`DELETE /api/v1/sites/${kinds[selected.kind].path}/:id`"
              size="small"
              type="danger"
              @click="remove(selected.kind, selected.kind === 'unit' ? unit : selected.kind === 'building' ? building : floor)"
              >删除</ui-button
            >
          </div>
        </header>

        <div v-if="selected.kind === 'floor'" class="site-plan-wrap">
          <p v-if="placing" class="site-placing" role="status">
            正在标注“{{ placing.name || placing.deviceName || placing.deviceId }}”：在平面图上点击位置，或用方向键移动十字光标后按 Enter
            确定，<button type="button" @click="placing = null">取消</button>
          </p>
          <div v-if="planLoading" class="site-plan-empty">正在加载平面图…</div>
          <div v-else-if="planError" class="site-plan-empty" role="alert">
            {{ planError }} <ui-button size="small" @click="loadPlan">重试</ui-button>
          </div>
          <div v-else-if="!planUrl" class="site-plan-empty">该楼层还没有平面图，上传 PNG 或 JPEG 图片后可在图上标注设备位置。</div>
          <div
            v-else
            ref="planBox"
            class="site-plan"
            :class="{ placing: Boolean(placing) }"
            :tabindex="placing ? 0 : -1"
            :role="placing ? 'application' : undefined"
            :aria-label="placing ? '楼层平面图标注区：方向键移动光标，Enter 确定位置，Esc 取消' : undefined"
            @click="placeAt"
            @keydown="placeByKey"
          >
            <img :src="planUrl" alt="楼层平面图" draggable="false" />
            <span
              v-if="placing"
              class="site-cursor"
              :style="{ left: `${cursor.x * 100}%`, top: `${cursor.y * 100}%` }"
              aria-hidden="true"
            />
            <button
              v-for="point in placedOnPlan"
              :key="point.id"
              type="button"
              class="site-marker"
              :class="{ dragging: dragging.id === point.id }"
              :style="markerStyle(point)"
              :title="`${point.name || point.deviceName || point.deviceId}${point.componentId ? ' · 部件 ' + point.componentId : ''}`"
              @pointerdown="startDrag($event, point)"
              @click.stop
            >
              <MapPin /><span>{{ point.name || point.deviceName || point.deviceId }}</span>
            </button>
          </div>
          <small v-if="planUrl" class="site-hint">拖动图上的标记可调整位置；告警详情会显示设备在此平面图上的位置。</small>
        </div>

        <h3 class="site-section-title">设备点位 · {{ visiblePoints.length }}</h3>
        <DataTableCard
          :page="pointPage"
          :page-size="pointPageSize"
          :total="pointTotal"
          @update:page="value => (pointPage = value)"
          @update:page-size="
            value => {
              pointPageSize = value
              pointPage = 1
            }
          "
        >
          <ui-table :data="shownPoints" :empty-text="selected.kind === 'floor' ? '该楼层暂无设备点位' : '暂无设备点位'">
            <ui-table-column label="设备" min-width="180"
              ><template #default="{ row }"
                ><b>{{ row.deviceName || row.deviceId }}</b
                ><small class="site-subline"
                  >{{ row.deviceId }}<template v-if="row.componentId"> · 部件 {{ row.componentId }}</template></small
                ></template
              ></ui-table-column
            >
            <ui-table-column label="点位名称" min-width="140"
              ><template #default="{ row }">{{ row.name || '—' }}</template></ui-table-column
            >
            <ui-table-column label="建筑 / 楼层" min-width="160"
              ><template #default="{ row }"
                >{{ locationText(row)
                }}<small v-if="row.floorId" class="site-subline">{{ row.x != null ? '已在平面图标注' : '未在平面图标注' }}</small></template
              ></ui-table-column
            >
            <ui-table-column label="操作" width="190" align="right" fixed="right"
              ><template #default="{ row }"><RowActions :actions="pointActions(row)" /></template
            ></ui-table-column>
          </ui-table>
        </DataTableCard>
      </template>
    </section>
  </div>

  <ui-dialog
    :model-value="dialog.visible"
    :title="`${form.id ? '编辑' : '新增'}${kinds[dialog.kind]?.label || ''}`"
    width="min(620px,94vw)"
    @update:model-value="value => value || closeEditor()"
  >
    <ui-alert v-if="dialog.error" type="error" :title="dialog.error" :closable="false" class="site-gap" />
    <ui-form label-position="top" :disabled="dialog.saving">
      <template v-if="dialog.kind === 'unit'">
        <div class="site-form-grid">
          <ui-form-item label="单位名称" required><ui-input v-model="form.name" maxlength="100" /></ui-form-item
          ><ui-form-item label="单位编码"><ui-input v-model="form.code" maxlength="64" placeholder="例如统一社会信用代码" /></ui-form-item
          ><ui-form-item label="联系人"><ui-input v-model="form.contact" maxlength="50" /></ui-form-item
          ><ui-form-item label="联系电话"><ui-input v-model="form.phone" maxlength="32" /></ui-form-item>
        </div>
        <ui-form-item label="地址"><ui-input v-model="form.address" maxlength="200" /></ui-form-item>
        <ui-form-item label="备注"><ui-input v-model="form.notes" type="textarea" :rows="2" maxlength="500" /></ui-form-item>
      </template>
      <template v-else-if="dialog.kind === 'building'">
        <ui-form-item label="所属单位" required
          ><ui-select v-model="form.unitId" filterable
            ><ui-option v-for="item in data.units" :key="item.id" :value="item.id" :label="item.name" /></ui-select
        ></ui-form-item>
        <div class="site-form-grid">
          <ui-form-item label="建筑名称" required><ui-input v-model="form.name" maxlength="100" /></ui-form-item
          ><ui-form-item label="地址"><ui-input v-model="form.address" maxlength="200" /></ui-form-item
          ><ui-form-item label="地上层数"><ui-input-number v-model="form.aboveFloors" :min="0" :max="300" :precision="0" /></ui-form-item
          ><ui-form-item label="地下层数"><ui-input-number v-model="form.belowFloors" :min="0" :max="20" :precision="0" /></ui-form-item>
        </div>
        <ui-form-item label="备注"><ui-input v-model="form.notes" type="textarea" :rows="2" maxlength="500" /></ui-form-item>
      </template>
      <template v-else-if="dialog.kind === 'floor'">
        <div class="site-form-grid">
          <ui-form-item label="楼层名称" required
            ><ui-input v-model="form.name" maxlength="50" placeholder="例如 3F、B1、屋面" /></ui-form-item
          ><ui-form-item label="楼层序号"><ui-input-number v-model="form.level" :min="-20" :max="300" :precision="0" /></ui-form-item>
        </div>
        <small class="site-hint">序号用于排序，地下层填负数。</small>
      </template>
      <template v-else-if="dialog.kind === 'point'">
        <ui-form-item label="设备" required
          ><ui-select
            v-model="form.deviceId"
            filterable
            remote
            :loading="devicesLoading"
            placeholder="输入设备名称或编号搜索"
            @search="onDeviceSearch"
            ><ui-option
              v-for="item in devices"
              :key="item.id"
              :label="`${item.name || item.id} · ${item.id}`"
              :value="item.id" /></ui-select
        ></ui-form-item>
        <ui-form-item label="部件编号（可选）"
          ><ui-input v-model="form.componentId" maxlength="128" placeholder="消防主机下的回路/点位编号；留空表示设备本身"
        /></ui-form-item>
        <ui-form-item label="点位名称"><ui-input v-model="form.name" maxlength="100" placeholder="例如 三层东侧走廊" /></ui-form-item>
        <div class="site-form-grid">
          <ui-form-item label="单位" required
            ><ui-select
              v-model="form.unitId"
              filterable
              @change="
                () => {
                  form.buildingId = ''
                  form.floorId = ''
                }
              "
              ><ui-option v-for="item in data.units" :key="item.id" :value="item.id" :label="item.name" /></ui-select
          ></ui-form-item>
          <ui-form-item label="建筑"
            ><ui-select v-model="form.buildingId" clearable filterable @change="form.floorId = ''"
              ><ui-option v-for="item in pointBuildings" :key="item.id" :value="item.id" :label="item.name" /></ui-select
          ></ui-form-item>
          <ui-form-item label="楼层"
            ><ui-select v-model="form.floorId" clearable :disabled="!form.buildingId"
              ><ui-option v-for="item in pointFloors" :key="item.id" :value="item.id" :label="floorLabel(item)" /></ui-select
          ></ui-form-item>
        </div>
        <small class="site-hint">设备本身（不填部件）的单位决定按单位授权的范围；更换楼层会清除原平面图位置。</small>
      </template>
    </ui-form>
    <template #footer
      ><ui-button :disabled="dialog.saving" @click="closeEditor">取消</ui-button
      ><ui-button type="primary" :loading="dialog.saving" @click="save">保存</ui-button></template
    >
  </ui-dialog>

  <ui-dialog v-model="importing.visible" title="批量导入单位与点位" width="min(620px,94vw)" :close-on-click-modal="!importing.running">
    <p class="site-hint">
      支持 XLSX（第一个工作表）或 CSV（UTF-8 或 Excel
      默认的中文编码），首行为表头：单位名称、单位编码、单位地址、建筑名称、楼层名称、楼层序号、设备编号、部件编号、点位名称。按名称匹配已有单位、建筑和楼层，不存在时自动创建；已有点位的设备会移动到新位置。任一行有误时不保存任何数据。
    </p>
    <input type="file" accept=".xlsx,.csv" :disabled="importing.running" @change="importing.file = $event.target.files?.[0] || null" />
    <ui-alert v-if="importing.error" type="error" :title="importing.error" :closable="false" class="site-gap" />
    <ul v-if="importing.result?.errors?.length" class="site-import-errors">
      <li v-for="item in importing.result.errors" :key="item.line">第 {{ item.line }} 行：{{ item.message }}</li>
    </ul>
    <ui-alert
      v-else-if="importing.result"
      type="success"
      :closable="false"
      class="site-gap"
      :title="`已处理 ${importing.result.rows} 行：新建单位 ${importing.result.units}、建筑 ${importing.result.buildings}、楼层 ${importing.result.floors}，新增点位 ${importing.result.pointsCreated}，更新点位 ${importing.result.pointsUpdated}`"
    />
    <template #footer
      ><ui-button :disabled="importing.running" @click="importing.visible = false">关闭</ui-button
      ><ui-button type="primary" :loading="importing.running" :disabled="!importing.file" @click="runImport">开始导入</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.site-hint {
  color: var(--text-muted);
  font-size: 12px;
}
.site-error {
  display: flex;
  gap: var(--space-3);
  align-items: center;
  margin-bottom: var(--space-3);
  color: var(--danger-text);
}
.site-layout {
  display: grid;
  grid-template-columns: minmax(200px, 260px) minmax(0, 1fr);
  gap: var(--space-4);
  align-items: start;
}
.site-tree,
.site-detail {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: var(--space-3);
  min-width: 0;
}
.site-tree {
  display: grid;
  gap: var(--space-2);
  max-height: calc(100vh - 220px);
  overflow: auto;
}
.site-tree button {
  display: flex;
  justify-content: space-between;
  gap: var(--space-2);
  width: 100%;
  padding: 6px 8px;
  border: 0;
  border-radius: var(--radius-md);
  background: none;
  color: var(--text);
  text-align: left;
  cursor: pointer;
  font: inherit;
}
.site-tree button:hover {
  background: var(--surface-hover);
}
.site-tree button.active {
  background: var(--primary-soft);
  color: var(--primary-text);
}
.site-tree small {
  color: var(--text-muted);
  font-size: 12px;
}
.site-tree-building {
  padding-left: var(--space-3);
}
.site-tree-floor {
  padding-left: var(--space-5) !important;
  font-size: 13px;
}
.site-empty {
  color: var(--text-muted);
  font-size: 13px;
}
.site-detail-header {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.site-detail-header h2 {
  margin: 0;
  font-size: var(--font-size-lg);
}
.site-detail-header small {
  color: var(--text-muted);
}
.site-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-items: flex-start;
}
.site-plan-wrap {
  display: grid;
  gap: var(--space-2);
  margin-bottom: var(--space-4);
}
.site-plan-empty {
  padding: var(--space-6);
  text-align: center;
  color: var(--text-muted);
  background: var(--surface-muted);
  border: 1px dashed var(--border);
  border-radius: var(--radius-md);
}
.site-plan {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-muted);
  user-select: none;
  touch-action: none;
}
.site-plan.placing {
  cursor: crosshair;
  outline: 2px solid var(--primary);
}
.site-plan img {
  display: block;
  width: 100%;
  height: auto;
  pointer-events: none;
}
.site-marker {
  position: absolute;
  transform: translate(-50%, -100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  border: 0;
  background: none;
  color: var(--danger);
  cursor: grab;
  padding: 0;
}
.site-marker svg {
  width: 24px;
  height: 24px;
  filter: drop-shadow(0 1px 1px rgba(0, 0, 0, 0.35));
}
.site-marker span {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 1px 4px;
  font-size: 11px;
  color: var(--text-strong);
  background: var(--surface);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-xs);
}
.site-plan:focus-visible {
  outline: 3px solid var(--primary);
  outline-offset: 2px;
}
.site-cursor {
  position: absolute;
  width: 24px;
  height: 24px;
  transform: translate(-50%, -50%);
  border: 2px solid var(--primary);
  border-radius: 50%;
  box-shadow: 0 0 0 2px var(--surface);
  pointer-events: none;
}
.site-cursor::before,
.site-cursor::after {
  content: '';
  position: absolute;
  background: var(--primary);
}
.site-cursor::before {
  left: 50%;
  top: -8px;
  bottom: -8px;
  width: 2px;
  transform: translateX(-50%);
}
.site-cursor::after {
  top: 50%;
  left: -8px;
  right: -8px;
  height: 2px;
  transform: translateY(-50%);
}
.site-marker.dragging {
  cursor: grabbing;
  opacity: 0.8;
}
.site-placing {
  margin: 0;
  padding: var(--space-2) var(--space-3);
  background: var(--info-soft);
  color: var(--info-text);
  border-radius: var(--radius-md);
}
.site-placing button {
  border: 0;
  background: none;
  color: var(--primary-text);
  cursor: pointer;
  text-decoration: underline;
}
.site-section-title {
  margin: var(--space-2) 0;
  font-size: var(--font-size-md);
}
.site-subline {
  display: block;
  color: var(--text-muted);
  font-size: 12px;
}
.site-form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 0 var(--space-3);
}
.site-gap {
  margin: var(--space-3) 0;
}
.site-import-errors {
  max-height: 240px;
  overflow: auto;
  margin: var(--space-3) 0 0;
  padding-left: 20px;
  color: var(--danger-text);
  font-size: 13px;
}
@media (max-width: 760px) {
  .site-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .site-tree {
    max-height: 260px;
  }
}
</style>
