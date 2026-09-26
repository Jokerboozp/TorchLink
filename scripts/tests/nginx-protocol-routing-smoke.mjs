// Real nginx routing, isolated upstream HTTP fixture. No Docker required.
// IOT_TEST_NGINX=/path/to/nginx node scripts/tests/nginx-protocol-routing-smoke.mjs
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn, spawnSync } from 'node:child_process'
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve, sep } from 'node:path'

const executable = process.env.IOT_TEST_NGINX
if (!executable) throw Error('Set IOT_TEST_NGINX to the nginx executable')
const fixture = createServer(async (req,res) => {
  const chunks=[]
  for await (const chunk of req) chunks.push(chunk)
  const path=new URL(req.url,'http://fixture').pathname
  const status=path==='/api/v2/protocols/' ? 404 : req.headers.authorization ? 200 : 401
  res.writeHead(status,{'Content-Type':'application/json'})
  res.end(JSON.stringify({path:req.url,method:req.method,authorization:req.headers.authorization,body:Buffer.concat(chunks).toString()}))
})
await new Promise(done=>fixture.listen(0,'127.0.0.1',done))
const probe=createServer()
await new Promise(done=>probe.listen(0,'127.0.0.1',done))
const port=probe.address().port
await new Promise(done=>probe.close(done))
const directory=await mkdtemp(join(tmpdir(),'iot-nginx-routing-'))
const prefix=directory.replaceAll('\\','/')+'/'
let child
try {
  await mkdir(join(directory,'logs'))
  await mkdir(join(directory,'temp'))
  const original=await readFile(new URL('../../iot_front/nginx.conf',import.meta.url),'utf8')
  const server=original.replace('listen 8080;',`listen 127.0.0.1:${port};`).replaceAll('platform-api:8080',`127.0.0.1:${fixture.address().port}`)
  await writeFile(join(directory,'nginx.conf'),`worker_processes 1;\ndaemon off;\npid nginx.pid;\nevents { worker_connections 64; }\nhttp { access_log off; ${server} }`)
  const syntax=spawnSync(executable,['-p',prefix,'-c','nginx.conf','-t'],{encoding:'utf8',windowsHide:true})
  assert.equal(syntax.status,0,syntax.stderr)
  child=spawn(executable,['-p',prefix,'-c','nginx.conf'],{stdio:'ignore',windowsHide:true})
  const request=(path,options={})=>fetch(`http://127.0.0.1:${port}${path}`,{redirect:'manual',signal:AbortSignal.timeout(5000),...options})
  let ready=false
  for(let i=0;i<50;i++) { try {await request('/health/live');ready=true;break} catch {await new Promise(done=>setTimeout(done,100))} }
  assert.equal(ready,true,'nginx did not start')
  const headers={Authorization:'Bearer routing-fixture','Content-Type':'application/json'}
  for(const suffix of ['', '/']) {
    const result=await request(`/api/v2/protocols${suffix}?page=2`,{headers})
    assert.equal(result.status,200,`protocol list ${suffix || '(no slash)'} returned ${result.status}, Location=${result.headers.get('location')}`)
    assert.equal((await result.json()).path,'/api/v2/protocols?page=2')
    const post=await request(`/api/v2/protocols${suffix}`,{headers,method:'POST',body:'{"name":"test"}'})
    assert.equal(post.status,200)
    const data=await post.json()
    assert.equal(data.method,'POST');assert.equal(data.body,'{"name":"test"}');assert.equal(data.authorization,headers.Authorization)
    assert.equal((await request(`/api/v2/protocols${suffix}`)).status,401,'authorization must still reach upstream')
  }
  for(const path of ['/api/v2/protocols/demo/releases/v1/source','/api/v1/products','/api/v2/device-access-profiles','/api/v2/protocol-source-template']) {
    const response=await request(path,{headers});assert.equal(response.status,200);assert.equal((await response.json()).path,path)
  }
  // Preserve the larger protocol source upload limit, above generic API's 12 MiB.
  const body='x'.repeat(13*1024*1024)
  const upload=await request('/api/v2/protocols/demo/source-releases',{method:'POST',headers,body})
  assert.equal(upload.status,200);assert.equal((await upload.json()).body.length,body.length)
  console.log('PASS: real nginx routes protocol lists with/without slash, preserves POST/query/auth, source uploads and related API routes')
} finally {
  if(child) {
    const exited=new Promise(done=>child.exitCode!==null ? done() : child.once('exit',done))
    spawnSync(executable,['-p',prefix,'-c','nginx.conf','-s','stop'],{stdio:'ignore',windowsHide:true})
    await exited
  }
  fixture.closeAllConnections();await new Promise(done=>fixture.close(done))
  if(!resolve(directory).startsWith(resolve(tmpdir())+sep+'iot-nginx-routing-')) throw Error('Unexpected test directory')
  await rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100})
}
