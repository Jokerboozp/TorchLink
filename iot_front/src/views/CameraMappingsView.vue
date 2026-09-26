<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, apiAll, notifyError } from '../api'
import { Plus, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { confirmDelete } from '../deleteAction'

const cameras = ref([])
const devices = ref([])
const loading = ref(false)
const dialogVisible = ref(false)
const editing = ref('')
const highlightedCameraId = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const blank = () => ({ cameraId:'', brand:'', cameraName:'', cameraPoint:'', building:'', floor:'', room:'', deviceId:'', enabled:true })
const camera = reactive(blank())
let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [data, deviceData] = await Promise.all([
      api(`/api/v1/integrations/video/cameras?page=${page.value}&pageSize=${pageSize.value}`),
      apiAll('/api/v1/device-registry')
    ])
    if (version !== loadVersion) return
    cameras.value = data.items || []
    devices.value = (deviceData.items || []).map(item => item.device || item).filter(item => item.id)
    total.value = Number(data.total ?? data.count ?? cameras.value.length)
  } catch (error) {
    if (version === loadVersion) notifyError(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

function open(value) {
  Object.assign(camera, blank(), value ? { ...value, deviceId:value.deviceId || value.relatedDeviceIds?.[0] || '' } : {})
  editing.value = value?.cameraId || ''
  dialogVisible.value = true
}

async function save() {
  try {
    const value = {
      cameraId:camera.cameraId.trim(), brand:camera.brand.trim(), cameraName:camera.cameraName.trim(),
      cameraPoint:camera.cameraPoint.trim(), building:camera.building.trim(), floor:camera.floor.trim(),
      room:camera.room.trim(), deviceId:camera.deviceId || '', enabled:camera.enabled
    }
    await api(editing.value ? `/api/v1/integrations/video/cameras/${encodeURIComponent(editing.value)}` : '/api/v1/integrations/video/cameras', {
      method: editing.value ? 'PUT' : 'POST', body: JSON.stringify(value)
    })
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
    UiMessage.info(`已定位摄像头：${target.cameraName || target.cameraId}。直播流由外部视频平台提供。`)
  } catch { /* ignore invalid navigation detail */ }
}

function rowClassName({ row }) { return row.cameraId === highlightedCameraId.value ? 'camera-highlight' : '' }
function remove(row) { return confirmDelete({ label:row.cameraName || row.cameraId, path:`/api/v1/integrations/video/cameras/${encodeURIComponent(row.cameraId)}`, onDeleted:load }) }
function changePage(value) { page.value = value; load() }
function changePageSize(value) { pageSize.value = value; page.value = 1; load() }

onMounted(async () => { await load(); consumeNavigationAction() })
function rowActions(row) {
  return [
    { key:'edit', label:'编辑', permission:'PUT /api/v1/integrations/video/cameras/:id', onClick:() => open(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/integrations/video/cameras/:id', onClick:() => remove(row) }
  ]
}
</script>

<template>
  <FilterBar>
    <p class="camera-hint">一个摄像头最多关联一个设备，一个设备可以关联多个摄像头。</p>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-permission="'POST /api/v1/integrations/video/cameras'" type="primary" @click="open()"><Plus />新增摄像头</ui-button>
    </template>
  </FilterBar>

  <ui-alert class="camera-intro" title="平台只保存摄像头基础信息和设备关联，不解析、拉取或预览视频流。设备告警会带出关联摄像头信息，直播流请由外部视频平台按摄像头信息提供。" type="info" :closable="false" show-icon />

  <DataTableCard :title="`摄像头 · ${total} 个`" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize">
    <ui-table v-loading="loading" :data="cameras" :row-class-name="rowClassName">
      <ui-table-column label="摄像头" min-width="180"><template #default="{ row }"><b>{{ row.cameraName }}</b><small class="subline">{{ row.cameraId }} · {{ row.brand || '品牌未填' }}</small></template></ui-table-column>
      <ui-table-column label="摄像头点位" min-width="170"><template #default="{ row }">{{ row.cameraPoint || '—' }}</template></ui-table-column>
      <ui-table-column label="位置" min-width="210"><template #default="{ row }">{{ [row.building, row.floor, row.room].filter(Boolean).join(' / ') || '—' }}</template></ui-table-column>
      <ui-table-column label="关联设备" min-width="180"><template #default="{ row }">{{ row.deviceId || '未关联' }}</template></ui-table-column>
      <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template></ui-table-column>
      <ui-table-column label="操作" width="140" align="right" fixed="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      <template #empty><ui-empty description="暂无摄像头信息" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog v-model="dialogVisible" :title="editing ? '编辑摄像头信息' : '新增摄像头信息'" width="min(680px, 94vw)">

    <ui-form :model="camera" label-position="top">
      <section class="camera-editor-section"><h3>摄像头身份</h3><p>填写外部视频平台的标识和现场可识别的名称。</p><div class="form-grid">
        <ui-form-item label="摄像头标识"><ui-input v-model="camera.cameraId" :disabled="!!editing" placeholder="外部视频平台摄像头标识" /></ui-form-item>
        <ui-form-item label="品牌"><ui-input v-model="camera.brand" placeholder="例如：海康、大华" /></ui-form-item>
        <ui-form-item label="摄像头名称"><ui-input v-model="camera.cameraName" placeholder="例如：一层大厅东侧" /></ui-form-item>
      </div></section>
      <section class="camera-editor-section"><h3>安装位置</h3><p>用于在告警联动时快速确认摄像头拍摄范围。</p><div class="form-grid">
        <ui-form-item label="摄像头点位"><ui-input v-model="camera.cameraPoint" placeholder="例如：东侧入口" /></ui-form-item>
        <ui-form-item label="建筑"><ui-input v-model="camera.building" /></ui-form-item>
        <ui-form-item label="楼层"><ui-input v-model="camera.floor" /></ui-form-item>
        <ui-form-item label="房间"><ui-input v-model="camera.room" /></ui-form-item>
      </div>
      </section>
      <section class="camera-editor-section"><h3>关联与状态</h3><p>每个摄像头最多关联一台设备，同一设备可关联多个摄像头。</p><ui-form-item label="关联设备（可选）"><ui-select v-model="camera.deviceId" clearable filterable placeholder="选择一个设备"><ui-option v-for="item in devices" :key="item.id" :label="`${item.name || item.id} · ${item.id}`" :value="item.id" /></ui-select></ui-form-item>
      <ui-form-item label="摄像头状态"><ui-switch v-model="camera.enabled" active-text="启用该摄像头" /></ui-form-item>
      <small>直播地址、开发工具包和流媒体服务不在此配置。</small></section>
    </ui-form>
    <template #footer><ui-button @click="dialogVisible=false">取消</ui-button><ui-button v-permission="['POST /api/v1/integrations/video/cameras','PUT /api/v1/integrations/video/cameras/:id']" type="primary" @click="save">保存</ui-button></template>
  </ui-dialog>
</template>

<style scoped>
.camera-hint { flex: 1 1 280px; margin: 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.camera-intro { margin-bottom: var(--space-4); }
.camera-editor-section{padding:15px 17px;margin-bottom:12px;border:1px solid var(--border);border-radius:10px;background:var(--surface)}.camera-editor-section h3{margin:0;color:var(--text);font-size:14px}.camera-editor-section p,.camera-editor-section small{display:block;margin:5px 0 13px;color:var(--text);font-size:12px;line-height:1.6}.camera-editor-section :deep(.n-form-item:last-of-type){margin-bottom:0}
@media(max-width:640px){.camera-editor-section{padding:13px}}
:deep(.ui-table .camera-highlight > td) { --n-merged-td-color:var(--primary-soft); --n-merged-td-color-hover:var(--primary-soft-hover); } /* 设置  样式。 */
</style>
