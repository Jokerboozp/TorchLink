import assert from 'node:assert/strict'
import test from 'node:test'
import { restoreSummary } from '../src/backupPresentation.js'
import { backupComponents } from '../src/labels.js'

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
  const result=restoreSummary({status:'COMPLETED',kinds:{raw:{restored:1}},components:{application:{status:'not_included'},governance:{status:'not_included'}}})
  assert.equal(result.tone,'success')
  assert.ok(result.lines.some(line=>line.includes('未包含')))
  assert.ok(!result.lines.some(line=>line.includes('已核对')))
  assert.ok(result.lines.some(line=>line.includes('未包含反复报警治理')))
})

test('combined FULL v5 shows application and governance coverage without double-counting retired executions', () => {
  // RestoreResult has no formatVersion. In v5 the shared analysis records and
  // pending executions belong to application; governance owns its domain data.
  const result = restoreSummary({status:'PARTIAL',kinds:{raw:{restored:7},parsed:{restored:7}},components:{
    application:{status:'restored',matches:true,retiredExecutions:2,partial:true,limitations:['冷存储来源仅恢复到制品暂存']},
    applicationObjects:{status:'restored',matches:true,objects:3},
    governance:{status:'restored',matches:true,analysisDocuments:'application',retiredExecutions:0},
    governanceObjects:{status:'restored',matches:true,objects:4},
  }})
  assert.equal(result.status,'PARTIAL')
  assert.equal(result.tone,'warning')
  assert.ok(result.lines.some(line=>line.includes('治理记录及版本关联已核对')))
  assert.ok(result.lines.some(line=>line.includes('4 份治理附件')))
  assert.ok(result.lines.some(line=>line.includes('3 份应用附件')))
  assert.equal(result.lines.filter(line=>line.includes('任务已退役')).length,1)
  assert.ok(result.lines.some(line=>line.includes('2 项待执行')))
  assert.deepEqual(result.limitations,['冷存储来源仅恢复到制品暂存'])
  assert.equal(backupComponents.governance,'反复报警治理')
})

test('legacy governance-only v4 retains its own fixed facts and retirement coverage', () => {
  const result = restoreSummary({status:'COMPLETED',kinds:{raw:{restored:1}},components:{
    application:{status:'not_included'},
    governance:{status:'restored',matches:true,analysisDocuments:'governance',retiredExecutions:1},
    governanceObjects:{status:'restored',objects:0,matches:true},
  }})
  assert.equal(result.tone,'success')
  assert.ok(result.lines.includes('治理固定分析事实已恢复并核对'))
  assert.ok(!result.lines.includes('本备份未包含固定分析与业务版本'))
  assert.ok(result.lines.some(line=>line.includes('1 项治理待执行')))
  assert.ok(result.lines.some(line=>line.includes('0 份治理附件')))
})
