import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { mkdtemp, mkdir, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { pathToFileURL } from 'node:url' /* 引入当前代码需要的依赖。 */

const harnessRoot = process.env.DSH_SOURCE_ROOT ?? '/harness' /* 声明 harnessRoot。 */
const runtimeRoot = process.env.DSH_RUNTIME_NODE_ROOT ?? join(harnessRoot, 'runtime-node') /* 声明 runtimeRoot。 */
const deploymentRoot = process.env.DSH_DEPLOYMENT_ROOT ?? join(harnessRoot, 'examples', 'iot-ops-agent') /* 声明 deploymentRoot。 */
const runtimeBin = process.env.DSH_RUNTIME_BIN /* 声明 runtimeBin。 */
  ?? join(runtimeRoot, 'node_modules', '@deepseek-ai', 'dsh', 'lib', 'bin.js') /* 执行当前语句并推进处理流程。 */
const sdkClientModule = process.env.DSH_SDK_CLIENT_MODULE /* 声明 sdkClientModule。 */
  ?? join(runtimeRoot, 'node_modules', '@deepseek-ai', 'dsh-sdk-client', 'lib', 'index.js') /* 执行当前语句并推进处理流程。 */
const patchFile = join(deploymentRoot, 'cordis.yml') /* 声明 patchFile。 */
const persona = 'Read-only IoT operations startup verifier.' /* 声明 persona。 */
const allowedTool = 'mcp__iot__query_alarm_list' /* 声明 allowedTool。 */
const modelRequests = [] /* 声明 modelRequests。 */
let toolCalls = 0 /* 声明 toolCalls。 */

const [{ McpServer }, { StreamableHTTPServerTransport }, { DeepSeekHarness }] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'mcp.js')).href), /* 引入当前代码需要的依赖。 */
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'streamableHttp.js')).href), /* 引入当前代码需要的依赖。 */
  import(pathToFileURL(sdkClientModule).href), /* 引入当前代码需要的依赖。 */
]) /* 结束当前表达式或代码块。 */

const httpServer = createServer((request, response) => { /* 声明 httpServer。 */
  void (async () => { /* 执行当前语句并推进处理流程。 */
    if (request.url === '/v1/chat/completions') { /* 判断条件并选择处理分支。 */
      let body = '' /* 声明 body。 */
      for await (const chunk of request) body += chunk /* 循环处理当前数据。 */
      const input = JSON.parse(body) /* 声明 input。 */
      modelRequests.push(input) /* 执行当前语句并推进处理流程。 */
      response.writeHead(200, { 'content-type': 'text/event-stream' }) /* 执行当前语句并推进处理流程。 */
      const send = (delta, finishReason = null) => response.write(`data: ${JSON.stringify({
        id: 'runtime-smoke-completion', object: 'chat.completion.chunk', created: 1,
        model: 'runtime-smoke', choices: [{ index: 0, delta, finish_reason: finishReason }],
      })}\n\n`)
      send({ role: 'assistant' }) /* 执行当前语句并推进处理流程。 */
      if (!input.messages.some(message => message.role === 'tool')) { /* 判断条件并选择处理分支。 */
        send({ tool_calls: [{ index: 0, id: 'smoke-call', type: 'function', function: { name: allowedTool, arguments: '{}' } }] }) /* 执行当前语句并推进处理流程。 */
        send({}, 'tool_calls') /* 执行当前语句并推进处理流程。 */
      } else { /* 结束当前表达式或代码块。 */
        send({ content: 'Runtime ' }) /* 执行当前语句并推进处理流程。 */
        send({ content: 'verified.' }) /* 执行当前语句并推进处理流程。 */
        send({}, 'stop') /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
      response.end('data: [DONE]\n\n') /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    const mcp = new McpServer( /* 声明 mcp。 */
      { name: 'iot-runtime-smoke', version: '1.0.0' }, /* 执行当前语句并推进处理流程。 */
      { capabilities: { tools: {} } }, /* 执行当前语句并推进处理流程。 */
    ) /* 结束当前表达式或代码块。 */
    mcp.registerTool('query_alarm_list', { /* 执行当前语句并推进处理流程。 */
      description: 'Returns an empty alarm list for runtime startup verification.', /* 执行当前语句并推进处理流程。 */
      inputSchema: {}, /* 执行当前语句并推进处理流程。 */
    }, async () => { /* 结束当前表达式或代码块。 */
      toolCalls += 1 /* 更新 toolCalls 的值。 */
      return { content: [{ type: 'text', text: '[]' }] } /* 返回当前处理结果。 */
    }) /* 结束当前表达式或代码块。 */
    const transport = new StreamableHTTPServerTransport({}) /* 声明 transport。 */
    response.once('close', () => { /* 执行当前语句并推进处理流程。 */
      void transport.close() /* 执行当前语句并推进处理流程。 */
      void mcp.close() /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    await mcp.connect(transport) /* 等待异步操作完成。 */
    await transport.handleRequest(request, response) /* 等待异步操作完成。 */
  })().catch(error => { /* 结束当前表达式或代码块。 */
    if (!response.headersSent) response.writeHead(500) /* 判断条件并选择处理分支。 */
    response.end(error instanceof Error ? error.message : String(error)) /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

let temporaryRoot /* 声明 temporaryRoot。 */
let harness /* 声明 harness。 */
let deadline /* 声明 deadline。 */
try { /* 执行当前语句并推进处理流程。 */
  await new Promise((resolveListen, reject) => { /* 等待异步操作完成。 */
    httpServer.once('error', reject) /* 执行当前语句并推进处理流程。 */
    httpServer.listen(0, '127.0.0.1', resolveListen) /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  const address = httpServer.address() /* 声明 address。 */
  if (address === null || typeof address === 'string') throw new Error('runtime smoke MCP server has no TCP address') /* 判断条件并选择处理分支。 */
  temporaryRoot = await mkdtemp(join(tmpdir(), 'iot-harness-runtime-smoke-')) /* 更新 temporaryRoot 的值。 */
  const workspace = join(temporaryRoot, 'workspace') /* 声明 workspace。 */
  const sessionRoot = join(temporaryRoot, 'sessions') /* 声明 sessionRoot。 */
  const harnessHome = join(temporaryRoot, 'dsh-home') /* 声明 harnessHome。 */
  await Promise.all([mkdir(workspace), mkdir(sessionRoot), mkdir(harnessHome)]) /* 等待异步操作完成。 */

  harness = new DeepSeekHarness({ /* 更新 harness 的值。 */
    dshBin: runtimeBin, /* 执行当前语句并推进处理流程。 */
    profile: 'sdk-minimal', /* 执行当前语句并推进处理流程。 */
    patches: [patchFile], /* 执行当前语句并推进处理流程。 */
    dshHome: harnessHome, /* 执行当前语句并推进处理流程。 */
    processCwd: runtimeRoot, /* 执行当前语句并推进处理流程。 */
    env: { /* 执行当前语句并推进处理流程。 */
      HOME: temporaryRoot, /* 执行当前语句并推进处理流程。 */
      PATH: process.env.PATH ?? '/usr/local/bin:/usr/bin:/bin', /* 执行当前语句并推进处理流程。 */
      TMPDIR: temporaryRoot, /* 执行当前语句并推进处理流程。 */
      DEEPSEEK_API_KEY: 'runtime-smoke-key-not-used-for-model-calls', /* 执行当前语句并推进处理流程。 */
      IOT_HARNESS_MODEL: 'runtime-smoke', /* 执行当前语句并推进处理流程。 */
      IOT_HARNESS_OLLAMA_BASE_URL: `http://127.0.0.1:${address.port}/v1`, /* 执行当前语句并推进处理流程。 */
      IOT_HARNESS_OLLAMA_API_KEY: 'ollama', /* 执行当前语句并推进处理流程。 */
      IOT_HARNESS_CONTEXT_WINDOW: '8192', /* 执行当前语句并推进处理流程。 */
      IOT_MCP_URL: `http://127.0.0.1:${address.port}/mcp`, /* 执行当前语句并推进处理流程。 */
      IOT_MCP_RUNTIME_KEY: 'runtime-smoke-loopback-key', /* 执行当前语句并推进处理流程。 */
      IOT_HARNESS_SESSION_ROOT: sessionRoot, /* 执行当前语句并推进处理流程。 */
      IOT_OPS_PERSONA: persona, /* 执行当前语句并推进处理流程。 */
      IOT_ALLOWED_TOOLS_JSON: JSON.stringify([allowedTool]), /* 执行当前语句并推进处理流程。 */
    }, /* 结束当前表达式或代码块。 */
    cwd: workspace, /* 执行当前语句并推进处理流程。 */
    provider: 'ollama', /* 执行当前语句并推进处理流程。 */
    model: 'runtime-smoke', /* 执行当前语句并推进处理流程。 */
    maxTokens: 256, /* 执行当前语句并推进处理流程。 */
    requestTimeoutMs: 15000, /* 执行当前语句并推进处理流程。 */
    shutdownTimeoutMs: 2000, /* 执行当前语句并推进处理流程。 */
    disposeEofGraceMs: 2000, /* 执行当前语句并推进处理流程。 */
    disposeGraceMs: 1000, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  await harness.start() /* 等待异步操作完成。 */
  const notifications = [] /* 声明 notifications。 */
  const result = await Promise.race([ /* 声明 result。 */
    harness.run('Query the alarms, then confirm verification.', { /* 执行当前语句并推进处理流程。 */
      sessionId: 'runtime-smoke', onNotification: notification => notifications.push(notification), /* 执行当前语句并推进处理流程。 */
    }), /* 结束当前表达式或代码块。 */
    new Promise((_, reject) => { deadline = setTimeout(() => reject(new Error('runtime smoke turn timed out')), 20000) }), /* 执行当前语句并推进处理流程。 */
  ]) /* 结束当前表达式或代码块。 */
  assert.equal(result.events.findLast(event => event.type === 'turn/end')?.data.reason.kind, 'completed') /* 验证实际结果符合预期。 */
  assert.equal(result.finalResponse, 'Runtime verified.') /* 验证实际结果符合预期。 */
  assert.equal(toolCalls, 1) /* 验证实际结果符合预期。 */
  assert.equal(modelRequests.length, 2) /* 验证实际结果符合预期。 */
  for (const request of modelRequests) { /* 循环处理当前数据。 */
    assert.ok(request.messages.some(message => ['system', 'developer'].includes(message.role) && message.content.includes(persona)), 'deployment persona reaches the model') /* 验证实际结果符合预期。 */
    assert.deepEqual(request.tools.map(tool => tool.function.name), [allowedTool], 'only the allowed MCP tool reaches the model') /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  const textChunks = notifications.filter(notification => notification.method === 'iot.text.delta') /* 声明 textChunks。 */
  assert.equal(textChunks.map(notification => notification.params.text).join(''), 'Runtime verified.', 'text streams through the SDK before completion') /* 验证实际结果符合预期。 */
  assert.ok(notifications.indexOf(textChunks[0]) < notifications.findIndex(notification => notification.params?.event?.type === 'assistant/message' /* 验证实际结果符合预期。 */
    && notification.params.event.data.message.content.some(block => block.type === 'text' && block.text === 'Runtime verified.')), 'text arrives before the final message') /* 执行当前语句并推进处理流程。 */
  process.stdout.write('DeepSeek Harness runtime smoke passed: persona, text stream, MCP execution, tool allowlist\n') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  clearTimeout(deadline) /* 执行当前语句并推进处理流程。 */
  if (harness !== undefined) await harness.close().catch(() => {}) /* 判断条件并选择处理分支。 */
  if (httpServer.listening) { /* 判断条件并选择处理分支。 */
    httpServer.closeAllConnections() /* 执行当前语句并推进处理流程。 */
    await new Promise(resolveClose => httpServer.close(resolveClose)) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  if (temporaryRoot !== undefined) await rm(temporaryRoot, { recursive: true, force: true }) /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */
