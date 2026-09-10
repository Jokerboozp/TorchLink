<script setup>
import DeviceConnection from '../components/DeviceConnection.vue'
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, notifyError, pretty } from '../api'

const emit = defineEmits(['navigate'])
const connectionDevice = ref('')
const endpointURL = path => new URL(path, location.origin).href
const registry = ref([])
const selected = ref('')
const guide = ref(null)
const loading = ref(false)
const guideDialog = ref(false)
const navigationRequested = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

function deviceName(id) {
  return registry.value.find(row => row.device.id === id)?.device.name || id || '未设置'
}

async function load() {
  loading.value = true
  try {
    const data = await api(`/api/v1/device-registry?page=${page.value}&pageSize=${pageSize.value}`)
    registry.value = data.items || []
    total.value = Number(data.total ?? data.count ?? registry.value.length)
    const raw = sessionStorage.getItem('iot:navigation-detail')
    if (raw) {
      const navigation = JSON.parse(raw)
      if (navigation.deviceId) {
        selected.value = navigation.deviceId
        navigationRequested.value = true
      }
      sessionStorage.removeItem('iot:navigation-detail')
    }
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
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

let guideRevision = 0
onBeforeUnmount(() => { guideRevision++ })
async function loadGuide() {
  const revision = ++guideRevision
  const deviceId = selected.value
  if (!selected.value) {
    guide.value = null
    return
  }
  try {
    guide.value = null
    const data = await api(`/api/v1/device-registry/${encodeURIComponent(deviceId)}/connection-guide`)
    if (revision !== guideRevision || deviceId !== selected.value) return
    if (data.credentialSupported === false) {
      connectionDevice.value = deviceId
      guideDialog.value = false
      return
    }
    guide.value = data
  } catch (error) {
    notifyError(error)
  }
}

async function openGuide(id) {
  selected.value = id
  await loadGuide()
  guideDialog.value = Boolean(guide.value)
}

async function copy(kind) {
  if (!guide.value) return
  const data = guide.value
  let text = pretty(data.payloadTemplate)
  if (kind === 'http') text = `curl -X ${data.http.method} "${endpointURL(data.http.url)}" -H "Content-Type: application/json" -H "X-Device-Key: ${data.accessKey}" -H "X-Device-Secret: <DEVICE_SECRET>" --data '${JSON.stringify(data.payloadTemplate)}'`
  if (kind === 'mqtt') text = `Broker: ${data.mqtt.broker}\nTopic: ${data.mqtt.topic}\nToken: POST ${location.origin}${data.mqtt.tokenEndpoint}\nX-Device-Key: ${data.accessKey}`
  if (kind === 'child') text = pretty(data.gateway.childPayloadTemplate)
  await navigator.clipboard.writeText(text)
  ElMessage.success('内容已复制')
}

onMounted(async () => {
  await load()
  if (selected.value) {
    await loadGuide()
    if (navigationRequested.value) guideDialog.value = Boolean(guide.value)
  }
})
</script>

<template>
  <DeviceConnection v-if="connectionDevice" :key="connectionDevice" :device-id="connectionDevice" @close="connectionDevice=''" @device="connectionDevice=$event" @navigate="(page,query)=>{connectionDevice='';emit('navigate',page,query)}"/>
  <div class="page-toolbar">
    <el-button plain type="primary" @click="emit('navigate','devices')">管理设备</el-button>
    <el-button :loading="loading" @click="load">刷新</el-button>
    <span>共 {{ total }} 台已注册设备，选择设备查看连接参数和报文示例。</span>
  </div>

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="registry" stripe>
      <el-table-column label="设备" min-width="230"><template #default="{ row }"><b>{{ row.device.name }}</b><small class="subline">{{ row.device.id }}</small></template></el-table-column>
      <el-table-column label="产品" min-width="170"><template #default="{ row }">{{ row.device.productId || '未绑定产品' }}</template></el-table-column>
      <el-table-column label="操作" width="130" fixed="right" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="openGuide(row.device.id)">连接指南</el-button></div></template></el-table-column>
      <template #empty><el-empty description="还没有注册设备，请先到设备管理创建" /></template>
    </el-table>
    <div class="list-pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>

  <el-dialog v-model="guideDialog" :title="`设备连接指南 · ${deviceName(selected)}`" width="min(780px, 94vw)">
    <el-empty v-if="!guide" description="正在加载接入指南" />
    <div v-else class="guide-stack">
      <p>设备密钥仅在注册或轮换时显示。按下列参数配置设备，上报后到“原始报文”核对解析结果。</p>
      <el-card v-if="guide.gateway" shadow="never" class="inner-card"><strong>网关自动注册子设备</strong><p>网关使用自己的凭证上报，报文中的设备标识和产品标识应指向子设备。</p><pre>{{ pretty(guide.gateway.childPayloadTemplate) }}</pre><el-button @click="copy('child')">复制子设备模板</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>HTTP 接入</strong><code>{{ guide.http.method }} {{ endpointURL(guide.http.url) }}</code><small>X-Device-Key: {{ guide.accessKey }}</small><el-button @click="copy('http')">复制 HTTP 示例</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>MQTT 接入</strong><code>{{ guide.mqtt.broker }}</code><code>{{ guide.mqtt.topic }}</code><el-button @click="copy('mqtt')">复制 MQTT 参数</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>报文模板</strong><pre>{{ pretty(guide.payloadTemplate) }}</pre><el-button @click="copy('payload')">复制报文模板</el-button></el-card>
    </div>
    <template #footer><el-button @click="guideDialog=false">关闭</el-button></template>
  </el-dialog>
</template>
