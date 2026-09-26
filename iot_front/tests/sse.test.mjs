import assert from 'node:assert/strict'
import test from 'node:test'

import { consumeSSE } from '../src/sse.js'

function byteStream(text, chunkSize = 1) {
  const bytes = new TextEncoder().encode(text)
  return new ReadableStream({
    start(controller) {
      for (let offset = 0; offset < bytes.length; offset += chunkSize) controller.enqueue(bytes.slice(offset, offset + chunkSize))
      controller.close()
    }
  })
}

test('SSE parser supports event fields, JSON event types, CRLF and split UTF-8 bytes', async () => {
  const source = [
    'event: run.started\r\nid: evt-1\r\ndata: {"runId":"run-1","conversationId":"conversation-1"}\r\n\r\n',
    'data: {"type":"text.delta","delta":"你好"}\n\n',
    'event: tool.started\ndata: {"toolCallId":"tool-1","toolName":"alarm.query","inputSummary":"高等级告警"}\n\n',
    'event: tool.completed\ndata: {"toolCallId":"tool-1","success":true,"outputSummary":"2 条"}\n\n',
    'event: run.completed\ndata: {"durationMs":\n',
    'data: 42}\n\n'
  ].join('')
  const events = []
  await consumeSSE(byteStream(source), event => events.push(event))

  assert.deepEqual(events.map(event => event.type), ['run.started','text.delta','tool.started','tool.completed','run.completed'])
  assert.equal(events[0].eventId, 'evt-1')
  assert.equal(events[1].delta, '你好')
  assert.equal(events[4].durationMs, 42)
})

test('SSE parser ignores a legacy DONE marker in favor of explicit terminal events', async () => {
  const events = []
  await consumeSSE(byteStream('data: [DONE]\n\n', 3), event => events.push(event))
  assert.deepEqual(events, [])
})

test('SSE parser rejects non-JSON event payloads without exposing their contents', async () => {
  await assert.rejects(
    consumeSSE(byteStream('event: run.failed\ndata: definitely-not-json\n\n', 5)),
    error => error.code === 'AI_STREAM_INVALID_EVENT' && !error.message.includes('definitely-not-json')
  )
})
