<script setup>
// 凭据字段：已保存的值从不回显，只显示是否已设置；编辑时选择保持原值、替换或清除。
import { computed } from 'vue'

const props = defineProps({ modelValue: { type: Object, default: () => ({}) }, allowClear: { type: Boolean, default: true }, placeholder: { type: String, default: '输入新值' }, label: { type: String, default: '凭据' } })
const emit = defineEmits(['update:modelValue'])
const secret = computed(() => props.modelValue || {})
const mode = computed(() => secret.value.mode || (secret.value.set ? 'keep' : 'replace'))
function update(patch) { emit('update:modelValue', { ...secret.value, ...patch }) }
</script>

<template>
  <div class="secret-field">
    <template v-if="secret.set">
      <ui-radio-group :model-value="mode" size="small" class="segmented-choice-group" :aria-label="`${label}处理方式`" @update:model-value="value => update({ mode: value, value: '' })">
        <ui-radio-button value="keep">保持原值</ui-radio-button>
        <ui-radio-button value="replace">替换</ui-radio-button>
        <ui-radio-button v-if="allowClear" value="clear">清除</ui-radio-button>
      </ui-radio-group>
      <span v-if="mode === 'keep'" class="secret-field__hint">已设置{{ secret.hint ? `（${secret.hint}）` : '' }}，不会回显</span>
      <span v-else-if="mode === 'clear'" class="secret-field__hint is-warning">保存后将删除已保存的{{ label }}</span>
    </template>
    <ui-input v-if="mode === 'replace'" :model-value="secret.value || ''" type="password" show-password autocomplete="new-password" :placeholder="placeholder" :aria-label="label" @update:model-value="value => update({ mode: 'replace', value })" />
  </div>
</template>

<style scoped>
.secret-field { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); min-width: 0; }
.secret-field .ui-input { flex: 1 1 200px; }
.secret-field__hint { color: var(--text-muted); font-size: var(--font-size-xs); }
.secret-field__hint.is-warning { color: var(--warning-text); }
</style>
