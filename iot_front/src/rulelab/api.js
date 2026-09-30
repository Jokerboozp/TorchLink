import { api, download } from '../api.js'
import { createClientId } from '../clientId.js'
export const ruleLabPath = (resource, id = '', operation = '') => `/api/v1/rule-lab/${resource}${id ? '/' + encodeURIComponent(id) : ''}${operation ? '/' + operation : ''}`
export function ruleLabRead(resource, id = '', operation = '', query = {}) {
 const params = new URLSearchParams(Object.entries(query).filter(([,value])=>value!=null&&value!==''))
 return api(ruleLabPath(resource,id,operation)+(params.size?'?'+params:''))
}
export const ruleLabWrite = (resource,id='',operation='',body={}) => api(ruleLabPath(resource,id,operation),{method:'POST',body:JSON.stringify(body),headers:{'Idempotency-Key':body.idempotencyKey || createClientId()}})
export async function ruleLabAll(resource,id='',operation='',query={}) {
 const items=[]
 for(let offset=0;;offset+=100){const result=await ruleLabRead(resource,id,operation,{...query,limit:100,offset});items.push(...(result.items||[]));if(!result.items?.length||(result.total!=null?items.length>=result.total:result.items.length<100))return{...result,items}}
}
export const ruleLabReport = (experimentId,runId) => download(ruleLabPath('experiments',experimentId,'report')+'?'+new URLSearchParams({runId}),`规则实验_${runId}.json`)
export const ruleLabAIRead=(experimentId,runId)=>ruleLabRead('experiments',experimentId,'ai-jobs',{runId})
export const ruleLabAIWrite=(experimentId,runId,body)=>api(ruleLabPath('experiments',experimentId,'ai-jobs')+'?'+new URLSearchParams({runId}),{method:'POST',body:JSON.stringify(body)})
export const publishRuleLab=(ruleId,body,confirmConflicts=false)=>api(`/api/v1/rules/${encodeURIComponent(ruleId)}/publish${confirmConflicts?'?confirmConflicts=true':''}`,{method:'POST',body:JSON.stringify(body)})
