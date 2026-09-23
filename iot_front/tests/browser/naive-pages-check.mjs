// 在隔离浏览器中打开全部主页面，检查 Naive UI 迁移后的可见结构。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' /* 使用本机 Edge 的独立会话。 */
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173' /* 只访问合成数据预览服务。 */
const pages = ['运行总览', '协议管理', '产品管理', '设备管理', '接入网关', '接入测试', '摄像头映射', '告警中心', '智能巡检', '原始报文', '告警规则', '模型管理', '智能助手', '知识库', '备份中心', '用户与权限'] /* 检查全部主菜单。 */
const profile = await mkdtemp(join(tmpdir(), 'iot-naive-pages-')) /* 隔离浏览器本地数据。 */
const child = spawn(browser, ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' }) /* 启动临时浏览器。 */
const delay = ms => new Promise(resolve => setTimeout(resolve, ms)) /* 给页面渲染留出短暂时间。 */
async function until(check) { for (let i = 0; i < 100; i++) { const value = await check(); if (value) return value; await delay(100) } throw new Error('页面等待超时') } /* 等待确定的页面状态。 */

let socket /* 保存本次 CDP 连接。 */
try { /* 所有浏览器资源在 finally 中释放。 */
  const port = await until(async () => { try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null } }) /* 获取临时调试端口。 */
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json() /* 查找浏览器页面。 */
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl) /* 连接临时页面。 */
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject }) /* 等待连接可用。 */
  let id = 0 /* 递增 CDP 请求标识。 */
  const pending = new Map() /* 关联请求与响应。 */
  const failures = [] /* 收集页面脚本错误。 */
  const warnings = [] /* 收集框架组件警告。 */
  socket.onmessage = event => { const message = JSON.parse(event.data); if (message.method === 'Runtime.exceptionThrown') failures.push(message.params.exceptionDetails?.exception?.description || message.params.exceptionDetails?.text); if (message.method === 'Runtime.consoleAPICalled' && ['warning', 'error'].includes(message.params.type)) warnings.push(message.params.args.map(arg => arg.value || arg.description || '').join(' ')); if (!message.id) return; const entry = pending.get(message.id); pending.delete(message.id); message.error ? entry.reject(new Error(message.error.message)) : entry.resolve(message.result) } /* 分发事件和请求结果。 */
  const call = (method, params = {}) => new Promise((resolve, reject) => { const next = ++id; pending.set(next, { resolve, reject }); socket.send(JSON.stringify({ id: next, method, params })) }) /* 发送 CDP 请求。 */
  const evaluate = async expression => { const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (value.exceptionDetails) throw new Error(value.exceptionDetails.exception?.description || value.exceptionDetails.text); return value.result.value } /* 读取页面可见状态。 */

  await call('Page.enable') /* 开启导航与截图。 */
  await call('Runtime.enable') /* 收集未处理的脚本异常。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 固定桌面视口。 */
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, options) => {
      const path = String(input);
      const body = path === '/api/v1/auth/login' ? { accessToken: 'fixture-token', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me' ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/events' ? { permissions: ['*'], alarms: [], devices: [] }
        : path.startsWith('/api/v1/dashboard?') ? { devices: 0, online: 0, activeAlarms: 0, highAlarms: 0, states: {}, products: [], trend: [], levels: [], updatedAt: Date.now() } : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` }) /* 注入仅供界面检查使用的身份与权限。 */
  await call('Page.navigate', { url: origin }) /* 打开合成数据前端。 */
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))")) /* 等待登录页。 */
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })); document.querySelector('.login-form button[type=submit]').click() })()") /* 完成夹具登录。 */
  await until(() => evaluate("document.querySelectorAll('.menu-item').length >= 16")) /* 确认全部主菜单可见。 */
  for (const name of pages) { /* 逐页检查标题、正文和脚本异常。 */
    await evaluate(`document.querySelector('.menu-item[aria-label=${JSON.stringify(name)}]').click()`) /* 打开目标页面。 */
    await until(() => evaluate(`document.querySelector('.page-context h1')?.innerText === ${JSON.stringify(name)}`)) /* 确认当前页面标题。 */
    await delay(180) /* 等待异步页面的首屏渲染。 */
    const text = await evaluate("document.querySelector('.main-content')?.innerText.trim() || ''") /* 读取可见正文。 */
    assert.ok(text.length > name.length, `${name} 缺少业务内容`) /* 防止页面只显示标题。 */
    if (['运行总览', '协议管理', '产品管理', '设备管理', '告警中心', '智能助手'].includes(name)) { const capture = await call('Page.captureScreenshot', { format: 'png' }); await writeFile(join(tmpdir(), `iot-naive-${pages.indexOf(name)}.png`), Buffer.from(capture.data, 'base64')) } /* 留存代表性页面的临时截图。 */
  } /* 结束页面遍历。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"产品管理\"]').click()") /* 打开产品管理检查表单。 */
  await until(() => evaluate("document.querySelector('.page-context h1')?.innerText === '产品管理'")) /* 等待页面切换。 */
  await evaluate("[...document.querySelectorAll('.page-toolbar button')].find(button => button.textContent.includes('新建产品')).click()") /* 打开新建产品弹窗。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('产品名称')))")) /* 确认 Naive UI 弹窗显示字段。 */
  await evaluate("[...document.querySelectorAll('.n-modal .n-form-item')].find(item => item.innerText.includes('设备分类')).querySelector('.n-base-selection').click()") /* 展开设备分类下拉框。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option => option.getClientRects().length))")) /* 确认选项实际可交互。 */
  await evaluate("[...document.querySelectorAll('.n-base-select-option')].find(option => option.getClientRects().length).click()") /* 选择首个设备分类。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 关闭产品表单。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")) /* 确认关闭完成。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"设备管理\"]').click()") /* 打开设备管理检查详情抽屉。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button => button.innerText.includes('连接详情')))")) /* 等待示例设备行。 */
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(button => button.innerText.includes('连接详情')).click()") /* 打开设备连接详情。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-drawer')].find(drawer => drawer.getClientRects().length && drawer.innerText.includes('设备连接与数据')))")) /* 确认抽屉显示。 */
  await evaluate("document.querySelector('.n-drawer .n-base-close').click()") /* 关闭受控抽屉。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-drawer')].some(drawer => drawer.getClientRects().length)")) /* 确认抽屉解除挂载。 */
  await evaluate("[...document.querySelectorAll('.page-toolbar button')].find(button => button.innerText.includes('添加独立设备')).click()") /* 打开设备编辑表单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('设备角色')))")) /* 确认角色控件显示。 */
  await evaluate("document.querySelector('.n-modal .n-collapse-item__header-main').click()") /* 展开更多设置。 */
  await until(() => evaluate("Boolean(document.querySelector('.n-modal [role=switch]'))")) /* 等待设备状态开关。 */
  const initialSwitch = await evaluate("document.querySelector('.n-modal [role=switch]').getAttribute('aria-checked')") /* 记录原状态。 */
  await evaluate("document.querySelector('.n-modal [role=switch]').click()") /* 切换字符串型设备状态。 */
  await until(() => evaluate(`document.querySelector('.n-modal [role=switch]').getAttribute('aria-checked') !== ${JSON.stringify(initialSwitch)}`)) /* 确认状态反转。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 离开未保存的设备表单。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")) /* 确认弹窗关闭。 */
  await evaluate("document.querySelector('[aria-label=\"打开用户菜单\"]').click()") /* 打开账户菜单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-dropdown-option')].find(option => option.getClientRects().length && option.innerText.includes('退出登录')))")) /* 确认账户操作可见。 */
  await evaluate("[...document.querySelectorAll('.n-dropdown-option')].find(option => option.getClientRects().length && option.innerText.includes('退出登录')).querySelector('.n-dropdown-option-body').click()") /* 执行退出登录。 */
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))")) /* 确认退出后返回登录页。 */
  assert.deepEqual(failures, [], `页面脚本异常：${failures.join(' | ')}`) /* 不接受未处理的页面错误。 */
  if (warnings.length) console.log(`页面警告 ${warnings.length} 条：${[...new Set(warnings)].slice(0, 8).join(' | ')}`) /* 输出需继续排查的框架警告。 */
  console.log(`PASS: ${pages.length} 个主页面正常渲染`) /* 报告界面检查结果。 */
} finally { /* 清理浏览器与临时配置。 */
  socket?.close() /* 关闭调试连接。 */
  const exited = new Promise(resolve => { if (child.exitCode !== null || child.signalCode !== null) resolve(); else child.once('exit', resolve) }) /* 等待浏览器结束。 */
  child.kill() /* 停止临时浏览器。 */
  const forceStop = setTimeout(() => child.kill('SIGKILL'), 3000) /* 防止浏览器进程残留。 */
  forceStop.unref() /* 不延长测试进程生命周期。 */
  await exited /* 确认浏览器已经结束。 */
  clearTimeout(forceStop) /* 取消强制停止定时器。 */
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }) /* 清理本次用户目录。 */
} /* 结束资源清理。 */
