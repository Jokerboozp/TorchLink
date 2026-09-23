// One-command orchestration of existing demo stages. No production writes unless run.
import { readFile, writeFile, mkdir } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { fileURLToPath } from 'node:url' /* 引入当前代码需要的依赖。 */
import { resolve, join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { randomBytes } from 'node:crypto' /* 引入当前代码需要的依赖。 */
import { demoConfig, redactDemo, demoGatewaysReady } from './lib/demo-config.mjs' /* 引入当前代码需要的依赖。 */
import { writeDemoSamples } from './lib/demo-samples.mjs' /* 引入当前代码需要的依赖。 */

const stages=['inspect','business','protocols','mqtt','children','services','refine','access'] /* 声明 stages。 */
const names={inspect:'读取功能入口',business:'HTTP设备、属性、告警、规则、回放、摄像头',protocols:'JSON/CSV/Go协议、产品与网关',mqtt:'MQTT模拟设备和控制回执',children:'主设备与子设备',services:'知识、AI问答、巡检、备份',refine:'Excel、TCP/UDP、接入测试和模型测试',access:'用户、角色和设备告警权限'} /* 声明 names。 */
const args=process.argv.slice(2), overrides={} /* 声明 args。 */
let dry=false, selected=stages /* 声明 dry。 */
const flags={'--origin':'IOT_DEMO_ORIGIN','--env-file':'IOT_DEMO_ENV_FILE','--tenant':'IOT_DEMO_TENANT','--prefix':'IOT_DEMO_PREFIX','--output':'IOT_DEMO_OUTPUT','--mqtt-url':'IOT_DEMO_MQTT_URL','--device-host':'IOT_DEMO_DEVICE_HOST','--tcp-port':'IOT_DEMO_TCP_PORT','--udp-port':'IOT_DEMO_UDP_PORT'} /* 声明 flags。 */
if(args.includes('--help')) { /* 判断条件并选择处理分支。 */
  console.log(`用法：node scripts/generate-demo-data.mjs --origin http://服务器:8080 [--env-file .env.offline] [--tenant tenant_001]
可选：--prefix demo-名称 --output 结果目录 --mqtt-url mqtt://服务器:1883
      --device-host 服务器 --tcp-port 29075 --udp-port 29076
      --skip-ai --skip-backup --skip-mqtt --skip-sockets --stages ${stages.join(',')} --dry-run
凭据：IOT_ADMIN_USER / IOT_ADMIN_PASSWORD；演示用户可另设 IOT_DEMO_USER_PASSWORD。
默认创建唯一演示前缀；产生真实演示及内置测试数据、模型请求、备份任务，不执行数据库恢复。
TCP/UDP 端口必须已映射且空闲；MQTT 需要 iot_front/node_modules/mqtt。
--dry-run 仅显示计划，不连接服务器、不生成数据。`)
  process.exit(0) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
for(let i=0;i<args.length;i++) { /* 循环处理当前数据。 */
  const arg=args[i] /* 声明 arg。 */
  if(arg==='--dry-run')dry=true /* 判断条件并选择处理分支。 */
  else if(['--skip-ai','--skip-backup','--skip-mqtt','--skip-sockets'].includes(arg))overrides['IOT_DEMO_'+arg.slice(2).replaceAll('-','_').toUpperCase()]='1' /* 判断条件并选择处理分支。 */
  else if(arg==='--stages'){selected=(args[++i]||'').split(',');if(selected.some(stage=>!stages.includes(stage)) || new Set(selected).size!==selected.length)throw Error('未知或重复运行阶段')} /* 判断条件并选择处理分支。 */
  else if(flags[arg]) {if(!args[i+1] || args[i+1].startsWith('--'))throw Error('参数缺少值：'+arg);overrides[flags[arg]]=args[++i]} /* 判断条件并选择处理分支。 */
  else throw Error('未知参数：'+arg) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
const root=fileURLToPath(new URL('../',import.meta.url)) /* 声明 root。 */
if(overrides.IOT_DEMO_ENV_FILE)overrides.IOT_DEMO_ENV_FILE=resolve(overrides.IOT_DEMO_ENV_FILE) /* 判断条件并选择处理分支。 */
if(overrides.IOT_DEMO_OUTPUT)overrides.IOT_DEMO_OUTPUT=resolve(overrides.IOT_DEMO_OUTPUT) /* 判断条件并选择处理分支。 */
process.chdir(root) /* 执行当前语句并推进处理流程。 */
const config=await demoConfig({...process.env,...overrides,IOT_DEMO_PREFIX:overrides.IOT_DEMO_PREFIX || process.env.IOT_DEMO_PREFIX || `demo-${new Date().toISOString().slice(0,10).replaceAll('-','')}-${randomBytes(3).toString('hex')}`}) /* 声明 config。 */
const {env,origin,prefix,tenant,dir,host,tcpPort,udpPort}=config /* 执行当前语句并推进处理流程。 */
console.log(`目标：${origin}；租户：${tenant}；前缀：${prefix}\n结果目录：${dir}\nTCP ${host}:${tcpPort}；UDP ${host}:${udpPort}`) /* 执行当前语句并推进处理流程。 */
for(const stage of selected)console.log(`${stage}：${names[stage]}${stage==='mqtt'&&env.IOT_DEMO_SKIP_MQTT==='1'?'（跳过）':''}`) /* 循环处理当前数据。 */
if(dry)process.exit(0) /* 判断条件并选择处理分支。 */
if(!env.IOT_ADMIN_PASSWORD)throw Error('缺少 IOT_ADMIN_PASSWORD，请通过环境变量或 --env-file 提供，不要放在命令行参数里') /* 判断条件并选择处理分支。 */
// Authenticate before starting simulators or mutating anything.
const login=await fetch(origin+'/api/v1/auth/login',{method:'POST',redirect:'error',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:env.IOT_ADMIN_USER||'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant}),signal:AbortSignal.timeout(15000)}) /* 声明 login。 */
if(!login.ok)throw Error(`登录失败 HTTP ${login.status}`) /* 判断条件并选择处理分支。 */
const identity=await login.json() /* 声明 identity。 */
if(identity.role!=='admin' || identity.tenantId!==tenant)throw Error('需要所选租户的内置管理员账户') /* 判断条件并选择处理分支。 */
await mkdir(dir,{recursive:true}) /* 等待异步操作完成。 */
let start=0 /* 声明 start。 */
try{const old=JSON.parse(await readFile(join(dir,'results.json'),'utf8'));if(old.origin!==origin||old.tenant!==tenant||old.prefix!==prefix)throw Error('输出目录已有其他运行数据，请换目录');start=old.results.length}catch(e){if(e.code!=='ENOENT')throw e} /* 执行当前语句并推进处理流程。 */
await writeDemoSamples(dir) /* 等待异步操作完成。 */
const childEnv={...env,IOT_DEMO_ORIGIN:origin,IOT_DEMO_PREFIX:prefix,IOT_DEMO_TENANT:tenant,IOT_DEMO_OUTPUT:dir,IOT_DEMO_DEVICE_HOST:host,IOT_DEMO_TCP_PORT:String(tcpPort),IOT_DEMO_UDP_PORT:String(udpPort),IOT_DEMO_MINUTES:'30'} /* 声明 childEnv。 */
let simulator,interrupted=false /* 声明 simulator。 */
const exits=[] /* 声明 exits。 */
function launch(file,args=[]) {const child=spawn(process.execPath,[join(root,file),...args],{cwd:root,env:childEnv,windowsHide:true,stdio:['ignore','pipe','pipe']});for(const stream of [child.stdout,child.stderr])stream.on('data',chunk=>process.stdout.write(redactDemo(chunk.toString(),env)));return child} /* 定义 launch 函数。 */
const wait=child=>new Promise(done=>{child.once('error',()=>done(1));child.once('exit',code=>done(code??1))}) /* 声明 wait。 */
let active /* 声明 active。 */
const stop=()=>{interrupted=true;active?.kill('SIGTERM');simulator?.kill('SIGTERM')} /* 声明 stop。 */
process.on('SIGINT',stop);process.on('SIGTERM',stop) /* 执行当前语句并推进处理流程。 */
try { /* 执行当前语句并推进处理流程。 */
  for(const stage of selected) { /* 循环处理当前数据。 */
    if(interrupted)break /* 判断条件并选择处理分支。 */
    if(stage==='mqtt'&&env.IOT_DEMO_SKIP_MQTT==='1'){exits.push({stage,status:'跳过',detail:'--skip-mqtt'});continue} /* 判断条件并选择处理分支。 */
    if(stage==='refine' && env.IOT_DEMO_SKIP_SOCKETS!=='1') { /* 判断条件并选择处理分支。 */
      const state=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>'{}')) /* 声明 state。 */
      let ready=false /* 声明 ready。 */
      for(let attempt=0;attempt<20&&!interrupted;attempt++) { /* 循环处理当前数据。 */
        try { /* 执行当前语句并推进处理流程。 */
          const response=await fetch(origin+'/api/v2/device-access-profiles',{headers:{Authorization:`Bearer ${identity.accessToken}`},redirect:'error',signal:AbortSignal.timeout(5000)}) /* 声明 response。 */
          if(response.ok && demoGatewaysReady((await response.json()).items||[],config,state.ids||{})){ready=true;break} /* 判断条件并选择处理分支。 */
        } catch {} /* 结束当前表达式或代码块。 */
        await new Promise(done=>setTimeout(done,500)) /* 等待异步操作完成。 */
      } /* 结束当前表达式或代码块。 */
      if(ready){simulator=launch('scripts/tests/demo-devices.mjs');simulator.done=wait(simulator)} /* 判断条件并选择处理分支。 */
      else {exits.push({stage:'simulator',status:'未通过',detail:'未确认本次演示协议的 TCP/UDP 网关正在监听，不发送套接字测试流量'});childEnv.IOT_DEMO_SKIP_SOCKETS='1'} /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    active=launch('scripts/tests/demo-platform.mjs',[stage]) /* 更新 active 的值。 */
    const timeout=setTimeout(()=>active?.kill('SIGTERM'),30*60*1000) /* 声明 timeout。 */
    const code=await wait(active);clearTimeout(timeout);active=null /* 声明 code。 */
    const current=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>'{}')) /* 声明 current。 */
    const skipped=(current.results||[]).slice(start).some(item=>item.stage===stage&&item.status==='跳过') /* 声明 skipped。 */
    exits.push({stage,status:code!==0?'未通过':skipped?'部分跳过':'通过',detail:code===0?'阶段完成；详见功能检查':`阶段退出码 ${code}`}) /* 执行当前语句并推进处理流程。 */
    if(stage==='refine'&&simulator){simulator.kill('SIGTERM');await simulator.done;simulator=null} /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} finally { /* 结束当前表达式或代码块。 */
  simulator?.kill('SIGTERM');if(simulator?.done)await simulator.done /* 执行当前语句并推进处理流程。 */
  const state=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>JSON.stringify({ids:{},results:[]}))) /* 声明 state。 */
  const results=state.results.slice(start) /* 声明 results。 */
  const report={origin,tenant,prefix,createdAt:new Date().toISOString(),interrupted,stages:exits,results,ids:state.ids,limits:['未执行数据库恢复、真实摄像头视频或真实物理设备控制','模拟器在脚本结束时停止；历史数据保留，在线状态可能随时间变化','AI和备份为真实服务调用；未通过和跳过项不计作验收通过','接口数据验证不等同于每个页面按钮的浏览器验收']} /* 声明 report。 */
  await writeFile(join(dir,'report.json'),JSON.stringify(report,null,2)) /* 等待异步操作完成。 */
  const esc=value=>String(value??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;') /* 声明 esc。 */
  await writeFile(join(dir,'report.html'),`<!doctype html><html lang="zh-CN"><meta charset="UTF-8"><title>平台演示数据运行报告</title><style>body{font:16px/1.6 system-ui;max-width:1100px;margin:30px auto;padding:20px}table{width:100%;border-collapse:collapse}td,th{border:1px solid #ddd;padding:8px;text-align:left}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><h1>平台演示数据运行报告</h1><p>${esc(origin)} · ${esc(tenant)} · ${esc(prefix)}</p><h2>阶段结果</h2><table>${exits.map(x=>`<tr><td>${esc(x.stage)}</td><td>${esc(x.status)}</td><td>${esc(x.detail)}</td></tr>`).join('')}</table><h2>功能检查</h2><table><tr><th>功能</th><th>结果</th><th>说明</th></tr>${results.map(x=>`<tr><td>${esc(x.name)}</td><td>${esc(x.status)}</td><td>${esc(x.detail)}</td></tr>`).join('')}</table><h2>生成的资源</h2><pre>${esc(JSON.stringify(state.ids,null,2))}</pre><h2>范围</h2><ul>${report.limits.map(x=>`<li>${esc(x)}</li>`).join('')}</ul>`) /* 等待异步操作完成。 */
  console.log(`报告：${join(dir,'report.html')}`) /* 执行当前语句并推进处理流程。 */
  if(interrupted||exits.some(x=>x.status==='未通过')||results.some(x=>x.status==='未通过'))process.exitCode=1 /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */
