<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, apiAll, notifyError } from '../api' /* 引入当前代码需要的依赖。 */

const cameras = ref([]) /* 声明 cameras。 */
const devices = ref([]) /* 声明 devices。 */
const loading = ref(false) /* 声明 loading。 */
const dialogVisible = ref(false) /* 声明 dialogVisible。 */
const editing = ref('') /* 声明 editing。 */
const highlightedCameraId = ref('') /* 声明 highlightedCameraId。 */
const page = ref(1) /* 声明 page。 */
const pageSize = ref(20) /* 声明 pageSize。 */
const total = ref(0) /* 声明 total。 */
const blank = () => ({ cameraId:'', brand:'', cameraName:'', cameraPoint:'', building:'', floor:'', room:'', deviceId:'', enabled:true }) /* 声明 blank。 */
const camera = reactive(blank()) /* 声明 camera。 */
let loadVersion = 0 /* 声明 loadVersion。 */
async function load() { /* 定义 load 函数。 */
  const version = ++loadVersion /* 声明 version。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [data, deviceData] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
      api(`/api/v1/integrations/video/cameras?page=${page.value}&pageSize=${pageSize.value}`), /* 执行当前语句并推进处理流程。 */
      apiAll('/api/v1/device-registry') /* 执行当前语句并推进处理流程。 */
    ]) /* 结束当前表达式或代码块。 */
    if (version !== loadVersion) return /* 判断条件并选择处理分支。 */
    cameras.value = data.items || [] /* 更新 cameras.value 的值。 */
    devices.value = (deviceData.items || []).map(item => item.device || item).filter(item => item.id) /* 更新 devices.value 的值。 */
    total.value = Number(data.total ?? data.count ?? cameras.value.length) /* 更新 total.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) notifyError(error) /* 判断条件并选择处理分支。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function open(value) { /* 定义 open 函数。 */
  Object.assign(camera, blank(), value ? { ...value, deviceId:value.deviceId || value.relatedDeviceIds?.[0] || '' } : {}) /* 执行当前语句并推进处理流程。 */
  editing.value = value?.cameraId || '' /* 更新 editing.value 的值。 */
  dialogVisible.value = true /* 更新 dialogVisible.value 的值。 */
} /* 结束当前表达式或代码块。 */

async function save() { /* 定义 save 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = { /* 声明 value。 */
      cameraId:camera.cameraId.trim(), brand:camera.brand.trim(), cameraName:camera.cameraName.trim(), /* 执行当前语句并推进处理流程。 */
      cameraPoint:camera.cameraPoint.trim(), building:camera.building.trim(), floor:camera.floor.trim(), /* 执行当前语句并推进处理流程。 */
      room:camera.room.trim(), deviceId:camera.deviceId || '', enabled:camera.enabled /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    await api(editing.value ? `/api/v1/integrations/video/cameras/${encodeURIComponent(editing.value)}` : '/api/v1/integrations/video/cameras', { /* 等待异步操作完成。 */
      method: editing.value ? 'PUT' : 'POST', body: JSON.stringify(value) /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    UiMessage.success('摄像头信息已保存') /* 执行当前语句并推进处理流程。 */
    dialogVisible.value = false /* 更新 dialogVisible.value 的值。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function consumeNavigationAction() { /* 定义 consumeNavigationAction 函数。 */
  const raw = sessionStorage.getItem('iot:navigation-detail') /* 声明 raw。 */
  if (!raw) return /* 判断条件并选择处理分支。 */
  sessionStorage.removeItem('iot:navigation-detail') /* 执行当前语句并推进处理流程。 */
  try { /* 执行当前语句并推进处理流程。 */
    const detail = JSON.parse(raw) /* 声明 detail。 */
    if (!detail.cameraId) return /* 判断条件并选择处理分支。 */
    const target = cameras.value.find(item => item.cameraId === detail.cameraId) /* 声明 target。 */
    if (!target) return UiMessage.warning(`当前列表未找到摄像头 ${detail.cameraId}`) /* 判断条件并选择处理分支。 */
    highlightedCameraId.value = target.cameraId /* 更新 highlightedCameraId.value 的值。 */
    UiMessage.info(`已定位摄像头：${target.cameraName || target.cameraId}。直播流由外部视频平台提供。`) /* 执行当前语句并推进处理流程。 */
  } catch { /* ignore invalid navigation detail */ } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function rowClassName({ row }) { return row.cameraId === highlightedCameraId.value ? 'camera-highlight' : '' } /* 定义 rowClassName 函数。 */
function changePage(value) { page.value = value; load() } /* 定义 changePage 函数。 */
function changePageSize(value) { pageSize.value = value; page.value = 1; load() } /* 定义 changePageSize 函数。 */

onMounted(async () => { await load(); consumeNavigationAction() }) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <div class="page-toolbar"> <!-- 渲染 div 界面元素。 -->
    <ui-button v-permission="'POST /api/v1/integrations/video/cameras'" type="primary" @click="open()">新增摄像头</ui-button> <!-- 渲染 ui-button 界面元素。 -->
    <ui-button @click="load">刷新</ui-button> <!-- 渲染 ui-button 界面元素。 -->
    <span>共 {{ total }} 个摄像头；一个摄像头最多关联一个设备，一个设备可以关联多个摄像头</span> <!-- 渲染 span 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->

  <ui-alert title="平台只保存摄像头基础信息和设备关联，不解析、拉取或预览视频流。设备告警会带出关联摄像头信息，直播流请由外部视频平台按摄像头信息提供。" type="info" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->

  <ui-card shadow="never" class="surface-card table-card"> <!-- 渲染 ui-card 界面元素。 -->
    <ui-table v-loading="loading" :data="cameras" stripe :row-class-name="rowClassName"> <!-- 渲染 ui-table 界面元素。 -->
      <ui-table-column label="摄像头" min-width="180"><template #default="{ row }"><b>{{ row.cameraName }}</b><small class="subline">{{ row.cameraId }} · {{ row.brand || '品牌未填' }}</small></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="摄像头点位" min-width="170"><template #default="{ row }">{{ row.cameraPoint || '—' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="位置" min-width="210"><template #default="{ row }">{{ [row.building, row.floor, row.room].filter(Boolean).join(' / ') || '—' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="关联设备" min-width="180"><template #default="{ row }">{{ row.deviceId || '未关联' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="状态" width="100" align="center"><template #default="{ row }"><ui-tag :type="row.enabled ? 'success' : 'info'" round>{{ row.enabled ? '已启用' : '已停用' }}</ui-tag></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="操作" width="100" align="center" fixed="right"><template #default="{ row }"><ui-button v-permission="'PUT /api/v1/integrations/video/cameras/:id'" plain type="primary" @click="open(row)">编辑</ui-button></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <template #empty><ui-empty description="暂无摄像头信息" /></template>
    </ui-table> <!-- 结束当前界面区域。 -->
    <div class="list-pagination"><ui-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /></div> <!-- 渲染 div 界面元素。 -->
  </ui-card> <!-- 结束当前界面区域。 -->

  <ui-dialog v-model="dialogVisible" :title="editing ? '编辑摄像头信息' : '新增摄像头信息'" width="min(680px, 94vw)"> <!-- 渲染 ui-dialog 界面元素。 -->

    <ui-form :model="camera" label-position="top"> <!-- 渲染 ui-form 界面元素。 -->
      <div class="form-grid"> <!-- 渲染 div 界面元素。 -->
        <ui-form-item label="摄像头标识"><ui-input v-model="camera.cameraId" :disabled="!!editing" placeholder="外部视频平台摄像头标识" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="品牌"><ui-input v-model="camera.brand" placeholder="例如：海康、大华" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="摄像头名称"><ui-input v-model="camera.cameraName" placeholder="例如：一层大厅东侧" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="摄像头点位"><ui-input v-model="camera.cameraPoint" placeholder="例如：东侧入口" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="建筑"><ui-input v-model="camera.building" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="楼层"><ui-input v-model="camera.floor" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="房间"><ui-input v-model="camera.room" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="关联设备（可选）"><ui-select v-model="camera.deviceId" clearable filterable placeholder="选择一个设备"><ui-option v-for="item in devices" :key="item.id" :label="`${item.name || item.id} · ${item.id}`" :value="item.id" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <ui-alert title="保存后只能保留一个设备关联；同一设备可以在多个摄像头记录中出现。直播地址、开发工具包、流媒体服务均不在此配置。" type="info" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
      <ui-form-item><ui-switch v-model="camera.enabled" active-text="启用该摄像头" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
    </ui-form> <!-- 结束当前界面区域。 -->
    <template #footer><ui-button @click="dialogVisible=false">取消</ui-button><ui-button v-permission="['POST /api/v1/integrations/video/cameras','PUT /api/v1/integrations/video/cameras/:id']" type="primary" @click="save">保存</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
:deep(.el-alert) { margin:-4px 0 18px; } /* 设置  样式。 */
:deep(.el-table .camera-highlight > td) { background:#eff6ff !important; } /* 设置  样式。 */
</style>
