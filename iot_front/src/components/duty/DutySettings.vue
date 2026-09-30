<script setup>
import {reactive,ref} from 'vue'
import {Plus,RefreshCw} from '@lucide/vue'
import {dutySave,dutyWrite} from '../../duty/api.js'
import {notifyError} from '../../api.js'
import {UiMessage,UiMessageBox} from '../../ui/feedback.js'
import {can} from '../../permissions.js'
import {createClientId} from '../../clientId.js'
import DutyUserSelect from './DutyUserSelect.vue'
const props=defineProps({catalog:Object})
const emit=defineEmits(['refresh'])
const tab=ref('stations'),dialog=ref(''),saving=ref(false),form=reactive({})
const names={stations:'岗位',teams:'班组','shift-templates':'班次模板'}
function edit(kind,row){dialog.value=kind;for(const key of Object.keys(form))delete form[key];Object.assign(form,kind==='stations'?{name:'',supervisorId:'',deviceIds:[],requiredPeople:1,reminderMinutes:15,enabled:true,timezone:'Asia/Shanghai'}:kind==='teams'?{name:'',memberIds:[],leaderId:''}:{name:'',startTime:'08:00',endTime:'20:00',endDayOffset:0},row || {},{idempotencyKey:createClientId()})}
async function save(){if(saving.value)return;saving.value=true;try{if(!form.name?.trim())throw Error('请填写名称');if(dialog.value==='stations' && !form.supervisorId)throw Error('请选择责任主管');if(dialog.value==='teams' && !form.memberIds.includes(form.leaderId))throw Error('交接负责人必须在班组成员中');await dutySave(dialog.value,form);dialog.value='';emit('refresh');UiMessage.success('已保存')}catch(error){notifyError(error)}finally{saving.value=false}}
async function remove(kind,row){try{await UiMessageBox.confirm(`确认删除${names[kind]}“${row.name}”？已被使用的配置由服务端检查。`,'删除确认',{type:'warning'});await dutyWrite(kind,row.id,'',{version:row.version},'DELETE');emit('refresh')}catch(error){if(error!=='cancel' && error!=='close')notifyError(error)}}
</script>
<template>
 <section class="duty-panel">
  <div class="duty-toolbar"><ui-tabs v-model="tab"><ui-tab-pane label="岗位" name="stations"/><ui-tab-pane label="班组" name="teams"/><ui-tab-pane label="班次模板" name="shift-templates"/></ui-tabs><ui-button @click="emit('refresh')"><RefreshCw/>刷新</ui-button><ui-button v-if="can('action:duty:settings')" type="primary" @click="edit(tab)"><Plus/>新增{{names[tab]}}</ui-button></div>
  <p class="duty-hint">岗位定义责任设备范围；排班不会自动增加人员权限。每个岗位同时只有一个实际责任班次。</p>
  <div class="duty-table-scroll"><ui-table :data="catalog[tab==='shift-templates'?'templates':tab] || []" empty-text="暂无配置，先创建岗位、班组及班次模板">
   <ui-table-column prop="name" label="名称" min-width="150"/>
   <ui-table-column v-if="tab==='stations'" label="责任主管"><template #default="{row}">{{catalog.userName(row.supervisorId)}}</template></ui-table-column>
   <ui-table-column v-if="tab==='stations'" label="设备范围"><template #default="{row}">{{row.deviceIds?.length || 0}} 台 · 范围版本 {{row.scopeVersion || 1}}</template></ui-table-column>
   <ui-table-column v-if="tab==='stations'" label="值班要求"><template #default="{row}">{{row.requiredPeople}} 人 · 提前 {{row.reminderMinutes}} 分钟提醒 · {{row.enabled?'启用':'停用'}}</template></ui-table-column>
   <ui-table-column v-if="tab==='teams'" label="人员"><template #default="{row}">{{(row.memberIds || []).map(catalog.userName).join('、')}}</template></ui-table-column>
   <ui-table-column v-if="tab==='teams'" label="交接负责人"><template #default="{row}">{{catalog.userName(row.leaderId)}}</template></ui-table-column>
   <ui-table-column v-if="tab==='shift-templates'" label="时间"><template #default="{row}">{{row.startTime}} — {{row.endDayOffset?'次日 ':''}}{{row.endTime}}</template></ui-table-column>
   <ui-table-column v-if="can('action:duty:settings')" label="操作" width="150" fixed="right"><template #default="{row}"><ui-button text size="small" @click="edit(tab,row)">编辑</ui-button><ui-button text type="danger" size="small" @click="remove(tab,row)">删除</ui-button></template></ui-table-column>
  </ui-table></div>
 </section>
 <ui-dialog :model-value="!!dialog" :title="`${form.id?'编辑':'新增'}${names[dialog] || ''}`" width="min(680px,94vw)" :close-on-click-modal="false" @close="dialog=''">
  <ui-form class="duty-form" label-position="top" :disabled="saving">
   <ui-form-item label="名称" required><ui-input v-model="form.name" maxlength="120"/></ui-form-item>
   <template v-if="dialog==='stations'">
    <ui-form-item label="责任主管" required><DutyUserSelect v-model="form.supervisorId" :users="catalog.users"/></ui-form-item>
    <ui-form-item label="负责设备（主子设备分别选择）" required><ui-select v-model="form.deviceIds" multiple filterable clearable><ui-option v-for="device in catalog.devices" :key="device.id || device.deviceId" :value="device.id || device.deviceId" :label="device.name || device.deviceName || device.id || device.deviceId"/></ui-select></ui-form-item>
    <div class="duty-grid"><ui-form-item label="要求人数"><ui-input-number v-model="form.requiredPeople" :min="1" :max="100"/></ui-form-item><ui-form-item label="提前提醒（分钟）"><ui-input-number v-model="form.reminderMinutes" :min="0" :max="240"/></ui-form-item></div>
    <ui-form-item label="时区"><ui-input v-model="form.timezone" placeholder="Asia/Shanghai"/></ui-form-item><ui-form-item label="启用岗位"><ui-switch v-model="form.enabled"/></ui-form-item>
   </template>
   <template v-if="dialog==='teams'">
    <ui-form-item label="班组成员" required><DutyUserSelect v-model="form.memberIds" :users="catalog.users" multiple/></ui-form-item>
    <ui-form-item label="默认交接负责人" required><DutyUserSelect v-model="form.leaderId" :users="catalog.users.filter(user=>form.memberIds.includes(user.username))"/></ui-form-item>
   </template>
   <template v-if="dialog==='shift-templates'"><div class="duty-grid"><ui-form-item label="开始时间"><ui-time-picker v-model="form.startTime"/></ui-form-item><ui-form-item label="结束时间"><ui-time-picker v-model="form.endTime"/></ui-form-item></div><ui-form-item label="结束日期"><ui-select v-model="form.endDayOffset"><ui-option :value="0" label="当天"/><ui-option :value="1" label="次日"/></ui-select></ui-form-item></template>
  </ui-form>
  <template #footer><ui-button :disabled="saving" @click="dialog=''">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存</ui-button></template>
 </ui-dialog>
</template>
