<script setup>
import FilePicker from '../components/FilePicker.vue'
import { transportLabel } from '../presentation'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, notifyError, parseJSON, pretty } from '../api'
import { parsers, messageTypes } from '../labels'

const props = defineProps({ initialRelease:{type:Object,default:null}, initialName:{type:String,default:''} })
const emit = defineEmits(['navigate','saved'])
const file = ref(null), sampleFile = ref(null), draft = ref(null), preview = ref(null), saved = ref(null)
const busy = ref(''), error = ref(''), step = ref('input'), configText = ref(''), startAddress = ref(0)
const form = reactive({ inputKind:'sample', name:'', protocol:`protocol-${crypto.randomUUID().slice(0,8)}`, version:'1.0.0', transport:'MQTT', payloadFormat:'json', pointTable:'', samplePayload:'' })
const isModbus = computed(() => draft.value?.parserType?.startsWith('modbus_'))
const supported = computed(() => ['configurable_json_parser','configurable_hex_parser','modbus_tcp_parser_v2','modbus_rtu_parser_v2'].includes(draft.value?.parserType))
const transports = computed(() => form.inputKind === 'point-table' ? ['MODBUS_TCP','MODBUS_RTU'] : ['MQTT','HTTP'])
const fields = computed(() => {
  const config=draft.value?.config || {}
  if (config.points) return config.points.map(p=>({name:p.identifier,label:p.name,address:`${p.address} · FC ${p.functionCode}`,type:p.dataType}))
  if (config.properties) return Object.entries(config.properties).map(([name,path])=>({name,address:typeof path==='string'?path:pretty(path),type:'JSON'}))
  return (config.fields || draft.value?.fields || []).map(p=>({name:p.name,address:p.offset != null ? `偏移 ${p.offset} · ${p.length} 字节` : p.address || p.coilAddress,type:p.type || p.dataType}))
})
let controller, disposed=false
onBeforeUnmount(()=>{disposed=true;controller?.abort()})
function changeKind() { file.value=null;form.samplePayload='';form.pointTable=''; form.transport=form.inputKind==='point-table'?'MODBUS_TCP':'MQTT';form.payloadFormat=form.inputKind==='point-table'?'hex':'json';error.value='' }
function chooseFile(event) {file.value=event.target.files?.[0] || null;error.value='';if(file.value && !form.name) form.name=file.value.name.replace(/\.[^.]+$/,'');if(/\.(hex|bin)$/i.test(file.value?.name || '')) form.payloadFormat='hex'}
async function chooseSample(event) {
  const selected=event.target.files?.[0];sampleFile.value=selected || null
  if (!selected) return
  if (selected.size>1024*1024) {error.value='样本文件不能超过 1 MiB';return}
  try {const value=/\.bin$/i.test(selected.name)?Array.from(new Uint8Array(await selected.arrayBuffer()),v=>v.toString(16).padStart(2,'0')).join(' '):await selected.text();if(!disposed){form.samplePayload=value;preview.value=null}}
  catch(e){error.value=e.message}
}
async function work(name, action) {
  if (busy.value) return
  busy.value=name;error.value='';controller=new AbortController()
  try {await action({signal:controller.signal})} catch(e) {if(!disposed&&e.name!=='AbortError'){error.value=e.message;notifyError(e)}} finally {if(!disposed)busy.value=''}
}
function setDraft(value) {draft.value=value;configText.value=pretty(value.config || {});preview.value=value.preview || null;startAddress.value=value.config?.blocks?.[0]?.startAddress || 0;step.value='review'}
async function generate() {
  if (!file.value && !(form.inputKind==='point-table'?form.pointTable.trim():form.samplePayload.trim())) return ElMessage.warning('请上传文件或填写内容')
  if (file.value?.size>32*1024*1024) return ElMessage.warning('文件不能超过 32 MiB')
  await work('generate',async options=>{
    const body=new FormData();if(file.value)body.append('file',file.value)
    for(const [key,value] of Object.entries(form))body.append(key,String(value))
    const value=await api('/api/v1/ai/protocol-assistant/generate',{...options,method:'POST',body})
    if(disposed)return
    saved.value=null;setDraft(value)
    if(value.samplePayload!=null)form.samplePayload=typeof value.samplePayload==='string'?value.samplePayload:pretty(value.samplePayload)
    if(value.name)form.name=value.name
    form.transport=value.transport;form.payloadFormat=value.payloadFormat
  })
}
function newVersion() { saved.value=null;form.version=`auto-${Date.now()}`;configText.value=pretty(draft.value.config);preview.value=null }
function updateConfig(value) { preview.value=null;try {const config=JSON.parse(value);if(config && typeof config==='object' && !Array.isArray(config))draft.value.config=config} catch {} }
function currentDraft() {
  const config=saved.value ? saved.value.config : parseJSON(configText.value,'字段映射')
  if (!config || Array.isArray(config) || typeof config!=='object') throw new Error('字段映射须为 JSON 对象')
  return {...draft.value,name:form.name,protocol:form.protocol,config:{...config,...(isModbus.value?{startAddress:startAddress.value}:{})}}
}
function payload() {return form.payloadFormat==='hex'?form.samplePayload.trim():parseJSON(form.samplePayload,'JSON 样本')}
async function runPreview() {
  if(!form.samplePayload.trim())return ElMessage.warning('请上传或填写真实样本报文')
  preview.value=null
  await work('preview',async options=>{
    const value=saved.value
      ? await api(`/api/v2/protocols/${encodeURIComponent(saved.value.protocolId)}/releases/${encodeURIComponent(saved.value.version)}/preview`,{...options,method:'POST',body:JSON.stringify({payload:payload(),...(isModbus.value?{startAddress:startAddress.value}:{})})})
      : await api('/api/v1/ai/protocol-assistant/preview',{...options,method:'POST',body:JSON.stringify({draft:currentDraft(),payload:payload(),payloadFormat:form.payloadFormat})})
    if(value.success===false)throw new Error(value.error || '解析失败')
    if(disposed)return
    preview.value=value.standardMessage
    if(value.release){saved.value=value.release;emit('saved')}
  })
}
async function save() {
  await work('save',async options=>{
    const value=await api('/api/v1/ai/protocol-assistant/publish',{...options,method:'POST',body:JSON.stringify({id:form.protocol,version:form.version,status:'DRAFT',draft:currentDraft(),payloadFormat:form.payloadFormat,...(form.samplePayload.trim()?{payload:payload()}:{})})})
    if(disposed)return
    saved.value=value.release;draft.value.config=value.release.config;preview.value=value.standardMessage || preview.value;emit('saved');ElMessage.success('协议已保存')
  })
}
async function publish() {
  await work('publish',async options=>{
    const release=saved.value
    const value=await api(`/api/v2/protocols/${encodeURIComponent(release.protocolId)}/releases/${encodeURIComponent(release.version)}/publish`,{...options,method:'POST',body:'{}'})
    if(disposed)return
    saved.value=value;emit('saved');ElMessage.success('协议已发布，可到产品管理绑定')
  })
}
onMounted(()=>{
  if(!props.initialRelease)return
  const release=props.initialRelease
  Object.assign(form,{inputKind:release.parserType.startsWith('modbus_')?'point-table':'sample',name:props.initialName || release.protocolId,protocol:release.protocolId,version:release.version,transport:release.transport,payloadFormat:release.payloadFormat})
  setDraft({...release,protocol:release.protocolId,name:form.name});saved.value=release
})
</script>

<template>
  <div class="protocol-generator" :aria-busy="!!busy">
    <el-alert v-if="error" :title="error" type="error" :closable="false" class="bottom-gap" />
    <el-form v-if="step==='input'" label-position="top" :disabled="!!busy" @submit.prevent="generate">
      <el-radio-group v-model="form.inputKind" @change="changeKind" class="bottom-gap" aria-label="上传类型"><el-radio-button value="sample">报文</el-radio-button><el-radio-button value="point-table">点表</el-radio-button></el-radio-group>
      <el-form-item :label="form.inputKind==='point-table'?'上传点表':'上传报文'"><FilePicker :key="form.inputKind" :accept="form.inputKind==='point-table'?'.xlsx,.csv':'.json,.txt,.hex,.bin'" @change="chooseFile" /><small class="subline">{{ file?.name || (form.inputKind==='point-table'?'Excel / CSV':'JSON / TXT / HEX / BIN') }}</small></el-form-item>
      <el-form-item v-if="form.inputKind==='point-table'" label="或粘贴 CSV 点表"><el-input v-model="form.pointTable" type="textarea" :rows="5" placeholder="name,functionCode,address,addressNotation,dataType&#10;temperature,3,0,zero_based,uint16" /></el-form-item>
      <el-form-item v-else label="或粘贴报文"><el-input v-model="form.samplePayload" type="textarea" :rows="5" placeholder='{"data":{"temperature":25.5,"smoke":false}}' /></el-form-item>
      <div class="form-grid"><el-form-item label="协议名称"><el-input v-model="form.name" placeholder="可由文件名生成" /></el-form-item><el-form-item label="传输方式"><el-select v-model="form.transport"><el-option v-for="item in transports" :key="item" :label="transportLabel(item)" :value="item" /></el-select></el-form-item></div>
      <template v-if="form.inputKind==='sample'"><el-form-item label="报文格式"><el-radio-group v-model="form.payloadFormat"><el-radio value="json">JSON</el-radio><el-radio value="hex">HEX</el-radio></el-radio-group></el-form-item><el-form-item label="字段说明（可选）"><el-input v-model="form.pointTable" type="textarea" :rows="2" placeholder="HEX 报文请说明字段偏移、长度、端序与单位" /></el-form-item></template>
      <div class="generator-actions"><el-button type="primary" :loading="busy==='generate'" native-type="submit">生成协议</el-button></div>
    </el-form>
    <template v-else>
      <div class="section-toolbar"><strong>{{ form.name }} <small class="muted-text">{{ parsers[draft.parserType] || draft.parserType }}</small></strong><el-button v-if="!saved" :disabled="!!busy" @click="step='input'">返回修改</el-button><el-button v-if="saved" :disabled="!!busy" @click="newVersion">新建版本</el-button><el-tag v-if="saved" :type="saved.status==='PUBLISHED'?'success':'info'">{{ {DRAFT:'草稿',VALIDATED:'已校验',PUBLISHED:'已发布'}[saved.status] || saved.status }}</el-tag></div>
      <details v-if="draft.warnings?.length" class="technical-details bottom-gap"><summary>需确认 {{ draft.warnings.length }} 项</summary><ul><li v-for="warning in draft.warnings" :key="warning">{{ warning }}</li></ul></details>
      <el-table :data="fields" max-height="230" size="small"><el-table-column prop="name" label="字段" min-width="140" /><el-table-column prop="address" label="路径 / 地址" min-width="190" /><el-table-column prop="type" label="类型" min-width="100" /></el-table>
      <template v-if="supported">
        <el-form label-position="top" class="top-gap" :disabled="!!busy">
          <div class="form-grid"><el-form-item label="协议标识"><el-input v-model="form.protocol" :disabled="!!saved" /></el-form-item><el-form-item label="版本"><el-input v-model="form.version" :disabled="!!saved" /></el-form-item></div>
          <details v-if="!saved" class="technical-details bottom-gap"><summary>编辑字段映射</summary><el-input v-model="configText" type="textarea" :rows="8" @input="updateConfig" /></details>
          <el-form-item label="样本报文"><FilePicker accept=".json,.txt,.hex,.bin" @change="chooseSample" /><el-input v-model="form.samplePayload" type="textarea" :rows="4" class="top-gap" @input="preview=null" :placeholder="isModbus?'填写设备返回的完整 Modbus 响应帧':'填写真实样本验证解析结果'" /></el-form-item>
          <el-form-item v-if="isModbus" label="响应起始地址"><el-input-number v-model="startAddress" :min="0" :max="65535" :precision="0" @change="preview=null" /></el-form-item>
          <div class="generator-actions"><el-button :loading="busy==='preview'" @click="runPreview">解析预览</el-button><el-button v-if="!saved" type="primary" :loading="busy==='save'" @click="save">保存协议</el-button><el-button v-else-if="saved.status==='VALIDATED'" type="primary" :loading="busy==='publish'" @click="publish">发布协议</el-button><span v-else-if="saved.status==='DRAFT'" class="muted-text">样本校验通过后可发布</span><el-button v-else-if="saved.status==='PUBLISHED'" @click="emit('navigate','products')">绑定产品</el-button></div>
        </el-form>
        <el-descriptions v-if="preview" class="top-gap" :column="1" border><el-descriptions-item label="消息类型">{{ messageTypes[preview.messageType]?.label || preview.messageType }}</el-descriptions-item><el-descriptions-item v-if="Object.keys(preview.event || {}).length" label="事件">{{ pretty(preview.event) }}</el-descriptions-item><el-descriptions-item v-for="(value,key) in preview.properties" :key="key" :label="key">{{ typeof value==='object'?pretty(value):value }}</el-descriptions-item></el-descriptions>
      </template>
      <el-alert v-else class="top-gap" type="info" title="当前资料不足以生成可运行映射，请补充字段说明，或上传专用 Go 协议源码。" :closable="false" />
    </template>
  </div>
</template>
