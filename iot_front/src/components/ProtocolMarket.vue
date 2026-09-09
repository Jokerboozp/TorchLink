<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, download, formatTime, session } from '../api'
const props=defineProps({protocols:{type:Array,default:()=>[]}})
const loading=ref(false),busy=ref(false),error=ref(''),enabled=ref(false),items=ref([]),publisher=ref(''),catalogUrl=ref(''),query=ref('')
const form=reactive({release:'',name:'',description:'',license:'',tags:''})
const available=computed(()=>props.protocols.flatMap(p=>(p.releases||[]).filter(r=>['VALIDATED','PUBLISHED'].includes(r.status)&&r.artifact?.build?.kind==='go-source'&&r.artifact?.filename?.toLowerCase().endsWith('.zip')&&!items.value.some(x=>x.protocolId===p.definition.id&&x.version===r.version)).map(r=>({key:p.definition.id+'@'+r.version,id:p.definition.id,version:r.version,name:p.definition.name}))))
const filtered=computed(()=>items.value.filter(x=>[x.protocolId,x.version,x.name,x.status,x.submittedBy,...(x.tags||[])].join(' ').toLowerCase().includes(query.value.trim().toLowerCase())))
const status=value=>({SUBMITTED:'待审核',APPROVED:'已上架',REJECTED:'已拒绝',WITHDRAWN:'已撤回'})[value]||value
async function load(){loading.value=true;error.value='';try{const data=await api('/api/v2/protocol-market');enabled.value=data.enabled;items.value=data.items||[];publisher.value=data.publisher||'';catalogUrl.value=data.catalogUrl||''}catch(e){error.value=e.message}finally{loading.value=false}}
async function submit(){
 const release=available.value.find(x=>x.key===form.release)
 if(!release||!form.name.trim()||!form.license.trim())return ElMessage.warning('请选择版本并填写名称与许可')
 try{await ElMessageBox.confirm(`提交 ${release.key} 的源码和制品摘要，另一名管理员审核通过后进入组织目录。`,'提交组织审核',{confirmButtonText:'提交审核',cancelButtonText:'取消'})}catch{return}
 busy.value=true;error.value=''
 try{await api(`/api/v2/protocol-market/${encodeURIComponent(release.id)}/${encodeURIComponent(release.version)}`,{method:'POST',body:JSON.stringify({name:form.name,description:form.description,license:form.license,tags:form.tags.split(/[，,]/).map(x=>x.trim()).filter(Boolean),confirmed:true})});form.release='';ElMessage.success('已提交，等待独立审核');await load()}catch(e){error.value=e.message}finally{busy.value=false}
}
async function review(row,decision){
 const generation=row.generation
 let reason
 try{const result=await ElMessageBox.prompt(decision==='WITHDRAWN'?'撤回后将停止新安装的源码下载，已有安装版本保留。请填写原因。':'请核实源码、许可和已执行的样例，填写审核意见。',decision==='APPROVED'?'审核并上架':decision==='REJECTED'?'拒绝提交':'撤回版本',{confirmButtonText:'确认',cancelButtonText:'取消',inputValidator:v=>!!v?.trim()||'请填写意见'});reason=result.value}catch{return}
 busy.value=true;error.value=''
 try{await api(`/api/v2/protocol-market/${encodeURIComponent(row.protocolId)}/${encodeURIComponent(row.version)}/review`,{method:'POST',body:JSON.stringify({decision,reason,generation,confirmed:true})});ElMessage.success(status(decision));await load()}catch(e){error.value=e.message}finally{busy.value=false}
}
async function source(row){
 // The existing authenticated download API checks the current tenant and hash.
 try{await download(`/api/v2/protocols/${encodeURIComponent(row.protocolId)}/releases/${encodeURIComponent(row.version)}/source`,`${row.protocolId}-${row.version}-source.zip`)}catch(e){error.value=e.message}
}
onMounted(load)
</script>
<template>
 <div v-loading="loading">
  <div class="market-toolbar"><el-input v-model="query" placeholder="搜索组织发布的协议或提交者" clearable/><el-button :disabled="busy" @click="load">刷新发布记录</el-button></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false"/>
  <el-empty v-else-if="!enabled&&!loading" description="尚未配置组织发布，请联系管理员"/>
  <template v-if="enabled">
   <p>发布组织：{{publisher}}。每个版本绑定已校验源码，由另一名管理员审核后分发。</p>
   <p class="muted-text">目录地址：{{catalogUrl}}。消费平台须配置可信公钥和独立读取凭据。</p>
   <el-collapse v-if="session.role!=='viewer'">
    <el-collapse-item title="提交协议版本" name="submit">
     <el-form label-position="top"><div class="market-grid">
      <el-form-item label="待提交版本"><el-select v-model="form.release" filterable placeholder="已实际校验的完整 Go ZIP"><el-option v-for="r in available" :key="r.key" :label="r.key" :value="r.key"/></el-select></el-form-item>
      <el-form-item label="目录名称"><el-input v-model="form.name" maxlength="256"/></el-form-item>
      <el-form-item label="组织许可"><el-input v-model="form.license" maxlength="128"/></el-form-item>
      <el-form-item label="标签"><el-input v-model="form.tags" placeholder="多个标签用逗号分隔"/></el-form-item>
     </div><el-form-item label="适用范围与说明"><el-input v-model="form.description" type="textarea" :rows="3" maxlength="4096"/></el-form-item>
     <el-button type="primary" :loading="busy" @click="submit">提交独立审核</el-button></el-form>
    </el-collapse-item>
   </el-collapse>
   <el-table :data="filtered" :row-key="row=>row.protocolId+'@'+row.version" empty-text="暂无组织发布记录">
    <el-table-column type="expand"><template #default="{row}"><div class="market-detail"><p>{{row.description}}</p><p>源码 SHA-256：{{row.sourceSha256}}</p><p>制品 SHA-256：{{row.packageSha256}}</p><p v-for="(review,index) in row.reviews" :key="index">{{formatTime(review.at)}} · {{review.actor}} · {{status(review.decision)}}：{{review.reason}}</p></div></template></el-table-column>
    <el-table-column label="协议版本" min-width="190"><template #default="{row}"><b>{{row.name}}</b><div>{{row.protocolId}} · {{row.version}}</div></template></el-table-column>
    <el-table-column label="提交者" min-width="130"><template #default="{row}">{{row.submittedBy}}<div class="muted-text">{{formatTime(row.submittedAt)}}</div></template></el-table-column>
    <el-table-column prop="license" label="许可" min-width="100"/>
    <el-table-column label="状态" min-width="100"><template #default="{row}">{{status(row.status)}}</template></el-table-column>
    <el-table-column label="操作" min-width="240"><template #default="{row}"><el-button link @click="source(row)">查看源码</el-button><template v-if="session.role==='admin'"><el-button v-if="row.status==='SUBMITTED'&&row.submittedBy!==session.user" :disabled="busy" @click="review(row,'APPROVED')">审核上架</el-button><el-button v-if="row.status==='SUBMITTED'&&row.submittedBy!==session.user" :disabled="busy" @click="review(row,'REJECTED')">拒绝</el-button><el-button v-if="['SUBMITTED','APPROVED'].includes(row.status)" :disabled="busy" @click="review(row,'WITHDRAWN')">撤回</el-button><span v-if="row.status==='SUBMITTED'&&row.submittedBy===session.user" class="muted-text">等待其他管理员审核</span></template></template></el-table-column>
   </el-table>
  </template>
 </div>
</template>
<style scoped>
.market-toolbar{display:flex;gap:8px;margin-bottom:12px}.market-toolbar .el-input{max-width:420px}.market-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.market-detail{padding:12px;overflow-wrap:anywhere}p{overflow-wrap:anywhere}.el-alert{margin-bottom:12px}@media(max-width:600px){.market-grid{grid-template-columns:1fr}}
</style>
