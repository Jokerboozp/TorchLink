<script setup>
import {computed,onBeforeUnmount,onMounted,reactive,ref} from 'vue'
import {ClipboardCheck,RefreshCw} from '@lucide/vue'
import {dutyAll,dutyRead} from '../duty/api.js'
import {can} from '../permissions.js'
import DutySettings from '../components/duty/DutySettings.vue'
import DutyRoster from '../components/duty/DutyRoster.vue'
import DutyCurrent from '../components/duty/DutyCurrent.vue'
import DutyItems from '../components/duty/DutyItems.vue'
import DutyHandovers from '../components/duty/DutyHandovers.vue'
const emit=defineEmits(['navigate'])
const tab=ref('current'),loading=ref(false),error=ref('')
const catalog=reactive({stations:[],teams:[],templates:[],users:[],devices:[],userName:id=>catalog.users.find(user=>user.username===id)?.displayName || id || '—',stationName:id=>catalog.stations.find(station=>station.id===id)?.name || '岗位已不可见',deviceName:id=>catalog.devices.find(device=>(device.id || device.deviceId)===id)?.name || id || '—'})
let generation=0
async function load(){const token=++generation;loading.value=true;error.value='';try{const [stations,teams,templates,options]=await Promise.all([dutyAll('stations'),dutyAll('teams'),dutyAll('shift-templates'),dutyRead('options')]);if(token!==generation)return;Object.assign(catalog,{stations:stations.items || [],teams:teams.items || [],templates:templates.items || [],users:options.users || [],devices:options.devices || []})}catch(exception){if(token===generation)error.value=exception.message || '值班配置加载失败'}finally{if(token===generation)loading.value=false}}
const allowedTabs=computed(()=>[{name:'current',label:'我的值班'},{name:'rosters',label:'排班日历'},{name:'handovers',label:'交接记录'},{name:'items',label:'跟进事项'},{name:'settings',label:'值班设置',allowed:can('action:duty:settings')}].filter(item=>item.allowed!==false))
const handoverId=ref('')
function showHandover(id){handoverId.value=id;tab.value='handovers'}
onMounted(()=>{load();try{const value=JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}');sessionStorage.removeItem('iot:navigation-detail');if(allowedTabs.value.some(item=>item.name===value.tab))tab.value=value.tab;if(value.handoverId)showHandover(value.handoverId)}catch{/* Invalid navigation detail has no effect. */}});onBeforeUnmount(()=>{generation++})
</script>
<template>
 <div class="duty-management">
  <div class="duty-heading"><div><h1><ClipboardCheck/>值班管理</h1><p>记录处置，核对交接，接续未完成事项。</p></div><ui-button :loading="loading" @click="load"><RefreshCw/>刷新配置</ui-button></div>
  <ui-alert v-if="error" type="error" :closable="false" :title="error"><ui-button size="small" @click="load">重新加载</ui-button></ui-alert>
  <ui-tabs v-model="tab"><ui-tab-pane v-for="item in allowedTabs" :key="item.name" :name="item.name" :label="item.label"/></ui-tabs>
  <ui-skeleton v-if="loading && !catalog.stations.length" :rows="5" animated/>
  <template v-else>
   <DutyCurrent v-if="tab==='current'" :catalog="catalog" @handover="showHandover"/>
   <DutyRoster v-if="tab==='rosters'" :catalog="catalog"/>
   <DutyHandovers v-if="tab==='handovers'" :catalog="catalog" :open-id="handoverId" @clear-open="handoverId=''" @navigate="(page,detail)=>emit('navigate',page,detail)"/>
   <DutyItems v-if="tab==='items'" :catalog="catalog"/>
   <DutySettings v-if="tab==='settings'" :catalog="catalog" @refresh="load"/>
  </template>
 </div>
</template>
<style>
.duty-management{display:grid;grid-template-columns:minmax(0,1fr);gap:16px;min-width:0}.duty-heading,.duty-toolbar,.duty-actions{display:flex;gap:10px;flex-wrap:wrap;align-items:center;justify-content:space-between}.duty-heading h1{display:flex;gap:8px;align-items:center;margin:0;font-size:20px}.duty-heading h1 svg{width:22px;height:22px}.duty-heading p,.duty-hint{margin:5px 0 0;color:var(--text-muted);font-size:12px;line-height:1.6}.duty-panel{display:grid;grid-template-columns:minmax(0,1fr);gap:14px;min-width:0}.duty-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.duty-form{display:grid;gap:10px;max-height:65vh;overflow-y:auto;padding-right:5px}.duty-form .n-form-item{min-width:0}.duty-toolbar>.ui-tabs{flex:1;min-width:0}.duty-native{width:100%;min-width:0;border:1px solid var(--border);border-radius:6px;background:var(--surface);color:var(--text);padding:7px 9px;font:inherit}.duty-actions{justify-content:flex-end}.duty-card{padding:16px;border:1px solid var(--border);border-radius:8px;background:var(--surface);min-width:0}.duty-card h3{margin:0 0 10px;font-size:14px}.duty-stack{display:grid;gap:12px}.duty-row{display:flex;gap:12px;justify-content:space-between;align-items:flex-start;min-width:0}.duty-row>div{min-width:0}.duty-row p{white-space:pre-wrap;overflow-wrap:anywhere;margin:6px 0;font-size:13px}.duty-meta{color:var(--text-muted);font-size:12px;line-height:1.6}.duty-label{display:block;color:var(--text-muted);font-size:12px;margin-bottom:6px}.duty-details{max-height:72vh;overflow-y:auto;display:grid;gap:16px;padding:0 5px 10px 0}.duty-table-scroll{overflow-x:auto;min-width:0}.duty-tags{display:flex;gap:6px;flex-wrap:wrap}.duty-snapshot-table{width:100%;border-collapse:collapse;font-size:12px}.duty-snapshot-table th,.duty-snapshot-table td{padding:9px;text-align:left;border-bottom:1px solid var(--border);vertical-align:top;overflow-wrap:anywhere}.duty-snapshot-table th{color:var(--text-muted);white-space:nowrap}.duty-snapshot-table td{min-width:100px}.duty-loading{padding:16px;color:var(--text-muted)}@media(max-width:640px){.duty-grid{grid-template-columns:1fr}.duty-heading{align-items:flex-start}.duty-heading h1{font-size:18px}.duty-heading>button{margin-left:auto}.duty-toolbar{align-items:stretch}.duty-toolbar .ui-select{width:100%}.duty-actions{justify-content:flex-start}.duty-card{padding:12px}.duty-form{max-height:60vh}.duty-details{max-height:68vh}.duty-row{flex-wrap:wrap}.duty-snapshot-table{min-width:560px}}
</style>
