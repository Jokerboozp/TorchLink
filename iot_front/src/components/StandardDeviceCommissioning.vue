<script setup>
import { onBeforeUnmount, reactive, ref, watch } from 'vue'
import mqtt from 'mqtt'
import { api, pretty, session, formatTime } from '../api'
import { StandardDeviceProbe, standardRawID, deviceRequest } from '../standardDeviceProbe'

const form = reactive({ deviceId: '', key: '', secret: '', transport: 'HTTP', kind: 'property' })
const sample = ref('{"temperature":26.5,"smoke":0}')
const identity = ref(null), busy = ref(false), state = ref('DISCONNECTED'), retryMs = ref(0)
const connections = ref(0)
const error = ref(''), last = ref(null), result = ref(null), commands = ref([]), records = ref([])
let active = true, generation = 0
const states = { DISCONNECTED: '未连接', CONNECTING: '正在认证连接', CONNECTED: '已连接', RETRYING: '等待重连', AUTH_FAILED: '认证拒绝，请检查或重新生成凭据' }
const probe = new StandardDeviceProbe({
  connect: (url, options) => mqtt.connect(url, options),
  token: async () => {
    const auth = await deviceRequest('/api/v1/device-mqtt/token', form)
    if (!identity.value || auth.publishTopic !== `/iot/up/${session.tenant}/${identity.value.productId}/${identity.value.id}/property`) {
      throw Object.assign(new Error('凭据与所选标准设备不匹配'), { status: 403 })
    }
    return auth
  },
  onState: (value, delay = 0) => { if (active) { if(value==='CONNECTED'&&state.value!=='CONNECTED')connections.value++; state.value = value; retryMs.value = delay } },
  onCommand: (topic, body) => { commands.value = [{ topic, body: body.slice(0, 65536), at: Date.now() }, ...commands.value].slice(0, 10) }
})
function reset() { generation++; probe.stop(); identity.value = null; last.value = null; result.value = null; error.value = ''; commands.value = []; records.value = []; connections.value = 0 }
watch(() => [form.deviceId, form.key, form.secret, form.transport], reset)
async function identify() {
  const expected = generation
  if (!form.deviceId || !form.key || !form.secret) throw new Error('请填写设备标识和设备凭据')
  const x = await api(`/api/v1/device-registry/${encodeURIComponent(form.deviceId)}/connection`, {signal:AbortSignal.timeout(5000)})
  if (expected !== generation || !active) throw new Error('配置已变化，请重新操作')
  if (!['HTTP', 'MQTT'].includes(x.connector)) throw new Error('请选择通过统一向导创建的 MQTT / HTTP 标准设备')
  if (x.device.accessKey !== form.key) throw new Error('接入密钥与所选设备不匹配')
  identity.value = x.device
  return x.device
}
async function connect() {
  busy.value = true; error.value = ''
  try { await identify(); await probe.start() } catch (e) { error.value = e.message } finally { busy.value = false }
}
async function inspect(record, expected) {
  for (let i = 0; i < 15; i++) {
    if (!active || expected !== generation) return
    try {
      const detail = await api(`/api/v1/raw-messages/${encodeURIComponent(record.rawMessageId)}`, {signal:AbortSignal.timeout(5000)})
      if (!active || expected !== generation) return
      result.value = detail
      if (detail.parseStatus === 'PARSED') { record.status = '已解析'; return }
      record.status = '已归档，等待解析'
    } catch (e) { if (e.status !== 404) throw e }
    await new Promise(resolve => setTimeout(resolve, 300))
  }
  record.status = result.value ? '已归档，解析待确认' : '未查到归档，请稍后刷新'
}
async function send(repeat = false) {
  busy.value = true; error.value = ''; result.value = null
  const expected = generation
  let record
  try {
    const device = await identify()
    if (!repeat) {
      const data = JSON.parse(sample.value)
      if (!data || Array.isArray(data) || typeof data !== 'object' || !Object.keys(data).length) throw new Error('data 必须是非空 JSON 对象')
      last.value = { kind: form.kind, body: JSON.stringify({ id: crypto.randomUUID(), timestamp: Date.now(), data }) }
    }
    if (!last.value) throw new Error('尚无可重发的消息')
    const { kind, body } = last.value
    if(new TextEncoder().encode(body).length > 64*1024)throw new Error('标准上报正文不能超过 64 KiB')
    const id = JSON.parse(body).id
    record = reactive({ id, rawMessageId: await standardRawID(session.tenant, device.productId, device.id, kind, id), at: Date.now(), transport: form.transport, repeat, status: '发送中' })
    if (!active || expected !== generation) return
    records.value = [record, ...records.value].slice(0, 20)
    if (form.transport === 'HTTP') {
      const path = [session.tenant, device.productId, device.id, kind].map(encodeURIComponent).join('/')
      const response = await deviceRequest(`/api/v1/device-ingest/standard/${path}`, form, body)
      record.rawMessageId = response.messageId
      record.duplicate = !response.created
      record.status = '平台已接收'
    } else {
      await probe.publish(`/iot/up/${session.tenant}/${device.productId}/${device.id}/${kind}`, body)
      record.status = 'Broker 已确认，等待平台归档'
    }
    await inspect(record, expected)
  } catch (e) { if (active && expected === generation) { if(record){record.status='发送或确认失败';record.errorCode=e.code||e.status||'SEND_FAILED'}; error.value = `${e.code || e.status || 'SEND_FAILED'}：${e.message}` } } finally { busy.value = false }
}
async function refresh() { busy.value = true; error.value = ''; try { if (records.value[0]) await inspect(records.value[0], generation) } catch (e) { error.value = e.message } finally { busy.value = false } }
function exportReport() {
  const report = { exportedAt: new Date().toISOString(), deviceId: form.deviceId, records: records.value, scope: '浏览器标准设备联调；Broker 确认与平台解析分别记录，不代表厂商真机验收。' }
  const url = URL.createObjectURL(new Blob([pretty(report)], { type: 'application/json' }))
  const a = document.createElement('a'); a.href = url; a.download = 'device-commissioning.json'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
}
onBeforeUnmount(() => { active = false; generation++; probe.stop(); form.secret = '' })
</script>
<template>
  <el-card class="standard-commissioning" shadow="never">
    <template #header><strong>标准设备联调（MQTT / HTTP）</strong></template>
    <p>使用“添加设备”向导生成的设备凭据，按真实设备认证路径发送数据。Secret 仅保留在当前页面；离开后清除。测试数据会进入当前设备的存储和已有规则。</p>
    <el-form label-width="110px" :disabled="busy">
      <el-form-item label="标准设备标识"><el-input v-model="form.deviceId" autocomplete="off"/></el-form-item>
      <el-form-item label="接入密钥"><el-input v-model="form.key" autocomplete="off"/></el-form-item>
      <el-form-item label="设备 Secret"><el-input v-model="form.secret" type="password" show-password autocomplete="new-password"/></el-form-item>
      <el-form-item label="通信方式"><el-radio-group v-model="form.transport"><el-radio value="HTTP">HTTP</el-radio><el-radio value="MQTT">MQTT</el-radio></el-radio-group></el-form-item>
      <el-form-item label="上报类型"><el-select v-model="form.kind"><el-option label="属性" value="property"/><el-option label="事件" value="event"/><el-option label="状态" value="state"/><el-option label="命令回执" value="command-reply"/></el-select></el-form-item>
      <el-form-item label="data JSON"><el-input v-model="sample" type="textarea" :rows="5"/></el-form-item>
    </el-form>
    <p>事件：{"type":"heartbeat"}；状态：{"connectionStatus":"CONNECTED"}；命令回执：{"commandId":"收到的命令 ID","success":true}。</p>
    <template v-if="form.transport==='MQTT'"><p>{{states[state]}}{{retryMs ? `（${retryMs/1000} 秒后重试）` : ''}}</p><el-button :disabled="busy" @click="connect">连接 / 重新认证</el-button><el-button @click="probe.stop()">手动断开</el-button><el-button :disabled="busy||state!=='CONNECTED'" @click="probe.dropConnection()">模拟断链并重连</el-button><span> 本次成功连接：<b data-testid="mqtt-connect-count">{{connections}}</b> 次</span><p>网络断开自动退避重连，每次重新获取令牌；认证拒绝停止重试。离线不缓存报文，恢复连接后可手动重发。</p></template>
    <el-button type="primary" :loading="busy" :disabled="form.transport==='MQTT'&&state!=='CONNECTED'" @click="send(false)">发送新消息</el-button>
    <el-button :disabled="busy||!last||(form.transport==='MQTT'&&state!=='CONNECTED')" @click="send(true)">重发同一条消息</el-button>
    <el-button :disabled="busy||!records.length" @click="refresh">刷新解析结果</el-button><el-button :disabled="!records.length" @click="exportReport">导出验收记录</el-button>
    <el-alert v-if="error" class="top-gap" :title="error" type="error" :closable="false"/>
    <p>新消息生成新 ID；重发保持原 ID、时间戳和内容，用于验证幂等。MQTT 发布成功只表示 Broker 确认，以平台 Raw 和解析结果为准。</p>
    <el-table :data="records" empty-text="暂无联调记录"><el-table-column label="时间"><template #default="{row}">{{formatTime(row.at)}}</template></el-table-column><el-table-column prop="id" label="消息 ID"/><el-table-column prop="transport" label="通信"/><el-table-column label="动作"><template #default="{row}">{{row.repeat?'同一消息重发':'新消息'}}{{row.duplicate?' · 平台已去重':''}}</template></el-table-column><el-table-column prop="status" label="平台处理"/></el-table>
    <template v-if="last"><h4>最近发送的原始 JSON</h4><pre>{{last.body}}</pre></template>
    <template v-if="result"><h4>Raw → Parsed → StandardMessage</h4><pre>{{pretty(result)}}</pre></template>
    <template v-if="commands.length"><h4>收到的命令（不会自动执行）</h4><pre v-for="(item,i) in commands" :key="i">{{formatTime(item.at)}} {{item.topic}}\n{{item.body}}</pre></template>
  </el-card>
</template>
<style scoped>.standard-commissioning{margin-bottom:20px}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--el-fill-color-light);padding:12px}p{line-height:1.7;color:var(--el-text-color-secondary)}</style>
