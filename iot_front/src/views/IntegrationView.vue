<script setup>
import { statusLabel } from '../presentation'
import { onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, formatTime, notifyError, parseJSON, pretty } from '../api'

const emit = defineEmits(['navigate'])
const endpointURL = path => new URL(path, location.origin).href
const registry = ref([])
const selected = ref('')
const guide = ref(null)
const loading = ref(false)
const guideDialog = ref(false)
const debugDialog = ref(false)
const navigationRequested = ref(false)
const debugResult = ref('等待发送。')
const debugPayload = ref(pretty({ payload:{ properties:{ temperature:88.5, smoke:true, battery:92 }, tags:{ cityCode:'city_001', districtCode:'district_01', buildingId:'A', deviceType:'smoke' } } }))
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

async function loadGuide() {
  if (!selected.value) {
    guide.value = null
    return
  }
  try {
    guide.value = await api(`/api/v1/device-registry/${encodeURIComponent(selected.value)}/connection-guide`)
  } catch (error) {
    notifyError(error)
  }
}

async function openGuide(id) {
  selected.value = id
  await loadGuide()
  guideDialog.value = true
}

function openDebug(id) {
  selected.value = id
  debugResult.value = '等待发送。'
  debugDialog.value = true
}

async function sendDebug() {
  if (!selected.value) return ElMessage.warning('请先选择设备')
  try {
    const body = parseJSON(debugPayload.value, '测试报文')
    body.messageId = body.messageId || `raw_debug_${Date.now()}`
    debugResult.value = '发送中…'
    debugResult.value = pretty(await api(`/api/v1/device-registry/${encodeURIComponent(selected.value)}/debug`, { method:'POST', body:JSON.stringify(body) }))
    ElMessage.success('数据已进入归档和解析链路')
  } catch (error) {
    debugResult.value = error.message
  }
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

watch(selected, loadGuide)
onMounted(async () => {
  await load()
  if (selected.value) {
    await loadGuide()
    if (navigationRequested.value) guideDialog.value = true
  }
})
</script>

<template>
  <div class="page-toolbar">
    <el-button plain type="primary" @click="emit('navigate','devices')">管理设备</el-button>
    <el-button :loading="loading" @click="load">刷新</el-button>
    <span>共 {{ total }} 台已注册设备，连接指南和数据联调入口位于列表右侧。</span>
  </div>

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="registry" stripe>
      <el-table-column label="设备" min-width="230"><template #default="{ row }"><b>{{ row.device.name }}</b><small class="subline">{{ row.device.id }}</small></template></el-table-column>
      <el-table-column label="产品" min-width="170"><template #default="{ row }">{{ row.device.productId || '未绑定产品' }}</template></el-table-column>
      <el-table-column label="接入密钥" min-width="190"><template #default="{ row }"><code>{{ row.device.accessKey }}</code><small class="subline">设备密钥 ···{{ row.device.secretHint }}</small></template></el-table-column>
      <el-table-column label="运行状态" width="120"><template #default="{ row }">{{ statusLabel(row.runtimeState?.businessStatus || 'NEVER_SEEN') }}</template></el-table-column>
      <el-table-column label="最后活跃" min-width="170"><template #default="{ row }">{{ formatTime(row.runtimeState?.lastSeenAt) }}</template></el-table-column>
      <el-table-column label="操作" width="250" fixed="right" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="openGuide(row.device.id)">连接指南</el-button><el-button plain type="primary" @click="openDebug(row.device.id)">数据联调</el-button></div></template></el-table-column>
      <template #empty><el-empty description="还没有注册设备，请先到设备管理创建" /></template>
    </el-table>
    <div class="list-pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>

  <el-dialog v-model="guideDialog" :title="`设备连接指南 · ${deviceName(selected)}`" width="min(780px, 94vw)">
    <el-empty v-if="!guide" description="正在加载接入指南" />
    <div v-else class="guide-stack">
      <el-alert title="接入配置已保存" description="设备、产品和协议关系已保存，按下面步骤配置真实设备即可。" type="success" :closable="false" show-icon />
      <el-steps direction="vertical" :active="3"><el-step title="保存设备凭证" :description="`接入密钥：${guide.accessKey}。设备密钥只在注册或轮换时显示。`" /><el-step title="选择接口或消息订阅接入" description="将下面地址、主题和报文模板配置到设备或网关。" /><el-step title="发送数据并验证" description="真实设备发送后可到“原始报文”查看证据链。" /></el-steps>
      <el-card v-if="guide.gateway" shadow="never" class="inner-card"><strong>网关自动注册子设备</strong><p>网关使用自己的凭证上报，报文中的设备标识和产品标识应指向子设备。</p><pre>{{ pretty(guide.gateway.childPayloadTemplate) }}</pre><el-button @click="copy('child')">复制子设备模板</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>接口接入</strong><code>{{ guide.http.method }} {{ endpointURL(guide.http.url) }}</code><small>X-Device-Key: {{ guide.accessKey }}</small><el-button @click="copy('http')">复制接口示例</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>消息订阅接入</strong><code>{{ guide.mqtt.broker }}</code><code>{{ guide.mqtt.topic }}</code><el-button @click="copy('mqtt')">复制消息订阅参数</el-button></el-card>
      <el-card shadow="never" class="inner-card"><strong>报文模板</strong><pre>{{ pretty(guide.payloadTemplate) }}</pre><el-button @click="copy('payload')">复制报文模板</el-button></el-card>
    </div>
    <template #footer><el-button @click="guideDialog=false">关闭</el-button></template>
  </el-dialog>

  <el-dialog v-model="debugDialog" :title="`数据联调 · ${deviceName(selected)}`" width="min(780px, 94vw)">
    <el-alert title="这里用于验证协议包、归档和告警链路" description="真实设备上报后请到原始报文查看解析结果；设备明确上报的告警无需规则即可进入告警中心，测试数据也会留下审计记录。" type="info" :closable="false" show-icon />
    <el-input class="top-gap" v-model="debugPayload" type="textarea" :rows="14" />
    <el-button class="top-gap" type="primary" @click="sendDebug">发送测试数据</el-button>
    <pre>{{ debugResult }}</pre>
    <template #footer><el-button @click="debugDialog=false">关闭</el-button></template>
  </el-dialog>
</template>
