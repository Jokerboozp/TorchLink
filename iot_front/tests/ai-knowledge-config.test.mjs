import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { aiProviderOptions, statusLabel } from '../src/presentation.js'
import { loadAllPages } from '../src/listPagination.js'

function component(file, api, names, allowed = () => true, extra = {}) {
  const notices = []
  const timers = new Map()
  let timerId = 0
  let unmount = () => {}
  const context = vm.createContext({
    computed,
    reactive,
    ref,
    watch,
    api,
    apiAll: (path, options) => loadAllPages(api, path, options),
    session: { token: 'token' },
    useUnsavedGuard() {},
    can: allowed,
    URL,
    AbortController,
    FormData,
    onMounted() {},
    onBeforeUnmount(fn) {
      unmount = fn
    },
    defineEmits: () => () => {},
    UiMessage: Object.fromEntries(['info', 'success', 'warning', 'error'].map(type => [type, message => notices.push({ type, message })])),
    notifyError: cause => notices.push({ type: 'error', message: cause.message }),
    setTimeout: callback => {
      timers.set(++timerId, callback)
      return timerId
    },
    clearTimeout: id => timers.delete(id),
    ...extra
  })
  const source = setupScript(new URL(`../src/${file}`, import.meta.url))
  const state = vm.runInContext(`${source}\n;({${names}})`, context)
  return { state, notices, timers, unmount: () => unmount() }
}

const stored = {
  baseUrl: 'https://embedding.example/v1',
  model: 'embedding-model',
  apiKeyConfigured: true,
  dimensions: 1024,
  batchSize: 16,
  queryInstruction: '检索：',
  timeoutSeconds: 60
}

test('Embedding configuration keeps the stored key when blank and clears it only explicitly', async () => {
  const requests = []
  const { state } = component(
    'components/EmbeddingConfig.vue',
    async (_path, options) => {
      if (options?.method === 'PUT') requests.push(JSON.parse(options.body))
      return { ...stored, apiKeyConfigured: !requests.at(-1)?.clearAPIKey }
    },
    'form,config,load,save'
  )
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

test('Embedding save confirms an index rebuild only when the vector space changes', async () => {
  const puts = [],
    asked = []
  let answer = false
  const { state } = component(
    'components/EmbeddingConfig.vue',
    async (_path, options) => {
      if (options?.method === 'PUT') puts.push(JSON.parse(options.body))
      return stored
    },
    'form,load,save',
    () => true,
    {
      UiMessageBox: {
        confirm: async message => {
          asked.push(message)
          if (!answer) throw new Error('cancel')
          return 'confirm'
        }
      }
    }
  )
  await state.load()
  state.form.batchSize = 32
  await state.save()
  assert.equal(asked.length, 0, '只改每批分片数不重建索引，无需确认')
  assert.equal(puts.length, 1)
  state.form.model = 'another-model'
  await state.save()
  assert.equal(asked.length, 1)
  assert.match(asked[0], /模型/)
  assert.match(asked[0], /旧索引/)
  assert.equal(puts.length, 1, '取消确认时不能保存')
  answer = true
  state.form.model = 'another-model'
  await state.save()
  assert.equal(puts.length, 2)
  assert.equal(puts[1].model, 'another-model')
})

test('Embedding tests use unsaved candidate settings and discard results after the candidate changes', async () => {
  let finish
  let request
  const { state } = component(
    'components/EmbeddingConfig.vue',
    (path, options) => {
      if (path.endsWith('embedding-config')) return Promise.resolve(stored)
      request = { path, body: JSON.parse(options.body) }
      return new Promise(resolve => {
        finish = resolve
      })
    },
    'form,load,testConnection,result,error'
  )
  await state.load()
  state.form.model = 'candidate-model'
  state.form.dimensions = 512
  const running = state.testConnection()
  assert.equal(request.path, '/api/v1/ai/embedding-test')
  assert.equal(request.body.model, 'candidate-model')
  assert.equal(request.body.dimensions, 512)
  assert.equal(request.body.apiKey, '')
  state.form.model = 'new-model'
  finish({ success: true, dimensions: 512, latencyMs: 20 })
  await running
  assert.equal(state.result.value, null)
  assert.equal(state.error.value, '')
})

test('Embedding save does not depend on a successful connection test', async () => {
  const requests = []
  const { state } = component(
    'components/EmbeddingConfig.vue',
    async (path, options) => {
      requests.push(path)
      if (path.endsWith('embedding-test')) return { success: false, error: 'API 暂不可用' }
      return stored
    },
    'load,testConnection,save,error'
  )
  await state.load()
  await state.testConnection()
  assert.equal(state.error.value, 'API 暂不可用')
  await state.save()
  assert.equal(requests.filter(path => path.endsWith('embedding-config')).length, 2)
  assert.equal(state.error.value, '')
})

test('Embedding invalid input and missing write permissions do not submit API calls', async () => {
  const requests = []
  const { state } = component(
    'components/EmbeddingConfig.vue',
    async (path, options) => {
      requests.push({ path, method: options?.method })
      return stored
    },
    'load,form,save,testConnection,error',
    permission => permission.startsWith('GET ')
  )
  await state.load()
  await state.save()
  await state.testConnection()
  assert.equal(requests.length, 1)
  const writable = component(
    'components/EmbeddingConfig.vue',
    async () => {
      throw new Error('should not submit')
    },
    'form,save,error'
  ).state
  Object.assign(writable.form, stored, { dimensions: 0 })
  await writable.save()
  assert.match(writable.error.value, /正整数/)
})

test('Embedding unmount cancels configuration reads and ignores the late response', async () => {
  let finish
  let signal
  const { state, unmount } = component(
    'components/EmbeddingConfig.vue',
    (_path, options) => {
      signal = options.signal
      return new Promise(resolve => {
        finish = resolve
      })
    },
    'load,config'
  )
  const running = state.load()
  unmount()
  assert.equal(signal.aborted, true)
  finish(stored)
  await running
  assert.equal(state.config.value, null)
})

test('knowledge pending documents trigger polling even when no global rebuild is active', async () => {
  let status = 'UPLOADED'
  const { state, timers, unmount } = component(
    'views/KnowledgeView.vue',
    async path => {
      if (path.includes('/knowledge/documents'))
        return { items: [{ id: 'doc', status }], total: 1, persistentIndex: true, indexState: { state: 'ready' } }
      return { items: [] }
    },
    'load,documents,binding'
  )
  assert.equal(state.binding.knowledgeBinding.value.retrievalMode, 'always')
  await state.load()
  assert.equal(timers.size, 1)
  status = 'INDEXED'
  await state.load(true)
  assert.equal(timers.size, 0)
  unmount()
})

test('knowledge accepted deletion keeps polling until the server removes the document', async () => {
  let items = [{ id: 'doc', status: 'DELETING' }]
  const { state, timers, unmount } = component(
    'views/KnowledgeView.vue',
    async path => {
      if (path.includes('/knowledge/documents')) return { items, total: items.length, indexState: { state: 'ready' } }
      return { items: [] }
    },
    'load,documents,documentActions'
  )
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
    .replace(/^import\s[^'"]*['"][^'"]+['"];?$/gm, '')
    .replace(/^export /gm, '')
  for (const result of [{ deleting: true }, { deleted: true }]) {
    const notices = []
    let refreshed = false
    const confirmDelete = vm.runInContext(
      `${source}\n;confirmDelete`,
      vm.createContext({
        api: async () => result,
        UiMessageBox: { confirm: async () => {} },
        UiMessage: { success: message => notices.push(message) },
        notifyError: () => {}
      })
    )
    await confirmDelete({
      label: 'fixture',
      path: '/fixture',
      onDeleted: async () => {
        refreshed = true
      }
    })
    assert.equal(notices[0], result.deleting ? '已提交删除，清理将在后台完成' : '删除成功')
    assert.equal(refreshed, true)
  }
})

// 只模拟上传用到的 XMLHttpRequest 接口，由测试控制进度、完成和取消。
function fakeUploads() {
  const requests = []
  class FakeXHR {
    constructor() {
      this.upload = {}
      this.headers = {}
      requests.push(this)
    }
    open(method, path) {
      Object.assign(this, { method, path })
    }
    setRequestHeader(name, value) {
      this.headers[name] = value
    }
    send(body) {
      this.body = body
    }
    abort() {
      this.aborted = true
      this.onabort?.()
    }
    progress(loaded, total) {
      this.upload.onprogress?.({ lengthComputable: true, loaded, total })
    }
    respond(status, data) {
      Object.assign(this, { status, responseText: JSON.stringify(data) })
      this.onload?.()
    }
  }
  return { requests, XMLHttpRequest: FakeXHR }
}

// The upload dialog starts open; emitted events are collected for assertions.
function uploadDialog(uploads) {
  const emitted = []
  const dialog = component(
    'components/knowledge/KnowledgeUploadDialog.vue',
    async () => ({ items: [], total: 0 }),
    'upload,cancelUpload,selectedFile,workflowId,uploading,uploadPercent,visible',
    () => true,
    {
      XMLHttpRequest: uploads.XMLHttpRequest,
      defineModel: () => ref(true),
      defineProps: () => ({ agents: [{ id: 'agent', name: '运维助手' }] }),
      defineEmits:
        () =>
        (...args) =>
          emitted.push(args),
      nextTick: callback => callback()
    }
  )
  return { ...dialog, emitted }
}

test('knowledge upload reports acceptance and leaves indexing status to the server', async () => {
  const uploads = fakeUploads()
  const { state, notices, emitted, unmount } = uploadDialog(uploads)
  assert.equal(state.workflowId.value, 'agent', '默认关联第一个智能体')
  state.selectedFile.value = new Blob(['manual'])
  const running = state.upload()
  const request = uploads.requests[0]
  assert.equal(request.path, '/api/v1/knowledge/documents')
  assert.equal(request.headers.Authorization, 'Bearer token')
  request.progress(3, 6)
  assert.equal(state.uploadPercent.value, 50, '进度按浏览器实际发送的字节计算')
  request.respond(201, { id: 'doc', workflowId: 'agent', status: 'UPLOADED' })
  await running
  assert.match(notices.find(item => item.type === 'success').message, /运维助手.*后台建立/)
  assert.doesNotMatch(notices.find(item => item.type === 'success').message, /已索引/)
  assert.equal(state.visible.value, false)
  assert.deepEqual(emitted, [['uploaded']])
  unmount()
})

test('cancelling a knowledge upload aborts it and ignores a late result', async () => {
  const uploads = fakeUploads()
  const { state, notices, emitted, unmount } = uploadDialog(uploads)
  state.selectedFile.value = new Blob(['manual'])
  const running = state.upload()
  uploads.requests[0].progress(1, 6)
  state.cancelUpload()
  await running
  assert.equal(uploads.requests[0].aborted, true)
  assert.equal(state.uploading.value, false)
  assert.equal(state.visible.value, false)
  assert.deepEqual(emitted, [], '取消后页面不刷新也不切换标签')
  assert.equal(notices.filter(item => item.type === 'success' || item.type === 'error').length, 0)
  unmount()
})

test('knowledge binding follows the agent list and keeps the loaded policy', async () => {
  const requests = []
  const { state } = component(
    'views/KnowledgeView.vue',
    async path => {
      requests.push(path)
      if (path.includes('/knowledge-binding')) return { retrievalMode: 'auto', topK: 3, minScore: 0.4 }
      if (path.includes('/ai/workflows')) return { items: [{ id: 'first' }, { id: 'second' }], total: 2 }
      return { items: [], total: 0 }
    },
    'load,binding'
  )
  await state.load()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(state.binding.bindingWorkflowId.value, 'first')
  assert.deepEqual(
    { ...state.binding.knowledgeBinding.value },
    { retrievalMode: 'auto', topK: 3, minScore: 0.4, noMatchPolicy: 'allow-model' }
  )
  await state.load(true)
  assert.equal(requests.filter(path => path.includes('/knowledge-binding')).length, 1, '已读取的策略不会被刷新列表覆盖')
})

test('knowledge polling failure keeps existing rows and exposes one inline error', async () => {
  let offline = false
  const { state, notices, unmount } = component(
    'views/KnowledgeView.vue',
    async path => {
      if (path.includes('/knowledge/documents')) {
        if (offline) throw new Error('文档索引状态暂不可用')
        return { items: [{ id: 'doc', status: 'INDEXING' }], total: 1 }
      }
      return { items: [] }
    },
    'load,documents,documentsError'
  )
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
  const failed = { id: 'doc /1', status: 'INDEX_FAILED', metadata: { indexError: 'API 暂不可用' } }
  let finish
  let calls = 0
  const { state, unmount } = component(
    'views/KnowledgeView.vue',
    async (path, options) => {
      if (options?.method === 'POST') {
        calls++
        assert.equal(path, '/api/v1/knowledge/documents/doc%20%2F1/retry')
        return new Promise(resolve => {
          finish = resolve
        })
      }
      return { items: [{ ...failed, status: 'UPLOADED' }], total: 1 }
    },
    'retryDocument,retrying,documents'
  )
  state.documents.value = [failed]
  const running = state.retryDocument(failed)
  await state.retryDocument(failed)
  assert.equal(calls, 1)
  finish({ ...failed, status: 'UPLOADED' })
  await running
  assert.equal(state.documents.value[0].status, 'UPLOADED')
  assert.equal(state.retrying.value.length, 0)
  unmount()
  const denied = component(
    'views/KnowledgeView.vue',
    async () => {
      throw new Error('should not submit')
    },
    'retryDocument',
    () => false
  )
  await denied.state.retryDocument(failed)
})

test('knowledge progress is computed only from server batch counts', () => {
  const document = reactive({ status: 'INDEXING', metadata: { indexProgress: { done: 3, total: 10 } } })
  const source = setupScript(new URL('../src/components/KnowledgeIndexStatus.vue', import.meta.url))
  const state = vm.runInContext(
    `${source}\n;({percentage,progressLabel,label})`,
    vm.createContext({ computed, statusLabel, defineProps: () => ({ document }) })
  )
  assert.equal(state.percentage.value, 30)
  assert.equal(state.progressLabel.value, '3 / 10 个分片已处理')
  document.metadata.indexProgress = { done: 0, total: 0 }
  document.status = 'UPLOADED'
  assert.equal(state.percentage.value, null)
  assert.equal(state.label.value, '等待建立索引')
})

test('model status refresh does not overwrite an edited provider form, and registers it as unsaved', async () => {
  const guards = []
  let saved = { provider: 'deepseek', baseUrl: 'https://api.deepseek.com', model: 'deepseek-flash', maxTokens: 2048 }
  const { state } = component(
    'views/AiProvidersView.vue',
    async (_path, options) => {
      if (options?.method === 'PUT') {
        saved = { ...JSON.parse(options.body) }
        return saved
      }
      return { items: [], total: 0, active: { id: 'deepseek' }, config: { ...saved, apiKeyConfigured: true } }
    },
    'providerForm,loadRuntime,applyProviderConfig,providerDirty',
    () => true,
    { providerOptions: aiProviderOptions, useUnsavedGuard: check => guards.push(check) }
  )
  await state.loadRuntime()
  assert.equal(state.providerDirty.value, false)
  state.providerForm.model = 'deepseek-pro'
  state.providerForm.apiKey = 'sk-test'
  saved = { ...saved, model: 'server-side-change' }
  await state.loadRuntime()
  assert.equal(state.providerForm.model, 'deepseek-pro', '刷新状态不能覆盖正在编辑的模型')
  assert.equal(state.providerForm.apiKey, 'sk-test', '刷新状态不能清空已填写的 API Key')
  assert.equal(
    guards.some(check => check()),
    true
  )
  await state.applyProviderConfig()
  assert.equal(state.providerDirty.value, false)
  assert.equal(state.providerForm.apiKey, '')
  assert.equal(
    guards.some(check => check()),
    false
  )
})
