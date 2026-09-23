// UUIDs used for client-generated resource names and idempotency keys.
// getRandomValues is also available on HTTP origins, unlike randomUUID.
export function createClientId(provider = globalThis.crypto) { /* 执行当前语句并推进处理流程。 */
  if (typeof provider?.randomUUID === 'function') return provider.randomUUID() /* 判断条件并选择处理分支。 */
  const bytes = provider.getRandomValues(new Uint8Array(16)) /* 声明 bytes。 */
  bytes[6] = (bytes[6] & 0x0f) | 0x40 /* 更新 bytes[6] 的值。 */
  bytes[8] = (bytes[8] & 0x3f) | 0x80 /* 更新 bytes[8] 的值。 */
  const hex = Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('') /* 声明 hex。 */
  return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
