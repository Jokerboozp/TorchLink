// Real nginx routing, isolated upstream HTTP fixture. No Docker required.
// IOT_TEST_NGINX=/path/to/nginx node scripts/tests/nginx-protocol-routing-smoke.mjs
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { spawn, spawnSync } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join, resolve, sep } from 'node:path' /* 引入当前代码需要的依赖。 */

const executable = process.env.IOT_TEST_NGINX /* 声明 executable。 */
if (!executable) throw Error('Set IOT_TEST_NGINX to the nginx executable') /* 判断条件并选择处理分支。 */
const fixture = createServer(async (req,res) => { /* 声明 fixture。 */
  const chunks=[] /* 声明 chunks。 */
  for await (const chunk of req) chunks.push(chunk) /* 循环处理当前数据。 */
  const path=new URL(req.url,'http://fixture').pathname /* 声明 path。 */
  const status=path==='/api/v2/protocols/' ? 404 : req.headers.authorization ? 200 : 401 /* 声明 status。 */
  res.writeHead(status,{'Content-Type':'application/json'}) /* 执行当前语句并推进处理流程。 */
  res.end(JSON.stringify({path:req.url,method:req.method,authorization:req.headers.authorization,body:Buffer.concat(chunks).toString()})) /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
await new Promise(done=>fixture.listen(0,'127.0.0.1',done)) /* 等待异步操作完成。 */
const probe=createServer() /* 声明 probe。 */
await new Promise(done=>probe.listen(0,'127.0.0.1',done)) /* 等待异步操作完成。 */
const port=probe.address().port /* 声明 port。 */
await new Promise(done=>probe.close(done)) /* 等待异步操作完成。 */
const directory=await mkdtemp(join(tmpdir(),'iot-nginx-routing-')) /* 声明 directory。 */
const prefix=directory.replaceAll('\\','/')+'/' /* 声明 prefix。 */
let child /* 声明 child。 */
try { /* 执行当前语句并推进处理流程。 */
  await mkdir(join(directory,'logs')) /* 等待异步操作完成。 */
  await mkdir(join(directory,'temp')) /* 等待异步操作完成。 */
  const original=await readFile(new URL('../../iot_front/nginx.conf',import.meta.url),'utf8') /* 声明 original。 */
  const server=original.replace('listen 8080;',`listen 127.0.0.1:${port};`).replaceAll('platform-api:8080',`127.0.0.1:${fixture.address().port}`) /* 声明 server。 */
  await writeFile(join(directory,'nginx.conf'),`worker_processes 1;\ndaemon off;\npid nginx.pid;\nevents { worker_connections 64; }\nhttp { access_log off; ${server} }`) /* 等待异步操作完成。 */
  const syntax=spawnSync(executable,['-p',prefix,'-c','nginx.conf','-t'],{encoding:'utf8',windowsHide:true}) /* 声明 syntax。 */
  assert.equal(syntax.status,0,syntax.stderr) /* 验证实际结果符合预期。 */
  child=spawn(executable,['-p',prefix,'-c','nginx.conf'],{stdio:'ignore',windowsHide:true}) /* 更新 child 的值。 */
  const request=(path,options={})=>fetch(`http://127.0.0.1:${port}${path}`,{redirect:'manual',signal:AbortSignal.timeout(5000),...options}) /* 声明 request。 */
  let ready=false /* 声明 ready。 */
  for(let i=0;i<50;i++) { try {await request('/health/live');ready=true;break} catch {await new Promise(done=>setTimeout(done,100))} } /* 循环处理当前数据。 */
  assert.equal(ready,true,'nginx did not start') /* 验证实际结果符合预期。 */
  const headers={Authorization:'Bearer routing-fixture','Content-Type':'application/json'} /* 声明 headers。 */
  for(const suffix of ['', '/']) { /* 循环处理当前数据。 */
    const result=await request(`/api/v2/protocols${suffix}?page=2`,{headers}) /* 声明 result。 */
    assert.equal(result.status,200,`protocol list ${suffix || '(no slash)'} returned ${result.status}, Location=${result.headers.get('location')}`) /* 验证实际结果符合预期。 */
    assert.equal((await result.json()).path,'/api/v2/protocols?page=2') /* 验证实际结果符合预期。 */
    const post=await request(`/api/v2/protocols${suffix}`,{headers,method:'POST',body:'{"name":"test"}'}) /* 声明 post。 */
    assert.equal(post.status,200) /* 验证实际结果符合预期。 */
    const data=await post.json() /* 声明 data。 */
    assert.equal(data.method,'POST');assert.equal(data.body,'{"name":"test"}');assert.equal(data.authorization,headers.Authorization) /* 验证实际结果符合预期。 */
    assert.equal((await request(`/api/v2/protocols${suffix}`)).status,401,'authorization must still reach upstream') /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  for(const path of ['/api/v2/protocols/demo/releases/v1/source','/api/v1/products','/api/v2/device-access-profiles','/api/v2/protocol-source-template']) { /* 循环处理当前数据。 */
    const response=await request(path,{headers});assert.equal(response.status,200);assert.equal((await response.json()).path,path) /* 声明 response。 */
  } /* 结束当前表达式或代码块。 */
  // Preserve the larger protocol source upload limit, above generic API's 12 MiB.
  const body='x'.repeat(13*1024*1024) /* 声明 body。 */
  const upload=await request('/api/v2/protocols/demo/source-releases',{method:'POST',headers,body}) /* 声明 upload。 */
  assert.equal(upload.status,200);assert.equal((await upload.json()).body.length,body.length) /* 验证实际结果符合预期。 */
  console.log('PASS: real nginx routes protocol lists with/without slash, preserves POST/query/auth, source uploads and related API routes') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  if(child) { /* 判断条件并选择处理分支。 */
    const exited=new Promise(done=>child.exitCode!==null ? done() : child.once('exit',done)) /* 声明 exited。 */
    spawnSync(executable,['-p',prefix,'-c','nginx.conf','-s','stop'],{stdio:'ignore',windowsHide:true}) /* 执行当前语句并推进处理流程。 */
    await exited /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  fixture.closeAllConnections();await new Promise(done=>fixture.close(done)) /* 执行当前语句并推进处理流程。 */
  if(!resolve(directory).startsWith(resolve(tmpdir())+sep+'iot-nginx-routing-')) throw Error('Unexpected test directory') /* 判断条件并选择处理分支。 */
  await rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
