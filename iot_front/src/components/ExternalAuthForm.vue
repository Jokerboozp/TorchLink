<script setup>
import { computed } from 'vue'
const props = defineProps({ modelValue: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue'])
const auth = computed(() => props.modelValue)
function set(key, value) {
  emit('update:modelValue', { ...auth.value, [key]: value })
}
const types = {
  none: '默认认证',
  bearer: 'Bearer Token',
  api_key: 'API Key',
  basic: '用户名与密码',
  hmac: 'HMAC 签名',
  token: '登录获取 Token（拉取）'
}
</script>
<template>
  <div class="form-grid">
    <ui-form-item label="认证方式"
      ><ui-select :model-value="auth.type" @update:model-value="set('type', $event)"
        ><ui-option v-for="(label, value) in types" :key="value" :label="label" :value="value" /></ui-select
    ></ui-form-item>
    <ui-form-item v-if="auth.type === 'basic'" label="认证用户名"
      ><ui-input :model-value="auth.username" autocomplete="off" @update:model-value="set('username', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type !== 'none'" :label="auth.secretSet ? '认证密钥（已保存，留空沿用）' : '认证密钥'"
      ><ui-input :model-value="auth.secret" type="password" autocomplete="new-password" @update:model-value="set('secret', $event)"
    /></ui-form-item>
    <ui-form-item v-if="['api_key', 'hmac', 'token'].includes(auth.type)" label="认证请求头"
      ><ui-input :model-value="auth.header" placeholder="例如 X-API-Key" @update:model-value="set('header', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type === 'api_key'" label="或使用查询参数"
      ><ui-input :model-value="auth.query" placeholder="例如 api_key（与请求头二选一）" @update:model-value="set('query', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type === 'hmac'" label="时间戳请求头"
      ><ui-input :model-value="auth.timestampHeader" placeholder="X-Timestamp" @update:model-value="set('timestampHeader', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type === 'token'" label="Token 登录地址"
      ><ui-input :model-value="auth.tokenUrl" placeholder="https://…" @update:model-value="set('tokenUrl', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type === 'token'" label="Token 取值路径"
      ><ui-input :model-value="auth.tokenPath" placeholder="data.token" @update:model-value="set('tokenPath', $event)"
    /></ui-form-item>
    <ui-form-item v-if="auth.type === 'token'" label="有效期取值路径"
      ><ui-input :model-value="auth.tokenExpiresPath" placeholder="expires_in" @update:model-value="set('tokenExpiresPath', $event)"
    /></ui-form-item>
  </div>
  <p v-if="auth.type === 'none'" class="auth-hint">拉取不附加认证；推送使用接口单独生成的接收密钥。</p>
  <ui-checkbox v-if="auth.secretSet" :model-value="auth.clearSecret" @update:model-value="set('clearSecret', $event)"
    >清除已保存的认证密钥</ui-checkbox
  >
</template>
<style scoped>
.auth-hint {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.7;
  margin: 8px 0 16px;
}
</style>
