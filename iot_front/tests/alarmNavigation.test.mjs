import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { alarmNavigation, alarmQuery } from '../src/alarmNavigation.js' /* 引入当前代码需要的依赖。 */

test('设备详情跳转保留设备范围，分页和状态筛选不会扩大查询范围', () => { /* 执行当前语句并推进处理流程。 */
  const navigation = alarmNavigation('{"deviceId":"device-a"}') /* 声明 navigation。 */
  const query = alarmQuery({ ...navigation, status: 'CLOSED', level: 'LOW' }, 2, 20) /* 声明 query。 */
  assert.equal(query.get('deviceId'), 'device-a') /* 验证实际结果符合预期。 */
  assert.equal(query.get('status'), 'CLOSED') /* 验证实际结果符合预期。 */
  assert.equal(query.get('page'), '2') /* 验证实际结果符合预期。 */
  assert.equal(alarmQuery({}, 1, 20).has('deviceId'), false) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('告警详情跳转与无效导航数据兼容', () => { /* 执行当前语句并推进处理流程。 */
  assert.deepEqual(alarmNavigation('{"alarmId":"alarm-a"}'), { deviceId: '', alarmId: 'alarm-a' }) /* 验证实际结果符合预期。 */
  for (const raw of [null, '{broken', 'null', '{"deviceId":{}}']) { /* 循环处理当前数据。 */
    assert.deepEqual(alarmNavigation(raw), { deviceId: '', alarmId: '' }) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
