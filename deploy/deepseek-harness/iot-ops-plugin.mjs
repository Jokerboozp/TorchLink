/**
 * Deployment-owned tool policy for the IoT operations assistant.
 *
 * `tools.restrict()` keeps disallowed tools out of each agent's prompt. It is
 * deliberately paired with the global monotonic guard below because Harness
 * documents scoped restrictions as visibility composition, not an authority
 * boundary.
 */

export const name = 'iot-ops-policy' /* 执行当前语句并推进处理流程。 */
export const inject = ['tools'] /* 执行当前语句并推进处理流程。 */

export const READ_ONLY_TOOL_CEILING = Object.freeze([ /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_system_overview', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_device_latest', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_alarm_list', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_property_history', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_similar_alarms', /* 执行当前语句并推进处理流程。 */
  'mcp__iot__query_knowledge_base', /* 执行当前语句并推进处理流程。 */
  // Draft-only tool: returns a disabled rule proposal and cannot persist,
  // enable, or execute an action without a separate human-approved API call.
  'mcp__iot__create_rule_draft', /* 执行当前语句并推进处理流程。 */
]) /* 结束当前表达式或代码块。 */

const ceiling = new Set(READ_ONLY_TOOL_CEILING) /* 声明 ceiling。 */

// v0.1.5 stores complete assistant messages in the session log. Live chunks
// are process-local, so bridge text through a private JSON-RPC notification
// without appending synthetic events to the durable log. SDK stdout is the
// protocol channel; reasoning and tool payloads never enter this notification.
export function forwardAssistantText({ agent, frame }, write = line => process.stdout.write(line)) { /* 执行当前语句并推进处理流程。 */
  if (frame?.type !== 'chunk' || frame.chunk?.type !== 'text-delta') return /* 判断条件并选择处理分支。 */
  const text = frame.chunk.text /* 声明 text。 */
  if (typeof text !== 'string' || text === '') return /* 判断条件并选择处理分支。 */
  write(`${JSON.stringify({
    jsonrpc: '2.0', method: 'iot.text.delta',
    params: { sessionId: String(agent.session.id), text },
  })}\n`)
} /* 结束当前表达式或代码块。 */

function resolveAllowedTools(config) { /* 定义 resolveAllowedTools 函数。 */
  if (config === null || typeof config !== 'object' || Array.isArray(config)) { /* 判断条件并选择处理分支。 */
    throw new TypeError('iot-ops-policy config must be an object') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const unknownKeys = Object.keys(config).filter(key => key !== 'allowedTools') /* 声明 unknownKeys。 */
  if (unknownKeys.length > 0) { /* 判断条件并选择处理分支。 */
    throw new TypeError(`iot-ops-policy config has unknown key(s): ${unknownKeys.join(', ')}`) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!Array.isArray(config.allowedTools) || config.allowedTools.length === 0) { /* 判断条件并选择处理分支。 */
    throw new TypeError('iot-ops-policy allowedTools must be a non-empty array') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const allowed = [] /* 声明 allowed。 */
  const seen = new Set() /* 声明 seen。 */
  for (const tool of config.allowedTools) { /* 循环处理当前数据。 */
    if (typeof tool !== 'string' || !ceiling.has(tool)) { /* 判断条件并选择处理分支。 */
      throw new TypeError(`iot-ops-policy refuses non-read-only tool: ${String(tool)}`) /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (seen.has(tool)) throw new TypeError(`iot-ops-policy duplicate tool: ${tool}`) /* 判断条件并选择处理分支。 */
    seen.add(tool) /* 执行当前语句并推进处理流程。 */
    allowed.push(tool) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return Object.freeze(allowed) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function apply(ctx, config) { /* 执行当前语句并推进处理流程。 */
  const allowedTools = resolveAllowedTools(config) /* 声明 allowedTools。 */
  const allowed = new Set(allowedTools) /* 声明 allowed。 */

  // Final execution gate. Other policy listeners cannot turn this denial into
  // an allow because ToolRuntime guards are monotonic.
  ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'tool not allowed') /* 执行当前语句并推进处理流程。 */

  // Prompt/catalog visibility. This also fails agent creation if MCP discovery
  // did not register every configured name, so a partial catalog fails closed.
  ctx.on('agent/created', ({ agent }) => { /* 执行当前语句并推进处理流程。 */
    agent.ctx.tools.restrict({ allow: allowedTools }) /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */

  ctx.on('agent/assistant-stream', payload => forwardAssistantText(payload)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
