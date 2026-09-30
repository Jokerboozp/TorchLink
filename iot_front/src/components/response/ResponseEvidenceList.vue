<script setup>
import { ref } from 'vue'
import { can } from '../../permissions.js'
import { responseAttachment } from '../../response/api.js'
import { evidenceKinds, responseTime, stateLabel } from '../../response/helpers.js'
const props=defineProps({items:{type:Array,default:()=>[]},devices:{type:Array,default:()=>[]}})
const emit=defineEmits(['navigate'])
const error=ref(''),busy=ref(''),deviceName=id=>props.devices.find(row=>row.id===id)?.name||id
async function download(row){if(busy.value)return;busy.value=row.id;error.value='';try{await responseAttachment(row.sourceId,row.description)}catch(cause){error.value=cause.message}finally{busy.value=''}}
</script>
<template><div class="response-stack"><ui-alert v-if="error" :title="error" type="warning" :closable="false"/><article v-for="ev in items" :key="ev.id" class="response-card"><strong>{{evidenceKinds[ev.kind]||ev.kind}} · {{deviceName(ev.deviceId)}}</strong><p>{{ev.description}}</p><p class="response-meta">{{stateLabel(ev.classification)}} · 发生 {{responseTime(ev.occurredAt)}} · 登记 {{responseTime(ev.recordedAt)}}</p><div class="response-actions"><ui-button v-if="ev.kind==='ATTACHMENT'&&can('GET /api/v1/response-attachments/:id')" text size="small" :loading="busy===ev.id" @click="download(ev)">受权下载原件</ui-button><ui-button v-if="ev.kind==='RAW_MESSAGE'&&can('menu:raw')" text size="small" @click="emit('navigate','raw',{deviceId:ev.deviceId,messageId:ev.sourceId})">读取具体原文</ui-button><ui-button v-if="['ALARM_LIFECYCLE','ALARM'].includes(ev.kind)&&ev.resourceId&&can('menu:alarms')" text size="small" @click="emit('navigate','alarms',{deviceId:ev.deviceId,alarmId:ev.resourceId})">查看关联告警</ui-button></div></article></div></template>
