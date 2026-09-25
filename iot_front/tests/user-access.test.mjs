import test from 'node:test'
import assert from 'node:assert/strict'
import { userAccessPayload } from '../src/userAccess.js'

test('editing a fetched user excludes server-owned fields rejected by the save endpoint', () => {
  const stored = {
    username:'operator', displayName:'运维员', enabled:true, roleIds:['reader'],
    permissions:['menu:devices'], deviceScope:'selected', deviceIds:['east-smoke'],
    sessionVersion:123, tenantId:'tenant-a', passwordHash:'server-only'
  }
  const form = userAccessPayload(stored)
  form.deviceIds.push('west-smoke')
  form.permissions.push('menu:alarms')
  const payload = JSON.parse(JSON.stringify(userAccessPayload(form)))
  assert.deepEqual(payload, {
    username:'operator', displayName:'运维员', password:'', enabled:true, roleIds:['reader'],
    permissions:['menu:devices','menu:alarms'], deviceScope:'selected', deviceIds:['east-smoke','west-smoke']
  })
  assert.deepEqual(stored.deviceIds, ['east-smoke'])
  assert.deepEqual(stored.permissions, ['menu:devices'])
})

test('unconfigured users default to no devices and preserve disabled accounts', () => {
  const form = userAccessPayload({ username:'operator', enabled:false, deviceIds:null, roleIds:null, permissions:null })
  assert.equal(form.deviceScope, 'none')
  assert.equal(form.enabled, false)
  assert.deepEqual(form.deviceIds, [])
  assert.deepEqual(form.roleIds, [])
  assert.deepEqual(form.permissions, [])
})
