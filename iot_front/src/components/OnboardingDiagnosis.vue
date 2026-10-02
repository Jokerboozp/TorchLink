<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { CheckCircle2, CircleDashed, XCircle } from '@lucide/vue'
import { api, formatTime, session } from '../api'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback'
import { diagnosisTagTypes } from '../onboardingPlan'
const props = defineProps({ status: Object, error: { type: String, default: '' }, refreshing: Boolean, updatedAt: Number, verification:Object, verificationBusy:Boolean, canVerify:Boolean })
const emit = defineEmits(['refresh', 'raw', 'verify', 'navigate'])
const editingConnection = ref(false), savingConnection = ref(false), connectionError = ref('')
const connectionForm = reactive({host:'',port:0,unitId:1,timeoutMs:3000})
const identityToken = session.token
let generation = 0, active = true
const ownConnection = computed(() => { const profile=props.status?.profile; return Boolean(!props.status?.parent && profile?.deviceId===props.status?.device?.id && (profile?.mode==='poll' || profile?.connectionMode==='dial')) })
const canCorrect = computed(() => ownConnection.value && ['admin','operator'].includes(session.role) && can('menu:profiles') && can('PUT /api/v2/device-access-profiles/:id'))
function editConnection() { if(!canCorrect.value)return; const profile=props.status.profile; Object.assign(connectionForm,{host:profile.host,port:profile.port,unitId:profile.unitId ?? 1,timeoutMs:profile.timeoutMs || 3000}); connectionError.value=''; editingConnection.value=true }
async function saveConnection() {
  if(savingConnection.value || !canCorrect.value)return
  if(!connectionForm.host.trim() || !Number.isInteger(connectionForm.port) || connectionForm.port<1 || connectionForm.port>65535) {connectionError.value='请填写设备地址及 1 至 65535 之间的端口';return}
  const current=++generation, profile=props.status.profile
  savingConnection.value=true;connectionError.value=''
  try {
    await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}`,{method:'PUT',body:JSON.stringify({...profile,...connectionForm,host:connectionForm.host.trim()})})
    if(!active || current!==generation || session.token!==identityToken)return
    editingConnection.value=false;UiMessage.success('连接参数已保存，请等待运行状态和真实上报确认');emit('refresh')
  } catch(cause) {if(active && current===generation && session.token===identityToken)connectionError.value=cause.message}
  finally {if(active && current===generation)savingConnection.value=false}
}
function debugProtocol(){emit('navigate','protocols',{preview:true,productId:props.status?.device?.productId,deviceId:props.status?.device?.id,rawMessageId:props.status?.ingest?.rawMessageId,protocolId:props.status?.protocolId,version:props.status?.protocolVersion})}
watch(()=>`${props.status?.device?.id || ''}/${props.status?.profile?.id || ''}`,()=>{generation++;editingConnection.value=false;savingConnection.value=false;connectionError.value=''})
onBeforeUnmount(()=>{active=false;generation++})
const icons = { passed: CheckCircle2, waiting: CircleDashed, failed: XCircle }
const states = { passed: '通过', waiting: '等待', failed: '未通过' }
const values = computed(() => Object.entries(props.status?.ingest?.standardMessage?.properties || {}).map(([id,value]) => { const field=props.status?.product?.thingModel?.properties?.find(item=>item.identifier===id); return {id,name:field?.name || id,unit:field?.unit || '',value:typeof value==='object'?JSON.stringify(value):String(value)} }))
</script>
<template>
  <section class="onboarding-diagnosis" aria-live="polite">
    <div class="diagnosis-heading"><h3>真实设备验证</h3><span v-if="updatedAt">{{ formatTime(updatedAt) }}</span><ui-button size="small" :loading="refreshing" @click="emit('refresh')">刷新结果</ui-button></div>
    <ui-alert v-if="error" :title="error" type="warning" :closable="false" show-icon />
    <template v-if="status?.diagnosis">
      <ui-tag :type="diagnosisTagTypes[status.diagnosis.tone]">{{ status.diagnosis.title }}</ui-tag>
      <p>{{ status.diagnosis.nextAction }}</p>
      <small v-if="status.diagnosis.previousParsedAt">上次成功解析：{{ formatTime(status.diagnosis.previousParsedAt) }}</small>
      <ol><li v-for="check in status.diagnosis.checks" :key="check.key" :class="`is-${check.state}`"><component :is="icons[check.state] || CircleDashed" :aria-label="states[check.state] || check.state" /><div><strong>{{ check.label }}</strong><p>{{ check.detail }}{{ check.at ? ` · ${formatTime(check.at)}` : '' }}</p></div></li></ol>
    </template>
    <p v-else-if="!error">等待设备连接与真实上报，保存设备不代表验证通过。</p>
    <details v-if="status?.profile?.lastError"><summary>连接错误</summary><pre>{{ status.profile.lastError }}</pre></details>
    <details v-if="status?.ingest?.parseError"><summary>解析错误</summary><pre>{{ status.ingest.parseError }}</pre></details>
    <div class="diagnosis-actions">
      <ui-button v-if="canCorrect" size="small" :disabled="savingConnection" @click="editConnection">修改本设备连接参数</ui-button>
      <ui-button v-else-if="status?.parent" v-permission="'menu:devices'" size="small" @click="emit('navigate','devices',{deviceId:status.parent.id})">查看主设备连接</ui-button>
      <ui-button v-else-if="status?.profile?.mode==='listener' && status.profile.connectionMode!=='dial' && !status.profile.deviceId" v-permission="'menu:products'" size="small" @click="emit('navigate','products',{productId:status.profile.productId || status.device?.productId,tab:'access'})">打开模板公共连接</ui-button>
      <ui-button v-if="status?.ingest?.parseError && status?.protocolId && status?.protocolVersion && can('POST /api/v2/protocols/:id/releases/:version/preview')" v-permission="'menu:protocols'" size="small" @click="debugProtocol">用当前原文调试协议版本</ui-button>
    </div>
    <section v-if="editingConnection" class="connection-correction"><h4>本设备连接参数</h4><ui-form label-position="top"><ui-form-item label="设备地址"><ui-input v-model="connectionForm.host"/></ui-form-item><ui-form-item label="设备端口"><ui-input-number v-model="connectionForm.port" :min="1" :max="65535"/></ui-form-item><ui-form-item v-if="status.profile.mode==='poll'" label="站号"><ui-input-number v-model="connectionForm.unitId" :min="0" :max="255"/></ui-form-item><ui-form-item label="超时（毫秒）"><ui-input-number v-model="connectionForm.timeoutMs" :min="100" :max="60000"/></ui-form-item></ui-form><ui-alert v-if="connectionError" :title="connectionError" type="error" :closable="false"/><div class="diagnosis-actions"><ui-button :disabled="savingConnection" @click="editingConnection=false">取消</ui-button><ui-button type="primary" :loading="savingConnection" @click="saveConnection">保存连接参数</ui-button></div></section>
    <p v-if="status?.ingest?.simulationCount">另有 {{ status.ingest.simulationCount }} 条测试或管理接口数据，不计入本次验证。</p>
    <dl v-if="values.length"><div v-for="item in values" :key="item.id"><dt>{{ item.name }}</dt><dd>{{ item.value }}{{ item.unit ? ` ${item.unit}` : '' }}</dd></div></dl>
    <section v-if="verification" class="verified-evidence"><strong>{{ verification.status==='VERIFIED' ? '验收通过' : '验收尚未通过' }}</strong><ul><li v-for="check in verification.checks || []" :key="check.key">{{ check.label }}：{{ check.detail || check.state }}</li></ul><small v-if="verification.verifiedAt">已记录 {{ formatTime(verification.verifiedAt) }}</small></section>
    <ui-button v-if="canVerify" size="small" :loading="verificationBusy" @click="emit('verify')">检查并保存验收结果</ui-button>
    <ui-button v-if="status?.ingest?.rawMessageId" v-permission="'menu:raw'" size="small" @click="emit('raw')">查看本次原始报文</ui-button>
  </section>
</template>
<style scoped>
.diagnosis-actions { display:flex;flex-wrap:wrap;gap:var(--space-2);margin:var(--space-3) 0; }.connection-correction { padding:var(--space-3);border:1px solid var(--border);border-radius:var(--radius-md); }.onboarding-diagnosis { padding:var(--space-4); border:1px solid var(--border); border-radius:var(--radius-lg); }
.diagnosis-heading { display:flex; flex-wrap:wrap; align-items:center; gap:var(--space-3); margin-bottom:var(--space-3); }.diagnosis-heading h3 { margin:0; flex:1; }.diagnosis-heading span,li p { color:var(--text-muted); font-size:var(--font-size-xs); }
ol { display:grid; gap:var(--space-3); list-style:none; padding:0; }li { display:flex; align-items:flex-start; gap:var(--space-2); }li svg { width:18px; height:18px; flex:none; }li p { margin:2px 0 0; }.is-passed svg { color:var(--success-text); }.is-failed svg { color:var(--danger-text); }dl>div { display:flex; gap:var(--space-3); }dd { margin:0; }pre { white-space:pre-wrap; overflow-wrap:anywhere; }
</style>
