import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */
import { transportLabel, formatLabel, statusLabel, errorMessage, platformLabel, toolName } from '../src/presentation.js' /* 引入当前代码需要的依赖。 */
import { pageGuide } from '../src/pageGuide.js' /* 引入当前代码需要的依赖。 */

test('display names handle canonical and lowercase wire values without mutating data', () => { /* 执行当前语句并推进处理流程。 */
  const data = { transport:'MQTT', format:'json', status:'INDEXED' } /* 声明 data。 */
  assert.equal(transportLabel(data.transport), 'MQTT') /* 验证实际结果符合预期。 */
  assert.equal(transportLabel('iot-standard'), '标准设备接入') /* 验证实际结果符合预期。 */
  assert.equal(transportLabel('tcp'), 'TCP') /* 验证实际结果符合预期。 */
  assert.equal(transportLabel('MQTT_HTTP'), 'MQTT / HTTP') /* 验证实际结果符合预期。 */
  assert.equal(formatLabel(data.format), 'JSON') /* 验证实际结果符合预期。 */
  assert.equal(statusLabel(data.status), '已建立索引') /* 验证实际结果符合预期。 */
  assert.equal(statusLabel('indexed'), '已建立索引') /* 验证实际结果符合预期。 */
  assert.equal(statusLabel('new-backend-status'), '未知状态') /* 验证实际结果符合预期。 */
  assert.equal(statusLabel(null), '未知状态') /* 验证实际结果符合预期。 */
  assert.equal(statusLabel('待人工复核'), '待人工复核') /* 验证实际结果符合预期。 */
  assert.deepEqual(data, { transport:'MQTT', format:'json', status:'INDEXED' }) /* 验证实际结果符合预期。 */
  assert.equal(platformLabel('windows-arm64'), 'Windows · arm64') /* 验证实际结果符合预期。 */
  assert.equal(toolName('mcp__iot__query_alarm_list'), '查询告警') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('errors give actionable Chinese feedback for network, permission and format failures', () => { /* 执行当前语句并推进处理流程。 */
  assert.equal(errorMessage(new TypeError('Failed to fetch')), '无法连接服务，请检查网络后重试') /* 验证实际结果符合预期。 */
  assert.equal(errorMessage({ status:403, message:'forbidden' }), '当前账户没有操作权限') /* 验证实际结果符合预期。 */
  assert.equal(errorMessage({ status:401, message:'invalid credentials' }), '身份验证失败，请检查账户信息或重新登录') /* 验证实际结果符合预期。 */
  assert.match(errorMessage(new Error('relation would create a cycle')), /循环/) /* 验证实际结果符合预期。 */
  assert.match(errorMessage(new Error('local connection credential reference was not found')), /未找到现场连接凭据/) /* 验证实际结果符合预期。 */
  assert.equal(errorMessage(new SyntaxError('Unexpected token')), '数据格式不正确，请检查括号、引号和字段值') /* 验证实际结果符合预期。 */
  assert.equal(errorMessage({ message:'设备名称不能为空' }), '设备名称不能为空') /* 验证实际结果符合预期。 */
  assert.equal(errorMessage({ status:502 }), '服务暂时不可用，请稍后重试') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('each existing navigation target has Chinese task guidance', () => { /* 执行当前语句并推进处理流程。 */
  assert.equal(Object.keys(pageGuide).length, 15) /* 验证实际结果符合预期。 */
  for (const guide of Object.values(pageGuide)) { /* 循环处理当前数据。 */
    assert.equal(guide.steps.length, 3) /* 验证实际结果符合预期。 */
    assert.match(guide.title, /[\u4e00-\u9fff]/) /* 验证实际结果符合预期。 */ // Keep technical protocol names such as TCP and Modbus.
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
