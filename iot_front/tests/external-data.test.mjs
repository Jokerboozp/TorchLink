import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import * as external from '../src/externalData.js'
import * as mediaHelpers from '../src/alarmMedia.js'
import { setupScript } from './helpers/vue.mjs'

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
function view(t, overrides = {}) {
  const cleanups = []
  const scope = effectScope()
  const context = {
    ...external,
    // 页面状态恢复由各自测试覆盖，此处替换为无副作用实现。
    usePageState: () => ({ restored: false }),
    computed,
    ref,
    watch,
    onMounted: () => {},
    onBeforeUnmount: fn => cleanups.push(fn),
    defineEmits: () => {},
    session: { tenant: 'tenant-a', user: 'alice', token: 'a' },
    permissionState: reactive({ accessVersion: 'v1' }),
    can: () => true,
    externalBase: '/api/v1/external-data',
    externalApi: { list: async () => ({ items: [], total: 0 }) },
    UiMessage: { success: () => {}, warning: () => {} },
    UiMessageBox: { confirm: async () => {} },
    notifyError: () => {},
    ...overrides
  }
  const script = setupScript(new URL('../src/views/ExternalDataView.vue', import.meta.url))
  const exposed =
    'load,items,page,sourceId,tab,view,runKind,bindRecord,endpointActions,detailOpen,detail,showDetail,operationOpen,openOperation,executeOperation,preview,previewLoading,previewError,sample,editorOpen,editorKind,editorValue,edit,save,rowActions,remove,rotateKey,retry'
  const component = scope.run(() => new Function('context', `with(context) { ${script}; return { ${exposed} } }`)(context))
  t.after(() => {
    cleanups.forEach(fn => fn())
    scope.stop()
  })
  return { component, context }
}

test('field editor preserves typed constants, false defaults, enum conversion and custom data paths', () => {
  const saved = [
    { target: 'online', type: 'boolean', constant: true, value: false, default: false, required: true },
    { target: 'data.temperature', path: 'reading.value', type: 'number', default: 0, values: { invalid: null } }
  ]
  assert.deepEqual(
    external.fieldsFromForm(external.fieldsToForm(saved)),
    saved.map(item => ({ ...item, path: item.path }))
  )
  assert.throws(() => external.fieldsFromForm([{ target: 'id', path: '', valueText: '', defaultText: '', valuesText: '{}' }]), /取值路径/)
  assert.throws(() => external.fieldsFromForm([{ target: 'id', constant: true, valueText: 'broken', valuesText: '{}' }]), /JSON 值/)
})

test('configuration validation retains revision and saved secret semantics and validates pull windows', () => {
  const source = external.validateSource({
    ...external.blankSource(' alice '),
    id: 's1',
    revision: 9,
    name: ' Video ',
    allowedHosts: [' api.example.com:443 ', 'api.example.com:443'],
    auth: { type: 'bearer', secret: '', secretSet: true }
  })
  assert.equal(source.username, 'alice')
  assert.equal(source.revision, 9)
  assert.deepEqual(source.allowedHosts, ['api.example.com:443'])
  assert.equal(source.auth.secret, '')
  assert.equal(source.auth.secretSet, true)
  assert.throws(() => external.timeWindow([20, 10]), /时间/)
  assert.deepEqual(external.timeWindow([10, 20]), { from: 10, to: 20 })
  assert.throws(
    () =>
      external.validateEndpoint({
        ...external.blankEndpoint('s1'),
        name: '拉取',
        mode: 'pull',
        url: 'https://api.example.com',
        intervalSeconds: 2
      }),
    /10 秒/
  )
  assert.deepEqual(external.jsonSample('[{"id":"a"},{"id":"b"}]'), [{ id: 'a' }, { id: 'b' }])
  assert.throws(() => external.jsonSample('"not an object"'), /对象或数组/)
})

test('request fence rejects superseded selection, changed identity and unmounted responses independently', () => {
  let owner = 'a'
  const fence = external.requestFence(() => owner)
  const oldList = fence.begin('list'),
    detail = fence.begin('detail'),
    newList = fence.begin('list')
  assert.equal(oldList(), false)
  assert.equal(detail(), true)
  assert.equal(newList(), true)
  owner = 'b'
  assert.equal(detail(), false)
  const current = fence.begin('detail')
  fence.dispose()
  assert.equal(current(), false)
})

test('list late response cannot overwrite a newer page or another identity', async t => {
  const requests = []
  const { component: c, context } = view(t, {
    externalApi: {
      list: () => {
        const request = deferred()
        requests.push(request)
        return request.promise
      }
    }
  })
  const first = c.load()
  c.page.value = 2
  const second = c.load()
  requests[1].resolve({ items: [{ id: 'new-page' }], total: 21 })
  await second
  requests[0].resolve({ items: [{ id: 'old-page' }], total: 99 })
  await first
  assert.equal(c.items.value[0].id, 'new-page')
  const third = c.load()
  context.session.user = 'bob'
  requests[2].resolve({ items: [{ id: 'alice-private' }], total: 1 })
  await third
  assert.equal(c.items.value[0].id, 'new-page')
})

test('detail selection and close fence late payloads', async t => {
  const requests = new Map()
  const { component: c } = view(t, {
    externalApi: {
      detail: id => {
        const request = deferred()
        requests.set(id, request)
        return request.promise
      }
    }
  })
  const first = c.showDetail({ id: 'r1' }),
    second = c.showDetail({ id: 'r2' })
  requests.get('r2').resolve({ entry: { id: 'r2' } })
  await second
  requests.get('r1').resolve({ entry: { id: 'r1' } })
  await first
  assert.equal(c.detail.value.entry.id, 'r2')
  const last = c.showDetail({ id: 'r3' })
  await nextTick()
  c.detailOpen.value = false
  await nextTick()
  requests.get('r3').resolve({ entry: { id: 'r3' } })
  await last
  assert.equal(c.detail.value, null)
})

test('slow background refreshes do not overlap, and confirmation cannot submit after an identity change', async t => {
  const pending = deferred(),
    confirmation = deferred(),
    calls = []
  const { component: c, context } = view(t, {
    externalApi: {
      list: () => {
        calls.push('list')
        return pending.promise
      },
      remove: async () => {
        calls.push('remove')
      },
      action: async () => {
        calls.push('action')
      }
    },
    UiMessageBox: { confirm: () => confirmation.promise }
  })
  const first = c.load(true)
  await c.load(true)
  assert.deepEqual(calls, ['list'])
  pending.resolve({ items: [], total: 0 })
  await first
  for (const action of [
    () => c.remove({ id: 's1' }),
    () => c.rotateKey({ id: 'e1', pushKeySet: true }),
    () => c.retry({ id: 'r1' }, 'records', true)
  ]) {
    const submitted = action()
    context.session.token += '-changed'
    confirmation.resolve()
    await submitted
  }
  assert.deepEqual(calls, ['list'])
})

test('preview does not mutate business records and cannot populate a different endpoint dialog', async t => {
  const pending = deferred(),
    calls = []
  const { component: c } = view(t, {
    externalApi: {
      action: (...args) => {
        calls.push(args)
        return pending.promise
      }
    }
  })
  c.openOperation({ id: 'endpoint-a' }, 'test')
  c.sample.value = '{"event":1}'
  const promise = c.executeOperation()
  c.openOperation({ id: 'endpoint-b' }, 'test')
  pending.resolve({ items: [{ event: { id: 'secret-a' } }] })
  await promise
  assert.equal(c.preview.value, null)
  assert.deepEqual(calls, [['endpoints', 'endpoint-a', 'test', { sample: { event: 1 } }]])
})

test('new and existing configurations use distinct permissions, and waiting jobs cannot be manually retried', t => {
  const { component: c } = view(t, { can: permission => permission === 'POST /api/v1/external-data/sources' })
  c.edit({ id: 'source-a', name: 'existing' })
  assert.equal(c.editorOpen.value, false)
  c.edit()
  assert.equal(c.editorOpen.value, true)
  assert.equal(c.editorValue.value.id, '')
  c.view.value = 'runs'
  c.runKind.value = 'jobs'
  assert.equal(c.rowActions({ id: 'j1', status: 'RETRY' }).length, 0)
  assert.equal(c.rowActions({ id: 'j1', status: 'FAILED' })[0].permission, 'POST /api/v1/external-data/jobs/:id/retry')
})

test('API client uses scoped revision deletes and preserves envelope payload for retry', async () => {
  const requests = []
  const source = readFileSync(new URL('../src/externalDataApi.js', import.meta.url), 'utf8')
    .replace(/^import\s[^'"]*['"][^'"]+['"];?$/gm, '')
    .replaceAll('export ', '')
  const { externalApi } = new Function('api', `${source}; return { externalApi }`)(async (...args) => {
    requests.push(args)
    return {}
  })
  await externalApi.remove('sources', { id: 's/a', revision: 4 })
  await externalApi.action('records', 'r1', 'retry', { revision: 6, useCurrentMapping: true })
  await externalApi.save('endpoints', {
    id: 'e1',
    revision: 2,
    name: '端点',
    runtime: { failedRecords: 3 },
    pushKey: 'server-only-hash',
    pushKeySet: true
  })
  assert.deepEqual(requests[0], ['/api/v1/external-data/sources/s%2Fa?revision=4', { method: 'DELETE' }])
  assert.deepEqual(JSON.parse(requests[1][1].body), { revision: 6, useCurrentMapping: true })
  assert.deepEqual(JSON.parse(requests[2][1].body), { id: 'e1', revision: 2, name: '端点' })
})

function mediaPanel(t, overrides = {}) {
  const scope = effectScope(),
    cleanups = [],
    urls = [],
    revoked = [],
    requests = []
  const props = reactive({
    alarm: {
      alarmId: 'alarm-1',
      details: {
        videoEvent: {
          eventId: 'event-1',
          snapshotUrl: 'local://video-alarm/t/snapshot.png',
          videoClipUrl: 'https://vendor.test/clip.mp4',
          raw: {
            snapshotTransferStatus: 'STORED',
            clipTransferStatus: 'FAILED',
            clipTransferError: '下载失败',
            mediaTransferStatus: 'FAILED'
          }
        }
      }
    }
  })
  const context = {
    ...mediaHelpers,
    props,
    computed,
    reactive,
    watch,
    defineProps: () => props,
    defineEmits: () => () => {},
    onBeforeUnmount: fn => cleanups.push(fn),
    can: () => true,
    session: reactive({ tenant: 't', user: 'alice', token: 'token-a' }),
    permissionState: reactive({ accessVersion: 'v1' }),
    api: async () => ({}),
    apiBlob: async (...args) => {
      requests.push(args)
      return new Blob(['image'], { type: 'image/png' })
    },
    URL: {
      createObjectURL: blob => {
        const url = `blob:test-${urls.length}`
        urls.push({ url, blob })
        return url
      },
      revokeObjectURL: url => revoked.push(url)
    },
    setInterval: () => 1,
    clearInterval: () => {},
    ...overrides
  }
  const script = setupScript(new URL('../src/components/AlarmMediaPanel.vue', import.meta.url))
  const component = scope.run(() => new Function('context', `with(context){ ${script}; return { load,retry,state,media } }`)(context))
  const dispose = () => {
    cleanups.forEach(fn => fn())
    scope.stop()
  }
  t.after(dispose)
  return { component, context, props, urls, revoked, requests, dispose }
}

test('media panel preserves independent screenshot and failed clip states without fetching external URLs', async t => {
  const { component: c, requests } = mediaPanel(t)
  assert.equal(c.media.value[0].stored, true)
  assert.equal(c.media.value[1].status, 'FAILED')
  assert.equal(c.media.value[1].error, '下载失败')
  assert.equal(requests.length, 0, 'opening detail must not download attachments')
  await c.load('clip')
  assert.equal(requests.length, 0, 'external URL is never passed to browser fetch')
  await c.load('snapshot')
  assert.equal(requests[0][0], '/api/v1/alarms/alarm-1/media/snapshot')
  assert.equal(c.state.snapshot.shown, true)
})

test('media object URLs are revoked on alarm and identity changes and disposal', async t => {
  const { component: c, context, props, revoked, dispose } = mediaPanel(t)
  await c.load('snapshot')
  props.alarm.alarmId = 'alarm-2'
  await nextTick()
  assert.deepEqual(revoked, ['blob:test-0'])
  assert.equal(c.state.snapshot.url, undefined)
  await c.load('snapshot')
  context.session.user = 'bob'
  await nextTick()
  assert.deepEqual(revoked, ['blob:test-0', 'blob:test-1'])
  await c.load('snapshot')
  dispose()
  assert.deepEqual(revoked, ['blob:test-0', 'blob:test-1', 'blob:test-2'])
})

test('late attachment response cannot populate another alarm and unsupported MIME is rejected', async t => {
  const pending = deferred()
  const { component: c, props, urls } = mediaPanel(t, { apiBlob: () => pending.promise })
  const load = c.load('snapshot')
  props.alarm.alarmId = 'alarm-2'
  await nextTick()
  pending.resolve(new Blob(['image'], { type: 'image/png' }))
  await load
  assert.equal(urls.length, 0)
  assert.equal(mediaHelpers.mediaMIMEAllowed('snapshot', 'image/svg+xml'), false)
  assert.equal(mediaHelpers.mediaMIMEAllowed('snapshot', 'text/html'), false)
  assert.equal(mediaHelpers.mediaMIMEAllowed('clip', 'video/mp4'), true)
})

test('waiting records prefill the exact binding and are retried after it is saved', async t => {
  const calls = []
  const { component: c } = view(t, {
    externalApi: {
      list: async kind => (kind === 'endpoints' ? { items: [{ id: 'e1', sourceId: 's1', kind: 'video_alarm' }] } : { items: [], total: 0 }),
      save: async (...args) => {
        calls.push(['save', ...args])
      },
      action: async (...args) => {
        calls.push(['action', ...args])
      }
    }
  })
  c.view.value = 'runs'
  const record = {
    id: 'r1',
    revision: 3,
    sourceId: 's1',
    endpointId: 'e1',
    status: 'WAITING_BINDING',
    body: { event: { objectId: 'cam-9' } }
  }
  const actions = c.rowActions(record)
  assert.equal(actions.find(action => action.key === 'bind').label, '关联并重试')
  assert.equal(
    actions.some(action => action.key === 'retry'),
    false
  )
  c.bindRecord(record)
  assert.equal(c.editorKind.value, 'bindings')
  assert.deepEqual(
    { sourceId: c.editorValue.value.sourceId, externalId: c.editorValue.value.externalId },
    { sourceId: 's1', externalId: 'cam-9' }
  )
  await c.save({ ...c.editorValue.value, targetId: 'camera-1' })
  assert.deepEqual(
    calls.map(call => call[0] + ':' + call[1]),
    ['save:bindings', 'action:records']
  )
  assert.deepEqual(calls[1].slice(1), ['records', 'r1', 'retry', { revision: 3, useCurrentMapping: false }])
})
