<script setup>
import EdgeNodes from './EdgeNodes.vue'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, notifyError, pretty } from '../api'
const props = defineProps({ products: { type: Array, default: () => [] } })
const emit = defineEmits(['close', 'created', 'navigate'])
const step = ref(0), busy = ref(false), preview = ref(null), result = ref(null), advanced = ref(false)
const protocols = ref([])
const edgeNodes=ref([]),edgeDialog=ref(false)
async function loadEdges(){try{edgeNodes.value=(await api('/api/v1/edge-nodes')).items||[]}catch(e){notifyError(e)}}
onMounted(loadEdges)
const profiles = ref([]), newProduct = ref(false), productName = ref('')
const existingProfileId = ref('')
const availableProfiles = computed(() => profiles.value.filter(x=>x.profile && x.type===form.type && x.profile.productId===form.productId && x.profile.enabled))
const form = reactive({ productId:'', deviceId:'', name:'', type:'MQTT', protocolId:'', protocolVersion:'', messageKind:'property', profile:{host:'0.0.0.0',port:26875,unitId:1,timeoutMs:3000,retries:0,autoRegister:false,collectorId:'',edgeNodeId:''} })
const sample = ref(pretty({ id:'msg-001', timestamp:Date.now(), data:{temperature:26.5, smoke:0, battery:87} }))
const csv = ref(''), point = reactive({identifier:'temperature',address:0,functionCode:3,dataType:'uint16',scale:1})
const standard = computed(() => ['MQTT','HTTP'].includes(form.type))
const listener = computed(() => ['TCP','UDP'].includes(form.type))
const published = computed(() => protocols.value.flatMap(x => (x.releases || []).filter(r => r.status === 'PUBLISHED' && (form.type === 'MODBUS_TCP' ? r.transport === 'MODBUS_TCP' : ['TCP_UDP',form.type].includes(r.transport) && r.capabilities?.includes('ingress'))).map(r => ({...r, name:x.definition.name}))))
const selected = computed({get:()=>`${form.protocolId}@${form.protocolVersion}`,set:v=>{[form.protocolId,form.protocolVersion]=v.split('@')}})
const kinds = [{value:'MQTT',label:'MQTT 标准设备'},{value:'HTTP',label:'HTTP 标准上报'},{value:'MODBUS_TCP',label:'Modbus TCP'},{value:'TCP',label:'TCP 设备'},{value:'UDP',label:'UDP 设备'}]
onMounted(async()=>{try{protocols.value=(await api('/api/v2/protocols')).items || []}catch(e){notifyError(e)}})
onMounted(async()=>{try{profiles.value=(await api('/api/v1/connectors')).items || []}catch(e){notifyError(e)}})
watch(newProduct,value=>{form.productId=value?`product-${crypto.randomUUID().slice(0,12)}`:''})
watch([()=>form.type,()=>form.productId],()=>{existingProfileId.value=''})
watch([existingProfileId,newProduct,productName],()=>{preview.value=null})
watch(()=>form.messageKind,kind=>{if(standard.value)sample.value=pretty({id:'msg-001',timestamp:Date.now(),data:kind==='state'?{connectionStatus:'CONNECTED'}:kind==='event'?{type:'deviceStarted',message:'设备启动'}:{temperature:26.5}})})
watch(()=>form.type, value=>{form.profile.host=value==='MODBUS_TCP'?'':'0.0.0.0';form.profile.port=value==='MODBUS_TCP'?502:26875;form.protocolId='';form.protocolVersion='';sample.value=['TCP','UDP'].includes(value)?'':pretty({id:'msg-001',timestamp:Date.now(),data:form.messageKind==='state'?{connectionStatus:'CONNECTED'}:form.messageKind==='event'?{type:'deviceStarted',message:'设备启动'}:{temperature:26.5}})})
watch([form,sample,csv,point],()=>{preview.value=null},{deep:true})
function request() { return {...form, productName:newProduct.value?productName.value:undefined, existingProfileId:existingProfileId.value, profile:{...form.profile}, payload:standard.value ? JSON.parse(sample.value) : listener.value ? sample.value.replace(/\s/g,'') : null, pointTableCsv:csv.value || `name,functionCode,address,addressNotation,dataType,scale\n${point.identifier},${point.functionCode},${point.address},zero_based,${point.dataType},${point.scale}\n`} }
async function test(){busy.value=true;preview.value=null;try{preview.value=await api('/api/v1/onboarding/test',{method:'POST',body:JSON.stringify(request())})}catch(e){notifyError(e)}finally{busy.value=false}}
async function finish(){if(!preview.value?.success)return;busy.value=true;try{result.value=await api('/api/v1/onboarding',{method:'POST',body:JSON.stringify({...request(),testToken:preview.value.testToken})});step.value=5;emit('created')}catch(e){notifyError(e)}finally{busy.value=false}}
async function importCSV(event){const file=event.target.files?.[0];if(!file)return;if(file.size>65536){notifyError(new Error('点表不能超过 64 KiB'));return}csv.value=await file.text()}
const identity = computed(()=>result.value ? `${result.value.device.tenantId}/${result.value.device.productId}/${result.value.device.id}` : '')
const ingestURL = computed(()=>`${location.origin}/api/v1/device-ingest/standard/${identity.value}/property`)
const nextDisabled = computed(()=>busy.value || (step.value===0&&(!form.productId||(newProduct.value&&!productName.value.trim()))) || (step.value===2&&(!form.name||!form.deviceId)) || (step.value>=3&&!preview.value?.success))
async function copy(){await navigator.clipboard.writeText(pretty(result.value));}
</script>

<template>
  <EdgeNodes v-if="edgeDialog" @close="edgeDialog=false" @changed="loadEdges"/>
  <el-dialog :model-value="true" title="添加设备" width="min(960px, 96vw)" :close-on-click-modal="false" :close-on-press-escape="!busy" :show-close="!busy" @close="emit('close')">
    <el-steps :active="step" finish-status="success" align-center><el-step v-for="name in ['选择产品','接入方式','参数配置','接入测试','数据预览','完成']" :key="name" :title="name" /></el-steps>
    <div class="onboarding-body" v-loading="busy">
      <el-form v-if="step===0" label-position="top"><el-switch v-model="newProduct" active-text="在向导中新建产品" /><el-form-item v-if="newProduct" label="新产品名称"><el-input v-model="productName" maxlength="256" /></el-form-item><el-form-item v-else label="产品"><el-select v-model="form.productId" filterable placeholder="选择产品"><el-option v-for="p in props.products.filter(p=>p.status==='ENABLED')" :key="p.id" :value="p.id" :label="p.name" /></el-select></el-form-item><el-empty v-if="!newProduct&&!props.products.length" description="请先创建产品"><el-button @click="emit('navigate','products')">产品管理</el-button></el-empty><p>复用产品配置，接入完成后自动创建设备与运行时所需资源。</p></el-form>
      <div v-if="step===1"><el-radio-group v-model="form.type" class="connector-options"><el-radio v-for="kind in kinds" :key="kind.value" :value="kind.value" border>{{kind.label}}</el-radio><el-radio value="EDGE" border disabled>Edge Agent（后续开放）</el-radio></el-radio-group><p><el-button link @click="emit('navigate','cameras')">视频设备：打开摄像头管理</el-button></p></div>
      <el-form v-if="step===2" label-position="top">
        <div class="onboarding-grid"><el-form-item label="设备名称"><el-input v-model="form.name" maxlength="256" /></el-form-item><el-form-item label="设备标识"><el-input v-model="form.deviceId" placeholder="例如 sensor-001；TCP/UDP 须与报文标识一致" maxlength="128" /></el-form-item></div>
        <el-form-item v-if="listener&&availableProfiles.length" label="监听实例"><el-select v-model="existingProfileId" clearable placeholder="新建监听实例"><el-option v-for="x in availableProfiles" :key="x.profile.id" :value="x.profile.id" :label="`${x.profile.host}:${x.profile.port} · ${x.profile.runtimeStatus} · ${x.sessions.length} 个会话`" /></el-select></el-form-item><div v-if="!standard&&!existingProfileId" class="onboarding-grid"><el-form-item :label="listener?'平台监听 IP':'设备 IP / 主机名'"><el-input v-model="form.profile.host" /></el-form-item><el-form-item label="端口"><el-input-number v-model="form.profile.port" :min="1" :max="65535" /></el-form-item></div>
        <template v-if="form.type==='MODBUS_TCP'"><el-form-item label="站号"><el-input-number v-model="form.profile.unitId" :min="0" :max="255" /></el-form-item><p>已绑定产品复用原点表；未绑定产品可填写一个点位，或导入 CSV 点表。</p><div class="onboarding-grid"><el-form-item label="属性标识"><el-input v-model="point.identifier" /></el-form-item><el-form-item label="零基地址"><el-input-number v-model="point.address" :min="0" :max="65535" /></el-form-item><el-form-item label="功能码"><el-select v-model="point.functionCode"><el-option v-for="n in [1,2,3,4]" :key="n" :value="n" :label="String(n)" /></el-select></el-form-item><el-form-item label="数据类型"><el-select v-model="point.dataType"><el-option v-for="n in ['bool','uint16','int16','uint32','int32','float32']" :key="n" :value="n" /></el-select></el-form-item><el-form-item label="倍率"><el-input-number v-model="point.scale" /></el-form-item></div><el-form-item label="可选：导入多点 CSV"><input type="file" accept=".csv" @change="importCSV" /><el-button v-if="csv" link @click="csv=''">清除导入</el-button></el-form-item><pre v-if="csv">{{csv}}</pre></template>
        <template v-if="standard"><el-form-item label="上报类型"><el-select v-model="form.messageKind"><el-option label="属性" value="property"/><el-option label="事件" value="event"/><el-option label="状态" value="state"/></el-select></el-form-item><el-form-item label="测试 JSON"><el-input v-model="sample" type="textarea" :rows="8" /></el-form-item></template>
        <el-form-item v-if="listener" label="一帧完整 HEX 样例"><el-input v-model="sample" type="textarea" :rows="5" placeholder="填写真实报文，用于检查设备识别与解析"/></el-form-item>
        <el-switch v-if="!standard" v-model="advanced" active-text="高级设置" />
        <div v-if="advanced&&!standard"><el-form-item label="协议（默认使用产品现有绑定）"><el-select v-model="selected" clearable filterable><el-option v-for="r in published" :key="r.protocolId+'@'+r.version" :value="r.protocolId+'@'+r.version" :label="`${r.name} · ${r.version}`" /></el-select></el-form-item><el-form-item label="超时（毫秒）"><el-input-number v-model="form.profile.timeoutMs" :min="1" :max="10000" /></el-form-item><el-form-item label="Edge 节点（归属登记）"><el-select v-model="form.profile.edgeNodeId" clearable :disabled="!!existingProfileId"><el-option v-for="edge in edgeNodes.filter(e=>e.status==='ENABLED')" :key="edge.id" :value="edge.id" :label="edge.name"/></el-select><el-button link @click="edgeDialog=true">管理节点</el-button></el-form-item><el-form-item label="Collector"><el-input v-model="form.profile.collectorId" /></el-form-item><el-form-item v-if="listener" label="自动注册后续设备"><el-switch v-model="form.profile.autoRegister" /></el-form-item></div>
      </el-form>
      <div v-if="step===3"><p>{{form.type==='MODBUS_TCP'?'平台将连接设备，执行一次点表读取。':listener?'检查本机监听端口和样例完整帧，设备实际连通情况请在启用后确认。':'校验标准上报格式并预览解析结果；设备凭据在完成时生成。'}}</p><el-button type="primary" :loading="busy" @click="test">运行接入测试</el-button><el-alert v-if="preview" :type="preview.success?'success':'error'" :closable="false" :title="preview.message" :description="`${preview.stage} · ${preview.errorCode} · ${preview.latencyMs||0} ms`" /><pre v-if="preview&&!preview.success&&(preview.rawRequest||preview.rawResponse)">{{pretty({requestHex:preview.rawRequest,responseHex:preview.rawResponse,exceptionCode:preview.exceptionCode})}}</pre></div>
      <div v-if="step===4&&preview"><el-alert :closable="false" type="info" :title="preview.message"/><el-descriptions :column="2" border><el-descriptions-item label="Protocol ID">{{preview.protocolId}}</el-descriptions-item><el-descriptions-item label="Protocol Version">{{preview.protocolVersion}}</el-descriptions-item><el-descriptions-item label="Parser">{{preview.parser}}</el-descriptions-item></el-descriptions><h4>Raw</h4><pre>{{pretty(preview.raw)}}</pre><template v-if="preview.rawRequest"><h4>Request HEX / Response HEX</h4><pre>{{preview.rawRequest}}
{{preview.rawResponse}}</pre></template><h4>Parsed</h4><pre>{{pretty(preview.parsed)}}</pre><h4>StandardMessage</h4><pre>{{pretty(preview.standardMessages)}}</pre><h4>属性 / 事件 / 告警映射</h4><pre>{{pretty(preview.mapping)}}</pre><template v-if="preview.pointTable"><h4>点表</h4><pre>{{pretty(preview.pointTable)}}</pre></template></div>
      <div v-if="step===5&&result"><el-alert type="success" title="设备已创建并启用" :closable="false"/><p>请立即保存设备 Secret；关闭后无法再次查询，可在设备列表重新生成或禁用。</p><el-descriptions :column="1" border><el-descriptions-item label="Device ID">{{result.device.id}}</el-descriptions-item><el-descriptions-item label="Client ID">{{result.clientId}}</el-descriptions-item><el-descriptions-item label="Username / X-Device-Key">{{result.username}}</el-descriptions-item><el-descriptions-item label="Device Secret">{{result.credential.secret}}</el-descriptions-item></el-descriptions><el-button @click="copy">复制接入结果</el-button><template v-if="standard"><p>HTTP：POST {{ingestURL}}，使用 X-Device-Key / X-Device-Secret 请求头，上报正文与测试 JSON 相同。event / state 替换末尾 property。</p><p>MQTT：先 POST /api/v1/device-mqtt/token，使用同一设备请求头换取 token，将 token 作为 MQTT password。使用返回的 username 和本页 Client ID。</p><pre>/iot/up/{{identity}}/property
/iot/up/{{identity}}/event
/iot/up/{{identity}}/state
/iot/up/{{identity}}/command-reply
/iot/down/{{identity}}/command</pre><p>命令回执的 data 包含 commandId 和 success（布尔值）。设备收到命令后应按 id 去重执行，再上报回执。</p></template><p v-else>运行时将自动加载接入配置。实际在线状态与接收数据请在设备列表和原始报文中查看。</p></div>
    </div>
    <template #footer><el-button :disabled="busy" @click="emit('close')">{{step===5?'关闭':'取消'}}</el-button><el-button v-if="step>0&&step<5" :disabled="busy" @click="step--">上一步</el-button><el-button v-if="step<4" type="primary" :disabled="nextDisabled" @click="step++">下一步</el-button><el-button v-if="step===4" type="primary" :disabled="busy||!preview?.success" :loading="busy" @click="finish">完成并启用</el-button></template>
  </el-dialog>
</template>

<style scoped>
.onboarding-body{max-height:65vh;overflow:auto;padding:22px 4px;min-height:260px}.onboarding-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 18px}.connector-options{display:flex;gap:12px;flex-wrap:wrap}.connector-options .el-radio{margin:0}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--el-fill-color-light);padding:12px;border-radius:6px}.el-alert,.el-descriptions{margin-top:14px}@media(max-width:600px){.onboarding-grid{grid-template-columns:1fr}}
</style>
