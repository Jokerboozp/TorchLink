<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, apiAll, notifyError } from '../api'

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
    ElMessage.success('摄像头信息已保存')
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
    if (!target) return ElMessage.warning(`当前列表未找到摄像头 ${detail.cameraId}`)
    highlightedCameraId.value = target.cameraId
    ElMessage.info(`已定位摄像头：${target.cameraName || target.cameraId}。直播流由外部视频平台提供。`)
  } catch { /* ignore invalid navigation detail */ }
}

function rowClassName({ row }) { return row.cameraId === highlightedCameraId.value ? 'camera-highlight' : '' }
function changePage(value) { page.value = value; load() }
function changePageSize(value) { pageSize.value = value; page.value = 1; load() }

onMounted(async () => { await load(); consumeNavigationAction() })
</script>

<template>
  <div class="page-toolbar">
    <el-button type="primary" @click="open()">新增摄像头</el-button>
    <el-button @click="load">刷新</el-button>
    <span>共 {{ total }} 个摄像头；一个摄像头最多关联一个设备，一个设备可以关联多个摄像头</span>
  </div>

  <el-alert title="平台只保存摄像头基础信息和设备关联，不解析、拉取或预览视频流。设备告警会带出关联摄像头信息，直播流请由外部视频平台按摄像头信息提供。" type="info" :closable="false" show-icon />

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="cameras" stripe :row-class-name="rowClassName">
      <el-table-column label="摄像头" min-width="180"><template #default="{ row }"><b>{{ row.cameraName }}</b><small class="subline">{{ row.cameraId }} · {{ row.brand || '品牌未填' }}</small></template></el-table-column>
      <el-table-column label="摄像头点位" min-width="170"><template #default="{ row }">{{ row.cameraPoint || '—' }}</template></el-table-column>
      <el-table-column label="位置" min-width="210"><template #default="{ row }">{{ [row.building, row.floor, row.room].filter(Boolean).join(' / ') || '—' }}</template></el-table-column>
      <el-table-column label="关联设备" min-width="180"><template #default="{ row }">{{ row.deviceId || '未关联' }}</template></el-table-column>
      <el-table-column label="状态" width="100" align="center"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'" round>{{ row.enabled ? '已启用' : '已停用' }}</el-tag></template></el-table-column>
      <el-table-column label="操作" width="100" align="center" fixed="right"><template #default="{ row }"><el-button plain type="primary" @click="open(row)">编辑</el-button></template></el-table-column>
      <template #empty><el-empty description="暂无摄像头信息" /></template>
    </el-table>
    <div class="list-pagination"><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /></div>
  </el-card>

  <el-dialog v-model="dialogVisible" :title="editing ? '编辑摄像头信息' : '新增摄像头信息'" width="min(680px, 94vw)">
    <el-form :model="camera" label-position="top">
      <div class="form-grid">
        <el-form-item label="摄像头 ID"><el-input v-model="camera.cameraId" :disabled="!!editing" placeholder="外部视频平台摄像头 ID" /></el-form-item>
        <el-form-item label="品牌"><el-input v-model="camera.brand" placeholder="例如：海康、大华" /></el-form-item>
        <el-form-item label="摄像头名称"><el-input v-model="camera.cameraName" placeholder="例如：一层大厅东侧" /></el-form-item>
        <el-form-item label="摄像头点位"><el-input v-model="camera.cameraPoint" placeholder="例如：东侧入口" /></el-form-item>
        <el-form-item label="建筑"><el-input v-model="camera.building" /></el-form-item>
        <el-form-item label="楼层"><el-input v-model="camera.floor" /></el-form-item>
        <el-form-item label="房间"><el-input v-model="camera.room" /></el-form-item>
        <el-form-item label="关联设备（可选）"><el-select v-model="camera.deviceId" clearable filterable placeholder="选择一个设备"><el-option v-for="item in devices" :key="item.id" :label="`${item.name || item.id} · ${item.id}`" :value="item.id" /></el-select></el-form-item>
      </div>
      <el-alert title="保存后只能保留一个设备关联；同一设备可以在多个摄像头记录中出现。直播地址、SDK、ZLMediaKit 均不在此配置。" type="info" :closable="false" show-icon />
      <el-form-item><el-switch v-model="camera.enabled" active-text="启用该摄像头" /></el-form-item>
    </el-form>
    <template #footer><el-button @click="dialogVisible=false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
  </el-dialog>
</template>

<style scoped>
:deep(.el-alert) { margin:-4px 0 18px; }
:deep(.el-table .camera-highlight > td) { background:#eff6ff !important; }
</style>
