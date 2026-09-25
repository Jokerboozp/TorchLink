<script setup>
import { computed, ref } from 'vue'
import { deviceRoles } from '../labels'

const props = defineProps({
  scope: { type: String, default: 'none' },
  deviceIds: { type: Array, default: () => [] },
  devices: { type: Array, default: () => [] },
  loading: Boolean,
  error: { type: String, default: '' },
  disabled: Boolean,
  canViewDevices: Boolean
})
const emit = defineEmits(['update:scope', 'update:deviceIds', 'retry', 'enable-device-menu'])
const query = ref('')
const selectedOnly = ref(false)
const selected = computed(() => new Set(props.deviceIds))
const options = computed(() => {
  const devices = props.devices.map(device => ({ ...device, missing: false }))
  const available = new Set(devices.map(device => device.id))
  for (const id of selected.value) {
    if (!available.has(id)) devices.push({ id, name: '设备已删除或不可用', missing: true })
  }
  return devices
})
const filtered = computed(() => {
  const text = query.value.trim().toLowerCase()
  return options.value.filter(device => (!selectedOnly.value || selected.value.has(device.id)) &&
    (!text || `${device.name || ''} ${device.id}`.toLowerCase().includes(text)))
})
function toggle(id, checked) {
  const next = new Set(props.deviceIds)
  if (checked) next.add(id)
  else next.delete(id)
  emit('update:deviceIds', [...next])
}
</script>

<template>
  <section class="device-scope-picker" aria-label="设备授权设置">
    <ui-radio-group :model-value="scope" :disabled="disabled" class="device-scope-options segmented-choice-group" aria-label="设备访问范围" @update:model-value="emit('update:scope', $event)">
      <ui-radio-button value="none">无设备</ui-radio-button>
      <ui-radio-button value="selected">指定设备</ui-radio-button>
      <ui-radio-button value="all">当前租户全部设备</ui-radio-button>
    </ui-radio-group>
    <p v-if="scope === 'none'" class="scope-note">此用户不能查看任何设备，也不会收到设备告警。</p>
    <p v-else-if="scope === 'all'" class="scope-note">允许查看当前租户的全部设备，包含以后新增的设备。</p>
    <template v-else>
      <div class="device-scope-search">
        <ui-input v-model="query" clearable :disabled="disabled" aria-label="搜索授权设备" placeholder="搜索设备名称或编号" />
        <ui-checkbox v-model="selectedOnly" :disabled="disabled">仅看已选</ui-checkbox>
      </div>
      <div class="device-scope-summary"><strong>已选 {{ selected.size }} 台</strong><span>勾选允许查看，取消勾选即撤销该设备授权。</span></div>
      <div v-if="loading" class="device-scope-empty" role="status">正在加载设备…</div>
      <div v-else-if="error" class="device-scope-empty" role="alert"><p>{{ error }}</p><ui-button :disabled="disabled" @click="emit('retry')">重新加载</ui-button></div>
      <ul v-else-if="filtered.length" class="device-scope-list" aria-label="可授权设备">
        <li v-for="device in filtered" :key="device.id">
          <ui-checkbox :model-value="selected.has(device.id)" :disabled="disabled" @change="toggle(device.id, $event)">
            <span class="device-scope-name">{{ device.name || device.id }}</span>
            <span class="device-scope-detail">{{ device.id }}<span v-if="device.deviceRole"> · {{ deviceRoles[device.deviceRole] || device.deviceRole }}</span><span v-if="device.missing"> · 请取消勾选</span></span>
          </ui-checkbox>
        </li>
      </ul>
      <p v-else class="device-scope-empty">{{ options.length ? '没有符合条件的设备' : '当前租户暂无设备，请先在设备管理中添加设备。' }}</p>
      <p class="scope-note">未勾选的设备不可见；主设备与子设备需分别勾选。未选择任何设备时，与“无设备”相同。</p>
    </template>
    <div v-if="scope !== 'none' && !canViewDevices" class="device-scope-permission" role="status">
      <p>该用户尚未获得设备管理查看权限，仅保存设备范围还不能查看设备。</p>
      <ui-button :disabled="disabled" @click="emit('enable-device-menu')">开通设备管理查看权限</ui-button>
    </div>
    <p v-else-if="scope !== 'none'" class="scope-note">已具备设备管理查看权限。告警、原始报文和智能助手还需各自的功能权限，数据均受此设备范围限制。</p>
  </section>
</template>

<style scoped>
.device-scope-picker { display: grid; gap: 12px; min-width: 0; }
.device-scope-options { display: flex; flex-wrap: wrap; gap: 8px; }
.device-scope-search { display: flex; align-items: center; gap: 12px; }
.device-scope-search > .ui-input { flex: 1; min-width: 0; }
.device-scope-search > .ui-checkbox { flex: none; }
.device-scope-summary { display: flex; flex-wrap: wrap; gap: 6px 12px; font-size: 12px; color: var(--text-muted); }
.device-scope-summary strong { color: var(--text); }
.device-scope-list { list-style: none; margin: 0; padding: 0; max-height: 300px; overflow-y: auto; overscroll-behavior: contain; border: 1px solid var(--border); border-radius: var(--radius-md); }
.device-scope-list li + li { border-top: 1px solid var(--border); }
.device-scope-list .ui-checkbox { display: flex; width: 100%; padding: 10px 12px; }
.device-scope-list :deep(.n-checkbox__label) { flex: 1; min-width: 0; }
.device-scope-name { display: block; color: var(--text); font-size: 13px; overflow-wrap: anywhere; }
.device-scope-detail { display: block; margin-top: 3px; color: var(--text-muted); font-size: 12px; overflow-wrap: anywhere; }
.device-scope-empty { margin: 0; padding: 20px 12px; text-align: center; color: var(--text-muted); background: var(--surface-muted); border-radius: var(--radius-md); font-size: 13px; }
.device-scope-permission { padding: 12px; background: var(--surface-muted); border-radius: var(--radius-md); }
.device-scope-permission p, .scope-note { margin: 0; color: var(--text-muted); font-size: 12px; line-height: 1.6; }
.device-scope-permission p { margin-bottom: 8px; }
@media (max-width: 640px) { .device-scope-options { flex-direction: column; align-items: stretch; }.device-scope-search { flex-wrap: wrap; }.device-scope-search > .ui-input { flex-basis: 100%; } }
</style>
