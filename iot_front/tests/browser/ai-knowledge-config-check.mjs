// Isolated UI acceptance with synthetic responses; no model service or database is contacted.
import { startBrowser } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { extname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = fileURLToPath(new URL('../../dist/', import.meta.url))
const now = Date.now()
const agent = { id:'fixture-assistant', name:'运维助手', enabled:true }
let embedding = { baseUrl:'https://embedding.example/v1', model:'fixture-embedding', apiKeyConfigured:true, dimensions:1024, batchSize:16, queryInstruction:'', timeoutSeconds:60 }
let documents = [
  { id:'indexing', filename:'设备手册.txt', workflowId:agent.id, status:'INDEXING', createdAt:now, metadata:{ size:2048, chunks:4, indexProgress:{ done:2, total:4 } } },
  { id:'failed', filename:'处置规范.txt', workflowId:agent.id, status:'INDEX_FAILED', createdAt:now, metadata:{ size:1024, chunks:2, indexError:'Embedding API 暂不可用', indexProgress:{ done:0, total:2 } } },
  { id:'pending', filename:'维修记录.txt', workflowId:agent.id, status:'UPLOADED', createdAt:now, metadata:{ size:1024 } }
]
const tests = []
const saves = []
let retries = 0
let documentReads = 0
const server = createServer(async (req,res) => {
  const path = new URL(req.url, 'http://fixture').pathname
  if (path.startsWith('/api/')) {
    let body = { items:[], total:0 }
    if (path === '/api/v1/auth/login') body = { accessToken:'fixture', username:'admin', tenantId:'demo', role:'admin', permissions:['*'] }
    if (path === '/api/v1/auth/me') body = { username:'admin', tenantId:'demo', role:'admin', permissions:['*'] }
    if (path === '/api/v1/events') body = { permissions:['*'], alarms:[], states:[], accessVersion:'' }
    if (path === '/api/v1/ai/providers') body = { items:[], active:{ id:'deepseek', enabled:true }, config:{ provider:'deepseek', model:'deepseek-flash', baseUrl:'https://api.deepseek.com', apiKeyConfigured:true, maxTokens:2048 }, healthy:true }
    if (path === '/api/v1/ai/embedding-config' || path === '/api/v1/ai/embedding-test') {
      if (req.method !== 'GET') {
        let raw = ''
        for await (const chunk of req) raw += chunk
        const candidate = JSON.parse(raw)
        if (path.endsWith('embedding-test')) {
          tests.push(candidate)
          body = { success:true, dimensions:candidate.dimensions, latencyMs:12, message:'测试完成' }
        } else {
          saves.push(candidate)
          embedding = { ...candidate, apiKeyConfigured:candidate.clearAPIKey ? false : Boolean(candidate.apiKey) || embedding.apiKeyConfigured }
          delete embedding.apiKey
          delete embedding.clearAPIKey
          body = embedding
        }
      } else body = embedding
    }
    if (path === '/api/v1/knowledge/documents') { documentReads++; body = { items:documents, total:documents.length, persistentIndex:true, indexMode:'postgres-pgvector', embeddingModel:embedding.model, indexState:{ state:'ready' } } }
    if (path === '/api/v1/ai/workflows') body = { items:[agent], total:1 }
    if (path.endsWith('/knowledge-binding')) body = { retrievalMode:'always', topK:5, minScore:0.25, noMatchPolicy:'allow-model' }
    if (path === '/api/v1/knowledge/documents/failed/retry' && req.method === 'POST') {
      retries++
      documents = documents.map(document => document.id === 'failed' ? { ...document, status:'UPLOADED', metadata:{ ...document.metadata, indexError:'', indexProgress:{ done:0, total:2 } } } : document)
      body = documents.find(document => document.id === 'failed')
      res.statusCode = 202
    }
    res.setHeader('content-type','application/json')
    res.end(JSON.stringify(body))
    return
  }
  try {
    const file = resolve(dist, path === '/' ? 'index.html' : path.slice(1))
    if (!file.startsWith(resolve(dist) + sep)) throw new Error('outside fixture')
    const data = await readFile(file)
    res.setHeader('content-type', ({ '.js':'text/javascript', '.css':'text/css', '.html':'text/html', '.svg':'image/svg+xml' })[extname(file)] || 'application/octet-stream')
    res.end(data)
  } catch { res.statusCode = 404; res.end() }
})
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
const origin = `http://127.0.0.1:${server.address().port}`
let browser
try {
  browser = await startBrowser({ timeout:15000, args:['--disable-background-timer-throttling'] })
  const { call, evaluate, until, errors } = browser
  await call('Runtime.enable')
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride', { width:1440, height:1100, deviceScaleFactor:1, mobile:false })
  await call('Page.addScriptToEvaluateOnNewDocument', { source:'localStorage.clear()' })
  await call('Page.navigate', { url:origin })
  await until(() => evaluate(`Boolean(document.querySelector('.login-form input[type=password]'))`))
  await evaluate(`(()=>{const input=document.querySelector('.login-form input[type=password]');input.value='fixture';input.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.login-form button[type=submit]').click()})()`)
  await until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="模型管理"]'))`))
  await evaluate(`document.querySelector('.nav-item[aria-label="模型管理"]').click()`)
  await until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('已保存密钥')`))
  assert.equal(await evaluate(`document.querySelector('.embedding-config input[type=password]').value`), '')
  await evaluate(`[...document.querySelectorAll('.embedding-config button')].find(button=>button.innerText.trim()==='测试 Embedding').click()`)
  await until(() => tests.length === 1)
  await until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('返回 1024 维向量')`))
  assert.equal(tests[0].apiKey, '')
  await evaluate(`document.querySelector('.embedding-config .n-checkbox').click()`)
  await evaluate(`[...document.querySelectorAll('.embedding-config button')].find(button=>button.innerText.trim()==='保存 Embedding 配置').click()`)
  await until(() => saves.length === 1)
  assert.equal(saves[0].clearAPIKey, true)
  await until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('尚未保存接口密钥')`))
  await evaluate(`document.querySelector('.embedding-config').scrollIntoView({block:'center'})`)
  const modelScreenshot = await call('Page.captureScreenshot', { format:'png', captureBeyondViewport:true })
  const modelPath = join(tmpdir(), 'iot-embedding-config-desktop.png')
  await writeFile(modelPath, Buffer.from(modelScreenshot.data, 'base64'))
  await evaluate(`document.querySelector('.nav-item[aria-label="知识库"]').click()`)
  await until(() => evaluate(`document.querySelector('.knowledge-page')?.innerText.includes('2 / 4 个分片已处理')`))
  assert.equal(await evaluate(`document.querySelector('.knowledge-page').innerText.includes('Embedding API 暂不可用')`), true)
  await evaluate(`[...document.querySelectorAll('.knowledge-table button')].find(button=>button.innerText.trim()==='重试索引').click()`)
  await until(() => retries === 1)
  await until(() => evaluate(`!document.querySelector('.knowledge-table').innerText.includes('索引失败')`))
  const readsBefore = documentReads
  documents = documents.map(document => ({ ...document, status:'INDEXED', metadata:{ ...document.metadata, indexProgress:{ done:document.metadata.chunks || 1, total:document.metadata.chunks || 1 } } }))
  await until(() => documentReads > readsBefore)
  await until(() => evaluate(`!document.querySelector('.knowledge-table').innerText.includes('等待建立索引')`))
  await call('Emulation.setDeviceMetricsOverride', { width:390, height:844, deviceScaleFactor:1, mobile:true })
  await until(() => evaluate(`getComputedStyle(document.querySelector('.knowledge-mobile-list')).display !== 'none'`))
  await until(() => evaluate(`document.querySelector('.app-sidebar').getBoundingClientRect().right <= 1`))
  assert.equal(await evaluate(`document.documentElement.scrollWidth <= innerWidth`), true)
  const screenshot = await call('Page.captureScreenshot', { format:'png' })
  const mobilePath = join(tmpdir(), 'iot-knowledge-config-mobile.png')
  await writeFile(mobilePath, Buffer.from(screenshot.data, 'base64'))
  assert.deepEqual(errors, [])
  console.log(`PASS: Embedding test/save/key clearing, document progress/failure/retry/polling, narrow screen. Screenshots: ${modelPath}, ${mobilePath}`)
} finally {
  try { await browser?.close() } finally {
    server.closeAllConnections()
    await new Promise(resolve => server.close(resolve))
  }
}
