// Camera live playback in real Chrome against a running API, Vite dev server
// and media server with a configured, live-enabled camera. Not part of
// `npm test`: it needs the video module deployed (see docs/PLATFORM.md).
//
// IOT_TEST_BROWSER   Chrome/Chromium executable
// IOT_TEST_ORIGIN    front-end origin, e.g. http://127.0.0.1:5173
// IOT_TEST_TOKEN     access token of a user allowed to watch the camera
// IOT_TEST_TENANT    tenant of the token (default tenant_001)
// IOT_TEST_CAMERA    camera name shown in the camera list
// IOT_TEST_HEADFUL=1 run headful off-screen (macOS may quit headless Chrome)
// IOT_TEST_DEVICE    optional: name of a device linked to the camera (device detail entry)
// IOT_TEST_ALARMS=1  optional: the newest alarm carries the camera (alarm detail entry)
// IOT_TEST_SCREENSHOT_DIR  optional directory for screenshots
import { startBrowser, delay } from '../helpers/browser.mjs'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import assert from 'node:assert/strict'

let browser, snapshot
try {
  browser = await startBrowser({ timeout: 30000, args: ['--autoplay-policy=no-user-gesture-required'] })
  const { call, evaluate, until } = browser
  const shot = async name => { if (process.env.IOT_TEST_SCREENSHOT_DIR) await writeFile(join(process.env.IOT_TEST_SCREENSHOT_DIR, name), Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')) }
  const click = async (text, scope = 'document') => until(() => evaluate(`(()=>{const e=[...${scope}.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`))
  snapshot = () => evaluate(`JSON.stringify({status:document.querySelector('.live-player .status-dot')?.textContent,overlay:document.querySelector('.live-player__overlay')?.textContent,note:document.querySelector('.live-player__note')?.textContent,pcs:window.__pcs?.map(p=>p.connectionState)})+'\\n'+document.body.innerText.slice(0,1500)`)

  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1360, height: 900, deviceScaleFactor: 1, mobile: false })
  // Record peer connections and session releases made by the player.
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant',${JSON.stringify(process.env.IOT_TEST_TENANT || 'tenant_001')});localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');
    window.__pcs=[];window.__released=[];
    const PC=window.RTCPeerConnection;window.RTCPeerConnection=function(...a){const p=new PC(...a);window.__pcs.push(p);return p};window.RTCPeerConnection.prototype=PC.prototype;
    const f=window.fetch;window.fetch=(url,o)=>{if(o?.method==='DELETE'&&String(url).includes('/play-sessions/'))window.__released.push(String(url));return f(url,o)};` })
  await call('Page.navigate', { url: process.env.IOT_TEST_ORIGIN })
  await click('摄像头映射')
  const camera = process.env.IOT_TEST_CAMERA
  await until(() => evaluate(`[...document.querySelectorAll('tr')].some(r=>r.textContent.includes(${JSON.stringify(camera)}))`))
  const openCamera = () => until(() => evaluate(`(()=>{const row=[...document.querySelectorAll('tr')].find(r=>r.textContent.includes(${JSON.stringify(camera)}));const b=row&&[...row.querySelectorAll('button')].find(b=>b.textContent.trim()==='观看');if(!b)return false;b.click();return true})()`))
  const decoded = () => evaluate(`(async()=>{const p=window.__pcs.at(-1);if(!p)return null;let v=null;(await p.getStats()).forEach(r=>{if(r.type==='inbound-rtp'&&r.kind==='video')v={decoded:r.framesDecoded,received:r.framesReceived,decoder:r.decoderImplementation||''}});return v})()`)
  const status = () => evaluate(`document.querySelector('.live-player .status-dot')?.textContent.trim()`)

  // 1. WebRTC: frames must keep advancing well past the first GOP.
  await openCamera()
  await until(async () => (await status())?.startsWith('播放中'))
  assert.match(await status(), /WebRTC/, 'WebRTC should be the first protocol')
  const first = await decoded()
  await delay(6000)
  const later = await decoded()
  assert.ok(later.decoded - first.decoded >= 60, `WebRTC frames must keep advancing (${first.decoded} -> ${later.decoded}, decoder ${later.decoder})`)
  const video = await evaluate(`(()=>{const v=document.querySelector('.live-player__video');return {w:v.videoWidth,h:v.videoHeight,muted:v.muted}})()`)
  assert.ok(video.w > 0 && video.muted, 'video must render muted by default')
  await shot('camera-live-webrtc.png')
  const webrtcState = { first, later, video }

  // 2. Closing the dialog destroys the player and releases the session.
  await click('关闭', `document.querySelector('.live-player-dialog')`)
  await until(() => evaluate(`window.__released.length>0 && !document.querySelector('.live-player')`))
  assert.equal(await evaluate(`window.__pcs.at(-1).connectionState`), 'closed', 'peer connection must be closed with the dialog')

  // 3. Without WebRTC the player uses HLS (hls.js / MSE) and frames advance.
  await evaluate(`window.__savedPC=window.RTCPeerConnection;window.RTCPeerConnection=undefined`)
  await openCamera()
  await until(async () => (await status())?.startsWith('播放中'))
  assert.match(await status(), /HLS/, 'HLS must be used when WebRTC is unavailable')
  const t0 = await evaluate(`document.querySelector('.live-player__video').currentTime`)
  await delay(4000)
  const t1 = await evaluate(`document.querySelector('.live-player__video').currentTime`)
  assert.ok(t1 - t0 > 2.5, `HLS playback must advance (${t0} -> ${t1})`)
  await shot('camera-live-hls.png')

  // 4. Narrow screen: no horizontal overflow in the dialog.
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await delay(400)
  assert.ok(await evaluate(`(()=>{const e=document.querySelector('.live-player-dialog');return e && e.scrollWidth<=e.clientWidth+1})()`), 'player dialog must not overflow on narrow screens')
  await shot('camera-live-mobile.png')
  await click('关闭', `document.querySelector('.live-player-dialog')`)
  await evaluate(`window.RTCPeerConnection=window.__savedPC`)
  await call('Emulation.setDeviceMetricsOverride', { width: 1360, height: 900, deviceScaleFactor: 1, mobile: false })

  // 5. Live configuration dialog: connection test of the current form.
  await until(() => evaluate(`(()=>{const row=[...document.querySelectorAll('tr')].find(r=>r.textContent.includes(${JSON.stringify(camera)}));const b=row&&[...row.querySelectorAll('button')].find(b=>b.textContent.trim()==='直播配置');if(!b)return false;b.click();return true})()`))
  await until(() => evaluate(`!!document.querySelector('.live-config') && !document.querySelector('.live-config.ui-loading')`))
  assert.ok(await evaluate(`[...document.querySelectorAll('.live-config input[type=password]')].every(i=>i.value==='')`), 'stored password must never be shown')
  await click('测试当前填写内容')
  const testText = await until(() => evaluate(`document.querySelector('.live-config__result')?.textContent.trim()`), 'camera connection test', 40000)
  assert.match(testText, /可播放|需转码播放/, `connection test result: ${testText}`)
  await shot('camera-live-config.png')
  await click('取消')
  const playFrom = async (label, scope) => {
    const released = await evaluate('window.__released.length')
    await click('观看直播', scope)
    await until(async () => (await status())?.startsWith('播放中'))
    assert.ok(await evaluate(`document.querySelector('.live-player__video').videoWidth>0`), `${label}: picture must render`)
    await shot(`camera-live-${label}.png`)
    await evaluate(`[...document.querySelectorAll('.live-player-dialog button')].find(b=>b.textContent.trim()==='关闭').click()`)
    await until(() => evaluate(`window.__released.length>${released} && !document.querySelector('.live-player')`))
  }

  // 6. Device detail reuses the player for the device's cameras.
  if (process.env.IOT_TEST_DEVICE) {
    await click('设备管理')
    await until(() => evaluate(`(()=>{const b=[...document.querySelectorAll('button.device-name')].find(b=>b.textContent.includes(${JSON.stringify(process.env.IOT_TEST_DEVICE)}));if(!b)return false;b.click();return true})()`))
    await until(() => evaluate(`document.querySelector('.device-cameras')?.textContent.includes(${JSON.stringify(camera)})`))
    await playFrom('device', `document.querySelector('.device-cameras')`)
  }
  // 7. Alarm detail offers the alarm's cameras.
  if (process.env.IOT_TEST_ALARMS) {
    await call('Page.navigate', { url: process.env.IOT_TEST_ORIGIN })
    await click('告警中心')
    await click('查看详情')
    await until(() => evaluate(`document.querySelector('.alarm-detail-dialog .linked-cameras')?.textContent.includes('观看直播')`))
    await playFrom('alarm', `document.querySelector('.alarm-detail-dialog .linked-cameras')`)
  }
  console.log('PASS', JSON.stringify({ webrtc: webrtcState, hlsAdvanced: Number((t1 - t0).toFixed(2)) }))
} catch (e) { if (snapshot) console.error(await snapshot().catch(() => '')); throw e } finally {
  await browser?.close()
}
