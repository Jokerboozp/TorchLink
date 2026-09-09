<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import FilePicker from '../components/FilePicker.vue'
import { transportLabel } from '../presentation'
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, notifyError, parseJSON, pretty } from '../api'
import { messageTypes, parsers } from '../labels'

const file = ref(null)
const draft = ref(null)
const fields = ref([])
const preview = ref(null)
const generating = ref(false)
const testing = ref(false)
const publishing = ref(false)
const published = ref(null)
const form = reactive({ name: '', protocol: '', transport: 'MODBUS_RTU', payloadFormat: 'hex', pointTable: '', samplePayload: '', id: '', version: '1.0.0' })
const transports = ['MODBUS_RTU', 'MODBUS_TCP', 'HTTP', 'MQTT', 'TCP']

function chooseFile(event) {
  file.value = event.target.files?.[0] || null
}

function typeInfo(value) {
  return messageTypes[value] || { label: value || '未设置', description: '请确认标准消息用途' }
}

function mappingConfig() {
  return {
    ...(draft.value?.config || {}),
    messageType: draft.value?.messageType || 'PROPERTY_REPORT',
    fields: fields.value.map(item => ({ ...item }))
  }
}

function rebuild() {
  if (!draft.value) return
  draft.value.fields = fields.value.map(item => ({ ...item }))
  draft.value.config = mappingConfig()
  preview.value = null
}

function addField() {
  fields.value.push({ name: 'new_field', label: '新字段', type: 'boolean', address: 'M0', coilAddress: 0, dataType: 'BOOL', normalValue: '0', reportValue: '1', description: '' })
  rebuild()
}

function removeField(index) {
  fields.value.splice(index, 1)
  rebuild()
}

function sampleValue() {
  return form.payloadFormat === 'hex' ? form.samplePayload.trim() : parseJSON(form.samplePayload, '样本结构化数据')
}

async function generate() {
  if (!file.value && !form.pointTable.trim()) return ElMessage.warning('请上传协议文件或填写点表')
  generating.value = true
  published.value = null
  try {
    const body = new FormData()
    if (file.value) body.append('file', file.value)
    for (const [key, value] of Object.entries(form)) body.append(key, String(value ?? ''))
    const result = await api('/api/v1/ai/protocol-assistant/generate', { method: 'POST', body })
    draft.value = result
    fields.value = (result.fields || []).map(item => ({ ...item }))
    Object.assign(form, { name: result.name || form.name, protocol: result.protocol || form.protocol, transport: result.transport || form.transport, payloadFormat: result.payloadFormat || form.payloadFormat })
    preview.value = result.preview || null
    ElMessage.success('设备协议映射草稿已生成')
  } catch (error) {
    notifyError(error)
  } finally {
    generating.value = false
  }
}

async function runPreview() {
  if (!draft.value) return ElMessage.warning('请先生成协议草稿')
  if (!form.samplePayload.trim()) return ElMessage.warning('请填写真实样本报文后再预览')
  rebuild()
  testing.value = true
  try {
    const result = await api('/api/v1/ai/protocol-assistant/preview', {
      method: 'POST',
      body: JSON.stringify({ draft: { ...draft.value, fields: fields.value, config: mappingConfig() }, payload: sampleValue(), payloadFormat: form.payloadFormat })
    })
    if (result.success === false) throw new Error(result.error || '解析失败')
    preview.value = result.standardMessage
    ElMessage.success('样本解析成功')
  } catch (error) {
    preview.value = null
    notifyError(error)
  } finally {
    testing.value = false
  }
}

async function publish() {
  if (!draft.value) return ElMessage.warning('请先生成协议草稿')
  rebuild()
  publishing.value = true
  try {
    const body = { id: form.id, version: form.version, status: 'DRAFT', draft: { ...draft.value, fields: fields.value, config: mappingConfig() }, payloadFormat: form.payloadFormat }
    if (form.samplePayload.trim()) body.payload = sampleValue()
    const result = await api('/api/v1/ai/protocol-assistant/publish', { method: 'POST', body: JSON.stringify(body) })
    published.value = result.package
    preview.value = result.standardMessage || preview.value
    ElMessage.success(result.package.status === 'PUBLISHED' ? '设备协议包已发布，可到产品管理绑定产品' : '设备协议包草稿已保存，请到协议开发上传协议源码包')
  } catch (error) {
    notifyError(error)
  } finally {
    publishing.value = false
  }
}
</script>

<template>
  <div class="protocol-assistant-page">
    <el-alert title="协议接入助手" description="上传电子表格点表后由协议代码生成地址映射；不会生成或执行脚本。点表用于生成与预览映射草稿；所有专用协议均通过上传协议源码包发布。" type="info" :closable="false" show-icon />
    <div class="assistant-grid top-gap">
      <el-card shadow="never" class="surface-card">
        <template #header><div class="card-header"><strong>1. 提供协议资料</strong><el-tag type="warning" round>人工确认后发布</el-tag></div></template>
        <el-form :model="form" label-position="top">
          <el-form-item label="协议文件"><FilePicker accept=".xlsx,.pdf,.docx,.pptx,.odt,.odp,.ods,.csv,.txt,.md,.json,.html,.htm,.xml" @change="chooseFile" /><small class="subline">{{ file?.name || '支持电子表格、办公文档、逗号分隔表格和文本，最大 32 兆字节' }}</small></el-form-item>
          <el-form-item label="点表 / 协议片段"><el-input v-model="form.pointTable" type="textarea" :rows="8" placeholder="可直接粘贴变量名、线圈/寄存器地址、类型、正常值、报警值、说明" /></el-form-item>
          <div class="form-grid">
            <el-form-item label="协议名称"><el-input v-model="form.name" placeholder="例如：库卡火花探测器" /></el-form-item>
            <el-form-item label="协议标识"><el-input v-model="form.protocol" placeholder="例如：vendor-modbus-v1" /></el-form-item>
            <el-form-item label="传输方式"><el-select v-model="form.transport"><el-option v-for="item in transports" :key="item" :label="transportLabel(item)" :value="item" /></el-select></el-form-item>
            <el-form-item label="载荷格式"><el-select v-model="form.payloadFormat"><el-option label="十六进制" value="hex" /><el-option label="结构化数据" value="json" /></el-select></el-form-item>
          </div>
          <el-form-item label="样本报文"><el-input v-model="form.samplePayload" type="textarea" :rows="6" :placeholder="form.payloadFormat === 'hex' ? '例如：00 01 00 00 00 05 01 01 02 03 01' : '例如：{&quot;temperature&quot;:25.5}'" /><small class="subline">电子表格只有地址表时可以先不填，但必须在协议调试中用真实响应验证。</small></el-form-item>
          <el-button type="primary" :loading="generating" @click="generate">生成设备协议映射</el-button>
        </el-form>
      </el-card>

      <el-card shadow="never" class="surface-card">
        <template #header><div class="card-header"><div><strong>2. 字段映射</strong><small v-if="draft">{{ parsers[draft.parserType] || draft.parserType }}</small></div><el-tag v-if="draft" type="success" round>{{ fields.length }} 个字段</el-tag></div></template>
        <el-empty v-if="!draft" description="生成草稿后在这里修改地址、类型和状态定义" :image-size="70" />
        <template v-else>
          <el-form label-position="top">
            <div class="form-grid">
              <el-form-item label="草稿名称"><el-input v-model="draft.name" /></el-form-item>
              <el-form-item label="消息类型"><el-select v-model="draft.messageType" @change="rebuild"><el-option v-for="(item, key) in messageTypes" :key="key" :label="item.label" :value="key" /></el-select><small class="subline">{{ typeInfo(draft.messageType).description }} 内部代码：{{ draft.messageType }}</small></el-form-item>
            </div>
            <el-table :data="fields" border size="small">
              <el-table-column label="字段名" min-width="150"><template #default="{ row }"><el-input v-model="row.name" @change="rebuild" /></template></el-table-column>
              <el-table-column label="线圈地址" width="125"><template #default="{ row }"><el-input v-model="row.coilAddress" @change="rebuild" /></template></el-table-column>
              <el-table-column label="数据类型" width="120"><template #default="{ row }"><el-input v-model="row.dataType" @change="rebuild" /></template></el-table-column>
              <el-table-column label="正常值" width="100"><template #default="{ row }"><el-input v-model="row.normalValue" @change="rebuild" /></template></el-table-column>
              <el-table-column label="报出值" width="100"><template #default="{ row }"><el-input v-model="row.reportValue" @change="rebuild" /></template></el-table-column>
              <el-table-column label="说明" min-width="180"><template #default="{ row }"><el-input v-model="row.description" @change="rebuild" /></template></el-table-column>
              <el-table-column label="操作" width="88"><template #default="{ $index }"><el-button plain type="danger" @click="removeField($index)">删除</el-button></template></el-table-column>
            </el-table>
            <div class="table-actions top-gap"><el-button plain @click="addField">新增字段</el-button><el-button plain @click="rebuild">保存映射修改</el-button></div>
            <el-alert v-if="draft.parserType === 'go_protocol_parser'" class="top-gap" title="此草稿需要协议源码包" description="保存映射草稿后，在“设备接入 → 源码接入”上传 .go 或项目压缩包，平台自动编译、验证样例并发布。" type="warning" :closable="false" />
            <div class="form-grid top-gap"><el-form-item label="协议包标识"><el-input v-model="form.id" placeholder="留空自动生成" /></el-form-item><el-form-item label="版本"><el-input v-model="form.version" /></el-form-item></div>
            <div class="table-actions"><el-button :loading="testing" @click="runPreview">运行解析预览</el-button><el-button type="primary" :loading="publishing" @click="publish">保存协议映射草稿</el-button></div>
          </el-form>
        </template>
      </el-card>
    </div>

    <el-card v-if="draft" shadow="never" class="surface-card top-gap">
      <template #header><strong>3. 解析结果预览</strong></template>
      <el-alert v-if="draft.warnings?.length" title="需要确认" type="warning" :closable="false" :description="draft.warnings.join('；')" />
      <el-empty v-if="!preview" description="填入真实样本报文后运行预览" :image-size="58" />
      <template v-else>
        <el-descriptions class="top-gap" :column="2" border><el-descriptions-item label="消息类型">{{ typeInfo(preview.messageType).label }}（{{ preview.messageType }}）</el-descriptions-item><el-descriptions-item label="解析器">{{ parsers[preview.parser] || preview.parser }}</el-descriptions-item></el-descriptions>
        <el-tabs class="top-gap"><el-tab-pane label="标准消息"><pre>{{ pretty(preview) }}</pre></el-tab-pane><el-tab-pane label="属性字段"><el-descriptions :column="2" border><el-descriptions-item v-for="(value, key) in preview.properties || {}" :key="key" :label="key">{{ value }}</el-descriptions-item></el-descriptions></el-tab-pane></el-tabs>
      </template>
      <el-alert v-if="published" class="top-gap" type="success" :closable="false" :title="`已保存：${published.name} · ${published.id}`" :description="published.status === 'PUBLISHED' ? '下一步到产品管理把协议包绑定到对应产品。' : '当前为草稿，请到协议开发上传协议源码包后再发布。'" />
    </el-card>
  </div>
</template>
