import test from 'node:test'
import assert from 'node:assert/strict'
import { alarmNavigation, alarmQuery } from '../src/alarmNavigation.js'

test('设备详情跳转保留设备范围，分页和状态筛选不会扩大查询范围', () => {
  const navigation = alarmNavigation('{"deviceId":"device-a"}')
  const query = alarmQuery({ ...navigation, status: 'CLOSED', level: 'LOW' }, 2, 20)
  assert.equal(query.get('deviceId'), 'device-a')
  assert.equal(query.get('status'), 'CLOSED')
  assert.equal(query.get('page'), '2')
  assert.equal(alarmQuery({}, 1, 20).has('deviceId'), false)
})

test('告警详情跳转与无效导航数据兼容', () => {
  assert.deepEqual(alarmNavigation('{"alarmId":"alarm-a"}'), { deviceId: '', alarmId: 'alarm-a' })
  for (const raw of [null, '{broken', 'null', '{"deviceId":{}}']) {
    assert.deepEqual(alarmNavigation(raw), { deviceId: '', alarmId: '' })
  }
})
