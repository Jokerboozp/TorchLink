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

const [{ McpServer }, { StreamableHTTPServerTransport }, { DeepSeekHarness }] = await Promise.all([
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'mcp.js')).href),
  import(pathToFileURL(join(runtimeRoot, 'node_modules', '@modelcontextprotocol', 'sdk', 'dist', 'esm', 'server', 'streamableHttp.js')).href),
  import(pathToFileURL(sdkClientModule).href),
])

const httpServer = createServer((request, response) => {
  void (async () => {
    const mcp = new McpServer(
      { name: 'iot-runtime-smoke', version: '1.0.0' },
      { capabilities: { tools: {} } },
    )
    mcp.registerTool('query_alarm_list', {
      description: 'Returns an empty alarm list for runtime startup verification.',
      inputSchema: {},
    }, async () => ({ content: [{ type: 'text', text: '[]' }] }))
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
      IOT_HARNESS_MODEL: 'qwen3:1.7b',
      IOT_HARNESS_OLLAMA_BASE_URL: 'http://127.0.0.1:11434/v1',
      IOT_HARNESS_OLLAMA_API_KEY: 'ollama',
      IOT_HARNESS_CONTEXT_WINDOW: '8192',
      IOT_MCP_URL: `http://127.0.0.1:${address.port}/mcp`,
      IOT_MCP_RUNTIME_KEY: 'runtime-smoke-loopback-key',
      IOT_HARNESS_SESSION_ROOT: sessionRoot,
      IOT_OPS_PERSONA: 'Read-only IoT operations startup verifier.',
      IOT_ALLOWED_TOOLS_JSON: JSON.stringify(['mcp__iot__query_alarm_list']),
    },
    cwd: workspace,
    provider: 'ollama',
    model: 'qwen3:1.7b',
    maxTokens: 256,
    requestTimeoutMs: 15000,
    shutdownTimeoutMs: 2000,
    disposeEofGraceMs: 2000,
    disposeGraceMs: 1000,
  })
  await harness.start()
  process.stdout.write('DeepSeek Harness runtime carrier smoke passed\n')
} finally {
  if (harness !== undefined) await harness.close().catch(() => {})
  if (httpServer.listening) {
    httpServer.closeAllConnections()
    await new Promise(resolveClose => httpServer.close(resolveClose))
  }
  if (temporaryRoot !== undefined) await rm(temporaryRoot, { recursive: true, force: true })
}
