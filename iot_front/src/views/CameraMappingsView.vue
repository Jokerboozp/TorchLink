<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, apiAll, notifyError, formatTime } from '../api'

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
const edgeNodes = ref([])
const onvifBusy = ref(false)
const onvif = reactive({ edgeNodeId:'', host:'', port:443, credentialRef:'', endpointPath:'/onvif/device_service' })
let editVersion = 0
const catalogVisible = ref(false), catalogNode = ref(''), catalogRows = ref([]), catalogBusy = ref(false), catalogError = ref('')
let catalogVersion = 0
async function openCatalog() {
  catalogVisible.value = true
  try { edgeNodes.value = (await api('/api/v1/edge-nodes')).items.filter(node => node.status === 'ENABLED') }
  catch(error) { notifyError(error) }
}
async function loadCatalog() {
  const version = ++catalogVersion
  catalogRows.value = []; catalogError.value = ''
  if (!catalogNode.value) return
  catalogBusy.value = true
  try {
    const value = await api(`/api/v1/edge-nodes/${encodeURIComponent(catalogNode.value)}/runtime`)
    if (version !== catalogVersion) return
    catalogRows.value = (value.heartbeat?.videoCatalog || []).flatMap(device => (device.channels || []).filter(channel => !channel.parental).map(channel => ({ ...channel, recorderId:device.deviceId, recorderName:device.name || device.deviceId, catalogAt:device.catalogAt, available:value.status === 'ONLINE' && device.registered && Date.now()-device.catalogAt <= 120000 })))
    catalogError.value = [value.heartbeat?.lastError, ...(value.heartbeat?.videoCatalog || []).map(device => device.lastError)].filter(Boolean).join('；')
  } catch(error) { if (version === catalogVersion) catalogError.value = error.message }
  finally { if (version === catalogVersion) catalogBusy.value = false }
}
async function importCatalog(row) {
  catalogBusy.value = true
  try {
    const value = await api(`/api/v1/edge-nodes/${encodeURIComponent(catalogNode.value)}/video-catalog/import`, {method:'POST', body:JSON.stringify({deviceId:row.recorderId,cameraId:row.deviceId})})
    ElMessage.success(value.created ? '摄像头信息已添加，可在列表中编辑位置和设备关联' : '该来源已添加，保留现有编辑信息')
    await load()
  } catch(error) { notifyError(error) }
  finally { catalogBusy.value = false }
}

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
	editVersion++
  Object.assign(camera, blank(), value ? { ...value, deviceId:value.deviceId || value.relatedDeviceIds?.[0] || '' } : {})
  editing.value = value?.cameraId || ''
  dialogVisible.value = true
  api('/api/v1/edge-nodes').then(data => { edgeNodes.value = (data.items || []).filter(node => node.status === 'ENABLED') }).catch(notifyError)
}

async function readONVIF() {
  const version = editVersion
  onvifBusy.value = true
  try {
    const result = await api('/api/v1/integrations/video/onvif/test', { method:'POST', body:JSON.stringify(onvif) })
    if (version !== editVersion || !dialogVisible.value) return
    const info = result.metadata || {}
    if (!editing.value && !camera.cameraId) camera.cameraId = info.serialNumber || ''
    if (!camera.brand) camera.brand = info.manufacturer || ''
    if (!camera.cameraName) camera.cameraName = info.model || ''
    ElMessage.success('已读取摄像头信息，请核对后保存')
  } catch (error) { notifyError(error) }
  finally { onvifBusy.value = false }
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
    <el-button @click="openCatalog">GB28181 目录</el-button>
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
    <el-collapse>
      <el-collapse-item title="从 ONVIF 摄像头读取基础信息" name="onvif">
        <el-form label-position="top">
          <div class="form-grid">
            <el-form-item label="现场节点"><el-select v-model="onvif.edgeNodeId" placeholder="选择已启用节点"><el-option v-for="node in edgeNodes" :key="node.id" :label="node.name || node.id" :value="node.id" /></el-select></el-form-item>
            <el-form-item label="摄像头地址"><el-input v-model="onvif.host" placeholder="IP 或主机名" /></el-form-item>
            <el-form-item label="端口"><el-input-number v-model="onvif.port" :min="1" :max="65535" /></el-form-item>
            <el-form-item label="现场凭据引用"><el-input v-model="onvif.credentialRef" placeholder="节点本地已配置的凭据名称" /></el-form-item>
            <el-form-item label="ONVIF 服务路径"><el-input v-model="onvif.endpointPath" /></el-form-item>
          </div>
          <el-button :loading="onvifBusy" :disabled="!onvif.edgeNodeId || !onvif.host || !onvif.credentialRef" @click="readONVIF">读取信息</el-button>
        </el-form>
      </el-collapse-item>
    </el-collapse>
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
  <el-dialog v-model="catalogVisible" title="GB28181 摄像头目录" width="min(900px, 94vw)">
    <p>选择运行 GB28181 元数据服务的独立现场节点。目录来自已认证注册设备，添加后可编辑位置和设备关联。</p>
    <el-select v-model="catalogNode" placeholder="选择视频节点" :disabled="catalogBusy" @change="loadCatalog"><el-option v-for="node in edgeNodes" :key="node.id" :label="node.name || node.id" :value="node.id"/></el-select>
    <el-button :disabled="!catalogNode" :loading="catalogBusy" @click="loadCatalog">刷新目录</el-button>
    <el-alert v-if="catalogError" :title="catalogError" type="error" :closable="false"/>
    <el-table :data="catalogRows" v-loading="catalogBusy" empty-text="暂无目录，请检查节点心跳和设备 SIP 注册">
      <el-table-column label="摄像头" min-width="180"><template #default="{row}">{{row.name || row.deviceId}}<small class="subline">{{row.deviceId}}</small></template></el-table-column>
      <el-table-column prop="recorderName" label="注册设备" min-width="160"/>
      <el-table-column prop="manufacturer" label="厂商" width="110"/>
      <el-table-column label="目录时间" min-width="170"><template #default="{row}">{{formatTime(row.catalogAt)}}</template></el-table-column>
      <el-table-column label="操作" width="110" fixed="right"><template #default="{row}"><el-button :disabled="!row.available || catalogBusy" @click="importCatalog(row)">添加</el-button></template></el-table-column>
    </el-table>
  </el-dialog>
</template>

<style scoped>
:deep(.el-alert) { margin:-4px 0 18px; }
:deep(.el-table .camera-highlight > td) { background:#eff6ff !important; }
</style>
