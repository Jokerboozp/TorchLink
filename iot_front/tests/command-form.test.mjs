import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { commandBody } from '../src/commandForm.js' /* 引入当前代码需要的依赖。 */

test('command forms preserve zero and false, omit empty optional fields and exclude undeclared values', () => { /* 执行当前语句并推进处理流程。 */
  const op = {identifier:'set',fields:[{identifier:'value',dataType:'integer',required:true},{identifier:'enabled',dataType:'boolean',required:true},{identifier:'note',dataType:'string'}]} /* 声明 op。 */
  assert.deepEqual(commandBody(op,{value:0,enabled:false,note:'',extra:'ignored'}),{type:'set',data:{value:0,enabled:false}}) /* 验证实际结果符合预期。 */
  assert.throws(()=>commandBody(op,{enabled:false}),/value/) /* 验证实际结果符合预期。 */
  assert.throws(()=>commandBody(op,{value:1.5,enabled:false}),/类型/) /* 验证实际结果符合预期。 */
  assert.throws(()=>commandBody(op,{value:1,enabled:'false'}),/类型/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('command forms accept structured parameters and require a defined command', () => { /* 执行当前语句并推进处理流程。 */
  const op = {identifier:'configure',fields:[{identifier:'settings',dataType:'object',required:true},{identifier:'items',dataType:'array'}]} /* 声明 op。 */
  assert.deepEqual(commandBody(op,{settings:{threshold:0,enabled:false},items:[1,'x']}),{type:'configure',data:{settings:{threshold:0,enabled:false},items:[1,'x']}}) /* 验证实际结果符合预期。 */
  assert.throws(()=>commandBody(op,{settings:[]}),/类型/) /* 验证实际结果符合预期。 */
  assert.throws(()=>commandBody(null,{}),/请选择/) /* 验证实际结果符合预期。 */
  assert.deepEqual(commandBody({identifier:'ping'},{}),{type:'ping',data:{}}) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
