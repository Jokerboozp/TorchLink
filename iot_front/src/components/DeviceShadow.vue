<script setup>
import { onMounted, onBeforeUnmount, watch, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, pretty, notifyError, formatTime } from '../api'
const props = defineProps({ deviceId:String })
const shadow = ref(null), history = ref([]), patch = ref('{}'), busy = ref(false)
const name = ref(''), names = ref([])
let request = 0
const base = (suffix='') => `/api/v1/device-registry/${encodeURIComponent(props.deviceId)}/shadow${suffix}${name.value ? `?name=${encodeURIComponent(name.value)}` : ''}`
async function load() {
  const current = ++request
  busy.value = true
  try { const [state, changes, list] = await Promise.all([api(base()), api(base('/history')), api(`/api/v1/device-registry/${encodeURIComponent(props.deviceId)}/shadows`)]); if(current!==request)return; shadow.value = state; history.value = changes.items || []; names.value=list.items||[] }
  catch(error) { if(current===request){shadow.value=null;history.value=[];notifyError(error)} }
  finally { if(current===request)busy.value = false }
}
async function select() { shadow.value=null;history.value=[];patch.value='{}';await load() }
async function create() {
  try { const result=await ElMessageBox.prompt('输入用途名称，保存期望属性或设备首次上报后才持久化。每台设备最多 16 个命名影子。','命名影子',{inputPattern:/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/,inputErrorMessage:'使用 1–64 位字母、数字、点、下划线或连字符'});name.value=result.value;await select() }
  catch(error){if(error!=='cancel'&&error!=='close')notifyError(error)}
}
async function save() {
  if (!shadow.value) return
  const targetDevice=props.deviceId, targetName=name.value, targetURL=base(), desiredVersion=shadow.value.desiredVersion
  busy.value = true
  try {
    const desired = JSON.parse(patch.value)
    if (!desired || Array.isArray(desired) || typeof desired !== 'object') throw new Error('请填写 JSON 对象')
    await ElMessageBox.confirm('确认修改该设备的期望状态？兼容设备读取后可能执行调整，只有实际上报一致才表示状态已达成。','确认期望状态')
    if(props.deviceId!==targetDevice || name.value!==targetName)throw new Error('设备或影子已切换，请重新核对期望状态')
    const updated=await api(targetURL, {method:'PATCH', body:JSON.stringify({expectedDesiredVersion:desiredVersion, desired, confirmed:true})})
    if(props.deviceId!==targetDevice || name.value!==targetName)return
    shadow.value = updated
    patch.value = '{}'
    ElMessage.success('期望状态已保存，等待设备读取与上报')
    await load()
  } catch(error) { if (error !== 'cancel' && error !== 'close') notifyError(error) }
  finally { busy.value = false }
}
onMounted(load)
watch(()=>props.deviceId,()=>{name.value='';names.value=[];select()})
onBeforeUnmount(()=>{request++})
</script>
<template>
  <section>
    <p>已上报状态来自成功解析的属性报文；期望状态须由兼容设备读取并处理。保存不代表已经执行。</p>
    <el-form label-position="top"><el-form-item label="影子名称"><el-select v-model="name" :disabled="busy" @change="select"><el-option value="" label="默认影子"/><el-option v-for="item in [...new Set([...names,...(name?[name]:[])])]" :key="item" :value="item" :label="item"/></el-select><el-button :disabled="busy || names.length>=16" @click="create">命名影子</el-button></el-form-item></el-form>
    <el-button :loading="busy" @click="load">刷新影子</el-button>
    <template v-if="shadow">
      <el-alert v-if="shadow.lastError" :title="shadow.lastError" :description="`影子更新失败，完整数据仍可通过原文和标准消息查看。消息：${shadow.errorMessageId}`" type="error" :closable="false" />
      <p>期望版本 {{shadow.desiredVersion}} · 状态版本 {{shadow.version}} · 更新时间 {{formatTime(shadow.updatedAt)}}</p>
      <h4>已上报</h4><pre>{{pretty(shadow.reported)}}</pre>
      <h4>期望状态</h4><pre>{{pretty(shadow.desired)}}</pre>
      <h4>尚未一致的属性</h4><pre>{{pretty(shadow.delta)}}</pre>
      <el-form label-position="top"><el-form-item label="修改期望属性（JSON）"><el-input v-model="patch" type="textarea" :rows="4" placeholder='{"targetTemperature":24}' /></el-form-item></el-form>
      <p>仅产品物模型中标记 writable 的属性可设置。null 清除对应期望属性；版本冲突时先刷新再核对修改。</p>
      <el-button :loading="busy" @click="save">保存期望状态</el-button>
      <h4>最近期望状态变更</h4><el-table :data="history" empty-text="暂无变更"><el-table-column prop="version" label="版本" width="80"/><el-table-column prop="actor" label="操作人"/><el-table-column label="时间"><template #default="{row}">{{formatTime(row.timestamp)}}</template></el-table-column><el-table-column label="期望状态"><template #default="{row}">{{pretty(row.desired)}}</template></el-table-column></el-table>
    </template>
  </section>
</template>
<style scoped>pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--el-fill-color-light);padding:12px}p{color:var(--el-text-color-secondary)}</style>
