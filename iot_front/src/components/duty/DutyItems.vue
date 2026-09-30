<script setup>
import {computed,onBeforeUnmount,onMounted,reactive,ref,watch} from 'vue'
import {Plus,RefreshCw} from '@lucide/vue'
import {dutyRead,dutyWrite} from '../../duty/api.js'
import {dutyTime,itemPayload,statusText} from '../../duty/state.js'
import {notifyError,session} from '../../api.js'
import {can} from '../../permissions.js'
import {UiMessage} from '../../ui/feedback.js'
import {createClientId} from '../../clientId.js'
import DutyUserSelect from './DutyUserSelect.vue'
import DataTableCard from '../layout/DataTableCard.vue'
const props=defineProps({catalog:Object,runId:{type:String,default:''},stationId:{type:String,default:''},embedded:Boolean,dialogOnly:Boolean})
const status=ref(''),station=ref(props.stationId),owner=ref(''),rows=ref([]),total=ref(0),page=ref(1),pageSize=ref(20),loading=ref(false),error=ref(''),dialog=ref(''),saving=ref(false),form=reactive({}),events=ref([]),runs=ref([]),original=ref(null)
const editable=computed(()=>can('action:duty:item'))
defineExpose({edit})
const selectedRun=computed(()=>runs.value.find(run=>run.id===form.runId))
let generation=0
async function load(){const token=++generation;loading.value=true;error.value='';try{const value=await dutyRead('items','','',{stationId:props.stationId || station.value,runId:props.runId,status:status.value,userId:owner.value,limit:pageSize.value,offset:(page.value-1)*pageSize.value});if(token!==generation)return;rows.value=value.items || [];total.value=value.total || 0}catch(exception){if(token===generation)error.value=exception.message}finally{if(token===generation)loading.value=false}}
watch([status,station,owner,pageSize,()=>props.runId],()=>{page.value=1;load()});watch(page,load)
function edit(row,kind='edit'){dialog.value=kind;original.value=row;void loadRuns();events.value=[];for(const key of Object.keys(form))delete form[key];Object.assign(form,{stationId:props.stationId || station.value || '',runId:props.runId,title:'',deviceId:'',alarmId:'',ownerId:session.user,status:'OPEN',nextAction:'',dueAt:null,result:'',reason:''},row || {},{idempotencyKey:createClientId(),statusKey:createClientId()});if(row?.id)loadHistory(row.id)}
async function loadRuns(){try{const value=await dutyRead('runs','','',{status:'ACTIVE',limit:100});runs.value=value.items || []}catch(exception){notifyError(exception)}}
function selectRun(){const run=selectedRun.value;if(run){form.stationId=run.stationId;form.ownerId=run.memberIds.includes(session.user)?session.user:run.leaderId}}
async function loadHistory(id){try{const value=await dutyRead('items',id,'events',{limit:100});if(form.id===id)events.value=value.items || []}catch(exception){notifyError(exception)}}
async function save(){if(saving.value)return;saving.value=true;try{const body=itemPayload(form);if(!form.id){if(!form.runId)throw Error('请选择当前实际值班');await dutyWrite('items','','',body)}else if(dialog.value==='transfer'){if(!form.reason?.trim())throw Error('请填写转交说明');await dutyWrite('items',form.id,'transfer',{...body,content:form.reason})}else{const updated=await dutyWrite('items',form.id,'',body,'PUT');if(form.status!==original.value?.status)await dutyWrite('items',form.id,'status',{...body,version:updated.version,idempotencyKey:form.statusKey})}dialog.value='';await load();UiMessage.success('事项已保存')}catch(exception){notifyError(exception)}finally{saving.value=false}}
onMounted(load);onBeforeUnmount(()=>generation++)
</script>
<template>
 <section v-if="!dialogOnly" class="duty-panel">
  <div class="duty-toolbar"><h3 v-if="embedded" class="item-title">本班跟进事项</h3><div v-else class="item-filters"><ui-select v-model="station" clearable filterable placeholder="全部岗位"><ui-option v-for="value in catalog.stations" :key="value.id" :value="value.id" :label="value.name"/></ui-select><ui-select v-model="status" clearable placeholder="全部状态"><ui-option v-for="value in ['OPEN','IN_PROGRESS','PENDING_VERIFICATION','DONE','CANCELLED']" :key="value" :value="value" :label="statusText(value)"/></ui-select><DutyUserSelect v-model="owner" :users="catalog.users" placeholder="全部负责人"/></div><div class="duty-actions"><ui-button :loading="loading" @click="load"><RefreshCw/>刷新</ui-button><ui-button v-if="editable" type="primary" @click="edit()"><Plus/>新增事项</ui-button></div></div>
  <DataTableCard :title="embedded?'':`跟进事项 · ${total} 条`" :total="total" :page="page" :page-size="pageSize" :error="error" @retry="load" @update:page="page=$event" @update:page-size="pageSize=$event">
   <ui-table v-loading="loading" :data="rows" empty-text="暂无跟进事项">
    <ui-table-column label="事项 / 下一步" min-width="260"><template #default="{row}"><strong>{{row.title}}</strong><p class="item-next">{{row.nextAction || '尚未填写下一步动作'}}</p><span class="duty-meta">{{catalog.stationName(row.stationId)}}<span v-if="row.deviceId"> · {{catalog.deviceName(row.deviceId)}}</span></span></template></ui-table-column>
    <ui-table-column label="主责人" min-width="100"><template #default="{row}">{{catalog.userName(row.ownerId)}}</template></ui-table-column>
    <ui-table-column label="状态 / 期限" min-width="170"><template #default="{row}"><ui-tag :type="row.status==='DONE'?'success':row.dueAt && row.dueAt<Date.now() && !['DONE','CANCELLED'].includes(row.status)?'danger':'info'">{{statusText(row.status)}}</ui-tag><p class="duty-meta">{{dutyTime(row.dueAt)}}<span v-if="row.dueAt && row.dueAt<Date.now() && !['DONE','CANCELLED'].includes(row.status)"> · 已逾期</span></p></template></ui-table-column>
    <ui-table-column label="操作" width="180" fixed="right"><template #default="{row}"><ui-button text size="small" @click="edit(row,'history')">记录</ui-button><template v-if="editable && !['DONE','CANCELLED'].includes(row.status)"><ui-button text size="small" @click="edit(row)">处理</ui-button><ui-button text size="small" @click="edit(row,'transfer')">转交</ui-button></template></template></ui-table-column>
   </ui-table>
  </DataTableCard>
 </section>
 <ui-dialog :model-value="!!dialog" :title="dialog==='history'?'事项处理与跨班历史':dialog==='transfer'?'转交事项':form.id?'处理事项':'新增跟进事项'" width="min(760px,94vw)" :close-on-click-modal="false" @close="dialog=''">
  <div class="duty-details"><ui-form v-if="dialog!=='history'" label-position="top" :disabled="saving">
   <ui-form-item v-if="!form.id" label="当前实际值班" required><ui-select v-model="form.runId" filterable :disabled="!!runId" @change="selectRun"><ui-option v-for="run in runs" :key="run.id" :value="run.id" :label="`${catalog.stationName(run.stationId)} · ${dutyTime(run.startedAt)} · ${catalog.userName(run.leaderId)}`"/></ui-select></ui-form-item>
   <ui-form-item label="事项标题" required><ui-input v-model="form.title" :disabled="dialog==='transfer'" maxlength="200"/></ui-form-item>
   <div v-if="dialog!=='transfer'" class="duty-grid"><ui-form-item label="关联设备"><ui-select v-model="form.deviceId" clearable filterable><ui-option v-for="device in catalog.devices.filter(device=>selectedRun?.deviceIds?.includes(device.id || device.deviceId))" :key="device.id || device.deviceId" :value="device.id || device.deviceId" :label="device.name || device.id || device.deviceId"/></ui-select></ui-form-item><ui-form-item label="关联告警 ID（选填）"><ui-input v-model="form.alarmId"/></ui-form-item></div>
   <ui-form-item label="内部主责人" required><DutyUserSelect v-model="form.ownerId" :users="catalog.users.filter(user=>selectedRun?.memberIds?.includes(user.username))" :disabled="!!form.id && dialog!=='transfer'"/></ui-form-item>
   <div v-if="dialog!=='transfer'" class="duty-grid"><ui-form-item label="状态"><ui-select v-model="form.status" :disabled="!form.id"><ui-option v-for="value in ['OPEN','IN_PROGRESS','PENDING_VERIFICATION','DONE','CANCELLED']" :key="value" :value="value" :label="statusText(value)"/></ui-select></ui-form-item><ui-form-item label="约定期限（选填）"><ui-date-time v-model="form.dueAt" clearable/></ui-form-item></div>
   <ui-form-item label="下一步动作" required><ui-input v-model="form.nextAction" type="textarea" :rows="3" placeholder="明确要核实什么、联系谁或采取什么措施"/></ui-form-item>
   <ui-form-item v-if="form.status==='DONE'" label="处理结果" required><ui-input v-model="form.result" type="textarea" :rows="3"/></ui-form-item>
   <ui-form-item v-if="form.status==='CANCELLED'||dialog==='transfer'" :label="dialog==='transfer'?'转交说明':'取消原因'" required><ui-input v-model="form.reason" type="textarea" :rows="3"/></ui-form-item>
  </ui-form><div v-else class="duty-card"><h3>{{form.title}}</h3><p>{{form.nextAction}}</p><p>{{statusText(form.status)}} · 主责 {{catalog.userName(form.ownerId)}} · 截止 {{dutyTime(form.dueAt)}}</p><p v-if="form.result">结果：{{form.result}}</p><p v-if="form.reason">原因：{{form.reason}}</p></div>
  <div v-if="form.id" class="duty-stack"><h3 class="item-title">处理记录</h3><p v-if="!events.length" class="duty-hint">暂无处理记录。</p><article v-for="event in events" :key="event.id" class="duty-card"><span class="duty-meta">{{dutyTime(event.occurredAt)}} · {{catalog.userName(event.actorId)}} · {{statusText(event.type)}}</span><p>{{event.content}}</p><p v-if="event.ownerId" class="duty-meta">{{catalog.userName(event.previousOwnerId)}} → {{catalog.userName(event.ownerId)}}</p></article></div></div>
  <template #footer><ui-button :disabled="saving" @click="dialog=''">关闭</ui-button><ui-button v-if="dialog!=='history'" type="primary" :loading="saving" @click="save">{{dialog==='transfer'?'确认转交':'保存'}}</ui-button></template>
 </ui-dialog>
</template>
<style scoped>.item-title{margin:0;font-size:15px}.item-filters{display:flex;gap:8px;flex:1;min-width:0}.item-filters>*{min-width:150px;max-width:220px}.item-next{margin:5px 0;font-size:12px;white-space:pre-wrap;overflow-wrap:anywhere}@media(max-width:640px){.item-filters{flex-basis:100%;flex-wrap:wrap}.item-filters>*{max-width:none;flex:1;min-width:120px}}</style>
