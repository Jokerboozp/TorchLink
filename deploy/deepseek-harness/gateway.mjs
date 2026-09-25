import { createHash, randomBytes, timingSafeEqual } from 'node:crypto' /* 引入当前代码需要的依赖。 */
import { access, mkdir, readFile, readdir, rename, stat, unlink, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { dirname, join, resolve } from 'node:path' /* 引入当前代码需要的依赖。 */
import { Readable } from 'node:stream' /* 引入当前代码需要的依赖。 */
import { pipeline } from 'node:stream/promises' /* 引入当前代码需要的依赖。 */
import { fileURLToPath, pathToFileURL } from 'node:url' /* 引入当前代码需要的依赖。 */

const here = dirname(fileURLToPath(import.meta.url)) /* 声明 here。 */
const DEFAULT_PLUGIN_DIR = join(here, 'plugins') /* 声明 DEFAULT_PLUGIN_DIR。 */
const DEFAULT_PATCH_FILE = join(here, 'cordis.yml') /* 声明 DEFAULT_PATCH_FILE。 */
const DEFAULT_RUNTIME_BIN = '/harness/runtime-node/node_modules/@deepseek-ai/dsh/lib/bin.js' /* 声明 DEFAULT_RUNTIME_BIN。 */
const DEFAULT_SDK_CLIENT_MODULE = '/harness/runtime-node/node_modules/@deepseek-ai/dsh-sdk-client/lib/index.js' /* 声明 DEFAULT_SDK_CLIENT_MODULE。 */
const DEFAULT_WORKSPACE = '/data/workspace' /* 声明 DEFAULT_WORKSPACE。 */
const DEFAULT_SESSION_ROOT = '/data/sessions' /* 声明 DEFAULT_SESSION_ROOT。 */
const DEFAULT_HARNESS_HOME = '/data/runtime-home' /* 声明 DEFAULT_HARNESS_HOME。 */
const DEFAULT_MCP_ORIGINS = 'http://platform-api:8080' /* 声明 DEFAULT_MCP_ORIGINS。 */

export const READ_ONLY_TOOL_CEILING = Object.freeze([ /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_system_overview', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_device_latest', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_alarm_list', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_property_history', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_similar_alarms', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_knowledge_base', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__create_rule_draft', /* 执行当前语句并推进处理流程。 */
]) /* 结束当前表达式或代码块。 */

const readOnlyToolCeiling = new Set(READ_ONLY_TOOL_CEILING) /* 声明 readOnlyToolCeiling。 */
const MANIFEST_KEYS = new Set([ /* 声明 MANIFEST_KEYS。 */
  'schemaVersion', /* 执行当前语句并推进处理流程。 */
  'id', /* 执行当前语句并推进处理流程。 */
  'name', /* 执行当前语句并推进处理流程。 */
  'description', /* 执行当前语句并推进处理流程。 */
  'version', /* 执行当前语句并推进处理流程。 */
  'enabled', /* 执行当前语句并推进处理流程。 */
  'persona', /* 执行当前语句并推进处理流程。 */
  'defaultModel', /* 执行当前语句并推进处理流程。 */
  'maxTokens', /* 执行当前语句并推进处理流程。 */
  'capabilities', /* 执行当前语句并推进处理流程。 */
  'allowedTools', /* 执行当前语句并推进处理流程。 */
]) /* 结束当前表达式或代码块。 */
const BODY_KEYS = new Set([ /* 声明 BODY_KEYS。 */
  'runId', /* 执行当前语句并推进处理流程。 */
  'conversationId', /* 执行当前语句并推进处理流程。 */
  'workflowId', /* 执行当前语句并推进处理流程。 */
  'question', /* 执行当前语句并推进处理流程。 */
  'mcpUrl', /* 执行当前语句并推进处理流程。 */
  'model', /* 执行当前语句并推进处理流程。 */
  'maxTokens', /* 执行当前语句并推进处理流程。 */
]) /* 结束当前表达式或代码块。 */
const PROVIDER_KEYS = new Set(['provider', 'baseUrl', 'model', 'apiKey']) /* 声明 PROVIDER_KEYS。 */
const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/ /* 声明 ID_PATTERN。 */
const MODEL_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/ /* 声明 MODEL_PATTERN。 */
const CAPABILITY_PATTERN = /^[^\u0000-\u001f\u007f]{1,64}$/u /* 声明 CAPABILITY_PATTERN。 */
const BUILTIN_PLUGIN_IDS = new Set(['alarm-handler', 'ops-assistant', 'system-observer', 'device-health-inspector', 'protocol-assistant']) /* 声明 BUILTIN_PLUGIN_IDS。 */

class HttpError extends Error { /* 定义 HttpError 类。 */
  constructor(status, code, message) { /* 执行当前语句并推进处理流程。 */
    super(message) /* 执行当前语句并推进处理流程。 */
    this.name = 'HttpError' /* 更新 this.name 的值。 */
    this.status = status /* 更新 this.status 的值。 */
    this.code = code /* 更新 this.code 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function integerOption(value, fallback, name, minimum, maximum) { /* 定义 integerOption 函数。 */
  const resolved = value === undefined || value === '' ? fallback : Number(value) /* 声明 resolved。 */
  if (!Number.isSafeInteger(resolved) || resolved < minimum || resolved > maximum) { /* 判断条件并选择处理分支。 */
    throw new Error(`${name} must be an integer from ${minimum} to ${maximum}`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return resolved /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function text(value, name, maxLength, pattern) { /* 定义 text 函数。 */
  if (typeof value !== 'string' || value.trim() === '' || value.length > maxLength) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'INVALID_REQUEST', `${name} is required and must be at most ${maxLength} characters`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (pattern !== undefined && !pattern.test(value)) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'INVALID_REQUEST', `${name} has an invalid format`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function hash(value) { /* 定义 hash 函数。 */
  return createHash('sha256').update(value).digest('hex') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function constantTimeEqual(actual, expected) { /* 定义 constantTimeEqual 函数。 */
  const left = Buffer.from(actual) /* 声明 left。 */
  const right = Buffer.from(expected) /* 声明 right。 */
  return left.length === right.length && timingSafeEqual(left, right) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function bearerToken(header) { /* 定义 bearerToken 函数。 */
  if (typeof header !== 'string' || header.length > 8192) return undefined /* 判断条件并选择处理分支。 */
  const match = /^Bearer ([^\s]+)$/i.exec(header) /* 声明 match。 */
  return match?.[1] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function configuredOrigins(value) { /* 定义 configuredOrigins 函数。 */
  const origins = new Set() /* 声明 origins。 */
  for (const item of value.split(',')) { /* 循环处理当前数据。 */
    const candidate = item.trim() /* 声明 candidate。 */
    if (candidate === '') continue /* 判断条件并选择处理分支。 */
    const url = new URL(candidate) /* 声明 url。 */
    if (!['http:', 'https:'].includes(url.protocol) || url.username !== '' || url.password !== '') { /* 判断条件并选择处理分支。 */
      throw new Error(`invalid MCP allowed origin: ${candidate}`) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    origins.add(url.origin) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  if (origins.size === 0) throw new Error('at least one MCP origin must be allowed') /* 判断条件并选择处理分支。 */
  return origins /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function validatedMcpUrl(value, allowedOrigins) { /* 定义 validatedMcpUrl 函数。 */
  const raw = text(value, 'mcpUrl', 2048) /* 声明 raw。 */
  let url /* 声明 url。 */
  try { /* 执行当前语句并推进处理流程。 */
    url = new URL(raw) /* 更新 url 的值。 */
  } catch { /* 结束当前表达式或代码块。 */
    throw new HttpError(422, 'MCP_URL_INVALID', 'mcpUrl must be an absolute HTTP(S) URL') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!['http:', 'https:'].includes(url.protocol) /* 判断条件并选择处理分支。 */
    || url.username !== '' /* 执行当前语句并推进处理流程。 */
    || url.password !== '' /* 执行当前语句并推进处理流程。 */
    || url.hash !== '' /* 执行当前语句并推进处理流程。 */
    || url.search !== '' /* 执行当前语句并推进处理流程。 */
    || url.pathname !== '/mcp/harness') { /* 执行当前语句并推进处理流程。 */
    throw new HttpError(422, 'MCP_URL_INVALID', 'mcpUrl must be an allowed origin with the exact /mcp/harness path') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!allowedOrigins.has(url.origin)) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'MCP_ORIGIN_NOT_ALLOWED', 'mcpUrl origin is not allowed') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return url.href /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function validatedManifest(raw, filename) { /* 定义 validatedManifest 函数。 */
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) { /* 判断条件并选择处理分支。 */
    throw new Error(`${filename}: manifest must be an object`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const unknown = Object.keys(raw).filter(key => !MANIFEST_KEYS.has(key)) /* 声明 unknown。 */
  if (unknown.length > 0) throw new Error(`${filename}: unknown field(s): ${unknown.join(', ')}`) /* 判断条件并选择处理分支。 */
  if (raw.schemaVersion !== 1) throw new Error(`${filename}: schemaVersion must be 1`) /* 判断条件并选择处理分支。 */
  const id = manifestString(raw.id, filename, 'id', 128, ID_PATTERN) /* 声明 id。 */
  const name = manifestString(raw.name, filename, 'name', 128) /* 声明 name。 */
  const description = manifestString(raw.description, filename, 'description', 1024) /* 声明 description。 */
  const version = manifestString(raw.version, filename, 'version', 64) /* 声明 version。 */
  const persona = manifestString(raw.persona, filename, 'persona', 16384) /* 声明 persona。 */
  const defaultModel = manifestString(raw.defaultModel, filename, 'defaultModel', 128, MODEL_PATTERN) /* 声明 defaultModel。 */
  if (typeof raw.enabled !== 'boolean') throw new Error(`${filename}: enabled must be a boolean`) /* 判断条件并选择处理分支。 */
  if (!Number.isSafeInteger(raw.maxTokens) || raw.maxTokens < 1 || raw.maxTokens > 262144) { /* 判断条件并选择处理分支。 */
    throw new Error(`${filename}: maxTokens must be an integer from 1 to 262144`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!Array.isArray(raw.capabilities) || raw.capabilities.length === 0 || raw.capabilities.length > 32) { /* 判断条件并选择处理分支。 */
    throw new Error(`${filename}: capabilities must be an array containing 1 to 32 items`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const capabilities = [] /* 声明 capabilities。 */
  const seenCapabilities = new Set() /* 声明 seenCapabilities。 */
  for (const capability of raw.capabilities) { /* 循环处理当前数据。 */
    if (typeof capability !== 'string' || !CAPABILITY_PATTERN.test(capability)) { /* 判断条件并选择处理分支。 */
      throw new Error(`${filename}: capability has an invalid format: ${String(capability)}`) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (seenCapabilities.has(capability)) throw new Error(`${filename}: duplicate capability: ${capability}`) /* 判断条件并选择处理分支。 */
    seenCapabilities.add(capability) /* 执行当前语句并推进处理流程。 */
    capabilities.push(capability) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  if (!Array.isArray(raw.allowedTools) || raw.allowedTools.length === 0) { /* 判断条件并选择处理分支。 */
    throw new Error(`${filename}: allowedTools must be a non-empty array`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const allowedTools = [] /* 声明 allowedTools。 */
  const seen = new Set() /* 声明 seen。 */
  for (const tool of raw.allowedTools) { /* 循环处理当前数据。 */
    if (typeof tool !== 'string' || !readOnlyToolCeiling.has(tool)) { /* 判断条件并选择处理分支。 */
      throw new Error(`${filename}: tool is outside the read-only security ceiling: ${String(tool)}`) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (seen.has(tool)) throw new Error(`${filename}: duplicate allowed tool: ${tool}`) /* 判断条件并选择处理分支。 */
    seen.add(tool) /* 执行当前语句并推进处理流程。 */
    allowedTools.push(tool) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return Object.freeze({ /* 返回当前处理结果。 */
    schemaVersion: 1, /* 执行当前语句并推进处理流程。 */
    id, /* 执行当前语句并推进处理流程。 */
    name, /* 执行当前语句并推进处理流程。 */
    description, /* 执行当前语句并推进处理流程。 */
    version, /* 执行当前语句并推进处理流程。 */
    enabled: raw.enabled, /* 执行当前语句并推进处理流程。 */
    persona, /* 执行当前语句并推进处理流程。 */
    defaultModel, /* 执行当前语句并推进处理流程。 */
    maxTokens: raw.maxTokens, /* 执行当前语句并推进处理流程。 */
    capabilities: Object.freeze(capabilities), /* 执行当前语句并推进处理流程。 */
    allowedTools: Object.freeze(allowedTools), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function manifestString(value, filename, name, maxLength, pattern) { /* 定义 manifestString 函数。 */
  if (typeof value !== 'string' || value.trim() === '' || value.length > maxLength) { /* 判断条件并选择处理分支。 */
    throw new Error(`${filename}: ${name} must be a non-empty string of at most ${maxLength} characters`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (pattern !== undefined && !pattern.test(value)) throw new Error(`${filename}: ${name} has an invalid format`) /* 判断条件并选择处理分支。 */
  return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export async function loadPluginCatalog(pluginDir = DEFAULT_PLUGIN_DIR) { /* 执行当前语句并推进处理流程。 */
  const entries = (await readdir(pluginDir, { withFileTypes: true })) /* 声明 entries。 */
    .filter(entry => entry.isFile() && entry.name.endsWith('.json')) /* 执行当前语句并推进处理流程。 */
    .sort((left, right) => left.name.localeCompare(right.name)) /* 执行当前语句并推进处理流程。 */
  if (entries.length === 0) throw new Error(`no plugin manifests found in ${pluginDir}`) /* 判断条件并选择处理分支。 */
  if (entries.length > 64) throw new Error(`too many plugin manifests in ${pluginDir}`) /* 判断条件并选择处理分支。 */
  const plugins = [] /* 声明 plugins。 */
  const ids = new Set() /* 声明 ids。 */
  for (const entry of entries) { /* 循环处理当前数据。 */
    const path = join(pluginDir, entry.name) /* 声明 path。 */
    const metadata = await stat(path) /* 声明 metadata。 */
    if (metadata.size > 65536) throw new Error(`${entry.name}: manifest exceeds 65536 bytes`) /* 判断条件并选择处理分支。 */
    let parsed /* 声明 parsed。 */
    try { /* 执行当前语句并推进处理流程。 */
      parsed = JSON.parse(await readFile(path, 'utf8')) /* 更新 parsed 的值。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      throw new Error(`${entry.name}: invalid JSON`, { cause: error }) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    const plugin = validatedManifest(parsed, entry.name) /* 声明 plugin。 */
    if (ids.has(plugin.id)) throw new Error(`${entry.name}: duplicate plugin id: ${plugin.id}`) /* 判断条件并选择处理分支。 */
    ids.add(plugin.id) /* 执行当前语句并推进处理流程。 */
    plugins.push(plugin) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return Object.freeze(plugins) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function publicPlugin(plugin) { /* 定义 publicPlugin 函数。 */
  return { /* 返回当前处理结果。 */
    schemaVersion: plugin.schemaVersion, /* 执行当前语句并推进处理流程。 */
    id: plugin.id, /* 执行当前语句并推进处理流程。 */
    name: plugin.name, /* 执行当前语句并推进处理流程。 */
    description: plugin.description, /* 执行当前语句并推进处理流程。 */
    version: plugin.version, /* 执行当前语句并推进处理流程。 */
    enabled: plugin.enabled, /* 执行当前语句并推进处理流程。 */
    defaultModel: plugin.defaultModel, /* 执行当前语句并推进处理流程。 */
    maxTokens: plugin.maxTokens, /* 执行当前语句并推进处理流程。 */
    capabilities: [...plugin.capabilities], /* 执行当前语句并推进处理流程。 */
    knowledgeEnabled: plugin.allowedTools.includes('mcp__iot__query_knowledge_base'), /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function adminPlugin(plugin) { /* 定义 adminPlugin 函数。 */
  return { /* 返回当前处理结果。 */
    schemaVersion: plugin.schemaVersion, /* 执行当前语句并推进处理流程。 */
    id: plugin.id, /* 执行当前语句并推进处理流程。 */
    name: plugin.name, /* 执行当前语句并推进处理流程。 */
    description: plugin.description, /* 执行当前语句并推进处理流程。 */
    version: plugin.version, /* 执行当前语句并推进处理流程。 */
    enabled: plugin.enabled, /* 执行当前语句并推进处理流程。 */
    persona: plugin.persona, /* 执行当前语句并推进处理流程。 */
    defaultModel: plugin.defaultModel, /* 执行当前语句并推进处理流程。 */
    maxTokens: plugin.maxTokens, /* 执行当前语句并推进处理流程。 */
    capabilities: [...plugin.capabilities], /* 执行当前语句并推进处理流程。 */
    allowedTools: [...plugin.allowedTools], /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function readBody(request, maxBytes) { /* 定义 readBody 函数。 */
  const chunks = [] /* 声明 chunks。 */
  let size = 0 /* 声明 size。 */
  for await (const chunk of request) { /* 循环处理当前数据。 */
    size += chunk.length /* 更新 size 的值。 */
    if (size > maxBytes) throw new HttpError(413, 'REQUEST_TOO_LARGE', 'request body is too large') /* 判断条件并选择处理分支。 */
    chunks.push(chunk) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return Buffer.concat(chunks) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function readJson(request, maxBytes) { /* 定义 readJson 函数。 */
  const contentType = request.headers['content-type'] ?? '' /* 声明 contentType。 */
  if (!contentType.toLowerCase().startsWith('application/json')) { /* 判断条件并选择处理分支。 */
    throw new HttpError(415, 'CONTENT_TYPE_REQUIRED', 'Content-Type must be application/json') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const body = await readBody(request, maxBytes) /* 声明 body。 */
  try { /* 执行当前语句并推进处理流程。 */
    return JSON.parse(body.toString('utf8')) /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    throw new HttpError(400, 'INVALID_JSON', 'request body must be valid JSON') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function validatedBody(raw, plugins, allowedOrigins, modelOverride) { /* 定义 validatedBody 函数。 */
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'INVALID_REQUEST', 'request body must be an object') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const unknown = Object.keys(raw).filter(key => !BODY_KEYS.has(key)) /* 声明 unknown。 */
  if (unknown.length > 0) throw new HttpError(422, 'INVALID_REQUEST', `unknown field(s): ${unknown.join(', ')}`) /* 判断条件并选择处理分支。 */
  const runId = text(raw.runId, 'runId', 128, ID_PATTERN) /* 声明 runId。 */
  const conversationId = text(raw.conversationId, 'conversationId', 128, ID_PATTERN) /* 声明 conversationId。 */
  const workflowId = text(raw.workflowId, 'workflowId', 128, ID_PATTERN) /* 声明 workflowId。 */
  const question = text(raw.question, 'question', 20000) /* 声明 question。 */
  const plugin = plugins.find(candidate => candidate.id === workflowId && candidate.enabled) /* 声明 plugin。 */
  if (plugin === undefined) throw new HttpError(404, 'WORKFLOW_NOT_FOUND', 'workflow plugin is not available') /* 判断条件并选择处理分支。 */
  const requestedModel = raw.model === undefined || raw.model === '' /* 声明 requestedModel。 */
    ? plugin.defaultModel /* 执行当前语句并推进处理流程。 */
    : text(raw.model, 'model', 128, MODEL_PATTERN) /* 执行当前语句并推进处理流程。 */
  const model = modelOverride ?? requestedModel /* 声明 model。 */
  const maxTokens = raw.maxTokens === undefined || raw.maxTokens === null /* 声明 maxTokens。 */
    ? plugin.maxTokens /* 执行当前语句并推进处理流程。 */
    : raw.maxTokens /* 执行当前语句并推进处理流程。 */
  if (!Number.isSafeInteger(maxTokens) || maxTokens < 1 || maxTokens > 262144) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'MAX_TOKENS_INVALID', 'maxTokens must be an integer from 1 to 262144') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return { /* 返回当前处理结果。 */
    runId, /* 执行当前语句并推进处理流程。 */
    conversationId, /* 执行当前语句并推进处理流程。 */
    workflowId, /* 执行当前语句并推进处理流程。 */
    question, /* 执行当前语句并推进处理流程。 */
    mcpUrl: validatedMcpUrl(raw.mcpUrl, allowedOrigins), /* 执行当前语句并推进处理流程。 */
    model, /* 执行当前语句并推进处理流程。 */
    // Provider settings are shared across workflows; each plugin retains its ceiling.
    maxTokens: Math.min(maxTokens, plugin.maxTokens),
    plugin, /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function json(response, status, body) { /* 定义 json 函数。 */
  const payload = `${JSON.stringify(body)}\n` /* 声明 payload。 */
  response.writeHead(status, { /* 执行当前语句并推进处理流程。 */
    'cache-control': 'no-store', /* 执行当前语句并推进处理流程。 */
    'content-length': Buffer.byteLength(payload), /* 执行当前语句并推进处理流程。 */
    'content-type': 'application/json; charset=utf-8', /* 执行当前语句并推进处理流程。 */
    'x-content-type-options': 'nosniff', /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  response.end(payload) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function problem(response, error) { /* 定义 problem 函数。 */
  const status = error instanceof HttpError ? error.status : 500 /* 声明 status。 */
  const code = error instanceof HttpError ? error.code : 'INTERNAL_ERROR' /* 声明 code。 */
  const message = error instanceof HttpError ? error.message : 'internal gateway error' /* 声明 message。 */
  json(response, status, { error: { code, message } }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function eventWriter(response, run) { /* 定义 eventWriter 函数。 */
  return (type, data = {}) => { /* 返回当前处理结果。 */
    if (response.destroyed || response.writableEnded) return false /* 判断条件并选择处理分支。 */
    response.write(`${JSON.stringify({ type, runId: run.runId, time: Date.now(), ...data })}\n`) /* 执行当前语句并推进处理流程。 */
    return true /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function sessionEvent(notification, sessionId) { /* 定义 sessionEvent 函数。 */
  if (notification?.method !== 'session.event' || notification.params?.sessionId !== sessionId) return undefined /* 判断条件并选择处理分支。 */
  const event = notification.params.event /* 声明 event。 */
  return event !== null && typeof event === 'object' ? event : undefined /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function toolResultFailed(data) { /* 定义 toolResultFailed 函数。 */
  if (data?.error !== undefined) return true /* 判断条件并选择处理分支。 */
  const content = data?.message?.content /* 声明 content。 */
  return Array.isArray(content) && content.some(item => item !== null && typeof item === 'object' && item.isError === true) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function validatedProviderConfig(raw) { /* 定义 validatedProviderConfig 函数。 */
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'INVALID_REQUEST', 'provider configuration must be an object') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const unknown = Object.keys(raw).filter(key => !PROVIDER_KEYS.has(key)) /* 声明 unknown。 */
  if (unknown.length > 0) throw new HttpError(422, 'INVALID_REQUEST', `unknown field(s): ${unknown.join(', ')}`) /* 判断条件并选择处理分支。 */
  const provider = text(raw.provider, 'provider', 64).trim().toLowerCase() /* 声明 provider。 */
  if (!['ollama', 'deepseek-official'].includes(provider)) { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'PROVIDER_INVALID', 'provider must be ollama or deepseek-official') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const model = text(raw.model, 'model', 128).trim() /* 声明 model。 */
  if (!MODEL_PATTERN.test(model)) throw new HttpError(422, 'INVALID_REQUEST', 'model has an invalid format') /* 判断条件并选择处理分支。 */
  const baseUrl = text(raw.baseUrl, 'baseUrl', 2048).trim() /* 声明 baseUrl。 */
  let parsed /* 声明 parsed。 */
  try { parsed = new URL(baseUrl) } catch { throw new HttpError(422, 'BASE_URL_INVALID', 'baseUrl must be an absolute HTTP(S) URL') } /* 执行当前语句并推进处理流程。 */
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username !== '' || parsed.password !== '' || parsed.search !== '' || parsed.hash !== '') { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'BASE_URL_INVALID', 'baseUrl must be an HTTP(S) URL without credentials or query parameters') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const apiKey = typeof raw.apiKey === 'string' ? raw.apiKey.trim() : '' /* 声明 apiKey。 */
  if (provider === 'deepseek-official' && apiKey === '') { /* 判断条件并选择处理分支。 */
    throw new HttpError(422, 'API_KEY_REQUIRED', 'apiKey is required for API providers') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return { provider, baseUrl: parsed.href.replace(/\/$/, ''), model, apiKey } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function publicProviderConfig(provider, baseUrl, model, apiKey) { /* 定义 publicProviderConfig 函数。 */
  return { /* 返回当前处理结果。 */
    provider, /* 执行当前语句并推进处理流程。 */
    baseUrl, /* 执行当前语句并推进处理流程。 */
    model, /* 执行当前语句并推进处理流程。 */
    apiKeyConfigured: provider !== 'ollama' && typeof apiKey === 'string' && apiKey.trim() !== '', /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function toolResultText(data) { /* 定义 toolResultText 函数。 */
  if (typeof data?.result === 'string') return data.result /* 判断条件并选择处理分支。 */
  if (data?.result !== null && typeof data?.result === 'object') return JSON.stringify(data.result) /* 判断条件并选择处理分支。 */
  const content = data?.message?.content /* 声明 content。 */
  if (!Array.isArray(content)) return undefined /* 判断条件并选择处理分支。 */
  const direct = content.find(item => item !== null && typeof item === 'object' && typeof item.text === 'string')?.text /* 声明 direct。 */
  if (typeof direct === 'string') return direct /* 判断条件并选择处理分支。 */
  const toolResult = content.find(item => item !== null && typeof item === 'object' && item.type === 'tool-result') /* 声明 toolResult。 */
  const text = Array.isArray(toolResult?.content) /* 声明 text。 */
    ? toolResult.content.find(item => item !== null && typeof item === 'object' && typeof item.text === 'string')?.text /* 执行当前语句并推进处理流程。 */
    : undefined /* 执行当前语句并推进处理流程。 */
  return typeof text === 'string' ? text : undefined /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function clientActionForTool(tool, data) { /* 定义 clientActionForTool 函数。 */
  if (tool !== 'mcp__iot__create_rule_draft') return undefined /* 判断条件并选择处理分支。 */
  const text = toolResultText(data) /* 声明 text。 */
  if (text === undefined || text.length > 65536) return undefined /* 判断条件并选择处理分支。 */
  let result /* 声明 result。 */
  try { result = JSON.parse(text) } catch { return undefined } /* 执行当前语句并推进处理流程。 */
  if (result?.kind !== 'ruleDraft' || result.draft === null || typeof result.draft !== 'object' || Array.isArray(result.draft)) return undefined /* 判断条件并选择处理分支。 */
  const allowed = ['id', 'name', 'alarmType', 'level', 'match', 'conditions', 'durationSeconds', 'recovery', 'actions', 'expression', 'enabled', 'version'] /* 声明 allowed。 */
  const draft = Object.fromEntries(allowed.filter(key => result.draft[key] !== undefined).map(key => [key, result.draft[key]])) /* 声明 draft。 */
  return { type: 'RULE_DRAFT_READY', draft, persisted: result.persisted === true, requiresHumanApproval: true } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function turnFailure(result) { /* 定义 turnFailure 函数。 */
  const turnEnd = [...result.events].reverse().find(event => event?.type === 'turn/end') /* 声明 turnEnd。 */
  const reason = turnEnd?.data?.reason /* 声明 reason。 */
  if (reason === undefined || reason.kind === 'completed') return undefined /* 判断条件并选择处理分支。 */
  const codes = { /* 声明 codes。 */
    aborted: 'RUN_ABORTED', /* 执行当前语句并推进处理流程。 */
    blocked: 'RUN_BLOCKED', /* 执行当前语句并推进处理流程。 */
    error: 'MODEL_ERROR', /* 执行当前语句并推进处理流程。 */
    'max-tokens': 'MAX_TOKENS', /* 执行当前语句并推进处理流程。 */
    interrupted: 'RUN_INTERRUPTED', /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return codes[reason.kind] ?? 'RUN_FAILED' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function safeRuntimeError(error) { /* 定义 safeRuntimeError 函数。 */
  const name = error instanceof Error ? error.name : '' /* 声明 name。 */
  if (name === 'RequestTimeoutError') return 'RUNTIME_TIMEOUT' /* 判断条件并选择处理分支。 */
  if (name === 'TransportClosedError') return 'RUNTIME_CLOSED' /* 判断条件并选择处理分支。 */
  if (name === 'JsonRpcResponseError') return 'RUNTIME_REJECTED' /* 判断条件并选择处理分支。 */
  if (name === 'SdkProtocolError') return 'RUNTIME_PROTOCOL_ERROR' /* 判断条件并选择处理分支。 */
  return 'RUNTIME_ERROR' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function proxyHeaders(request, mcpToken, bodyLength) { /* 定义 proxyHeaders 函数。 */
  const headers = new Headers({ /* 声明 headers。 */
    authorization: `Bearer ${mcpToken}`, /* 执行当前语句并推进处理流程。 */
    'content-length': String(bodyLength), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  for (const name of ['accept', 'content-type', 'mcp-protocol-version', 'mcp-session-id']) { /* 循环处理当前数据。 */
    const value = request.headers[name] /* 声明 value。 */
    if (typeof value === 'string') headers.set(name, value) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  return headers /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function copyProxyResponseHeaders(upstream, response) { /* 定义 copyProxyResponseHeaders 函数。 */
  for (const name of ['cache-control', 'content-type', 'mcp-session-id', 'retry-after']) { /* 循环处理当前数据。 */
    const value = upstream.headers.get(name) /* 声明 value。 */
    if (value !== null) response.setHeader(name, value) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  response.setHeader('x-content-type-options', 'nosniff') /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function proxyProblem(response, status, code) { /* 定义 proxyProblem 函数。 */
  if (response.headersSent || response.destroyed) { /* 判断条件并选择处理分支。 */
    if (!response.writableEnded) response.end() /* 判断条件并选择处理分支。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  json(response, status, { error: { code, message: 'MCP loopback proxy rejected the request' } }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function createLoopbackMcpProxy(routes, options) { /* 定义 createLoopbackMcpProxy 函数。 */
  const server = createServer(async (request, response) => { /* 声明 server。 */
    try { /* 执行当前语句并推进处理流程。 */
      const url = new URL(request.url ?? '/', 'http://loopback.invalid') /* 声明 url。 */
      if (request.method !== 'POST' || url.pathname !== '/mcp' || url.search !== '') { /* 判断条件并选择处理分支。 */
        throw new HttpError(404, 'PROXY_ROUTE_NOT_FOUND', 'proxy route not found') /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      const supplied = request.headers['x-iot-runtime-key'] /* 声明 supplied。 */
      if (typeof supplied !== 'string' || supplied.length > 512) { /* 判断条件并选择处理分支。 */
        throw new HttpError(401, 'RUNTIME_KEY_INVALID', 'runtime key is invalid') /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      const route = routes.get(hash(supplied)) /* 声明 route。 */
      if (route === undefined || !constantTimeEqual(supplied, route.runtimeAccessKey)) { /* 判断条件并选择处理分支。 */
        throw new HttpError(401, 'RUNTIME_KEY_INVALID', 'runtime key is invalid') /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      const body = await readBody(request, options.maximumBodyBytes) /* 声明 body。 */
      const controller = new AbortController() /* 声明 controller。 */
      const timeout = setTimeout(() => controller.abort(), options.timeoutMs) /* 声明 timeout。 */
      timeout.unref() /* 执行当前语句并推进处理流程。 */
      let clientGone = false /* 声明 clientGone。 */
      response.once('close', () => { /* 执行当前语句并推进处理流程。 */
        if (!response.writableEnded) { /* 判断条件并选择处理分支。 */
          clientGone = true /* 更新 clientGone 的值。 */
          controller.abort() /* 执行当前语句并推进处理流程。 */
        } /* 结束当前表达式或代码块。 */
      }) /* 结束当前表达式或代码块。 */
      try { /* 执行当前语句并推进处理流程。 */
        const upstream = await fetch(route.mcpUrl, { /* 声明 upstream。 */
          method: 'POST', /* 执行当前语句并推进处理流程。 */
          headers: proxyHeaders(request, route.mcpToken, body.length), /* 执行当前语句并推进处理流程。 */
          body, /* 执行当前语句并推进处理流程。 */
          redirect: 'error', /* 执行当前语句并推进处理流程。 */
          signal: controller.signal, /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
        route.lastUsed = Date.now() /* 更新 route.lastUsed 的值。 */
        if (clientGone) return /* 判断条件并选择处理分支。 */
        copyProxyResponseHeaders(upstream, response) /* 执行当前语句并推进处理流程。 */
        response.writeHead(upstream.status) /* 执行当前语句并推进处理流程。 */
        if (upstream.body === null) response.end() /* 判断条件并选择处理分支。 */
        else await pipeline(Readable.fromWeb(upstream.body), response) /* 执行当前语句并推进处理流程。 */
      } finally { /* 结束当前表达式或代码块。 */
        clearTimeout(timeout) /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      if (response.destroyed) return /* 判断条件并选择处理分支。 */
      if (error instanceof HttpError) proxyProblem(response, error.status, error.code) /* 判断条件并选择处理分支。 */
      else if (error?.name === 'AbortError') proxyProblem(response, 504, 'MCP_PROXY_TIMEOUT') /* 判断条件并选择处理分支。 */
      else proxyProblem(response, 502, 'MCP_UPSTREAM_FAILED') /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  server.headersTimeout = 10000 /* 更新 server.headersTimeout 的值。 */
  server.requestTimeout = options.timeoutMs + 5000 /* 更新 server.requestTimeout 的值。 */
  server.keepAliveTimeout = 5000 /* 更新 server.keepAliveTimeout 的值。 */
  return server /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function childEnvironment(spec) { /* 定义 childEnvironment 函数。 */
  const environment = {} /* 声明 environment。 */
  const inherited = [ /* 声明 inherited。 */
    'DEEPSEEK_API_KEY', /* 执行当前语句并推进处理流程。 */
    'DEEPSEEK_BASE_URL', /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MODEL', /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_OLLAMA_BASE_URL', /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_OLLAMA_API_KEY', /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_CONTEXT_WINDOW', /* 执行当前语句并推进处理流程。 */
    'HTTP_PROXY', /* 执行当前语句并推进处理流程。 */
    'HTTPS_PROXY', /* 执行当前语句并推进处理流程。 */
    'NO_PROXY', /* 执行当前语句并推进处理流程。 */
    'NODE_USE_ENV_PROXY', /* 执行当前语句并推进处理流程。 */
    'NODE_EXTRA_CA_CERTS', /* 执行当前语句并推进处理流程。 */
    'SSL_CERT_FILE', /* 执行当前语句并推进处理流程。 */
  ] /* 结束当前表达式或代码块。 */
  for (const name of inherited) { /* 循环处理当前数据。 */
    if (process.env[name] !== undefined) environment[name] = process.env[name] /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  environment.HOME = process.env.HOME ?? '/tmp' /* 更新 environment.HOME 的值。 */
  environment.PATH = process.env.PATH ?? '/usr/local/bin:/usr/bin:/bin' /* 更新 environment.PATH 的值。 */
  environment.TMPDIR = process.env.TMPDIR ?? '/tmp' /* 更新 environment.TMPDIR 的值。 */
  environment.IOT_MCP_URL = spec.proxyMcpUrl /* 更新 environment.IOT_MCP_URL 的值。 */
  environment.IOT_MCP_RUNTIME_KEY = spec.runtimeAccessKey /* 更新 environment.IOT_MCP_RUNTIME_KEY 的值。 */
  environment.IOT_HARNESS_SESSION_ROOT = spec.sessionRoot /* 更新 environment.IOT_HARNESS_SESSION_ROOT 的值。 */
  environment.IOT_OPS_PERSONA = spec.plugin.persona /* 更新 environment.IOT_OPS_PERSONA 的值。 */
  environment.IOT_ALLOWED_TOOLS_JSON = JSON.stringify(spec.plugin.allowedTools) /* 更新 environment.IOT_ALLOWED_TOOLS_JSON 的值。 */
  if (spec.provider === 'ollama') { /* 判断条件并选择处理分支。 */
    environment.IOT_HARNESS_OLLAMA_BASE_URL = spec.baseUrl /* 更新 environment.IOT_HARNESS_OLLAMA_BASE_URL 的值。 */
    environment.IOT_HARNESS_OLLAMA_API_KEY = spec.apiKey || 'ollama' /* 更新 environment.IOT_HARNESS_OLLAMA_API_KEY 的值。 */
  } else { /* 结束当前表达式或代码块。 */
    environment.DEEPSEEK_BASE_URL = spec.baseUrl /* 更新 environment.DEEPSEEK_BASE_URL 的值。 */
    environment.DEEPSEEK_API_KEY = spec.apiKey /* 更新 environment.DEEPSEEK_API_KEY 的值。 */
  } /* 结束当前表达式或代码块。 */
  environment.IOT_HARNESS_MODEL = spec.model /* 更新 environment.IOT_HARNESS_MODEL 的值。 */
  return environment /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function officialHarnessFactory(spec) { /* 定义 officialHarnessFactory 函数。 */
  const { DeepSeekHarness } = await import(pathToFileURL(spec.sdkClientModule).href) /* 执行当前语句并推进处理流程。 */
  return new DeepSeekHarness({ /* 返回当前处理结果。 */
    dshBin: spec.runtimeBin, /* 执行当前语句并推进处理流程。 */
    profile: 'sdk-minimal', /* 执行当前语句并推进处理流程。 */
    patches: [spec.patchFile], /* 执行当前语句并推进处理流程。 */
    dshHome: spec.harnessHome, /* 执行当前语句并推进处理流程。 */
    processCwd: spec.runtimeCwd, /* 执行当前语句并推进处理流程。 */
    env: childEnvironment(spec), /* 执行当前语句并推进处理流程。 */
    cwd: spec.workspace, /* 执行当前语句并推进处理流程。 */
    provider: spec.provider, /* 执行当前语句并推进处理流程。 */
    model: spec.model, /* 执行当前语句并推进处理流程。 */
    maxTokens: spec.maxTokens, /* 执行当前语句并推进处理流程。 */
    requestTimeoutMs: spec.requestTimeoutMs, /* 执行当前语句并推进处理流程。 */
    shutdownTimeoutMs: 2000, /* 执行当前语句并推进处理流程。 */
    disposeEofGraceMs: 6000, /* 执行当前语句并推进处理流程。 */
    disposeGraceMs: 3000, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function runtimeKey(run) { /* 定义 runtimeKey 函数。 */
  return hash(JSON.stringify({ /* 返回当前处理结果。 */
    workflow: run.plugin, /* 执行当前语句并推进处理流程。 */
    model: run.model, /* 执行当前语句并推进处理流程。 */
    maxTokens: run.maxTokens, /* 执行当前语句并推进处理流程。 */
  })) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function withTimeout(task, timeoutMs, onTimeout) { /* 定义 withTimeout 函数。 */
  let timeout /* 声明 timeout。 */
  const deadline = new Promise((_, reject) => { /* 声明 deadline。 */
    timeout = setTimeout(() => { /* 更新 timeout 的值。 */
      void Promise.resolve(onTimeout()) /* 执行当前语句并推进处理流程。 */
        .catch(() => {}) /* 执行当前语句并推进处理流程。 */
        .finally(() => reject(Object.assign(new Error('run timeout'), { name: 'RequestTimeoutError' }))) /* 执行当前语句并推进处理流程。 */
    }, timeoutMs) /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  return Promise.race([task, deadline]).finally(() => clearTimeout(timeout)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function createGateway(options = {}) { /* 执行当前语句并推进处理流程。 */
  const gatewayToken = options.gatewayToken ?? process.env.IOT_HARNESS_GATEWAY_TOKEN /* 声明 gatewayToken。 */
  if (typeof gatewayToken !== 'string' || gatewayToken.length < 32 || gatewayToken.length > 512) { /* 判断条件并选择处理分支。 */
    throw new Error('IOT_HARNESS_GATEWAY_TOKEN must contain 32 to 512 characters') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const pluginDir = resolve(options.pluginDir ?? process.env.IOT_HARNESS_PLUGIN_DIR ?? DEFAULT_PLUGIN_DIR) /* 声明 pluginDir。 */
  const patchFile = resolve( /* 声明 patchFile。 */
    options.patchFile /* 执行当前语句并推进处理流程。 */
      ?? options.cordisConfig /* 执行当前语句并推进处理流程。 */
      ?? process.env.IOT_HARNESS_PATCH_FILE /* 执行当前语句并推进处理流程。 */
      ?? process.env.DSH_CORDIS_CONFIG /* 执行当前语句并推进处理流程。 */
      ?? DEFAULT_PATCH_FILE, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const runtimeBin = resolve(options.runtimeBin ?? process.env.DSH_RUNTIME_BIN ?? DEFAULT_RUNTIME_BIN) /* 声明 runtimeBin。 */
  const sdkClientModule = resolve( /* 声明 sdkClientModule。 */
    options.sdkClientModule ?? process.env.DSH_SDK_CLIENT_MODULE ?? DEFAULT_SDK_CLIENT_MODULE, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const runtimeCwd = resolve(options.runtimeCwd ?? process.env.DSH_RUNTIME_CWD ?? '/harness/runtime-node') /* 声明 runtimeCwd。 */
  const workspace = resolve(options.workspace ?? process.env.IOT_HARNESS_WORKSPACE ?? DEFAULT_WORKSPACE) /* 声明 workspace。 */
  const sessionRoot = resolve( /* 声明 sessionRoot。 */
    options.sessionRoot ?? process.env.IOT_HARNESS_SESSION_ROOT ?? DEFAULT_SESSION_ROOT, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const harnessHome = resolve(options.harnessHome ?? process.env.IOT_HARNESS_HOME ?? DEFAULT_HARNESS_HOME) /* 声明 harnessHome。 */
  const allowedOrigins = configuredOrigins( /* 声明 allowedOrigins。 */
    options.allowedMcpOrigins ?? process.env.IOT_HARNESS_MCP_ALLOWED_ORIGINS ?? DEFAULT_MCP_ORIGINS, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  let modelProvider = options.modelProvider ?? process.env.IOT_HARNESS_PROVIDER ?? 'ollama' /* 声明 modelProvider。 */
  if (!['deepseek-official', 'ollama'].includes(modelProvider)) { /* 判断条件并选择处理分支。 */
    throw new Error('IOT_HARNESS_PROVIDER must be deepseek-official or ollama') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  let configuredModel = options.model /* 声明 configuredModel。 */
    ?? process.env.IOT_HARNESS_MODEL /* 执行当前语句并推进处理流程。 */
    ?? process.env.IOT_AI_HARNESS_MODEL /* 执行当前语句并推进处理流程。 */
    ?? 'qwen3:1.7b' /* 执行当前语句并推进处理流程。 */
  if (typeof configuredModel !== 'string' || !MODEL_PATTERN.test(configuredModel.trim())) { /* 判断条件并选择处理分支。 */
    throw new Error('IOT_HARNESS_MODEL must contain a valid model name') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  configuredModel = configuredModel.trim() /* 更新 configuredModel 的值。 */
  let configuredBaseURL = options.baseURL /* 声明 configuredBaseURL。 */
    ?? (modelProvider === 'ollama' /* 执行当前语句并推进处理流程。 */
      ? process.env.IOT_HARNESS_OLLAMA_BASE_URL /* 执行当前语句并推进处理流程。 */
      : process.env.DEEPSEEK_BASE_URL) /* 执行当前语句并推进处理流程。 */
    ?? (modelProvider === 'ollama' ? 'http://ollama:11434/v1' : 'https://api.deepseek.com') /* 执行当前语句并推进处理流程。 */
  let configuredAPIKey = options.apiKey /* 声明 configuredAPIKey。 */
    ?? (modelProvider === 'ollama' /* 执行当前语句并推进处理流程。 */
      ? process.env.IOT_HARNESS_OLLAMA_API_KEY /* 执行当前语句并推进处理流程。 */
      : process.env.DEEPSEEK_API_KEY) /* 执行当前语句并推进处理流程。 */
    ?? (modelProvider === 'ollama' ? 'ollama' : '') /* 执行当前语句并推进处理流程。 */
  configuredBaseURL = typeof configuredBaseURL === 'string' ? configuredBaseURL.trim().replace(/\/$/, '') : configuredBaseURL /* 更新 configuredBaseURL 的值。 */
  configuredAPIKey = typeof configuredAPIKey === 'string' ? configuredAPIKey.trim() : '' /* 更新 configuredAPIKey 的值。 */
  const maximumBodyBytes = integerOption(options.maximumBodyBytes, 32768, 'maximumBodyBytes', 1024, 1048576) /* 声明 maximumBodyBytes。 */
  const maxConcurrency = integerOption( /* 声明 maxConcurrency。 */
    options.maxConcurrency ?? process.env.IOT_HARNESS_MAX_CONCURRENCY, /* 执行当前语句并推进处理流程。 */
    4, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MAX_CONCURRENCY', /* 执行当前语句并推进处理流程。 */
    1, /* 执行当前语句并推进处理流程。 */
    64, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const maxCachedConversations = integerOption( /* 声明 maxCachedConversations。 */
    options.maxCachedConversations ?? process.env.IOT_HARNESS_MAX_CACHED_CONVERSATIONS, /* 执行当前语句并推进处理流程。 */
    32, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MAX_CACHED_CONVERSATIONS', /* 执行当前语句并推进处理流程。 */
    1, /* 执行当前语句并推进处理流程。 */
    256, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const conversationTtlMs = integerOption( /* 声明 conversationTtlMs。 */
    options.conversationTtlMs ?? process.env.IOT_HARNESS_CONVERSATION_TTL_MS, /* 执行当前语句并推进处理流程。 */
    600000, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_CONVERSATION_TTL_MS', /* 执行当前语句并推进处理流程。 */
    1000, /* 执行当前语句并推进处理流程。 */
    86400000, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const runTimeoutMs = integerOption( /* 声明 runTimeoutMs。 */
    options.runTimeoutMs ?? process.env.IOT_HARNESS_RUN_TIMEOUT_MS, /* 执行当前语句并推进处理流程。 */
    180000, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_RUN_TIMEOUT_MS', /* 执行当前语句并推进处理流程。 */
    1000, /* 执行当前语句并推进处理流程。 */
    1800000, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const requestTimeoutMs = integerOption( /* 声明 requestTimeoutMs。 */
    options.requestTimeoutMs ?? process.env.IOT_HARNESS_RPC_TIMEOUT_MS, /* 执行当前语句并推进处理流程。 */
    90000, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_RPC_TIMEOUT_MS', /* 执行当前语句并推进处理流程。 */
    1000, /* 执行当前语句并推进处理流程。 */
    600000, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const proxyPort = integerOption( /* 声明 proxyPort。 */
    options.proxyPort ?? process.env.IOT_HARNESS_MCP_PROXY_PORT, /* 执行当前语句并推进处理流程。 */
    8092, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MCP_PROXY_PORT', /* 执行当前语句并推进处理流程。 */
    0, /* 执行当前语句并推进处理流程。 */
    65535, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const proxyMaximumBodyBytes = integerOption( /* 声明 proxyMaximumBodyBytes。 */
    options.proxyMaximumBodyBytes ?? process.env.IOT_HARNESS_MCP_PROXY_MAX_BODY_BYTES, /* 执行当前语句并推进处理流程。 */
    1048576, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MCP_PROXY_MAX_BODY_BYTES', /* 执行当前语句并推进处理流程。 */
    1024, /* 执行当前语句并推进处理流程。 */
    1048576, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const proxyTimeoutMs = integerOption( /* 声明 proxyTimeoutMs。 */
    options.proxyTimeoutMs ?? process.env.IOT_HARNESS_MCP_PROXY_TIMEOUT_MS, /* 执行当前语句并推进处理流程。 */
    65000, /* 执行当前语句并推进处理流程。 */
    'IOT_HARNESS_MCP_PROXY_TIMEOUT_MS', /* 执行当前语句并推进处理流程。 */
    1000, /* 执行当前语句并推进处理流程。 */
    300000, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
  const harnessFactory = options.harnessFactory ?? officialHarnessFactory /* 声明 harnessFactory。 */
  const activeRuns = new Set() /* 声明 activeRuns。 */
  const reservedRunIds = new Set() /* 声明 reservedRunIds。 */
  const activeConversations = new Set() /* 声明 activeConversations。 */
  const conversationLocks = new Map() /* 声明 conversationLocks。 */
  const conversations = new Map() /* 声明 conversations。 */
  const runtimeRoutes = new Map() /* 声明 runtimeRoutes。 */
  const proxyServer = createLoopbackMcpProxy(runtimeRoutes, { /* 声明 proxyServer。 */
    maximumBodyBytes: proxyMaximumBodyBytes, /* 执行当前语句并推进处理流程。 */
    timeoutMs: proxyTimeoutMs, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  let proxyMcpUrl /* 声明 proxyMcpUrl。 */
  let closing = false /* 声明 closing。 */
  let sweeping = false /* 声明 sweeping。 */

  const acquireConversationLock = async (cacheKey) => { /* 声明 acquireConversationLock。 */
    let state = conversationLocks.get(cacheKey) /* 声明 state。 */
    if (state === undefined) { /* 判断条件并选择处理分支。 */
      state = { locked: false, waiters: [] } /* 更新 state 的值。 */
      conversationLocks.set(cacheKey, state) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    if (state.locked) { /* 判断条件并选择处理分支。 */
      if (state.waiters.length >= 8) { /* 判断条件并选择处理分支。 */
        throw new HttpError(429, 'CONVERSATION_QUEUE_FULL', 'conversation queue is full') /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      await new Promise(resolveWaiter => state.waiters.push(resolveWaiter)) /* 等待异步操作完成。 */
    } else { /* 结束当前表达式或代码块。 */
      state.locked = true /* 更新 state.locked 的值。 */
    } /* 结束当前表达式或代码块。 */
    let released = false /* 声明 released。 */
    return () => { /* 返回当前处理结果。 */
      if (released) return /* 判断条件并选择处理分支。 */
      released = true /* 更新 released 的值。 */
      const next = state.waiters.shift() /* 声明 next。 */
      if (next !== undefined) next() /* 判断条件并选择处理分支。 */
      else { /* 执行当前语句并推进处理流程。 */
        state.locked = false /* 更新 state.locked 的值。 */
        if (conversationLocks.get(cacheKey) === state) conversationLocks.delete(cacheKey) /* 判断条件并选择处理分支。 */
      } /* 结束当前表达式或代码块。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */

  const closeEntry = async (cacheKey, entry) => { /* 声明 closeEntry。 */
    if (conversations.get(cacheKey) === entry) conversations.delete(cacheKey) /* 判断条件并选择处理分支。 */
    runtimeRoutes.delete(entry.routeDigest) /* 执行当前语句并推进处理流程。 */
    try { /* 执行当前语句并推进处理流程。 */
      await entry.harness.close() /* 等待异步操作完成。 */
    } catch { /* 结束当前表达式或代码块。 */
      // The SDK owns its complete EOF/SIGTERM/SIGKILL reap ladder.
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */

  const evictOne = async () => { /* 声明 evictOne。 */
    const candidate = [...conversations.entries()] /* 声明 candidate。 */
      .filter(([key]) => !conversationLocks.has(key)) /* 执行当前语句并推进处理流程。 */
      .sort((left, right) => left[1].lastUsed - right[1].lastUsed)[0] /* 执行当前语句并推进处理流程。 */
    if (candidate === undefined) return false /* 判断条件并选择处理分支。 */
    await closeEntry(candidate[0], candidate[1]) /* 等待异步操作完成。 */
    return true /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */

  const acquireHarness = async (run, mcpToken, cacheKey) => { /* 声明 acquireHarness。 */
    const key = runtimeKey(run) /* 声明 key。 */
    let entry = conversations.get(cacheKey) /* 声明 entry。 */
    if (entry !== undefined && entry.key !== key) { /* 判断条件并选择处理分支。 */
      await closeEntry(cacheKey, entry) /* 等待异步操作完成。 */
      entry = undefined /* 更新 entry 的值。 */
    } /* 结束当前表达式或代码块。 */
    if (entry === undefined) { /* 判断条件并选择处理分支。 */
      while (conversations.size >= maxCachedConversations) { /* 循环处理当前数据。 */
        if (!await evictOne()) throw new HttpError(429, 'CONVERSATION_CAPACITY_EXCEEDED', 'conversation cache is full') /* 判断条件并选择处理分支。 */
      } /* 结束当前表达式或代码块。 */
      if (proxyMcpUrl === undefined) throw new Error('MCP loopback proxy is not listening') /* 判断条件并选择处理分支。 */
      const runtimeAccessKey = randomBytes(32).toString('base64url') /* 声明 runtimeAccessKey。 */
      const routeDigest = hash(runtimeAccessKey) /* 声明 routeDigest。 */
      const route = { runtimeAccessKey, mcpUrl: run.mcpUrl, mcpToken, lastUsed: Date.now() } /* 声明 route。 */
      runtimeRoutes.set(routeDigest, route) /* 执行当前语句并推进处理流程。 */
      const sessionId = `iot-${hash(run.conversationId).slice(0, 16)}-${randomBytes(12).toString('hex')}` /* 声明 sessionId。 */
      let harness /* 声明 harness。 */
      try { /* 执行当前语句并推进处理流程。 */
        harness = await harnessFactory({ /* 更新 harness 的值。 */
          ...run, /* 执行当前语句并推进处理流程。 */
          provider: modelProvider, /* 执行当前语句并推进处理流程。 */
          baseUrl: configuredBaseURL, /* 执行当前语句并推进处理流程。 */
          apiKey: configuredAPIKey, /* 执行当前语句并推进处理流程。 */
          runtimeBin, /* 执行当前语句并推进处理流程。 */
          sdkClientModule, /* 执行当前语句并推进处理流程。 */
          patchFile, /* 执行当前语句并推进处理流程。 */
          runtimeCwd, /* 执行当前语句并推进处理流程。 */
          workspace, /* 执行当前语句并推进处理流程。 */
          sessionRoot, /* 执行当前语句并推进处理流程。 */
          harnessHome: join(harnessHome, sessionId), /* 执行当前语句并推进处理流程。 */
          proxyMcpUrl, /* 执行当前语句并推进处理流程。 */
          runtimeAccessKey, /* 执行当前语句并推进处理流程。 */
          requestTimeoutMs, /* 执行当前语句并推进处理流程。 */
          sessionId, /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
      } catch (error) { /* 结束当前表达式或代码块。 */
        runtimeRoutes.delete(routeDigest) /* 执行当前语句并推进处理流程。 */
        throw error /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      entry = { harness, key, lastUsed: Date.now(), route, routeDigest, sessionId } /* 更新 entry 的值。 */
      conversations.set(cacheKey, entry) /* 执行当前语句并推进处理流程。 */
    } else { /* 结束当前表达式或代码块。 */
      // The short-lived bearer changes independently of the resident Harness
      // process. Only the loopback route sees it; child env and JSONL never do.
      entry.route.mcpUrl = run.mcpUrl /* 更新 entry.route.mcpUrl 的值。 */
      entry.route.mcpToken = mcpToken /* 更新 entry.route.mcpToken 的值。 */
      entry.route.lastUsed = Date.now() /* 更新 entry.route.lastUsed 的值。 */
    } /* 结束当前表达式或代码块。 */
    entry.lastUsed = Date.now() /* 更新 entry.lastUsed 的值。 */
    return entry /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */

  const sweep = async () => { /* 声明 sweep。 */
    if (sweeping || closing) return /* 判断条件并选择处理分支。 */
    sweeping = true /* 更新 sweeping 的值。 */
    try { /* 执行当前语句并推进处理流程。 */
      const cutoff = Date.now() - conversationTtlMs /* 声明 cutoff。 */
      const stale = [...conversations.entries()] /* 声明 stale。 */
        .filter(([key, entry]) => !conversationLocks.has(key) && entry.lastUsed < cutoff) /* 执行当前语句并推进处理流程。 */
      await Promise.all(stale.map(([key, entry]) => closeEntry(key, entry))) /* 等待异步操作完成。 */
    } finally { /* 结束当前表达式或代码块。 */
      sweeping = false /* 更新 sweeping 的值。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  const sweepTimer = setInterval(() => { void sweep() }, Math.min(60000, conversationTtlMs)) /* 声明 sweepTimer。 */
  sweepTimer.unref() /* 执行当前语句并推进处理流程。 */

  const requireGatewayToken = (request) => { /* 声明 requireGatewayToken。 */
    const supplied = request.headers['x-iot-harness-token'] /* 声明 supplied。 */
    if (typeof supplied !== 'string' || !constantTimeEqual(supplied, gatewayToken)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(401, 'HARNESS_TOKEN_INVALID', 'X-IOT-Harness-Token is invalid') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */

  const handleHealth = async (response) => { /* 声明 handleHealth。 */
    try { /* 执行当前语句并推进处理流程。 */
      const [plugins, revision] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
        loadPluginCatalog(pluginDir), /* 执行当前语句并推进处理流程。 */
        readFile(join(here, 'REVISION'), 'utf8'), /* 执行当前语句并推进处理流程。 */
        access(patchFile), /* 执行当前语句并推进处理流程。 */
        access(runtimeBin), /* 执行当前语句并推进处理流程。 */
        access(sdkClientModule), /* 执行当前语句并推进处理流程。 */
      ]) /* 结束当前表达式或代码块。 */
      json(response, 200, { /* 执行当前语句并推进处理流程。 */
        status: closing ? 'stopping' : 'ok', /* 执行当前语句并推进处理流程。 */
        revision: revision.trim(), /* 执行当前语句并推进处理流程。 */
        pluginCount: plugins.filter(plugin => plugin.enabled).length, /* 执行当前语句并推进处理流程。 */
        activeRuns: activeRuns.size, /* 执行当前语句并推进处理流程。 */
        cachedConversations: conversations.size, /* 执行当前语句并推进处理流程。 */
        mcpProxy: proxyServer.listening ? 'ready' : 'not-ready', /* 执行当前语句并推进处理流程。 */
        modelProvider, /* 执行当前语句并推进处理流程。 */
        model: configuredModel, /* 执行当前语句并推进处理流程。 */
        deepseekConfigured: modelProvider === 'deepseek-official' && Boolean(configuredAPIKey), /* 执行当前语句并推进处理流程。 */
      }) /* 结束当前表达式或代码块。 */
    } catch { /* 结束当前表达式或代码块。 */
      json(response, 503, { status: 'not-ready' }) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */

  const handleProviderConfig = async (request, response) => { /* 声明 handleProviderConfig。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    if (request.method === 'GET') { /* 判断条件并选择处理分支。 */
      json(response, 200, publicProviderConfig(modelProvider, configuredBaseURL, configuredModel, configuredAPIKey)) /* 执行当前语句并推进处理流程。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    if (request.method !== 'PUT') throw new HttpError(405, 'METHOD_NOT_ALLOWED', 'method is not allowed') /* 判断条件并选择处理分支。 */
    if (activeRuns.size > 0) throw new HttpError(409, 'RUNS_ACTIVE', 'wait for active workflow runs to finish before changing the provider') /* 判断条件并选择处理分支。 */
    const candidate = validatedProviderConfig(await readJson(request, maximumBodyBytes)) /* 声明 candidate。 */
    modelProvider = candidate.provider /* 更新 modelProvider 的值。 */
    configuredBaseURL = candidate.baseUrl /* 更新 configuredBaseURL 的值。 */
    configuredModel = candidate.model /* 更新 configuredModel 的值。 */
    configuredAPIKey = candidate.apiKey /* 更新 configuredAPIKey 的值。 */
    await Promise.all([...conversations.entries()].map(([key, entry]) => closeEntry(key, entry))) /* 等待异步操作完成。 */
    json(response, 200, publicProviderConfig(modelProvider, configuredBaseURL, configuredModel, configuredAPIKey)) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const handlePlugins = async (request, response) => { /* 声明 handlePlugins。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    const plugins = await loadPluginCatalog(pluginDir) /* 声明 plugins。 */
    json(response, 200, { items: plugins.filter(plugin => plugin.enabled).map(publicPlugin) }) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const handleAdminPlugins = async (request, response) => { /* 声明 handleAdminPlugins。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    const plugins = await loadPluginCatalog(pluginDir) /* 声明 plugins。 */
    json(response, 200, { items: plugins.map(adminPlugin), count: plugins.length }) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const handleSavePlugin = async (request, response) => { /* 声明 handleSavePlugin。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    const raw = await readJson(request, maximumBodyBytes) /* 声明 raw。 */
    let candidate /* 声明 candidate。 */
    try { /* 执行当前语句并推进处理流程。 */
      candidate = validatedManifest(raw, 'submitted-agent.json') /* 更新 candidate 的值。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      throw new HttpError(422, 'AGENT_MANIFEST_INVALID', error instanceof Error ? error.message : 'agent manifest is invalid') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (BUILTIN_PLUGIN_IDS.has(candidate.id)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(409, 'BUILTIN_PLUGIN_IMMUTABLE', 'built-in workflow plugins cannot be overwritten') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    await mkdir(pluginDir, { recursive: true }) /* 等待异步操作完成。 */
    const target = join(pluginDir, `${candidate.id}.json`) /* 声明 target。 */
    let updating = true /* 声明 updating。 */
    try { await access(target) } catch { updating = false } /* 执行当前语句并推进处理流程。 */
    const current = await loadPluginCatalog(pluginDir) /* 声明 current。 */
    if (!updating && current.some(plugin => plugin.id === candidate.id)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(409, 'WORKFLOW_ALREADY_EXISTS', 'a workflow with this id already exists') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    const temporary = join(pluginDir, `.${candidate.id}.${randomBytes(8).toString('hex')}.tmp`) /* 声明 temporary。 */
    await writeFile(temporary, `${JSON.stringify(raw, null, 2)}\n`, { encoding: 'utf8', mode: 0o600, flag: 'wx' }) /* 等待异步操作完成。 */
    await rename(temporary, target) /* 等待异步操作完成。 */
    json(response, updating ? 200 : 201, publicPlugin(candidate)) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const handleDeletePlugin = async (request, response, workflowID) => { /* 声明 handleDeletePlugin。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    if (!ID_PATTERN.test(workflowID)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (BUILTIN_PLUGIN_IDS.has(workflowID)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(409, 'BUILTIN_PLUGIN_IMMUTABLE', 'built-in workflow plugins cannot be deleted') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    const target = join(pluginDir, `${workflowID}.json`) /* 声明 target。 */
    try { /* 执行当前语句并推进处理流程。 */
      await unlink(target) /* 等待异步操作完成。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      if (error?.code === 'ENOENT') { /* 判断条件并选择处理分支。 */
        throw new HttpError(404, 'WORKFLOW_NOT_FOUND', 'workflow plugin was not found') /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
      throw error /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    json(response, 200, { deleted: true, id: workflowID }) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const handleChat = async (request, response) => { /* 声明 handleChat。 */
    requireGatewayToken(request) /* 执行当前语句并推进处理流程。 */
    const mcpToken = bearerToken(request.headers.authorization) /* 声明 mcpToken。 */
    if (mcpToken === undefined) throw new HttpError(401, 'MCP_TOKEN_INVALID', 'Authorization Bearer token is required') /* 判断条件并选择处理分支。 */
    const plugins = await loadPluginCatalog(pluginDir) /* 声明 plugins。 */
    const run = validatedBody( /* 声明 run。 */
      await readJson(request, maximumBodyBytes), /* 等待异步操作完成。 */
      plugins, /* 执行当前语句并推进处理流程。 */
      allowedOrigins, /* 执行当前语句并推进处理流程。 */
      configuredModel, /* 执行当前语句并推进处理流程。 */
    ) /* 结束当前表达式或代码块。 */
    const cacheKey = run.conversationId /* 声明 cacheKey。 */
    if (reservedRunIds.has(run.runId)) throw new HttpError(409, 'RUN_ALREADY_ACTIVE', 'runId is already active') /* 判断条件并选择处理分支。 */
    reservedRunIds.add(run.runId) /* 执行当前语句并推进处理流程。 */
    let releaseConversation /* 声明 releaseConversation。 */
    try { /* 执行当前语句并推进处理流程。 */
      releaseConversation = await acquireConversationLock(cacheKey) /* 更新 releaseConversation 的值。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      reservedRunIds.delete(run.runId) /* 执行当前语句并推进处理流程。 */
      throw error /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (closing) { /* 判断条件并选择处理分支。 */
      reservedRunIds.delete(run.runId) /* 执行当前语句并推进处理流程。 */
      releaseConversation() /* 执行当前语句并推进处理流程。 */
      throw new HttpError(503, 'GATEWAY_STOPPING', 'gateway is stopping') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (activeRuns.size >= maxConcurrency) { /* 判断条件并选择处理分支。 */
      reservedRunIds.delete(run.runId) /* 执行当前语句并推进处理流程。 */
      releaseConversation() /* 执行当前语句并推进处理流程。 */
      throw new HttpError(429, 'CAPACITY_EXCEEDED', 'gateway concurrency limit reached') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */

    activeRuns.add(run.runId) /* 执行当前语句并推进处理流程。 */
    activeConversations.add(cacheKey) /* 执行当前语句并推进处理流程。 */
    response.writeHead(200, { /* 执行当前语句并推进处理流程。 */
      'cache-control': 'no-store', /* 执行当前语句并推进处理流程。 */
      'content-type': 'application/x-ndjson; charset=utf-8', /* 执行当前语句并推进处理流程。 */
      'x-accel-buffering': 'no', /* 执行当前语句并推进处理流程。 */
      'x-content-type-options': 'nosniff', /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    const emit = eventWriter(response, run) /* 声明 emit。 */
    emit('run.started', { /* 执行当前语句并推进处理流程。 */
      conversationId: run.conversationId, /* 执行当前语句并推进处理流程。 */
      workflowId: run.workflowId, /* 执行当前语句并推进处理流程。 */
      model: run.model, /* 执行当前语句并推进处理流程。 */
      maxTokens: run.maxTokens, /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    let entry /* 声明 entry。 */
    let clientGone = false /* 声明 clientGone。 */
    let responseFinished = false /* 声明 responseFinished。 */
    const calls = new Map() /* 声明 calls。 */
    let emittedText = false /* 声明 emittedText。 */
    response.on('close', () => { /* 执行当前语句并推进处理流程。 */
      if (responseFinished) return /* 判断条件并选择处理分支。 */
      clientGone = true /* 更新 clientGone 的值。 */
      if (entry !== undefined) void closeEntry(cacheKey, entry) /* 判断条件并选择处理分支。 */
    }) /* 结束当前表达式或代码块。 */

    try { /* 执行当前语句并推进处理流程。 */
      entry = await acquireHarness(run, mcpToken, cacheKey) /* 更新 entry 的值。 */
      const result = await withTimeout(entry.harness.run(run.question, { /* 声明 result。 */
        sessionId: entry.sessionId, /* 执行当前语句并推进处理流程。 */
        onNotification(notification) { /* 执行当前语句并推进处理流程。 */
          if (notification?.method === 'iot.text.delta') { /* 判断条件并选择处理分支。 */
            const params = notification.params /* 声明 params。 */
            if (params?.sessionId === entry.sessionId && typeof params.text === 'string' && params.text !== '') { /* 判断条件并选择处理分支。 */
              emittedText = true /* 更新 emittedText 的值。 */
              emit('text.delta', { delta: params.text }) /* 执行当前语句并推进处理流程。 */
            } /* 结束当前表达式或代码块。 */
            return /* 返回当前处理结果。 */
          } /* 结束当前表达式或代码块。 */
          const event = sessionEvent(notification, entry.sessionId) /* 声明 event。 */
          if (event === undefined) return /* 判断条件并选择处理分支。 */
          if (event.type === 'tool/call') { /* 判断条件并选择处理分支。 */
            const callId = event.data?.callId /* 声明 callId。 */
            const tool = event.data?.name /* 声明 tool。 */
            if (typeof callId === 'string' && typeof tool === 'string') { /* 判断条件并选择处理分支。 */
              calls.set(callId, tool) /* 执行当前语句并推进处理流程。 */
              emit('tool.started', { callId, tool }) /* 执行当前语句并推进处理流程。 */
            } /* 结束当前表达式或代码块。 */
            return /* 返回当前处理结果。 */
          } /* 结束当前表达式或代码块。 */
          if (event.type === 'tool/result') { /* 判断条件并选择处理分支。 */
            const callId = event.data?.callId ?? event.data?.message?.source?.callId /* 声明 callId。 */
            if (typeof callId === 'string') { /* 判断条件并选择处理分支。 */
              const tool = calls.get(callId) /* 声明 tool。 */
              const clientAction = clientActionForTool(tool, event.data) /* 声明 clientAction。 */
              emit('tool.completed', { /* 执行当前语句并推进处理流程。 */
                callId, /* 执行当前语句并推进处理流程。 */
                ...(tool === undefined ? {} : { tool }), /* 执行当前语句并推进处理流程。 */
                success: !toolResultFailed(event.data), /* 执行当前语句并推进处理流程。 */
                ...(clientAction === undefined ? {} : { data: { clientAction } }), /* 执行当前语句并推进处理流程。 */
              }) /* 结束当前表达式或代码块。 */
            } /* 结束当前表达式或代码块。 */
          } /* 结束当前表达式或代码块。 */
        }, /* 结束当前表达式或代码块。 */
      }), runTimeoutMs, () => closeEntry(cacheKey, entry)) /* 结束当前表达式或代码块。 */
      entry.lastUsed = Date.now() /* 更新 entry.lastUsed 的值。 */
      if (!emittedText && result.finalResponse !== '') emit('text.delta', { delta: result.finalResponse }) /* 判断条件并选择处理分支。 */
      const failure = turnFailure(result) /* 声明 failure。 */
      if (failure === undefined) { /* 判断条件并选择处理分支。 */
        emit('run.completed', { /* 执行当前语句并推进处理流程。 */
          conversationId: run.conversationId, /* 执行当前语句并推进处理流程。 */
          workflowId: run.workflowId, /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
      } else { /* 结束当前表达式或代码块。 */
        emit('run.failed', { code: failure, message: 'Harness run did not complete successfully' }) /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      if (entry !== undefined) await closeEntry(cacheKey, entry) /* 判断条件并选择处理分支。 */
      if (!clientGone) { /* 判断条件并选择处理分支。 */
        emit('run.failed', { code: safeRuntimeError(error), message: 'Harness runtime request failed' }) /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
    } finally { /* 结束当前表达式或代码块。 */
      activeRuns.delete(run.runId) /* 执行当前语句并推进处理流程。 */
      reservedRunIds.delete(run.runId) /* 执行当前语句并推进处理流程。 */
      activeConversations.delete(cacheKey) /* 执行当前语句并推进处理流程。 */
      releaseConversation() /* 执行当前语句并推进处理流程。 */
      responseFinished = true /* 更新 responseFinished 的值。 */
      if (!response.writableEnded && !response.destroyed) response.end() /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */

  const route = async (request, response) => { /* 声明 route。 */
    const url = new URL(request.url ?? '/', 'http://gateway.invalid') /* 声明 url。 */
    if (url.search !== '') throw new HttpError(400, 'QUERY_NOT_ALLOWED', 'query parameters are not supported') /* 判断条件并选择处理分支。 */
    if (request.method === 'GET' && url.pathname === '/health') return handleHealth(response) /* 判断条件并选择处理分支。 */
    if (['GET', 'PUT', 'POST', 'DELETE'].includes(request.method) && url.pathname === '/v1/provider') return handleProviderConfig(request, response) /* 判断条件并选择处理分支。 */
    if (request.method === 'GET' && url.pathname === '/v1/plugins/admin') return handleAdminPlugins(request, response) /* 判断条件并选择处理分支。 */
    if (request.method === 'GET' && url.pathname === '/v1/plugins') return handlePlugins(request, response) /* 判断条件并选择处理分支。 */
    if (request.method === 'POST' && url.pathname === '/v1/plugins') return handleSavePlugin(request, response) /* 判断条件并选择处理分支。 */
    if (request.method === 'DELETE' && url.pathname.startsWith('/v1/plugins/')) { /* 判断条件并选择处理分支。 */
      const encodedID = url.pathname.slice('/v1/plugins/'.length) /* 声明 encodedID。 */
      if (encodedID === '' || encodedID.includes('/')) throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format') /* 判断条件并选择处理分支。 */
      let workflowID /* 声明 workflowID。 */
      try { workflowID = decodeURIComponent(encodedID) } catch { throw new HttpError(422, 'WORKFLOW_ID_INVALID', 'workflow id has an invalid format') } /* 执行当前语句并推进处理流程。 */
      return handleDeletePlugin(request, response, workflowID) /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    if (request.method === 'POST' && url.pathname === '/v1/chat/stream') return handleChat(request, response) /* 判断条件并选择处理分支。 */
    if (['/health', '/v1/plugins', '/v1/plugins/admin', '/v1/chat/stream'].includes(url.pathname)) { /* 判断条件并选择处理分支。 */
      throw new HttpError(405, 'METHOD_NOT_ALLOWED', 'method is not allowed') /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    throw new HttpError(404, 'NOT_FOUND', 'route not found') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */

  const server = createServer((request, response) => { /* 声明 server。 */
    void route(request, response).catch((error) => { /* 执行当前语句并推进处理流程。 */
      if (!response.headersSent) problem(response, error) /* 判断条件并选择处理分支。 */
      else if (!response.writableEnded && !response.destroyed) response.end() /* 判断条件并选择处理分支。 */
    }) /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  server.headersTimeout = 10000 /* 更新 server.headersTimeout 的值。 */
  server.requestTimeout = 30000 /* 更新 server.requestTimeout 的值。 */
  server.keepAliveTimeout = 5000 /* 更新 server.keepAliveTimeout 的值。 */

  return { /* 返回当前处理结果。 */
    server, /* 执行当前语句并推进处理流程。 */
    async listen(port = 8091, host = '0.0.0.0') { /* 执行当前语句并推进处理流程。 */
      if (!proxyServer.listening) { /* 判断条件并选择处理分支。 */
        await new Promise((resolveListen, reject) => { /* 等待异步操作完成。 */
          const onError = error => { /* 声明 onError。 */
            proxyServer.off('listening', onListening) /* 执行当前语句并推进处理流程。 */
            reject(error) /* 执行当前语句并推进处理流程。 */
          } /* 结束当前表达式或代码块。 */
          const onListening = () => { /* 声明 onListening。 */
            proxyServer.off('error', onError) /* 执行当前语句并推进处理流程。 */
            resolveListen() /* 执行当前语句并推进处理流程。 */
          } /* 结束当前表达式或代码块。 */
          proxyServer.once('error', onError) /* 执行当前语句并推进处理流程。 */
          proxyServer.once('listening', onListening) /* 执行当前语句并推进处理流程。 */
          proxyServer.listen(proxyPort, '127.0.0.1') /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
        const address = proxyServer.address() /* 声明 address。 */
        if (address === null || typeof address === 'string') throw new Error('MCP loopback proxy has no TCP address') /* 判断条件并选择处理分支。 */
        proxyMcpUrl = `http://127.0.0.1:${address.port}/mcp` /* 更新 proxyMcpUrl 的值。 */
      } /* 结束当前表达式或代码块。 */
      await new Promise((resolveListen, reject) => { /* 等待异步操作完成。 */
        const onError = error => { /* 声明 onError。 */
          server.off('listening', onListening) /* 执行当前语句并推进处理流程。 */
          reject(error) /* 执行当前语句并推进处理流程。 */
        } /* 结束当前表达式或代码块。 */
        const onListening = () => { /* 声明 onListening。 */
          server.off('error', onError) /* 执行当前语句并推进处理流程。 */
          resolveListen() /* 执行当前语句并推进处理流程。 */
        } /* 结束当前表达式或代码块。 */
        server.once('error', onError) /* 执行当前语句并推进处理流程。 */
        server.once('listening', onListening) /* 执行当前语句并推进处理流程。 */
        server.listen(port, host) /* 执行当前语句并推进处理流程。 */
      }) /* 结束当前表达式或代码块。 */
      return server.address() /* 返回当前处理结果。 */
    }, /* 结束当前表达式或代码块。 */
    async close() { /* 执行当前语句并推进处理流程。 */
      if (closing) return /* 判断条件并选择处理分支。 */
      closing = true /* 更新 closing 的值。 */
      clearInterval(sweepTimer) /* 执行当前语句并推进处理流程。 */
      await Promise.all([...conversations.entries()].map(([key, entry]) => closeEntry(key, entry))) /* 等待异步操作完成。 */
      if (server.listening) { /* 判断条件并选择处理分支。 */
        await new Promise(resolveClose => server.close(() => resolveClose())) /* 等待异步操作完成。 */
      } /* 结束当前表达式或代码块。 */
      if (proxyServer.listening) { /* 判断条件并选择处理分支。 */
        await new Promise(resolveClose => proxyServer.close(() => resolveClose())) /* 等待异步操作完成。 */
      } /* 结束当前表达式或代码块。 */
    }, /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function main() { /* 定义 main 函数。 */
  const gateway = createGateway() /* 声明 gateway。 */
  const host = process.env.IOT_HARNESS_HOST ?? '0.0.0.0' /* 声明 host。 */
  const port = integerOption(process.env.IOT_HARNESS_PORT, 8091, 'IOT_HARNESS_PORT', 1, 65535) /* 声明 port。 */
  await gateway.listen(port, host) /* 等待异步操作完成。 */
  process.stderr.write(`iot-harness-gateway listening on ${host}:${port}\n`) /* 执行当前语句并推进处理流程。 */
  let stopping = false /* 声明 stopping。 */
  const stop = async () => { /* 声明 stop。 */
    if (stopping) return /* 判断条件并选择处理分支。 */
    stopping = true /* 更新 stopping 的值。 */
    await gateway.close() /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  process.once('SIGTERM', () => { void stop().then(() => process.exit(0)) }) /* 执行当前语句并推进处理流程。 */
  process.once('SIGINT', () => { void stop().then(() => process.exit(130)) }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

if (process.argv[1] !== undefined && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) { /* 判断条件并选择处理分支。 */
  await main() /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
