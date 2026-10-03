import test from 'node:test'
import assert from 'node:assert/strict'
import { channelForm, channelPayload, policyForm, policyPayload, stageSummary } from '../src/notifications.js'

test('editing a channel never echoes credentials and keeps them unless re-entered', () => {
  const stored = { id:'ding', name:'值班群', type:'dingtalk', enabled:true, version:3, secretSet:true, config:{ urlHint:'https://oapi.dingtalk.com' } }
  const form = channelForm(stored)
  assert.deepEqual(form.secret, { url:'', signSecret:'', password:'' })
  const payload = channelPayload(form)
  assert.equal(payload.secret, undefined)
  assert.deepEqual(payload.config, {})
  form.secret.signSecret = ' NEW '
  assert.deepEqual(channelPayload(form).secret, { signSecret:'NEW' })
})

test('new channels always send credentials and SMTP settings are normalized', () => {
  const form = channelForm()
  form.type = 'smtp'
  form.name = ' 邮件 '
  Object.assign(form.config, { host:' smtp.example.com ', port:'587', security:'starttls', from:'alarm@example.com' })
  const payload = channelPayload(form)
  assert.equal(payload.name, '邮件')
  assert.deepEqual(payload.config, { host:'smtp.example.com', port:587, security:'starttls', username:'', from:'alarm@example.com' })
  assert.deepEqual(payload.secret, {})
})

test('policy payload forces an immediate first stage and splits recipient lists', () => {
  const form = policyForm()
  form.name = '火警'
  form.stages[0].delaySeconds = 30
  form.stages[0].channelIds = ['ding']
  form.stages[1].emails = 'a@x.cn， b@x.cn'
  form.stages[1].stationIds = ['s1']
  const payload = policyPayload(form)
  assert.equal(payload.stages[0].delaySeconds, 0)
  assert.deepEqual(payload.stages[1].emails, ['a@x.cn', 'b@x.cn'])
  assert.deepEqual(payload.stages[1].stationIds, [], 'stations only apply to on-duty notification')
  assert.equal(stageSummary({ delaySeconds:180, channelIds:['ding'], onDuty:true }, 1, id => id === 'ding' ? '值班群' : id), '3 分钟未确认 → 值班群（当班人员）')
})
