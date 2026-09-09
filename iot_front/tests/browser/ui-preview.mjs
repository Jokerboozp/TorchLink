import http from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
// 独立的界面验收夹具：仅绑定回环地址，不代理真实服务。
const root=fileURLToPath(new URL('../../dist/', import.meta.url))
const port=Number(process.env.IOT_UI_PREVIEW_PORT || 4173)
const now=Date.now()
const products=[{id:'product-demo',name:'烟雾探测器',category:'smoke',transport:'MQTT',payloadFormat:'json',status:'ENABLED',description:'浏览器验收示例产品'}]
const devices=[{device:{id:'device-demo',name:'一层走廊烟感',productId:'product-demo',deviceRole:'DIRECT',status:'ENABLED',accessKey:'示例接入标识',secretHint:'示例'},runtimeState:{businessStatus:'ONLINE',lastSeenAt:now,connectionStatus:'CONNECTED'},childCount:0}]
const alarms=[{alarmId:'alarm-demo',deviceId:'device-demo',deviceName:'一层走廊烟感',alarmType:'FIRE_RISK',alarmLevel:'HIGH',status:'ACTIVE',source:'device',triggerCount:1,firstTriggeredAt:now,lastTriggeredAt:now}]
const list=items=>({items,total:items.length,count:items.length,page:1,pageSize:20})
const server=http.createServer(async(req,res)=>{const u=new URL(req.url,'http://localhost');if(u.pathname.startsWith('/api/')){
res.setHeader('Content-Type','application/json; charset=utf-8');let data=list([])
if(u.pathname==='/api/v1/auth/login')data={accessToken:'local-ui-fixture',tenantId:'界面验收租户',role:'admin'}
else if(req.method!=='GET'){res.statusCode=503;data={message:'界面验收环境不执行实际业务操作'}}
else if(u.pathname==='/api/v1/products')data=list(products)
else if(u.pathname==='/api/v1/device-registry')data=list(devices)
else if(u.pathname==='/api/v1/devices')data={...list([]),total:u.searchParams.has('unregistered') ? 0 : 1,online:1,offline:0}
else if(u.pathname==='/api/v1/alarms')data=list(alarms)
else if(u.pathname==='/api/v1/alarms/alarm-demo')data=alarms[0]
else if(u.pathname==='/api/v1/ai/providers')data={...list([]),active:{id:'disabled',name:'未启用',enabled:false},healthy:false,healthMessage:'验收环境未连接模型',config:null}
else if(u.pathname==='/api/v1/knowledge/documents')data={...list([{id:'document-demo',filename:'消防设备手册',workflowId:'assistant',status:'INDEXED',createdAt:now,metadata:{chunks:3,size:2048}}]),persistentIndex:true,indexMode:'weaviate'}
else if(u.pathname==='/api/v1/ai/workflows')data=list([{id:'assistant',name:'运维助手',description:'查询设备和告警，检索处置知识',enabled:true,capabilities:['chat']}])
else if(u.pathname.endsWith('/knowledge-binding'))data={retrievalMode:'auto',topK:5,minScore:0.25,noMatchPolicy:'allow-model'}
else if(u.pathname==='/api/v1/ai/health-inspection/progress')data={status:'idle'}
else if(u.pathname==='/api/v1/connectors/types')data=list(['MQTT','HTTP','TCP','UDP','MODBUS_TCP'].map(type=>({type,name:type,supported:true})))
res.end(JSON.stringify(data));return}
try{const pathname=u.pathname==='/'?'index.html':u.pathname;const content=await readFile(join(root,pathname));res.setHeader('Content-Type',({'.html':'text/html; charset=utf-8','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'})[extname(pathname)]||'application/octet-stream');res.end(content)}catch{res.statusCode=404;res.end('未找到文件')}})
server.listen(port,'127.0.0.1',()=>console.log(`界面验收预览：http://127.0.0.1:${port}（仅合成数据，所有业务写操作禁用）`))
