<script setup>
import { onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, notifyError, formatTime } from '../api'
const emit=defineEmits(['close','changed'])
const items=ref([]),busy=ref(false),editing=ref(false),credential=ref(null),runtime=ref(null),profiles=ref([]),selectedProfile=ref(''),targetNode=ref('')
const blank=()=>({id:'',name:'',status:'ENABLED',description:''})
const form=reactive(blank())
async function load(){try{items.value=(await api('/api/v1/edge-nodes')).items||[]}catch(e){notifyError(e)}}
function edit(item){Object.assign(form,item||blank());editing.value=!!item}
async function save(){busy.value=true;try{await api(editing.value?`/api/v1/edge-nodes/${encodeURIComponent(form.id)}`:'/api/v1/edge-nodes',{method:editing.value?'PUT':'POST',body:JSON.stringify(form)});edit();await load();emit('changed')}catch(e){notifyError(e)}finally{busy.value=false}}
async function rotate(row){
 try{await ElMessageBox.confirm('重新生成后旧节点凭据立即失效，请更新现场 Agent 配置。','重新生成节点凭据',{type:'warning'});credential.value=await api(`/api/v1/edge-nodes/${encodeURIComponent(row.id)}/credentials`,{method:'POST',body:'{}'})}catch(e){if(e!=='cancel'&&e!=='close')notifyError(e)}
}
async function inspect(row){try{runtime.value=await api(`/api/v1/edge-nodes/${encodeURIComponent(row.id)}/runtime`);targetNode.value=row.id;profiles.value=(await api('/api/v2/device-access-profiles')).items}catch(e){notifyError(e)}}
async function assign(){
 const p=profiles.value.find(p=>p.id===selectedProfile.value);if(!p)return
 busy.value=true
 try{await api(`/api/v2/device-access-profiles/${encodeURIComponent(p.id)}`,{method:'PUT',body:JSON.stringify({...p,edgeNodeId:targetNode.value})});selectedProfile.value='';emit('changed');await inspect({id:targetNode.value})}catch(e){notifyError(e)}finally{busy.value=false}
}
onBeforeUnmount(()=>{credential.value=null})
onMounted(load)
</script>
<template>
<el-dialog :model-value="true" title="Edge 节点登记" width="min(720px,96vw)" append-to-body @close="emit('close')">
<p>登记节点后生成一次性凭据，在现场启动 Agent。Agent 支持现场协议采集及已发布 Go 协议的 TCP/UDP 接入；配置分配后请检查心跳和首条数据。</p>
<el-table :data="items"><el-table-column prop="id" label="标识"/><el-table-column prop="name" label="名称"/><el-table-column prop="status" label="状态"/><el-table-column><template #default="{row}"><el-button link @click="edit(row)">编辑</el-button><el-button link @click="rotate(row)">凭据</el-button><el-button link @click="inspect(row)">运行与分配</el-button></template></el-table-column></el-table>
<el-alert v-if="credential" title="节点 Secret 仅本次显示，请立即保存" type="warning" :closable="false"/><pre v-if="credential">节点：{{credential.nodeId}}
租户：{{credential.tenantId}}
Secret：{{credential.secret}}</pre>
<div v-if="runtime"><p>运行状态：{{({ONLINE:'在线',OFFLINE:'离线',WAITING:'等待心跳'})[runtime.status]}} · 最后心跳：{{formatTime(runtime.heartbeat?.lastSeenAt)}} · 待补传：{{runtime.heartbeat?.queueDepth||0}} · 待处置拒收：{{runtime.heartbeat?.rejectedDepth||0}}</p><p>{{runtime.heartbeat?.lastError}}</p><el-select v-model="selectedProfile" placeholder="选择已有采集或监听配置"><el-option v-for="p in profiles" :key="p.id" :value="p.id" :label="`${p.deviceId} · ${p.host}:${p.port}`"/></el-select><el-button :disabled="!selectedProfile" :loading="busy" @click="assign">分配到此节点</el-button><el-button @click="inspect({id:targetNode})">刷新心跳</el-button></div>
<el-form label-position="top"><el-form-item label="节点标识"><el-input v-model="form.id" :disabled="editing" maxlength="128"/></el-form-item><el-form-item label="名称"><el-input v-model="form.name" maxlength="256"/></el-form-item><el-form-item label="状态"><el-select v-model="form.status"><el-option value="ENABLED" label="允许关联"/><el-option value="DISABLED" label="停用"/></el-select></el-form-item><el-form-item label="说明"><el-input v-model="form.description" type="textarea" maxlength="4096"/></el-form-item></el-form>
<template #footer><el-button @click="edit()">新增节点</el-button><el-button type="primary" :loading="busy" @click="save">保存登记</el-button></template>
</el-dialog>
</template>
