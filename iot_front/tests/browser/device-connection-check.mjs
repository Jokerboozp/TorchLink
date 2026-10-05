// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { startBrowser, delay } from '../helpers/browser.mjs'
import { writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import assert from 'node:assert/strict'
let browser, snapshot
try {
  browser = await startBrowser({ timeout: 25000 })
  const { call, evaluate, until } = browser
  snapshot = () => evaluate('document.body.innerText.slice(0,7000)')
  await call('Page.enable')
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`
  })
  await call('Page.navigate', { url: process.env.IOT_TEST_ORIGIN })
  const click = async text =>
    until(() =>
      evaluate(
        `(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`
      )
    )

  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `window.__detailRequests=[];window.__failHistory=false;const fetchOriginal=window.fetch;window.fetch=(url,options)=>{const path=String(url);if(path.includes('/device-registry/'))window.__detailRequests.push(path);if(window.__failHistory && /\\/history\\?page=/.test(path))return Promise.resolve(new Response(JSON.stringify({detail:'历史服务暂时不可用'}),{status:503,headers:{'Content-Type':'application/json'}}));return fetchOriginal(url,options)}`
  })
  await call('Page.reload')
  await call('Emulation.setDeviceMetricsOverride', { width: 1360, height: 900, deviceScaleFactor: 1, mobile: false })
  await click('设备管理')
  await click('详情')
  await until(() => evaluate(`document.querySelector('.ui-drawer .ui-descriptions')`))
  await delay(400)
  assert.equal(
    await evaluate(`window.__detailRequests.filter(p=>p.endsWith('/children?page=1&pageSize=20')).length`),
    0,
    'ordinary device must not request children'
  )
  assert.equal(
    await evaluate(`window.__detailRequests.some(p=>p.includes('/shadow') || p.includes('/commands'))`),
    false,
    'removed features must not issue requests'
  )
  assert.equal(
    await evaluate(
      `document.querySelector('.ui-drawer').textContent.includes('设备影子') || document.querySelector('.ui-drawer').textContent.includes('设备孪生与拓扑')`
    ),
    false,
    'removed features must not have controls'
  )
  assert.ok(
    await evaluate(`document.querySelector('.device-access-info .ui-descriptions')?.textContent.includes('/api/v1/device-ingest/')`),
    'access information must be in structured cells'
  )
  assert.ok(
    await evaluate(`document.querySelector('.device-properties .ui-table')?.textContent.includes('42')`),
    'properties must be in a table'
  )
  await evaluate('window.__failHistory=true')
  await until(() =>
    evaluate(
      `(()=>{const e=[...document.querySelectorAll('.device-connection-drawer button')].find(e=>e.textContent.trim()==='刷新'&&!e.disabled);if(!e)return false;e.click();return true})()`
    )
  )
  await until(() => evaluate(`document.querySelector('.device-history .ui-alert')?.textContent.includes('历史服务暂时不可用')`))
  assert.ok(
    await evaluate(`document.querySelector('.device-properties')?.textContent.includes('42')`),
    'optional error must not hide device data'
  )
  await evaluate('window.__failHistory=false')
  await until(() =>
    evaluate(
      `(()=>{const e=[...document.querySelectorAll('.device-connection-drawer button')].find(e=>e.textContent.trim()==='刷新'&&!e.disabled);if(!e)return false;e.click();return true})()`
    )
  )
  await until(() => evaluate(`!document.querySelector('.device-history .ui-alert')`))
  if (process.env.IOT_TEST_SCREENSHOT_DIR)
    await writeFile(
      join(process.env.IOT_TEST_SCREENSHOT_DIR, 'device-connection-desktop.png'),
      Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
    )
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await delay(300)
  assert.ok(
    await evaluate(`(()=>{const e=document.querySelector('.n-drawer-body-content-wrapper');return e.scrollWidth<=e.clientWidth+1})()`),
    'drawer content must not overflow'
  )
  assert.ok(
    await evaluate(`[...document.querySelectorAll('.device-summary .n-descriptions-table tr')].every(e=>e.children.length===2)`),
    'mobile overview must use one label/value pair per row'
  )
  assert.ok(
    await evaluate(
      `(()=>{const card=document.querySelector('.connection-section');const body=getComputedStyle(document.querySelector('.device-connection-drawer .n-drawer-body-content-wrapper')).backgroundColor;return getComputedStyle(card).backgroundColor!==body && getComputedStyle(card).borderTopWidth!=='0px'})()`
    ),
    'card boundaries must be distinct from background'
  )
  if (process.env.IOT_TEST_SCREENSHOT_DIR)
    await writeFile(
      join(process.env.IOT_TEST_SCREENSHOT_DIR, 'device-connection-mobile.png'),
      Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
    )
  await evaluate(
    `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`
  )
  // Remove the startup admin token hook before reload.
  await call('Page.navigate', { url: 'about:blank' })
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`
  })
  await call('Page.navigate', { url: process.env.IOT_TEST_ORIGIN })
  await click('设备管理')
  await click('详情')
  await until(() => evaluate(`document.querySelector('.ui-drawer .ui-descriptions')`))
  assert.equal(
    await evaluate(`document.querySelector('.ui-drawer').textContent.includes('重新生成凭据')`),
    false,
    'viewer must not be offered credential mutation'
  )
  console.log(
    'PASS: structured fields, distinct sections, mobile layout, no unrelated API requests, independent failure recovery and viewer permissions'
  )
} catch (e) {
  if (snapshot) console.error(await snapshot())
  throw e
} finally {
  await browser?.close()
}
