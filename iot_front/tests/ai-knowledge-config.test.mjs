import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { statusLabel } from '../src/presentation.js'

function component(file, api, names, allowed = () => true) {
  const notices = []
  const timers = new Map()
  let timerId = 0
  let unmount = () => {}
  const context = vm.createContext({ computed, reactive, ref, watch, api, can:allowed, URL, AbortController, FormData,
    onMounted(){}, onBeforeUnmount(fn){ unmount = fn }, defineEmits:() => () => {},
    UiMessage:Object.fromEntries(['info','success','warning','error'].map(type => [type, message => notices.push({ type, message })])),
    notifyError:cause => notices.push({ type:'error', message:cause.message }),
    setTimeout:callback => { timers.set(++timerId, callback); return timerId }, clearTimeout:id => timers.delete(id)
  })
  const source = setupScript(new URL(`../src/${file}`, import.meta.url))
  const state = vm.runInContext(`${source}\n;({${names}})`, context)
  return { state, notices, timers, unmount:() => unmount() }
}

const stored = { baseUrl:'https://embedding.example/v1', model:'embedding-model', apiKeyConfigured:true, dimensions:1024, batchSize:16, queryInstruction:'检索：', timeoutSeconds:60 }

test('Embedding configuration keeps the stored key when blank and clears it only explicitly', async () => {
  const requests = []
  const { state } = component('components/EmbeddingConfig.vue', async (_path, options) => {
    if (options?.method === 'PUT') requests.push(JSON.parse(options.body))
    return { ...stored, apiKeyConfigured:!requests.at(-1)?.clearAPIKey }
  }, 'form,config,load,save')
  await state.load()
  assert.equal(state.form.apiKey, '')
  assert.equal(state.config.value.apiKeyConfigured, true)
  await state.save()
  assert.equal(requests[0].apiKey, '')
  assert.equal(requests[0].clearAPIKey, false)
  state.form.clearAPIKey = true
  await state.save()
  assert.equal(requests[1].apiKey, '')
  assert.equal(requests[1].clearAPIKey, true)
  assert.equal(state.form.clearAPIKey, false)
  assert.equal(state.config.value.apiKeyConfigured, false)
})

test('Embedding tests use unsaved candidate settings and discard results after the candidate changes', async () => {
  let finish
  let request
  const { state } = component('components/EmbeddingConfig.vue', (path, options) => {
    if (path.endsWith('embedding-config')) return Promise.resolve(stored)
    request = { path, body:JSON.parse(options.body) }
    return new Promise(resolve => { finish = resolve })
  }, 'form,load,testConnection,result,error')
  await state.load()
  state.form.model = 'candidate-model'
  state.form.dimensions = 512
  const running = state.testConnection()
  assert.equal(request.path, '/api/v1/ai/embedding-test')
  assert.equal(request.body.model, 'candidate-model')
  assert.equal(request.body.dimensions, 512)
  assert.equal(request.body.apiKey, '')
  state.form.model = 'new-model'
  finish({ success:true, dimensions:512, latencyMs:20 })
  await running
  assert.equal(state.result.value, null)
  assert.equal(state.error.value, '')
})

test('Embedding save does not depend on a successful connection test', async () => {
  const requests = []
  const { state } = component('components/EmbeddingConfig.vue', async (path, options) => {
    requests.push(path)
    if (path.endsWith('embedding-test')) return { success:false, error:'API 暂不可用' }
    return stored
  }, 'load,testConnection,save,error')
  await state.load()
  await state.testConnection()
  assert.equal(state.error.value, 'API 暂不可用')
  await state.save()
  assert.equal(requests.filter(path => path.endsWith('embedding-config')).length, 2)
  assert.equal(state.error.value, '')
})

test('Embedding invalid input and missing write permissions do not submit API calls', async () => {
  const requests = []
  const { state } = component('components/EmbeddingConfig.vue', async (path, options) => {
    requests.push({ path, method:options?.method })
    return stored
  }, 'load,form,save,testConnection,error', permission => permission.startsWith('GET '))
  await state.load()
  await state.save()
  await state.testConnection()
  assert.equal(requests.length, 1)
  const writable = component('components/EmbeddingConfig.vue', async () => { throw new Error('should not submit') }, 'form,save,error').state
  Object.assign(writable.form, stored, { dimensions:0 })
  await writable.save()
  assert.match(writable.error.value, /正整数/)
})

test('Embedding unmount cancels configuration reads and ignores the late response', async () => {
  let finish
  let signal
  const { state, unmount } = component('components/EmbeddingConfig.vue', (_path, options) => {
    signal = options.signal
    return new Promise(resolve => { finish = resolve })
  }, 'load,config')
  const running = state.load()
  unmount()
  assert.equal(signal.aborted, true)
  finish(stored)
  await running
  assert.equal(state.config.value, null)
})

test('knowledge pending documents trigger polling even when no global rebuild is active', async () => {
  let status = 'UPLOADED'
  const { state, timers, unmount } = component('views/KnowledgeView.vue', async path => {
    if (path.includes('/knowledge/documents')) return { items:[{ id:'doc', status }], total:1, persistentIndex:true, indexState:{ state:'ready' } }
    return { items:[] }
  }, 'load,documents,knowledgeBinding')
  assert.equal(state.knowledgeBinding.value.retrievalMode, 'always')
  await state.load()
  assert.equal(timers.size, 1)
  status = 'INDEXED'
  await state.load(true)
  assert.equal(timers.size, 0)
  unmount()
})

test('knowledge accepted deletion keeps polling until the server removes the document', async () => {
  let items = [{ id:'doc', status:'DELETING' }]
  const { state, timers, unmount } = component('views/KnowledgeView.vue', async path => {
    if (path.includes('/knowledge/documents')) return { items, total:items.length, indexState:{ state:'ready' } }
    return { items:[] }
  }, 'load,documents,documentActions')
  await state.load()
  assert.equal(timers.size, 1)
  assert.equal(state.documentActions(items[0]).find(action => action.key === 'delete').disabled, true)
  items = []
  await state.load(true)
  assert.equal(state.documents.value.length, 0)
  assert.equal(timers.size, 0)
  assert.equal(statusLabel('DELETING'), '删除中')
  unmount()
})

test('delete confirmation distinguishes accepted background cleanup from completed deletion', async () => {
  const source = readFileSync(new URL('../src/deleteAction.js', import.meta.url), 'utf8')
    .replace(/^import .*$/gm, '').replace('export async function', 'async function')
  for (const result of [{ deleting:true }, { deleted:true }]) {
    const notices = []
    let refreshed = false
    const confirmDelete = vm.runInContext(`${source}\n;confirmDelete`, vm.createContext({ api:async () => result,
      UiMessageBox:{ confirm:async () => {} }, UiMessage:{ success:message => notices.push(message) }, notifyError:() => {} }))
    await confirmDelete({ label:'fixture', path:'/fixture', onDeleted:async () => { refreshed = true } })
    assert.equal(notices[0], result.deleting ? '已提交删除，清理将在后台完成' : '删除成功')
    assert.equal(refreshed, true)
  }
})

test('knowledge upload reports acceptance and leaves indexing status to the server', async () => {
  const { state, notices, unmount } = component('views/KnowledgeView.vue', async (_path, options) => {
    if (options?.method === 'POST') return { id:'doc', workflowId:'agent', status:'UPLOADED' }
    return { items:[], total:0 }
  }, 'upload,selectedFile,workflowId')
  state.workflowId.value = 'agent'
  state.selectedFile.value = new Blob(['manual'])
  await state.upload()
  assert.match(notices.find(item => item.type === 'success').message, /后台建立/)
  assert.doesNotMatch(notices.find(item => item.type === 'success').message, /已索引/)
  unmount()
})

test('knowledge polling failure keeps existing rows and exposes one inline error', async () => {
  let offline = false
  const { state, notices, unmount } = component('views/KnowledgeView.vue', async path => {
    if (path.includes('/knowledge/documents')) {
      if (offline) throw new Error('文档索引状态暂不可用')
      return { items:[{ id:'doc', status:'INDEXING' }], total:1 }
    }
    return { items:[] }
  }, 'load,documents,documentsError')
  await state.load()
  offline = true
  await state.load(true)
  await state.load(true)
  assert.equal(state.documents.value[0].id, 'doc')
  assert.equal(state.documentsError.value, '文档索引状态暂不可用')
  assert.equal(notices.length, 0)
  unmount()
})

test('knowledge retry is permission checked and prevents duplicate submissions', async () => {
  const failed = { id:'doc /1', status:'INDEX_FAILED', metadata:{ indexError:'API 暂不可用' } }
  let finish
  let calls = 0
  const { state, unmount } = component('views/KnowledgeView.vue', async (path, options) => {
    if (options?.method === 'POST') {
      calls++
      assert.equal(path, '/api/v1/knowledge/documents/doc%20%2F1/retry')
      return new Promise(resolve => { finish = resolve })
    }
    return { items:[{ ...failed, status:'UPLOADED' }], total:1 }
  }, 'retryDocument,retrying,documents')
  state.documents.value = [failed]
  const running = state.retryDocument(failed)
  await state.retryDocument(failed)
  assert.equal(calls, 1)
  finish({ ...failed, status:'UPLOADED' })
  await running
  assert.equal(state.documents.value[0].status, 'UPLOADED')
  assert.equal(state.retrying.value.length, 0)
  unmount()
  const denied = component('views/KnowledgeView.vue', async () => { throw new Error('should not submit') }, 'retryDocument', () => false)
  await denied.state.retryDocument(failed)
})

test('knowledge progress is computed only from server batch counts', () => {
  const document = reactive({ status:'INDEXING', metadata:{ indexProgress:{ done:3, total:10 } } })
  const source = setupScript(new URL('../src/components/KnowledgeIndexStatus.vue', import.meta.url))
  const state = vm.runInContext(`${source}\n;({percentage,progressLabel,label})`, vm.createContext({ computed, statusLabel, defineProps:() => ({ document }) }))
  assert.equal(state.percentage.value, 30)
  assert.equal(state.progressLabel.value, '3 / 10 个分片已处理')
  document.metadata.indexProgress = { done:0, total:0 }
  document.status = 'UPLOADED'
  assert.equal(state.percentage.value, null)
  assert.equal(state.label.value, '等待建立索引')
})

test('knowledge agent configuration stays available when the document list fails', async () => {
  let unavailable = true
  const requests = []
  const {state,notices,unmount} = component('views/KnowledgeView.vue', async path => {
    requests.push(path)
    if (path.includes('/knowledge/documents')) {
      if (unavailable) throw new Error('文档暂不可用')
      return {items:[{id:'doc',status:'INDEXED'}],total:1}
    }
    if (path.includes('/knowledge-binding')) return {topK:7}
    return {items:[{id:'device-health-inspector',name:'设备巡检',enabled:true},{id:'disabled',enabled:false}]}
  }, 'load,agents,workflowId,bindingWorkflowId,loadedBindingWorkflowId,documents,documentsError,agentError,loading')
  await state.load()
  assert.equal(state.agents.value.length, 1)
  assert.equal(state.workflowId.value, 'device-health-inspector')
  assert.equal(state.bindingWorkflowId.value, 'device-health-inspector')
  await new Promise(resolve=>setImmediate(resolve))
  assert.equal(state.loadedBindingWorkflowId.value, 'device-health-inspector')
  assert.ok(requests.some(path=>path.endsWith('/device-health-inspector/knowledge-binding')))
  assert.equal(state.documentsError.value, '文档暂不可用')
  assert.equal(state.agentError.value, '')
  assert.equal(state.loading.value, false)
  assert.equal(notices.length, 0)
  unavailable = false; await state.load()
  assert.equal(state.documents.value[0].id, 'doc')
  assert.equal(state.documentsError.value, '')
  unmount()
})

test('knowledge document reads remain usable when the agent catalog fails', async () => {
  const {state,unmount} = component('views/KnowledgeView.vue', async path => {
    if (path.includes('/knowledge/documents')) return {items:[{id:'doc',status:'INDEXED'}],total:1}
    throw new Error('智能体目录暂不可用')
  }, 'load,documents,documentsError,agentError')
  await state.load()
  assert.equal(state.documents.value[0].id, 'doc')
  assert.equal(state.documentsError.value, '')
  assert.equal(state.agentError.value, '智能体目录暂不可用')
  unmount()
})
