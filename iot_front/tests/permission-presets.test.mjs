import test from 'node:test'
import assert from 'node:assert/strict'
import { featureLevel, applyFeatureLevel, roleDeviceScope } from '../src/permissionPresets.js'
const group = {id:'menu:devices',menu:'devices',actions:[{id:'PUT /devices/:id'},{id:'DELETE /devices/:id'}]}
test('feature presets preserve other features and distinguish custom permissions', () => {
 const current = ['menu:devices','PUT /devices/:id','menu:alarms']
 assert.equal(featureLevel(group,current),'custom')
 assert.deepEqual(applyFeatureLevel(group,current,'view'),['menu:alarms','menu:devices'])
 assert.deepEqual(applyFeatureLevel(group,current,'none'),['menu:alarms'])
 const managed = applyFeatureLevel(group,current,'manage')
 assert.equal(featureLevel(group,managed),'manage')
 assert.deepEqual(current,['menu:devices','PUT /devices/:id','menu:alarms'])
})
test('assistant question preset grants both chat paths without granting workflow management', () => {
 const ai = {id:'menu:ai',menu:'ai',actions:['POST /api/v1/ai/chat','POST /api/v1/ai/chat/stream','POST /api/v1/ai/workflows'].map(id=>({id}))}
 assert.deepEqual(applyFeatureLevel(ai,[],'view'),['menu:ai','POST /api/v1/ai/chat','POST /api/v1/ai/chat/stream'])
 assert.equal(featureLevel(ai,['menu:ai','POST /api/v1/ai/chat']),'custom')
})
test('inherited device summary unions only assigned roles and handles unconfigured roles', () => {
 const roles=[{id:'east',deviceScope:'selected',deviceIds:['a','b']},{id:'west',deviceScope:'selected',deviceIds:['b','c']},{id:'all',deviceScope:'all'},{id:'legacy'}]
 assert.deepEqual(roleDeviceScope(['east','west'],roles),{deviceScope:'selected',deviceIds:['a','b','c']})
 assert.deepEqual(roleDeviceScope(['east','all'],roles),{deviceScope:'all',deviceIds:[]})
 assert.deepEqual(roleDeviceScope(['legacy','missing'],roles),{deviceScope:'none',deviceIds:[]})
})
