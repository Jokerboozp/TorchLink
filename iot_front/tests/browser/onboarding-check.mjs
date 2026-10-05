// 真实 Chromium + 已构建前端 + 隔离的 Go API：新建模板、添加设备、设备上报、连接详情、接入点会话、摄像头与模型来源。
import { startBrowser } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'

const origin = process.env.IOT_TEST_ORIGIN
let browser
try {
  browser = await startBrowser({ args: ['--use-mock-keychain', '--password-store=basic'] })
  const { call, evaluate, until, errors: failures } = browser
  const button = (text, scope = 'body') =>
    evaluate(
      `(() => {const item=[...document.querySelectorAll(${JSON.stringify(`${scope} button`)})].find(b=>b.innerText.trim()===${JSON.stringify(text)}&&!b.disabled&&b.getClientRects().length);if(!item)return false;item.click();return true})()`
    )
  const setInput = (label, value, scope = 'body') =>
    evaluate(
      `(() => {const input=document.querySelector(${JSON.stringify(`${scope} input[aria-label="${label}"]`)})||[...document.querySelectorAll(${JSON.stringify(`${scope} .n-form-item`)})].find(item=>item.querySelector('.n-form-item-label')?.innerText.replace('*','').trim()===${JSON.stringify(label)})?.querySelector('input');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));return true})()`
    )
  // 下拉选项：选项未出现时重新点开选择框，避免抽屉或表单刷新时首次点击落空。
  const pick = (selection, option, note) =>
    until(
      () =>
        evaluate(
          `(() => {const item=[...document.querySelectorAll('.n-base-select-option')].find(o=>o.getClientRects().length&&o.innerText.trim().startsWith(${JSON.stringify(option)}));if(item){item.click();return true}const box=${selection};if(box&&!document.querySelector('.n-base-select-menu'))box.click();return false})()`
        ),
      note || option
    )
  const choose = (scope, label, option) =>
    pick(
      `[...document.querySelectorAll(${JSON.stringify(`${scope} .n-form-item`)})].find(item=>item.innerText.includes(${JSON.stringify(label)}))?.querySelector('.n-base-selection')`,
      option,
      `${label}：${option}`
    )
  const openPage = async name => {
    await until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="${name}"]'))`), name)
    await evaluate(`document.querySelector('.nav-item[aria-label="${name}"]').click()`)
  }
  const drawer = "[...document.querySelectorAll('.n-drawer')].find(item=>item.getClientRects().length)"
  const closeDrawer = async () => {
    await evaluate(`${drawer}?.querySelector('.n-base-close')?.click()`)
    await until(() => evaluate(`!${drawer}`), '关闭抽屉')
  }
  const api = async (path, init = {}) => {
    const response = await fetch(origin + path, {
      ...init,
      headers: { Authorization: `Bearer ${process.env.IOT_TEST_TOKEN}`, 'Content-Type': 'application/json', ...init.headers }
    })
    return { status: response.status, body: await response.json().catch(() => ({})) }
  }

  await call('Page.enable')
  await call('Runtime.enable')
  await call('Inspector.enable').catch(() => {})
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`
  })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 10"), '主导航')

  // Modbus 协议版本由 Go 测试预先发布，这里通过统一的添加设备接口登记采集设备。
  const modbus = await api('/api/v1/onboarding', {
    method: 'POST',
    body: JSON.stringify({
      requestId: 'browser-modbus',
      trial: true,
      productId: 'browser-modbus-product',
      device: { id: 'browser-modbus-preview', name: 'Modbus 预览' },
      connection: { mode: 'poll', host: '127.0.0.1', port: Number(process.env.IOT_TEST_MODBUS_PORT), unitId: 1, timeoutMs: 3000 }
    })
  })
  assert.equal(modbus.status, 201, JSON.stringify(modbus.body))

  // 模板按连续页面准备，首台实机在同页登记；尚未验收不会进入日常复用列表。
  await openPage('设备模板')
  await until(() => button('新建设备模板', '.filter-bar'), '新建设备模板')
  await until(() => setInput('模板名称', '浏览器标准产品', '.product-preparation'), '模板名称')
  await evaluate("document.querySelector('.product-preparation details summary').click()")
  await until(
    () =>
      evaluate(
        "(()=>{const input=document.querySelector('.product-preparation details input');if(!input)return false;input.value='browser-standard-product';input.dispatchEvent(new Event('input',{bubbles:true}));return true})()"
      ),
    '模板标识'
  )
  await until(() => button('保存并继续', '.product-preparation'), '保存模板信息')
  await choose('.product-preparation', '默认上报通道', 'HTTP')
  await until(() => button('保存并继续', '.product-preparation'), '保存通信协议')
  await until(() => setInput('最少有效报文数', '1', '.product-preparation'), '单报文验收规则')
  await until(() => button('应用配置并验证首台设备', '.product-preparation'), '应用配置')
  await until(() => button('添加首台验证设备', '.product-preparation'), '添加首台验证设备')
  assert.equal((await api('/api/v1/products/browser-standard-product/preparation')).body.candidate.verificationRules.minMessages, 1)
  await until(
    () => evaluate("document.querySelector('.onboarding__summary')?.innerText.includes('标准 MQTT / HTTP 上报')"),
    '首台复用当前模板'
  )
  await until(() => setInput('设备名称', '浏览器传感器', '.onboarding'), '设备名称')
  await setInput('设备编号', 'browser-device', '.onboarding')
  await evaluate("[...document.querySelectorAll('.onboarding .n-radio-button')].find(b=>b.innerText.trim()==='HTTP').click()")
  await until(() => button('保存并生成接入信息', '.onboarding'), '保存设备')
  await until(() => evaluate("Boolean(document.querySelector('.onboarding__secret code'))"), '一次性密钥')
  const [accessKey, secret] = await evaluate("[...document.querySelectorAll('.onboarding__secret code')].map(e=>e.innerText.trim())")
  assert.ok(accessKey && secret, '向导未显示 AccessKey 与 Secret')
  assert.ok(
    await evaluate(`!Object.values(localStorage).some(value=>String(value).includes(${JSON.stringify(secret)}))`),
    '设备密钥写入了浏览器存储'
  )

  // 设备侧 HTTP 上报：错误密钥被拒绝，正确密钥解析成功，同一消息重发被去重。
  const report = JSON.stringify({ version: '1.0', id: 'browser-http-1', timestamp: Date.now(), data: { temperature: 26.5 } })
  const ingest = (key, value) =>
    fetch(`${origin}/api/v1/device-ingest/standard/tenant/browser-standard-product/browser-device/property`, {
      method: 'POST',
      headers: { 'X-Device-Key': key, 'X-Device-Secret': value, 'Content-Type': 'application/json' },
      body: report
    }).then(async r => ({ status: r.status, body: await r.json() }))
  const rejected = await ingest(accessKey, 'invalid-secret')
  assert.deepEqual([rejected.status, rejected.body.errorCode], [401, 'AUTH_FAILED'])
  const accepted = await ingest(accessKey, secret)
  assert.deepEqual([accepted.status, accepted.body.created], [202, true], JSON.stringify(accepted.body))
  const duplicate = await ingest(accessKey, secret)
  assert.deepEqual([duplicate.status, duplicate.body.created], [202, false], JSON.stringify(duplicate.body))
  if (process.env.IOT_TEST_MQTT_WEBSOCKET) {
    // 设备侧 MQTT：用设备凭据换取短期令牌，通过真实 Broker 的 WebSocket 上报。
    const { default: mqtt } = await import('mqtt')
    const grant = await fetch(`${origin}/api/v1/device-mqtt/token`, {
      method: 'POST',
      headers: { 'X-Device-Key': accessKey, 'X-Device-Secret': secret }
    }).then(r => r.json())
    const client = await mqtt.connectAsync(process.env.IOT_TEST_MQTT_WEBSOCKET, {
      username: grant.username,
      password: grant.token,
      clientId: `device-${accessKey}`,
      clean: true,
      reconnectPeriod: 0,
      connectTimeout: 10000
    })
    await client.publishAsync(
      grant.publishTopic,
      JSON.stringify({ version: '1.0', id: 'browser-mqtt-1', timestamp: Date.now(), data: { temperature: 27 } }),
      { qos: 1 }
    )
    await client.endAsync()
    await until(async () => (await api('/api/v1/device-registry/browser-device/history?kind=property')).body.total >= 2, 'MQTT 上报入库')
    console.log('PASS: device MQTT token exchange and property report through the live Broker WebSocket')
  }

  // 向导第三步刷新后确认已收到并解析上报。
  await until(() => button('我已保存', '.onboarding'), '我已保存')
  await until(() => button('刷新结果', '.onboarding'), '立即刷新')
  const latest = process.env.IOT_TEST_MQTT_WEBSOCKET ? '27' : '26.5'
  await until(
    () => evaluate(`(document.querySelector('.onboarding')?.innerText||'').includes(${JSON.stringify(latest)})`),
    '向导显示最新上报值'
  )
  await until(() => button('保存并退出', '.onboarding'), '退出首台配置')
  await until(() => button('检查并保存验收结果', '.product-preparation'), '记录真实验收')
  await until(() => evaluate("document.querySelector('.product-preparation')?.innerText.includes('验收通过')"), '模板验收通过')
  await until(() => button('保存并返回', '.product-preparation'), '返回模板列表')
  await openPage('设备管理')

  // 连接详情：历史分区与窄屏抽屉；密钥不再显示。
  const openDetail = name =>
    until(
      () =>
        evaluate(
          `(() => {const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.innerText.includes(${JSON.stringify(name)}));const item=[...(row?.querySelectorAll('.row-actions button')||[])].find(b=>b.innerText.trim()==='详情');if(!item)return false;item.click();return true})()`
        ),
      `${name} 详情`
    )
  await openDetail('浏览器传感器')
  await until(() => evaluate(`${drawer}?.innerText.includes('连接与状态历史')`), '连接详情')
  const detailText = await evaluate(`${drawer}.innerText`)
  for (const section of ['最近事件', '最近告警', 'iot-standard']) assert.ok(detailText.includes(section), `连接详情缺少“${section}”`)
  assert.ok(!detailText.includes(secret), '连接详情显示了设备密钥')
  assert.ok(!/设备影子|设备孪生与拓扑/.test(detailText), '连接详情仍显示已移除的影子或拓扑')
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await until(() => evaluate(`${drawer}?.getBoundingClientRect().width<=391`), '窄屏抽屉宽度')
  await closeDrawer()
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })

  // 设备专属采集连接原地纠正，保留已有协议与查询配置。
  await openDetail('Modbus 预览')
  await until(() => button('修改本设备连接参数', '.onboarding-diagnosis'), '修改本设备连接参数')
  await until(() => setInput('站号', '0', '.connection-correction'), '采集站号')
  await setInput('超时（毫秒）', '4000', '.connection-correction')
  await until(() => button('保存连接参数', '.connection-correction'), '保存本设备连接参数')
  await until(() => evaluate('!document.querySelector(".connection-correction")'), '连接参数保存完成')
  const corrected = (await api('/api/v1/device-registry/browser-modbus-preview/connection')).body
  assert.equal(corrected.profile.unitId, 0)
  assert.equal(corrected.profile.timeoutMs, 4000)
  assert.equal(corrected.profile.protocolId, 'browser-modbus')
  await closeDrawer()

  // 历史设备关联多个接入点：选择后只展示该接入点的会话。
  await openDetail('历史设备')
  await until(() => evaluate(`${drawer}?.innerText.includes('设备关联了多个接入点')`), '多个接入点提示')
  await pick(`${drawer}?.querySelector('.profile-picker .n-base-selection')`, 'listener-a', '选择接入点 listener-a')
  await until(() => evaluate(`(${drawer}?.querySelector('.ui-descriptions')?.innerText||'').includes('TCP')`), '接入点信息')
  await until(
    () =>
      evaluate(
        `(() => {const text=[...${drawer}.querySelectorAll('.ui-table')].map(t=>t.innerText).join(' ');return text.includes('listener-a')&&!text.includes('listener-b')})()`
      ),
    '只显示所选接入点的会话'
  )
  await until(() => button('打开模板公共连接', '.onboarding-diagnosis'), '共享连接跳转模板')
  await until(() => evaluate("document.querySelector('.product-preparation h2')?.innerText==='历史产品'"), '对应模板公共配置')
  await until(() => button('保存并返回', '.product-preparation'), '返回设备模板列表')

  // 运行会话仍按设备与所选接入点查看；模板设置已经改成完整页面。
  await openPage('设备模板')
  await until(
    () =>
      evaluate(
        "(()=>{const item=[...document.querySelectorAll('.product-name')].find(x=>x.textContent.includes('浏览器标准产品'));if(!item)return false;item.click();return true})()"
      ),
    '打开模板准备页'
  )
  await until(() => evaluate("document.querySelector('.product-preparation')?.innerText.includes('可以复用')"), '已验收模板状态')
  await until(() => button('保存并返回', '.product-preparation'), '返回模板列表')

  // 摄像头只登记元数据，不出现视频协议入口。
  await openPage('摄像头映射')
  await until(() => button('新增摄像头', '.filter-bar'), '新增摄像头')
  await until(() => setInput('摄像头标识', 'browser-camera', '.n-modal'), '摄像头标识')
  assert.equal(await evaluate("/国标视频目录|ONVIF/.test(document.querySelector('.n-modal').innerText)"), false)
  await setInput('摄像头名称', '直接登记摄像头', '.n-modal')
  await until(() => button('保存', '.n-modal'), '保存摄像头')
  await until(() => evaluate("document.querySelector('.app-content')?.innerText.includes('直接登记摄像头')"), '摄像头出现在列表')

  // 模型管理保留具体技术名称，只查看选项，不测试或应用配置。
  await openPage('模型管理')
  await until(() => evaluate("Boolean(document.querySelector('.provider-select .n-base-selection'))"), '模型来源')
  await evaluate("document.querySelector('.provider-select .n-base-selection').click()")
  await until(
    () => evaluate("[...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).length>=2"),
    '模型来源选项'
  )
  assert.deepEqual(
    new Set(
      await evaluate(
        "[...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).map(e=>e.innerText.trim())"
      )
    ),
    new Set(['DeepSeek', 'OpenAI 兼容 API'])
  )

  assert.deepEqual(failures, [], `页面脚本异常：${failures.join(' | ')}`)
  console.log(
    'PASS: template and device created in the UI, device HTTP credential rejection, parsing and deduplication, wizard verification, connection drawer and narrow layout, device poll parameter correction, shared listener template navigation, access point selection and sessions, camera metadata, provider names'
  )
} catch (error) {
  if (browser)
    try {
      console.error('PAGE:', (await browser.evaluate('document.body.innerText')).slice(-5000))
      console.error('ERRORS:', browser.errors)
    } catch {
      /* 诊断输出失败不掩盖原始错误。 */
    }
  throw error
} finally {
  await browser?.close()
}
