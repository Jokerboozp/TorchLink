import { api, download } from '../api.js'
export const maintenanceKinds={assets:'ASSET_INSTANCE','maintenance-records':'MAINTENANCE_RECORD','maintenance-contexts':'OPERATING_CONTEXT','maintenance-fault-cycles':'MAINTENANCE_FAULT_ASSESSMENT','maintenance-costs':'MAINTENANCE_COST','investment-scenarios':'INVESTMENT_SCENARIO','maintenance-admissions':'MAINTENANCE_ADMISSION'}
export const maintenancePath=(resource,id='',operation='')=>`/api/v1/${resource}${id?'/'+encodeURIComponent(id):''}${operation?'/'+operation:''}`
export function maintenanceRead(resource,id='',operation='',query={}){const p=new URLSearchParams(Object.entries(query).filter(([,v])=>v!=null&&v!==''));return api(maintenancePath(resource,id,operation)+(p.size?'?'+p:''))}
export const maintenanceWrite=(resource,id='',operation='',body={})=>api(maintenancePath(resource,id,operation),{method:'POST',headers:{'Idempotency-Key':body.idempotencyKey||''},body:JSON.stringify(body)})
export const maintenanceRevision=(id,resource)=>maintenanceRead('maintenance-revisions',id,'',{kind:maintenanceKinds[resource]})
export async function maintenanceAll(resource,id='',operation='',query={}){const items=[];for(let offset=0;;offset+=100){const result=await maintenanceRead(resource,id,operation,{...query,limit:100,offset});items.push(...(result.items||[]));if(!result.items?.length||(result.total!=null?items.length>=result.total:result.items.length<100))return{...result,items}}}
export const maintenanceExport=(resource,id)=>download(maintenancePath(resource,id,'export'),`维护与投入_${id}.json`)
export const maintenanceAttachment=(id,name)=>download(maintenancePath('maintenance-attachments',id),name||'维护资料')
export async function maintenanceUpload(file,deviceIds,key){const body=new FormData();body.append('file',file);body.append('deviceIdsJSON',JSON.stringify(deviceIds));body.append('idempotencyKey',key);return api(maintenancePath('maintenance-attachments'),{method:'POST',headers:{'Idempotency-Key':key},body})}
