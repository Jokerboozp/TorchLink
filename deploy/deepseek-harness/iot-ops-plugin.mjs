/**
 * Deployment-owned tool policy for the IoT operations assistant.
 *
 * `tools.restrict()` keeps disallowed tools out of each agent's prompt. It is
 * deliberately paired with the global monotonic guard below because Harness
 * documents scoped restrictions as visibility composition, not an authority
 * boundary.
 */

export const name = 'iot-ops-policy'
export const inject = ['tools']

export const READ_ONLY_TOOL_CEILING = Object.freeze([
  'mcp__iot__query_system_overview',
  'mcp__iot__query_device_latest',
  'mcp__iot__query_alarm_list',
  'mcp__iot__query_property_history',
  'mcp__iot__query_similar_alarms',
  'mcp__iot__query_knowledge_base',
  // Draft-only tool: returns a disabled rule proposal and cannot persist,
  // enable, or execute an action without a separate human-approved API call.
  'mcp__iot__create_rule_draft',
])

const ceiling = new Set(READ_ONLY_TOOL_CEILING)

// v0.1.5 stores complete assistant messages in the session log. Live chunks
// are process-local, so bridge text through a private JSON-RPC notification
// without appending synthetic events to the durable log. SDK stdout is the
// protocol channel; reasoning and tool payloads never enter this notification.
export function forwardAssistantText({ agent, frame }, write = line => process.stdout.write(line)) {
  if (frame?.type !== 'chunk' || frame.chunk?.type !== 'text-delta') return
  const text = frame.chunk.text
  if (typeof text !== 'string' || text === '') return
  write(`${JSON.stringify({
    jsonrpc: '2.0', method: 'iot.text.delta',
    params: { sessionId: String(agent.session.id), text },
  })}\n`)
}

function resolveAllowedTools(config) {
  if (config === null || typeof config !== 'object' || Array.isArray(config)) {
    throw new TypeError('iot-ops-policy config must be an object')
  }
  const unknownKeys = Object.keys(config).filter(key => key !== 'allowedTools')
  if (unknownKeys.length > 0) {
    throw new TypeError(`iot-ops-policy config has unknown key(s): ${unknownKeys.join(', ')}`)
  }
  if (!Array.isArray(config.allowedTools) || config.allowedTools.length === 0) {
    throw new TypeError('iot-ops-policy allowedTools must be a non-empty array')
  }
  const allowed = []
  const seen = new Set()
  for (const tool of config.allowedTools) {
    if (typeof tool !== 'string' || !ceiling.has(tool)) {
      throw new TypeError(`iot-ops-policy refuses non-read-only tool: ${String(tool)}`)
    }
    if (seen.has(tool)) throw new TypeError(`iot-ops-policy duplicate tool: ${tool}`)
    seen.add(tool)
    allowed.push(tool)
  }
  return Object.freeze(allowed)
}

export function apply(ctx, config) {
  const allowedTools = resolveAllowedTools(config)
  const allowed = new Set(allowedTools)

  // Final execution gate. Other policy listeners cannot turn this denial into
  // an allow because ToolRuntime guards are monotonic.
  ctx.tools.guard(execution => allowed.has(execution.name) ? undefined : 'tool not allowed')

  // Prompt/catalog visibility. This also fails agent creation if MCP discovery
  // did not register every configured name, so a partial catalog fails closed.
  ctx.on('agent/created', ({ agent }) => {
    agent.ctx.tools.restrict({ allow: allowedTools })
  })

  ctx.on('agent/assistant-stream', payload => forwardAssistantText(payload))
}
