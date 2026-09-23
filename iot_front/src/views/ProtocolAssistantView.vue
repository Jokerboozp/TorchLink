<script setup>
import { createClientId } from '../clientId' /* 引入当前代码需要的依赖。 */
import FilePicker from '../components/FilePicker.vue' /* 引入当前代码需要的依赖。 */
import ProtocolMappingEditor from '../components/ProtocolMappingEditor.vue' /* 引入当前代码需要的依赖。 */
import { mappingRows, mappingConfig } from '../protocolMapping' /* 引入当前代码需要的依赖。 */
import { transportLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, notifyError, parseJSON, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { parsers, messageTypes } from '../labels' /* 引入当前代码需要的依赖。 */

const props = defineProps({ initialRelease:{type:Object,default:null}, initialName:{type:String,default:''} }) /* 声明 props。 */
const emit = defineEmits(['navigate','saved']) /* 声明 emit。 */
const file = ref(null), sampleFile = ref(null), draft = ref(null), preview = ref(null), saved = ref(null) /* 声明 file。 */
const busy = ref(''), error = ref(''), step = ref('input'), mapping = ref([]), startAddress = ref(0) /* 声明 busy。 */
const form = reactive({ inputKind:'sample', name:'', protocol:`protocol-${createClientId().slice(0,8)}`, version:'1.0.0', transport:'MQTT', payloadFormat:'json', pointTable:'', samplePayload:'' }) /* 声明 form。 */
const isModbus = computed(() => draft.value?.parserType?.startsWith('modbus_')) /* 声明 isModbus。 */
const supported = computed(() => ['configurable_json_parser','configurable_hex_parser','modbus_tcp_parser_v2','modbus_rtu_parser_v2'].includes(draft.value?.parserType)) /* 声明 supported。 */
const transports = computed(() => form.inputKind === 'point-table' ? ['MODBUS_TCP','MODBUS_RTU'] : ['MQTT','HTTP']) /* 声明 transports。 */
const fields = computed(() => { /* 声明 fields。 */
  const config=draft.value?.config || {} /* 声明 config。 */
  if (config.points) return config.points.map(p=>({name:p.identifier,label:p.name,address:`${p.address} · FC ${p.functionCode}`,type:p.dataType})) /* 判断条件并选择处理分支。 */
  if (config.properties) return Object.entries(config.properties).map(([name,path])=>({name,address:typeof path==='string'?path:pretty(path),type:'JSON'})) /* 判断条件并选择处理分支。 */
  return (config.fields || draft.value?.fields || []).map(p=>({name:p.name,address:p.offset != null ? `偏移 ${p.offset} · ${p.length} 字节` : p.address || p.coilAddress,type:p.type || p.dataType})) /* 返回当前处理结果。 */
}) /* 结束当前表达式或代码块。 */
let controller, disposed=false /* 声明 controller。 */
onBeforeUnmount(()=>{disposed=true;controller?.abort()}) /* 执行当前语句并推进处理流程。 */
function changeKind() { file.value=null;form.samplePayload='';form.pointTable=''; form.transport=form.inputKind==='point-table'?'MODBUS_TCP':'MQTT';form.payloadFormat=form.inputKind==='point-table'?'hex':'json';error.value='' } /* 定义 changeKind 函数。 */
function chooseFile(event) {file.value=event.target.files?.[0] || null;error.value='';if(file.value && !form.name) form.name=file.value.name.replace(/\.[^.]+$/,'');if(/\.(hex|bin)$/i.test(file.value?.name || '')) form.payloadFormat='hex'} /* 定义 chooseFile 函数。 */
async function chooseSample(event) { /* 定义 chooseSample 函数。 */
  const selected=event.target.files?.[0];sampleFile.value=selected || null /* 声明 selected。 */
  if (!selected) return /* 判断条件并选择处理分支。 */
  if (selected.size>1024*1024) {error.value='样本文件不能超过 1 MiB';return} /* 判断条件并选择处理分支。 */
  try {const value=/\.bin$/i.test(selected.name)?Array.from(new Uint8Array(await selected.arrayBuffer()),v=>v.toString(16).padStart(2,'0')).join(' '):await selected.text();if(!disposed){form.samplePayload=value;preview.value=null}} /* 执行当前语句并推进处理流程。 */
  catch(e){error.value=e.message} /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function work(name, action) { /* 定义 work 函数。 */
  if (busy.value) return /* 判断条件并选择处理分支。 */
  busy.value=name;error.value='';controller=new AbortController() /* 更新 busy.value 的值。 */
  try {await action({signal:controller.signal})} catch(e) {if(!disposed&&e.name!=='AbortError'){error.value=e.message;notifyError(e)}} finally {if(!disposed)busy.value=''} /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
function setDraft(value) {draft.value=value;mapping.value=mappingRows(value.config || {}, value.parserType);preview.value=value.preview || null;startAddress.value=value.config?.blocks?.[0]?.startAddress || 0;step.value='review'} /* 定义 setDraft 函数。 */
async function generate() { /* 定义 generate 函数。 */
  if (!file.value && !(form.inputKind==='point-table'?form.pointTable.trim():form.samplePayload.trim())) return UiMessage.warning('请上传文件或填写内容') /* 判断条件并选择处理分支。 */
  if (file.value?.size>32*1024*1024) return UiMessage.warning('文件不能超过 32 MiB') /* 判断条件并选择处理分支。 */
  await work('generate',async options=>{ /* 等待异步操作完成。 */
    const body=new FormData();if(file.value)body.append('file',file.value) /* 声明 body。 */
    for(const [key,value] of Object.entries(form))body.append(key,String(value)) /* 循环处理当前数据。 */
    const value=await api('/api/v1/ai/protocol-assistant/generate',{...options,method:'POST',body}) /* 声明 value。 */
    if(disposed)return /* 判断条件并选择处理分支。 */
    saved.value=null;setDraft(value) /* 更新 saved.value 的值。 */
    if(value.samplePayload!=null)form.samplePayload=typeof value.samplePayload==='string'?value.samplePayload:pretty(value.samplePayload) /* 判断条件并选择处理分支。 */
    if(value.name)form.name=value.name /* 判断条件并选择处理分支。 */
    form.transport=value.transport;form.payloadFormat=value.payloadFormat /* 更新 form.transport 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
function newVersion() { saved.value=null;form.version=`auto-${Date.now()}`;draft.value={...draft.value,config:JSON.parse(JSON.stringify(draft.value.config))};mapping.value=mappingRows(draft.value.config,draft.value.parserType);preview.value=null } /* 定义 newVersion 函数。 */
function updateConfig() { preview.value=null } /* 定义 updateConfig 函数。 */
function addMapping() { /* 定义 addMapping 函数。 */
  mapping.value.push(isModbus.value /* 执行当前语句并推进处理流程。 */
    ? {identifier:'',name:'',functionCode:3,address:0,addressNotation:'zero_based',dataType:'uint16',registerCount:1,byteOrder:'big',wordOrder:'ABCD',scale:1,offset:0,pollIntervalSec:10,access:'read'} /* 执行当前语句并推进处理流程。 */
    : draft.value.parserType==='configurable_json_parser' ? {name:'',path:'',type:'',scale:1,original:''} /* 执行当前语句并推进处理流程。 */
    : {name:'',offset:0,length:2,type:'uint16',endian:'big',scale:1}) /* 执行当前语句并推进处理流程。 */
  updateConfig() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
function removeMapping(index) { mapping.value.splice(index,1);updateConfig() } /* 定义 removeMapping 函数。 */
function currentDraft() { /* 定义 currentDraft 函数。 */
  const config=saved.value ? saved.value.config : mappingConfig(draft.value.config,draft.value.parserType,mapping.value) /* 声明 config。 */
  return {...draft.value,name:form.name,protocol:form.protocol,config:{...config,...(isModbus.value?{startAddress:startAddress.value}:{})}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function payload() {return form.payloadFormat==='hex'?form.samplePayload.trim():parseJSON(form.samplePayload,'JSON 样本')} /* 定义 payload 函数。 */
async function runPreview() { /* 定义 runPreview 函数。 */
  if(!form.samplePayload.trim())return UiMessage.warning('请上传或填写真实样本报文') /* 判断条件并选择处理分支。 */
  preview.value=null /* 更新 preview.value 的值。 */
  await work('preview',async options=>{ /* 等待异步操作完成。 */
    const value=saved.value /* 声明 value。 */
      ? await api(`/api/v2/protocols/${encodeURIComponent(saved.value.protocolId)}/releases/${encodeURIComponent(saved.value.version)}/preview`,{...options,method:'POST',body:JSON.stringify({payload:payload(),...(isModbus.value?{startAddress:startAddress.value}:{})})}) /* 执行当前语句并推进处理流程。 */
      : await api('/api/v1/ai/protocol-assistant/preview',{...options,method:'POST',body:JSON.stringify({draft:currentDraft(),payload:payload(),payloadFormat:form.payloadFormat})}) /* 执行当前语句并推进处理流程。 */
    if(value.success===false)throw new Error(value.error || '解析失败') /* 判断条件并选择处理分支。 */
    if(disposed)return /* 判断条件并选择处理分支。 */
    preview.value=value.standardMessage /* 更新 preview.value 的值。 */
    if(value.release){saved.value=value.release;emit('saved')} /* 判断条件并选择处理分支。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function save() { /* 定义 save 函数。 */
  await work('save',async options=>{ /* 等待异步操作完成。 */
    const value=await api('/api/v1/ai/protocol-assistant/publish',{...options,method:'POST',body:JSON.stringify({id:form.protocol,version:form.version,status:'DRAFT',draft:currentDraft(),payloadFormat:form.payloadFormat,...(form.samplePayload.trim()?{payload:payload()}:{})})}) /* 声明 value。 */
    if(disposed)return /* 判断条件并选择处理分支。 */
    saved.value=value.release;draft.value.config=value.release.config;preview.value=value.standardMessage || preview.value;emit('saved');UiMessage.success('协议已保存') /* 更新 saved.value 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function publish() { /* 定义 publish 函数。 */
  await work('publish',async options=>{ /* 等待异步操作完成。 */
    const release=saved.value /* 声明 release。 */
    const value=await api(`/api/v2/protocols/${encodeURIComponent(release.protocolId)}/releases/${encodeURIComponent(release.version)}/publish`,{...options,method:'POST',body:'{}'}) /* 声明 value。 */
    if(disposed)return /* 判断条件并选择处理分支。 */
    saved.value=value;emit('saved');UiMessage.success('协议已发布，可到产品管理绑定') /* 更新 saved.value 的值。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
onMounted(()=>{ /* 执行当前语句并推进处理流程。 */
  if(!props.initialRelease)return /* 判断条件并选择处理分支。 */
  const release=props.initialRelease /* 声明 release。 */
  Object.assign(form,{inputKind:release.parserType.startsWith('modbus_')?'point-table':'sample',name:props.initialName || release.protocolId,protocol:release.protocolId,version:release.version,transport:release.transport,payloadFormat:release.payloadFormat}) /* 执行当前语句并推进处理流程。 */
  setDraft({...release,protocol:release.protocolId,name:form.name});saved.value=release /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
</script>

<template>
  <div class="protocol-generator" :aria-busy="!!busy"> <!-- 渲染 div 界面元素。 -->
    <ui-alert v-if="error" :title="error" type="error" :closable="false" class="bottom-gap" /> <!-- 渲染 ui-alert 界面元素。 -->
    <ui-form v-if="step==='input'" label-position="top" :disabled="!!busy" @submit.prevent="generate"> <!-- 渲染 ui-form 界面元素。 -->
      <section class="generator-section"><div class="generator-section-heading"><h3>提供协议资料</h3><p>选择报文或点表，上传文件或直接粘贴内容。</p></div>
      <ui-radio-group v-model="form.inputKind" @change="changeKind" class="bottom-gap segmented-choice-group" aria-label="上传类型"><ui-radio-button value="sample">报文</ui-radio-button><ui-radio-button value="point-table">点表</ui-radio-button></ui-radio-group> <!-- 渲染 ui-radio-group 界面元素。 -->
      <ui-form-item :label="form.inputKind==='point-table'?'上传点表':'上传报文'"><FilePicker :key="form.inputKind" :accept="form.inputKind==='point-table'?'.xlsx,.csv':'.json,.txt,.hex,.bin'" @change="chooseFile" /><small class="subline">{{ file?.name || (form.inputKind==='point-table'?'Excel / CSV':'JSON / TXT / HEX / BIN') }}</small></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item v-if="form.inputKind==='point-table'" label="或粘贴 CSV 点表"><ui-input v-model="form.pointTable" type="textarea" :rows="5" placeholder="name,functionCode,address,addressNotation,dataType&#10;temperature,3,0,zero_based,uint16" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item v-else label="或粘贴报文"><ui-input v-model="form.samplePayload" type="textarea" :rows="5" placeholder='{"data":{"temperature":25.5,"smoke":false}}' /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      </section>
      <section class="generator-section"><div class="generator-section-heading"><h3>协议基本信息</h3><p>确认协议名称、传输方式和样本解析格式。</p></div>
      <div class="form-grid"><ui-form-item label="协议名称"><ui-input v-model="form.name" placeholder="可由文件名生成" /></ui-form-item><ui-form-item label="传输方式"><ui-select v-model="form.transport"><ui-option v-for="item in transports" :key="item" :label="transportLabel(item)" :value="item" /></ui-select></ui-form-item></div> <!-- 渲染 div 界面元素。 -->
      <template v-if="form.inputKind==='sample'"><ui-form-item label="报文格式"><ui-radio-group v-model="form.payloadFormat"><ui-radio value="json">JSON</ui-radio><ui-radio value="hex">HEX</ui-radio></ui-radio-group></ui-form-item><ui-form-item label="字段说明（可选）"><ui-input v-model="form.pointTable" type="textarea" :rows="2" placeholder="HEX 报文请说明字段偏移、长度、端序与单位" /></ui-form-item></template>
      </section>
      <div class="generator-actions"><ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" type="primary" :loading="busy==='generate'" native-type="submit">生成协议</ui-button></div> <!-- 渲染 div 界面元素。 -->
    </ui-form> <!-- 结束当前界面区域。 -->
    <template v-else>
      <div class="section-toolbar"><strong>{{ form.name }} <small class="muted-text">{{ parsers[draft.parserType] || draft.parserType }}</small></strong><ui-button v-if="!saved" :disabled="!!busy" @click="step='input'">返回修改</ui-button><ui-button v-if="saved" :disabled="!!busy" @click="newVersion">新建版本</ui-button><ui-tag v-if="saved" :type="saved.status==='PUBLISHED'?'success':'info'">{{ {DRAFT:'草稿',VALIDATED:'已校验',PUBLISHED:'已发布'}[saved.status] || saved.status }}</ui-tag></div> <!-- 渲染 div 界面元素。 -->
      <details v-if="draft.warnings?.length" class="technical-details bottom-gap"><summary>需确认 {{ draft.warnings.length }} 项</summary><ul><li v-for="warning in draft.warnings" :key="warning">{{ warning }}</li></ul></details> <!-- 渲染 details 界面元素。 -->
      <ui-table v-if="saved || !supported" :data="fields" max-height="230" size="small"><ui-table-column prop="name" label="字段" min-width="140" /><ui-table-column prop="address" label="路径 / 地址" min-width="190" /><ui-table-column prop="type" label="类型" min-width="100" /></ui-table> <!-- 渲染 ui-table 界面元素。 -->
      <template v-if="supported">
        <ui-form label-position="top" class="top-gap" :disabled="!!busy"> <!-- 渲染 ui-form 界面元素。 -->
          <div class="form-grid"><ui-form-item label="协议标识"><ui-input v-model="form.protocol" :disabled="!!saved" /></ui-form-item><ui-form-item label="版本"><ui-input v-model="form.version" :disabled="!!saved" /></ui-form-item></div> <!-- 渲染 div 界面元素。 -->
          <ProtocolMappingEditor v-if="!saved" :rows="mapping" :parser-type="draft.parserType" @change="updateConfig" @add="addMapping" @remove="removeMapping" /> <!-- 渲染 ProtocolMappingEditor 界面元素。 -->
          <ui-form-item label="样本报文"><FilePicker accept=".json,.txt,.hex,.bin" @change="chooseSample" /><ui-input v-model="form.samplePayload" type="textarea" :rows="4" class="top-gap" @input="preview=null" :placeholder="isModbus?'填写设备返回的完整 Modbus 响应帧':'填写真实样本验证解析结果'" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
          <ui-form-item v-if="isModbus" label="响应起始地址"><ui-input-number v-model="startAddress" :min="0" :max="65535" :precision="0" @change="preview=null" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
          <div class="generator-actions"><ui-button v-permission="['POST /api/v1/ai/protocol-assistant/preview','POST /api/v2/protocols/:id/releases/:version/preview']" :loading="busy==='preview'" @click="runPreview">解析预览</ui-button><ui-button v-permission="'POST /api/v1/ai/protocol-assistant/publish'" v-if="!saved" type="primary" :loading="busy==='save'" @click="save">保存协议</ui-button><ui-button v-permission="'POST /api/v2/protocols/:id/releases/:version/publish'" v-else-if="saved.status==='VALIDATED'" type="primary" :loading="busy==='publish'" @click="publish">发布协议</ui-button><span v-else-if="saved.status==='DRAFT'" class="muted-text">样本校验通过后可发布</span><ui-button v-permission="'menu:products'" v-else-if="saved.status==='PUBLISHED'" @click="emit('navigate','products')">绑定产品</ui-button></div> <!-- 渲染 div 界面元素。 -->
        </ui-form> <!-- 结束当前界面区域。 -->
        <ui-descriptions v-if="preview" class="top-gap" :column="1" border><ui-descriptions-item label="消息类型">{{ messageTypes[preview.messageType]?.label || preview.messageType }}</ui-descriptions-item><ui-descriptions-item v-if="Object.keys(preview.event || {}).length" label="事件">{{ pretty(preview.event) }}</ui-descriptions-item><ui-descriptions-item v-for="(value,key) in preview.properties" :key="key" :label="key">{{ typeof value==='object'?pretty(value):value }}</ui-descriptions-item></ui-descriptions> <!-- 渲染 ui-descriptions 界面元素。 -->
      </template>
      <ui-alert v-else class="top-gap" type="info" title="当前资料不足以生成可运行映射，请补充字段说明，或上传专用 Go 协议源码。" :closable="false" />
    </template>
  </div>
</template>
<style scoped>
.generator-section{padding:16px 18px;margin-bottom:13px;border:1px solid #dce6f1;border-radius:10px;background:#f9fbfe}.generator-section-heading{margin-bottom:14px}.generator-section-heading h3{margin:0;color:#223f60;font-size:14px}.generator-section-heading p{margin:5px 0 0;color:#64778c;font-size:12px;line-height:1.6}.generator-section :deep(.n-form-item:last-child){margin-bottom:0}
@media(max-width:640px){.generator-section{padding:13px}}
</style>
