import { reactive } from 'vue'
import { api, session } from './api'

// 直播模块状态按“租户 + 用户”缓存在内存中，不写入浏览器存储；切换身份时清空。
export const liveState = reactive({ key: '', status: null, canWatch: false, canManageModule: false, loading: false })

function identityKey() {
  return `${session.tenant}\u0000${session.user}`
}

let pending = null
export async function loadLiveStatus(force = false) {
  const key = identityKey()
  if (!force && liveState.key === key && liveState.status) return liveState
  if (pending && !force) return pending
  liveState.loading = true
  pending = api('/api/v1/video/status')
    .then(data => {
      if (key !== identityKey()) return liveState // 请求期间已切换身份，丢弃旧结果。
      liveState.key = key
      liveState.status = data.status || data
      liveState.canWatch = Boolean(data.canWatch)
      liveState.canManageModule = Boolean(data.canManageModule)
      return liveState
    })
    .catch(() => {
      if (key === identityKey())
        Object.assign(liveState, {
          key,
          status: { state: 'unknown', message: '无法读取直播模块状态' },
          canWatch: false,
          canManageModule: false
        })
      return liveState
    })
    .finally(() => {
      liveState.loading = false
      pending = null
    })
  return pending
}

export function resetLiveState() {
  Object.assign(liveState, { key: '', status: null, canWatch: false, canManageModule: false, loading: false })
  pending = null
}

const liveEnabled = () => liveState.status?.state === 'enabled'
export const liveUsable = () => liveEnabled() && liveState.canWatch

export const moduleStateText = {
  not_deployed: '未部署',
  misconfigured: '部署配置无效',
  disabled: '已部署，未启用',
  enabled: '已启用，运行正常',
  degraded: '已启用，媒体服务异常',
  unknown: '状态未知'
}
export const moduleStateTone = {
  not_deployed: 'neutral',
  misconfigured: 'danger',
  disabled: 'neutral',
  enabled: 'success',
  degraded: 'warning',
  unknown: 'neutral'
}

export const testStatusText = {
  PLAYABLE: '可播放',
  TRANSCODE_REQUIRED: '需转码播放',
  CODEC_INCOMPATIBLE: '编码不兼容',
  AUTH_FAILED: '认证失败',
  UNREACHABLE: '地址不可达',
  STREAM_NOT_FOUND: '通道或码流不存在',
  PROTOCOL_ERROR: '设备应答异常',
  MEDIA_UNAVAILABLE: '媒体服务不可用',
  MEDIA_PULL_FAILED: '媒体服务取流失败',
  TARGET_DENIED: '地址未通过校验',
  UNSUPPORTED: '不支持的设备'
}
export const testStatusTone = { PLAYABLE: 'success', TRANSCODE_REQUIRED: 'info', CODEC_INCOMPATIBLE: 'warning' }

// 摄像头列表中的直播状态：未配置、未启用、未检测、可播放、失败等。
export function cameraLiveBadge(live) {
  if (!live?.configured) return { label: '未配置', tone: 'neutral' }
  if (!live.enabled) return { label: '未启用', tone: 'neutral' }
  if (live.lastPlayableAt) return { label: '可播放', tone: 'success' }
  if (!live.testStatus) return { label: '未检测', tone: 'neutral' }
  return { label: testStatusText[live.testStatus] || live.testStatus, tone: testStatusTone[live.testStatus] || 'danger' }
}

// 浏览器对 H.265 的支持需要实际探测，不能按浏览器名称推断。
export function browserCaps() {
  let webrtcH265
  try {
    const codecs = globalThis.RTCRtpReceiver?.getCapabilities?.('video')?.codecs || []
    webrtcH265 = codecs.some(codec => /h265|hevc/i.test(codec.mimeType || ''))
  } catch {
    webrtcH265 = false
  }
  // 平台 HLS 使用 MPEG-TS 分片，H.265 分片的浏览器兼容性不稳定，统一按不支持处理。
  return { webrtcH265, hlsH265: false }
}

export const supportsWebRTC = () => typeof globalThis.RTCPeerConnection === 'function'

export function cameraLocation(camera) {
  return [camera?.building, camera?.floor, camera?.room, camera?.cameraPoint].filter(Boolean).join(' / ')
}
