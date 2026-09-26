import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn, spawnSync } from 'node:child_process'
import { createServer } from 'node:net'
import { createServer as createHTTPServer } from 'node:http'
import { randomUUID } from 'node:crypto'
import { demoConfig, redactDemo, demoGatewaysReady } from '../lib/demo-config.mjs'
import { writeDemoSamples } from '../lib/demo-samples.mjs'

test('演示配置校验地址、租户、前缀与端口并隐藏凭据',async()=>{
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-config-')),file=join(dir,'env')
 await writeFile(file,'IOT_ADMIN_PASSWORD=fixture-secret-password\nIOT_ADMIN_TENANTS=tenant_test\n')
 const c=await demoConfig({IOT_DEMO_ENV_FILE:file,IOT_DEMO_ORIGIN:'http://192.0.2.10:8080',IOT_DEMO_PREFIX:'demo-check',IOT_DEMO_TCP_PORT:'26875'})
 assert.equal(c.host,'192.0.2.10');assert.equal(c.tenant,'tenant_test');assert.equal(c.tcpPort,26875)
 assert.equal(redactDemo('fixture-secret-password',c.env),'<REDACTED>')
 for(const extra of [{IOT_DEMO_ORIGIN:'http://user:pass@example.com'},{IOT_DEMO_ORIGIN:'http://example.com/api'},{IOT_DEMO_PREFIX:'../real'},{IOT_DEMO_TCP_PORT:'0'}])await assert.rejects(demoConfig({IOT_DEMO_ENV_FILE:file,...extra}))
 await writeDemoSamples(dir)
 assert.ok((await readFile(join(dir,'演示点表.xlsx'))).subarray(0,2).equals(Buffer.from('PK')))
})

test('dry-run无需密码、不连接目标，也不创建数据目录',()=>{
 const env={...process.env,IOT_ADMIN_PASSWORD:''}
 const run=spawnSync(process.execPath,['scripts/generate-demo-data.mjs','--origin','http://127.0.0.1:1','--dry-run'],{encoding:'utf8',env,windowsHide:true})
 assert.equal(run.status,0,run.stderr);assert.match(run.stdout,/access：/);assert.match(run.stdout,/refine：/)
 const bad=spawnSync(process.execPath,['scripts/generate-demo-data.mjs','--stages','typo','--dry-run'],{encoding:'utf8',env,windowsHide:true})
 assert.notEqual(bad.status,0)
})

test('模拟器只连接本次演示协议且已监听的网关',()=>{
 const config={prefix:'demo-check',tcpPort:29075,udpPort:29076},ids={tcpProduct:'demo-product',tcpProtocol:{id:'demo-protocol',version:'v1'}}
 const profiles=['tcp','udp'].map((network,i)=>({id:`demo-check-${network}-gateway`,productId:ids.tcpProduct,protocolId:ids.tcpProtocol.id,protocolVersion:'v1',mode:'listener',network,port:29075+i,enabled:true,runtimeStatus:'LISTENING'}))
 assert.ok(demoGatewaysReady(profiles,config,ids))
 for(const change of [{id:'real-gateway'},{productId:'real-product'},{protocolId:'real-protocol'},{protocolVersion:'v2'},{runtimeStatus:'ERROR'},{port:1883},{enabled:false}]) assert.equal(demoGatewaysReady([{...profiles[0],...change},profiles[1]],config,ids),false)
})

const exe=process.env.IOT_DEMO_TEST_API_EXE
test('阶段接口失败会生成失败报告并返回非零，不泄露响应中的凭据',async()=>{
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-failure-')),password=randomUUID(),envFile=join(dir,'env')
 await writeFile(envFile,`IOT_ADMIN_PASSWORD=${password}\nIOT_ADMIN_TENANTS=tenant_failure\n`)
 const server=createHTTPServer((req,res)=>{res.setHeader('Content-Type','application/json');if(req.url==='/api/v1/auth/login')res.end(JSON.stringify({accessToken:'fixture-token',role:'admin',tenantId:'tenant_failure'}));else {res.statusCode=503;res.end(JSON.stringify({detail:password}))}})
 await new Promise(done=>server.listen(0,'127.0.0.1',done))
 try {
  const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith('IOT_')))
  const child=spawn(process.execPath,['scripts/generate-demo-data.mjs','--origin',`http://127.0.0.1:${server.address().port}`,'--env-file',envFile,'--prefix','demo-failure','--output',join(dir,'out'),'--stages','inspect'],{env,windowsHide:true,stdio:['ignore','pipe','pipe']})
  let output='';for(const stream of [child.stdout,child.stderr])stream.on('data',b=>{output+=b.toString()})
  const code=await new Promise(done=>child.once('exit',done))
  assert.equal(code,1);assert.ok(!output.includes(password))
  const report=JSON.parse(await readFile(join(dir,'out','report.json'),'utf8'))
  assert.ok(report.results.length>0);assert.ok(report.results.every(x=>x.status==='未通过'));assert.equal(report.stages[0].status,'未通过')
  assert.ok(!JSON.stringify(report).includes(password))
 } finally {server.closeAllConnections();await new Promise(done=>server.close(done))}
})

test('真实隔离 API 执行演示业务、协议、套接字、Excel与权限流程', {skip:!exe,timeout:240000}, async()=>{
 const port=async()=>{const s=createServer();await new Promise(done=>s.listen(0,'127.0.0.1',done));const p=s.address().port;await new Promise(done=>s.close(done));return p}
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-api-')),httpPort=await port(),tcpPort=await port(),udpPort=await port()
 const password=randomUUID(),envFile=join(dir,'test.env')
 await writeFile(envFile,`IOT_DEV_MODE=true\nIOT_HTTP_ADDR=127.0.0.1:${httpPort}\nIOT_DATA_DIR=${dir.replaceAll('\\','/')}\nIOT_ADMIN_USER=admin\nIOT_ADMIN_PASSWORD=${password}\nIOT_ADMIN_TENANTS=tenant_demo_test\nIOT_JWT_SECRET=${randomUUID()}\n`)
 const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith('IOT_')))
 const api=spawn(exe,['--env-file',envFile],{env,windowsHide:true,stdio:['ignore','pipe','pipe']})
 let apiLog='';for(const stream of [api.stdout,api.stderr])stream.on('data',b=>{apiLog=(apiLog+b.toString()).slice(-6000)})
 const stopped=new Promise(done=>api.once('exit',done))
 try {
  let ready=false
  for(let i=0;i<100;i++){try{const r=await fetch(`http://127.0.0.1:${httpPort}/health/live`);if(r.ok){ready=true;break}}catch{}await new Promise(done=>setTimeout(done,100))}
  assert.ok(ready,'isolated API failed to start')
  const child=spawn(process.execPath,['scripts/generate-demo-data.mjs','--origin',`http://127.0.0.1:${httpPort}`,'--env-file',envFile,'--prefix','demo-integration','--output',join(dir,'report'),'--tcp-port',String(tcpPort),'--udp-port',String(udpPort),'--stages','business,protocols,children,services,refine,access','--skip-ai','--skip-backup'],{env,windowsHide:true,stdio:['ignore','pipe','pipe']})
  let output='';for(const stream of [child.stdout,child.stderr])stream.on('data',b=>{output+=b.toString()})
  const code=await new Promise(done=>child.once('exit',done))
  const report=JSON.parse(await readFile(join(dir,'report','report.json'),'utf8'))
  assert.equal(code,0,output.replaceAll(password,'<REDACTED>'))
  assert.ok(report.results.some(x=>x.name.includes('Excel')&&x.status==='通过'))
  assert.ok(report.results.some(x=>x.name.includes('权限')||x.name.includes('范围验证')))
  assert.ok(report.results.some(x=>x.name.includes('TCP/UDP 实际')&&x.status==='通过'))
  assert.ok(report.results.some(x=>x.status==='跳过'))
  assert.ok(!JSON.stringify(report).includes(password))
  console.log(`隔离运行报告：${join(dir,'report','report.html')}`)
 } finally {api.kill('SIGTERM');await stopped}
})
