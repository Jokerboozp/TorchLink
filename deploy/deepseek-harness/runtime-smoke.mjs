import assert from 'node:assert/strict'
import { mkdtemp, mkdir, rm } from 'node:fs/promises'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

const harnessRoot = process.env.DSH_SOURCE_ROOT ?? '/harness'
const runtimeRoot = process.env.DSH_RUNTIME_NODE_ROOT ?? join(harnessRoot, 'runtime-node')
const deploymentRoot = process.env.DSH_DEPLOYMENT_ROOT ?? join(harnessRoot, 'examples', 'iot-ops-agent')
const runtimeBin = process.env.DSH_RUNTIME_BIN
  ?? join(runtimeRoot, 'node_modules', '@deepseek-ai', 'dsh', 'lib', 'bin.js')
const sdkClientModule = process.env.DSH_SDK_CLIENT_MODULE
  ?? join(runtimeRoot, 'node_modules', '@deepseek-ai', 'dsh-sdk-client', 'lib', 'index.js')
const patchFile = join(deploymentRoot, 'cordis.yml')
const persona = 'Read-only IoT operations startup verifier.'
const allowedTool = 'mcp__iot__query_alarm_list'
const modelRequests = []
let toolCalls = 0

const [{ McpServer }, { StreamableHTTPServerTransport }, { DeepSeekHarness }] = await Promise.all([
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'mcp.js')).href),
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'streamableHttp.js')).href),
  import(pathToFileURL(sdkClientModule).href),
])

const httpServer = createServer((request, response) => {
  void (async () => {
    if (request.url === '/v1/chat/completions') {
      let body = ''
      for await (const chunk of request) body += chunk
      const input = JSON.parse(body)
      modelRequests.push(input)
      response.writeHead(200, { 'content-type': 'text/event-stream' })
      const send = (delta, finishReason = null) => response.write(`data: ${JSON.stringify({
        id: 'runtime-smoke-completion', object: 'chat.completion.chunk', created: 1,
        model: 'runtime-smoke', choices: [{ index: 0, delta, finish_reason: finishReason }],
      })}\n\n`)
      send({ role: 'assistant' })
      if (!input.messages.some(message => message.role === 'tool')) {
        send({ tool_calls: [{ index: 0, id: 'smoke-call', type: 'function', function: { name: allowedTool, arguments: '{}' } }] })
        send({}, 'tool_calls')
      } else {
        send({ content: 'Runtime ' })
        send({ content: 'verified.' })
        send({}, 'stop')
      }
      response.end('data: [DONE]\n\n')
      return
    }
    const mcp = new McpServer(
      { name: 'iot-runtime-smoke', version: '1.0.0' },
      { capabilities: { tools: {} } },
    )
    mcp.registerTool('query_alarm_list', {
      description: 'Returns an empty alarm list for runtime startup verification.',
      inputSchema: {},
    }, async () => {
      toolCalls += 1
      return { content: [{ type: 'text', text: '[]' }] }
    })
    const transport = new StreamableHTTPServerTransport({})
    response.once('close', () => {
      void transport.close()
      void mcp.close()
    })
    await mcp.connect(transport)
    await transport.handleRequest(request, response)
  })().catch(error => {
    if (!response.headersSent) response.writeHead(500)
    response.end(error instanceof Error ? error.message : String(error))
  })
})

let temporaryRoot
let harness
let deadline
try {
  await new Promise((resolveListen, reject) => {
    httpServer.once('error', reject)
    httpServer.listen(0, '127.0.0.1', resolveListen)
  })
  const address = httpServer.address()
  if (address === null || typeof address === 'string') throw new Error('runtime smoke MCP server has no TCP address')
  temporaryRoot = await mkdtemp(join(tmpdir(), 'iot-harness-runtime-smoke-'))
  const workspace = join(temporaryRoot, 'workspace')
  const sessionRoot = join(temporaryRoot, 'sessions')
  const harnessHome = join(temporaryRoot, 'dsh-home')
  await Promise.all([mkdir(workspace), mkdir(sessionRoot), mkdir(harnessHome)])

  harness = new DeepSeekHarness({
    dshBin: runtimeBin,
    profile: 'sdk-minimal',
    patches: [patchFile],
    dshHome: harnessHome,
    processCwd: runtimeRoot,
    env: {
      HOME: temporaryRoot,
      PATH: process.env.PATH ?? '/usr/local/bin:/usr/bin:/bin',
      TMPDIR: temporaryRoot,
      DEEPSEEK_API_KEY: 'runtime-smoke-key-not-used-for-model-calls',
      IOT_HARNESS_MODEL: 'runtime-smoke',
      IOT_HARNESS_OLLAMA_BASE_URL: `http://127.0.0.1:${address.port}/v1`,
      IOT_HARNESS_OLLAMA_API_KEY: 'ollama',
      IOT_HARNESS_CONTEXT_WINDOW: '8192',
      IOT_MCP_URL: `http://127.0.0.1:${address.port}/mcp`,
      IOT_MCP_RUNTIME_KEY: 'runtime-smoke-loopback-key',
      IOT_HARNESS_SESSION_ROOT: sessionRoot,
      IOT_OPS_PERSONA: persona,
      IOT_ALLOWED_TOOLS_JSON: JSON.stringify([allowedTool]),
    },
    cwd: workspace,
    provider: 'ollama',
    model: 'runtime-smoke',
    maxTokens: 256,
    requestTimeoutMs: 15000,
    shutdownTimeoutMs: 2000,
    disposeEofGraceMs: 2000,
    disposeGraceMs: 1000,
  })
  await harness.start()
  const notifications = []
  const result = await Promise.race([
    harness.run('Query the alarms, then confirm verification.', {
      sessionId: 'runtime-smoke', onNotification: notification => notifications.push(notification),
    }),
    new Promise((_, reject) => { deadline = setTimeout(() => reject(new Error('runtime smoke turn timed out')), 20000) }),
  ])
  assert.equal(result.events.findLast(event => event.type === 'turn/end')?.data.reason.kind, 'completed')
  assert.equal(result.finalResponse, 'Runtime verified.')
  assert.equal(toolCalls, 1)
  assert.equal(modelRequests.length, 2)
  for (const request of modelRequests) {
    assert.ok(request.messages.some(message => ['system', 'developer'].includes(message.role) && message.content.includes(persona)), 'deployment persona reaches the model')
    assert.deepEqual(request.tools.map(tool => tool.function.name), [allowedTool], 'only the allowed MCP tool reaches the model')
  }
  const textChunks = notifications.filter(notification => notification.method === 'iot.text.delta')
  assert.equal(textChunks.map(notification => notification.params.text).join(''), 'Runtime verified.', 'text streams through the SDK before completion')
  assert.ok(notifications.indexOf(textChunks[0]) < notifications.findIndex(notification => notification.params?.event?.type === 'assistant/message'
    && notification.params.event.data.message.content.some(block => block.type === 'text' && block.text === 'Runtime verified.')), 'text arrives before the final message')
  process.stdout.write('DeepSeek Harness runtime smoke passed: persona, text stream, MCP execution, tool allowlist\n')
} finally {
  clearTimeout(deadline)
  if (harness !== undefined) await harness.close().catch(() => {})
  if (httpServer.listening) {
    httpServer.closeAllConnections()
    await new Promise(resolveClose => httpServer.close(resolveClose))
  }
  if (temporaryRoot !== undefined) await rm(temporaryRoot, { recursive: true, force: true })
}
