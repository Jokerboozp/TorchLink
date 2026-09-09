<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, formatTime, session, pretty } from '../api'
import { businessStatuses, label } from '../labels'
const props=defineProps({deviceId:String})
const focus=ref(props.deviceId), twin=ref(null), busy=ref(false), error=ref(''), target=ref(''), kind=ref('contains'), choices=ref([]), messages=ref([])
const kinds={contains:'包含',monitors:'监测',depends_on:'依赖'}
const canEdit=computed(()=>['operator','admin'].includes(session.role))
const marker='twin-'+crypto.randomUUID()
let revision=0
const name=id=>twin.value?.nodes.find(n=>n.id===id)?.name||id
const visibleNodes=computed(()=>{
 const nodes=twin.value?.nodes||[],root=nodes.find(n=>n.id===focus.value)
 return (root?[root,...nodes.filter(n=>n.id!==root.id)]:nodes).slice(0,16).map((node,i,all)=>{
  const angle=2*Math.PI*(i-1)/Math.max(all.length-1,1)
  return {...node,x:i===0?300:300+220*Math.cos(angle),y:i===0?185:185+140*Math.sin(angle)}
 })
})
const lines=computed(()=>{
 const positions=Object.fromEntries(visibleNodes.value.map(n=>[n.id,n]))
 return (twin.value?.relations||[]).filter(r=>positions[r.source]&&positions[r.target]).map(r=>{const a=positions[r.source],b=positions[r.target],length=Math.hypot(b.x-a.x,b.y-a.y)||1;return {...r,a,b,endX:b.x-(b.x-a.x)*29/length,endY:b.y-(b.y-a.y)*29/length}})
})
async function load(id=focus.value){
 const current=++revision;busy.value=true;error.value=''
 try{
  const [value,history]=await Promise.all([api(`/api/v1/device-twins/${encodeURIComponent(id)}`),api(`/api/v1/device-registry/${encodeURIComponent(id)}/history?kind=property&pageSize=10`)])
  if(current!==revision)return
  focus.value=id;twin.value=value;messages.value=history.items||[]
 }catch(e){if(current===revision)error.value=e.message}
 finally{if(current===revision)busy.value=false}
}
async function update(add=[],remove=[]){
 if(!twin.value)return
 busy.value=true;error.value=''
 try{await api('/api/v1/device-twin-topology',{method:'PATCH',body:JSON.stringify({expectedVersion:twin.value.version,add,remove})});target.value='';await load()}
 catch(e){error.value=e.message}
 finally{busy.value=false}
}
async function remove(row){try{await ElMessageBox.confirm(`解除“${name(row.source)} ${kinds[row.kind]} ${name(row.target)}”的关系？`,'解除关系')}catch{return};await update([],[row.id])}
onMounted(async()=>{
 await load()
 try{choices.value=(await api('/api/v1/device-registry?pageSize=100')).items.map(item=>item.device||item)}catch(e){error.value=e.message}
})
onBeforeUnmount(()=>{revision++})
</script>
<template>
 <section v-loading="busy" class="device-twin">
  <p>当前查看：{{name(focus)}}。拓扑关系不触发设备控制；连接、告警与属性继续来自原有状态和已解析报文。</p>
  <el-button @click="load()">刷新孪生</el-button><el-button v-if="focus!==props.deviceId" @click="load(props.deviceId)">返回当前设备</el-button>
  <el-alert v-if="error" :title="error" type="error" :closable="false"/>
  <template v-if="twin">
   <p>拓扑版本 {{twin.version}} · {{formatTime(twin.updatedAt)}}</p>
   <svg viewBox="0 0 600 370" aria-label="设备关系拓扑" class="twin-graph">
    <defs><marker :id="marker" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="var(--el-text-color-secondary)"/></marker></defs>
    <g v-for="edge in lines" :key="edge.id"><line :x1="edge.a.x" :y1="edge.a.y" :x2="edge.endX" :y2="edge.endY" :marker-end="`url(#${marker})`"/><title>{{name(edge.source)}} {{kinds[edge.kind]}} {{name(edge.target)}}</title></g>
    <g v-for="node in visibleNodes" :key="node.id" role="button" tabindex="0" :aria-label="`查看 ${node.name||node.id}`" @click="load(node.id)" @keydown.enter="load(node.id)"><circle :cx="node.x" :cy="node.y" :r="node.id===focus?24:18" :class="{alarm:node.businessStatus==='ALARM',selected:node.id===focus}"/><text :x="node.x" :y="node.y+38" text-anchor="middle">{{(node.name||node.id).slice(0,13)}}</text><title>{{node.name||node.id}} · {{label(businessStatuses,node.businessStatus)}}</title></g>
   </svg>
   <p v-if="twin.nodes.length>16||twin.truncated">图中最多展示 16 个节点，完整当前查询结果见下表；可点击设备继续查看邻近关系。</p>
   <el-table :data="twin.relations" empty-text="暂无关系"><el-table-column label="来源" min-width="130"><template #default="{row}"><el-button link @click="load(row.source)">{{name(row.source)}}</el-button></template></el-table-column><el-table-column label="关系" width="80"><template #default="{row}">{{kinds[row.kind]}}</template></el-table-column><el-table-column label="目标" min-width="130"><template #default="{row}"><el-button link @click="load(row.target)">{{name(row.target)}}</el-button></template></el-table-column><el-table-column v-if="canEdit" label="操作" width="90"><template #default="{row}"><el-button link type="danger" @click="remove(row)">解除</el-button></template></el-table-column></el-table>
   <el-form v-if="canEdit" label-position="top" class="twin-form"><el-form-item label="关系类型"><el-select v-model="kind"><el-option v-for="(text,value) in kinds" :key="value" :value="value" :label="text"/></el-select></el-form-item><el-form-item label="目标设备"><el-select v-model="target" filterable allow-create default-first-option placeholder="选择设备或填写准确设备 ID"><el-option v-for="device in choices.filter(d=>d.id!==focus)" :key="device.id" :value="device.id" :label="device.name||device.id"/></el-select></el-form-item><el-button :disabled="!target||busy" @click="update([{source:focus,target,kind}])">添加关系</el-button></el-form>
   <h4>当前设备影子</h4><pre>{{pretty({reported:twin.shadow.reported,desired:twin.shadow.desired,delta:twin.shadow.delta})}}</pre>
   <h4>最近属性上报</h4><el-table :data="messages" empty-text="暂无已解析属性报文"><el-table-column label="时间" min-width="160"><template #default="{row}">{{formatTime(row.timestamp)}}</template></el-table-column><el-table-column label="属性" min-width="220"><template #default="{row}">{{pretty(row.properties)}}</template></el-table-column></el-table>
  </template>
 </section>
</template>
<style scoped>
.twin-graph{width:100%;max-height:370px;background:var(--el-fill-color-light);border-radius:8px;margin-top:12px}.twin-graph line{stroke:var(--el-text-color-secondary);stroke-width:1.5}.twin-graph circle{fill:var(--el-color-primary-light-7);stroke:var(--el-color-primary);stroke-width:2}.twin-graph circle.selected{fill:var(--el-color-primary-light-5)}.twin-graph circle.alarm{fill:var(--el-color-danger-light-5);stroke:var(--el-color-danger)}.twin-graph text{font-size:13px;fill:var(--el-text-color-primary)}.twin-graph [role=button]{cursor:pointer}.twin-form{margin-top:16px;display:grid;grid-template-columns:1fr 2fr;gap:10px}.el-alert{margin-top:10px}p{color:var(--el-text-color-secondary)}pre{white-space:pre-wrap;overflow-wrap:anywhere}
</style>
