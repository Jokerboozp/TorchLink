import type { AIWorkflowEvent } from './types/api.ts'

/** A parsed server-sent event: the JSON payload plus the SSE event name and id. */
export type StreamEvent = AIWorkflowEvent & { eventId?: string; [key: string]: unknown }

class StreamError extends Error {
  code: string
  constructor(message: string, code: string) {
    super(message)
    this.code = code
  }
}

function normalizeBuffer(value: string) {
  return value.replace(/\r\n/g, '\n')
}

function eventFromBlock(block: string): StreamEvent | null {
  let eventName = ''
  let eventID = ''
  const data: string[] = []
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
  let payload: any
  try {
    payload = JSON.parse(raw)
  } catch {
    throw new StreamError('AI stream returned invalid JSON', 'AI_STREAM_INVALID_EVENT')
  }
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) payload = { data: payload }
  return { ...payload, type: payload.type || eventName || 'message', eventId: payload.eventId || eventID || undefined }
}

export async function consumeSSE(
  stream: ReadableStream<Uint8Array> | null | undefined,
  onEvent: (event: StreamEvent) => void | Promise<void> = () => {}
) {
  if (!stream?.getReader) throw new StreamError('AI stream is unavailable', 'AI_STREAM_UNAVAILABLE')
  const reader = stream.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      buffer = normalizeBuffer(buffer + decoder.decode(value || new Uint8Array(), { stream: !done }))
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
