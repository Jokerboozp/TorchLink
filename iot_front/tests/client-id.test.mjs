import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { createClientId } from '../src/clientId.js' /* 引入当前代码需要的依赖。 */

test('HTTP fallback preserves UUID v4 version and variant using random bytes', () => { /* 执行当前语句并推进处理流程。 */
  let calls=0 /* 声明 calls。 */
  const provider={getRandomValues(bytes){calls++;return bytes.fill(255)}} /* 声明 provider。 */
  assert.equal(createClientId(provider),'ffffffff-ffff-4fff-bfff-ffffffffffff') /* 验证实际结果符合预期。 */
  assert.equal(calls,1) /* 验证实际结果符合预期。 */
  const ids=new Set(Array.from({length:100},()=>createClientId({getRandomValues:bytes=>crypto.getRandomValues(bytes)}))) /* 声明 ids。 */
  assert.equal(ids.size,100) /* 验证实际结果符合预期。 */
  for(const id of ids) assert.match(id,/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/) /* 循环处理当前数据。 */
}) /* 结束当前表达式或代码块。 */

test('HTTPS keeps the browser native UUID implementation', () => { /* 执行当前语句并推进处理流程。 */
  assert.equal(createClientId({randomUUID(){return 'native-id'},getRandomValues(){throw Error('unexpected fallback')}}),'native-id') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
