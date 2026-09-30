import {api,download} from '../api.js'
import {createClientId} from '../clientId.js'
export function dutyDoc(value) {
  if(!value || typeof value!=='object')return value
  if(Array.isArray(value))return value.map(dutyDoc)
  if(value.body && value.id && value.version!=null && typeof value.kind==='string')return {...value.body,id:value.id,version:value.version,createdAt:value.createdAt,updatedAt:value.updatedAt}
  return Object.fromEntries(Object.entries(value).map(([key,item])=>[key,Array.isArray(item)?item.map(dutyDoc):item]))
}
export const dutyPath=(kind,id='',operation='')=>`/api/v1/duty/${kind}${id?'/'+encodeURIComponent(id):''}${operation?'/'+operation:''}`
export const dutyRead=(kind,id='',operation='',query={})=>{
  const params=new URLSearchParams(Object.entries(query).filter(([,value])=>value!=='' && value!=null))
  return api(dutyPath(kind,id,operation)+(params.size?'?'+params:'')).then(dutyDoc)
}
export async function dutyAll(kind,query={}) {
  const items=[]
  for(let offset=0;;offset+=100){const value=await dutyRead(kind,'','',{...query,limit:100,offset});items.push(...(value.items || []));if(!(value.items?.length) || items.length>=Number(value.total ?? items.length))return {...value,items}}
}
export const dutyWrite=(kind,id,operation,value={},method='POST')=>{
  const {id:resourceId,version,expectedVersion,idempotencyKey,...body}=value
  return api(dutyPath(kind,id,operation),{method,body:JSON.stringify({expectedVersion:expectedVersion ?? version ?? 0,idempotencyKey:idempotencyKey || createClientId(),body})}).then(dutyDoc)
}
export const dutySave=(kind,value)=>dutyWrite(kind,value.id || '','',value,value.id?'PUT':'POST')
export const dutyPDF=(id,revisionId)=>download(dutyPath('revisions',revisionId || id,'pdf'),'值班交接.pdf')
export const dutyEventsCSV=id=>download(dutyPath('revisions',id,'events.csv'),'值班交接事件.csv')
export const dutyAttachment=(id,name)=>download(dutyPath('attachments',id),name || '值班附件')
export async function uploadDutyAttachments(files,runId) {
  const values=[]
  for(const file of files || []) {
    const body=new FormData();body.append('file',file)
    values.push(await api(dutyPath('runs',runId,'attachments'),{method:'POST',body}))
  }
  return values
}
