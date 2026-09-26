function normalizeBuffer(value) {
  return value.replace(/\r\n/g, '\n')
}

function eventFromBlock(block) {
  let eventName = ''
  let eventID = ''
  const data = []
  for (const line of block.split('\n')) {
    if (!line || line.startsWith(':')) continue
    const separator = line.indexOf(':')
    const field = separator === -1 ? line : line.slice(0, separator)
    let value = separator === -1 ? '' : line.slice(separator + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') eventName = value
    if (field === 'id') eventID = value
    if (field === 'data') data.push(value)
  }
  if (!data.length) return null
  const raw = data.join('\n')
  // The workflow contract has explicit run.completed/run.failed events. A legacy
  // sentinel must not turn a failed run into a successful one.
  if (raw === '[DONE]') return null
  let payload
  try {
    payload = JSON.parse(raw)
  } catch {
    const error = new Error('AI stream returned invalid JSON')
    error.code = 'AI_STREAM_INVALID_EVENT'
    throw error
  }
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) payload = { data:payload }
  return { ...payload, type:payload.type || eventName || 'message', eventId:payload.eventId || eventID || undefined }
}

export async function consumeSSE(stream, onEvent = () => {}) {
  if (!stream?.getReader) {
    const error = new Error('AI stream is unavailable')
    error.code = 'AI_STREAM_UNAVAILABLE'
    throw error
  }
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      buffer = normalizeBuffer(buffer + decoder.decode(value || new Uint8Array(), { stream:!done }))
      let boundary = buffer.indexOf('\n\n')
      while (boundary !== -1) {
        const block = buffer.slice(0, boundary)
        buffer = buffer.slice(boundary + 2)
        const event = eventFromBlock(block)
        if (event) await onEvent(event)
        boundary = buffer.indexOf('\n\n')
      }
      if (done) break
    }
    const finalEvent = eventFromBlock(buffer.trim())
    if (finalEvent) await onEvent(finalEvent)
  } finally {
    reader.releaseLock()
  }
}
