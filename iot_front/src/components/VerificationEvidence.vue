<script setup>
import { formatTime } from '../api'
defineProps({evidence:Object,title:{type:String,default:'验收证据'},trial:Boolean})
const emit=defineEmits(['navigate'])
</script>
<template>
  <details v-if="evidence" class="verification-evidence"><summary>{{ title }}</summary><p v-if="trial">这是候选配置的隔离试验证据；正式设备仍须按当前配置完成现场验收。</p><p>设备：{{ evidence.deviceId }} · {{ evidence.verifiedAt ? `验收于 ${formatTime(evidence.verifiedAt)}` : `检查于 ${formatTime(evidence.checkedAt)}` }} · {{ evidence.messageCount || 0 }} 条有效报文</p><div class="evidence-links"><ui-button v-permission="'menu:devices'" size="small" @click="emit('navigate','devices',{deviceId:evidence.deviceId})">查看验证设备</ui-button><ui-button v-for="(id,index) in evidence.rawMessageIds || []" :key="id" v-permission="'menu:raw'" size="small" @click="emit('navigate','raw',{deviceId:evidence.deviceId,rawMessageId:id})">原文 {{ index+1 }}</ui-button></div></details>
</template>
<style scoped>
.verification-evidence{margin:var(--space-3) 0;padding:var(--space-3);border:1px solid var(--border);border-radius:var(--radius-md)}summary{cursor:pointer;font-weight:var(--font-weight-semibold)}p{font-size:var(--font-size-sm);color:var(--text-muted)}.evidence-links{display:flex;flex-wrap:wrap;gap:var(--space-2)}
</style>
