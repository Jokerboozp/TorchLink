import { createHash, randomBytes, timingSafeEqual } from 'node:crypto'
import { constants as fsConstants } from 'node:fs'
import { access, lstat, mkdir, open, opendir, readFile, readdir, realpath, rename, stat, unlink, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { dirname, join, relative, resolve, sep } from 'node:path'
import { Readable } from 'node:stream'
import { pipeline } from 'node:stream/promises'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { READ_ONLY_TOOL_CEILING } from './iot-ops-plugin.mjs'

const here = dirname(fileURLToPath(import.meta.url))
const DEFAULT_PLUGIN_DIR = join(here, 'plugins')
const DEFAULT_PATCH_FILE = join(here, 'cordis.yml')
const DEFAULT_RUNTIME_BIN = '/harness/runtime-node/node_modules/@deepseek-ai/dsh/lib/bin.js'
const DEFAULT_SDK_CLIENT_MODULE = '/harness/runtime-node/node_modules/@deepseek-ai/dsh-sdk-client/lib/index.js'
const DEFAULT_WORKSPACE = '/data/workspace'
const DEFAULT_SESSION_ROOT = '/data/sessions'
const DEFAULT_HARNESS_HOME = '/data/runtime-home'
const DEFAULT_MCP_ORIGINS = 'http://platform-api:8080'

const readOnlyToolCeiling = new Set(READ_ONLY_TOOL_CEILING)
const MANIFEST_KEYS = new Set([
  'schemaVersion',
  'id',
  'name',
  'description',
  'version',
  'enabled',
  'persona',
  'defaultModel',
  'maxTokens',
  'capabilities',
  'allowedTools',
])
const BODY_KEYS = new Set([
  'tenantId',
  'actor',
  'runId',
  'conversationId',
  'workflowId',
  'question',
  'mcpUrl',
  'model',
  'maxTokens',
])
const PROVIDER_KEYS = new Set(['provider', 'baseUrl', 'model', 'apiKey'])
// deepseek-official: DeepSeek cloud API; openai-compatible: external
// OpenAI Chat Completions APIs.
const MODEL_PROVIDERS = ['deepseek-official', 'openai-compatible']
const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/
const MODEL_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/
const CAPABILITY_PATTERN = /^[^\u0000-\u001f\u007f]{1,64}$/u
const BUILTIN_PLUGIN_IDS = new Set(['alarm-handler', 'ops-assistant', 'system-observer', 'device-health-inspector', 'protocol-assistant', 'rule-drafter'])

class HttpError extends Error {
  constructor(status, code, message) {
    super(message)
    this.name = 'HttpError'
    this.status = status
    this.code = code
  }
}

function integerOption(value, fallback, name, minimum, maximum) {
  const resolved = value === undefined || value === '' ? fallback : Number(value)
  if (!Number.isSafeInteger(resolved) || resolved < minimum || resolved > maximum) {
    throw new Error(`${name} must be an integer from ${minimum} to ${maximum}`)
  }
  return resolved
}

function text(value, name, maxLength, pattern) {
  if (typeof value !== 'string' || value.trim() === '' || value.length > maxLength) {
    throw new HttpError(422, 'INVALID_REQUEST', `${name} is required and must be at most ${maxLength} characters`)
  }
  if (pattern !== undefined && !pattern.test(value)) {
    throw new HttpError(422, 'INVALID_REQUEST', `${name} has an invalid format`)
  }
  return value
}

function hash(value) {
  return createHash('sha256').update(value).digest('hex')
}

function constantTimeEqual(actual, expected) {
  const left = Buffer.from(actual)
  const right = Buffer.from(expected)
  return left.length === right.length && timingSafeEqual(left, right)
}

const BACKUP_SEGMENT_PATTERN = /^(?:[A-Za-z0-9._-]|~[A-F0-9]{4}){1,255}$/
const BACKUP_GENERATION_PATTERN = /^session(?:\.v[1-9][0-9]{0,15})?\.jsonl(?:\.zstd)?$/

function snapshotChanged() {
  return new HttpError(409, 'SNAPSHOT_CHANGED', 'Harness persistence changed while copying; retry the snapshot')
}

function sameSnapshotStat(left, right) {
  return ['dev', 'ino', 'size', 'mode', 'nlink', 'mtimeNs', 'ctimeNs']
    .every(key => left[key] === right[key])
}

function safeBackupSegment(name) {
  return name !== '.' && name !== '..' && BACKUP_SEGMENT_PATTERN.test(name)
}

// Only the two persistence roots enter a snapshot. Runtime homes contain
// provider configuration, while workspace and process environment are never read.
async function captureBackupSnapshot(pluginDir, sessionRoot, limits, afterRead) {
  const roots = [{ name: 'plugins', root: pluginDir }, { name: 'sessions', root: sessionRoot }]
  const scan = async () => {
    const nodes = new Map()
    const files = []
    let totalBytes = 0
    for (const source of roots) {
      let rootStat
      try { rootStat = await lstat(source.root, { bigint: true }) } catch (error) {
        if (error?.code === 'ENOENT' && source.name === 'sessions') continue
        throw error
      }
      if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) {
        throw new HttpError(422, 'SNAPSHOT_UNSAFE_PATH', 'Harness persistence root must be a regular directory')
      }
      const canonicalRoot = await realpath(source.root)
      const visit = async (diskPath, segments, details) => {
        const path = [source.name, ...segments].join('/')
        if (nodes.size >= limits.maxFiles * 4 + 128) {
          throw new HttpError(413, 'SNAPSHOT_LIMIT_EXCEEDED', 'Harness snapshot contains too many filesystem entries')
        }
        nodes.set(path, { diskPath, details, canonicalRoot })
        if (details.isSymbolicLink() || (!details.isDirectory() && !details.isFile())
          || (details.isFile() && details.nlink !== 1n)) {
          throw new HttpError(422, 'SNAPSHOT_UNSAFE_PATH', 'Harness snapshot refuses links and special files')
        }
        const actual = await realpath(diskPath)
        const withinRoot = relative(canonicalRoot, actual)
        if (withinRoot === '..' || withinRoot.startsWith(`..${sep}`) || resolve(canonicalRoot, withinRoot) !== actual) {
          throw new HttpError(422, 'SNAPSHOT_UNSAFE_PATH', 'Harness snapshot path is outside its persistence root')
        }
        if (details.isDirectory()) {
          if ((source.name === 'plugins' && segments.length > 0) || segments.length > 2) {
            throw new HttpError(422, 'SNAPSHOT_UNSAFE_PATH', 'Harness snapshot contains an unsupported persistence directory')
          }
          const directory = await opendir(diskPath)
          for await (const entry of directory) {
            // Manifests use the validated workflow ID; JSONL directories use
            // upstream's escaped project/session segments. Neither admits paths.
            const manifestName = source.name === 'plugins' && segments.length === 0
              && entry.name.endsWith('.json') && ID_PATTERN.test(entry.name.slice(0, -5))
            if (!manifestName && !safeBackupSegment(entry.name)) {
              throw new HttpError(422, 'SNAPSHOT_UNSAFE_PATH', 'Harness snapshot contains an unsafe path segment')
            }
            const child = join(diskPath, entry.name)
            await visit(child, [...segments, entry.name], await lstat(child, { bigint: true }))
          }
          return
        }
        const included = source.name === 'plugins'
          ? segments.length === 1 && segments[0].endsWith('.json') && ID_PATTERN.test(segments[0].slice(0, -5))
          : segments.length === 3 && BACKUP_GENERATION_PATTERN.test(segments[2])
        // Locks, incomplete generations and unrelated files are never restored.
        if (!included) return
        if (source.name === 'plugins' && details.size > 65536n) {
          throw new HttpError(422, 'SNAPSHOT_MANIFEST_INVALID', 'Harness Agent manifest exceeds its size limit')
        }
        if (files.length >= limits.maxFiles || details.size > BigInt(limits.maxBytes - totalBytes)) {
          throw new HttpError(413, 'SNAPSHOT_LIMIT_EXCEEDED', 'Harness snapshot exceeds its file or byte limit')
        }
        totalBytes += Number(details.size)
        files.push({ path, diskPath, details, canonicalRoot })
      }
      await visit(source.root, [], rootStat)
    }
    files.sort((left, right) => left.path.localeCompare(right.path, 'en'))
    return { nodes, files, totalBytes }
  }
  const readStable = async (file) => {
    const details = await lstat(file.diskPath, { bigint: true })
    if (!details.isFile() || !sameSnapshotStat(details, file.details)) throw snapshotChanged()
    const actual = await realpath(file.diskPath)
    const withinRoot = relative(file.canonicalRoot, actual)
    if (withinRoot === '..' || withinRoot.startsWith(`..${sep}`)) throw snapshotChanged()
    const handle = await open(file.diskPath, fsConstants.O_RDONLY | (fsConstants.O_NOFOLLOW ?? 0))
    try {
      if (!sameSnapshotStat(await handle.stat({ bigint: true }), file.details)) throw snapshotChanged()
      // Read at most the scanned size plus one byte even if an external writer
      // appends continuously. Never let readFile allocate from a changing size.
      const buffer = Buffer.alloc(Number(file.details.size) + 1)
      let offset = 0
      while (offset < buffer.length) {
        const { bytesRead } = await handle.read(buffer, offset, buffer.length - offset, offset)
        if (bytesRead === 0) break
        offset += bytesRead
      }
      if (offset !== Number(file.details.size)
        || !sameSnapshotStat(await handle.stat({ bigint: true }), file.details)) throw snapshotChanged()
      return buffer.subarray(0, offset)
    } finally {
      await handle.close()
    }
  }
  try {
    const initial = await scan()
    const entries = []
    for (const file of initial.files) {
      const bytes = await readStable(file)
      if (file.path.startsWith('plugins/')) {
        try {
          const manifest = validatedManifest(JSON.parse(bytes.toString('utf8')), file.path)
          if (file.path !== `plugins/${manifest.id}.json`) throw new Error('manifest ID does not match filename')
        } catch {
          throw new HttpError(422, 'SNAPSHOT_MANIFEST_INVALID', 'Harness snapshot contains an invalid Agent manifest')
        }
      }
      entries.push({ path: file.path, base64: bytes.toString('base64'), sha256: hash(bytes), size: bytes.length })
      await afterRead?.(file.path)
    }
    // A second checksum pass catches in-place rewrites. The final complete
    // directory scan also catches replacement, deletion and newly added files.
    for (let index = 0; index < initial.files.length; index++) {
      if (hash(await readStable(initial.files[index])) !== entries[index].sha256) throw snapshotChanged()
    }
    const final = await scan()
    if (final.nodes.size !== initial.nodes.size) throw snapshotChanged()
    for (const [path, node] of initial.nodes) {
      const candidate = final.nodes.get(path)
      if (candidate === undefined || candidate.canonicalRoot !== node.canonicalRoot
        || !sameSnapshotStat(candidate.details, node.details)) throw snapshotChanged()
    }
    return { formatVersion: 1, createdAt: new Date().toISOString(), entries, fileCount: entries.length, totalBytes: initial.totalBytes }
  } catch (error) {
    if (['ENOENT', 'ENOTDIR', 'ELOOP'].includes(error?.code)) throw snapshotChanged()
    throw error
  }
}

function bearerToken(header) {
  if (typeof header !== 'string' || header.length > 8192) return undefined
  const match = /^Bearer ([^\s]+)$/i.exec(header)
  return match?.[1]
}

function configuredOrigins(value) {
  const origins = new Set()
  for (const item of value.split(',')) {
    const candidate = item.trim()
    if (candidate === '') continue
    const url = new URL(candidate)
    if (!['http:', 'https:'].includes(url.protocol) || url.username !== '' || url.password !== '') {
      throw new Error(`invalid MCP allowed origin: ${candidate}`)
    }
    origins.add(url.origin)
  }
  if (origins.size === 0) throw new Error('at least one MCP origin must be allowed')
  return origins
}

function validatedMcpUrl(value, allowedOrigins) {
  const raw = text(value, 'mcpUrl', 2048)
  let url
  try {
    url = new URL(raw)
  } catch {
    throw new HttpError(422, 'MCP_URL_INVALID', 'mcpUrl must be an absolute HTTP(S) URL')
  }
  if (!['http:', 'https:'].includes(url.protocol)
    || url.username !== ''
    || url.password !== ''
    || url.hash !== ''
    || url.search !== ''
    || url.pathname !== '/mcp/harness') {
    throw new HttpError(422, 'MCP_URL_INVALID', 'mcpUrl must be an allowed origin with the exact /mcp/harness path')
  }
  if (!allowedOrigins.has(url.origin)) {
    throw new HttpError(422, 'MCP_ORIGIN_NOT_ALLOWED', 'mcpUrl origin is not allowed')
  }
  return url.href
}

function validatedManifest(raw, filename) {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error(`${filename}: manifest must be an object`)
  }
  const unknown = Object.keys(raw).filter(key => !MANIFEST_KEYS.has(key))
  if (unknown.length > 0) throw new Error(`${filename}: unknown field(s): ${unknown.join(', ')}`)
  if (raw.schemaVersion !== 1) throw new Error(`${filename}: schemaVersion must be 1`)
  const id = manifestString(raw.id, filename, 'id', 128, ID_PATTERN)
  const name = manifestString(raw.name, filename, 'name', 128)
  const description = manifestString(raw.description, filename, 'description', 1024)
  const version = manifestString(raw.version, filename, 'version', 64)
  const persona = manifestString(raw.persona, filename, 'persona', 16384)
  const defaultModel = manifestString(raw.defaultModel, filename, 'defaultModel', 128, MODEL_PATTERN)
  if (typeof raw.enabled !== 'boolean') throw new Error(`${filename}: enabled must be a boolean`)
  if (!Number.isSafeInteger(raw.maxTokens) || raw.maxTokens < 1 || raw.maxTokens > 262144) {
    throw new Error(`${filename}: maxTokens must be an integer from 1 to 262144`)
  }
  if (!Array.isArray(raw.capabilities) || raw.capabilities.length === 0 || raw.capabilities.length > 32) {
    throw new Error(`${filename}: capabilities must be an array containing 1 to 32 items`)
  }
  const capabilities = []
  const seenCapabilities = new Set()
  for (const capability of raw.capabilities) {
    if (typeof capability !== 'string' || !CAPABILITY_PATTERN.test(capability)) {
      throw new Error(`${filename}: capability has an invalid format: ${String(capability)}`)
    }
    if (seenCapabilities.has(capability)) throw new Error(`${filename}: duplicate capability: ${capability}`)
    seenCapabilities.add(capability)
    capabilities.push(capability)
  }
  if (!Array.isArray(raw.allowedTools) || raw.allowedTools.length === 0) {
    throw new Error(`${filename}: allowedTools must be a non-empty array`)
  }
  const allowedTools = []
  const seen = new Set()
  for (const tool of raw.allowedTools) {
    if (typeof tool !== 'string' || !readOnlyToolCeiling.has(tool)) {
      throw new Error(`${filename}: tool is outside the read-only security ceiling: ${String(tool)}`)
    }
    if (seen.has(tool)) throw new Error(`${filename}: duplicate allowed tool: ${tool}`)
    seen.add(tool)
    allowedTools.push(tool)
  }
  return Object.freeze({
    schemaVersion: 1,
    id,
    name,
    description,
    version,
    enabled: raw.enabled,
    persona,
    defaultModel,
    maxTokens: raw.maxTokens,
    capabilities: Object.freeze(capabilities),
    allowedTools: Object.freeze(allowedTools),
  })
}

function manifestString(value, filename, name, maxLength, pattern) {
  if (typeof value !== 'string' || value.trim() === '' || value.length > maxLength) {
    throw new Error(`${filename}: ${name} must be a non-empty string of at most ${maxLength} characters`)
  }
  if (pattern !== undefined && !pattern.test(value)) throw new Error(`${filename}: ${name} has an invalid format`)
  return value
}

export async function loadPluginCatalog(pluginDir = DEFAULT_PLUGIN_DIR) {
  const entries = (await readdir(pluginDir, { withFileTypes: true }))
    .filter(entry => entry.isFile() && entry.name.endsWith('.json'))
    .sort((left, right) => left.name.localeCompare(right.name))
  if (entries.length === 0) throw new Error(`no plugin manifests found in ${pluginDir}`)
  if (entries.length > 64) throw new Error(`too many plugin manifests in ${pluginDir}`)
  const plugins = []
  const ids = new Set()
  for (const entry of entries) {
    const path = join(pluginDir, entry.name)
    const metadata = await stat(path)
    if (metadata.size > 65536) throw new Error(`${entry.name}: manifest exceeds 65536 bytes`)
    let parsed
    try {
      parsed = JSON.parse(await readFile(path, 'utf8'))
    } catch (error) {
      throw new Error(`${entry.name}: invalid JSON`, { cause: error })
    }
    const plugin = validatedManifest(parsed, entry.name)
    if (ids.has(plugin.id)) throw new Error(`${entry.name}: duplicate plugin id: ${plugin.id}`)
    ids.add(plugin.id)
    plugins.push(plugin)
  }
  return Object.freeze(plugins)
}

function publicPlugin(plugin) {
  return {
    schemaVersion: plugin.schemaVersion,
    id: plugin.id,
    name: plugin.name,
    description: plugin.description,
    version: plugin.version,
    enabled: plugin.enabled,
    defaultModel: plugin.defaultModel,
    maxTokens: plugin.maxTokens,
    capabilities: [...plugin.capabilities],
    knowledgeEnabled: plugin.allowedTools.includes('mcp__iot__query_knowledge_base'),
  }
}

function adminPlugin(plugin) {
  return {
    schemaVersion: plugin.schemaVersion,
    id: plugin.id,
    name: plugin.name,
    description: plugin.description,
    version: plugin.version,
    enabled: plugin.enabled,
    persona: plugin.persona,
    defaultModel: plugin.defaultModel,
    maxTokens: plugin.maxTokens,
    capabilities: [...plugin.capabilities],
    allowedTools: [...plugin.allowedTools],
  }
}

async function readBody(request, maxBytes) {
  const chunks = []
  let size = 0
  for await (const chunk of request) {
    size += chunk.length
    if (size > maxBytes) throw new HttpError(413, 'REQUEST_TOO_LARGE', 'request body is too large')
    chunks.push(chunk)
  }
  return Buffer.concat(chunks)
}

async function readJson(request, maxBytes) {
  const contentType = request.headers['content-type'] ?? ''
  if (!contentType.toLowerCase().startsWith('application/json')) {
    throw new HttpError(415, 'CONTENT_TYPE_REQUIRED', 'Content-Type must be application/json')
  }
  const body = await readBody(request, maxBytes)
  try {
    return JSON.parse(body.toString('utf8'))
  } catch {
    throw new HttpError(400, 'INVALID_JSON', 'request body must be valid JSON')
  }
}

function validatedBody(raw, plugins, allowedOrigins, modelOverride) {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new HttpError(422, 'INVALID_REQUEST', 'request body must be an object')
  }
  const unknown = Object.keys(raw).filter(key => !BODY_KEYS.has(key))
  if (unknown.length > 0) throw new HttpError(422, 'INVALID_REQUEST', `unknown field(s): ${unknown.join(', ')}`)
  const runId = text(raw.runId, 'runId', 128, ID_PATTERN)
  const conversationId = text(raw.conversationId, 'conversationId', 128, ID_PATTERN)
  const workflowId = text(raw.workflowId, 'workflowId', 128, ID_PATTERN)
  const question = text(raw.question, 'question', 20000)
  const plugin = plugins.find(candidate => candidate.id === workflowId && candidate.enabled)
  if (plugin === undefined) throw new HttpError(404, 'WORKFLOW_NOT_FOUND', 'workflow plugin is not available')
  const requestedModel = raw.model === undefined || raw.model === ''
    ? plugin.defaultModel
    : text(raw.model, 'model', 128, MODEL_PATTERN)
  const model = modelOverride ?? requestedModel
  const maxTokens = raw.maxTokens === undefined || raw.maxTokens === null
    ? plugin.maxTokens
    : raw.maxTokens
  if (!Number.isSafeInteger(maxTokens) || maxTokens < 1 || maxTokens > 262144) {
    throw new HttpError(422, 'MAX_TOKENS_INVALID', 'maxTokens must be an integer from 1 to 262144')
  }
  return {
    runId,
    tenantId: raw.tenantId === undefined ? '' : text(raw.tenantId, 'tenantId', 128, ID_PATTERN),
    actor: raw.actor === undefined ? '' : text(raw.actor, 'actor', 256),
    conversationId,
    workflowId,
    question,
    mcpUrl: validatedMcpUrl(raw.mcpUrl, allowedOrigins),
    model,
    // Provider settings are shared across workflows; each plugin retains its ceiling.
    maxTokens: Math.min(maxTokens, plugin.maxTokens),
    plugin,
  }
}

function json(response, status, body) {
  const payload = `${JSON.stringify(body)}\n`
  response.writeHead(status, {
    'cache-control': 'no-store',
    'content-length': Buffer.byteLength(payload),
    'content-type': 'application/json; charset=utf-8',
    'x-content-type-options': 'nosniff',
  })
  response.end(payload)
}

function problem(response, error) {
  const status = error instanceof HttpError ? error.status : 500
  const code = error instanceof HttpError ? error.code : 'INTERNAL_ERROR'
  const message = error instanceof HttpError ? error.message : 'internal gateway error'
  json(response, status, { error: { code, message } })
}

function eventWriter(response, run) {
  return (type, data = {}) => {
    if (response.destroyed || response.writableEnded) return false
    response.write(`${JSON.stringify({ type, runId: run.runId, time: Date.now(), ...data })}\n`)
    return true
  }
}

function sessionEvent(notification, sessionId) {
  if (notification?.method !== 'session.event' || notification.params?.sessionId !== sessionId) return undefined
  const event = notification.params.event
  return event !== null && typeof event === 'object' ? event : undefined
}

// addUsage adds one step's TokenUsage to total and reports whether the step
// carried any accounting. Only non-negative safe integers are counted.
export function addUsage(total, step) {
  if (step === null || typeof step !== 'object') return false
  let reported = false
  for (const key of ['inputTokens', 'outputTokens', 'cacheReadTokens', 'reasoningTokens']) {
    const value = step[key]
    if (Number.isSafeInteger(value) && value >= 0) {
      total[key] += value
      reported = true
    }
  }
  return reported
}

function toolResultFailed(data) {
  if (data?.error !== undefined) return true
  const content = data?.message?.content
  return Array.isArray(content) && content.some(item => item !== null && typeof item === 'object' && item.isError === true)
}

function validatedProviderConfig(raw) {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new HttpError(422, 'INVALID_REQUEST', 'provider configuration must be an object')
  }
  const unknown = Object.keys(raw).filter(key => !PROVIDER_KEYS.has(key))
  if (unknown.length > 0) throw new HttpError(422, 'INVALID_REQUEST', `unknown field(s): ${unknown.join(', ')}`)
  const provider = text(raw.provider, 'provider', 64).trim().toLowerCase()
  if (!MODEL_PROVIDERS.includes(provider)) {
    throw new HttpError(422, 'PROVIDER_INVALID', 'provider must be deepseek-official or openai-compatible')
  }
  const model = text(raw.model, 'model', 128).trim()
  if (!MODEL_PATTERN.test(model)) throw new HttpError(422, 'INVALID_REQUEST', 'model has an invalid format')
  const baseUrl = text(raw.baseUrl, 'baseUrl', 2048).trim()
  let parsed
  try { parsed = new URL(baseUrl) } catch { throw new HttpError(422, 'BASE_URL_INVALID', 'baseUrl must be an absolute HTTP(S) URL') }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username !== '' || parsed.password !== '' || parsed.search !== '' || parsed.hash !== '') {
    throw new HttpError(422, 'BASE_URL_INVALID', 'baseUrl must be an HTTP(S) URL without credentials or query parameters')
  }
  const apiKey = typeof raw.apiKey === 'string' ? raw.apiKey.trim() : ''
  if (provider === 'deepseek-official' && apiKey === '') {
    throw new HttpError(422, 'API_KEY_REQUIRED', 'apiKey is required for API providers')
  }
  return { provider, baseUrl: parsed.href.replace(/\/$/, ''), model, apiKey }
}

function publicProviderConfig(provider, baseUrl, model, apiKey, instanceId) {
  return {
    provider,
    baseUrl,
    model,
    apiKeyConfigured: typeof apiKey === 'string' && apiKey.trim() !== '',
    // Provider settings live in memory; a new instanceId tells the platform
    // this process restarted and must be configured again.
    instanceId,
  }
}

function toolResultText(data) {
  if (typeof data?.result === 'string') return data.result
  if (data?.result !== null && typeof data?.result === 'object') return JSON.stringify(data.result)
  const content = data?.message?.content
  if (!Array.isArray(content)) return undefined
  const direct = content.find(item => item !== null && typeof item === 'object' && typeof item.text === 'string')?.text
  if (typeof direct === 'string') return direct
  const toolResult = content.find(item => item !== null && typeof item === 'object' && item.type === 'tool-result')
  const text = Array.isArray(toolResult?.content)
    ? toolResult.content.find(item => item !== null && typeof item === 'object' && typeof item.text === 'string')?.text
    : undefined
  return typeof text === 'string' ? text : undefined
}

function clientActionForTool(tool, data) {
  if (tool !== 'mcp__iot__create_rule_draft') return undefined
  const text = toolResultText(data)
  if (text === undefined || text.length > 65536) return undefined
  let result
  try { result = JSON.parse(text) } catch { return undefined }
  if (result?.kind !== 'ruleDraft' || result.draft === null || typeof result.draft !== 'object' || Array.isArray(result.draft)) return undefined
  const allowed = ['id', 'name', 'alarmType', 'level', 'match', 'conditions', 'durationSeconds', 'recovery', 'actions', 'expression', 'enabled', 'version']
  const draft = Object.fromEntries(allowed.filter(key => result.draft[key] !== undefined).map(key => [key, result.draft[key]]))
  return { type: 'RULE_DRAFT_READY', draft, persisted: result.persisted === true, requiresHumanApproval: true }
}

function turnFailure(result) {
  const turnEnd = [...result.events].reverse().find(event => event?.type === 'turn/end')
  const reason = turnEnd?.data?.reason
  if (reason === undefined || reason.kind === 'completed') return undefined
  const codes = {
    aborted: 'RUN_ABORTED',
    blocked: 'RUN_BLOCKED',
    error: 'MODEL_ERROR',
    'max-tokens': 'MAX_TOKENS',
    interrupted: 'RUN_INTERRUPTED',
  }
  return codes[reason.kind] ?? 'RUN_FAILED'
}

function safeRuntimeError(error) {
  const name = error instanceof Error ? error.name : ''
  if (name === 'RequestTimeoutError') return 'RUNTIME_TIMEOUT'
  if (name === 'TransportClosedError') return 'RUNTIME_CLOSED'
  if (name === 'JsonRpcResponseError') return 'RUNTIME_REJECTED'
  if (name === 'SdkProtocolError') return 'RUNTIME_PROTOCOL_ERROR'
  return 'RUNTIME_ERROR'
}

function proxyHeaders(request, mcpToken, bodyLength) {
  const headers = new Headers({
    authorization: `Bearer ${mcpToken}`,
    'content-length': String(bodyLength),
  })
  for (const name of ['accept', 'content-type', 'mcp-protocol-version', 'mcp-session-id']) {
    const value = request.headers[name]
    if (typeof value === 'string') headers.set(name, value)
  }
  return headers
}

function copyProxyResponseHeaders(upstream, response) {
  for (const name of ['cache-control', 'content-type', 'mcp-session-id', 'retry-after']) {
    const value = upstream.headers.get(name)
    if (value !== null) response.setHeader(name, value)
  }
  response.setHeader('x-content-type-options', 'nosniff')
}

function proxyProblem(response, status, code) {
  if (response.headersSent || response.destroyed) {
    if (!response.writableEnded) response.end()
    return
  }
  json(response, status, { error: { code, message: 'MCP loopback proxy rejected the request' } })
}

function createLoopbackMcpProxy(routes, options) {
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? '/', 'http://loopback.invalid')
      if (request.method !== 'POST' || url.pathname !== '/mcp' || url.search !== '') {
        throw new HttpError(404, 'PROXY_ROUTE_NOT_FOUND', 'proxy route not found')
      }
      const supplied = request.headers['x-iot-runtime-key']
      if (typeof supplied !== 'string' || supplied.length > 512) {
        throw new HttpError(401, 'RUNTIME_KEY_INVALID', 'runtime key is invalid')
      }
      const route = routes.get(hash(supplied))
      if (route === undefined || !constantTimeEqual(supplied, route.runtimeAccessKey)) {
        throw new HttpError(401, 'RUNTIME_KEY_INVALID', 'runtime key is invalid')
      }
      const body = await readBody(request, options.maximumBodyBytes)
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), options.timeoutMs)
      timeout.unref()
      let clientGone = false
      response.once('close', () => {
        if (!response.writableEnded) {
          clientGone = true
          controller.abort()
        }
      })
      try {
        const upstream = await fetch(route.mcpUrl, {
          method: 'POST',
          headers: proxyHeaders(request, route.mcpToken, body.length),
          body,
          redirect: 'error',
          signal: controller.signal,
        })
        route.lastUsed = Date.now()
        if (clientGone) return
        copyProxyResponseHeaders(upstream, response)
        response.writeHead(upstream.status)
        if (upstream.body === null) response.end()
        else await pipeline(Readable.fromWeb(upstream.body), response)
      } finally {
        clearTimeout(timeout)
      }
    } catch (error) {
      if (response.destroyed) return
      if (error instanceof HttpError) proxyProblem(response, error.status, error.code)
      else if (error?.name === 'AbortError') proxyProblem(response, 504, 'MCP_PROXY_TIMEOUT')
      else proxyProblem(response, 502, 'MCP_UPSTREAM_FAILED')
    }
  })
  server.headersTimeout = 10000
  server.requestTimeout = options.timeoutMs + 5000
  server.keepAliveTimeout = 5000
  return server
}

function childEnvironment(spec) {
  const environment = {}
  const inherited = [
    'DEEPSEEK_API_KEY',
    'DEEPSEEK_BASE_URL',
    'IOT_HARNESS_MODEL',
    'IOT_HARNESS_OPENAI_BASE_URL',
    'IOT_HARNESS_OPENAI_API_KEY',
    'IOT_HARNESS_CONTEXT_WINDOW',
    'HTTP_PROXY',
    'HTTPS_PROXY',
    'NO_PROXY',
    'NODE_USE_ENV_PROXY',
    'NODE_EXTRA_CA_CERTS',
    'SSL_CERT_FILE',
  ]
  for (const name of inherited) {
    if (process.env[name] !== undefined) environment[name] = process.env[name]
  }
  environment.HOME = process.env.HOME ?? '/tmp'
  environment.PATH = process.env.PATH ?? '/usr/local/bin:/usr/bin:/bin'
  environment.TMPDIR = process.env.TMPDIR ?? '/tmp'
  environment.IOT_MCP_URL = spec.proxyMcpUrl
  environment.IOT_MCP_RUNTIME_KEY = spec.runtimeAccessKey
  environment.IOT_HARNESS_SESSION_ROOT = spec.sessionRoot
  environment.IOT_OPS_PERSONA = spec.plugin.persona
  environment.IOT_ALLOWED_TOOLS_JSON = JSON.stringify(spec.plugin.allowedTools)
  if (spec.provider === 'openai-compatible') {
    // The SDK needs an auth placeholder for compatible APIs without an API Key.
    environment.IOT_HARNESS_OPENAI_BASE_URL = spec.baseUrl
    environment.IOT_HARNESS_OPENAI_API_KEY = spec.apiKey || 'EMPTY'
  } else {
    environment.DEEPSEEK_BASE_URL = spec.baseUrl
    environment.DEEPSEEK_API_KEY = spec.apiKey
  }
  environment.IOT_HARNESS_MODEL = spec.model
  return environment
}

async function officialHarnessFactory(spec) {
  const { DeepSeekHarness } = await import(pathToFileURL(spec.sdkClientModule).href)
  return new DeepSeekHarness({
    dshBin: spec.runtimeBin,
    profile: 'sdk-minimal',
    patches: [spec.patchFile],
    dshHome: spec.harnessHome,
    processCwd: spec.runtimeCwd,
    env: childEnvironment(spec),
    cwd: spec.workspace,
    provider: spec.provider,
    model: spec.model,
    maxTokens: spec.maxTokens,
    requestTimeoutMs: spec.requestTimeoutMs,
    shutdownTimeoutMs: 2000,
    disposeEofGraceMs: 6000,
    disposeGraceMs: 3000,
  })
}

function runtimeKey(run) {
  return hash(JSON.stringify({
    workflow: run.plugin,
    model: run.model,
    maxTokens: run.maxTokens,
  }))
}

function withTimeout(task, timeoutMs, onTimeout) {
  let timeout
  const deadline = new Promise((_, reject) => {
    timeout = setTimeout(() => {
      void Promise.resolve(onTimeout())
        .catch(() => {})
        .finally(() => reject(Object.assign(new Error('run timeout'), { name: 'RequestTimeoutError' })))
    }, timeoutMs)
  })
  return Promise.race([task, deadline]).finally(() => clearTimeout(timeout))
}

export function createGateway(options = {}) {
  const gatewayToken = options.gatewayToken ?? process.env.IOT_HARNESS_GATEWAY_TOKEN
  if (typeof gatewayToken !== 'string' || gatewayToken.length < 32 || gatewayToken.length > 512) {
    throw new Error('IOT_HARNESS_GATEWAY_TOKEN must contain 32 to 512 characters')
  }
  const pluginDir = resolve(options.pluginDir ?? process.env.IOT_HARNESS_PLUGIN_DIR ?? DEFAULT_PLUGIN_DIR)
  const patchFile = resolve(
    options.patchFile
      ?? options.cordisConfig
      ?? process.env.IOT_HARNESS_PATCH_FILE
      ?? process.env.DSH_CORDIS_CONFIG
      ?? DEFAULT_PATCH_FILE,
  )
  const runtimeBin = resolve(options.runtimeBin ?? process.env.DSH_RUNTIME_BIN ?? DEFAULT_RUNTIME_BIN)
  const sdkClientModule = resolve(
    options.sdkClientModule ?? process.env.DSH_SDK_CLIENT_MODULE ?? DEFAULT_SDK_CLIENT_MODULE,
  )
  const runtimeCwd = resolve(options.runtimeCwd ?? process.env.DSH_RUNTIME_CWD ?? '/harness/runtime-node')
  const workspace = resolve(options.workspace ?? process.env.IOT_HARNESS_WORKSPACE ?? DEFAULT_WORKSPACE)
  const sessionRoot = resolve(
    options.sessionRoot ?? process.env.IOT_HARNESS_SESSION_ROOT ?? DEFAULT_SESSION_ROOT,
  )
  const harnessHome = resolve(options.harnessHome ?? process.env.IOT_HARNESS_HOME ?? DEFAULT_HARNESS_HOME)
  const allowedOrigins = configuredOrigins(
    options.allowedMcpOrigins ?? process.env.IOT_HARNESS_MCP_ALLOWED_ORIGINS ?? DEFAULT_MCP_ORIGINS,
  )
  const instanceId = randomBytes(8).toString('hex')
  let modelProvider = options.modelProvider ?? process.env.IOT_HARNESS_PROVIDER ?? 'deepseek-official'
  if (!MODEL_PROVIDERS.includes(modelProvider)) {
    throw new Error('IOT_HARNESS_PROVIDER must be deepseek-official or openai-compatible')
  }
  let configuredModel = options.model
    ?? process.env.IOT_HARNESS_MODEL
    ?? process.env.IOT_AI_HARNESS_MODEL
    ?? 'deepseek-flash'
  if (typeof configuredModel !== 'string' || !MODEL_PATTERN.test(configuredModel.trim())) {
    throw new Error('IOT_HARNESS_MODEL must contain a valid model name')
  }
  configuredModel = configuredModel.trim()
  const openAICompatible = modelProvider === 'openai-compatible'
  let configuredBaseURL = options.baseURL
    ?? (openAICompatible ? process.env.IOT_HARNESS_OPENAI_BASE_URL : process.env.DEEPSEEK_BASE_URL)
    ?? (openAICompatible ? '' : 'https://api.deepseek.com')
  let configuredAPIKey = options.apiKey
    ?? (openAICompatible ? process.env.IOT_HARNESS_OPENAI_API_KEY : process.env.DEEPSEEK_API_KEY)
    ?? ''
  if (openAICompatible && !configuredBaseURL) {
    throw new Error('IOT_HARNESS_OPENAI_BASE_URL is required for the openai-compatible provider')
  }
  configuredBaseURL = typeof configuredBaseURL === 'string' ? configuredBaseURL.trim().replace(/\/$/, '') : configuredBaseURL
  configuredAPIKey = typeof configuredAPIKey === 'string' ? configuredAPIKey.trim() : ''
  const maximumBodyBytes = integerOption(options.maximumBodyBytes, 32768, 'maximumBodyBytes', 1024, 1048576)
  const backupLimits = {
    maxFiles: integerOption(options.backupMaxFiles, 4096, 'backupMaxFiles', 1, 16384),
    maxBytes: integerOption(options.backupMaxBytes, 64 * 1024 * 1024, 'backupMaxBytes', 1, 256 * 1024 * 1024),
  }
  const maxConcurrency = integerOption(
    options.maxConcurrency ?? process.env.IOT_HARNESS_MAX_CONCURRENCY,
    4,
    'IOT_HARNESS_MAX_CONCURRENCY',
    1,
    64,
  )
  const maxCachedConversations = integerOption(
    options.maxCachedConversations ?? process.env.IOT_HARNESS_MAX_CACHED_CONVERSATIONS,
    32,
    'IOT_HARNESS_MAX_CACHED_CONVERSATIONS',
    1,
    256,
  )
  const conversationTtlMs = integerOption(
    options.conversationTtlMs ?? process.env.IOT_HARNESS_CONVERSATION_TTL_MS,
    600000,
    'IOT_HARNESS_CONVERSATION_TTL_MS',
    1000,
    86400000,
  )
  const runTimeoutMs = integerOption(
    options.runTimeoutMs ?? process.env.IOT_HARNESS_RUN_TIMEOUT_MS,
    180000,
    'IOT_HARNESS_RUN_TIMEOUT_MS',
    1000,
    1800000,
  )
  const requestTimeoutMs = integerOption(
    options.requestTimeoutMs ?? process.env.IOT_HARNESS_RPC_TIMEOUT_MS,
    90000,
    'IOT_HARNESS_RPC_TIMEOUT_MS',
    1000,
    600000,
  )
  const proxyPort = integerOption(
    options.proxyPort ?? process.env.IOT_HARNESS_MCP_PROXY_PORT,
    8092,
    'IOT_HARNESS_MCP_PROXY_PORT',
    0,
    65535,
  )
  const proxyMaximumBodyBytes = integerOption(
    options.proxyMaximumBodyBytes ?? process.env.IOT_HARNESS_MCP_PROXY_MAX_BODY_BYTES,
    1048576,
    'IOT_HARNESS_MCP_PROXY_MAX_BODY_BYTES',
    1024,
    1048576,
  )
  const proxyTimeoutMs = integerOption(
    options.proxyTimeoutMs ?? process.env.IOT_HARNESS_MCP_PROXY_TIMEOUT_MS,
    65000,
    'IOT_HARNESS_MCP_PROXY_TIMEOUT_MS',
    1000,
    300000,
  )
  const harnessFactory = options.harnessFactory ?? officialHarnessFactory
  const activeRuns = new Set()
  const managedRuns = new Map()
  const reservedRunIds = new Set()
  const conversationLocks = new Map()
  const conversations = new Map()
  const runtimeRoutes = new Map()
  const pendingCloses = new Set()
  const unconfirmedCloses = new Set()
  const proxyServer = createLoopbackMcpProxy(runtimeRoutes, {
    maximumBodyBytes: proxyMaximumBodyBytes,
    timeoutMs: proxyTimeoutMs,
  })
  let proxyMcpUrl
  let closing = false
  let sweeping = false
  let snapshotInProgress = false
  let persistenceWriters = 0

  const withPersistenceWrite = async (handler, request, response, ...args) => {
    requireGatewayToken(request)
    if (snapshotInProgress) throw new HttpError(409, 'SNAPSHOT_BUSY', 'Harness snapshot is in progress; retry the operation')
    persistenceWriters++
    try { return await handler(request, response, ...args) } finally { persistenceWriters-- }
  }

  const acquireConversationLock = async (cacheKey, signal) => {
    signal?.throwIfAborted()
    let state = conversationLocks.get(cacheKey)
    if (state === undefined) {
      state = { locked: false, waiters: [] }
      conversationLocks.set(cacheKey, state)
    }
    if (state.locked) {
      if (state.waiters.length >= 8) {
        throw new HttpError(429, 'CONVERSATION_QUEUE_FULL', 'conversation queue is full')
      }
      await new Promise((resolveWaiter, reject) => {
        const waiter = () => { signal?.removeEventListener('abort', abort); resolveWaiter() }
        const abort = () => {
          const index = state.waiters.indexOf(waiter)
          if (index >= 0) state.waiters.splice(index, 1)
          reject(signal.reason)
        }
        state.waiters.push(waiter)
        signal?.addEventListener('abort', abort, { once: true })
      })
    } else {
      state.locked = true
    }
    let released = false
    return () => {
      if (released) return
      released = true
      const next = state.waiters.shift()
      if (next !== undefined) next()
      else {
        state.locked = false
        if (conversationLocks.get(cacheKey) === state) conversationLocks.delete(cacheKey)
      }
    }
  }

  const closeEntry = async (cacheKey, entry) => {
    if (entry.closing !== undefined) return entry.closing
    if (conversations.get(cacheKey) === entry) conversations.delete(cacheKey)
    runtimeRoutes.delete(entry.routeDigest)
    // The SDK owns its EOF/SIGTERM/SIGKILL reap ladder. Share the close promise
    // so stopping never releases capacity before process teardown completes.
    entry.closing = Promise.resolve().then(() => entry.harness.close()).catch(error => {
      entry.closeError = error
      unconfirmedCloses.add(entry)
    })
      .finally(() => pendingCloses.delete(entry.closing))
    pendingCloses.add(entry.closing)
    return entry.closing
  }

  const evictOne = async () => {
    const candidate = [...conversations.entries()]
      .filter(([key]) => !conversationLocks.has(key))
      .sort((left, right) => left[1].lastUsed - right[1].lastUsed)[0]
    if (candidate === undefined) return false
    await closeEntry(candidate[0], candidate[1])
    return true
  }

  const acquireHarness = async (run, mcpToken, cacheKey) => {
    const key = runtimeKey(run)
    let entry = conversations.get(cacheKey)
    if (entry !== undefined && entry.key !== key) {
      await closeEntry(cacheKey, entry)
      entry = undefined
    }
    if (entry === undefined) {
      while (conversations.size >= maxCachedConversations) {
        if (!await evictOne()) throw new HttpError(429, 'CONVERSATION_CAPACITY_EXCEEDED', 'conversation cache is full')
      }
      if (proxyMcpUrl === undefined) throw new Error('MCP loopback proxy is not listening')
      const runtimeAccessKey = randomBytes(32).toString('base64url')
      const routeDigest = hash(runtimeAccessKey)
      const route = { runtimeAccessKey, mcpUrl: run.mcpUrl, mcpToken, lastUsed: Date.now() }
      runtimeRoutes.set(routeDigest, route)
      const sessionId = `iot-${hash(run.conversationId).slice(0, 16)}-${randomBytes(12).toString('hex')}`
      let harness
      try {
        harness = await harnessFactory({
          ...run,
          provider: modelProvider,
          baseUrl: configuredBaseURL,
          apiKey: configuredAPIKey,
          runtimeBin,
          sdkClientModule,
          patchFile,
          runtimeCwd,
          workspace,
          sessionRoot,
          harnessHome: join(harnessHome, sessionId),
          proxyMcpUrl,
          runtimeAccessKey,
          requestTimeoutMs,
          sessionId,
        })
      } catch (error) {
        runtimeRoutes.delete(routeDigest)
        throw error
      }
      entry = { harness, key, lastUsed: Date.now(), route, routeDigest, sessionId }
      conversations.set(cacheKey, entry)
    } else {
      // The short-lived bearer changes independently of the resident Harness
      // process. Only the loopback route sees it; child env and JSONL never do.
      entry.route.mcpUrl = run.mcpUrl
      entry.route.mcpToken = mcpToken
      entry.route.lastUsed = Date.now()
    }
    entry.lastUsed = Date.now()
    return entry
  }

  const sweep = async () => {
    if (sweeping || closing || snapshotInProgress) return
    sweeping = true
    try {
      const cutoff = Date.now() - conversationTtlMs
      const stale = [...conversations.entries()]
        .filter(([key, entry]) => !conversationLocks.has(key) && entry.lastUsed < cutoff)
      await Promise.all(stale.map(([key, entry]) => closeEntry(key, entry)))
    } finally {
      sweeping = false
    }
  }
  const sweepTimer = setInterval(() => { void sweep() }, Math.min(60000, conversationTtlMs))
  sweepTimer.unref()

  const requireGatewayToken = (request) => {
    const supplied = request.headers['x-iot-harness-token']
    if (typeof supplied !== 'string' || !constantTimeEqual(supplied, gatewayToken)) {
      throw new HttpError(401, 'HARNESS_TOKEN_INVALID', 'X-IOT-Harness-Token is invalid')
    }
  }

  const handleBackupSnapshot = async (request, response) => {
    requireGatewayToken(request)
    if (request.headers['transfer-encoding'] !== undefined || Number(request.headers['content-length'] ?? 0) !== 0) {
      throw new HttpError(400, 'SNAPSHOT_BODY_NOT_ALLOWED', 'snapshot does not accept a request body')
    }
    if (closing || snapshotInProgress || persistenceWriters > 0 || managedRuns.size > 0
      || pendingCloses.size > 0 || unconfirmedCloses.size > 0) {
      throw new HttpError(409, 'SNAPSHOT_BUSY', 'Harness is writing persistence; retry after workflow runs finish')
    }
    // Acquire before any await, so a manifest mutation or initializing run
    // cannot enter between the idle check and the first filesystem read.
    snapshotInProgress = true
    try {
      const snapshot = await captureBackupSnapshot(pluginDir, sessionRoot, backupLimits, options.backupAfterRead)
      json(response, 200, snapshot)
    } finally {
      snapshotInProgress = false
    }
  }

  const handleHealth = async (response) => {
    try {
      const [plugins, revision] = await Promise.all([
        loadPluginCatalog(pluginDir),
        readFile(join(here, 'REVISION'), 'utf8'),
        access(patchFile),
        access(runtimeBin),
        access(sdkClientModule),
      ])
      json(response, 200, {
        status: closing ? 'stopping' : 'ok',
        revision: revision.trim(),
        pluginCount: plugins.filter(plugin => plugin.enabled).length,
        activeRuns: activeRuns.size,
        cachedConversations: conversations.size,
        mcpProxy: proxyServer.listening ? 'ready' : 'not-ready',
        modelProvider,
        model: configuredModel,
        deepseekConfigured: modelProvider === 'deepseek-official' && Boolean(configuredAPIKey),
      })
    } catch {
      json(response, 503, { status: 'not-ready' })
    }
  }

  const handleProviderConfig = async (request, response) => {
    requireGatewayToken(request)
    if (request.method === 'GET') {
      json(response, 200, publicProviderConfig(modelProvider, configuredBaseURL, configuredModel, configuredAPIKey, instanceId))
      return
    }
    if (request.method !== 'PUT') throw new HttpError(405, 'METHOD_NOT_ALLOWED', 'method is not allowed')
    if (managedRuns.size > 0) throw new HttpError(409, 'RUNS_ACTIVE', 'wait for active workflow runs to finish before changing the provider')
    const candidate = validatedProviderConfig(await readJson(request, maximumBodyBytes))
    modelProvider = candidate.provider
    configuredBaseURL = candidate.baseUrl
    configuredModel = candidate.model
    configuredAPIKey = candidate.apiKey
    await Promise.all([...conversations.entries()].map(([key, entry]) => closeEntry(key, entry)))
    json(response, 200, publicProviderConfig(modelProvider, configuredBaseURL, configuredModel, configuredAPIKey, instanceId))
  }

  const handlePlugins = async (request, response) => {
    requireGatewayToken(request)
    const plugins = await loadPluginCatalog(pluginDir)
    json(response, 200, { items: plugins.filter(plugin => plugin.enabled).map(publicPlugin) })
  }

  const handleAdminPlugins = async (request, response) => {
    requireGatewayToken(request)
    const plugins = await loadPluginCatalog(pluginDir)
    json(response, 200, { items: plugins.map(adminPlugin), count: plugins.length })
  }

  const handleSavePlugin = async (request, response) => {
    requireGatewayToken(request)
    const raw = await readJson(request, maximumBodyBytes)
    let candidate
    try {
      candidate = validatedManifest(raw, 'submitted-agent.json')
    } catch (error) {
      throw new HttpError(422, 'AGENT_MANIFEST_INVALID', error instanceof Error ? error.message : 'agent manifest is invalid')
    }
    if (BUILTIN_PLUGIN_IDS.has(candidate.id)) {
      throw new HttpError(409, 'BUILTIN_PLUGIN_IMMUTABLE', 'built-in workflow plugins cannot be overwritten')
    }
    await mkdir(pluginDir, { recursive: true })
    const target = join(pluginDir, `${candidate.id}.json`)
    let updating = true
    try { await access(target) } catch { updating = false }
    const current = await loadPluginCatalog(pluginDir)
    if (!updating && current.some(plugin => plugin.id === candidate.id)) {
      throw new HttpError(409, 'WORKFLOW_ALREADY_EXISTS', 'a workflow with this id already exists')
    }
    const temporary = join(pluginDir, `.${candidate.id}.${randomBytes(8).toString('hex')}.tmp`)
    await writeFile(temporary, `${JSON.stringify(raw, null, 2)}\n`, { encoding: 'utf8', mode: 0o600, flag: 'wx' })
    await rename(temporary, target)
    json(response, updating ? 200 : 201, publicPlugin(candidate))
  }

  const handleDeletePlugin = async (request, response, workflowID) => {
    requireGatewayToken(request)
    if (!ID_PATTERN.test(workflowID)) {
      throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format')
    }
    if (BUILTIN_PLUGIN_IDS.has(workflowID)) {
      throw new HttpError(409, 'BUILTIN_PLUGIN_IMMUTABLE', 'built-in workflow plugins cannot be deleted')
    }
    const target = join(pluginDir, `${workflowID}.json`)
    try {
      await unlink(target)
    } catch (error) {
      if (error?.code === 'ENOENT') {
        throw new HttpError(404, 'WORKFLOW_NOT_FOUND', 'workflow plugin was not found')
      }
      throw error
    }
    json(response, 200, { deleted: true, id: workflowID })
  }

  const handleRuns = async (request, response, runId) => {
    requireGatewayToken(request)
    const tenant = text(request.headers['x-iot-tenant-id'], 'tenantId', 128, ID_PATTERN)
    if (runId === undefined) {
      const items = [...managedRuns.values()].filter(item => item.tenantId === tenant)
        .map(({ controller, ...item }) => item)
      json(response, 200, { items })
      return
    }
    const item = managedRuns.get(runId)
    if (item === undefined || item.tenantId !== tenant) throw new HttpError(404, 'RUN_NOT_FOUND', 'run is not active')
    if (item.status === 'stop_failed') throw new HttpError(503, 'RUN_STOP_FAILED', 'runtime exit could not be confirmed; restart Harness')
    item.status = 'stopping'
    item.controller.abort(new HttpError(409, 'RUN_STOPPED', 'AI 工作流已被管理员强制停止'))
    json(response, 202, { runId, status: 'stopping' })
  }

  const handleChat = async (request, response) => {
    requireGatewayToken(request)
    const mcpToken = bearerToken(request.headers.authorization)
    if (mcpToken === undefined) throw new HttpError(401, 'MCP_TOKEN_INVALID', 'Authorization Bearer token is required')
    const plugins = await loadPluginCatalog(pluginDir)
    const run = validatedBody(
      await readJson(request, maximumBodyBytes),
      plugins,
      allowedOrigins,
      configuredModel,
    )
    if (modelProvider === 'deepseek-official' && !configuredAPIKey) {
      throw new HttpError(503, 'API_KEY_REQUIRED', '请在模型管理中填写 DeepSeek API Key，测试并应用后启用 AI 功能')
    }
    const cacheKey = run.conversationId
    if (reservedRunIds.has(run.runId)) throw new HttpError(409, 'RUN_ALREADY_ACTIVE', 'runId is already active')
    reservedRunIds.add(run.runId)
    const controller = new AbortController()
    const control = { runId: run.runId, tenantId: run.tenantId, actor: run.actor,
      workflowId: run.workflowId, workflowName: run.plugin.name, model: run.model,
      status: 'queued', startedAt: Date.now(), controller }
    managedRuns.set(run.runId, control)
    let entry
    let clientGone = false
    let responseFinished = false
    let stopFailed = false
    const onClientGone = () => {
      if (responseFinished) return
      clientGone = true
      controller.abort(new HttpError(409, 'RUN_ABORTED', 'client disconnected'))
    }
    response.on('close', onClientGone)
    let releaseConversation
    const clearPending = () => {
      managedRuns.delete(run.runId)
      reservedRunIds.delete(run.runId)
      response.removeListener('close', onClientGone)
    }
    try {
      releaseConversation = await acquireConversationLock(cacheKey, controller.signal)
    } catch (error) {
      clearPending()
      throw error
    }
    if (closing) {
      clearPending()
      releaseConversation()
      throw new HttpError(503, 'GATEWAY_STOPPING', 'gateway is stopping')
    }
    if (activeRuns.size >= maxConcurrency) {
      clearPending()
      releaseConversation()
      throw new HttpError(429, 'CAPACITY_EXCEEDED', 'gateway concurrency limit reached')
    }

    activeRuns.add(run.runId)
    if (!controller.signal.aborted) control.status = 'starting'
    response.writeHead(200, {
      'cache-control': 'no-store',
      'content-type': 'application/x-ndjson; charset=utf-8',
      'x-accel-buffering': 'no',
      'x-content-type-options': 'nosniff',
    })
    const emit = eventWriter(response, run)
    emit('run.started', {
      conversationId: run.conversationId,
      workflowId: run.workflowId,
      model: run.model,
      maxTokens: run.maxTokens,
    })
    const calls = new Map()
    // Token usage of every model step; reported with the terminal event.
    const usage = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, reasoningTokens: 0 }
    let usageReported = false
    const accounting = () => ({ ...(usageReported ? { usage: { ...usage } } : {}), toolCalls: calls.size })
    let emittedText = false
    let rejectStopped
    const stopped = new Promise((_, reject) => { rejectStopped = reject })
    // A handler is attached immediately, including while the runtime starts.
    stopped.catch(() => {})
    const onStopped = () => {
      control.status = 'stopping'
      if (entry !== undefined) void closeEntry(cacheKey, entry)
      rejectStopped(controller.signal.reason)
    }
    controller.signal.addEventListener('abort', onStopped, { once: true })

    try {
      controller.signal.throwIfAborted()
      entry = await acquireHarness(run, mcpToken, cacheKey)
      controller.signal.throwIfAborted()
      control.status = 'running'
      const result = await withTimeout(Promise.race([stopped, entry.harness.run(run.question, {
        sessionId: entry.sessionId,
        onNotification(notification) {
          if (notification?.method === 'iot.text.delta') {
            const params = notification.params
            if (params?.sessionId === entry.sessionId && typeof params.text === 'string' && params.text !== '') {
              emittedText = true
              emit('text.delta', { delta: params.text })
            }
            return
          }
          const event = sessionEvent(notification, entry.sessionId)
          if (event === undefined) return
          if (event.type === 'assistant/message') {
            usageReported = addUsage(usage, event.data?.usage) || usageReported
            return
          }
          if (event.type === 'tool/call') {
            const callId = event.data?.callId
            const tool = event.data?.name
            if (typeof callId === 'string' && typeof tool === 'string') {
              calls.set(callId, tool)
              emit('tool.started', { callId, tool })
            }
            return
          }
          if (event.type === 'tool/result') {
            const callId = event.data?.callId ?? event.data?.message?.source?.callId
            if (typeof callId === 'string') {
              const tool = calls.get(callId)
              const clientAction = clientActionForTool(tool, event.data)
              emit('tool.completed', {
                callId,
                ...(tool === undefined ? {} : { tool }),
                success: !toolResultFailed(event.data),
                ...(clientAction === undefined ? {} : { data: { clientAction } }),
              })
            }
          }
        },
      })]), runTimeoutMs, () => closeEntry(cacheKey, entry))
      controller.signal.throwIfAborted()
      entry.lastUsed = Date.now()
      if (!emittedText && result.finalResponse !== '') emit('text.delta', { delta: result.finalResponse })
      const failure = turnFailure(result)
      if (failure === undefined) {
        emit('run.completed', {
          conversationId: run.conversationId,
          workflowId: run.workflowId,
          ...accounting(),
        })
      } else {
        emit('run.failed', { code: failure, message: 'Harness run did not complete successfully', ...accounting() })
      }
    } catch (error) {
      if (entry !== undefined) await closeEntry(cacheKey, entry)
      stopFailed = controller.signal.aborted && entry?.closeError !== undefined
      if (stopFailed) control.status = 'stop_failed'
      if (!clientGone) {
        const manualStop = controller.signal.reason?.code === 'RUN_STOPPED'
        emit('run.failed', { code: stopFailed ? 'RUN_STOP_FAILED' : manualStop ? 'RUN_STOPPED' : safeRuntimeError(error), message: stopFailed ? '无法确认工作流进程退出，请联系管理员重启 Harness' : manualStop ? 'AI 工作流已被管理员强制停止' : 'Harness runtime request failed', ...accounting() })
      }
    } finally {
      // A failed process reap must not advertise a free slot or permit model
      // changes while an unconfirmed child may still be alive.
      if (!stopFailed) {
        activeRuns.delete(run.runId)
        clearPending()
        releaseConversation()
      }
      response.removeListener('close', onClientGone)
      controller.signal.removeEventListener('abort', onStopped)
      responseFinished = true
      if (!response.writableEnded && !response.destroyed) response.end()
    }
  }

  const route = async (request, response) => {
    const url = new URL(request.url ?? '/', 'http://gateway.invalid')
    if (url.search !== '') throw new HttpError(400, 'QUERY_NOT_ALLOWED', 'query parameters are not supported')
    if (request.method === 'GET' && url.pathname === '/health') return handleHealth(response)
    if (request.method === 'GET' && url.pathname === '/v1/backup/snapshot') return handleBackupSnapshot(request, response)
    if (request.method === 'GET' && url.pathname === '/v1/runs') return handleRuns(request, response)
    if (request.method === 'POST' && url.pathname.startsWith('/v1/runs/') && url.pathname.endsWith('/stop')) {
      const raw = url.pathname.slice('/v1/runs/'.length, -'/stop'.length)
      let runId
      try { runId = decodeURIComponent(raw) } catch { throw new HttpError(422, 'RUN_ID_INVALID', 'invalid run ID') }
      text(runId, 'runId', 128, ID_PATTERN)
      return handleRuns(request, response, runId)
    }
    if (['GET', 'PUT', 'POST', 'DELETE'].includes(request.method) && url.pathname === '/v1/provider') {
      return request.method === 'PUT'
        ? withPersistenceWrite(handleProviderConfig, request, response)
        : handleProviderConfig(request, response)
    }
    if (request.method === 'GET' && url.pathname === '/v1/plugins/admin') return handleAdminPlugins(request, response)
    if (request.method === 'GET' && url.pathname === '/v1/plugins') return handlePlugins(request, response)
    if (request.method === 'POST' && url.pathname === '/v1/plugins') return withPersistenceWrite(handleSavePlugin, request, response)
    if (request.method === 'DELETE' && url.pathname.startsWith('/v1/plugins/')) {
      const encodedID = url.pathname.slice('/v1/plugins/'.length)
      if (encodedID === '' || encodedID.includes('/')) throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format')
      let workflowID
      try { workflowID = decodeURIComponent(encodedID) } catch { throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format') }
      return withPersistenceWrite(handleDeletePlugin, request, response, workflowID)
    }
    if (request.method === 'POST' && url.pathname === '/v1/chat/stream') return withPersistenceWrite(handleChat, request, response)
    if (['/health', '/v1/plugins', '/v1/plugins/admin', '/v1/chat/stream', '/v1/backup/snapshot'].includes(url.pathname)) {
      throw new HttpError(405, 'METHOD_NOT_ALLOWED', 'method is not allowed')
    }
    throw new HttpError(404, 'NOT_FOUND', 'route not found')
  }

  const server = createServer((request, response) => {
    void route(request, response).catch((error) => {
      if (!response.headersSent && ['SNAPSHOT_BUSY', 'SNAPSHOT_CHANGED'].includes(error?.code)) response.setHeader('retry-after', '1')
      if (!response.headersSent) problem(response, error)
      else if (!response.writableEnded && !response.destroyed) response.end()
    })
  })
  server.headersTimeout = 10000
  server.requestTimeout = 30000
  server.keepAliveTimeout = 5000

  return {
    server,
    async listen(port = 8091, host = '0.0.0.0') {
      if (!proxyServer.listening) {
        await new Promise((resolveListen, reject) => {
          const onError = error => {
            proxyServer.off('listening', onListening)
            reject(error)
          }
          const onListening = () => {
            proxyServer.off('error', onError)
            resolveListen()
          }
          proxyServer.once('error', onError)
          proxyServer.once('listening', onListening)
          proxyServer.listen(proxyPort, '127.0.0.1')
        })
        const address = proxyServer.address()
        if (address === null || typeof address === 'string') throw new Error('MCP loopback proxy has no TCP address')
        proxyMcpUrl = `http://127.0.0.1:${address.port}/mcp`
      }
      await new Promise((resolveListen, reject) => {
        const onError = error => {
          server.off('listening', onListening)
          reject(error)
        }
        const onListening = () => {
          server.off('error', onError)
          resolveListen()
        }
        server.once('error', onError)
        server.once('listening', onListening)
        server.listen(port, host)
      })
      return server.address()
    },
    async close() {
      if (closing) return
      closing = true
      clearInterval(sweepTimer)
      for (const item of managedRuns.values()) item.controller.abort(new HttpError(503, 'GATEWAY_STOPPING', 'gateway is stopping'))
      await Promise.all([...conversations.entries()].map(([key, entry]) => closeEntry(key, entry)))
      if (server.listening) {
        await new Promise(resolveClose => server.close(() => resolveClose()))
      }
      if (proxyServer.listening) {
        await new Promise(resolveClose => proxyServer.close(() => resolveClose()))
      }
    },
  }
}

async function main() {
  const gateway = createGateway()
  const host = process.env.IOT_HARNESS_HOST ?? '0.0.0.0'
  const port = integerOption(process.env.IOT_HARNESS_PORT, 8091, 'IOT_HARNESS_PORT', 1, 65535)
  await gateway.listen(port, host)
  process.stderr.write(`iot-harness-gateway listening on ${host}:${port}\n`)
  let stopping = false
  const stop = async () => {
    if (stopping) return
    stopping = true
    await gateway.close()
  }
  process.once('SIGTERM', () => { void stop().then(() => process.exit(0)) })
  process.once('SIGINT', () => { void stop().then(() => process.exit(130)) })
}

if (process.argv[1] !== undefined && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await main()
}
