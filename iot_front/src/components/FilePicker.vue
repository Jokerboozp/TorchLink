<script setup>
import { ref } from 'vue'
defineProps({ accept: { type:String, default:'' }, disabled: Boolean })
const emit = defineEmits(['change'])
const filename = ref('')
function choose(event) {
  filename.value = event.target.files?.[0]?.name || ''
  emit('change', event)
}
</script>

<template>
  <div class="file-picker" :class="{ 'is-disabled': disabled }">
    <label class="file-picker-button"><span>选择文件</span><input type="file" :accept="accept" :disabled="disabled" aria-label="选择文件" @change="choose" /></label>
    <span class="file-picker-name">{{ filename || '尚未选择文件' }}</span>
  </div>
</template>

<style scoped>
.file-picker { display: flex; align-items: center; gap: 10px; max-width: 100%; min-width: 0; }
.file-picker-button { position: relative; display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; flex: none; border: 1px solid #cbd5e1; border-radius: 4px; color: #334155; background: #fff; font-size: 12px; }
.file-picker-button:focus-within { outline: 2px solid var(--primary); outline-offset: 2px; }
.file-picker-button input { position: absolute; inset: 0; width: 100%; height: 100%; padding: 0; opacity: 0; cursor: pointer; }
.file-picker-name { color: #64748b; font-size: 12px; overflow-wrap: anywhere; }
.is-disabled { opacity: .55; }.is-disabled input { cursor: not-allowed; }
</style>
