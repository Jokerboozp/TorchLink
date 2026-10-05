<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, notifyError } from '../api'
import { Plus, RadioTower, RefreshCw } from '@lucide/vue'
import CameraLiveConfig from '../components/CameraLiveConfig.vue'
import GBDevicesDialog from '../components/GBDevicesDialog.vue'
import LivePlayerDialog from '../components/LivePlayerDialog.vue'
import { cameraLiveBadge, liveState, liveUsable, loadLiveStatus, moduleStateText, moduleStateTone } from '../liveVideo'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { confirmDelete } from '../deleteAction'
import { errorMessage } from '../presentation'
import { usePageState } from '../composables/usePageState.js'
import { confirmClose, trackDialogForm } from '../composables/unsavedGuard.js'

const cameras = ref([])
const devices = ref([])
const devicesLoading = ref(false)
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref('')
const highlightedCameraId = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const blank = () => ({
  cameraId: '',
  brand: '',
  cameraName: '',
  cameraPoint: '',
  building: '',
  floor: '',
  room: '',
  deviceId: '',
  enabled: true
})
const camera = reactive(blank())
// 页码与每页条数在刷新或切换菜单后恢复。
usePageState('cameras', { page, pageSize })
// 编辑弹窗关闭前检查未保存的修改。
const formGuard = trackDialogForm(dialogVisible, () => camera)
async function closeEditor() {
  if (await confirmClose(formGuard.dirty())) dialogVisible.value = false
}
let loadVersion = 0
const loadError = ref('')
const liveConfigVisible = ref(false)
const liveConfigCamera = ref(null)
const playerVisible = ref(false)
const playerCamera = ref(null)
const moduleSaving = ref(false)
const gbVisible = ref(false)
const liveStatus = computed(() => liveState.status)
const liveDeployed = computed(() => Boolean(liveStatus.value?.deployed) && liveStatus.value?.state !== 'misconfigured')

function openLiveConfig(row) {
  liveConfigCamera.value = row
  liveConfigVisible.value = true
}
function openPlayer(row) {
  playerCamera.value = row
  playerVisible.value = true
}

// 平台级开关：关闭后服务端立即结束全部播放和媒体任务，保留直播配置；媒体容器不受影响。
async function toggleModule(enabled) {
  if (moduleSaving.value) return
  moduleSaving.value = true
  try {
    await api('/api/v1/video/module', { method: 'PUT', body: JSON.stringify({ enabled }) })
    UiMessage.success(enabled ? '直播功能已启用' : '直播功能已关闭，正在播放的画面已结束')
    await loadLiveStatus(true)
  } catch (error) {
    notifyError(error)
  } finally {
    moduleSaving.value = false
  }
}

async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const data = await api(`/api/v1/integrations/video/cameras?page=${page.value}&pageSize=${pageSize.value}`)
    if (version !== loadVersion) return
    cameras.value = data.items || []
    total.value = Number(data.total ?? data.count ?? cameras.value.length)
    loadError.value = ''
  } catch (error) {
    if (version === loadVersion) loadError.value = error?.status === 401 ? '' : errorMessage(error) || '摄像头读取失败'
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

// 关联设备按关键字向服务端检索，不预先加载全部设备：设备量大时整表拉取会让页面长时间停在加载中。
let deviceSearchVersion = 0
let deviceSearchTimer = 0
async function searchDevices(keyword = '') {
  const version = ++deviceSearchVersion
  devicesLoading.value = true
  try {
    const query = new URLSearchParams({ page: '1', pageSize: '50' })
    if (keyword.trim()) query.set('q', keyword.trim())
    const data = await api(`/api/v1/device-registry?${query}`)
    if (version !== deviceSearchVersion) return
    const found = (data.items || []).map(item => item.device || item).filter(item => item.id)
    // 已选设备不在检索结果中时仍保留选项，避免只显示编号或被清空。
    if (camera.deviceId && !found.some(item => item.id === camera.deviceId)) found.unshift({ id: camera.deviceId, name: camera.deviceId })
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

function open(value) {
  Object.assign(camera, blank(), value ? { ...value, deviceId: value.deviceId || value.relatedDeviceIds?.[0] || '' } : {})
  editing.value = value?.cameraId || ''
  devices.value = camera.deviceId ? [{ id: camera.deviceId, name: camera.deviceId }] : []
  dialogVisible.value = true
  searchDevices()
}

async function save() {
  try {
    const value = {
      cameraId: camera.cameraId.trim(),
      brand: camera.brand.trim(),
      cameraName: camera.cameraName.trim(),
      cameraPoint: camera.cameraPoint.trim(),
      building: camera.building.trim(),
      floor: camera.floor.trim(),
      room: camera.room.trim(),
      deviceId: camera.deviceId || '',
      enabled: camera.enabled
    }
    await api(
      editing.value ? `/api/v1/integrations/video/cameras/${encodeURIComponent(editing.value)}` : '/api/v1/integrations/video/cameras',
      {
        method: editing.value ? 'PUT' : 'POST',
        body: JSON.stringify(value)
      }
    )
    UiMessage.success('摄像头信息已保存')
    dialogVisible.value = false
    await load()
  } catch (error) {
    notifyError(error)
  }
}

function consumeNavigationAction() {
  const raw = sessionStorage.getItem('iot:navigation-detail')
  if (!raw) return
  sessionStorage.removeItem('iot:navigation-detail')
  try {
    const detail = JSON.parse(raw)
    if (!detail.cameraId) return
    const target = cameras.value.find(item => item.cameraId === detail.cameraId)
    if (!target) return UiMessage.warning(`当前列表未找到摄像头 ${detail.cameraId}`)
    highlightedCameraId.value = target.cameraId
    if (detail.play && liveUsable() && target.live?.enabled) return openPlayer(target)
    UiMessage.info(`已定位摄像头：${target.cameraName || target.cameraId}`)
  } catch {
    /* ignore invalid navigation detail */
  }
}

function rowClassName({ row }) {
  return row.cameraId === highlightedCameraId.value ? 'camera-highlight' : ''
}
function remove(row) {
  return confirmDelete({
    label: row.cameraName || row.cameraId,
    path: `/api/v1/integrations/video/cameras/${encodeURIComponent(row.cameraId)}`,
    onDeleted: load
  })
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

onMounted(async () => {
  // 从告警等页面跳转定位摄像头时从第一页开始，不沿用上次保存的页码。
  if (sessionStorage.getItem('iot:navigation-detail')) page.value = 1
  await Promise.all([load(), loadLiveStatus(true)])
  consumeNavigationAction()
})
function rowActions(row) {
  return [
    { key: 'watch', label: '观看', hidden: !liveUsable() || !row.enabled || !row.live?.enabled, onClick: () => openPlayer(row) },
    {
      key: 'live',
      label: '直播配置',
      permission: 'PUT /api/v1/integrations/video/cameras/:id/live',
      hidden: !liveDeployed.value,
      onClick: () => openLiveConfig(row)
    },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/integrations/video/cameras/:id', onClick: () => open(row) },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/integrations/video/cameras/:id',
      onClick: () => remove(row)
    }
  ]
}
</script>

<template>
  <FilterBar>
    <p class="camera-hint">一个摄像头最多关联一个设备，一个设备可以关联多个摄像头。</p>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-if="liveDeployed" v-permission="'PUT /api/v1/integrations/video/gb28181/devices/:deviceId'" @click="gbVisible = true"
        ><RadioTower />国标设备</ui-button
      >
      <ui-button v-permission="'POST /api/v1/integrations/video/cameras'" type="primary" @click="open()"><Plus />新增摄像头</ui-button>
    </template>
  </FilterBar>

  <section class="camera-module" aria-label="直播模块状态">
    <div class="camera-module__text">
      <strong>摄像头直播</strong>
      <StatusDot :tone="moduleStateTone[liveStatus?.state] || 'neutral'" :label="moduleStateText[liveStatus?.state] || '读取中'" />
      <small>{{ liveStatus?.message || '基础资料、设备关联和告警摄像头信息始终可用；直播默认启用，未配置直播的摄像头只保留资料。' }}</small>
    </div>
    <ui-switch
      v-if="liveState.canManageModule && liveDeployed"
      :model-value="Boolean(liveStatus?.enabled)"
      :disabled="moduleSaving"
      active-text="已启用"
      inactive-text="已关闭"
      @update:model-value="toggleModule"
    />
  </section>

  <DataTableCard
    :title="`摄像头 · ${total} 个`"
    :error="loadError"
    @retry="load()"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="changePage"
    @update:page-size="changePageSize"
  >
    <ui-table v-loading="loading" :data="cameras" :row-class-name="rowClassName">
      <ui-table-column label="摄像头" min-width="180"
        ><template #default="{ row }"
          ><b>{{ row.cameraName }}</b
          ><small class="subline">{{ row.cameraId }} · {{ row.brand || '品牌未填' }}</small></template
        ></ui-table-column
      >
      <ui-table-column label="摄像头点位" min-width="170"
        ><template #default="{ row }">{{ row.cameraPoint || '—' }}</template></ui-table-column
      >
      <ui-table-column label="位置" min-width="210"
        ><template #default="{ row }">{{
          [row.building, row.floor, row.room].filter(Boolean).join(' / ') || '—'
        }}</template></ui-table-column
      >
      <ui-table-column label="关联设备" min-width="180"
        ><template #default="{ row }">{{ row.deviceId || '未关联' }}</template></ui-table-column
      >
      <ui-table-column label="状态" width="100"
        ><template #default="{ row }"
          ><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template
      ></ui-table-column>
      <ui-table-column v-if="liveDeployed" label="直播" width="130"
        ><template #default="{ row }"><StatusDot v-bind="cameraLiveBadge(row.live)" /></template
      ></ui-table-column>
      <ui-table-column label="操作" width="200" align="right" fixed="right"
        ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
      ></ui-table-column>
      <template #empty><ui-empty description="暂无摄像头信息" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog
    :model-value="dialogVisible"
    :title="editing ? '编辑摄像头信息' : '新增摄像头信息'"
    width="min(680px, 94vw)"
    @update:model-value="value => value || closeEditor()"
  >
    <ui-form :model="camera" label-position="top">
      <section class="camera-editor-section">
        <h3>摄像头身份</h3>
        <p>填写摄像头标识和现场可识别的名称。</p>
        <div class="form-grid">
          <ui-form-item label="摄像头标识"
            ><ui-input v-model="camera.cameraId" :disabled="!!editing" placeholder="例如 camera-001"
          /></ui-form-item>
          <ui-form-item label="品牌"><ui-input v-model="camera.brand" placeholder="例如：海康、大华" /></ui-form-item>
          <ui-form-item label="摄像头名称"><ui-input v-model="camera.cameraName" placeholder="例如：一层大厅东侧" /></ui-form-item>
        </div>
      </section>
      <section class="camera-editor-section">
        <h3>安装位置</h3>
        <p>用于在告警联动时快速确认摄像头拍摄范围。</p>
        <div class="form-grid">
          <ui-form-item label="摄像头点位"><ui-input v-model="camera.cameraPoint" placeholder="例如：东侧入口" /></ui-form-item>
          <ui-form-item label="建筑"><ui-input v-model="camera.building" /></ui-form-item>
          <ui-form-item label="楼层"><ui-input v-model="camera.floor" /></ui-form-item>
          <ui-form-item label="房间"><ui-input v-model="camera.room" /></ui-form-item>
        </div>
      </section>
      <section class="camera-editor-section">
        <h3>关联与状态</h3>
        <p>每个摄像头最多关联一台设备，同一设备可关联多个摄像头。</p>
        <ui-form-item label="关联设备（可选）"
          ><ui-select
            v-model="camera.deviceId"
            clearable
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
        <ui-form-item label="摄像头状态"><ui-switch v-model="camera.enabled" active-text="启用该摄像头" /></ui-form-item>
        <small>直播接入在列表的“直播配置”中单独设置，保存这里的资料不会改动直播配置。</small>
      </section>
    </ui-form>
    <template #footer
      ><ui-button @click="closeEditor">取消</ui-button
      ><ui-button
        v-permission="['POST /api/v1/integrations/video/cameras', 'PUT /api/v1/integrations/video/cameras/:id']"
        type="primary"
        @click="save"
        >保存</ui-button
      ></template
    >
  </ui-dialog>
  <CameraLiveConfig v-model="liveConfigVisible" :camera="liveConfigCamera" @saved="load" />
  <LivePlayerDialog v-model="playerVisible" :camera="playerCamera" />
  <GBDevicesDialog v-if="liveDeployed" v-model="gbVisible" />
</template>

<style scoped>
.camera-hint {
  flex: 1 1 280px;
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.camera-module {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  margin-bottom: var(--space-4);
  padding: 12px 16px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.camera-module__text {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px var(--space-3);
  min-width: 0;
}
.camera-module__text small {
  flex-basis: 100%;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}
.camera-editor-section {
  padding: 15px 17px;
  margin-bottom: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
.camera-editor-section h3 {
  margin: 0;
  color: var(--text);
  font-size: 14px;
}
.camera-editor-section p,
.camera-editor-section small {
  display: block;
  margin: 5px 0 13px;
  color: var(--text);
  font-size: 12px;
  line-height: 1.6;
}
.camera-editor-section :deep(.n-form-item:last-of-type) {
  margin-bottom: 0;
}
@media (max-width: 640px) {
  .camera-editor-section {
    padding: 13px;
  }
}
:deep(.ui-table .camera-highlight > td) {
  --n-merged-td-color: var(--primary-soft);
  --n-merged-td-color-hover: var(--primary-soft-hover);
} /* 设置  样式。 */
</style>
