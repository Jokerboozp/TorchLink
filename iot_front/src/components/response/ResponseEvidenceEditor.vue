<script setup>
import { useDeviceLabels } from '../../useDeviceCatalog.js'
import { onBeforeUnmount, ref } from 'vue'
import { Plus, Paperclip, Trash2 } from '@lucide/vue'
import { can } from '../../permissions.js'
import { createClientId } from '../../clientId.js'
import { responseAttachment, responseUpload } from '../../response/api.js'
import { evidenceKinds } from '../../response/helpers.js'
const props=defineProps({modelValue:{type:Array,default:()=>[]},devices:{type:Array,default:()=>[]},executionId:String,disabled:Boolean})
useDeviceLabels(() => props.devices.map(row => row.id))
const emit=defineEmits(['update:modelValue'])
const fileInput=ref(null),uploading=ref(false),error=ref(''),pendingFile=ref(null)
let generation=0,disposed=false,uploadKey='',uploadFile=null
function add(){emit('update:modelValue',[...props.modelValue,{kind:'MANUAL_RECORD',deviceId:props.devices[0]?.id||'',sourceId:'',description:''}])}
function remove(index){emit('update:modelValue',props.modelValue.filter((_,i)=>i!==index))}
async function upload(event){const file=event?.target?.files?.[0]||pendingFile.value;if(!file||uploading.value)return;const token=generation;uploading.value=true;error.value='';if(uploadFile!==file){uploadFile=file;pendingFile.value=file;uploadKey=createClientId()}try{if(file.size>16*1024*1024)throw Error('附件最大16MiB');const value=await responseUpload(props.executionId,file,uploadKey);if(token!==generation||disposed)return;emit('update:modelValue',[...props.modelValue,{kind:'ATTACHMENT',deviceId:props.devices[0]?.id||'',sourceId:value.id,description:value.body.name}]);uploadFile=null;pendingFile.value=null;uploadKey='';if(fileInput.value)fileInput.value.value=''}catch(cause){if(token===generation&&!disposed)error.value=cause.message}finally{if(token===generation)uploading.value=false}}
async function download(row){try{await responseAttachment(row.sourceId,row.description)}catch(cause){error.value=cause.message}}
onBeforeUnmount(()=>{disposed=true;generation++})
</script>
<template><div class="response-stack"><div class="response-toolbar"><strong>具体证据资料</strong><div class="response-actions"><ui-button :disabled="disabled" size="small" @click="add"><Plus/>增加证据</ui-button><ui-button v-if="executionId&&can('POST /api/v1/response-runs/:id/attachments')" size="small" :disabled="disabled" :loading="uploading" @click="fileInput.click()"><Paperclip/>上传受权附件</ui-button><input ref="fileInput" type="file" style="display:none" @change="upload"></div></div><ui-alert v-if="error" :title="error" type="error" :closable="false"/><ui-button v-if="pendingFile&&!uploading" size="small" :disabled="disabled" @click="upload()">重试上传 {{pendingFile.name}}</ui-button><article v-for="(row,index) in modelValue" :key="index" class="response-card"><div class="response-grid"><ui-form-item label="证据类型"><ui-select v-model="row.kind" :disabled="disabled||row.kind==='ATTACHMENT'" @change="row.sourceId='' "><ui-option v-for="[value,label] in Object.entries(evidenceKinds)" :key="value" :value="value" :label="label"/></ui-select></ui-form-item><ui-form-item label="证据设备"><ui-select v-model="row.deviceId" :disabled="disabled"><ui-option v-for="d in devices" :key="d.id" :value="d.id" :label="d.name || d.id"/></ui-select></ui-form-item></div><ui-form-item v-if="row.kind!=='MANUAL_RECORD'" label="具体资料引用 ID"><ui-input v-model="row.sourceId" :disabled="disabled||row.kind==='ATTACHMENT'" placeholder="引用一条实际资料；保存时由服务器读取并校验"/></ui-form-item><ui-form-item label="实际内容或证据说明"><ui-input v-model="row.description" type="textarea" :rows="2" :disabled="disabled"/></ui-form-item><div class="response-actions"><ui-button v-if="row.kind==='ATTACHMENT'&&can('GET /api/v1/response-attachments/:id')" size="small" text @click="download(row)">受权下载附件</ui-button><ui-button :disabled="disabled" size="small" text @click="remove(index)"><Trash2/>移除关联</ui-button></div></article><p v-if="!modelValue.length" class="response-hint">尚未关联证据；节点完成率按固定流程的规定证据计算。</p></div></template>
