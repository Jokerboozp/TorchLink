<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, formatTime, session } from '../api'
const props = defineProps({ protocols:{type:Array,default:()=>[]} })
const emit = defineEmits(['installed'])
const loading=ref(false), installing=ref(''), error=ref(''), enabled=ref(false), catalog=ref(null), query=ref('')
const entries=computed(()=>(catalog.value?.entries||[]).filter(entry=>[entry.id,entry.name,entry.description,entry.publisher,...(entry.tags||[])].join(' ').toLowerCase().includes(query.value.trim().toLowerCase())))
const isInstalled=entry=>props.protocols.some(item=>item.definition.id===entry.id&&item.releases?.some(release=>release.version===entry.version))
async function load(){
 loading.value=true;error.value=''
 try{const result=await api('/api/v2/protocol-catalog');enabled.value=result.enabled;catalog.value=result.catalog||null}
 catch(e){catalog.value=null;error.value=e.message}
 finally{loading.value=false}
}
async function install(entry){
 const digest=catalog.value?.digest
 try{await ElMessageBox.confirm(`安装 ${entry.name} ${entry.version} 将在服务进程权限下执行协议样例校验。签名来源：${entry.publisher||'未填写发布者'}。请确认信任此来源。`,'安装协议',{confirmButtonText:'安装并校验',cancelButtonText:'取消',type:'warning'})}
 catch{return}
 installing.value=entry.id+'@'+entry.version;error.value=''
 try{
  await api('/api/v2/protocol-catalog/install',{method:'POST',body:JSON.stringify({id:entry.id,version:entry.version,digest,confirmed:true})})
  ElMessage.success('源码与样例校验通过，已保存校验版本，可在版本列表发布')
  emit('installed')
 }catch(e){error.value=e.message}
 finally{installing.value=''}
}
onMounted(load)
</script>
<template>
 <div v-loading="loading">
  <div class="catalog-toolbar"><el-input v-model="query" placeholder="搜索协议、发布者或标签" clearable/><el-button :disabled="!!installing" @click="load">刷新目录</el-button></div>
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon/>
  <el-empty v-else-if="!enabled && !loading" description="尚未配置可信协议目录，请联系管理员"/>
  <template v-if="catalog">
   <p class="muted-text">已验证目录签名，有效至 {{formatTime(catalog.expiresAt)}}。安装后保留为校验版本，可在版本列表发布和绑定产品。</p>
   <el-table :data="entries" empty-text="没有匹配的协议">
    <el-table-column label="协议" min-width="210"><template #default="{row}"><strong>{{row.name}}</strong><div class="muted-text">{{row.id}} · {{row.version}}</div><p>{{row.description}}</p></template></el-table-column>
    <el-table-column prop="publisher" label="发布者" min-width="140"/>
    <el-table-column prop="license" label="许可" min-width="110"/>
    <el-table-column label="标签" min-width="140"><template #default="{row}">{{(row.tags||[]).join('、')||'—'}}</template></el-table-column>
    <el-table-column label="操作" width="140" fixed="right"><template #default="{row}"><el-button v-if="session.role==='admin'" :disabled="!!installing||isInstalled(row)" :loading="installing===row.id+'@'+row.version" @click="install(row)">{{isInstalled(row)?'已安装':'安装并校验'}}</el-button><span v-else class="muted-text">管理员可安装</span></template></el-table-column>
   </el-table>
  </template>
 </div>
</template>
<style scoped>
.catalog-toolbar{display:flex;gap:8px;margin-bottom:12px}.catalog-toolbar .el-input{max-width:420px}
p{white-space:normal;overflow-wrap:anywhere}.el-alert{margin-bottom:12px}
</style>
