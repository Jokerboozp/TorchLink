<script setup>
import {computed,onBeforeUnmount,onMounted,ref} from 'vue'
import {ClipboardCheck} from '@lucide/vue'
import {dutyRead,dutyWrite} from '../../duty/api.js'
import {notifyError} from '../../api.js'
import {dutyNoticeTarget} from '../../duty/state.js'
const emit=defineEmits(['navigate'])
const items=ref([]),visible=ref(false),loading=ref(false),error=ref('')
let timer=0,generation=0
const unread=computed(()=>items.value.filter(item=>!item.readAt).length)
async function load(){const token=++generation;clearTimeout(timer);loading.value=true;try{const value=await dutyRead('notifications','','',{limit:100});if(token!==generation)return;items.value=value.items || [];error.value=''}catch(exception){if(token===generation)error.value=exception.message}finally{if(token===generation){loading.value=false;timer=setTimeout(load,30000)}}}
async function read(item){try{await dutyWrite('notifications',item.id,'read',{version:item.version});await load()}catch(exception){notifyError(exception)}}
function navigate(item){visible.value=false;emit('navigate','duty',dutyNoticeTarget(item))}
onMounted(load);onBeforeUnmount(()=>{generation++;clearTimeout(timer)})
</script>
<template>
 <button class="topbar-button" type="button" :aria-label="`值班提醒${unread?' · '+unread+'条未读':''}`" @click="visible=true;load()"><ClipboardCheck/><span>值班提醒<span v-if="unread"> · {{unread}}</span></span></button>
 <ui-dialog v-model="visible" title="值班提醒" width="min(600px,94vw)">
  <div class="duty-notice-list"><ui-alert v-if="error" type="error" :closable="false" :title="error"/><ui-skeleton v-if="loading && !items.length" :rows="4"/><ui-empty v-else-if="!items.length" description="暂无值班提醒"/><article v-for="item in items" :key="item.id" class="duty-notice"><div><strong>{{item.title}}</strong><ui-tag v-if="!item.readAt" size="small">未读</ui-tag><p>{{item.content}}</p></div><div class="duty-actions"><ui-button size="small" @click="navigate(item)">查看</ui-button><ui-button v-if="!item.readAt" size="small" @click="read(item)">已读</ui-button></div></article></div>
 </ui-dialog>
</template>
<style scoped>.duty-notice-list{max-height:60vh;overflow-y:auto;display:grid;gap:12px}.duty-notice{padding:12px;border:1px solid var(--border);border-radius:8px;display:grid;gap:8px}.duty-notice strong{margin-right:8px;font-size:14px}.duty-notice p{font-size:13px;white-space:pre-wrap;overflow-wrap:anywhere}.duty-actions{display:flex;gap:8px;justify-content:flex-end}</style>
