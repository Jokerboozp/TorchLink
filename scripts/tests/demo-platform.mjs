// Run from the repository root. Creates demo and built-in test resources; services may inspect or back up the tenant.
import {readFile,writeFile,mkdir} from 'node:fs/promises'
import {demoConfig,redactDemo} from '../lib/demo-config.mjs'
import {randomUUID} from 'node:crypto'
import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
const {env,origin,prefix,dir,tenant,tcpPort,udpPort}=await demoConfig()
await mkdir(dir,{recursive:true})
let token, credential
let state={prefix,origin,tenant,results:[],ids:{}}
try{const previous=JSON.parse(await readFile(`${dir}/results.json`,'utf8'));if(previous.origin!==origin || previous.tenant!==tenant || previous.prefix!==prefix)throw Error('结果目录属于其他地址、租户或旧格式，请换一个输出目录');state=previous}catch(e){if(e.code!=='ENOENT')throw e}
async function req(path,{method='GET',body,device=false,raw=false,binary=false,authToken=token}={}){
 const headers={};if(authToken)headers.Authorization=`Bearer ${authToken}`
 if(device){delete headers.Authorization;headers['X-Device-Key']=credential.accessKey;headers['X-Device-Secret']=credential.secret}
 if(body!==undefined && !(body instanceof FormData))headers['Content-Type']='application/json'
 const response=await fetch(origin+path,{method,headers,redirect:'error',body:body instanceof FormData?body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(240000)})
 if(!response.ok){const failure=new Error(`${method} ${path}: HTTP ${response.status}`);failure.status=response.status;throw failure}
 return binary?Buffer.from(await response.arrayBuffer()):raw?response.text():response.json()
}
async function check(name,action){
 const skip=env.IOT_DEMO_SKIP_AI==='1' && /真实模型|智能巡检|巡检成功|模型管理/.test(name) || env.IOT_DEMO_SKIP_BACKUP==='1' && name.startsWith('备份') || env.IOT_DEMO_SKIP_SOCKETS==='1' && /TCP\/UDP 实际/.test(name)
 if(skip){state.results.push({stage,name,status:'跳过',detail:'按运行参数跳过',at:new Date().toISOString()});console.log(`跳过：${name}`)}
 else try{const detail=await action();state.results.push({stage,name,status:'通过',detail:detail || '',at:new Date().toISOString()});console.log(`通过：${name}${detail?' · '+detail:''}`)}
 catch(e){const detail=redactDemo(e.message,env);state.results.push({stage,name,status:'未通过',detail,at:new Date().toISOString()});console.log(`未通过：${name} · ${detail}`)}
 await writeFile(`${dir}/results.json`,JSON.stringify(state,null,2))
}
async function list(path){const items=[];for(let page=1;page<=10000;page++){const value=await req(`${path}${path.includes('?')?'&':'?'}page=${page}&pageSize=100`);items.push(...(value.items||[]));if(!(value.items?.length) || items.length>=Number(value.total??value.count??items.length))return {...value,items}}throw Error('分页超过限制')}
async function until(path,condition){for(let n=0;n<40;n++){try{const value=await req(path);if(condition(value))return value}catch(e){if(e.status!==404)throw e}await new Promise(r=>setTimeout(r,500))}throw new Error(`等待状态超时 ${path}`)}
const login=await req('/api/v1/auth/login',{method:'POST',body:{username:env.IOT_ADMIN_USER || 'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant}})
token=login.accessToken;assert.ok(token)
const stage=process.argv[2] || 'inspect'
const resultStart=state.results.length
if(stage==='inspect'){
 for(const [name,path] of Object.entries({产品:'/api/v1/products',设备:'/api/v1/device-registry',协议:'/api/v2/protocols',网关:'/api/v2/device-access-profiles',告警:'/api/v1/alarms',规则:'/api/v1/rules',原文:'/api/v1/raw-messages',摄像头:'/api/v1/integrations/video/cameras',知识:'/api/v1/knowledge/documents',模型:'/api/v1/ai/providers',智能体:'/api/v1/ai/workflows',备份:'/api/v1/backups',巡检:'/api/v1/ai/health-inspection/progress'})){
  await check(`读取${name}`,async()=>{let v;try{v=await req(path)}catch(e){if(name==='巡检'&&e.status===404)return '当前无巡检任务；业务空状态';throw e}return JSON.stringify({count:v.total??v.items?.length,status:v.status,healthy:v.healthy,indexMode:v.indexMode})})
 }
}
if(stage==='business'){
 const productId=`${prefix}-http`,deviceId=`${prefix}-sensor`, stamp=Date.now()
 state.ids.productId=productId;state.ids.deviceId=deviceId
 await check('演示产品与设备登记',async()=>{
  try{await req(`/api/v1/device-registry/${deviceId}/connection`);const v=await req(`/api/v1/device-registry/${deviceId}/credentials`,{method:'POST'});credential=v.credential}
  catch(e){if(e.status!==404)throw e;const draft={productId,productName:'演示 · 消防环境监测',deviceId,name:'演示 · 一层温湿度传感器',type:'HTTP',messageKind:'property',payload:{id:`demo-setup-${stamp}`,timestamp:stamp,data:{temperature:26.5,humidity:48}}};const preview=await req('/api/v1/onboarding/test',{method:'POST',body:draft});assert.equal(preview.success,true);const v=await req('/api/v1/onboarding',{method:'POST',body:{...draft,testToken:preview.testToken}});credential=v.credential}
  assert.ok(credential?.secret);return deviceId
 })
 const base=`/api/v1/device-ingest/standard/${tenant}/${productId}/${deviceId}/`
 const ingest=async(kind,data,id)=>{const v=await req(base+kind,{method:'POST',body:{id:id || `demo-${kind}-${Date.now()}`,timestamp:Date.now(),data},device:true});await until(`/api/v1/raw-messages/${v.messageId}`,x=>x.parseStatus==='PARSED');return v}
 await check('属性上报、历史曲线与在线状态',async()=>{for(let n=0;n<8;n++)await ingest('property',{temperature:24+n*0.5,humidity:45+n,demoGroup:prefix});await ingest('state',{connectionStatus:'CONNECTED'});const v=await req(`/api/v1/device-registry/${deviceId}/connection`);assert.ok(v.device);return '温度 24–27.5℃、湿度 45–52%，8 条属性记录'})
 await check('演示规则、告警触发、确认与关闭',async()=>{
  const rule={id:`${prefix}-high-temp`,name:'演示 · 高温告警（仅演示设备）',alarmType:'DEVICE_FAULT',level:'LOW',match:'all',enabled:true,conditions:[{field:'demoGroup',operator:'eq',value:prefix},{field:'temperature',operator:'gt',value:40}]}
  const all=await list('/api/v1/rules');await req(all.items?.some(x=>x.id===rule.id)?`/api/v1/rules/${rule.id}`:'/api/v1/rules',{method:all.items?.some(x=>x.id===rule.id)?'PUT':'POST',body:rule})
  await ingest('property',{demoGroup:prefix,temperature:58,humidity:38})
  const alarms=await until(`/api/v1/alarms?deviceId=${deviceId}&pageSize=100`,x=>x.items?.some(a=>a.status==='ACTIVE'))
  const alarm=alarms.items.find(a=>a.status==='ACTIVE');state.ids.closedAlarm=alarm.alarmId
  assert.equal((await req(`/api/v1/alarms/${alarm.alarmId}/actions`,{method:'POST',body:{action:'ACKED'}})).status,'ACKED')
  assert.equal((await req(`/api/v1/alarms/${alarm.alarmId}/actions`,{method:'POST',body:{action:'CLOSED'}})).status,'CLOSED')
  await ingest('property',{demoGroup:prefix,temperature:62,humidity:35});return '保留已关闭告警与新触发告警，规则限定演示标记'
 })
 await check('原始报文详情、下载与试运行回放',async()=>{
  const raw=(await req(`/api/v1/raw-messages?deviceId=${deviceId}&pageSize=10`)).items[0];const id=raw.messageId;state.ids.raw=id
  assert.equal((await req(`/api/v1/raw-messages/${id}/download`)).messageId,id)
  const replay=await req('/api/v1/raw-messages/replay',{method:'POST',body:{deviceId,productId,start:stamp-1000,end:Date.now()+1000,mode:'DRY_RUN',ratePerSecond:20}})
  const done=await until(`/api/v1/replays/${replay.id}`,v=>['COMPLETED','FAILED'].includes(v.status));assert.equal(done.status,'COMPLETED');assert.equal(done.failed,0);state.ids.replay=replay.id;return `${done.processed} 条回放成功`
 })
 await check('摄像头登记与设备映射',async()=>{
  const camera={cameraId:`${prefix}-camera`,cameraName:'演示 · 一层走廊摄像头',brand:'dahua',cameraPoint:'演示园区一层走廊',building:'演示楼',floor:'1F',room:'走廊',deviceId,enabled:true}
  const all=await list('/api/v1/integrations/video/cameras');const exists=all.items?.some(x=>x.cameraId===camera.cameraId)
  await req('/api/v1/integrations/video/cameras'+(exists?`/${camera.cameraId}`:''),{method:exists?'PUT':'POST',body:camera});state.ids.camera=camera.cameraId;return '仅摄像头元数据和映射，不伪造视频流'
 })
}

if(stage==='protocols'){
 for(const [kind,transport] of [['json','HTTP'],['points','MODBUS_TCP']])await check(`协议生成与发布：${kind}`,async()=>{
  const id=`${prefix}-${kind}`,form=new FormData()
  form.append('inputKind',kind==='json'?'sample':'point-table');form.append('transport',transport);form.append('payloadFormat',kind==='json'?'json':'hex');form.append('name',kind==='json'?'演示 · JSON 字段映射':'演示 · Modbus 温湿度点表')
  const sample=kind==='json'?JSON.stringify({data:{temperature:25.5,humidity:48}}):'name,identifier,functionCode,address,addressNotation,dataType,scale,unit\n温度,temperature,3,0,zero_based,uint16,0.1,℃\n湿度,humidity,3,1,zero_based,uint16,0.1,%'
  const filename=kind==='json'?'演示报文.json':'演示点表.csv';await writeFile(`${dir}/${filename}`,sample);form.append('file',new Blob([sample]),filename)
  const draft=await req('/api/v1/ai/protocol-assistant/generate',{method:'POST',body:form})
  if(kind==='points')draft.config.startAddress=0
  const payload=kind==='json'?JSON.parse(sample):'00 01 00 00 00 07 01 03 04 00 FF 01 E0'
  const version=`demo-${Date.now()}`
  const preview=await req('/api/v1/ai/protocol-assistant/preview',{method:'POST',body:{draft,payload,payloadFormat:draft.payloadFormat}})
  assert.notEqual(preview.success,false)
  const saved=await req('/api/v1/ai/protocol-assistant/publish',{method:'POST',body:{id,version,draft,payload,payloadFormat:draft.payloadFormat,status:'DRAFT'}})
  await req(`/api/v2/protocols/${id}/releases/${version}/publish`,{method:'POST',body:{}});state.ids[kind+'Protocol']={id,version};return id
 })
 await check('Go 协议源码编译、样例与发布',async()=>{
  let source=await readFile('internal/protocolbuild/functiontemplates/tcp.go.txt','utf8')
  source=source.replaceAll('DeviceID: "7"',`DeviceID: "${prefix}-tcp-7"`).replace('DeviceID: strconv.Itoa',`DeviceID: "${prefix}-tcp-" + strconv.Itoa`)
  await writeFile(`${dir}/演示TCP协议.go`,source)
  const id=`${prefix}-tcp`,version=`demo-${Date.now()}`,form=new FormData();form.append('file',new Blob([source]),'demo.go');form.append('name','演示 · TCP/UDP 温度与探测协议');form.append('version',version);form.append('publish','true')
  const value=await req(`/api/v2/protocols/${id}/source-releases`,{method:'POST',body:form});state.ids.tcpProtocol={id,version:value.release?.version || version};return id
 })
 if(state.ids.tcpProtocol)await check('产品协议绑定与多接入网关',async()=>{
  const {id,version}=state.ids.tcpProtocol,productId=`${prefix}-tcp-product`
  const product={id:productId,name:'演示 · TCP 消防主机',category:'gateway',transport:'TCP',payloadFormat:'hex',protocolPackageId:`${id}@${version}`,status:'ENABLED',description:'演示软件接入网关与多设备连接',thingModel:{properties:[{identifier:'temperature',name:'温度',dataType:'number',unit:'℃'}],events:[],commands:[{identifier:'ping',name:'探测设备',fields:[]}]}}
  const products=await list('/api/v1/products');const exists=products.items.some(x=>x.id===productId);await req('/api/v1/products'+(exists?'/'+productId:''),{method:exists?'PUT':'POST',body:product})
  await req(`/api/v2/products/${productId}/protocol-binding`,{method:'POST',body:{protocolId:id,version}})
  for(const [network,port] of [['tcp',tcpPort],['udp',udpPort]]){
   const profile={id:`${prefix}-${network}-gateway`,productId,protocolId:id,protocolVersion:version,mode:'listener',network,host:'0.0.0.0',port,timeoutMs:5000,autoRegister:true,enabled:env.IOT_DEMO_SKIP_SOCKETS!=='1',connectionMode:'listen'}
   const all=await req('/api/v2/device-access-profiles');const exists=all.items.some(x=>x.id===profile.id)
   if(profile.enabled && all.items.some(x=>x.id!==profile.id && x.enabled && x.mode==='listener' && x.network===network && Number(x.port)===port))throw Error(`演示 ${network} 端口 ${port} 已由其他网关占用，请指定空闲端口`)
   await req('/api/v2/device-access-profiles'+(exists?'/'+profile.id:''),{method:exists?'PUT':'POST',body:profile})
  }
  state.ids.tcpProduct=productId;return `TCP ${tcpPort}、UDP ${udpPort}，网关${env.IOT_DEMO_SKIP_SOCKETS==='1'?'未启用':'已启用'}`
 })
}
if(stage==='services'){
 await check('知识库上传与文档详情读取',async()=>{
  const text=`演示园区消防处置手册。演示一层温湿度传感器正常温度为20至30摄氏度。温度超过40摄氏度触发演示高温告警，应先核对设备位置和现场情况，再确认告警。演示接入网关TCP端口${tcpPort}，UDP端口${udpPort}。设备支持ping探测，成功应答表示连接正常。本资料仅用于功能演示。`
  await writeFile(`${dir}/演示消防处置手册.txt`,text)
  const form=new FormData();form.append('file',new Blob([text]),'演示消防处置手册.txt');form.append('workflowId','ops-assistant')
  const value=await req('/api/v1/knowledge/documents',{method:'POST',body:form});state.ids.document=value.id
  const detail=await req(`/api/v1/knowledge/documents/${value.id}`);return `文档 ${value.id}，状态 ${value.status || detail.status || '已返回详情'}`
 })
 await check('智能助手真实模型回答',async()=>{
  const text=await req('/api/v1/ai/chat/stream',{method:'POST',body:{question:'请查询名称含“演示”的设备及告警，并结合演示消防处置手册给出简短说明。',workflowId:'ops-assistant',conversationId:`${prefix}-chat`,maxTokens:600},raw:true})
  await writeFile(`${dir}/ai-chat.sse`,text);assert.ok(text.includes('data:'));if(/event: error/.test(text))throw new Error('模型流返回错误事件，详见本地记录');return '已保存真实流式回答'
 })
 await check('智能巡检任务与结果',async()=>{
  const job=await req('/api/v1/ai/health-inspection/run',{method:'POST',body:{}});state.ids.inspection=job.jobId || job.id
  for(let i=0;i<150;i++){const v=await req('/api/v1/ai/health-inspection/progress');if(v.status!=='running'){await writeFile(`${dir}/inspection.json`,JSON.stringify(v,null,2));assert.equal(v.status,'succeeded');return `巡检任务 ${job.id}`}await new Promise(r=>setTimeout(r,1000))}throw new Error('巡检等待超时')
 })
 await check('备份生成、文件清单与文件校验',async()=>{
  const value=await req('/api/v1/backups',{method:'POST',body:{type:'FULL'}});const id=value.id || value.backup?.id;if(!id)throw new Error('备份响应缺少 ID');state.ids.backup=id
  await req(`/api/v1/backups/${encodeURIComponent(id)}/files`);await req(`/api/v1/backups/${encodeURIComponent(id)}/restore-drill`,{method:'POST'});return `备份 ${id}，仅文件校验，未恢复业务数据库`
 })
}

if(stage==='refine'){
 await check('Excel 点表上传、字段预览与发布',async()=>{
  const form=new FormData();for(const [k,v] of Object.entries({inputKind:'point-table',transport:'MODBUS_TCP',payloadFormat:'hex',name:'演示 · Excel 温湿度点表'}))form.append(k,v)
  form.append('file',new Blob([await readFile(`${dir}/演示点表.xlsx`)]),'演示点表.xlsx')
  const draft=await req('/api/v1/ai/protocol-assistant/generate',{method:'POST',body:form});draft.config.startAddress=0
  const payload='00 01 00 00 00 07 01 03 04 00 FF 01 E0',id=`${prefix}-points`,version=`demo-${Date.now()}`
  const preview=await req('/api/v1/ai/protocol-assistant/preview',{method:'POST',body:{draft,payload,payloadFormat:'hex'}});assert.notEqual(preview.success,false)
  assert.equal(preview.standardMessage.properties.temperature,25.5)
  await req('/api/v1/ai/protocol-assistant/publish',{method:'POST',body:{id,version,draft,payload,payloadFormat:'hex',status:'DRAFT'}})
  await req(`/api/v2/protocols/${id}/releases/${version}/publish`,{method:'POST',body:{}});state.ids.pointsProtocol={id,version};return '温度 25.5℃、湿度48%，已发布'
 })
 await check('TCP/UDP 实际上报、设备命名及探测命令应答',async()=>{
  for(const [id,name] of [[7,'演示 · TCP 消防主机 A'],[8,'演示 · TCP 消防主机 B'],[9,'演示 · UDP 消防主机']]){
   const deviceId=`${prefix}-tcp-${id}`,v=await until(`/api/v1/device-registry/${deviceId}/connection`,x=>x.latest || x.latestProperties?.length)
   await req(`/api/v1/device-registry/${deviceId}`,{method:'PUT',body:{...v.device,name}})
   assert.ok(v.latest || v.latestProperties?.length)
  }
  const reply=await req(`/api/v2/device-access-profiles/${prefix}-tcp-gateway/devices/${prefix}-tcp-7/commands`,{method:'POST',body:{type:'ping',requestId:`demo-ping-${Date.now()}`,confirmed:true}})
  assert.equal(reply.status,'acknowledged');state.ids.commandRaw=reply.rawMessageId;await req(`/api/v1/raw-messages/${reply.rawMessageId}`);return '两台TCP、一台UDP通过真实套接字上报，ping已应答'
 })
 await check('巡检成功状态与报告读取',async()=>{const report=JSON.parse(await readFile(`${dir}/inspection.json`,'utf8'));assert.equal(report.status,'succeeded');return `真实巡检报告已保存，任务 ${report.jobId || state.ids.inspection || "已完成"}`})
 await check('模型管理连接测试',async()=>{const active=await req('/api/v1/ai/providers');const {provider,baseUrl,model}=active.config;const result=await req('/api/v1/ai/providers/test',{method:'POST',body:{provider,baseUrl,model}});assert.equal(result.success,true);return '沿用当前模型，未修改全局配置'})
 await check('接入测试：正常、告警、恢复与事件模板',async()=>{
  const v=await req('/api/v1/test-devices/provision',{method:'POST',body:{reset:false}})
  for(const kind of ['data','alarm','recovery','event']){const body={...v.templates[kind],messageId:`demo-template-${kind}-${Date.now()}`};const sent=await req(`/api/v1/device-registry/${v.device.id}/debug`,{method:'POST',body});const id=sent.archive?.messageId || sent.messageId || body.messageId;await until(`/api/v1/raw-messages/${id}`,x=>x.parseStatus==='PARSED')}
  state.ids.testDevice=v.device.id;return '四种内置模板均完成真实解析'
 })
}
if(stage==='refine' || stage==='children'){
 await check('演示主子设备登记与归属',async()=>{
  for(const [id,name,role,gatewayId] of [[`${prefix}-parent`,'演示 · 楼层实体网关','GATEWAY',''],[`${prefix}-child`,'演示 · 网关下烟感','CHILD',`${prefix}-parent`]]){
   const body={id,name,productId:state.ids.productId,status:'ENABLED',deviceRole:role,gatewayId,description:'功能演示数据'}
   const all=await list('/api/v1/device-registry');const exists=all.items.some(x=>x.device.id===id);await req('/api/v1/device-registry'+(exists?'/'+id:''),{method:exists?'PUT':'POST',body})
  }
  const children=await req(`/api/v1/device-registry/${prefix}-parent/children`);assert.ok(children.items.length);return '实体网关与子设备归属可查看'
 })
}
if(stage==='mqtt')await check('MQTT 上报、设备控制及命令回执',async()=>{
 const deviceId=`${prefix}-mqtt-sensor`,productId=`${prefix}-mqtt-product`,stamp=Date.now()
 try{await req(`/api/v1/device-registry/${deviceId}/connection`);credential=(await req(`/api/v1/device-registry/${deviceId}/credentials`,{method:'POST'})).credential}
 catch(e){if(e.status!==404)throw e;const draft={deviceId,productId,productName:'演示 · MQTT 可控设备',name:'演示 · MQTT 温控器',type:'MQTT',messageKind:'property',payload:{id:`demo-mqtt-setup-${stamp}`,timestamp:stamp,data:{temperature:25}}};const v=await req('/api/v1/onboarding/test',{method:'POST',body:draft});assert.equal(v.success,true);credential=(await req('/api/v1/onboarding',{method:'POST',body:{...draft,testToken:v.testToken}})).credential}
 const products=await list('/api/v1/products');const product=products.items.find(x=>x.id===productId)
 await req(`/api/v1/products/${productId}`,{method:'PUT',body:{...product,thingModel:{properties:[{identifier:'temperature',name:'温度',dataType:'number',unit:'℃'}],events:[],commands:[{identifier:'set-temperature',name:'设置目标温度',fields:[{identifier:'value',name:'目标温度',dataType:'integer',required:true,unit:'℃'}]}]}}})
 const auth=await req('/api/v1/device-mqtt/token',{method:'POST',device:true})
 const mqtt=createRequire(new URL('../../iot_front/package.json',import.meta.url))('mqtt')
 const broker=(env.IOT_DEMO_MQTT_URL || `mqtt://${new URL(origin).hostname}:1883`).replace(/^tcp:/,'mqtt:')
 const client=mqtt.connect(broker,{clientId:`${prefix}-client`,username:auth.username,password:auth.token,reconnectPeriod:0,connectTimeout:12000})
 try{
  await new Promise((resolve,reject)=>{client.once('connect',resolve);client.once('error',()=>reject(new Error('MQTT连接失败')))})
  const topic=`/iot/up/${tenant}/${productId}/${deviceId}`
  await client.subscribeAsync(`/iot/down/${tenant}/${productId}/${deviceId}/command`,{qos:1})
  client.on('message',async(_,bytes)=>{const cmd=JSON.parse(bytes);await client.publishAsync(topic+'/command-reply',JSON.stringify({id:`demo-reply-${cmd.id}`,timestamp:Date.now(),data:{commandId:cmd.id,success:true,result:'演示设备已设置目标温度'}}),{qos:1})})
  await client.publishAsync(topic+'/property',JSON.stringify({id:`demo-mqtt-${stamp}`,timestamp:stamp,data:{temperature:26,humidity:51}}),{qos:1})
  await client.publishAsync(topic+'/state',JSON.stringify({id:`demo-mqtt-state-${stamp}`,timestamp:Date.now(),data:{connectionStatus:'CONNECTED'}}),{qos:1})
  const command=await req(`/api/v1/device-registry/${deviceId}/commands`,{method:'POST',body:{id:`demo-command-${stamp}`,type:'set-temperature',data:{value:27},confirmed:true}})
  const history=await until(`/api/v1/device-registry/${deviceId}/commands`,v=>v.items.some(x=>x.id===command.id&&x.status==='SUCCEEDED'))
  state.ids.mqttDevice=deviceId;state.ids.mqttCommand=command.id;return '真实 Broker 上报与回执，命令状态 SUCCEEDED'
 }finally{await client.endAsync()}
})
if(stage==='access')await check('用户、角色、租户和设备告警范围验证',async()=>{
 assert.ok(state.ids.deviceId,'先运行 business 阶段')
 const role={id:`${prefix}-reader`,name:'演示 · 设备与告警只读',permissions:['menu:devices','menu:alarms','menu:dashboard']}
 const roles=await req('/api/v1/access/roles')
 await req('/api/v1/access/roles'+(roles.items?.some(x=>x.id===role.id)?'/'+role.id:''),{method:roles.items?.some(x=>x.id===role.id)?'PUT':'POST',body:role})
 const users=await req('/api/v1/access/users')
 state.ids.users=[]
 for(const [suffix,scope] of [['reader','selected'],['empty','none']]){
  const password=env.IOT_DEMO_USER_PASSWORD || randomUUID()+'Aa9!'
  const user={username:`${prefix}-${suffix}`,displayName:`演示 · ${scope==='none'?'无设备用户':'指定设备只读用户'}`,password,enabled:true,roleIds:[role.id],permissions:[],deviceScope:scope,deviceIds:scope==='selected'?[state.ids.deviceId]:[]}
  const existing=users.items?.find(x=>x.username===user.username)
  if(existing && !existing.displayName?.startsWith('演示'))throw Error('同名账户不带演示标记，拒绝修改')
  await req('/api/v1/access/users'+(existing?'/'+user.username:''),{method:existing?'PUT':'POST',body:user})
  if(existing)await req(`/api/v1/access/users/${user.username}/password`,{method:'POST',body:{password}})
  const login=await req('/api/v1/auth/login',{method:'POST',body:{username:user.username,password,tenantId:tenant},authToken:null})
  assert.equal(login.tenantId,tenant)
  const options={authToken:login.accessToken}
  const devices=await req('/api/v1/device-registry',options),events=await req('/api/v1/events',options)
  assert.equal(devices.total,scope==='selected'?1:0)
  assert.ok(events.alarms.every(x=>scope==='selected' && x.deviceId===state.ids.deviceId))
  if(scope==='none'){assert.ok(!events.permissions.includes('menu:alarms'));assert.equal(events.devices.length,0);assert.equal(events.alarms.length,0)}
  else { const visible=await req('/api/v1/alarms',options);assert.ok(visible.items.length>0);assert.ok(events.alarms.length>0);assert.ok(visible.items.every(x=>x.deviceId===state.ids.deviceId));await assert.rejects(req(`/api/v1/device-registry/${prefix}-parent/connection`,options),e=>e.status===403) }
  await assert.rejects(req('/api/v1/mqtt/token',{...options,method:'POST'}),e=>e.status===403)
  state.ids.users.push(user.username)
 }
 return `租户 ${tenant}；两个演示用户已创建。密码来自 IOT_DEMO_USER_PASSWORD 或随机生成，不写入报告；管理员可重置。`
})
console.log(`记录：${dir}/results.json`)
if(state.results.slice(resultStart).some(item=>item.status==='未通过'))process.exitCode=1
