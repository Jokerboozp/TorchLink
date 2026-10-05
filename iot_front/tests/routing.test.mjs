import test from 'node:test'
import assert from 'node:assert/strict'
import { parsePath, pathFor } from '../src/routing.js'

const pages = { dashboard: {}, alarms: {}, opsOverview: {}, notifications: {} }

test('menu pages and alarm details round-trip through the address', () => {
  assert.equal(pathFor('opsOverview'), '/ops-overview')
  assert.deepEqual(parsePath('/ops-overview', pages), { page: 'opsOverview', detail: null })
  assert.equal(pathFor('alarms', { alarmId: 'alarm/1 x' }), '/alarms/alarm%2F1%20x')
  assert.deepEqual(parsePath('/alarms/alarm%2F1%20x', pages), { page: 'alarms', detail: { alarmId: 'alarm/1 x' } })
})

test('simple navigation targets survive refresh; complex objects stay out of the address', () => {
  const path = pathFor('raw', { deviceId: 'device 1', rawMessageId: 'raw/1', range: { from: 'now-6h' } })
  assert.equal(path, '/raw?deviceId=device+1&rawMessageId=raw%2F1')
  const [pathname, search] = path.split('?')
  assert.deepEqual(parsePath(pathname, { raw: {} }, '?' + search), { page: 'raw', detail: { deviceId: 'device 1', rawMessageId: 'raw/1' } })
  assert.deepEqual(parsePath('/alarms/a1', pages, '?deviceId=d1&other=x'), { page: 'alarms', detail: { alarmId: 'a1', deviceId: 'd1' } })
  assert.equal(pathFor('rules', { ruleDraft: { name: 'x' } }), '/rules')
})

test('unknown or malformed paths fall back to the first permitted page', () => {
  for (const path of ['/', '', '/missing', '/%E0%A4%A', '/constructor', '/__proto__']) {
    assert.equal(parsePath(path, pages).page, '', path)
  }
})

test('list filters persist in the address and per-user session storage', async () => {
  const { nextTick, ref } = await import('vue')
  const { withPageState } = await import('../src/routing.js')
  const memory = () => {
    const values = new Map()
    return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, String(value)), values }
  }
  const location = { pathname: '/alarms', search: '?s.status=ACTIVE&s.page=3', hash: '' }
  const history = {
    state: null,
    replaceState(_, __, next) {
      const [path, query = ''] = next.split('?')
      location.pathname = path
      location.search = query ? `?${query}` : ''
    }
  }
  Object.assign(globalThis, { window: { location, history }, sessionStorage: memory(), localStorage: memory() })
  localStorage.setItem('iot_tenant', 'tenant-a')
  localStorage.setItem('iot_user', 'alice')
  try {
    const { usePageState } = await import('../src/composables/usePageState.js')
    const status = ref('')
    const page = ref(1)
    const keyword = ref('')
    assert.equal(usePageState('alarms', { status, page, keyword }).restored, true)
    assert.equal(status.value, 'ACTIVE')
    assert.equal(page.value, 3)
    keyword.value = '东区'
    page.value = 1
    await nextTick()
    // 默认值不写入地址；会话存储按租户与用户区分。
    assert.equal(new URLSearchParams(location.search).get('s.keyword'), '东区')
    assert.equal(new URLSearchParams(location.search).has('s.page'), false)
    assert.deepEqual(JSON.parse(sessionStorage.getItem('iot:page-state:tenant-a:alice:alarms')), {
      status: 'ACTIVE',
      page: 1,
      keyword: '东区'
    })

    // 切换菜单后地址没有筛选参数时从会话存储恢复；其他用户读不到。
    location.search = ''
    const again = { status: ref(''), page: ref(1), keyword: ref('') }
    usePageState('alarms', again)
    assert.equal(again.keyword.value, '东区')
    localStorage.setItem('iot_user', 'bob')
    const other = { status: ref(''), page: ref(1), keyword: ref('') }
    location.search = ''
    assert.equal(usePageState('alarms', other).restored, false)
    assert.equal(other.keyword.value, '')

    // 对象与数组状态（如时间范围）可以是响应式对象，恢复时按 JSON 读取。
    const { reactive } = await import('vue')
    location.search = '?s.range=' + encodeURIComponent(JSON.stringify({ from: 'now-6h', to: 'now' }))
    const range = ref(reactive({ from: 'now-1h', to: 'now' }))
    usePageState('opsMetrics', { range })
    assert.deepEqual({ ...range.value }, { from: 'now-6h', to: 'now' })

    // 刷新改写地址时保留页面状态参数。
    assert.equal(withPageState('/alarms?deviceId=d1', '?s.status=ACTIVE&x=1'), '/alarms?deviceId=d1&s.status=ACTIVE')
    assert.equal(withPageState('/alarms', '?x=1'), '/alarms')
  } finally {
    delete globalThis.window
    delete globalThis.sessionStorage
    delete globalThis.localStorage
  }
})
