import {createClientId} from '../clientId.js'
export const dutyStatus = {
  DRAFT:'编制中', PUBLISHED:'已发布', CANCELLED:'已取消', ACTIVE:'当班', ENDED:'已结束',
  OPEN:'待处理', IN_PROGRESS:'处理中', PENDING_VERIFICATION:'待核实', DONE:'已完成',
  SUBMITTED:'待接班', RETURNED:'已退回', ACCEPTED:'已接收', VOID:'已作废',
  QUEUED:'排队中', RUNNING:'执行中', SUCCEEDED:'已完成', FAILED:'失败', STOPPED:'已停止',
  COLLECTING:'整理事实', RETRIEVING:'检索依据', GENERATING:'模型整理', VALIDATING:'校验结果', SAVING:'保存结果',
  WAITING:'已到岗，待接班', ONLINE:'在线', OFFLINE:'离线', ALARM:'告警中', UNKNOWN:'未知',
  CONNECTED:'已连接', DISCONNECTED:'未连接', NEVER_SEEN:'从未上线', SUSPECTED_OFFLINE:'疑似离线',
  ACKED:'已确认', RECOVERED:'已恢复', CLOSED:'已关闭', SUPPRESSED:'已抑制',
  ALARM_CREATED:'新增告警', ALARM_REPORTED:'告警上报', ALARM_ACKNOWLEDGED:'告警确认', ALARM_RECOVERED:'告警恢复', ALARM_CLOSED:'告警关闭', DEVICE_OFFLINE:'设备离线', DEVICE_ONLINE:'设备上线',
  CREATED:'新增事项', UPDATE:'修改事项', TRANSFER:'转交事项', HANDOVER:'跨班接续', COMPLETE:'完成事项', CANCEL:'取消事项', STATUS:'修改状态'
}
export const statusText = value => dutyStatus[String(value || '').toUpperCase()] || ({'duty.run.arrive':'人员到岗','duty.run.open':'首次开班','duty.run.substitute':'安排代班','duty.run.end':'实际值班结束','duty.record.create':'追加处置记录','duty.record.correct':'更正处置记录','duty.handover.created':'生成交接','duty.handover.update':'交接新版本','duty.handover.submit':'提交交班','duty.handover.accept':'确认接班','duty.handover.return':'退回交接','duty.handover.amend':'交接补充','duty.handover.void':'作废交接','duty.item.created':'新增事项','duty.item.handover':'跨班接续','duty.item.update':'更新事项','duty.item.transfer':'转交事项','duty.item.complete':'完成事项','duty.item.cancel':'取消事项','duty.item.status':'事项状态变化'})[value] || value || '—'
export const isAIRunning = job => ['QUEUED','RUNNING'].includes(String(job?.status || '').toUpperCase())
export function dutyTime(value) {
  if (!value) return '—'
  const date = new Date(typeof value === 'number' || /^\d+$/.test(String(value)) ? Number(value) : value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', {hour12:false})
}
export function localDate(value = Date.now()) {
  const date = new Date(value)
  return `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`
}
export function monthDays(month) {
  if (!/^\d{4}-\d{2}$/.test(month || '')) return []
  const [year, number] = month.split('-').map(Number)
  if (number < 1 || number > 12) return []
  const first = new Date(year,number-1,1), count = new Date(year,number,0).getDate()
  return [ ...Array.from({length:(first.getDay()+6)%7},() => null), ...Array.from({length:count},(_,index) => `${month}-${String(index+1).padStart(2,'0')}`) ]
}
export function rosterCalendar(rosters, month) {
  const byDay = new Map()
  for (const row of rosters || []) {
    const key = localDate(row.startAt)
    if (!byDay.has(key)) byDay.set(key,[])
    byDay.get(key).push(row)
  }
  return monthDays(month).map(date => ({date,items:date ? (byDay.get(date) || []).sort((a,b) => Number(a.startAt)-Number(b.startAt)) : []}))
}
export function rosterPayload(form) {
  const result = {stationId:form.stationId || '', templateId:form.templateId || '', startAt:Number(form.startAt), endAt:Number(form.endAt), memberIds:[...new Set(form.memberIds || [])], leaderId:form.leaderId || '', reason:form.reason?.trim() || ''}
  if (form.version != null) result.version = form.version
  if (!result.stationId || !result.leaderId || !result.memberIds.includes(result.leaderId)) throw Error('请选择岗位，并从当班人员中指定交接负责人')
  if (!Number.isFinite(result.startAt) || !Number.isFinite(result.endAt) || result.endAt<=result.startAt) throw Error('结束时间必须晚于开始时间')
  return result
}
export function parseRosterCSV(text) {
  // Quoted values support commas, escaped quotes and CRLF. Errors include the line.
  const rows=[], fields=[]; let field='', quoted=false
  for(let index=0;index<text.length;index++) {
    const character=text[index]
    if(character==='"') {
      if(quoted && text[index+1]==='"'){field+='"';index++}
      else if(quoted || !field) quoted=!quoted
      else throw Error('CSV 引号位置不正确')
    } else if(character===',' && !quoted){fields.push(field);field=''}
    else if((character==='\n' || character==='\r') && !quoted){if(character==='\r' && text[index+1]==='\n')index++;fields.push(field);if(fields.some(Boolean))rows.push(fields.splice(0));else fields.length=0;field=''}
    else field+=character
  }
  if(quoted)throw Error('CSV 存在未闭合的引号')
  fields.push(field);if(fields.some(Boolean))rows.push(fields)
  if(rows.length<2)throw Error('CSV 至少需要表头和一条排班')
  const headers=rows.shift().map(value=>value.replace(/^\uFEFF/,'').trim())
  const required=['stationId','startAt','endAt','memberIds','leaderId']
  if(required.some(name=>!headers.includes(name)))throw Error('CSV 表头需包含 stationId,startAt,endAt,memberIds,leaderId')
  if(new Set(headers).size!==headers.length)throw Error('CSV 表头不能重复')
  return rows.map((values,index)=>{
    if(values.length!==headers.length)throw Error(`CSV 第 ${index+2} 行列数不一致`)
    const row=Object.fromEntries(headers.map((name,column)=>[name,values[column].trim()]))
    const stamp=value=>/^\d+$/.test(value)?Number(value):Date.parse(value)
    try{return {...rosterPayload({...row,startAt:stamp(row.startAt),endAt:stamp(row.endAt),memberIds:row.memberIds.split(/[;；]/).map(value=>value.trim()).filter(Boolean)}),sourceId:row.sourceId || `csv-${index+2}`}}
    catch(error){throw Error(`CSV 第 ${index+2} 行：${error.message}`)}
  })
}
export function itemPayload(form) {
  if (!form.title?.trim()) throw Error('请填写事项标题')
  if (!form.ownerId) throw Error('请选择内部主责人')
  const value={...form,title:form.title.trim(),nextAction:form.nextAction?.trim() || '',result:form.result?.trim() || '',reason:form.reason?.trim() || ''}
  if(value.status==='DONE' && !value.result)throw Error('完成事项必须填写处理结果')
  if(value.status==='CANCELLED' && !value.reason)throw Error('取消事项必须填写原因')
  return value
}
export function handoverActionPayload(detail, revision, delta, extra = {}) {
  if (!detail?.id || !revision) throw Error('请先加载交接版本')
  return {version:detail.version,revisionId:revision.id,revision:revision.revision || revision.number, snapshotHash:delta?.snapshotHash || revision.snapshotHash || revision.snapshot?.hash || '',...extra}
}
export function dutyActionKey(storage,identity,id,operation,version,provider=globalThis.crypto) {
  const key='iot:duty-action:'+JSON.stringify([identity.tenant,identity.user,identity.accessVersion || '',id,operation,version])
  try {const previous=storage?.getItem(key);if(/^[0-9a-f]{8}-[0-9a-f-]{27}$/i.test(previous || ''))return previous}catch{/* Storage is optional; retries in the open form still reuse the key. */}
  const value=createClientId(provider)
  try{storage?.setItem(key,value)}catch{/* A disabled storage must not block signoff. */}
  return value
}
export function clearDutyActionKey(storage,identity,id,operation,version){try{storage?.removeItem('iot:duty-action:'+JSON.stringify([identity.tenant,identity.user,identity.accessVersion || '',id,operation,version]))}catch{/* Optional storage. */}}
export function dutyNoticeTarget(notice) {
  const handover=['HANDOVER_PENDING','HANDOVER_RETURNED','HANDOVER_ACCEPTED'].includes(notice.type)
  return {tab:handover?'handovers':String(notice.type).startsWith('ITEM_')?'items':'current',handoverId:handover?notice.resourceId:''}
}
