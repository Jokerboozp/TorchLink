import { api, download } from '../api.js'
export const responsePath=(resource,id='',operation='')=>`/api/v1/${resource}${id?'/'+encodeURIComponent(id):''}${operation?'/'+operation:''}`
export function responseRead(resource,id='',operation='',query={}){const p=new URLSearchParams(Object.entries({...(resource==='response-revisions'?{kind:'RESPONSE_EXECUTION'}:{}),...query}).filter(([,v])=>v!=null&&v!==''));return api(responsePath(resource,id,operation)+(p.size?'?'+p:''))}
export const responseWrite=(resource,id='',operation='',body={},method='POST')=>api(responsePath(resource,id,operation),{method,body:JSON.stringify(body),headers:{'Idempotency-Key':body.idempotencyKey||''}})
export async function responseAll(resource,id='',operation='',query={}){const items=[];for(let offset=0;;offset+=100){const value=await responseRead(resource,id,operation,{...query,limit:100,offset});items.push(...(value.items||[]));if(!value.items?.length||(value.total!=null?items.length>=value.total:value.items.length<100))return{...value,items}}}
export const responseExport=id=>download(responsePath('response-evaluations',id,'export'),`处置复盘_${id}.json`)
export const responseAttachment=(id,name)=>download(responsePath('response-attachments',id),name||'复盘附件')
export async function responseUpload(executionId,file,key){const body=new FormData();body.append('file',file);body.append('idempotencyKey',key);return api(responsePath('response-runs',executionId,'attachments'),{method:'POST',headers:{'Idempotency-Key':key},body})}
