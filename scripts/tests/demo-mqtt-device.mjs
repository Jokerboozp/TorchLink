// Keeps the selected demo controllable for a configurable duration. Credentials are never written to disk.
import {demoConfig} from '../lib/demo-config.mjs' /* 引入当前代码需要的依赖。 */
import {createRequire} from 'node:module' /* 引入当前代码需要的依赖。 */
const {env,origin,tenant,prefix}=await demoConfig() /* 执行当前语句并推进处理流程。 */
const device=`${prefix}-mqtt-sensor`,product=`${prefix}-mqtt-product` /* 声明 device。 */
async function req(path,body,headers={}){const r=await fetch(origin+path,{method:'POST',redirect:'error',headers:{'Content-Type':'application/json',...headers},body:JSON.stringify(body||{}),signal:AbortSignal.timeout(15000)});if(!r.ok)throw Error(`演示模拟器 HTTP ${r.status}`);return r.json()} /* 定义 req 函数。 */
const login=await req('/api/v1/auth/login',{username:env.IOT_ADMIN_USER||'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant}) /* 声明 login。 */
const {credential}=await req(`/api/v1/device-registry/${device}/credentials`,{}, {Authorization:`Bearer ${login.accessToken}`}) /* 执行当前语句并推进处理流程。 */
const mqtt=createRequire(new URL('../../iot_front/package.json',import.meta.url))('mqtt') /* 声明 mqtt。 */
let client,stopping=false,target=27 /* 声明 client。 */
const topic=`/iot/up/${tenant}/${product}/${device}` /* 声明 topic。 */
async function connect(){ /* 定义 connect 函数。 */
 const auth=await req('/api/v1/device-mqtt/token',{}, {'X-Device-Key':credential.accessKey,'X-Device-Secret':credential.secret}) /* 声明 auth。 */
 if(client)await client.endAsync() /* 判断条件并选择处理分支。 */
 client=mqtt.connect((env.IOT_DEMO_MQTT_URL||`mqtt://${new URL(origin).hostname}:1883`).replace(/^tcp:/,'mqtt:'),{clientId:`${prefix}-live`,username:auth.username,password:auth.token,reconnectPeriod:2000}) /* 更新 client 的值。 */
 client.on('error',()=>{}) /* 执行当前语句并推进处理流程。 */
 client.on('connect',async()=>{await client.subscribeAsync(`/iot/down/${tenant}/${product}/${device}/command`,{qos:1});await send('state',{connectionStatus:'CONNECTED'})}) /* 执行当前语句并推进处理流程。 */
 client.on('message',async(_,bytes)=>{try{const cmd=JSON.parse(bytes);target=Number(cmd.data?.value??cmd.params?.value??target);await send('command-reply',{commandId:cmd.id,success:true,result:`演示目标温度已设置为 ${target}℃`});await send('property',{temperature:target,humidity:51,demoGroup:prefix})}catch{console.log('演示命令处理失败')}}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function send(kind,data){if(client?.connected)await client.publishAsync(`${topic}/${kind}`,JSON.stringify({id:`demo-live-${kind}-${Date.now()}`,timestamp:Date.now(),data}),{qos:1})} /* 定义 send 函数。 */
await connect() /* 等待异步操作完成。 */
const telemetry=setInterval(()=>send('property',{temperature:target,humidity:51,demoGroup:prefix}).catch(()=>{}),10000) /* 声明 telemetry。 */
const renewal=setInterval(()=>{if(!stopping)connect().catch(()=>console.log('演示令牌刷新失败'))},180000) /* 声明 renewal。 */
async function stop(){if(stopping)return;stopping=true;clearTimeout(endTimer);clearInterval(telemetry);clearInterval(renewal);await client?.endAsync()} /* 定义 stop 函数。 */
const endTimer=setTimeout(stop,Number(env.IOT_DEMO_MINUTES||120)*60000);process.on('SIGINT',stop);process.on('SIGTERM',stop) /* 声明 endTimer。 */
console.log(`演示 MQTT 温控器已启动，每10秒上报，支持设置目标温度；${Number(env.IOT_DEMO_MINUTES||120)}分钟后自动停止。`) /* 执行当前语句并推进处理流程。 */
