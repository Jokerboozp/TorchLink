// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { startBrowser, delay } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
let browser
try {
  browser = await startBrowser({ timeout: 60000 })
  const { call, evaluate, until } = browser
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
  await click('告警中心')
  await until(() => evaluate(`document.body.textContent.includes('二楼走廊')`))
  assert.equal(await evaluate(`document.querySelectorAll('.n-data-table-tbody .n-data-table-tr').length`), 2)
  await click('查看详情')
  await until(() => evaluate(`document.querySelector('.n-modal')?.textContent.includes('loop-1/node-7')`))
  assert.equal(await evaluate(`document.querySelector('.n-modal').textContent.includes('部件位置')`), true)
  assert.equal(await evaluate(`document.querySelector('.n-modal').textContent.includes('loop-1/node-8')`), false)
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await delay(200)
  assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`), true)
  await click('关闭详情')
  await evaluate(
    `(async()=>{const result=await (await fetch('/api/v1/alarms?status=ACTIVE',{headers:{Authorization:'Bearer '+localStorage.getItem('iot_token')}})).json();for(const item of result.items)window.dispatchEvent(new CustomEvent('iot:realtime',{detail:{topic:'/iot/alarm/raised/c/d/b/smoke/controller',payload:item}}))})()`
  )
  await until(() => evaluate(`document.querySelectorAll('.global-alert-popup').length===2`))
  assert.equal(await evaluate(`[...document.querySelectorAll('.global-alert-popup')].every(e=>e.textContent.includes('二楼走廊'))`), true)
  console.log(
    'PASS: authenticated component alarm list/detail, correct part and location, no normal-part alarm, same-message fire/fault popups remain separate, narrow viewport'
  )
} finally {
  await browser?.close()
}
