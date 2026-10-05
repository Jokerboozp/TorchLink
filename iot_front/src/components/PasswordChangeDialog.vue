<script setup>
// 修改登录密码：首次登录（管理员设置或重置密码后）必须修改，也可从账户菜单主动修改。
// 成功后服务端结束其他会话并返回新的会话。
import { reactive, ref, watch } from 'vue'
import { api } from '../api'

const props = defineProps({
  modelValue: Boolean,
  required: Boolean,
  changeToken: { type: String, default: '' },
  currentPassword: { type: String, default: '' }
})
const emit = defineEmits(['update:modelValue', 'changed'])
const form = reactive({ current: '', next: '', confirm: '' })
const saving = ref(false),
  error = ref('')
watch(
  () => props.modelValue,
  open => {
    if (!open) return
    Object.assign(form, { current: props.currentPassword, next: '', confirm: '' })
    error.value = ''
  }
)

async function submit() {
  if (saving.value) return
  if (form.next !== form.confirm) {
    error.value = '两次输入的新密码不一致'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const headers = props.changeToken ? { Authorization: `Bearer ${props.changeToken}` } : {}
    const data = await api('/api/v1/auth/password', {
      method: 'POST',
      headers,
      body: JSON.stringify({ currentPassword: form.current, newPassword: form.next })
    })
    emit('changed', data)
    emit('update:modelValue', false)
  } catch (e) {
    error.value = e.message || '修改密码失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ui-dialog
    :model-value="modelValue"
    :title="required ? '首次登录请修改密码' : '修改密码'"
    width="min(460px,94vw)"
    :show-close="!required && !saving"
    :close-on-click-modal="false"
    :close-on-press-escape="!required"
    @update:model-value="v => emit('update:modelValue', v)"
  >
    <ui-alert v-if="required" type="info" :closable="false" title="管理员为你设置了初始密码，修改后才能继续使用平台。" class="pwd-gap" />
    <ui-alert v-if="error" type="error" :title="error" :closable="false" class="pwd-gap" />
    <ui-form label-position="top" :disabled="saving" @submit.prevent="submit">
      <ui-form-item v-if="!required" label="当前密码"
        ><ui-input v-model="form.current" type="password" show-password autocomplete="current-password"
      /></ui-form-item>
      <ui-form-item label="新密码"
        ><ui-input v-model="form.next" type="password" show-password autocomplete="new-password" placeholder="至少 10 位，最长 72 字节"
      /></ui-form-item>
      <ui-form-item label="确认新密码"
        ><ui-input v-model="form.confirm" type="password" show-password autocomplete="new-password" @keyup.enter="submit"
      /></ui-form-item>
    </ui-form>
    <template #footer
      ><ui-button v-if="!required" :disabled="saving" @click="emit('update:modelValue', false)">取消</ui-button
      ><ui-button type="primary" :loading="saving" @click="submit">修改密码</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.pwd-gap {
  margin-bottom: var(--space-3);
}
</style>
