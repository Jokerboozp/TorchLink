<script setup>
import { onMounted, reactive, ref } from 'vue'
import { api, apiAll, notifyError } from '../api'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback.js'
import ProtocolAccessSettings from './ProtocolAccessSettings.vue'

const props = defineProps({ product:Object, parent:Object, profile:Object, deviceId:String, existingIds:{type:Array,default:()=>[]} })
const emit = defineEmits(['saved','close'])
const busy = ref(false), bindingBusy = ref(false), products = ref([])
const productId = props.profile?.productId || props.product?.id || props.parent?.productId || ''
const form = reactive({
  id:'', productId, protocolId:'', protocolVersion:'', mode:'listener', network:'tcp',
  host:'0.0.0.0', publicHost:'', port:26875, timeoutMs:5000, autoRegister:false,
  enabled:true, connectionMode:'listen', deviceId:props.deviceId || '', queries:[], childProducts:[],
  unitId:1, intervalMs:10000, retries:0, wireFormat:'',
  ...(props.profile ? JSON.parse(JSON.stringify(props.profile)) : {})
})
const editing = Boolean(props.profile?.id)

onMounted(async () => {
  try {
    const result = await apiAll('/api/v1/products')
    products.value = result.items || []
    if (!editing && form.productId) {
      bindingBusy.value = true
      const binding = await api(`/api/v2/products/${encodeURIComponent(form.productId)}/protocol-binding`)
      form.protocolId = binding.protocolId || ''
      form.protocolVersion = binding.version || ''
      const transport = props.product?.transport || products.value.find(p => p.id === form.productId)?.transport || ''
      if (transport.toUpperCase().includes('MODBUS')) { form.mode = 'poll'; form.network = 'tcp'; form.connectionMode = ''; form.host = ''; form.deviceId = props.parent?.id || props.deviceId || ''; form.wireFormat = transport.toUpperCase().includes('RTU') ? 'rtu_over_tcp' : '' }
      else if (transport.includes('UDP')) form.network = 'udp'
    }
  } catch (cause) { notifyError(cause) }
  finally { bindingBusy.value = false }
})

async function save() {
  if (busy.value || bindingBusy.value) return
  if (!can(editing ? 'PUT /api/v2/device-access-profiles/:id' : 'POST /api/v2/device-access-profiles')) return UiMessage.warning('当前账号没有保存平台连接配置的权限')
  if (!form.id.trim() || !form.productId || !form.protocolId || !form.protocolVersion) return UiMessage.warning('请填写连接标识并确认模板已有发布的协议版本')
  if (!editing && props.existingIds.includes(form.id.trim())) return UiMessage.warning('连接标识已存在，请选择其他名称')
  if (!form.host.trim() || !Number.isInteger(form.port) || form.port < 1 || form.port > 65535) return UiMessage.warning('请填写有效的地址和端口')
  if (form.mode === 'listener' && form.connectionMode !== 'dial' && (!form.publicHost.trim() || ['0.0.0.0','::','[::]'].includes(form.publicHost.trim()))) return UiMessage.warning('请填写现场设备可达的平台对外地址')
  if ((form.mode === 'poll' || form.connectionMode === 'dial') && !form.deviceId.trim()) return UiMessage.warning('主动连接需要指定已登记设备标识')
  busy.value = true
  try {
    const body = JSON.parse(JSON.stringify(form))
    const result = await api(editing ? `/api/v2/device-access-profiles/${encodeURIComponent(body.id)}` : '/api/v2/device-access-profiles', { method:editing ? 'PUT' : 'POST', body:JSON.stringify(body) })
    UiMessage.success('平台连接配置已保存')
    emit('saved', result.profile?.id || body.id)
  } catch (cause) { notifyError(cause) }
  finally { busy.value = false }
}
</script>

<template>
  <div class="profile-helper">
    <p>在这里补齐本次接入需要的连接。共享监听可继续用于同模板的其他设备。</p>
    <ui-form label-position="top" :disabled="busy || bindingBusy">
      <div class="profile-helper-grid">
        <ui-form-item label="连接标识" required><ui-input v-model="form.id" :disabled="editing" placeholder="例如 fire-tcp" /></ui-form-item>
        <ui-form-item label="关联模板"><ui-input :model-value="products.find(p=>p.id===form.productId)?.name || form.productId" disabled /></ui-form-item>
      </div>
      <p class="profile-helper-binding">模板当前协议：{{ form.protocolId || '等待读取' }}{{ form.protocolVersion ? ` @ ${form.protocolVersion}` : '' }}</p>
      <div class="profile-helper-grid">
        <ui-form-item label="连接方式"><ui-select v-model="form.mode" @change="value=>{form.connectionMode=value==='poll'?'':'listen';if(value==='poll')form.network='tcp'}"><ui-option value="listener" label="设备连接平台" /><ui-option value="poll" label="平台定时采集" /></ui-select></ui-form-item>
        <ui-form-item v-if="form.mode==='listener'" label="网络协议"><ui-select v-model="form.network" @change="value=>{if(value==='udp') form.connectionMode='listen'}"><ui-option value="tcp" label="TCP" /><ui-option value="udp" label="UDP" /></ui-select></ui-form-item>
        <ui-form-item v-if="form.mode==='listener' && form.network==='tcp'" label="连接方向"><ui-select v-model="form.connectionMode"><ui-option value="listen" label="设备连接平台" /><ui-option value="dial" label="平台连接设备" /></ui-select></ui-form-item>
        <ui-form-item v-if="form.mode==='poll' || form.connectionMode==='dial'" label="目标设备标识"><ui-input v-model="form.deviceId" placeholder="已登记设备编号" /></ui-form-item>
        <ui-form-item :label="form.mode==='poll' || form.connectionMode==='dial' ? '设备地址 / 主机名' : '本机监听地址'"><ui-input v-model="form.host" :placeholder="form.mode==='poll' || form.connectionMode==='dial' ? '设备可达地址' : '例如 0.0.0.0'" /></ui-form-item>
        <ui-form-item v-if="form.mode==='listener' && form.connectionMode!=='dial'" label="平台对外地址" required><ui-input v-model="form.publicHost" placeholder="现场设备可达域名或 IP" /></ui-form-item>
        <ui-form-item label="端口"><ui-input-number v-model="form.port" :min="1" :max="65535" /></ui-form-item>
        <ui-form-item v-if="form.mode==='poll'" label="站号"><ui-input-number v-model="form.unitId" :min="0" :max="255" /></ui-form-item>
        <ui-form-item v-if="form.mode==='poll'" label="采集周期（秒）"><ui-input-number :model-value="form.intervalMs/1000" :min="1" @update:model-value="value=>form.intervalMs=Math.round(Number(value)*1000)" /></ui-form-item>
      </div>
      <p v-if="form.mode==='listener' && form.connectionMode!=='dial'" class="profile-helper-binding">本机监听地址只供服务绑定；现场设备须填写平台对外地址和端口。</p>
      <ui-form-item label="操作超时（秒）"><ui-input-number :model-value="form.timeoutMs/1000" :min="0.001" :max="30" :step="0.5" @update:model-value="value=>form.timeoutMs=Math.round(Number(value)*1000)" /></ui-form-item>
      <ui-form-item v-if="form.mode==='listener'" label="自动登记新设备"><ui-switch v-model="form.autoRegister" /></ui-form-item>
      <ui-form-item label="启用连接"><ui-switch v-model="form.enabled" /></ui-form-item>
      <details v-if="form.mode==='listener'"><summary>定时读取与子设备映射</summary><ProtocolAccessSettings :profile="form" :can-poll="form.network==='tcp'" :products="products" :product-id="form.productId" /></details>
      <div class="profile-helper-actions"><ui-button @click="emit('close')">取消</ui-button><ui-button type="primary" :loading="busy || bindingBusy" @click="save">保存并返回向导</ui-button></div>
    </ui-form>
  </div>
</template>

<style scoped>
.profile-helper{padding:4px 20px 28px;max-width:760px;color:var(--foreground)}
.profile-helper>p,.profile-helper-binding{color:var(--muted-foreground);font-size:13px;line-height:1.6}
.profile-helper-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 16px}
.profile-helper-grid>*{min-width:0}
.profile-helper details{margin:12px 0;padding:14px;border:1px solid var(--border);border-radius:8px}
.profile-helper summary{cursor:pointer;color:var(--accent-foreground);font-weight:600}
.profile-helper-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:20px;padding-top:16px;border-top:1px solid var(--border)}
@media(max-width:640px){.profile-helper{padding-inline:12px}.profile-helper-grid{grid-template-columns:1fr}}
</style>
