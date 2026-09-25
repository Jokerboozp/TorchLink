import { reactive, watchEffect } from 'vue' /* 引入当前代码需要的依赖。 */
import { api, session } from './api' /* 引入当前代码需要的依赖。 */

function saved(){try{const value=JSON.parse(localStorage.getItem('iot_permissions')||'null');return Array.isArray(value)?value:null}catch{return null}} /* 定义 saved 函数。 */
export const permissionState=reactive({items:saved() || (session.role==='admin'?['*']:[]),ready:false,accessVersion:session.accessVersion}) /* 执行当前语句并推进处理流程。 */
export function can(permission){return permissionState.items.includes('*') || (Array.isArray(permission)?permission.some(can):permissionState.items.includes(permission))} /* 执行当前语句并推进处理流程。 */
export async function refreshPermissions(){ /* 执行当前语句并推进处理流程。 */
 const identity=await api('/api/v1/auth/me') /* 声明 identity。 */
 applyAccessVersion(identity.accessVersion)
 permissionState.items=identity.permissions||[];permissionState.ready=true /* 更新 permissionState.items 的值。 */
 localStorage.setItem('iot_permissions',JSON.stringify(permissionState.items));return identity /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
export function applyAccessVersion(value = '') {
 localStorage.setItem('iot_access_version', value)
 permissionState.accessVersion = value
}
export function resetPermissions(){permissionState.accessVersion='';permissionState.items=[];permissionState.ready=false} /* 执行当前语句并推进处理流程。 */
export const permissionDirective={ /* 执行当前语句并推进处理流程。 */
 mounted(el,binding){el.__permissionValue=binding.value;el.__permissionStop=watchEffect(()=>{el.style.display=can(el.__permissionValue)?'':'none';el.setAttribute('data-permission',JSON.stringify(el.__permissionValue))})}, /* 执行当前语句并推进处理流程。 */
 updated(el,binding){el.__permissionValue=binding.value;el.style.display=can(binding.value)?'':'none'}, /* 执行当前语句并推进处理流程。 */
 unmounted(el){el.__permissionStop?.()} /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
