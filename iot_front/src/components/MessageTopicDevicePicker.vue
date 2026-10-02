<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, session } from '../api'
import { permissionState } from '../permissions'

const props = defineProps({ modelValue:{ type:Array, default:() => [] }, disabled:Boolean })
const emit = defineEmits(['update:modelValue'])
const query = ref(''), rows = ref([]), page = ref(1), total = ref(0), loading = ref(false), error = ref('')
const selected = computed(() => new Set(props.modelValue))
let version = 0, timer = null, controller = null, disposed = false
const identity = () => [session.token, session.tenant, session.user, permissionState.accessVersion].join('\n')
function invalidate() { version++; controller?.abort(); clearTimeout(timer) }
async function load() {
  invalidate()
  const currentVersion = version, currentIdentity = identity()
  controller = new AbortController()
  loading.value = true; error.value = ''; rows.value = []
  try {
    const result = await api(`/api/v1/device-registry?page=${page.value}&pageSize=50&q=${encodeURIComponent(query.value.trim())}`, { signal:controller.signal })
    if (disposed || version !== currentVersion || currentIdentity !== identity()) return
    rows.value = (result.items || []).map(item => item.device || item)
    total.value = Number(result.total || 0)
  } catch (cause) {
    if (!disposed && version === currentVersion && currentIdentity === identity() && cause.name !== 'AbortError') error.value = cause.message || '设备读取失败'
  } finally {
    if (!disposed && version === currentVersion && currentIdentity === identity()) loading.value = false
  }
}
function search() { invalidate(); rows.value = []; page.value = 1; timer = setTimeout(load, 250) }
function setPage(value) { page.value = value; load() }
function toggle(id, checked) {
  if (props.disabled) return
  const next = new Set(props.modelValue)
  if (checked) next.add(id); else next.delete(id)
  emit('update:modelValue', [...next])
}
watch(() => permissionState.accessVersion, () => { invalidate(); rows.value = []; total.value = 0 })
onMounted(load)
onBeforeUnmount(() => { disposed = true; invalidate() })
</script>

<template>
  <section class="topic-device-picker" aria-label="对接账号设备选择">
    <ui-input v-model="query" clearable :disabled="disabled" placeholder="搜索设备名称或编号" aria-label="搜索对接设备" @input="search" />
    <p>已选 {{ modelValue.length }} 台。最终分发范围始终受绑定用户当前设备权限限制，主设备与子设备分别授权。</p>
    <div v-if="modelValue.length" class="topic-device-selected"><ui-button v-for="id in modelValue" :key="id" size="small" :disabled="disabled" @click="toggle(id, false)">{{ id }} · 移除</ui-button></div>
    <div v-if="loading" class="topic-device-empty" role="status">正在读取设备…</div>
    <div v-else-if="error" class="topic-device-empty" role="alert">{{ error }} <ui-button size="small" @click="load">重试</ui-button></div>
    <ul v-else-if="rows.length" class="topic-device-list"><li v-for="device in rows" :key="device.id"><ui-checkbox :model-value="selected.has(device.id)" :disabled="disabled" @update:model-value="checked => toggle(device.id, checked)"><strong>{{ device.name || device.id }}</strong><small>{{ device.id }}</small></ui-checkbox></li></ul>
    <p v-else class="topic-device-empty">没有符合条件的设备</p>
    <ui-pagination v-if="total > 50" :current-page="page" :page-size="50" :total="total" layout="prev,pager,next" @update:current-page="setPage" />
  </section>
</template>

<style scoped>
.topic-device-picker { display:grid; gap:var(--space-2); min-width:0; }
.topic-device-picker p { margin:0; color:var(--text-muted); font-size:var(--font-size-sm); line-height:1.6; }
.topic-device-selected { display:flex; flex-wrap:wrap; gap:var(--space-2); max-height:100px; overflow-y:auto; }
.topic-device-selected :deep(.n-button__content) { max-width:260px; overflow:hidden; text-overflow:ellipsis; display:block; }
.topic-device-list { margin:0; padding:0; list-style:none; max-height:240px; overflow-y:auto; border:1px solid var(--border); border-radius:var(--radius-md); }
.topic-device-list li { padding:var(--space-2) var(--space-3); }
.topic-device-list li + li { border-top:1px solid var(--border); }
.topic-device-list small { display:block; color:var(--text-muted); overflow-wrap:anywhere; }
.topic-device-empty { padding:var(--space-3); text-align:center; color:var(--text-muted); }
</style>
