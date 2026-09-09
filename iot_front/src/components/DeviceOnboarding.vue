<script setup>
import EdgeNodes from './EdgeNodes.vue'
import { computed, onMounted, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { api, notifyError, pretty, formatTime } from '../api'
const props = defineProps({ products: { type: Array, default: () => [] } })
const emit = defineEmits(['close', 'created', 'navigate', 'detail'])
const step = ref(0), busy = ref(false), preview = ref(null), result = ref(null), advanced = ref(false)
const protocols = ref([])
const edgeNodes=ref([]),edgeDialog=ref(false)
async function loadEdges(){try{edgeNodes.value=(await api('/api/v1/edge-nodes')).items||[]}catch(e){notifyError(e)}}
onMounted(loadEdges)
const profiles = ref([]), newProduct = ref(false), productName = ref('')
const existingProfileId = ref('')
const availableProfiles = computed(() => profiles.value.filter(x=>x.profile && x.type===form.type && x.profile.productId===form.productId && x.profile.enabled))
const form = reactive({ productId:'', deviceId:'', name:'', type:'MQTT', protocolId:'', protocolVersion:'', messageKind:'property',pollIntervalSec:10, profile:{host:'0.0.0.0',port:26875,unitId:1,timeoutMs:3000,retries:0,autoRegister:false,collectorId:'',edgeNodeId:'',credentialRef:'',endpointPath:'',serialPort:'',baudRate:9600,parity:'E',stopBits:1} })
const sample = ref(pretty({ id:'msg-001', timestamp:Date.now(), data:{temperature:26.5, smoke:0, battery:87} }))
const csv = ref(''), point = reactive({identifier:'temperature',address:0,functionCode:3,dataType:'uint16',scale:1,byteOrder:'big',wordOrder:'ABCD',unit:''})
const standard = computed(() => ['MQTT','HTTP'].includes(form.type))
const modbus = computed(() => ['MODBUS_TCP','MODBUS_RTU'].includes(form.type))
const fieldProtocol = computed(() => ['OPC_UA','SNMP','BACNET'].includes(form.type))
const activeRead = computed(() => modbus.value || fieldProtocol.value)
const readAddress = ref('')
const serial = computed(() => form.type === 'MODBUS_RTU')
const listener = computed(() => ['TCP','UDP'].includes(form.type))
const published = computed(() => protocols.value.flatMap(x => (x.releases || []).filter(r => r.status === 'PUBLISHED' && (activeRead.value ? r.transport === form.type : ['TCP_UDP',form.type].includes(r.transport) && r.capabilities?.includes('ingress'))).map(r => ({...r, name:x.definition.name}))))
const selected = computed({get:()=>`${form.protocolId}@${form.protocolVersion}`,set:v=>{[form.protocolId,form.protocolVersion]=v.split('@')}})
const kinds = ref([])
onMounted(async()=>{try{kinds.value=(await api('/api/v1/connectors/types')).items.filter(x=>x.supported).map(x=>({value:x.type,label:x.name}))}catch(e){notifyError(e)}})
onMounted(async()=>{try{protocols.value=(await api('/api/v2/protocols')).items || []}catch(e){notifyError(e)}})
onMounted(async()=>{try{profiles.value=(await api('/api/v1/connectors')).items || []}catch(e){notifyError(e)}})
watch(newProduct,value=>{form.productId=value?`product-${crypto.randomUUID().slice(0,12)}`:''})
watch([()=>form.type,()=>form.productId],()=>{existingProfileId.value=''})
watch([existingProfileId,newProduct,productName],()=>{preview.value=null})
watch(()=>form.messageKind,kind=>{if(standard.value)sample.value=pretty({id:'msg-001',timestamp:Date.now(),data:kind==='state'?{connectionStatus:'CONNECTED'}:kind==='event'?{type:'deviceStarted',message:'设备启动'}:{temperature:26.5}})})
const drafts = new Map()
watch(()=>form.type, (value,old)=>{
 drafts.set(old,{profile:{...form.profile},protocolId:form.protocolId,protocolVersion:form.protocolVersion,sample:sample.value})
 const saved=drafts.get(value)
 Object.assign(form.profile,saved?.profile||{host:value==='MODBUS_TCP'?'':'0.0.0.0',port:value==='MODBUS_TCP'?502:26875})
 if(['MODBUS_RTU','OPC_UA','SNMP','BACNET'].includes(value))advanced.value=true
 if(value==='OPC_UA')form.profile.port=4840
 if(value==='SNMP')form.profile.port=161
 if(value==='BACNET')form.profile.port=47808
 if(!['MODBUS_TCP','MODBUS_RTU','OPC_UA','SNMP','BACNET','TCP','UDP'].includes(value))form.profile.edgeNodeId=''
 form.protocolId=saved?.protocolId||'';form.protocolVersion=saved?.protocolVersion||''
 if(['TCP','UDP'].includes(value))sample.value=saved?.sample||''
 else if(!['MQTT','HTTP'].includes(old))sample.value=pretty({id:'msg-001',timestamp:Date.now(),data:form.messageKind==='state'?{connectionStatus:'CONNECTED'}:form.messageKind==='event'?{type:'deviceStarted',message:'设备启动'}:{temperature:26.5}})
})
watch(()=>form.profile.edgeNodeId,value=>{if(value&&listener.value)form.profile.autoRegister=false})
watch([form,sample,csv,point,readAddress],()=>{preview.value=null},{deep:true})
function csvCell(value) { return '"' + String(value).replaceAll('"', '""') + '"' }
function request() { return {...form, productName:newProduct.value?productName.value:undefined, existingProfileId:existingProfileId.value, profile:{...form.profile}, readPoints:fieldProtocol.value?[{identifier:point.identifier,address:readAddress.value,scale:point.scale}]:undefined, payload:standard.value ? JSON.parse(sample.value) : listener.value ? sample.value.replace(/\s/g,'') : null, pointTableCsv:csv.value || `name,functionCode,address,addressNotation,dataType,scale,byteOrder,wordOrder,unit\n${csvCell(point.identifier)},${point.functionCode},${point.address},zero_based,${point.dataType},${point.scale},${point.byteOrder},${point.wordOrder},${csvCell(point.unit)}\n`} }
let alive=true, revision=0, timer, pollCount=0
const receipt=ref(null), receiptError=ref('')
const abort=new AbortController()
watch([form,sample,csv,point,readAddress,existingProfileId,newProduct,productName],()=>{revision++;preview.value=null},{deep:true,flush:'sync'})
onBeforeUnmount(()=>{alive=false;clearTimeout(timer);abort.abort();result.value=null;drafts.clear()})
async function test(){
 if(busy.value)return
 const generation=revision
 busy.value=true;preview.value=null
 try{const value=await api('/api/v1/onboarding/test',{method:'POST',body:JSON.stringify(request()),signal:abort.signal});if(alive&&generation===revision)preview.value=value}
 catch(e){if(alive&&generation===revision){if(e.testResult)preview.value=e.testResult;else notifyError(e)}}
 finally{if(alive)busy.value=false}
}
async function refreshReceipt(restart=false){
 clearTimeout(timer);if(restart)pollCount=0
 if(!alive||!result.value)return
 try{
  const current=await api(`/api/v1/device-registry/${encodeURIComponent(result.value.device.id)}/connection`,{signal:abort.signal})
  if(!alive)return
  receipt.value=current;receiptError.value=''
  if(!current.ingest?.parsed&&++pollCount<60)timer=setTimeout(()=>refreshReceipt(),2000)
 }catch(e){if(alive)receiptError.value=e.message}
}
async function finish(){
 if(busy.value||!preview.value?.success)return
 busy.value=true
 try{const value=await api('/api/v1/onboarding',{method:'POST',body:JSON.stringify({...request(),testToken:preview.value.testToken}),signal:abort.signal});if(!alive)return;result.value=value;step.value=5;emit('created');refreshReceipt(true)}
 catch(e){if(alive)notifyError(e)}finally{if(alive)busy.value=false}
}
const receiptText=computed(()=>({WAITING_FOR_DATA:'等待设备首条上报',RAW_RECEIVED:'已收到原文，等待解析',PARSE_FAILED:'已收到原文，解析失败',PARSED:'已收到并成功解析'}[receipt.value?.ingest?.stage]||'正在查询接收状态'))

async function importCSV(event){const file=event.target.files?.[0];if(!file)return;if(file.size>65536){notifyError(new Error('点表不能超过 64 KiB'));return}csv.value=await file.text()}
const identity = computed(()=>result.value ? `${result.value.device.tenantId}/${result.value.device.productId}/${result.value.device.id}` : '')
const ingestURL = computed(()=>new URL(result.value?.accessInfo?.httpUrl||`/api/v1/device-ingest/standard/${identity.value}/property`,location.origin).href)
const nextDisabled = computed(()=>busy.value || (step.value===0&&(!form.productId||(newProduct.value&&!productName.value.trim()))) || (step.value===2&&(!form.name||!form.deviceId)) || (step.value>=3&&!preview.value?.success))
async function copy(){await navigator.clipboard.writeText(pretty(result.value));}
</script>

<template>
  <EdgeNodes v-if="edgeDialog" @close="edgeDialog=false" @changed="loadEdges"/>
  <el-dialog :model-value="true" title="添加设备" width="min(960px, 96vw)" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="emit('close')">
    <el-steps :active="step" finish-status="success" align-center><el-step v-for="name in ['选择产品','接入方式','参数配置','接入测试','数据预览','完成']" :key="name" :title="name" /></el-steps>
    <div class="onboarding-body" v-loading="busy">
      <el-form v-if="step===0" label-position="top"><el-switch v-model="newProduct" active-text="在向导中新建产品" /><el-form-item v-if="newProduct" label="新产品名称"><el-input v-model="productName" maxlength="256" /></el-form-item><el-form-item v-else label="产品"><el-select v-model="form.productId" filterable placeholder="选择产品"><el-option v-for="p in props.products.filter(p=>p.status==='ENABLED')" :key="p.id" :value="p.id" :label="p.name" /></el-select></el-form-item><el-empty v-if="!newProduct&&!props.products.length" description="请先创建产品"><el-button @click="emit('navigate','products')">产品管理</el-button></el-empty><p>复用产品配置，接入完成后自动创建设备与运行时所需资源。</p></el-form>
      <div v-if="step===1"><el-radio-group v-model="form.type" class="connector-options"><el-radio v-for="kind in kinds" :key="kind.value" :value="kind.value" border>{{kind.label}}</el-radio></el-radio-group><p>现场采集或监听可在参数配置的高级选项中分配 Edge 节点。</p><p><el-button link @click="emit('navigate','cameras')">视频设备：打开摄像头管理</el-button></p></div>
      <el-form v-if="step===2" label-position="top">
        <div class="onboarding-grid"><el-form-item label="设备名称"><el-input v-model="form.name" maxlength="256" /></el-form-item><el-form-item label="设备标识"><el-input v-model="form.deviceId" placeholder="例如 sensor-001；TCP/UDP 须与报文标识一致" maxlength="128" /></el-form-item></div>
        <el-form-item v-if="listener&&availableProfiles.length" label="监听实例"><el-select v-model="existingProfileId" clearable placeholder="新建监听实例"><el-option v-for="x in availableProfiles" :key="x.profile.id" :value="x.profile.id" :label="`${x.profile.host}:${x.profile.port} · ${x.profile.runtimeStatus} · ${x.sessions.length} 个会话`" /></el-select></el-form-item><div v-if="!standard&&!serial&&!existingProfileId" class="onboarding-grid"><el-form-item :label="listener?'平台监听 IP':'设备 IP / 主机名'"><el-input v-model="form.profile.host" /></el-form-item><el-form-item label="端口"><el-input-number v-model="form.profile.port" :min="1" :max="65535" /></el-form-item></div>
        <div v-if="serial" class="onboarding-grid"><el-form-item label="串口（节点本地白名单内）"><el-input v-model="form.profile.serialPort" placeholder="/dev/ttyUSB0 / COM3" /></el-form-item><el-form-item label="波特率"><el-input-number v-model="form.profile.baudRate" :min="300" :max="115200" /></el-form-item><el-form-item label="校验"><el-select v-model="form.profile.parity"><el-option value="N" label="无校验"/><el-option value="E" label="偶校验"/><el-option value="O" label="奇校验"/></el-select></el-form-item><el-form-item label="停止位"><el-input-number v-model="form.profile.stopBits" :min="1" :max="2" /></el-form-item></div><div v-if="fieldProtocol" class="onboarding-grid"><el-form-item label="属性标识"><el-input v-model="point.identifier" /></el-form-item><el-form-item :label="form.type==='OPC_UA'?'Node ID':form.type==='BACNET'?'对象类型:实例:属性':'OID'"><el-input v-model="readAddress" :placeholder="form.type==='OPC_UA'?'ns=2;s=temperature':form.type==='BACNET'?'0:1:85':'.1.3.6.1.2.1.1.1.0'" /></el-form-item><el-form-item label="现场凭据引用"><el-input v-model="form.profile.credentialRef" placeholder="节点本地凭据文件中的名称" /></el-form-item><el-form-item v-if="form.type==='OPC_UA'" label="Endpoint 路径（可选）"><el-input v-model="form.profile.endpointPath" placeholder="/server" /></el-form-item><el-form-item label="采集周期（秒）"><el-input-number v-model="form.pollIntervalSec" :min="1" :max="3600" /></el-form-item></div><template v-if="modbus"><el-form-item label="站号"><el-input-number v-model="form.profile.unitId" :min="0" :max="255" /></el-form-item><el-form-item label="默认采集周期（秒）"><el-input-number v-model="form.pollIntervalSec" :min="1" :max="3600" /></el-form-item><el-form-item label="重试次数"><el-input-number v-model="form.profile.retries" :min="0" :max="3" /></el-form-item><p>已绑定产品复用原点表；未绑定产品可填写一个点位，或导入 CSV 点表。</p><div class="onboarding-grid"><el-form-item label="属性标识"><el-input v-model="point.identifier" /></el-form-item><el-form-item label="零基地址"><el-input-number v-model="point.address" :min="0" :max="65535" /></el-form-item><el-form-item label="功能码"><el-select v-model="point.functionCode"><el-option v-for="n in [1,2,3,4]" :key="n" :value="n" :label="String(n)" /></el-select></el-form-item><el-form-item label="数据类型"><el-select v-model="point.dataType"><el-option v-for="n in ['bool','uint16','int16','uint32','int32','float32']" :key="n" :value="n" /></el-select></el-form-item><el-form-item label="倍率"><el-input-number v-model="point.scale" /></el-form-item><el-form-item label="字节序"><el-select v-model="point.byteOrder"><el-option value="big" label="大端"/><el-option value="little" label="小端"/></el-select></el-form-item><el-form-item label="字序（多寄存器）"><el-select v-model="point.wordOrder"><el-option v-for="value in ['ABCD','CDAB','BADC','DCBA']" :key="value" :value="value"/></el-select></el-form-item><el-form-item label="单位"><el-input v-model="point.unit" /></el-form-item></div><el-form-item label="可选：导入多点 CSV"><input type="file" accept=".csv" @change="importCSV" /><el-button v-if="csv" link @click="csv=''">清除导入</el-button></el-form-item><pre v-if="csv">{{csv}}</pre></template>
        <template v-if="standard"><el-form-item label="上报类型"><el-select v-model="form.messageKind"><el-option label="属性" value="property"/><el-option label="事件" value="event"/><el-option label="状态" value="state"/></el-select></el-form-item><el-form-item label="测试 JSON"><el-input v-model="sample" type="textarea" :rows="8" /></el-form-item></template>
        <el-form-item v-if="listener" label="一帧完整 HEX 样例"><el-input v-model="sample" type="textarea" :rows="5" placeholder="填写真实报文，用于检查设备识别与解析"/></el-form-item>
        <el-switch v-if="!standard" v-model="advanced" active-text="高级设置" />
        <div v-if="advanced&&!standard"><el-form-item label="协议（默认使用产品现有绑定）"><el-select v-model="selected" clearable filterable><el-option v-for="r in published" :key="r.protocolId+'@'+r.version" :value="r.protocolId+'@'+r.version" :label="`${r.name} · ${r.version}`" /></el-select></el-form-item><el-form-item label="超时（毫秒）"><el-input-number v-model="form.profile.timeoutMs" :min="1" :max="10000" /></el-form-item><el-form-item label="现场 Edge 节点"><el-select v-model="form.profile.edgeNodeId" :clearable="!serial" :disabled="!activeRead&&!listener"><el-option v-for="edge in edgeNodes.filter(e=>e.status==='ENABLED')" :key="edge.id" :value="edge.id" :label="edge.name"/></el-select><el-button link @click="edgeDialog=true">管理节点</el-button></el-form-item><el-form-item label="Collector"><el-input v-model="form.profile.collectorId" /></el-form-item><el-form-item v-if="listener" label="自动注册后续设备"><el-switch v-model="form.profile.autoRegister" /></el-form-item></div>
      </el-form>
      <div v-if="step===3"><p>{{activeRead?(form.profile.edgeNodeId?'由现场节点执行实际点表读取，平台校验响应。':'平台将连接设备，执行一次点表读取。'):listener?(form.profile.edgeNodeId?'本步骤校验样例完整帧；现场监听是否启动及设备连通情况，请在保存后查看节点心跳和首条数据。':'检查本机监听端口和样例完整帧，设备实际连通情况请在启用后确认。'):'校验标准上报格式并预览解析结果；设备凭据在完成时生成。'}}</p><el-button type="primary" :loading="busy" @click="test">运行接入测试</el-button><el-alert v-if="preview" :type="preview.success?'success':'error'" :closable="false" :title="preview.message" :description="`${preview.stage} · ${preview.errorCode} · ${preview.latencyMs||0} ms`" /><pre v-if="preview&&!preview.success&&(preview.rawRequest||preview.rawResponse)">{{pretty({requestHex:preview.rawRequest,responseHex:preview.rawResponse,exceptionCode:preview.exceptionCode})}}</pre></div>
      <div v-if="step===4&&preview"><el-alert :closable="false" type="info" :title="preview.message"/><el-descriptions :column="2" border><el-descriptions-item label="Protocol ID">{{preview.protocolId}}</el-descriptions-item><el-descriptions-item label="Protocol Version">{{preview.protocolVersion}}</el-descriptions-item><el-descriptions-item label="数据来源">{{['network-read','edge-read'].includes(preview.source)?'目标设备/模拟器读取':'样例预览'}}</el-descriptions-item><el-descriptions-item label="Parser">{{preview.parser}}</el-descriptions-item></el-descriptions><h4>Raw</h4><pre>{{pretty(preview.raw)}}</pre><template v-if="preview.rawRequest"><h4>Request HEX / Response HEX</h4><pre>{{preview.rawRequest}}
{{preview.rawResponse}}</pre></template><h4>Parsed</h4><pre>{{pretty(preview.parsed)}}</pre><h4>StandardMessage</h4><pre>{{pretty(preview.standardMessages)}}</pre><h4>属性 / 事件 / 告警映射</h4><pre>{{pretty(preview.mapping)}}</pre><template v-if="preview.pointTable"><h4>点表</h4><pre>{{pretty(preview.pointTable)}}</pre></template></div>
      <div v-if="step===5&&result"><el-alert type="success" title="接入配置已保存，设备已启用" :closable="false"/><p v-if="result.credential?.secret">请立即保存设备 Secret；关闭后无法再次查询，可在设备详情重新生成或禁用。</p><el-alert v-else type="warning" :closable="false" title="已恢复上次保存结果，未重复生成凭据" description="原 Secret 仅首次返回。如上次响应丢失，请进入设备详情重新生成凭据。"/><el-descriptions :column="1" border><el-descriptions-item label="Device ID">{{result.device.id}}</el-descriptions-item><el-descriptions-item label="Client ID">{{result.clientId}}</el-descriptions-item><el-descriptions-item label="Username / X-Device-Key">{{result.username}}</el-descriptions-item><el-descriptions-item label="Device Secret">{{result.credential?.secret||'本次不返回；需要时在详情轮换'}}</el-descriptions-item></el-descriptions><el-button @click="copy">复制接入结果</el-button><template v-if="standard"><p>HTTP：POST {{ingestURL}}，使用 X-Device-Key / X-Device-Secret 请求头，上报正文与测试 JSON 相同。event / state 替换末尾 property。</p><p>MQTT Broker：{{result.accessInfo?.mqttBroker||'未配置对外地址，请联系管理员'}}。先 POST /api/v1/device-mqtt/token，使用同一设备请求头换取 token，将 token 作为 MQTT password。使用返回的 username 和本页 Client ID。</p><pre>/iot/up/{{identity}}/property
/iot/up/{{identity}}/event
/iot/up/{{identity}}/state
/iot/up/{{identity}}/command-reply
/iot/down/{{identity}}/command</pre><p>命令回执的 data 包含 commandId 和 success（布尔值）。设备收到命令后应按 id 去重执行，再上报回执。</p></template><p v-else>运行时将自动加载接入配置。监听地址不等于对外地址；请核对容器端口映射、防火墙及设备路由。</p>
        <el-alert :closable="false" :type="receipt?.ingest?.parsed?'success':receipt?.ingest?.parseError?'error':'info'" :title="receiptText"/>
        <p>运行时：{{receipt?.profile?.runtimeStatus||'按设备上报更新'}} · 收到原文：{{receipt?.ingest?.rawReceived?'是':'否'}} · 成功解析：{{receipt?.ingest?.parsed?'是':'否'}}</p>
        <p v-if="receipt?.ingest?.rawReceived">平台接收时间：{{formatTime(receipt.ingest.receivedAt)}} · 原文 ID：{{receipt.ingest.rawMessageId}}</p>
        <p v-if="receipt?.profile?.lastError||receipt?.ingest?.parseError">{{receipt?.profile?.lastError||receipt?.ingest?.parseError}}</p>
        <p v-if="receiptError">状态查询失败：{{receiptError}}</p><p v-if="pollCount>=60&&!receipt?.ingest?.parsed">已停止自动等待，可刷新继续。配置仍然保留。</p>
        <el-button @click="refreshReceipt(true)">刷新接收状态</el-button><el-button v-if="receipt?.ingest?.rawReceived" @click="emit('navigate','raw',{deviceId:result.device.id})">原始报文与解析结果</el-button>
        <pre v-if="receipt?.ingest?.standardMessage">{{pretty(receipt.ingest.standardMessage)}}</pre>
      </div>
    </div>
    <template #footer><el-button :disabled="busy" @click="emit('close')">{{step===5?'关闭':'取消'}}</el-button><el-button v-if="step>0&&step<5" :disabled="busy" @click="step--">上一步</el-button><el-button v-if="step<4" type="primary" :disabled="nextDisabled" @click="step++">下一步</el-button><el-button v-if="step===5" type="primary" @click="emit('detail',result.device.id)">进入设备详情</el-button><el-button v-if="step===4" type="primary" :disabled="busy||!preview?.success" :loading="busy" @click="finish">完成并启用</el-button></template>
  </el-dialog>
</template>

<style scoped>
.onboarding-body{max-height:65vh;overflow:auto;padding:22px 4px;min-height:260px}.onboarding-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 18px}.connector-options{display:flex;gap:12px;flex-wrap:wrap}.connector-options .el-radio{margin:0}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--el-fill-color-light);padding:12px;border-radius:6px}.el-alert,.el-descriptions{margin-top:14px}@media(max-width:600px){.onboarding-grid{grid-template-columns:1fr}}
</style>
