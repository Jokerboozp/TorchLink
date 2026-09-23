import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */

import { consumeSSE } from '../src/sse.js' /* 引入当前代码需要的依赖。 */

function byteStream(text, chunkSize = 1) { /* 定义 byteStream 函数。 */
  const bytes = new TextEncoder().encode(text) /* 声明 bytes。 */
  return new ReadableStream({ /* 返回当前处理结果。 */
    start(controller) { /* 执行当前语句并推进处理流程。 */
      for (let offset = 0; offset < bytes.length; offset += chunkSize) controller.enqueue(bytes.slice(offset, offset + chunkSize)) /* 循环处理当前数据。 */
      controller.close() /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

test('SSE parser supports event fields, JSON event types, CRLF and split UTF-8 bytes', async () => { /* 执行当前语句并推进处理流程。 */
  const source = [ /* 声明 source。 */
    'event: run.started\r\nid: evt-1\r\ndata: {"runId":"run-1","conversationId":"conversation-1"}\r\n\r\n', /* 执行当前语句并推进处理流程。 */
    'data: {"type":"text.delta","delta":"你好"}\n\n', /* 执行当前语句并推进处理流程。 */
    'event: tool.started\ndata: {"toolCallId":"tool-1","toolName":"alarm.query","inputSummary":"高等级告警"}\n\n', /* 执行当前语句并推进处理流程。 */
    'event: tool.completed\ndata: {"toolCallId":"tool-1","success":true,"outputSummary":"2 条"}\n\n', /* 执行当前语句并推进处理流程。 */
    'event: run.completed\ndata: {"durationMs":\n', /* 执行当前语句并推进处理流程。 */
    'data: 42}\n\n' /* 执行当前语句并推进处理流程。 */
  ].join('') /* 结束当前表达式或代码块。 */
  const events = [] /* 声明 events。 */
  await consumeSSE(byteStream(source), event => events.push(event)) /* 等待异步操作完成。 */

  assert.deepEqual(events.map(event => event.type), ['run.started','text.delta','tool.started','tool.completed','run.completed']) /* 验证实际结果符合预期。 */
  assert.equal(events[0].eventId, 'evt-1') /* 验证实际结果符合预期。 */
  assert.equal(events[1].delta, '你好') /* 验证实际结果符合预期。 */
  assert.equal(events[4].durationMs, 42) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('SSE parser ignores a legacy DONE marker in favor of explicit terminal events', async () => { /* 执行当前语句并推进处理流程。 */
  const events = [] /* 声明 events。 */
  await consumeSSE(byteStream('data: [DONE]\n\n', 3), event => events.push(event)) /* 等待异步操作完成。 */
  assert.deepEqual(events, []) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('SSE parser rejects non-JSON event payloads without exposing their contents', async () => { /* 执行当前语句并推进处理流程。 */
  await assert.rejects( /* 验证实际结果符合预期。 */
    consumeSSE(byteStream('event: run.failed\ndata: definitely-not-json\n\n', 5)), /* 执行当前语句并推进处理流程。 */
    error => error.code === 'AI_STREAM_INVALID_EVENT' && !error.message.includes('definitely-not-json') /* 更新 error 的值。 */
  ) /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
