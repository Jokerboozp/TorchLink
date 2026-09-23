// Verify the visible strokes of the sidebar brand fit inside its SVG viewport.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const profile = await mkdtemp(join(tmpdir(), 'iot-logo-check-'))
const child = spawn(browser, ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check) { for (let i = 0; i < 100; i++) { const value = await check(); if (value) return value; await delay(100) } throw new Error('浏览器等待超时') }

let socket
try {
  const port = await until(async () => { try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null } })
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let id = 0
  const pending = new Map()
  socket.onmessage = event => { const message = JSON.parse(event.data); if (!message.id) return; const entry = pending.get(message.id); pending.delete(message.id); message.error ? entry.reject(new Error(message.error.message)) : entry.resolve(message.result) }
  const call = (method, params = {}) => new Promise((resolve, reject) => { const next = ++id; pending.set(next, { resolve, reject }); socket.send(JSON.stringify({ id: next, method, params })) })
  const evaluate = async expression => { const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (value.exceptionDetails) throw new Error(value.exceptionDetails.text); return value.result.value }
  const logo = pathToFileURL(join(process.cwd(), 'public', 'torchlink-sidebar.svg')).href
  await call('Page.navigate', { url: logo })
  const geometry = await until(() => evaluate(`(() => {const svg=document.querySelector('svg');if(!svg)return null;const box=svg.viewBox.baseVal;const paths=[...svg.querySelectorAll('path')].map(path=>{const r=path.getBBox(),style=getComputedStyle(path),stroke=style.stroke==='none'?0:parseFloat(style.strokeWidth)||0;return {minX:r.x-stroke/2,minY:r.y-stroke/2,maxX:r.x+r.width+stroke/2,maxY:r.y+r.height+stroke/2}});return {viewport:{minX:box.x,minY:box.y,maxX:box.x+box.width,maxY:box.y+box.height,width:box.width},paths}})()`))
  for (const [index, path] of geometry.paths.entries()) {
    assert.ok(path.minX >= geometry.viewport.minX && path.minY >= geometry.viewport.minY && path.maxX <= geometry.viewport.maxX && path.maxY <= geometry.viewport.maxY, `侧栏 Logo 路径 ${index + 1} 被 SVG 视口裁切：${JSON.stringify({ path, viewport: geometry.viewport })}`)
  }
  console.log('侧栏 Logo 的路径与描边均在 SVG 视口内')

  if (process.env.IOT_UI_PREVIEW_ORIGIN) {
    await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
    await call('Page.navigate', { url: process.env.IOT_UI_PREVIEW_ORIGIN })
    await until(() => evaluate("Boolean(document.querySelector('.login-form #password'))"))
    await evaluate("(()=>{const input=document.querySelector('.login-form #password');input.value='fixture';input.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.login-form').requestSubmit()})()")
    await until(() => evaluate("document.querySelector('.brand-logo img')?.complete && document.querySelector('.brand-logo img')?.naturalWidth > 0"))
    const inspect = () => evaluate(`(() => {const aside=document.querySelector('.app-aside'),brand=document.querySelector('.brand'),logo=document.querySelector('.brand-logo'),img=logo.querySelector('img'),a=aside.getBoundingClientRect(),b=brand.getBoundingClientRect(),l=logo.getBoundingClientRect(),i=img.getBoundingClientRect();return {collapsed:aside.classList.contains('is-collapsed'),aside:{left:a.left,right:a.right},brand:{top:b.top,bottom:b.bottom},logo:{left:l.left,right:l.right,top:l.top,bottom:l.bottom,width:l.width,height:l.height},image:{left:i.left,width:i.width,height:i.height}}})()`)
    const expanded = await inspect()
    assert.ok(expanded.logo.top >= expanded.brand.top && expanded.logo.bottom <= expanded.brand.bottom && expanded.logo.right <= expanded.aside.right, `展开侧栏 Logo 越界：${JSON.stringify(expanded)}`)
    const screenshot = async name => { const result = await call('Page.captureScreenshot', { format: 'png' }); await writeFile(join(tmpdir(), name), Buffer.from(result.data, 'base64')) }
    await screenshot('iot-sidebar-logo-expanded.png')
    await evaluate("document.querySelector('.collapse-button').click()")
    await until(async () => { const state = await inspect(); return state.collapsed ? state : null })
    await delay(250) // Wait for the sidebar width transition before checking the final clipped state.
    const collapsed = await inspect()
    const icon = geometry.paths[0]
    const scale = collapsed.image.width / geometry.viewport.width
    assert.ok(collapsed.logo.top >= collapsed.brand.top && collapsed.logo.bottom <= collapsed.brand.bottom && collapsed.logo.left + icon.minX * scale >= collapsed.aside.left && collapsed.logo.left + icon.maxX * scale <= collapsed.logo.right, `折叠侧栏图形被裁切：${JSON.stringify(collapsed)}`)
    await screenshot('iot-sidebar-logo-collapsed.png')
    console.log('展开与折叠侧栏的 Logo 均在可见区域内')
  }
} finally {
  socket?.close()
  child.kill()
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
