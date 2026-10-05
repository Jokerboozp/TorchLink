<script setup>
// 摄像头直播播放器：优先 WebRTC，失败后切换一次 HLS，不在两种协议间循环。
// 播放会话由服务端签发并按心跳续期；关闭、切换或卸载时立即释放。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Maximize, RefreshCw, Volume2, VolumeX } from '@lucide/vue'
import { ApiError, api, session } from '../api'
import { errorMessage } from '../presentation'
import { browserCaps, supportsWebRTC } from '../liveVideo'
import StatusDot from './layout/StatusDot.vue'

const props = defineProps({
  cameraId: { type: String, required: true },
  cameraName: { type: String, default: '' },
  location: { type: String, default: '' }
})
const emit = defineEmits(['ended'])

const container = ref(null)
const video = ref(null)
const phase = ref('idle') // idle | connecting | playing | reconnecting | blocked | error | ended
const message = ref('')
const protocol = ref('')
const stream = ref('')
const streams = ref([])
const grant = ref(null)
const muted = ref(true)

let generation = 0
let sessionId = ''
let heartbeatTimer = 0
let pc = null
let hls = null
let hlsTried = false
let reconnects = 0
let rendering = false
let watchdogTimer = 0

const phaseText = computed(
  () =>
    ({
      idle: '准备中',
      connecting: '连接中',
      playing: '播放中',
      reconnecting: '重新连接中',
      blocked: '等待播放',
      error: '播放失败',
      ended: '已结束'
    })[phase.value]
)
const phaseTone = computed(
  () => ({ playing: 'success', connecting: 'info', reconnecting: 'warning', blocked: 'info', error: 'danger' })[phase.value] || 'neutral'
)
const protocolText = computed(() => ({ webrtc: 'WebRTC', hls: 'HLS' })[protocol.value] || '')
const profileText = computed(() => (grant.value?.profile && grant.value.profile !== 'direct' ? grant.value.profileName : '原始码流'))

function authHeaders(extra = {}) {
  return { ...(session.token ? { Authorization: `Bearer ${session.token}` } : {}), ...extra }
}

function teardownMedia() {
  window.clearInterval(heartbeatTimer)
  heartbeatTimer = 0
  window.clearInterval(watchdogTimer)
  watchdogTimer = 0
  if (pc) {
    try {
      pc.ontrack = null
      pc.onconnectionstatechange = null
      pc.close()
    } catch {
      /* 已关闭 */
    }
    pc = null
  }
  if (hls) {
    try {
      hls.destroy()
    } catch {
      /* 已销毁 */
    }
    hls = null
  }
  const element = video.value
  if (element) {
    element.pause()
    element.srcObject = null
    element.removeAttribute('src')
    element.load()
  }
  rendering = false
}

// keepalive 让页面关闭或刷新时的释放请求也能发出；即使失败，服务端租约也会到期回收。
function releaseSession(id = sessionId) {
  if (!id) return
  if (id === sessionId) sessionId = ''
  fetch(`/api/v1/video/play-sessions/${encodeURIComponent(id)}`, { method: 'DELETE', keepalive: true, headers: authHeaders() }).catch(
    () => {}
  )
}

function fail(text) {
  teardownMedia()
  releaseSession()
  phase.value = 'error'
  message.value = text
}

function waitFirstFrame(element, gen, timeoutMs) {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      cleanup()
      reject(new Error('在限定时间内未收到画面'))
    }, timeoutMs)
    const check = () => {
      if (gen !== generation) {
        cleanup()
        reject(new Error('stale'))
        return
      }
      if (element.videoWidth > 0 && element.readyState >= 2) {
        cleanup()
        resolve()
      }
    }
    const cleanup = () => {
      window.clearTimeout(timer)
      element.removeEventListener('loadeddata', check)
      element.removeEventListener('playing', check)
      element.removeEventListener('resize', check)
    }
    element.addEventListener('loadeddata', check)
    element.addEventListener('playing', check)
    element.addEventListener('resize', check)
    check()
  })
}

// play() 在收到首帧前可能一直不返回，不能阻塞首帧超时与协议切换，因此不等待其结果。
function autoplay(element, gen) {
  element.muted = muted.value
  element.play().catch(() => {
    // 浏览器阻止自动播放时保留画面元素，等待用户点击。
    if (gen === generation) phase.value = 'blocked'
  })
}

async function playWebRTC(gen) {
  const element = video.value
  const peer = new RTCPeerConnection({ iceServers: [] })
  pc = peer
  peer.addTransceiver('video', { direction: 'recvonly' })
  peer.addTransceiver('audio', { direction: 'recvonly' })
  peer.ontrack = event => {
    if (gen !== generation) return
    const mediaStream = event.streams?.[0] || new MediaStream([event.track])
    if (element.srcObject !== mediaStream) element.srcObject = mediaStream
  }
  peer.onconnectionstatechange = () => {
    if (gen !== generation) return
    if (peer.connectionState === 'failed') void recover(gen, 'WebRTC 连接中断')
  }
  await peer.setLocalDescription(await peer.createOffer())
  await new Promise(resolve => {
    if (peer.iceGatheringState === 'complete') return resolve()
    const timer = window.setTimeout(resolve, 2000)
    peer.addEventListener('icegatheringstatechange', () => {
      if (peer.iceGatheringState === 'complete') {
        window.clearTimeout(timer)
        resolve()
      }
    })
  })
  if (gen !== generation) return
  const response = await fetch(grant.value.whepUrl, {
    method: 'POST',
    headers: authHeaders({ 'Content-Type': 'application/sdp' }),
    body: peer.localDescription.sdp
  })
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new ApiError(data.detail || 'WebRTC 协商失败', { ...data, status: response.status })
  }
  const answer = await response.text()
  if (gen !== generation) return
  await peer.setRemoteDescription({ type: 'answer', sdp: answer })
  autoplay(element, gen)
  await waitFirstFrame(element, gen, 10000)
}

async function playHLS(gen) {
  const element = video.value
  const url = grant.value.hlsUrl
  // 优先使用 hls.js（Chrome、Edge、Firefox），不支持 MSE 时才使用原生 HLS（iOS Safari）。
  const { default: Hls } = await import('hls.js/light')
  if (gen !== generation) return
  if (Hls.isSupported()) {
    hls = new Hls({
      liveSyncDurationCount: 2,
      manifestLoadingMaxRetry: 2,
      levelLoadingMaxRetry: 2,
      fragLoadingMaxRetry: 2,
      enableWorker: true
    })
    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (gen !== generation || !data?.fatal) return
      void recover(gen, data.response?.code === 403 ? '播放凭证已失效' : 'HLS 播放中断')
    })
    hls.loadSource(url)
    hls.attachMedia(element)
  } else if (element.canPlayType('application/vnd.apple.mpegurl')) {
    element.src = url
  } else {
    throw new Error('当前浏览器不支持 WebRTC 或 HLS 播放')
  }
  autoplay(element, gen)
  await waitFirstFrame(element, gen, 20000)
}

async function start(preferred = supportsWebRTC() && !hlsTried ? 'webrtc' : 'hls', variant = stream.value) {
  const gen = ++generation
  teardownMedia()
  releaseSession()
  phase.value = phase.value === 'reconnecting' ? 'reconnecting' : 'connecting'
  message.value = ''
  protocol.value = preferred
  try {
    const issued = await api(`/api/v1/video/cameras/${encodeURIComponent(props.cameraId)}/play-sessions`, {
      method: 'POST',
      body: JSON.stringify({ stream: variant, protocol: preferred, caps: browserCaps() })
    })
    if (gen !== generation) {
      releaseSession(issued.sessionId)
      return
    }
    grant.value = issued
    sessionId = issued.sessionId
    stream.value = issued.stream
    streams.value = issued.streams || []
    heartbeatTimer = window.setInterval(() => heartbeat(gen), Math.max(5, Number(issued.heartbeatSeconds || 15)) * 1000)
    if (preferred === 'webrtc') await playWebRTC(gen)
    else await playHLS(gen)
    if (gen !== generation) return
    rendering = true
    reconnects = 0
    if (phase.value !== 'blocked') phase.value = 'playing'
    startWatchdog(gen)
    void heartbeat(gen)
  } catch (error) {
    if (gen !== generation || error?.message === 'stale') return
    // 服务端拒绝（权限、配置、编码、上限）不属于协议问题，不再切换协议。
    if (error instanceof ApiError) return fail(errorMessage(error))
    if (preferred === 'webrtc' && !hlsTried) {
      hlsTried = true
      message.value = 'WebRTC 未能出画面，正在改用 HLS'
      return start('hls', variant)
    }
    fail(error?.message || '播放失败')
  }
}

async function heartbeat(gen) {
  if (gen !== generation || !sessionId) return
  try {
    const result = await api(`/api/v1/video/play-sessions/${encodeURIComponent(sessionId)}/heartbeat`, {
      method: 'POST',
      body: JSON.stringify({ rendering })
    })
    if (gen !== generation) return
    if (result.mediaState === 'restarted' || result.mediaState === 'reconnecting')
      void recover(gen, result.message || '媒体服务已恢复，正在重新连接')
  } catch (error) {
    if (gen !== generation) return
    const status = error?.status
    if (status === 410 || status === 403 || status === 409 || status === 404) {
      teardownMedia()
      sessionId = ''
      phase.value = 'ended'
      message.value = errorMessage(error)
      emit('ended')
    }
    // 网络抖动时保留播放，下一次心跳重试；超过租约后服务端会回收。
  }
}

// 画面冻结检测：连接仍在但 8 秒没有新帧时视为播放中断。WebRTC 冻结时切换到 HLS（只切换一次）。
function startWatchdog(gen) {
  const element = video.value
  let lastFrames = -1,
    lastTime = -1,
    stalledSince = Date.now()
  window.clearInterval(watchdogTimer)
  watchdogTimer = window.setInterval(() => {
    if (gen !== generation || phase.value !== 'playing' || !element || element.paused) {
      stalledSince = Date.now()
      return
    }
    const frames = element.getVideoPlaybackQuality?.().totalVideoFrames ?? -1
    const time = element.currentTime
    if (frames !== lastFrames || time !== lastTime) {
      lastFrames = frames
      lastTime = time
      stalledSince = Date.now()
      return
    }
    if (Date.now() - stalledSince < 8000) return
    window.clearInterval(watchdogTimer)
    if (protocol.value === 'webrtc' && !hlsTried) {
      hlsTried = true
      phase.value = 'reconnecting'
      message.value = 'WebRTC 画面停止更新，正在改用 HLS'
      void start('hls', stream.value)
    } else {
      void recover(gen, '画面停止更新')
    }
  }, 2000)
}

// 有界重连：最多 3 次，每次间隔递增；超过后需要用户手动重试。
async function recover(gen, reason) {
  if (gen !== generation || phase.value === 'reconnecting') return
  if (reconnects >= 3) return fail(`${reason}，已停止自动重连`)
  reconnects += 1
  phase.value = 'reconnecting'
  message.value = reason
  await new Promise(resolve => window.setTimeout(resolve, 1500 * reconnects))
  if (gen !== generation) return
  void start(protocol.value, stream.value)
}

function retry() {
  reconnects = 0
  hlsTried = false
  void start(undefined, stream.value)
}
function switchStream(value) {
  if (value !== stream.value) {
    reconnects = 0
    void start(protocol.value || undefined, value)
  }
}
async function resume() {
  try {
    await video.value.play()
    phase.value = 'playing'
  } catch {
    phase.value = 'blocked'
  }
}
function toggleMute() {
  muted.value = !muted.value
  if (video.value) video.value.muted = muted.value
  if (!muted.value) video.value?.play().catch(() => {})
}
function fullscreen() {
  const element = container.value
  if (document.fullscreenElement) document.exitFullscreen?.()
  else element?.requestFullscreen?.().catch(() => video.value?.webkitEnterFullscreen?.())
}

function onPageHide() {
  releaseSession()
}
function onIdentityChange() {
  generation++
  teardownMedia()
  releaseSession()
  phase.value = 'ended'
}

watch(
  () => props.cameraId,
  () => {
    reconnects = 0
    hlsTried = false
    stream.value = ''
    void start()
  }
)
onMounted(() => {
  window.addEventListener('pagehide', onPageHide)
  window.addEventListener('iot:unauthorized', onIdentityChange)
  void start()
})
onBeforeUnmount(() => {
  generation++
  window.removeEventListener('pagehide', onPageHide)
  window.removeEventListener('iot:unauthorized', onIdentityChange)
  teardownMedia()
  releaseSession()
})
</script>

<template>
  <div class="live-player">
    <div class="live-player__head">
      <div class="live-player__title">
        <strong>{{ cameraName || cameraId }}</strong>
        <small v-if="location">{{ location }}</small>
      </div>
      <StatusDot :tone="phaseTone" :label="protocolText ? `${phaseText} · ${protocolText}` : phaseText" />
    </div>
    <div ref="container" class="live-player__stage">
      <video ref="video" class="live-player__video" muted playsinline autoplay disablepictureinpicture />
      <div v-if="phase !== 'playing'" class="live-player__overlay" aria-live="polite">
        <template v-if="phase === 'blocked'"
          ><p>浏览器阻止了自动播放</p>
          <ui-button size="small" type="primary" @click="resume">开始播放</ui-button></template
        >
        <template v-else-if="phase === 'error' || phase === 'ended'"
          ><p>{{ message || phaseText }}</p>
          <ui-button v-if="phase === 'error'" size="small" @click="retry"><RefreshCw />重试</ui-button></template
        >
        <template v-else
          ><span class="live-player__spinner" aria-hidden="true" />
          <p>{{ message || phaseText }}</p></template
        >
      </div>
    </div>
    <div class="live-player__bar">
      <ui-radio-group
        v-if="streams.length > 1"
        :model-value="stream"
        size="small"
        class="segmented-choice-group"
        aria-label="码流"
        @update:model-value="switchStream"
      >
        <ui-radio-button value="main">主码流</ui-radio-button>
        <ui-radio-button value="sub">子码流</ui-radio-button>
      </ui-radio-group>
      <span class="live-player__meta"
        >{{ profileText }}<template v-if="grant?.videoCodec"> · 源 {{ grant.videoCodec }}</template></span
      >
      <span class="live-player__actions">
        <ui-button size="small" text :aria-label="muted ? '开启声音' : '静音'" :title="muted ? '开启声音' : '静音'" @click="toggleMute"
          ><VolumeX v-if="muted" /><Volume2 v-else
        /></ui-button>
        <ui-button size="small" text aria-label="重试" title="重新连接" @click="retry"><RefreshCw /></ui-button>
        <ui-button size="small" text aria-label="全屏" title="全屏" @click="fullscreen"><Maximize /></ui-button>
      </span>
    </div>
    <p v-if="grant" class="live-player__note">{{ grant.reason ? grant.reason + '。' : '' }}{{ grant.audioNote }}</p>
  </div>
</template>

<style scoped>
.live-player {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
}
.live-player__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  min-width: 0;
}
.live-player__title {
  display: grid;
  min-width: 0;
}
.live-player__title strong,
.live-player__title small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.live-player__title small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.live-player__stage {
  position: relative;
  aspect-ratio: 16 / 9;
  width: 100%;
  overflow: hidden;
  background: var(--media-stage-bg);
  border-radius: 6px;
}
.live-player__video {
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: var(--media-stage-bg);
}
.live-player__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-4);
  color: var(--media-overlay-text);
  text-align: center;
  background: var(--media-overlay-bg);
}
.live-player__overlay p {
  margin: 0;
  font-size: var(--font-size-sm);
  line-height: 1.6;
}
.live-player__spinner {
  width: 22px;
  height: 22px;
  border: 2px solid var(--media-spinner-track);
  border-top-color: var(--media-spinner-head);
  border-radius: 50%;
  animation: live-spin 0.9s linear infinite;
}
@keyframes live-spin {
  to {
    transform: rotate(360deg);
  }
}
.live-player__bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
}
.live-player__meta {
  flex: 1 1 160px;
  min-width: 0;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.live-player__actions {
  display: inline-flex;
  gap: 2px;
  margin-left: auto;
}
.live-player__note {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  line-height: 1.6;
}
.live-player__stage:fullscreen {
  border-radius: 0;
}
</style>
