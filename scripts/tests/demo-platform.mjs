// Run from the repository root. Creates demo and built-in test resources; services may inspect or back up the tenant.
import {readFile,writeFile,mkdir} from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import {demoConfig,redactDemo} from '../lib/demo-config.mjs' /* 引入当前代码需要的依赖。 */
import {randomUUID} from 'node:crypto' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import {createRequire} from 'node:module' /* 引入当前代码需要的依赖。 */
const {env,origin,prefix,dir,tenant,tcpPort,udpPort}=await demoConfig() /* 执行当前语句并推进处理流程。 */
await mkdir(dir,{recursive:true}) /* 等待异步操作完成。 */
let token, credential /* 声明 token。 */
let state={prefix,origin,tenant,results:[],ids:{}} /* 声明 state。 */
try{const previous=JSON.parse(await readFile(`${dir}/results.json`,'utf8'));if(previous.origin!==origin || previous.tenant!==tenant || previous.prefix!==prefix)throw Error('结果目录属于其他地址、租户或旧格式，请换一个输出目录');state=previous}catch(e){if(e.code!=='ENOENT')throw e} /* 执行当前语句并推进处理流程。 */
async function req(path,{method='GET',body,device=false,raw=false,binary=false,authToken=token}={}){ /* 定义 req 函数。 */
 const headers={};if(authToken)headers.Authorization=`Bearer ${authToken}` /* 声明 headers。 */
 if(device){delete headers.Authorization;headers['X-Device-Key']=credential.accessKey;headers['X-Device-Secret']=credential.secret} /* 判断条件并选择处理分支。 */
 if(body!==undefined && !(body instanceof FormData))headers['Content-Type']='application/json' /* 判断条件并选择处理分支。 */
 const response=await fetch(origin+path,{method,headers,redirect:'error',body:body instanceof FormData?body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(240000)}) /* 声明 response。 */
 if(!response.ok){const text=await response.text().catch(()=>'');let reason='';try{const v=JSON.parse(text);reason=v.detail||v.error||v.title||''}catch{reason=text.slice(0,160)}const failure=new Error(`${method} ${path}: HTTP ${response.status}${reason?' · '+reason:''}`);failure.status=response.status;throw failure} /* 判断条件并选择处理分支。 */
 return binary?Buffer.from(await response.arrayBuffer()):raw?response.text():response.json() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
async function check(name,action){ /* 定义 check 函数。 */
 const skip=env.IOT_DEMO_SKIP_AI==='1' && /真实模型|智能巡检|巡检成功|模型管理/.test(name) || env.IOT_DEMO_SKIP_BACKUP==='1' && name.startsWith('备份') || env.IOT_DEMO_SKIP_SOCKETS==='1' && /TCP\/UDP 实际/.test(name) /* 声明 skip。 */
 if(skip){state.results.push({stage,name,status:'跳过',detail:'按运行参数跳过',at:new Date().toISOString()});console.log(`跳过：${name}`)} /* 判断条件并选择处理分支。 */
 else try{const detail=await action();state.results.push({stage,name,status:'通过',detail:detail || '',at:new Date().toISOString()});console.log(`通过：${name}${detail?' · '+detail:''}`)} /* 执行当前语句并推进处理流程。 */
 catch(e){const detail=redactDemo(e.message,env);state.results.push({stage,name,status:'未通过',detail,at:new Date().toISOString()});console.log(`未通过：${name} · ${detail}`)} /* 执行当前语句并推进处理流程。 */
 await writeFile(`${dir}/results.json`,JSON.stringify(state,null,2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
async function list(path){const items=[];for(let page=1;page<=10000;page++){const value=await req(`${path}${path.includes('?')?'&':'?'}page=${page}&pageSize=100`);items.push(...(value.items||[]));if(!(value.items?.length) || items.length>=Number(value.total??value.count??items.length))return {...value,items}}throw Error('分页超过限制')} /* 定义 list 函数。 */
async function until(path,condition){for(let n=0;n<40;n++){try{const value=await req(path);if(condition(value))return value}catch(e){if(e.status!==404)throw e}await new Promise(r=>setTimeout(r,500))}throw new Error(`等待状态超时 ${path}`)} /* 定义 until 函数。 */
const login=await req('/api/v1/auth/login',{method:'POST',body:{username:env.IOT_ADMIN_USER || 'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant}}) /* 声明 login。 */
token=login.accessToken;assert.ok(token) /* 验证实际结果符合预期。 */
const stage=process.argv[2] || 'inspect' /* 声明 stage。 */
const resultStart=state.results.length /* 声明 resultStart。 */
if(stage==='inspect'){ /* 判断条件并选择处理分支。 */
 for(const [name,path] of Object.entries({产品:'/api/v1/products',设备:'/api/v1/device-registry',协议:'/api/v2/protocols',网关:'/api/v2/device-access-profiles',告警:'/api/v1/alarms',规则:'/api/v1/rules',原文:'/api/v1/raw-messages',摄像头:'/api/v1/integrations/video/cameras',知识:'/api/v1/knowledge/documents',模型:'/api/v1/ai/providers',智能体:'/api/v1/ai/workflows',备份:'/api/v1/backups',巡检:'/api/v1/ai/health-inspection/progress'})){ /* 循环处理当前数据。 */
  await check(`读取${name}`,async()=>{let v;try{v=await req(path)}catch(e){if(name==='巡检'&&e.status===404)return '当前无巡检任务；业务空状态';throw e}return JSON.stringify({count:v.total??v.items?.length,status:v.status,healthy:v.healthy,indexMode:v.indexMode})}) /* 等待异步操作完成。 */
 } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
if(stage==='business'){ /* 判断条件并选择处理分支。 */
 const productId=`${prefix}-http`,deviceId=`${prefix}-sensor`, stamp=Date.now() /* 声明 productId。 */
 state.ids.productId=productId;state.ids.deviceId=deviceId /* 更新 state.ids.productId 的值。 */
 await check('演示产品与设备登记',async()=>{ /* 等待异步操作完成。 */
  try{await req(`/api/v1/device-registry/${deviceId}/connection`);const v=await req(`/api/v1/device-registry/${deviceId}/credentials`,{method:'POST'});credential=v.credential} /* 执行当前语句并推进处理流程。 */
  catch(e){if(e.status!==404)throw e;const v=await req('/api/v1/onboarding',{method:'POST',body:{requestId:randomUUID(),newProduct:{id:productId,name:'演示 · 消防环境监测',category:'other',protocolPackageId:'iot-standard@1.0.0',transport:'HTTP'},device:{id:deviceId,name:'演示 · 一层温湿度传感器',deviceRole:'DIRECT'},connection:{mode:'standard',transport:'HTTP'}}});credential=v.credential} /* 统一添加设备接口同时创建模板与设备。 */
  assert.ok(credential?.secret);return deviceId /* 验证实际结果符合预期。 */
 }) /* 结束当前表达式或代码块。 */
 const base=`/api/v1/device-ingest/standard/${tenant}/${productId}/${deviceId}/` /* 声明 base。 */
 const ingest=async(kind,data,id)=>{const v=await req(base+kind,{method:'POST',body:{id:id || `demo-${kind}-${Date.now()}`,timestamp:Date.now(),data},device:true});await until(`/api/v1/raw-messages/${v.messageId}`,x=>x.parseStatus==='PARSED');return v} /* 声明 ingest。 */
 await check('属性上报、历史曲线与在线状态',async()=>{for(let n=0;n<8;n++)await ingest('property',{temperature:24+n*0.5,humidity:45+n,demoGroup:prefix});await ingest('state',{connectionStatus:'CONNECTED'});const v=await req(`/api/v1/device-registry/${deviceId}/connection`);assert.ok(v.device);return '温度 24–27.5℃、湿度 45–52%，8 条属性记录'}) /* 验证实际结果符合预期。 */
 await check('演示规则、告警触发、确认与关闭',async()=>{ /* 等待异步操作完成。 */
  const rule={id:`${prefix}-high-temp`,name:'演示 · 高温告警（仅演示设备）',alarmType:'DEVICE_FAULT',level:'LOW',match:'all',enabled:true,conditions:[{field:'demoGroup',operator:'eq',value:prefix},{field:'temperature',operator:'gt',value:40}]} /* 声明 rule。 */
  const all=await list('/api/v1/rules');await req(all.items?.some(x=>x.id===rule.id)?`/api/v1/rules/${rule.id}`:'/api/v1/rules',{method:all.items?.some(x=>x.id===rule.id)?'PUT':'POST',body:rule}) /* 声明 all。 */
  await ingest('property',{demoGroup:prefix,temperature:58,humidity:38}) /* 等待异步操作完成。 */
  const alarms=await until(`/api/v1/alarms?deviceId=${deviceId}&pageSize=100`,x=>x.items?.some(a=>a.status==='ACTIVE')) /* 声明 alarms。 */
  const alarm=alarms.items.find(a=>a.status==='ACTIVE');state.ids.closedAlarm=alarm.alarmId /* 声明 alarm。 */
  assert.equal((await req(`/api/v1/alarms/${alarm.alarmId}/actions`,{method:'POST',body:{action:'ACKED'}})).status,'ACKED') /* 验证实际结果符合预期。 */
  assert.equal((await req(`/api/v1/alarms/${alarm.alarmId}/actions`,{method:'POST',body:{action:'CLOSED'}})).status,'CLOSED') /* 验证实际结果符合预期。 */
  await ingest('property',{demoGroup:prefix,temperature:62,humidity:35});return '保留已关闭告警与新触发告警，规则限定演示标记' /* 等待异步操作完成。 */
 }) /* 结束当前表达式或代码块。 */
 await check('原始报文详情、下载与试运行回放',async()=>{ /* 等待异步操作完成。 */
  const raw=(await req(`/api/v1/raw-messages?deviceId=${deviceId}&pageSize=10`)).items[0];const id=raw.messageId;state.ids.raw=id /* 声明 raw。 */
  assert.equal((await req(`/api/v1/raw-messages/${id}/download`)).messageId,id) /* 验证实际结果符合预期。 */
  const replay=await req('/api/v1/raw-messages/replay',{method:'POST',body:{deviceId,productId,start:stamp-1000,end:Date.now()+1000,mode:'DRY_RUN',ratePerSecond:20}}) /* 声明 replay。 */
  const done=await until(`/api/v1/replays/${replay.id}`,v=>['COMPLETED','FAILED'].includes(v.status));assert.equal(done.status,'COMPLETED');assert.equal(done.failed,0);state.ids.replay=replay.id;return `${done.processed} 条回放成功` /* 声明 done。 */
 }) /* 结束当前表达式或代码块。 */
 await check('摄像头登记与设备映射',async()=>{ /* 等待异步操作完成。 */
  const camera={cameraId:`${prefix}-camera`,cameraName:'演示 · 一层走廊摄像头',brand:'dahua',cameraPoint:'演示园区一层走廊',building:'演示楼',floor:'1F',room:'走廊',deviceId,enabled:true} /* 声明 camera。 */
  const all=await list('/api/v1/integrations/video/cameras');const exists=all.items?.some(x=>x.cameraId===camera.cameraId) /* 声明 all。 */
  await req('/api/v1/integrations/video/cameras'+(exists?`/${camera.cameraId}`:''),{method:exists?'PUT':'POST',body:camera});state.ids.camera=camera.cameraId;return '仅摄像头元数据和映射，不伪造视频流' /* 等待异步操作完成。 */
 }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

if(stage==='protocols'){ /* 判断条件并选择处理分支。 */
 for(const [kind,transport] of [['json','HTTP'],['points','MODBUS_TCP']])await check(`协议生成与发布：${kind}`,async()=>{ /* 循环处理当前数据。 */
  const id=`${prefix}-${kind}`,form=new FormData() /* 声明 id。 */
  form.append('inputKind',kind==='json'?'sample':'point-table');form.append('transport',transport);form.append('payloadFormat',kind==='json'?'json':'hex');form.append('name',kind==='json'?'演示 · JSON 字段映射':'演示 · Modbus 温湿度点表') /* 执行当前语句并推进处理流程。 */
  const sample=kind==='json'?JSON.stringify({data:{temperature:25.5,humidity:48}}):'name,identifier,functionCode,address,addressNotation,dataType,scale,unit\n温度,temperature,3,0,zero_based,uint16,0.1,℃\n湿度,humidity,3,1,zero_based,uint16,0.1,%' /* 声明 sample。 */
  const filename=kind==='json'?'演示报文.json':'演示点表.csv';await writeFile(`${dir}/${filename}`,sample);form.append('file',new Blob([sample]),filename) /* 声明 filename。 */
  const draft=await req('/api/v1/ai/protocol-assistant/generate',{method:'POST',body:form}) /* 声明 draft。 */
  if(kind==='points')draft.config.startAddress=0 /* 判断条件并选择处理分支。 */
  const payload=kind==='json'?JSON.parse(sample):'00 01 00 00 00 07 01 03 04 00 FF 01 E0' /* 声明 payload。 */
  const version=`demo-${Date.now()}` /* 声明 version。 */
  const preview=await req('/api/v1/ai/protocol-assistant/preview',{method:'POST',body:{draft,payload,payloadFormat:draft.payloadFormat}}) /* 声明 preview。 */
  assert.notEqual(preview.success,false) /* 验证实际结果符合预期。 */
  const saved=await req('/api/v1/ai/protocol-assistant/publish',{method:'POST',body:{id,version,draft,payload,payloadFormat:draft.payloadFormat,status:'DRAFT'}}) /* 声明 saved。 */
  await req(`/api/v2/protocols/${id}/releases/${version}/publish`,{method:'POST',body:{}});state.ids[kind+'Protocol']={id,version};return id /* 等待异步操作完成。 */
 }) /* 结束当前表达式或代码块。 */
 await check('Go 协议源码编译、样例与发布',async()=>{ /* 等待异步操作完成。 */
  let source=await readFile('internal/protocolbuild/functiontemplates/tcp.go.txt','utf8') /* 声明 source。 */
  source=source.replaceAll('DeviceID: "7"',`DeviceID: "${prefix}-tcp-7"`).replace('DeviceID: strconv.Itoa',`DeviceID: "${prefix}-tcp-" + strconv.Itoa`) /* 更新 source 的值。 */
  await writeFile(`${dir}/演示TCP协议.go`,source) /* 等待异步操作完成。 */
  const id=`${prefix}-tcp`,version=`demo-${Date.now()}`,form=new FormData();form.append('file',new Blob([source]),'demo.go');form.append('name','演示 · TCP/UDP 温度与探测协议');form.append('version',version);form.append('publish','true') /* 声明 id。 */
  const value=await req(`/api/v2/protocols/${id}/source-releases`,{method:'POST',body:form});state.ids.tcpProtocol={id,version:value.release?.version || version};return id /* 声明 value。 */
 }) /* 结束当前表达式或代码块。 */
 if(state.ids.tcpProtocol)await check('产品协议绑定与多接入网关',async()=>{ /* 判断条件并选择处理分支。 */
  const {id,version}=state.ids.tcpProtocol,productId=`${prefix}-tcp-product` /* 执行当前语句并推进处理流程。 */
  const product={id:productId,name:'演示 · TCP 消防主机',category:'gateway',transport:'TCP',payloadFormat:'hex',protocolPackageId:`${id}@${version}`,status:'ENABLED',description:'演示软件接入网关与多设备连接',thingModel:{properties:[{identifier:'temperature',name:'温度',dataType:'number',unit:'℃'}],events:[],commands:[{identifier:'ping',name:'探测设备',fields:[]}]}} /* 声明 product。 */
  const products=await list('/api/v1/products');const exists=products.items.some(x=>x.id===productId);await req('/api/v1/products'+(exists?'/'+productId:''),{method:exists?'PUT':'POST',body:product}) /* 声明 products。 */
  await req(`/api/v2/products/${productId}/protocol-binding`,{method:'POST',body:{protocolId:id,version}}) /* 等待异步操作完成。 */
  for(const [network,port] of [['tcp',tcpPort],['udp',udpPort]]){ /* 循环处理当前数据。 */
   const profile={id:`${prefix}-${network}-gateway`,productId,protocolId:id,protocolVersion:version,mode:'listener',network,host:'0.0.0.0',port,timeoutMs:5000,autoRegister:true,enabled:env.IOT_DEMO_SKIP_SOCKETS!=='1',connectionMode:'listen'} /* 声明 profile。 */
   const all=await req('/api/v2/device-access-profiles');const exists=all.items.some(x=>x.id===profile.id) /* 声明 all。 */
   if(profile.enabled && all.items.some(x=>x.id!==profile.id && x.enabled && x.mode==='listener' && x.network===network && Number(x.port)===port))throw Error(`演示 ${network} 端口 ${port} 已由其他网关占用，请指定空闲端口`) /* 判断条件并选择处理分支。 */
   await req('/api/v2/device-access-profiles'+(exists?'/'+profile.id:''),{method:exists?'PUT':'POST',body:profile}) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  state.ids.tcpProduct=productId;return `TCP ${tcpPort}、UDP ${udpPort}，网关${env.IOT_DEMO_SKIP_SOCKETS==='1'?'未启用':'已启用'}` /* 更新 state.ids.tcpProduct 的值。 */
 }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
if(stage==='services'){ /* 判断条件并选择处理分支。 */
 await check('知识库上传与文档详情读取',async()=>{ /* 等待异步操作完成。 */
  const text=`演示园区消防处置手册。演示一层温湿度传感器正常温度为20至30摄氏度。温度超过40摄氏度触发演示高温告警，应先核对设备位置和现场情况，再确认告警。演示接入网关TCP端口${tcpPort}，UDP端口${udpPort}。设备支持ping探测，成功应答表示连接正常。本资料仅用于功能演示。` /* 声明 text。 */
  await writeFile(`${dir}/演示消防处置手册.txt`,text) /* 等待异步操作完成。 */
  const form=new FormData();form.append('file',new Blob([text]),'演示消防处置手册.txt');form.append('workflowId','ops-assistant') /* 声明 form。 */
  const value=await req('/api/v1/knowledge/documents',{method:'POST',body:form});state.ids.document=value.id /* 声明 value。 */
  const detail=await req(`/api/v1/knowledge/documents/${value.id}`);return `文档 ${value.id}，状态 ${value.status || detail.status || '已返回详情'}` /* 声明 detail。 */
 }) /* 结束当前表达式或代码块。 */
 await check('智能助手真实模型回答',async()=>{ /* 等待异步操作完成。 */
  const text=await req('/api/v1/ai/chat/stream',{method:'POST',body:{question:'请查询名称含“演示”的设备及告警，并结合演示消防处置手册给出简短说明。',workflowId:'ops-assistant',conversationId:`${prefix}-chat`,maxTokens:600},raw:true}) /* 声明 text。 */
  await writeFile(`${dir}/ai-chat.sse`,text);assert.ok(text.includes('data:'));if(/event: error/.test(text))throw new Error('模型流返回错误事件，详见本地记录');return '已保存真实流式回答' /* 验证实际结果符合预期。 */
 }) /* 结束当前表达式或代码块。 */
 await check('智能巡检任务与结果',async()=>{ /* 等待异步操作完成。 */
  const job=await req('/api/v1/ai/health-inspection/run',{method:'POST',body:{}});state.ids.inspection=job.jobId || job.id /* 声明 job。 */
  for(let i=0;i<150;i++){const v=await req('/api/v1/ai/health-inspection/progress');if(v.status!=='running'){await writeFile(`${dir}/inspection.json`,JSON.stringify(v,null,2));assert.equal(v.status,'succeeded');return `巡检任务 ${job.id}`}await new Promise(r=>setTimeout(r,1000))}throw new Error('巡检等待超时') /* 循环处理当前数据。 */
 }) /* 结束当前表达式或代码块。 */
 await check('备份生成、文件清单与文件校验',async()=>{ /* 等待异步操作完成。 */
  const value=await req('/api/v1/backups',{method:'POST',body:{type:'FULL'}});const id=value.id || value.backup?.id;if(!id)throw new Error('备份响应缺少 ID');state.ids.backup=id /* 声明 value。 */
  await req(`/api/v1/backups/${encodeURIComponent(id)}/files`);await req(`/api/v1/backups/${encodeURIComponent(id)}/restore-drill`,{method:'POST'});return `备份 ${id}，仅文件校验，未恢复业务数据库` /* 等待异步操作完成。 */
 }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

if(stage==='refine'){ /* 判断条件并选择处理分支。 */
 await check('Excel 点表上传、字段预览与发布',async()=>{ /* 等待异步操作完成。 */
  const form=new FormData();for(const [k,v] of Object.entries({inputKind:'point-table',transport:'MODBUS_TCP',payloadFormat:'hex',name:'演示 · Excel 温湿度点表'}))form.append(k,v) /* 声明 form。 */
  form.append('file',new Blob([await readFile(`${dir}/演示点表.xlsx`)]),'演示点表.xlsx') /* 执行当前语句并推进处理流程。 */
  const draft=await req('/api/v1/ai/protocol-assistant/generate',{method:'POST',body:form});draft.config.startAddress=0 /* 声明 draft。 */
  const payload='00 01 00 00 00 07 01 03 04 00 FF 01 E0',id=`${prefix}-points`,version=`demo-${Date.now()}` /* 声明 payload。 */
  const preview=await req('/api/v1/ai/protocol-assistant/preview',{method:'POST',body:{draft,payload,payloadFormat:'hex'}});assert.notEqual(preview.success,false) /* 声明 preview。 */
  assert.equal(preview.standardMessage.properties.temperature,25.5) /* 验证实际结果符合预期。 */
  await req('/api/v1/ai/protocol-assistant/publish',{method:'POST',body:{id,version,draft,payload,payloadFormat:'hex',status:'DRAFT'}}) /* 等待异步操作完成。 */
  await req(`/api/v2/protocols/${id}/releases/${version}/publish`,{method:'POST',body:{}});state.ids.pointsProtocol={id,version};return '温度 25.5℃、湿度48%，已发布' /* 等待异步操作完成。 */
 }) /* 结束当前表达式或代码块。 */
 await check('TCP/UDP 实际上报、设备命名及探测命令应答',async()=>{ /* 等待异步操作完成。 */
  for(const [id,name] of [[7,'演示 · TCP 消防主机 A'],[8,'演示 · TCP 消防主机 B'],[9,'演示 · UDP 消防主机']]){ /* 循环处理当前数据。 */
   const deviceId=`${prefix}-tcp-${id}`,v=await until(`/api/v1/device-registry/${deviceId}/connection`,x=>x.latest || x.latestProperties?.length) /* 声明 deviceId。 */
   await req(`/api/v1/device-registry/${deviceId}`,{method:'PUT',body:{...v.device,name}}) /* 等待异步操作完成。 */
   assert.ok(v.latest || v.latestProperties?.length) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  const reply=await req(`/api/v2/device-access-profiles/${prefix}-tcp-gateway/devices/${prefix}-tcp-7/commands`,{method:'POST',body:{type:'ping',requestId:`demo-ping-${Date.now()}`,confirmed:true}}) /* 声明 reply。 */
  assert.equal(reply.status,'acknowledged');state.ids.commandRaw=reply.rawMessageId;await req(`/api/v1/raw-messages/${reply.rawMessageId}`);return '两台TCP、一台UDP通过真实套接字上报，ping已应答' /* 验证实际结果符合预期。 */
 }) /* 结束当前表达式或代码块。 */
 await check('巡检成功状态与报告读取',async()=>{const report=JSON.parse(await readFile(`${dir}/inspection.json`,'utf8'));assert.equal(report.status,'succeeded');return `真实巡检报告已保存，任务 ${report.jobId || state.ids.inspection || "已完成"}`}) /* 验证实际结果符合预期。 */
 await check('模型管理连接测试',async()=>{const active=await req('/api/v1/ai/providers');const {provider,baseUrl,model}=active.config;const result=await req('/api/v1/ai/providers/test',{method:'POST',body:{provider,baseUrl,model}});assert.equal(result.success,true);return '沿用当前模型，未修改全局配置'}) /* 验证实际结果符合预期。 */
 await check('接入测试：正常、告警、恢复与事件模板',async()=>{ /* 等待异步操作完成。 */
  const v=await req('/api/v1/test-devices/provision',{method:'POST',body:{reset:false}}) /* 声明 v。 */
  for(const kind of ['data','alarm','recovery','event']){const body={...v.templates[kind],messageId:`demo-template-${kind}-${Date.now()}`};const sent=await req(`/api/v1/device-registry/${v.device.id}/debug`,{method:'POST',body});const id=sent.archive?.messageId || sent.messageId || body.messageId;await until(`/api/v1/raw-messages/${id}`,x=>x.parseStatus==='PARSED')} /* 循环处理当前数据。 */
  state.ids.testDevice=v.device.id;return '四种内置模板均完成真实解析' /* 更新 state.ids.testDevice 的值。 */
 }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
if(stage==='refine' || stage==='children'){ /* 判断条件并选择处理分支。 */
 await check('演示主子设备登记与归属',async()=>{ /* 等待异步操作完成。 */
  for(const [id,name,role,gatewayId] of [[`${prefix}-parent`,'演示 · 楼层实体网关','GATEWAY',''],[`${prefix}-child`,'演示 · 网关下烟感','CHILD',`${prefix}-parent`]]){ /* 循环处理当前数据。 */
   const body={id,name,productId:state.ids.productId,status:'ENABLED',deviceRole:role,gatewayId,description:'功能演示数据'} /* 声明 body。 */
   const all=await list('/api/v1/device-registry');const exists=all.items.some(x=>x.device.id===id);await req('/api/v1/device-registry'+(exists?'/'+id:''),{method:exists?'PUT':'POST',body}) /* 声明 all。 */
  } /* 结束当前表达式或代码块。 */
  const children=await req(`/api/v1/device-registry/${prefix}-parent/children`);assert.ok(children.items.length);return '实体网关与子设备归属可查看' /* 声明 children。 */
 }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
if(stage==='mqtt')await check('MQTT 上报、设备控制及命令回执',async()=>{ /* 判断条件并选择处理分支。 */
 const deviceId=`${prefix}-mqtt-sensor`,productId=`${prefix}-mqtt-product`,stamp=Date.now() /* 声明 deviceId。 */
 try{await req(`/api/v1/device-registry/${deviceId}/connection`);credential=(await req(`/api/v1/device-registry/${deviceId}/credentials`,{method:'POST'})).credential} /* 执行当前语句并推进处理流程。 */
 catch(e){if(e.status!==404)throw e;credential=(await req('/api/v1/onboarding',{method:'POST',body:{requestId:randomUUID(),newProduct:{id:productId,name:'演示 · MQTT 可控设备',category:'other',protocolPackageId:'iot-standard@1.0.0',transport:'MQTT'},device:{id:deviceId,name:'演示 · MQTT 温控器',deviceRole:'DIRECT'},connection:{mode:'standard',transport:'MQTT'}}})).credential} /* 统一添加设备接口同时创建模板与设备。 */
 const products=await list('/api/v1/products');const product=products.items.find(x=>x.id===productId) /* 声明 products。 */
 await req(`/api/v1/products/${productId}`,{method:'PUT',body:{...product,thingModel:{properties:[{identifier:'temperature',name:'温度',dataType:'number',unit:'℃'}],events:[],commands:[{identifier:'set-temperature',name:'设置目标温度',fields:[{identifier:'value',name:'目标温度',dataType:'integer',required:true,unit:'℃'}]}]}}}) /* 等待异步操作完成。 */
 const auth=await req('/api/v1/device-mqtt/token',{method:'POST',device:true}) /* 声明 auth。 */
 const mqtt=createRequire(new URL('../../iot_front/package.json',import.meta.url))('mqtt') /* 声明 mqtt。 */
 const broker=(env.IOT_DEMO_MQTT_URL || `mqtt://${new URL(origin).hostname}:1883`).replace(/^tcp:/,'mqtt:') /* 声明 broker。 */
 const client=mqtt.connect(broker,{clientId:`${prefix}-client`,username:auth.username,password:auth.token,reconnectPeriod:0,connectTimeout:12000}) /* 声明 client。 */
 try{ /* 执行当前语句并推进处理流程。 */
  await new Promise((resolve,reject)=>{client.once('connect',resolve);client.once('error',()=>reject(new Error('MQTT连接失败')))}) /* 等待异步操作完成。 */
  const topic=`/iot/up/${tenant}/${productId}/${deviceId}` /* 声明 topic。 */
  await client.subscribeAsync(`/iot/down/${tenant}/${productId}/${deviceId}/command`,{qos:1}) /* 等待异步操作完成。 */
  client.on('message',async(_,bytes)=>{const cmd=JSON.parse(bytes);await client.publishAsync(topic+'/command-reply',JSON.stringify({id:`demo-reply-${cmd.id}`,timestamp:Date.now(),data:{commandId:cmd.id,success:true,result:'演示设备已设置目标温度'}}),{qos:1})}) /* 执行当前语句并推进处理流程。 */
  await client.publishAsync(topic+'/property',JSON.stringify({id:`demo-mqtt-${stamp}`,timestamp:stamp,data:{temperature:26,humidity:51}}),{qos:1}) /* 等待异步操作完成。 */
  await client.publishAsync(topic+'/state',JSON.stringify({id:`demo-mqtt-state-${stamp}`,timestamp:Date.now(),data:{connectionStatus:'CONNECTED'}}),{qos:1}) /* 等待异步操作完成。 */
  const command=await req(`/api/v1/device-registry/${deviceId}/commands`,{method:'POST',body:{id:`demo-command-${stamp}`,type:'set-temperature',data:{value:27},confirmed:true}}) /* 声明 command。 */
  const history=await until(`/api/v1/device-registry/${deviceId}/commands`,v=>v.items.some(x=>x.id===command.id&&x.status==='SUCCEEDED')) /* 声明 history。 */
  state.ids.mqttDevice=deviceId;state.ids.mqttCommand=command.id;return '真实 Broker 上报与回执，命令状态 SUCCEEDED' /* 更新 state.ids.mqttDevice 的值。 */
 }finally{await client.endAsync()} /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
if(stage==='access')await check('用户、角色、租户和设备告警范围验证',async()=>{ /* 判断条件并选择处理分支。 */
 assert.ok(state.ids.deviceId,'先运行 business 阶段') /* 验证实际结果符合预期。 */
 const role={id:`${prefix}-reader`,name:'演示 · 设备与告警只读',permissions:['menu:devices','menu:alarms','menu:dashboard']} /* 声明 role。 */
 const roles=await req('/api/v1/access/roles') /* 声明 roles。 */
 await req('/api/v1/access/roles'+(roles.items?.some(x=>x.id===role.id)?'/'+role.id:''),{method:roles.items?.some(x=>x.id===role.id)?'PUT':'POST',body:role}) /* 等待异步操作完成。 */
 const users=await req('/api/v1/access/users') /* 声明 users。 */
 state.ids.users=[] /* 更新 state.ids.users 的值。 */
 for(const [suffix,scope] of [['reader','selected'],['empty','none']]){ /* 循环处理当前数据。 */
  const password=env.IOT_DEMO_USER_PASSWORD || randomUUID()+'Aa9!' /* 声明 password。 */
  const user={username:`${prefix}-${suffix}`,displayName:`演示 · ${scope==='none'?'无设备用户':'指定设备只读用户'}`,password,enabled:true,roleIds:[role.id],permissions:[],deviceScope:scope,deviceIds:scope==='selected'?[state.ids.deviceId]:[]} /* 声明 user。 */
  const existing=users.items?.find(x=>x.username===user.username) /* 声明 existing。 */
  if(existing && !existing.displayName?.startsWith('演示'))throw Error('同名账户不带演示标记，拒绝修改') /* 判断条件并选择处理分支。 */
  await req('/api/v1/access/users'+(existing?'/'+user.username:''),{method:existing?'PUT':'POST',body:user}) /* 等待异步操作完成。 */
  if(existing)await req(`/api/v1/access/users/${user.username}/password`,{method:'POST',body:{password}}) /* 判断条件并选择处理分支。 */
  const login=await req('/api/v1/auth/login',{method:'POST',body:{username:user.username,password,tenantId:tenant},authToken:null}) /* 声明 login。 */
  assert.equal(login.tenantId,tenant) /* 验证实际结果符合预期。 */
  const options={authToken:login.accessToken} /* 声明 options。 */
  const devices=await req('/api/v1/device-registry',options),events=await req('/api/v1/events',options) /* 声明 devices。 */
  assert.equal(devices.total,scope==='selected'?1:0) /* 验证实际结果符合预期。 */
  assert.ok(events.alarms.every(x=>scope==='selected' && x.deviceId===state.ids.deviceId)) /* 验证实际结果符合预期。 */
  if(scope==='none'){assert.ok(!events.permissions.includes('menu:alarms'));assert.equal(events.devices.length,0);assert.equal(events.alarms.length,0)} /* 判断条件并选择处理分支。 */
  else { const visible=await req('/api/v1/alarms',options);assert.ok(visible.items.length>0);assert.ok(events.alarms.length>0);assert.ok(visible.items.every(x=>x.deviceId===state.ids.deviceId));await assert.rejects(req(`/api/v1/device-registry/${prefix}-parent/connection`,options),e=>e.status===403) } /* 验证实际结果符合预期。 */
  await assert.rejects(req('/api/v1/mqtt/token',{...options,method:'POST'}),e=>e.status===403) /* 验证实际结果符合预期。 */
  state.ids.users.push(user.username) /* 执行当前语句并推进处理流程。 */
 } /* 结束当前表达式或代码块。 */
 return `租户 ${tenant}；两个演示用户已创建。密码来自 IOT_DEMO_USER_PASSWORD 或随机生成，不写入报告；管理员可重置。` /* 返回当前处理结果。 */
}) /* 结束当前表达式或代码块。 */
console.log(`记录：${dir}/results.json`) /* 执行当前语句并推进处理流程。 */
if(state.results.slice(resultStart).some(item=>item.status==='未通过'))process.exitCode=1 /* 判断条件并选择处理分支。 */
