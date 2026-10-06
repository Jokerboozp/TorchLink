import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { stripTypeScriptTypes } from 'node:module'

// liveVideo.ts imports the API client; load only the pure helpers here.
// Normalize CRLF first so the block removal below also matches core.autocrlf=true checkouts.
const source = stripTypeScriptTypes(fs.readFileSync(new URL('../src/liveVideo.ts', import.meta.url), 'utf8').replace(/\r\n/g, '\n'))
const pure = source
  .replace(/^import\s[^'"]*['"][^'"]+['"];?$/gm, '')
  .replace(/export const liveState[\s\S]*?export function resetLiveState\(\) \{[\s\S]*?\n\}\n/, '')
const mod = await import('data:text/javascript,' + encodeURIComponent(pure))

test('camera live badge distinguishes configuration and test states', () => {
  assert.deepEqual(mod.cameraLiveBadge(undefined), { label: '未配置', tone: 'neutral' })
  assert.deepEqual(mod.cameraLiveBadge({ configured: true, enabled: false }), { label: '未启用', tone: 'neutral' })
  assert.deepEqual(mod.cameraLiveBadge({ configured: true, enabled: true }), { label: '未检测', tone: 'neutral' })
  assert.deepEqual(mod.cameraLiveBadge({ configured: true, enabled: true, testStatus: 'AUTH_FAILED' }), {
    label: '认证失败',
    tone: 'danger'
  })
  assert.deepEqual(mod.cameraLiveBadge({ configured: true, enabled: true, testStatus: 'TRANSCODE_REQUIRED' }), {
    label: '需转码播放',
    tone: 'info'
  })
  // A real browser render outranks an older test result.
  assert.deepEqual(mod.cameraLiveBadge({ configured: true, enabled: true, testStatus: 'MEDIA_UNAVAILABLE', lastPlayableAt: 1 }), {
    label: '可播放',
    tone: 'success'
  })
})

test('browser capability detection never assumes H.265 support', () => {
  const saved = globalThis.RTCRtpReceiver
  try {
    delete globalThis.RTCRtpReceiver
    assert.deepEqual(mod.browserCaps(), { webrtcH265: false, hlsH265: false })
    globalThis.RTCRtpReceiver = { getCapabilities: () => ({ codecs: [{ mimeType: 'video/H264' }, { mimeType: 'video/H265' }] }) }
    assert.deepEqual(mod.browserCaps(), { webrtcH265: true, hlsH265: false })
    globalThis.RTCRtpReceiver = {
      getCapabilities: () => {
        throw new Error('blocked')
      }
    }
    assert.equal(mod.browserCaps().webrtcH265, false)
  } finally {
    if (saved) globalThis.RTCRtpReceiver = saved
    else delete globalThis.RTCRtpReceiver
  }
})

test('camera location joins known parts', () => {
  assert.equal(mod.cameraLocation({ building: 'A栋', floor: '1F', cameraPoint: '东侧入口' }), 'A栋 / 1F / 东侧入口')
})

test('nginx keeps media behind per-request authorization', () => {
  const nginx = fs.readFileSync(new URL('../nginx.conf', import.meta.url), 'utf8')
  assert.match(nginx, /location ~ "\^\/media\/hls\/[\s\S]*?auth_request \/__video_media_auth;/)
  // A plain prefix fallback: '^~' would stop nginx from trying the HLS regex location.
  assert.match(nginx, /location \/media\/ \{\s*return 404;/)
  assert.doesNotMatch(nginx, /location \^~ \/media\//)
  assert.match(nginx, /resolver 127\.0\.0\.11/)
})
