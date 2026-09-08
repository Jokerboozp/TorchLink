<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api, notifyError } from '../api'
const emit=defineEmits(['close','changed'])
const items=ref([]),busy=ref(false),editing=ref(false)
const blank=()=>({id:'',name:'',status:'ENABLED',description:''})
const form=reactive(blank())
async function load(){try{items.value=(await api('/api/v1/edge-nodes')).items||[]}catch(e){notifyError(e)}}
function edit(item){Object.assign(form,item||blank());editing.value=!!item}
async function save(){busy.value=true;try{await api(editing.value?`/api/v1/edge-nodes/${encodeURIComponent(form.id)}`:'/api/v1/edge-nodes',{method:editing.value?'PUT':'POST',body:JSON.stringify(form)});edit();await load();emit('changed')}catch(e){notifyError(e)}finally{busy.value=false}}
onMounted(load)
</script>
<template>
<el-dialog :model-value="true" title="Edge 节点登记" width="min(720px,96vw)" append-to-body @close="emit('close')">
<p>登记现场节点及归属。此记录不会部署 Agent，也不会把平台采集任务迁移到远端；状态表示是否允许关联。</p>
<el-table :data="items"><el-table-column prop="id" label="标识"/><el-table-column prop="name" label="名称"/><el-table-column prop="status" label="状态"/><el-table-column><template #default="{row}"><el-button link @click="edit(row)">编辑</el-button></template></el-table-column></el-table>
<el-form label-position="top"><el-form-item label="节点标识"><el-input v-model="form.id" :disabled="editing" maxlength="128"/></el-form-item><el-form-item label="名称"><el-input v-model="form.name" maxlength="256"/></el-form-item><el-form-item label="状态"><el-select v-model="form.status"><el-option value="ENABLED" label="允许关联"/><el-option value="DISABLED" label="停用"/></el-select></el-form-item><el-form-item label="说明"><el-input v-model="form.description" type="textarea" maxlength="4096"/></el-form-item></el-form>
<template #footer><el-button @click="edit()">新增节点</el-button><el-button type="primary" :loading="busy" @click="save">保存登记</el-button></template>
</el-dialog>
</template>
