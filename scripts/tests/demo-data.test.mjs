import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { mkdtemp, writeFile, readFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { spawn, spawnSync } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:net' /* 引入当前代码需要的依赖。 */
import { createServer as createHTTPServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { randomUUID } from 'node:crypto' /* 引入当前代码需要的依赖。 */
import { demoConfig, redactDemo, demoGatewaysReady } from '../lib/demo-config.mjs' /* 引入当前代码需要的依赖。 */
import { writeDemoSamples } from '../lib/demo-samples.mjs' /* 引入当前代码需要的依赖。 */

test('演示配置校验地址、租户、前缀与端口并隐藏凭据',async()=>{ /* 执行当前语句并推进处理流程。 */
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-config-')),file=join(dir,'env') /* 声明 dir。 */
 await writeFile(file,'IOT_ADMIN_PASSWORD=fixture-secret-password\nIOT_ADMIN_TENANTS=tenant_test\n') /* 等待异步操作完成。 */
 const c=await demoConfig({IOT_DEMO_ENV_FILE:file,IOT_DEMO_ORIGIN:'http://192.0.2.10:8080',IOT_DEMO_PREFIX:'demo-check',IOT_DEMO_TCP_PORT:'26875'}) /* 声明 c。 */
 assert.equal(c.host,'192.0.2.10');assert.equal(c.tenant,'tenant_test');assert.equal(c.tcpPort,26875) /* 验证实际结果符合预期。 */
 assert.equal(redactDemo('fixture-secret-password',c.env),'<REDACTED>') /* 验证实际结果符合预期。 */
 for(const extra of [{IOT_DEMO_ORIGIN:'http://user:pass@example.com'},{IOT_DEMO_ORIGIN:'http://example.com/api'},{IOT_DEMO_PREFIX:'../real'},{IOT_DEMO_TCP_PORT:'0'}])await assert.rejects(demoConfig({IOT_DEMO_ENV_FILE:file,...extra})) /* 循环处理当前数据。 */
 await writeDemoSamples(dir) /* 等待异步操作完成。 */
 assert.ok((await readFile(join(dir,'演示点表.xlsx'))).subarray(0,2).equals(Buffer.from('PK'))) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('dry-run无需密码、不连接目标，也不创建数据目录',()=>{ /* 执行当前语句并推进处理流程。 */
 const env={...process.env,IOT_ADMIN_PASSWORD:''} /* 声明 env。 */
 const run=spawnSync(process.execPath,['scripts/generate-demo-data.mjs','--origin','http://127.0.0.1:1','--dry-run'],{encoding:'utf8',env,windowsHide:true}) /* 声明 run。 */
 assert.equal(run.status,0,run.stderr);assert.match(run.stdout,/access：/);assert.match(run.stdout,/refine：/) /* 验证实际结果符合预期。 */
 const bad=spawnSync(process.execPath,['scripts/generate-demo-data.mjs','--stages','typo','--dry-run'],{encoding:'utf8',env,windowsHide:true}) /* 声明 bad。 */
 assert.notEqual(bad.status,0) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('模拟器只连接本次演示协议且已监听的网关',()=>{ /* 执行当前语句并推进处理流程。 */
 const config={prefix:'demo-check',tcpPort:29075,udpPort:29076},ids={tcpProduct:'demo-product',tcpProtocol:{id:'demo-protocol',version:'v1'}} /* 声明 config。 */
 const profiles=['tcp','udp'].map((network,i)=>({id:`demo-check-${network}-gateway`,productId:ids.tcpProduct,protocolId:ids.tcpProtocol.id,protocolVersion:'v1',mode:'listener',network,port:29075+i,enabled:true,runtimeStatus:'LISTENING'})) /* 声明 profiles。 */
 assert.ok(demoGatewaysReady(profiles,config,ids)) /* 验证实际结果符合预期。 */
 for(const change of [{id:'real-gateway'},{productId:'real-product'},{protocolId:'real-protocol'},{protocolVersion:'v2'},{runtimeStatus:'ERROR'},{port:1883},{enabled:false}]) assert.equal(demoGatewaysReady([{...profiles[0],...change},profiles[1]],config,ids),false) /* 循环处理当前数据。 */
}) /* 结束当前表达式或代码块。 */

const exe=process.env.IOT_DEMO_TEST_API_EXE /* 声明 exe。 */
test('阶段接口失败会生成失败报告并返回非零，不泄露响应中的凭据',async()=>{ /* 执行当前语句并推进处理流程。 */
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-failure-')),password=randomUUID(),envFile=join(dir,'env') /* 声明 dir。 */
 await writeFile(envFile,`IOT_ADMIN_PASSWORD=${password}\nIOT_ADMIN_TENANTS=tenant_failure\n`) /* 等待异步操作完成。 */
 const server=createHTTPServer((req,res)=>{res.setHeader('Content-Type','application/json');if(req.url==='/api/v1/auth/login')res.end(JSON.stringify({accessToken:'fixture-token',role:'admin',tenantId:'tenant_failure'}));else {res.statusCode=503;res.end(JSON.stringify({detail:password}))}}) /* 声明 server。 */
 await new Promise(done=>server.listen(0,'127.0.0.1',done)) /* 等待异步操作完成。 */
 try { /* 执行当前语句并推进处理流程。 */
  const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith('IOT_'))) /* 声明 env。 */
  const child=spawn(process.execPath,['scripts/generate-demo-data.mjs','--origin',`http://127.0.0.1:${server.address().port}`,'--env-file',envFile,'--prefix','demo-failure','--output',join(dir,'out'),'--stages','inspect'],{env,windowsHide:true,stdio:['ignore','pipe','pipe']}) /* 声明 child。 */
  let output='';for(const stream of [child.stdout,child.stderr])stream.on('data',b=>{output+=b.toString()}) /* 声明 output。 */
  const code=await new Promise(done=>child.once('exit',done)) /* 声明 code。 */
  assert.equal(code,1);assert.ok(!output.includes(password)) /* 验证实际结果符合预期。 */
  const report=JSON.parse(await readFile(join(dir,'out','report.json'),'utf8')) /* 声明 report。 */
  assert.ok(report.results.length>0);assert.ok(report.results.every(x=>x.status==='未通过'));assert.equal(report.stages[0].status,'未通过') /* 验证实际结果符合预期。 */
  assert.ok(!JSON.stringify(report).includes(password)) /* 验证实际结果符合预期。 */
 } finally {server.closeAllConnections();await new Promise(done=>server.close(done))} /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

test('真实隔离 API 执行演示业务、协议、套接字、Excel与权限流程', {skip:!exe,timeout:240000}, async()=>{ /* 执行当前语句并推进处理流程。 */
 const port=async()=>{const s=createServer();await new Promise(done=>s.listen(0,'127.0.0.1',done));const p=s.address().port;await new Promise(done=>s.close(done));return p} /* 声明 port。 */
 const dir=await mkdtemp(join(tmpdir(),'iot-demo-api-')),httpPort=await port(),tcpPort=await port(),udpPort=await port() /* 声明 dir。 */
 const password=randomUUID(),envFile=join(dir,'test.env') /* 声明 password。 */
 await writeFile(envFile,`IOT_DEV_MODE=true\nIOT_HTTP_ADDR=127.0.0.1:${httpPort}\nIOT_DATA_DIR=${dir.replaceAll('\\','/')}\nIOT_ADMIN_USER=admin\nIOT_ADMIN_PASSWORD=${password}\nIOT_ADMIN_TENANTS=tenant_demo_test\nIOT_JWT_SECRET=${randomUUID()}\n`) /* 等待异步操作完成。 */
 const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith('IOT_'))) /* 声明 env。 */
 const api=spawn(exe,['--env-file',envFile],{env,windowsHide:true,stdio:['ignore','pipe','pipe']}) /* 声明 api。 */
 let apiLog='';for(const stream of [api.stdout,api.stderr])stream.on('data',b=>{apiLog=(apiLog+b.toString()).slice(-6000)}) /* 声明 apiLog。 */
 const stopped=new Promise(done=>api.once('exit',done)) /* 声明 stopped。 */
 try { /* 执行当前语句并推进处理流程。 */
  let ready=false /* 声明 ready。 */
  for(let i=0;i<100;i++){try{const r=await fetch(`http://127.0.0.1:${httpPort}/health/live`);if(r.ok){ready=true;break}}catch{}await new Promise(done=>setTimeout(done,100))} /* 循环处理当前数据。 */
  assert.ok(ready,'isolated API failed to start') /* 验证实际结果符合预期。 */
  const child=spawn(process.execPath,['scripts/generate-demo-data.mjs','--origin',`http://127.0.0.1:${httpPort}`,'--env-file',envFile,'--prefix','demo-integration','--output',join(dir,'report'),'--tcp-port',String(tcpPort),'--udp-port',String(udpPort),'--stages','business,protocols,children,services,refine,access','--skip-ai','--skip-backup'],{env,windowsHide:true,stdio:['ignore','pipe','pipe']}) /* 声明 child。 */
  let output='';for(const stream of [child.stdout,child.stderr])stream.on('data',b=>{output+=b.toString()}) /* 声明 output。 */
  const code=await new Promise(done=>child.once('exit',done)) /* 声明 code。 */
  const report=JSON.parse(await readFile(join(dir,'report','report.json'),'utf8')) /* 声明 report。 */
  assert.equal(code,0,output.replaceAll(password,'<REDACTED>')) /* 验证实际结果符合预期。 */
  assert.ok(report.results.some(x=>x.name.includes('Excel')&&x.status==='通过')) /* 验证实际结果符合预期。 */
  assert.ok(report.results.some(x=>x.name.includes('权限')||x.name.includes('范围验证'))) /* 验证实际结果符合预期。 */
  assert.ok(report.results.some(x=>x.name.includes('TCP/UDP 实际')&&x.status==='通过')) /* 验证实际结果符合预期。 */
  assert.ok(report.results.some(x=>x.status==='跳过')) /* 验证实际结果符合预期。 */
  assert.ok(!JSON.stringify(report).includes(password)) /* 验证实际结果符合预期。 */
  console.log(`隔离运行报告：${join(dir,'report','report.html')}`) /* 执行当前语句并推进处理流程。 */
 } finally {api.kill('SIGTERM');await stopped} /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
