function normalizeBuffer(value) { /* 定义 normalizeBuffer 函数。 */
  return value.replace(/\r\n/g, '\n') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function eventFromBlock(block) { /* 定义 eventFromBlock 函数。 */
  let eventName = '' /* 声明 eventName。 */
  let eventID = '' /* 声明 eventID。 */
  const data = [] /* 声明 data。 */
  for (const line of block.split('\n')) { /* 循环处理当前数据。 */
    if (!line || line.startsWith(':')) continue /* 判断条件并选择处理分支。 */
    const separator = line.indexOf(':') /* 声明 separator。 */
    const field = separator === -1 ? line : line.slice(0, separator) /* 声明 field。 */
    let value = separator === -1 ? '' : line.slice(separator + 1) /* 声明 value。 */
    if (value.startsWith(' ')) value = value.slice(1) /* 判断条件并选择处理分支。 */
    if (field === 'event') eventName = value /* 判断条件并选择处理分支。 */
    if (field === 'id') eventID = value /* 判断条件并选择处理分支。 */
    if (field === 'data') data.push(value) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  if (!data.length) return null /* 判断条件并选择处理分支。 */
  const raw = data.join('\n') /* 声明 raw。 */
  // The workflow contract has explicit run.completed/run.failed events. A legacy
  // sentinel must not turn a failed run into a successful one.
  if (raw === '[DONE]') return null /* 判断条件并选择处理分支。 */
  let payload /* 声明 payload。 */
  try { /* 执行当前语句并推进处理流程。 */
    payload = JSON.parse(raw) /* 更新 payload 的值。 */
  } catch { /* 结束当前表达式或代码块。 */
    const error = new Error('AI stream returned invalid JSON') /* 声明 error。 */
    error.code = 'AI_STREAM_INVALID_EVENT' /* 更新 error.code 的值。 */
    throw error /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) payload = { data:payload } /* 判断条件并选择处理分支。 */
  return { ...payload, type:payload.type || eventName || 'message', eventId:payload.eventId || eventID || undefined } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export async function consumeSSE(stream, onEvent = () => {}) { /* 执行当前语句并推进处理流程。 */
  if (!stream?.getReader) { /* 判断条件并选择处理分支。 */
    const error = new Error('AI stream is unavailable') /* 声明 error。 */
    error.code = 'AI_STREAM_UNAVAILABLE' /* 更新 error.code 的值。 */
    throw error /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const reader = stream.getReader() /* 声明 reader。 */
  const decoder = new TextDecoder() /* 声明 decoder。 */
  let buffer = '' /* 声明 buffer。 */
  try { /* 执行当前语句并推进处理流程。 */
    while (true) { /* 循环处理当前数据。 */
      const { value, done } = await reader.read() /* 执行当前语句并推进处理流程。 */
      buffer = normalizeBuffer(buffer + decoder.decode(value || new Uint8Array(), { stream:!done })) /* 更新 buffer 的值。 */
      let boundary = buffer.indexOf('\n\n') /* 声明 boundary。 */
      while (boundary !== -1) { /* 循环处理当前数据。 */
        const block = buffer.slice(0, boundary) /* 声明 block。 */
        buffer = buffer.slice(boundary + 2) /* 更新 buffer 的值。 */
        const event = eventFromBlock(block) /* 声明 event。 */
        if (event) await onEvent(event) /* 判断条件并选择处理分支。 */
        boundary = buffer.indexOf('\n\n') /* 更新 boundary 的值。 */
      } /* 结束当前表达式或代码块。 */
      if (done) break /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
    const finalEvent = eventFromBlock(buffer.trim()) /* 声明 finalEvent。 */
    if (finalEvent) await onEvent(finalEvent) /* 判断条件并选择处理分支。 */
  } finally { /* 结束当前表达式或代码块。 */
    reader.releaseLock() /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
