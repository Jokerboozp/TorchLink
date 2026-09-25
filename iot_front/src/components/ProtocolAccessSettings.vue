<script setup>
import { computed, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { api } from '../api' /* 引入当前代码需要的依赖。 */
const props=defineProps({profile:{type:Object,required:true},products:{type:Array,default:()=>[]},productId:String,canPoll:{type:Boolean,default:true}}) /* 声明 props。 */
const bindings=ref({}), errors=ref({}) /* 声明 bindings。 */
const childTemplates=computed(()=>props.products.filter(x=>x.id!==props.productId && x.status==='ENABLED'))
let revision=0 /* 声明 revision。 */
watch(()=>props.profile.childProducts?.map(x=>x.productId).join('|'),async()=>{ /* 执行当前语句并推进处理流程。 */
 const current=++revision /* 声明 current。 */
 const result=await Promise.all((props.profile.childProducts||[]).filter(x=>x.productId).map(async x=>{ /* 声明 result。 */
  try{return [x.productId,await api(`/api/v2/products/${encodeURIComponent(x.productId)}/protocol-binding`),'']} /* 执行当前语句并推进处理流程。 */
  catch(e){return [x.productId,null,e.message||'读取协议失败']} /* 执行当前语句并推进处理流程。 */
 })) /* 结束当前表达式或代码块。 */
 if(current!==revision)return /* 判断条件并选择处理分支。 */
 bindings.value=Object.fromEntries(result.map(x=>[x[0],x[1]]));errors.value=Object.fromEntries(result.map(x=>[x[0],x[2]])) /* 更新 bindings.value 的值。 */
},{immediate:true}) /* 结束当前表达式或代码块。 */
function addChild(){(props.profile.childProducts ||= []).push({type:'',productId:''})} /* 定义 addChild 函数。 */
function addQuery(){(props.profile.queries ||= []).push({type:'',intervalSec:10})} /* 定义 addQuery 函数。 */
</script>
<template>
<div class="protocol-access-settings">
  <section v-if="canPoll" class="access-option-section">
    <header class="access-section-heading"><div><h4>定时读取设备数据</h4><p>仅在已发布协议提供读取能力时设置。按协议填写读取内容；平台等待应答后再执行下一次读取。</p></div><span>{{ profile.queries?.length || 0 }} / 32</span></header>
    <div v-if="!profile.queries?.length" class="access-empty">尚未设置定时读取。需要平台主动读取设备数据时，再添加读取任务。</div>
    <div v-for="(query,index) in profile.queries||[]" :key="index" class="access-config-card">
      <div class="access-card-heading"><strong>读取任务 {{ index + 1 }}</strong><ui-button plain @click="profile.queries.splice(index,1)">移除</ui-button></div>
      <div class="access-field-grid">
        <div class="access-field"><span>协议读取内容</span><ui-input v-model="query.type" placeholder="填写协议声明的读取内容" aria-label="读取内容" /></div>
        <div class="access-field"><span>读取间隔（秒）</span><ui-input-number v-model="query.intervalSec" :min="1" :max="86400" aria-label="查询周期秒" /></div>
      </div>
    </div>
    <ui-button class="access-add-button" :disabled="(profile.queries?.length||0)>=32" @click="addQuery">添加定时读取</ui-button>
  </section>

  <section class="access-option-section">
    <header class="access-section-heading"><div><h4>子设备类型映射</h4><p>把协议返回的子设备类型关联到设备模板。模板须先绑定协议，识别到的子设备才会自动登记。</p></div><span>{{ profile.childProducts?.length || 0 }} / 64</span></header>
    <div v-if="!profile.childProducts?.length" class="access-empty">尚未设置子设备映射。主设备不下接其他设备时，可保持为空。</div>
    <div v-for="(child,index) in profile.childProducts||[]" :key="index" class="access-config-card">
      <div class="access-card-heading"><strong>子设备映射 {{ index + 1 }}</strong><ui-button plain @click="profile.childProducts.splice(index,1)">移除</ui-button></div>
      <div class="access-field-grid">
        <div class="access-field"><span>协议返回的子设备类型</span><ui-input v-model="child.type" placeholder="例如协议中的类型标识" aria-label="子设备类型" /></div>
        <div class="access-field"><span>对应设备模板</span><ui-select v-model="child.productId" filterable placeholder="选择已启用的子设备模板" aria-label="子设备产品"><ui-option v-for="p in childTemplates" :key="p.id" :value="p.id" :label="p.name" /></ui-select></div>
      </div>
      <p v-if="bindings[child.productId]" class="access-binding">已绑定协议：{{ bindings[child.productId].protocolId }} · {{ bindings[child.productId].version }}</p>
      <p v-else-if="child.productId" class="access-binding binding-error">{{ errors[child.productId] || '正在读取模板绑定协议…' }}</p>
      <p v-else class="access-binding">选择模板后显示其绑定协议。</p>
    </div>
    <p v-if="!childTemplates.length" class="access-template-help">当前没有其他已启用的设备模板，请先在设备模板中创建并绑定子设备协议。</p>
    <ui-button class="access-add-button" :disabled="(profile.childProducts?.length||0)>=64" @click="addChild">添加子设备产品</ui-button>
  </section>
</div>
</template>
<style scoped>
.protocol-access-settings{display:grid;gap:14px;padding:14px 0 2px}
.access-option-section{min-width:0;padding:16px;border:1px solid var(--border);border-radius:10px;background:var(--surface)}
.access-section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:14px}
.access-section-heading h4{margin:0;color:var(--text);font-size:14px}
.access-section-heading p{margin:5px 0 0;color:var(--text);font-size:12px;line-height:1.6}
.access-section-heading>span{flex:none;padding:3px 8px;border-radius:999px;color:var(--text);background:var(--surface-muted);font-size:11px;white-space:nowrap}
.access-empty{padding:15px;border:1px dashed var(--info-border);border-radius:8px;color:var(--text);background:var(--surface);font-size:12px;line-height:1.6}
.access-config-card{padding:14px;border:1px solid var(--border);border-radius:9px;background:var(--surface)}
.access-config-card+.access-config-card{margin-top:10px}
.access-card-heading{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-bottom:12px}
.access-card-heading strong{color:var(--text);font-size:13px}
.access-field-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
.access-field{display:grid;min-width:0;gap:6px;color:var(--text);font-size:12px;font-weight:600}
.access-field :deep(.n-input),.access-field :deep(.n-select),.access-field :deep(.n-input-number){width:100%;min-width:0}
.access-binding,.access-template-help{margin:10px 0 0;color:var(--text);font-size:12px;line-height:1.6;overflow-wrap:anywhere}
.access-binding.binding-error{color:var(--danger)}
.access-add-button{margin-top:12px}
@media(max-width:600px){.protocol-access-settings{padding-top:10px}.access-option-section{padding:13px}.access-field-grid{grid-template-columns:1fr}.access-section-heading{flex-wrap:wrap}.access-config-card{padding:12px}}
</style>
