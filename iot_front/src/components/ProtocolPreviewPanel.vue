<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, apiAll, parseJSON, pretty } from '../api'
import { transportLabel } from '../presentation'
import { messageTypeLabel } from '../labels'
import { can } from '../permissions'

const props = defineProps({ context:{type:Object,default:null}, initialProductId:{type:String,default:''}, initialProtocolId:{type:String,default:''}, initialVersion:{type:String,default:''} })
let origin=props.context || {}
const initialProduct=origin.productId || props.initialProductId || '', initialProtocol=origin.protocolId || props.initialProtocolId || '', initialVersion=origin.version || props.initialVersion || ''
const products=ref([]), protocols=ref([]), productId=ref(initialProduct), packageId=ref(initialProtocol && initialVersion ? `${initialProtocol}@${initialVersion}` : '')
const mode=ref(packageId.value?'release':initialProduct?'template':'release'), payload=ref(''), startAddress=ref(0), deviceId=ref(origin.deviceId || 'protocol_preview'), state=ref('{}')
const messageKind=ref('property')
const messageKinds=[{value:'property',label:'属性上报'},{value:'event',label:'事件上报'},{value:'alarm',label:'告警上报'},{value:'state',label:'状态变更'},{value:'command-reply',label:'命令回复'}]
const operation=ref('decode'), chunks=ref(''), datagram=ref(false), command=ref('{"type":"ping"}'), expected=ref(''), sampleTime=ref(0)
const rawMessageId=ref(origin.rawMessageId || ''), rawInfo=ref(null), rawLoading=ref(false)
const loading=ref(false), busy=ref(false), error=ref(''), result=ref(null)
let disposed=false, revision=0, controller, rawController, rawGeneration=0, contextLoaded=false, applyingRaw=false
const releases=computed(()=>protocols.value.flatMap(item=>(item.releases || []).filter(release=>release.status!=='REVOKED').map(release=>({...release,id:`${release.protocolId}@${release.version}`,name:item.definition.name}))))
const selectedProduct=computed(()=>products.value.find(item=>item.id===productId.value))
const selectedRelease=computed(()=>releases.value.find(item=>item.id===(mode.value==='template'?selectedProduct.value?.protocolPackageId:packageId.value)))
const modbus=computed(()=>selectedRelease.value?.parserType?.startsWith('modbus_'))
const standardProtocol=computed(()=>selectedRelease.value?.parserType==='iot_standard_parser')
const goProtocol=computed(()=>selectedRelease.value?.parserType==='go_protocol_parser')
const expectedPlaceholder=computed(()=>operation.value==='decode'?'例如 {"standardMessage":{"properties":{"temperature":42}}}':operation.value==='ingress'?'例如 {"needMore":false,"frames":[{"reply":"AA012A"}]}':'例如 {"reply":"AA012A"}')
const operationOptions=computed(()=>[{value:'decode',label:'decode · 解析'},{value:'ingress',label:'ingress · 拆帧与应答'},{value:'encode',label:'encode · 命令编码'}].filter(item=>item.value==='decode' || (goProtocol.value && selectedRelease.value?.capabilities?.includes(item.value))))
watch([mode,productId,packageId,payload,startAddress,deviceId,state,operation,chunks,datagram,command,expected,sampleTime,messageKind],()=>{revision+=1;result.value=null;error.value=''}, {flush:'sync'})
watch([mode,productId,packageId,deviceId,rawMessageId],()=>{if(!applyingRaw){revision+=1;result.value=null;rawInfo.value=null;rawController?.abort();rawGeneration+=1;rawLoading.value=false}}, {flush:'sync'})
watch(selectedRelease,()=>{if(!operationOptions.value.some(item=>item.value===operation.value))operation.value='decode'})
watch(()=>props.context,async value=>{origin=value || {};rawInfo.value=null;productId.value=origin.productId || '';packageId.value=origin.protocolId && origin.version?`${origin.protocolId}@${origin.version}`:'';mode.value=packageId.value?'release':productId.value?'template':'release';deviceId.value=origin.deviceId || 'protocol_preview';payload.value='';chunks.value='';state.value='{}';expected.value='';sampleTime.value=0;rawMessageId.value=origin.rawMessageId || '';if(rawMessageId.value)await loadRaw()}, {deep:true})
onBeforeUnmount(()=>{disposed=true;controller?.abort();rawController?.abort();rawGeneration+=1})
onMounted(async()=>{await load();if(!disposed && rawMessageId.value && !contextLoaded){contextLoaded=true;await loadRaw()}})
async function load() {
  if(loading.value)return
  loading.value=true;error.value='';revision+=1;result.value=null
  try {
    const [catalog,templates]=await Promise.all([api('/api/v2/protocols'),can('GET /api/v1/products')?apiAll('/api/v1/products'):Promise.resolve({items:[]})])
    if(disposed)return
    protocols.value=catalog.items || [];products.value=templates.items || []
  } catch(e){if(!disposed)error.value=e.message} finally{if(!disposed)loading.value=false}
}
async function loadRaw() {
  if(!can('GET /api/v1/raw-messages/:id') || !rawMessageId.value.trim())return
  const id=rawMessageId.value.trim(), generation=++rawGeneration
  rawController?.abort();rawController=new AbortController();rawLoading.value=true;error.value='';revision+=1;result.value=null
  try {
    const detail=await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`,{signal:rawController.signal})
    if(disposed || generation!==rawGeneration || id!==rawMessageId.value.trim())return
    const raw=detail.message
    if(!raw?.protocolId || !raw?.protocolVersion)throw new Error('此原文没有归档协议版本，不能用当前版本代替')
    if(origin.deviceId && raw.deviceId!==origin.deviceId)throw new Error('原文不属于当前设备，请核对原文编号')
    applyingRaw=true
    mode.value='release';packageId.value=`${raw.protocolId}@${raw.protocolVersion}`;productId.value=raw.productId || '';deviceId.value=raw.deviceId || 'protocol_preview'
    operation.value='decode';messageKind.value=raw.headers?.messageKind || '';payload.value=raw.payloadFormat==='hex'?String(raw.payload || ''):pretty(raw.payload);chunks.value=raw.payloadFormat==='hex'?String(raw.payload || ''):''
    state.value=pretty(raw.metadata?.protocolState || {});sampleTime.value=raw.receivedAt || 0;startAddress.value=Number(raw.metadata?.startAddress || 0)
    const message=detail.standardMessage
    expected.value=message?pretty({standardMessage:Object.fromEntries(['messageType','properties','event','tags'].filter(key=>message[key]!=null).map(key=>[key,message[key]]))}):''
    rawInfo.value={id,deviceId:raw.deviceId,protocolPackageId:packageId.value,parseStatus:detail.parseStatus,parseError:detail.parseError}
    applyingRaw=false
    if(!selectedRelease.value)error.value='已载入原文，但归档版本不可用或已撤销；不会自动改用其他版本'
  }catch(e){applyingRaw=false;if(!disposed && generation===rawGeneration && e.name!=='AbortError')error.value=e.message}finally{if(!disposed && generation===rawGeneration)rawLoading.value=false}
}
function fillStandardSample() {
  rawMessageId.value='';rawInfo.value=null;sampleTime.value=0;expected.value=''
  const sample={version:'1.0',id:'preview-1',timestamp:Date.now(),data:{}}
  if(messageKind.value==='property')sample.data={temperature:25}
  else if(messageKind.value==='event'){sample.event='selfTest';sample.data={result:'ok'}}
  else if(messageKind.value==='alarm')sample.data={alarmType:'SMOKE_DETECTED',alarmLevel:'HIGH',content:'烟雾告警样例'}
  else if(messageKind.value==='state')sample.online=true
  else if(messageKind.value==='command-reply'){sample.commandId='preview-command';sample.success=true}
  payload.value=pretty(sample)
}
function objectJSON(value,label){const parsed=parseJSON(value,label);if(!parsed || Array.isArray(parsed) || typeof parsed!=='object')throw new Error(`${label}必须是 JSON 对象`);return parsed}
async function preview() {
  if(busy.value || !selectedRelease.value)return
  busy.value=true;error.value='';result.value=null;controller=new AbortController()
  const release=selectedRelease.value, requestRevision=revision, selectedOperation=goProtocol.value?operation.value:'decode'
  try {
    const body={readOnly:true,operation:selectedOperation}
    if(rawInfo.value)body.rawMessageId=rawInfo.value.id
    if(selectedOperation==='encode')body.command=objectJSON(command.value,'命令')
    else if(selectedOperation==='ingress') {
      body.chunks=(chunks.value || payload.value).split(/\r?\n/).map(value=>value.trim()).filter(Boolean);body.datagram=datagram.value
      if(!body.chunks.length)throw new Error('请填写 HEX 报文片段，每行作为一次收到的数据')
    } else {
      if(!payload.value.trim())throw new Error('请填写样本报文')
      body.payload=release.payloadFormat==='hex'?payload.value.trim():parseJSON(payload.value,'JSON 样本')
    }
    if(standardProtocol.value)body.messageKind=messageKind.value
    if(modbus.value)body.startAddress=startAddress.value
    if(goProtocol.value){body.deviceId=deviceId.value;body.state=objectJSON(state.value,'帧前状态');if(sampleTime.value)body.now=sampleTime.value}
    if(expected.value.trim())body.expected=objectJSON(expected.value,'预期结果')
    const value=await api(`/api/v2/protocols/${encodeURIComponent(release.protocolId)}/releases/${encodeURIComponent(release.version)}/preview`,{method:'POST',signal:controller.signal,body:JSON.stringify(body)})
    if(disposed || requestRevision!==revision)return
    result.value={...value,protocolPackageId:release.id,templateName:mode.value==='template'?selectedProduct.value?.name:''}
  }catch(e){if(!disposed && requestRevision===revision && e.name!=='AbortError')error.value=e.message}finally{if(!disposed)busy.value=false}
}
</script>

<template>
  <ui-card shadow="never" class="protocol-preview-panel">
    <template #header><div class="section-toolbar"><strong>协议样本工作台</strong><ui-button size="small" :loading="loading" @click="load">刷新列表</ui-button></div></template>
    <p class="muted-text">使用固定版本计算拆帧、解析或命令编码结果，不向设备发送报文、不登记设备、不写入设备数据。这里的结果不能作为现场验收。</p>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" />
    <div v-if="can('GET /api/v1/raw-messages/:id')" class="raw-loader"><ui-input v-model="rawMessageId" aria-label="样本原文编号" placeholder="输入已授权原文编号，载入实际版本和帧前状态"/><ui-button :loading="rawLoading" :disabled="!rawMessageId.trim()" @click="loadRaw">载入原文</ui-button></div>
    <ui-alert v-if="rawInfo" type="info" :title="`原文 ${rawInfo.id} · 归档版本 ${rawInfo.protocolPackageId}`" :closable="false"><span>已读取样本，点击试跑后才执行协议；不会创建回放任务。{{ rawInfo.parseError || '' }}</span></ui-alert>
    <ui-form label-position="top" :disabled="loading || rawLoading">
      <ui-radio-group v-model="mode" class="top-gap"><ui-radio-button v-if="can('GET /api/v1/products')" value="template">按设备模板</ui-radio-button><ui-radio-button value="release">按协议版本</ui-radio-button></ui-radio-group>
      <ui-form-item v-if="mode==='template'" label="设备模板"><ui-select v-model="productId" filterable placeholder="选择要测试的设备模板"><ui-option v-for="item in products" :key="item.id" :value="item.id" :label="item.name" /></ui-select></ui-form-item>
      <ui-form-item v-else label="协议版本"><ui-select v-model="packageId" filterable placeholder="选择固定版本"><ui-option v-for="item in releases" :key="item.id" :value="item.id" :label="`${item.name} · ${item.version} · ${{DRAFT:'草稿',VALIDATED:'已校验',PUBLISHED:'已发布',DEPRECATED:'已弃用'}[item.status] || item.status}`" /></ui-select></ui-form-item>
      <p v-if="selectedRelease" class="muted-text">{{ selectedRelease.protocolId }} @ {{ selectedRelease.version }} · {{ transportLabel(selectedRelease.transport) }}</p>
      <ui-alert v-else-if="mode==='template' && productId" type="info" title="该模板没有可预览的协议版本，请先在设备模板中选择协议。" :closable="false" />
      <template v-if="selectedRelease">
        <ui-form-item v-if="standardProtocol" label="标准消息类型"><ui-select v-model="messageKind" :disabled="!!rawInfo"><ui-option v-for="item in messageKinds" :key="item.value" :value="item.value" :label="item.label"/></ui-select><small v-if="rawInfo">沿用原文的接入类型；填入示例可解除原文关联。</small><ui-button size="small" class="top-gap" @click="fillStandardSample">填入此类型示例</ui-button></ui-form-item>
        <ui-form-item v-if="goProtocol" label="协议操作"><ui-select v-model="operation"><ui-option v-for="item in operationOptions" :key="item.value" :value="item.value" :label="item.label"/></ui-select></ui-form-item>
        <template v-if="goProtocol && operation==='ingress'"><ui-form-item label="HEX 报文片段（每行模拟一次接收）"><ui-input v-model="chunks" type="textarea" :rows="5" placeholder="可将一帧分成多行模拟半帧；一行放多帧模拟粘包"/></ui-form-item><ui-checkbox v-model="datagram">UDP 完整数据报（每行必须是完整数据报）</ui-checkbox></template>
        <ui-form-item v-else-if="goProtocol && operation==='encode'" label="命令 JSON"><ui-input v-model="command" type="textarea" :rows="5" placeholder='{"type":"read"}'/><small>仅显示编码后的 HEX、应答关联标识和状态，不发送命令。</small></ui-form-item>
        <ui-form-item v-else label="样本报文"><ui-input v-model="payload" type="textarea" :rows="6" :placeholder="selectedRelease.payloadFormat==='hex'?'填写完整 HEX 报文':'填写 JSON 报文'" /></ui-form-item>
        <ui-form-item v-if="modbus" label="响应起始地址"><ui-input-number v-model="startAddress" :min="0" :max="65535" :precision="0" /></ui-form-item>
        <ui-collapse v-if="goProtocol"><ui-collapse-item name="context" title="协议上下文"><ui-form-item label="模拟设备标识"><ui-input v-model="deviceId" maxlength="128" /></ui-form-item><ui-form-item label="帧前状态 JSON"><ui-input v-model="state" type="textarea" :rows="3" /></ui-form-item><small>来自原文时沿用归档帧前状态和接收时间。拆帧中的子设备仅展示观察结果，不会登记或分发。</small></ui-collapse-item></ui-collapse>
        <ui-form-item label="预期结果 JSON（可选，填写需要核对的字段）"><ui-input v-model="expected" type="textarea" :rows="4" :placeholder="expectedPlaceholder"/><small>对象只比较填写的字段；数组须填写相同数量的元素，并按顺序比较。</small></ui-form-item>
        <div class="preview-actions"><ui-button v-permission="'POST /api/v2/protocols/:id/releases/:version/preview'" type="primary" :loading="busy" :disabled="rawLoading" @click="preview">只读试跑</ui-button></div>
      </template>
    </ui-form>
    <section v-if="result" class="top-gap"><ui-tag type="success">试跑完成 · {{ result.protocolPackageId }}</ui-tag><p v-if="result.standardMessage">{{ messageTypeLabel(result.standardMessage.messageType) }}</p>
      <ui-alert v-if="result.comparison" :type="result.comparison.matched?'success':'warning'" :title="result.comparison.matched?'实际结果符合预期字段':'实际结果与预期存在差异'" :closable="false"/>
      <div v-if="result.comparison && !result.comparison.matched" class="comparison-table"><table><thead><tr><th>字段</th><th>预期</th><th>实际</th></tr></thead><tbody><tr v-for="(diff,index) in result.comparison.differences" :key="index"><td>{{ diff.path }}</td><td><pre>{{ pretty(diff.expected) }}</pre></td><td><pre>{{ diff.missing?'字段不存在':pretty(diff.actual) }}</pre></td></tr></tbody></table></div>
      <template v-if="result.operation==='ingress'"><p>完整帧 {{ result.operationResult.frames.length }} 条 · {{ result.operationResult.needMore?'仍在等待后续字节':'全部缓冲已消费' }}</p><div v-for="(frame,index) in result.operationResult.frames" :key="index" class="frame-result"><strong>第 {{ index+1 }} 帧 · 设备 {{ frame.deviceId }}</strong><p>HEX：{{ frame.frameHex }}</p><p>ACK / 应答：{{ frame.reply || '无' }} · 关联标识：{{ frame.correlationId || '无' }}</p><ui-alert v-if="frame.decodeError" type="warning" :title="frame.decodeError" :closable="false"/><pre v-else>{{ pretty(frame.standardMessage) }}</pre></div></template>
      <div v-if="result.operation==='encode'"><p>编码 HEX：{{ result.operationResult.reply }}</p><p>关联标识：{{ result.operationResult.correlationId || '无' }}</p></div>
      <ui-collapse><ui-collapse-item name="actual" title="完整实际结果"><pre class="preview-result">{{ pretty(result.operationResult || result.standardMessage) }}</pre></ui-collapse-item><ui-collapse-item v-if="result.comparison" name="expected" title="预期结果"><pre class="preview-result">{{ pretty(result.comparison.expected) }}</pre></ui-collapse-item></ui-collapse>
    </section>
  </ui-card>
</template>

<style scoped>
.protocol-preview-panel { min-width:0; margin-bottom:20px; }
.preview-actions { display:flex; justify-content:flex-end; margin-top:16px; }
.raw-loader { display:flex; gap:8px; margin:16px 0; }
.preview-result,.frame-result pre { max-height:360px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; }
.frame-result { padding:12px; border:1px solid var(--border); border-radius:8px; margin-top:12px; overflow-wrap:anywhere; }
.comparison-table { overflow:auto; margin:12px 0; }
.comparison-table table { width:100%; border-collapse:collapse; text-align:left; }
.comparison-table th,.comparison-table td { padding:8px; border-bottom:1px solid var(--border); vertical-align:top; }
.comparison-table pre { white-space:pre-wrap; overflow-wrap:anywhere; max-width:320px; margin:0; }
@media(max-width:600px){.raw-loader{flex-direction:column}}
</style>
