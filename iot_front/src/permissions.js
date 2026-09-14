import { reactive, watchEffect } from 'vue'
import { api, session } from './api'

function saved(){try{const value=JSON.parse(localStorage.getItem('iot_permissions')||'null');return Array.isArray(value)?value:null}catch{return null}}
export const permissionState=reactive({items:saved() || (session.role==='admin'?['*']:[]),ready:false})
export function can(permission){return permissionState.items.includes('*') || (Array.isArray(permission)?permission.some(can):permissionState.items.includes(permission))}
export async function refreshPermissions(){
 const identity=await api('/api/v1/auth/me')
 permissionState.items=identity.permissions||[];permissionState.ready=true
 localStorage.setItem('iot_permissions',JSON.stringify(permissionState.items));return identity
}
export function resetPermissions(){permissionState.items=[];permissionState.ready=false}
export const permissionDirective={
 mounted(el,binding){el.__permissionValue=binding.value;el.__permissionStop=watchEffect(()=>{el.style.display=can(el.__permissionValue)?'':'none';el.setAttribute('data-permission',JSON.stringify(el.__permissionValue))})},
 updated(el,binding){el.__permissionValue=binding.value;el.style.display=can(binding.value)?'':'none'},
 unmounted(el){el.__permissionStop?.()}
}
