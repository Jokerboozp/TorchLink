<script setup>
// 设备凭据：禁用或重新生成；新密钥只在生成后显示一次，旧凭据的消息服务撤销结果逐条列出。
import { pretty } from '../../api'

defineProps({
  // 设备连接详情。
  data: { type: Object, required: true },
  // 刚生成的凭据，仅本次显示。
  credential: { type: Object, default: null },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['disable', 'rotate'])
</script>

<template>
  <section class="connection-section device-credentials">
    <h3>设备凭据</h3>
    <p>重新生成后旧凭据立即停用，新密钥仅显示一次。</p>
    <div class="section-actions">
      <ui-button
        v-permission="'DELETE /api/v1/device-registry/:id/credentials'"
        :disabled="loading || actionBusy || !data.credentialEnabled"
        @click="emit('disable')"
        >禁用凭据</ui-button
      ><ui-button v-permission="'POST /api/v1/device-registry/:id/credentials'" :disabled="loading || actionBusy" @click="emit('rotate')"
        >重新生成凭据</ui-button
      >
    </div>
    <pre v-if="credential">仅本次显示，请妥善保存：{{ pretty(credential) }}</pre>
    <p v-for="revocation in data.revocations" :key="revocation.id">
      旧凭据消息服务撤销：{{ revocation.status === 'REVOKED' ? '已完成' : '待完成（平台已停用旧凭据）' }}
    </p>
  </section>
</template>

<style scoped src="./connection-section.css"></style>
