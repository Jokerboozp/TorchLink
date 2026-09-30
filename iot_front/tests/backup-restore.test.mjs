import assert from 'node:assert/strict'
import test from 'node:test'
import { restoreSummary } from '../src/backupPresentation.js'

test('matching artifacts with cold source staging stays partial in the recovery result', () => {
  const result = restoreSummary({status:'PARTIAL', kinds:{raw:{restored:40,matches:true},parsed:{restored:40,matches:true}}, components:{application:{status:'restored',matches:true,partial:true,retiredExecutions:2,limitations:['冷存储仅恢复到制品暂存，未重建可查询来源']},applicationObjects:{status:'restored',objects:3,originalHashUnknown:1}}})
  assert.equal(result.tone,'warning')
  assert.ok(result.lines.some(line=>line.includes('80 条')))
  assert.ok(result.lines.some(line=>line.includes('2 项')&&line.includes('退役')))
  assert.ok(result.lines.some(line=>line.includes('旧附件')))
  assert.deepEqual(result.limitations,['冷存储仅恢复到制品暂存，未重建可查询来源'])
  assert.ok(!result.title.includes('不一致'))
})

test('a completed legacy device backup does not acquire application recovery coverage', () => {
  const result=restoreSummary({status:'COMPLETED',kinds:{raw:{restored:1}},components:{application:{status:'not_included'}}})
  assert.equal(result.tone,'success')
  assert.ok(result.lines.some(line=>line.includes('未包含')))
  assert.ok(!result.lines.some(line=>line.includes('已核对')))
})
