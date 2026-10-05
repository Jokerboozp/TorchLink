<script setup>
import { computed, onBeforeUnmount, reactive, watch } from 'vue'
import { api, apiBlob, formatTime, session } from '../api'
import { can, permissionState } from '../permissions'
import { alarmMediaItems, mediaDownloadName, mediaMIMEAllowed } from '../alarmMedia'
import StatusDot from './layout/StatusDot.vue'
const props = defineProps({ alarm: { type: Object, required: true } })
const emit = defineEmits(['refresh'])
const media = computed(() => alarmMediaItems(props.alarm))
const metadata = computed(() => props.alarm.details?.videoEvent?.raw || {})
const state = reactive({ snapshot: {}, clip: {}, retrying: false, retryError: '' })
const controllers = new Map()
const readPermission = 'GET /api/v1/alarms/:id/media/:kind'
const retryPermission = 'POST /api/v1/alarms/:id/media/retry'
let version = 0,
  poll = 0,
  disposed = false
const identity = () => [session.tenant, session.user, session.token, permissionState.accessVersion].join('|')
const statusName = value => ({ STORED: '已归档', FAILED: '转存失败', PENDING: '等待转存', RUNNING: '正在转存' })[value] || '等待转存'
function clear() {
  version++
  for (const controller of controllers.values()) controller.abort()
  controllers.clear()
  for (const kind of ['snapshot', 'clip']) {
    if (state[kind].url) URL.revokeObjectURL(state[kind].url)
    state[kind] = {}
  }
  state.retryError = ''
  state.retrying = false
}
watch(
  () =>
    [
      props.alarm.alarmId,
      props.alarm.details?.videoEvent?.eventId,
      props.alarm.details?.videoEvent?.snapshotUrl,
      props.alarm.details?.videoEvent?.videoClipUrl,
      identity(),
      can(readPermission)
    ].join('|'),
  clear
)
watch(
  () => media.value.some(item => !item.stored),
  active => {
    clearInterval(poll)
    poll = 0
    if (active)
      poll = setInterval(() => {
        if (!document.hidden) emit('refresh')
      }, 5000)
  },
  { immediate: true }
)
async function load(kind, download = false) {
  if (!can(readPermission) || state[kind].loading || !media.value.find(item => item.kind === kind)?.stored) return
  const currentVersion = version,
    owner = identity(),
    alarmId = props.alarm.alarmId
  const current = () =>
    !disposed && version === currentVersion && identity() === owner && props.alarm.alarmId === alarmId && can(readPermission)
  state[kind].error = ''
  try {
    if (!state[kind].url) {
      state[kind].loading = true
      const controller = new AbortController()
      controllers.set(kind, controller)
      const blob = await apiBlob(`/api/v1/alarms/${encodeURIComponent(alarmId)}/media/${kind}`, { signal: controller.signal })
      if (!current()) return
      if (!mediaMIMEAllowed(kind, blob.type)) throw new Error('附件格式暂不支持安全预览或下载')
      state[kind].url = URL.createObjectURL(blob)
      state[kind].mime = blob.type
    }
    if (!current()) return
    if (download) {
      const anchor = document.createElement('a')
      anchor.href = state[kind].url
      anchor.download = mediaDownloadName(alarmId, kind, state[kind].mime)
      anchor.click()
    } else state[kind].shown = true
  } catch (error) {
    if (current() && error.name !== 'AbortError') state[kind].error = error.message || '附件读取失败'
  } finally {
    if (current()) {
      state[kind].loading = false
      controllers.delete(kind)
    }
  }
}
async function retry() {
  if (state.retrying || !can(retryPermission)) return
  const currentVersion = version,
    owner = identity(),
    alarmId = props.alarm.alarmId
  state.retrying = true
  state.retryError = ''
  try {
    await api(`/api/v1/alarms/${encodeURIComponent(alarmId)}/media/retry`, { method: 'POST', body: '{}' })
    if (!disposed && version === currentVersion && owner === identity()) emit('refresh')
  } catch (error) {
    if (!disposed && version === currentVersion && owner === identity()) state.retryError = error.message || '重新转存失败'
  } finally {
    if (!disposed && version === currentVersion) state.retrying = false
  }
}
onBeforeUnmount(() => {
  disposed = true
  clearInterval(poll)
  clear()
})
</script>

<template>
  <section v-if="media.length" class="alarm-media" aria-label="告警截图与视频片段">
    <header>
      <strong>告警附件</strong
      ><ui-button
        v-if="media.some(item => !item.stored)"
        v-permission="retryPermission"
        size="small"
        :loading="state.retrying"
        @click="retry"
        >重新转存</ui-button
      >
    </header>
    <p v-if="media.some(item => !item.stored)" class="hint">
      附件在后台转存，失败后会自动重试，告警记录已保留。{{ metadata.mediaRetryAt ? `下次重试：${formatTime(metadata.mediaRetryAt)}` : '' }}
    </p>
    <p v-if="state.retryError" class="failure" role="alert">{{ state.retryError }}</p>
    <article v-for="item in media" :key="item.kind">
      <div class="media-heading">
        <b>{{ item.label }}</b
        ><StatusDot :label="statusName(item.status)" :tone="item.status === 'FAILED' ? 'danger' : item.stored ? 'success' : 'warning'" />
        <div class="media-actions">
          <ui-button
            v-permission="readPermission"
            size="small"
            :disabled="!item.stored"
            :loading="state[item.kind].loading"
            @click="load(item.kind)"
            >{{ item.kind === 'snapshot' ? '查看截图' : '查看片段' }}</ui-button
          ><ui-button
            v-permission="readPermission"
            size="small"
            :disabled="!item.stored || state[item.kind].loading"
            @click="load(item.kind, true)"
            >下载</ui-button
          >
        </div>
      </div>
      <p v-if="item.error" class="failure">{{ item.error }}</p>
      <p v-if="state[item.kind].error" class="failure" role="alert">{{ state[item.kind].error }}</p>
      <template v-if="state[item.kind].shown"
        ><img v-if="item.kind === 'snapshot'" :src="state.snapshot.url" alt="告警截图" /><video
          v-else
          :src="state.clip.url"
          controls
          playsinline
          preload="metadata"
        /><ui-button text size="small" @click="state[item.kind].shown = false">收起预览</ui-button></template
      >
    </article>
  </section>
</template>

<style scoped>
.alarm-media {
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 14px;
  margin-top: 16px;
}
.alarm-media header,
.media-heading {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.alarm-media header {
  justify-content: space-between;
}
.media-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}
.alarm-media article {
  border-top: 1px solid var(--border);
  padding-top: 12px;
  margin-top: 12px;
}
.alarm-media b,
.alarm-media strong {
  font-size: 13px;
}
.hint,
.failure {
  font-size: 12px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.hint {
  color: var(--text-muted);
}
.failure {
  color: var(--danger-text);
}
img,
video {
  display: block;
  max-width: 100%;
  max-height: 420px;
  object-fit: contain;
  margin: 12px auto;
  background: var(--surface-sunken);
}
@media (max-width: 540px) {
  .media-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>
