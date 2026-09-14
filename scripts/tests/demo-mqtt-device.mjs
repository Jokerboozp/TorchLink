// Keeps the local demo controllable for two hours. Credentials are never written to disk.
import {readFile} from 'node:fs/promises'
import {parseEnv} from 'node:util'
import {createRequire} from 'node:module'
const env={...parseEnv(await readFile('.env.local','utf8')),...process.env}
const tenant=(env.IOT_ADMIN_TENANTS||'tenant_001').split(',')[0].trim(),device='demo-20260914-mqtt-sensor',product='demo-20260914-mqtt-product'
async function req(path,body,headers={}){const r=await fetch('http://127.0.0.1:5173'+path,{method:'POST',headers:{'Content-Type':'application/json',...headers},body:JSON.stringify(body||{})});if(!r.ok)throw Error(`演示模拟器 HTTP ${r.status}`);return r.json()}
const login=await req('/api/v1/auth/login',{username:env.IOT_ADMIN_USER||'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant})
const {credential}=await req(`/api/v1/device-registry/${device}/credentials`,{}, {Authorization:`Bearer ${login.accessToken}`})
const mqtt=createRequire(new URL('../../iot_front/package.json',import.meta.url))('mqtt')
let client,stopping=false,target=27
const topic=`/iot/up/${tenant}/${product}/${device}`
async function connect(){
 const auth=await req('/api/v1/device-mqtt/token',{}, {'X-Device-Key':credential.accessKey,'X-Device-Secret':credential.secret})
 if(client)await client.endAsync()
 client=mqtt.connect((env.IOT_DEVICE_MQTT_PUBLIC_URL||env.IOT_MQTT_BROKER).replace(/^tcp:/,'mqtt:'),{clientId:'demo-20260914-live',username:auth.username,password:auth.token,reconnectPeriod:2000})
 client.on('error',()=>{})
 client.on('connect',async()=>{await client.subscribeAsync(`/iot/down/${tenant}/${product}/${device}/command`,{qos:1});await send('state',{connectionStatus:'CONNECTED'})})
 client.on('message',async(_,bytes)=>{try{const cmd=JSON.parse(bytes);target=Number(cmd.data?.value??cmd.params?.value??target);await send('command-reply',{commandId:cmd.id,success:true,result:`演示目标温度已设置为 ${target}℃`});await send('property',{temperature:target,humidity:51,demoGroup:'demo-20260914'})}catch{console.log('演示命令处理失败')}})
}
async function send(kind,data){if(client?.connected)await client.publishAsync(`${topic}/${kind}`,JSON.stringify({id:`demo-live-${kind}-${Date.now()}`,timestamp:Date.now(),data}),{qos:1})}
await connect()
const telemetry=setInterval(()=>send('property',{temperature:target,humidity:51,demoGroup:'demo-20260914'}).catch(()=>{}),10000)
const renewal=setInterval(()=>{if(!stopping)connect().catch(()=>console.log('演示令牌刷新失败'))},180000)
async function stop(){if(stopping)return;stopping=true;clearInterval(telemetry);clearInterval(renewal);await client?.endAsync()}
setTimeout(stop,120*60000);process.on('SIGINT',stop);process.on('SIGTERM',stop)
console.log('演示 MQTT 温控器已启动，每10秒上报，支持设置目标温度；两小时后自动停止。')
