// 用隔离的合成 API 数据检查不同来源协议的统一版本入口。
import { startBrowser, delay } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// 样本覆盖生成映射、Go 源码、历史版本和尚未创建版本的协议。
const fixtures = [
  { definition: { id: 'generated-fixture', name: '生成映射协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'json', transport: 'MQTT', artifact: { generatedMapping: true } }] },
  { definition: { id: 'source-fixture', name: 'Go 源码协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'go-protocol-v2', transport: 'TCP', artifact: { build: { kind: 'go-source' }, filename: 'protocol.go' } }] },
  { definition: { id: 'legacy-fixture', name: '历史协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'json', transport: 'HTTP', artifact: {} }] },
  { definition: { id: 'empty-fixture', name: '待创建版本的协议' }, releases: [] }
]
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'

let browser
try {
  browser = await startBrowser()
  const { call, evaluate, until } = browser

  // 登录、权限轮询和协议目录均返回合成数据，不连接真实业务服务。
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) // 固定桌面尺寸以检查表格和导航布局。
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    const protocolFixture = ${JSON.stringify({ items: fixtures, total: fixtures.length })};
    window.fetch = (input, options) => {
      const path = String(input);
      const body = path === '/api/v1/auth/login' ? { accessToken: 'fixture-token', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me' ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/events' ? { permissions: ['*'], alarms: [], devices: [] }
        : path === '/api/v2/protocols' ? protocolFixture : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("Boolean(document.querySelector('.login-form button[type=submit]'))"))
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })) })()")
  await evaluate("document.querySelector('.login-form button[type=submit]').click()")
  await until(() => evaluate("Boolean(document.querySelector('.nav-item[aria-label=\"设备通信协议\"]'))"))
  await evaluate("document.querySelector('.nav-item[aria-label=\"设备通信协议\"]').click()")
  await until(() => evaluate("document.querySelectorAll('.n-data-table-tr').length >= 4")).catch(async error => { throw new Error(`${error.message}: ${await evaluate('document.body.innerText.slice(0, 800)')}`) })
  const screenshot = await call('Page.captureScreenshot', { format: 'png' }) // 截取协议页用于视觉核对。
  await writeFile(join(tmpdir(), 'iot-naive-protocol.png'), Buffer.from(screenshot.data, 'base64')) // 截图保存在临时目录，不进入代码仓库。

  // 每个协议的行操作一致，版本详情再按制品能力显示专项操作。
  const rows = "[...document.querySelectorAll('.n-data-table-tr')].filter(row => row.querySelector('td') && !row.closest('.ui-dialog'))"
  const labels = await evaluate(`${rows}.map(row => row.querySelector('.row-actions')?.innerText.replace(/\\s+/g, ' ').trim())`)
  assert.deepEqual(labels, Array(4).fill('管理版本 删除协议'), `操作列不一致：${labels.join(' / ')}`)
  const dialog = "[...document.querySelectorAll('.ui-dialog')].find(item => item.getClientRects().length)"
  const closeDialog = async () => {
    await evaluate(`${dialog}?.querySelector('.n-base-close')?.click()`)
    await until(() => evaluate(`!${dialog}`))
    await delay(120)
  }
  for (const [index, expected] of ['解析测试', '源码', '暂无可执行操作', ''].entries()) {
    await evaluate(`${rows}[${index}].querySelector('.row-actions button').click()`)
    await until(() => evaluate(`${dialog}?.innerText.includes('版本管理')`))
    if (!expected) {
      assert.ok(await evaluate(`${dialog}.innerText.includes('暂无版本')`), '无版本协议未显示明确状态')
      await closeDialog()
      continue
    }
    await evaluate(`[...${dialog}.querySelectorAll('button')].find(button => button.innerText.trim() === '详情').click()`)
    await until(() => evaluate(`${dialog}?.querySelector('.release-detail-actions')`))
    const detail = await evaluate(`${dialog}.querySelector('.release-detail-actions').innerText`)
    assert.ok(detail.includes(expected), `${fixtures[index].definition.name} 的版本详情缺少“${expected}”：${detail}`)
    await closeDialog()
  }
  console.log('PASS: 协议行操作一致，三种版本详情按制品显示专项操作，无版本协议显示明确状态')
} finally {
  await browser?.close()
}
