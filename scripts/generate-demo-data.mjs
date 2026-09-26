// One-command orchestration of existing demo stages. No production writes unless run.
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve, join } from 'node:path'
import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { demoConfig, redactDemo, demoGatewaysReady } from './lib/demo-config.mjs'
import { writeDemoSamples } from './lib/demo-samples.mjs'

const stages=['inspect','business','protocols','mqtt','children','services','refine','access']
const names={inspect:'读取功能入口',business:'HTTP设备、属性、告警、规则、回放、摄像头',protocols:'JSON/CSV/Go协议、产品与网关',mqtt:'MQTT模拟设备和控制回执',children:'主设备与子设备',services:'知识、AI问答、巡检、备份',refine:'Excel、TCP/UDP、接入测试和模型测试',access:'用户、角色和设备告警权限'}
const args=process.argv.slice(2), overrides={}
let dry=false, selected=stages
const flags={'--origin':'IOT_DEMO_ORIGIN','--env-file':'IOT_DEMO_ENV_FILE','--tenant':'IOT_DEMO_TENANT','--prefix':'IOT_DEMO_PREFIX','--output':'IOT_DEMO_OUTPUT','--mqtt-url':'IOT_DEMO_MQTT_URL','--device-host':'IOT_DEMO_DEVICE_HOST','--tcp-port':'IOT_DEMO_TCP_PORT','--udp-port':'IOT_DEMO_UDP_PORT'}
if(args.includes('--help')) {
  console.log(`用法：node scripts/generate-demo-data.mjs --origin http://服务器:8080 [--env-file .env.offline] [--tenant tenant_001]
可选：--prefix demo-名称 --output 结果目录 --mqtt-url mqtt://服务器:1883
      --device-host 服务器 --tcp-port 29075 --udp-port 29076
      --skip-ai --skip-backup --skip-mqtt --skip-sockets --stages ${stages.join(',')} --dry-run
凭据：IOT_ADMIN_USER / IOT_ADMIN_PASSWORD；演示用户可另设 IOT_DEMO_USER_PASSWORD。
默认创建唯一演示前缀；产生真实演示及内置测试数据、模型请求、备份任务，不执行数据库恢复。
TCP/UDP 端口必须已映射且空闲；MQTT 需要 iot_front/node_modules/mqtt。
--dry-run 仅显示计划，不连接服务器、不生成数据。`)
  process.exit(0)
}
for(let i=0;i<args.length;i++) {
  const arg=args[i]
  if(arg==='--dry-run')dry=true
  else if(['--skip-ai','--skip-backup','--skip-mqtt','--skip-sockets'].includes(arg))overrides['IOT_DEMO_'+arg.slice(2).replaceAll('-','_').toUpperCase()]='1'
  else if(arg==='--stages'){selected=(args[++i]||'').split(',');if(selected.some(stage=>!stages.includes(stage)) || new Set(selected).size!==selected.length)throw Error('未知或重复运行阶段')}
  else if(flags[arg]) {if(!args[i+1] || args[i+1].startsWith('--'))throw Error('参数缺少值：'+arg);overrides[flags[arg]]=args[++i]}
  else throw Error('未知参数：'+arg)
}
const root=fileURLToPath(new URL('../',import.meta.url))
if(overrides.IOT_DEMO_ENV_FILE)overrides.IOT_DEMO_ENV_FILE=resolve(overrides.IOT_DEMO_ENV_FILE)
if(overrides.IOT_DEMO_OUTPUT)overrides.IOT_DEMO_OUTPUT=resolve(overrides.IOT_DEMO_OUTPUT)
process.chdir(root)
const config=await demoConfig({...process.env,...overrides,IOT_DEMO_PREFIX:overrides.IOT_DEMO_PREFIX || process.env.IOT_DEMO_PREFIX || `demo-${new Date().toISOString().slice(0,10).replaceAll('-','')}-${randomBytes(3).toString('hex')}`})
const {env,origin,prefix,tenant,dir,host,tcpPort,udpPort}=config
console.log(`目标：${origin}；租户：${tenant}；前缀：${prefix}\n结果目录：${dir}\nTCP ${host}:${tcpPort}；UDP ${host}:${udpPort}`)
for(const stage of selected)console.log(`${stage}：${names[stage]}${stage==='mqtt'&&env.IOT_DEMO_SKIP_MQTT==='1'?'（跳过）':''}`)
if(dry)process.exit(0)
if(!env.IOT_ADMIN_PASSWORD)throw Error('缺少 IOT_ADMIN_PASSWORD，请通过环境变量或 --env-file 提供，不要放在命令行参数里')
// Authenticate before starting simulators or mutating anything.
const login=await fetch(origin+'/api/v1/auth/login',{method:'POST',redirect:'error',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:env.IOT_ADMIN_USER||'admin',password:env.IOT_ADMIN_PASSWORD,tenantId:tenant}),signal:AbortSignal.timeout(15000)})
if(!login.ok)throw Error(`登录失败 HTTP ${login.status}`)
const identity=await login.json()
if(identity.role!=='admin' || identity.tenantId!==tenant)throw Error('需要所选租户的内置管理员账户')
await mkdir(dir,{recursive:true})
let start=0
try{const old=JSON.parse(await readFile(join(dir,'results.json'),'utf8'));if(old.origin!==origin||old.tenant!==tenant||old.prefix!==prefix)throw Error('输出目录已有其他运行数据，请换目录');start=old.results.length}catch(e){if(e.code!=='ENOENT')throw e}
await writeDemoSamples(dir)
const childEnv={...env,IOT_DEMO_ORIGIN:origin,IOT_DEMO_PREFIX:prefix,IOT_DEMO_TENANT:tenant,IOT_DEMO_OUTPUT:dir,IOT_DEMO_DEVICE_HOST:host,IOT_DEMO_TCP_PORT:String(tcpPort),IOT_DEMO_UDP_PORT:String(udpPort),IOT_DEMO_MINUTES:'30'}
let simulator,interrupted=false
const exits=[]
function launch(file,args=[]) {const child=spawn(process.execPath,[join(root,file),...args],{cwd:root,env:childEnv,windowsHide:true,stdio:['ignore','pipe','pipe']});for(const stream of [child.stdout,child.stderr])stream.on('data',chunk=>process.stdout.write(redactDemo(chunk.toString(),env)));return child}
const wait=child=>new Promise(done=>{child.once('error',()=>done(1));child.once('exit',code=>done(code??1))})
let active
const stop=()=>{interrupted=true;active?.kill('SIGTERM');simulator?.kill('SIGTERM')}
process.on('SIGINT',stop);process.on('SIGTERM',stop)
try {
  for(const stage of selected) {
    if(interrupted)break
    if(stage==='mqtt'&&env.IOT_DEMO_SKIP_MQTT==='1'){exits.push({stage,status:'跳过',detail:'--skip-mqtt'});continue}
    if(stage==='refine' && env.IOT_DEMO_SKIP_SOCKETS!=='1') {
      const state=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>'{}'))
      let ready=false
      for(let attempt=0;attempt<20&&!interrupted;attempt++) {
        try {
          const response=await fetch(origin+'/api/v2/device-access-profiles',{headers:{Authorization:`Bearer ${identity.accessToken}`},redirect:'error',signal:AbortSignal.timeout(5000)})
          if(response.ok && demoGatewaysReady((await response.json()).items||[],config,state.ids||{})){ready=true;break}
        } catch {}
        await new Promise(done=>setTimeout(done,500))
      }
      if(ready){simulator=launch('scripts/tests/demo-devices.mjs');simulator.done=wait(simulator)}
      else {exits.push({stage:'simulator',status:'未通过',detail:'未确认本次演示协议的 TCP/UDP 网关正在监听，不发送套接字测试流量'});childEnv.IOT_DEMO_SKIP_SOCKETS='1'}
    }
    active=launch('scripts/tests/demo-platform.mjs',[stage])
    const timeout=setTimeout(()=>active?.kill('SIGTERM'),30*60*1000)
    const code=await wait(active);clearTimeout(timeout);active=null
    const current=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>'{}'))
    const skipped=(current.results||[]).slice(start).some(item=>item.stage===stage&&item.status==='跳过')
    exits.push({stage,status:code!==0?'未通过':skipped?'部分跳过':'通过',detail:code===0?'阶段完成；详见功能检查':`阶段退出码 ${code}`})
    if(stage==='refine'&&simulator){simulator.kill('SIGTERM');await simulator.done;simulator=null}
  }
} finally {
  simulator?.kill('SIGTERM');if(simulator?.done)await simulator.done
  const state=JSON.parse(await readFile(join(dir,'results.json'),'utf8').catch(()=>JSON.stringify({ids:{},results:[]})))
  const results=state.results.slice(start)
  const report={origin,tenant,prefix,createdAt:new Date().toISOString(),interrupted,stages:exits,results,ids:state.ids,limits:['未执行数据库恢复、真实摄像头视频或真实物理设备控制','模拟器在脚本结束时停止；历史数据保留，在线状态可能随时间变化','AI和备份为真实服务调用；未通过和跳过项不计作验收通过','接口数据验证不等同于每个页面按钮的浏览器验收']}
  await writeFile(join(dir,'report.json'),JSON.stringify(report,null,2))
  const esc=value=>String(value??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('"','&quot;')
  await writeFile(join(dir,'report.html'),`<!doctype html><html lang="zh-CN"><meta charset="UTF-8"><title>平台演示数据运行报告</title><style>body{font:16px/1.6 system-ui;max-width:1100px;margin:30px auto;padding:20px}table{width:100%;border-collapse:collapse}td,th{border:1px solid #ddd;padding:8px;text-align:left}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><h1>平台演示数据运行报告</h1><p>${esc(origin)} · ${esc(tenant)} · ${esc(prefix)}</p><h2>阶段结果</h2><table>${exits.map(x=>`<tr><td>${esc(x.stage)}</td><td>${esc(x.status)}</td><td>${esc(x.detail)}</td></tr>`).join('')}</table><h2>功能检查</h2><table><tr><th>功能</th><th>结果</th><th>说明</th></tr>${results.map(x=>`<tr><td>${esc(x.name)}</td><td>${esc(x.status)}</td><td>${esc(x.detail)}</td></tr>`).join('')}</table><h2>生成的资源</h2><pre>${esc(JSON.stringify(state.ids,null,2))}</pre><h2>范围</h2><ul>${report.limits.map(x=>`<li>${esc(x)}</li>`).join('')}</ul>`)
  console.log(`报告：${join(dir,'report.html')}`)
  if(interrupted||exits.some(x=>x.status==='未通过')||results.some(x=>x.status==='未通过'))process.exitCode=1
}
