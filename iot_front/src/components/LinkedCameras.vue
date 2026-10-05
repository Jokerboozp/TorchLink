<script setup>
// 设备详情、告警详情共用的关联摄像头列表：资料定位始终显示，直播入口按模块状态和权限显示。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Video } from '@lucide/vue'
import { api } from '../api'
import { cameraLocation, liveState, liveUsable, loadLiveStatus } from '../liveVideo'
import LivePlayerDialog from './LivePlayerDialog.vue'

const props = defineProps({
  deviceId: { type: String, default: '' },
  cameras: { type: Array, default: null }
})
const items = ref([])
const loading = ref(false)
const error = ref('')
const playerVisible = ref(false)
const playerCamera = ref(null)
let version = 0

const liveOn = computed(() => liveUsable())
const liveHint = computed(() => {
  const state = liveState.status?.state
  if (!state || state === 'not_deployed') return ''
  if (!liveState.canWatch) return '无观看直播权限'
  return { disabled: '直播功能未启用', degraded: '媒体服务暂不可用', misconfigured: '直播模块配置无效' }[state] || ''
})

async function load() {
  const current = ++version
  error.value = ''
  loading.value = true
  try {
    await loadLiveStatus()
    let rows = props.cameras ? props.cameras.map(item => ({ ...item })) : []
    if (!props.cameras && props.deviceId)
      rows = (await api(`/api/v1/video/devices/${encodeURIComponent(props.deviceId)}/cameras`)).items || []
    else if (liveUsable()) {
      // 告警摘要只含安全的摄像头元数据；逐个确认当前用户能否观看。
      rows = await Promise.all(
        rows.map(async row => {
          try {
            return { ...row, ...(await api(`/api/v1/video/cameras/${encodeURIComponent(row.cameraId)}`)) }
          } catch {
            return { ...row, liveAvailable: false }
          }
        })
      )
    }
    if (current === version) items.value = rows
  } catch (cause) {
    if (current === version) {
      error.value = cause?.message || '读取关联摄像头失败'
      items.value = props.cameras || []
    }
  } finally {
    if (current === version) loading.value = false
  }
}

function watchLive(row) {
  playerCamera.value = row
  playerVisible.value = true
}

// 以摄像头标识作为依赖，父组件重新渲染生成的新数组不会触发重复请求。
watch(() => `${props.deviceId}|${props.cameras ? props.cameras.map(item => item.cameraId).join(',') : '-'}`, load, { immediate: true })
onBeforeUnmount(() => {
  version++
})
</script>

<template>
  <div class="linked-cameras" v-loading="loading">
    <p v-if="error" class="linked-cameras__muted">{{ error }}</p>
    <p v-else-if="!items.length && !loading" class="linked-cameras__muted">未关联摄像头</p>
    <ul v-else class="linked-cameras__list">
      <li v-for="row in items" :key="row.cameraId">
        <Video class="linked-cameras__icon" aria-hidden="true" />
        <div class="linked-cameras__text">
          <strong>{{ row.cameraName || row.cameraId }}</strong>
          <small>{{ [row.brand, cameraLocation(row)].filter(Boolean).join(' · ') || row.cameraId }}</small>
        </div>
        <ui-button v-if="liveOn && row.liveAvailable" size="small" type="primary" @click="watchLive(row)">观看直播</ui-button>
        <small v-else-if="liveOn" class="linked-cameras__muted">未配置直播</small>
        <small v-else-if="liveHint" class="linked-cameras__muted">{{ liveHint }}</small>
      </li>
    </ul>
    <LivePlayerDialog v-model="playerVisible" :camera="playerCamera" />
  </div>
</template>

<style scoped>
.linked-cameras {
  position: relative;
  min-height: 24px;
}
.linked-cameras__list {
  display: grid;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}
.linked-cameras__list li {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.linked-cameras__icon {
  width: 16px;
  height: 16px;
  flex: none;
  color: var(--text-muted);
}
.linked-cameras__text {
  display: grid;
  flex: 1;
  min-width: 0;
}
.linked-cameras__text strong,
.linked-cameras__text small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.linked-cameras__text small,
.linked-cameras__muted {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
</style>
