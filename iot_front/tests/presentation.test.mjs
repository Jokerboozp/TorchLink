import assert from 'node:assert/strict'
import test from 'node:test'
import { transportLabel, formatLabel, statusLabel, errorMessage, platformLabel, toolName } from '../src/presentation.js'
import { pageGuide } from '../src/pageGuide.js'

test('display names handle canonical and lowercase wire values without mutating data', () => {
  const data = { transport:'MQTT', format:'json', status:'INDEXED' }
  assert.equal(transportLabel(data.transport), 'MQTT')
  assert.equal(transportLabel('iot-standard'), '标准设备接入')
  assert.equal(transportLabel('tcp'), 'TCP')
  assert.equal(formatLabel(data.format), 'JSON')
  assert.equal(statusLabel(data.status), '已建立索引')
  assert.equal(statusLabel('indexed'), '已建立索引')
  assert.equal(statusLabel('new-backend-status'), '未知状态')
  assert.equal(statusLabel(null), '未知状态')
  assert.equal(statusLabel('待人工复核'), '待人工复核')
  assert.deepEqual(data, { transport:'MQTT', format:'json', status:'INDEXED' })
  assert.equal(platformLabel('windows-arm64'), 'Windows · arm64')
  assert.equal(toolName('mcp__iot__query_alarm_list'), '查询告警')
})

test('errors give actionable Chinese feedback for network, permission and format failures', () => {
  assert.equal(errorMessage(new TypeError('Failed to fetch')), '无法连接服务，请检查网络后重试')
  assert.equal(errorMessage({ status:403, message:'forbidden' }), '当前账户没有操作权限')
  assert.equal(errorMessage({ status:401, message:'invalid credentials' }), '身份验证失败，请检查账户信息或重新登录')
  assert.match(errorMessage(new Error('relation would create a cycle')), /循环/)
  assert.match(errorMessage(new Error('local connection credential reference was not found')), /未找到现场连接凭据/)
  assert.equal(errorMessage(new SyntaxError('Unexpected token')), '数据格式不正确，请检查括号、引号和字段值')
  assert.equal(errorMessage({ message:'设备名称不能为空' }), '设备名称不能为空')
  assert.equal(errorMessage({ status:502 }), '服务暂时不可用，请稍后重试')
})

test('each existing navigation target has Chinese task guidance', () => {
  assert.equal(Object.keys(pageGuide).length, 15)
  for (const guide of Object.values(pageGuide)) {
    assert.equal(guide.steps.length, 3)
    assert.doesNotMatch([guide.title, guide.sub, ...guide.steps].join(''), /[a-z]/i)
  }
})
