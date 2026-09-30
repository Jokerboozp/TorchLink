import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { link, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { after, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { createGateway, loadPluginCatalog } from './gateway.mjs'
import { apply as applyPolicy, forwardAssistantText } from './iot-ops-plugin.mjs'

const deploymentDir = dirname(fileURLToPath(import.meta.url))
const gatewayToken = 'test-harness-token-that-is-at-least-32-chars'
const cloudBaseUrl = 'https://model.example/v1'
const cloudModel = 'cloud-chat-test'
const openedGateways = []
const openedServers = []
const temporaryDirectories = []

after(async () => {
  await Promise.allSettled(openedGateways.splice(0).map(gateway => gateway.close()))
  await Promise.allSettled(openedServers.splice(0).map(server => new Promise(resolve => server.close(resolve))))
  await Promise.allSettled(temporaryDirectories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

function result(finalResponse = '') {
  return {
    finalResponse,
    events: [{ type: 'turn/end', data: { reason: { kind: 'completed' } } }],
  }
}

async function startGateway(factory, options = {}) {
  const gateway = createGateway({
    gatewayToken,
    pluginDir: join(deploymentDir, 'plugins'),
    patchFile: join(deploymentDir, 'cordis.yml'),
    runtimeBin: fileURLToPath(import.meta.url),
    sdkClientModule: fileURLToPath(import.meta.url),
    runtimeCwd: deploymentDir,
    workspace: deploymentDir,
    sessionRoot: join(tmpdir(), 'iot-harness-test-sessions'),
    proxyPort: 0,
    modelProvider: 'openai-compatible',
    baseURL: cloudBaseUrl,
    apiKey: '',
    model: cloudModel,
    harnessFactory: factory,
    ...options,
  })
  openedGateways.push(gateway)
  const address = await gateway.listen(0, '127.0.0.1')
  return { gateway, baseUrl: `http://127.0.0.1:${address.port}` }
}

function requestBody(overrides = {}) {
  return {
    runId: 'run-1',
    conversationId: 'tenant-user-conversation-hash',
    workflowId: 'ops-assistant',
    question: '检查当前高等级告警',
    mcpUrl: 'http://platform-api:8080/mcp/harness',
    model: cloudModel,
    maxTokens: 1200,
    ...overrides,
  }
}

async function chat(baseUrl, body, bearer = 'short-lived-mcp-jwt') {
  return fetch(`${baseUrl}/v1/chat/stream`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${bearer}`,
      'content-type': 'application/json',
      'x-iot-harness-token': gatewayToken,
    },
    body: JSON.stringify(body),
  })
}

async function ndjson(response) {
  const payload = await response.text()
  return { payload, events: payload.trim().split('\n').filter(Boolean).map(line => JSON.parse(line)) }
}

async function waitUntil(check) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (await check()) return
    await new Promise(resolve => setTimeout(resolve, 10))
  }
  assert.fail('condition did not become true')
}

const controlHeaders = { 'x-iot-harness-token': gatewayToken }

async function snapshotFixture(options = {}, factory = async () => ({ run: async () => result(), close: async () => {} })) {
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-snapshot-'))
  temporaryDirectories.push(root)
  const pluginDir = join(root, 'plugins')
  const sessionRoot = join(root, 'sessions')
  const workspace = join(root, 'workspace')
  const harnessHome = join(root, 'runtime-home')
  await Promise.all([pluginDir, sessionRoot, workspace, harnessHome].map(path => mkdir(path)))
  const seed = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8'))
  await writeFile(join(pluginDir, `${seed.id}.json`), JSON.stringify(seed))
  const directory = join(sessionRoot, '--data-workspace--', 'iot-test-session')
  await mkdir(directory, { recursive: true })
  const sessionFile = join(directory, 'session.v1.jsonl')
  const sessionContent = `${JSON.stringify({ type: 'session', version: 1, id: 'iot-test-session' })}\n${JSON.stringify({ type: 'text', text: '会话备份内容' })}\n`
  await writeFile(sessionFile, sessionContent)
  return { root, pluginDir, sessionRoot, workspace, harnessHome, directory, sessionFile, sessionContent, seed,
    ...await startGateway(factory, { pluginDir, sessionRoot, workspace, harnessHome, ...options }) }
}

test('backup snapshot requires the control token and refuses caller paths and methods', async () => {
  const { baseUrl } = await snapshotFixture()
  assert.equal((await fetch(`${baseUrl}/v1/backup/snapshot`)).status, 401)
  assert.equal((await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: { 'x-iot-harness-token': 'wrong-control-token' } })).status, 401)
  const traversal = await fetch(`${baseUrl}/v1/backup/snapshot?path=../../etc/passwd`, { headers: controlHeaders })
  assert.equal(traversal.status, 400)
  assert.equal((await traversal.json()).error.code, 'QUERY_NOT_ALLOWED')
  assert.equal((await fetch(`${baseUrl}/v1/backup/snapshot/..%2f..%2fetc%2fpasswd`, { headers: controlHeaders })).status, 404)
  assert.equal((await fetch(`${baseUrl}/v1/backup/snapshot`, { method: 'POST', headers: controlHeaders, body: '{}' })).status, 405)
})

test('backup snapshot roundtrips dynamic Agent manifests and committed sessions without runtime secrets', async () => {
  const fixture = await snapshotFixture({ apiKey: 'provider-secret-never-backed-up' })
  const { baseUrl, root, seed, sessionContent, directory, workspace, harnessHome } = fixture
  const manifest = { ...seed, id: 'dynamic:backup-agent', name: '动态备份 Agent', enabled: false,
    persona: '保留完整 Agent 提示词', allowedTools: ['mcp__iot__query_system_overview'] }
  const created = await fetch(`${baseUrl}/v1/plugins`, { method: 'POST', headers: { ...controlHeaders, 'content-type': 'application/json' }, body: JSON.stringify(manifest) })
  assert.equal(created.status, 201)
  const privateBytes = 'provider-secret-never-backed-up\nserver-credential-never-backed-up'
  await Promise.all([
    writeFile(join(harnessHome, 'provider.json'), privateBytes),
    writeFile(join(workspace, '.env'), privateBytes),
    writeFile(join(root, '.env'), privateBytes),
    writeFile(join(directory, 'session.lock'), 'not-a-restorable-lease'),
    writeFile(join(directory, 'session.v2.jsonl.aabbcc.tmp'), 'incomplete generation'),
    writeFile(join(directory, 'provider.json'), privateBytes),
    writeFile(join(directory, 'session.v2.jsonl.zstd'), Buffer.from([0x28, 0xb5, 0x2f, 0xfd, 1, 2, 3])),
  ])
  const response = await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
  assert.equal(response.status, 200)
  assert.equal(response.headers.get('cache-control'), 'no-store')
  const snapshot = await response.json()
  assert.deepEqual(Object.keys(snapshot).sort(), ['createdAt', 'entries', 'fileCount', 'formatVersion', 'totalBytes'])
  assert.equal(snapshot.formatVersion, 1)
  assert.ok(Number.isFinite(Date.parse(snapshot.createdAt)))
  assert.equal(snapshot.fileCount, 4)
  assert.equal(snapshot.totalBytes, snapshot.entries.reduce((sum, entry) => sum + entry.size, 0))
  const restored = join(root, 'restored')
  for (const entry of snapshot.entries) {
    assert.deepEqual(Object.keys(entry).sort(), ['base64', 'path', 'sha256', 'size'])
    const bytes = Buffer.from(entry.base64, 'base64')
    assert.equal(bytes.length, entry.size)
    assert.equal(createHash('sha256').update(bytes).digest('hex'), entry.sha256)
    assert.equal(bytes.includes(Buffer.from('provider-secret-never-backed-up')), false)
    assert.equal(bytes.includes(Buffer.from('server-credential-never-backed-up')), false)
    assert.equal(bytes.includes(Buffer.from(gatewayToken)), false)
    const target = join(restored, entry.path)
    await mkdir(dirname(target), { recursive: true })
    await writeFile(target, bytes)
  }
  assert.deepEqual(snapshot.entries.map(entry => entry.path), [
    'plugins/dynamic:backup-agent.json', 'plugins/ops-assistant.json',
    'sessions/--data-workspace--/iot-test-session/session.v1.jsonl',
    'sessions/--data-workspace--/iot-test-session/session.v2.jsonl.zstd',
  ])
  const restoredCatalog = await loadPluginCatalog(join(restored, 'plugins'))
  assert.deepEqual(restoredCatalog.find(plugin => plugin.id === manifest.id), manifest)
  assert.equal(await readFile(join(restored, 'sessions', '--data-workspace--', 'iot-test-session', 'session.v1.jsonl'), 'utf8'), sessionContent)
})

test('backup snapshot refuses symlinks, hardlinks, unsafe names and substituted roots', async () => {
  for (const kind of ['file-symlink', 'directory-symlink', 'hardlink', 'unsafe-name', 'root-symlink']) {
    const fixture = await snapshotFixture()
    const outside = join(fixture.root, 'private-secret.jsonl')
    await writeFile(outside, 'secret outside snapshot roots')
    if (kind === 'file-symlink') {
      await symlink(outside, join(fixture.directory, 'session.v2.jsonl'))
    } else if (kind === 'directory-symlink') {
      await symlink(fixture.harnessHome, join(fixture.sessionRoot, 'linked-project'), 'dir')
    } else if (kind === 'hardlink') {
      await link(outside, join(fixture.directory, 'session.v2.jsonl'))
    } else if (kind === 'unsafe-name') {
      await writeFile(join(fixture.directory, '..\\private-secret.jsonl'), 'unsafe separator')
    } else {
      await rm(fixture.sessionRoot, { recursive: true })
      await symlink(fixture.harnessHome, fixture.sessionRoot, 'dir')
    }
    const response = await fetch(`${fixture.baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
    assert.equal(response.status, 422, kind)
    const body = await response.json()
    assert.equal(body.error.code, 'SNAPSHOT_UNSAFE_PATH', kind)
    assert.equal(JSON.stringify(body).includes('secret outside snapshot roots'), false)
  }
})

test('backup snapshot enforces decoded byte and file count limits before returning any entries', async () => {
  for (const options of [{ backupMaxBytes: 1 }, { backupMaxFiles: 1 }]) {
    const { baseUrl } = await snapshotFixture(options)
    const response = await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
    assert.equal(response.status, 413)
    const body = await response.json()
    assert.equal(body.error.code, 'SNAPSHOT_LIMIT_EXCEEDED')
    assert.equal(body.entries, undefined)
  }
})

test('backup snapshot rejects concurrent session rewrites and newly published files, then retries cleanly', async () => {
  for (const kind of ['rewrite', 'create']) {
    let changed = false
    let fixture
    fixture = await snapshotFixture({ backupAfterRead: async path => {
      if (changed || !path.startsWith('sessions/')) return
      changed = true
      if (kind === 'rewrite') await writeFile(fixture.sessionFile, 'concurrent new session content\n')
      else await writeFile(join(fixture.directory, 'session.v2.jsonl'), 'newly published generation\n')
    } })
    const response = await fetch(`${fixture.baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
    assert.equal(response.status, 409, kind)
    assert.equal(response.headers.get('retry-after'), '1')
    const body = await response.json()
    assert.equal(body.error.code, 'SNAPSHOT_CHANGED')
    assert.equal(body.entries, undefined)
    const retry = await fetch(`${fixture.baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
    assert.equal(retry.status, 200, kind)
    const copied = await retry.json()
    const entry = copied.entries.find(item => item.path.endsWith(kind === 'rewrite' ? 'session.v1.jsonl' : 'session.v2.jsonl'))
    assert.equal(Buffer.from(entry.base64, 'base64').toString('utf8'), kind === 'rewrite' ? 'concurrent new session content\n' : 'newly published generation\n')
  }
})

test('backup snapshot freezes new runs and manifest mutations only while copying', async () => {
  let reachedCopy
  let releaseCopy
  const copying = new Promise(resolve => { reachedCopy = resolve })
  const released = new Promise(resolve => { releaseCopy = resolve })
  let pause = true
  const { baseUrl, seed } = await snapshotFixture({ backupAfterRead: async () => {
    if (!pause) return
    pause = false
    reachedCopy()
    await released
  } })
  const pending = fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
  await copying
  const manifest = { ...seed, id: 'new-agent' }
  const post = () => fetch(`${baseUrl}/v1/plugins`, { method: 'POST', headers: { ...controlHeaders, 'content-type': 'application/json' }, body: JSON.stringify(manifest) })
  for (const operation of [post(), chat(baseUrl, requestBody()), fetch(`${baseUrl}/v1/plugins/new-agent`, { method: 'DELETE', headers: controlHeaders }), fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })]) {
    const response = await operation
    assert.equal(response.status, 409)
    assert.equal(response.headers.get('retry-after'), '1')
    assert.equal((await response.json()).error.code, 'SNAPSHOT_BUSY')
  }
  releaseCopy()
  assert.equal((await pending).status, 200)
  assert.equal((await post()).status, 201)
  assert.equal((await ndjson(await chat(baseUrl, requestBody()))).events.at(-1).type, 'run.completed')
})

test('backup snapshot refuses active runtimes without stopping them and succeeds once they finish', async () => {
  let completeRun
  let closeCount = 0
  const { baseUrl } = await snapshotFixture({}, async () => ({
    run: () => new Promise(resolve => { completeRun = () => resolve(result('complete')) }),
    close: async () => { closeCount++ },
  }))
  const stream = await chat(baseUrl, requestBody({ tenantId: 'tenant-a', actor: 'admin' }))
  await waitUntil(() => completeRun !== undefined)
  const busy = await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
  assert.equal(busy.status, 409)
  assert.equal((await busy.json()).error.code, 'SNAPSHOT_BUSY')
  assert.equal(closeCount, 0)
  completeRun()
  assert.equal((await ndjson(stream)).events.at(-1).type, 'run.completed')
  assert.equal((await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })).status, 200)
  assert.equal(closeCount, 0)
})

test('backup snapshot remains blocked when an idle runtime cannot confirm shutdown', async () => {
  const { baseUrl } = await snapshotFixture({}, async () => ({
    run: async () => result('complete'), close: async () => { throw new Error('runtime exit was not confirmed') },
  }))
  assert.equal((await ndjson(await chat(baseUrl, requestBody()))).events.at(-1).type, 'run.completed')
  const updated = await fetch(`${baseUrl}/v1/provider`, { method: 'PUT',
    headers: { ...controlHeaders, 'content-type': 'application/json' },
    body: JSON.stringify({ provider: 'openai-compatible', baseUrl: 'http://new-provider:8080/v1', model: 'new-model', apiKey: '' }),
  })
  assert.equal(updated.status, 200)
  const response = await fetch(`${baseUrl}/v1/backup/snapshot`, { headers: controlHeaders })
  assert.equal(response.status, 409)
  assert.equal((await response.json()).error.code, 'SNAPSHOT_BUSY')
})

test('run management isolates tenants and stops a hung runtime before releasing capacity', async () => {
  let closeCount = 0
  let releaseClose
  const closing = new Promise(resolve => { releaseClose = resolve })
  const { baseUrl } = await startGateway(async () => ({
    run: () => new Promise(() => {}),
    close: async () => { closeCount++; await closing },
  }))
  const headers = { 'x-iot-harness-token': gatewayToken, 'x-iot-tenant-id': 'tenant-a' }
  const list = async () => (await fetch(`${baseUrl}/v1/runs`, { headers })).json()
  const response = await chat(baseUrl, requestBody({ tenantId: 'tenant-a', actor: 'admin', question: 'private prompt' }))
  await waitUntil(async () => (await list()).items[0]?.status === 'running')
  const listed = await list()
  assert.equal(listed.items[0].actor, 'admin')
  assert.equal(listed.items[0].workflowName.length > 0, true)
  assert.equal(JSON.stringify(listed).includes('private prompt'), false)
  assert.equal(JSON.stringify(listed).includes(gatewayToken), false)
  assert.equal((await fetch(`${baseUrl}/v1/runs`)).status, 401)
  const other = { ...headers, 'x-iot-tenant-id': 'tenant-b' }
  assert.deepEqual((await (await fetch(`${baseUrl}/v1/runs`, { headers: other })).json()).items, [])
  assert.equal((await fetch(`${baseUrl}/v1/runs/run-1/stop`, { method:'POST', headers:other })).status, 404)
  assert.equal(closeCount, 0)
  assert.equal((await fetch(`${baseUrl}/v1/runs/run-1/stop`, { method:'POST', headers })).status, 202)
  assert.equal((await fetch(`${baseUrl}/v1/runs/run-1/stop`, { method:'POST', headers })).status, 202)
  await waitUntil(() => closeCount === 1)
  assert.equal((await list()).items[0].status, 'stopping')
  assert.equal((await (await fetch(`${baseUrl}/health`)).json()).activeRuns, 1)
  const configure = () => fetch(`${baseUrl}/v1/provider`, { method:'PUT', headers:{...headers,'content-type':'application/json'}, body:JSON.stringify({provider:'openai-compatible',baseUrl:cloudBaseUrl,model:'new-model',apiKey:''}) })
  assert.equal((await configure()).status, 409)
  releaseClose()
  const { events } = await ndjson(response)
  assert.equal(events.at(-1).code, 'RUN_STOPPED')
  assert.equal(events.some(event => event.type === 'run.completed'), false)
  assert.deepEqual((await list()).items, [])
  assert.equal((await (await fetch(`${baseUrl}/health`)).json()).activeRuns, 0)
  assert.equal((await configure()).status, 200)
  assert.equal((await fetch(`${baseUrl}/v1/runs/run-1/stop`, { method:'POST', headers })).status, 404)
})

test('queued runs can be stopped without interrupting another run in the conversation', async () => {
  let executions = 0
  const { baseUrl } = await startGateway(async () => ({run: () => { executions++; return new Promise(() => {}) }, close: async () => {}}))
  const headers = { 'x-iot-harness-token':gatewayToken, 'x-iot-tenant-id':'tenant-a' }
  const first = await chat(baseUrl, requestBody({tenantId:'tenant-a',actor:'user'}))
  const queued = chat(baseUrl, requestBody({tenantId:'tenant-a',actor:'user',runId:'queued'}))
  await waitUntil(async () => (await (await fetch(`${baseUrl}/v1/runs`,{headers})).json()).items.some(item => item.runId === 'queued' && item.status === 'queued'))
  assert.equal((await fetch(`${baseUrl}/v1/runs/queued/stop`, {method:'POST',headers})).status, 202)
  const queuedResponse = await queued
  assert.equal(queuedResponse.status, 409)
  assert.equal((await queuedResponse.json()).error.code, 'RUN_STOPPED')
  assert.equal(executions, 1)
  assert.equal((await (await fetch(`${baseUrl}/health`)).json()).activeRuns, 1)
  await fetch(`${baseUrl}/v1/runs/run-1/stop`, {method:'POST',headers})
  await ndjson(first)
})

test('stopping during runtime initialization closes the late process without running a prompt', async () => {
  let releaseFactory
  let closed = 0
  let executed = 0
  const { baseUrl } = await startGateway(() => new Promise(resolve => { releaseFactory = resolve }))
  const headers = {'x-iot-harness-token':gatewayToken,'x-iot-tenant-id':'tenant-a'}
  const response = await chat(baseUrl, requestBody({tenantId:'tenant-a',actor:'admin'}))
  await waitUntil(() => releaseFactory !== undefined)
  await fetch(`${baseUrl}/v1/runs/run-1/stop`,{method:'POST',headers})
  assert.equal((await (await fetch(`${baseUrl}/v1/runs`,{headers})).json()).items[0].status,'stopping')
  releaseFactory({run: async () => {executed++;return result()},close: async () => {closed++}})
  assert.equal((await ndjson(response)).events.at(-1).code,'RUN_STOPPED')
  assert.equal(closed,1)
  assert.equal(executed,0)
})

test('failed process teardown stays visible and cannot falsely release model switching', async () => {
  const { baseUrl } = await startGateway(async () => ({run:()=>new Promise(()=>{}),close:async()=>{throw new Error('process did not exit')}}))
  const headers={'x-iot-harness-token':gatewayToken,'x-iot-tenant-id':'tenant-a'}
  const response=await chat(baseUrl,requestBody({tenantId:'tenant-a',actor:'admin'}))
  await waitUntil(async()=>(await(await fetch(`${baseUrl}/v1/runs`,{headers})).json()).items[0]?.status==='running')
  await fetch(`${baseUrl}/v1/runs/run-1/stop`,{method:'POST',headers})
  assert.equal((await ndjson(response)).events.at(-1).code,'RUN_STOP_FAILED')
  assert.equal((await(await fetch(`${baseUrl}/v1/runs`,{headers})).json()).items[0].status,'stop_failed')
  assert.equal((await(await fetch(`${baseUrl}/health`)).json()).activeRuns,1)
  assert.equal((await fetch(`${baseUrl}/v1/provider`,{method:'PUT',headers:{...headers,'content-type':'application/json'},body:'{}'})).status,409)
})

test('catalog is manifest-driven and exposes capabilities without policy internals', async () => {
  const plugins = await loadPluginCatalog(join(deploymentDir, 'plugins'))
  assert.deepEqual(plugins.map(plugin => plugin.id), ['alarm-handler', 'data-quality-analyst', 'device-health-inspector', 'duty-handover', 'maintenance-investment-advisor', 'maintenance-outcome-reviewer', 'monitoring-continuity-reviewer', 'ops-assistant', 'protocol-assistant', 'response-reviewer', 'rule-drafter', 'rule-policy-analyst', 'system-observer'])
  assert.ok(plugins.every(plugin => plugin.capabilities.length > 0))

  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }))
  const unauthorized = await fetch(`${baseUrl}/v1/plugins`)
  assert.equal(unauthorized.status, 401)
  const response = await fetch(`${baseUrl}/v1/plugins`, {
    headers: { 'x-iot-harness-token': gatewayToken },
  })
  assert.equal(response.status, 200)
  const body = await response.json()
  assert.equal(body.items.length, plugins.length)
  assert.ok(body.items.every(plugin => Array.isArray(plugin.capabilities)))
  assert.ok(body.items.every(plugin => plugin.persona === undefined && plugin.allowedTools === undefined))
})

test('admin API atomically creates a persistent Agent manifest that is immediately listed', async () => {
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-dynamic-agent-'))
  temporaryDirectories.push(root)
  const seed = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8'))
  seed.id = 'seed-agent'
  seed.name = 'Seed Agent'
  await writeFile(join(root, 'seed-agent.json'), JSON.stringify(seed))
  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }), { pluginDir: root })
  const manifest = {
    ...seed,
    id: 'dynamic-status-agent',
    name: 'Dynamic Status Agent',
    description: 'Created from the management UI',
    allowedTools: ['mcp__iot__query_system_overview'],
  }
  const created = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify(manifest),
  })
  assert.equal(created.status, 201)
  assert.equal((await created.json()).id, manifest.id)
  const persisted = JSON.parse(await readFile(join(root, `${manifest.id}.json`), 'utf8'))
  assert.equal(persisted.name, manifest.name)
  const listed = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.deepEqual((await listed.json()).items.map(plugin => plugin.id), ['dynamic-status-agent', 'seed-agent'])
  const adminListed = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(adminListed.status, 200)
  const adminBody = await adminListed.json()
  assert.equal(adminBody.count, 2)
  assert.equal(adminBody.items.find(plugin => plugin.id === manifest.id).persona, manifest.persona)
  assert.deepEqual(adminBody.items.find(plugin => plugin.id === manifest.id).allowedTools, manifest.allowedTools)
  const disabled = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ ...manifest, enabled: false }),
  })
  assert.equal(disabled.status, 200)
  const publicAfterDisable = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.deepEqual((await publicAfterDisable.json()).items.map(plugin => plugin.id), ['seed-agent'])
  const adminAfterDisable = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal((await adminAfterDisable.json()).items.find(plugin => plugin.id === manifest.id).enabled, false)
  const deleted = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(deleted.status, 200)
  const missing = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(missing.status, 404)
  const immutable = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST', headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ ...manifest, id: 'ops-assistant' }),
  })
  assert.equal(immutable.status, 409)
  const immutableDelete = await fetch(`${baseUrl}/v1/plugins/ops-assistant`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(immutableDelete.status, 409)
})

test('gateway refuses internal tokens shorter than 32 characters', () => {
  assert.throws(
    () => createGateway({ gatewayToken: 'too-short' }),
    /32 to 512/,
  )
})

test('gateway rejects an unsupported model provider', () => {
  assert.throws(
    () => createGateway({ gatewayToken, modelProvider: 'unsupported' }),
    /IOT_HARNESS_PROVIDER/,
  )
})

test('provider endpoint switches the resident runtime and redacts API keys', async () => {
  let factorySpec
  const { baseUrl } = await startGateway(async spec => {
    factorySpec = spec
    return { run: async () => result('provider switched'), close: async () => {} }
  })
  const initial = await fetch(`${baseUrl}/v1/provider`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(initial.status, 200)
  assert.deepEqual(await initial.json(), {
    provider: 'openai-compatible',
    baseUrl: cloudBaseUrl,
    model: cloudModel,
    apiKeyConfigured: false,
  })
  const updated = await fetch(`${baseUrl}/v1/provider`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ provider: 'deepseek-official', baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat', apiKey: 'provider-test-key' }),
  })
  assert.equal(updated.status, 200)
  const updatedBody = await updated.json()
  assert.deepEqual(updatedBody, {
    provider: 'deepseek-official',
    baseUrl: 'https://api.deepseek.com',
    model: 'deepseek-chat',
    apiKeyConfigured: true,
  })
  assert.equal(updatedBody.apiKey, undefined)
  const removed = await fetch(`${baseUrl}/v1/provider`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ provider: 'unsupported', baseUrl: cloudBaseUrl, model: cloudModel, apiKey: '' }),
  })
  assert.equal(removed.status, 422)
  const response = await chat(baseUrl, requestBody({ runId: 'provider-switched', model: 'legacy-model' }))
  assert.equal(response.status, 200)
  await response.text()
  assert.equal(factorySpec.provider, 'deepseek-official')
  assert.equal(factorySpec.baseUrl, 'https://api.deepseek.com')
  assert.equal(factorySpec.model, 'deepseek-chat')
  assert.equal(factorySpec.apiKey, 'provider-test-key')
})

test('manifest security ceiling rejects a write-capable tool', async () => {
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-manifest-'))
  temporaryDirectories.push(root)
  const valid = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8'))
  valid.id = 'unsafe-plugin'
  valid.allowedTools = ['mcp__iot__control_device']
  await writeFile(join(root, 'unsafe.json'), JSON.stringify(valid))
  await assert.rejects(() => loadPluginCatalog(root), /outside the read-only security ceiling/)
})

test('Cordis policy installs a monotonic global guard and prompt restriction', () => {
  let guard
  let created
  let restricted
  const ctx = {
    tools: { guard: callback => { guard = callback } },
    on: (name, callback) => {
      assert.ok(['agent/created', 'agent/assistant-stream'].includes(name))
      if (name === 'agent/created') created = callback
    },
  }
  applyPolicy(ctx, { allowedTools: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] })
  assert.equal(guard({ name: 'mcp__iot__query_alarm_list' }), undefined)
  assert.equal(guard({ name: 'mcp__iot__create_rule_draft' }), undefined)
  assert.equal(guard({ name: 'mcp__iot__control_device' }), 'tool not allowed')
  created({ agent: { ctx: { tools: { restrict: config => { restricted = config } } } } })
  assert.deepEqual(restricted, { allow: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] })
})

test('runtime stream bridge forwards only text with its owning session', () => {
  const frames = []
  const write = line => frames.push(JSON.parse(line))
  const agent = { session: { id: 'session-one' } }
  for (const frame of [
    { type: 'start' },
    { type: 'chunk', chunk: { type: 'reasoning-delta', text: 'SECRET_REASONING' } },
    { type: 'chunk', chunk: { type: 'tool-call', arguments: 'SECRET_ARGUMENTS' } },
    { type: 'chunk', chunk: { type: 'text-delta', text: '' } },
    { type: 'chunk', chunk: { type: 'text-delta', text: '可见结论' } },
    { type: 'end' },
  ]) forwardAssistantText({ agent, frame }, write)
  assert.deepEqual(frames, [{ jsonrpc: '2.0', method: 'iot.text.delta', params: { sessionId: 'session-one', text: '可见结论' } }])
})

test('stream emits only the public NDJSON event vocabulary and suppresses reasoning/results', async () => {
  let factorySpec
  const { baseUrl } = await startGateway(async spec => {
    factorySpec = spec
    return {
      async run(_question, options) {
        const notify = event => options.onNotification({
          method: 'session.event',
          params: { sessionId: options.sessionId, event },
        })
        notify({ type: 'assistant/chunk', data: { chunk: { type: 'reasoning-delta', text: 'SECRET_REASONING' } } })
        options.onNotification({ method: 'iot.text.delta', params: { sessionId: 'other-session', text: 'SECRET_OTHER_SESSION' } })
        options.onNotification({ method: 'iot.text.delta', params: { sessionId: options.sessionId, text: '可见结论' } })
        notify({ type: 'tool/call', data: { callId: 'call-direct', name: 'mcp__iot__query_alarm_list', arguments: { secret: true } } })
        notify({ type: 'tool/result', data: { callId: 'call-direct', result: 'SECRET_TOOL_RESULT' } })
        notify({ type: 'tool/call', data: { callId: 'call-legacy', name: 'mcp__iot__query_knowledge_base' } })
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-legacy' }, content: [{ text: 'SECRET_LEGACY_RESULT' }] } } })
        notify({ type: 'tool/call', data: { callId: 'call-draft', name: 'mcp__iot__create_rule_draft' } })
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-draft' }, content: [{ type:'tool-result', isError:false, content:[{ type:'text', text:JSON.stringify({ kind:'ruleDraft', persisted:true, draft:{ id:'draft-1', name:'高温联动', conditions:[{ field:'temperature', operator:'>', value:80 }], actions:[{ type:'OPEN_CAMERA', cameraId:'camera-001' }], internalSecret:'MUST_NOT_LEAK' } }) }] }] } } })
        return result('fallback must not duplicate streamed text')
      },
      async close() {},
    }
  })
  const response = await chat(baseUrl, requestBody({ model: 'legacy-model' }))
  assert.equal(response.status, 200)
  const { payload, events } = await ndjson(response)
  assert.deepEqual(events.map(event => event.type), [
    'run.started',
    'text.delta',
    'tool.started',
    'tool.completed',
    'tool.started',
    'tool.completed',
    'tool.started',
    'tool.completed',
    'run.completed',
  ])
  assert.equal(events[3].success, true)
  assert.equal(events[5].success, true)
  assert.equal(events[7].data.clientAction.type, 'RULE_DRAFT_READY')
  assert.equal(events[7].data.clientAction.draft.actions[0].cameraId, 'camera-001')
  assert.equal(events[7].data.clientAction.persisted, true)
  assert.doesNotMatch(payload, /SECRET_REASONING|SECRET_TOOL_RESULT|SECRET_LEGACY_RESULT|SECRET_OTHER_SESSION|arguments/)
  assert.doesNotMatch(payload, /MUST_NOT_LEAK/)
  assert.equal(factorySpec.mcpToken, undefined)
  assert.equal(factorySpec.provider, 'openai-compatible')
  assert.equal(factorySpec.model, cloudModel)
  assert.match(factorySpec.proxyMcpUrl, /^http:\/\/127\.0\.0\.1:\d+\/mcp$/)
  assert.equal(factorySpec.runtimeAccessKey.length, 43)
  assert.ok(factorySpec.plugin.persona.includes('AI 运维助手'))
  assert.equal(factorySpec.patchFile, join(deploymentDir, 'cordis.yml'))
  assert.ok(factorySpec.harnessHome.endsWith(`\\${factorySpec.sessionId}`) || factorySpec.harnessHome.endsWith(`/${factorySpec.sessionId}`))
})

test('workflow token ceiling caps provider output limits when switching assistants', async () => {
  const specs = []
  const { baseUrl } = await startGateway(async spec => {
    specs.push(spec)
    return { run: async () => result('状态查询完成'), close: async () => {} }
  })
  const cases = [
    { workflowId: 'ops-assistant', maxTokens: 8192, expected: 8192 },
    { workflowId: 'system-observer', maxTokens: 8192, expected: 4096 },
    { workflowId: 'system-observer', maxTokens: 1024, expected: 1024 },
    { workflowId: 'system-observer', maxTokens: undefined, expected: 4096 },
  ]
  for (const [index, { workflowId, maxTokens, expected }] of cases.entries()) {
    const response = await chat(baseUrl, requestBody({ runId: `token-limit-${index}`, workflowId, maxTokens }))
    assert.equal(response.status, 200)
    const { events } = await ndjson(response)
    assert.equal(events.at(-1).type, 'run.completed')
    assert.equal(specs.at(-1).maxTokens, expected)
  }
  const factoryCalls = specs.length
  for (const maxTokens of [0, -1, 1.5, '8192', 262145]) {
    const response = await chat(baseUrl, requestBody({ workflowId: 'system-observer', maxTokens }))
    assert.equal(response.status, 422)
    assert.equal((await response.json()).error.code, 'MAX_TOKENS_INVALID')
  }
  assert.equal(specs.length, factoryCalls)
})

test('upstream MCP path is exactly /mcp/harness', async () => {
  let factoryCalls = 0
  const { baseUrl } = await startGateway(async () => {
    factoryCalls += 1
    return { run: async () => result(), close: async () => {} }
  })
  const wrong = await chat(baseUrl, requestBody({ mcpUrl: 'http://platform-api:8080/mcp' }))
  assert.equal(wrong.status, 422)
  const correct = await chat(baseUrl, requestBody({ runId: 'run-correct' }))
  assert.equal(correct.status, 200)
  assert.equal(factoryCalls, 1)
})

test('loopback proxy rotates short-lived JWT without rebuilding the resident conversation', async () => {
  const authorizations = []
  const upstream = createServer(async (request, response) => {
    authorizations.push(request.headers.authorization)
    for await (const _chunk of request) { /* drain */ }
    response.writeHead(200, { 'content-type': 'application/json', 'mcp-session-id': 'test-session' })
    response.end('{"jsonrpc":"2.0","id":1,"result":{}}')
  })
  openedServers.push(upstream)
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve))
  const upstreamAddress = upstream.address()
  const mcpUrl = `http://127.0.0.1:${upstreamAddress.port}/mcp/harness`
  let factoryCalls = 0
  let sdkSessionId
  const { baseUrl } = await startGateway(async spec => {
    factoryCalls += 1
    return {
      async run(_question, options) {
        sdkSessionId ??= options.sessionId
        assert.equal(options.sessionId, sdkSessionId)
        const proxied = await fetch(spec.proxyMcpUrl, {
          method: 'POST',
          headers: {
            accept: 'application/json',
            'content-type': 'application/json',
            'x-iot-runtime-key': spec.runtimeAccessKey,
          },
          body: '{"jsonrpc":"2.0","id":1,"method":"tools/list"}',
        })
        assert.equal(proxied.status, 200)
        return result('ok')
      },
      async close() {},
    }
  }, { allowedMcpOrigins: new URL(mcpUrl).origin })

  const first = await chat(baseUrl, requestBody({ runId: 'run-jwt-1', mcpUrl }), 'jwt-generation-one')
  assert.equal(first.status, 200)
  await first.text()
  const second = await chat(baseUrl, requestBody({ runId: 'run-jwt-2', mcpUrl }), 'jwt-generation-two')
  assert.equal(second.status, 200)
  await second.text()

  assert.equal(factoryCalls, 1)
  assert.deepEqual(authorizations, ['Bearer jwt-generation-one', 'Bearer jwt-generation-two'])
})

test('requests sharing a conversation are serialized through one resident runtime', async () => {
  let running = 0
  let maximumRunning = 0
  let factoryCalls = 0
  const { baseUrl } = await startGateway(async () => {
    factoryCalls += 1
    return {
      async run() {
        running += 1
        maximumRunning = Math.max(maximumRunning, running)
        await new Promise(resolve => setTimeout(resolve, 30))
        running -= 1
        return result('done')
      },
      async close() {},
    }
  })
  const [first, second] = await Promise.all([
    chat(baseUrl, requestBody({ runId: 'run-serial-1' })),
    chat(baseUrl, requestBody({ runId: 'run-serial-2' })),
  ])
  assert.equal(first.status, 200)
  assert.equal(second.status, 200)
  await Promise.all([first.text(), second.text()])
  assert.equal(maximumRunning, 1)
  assert.equal(factoryCalls, 1)
})

test('DeepSeek without a key stays healthy but rejects workflows before launching a runtime', async () => {
  let launched = false
  const { baseUrl } = await startGateway(async () => { launched = true }, {
    modelProvider: 'deepseek-official', model: 'deepseek-flash',
    baseURL: 'https://api.deepseek.com', apiKey: '',
  })
  const health = await fetch(`${baseUrl}/health`)
  assert.equal(health.status, 200)
  assert.equal((await health.json()).deepseekConfigured, false)
  const response = await chat(baseUrl, requestBody({ model: 'deepseek-flash' }))
  assert.equal(response.status, 503)
  assert.match(await response.text(), /API_KEY_REQUIRED/)
  assert.equal(launched, false)
})

test('duty handover is immutable and exposes only its resource-bound snapshot and knowledge', async () => {
  const catalog = await loadPluginCatalog(join(deploymentDir, 'plugins'))
  const manifest = catalog.find(plugin => plugin.id === 'duty-handover')
  assert.deepEqual(manifest.allowedTools, ['mcp__iot__query_duty_snapshot', 'mcp__iot__query_knowledge_base'])
  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }))
  const response = await fetch(`${baseUrl}/v1/plugins`, { method: 'POST', headers: { ...controlHeaders, 'content-type': 'application/json' }, body: JSON.stringify({ ...manifest, persona: 'overwrite built-in' }) })
  assert.equal(response.status, 409)
  assert.equal((await response.json()).error.code, 'BUILTIN_PLUGIN_IMMUTABLE')
  assert.equal((await fetch(`${baseUrl}/v1/plugins/duty-handover`, { method: 'DELETE', headers: controlHeaders })).status, 409)
})

test('duty workflow runtime policy denies general device tools and all write tools', () => {
  let guard
  let restrict
  const listeners = new Map()
  applyPolicy({ tools: { guard(fn) { guard = fn } }, on(name, fn) { listeners.set(name, fn) } }, { allowedTools: ['mcp__iot__query_duty_snapshot', 'mcp__iot__query_knowledge_base'] })
  listeners.get('agent/created')({ agent: { ctx: { tools: { restrict(value) { restrict = value } } } } })
  assert.deepEqual(restrict.allow, ['mcp__iot__query_duty_snapshot', 'mcp__iot__query_knowledge_base'])
  assert.equal(guard({ name: 'mcp__iot__query_duty_snapshot' }), undefined)
  for (const name of ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft', 'shell', 'jobs', 'goal', 'skills', 'subagent', 'device_control']) assert.equal(guard({ name }), 'tool not allowed')
})


for (const workflow of ['data-quality-analyst', 'monitoring-continuity-reviewer', 'rule-policy-analyst', 'response-reviewer', 'maintenance-outcome-reviewer', 'maintenance-investment-advisor']) test(`${workflow} is an immutable business workflow with only bound facts and knowledge`, async () => {
  const catalog = await loadPluginCatalog(join(deploymentDir, 'plugins'))
  const manifest = catalog.find(plugin => plugin.id === workflow)
  assert.deepEqual(manifest.allowedTools, ['mcp__iot__query_analysis_snapshot', 'mcp__iot__query_knowledge_base'])
  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }))
  const response = await fetch(`${baseUrl}/v1/plugins`, { method: 'POST', headers: { ...controlHeaders, 'content-type': 'application/json' }, body: JSON.stringify({ ...manifest, persona: 'overwrite built-in' }) })
  assert.equal(response.status, 409)
  assert.equal((await fetch(`${baseUrl}/v1/plugins/${workflow}`, { method: 'DELETE', headers: controlHeaders })).status, 409)
  let guard
  const listeners = new Map()
  applyPolicy({ tools: { guard(fn) { guard = fn } }, on(name, fn) { listeners.set(name, fn) } }, { allowedTools: manifest.allowedTools })
  assert.equal(guard({ name: 'mcp__iot__query_analysis_snapshot' }), undefined)
  for (const name of ['mcp__iot__query_device_latest', 'mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft', 'shell', 'jobs', 'goal', 'skills', 'subagent', 'device_control']) assert.equal(guard({ name }), 'tool not allowed')
})
