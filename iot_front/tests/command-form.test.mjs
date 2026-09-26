import test from 'node:test'
import assert from 'node:assert/strict'
import { commandBody } from '../src/commandForm.js'

test('command forms preserve zero and false, omit empty optional fields and exclude undeclared values', () => {
  const op = {identifier:'set',fields:[{identifier:'value',dataType:'integer',required:true},{identifier:'enabled',dataType:'boolean',required:true},{identifier:'note',dataType:'string'}]}
  assert.deepEqual(commandBody(op,{value:0,enabled:false,note:'',extra:'ignored'}),{type:'set',data:{value:0,enabled:false}})
  assert.throws(()=>commandBody(op,{enabled:false}),/value/)
  assert.throws(()=>commandBody(op,{value:1.5,enabled:false}),/类型/)
  assert.throws(()=>commandBody(op,{value:1,enabled:'false'}),/类型/)
})

test('command forms accept structured parameters and require a defined command', () => {
  const op = {identifier:'configure',fields:[{identifier:'settings',dataType:'object',required:true},{identifier:'items',dataType:'array'}]}
  assert.deepEqual(commandBody(op,{settings:{threshold:0,enabled:false},items:[1,'x']}),{type:'configure',data:{settings:{threshold:0,enabled:false},items:[1,'x']}})
  assert.throws(()=>commandBody(op,{settings:[]}),/类型/)
  assert.throws(()=>commandBody(null,{}),/请选择/)
  assert.deepEqual(commandBody({identifier:'ping'},{}),{type:'ping',data:{}})
})
