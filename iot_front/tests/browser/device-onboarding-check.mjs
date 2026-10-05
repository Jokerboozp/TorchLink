import { startBrowser } from '../helpers/browser.mjs'
import { writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'
const origin = process.env.IOT_TEST_ORIGIN
let browser
try {
  browser = await startBrowser({ timeout: 16000 })
  const { call, evaluate, until, errors } = browser
  const click = async (text, scope = 'body') =>
    until(
      () =>
        evaluate(
          `(()=>{const e=[...document.querySelectorAll(${JSON.stringify(scope + ' button')})].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`
        ),
      text
    )
  const page = async name =>
    until(
      () =>
        evaluate(`(()=>{const e=document.querySelector('.nav-item[aria-label="${name}"]');if(!e)return false;e.click();return true})()`),
      name
    )
  const fill = async (label, value, scope = 'body') =>
    until(
      () =>
        evaluate(
          `(()=>{const input=document.querySelector(${JSON.stringify(scope + ' input[aria-label="' + label + '"]')})||[...document.querySelectorAll(${JSON.stringify(scope + ' .n-form-item')})].find(e=>e.querySelector('.n-form-item-label')?.textContent.trim().startsWith(${JSON.stringify(label)}))?.querySelector('input');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));return true})()`
        ),
      label
    )
  const select = async (label, value, scope = 'body') => {
    await until(
      () =>
        evaluate(
          `(()=>{const item=[...document.querySelectorAll(${JSON.stringify(scope + ' .n-form-item')})].find(e=>e.querySelector('.n-form-item-label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const e=item?.querySelector('.n-base-selection');if(!e)return false;e.click();return true})()`
        ),
      label
    )
    await until(
      () =>
        evaluate(
          `(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.getClientRects().length&&e.textContent.includes(${JSON.stringify(value)}));if(!e)return false;e.click();return true})()`
        ),
      value
    )
  }
  const waitText = (text, scope = 'body') =>
    until(() => evaluate(`(document.querySelector(${JSON.stringify(scope)})?.innerText||'').includes(${JSON.stringify(text)})`), text)
  const api = async (path, init = {}) => {
    const r = await fetch(origin + path, {
      ...init,
      headers: { Authorization: `Bearer ${process.env.IOT_TEST_TOKEN}`, 'Content-Type': 'application/json', ...init.headers }
    })
    return { status: r.status, body: await r.json().catch(() => ({})) }
  }
  const capture = async name => {
    if (name.startsWith('mobile'))
      await until(
        () => evaluate("innerWidth>767 || (document.querySelector('.app-sidebar')?.getBoundingClientRect().right || 0)<=1"),
        'mobile navigation closed'
      )
    await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
    assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth+2'), `${name} overflow`)
    const shot = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(join(tmpdir(), `iot-onboarding-${name}.png`), Buffer.from(shot.data, 'base64'))
  }
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`
  })
  await call('Page.navigate', { url: origin })

  // 未完成模板保存到后端，刷新后继续原草稿，不先创建运行资源。
  await page('设备模板')
  await click('新建设备模板')
  await fill('模板名称', '浏览器新模板', '.product-preparation')
  await capture('template-information')
  await click('保存并返回', '.product-preparation')
  await waitText('继续准备设备模板')
  assert.equal((await api('/api/v1/products')).body.total, 0)
  await call('Page.navigate', { url: origin })
  await page('设备模板')
  await click('浏览器新模板', '.template-drafts')
  await until(
    () => evaluate("document.querySelector('.product-preparation .n-form-item input')?.value==='浏览器新模板'"),
    'persistent template draft'
  )
  await click('保存并继续', '.product-preparation')
  await select('默认上报通道', 'HTTP', '.product-preparation')
  await evaluate("[...document.querySelectorAll('.thing-model-editor summary')].find(e=>e.textContent.includes('属性字段')).click()")
  await click('添加字段', '.thing-model-editor')
  await fill('字段标识', 'temperature', '.thing-model-editor')
  await fill('显示名称', '温度', '.thing-model-editor')
  await fill('单位', '℃', '.thing-model-editor')
  await capture('template-protocol')
  await click('保存并继续', '.product-preparation')
  await fill('必需字段', 'temperature', '.product-preparation')
  await capture('template-rules')
  await click('应用配置并验证首台设备', '.product-preparation')
  await waitText('首台设备验证', '.product-preparation')
  const list = (await api('/api/v1/products')).body.items
  assert.equal(list.length, 1)
  const productId = list[0].id
  assert.match(productId, /^product_[a-zA-Z0-9]{12}$/)
  assert.equal(list[0].thingModel.properties[0].identifier, 'temperature')
  assert.equal(list[0].thingModel.properties[0].unit, '℃')
  assert.equal(list[0].reusable, false)
  await click('添加首台验证设备', '.product-preparation')
  await waitText('标准 MQTT / HTTP 上报', '.onboarding')
  await fill('设备名称', '浏览器现场设备', '.onboarding')
  await fill('设备编号', 'site-device-01', '.onboarding')
  await click('保存并生成接入信息', '.onboarding')
  await waitText('设备密钥只显示这一次', '.onboarding')
  const field = await evaluate(
    "(()=>{const rows=[...document.querySelectorAll('.onboarding__kv')].map(row=>[row.querySelector('span')?.innerText,row.querySelector('code')?.innerText]);const get=name=>rows.find(([key])=>key===name)?.[1];return {url:get('上报地址'),key:get('AccessKey'),secret:get('Secret')}})()"
  )
  assert.ok(field.url && field.key && field.secret)
  assert.equal(await evaluate(`Object.values(localStorage).some(v=>String(v).includes(${JSON.stringify(field.secret)}))`), false)
  // 保存配置、一次解析和达到验收数量分别检查；只使用设备HTTP凭据入口产生证据。
  const unverified = await api(`/api/v1/products/${productId}/verification`, {
    method: 'POST',
    body: JSON.stringify({ deviceId: 'site-device-01' })
  })
  assert.notEqual(unverified.body.status, 'VERIFIED')
  for (let n = 1; n <= 2; n++) {
    const result = await fetch(field.url.replace('https://devices.example.test', origin), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Device-Key': field.key, 'X-Device-Secret': field.secret },
      body: JSON.stringify({ version: '1.0', id: `field-${n}`, timestamp: Date.now(), data: { temperature: 26 + n } })
    })
    assert.equal(result.status, 202)
  }
  await click('刷新结果', '.onboarding')
  await waitText('温度', '.onboarding')
  await capture('first-device-evidence')
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await capture('mobile-first-device')
  await click('我已保存', '.onboarding')
  await call('Page.navigate', { url: origin })
  await page('设备模板')
  await until(
    () =>
      evaluate(
        "(()=>{const b=[...document.querySelectorAll('.product-name')].find(e=>e.textContent.includes('浏览器新模板'));if(!b)return false;b.click();return true})()"
      ),
    'resume applied template'
  )
  await until(
    () =>
      evaluate(
        "(()=>{const b=document.querySelectorAll('.preparation-steps button')[3];if(!b || b.disabled)return false;b.click();return true})()"
      ),
    'resume first verification'
  )
  await click('添加首台验证设备', '.product-preparation')
  await waitText('现场配置与验证', '.onboarding')
  assert.equal((await api('/api/v1/device-registry')).body.total, 1)
  await click('保存并退出', '.onboarding')
  await click('检查并保存验收结果', '.product-preparation')
  await waitText('验收通过', '.product-preparation')
  assert.equal((await api('/api/v1/products')).body.items[0].reusable, true)
  await click('保存并返回', '.product-preparation')

  // 日常接入只有可复用模板；草稿在后端保存，刷新后从记录恢复实例参数。
  await page('设备管理')
  await click('添加设备')
  await select('设备模板', '浏览器新模板', '.onboarding')
  await click('下一步', '.onboarding')
  await fill('设备名称', '同模板第二台设备', '.onboarding')
  await fill('设备编号', 'site-device-02', '.onboarding')
  await click('保存并返回', '.onboarding')
  await call('Page.navigate', { url: origin })
  await page('设备管理')
  await click('接入草稿')
  await until(
    () =>
      evaluate(
        "(()=>{const row=[...document.querySelectorAll('.n-modal .n-data-table-tr')].find(e=>e.textContent.includes('同模板第二台设备'));const b=row?.querySelector('button');if(!b)return false;b.click();return true})()"
      ),
    'resume instance draft'
  )
  await until(
    () => evaluate("document.querySelector('.onboarding input[aria-label=\"设备编号\"]')?.value==='site-device-02'"),
    'instance identity restored'
  )
  await capture('mobile-instance-draft')
  await click('保存并生成接入信息', '.onboarding')
  await waitText('设备密钥只显示这一次', '.onboarding')
  await click('我已保存', '.onboarding')
  await click('保存并退出', '.onboarding')

  // 批量任务保留逐台结果，领取密钥不会写入浏览器存储。
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await click('批量添加')
  await select('设备模板', '浏览器新模板', '.batch-onboarding')
  await evaluate(
    "(()=>{const input=document.querySelector('.batch-onboarding textarea');input.value='batch-01,批量设备一,一层\\nbatch-02,批量设备二,二层';input.dispatchEvent(new Event('input',{bubbles:true}))})()"
  )
  await click('检查清单', '.batch-onboarding')
  await waitText('2 台通过登记检查', '.batch-onboarding')
  await click('确认登记 2 台设备', '.batch-onboarding')
  await waitText('已登记 2', '.batch-onboarding')
  await capture('batch-result')
  await click('领取本批设备密钥', '.batch-onboarding')
  await waitText('设备密钥只在领取时显示', '.batch-onboarding')
  assert.equal(await evaluate("Object.values(localStorage).some(v=>String(v).includes('ds_'))"), false)
  await click('我已保存，清除显示', '.batch-onboarding')
  await click('返回设备列表', '.batch-onboarding')
  await click('批量记录')
  await waitText('批量登记记录', '.n-modal')
  await click('继续查看', '.n-modal')
  await waitText('已登记 2', '.batch-onboarding')
  assert.deepEqual(errors, [], errors.join(' | '))
  console.log(
    'PASS: template draft recovery, continuous preparation, first-device real evidence, reusable daily template, server instance draft recovery, batch preflight/results/credentials and 390px layout'
  )
} catch (error) {
  if (browser) {
    try {
      console.error('PAGE:', (await browser.evaluate('document.body.innerText')).slice(-4500))
      console.error('ERRORS:', browser.errors)
    } catch {
      /* 诊断输出失败不掩盖原始错误。 */
    }
  }
  throw error
} finally {
  await browser?.close()
}
