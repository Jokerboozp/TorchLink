<script setup>
import { ref, watch } from 'vue'
import { api } from '../api'
const props=defineProps({profile:{type:Object,required:true},products:{type:Array,default:()=>[]},productId:String,canPoll:{type:Boolean,default:true}})
const bindings=ref({}), errors=ref({})
let revision=0
watch(()=>props.profile.childProducts?.map(x=>x.productId).join('|'),async()=>{
 const current=++revision
 const result=await Promise.all((props.profile.childProducts||[]).filter(x=>x.productId).map(async x=>{
  try{return [x.productId,await api(`/api/v2/products/${encodeURIComponent(x.productId)}/protocol-binding`),'']}
  catch(e){return [x.productId,null,e.message||'读取协议失败']}
 }))
 if(current!==revision)return
 bindings.value=Object.fromEntries(result.map(x=>[x[0],x[1]]));errors.value=Object.fromEntries(result.map(x=>[x[0],x[2]]))
},{immediate:true})
function addChild(){(props.profile.childProducts ||= []).push({type:'',productId:''})}
function addQuery(){(props.profile.queries ||= []).push({type:'',intervalSec:10})}
</script>
<template>
<section class="protocol-access-settings">
 <template v-if="canPoll"><h4>定时查询</h4><p>填写 Go 协议支持的查询类型。平台等待本次应答后再发送下一条；查询超时会断开连接，等待重新连接。</p>
 <div v-for="(query,index) in profile.queries||[]" :key="index" class="setting-row">
  <el-input v-model="query.type" placeholder="查询类型，例如 read-status" aria-label="查询类型" />
  <el-input-number v-model="query.intervalSec" :min="1" :max="86400" aria-label="查询周期秒" /><span>秒</span>
  <el-button @click="profile.queries.splice(index,1)">移除</el-button>
 </div>
 <el-button :disabled="(profile.queries?.length||0)>=32" @click="addQuery">添加定时查询</el-button></template>
 <h4>子设备产品与协议</h4><p>先为子设备产品绑定协议，再在这里配置报文类型与产品的对应关系。主设备注册后，上报的子设备将自动登记并关联；不同产品可以使用不同协议。</p>
 <div v-for="(child,index) in profile.childProducts||[]" :key="index" class="child-setting">
  <div class="setting-row"><el-input v-model="child.type" placeholder="协议返回的子设备类型" aria-label="子设备类型" />
   <el-select v-model="child.productId" filterable placeholder="子设备产品" aria-label="子设备产品"><el-option v-for="p in products.filter(x=>x.id!==productId && x.status==='ENABLED')" :key="p.id" :value="p.id" :label="p.name" /></el-select>
   <el-button @click="profile.childProducts.splice(index,1)">移除</el-button></div>
  <p v-if="bindings[child.productId]">协议：{{bindings[child.productId].protocolId}} · {{bindings[child.productId].version}}</p>
  <p v-else-if="child.productId" class="binding-error">{{errors[child.productId]||'正在读取协议…'}}</p>
 </div>
 <el-button :disabled="(profile.childProducts?.length||0)>=64" @click="addChild">添加子设备产品</el-button>
</section>
</template>
<style scoped>
.setting-row{display:flex;align-items:center;gap:8px;margin:8px 0}.setting-row .el-input,.setting-row .el-select{flex:1;min-width:120px}.child-setting{margin:12px 0}.binding-error{color:var(--el-color-danger)}p{font-size:13px;color:var(--el-text-color-secondary)}@media(max-width:600px){.setting-row{flex-wrap:wrap}}
</style>
