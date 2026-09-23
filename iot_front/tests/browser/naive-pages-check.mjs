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
        : path === '/api/v1/mqtt/token' ? { websocketUrl:'ws://127.0.0.1:1', username:'fixture', token:'fixture', subscriptions:[] }
        : path === '/api/v2/protocol-source-template' ? { compilerAvailable:true, targetPlatforms:['linux-amd64','linux-arm64','windows-amd64','windows-arm64','darwin-amd64','darwin-arm64'] }
        : path.startsWith('/api/v1/raw-messages?') ? { items: [{ messageId: 'raw-demo', receivedAt: Date.now(), productId: 'product-demo', deviceId: 'device-demo', protocol: 'MQTT', parsed: true, parsedMessageType: 'PROPERTY_REPORT', payloadSize: 4, payloadHash: 'fixture-hash' }], total: 1 }
        : path === '/api/v1/raw-messages/raw-demo' ? { parseStatus: 'PARSED', message: { messageId: 'raw-demo', deviceId: 'device-demo', productId: 'product-demo', payload: 'AA01', receivedAt: Date.now(), protocol: 'MQTT', payloadFormat: 'hex' }, standardMessage: { messageType: 'PROPERTY_REPORT', properties: { temperature: 42 } }, archive: { payloadHash: 'fixture-hash' } }
        : path.startsWith('/api/v1/alarms?') ? { items: [{ alarmId:'alarm-demo', deviceId:'device-demo', deviceName:'测试设备', alarmType:'MANUAL_ALARM', alarmLevel:'HIGH', status:'ACTIVE', source:'device', lastTriggeredAt:Date.now() }], total:1 }
        : path === '/api/v1/alarms/alarm-demo' ? { alarmId:'alarm-demo', deviceId:'device-demo', deviceName:'测试设备', alarmType:'MANUAL_ALARM', alarmLevel:'HIGH', status:'ACTIVE', source:'device', firstTriggeredAt:Date.now(), lastTriggeredAt:Date.now(), triggerCount:1, diagnosticLines:Array.from({length:80},(_,index)=>'第 '+(index+1)+' 条诊断记录') }
        : path === '/api/v1/ai/alarm-analysis/alarm-demo' ? { summary:'设备多次触发故障告警，需要检查现场状态', riskLevel:'MEDIUM', confidence:0.85, possibleReasons:['设备状态异常','通信链路抖动'], suggestions:['检查设备电源和网络','核对告警历史'], model:'fixture', createdAt:Date.now() }
        : path.startsWith('/api/v1/rules?') ? { items: [{ id:'rule-demo', name:'演示规则', alarmType:'DEVICE_FAULT', level:'HIGH', enabled:true, conditions:[], actions:[] }], total:1 }
        : path === '/api/v1/access/users' ? { tenantId:'fixture', items:[{ username:'operator-demo', displayName:'操作员', enabled:true, roleIds:[], deviceScope:'none' }] }
        : path === '/api/v1/access/roles' || path === '/api/v1/access/permissions' || path === '/api/v1/access/device-options' ? { items:[] }
        : path.startsWith('/api/v1/backups?') ? { items:[{ id:'backup-demo', type:'DEVICE_DAILY', status:'COMPLETED', startedAt:Date.now(), completedAt:Date.now() }], total:1 }
        : path === '/api/v1/backups/backup-demo' ? { id:'backup-demo', type:'DEVICE_DAILY', status:'COMPLETED', startedAt:Date.now(), completedAt:Date.now(), details:{}, objectKey:'backup/manifest.json' }
        : path.startsWith('/api/v1/backups/backup-demo/files?') ? { artifacts:[{ component:'原始报文', filename:'raw-messages.jsonl.gz', size:313, checksum:'fixture' }], total:1, components:{ rawMessages:{ records:1 } } }
        : path.startsWith('/api/v1/dashboard?') ? { devices: 3, online: 2, activeAlarms: 2, highAlarms: 1, states: { ONLINE:2, OFFLINE:1 }, products: [{ key:'product-demo', name:'演示烟感', count:3 }], trend: [], levels: { HIGH:1, MEDIUM:1 }, updatedAt: Date.now() } : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` }) /* 注入仅供界面检查使用的身份与权限。 */
  await call('Page.navigate', { url: origin }) /* 打开合成数据前端。 */
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))")).catch(async error => { throw new Error(`${error.message}；表单=${await evaluate("document.querySelector('.login-form')?.innerHTML.slice(0,500)")}；异常=${failures.slice(0,2).join(' | ')}；警告=${warnings.slice(0,3).join(' | ')}`) }) /* 等待登录页并报告首屏脚本异常。 */
  const loginBrand = await evaluate("(() => {const root=getComputedStyle(document.documentElement),button=document.querySelector('.login-submit');return {navy:root.getPropertyValue('--brand-navy').trim(),primary:root.getPropertyValue('--primary').trim(),button:getComputedStyle(button).backgroundColor}})()") /* 读取最终计算后的登录页主色。 */
  assert.ok(loginBrand.navy==='#13386c' && loginBrand.primary==='#13386c' && loginBrand.button==='rgb(19, 56, 108)', `登录页未使用品牌深蓝主色：${JSON.stringify(loginBrand)}`) /* 登录按钮与主题变量都应采用品牌深蓝。 */
  const loginCapture = await call('Page.captureScreenshot', { format:'png' }) /* 留存登录页视觉检查截图。 */
  await writeFile(join(tmpdir(), 'iot-brand-login.png'), Buffer.from(loginCapture.data, 'base64')) /* 保存登录页截图。 */
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })); document.querySelector('.login-form button[type=submit]').click() })()") /* 完成夹具登录。 */
  await until(() => evaluate("document.querySelectorAll('.menu-item').length >= 16")) /* 确认全部主菜单可见。 */
  const asideBrand = await evaluate("(() => {const aside=document.querySelector('.app-aside'),menu=aside.querySelector('.menu-item:not(.active)'),brand=aside.querySelector('.brand-logo');return {background:getComputedStyle(aside).backgroundImage,menu:getComputedStyle(menu).color,logo:getComputedStyle(brand).backgroundColor}})()") /* 读取实际渲染的导航颜色。 */
  assert.ok(asideBrand.background.includes('rgb(19, 56, 108)') && asideBrand.menu==='rgb(220, 232, 245)' && asideBrand.logo==='rgb(255, 255, 255)', `深蓝侧栏、浅色菜单或白底品牌标识未生效：${JSON.stringify(asideBrand)}`) /* 检查导航可读性及白底 logo。 */
  await evaluate("document.querySelector('.collapse-button').click()") /* 验证折叠导航。 */
  assert.ok(await evaluate("(() => {const aside=document.querySelector('.app-aside'),menu=aside.querySelector('.menu-item:not(.active)');return aside.classList.contains('is-collapsed') && menu.getBoundingClientRect().width>0 && getComputedStyle(menu).color==='rgb(220, 232, 245)'})()"), '折叠态导航图标不可见') /* 折叠后仍保留可读菜单。 */
  await evaluate("document.querySelector('.collapse-button').click()") /* 恢复完整侧栏。 */
  for (const name of pages) { /* 逐页检查标题、正文和脚本异常。 */
    await evaluate(`document.querySelector('.menu-item[aria-label=${JSON.stringify(name)}]').click()`) /* 打开目标页面。 */
    await until(() => evaluate(`document.querySelector('.page-context h1')?.innerText === ${JSON.stringify(name)}`)).catch(async error => { throw new Error(`${name} 页面未能切换：${error.message}；当前 ${await evaluate("document.querySelector('.page-context h1')?.innerText || document.body.innerText.slice(0, 200)")}；异常 ${failures.slice(0, 2).join(' | ')}`) }) /* 确认当前页面标题。 */
    await delay(180) /* 等待异步页面的首屏渲染。 */
    const text = await evaluate("document.querySelector('.main-content')?.innerText.trim() || ''") /* 读取可见正文。 */
    assert.ok(text.length > name.length, `${name} 缺少业务内容`) /* 防止页面只显示标题。 */
    if (name==='运行总览') assert.ok(await evaluate("(() => {const cards=[...document.querySelectorAll('.stat-card')];return cards.length===4 && cards.map(card=>getComputedStyle(card).borderTopColor).join('|')==='rgb(19, 56, 108)|rgb(22, 163, 74)|rgb(243, 129, 40)|rgb(220, 38, 38)' && getComputedStyle(document.querySelector('.device-ring circle')).transitionProperty.includes('stroke-dashoffset')})()"), '仪表盘色条或圆环过渡未生效') /* 四种 KPI 语义色及圆环过渡应同时出现。 */
    if (name==='设备管理') assert.ok(await evaluate("(() => {const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(item=>item.innerText.includes('一层走廊烟感'));return row && row.querySelectorAll('.n-tag').length===1 && row.querySelector('.device-enabled-state')?.innerText.includes('已启用') && row.querySelector('.device-role-text')?.innerText.includes('直接设备')})()"), '设备列表仍堆叠多个状态标签') /* 仅运行状态保留标签。 */
    if (['运行总览', '协议管理', '产品管理', '设备管理', '告警中心', '智能助手'].includes(name)) { const capture = await call('Page.captureScreenshot', { format: 'png' }); await writeFile(join(tmpdir(), `iot-naive-${pages.indexOf(name)}.png`), Buffer.from(capture.data, 'base64')) } /* 留存代表性页面的临时截图。 */
  } /* 结束页面遍历。 */
  await call('Emulation.setDeviceMetricsOverride', { width:390, height:844, deviceScaleFactor:1, mobile:true }) /* 检查手机底部导航断点。 */
  await delay(250) /* 等待侧栏宽度过渡完成。 */
  const mobileBrand = await evaluate("(() => {const aside=document.querySelector('.app-aside'),menu=aside.querySelector('.menu-item:not(.active)'),r=aside.getBoundingClientRect();return {bottom:r.bottom,width:r.width,viewport:innerWidth,height:innerHeight,background:getComputedStyle(aside).backgroundColor,menu:getComputedStyle(menu).color}})()") /* 读取手机导航的最终尺寸与颜色。 */
  assert.ok(mobileBrand.bottom<=mobileBrand.height+1 && mobileBrand.width>=mobileBrand.viewport-2 && mobileBrand.background==='rgb(19, 56, 108)' && mobileBrand.menu==='rgb(220, 232, 245)', `手机底部导航未沿用品牌色或布局溢出：${JSON.stringify(mobileBrand)}`) /* 窄屏仍可读取导航入口。 */
  await call('Emulation.setDeviceMetricsOverride', { width:1440, height:900, deviceScaleFactor:1, mobile:false }) /* 恢复桌面视口。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"协议管理\"]').click()") /* 检查上传源码弹窗的额外目标选择。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.page-toolbar button')].find(button=>button.innerText.includes('上传源码')))")) /* 等待协议工具栏。 */
  await evaluate("[...document.querySelectorAll('.page-toolbar button')].find(button=>button.innerText.includes('上传源码')).click()") /* 打开源码上传弹窗。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('编译选项')))")) /* 等待编译选项。 */
  const sourceLayout = await evaluate("(() => {const modal=document.querySelector('.source-upload-dialog'),file=modal.querySelector('.source-file-item'),templates=modal.querySelector('.source-template-panel'),options=modal.querySelector('.source-compile-options'),publish=modal.querySelector('.source-publish-panel'),footer=modal.querySelector('.source-submit-row'),r=e=>e.getBoundingClientRect();return {separateRows:r(templates).top>=r(file).bottom,optionsAfterTemplates:r(options).top>=r(templates).bottom,publishAfterOptions:r(publish).top>=r(options).bottom,templateButtons:templates.querySelectorAll('button').length,footerButton:footer.querySelector('button')?.innerText,withinModal:r(templates).left>=r(modal).left&&r(templates).right<=r(modal).right}})()") /* 检查文件、模板、选项与发布区分行展示。 */
  assert.ok(sourceLayout.separateRows && sourceLayout.optionsAfterTemplates && sourceLayout.publishAfterOptions && sourceLayout.withinModal && sourceLayout.templateButtons===2 && sourceLayout.footerButton.includes('上传、编译并发布'), `上传源码分组布局异常：${JSON.stringify(sourceLayout)}`) /* 模板操作不能再挤在文件名同一行。 */
  await evaluate("document.querySelector('.source-publish-panel .n-switch').click()") /* 切换到只保存已校验版本。 */
  assert.ok(await evaluate("(() => {const modal=document.querySelector('.source-upload-dialog');return modal.querySelector('.source-publish-panel').innerText.includes('稍后可在协议版本中发布') && modal.querySelector('.source-submit-row button').innerText.includes('上传、编译并校验')})()"), '仅校验模式的结果说明或提交按钮未同步更新') /* 操作结果须随发布开关变化。 */
  await evaluate("document.querySelector('.source-publish-panel .n-switch').click()") /* 恢复默认的立即发布。 */
  await delay(300) /* 等待弹窗动画结束，截图应呈现最终布局。 */
  const sourceCapture = await call('Page.captureScreenshot', { format:'png' }) /* 留存弹窗的合成数据截图供布局复核。 */
  await writeFile(join(tmpdir(), 'iot-source-upload-desktop.png'), Buffer.from(sourceCapture.data, 'base64')) /* 保存桌面视口截图。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-collapse-item__header-main').click()") /* 展开额外目标。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal .n-form-item')].find(item=>item.innerText.includes('额外编译目标') && item.getBoundingClientRect().height>0))")) /* 等待目标选择器真正展开。 */
  await delay(250) /* 等待折叠动画结束再检查滚动尺寸。 */
  assert.ok(await evaluate("(() => {const modal=document.querySelector('.source-upload-dialog'),select=modal.querySelector('.source-target-item .n-base-selection'),help=modal.querySelector('.source-target-help');return select && help && help.getBoundingClientRect().top>=select.getBoundingClientRect().bottom})()"), '额外编译目标说明仍与下拉框挤在同一行') /* 编译说明应位于选择器下方。 */
  await evaluate("[...document.querySelectorAll('.n-modal .n-form-item')].find(item=>item.innerText.includes('额外编译目标')).querySelector('.n-base-selection').click()") /* 展开多选菜单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option=>option.getClientRects().length))")) /* 等待可见选项。 */
  const targetOption = await evaluate("(() => {const option=[...document.querySelectorAll('.n-base-select-option')].find(item=>item.getClientRects().length),r=option.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;return {outsideModal:!option.closest('.n-modal'),x,y,visible:r.top>=0&&r.bottom<=innerHeight,hit:document.elementFromPoint(x,y)?.closest('.n-base-select-option')===option}})()") /* 检查菜单未被弹窗遮住。 */
  assert.ok(targetOption.outsideModal && targetOption.visible && targetOption.hit, `额外编译目标菜单被裁切或遮挡：${JSON.stringify(targetOption)}`) /* 选项必须能接收真实鼠标事件。 */
  await call('Input.dispatchMouseEvent', { type:'mousePressed', x:targetOption.x, y:targetOption.y, button:'left', clickCount:1 }) /* 用鼠标选择一个编译目标。 */
  await call('Input.dispatchMouseEvent', { type:'mouseReleased', x:targetOption.x, y:targetOption.y, button:'left', clickCount:1 }) /* 完成点击。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal .n-base-selection')].find(item=>item.innerText.includes('Linux')))")) /* 确认实际多选值已更新。 */
  await call('Emulation.setDeviceMetricsOverride', { width:1000, height:600, deviceScaleFactor:1, mobile:false }) /* 用短视口验证上传表单滚动。 */
  const sourceScroll = await evaluate("(() => {const modal=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length),body=modal.querySelector('.n-card-content');body.scrollTop=250;return {top:body.scrollTop,scrollHeight:body.scrollHeight,clientHeight:body.clientHeight,modalHeight:modal.getBoundingClientRect().height,overflow:getComputedStyle(body).overflowY,modalDisplay:getComputedStyle(modal).display,bodyFlex:getComputedStyle(body).flex,modalMaxHeight:getComputedStyle(modal).maxHeight,children:[...modal.children].map(e=>[e.className,e.getBoundingClientRect().height])}})()") /* 读取上传弹窗滚动范围。 */
  assert.ok(sourceScroll.top>0, `额外编译目标展开后上传源码正文无法滚动：${JSON.stringify(sourceScroll)}`) /* 弹窗正文仍可上下滚动。 */
  const sourceWheel = await evaluate("(() => {const body=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-card-content'),r=body.getBoundingClientRect();body.scrollTop=0;return {x:r.left+20,y:r.top+Math.min(90,r.height/2)}})()") /* 定位弹窗正文的滚轮测试点。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:sourceWheel.x, y:sourceWheel.y, deltaX:0, deltaY:300 }) /* 验证向下滚动。 */
  await until(() => evaluate("document.querySelector('.n-modal .n-card-content').scrollTop>0")) /* 等待正文滚动。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:sourceWheel.x, y:sourceWheel.y, deltaX:0, deltaY:-300 }) /* 验证向上滚动。 */
  await until(() => evaluate("document.querySelector('.n-modal .n-card-content').scrollTop===0")) /* 确认返回顶部。 */
  await call('Emulation.setDeviceMetricsOverride', { width:1440, height:900, deviceScaleFactor:1, mobile:false }) /* 恢复桌面视口。 */
  await call('Emulation.setDeviceMetricsOverride', { width:390, height:844, deviceScaleFactor:1, mobile:true }) /* 检查窄屏模板与提交按钮的换行。 */
  const mobileSourceLayout = await evaluate("(() => {const modal=document.querySelector('.source-upload-dialog'),panel=modal.querySelector('.source-template-panel'),footer=modal.querySelector('.source-submit-row'),button=footer.querySelector('button'),r=e=>e.getBoundingClientRect();return {viewport:innerWidth,documentWidth:document.documentElement.scrollWidth,modalRight:r(modal).right,panelRight:r(panel).right,buttonRight:r(button).right,footerWidth:r(footer).width,buttonWidth:r(button).width}})()") /* 读取手机视口中各分组的宽度。 */
  assert.ok(mobileSourceLayout.documentWidth<=mobileSourceLayout.viewport+2 && mobileSourceLayout.modalRight<=mobileSourceLayout.viewport+1 && mobileSourceLayout.panelRight<=mobileSourceLayout.viewport+1 && mobileSourceLayout.buttonRight<=mobileSourceLayout.viewport+1, `上传源码窄屏横向溢出：${JSON.stringify(mobileSourceLayout)}`) /* 窄屏仍可完整看到模板与提交按钮。 */
  await call('Emulation.setDeviceMetricsOverride', { width:1440, height:900, deviceScaleFactor:1, mobile:false }) /* 恢复桌面视口。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭上传弹窗。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待弹窗关闭。 */
  for (const [page, label] of [['告警中心', '关闭告警'], ['告警规则', '删除'], ['用户与权限', '删除']]) { /* 检查三个列表中的危险操作可见。 */
    await evaluate(`document.querySelector('.menu-item[aria-label=${JSON.stringify(page)}]').click()`) /* 打开目标列表。 */
    await until(() => evaluate(`Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button => button.innerText.trim() === ${JSON.stringify(label)}))`)) /* 等待目标按钮。 */
    const buttonColor = await evaluate(`(() => {const button=[...document.querySelectorAll('.n-data-table-tbody button')].find(item=>item.innerText.trim()===${JSON.stringify(label)}),s=getComputedStyle(button);return {color:s.color,background:s.backgroundColor,disabled:button.disabled,className:button.className}})()`) /* 读取真实颜色。 */
    assert.ok(buttonColor.color!==buttonColor.background && buttonColor.color!=='rgb(255, 255, 255)', `${page}的${label}按钮文字不可见：${JSON.stringify(buttonColor)}`) /* 危险按钮文字不能与浅色背景融为一体。 */
  } /* 结束危险操作检查。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"告警中心\"]').click()") /* 复现告警详情的长报文滚动。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('查看详情')))")) /* 等待告警列表。 */
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('查看详情')).click()") /* 打开告警详情。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('diagnosticLines')))")) /* 等待长报文显示。 */
  await until(() => evaluate("Boolean(document.querySelector('.alarm-detail-dialog .analysis-grid'))")) /* 等待内层研判卡片加载，复现滚轮停住的区域。 */
  assert.ok(await evaluate("(() => {const modal=document.querySelector('.alarm-detail-dialog'),body=modal.querySelector(':scope > .n-card-content'),inner=modal.querySelector('.top-gap > .n-card-content');return body && inner && getComputedStyle(body).overflowY==='auto' && getComputedStyle(inner).overflowY==='visible'})()"), '告警研判卡片错误地成为第二个滚动容器') /* 详情只允许最外层正文接收滚轮。 */
  const analysisWheel = await evaluate("(() => {const modal=document.querySelector('.alarm-detail-dialog'),body=modal.querySelector(':scope > .n-card-content'),inner=modal.querySelector('.top-gap > .n-card-content');inner.scrollIntoView({block:'center'});const r=inner.getBoundingClientRect();return {x:r.left+Math.min(100,r.width/2),y:Math.max(120,Math.min(innerHeight-120,r.top+Math.min(80,r.height/2))),top:body.scrollTop}})()") /* 把鼠标放在实际研判内容上。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:analysisWheel.x, y:analysisWheel.y, deltaX:0, deltaY:120 }) /* 第一次滚轮下滑。 */
  await until(() => evaluate(`document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop>${analysisWheel.top}`)) /* 外层正文应响应第一次滚轮。 */
  const analysisFirstTop = await evaluate("document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop") /* 记录第一次滚动位置。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:analysisWheel.x, y:analysisWheel.y, deltaX:0, deltaY:120 }) /* 在同一位置连续下滑。 */
  await until(() => evaluate(`document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop>${analysisFirstTop}`)) /* 第二次滚轮不能被内层卡片截断。 */
  assert.ok(await evaluate("(() => {const modal=[...document.querySelectorAll('.n-modal')].find(item=>item.getClientRects().length),pre=modal.querySelector('pre');return pre.scrollHeight<=pre.clientHeight+1 && getComputedStyle(pre).overflowY==='visible'})()"), '告警详情的 JSON 报文仍单独截取滚轮') /* 详情只能保留外层正文滚动。 */
  const alarmWheel = await evaluate("(() => {const modal=[...document.querySelectorAll('.n-modal')].find(item=>item.getClientRects().length),body=modal.querySelector('.n-card-content'),pre=modal.querySelector('pre');body.scrollTop=Math.max(0,pre.offsetTop-160);const rect=pre.getBoundingClientRect();return {x:rect.left+Math.min(100,rect.width/2),y:Math.max(120,Math.min(innerHeight-120,rect.top+80)),top:body.scrollTop}})()") /* 将滚轮定位在报文区域。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:alarmWheel.x, y:alarmWheel.y, deltaX:0, deltaY:260 }) /* 在报文上向下滚动。 */
  await until(() => evaluate(`document.querySelector('.n-modal .n-card-content').scrollTop>${alarmWheel.top}`)) /* 外层详情应继续滚动。 */
  await call('Input.dispatchMouseEvent', { type:'mouseWheel', x:alarmWheel.x, y:alarmWheel.y, deltaX:0, deltaY:-260 }) /* 在报文上向上滚动。 */
  await until(() => evaluate(`document.querySelector('.n-modal .n-card-content').scrollTop<=${alarmWheel.top}`)) /* 验证双向滚动。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭告警详情。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待详情关闭。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"产品管理\"]').click()") /* 检查协议回滚弹窗。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('协议版本')))")) /* 等待产品数据。 */
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('协议版本')).click()") /* 打开协议版本弹窗。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('回滚上一版本')))")) /* 等待弹窗页脚。 */
  assert.ok(await evaluate("(() => {const modal=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length),buttons=[...modal.querySelectorAll('button')].filter(b=>['回滚上一版本','绑定协议'].includes(b.innerText.trim())),a=buttons[0].getBoundingClientRect(),b=buttons[1].getBoundingClientRect(),style=getComputedStyle(buttons[0]);return buttons.length===2 && a.right+6<=b.left && style.color!=='rgb(255, 255, 255)'})()"), '回滚上一版本按钮不可读或紧贴绑定按钮') /* 弹窗页脚保留文字与间距。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭绑定弹窗。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待弹窗关闭。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"接入网关\"]').click()") /* 检查接入网关表单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.page-toolbar button')].find(button=>button.innerText.includes('新建网关')))")) /* 等待工具栏。 */
  await evaluate("[...document.querySelectorAll('.page-toolbar button')].find(button=>button.innerText.includes('新建网关')).click()") /* 打开网关表单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('保存接入网关')))")) /* 等待表单内容。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-collapse-item__header-main')?.click()") /* 展开网关高级配置。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1000, height: 600, deviceScaleFactor: 1, mobile: false }) /* 用较短视口检查长表单。 */
  const gatewayLayout = await evaluate("(() => {const m=[...document.querySelectorAll('.n-modal')].find(x=>x.getClientRects().length),body=m.querySelector('.n-card-content'),save=[...m.querySelectorAll('button')].find(b=>b.innerText.includes('保存接入网关'));body.scrollTop=300;return {scrollHeight:body.scrollHeight,clientHeight:body.clientHeight,scrollTop:body.scrollTop,buttonRight:save.getBoundingClientRect().right,modalRight:m.getBoundingClientRect().right}})()") /* 读取网关表单几何信息。 */
  assert.ok(gatewayLayout.scrollTop>0 && gatewayLayout.buttonRight<=gatewayLayout.modalRight-12, `网关编辑表单无法滚动或保存按钮错位：${JSON.stringify(gatewayLayout)}`) /* 长表单保持可滚动且按钮在弹窗内。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭网关表单。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待表单关闭。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"备份中心\"]').click()") /* 检查备份详情说明。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('详情 / 文件')))")) /* 等待备份行。 */
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('详情 / 文件')).click()") /* 打开备份详情。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal .section-heading span')].find(item=>item.innerText.includes('清单中的每个文件')))")) /* 等待说明文字。 */
  assert.ok(await evaluate("(() => {const text=[...document.querySelectorAll('.n-modal .section-heading span')].find(item=>item.innerText.includes('清单中的每个文件'));return getComputedStyle(text).color!=='rgb(242, 242, 244)'})()"), '备份详情说明文字与白色背景过于接近') /* 明确使用深色辅助文本。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭备份详情。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待详情关闭。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"智能助手\"]').click()") /* 检查智能助手滚动区。 */
  await until(() => evaluate("Boolean(document.querySelector('.control-scroll') && document.querySelector('.chat-log'))")) /* 等待双栏内容。 */
  assert.ok(await evaluate("(() => {const left=document.querySelector('.control-scroll');left.scrollTop=200;return left.scrollTop>0})()"), '智能助手运行参数无法向下滚动') /* 左侧长配置须可达。 */
  assert.ok(await evaluate("(() => {const log=document.querySelector('.chat-log');for(let i=0;i<30;i++){const item=document.createElement('p');item.textContent='滚动测试';log.append(item)}log.scrollTop=200;return log.scrollTop>0})()"), '智能助手对话记录无法向下滚动') /* 对话滚动容器需保持有效。 */
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
  await evaluate("document.querySelector('.menu-item[aria-label=\"原始报文\"]').click()") /* 打开带解析结果的示例报文。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(row => row.innerText.includes('raw-demo')))")) /* 确认列表取得样本。 */
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(row => row.innerText.includes('raw-demo')).querySelector('button').click()") /* 查看报文详情。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('报文详情与解析结果')))")) /* 等待详情弹窗。 */
  assert.ok(await evaluate("document.querySelector('.n-modal')?.innerText.includes('标准解析结果') && document.querySelector('.n-modal')?.innerText.includes('原始报文')"), '报文详情缺少原文与解析结果标签') /* 两种记录都须可访问。 */
  assert.ok(await evaluate("document.querySelector('.n-modal')?.innerText.includes('temperature')"), '报文详情未显示解析后的字段') /* 默认展示解析内容。 */
  await evaluate("[...document.querySelectorAll('.n-modal .n-tabs-tab')].find(tab => tab.innerText.includes('原始报文')).click()") /* 切换到原始记录。 */
  await until(() => evaluate("document.querySelector('.n-modal')?.innerText.includes('AA01')")) /* 确认原始载荷可读。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 关闭报文详情。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"运行总览\"]').click()") /* 检查总览页交互。 */
  await until(() => evaluate("Boolean(document.querySelector('.dashboard-page .n-radio-group'))")) /* 等待趋势切换。 */
  assert.ok(await evaluate("(() => {const card=document.querySelector('.stat-card');const label=card.querySelector('span').getBoundingClientRect(),value=card.querySelector('strong').getBoundingClientRect(),note=card.querySelector('small').getBoundingClientRect();return label.bottom<value.top && value.bottom<note.top})()"), '总览统计卡片文字相互重叠') /* 数值和说明必须分行显示。 */
  assert.ok(await evaluate("document.querySelector('.app-shell').getBoundingClientRect().bottom <= innerHeight + 1"), '工作区超过视口高度，页面无法上下滚动') /* 工作区必须受视口约束。 */
  assert.ok(await evaluate("(() => {const e=document.querySelector('.main-content');e.scrollTop=200;return e.scrollTop>0})()"), '总览内容无法纵向滚动') /* 超长页面须可滚动。 */
  await evaluate("document.querySelector('.main-content').scrollTop=0") /* 恢复首屏。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 1000, y: 650, deltaX: 0, deltaY: 320 }) /* 模拟向下滚动。 */
  await until(() => evaluate("document.querySelector('.main-content').scrollTop > 0")) /* 确认鼠标滚轮可用。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 1000, y: 650, deltaX: 0, deltaY: -320 }) /* 模拟向上滚动。 */
  await until(() => evaluate("document.querySelector('.main-content').scrollTop === 0")) /* 确认双向滚动。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 100, y: 550, deltaX: 0, deltaY: 300 }) /* 检查侧栏向下滚动。 */
  await until(() => evaluate("document.querySelector('.menu-scroll').scrollTop > 0")) /* 确认底部菜单可达。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 100, y: 550, deltaX: 0, deltaY: -300 }) /* 检查侧栏向上滚动。 */
  await until(() => evaluate("document.querySelector('.menu-scroll').scrollTop === 0")) /* 确认侧栏双向滚动。 */
  assert.ok(await evaluate("(() => {const b=[...document.querySelectorAll('.dashboard-page .n-radio-button')];return b.length===2 && getComputedStyle(b[0]).backgroundColor!==getComputedStyle(b[1]).backgroundColor})()"), '7 天与 30 天切换按钮缺少清晰的选中样式') /* 趋势切换状态须明确。 */
  await evaluate("[...document.querySelectorAll('.dashboard-page .n-radio-button')][1].click()") /* 切换到近 30 天。 */
  await until(() => evaluate("[...document.querySelectorAll('.dashboard-page .n-radio-button')][1].classList.contains('n-radio-button--checked')")) /* 确认选中状态同步。 */
  await evaluate("document.querySelector('[aria-label=\"告警提醒设置\"]').click()") /* 检查设置弹窗。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('告警提醒设置')))")) /* 等待设置弹窗。 */
  assert.ok(await evaluate("document.querySelector('.n-modal')?.innerText.includes('当前静默时段：')"), '告警提醒设置缺少静默时段说明') /* 提示标题必须显示。 */
  await delay(250) /* 等待弹窗动画结束后检查布局。 */
  const settingsCapture = await call('Page.captureScreenshot', { format: 'png' }); await writeFile(join(tmpdir(), 'iot-naive-settings.png'), Buffer.from(settingsCapture.data, 'base64')) /* 留存设置弹窗的合成数据截图。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }) /* 检查窄屏设置。 */
  assert.ok(await evaluate("(() => {const m=document.querySelector('.n-modal'),r=m.getBoundingClientRect();return r.left>=0 && r.right<=innerWidth+1 && [...m.querySelectorAll('.alert-setting-row,.alert-quiet-times,.n-time-picker')].every(e=>{const c=e.getBoundingClientRect();return c.left>=r.left+6 && c.right<=r.right-6})})()"), '窄屏告警设置内容被裁切') /* 控件须留在弹窗范围内。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 关闭设置弹窗。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")) /* 等待关闭。 */
  assert.ok(await evaluate("document.querySelector('.app-shell').getBoundingClientRect().bottom <= innerHeight + 1"), '窄屏工作区超过视口高度') /* 窄屏滚动区域也须留在视口内。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 560, deviceScaleFactor: 1, mobile: true }) /* 缩短视口验证长表单滚动。 */
  await evaluate("document.querySelector('.menu-item[aria-label=\"产品管理\"]').click()") /* 打开长表单所在页面。 */
  await until(() => evaluate("document.querySelector('.page-context h1')?.innerText === '产品管理'")) /* 等待页面切换。 */
  await evaluate("[...document.querySelectorAll('.page-toolbar button')].find(button => button.innerText.includes('新建产品')).click()") /* 打开产品表单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length))")) /* 等待弹窗显示。 */
  assert.ok(await evaluate("(() => {const modal=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length),body=modal.querySelector('.n-card-content');body.scrollTop=200;return modal.getBoundingClientRect().bottom<=innerHeight+1 && body.scrollTop>0})()"), '窄屏长弹窗正文无法上下滚动') /* 长表单应在弹窗内部滚动。 */
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()") /* 关闭产品表单。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")) /* 等待关闭。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  await evaluate("document.querySelector('[aria-label=\"打开用户菜单\"]').click()") /* 打开账户菜单。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-dropdown-option')].find(option => option.getClientRects().length && option.innerText.includes('退出登录')))")) /* 确认账户操作可见。 */
  assert.notEqual(await evaluate("getComputedStyle([...document.querySelectorAll('.n-dropdown-menu')].find(menu => menu.getClientRects().length)).display"), 'flex', '账户菜单被横向 flex 样式破坏') /* 菜单须按列表纵向排布。 */
  const logoutLayout = await evaluate("(() => {const label=[...document.querySelectorAll('.ui-dropdown-label')].find(item=>item.getClientRects().length),icon=label.querySelector('svg').getBoundingClientRect(),text=label.getBoundingClientRect();return {width:text.width,height:text.height,iconHeight:icon.height,display:getComputedStyle(label).display}})()") /* 读取退出菜单布局。 */
  assert.ok(logoutLayout.width>60 && logoutLayout.iconHeight<=logoutLayout.height && logoutLayout.display==='inline-flex', `退出登录图标与文字未排在同一行：${JSON.stringify(logoutLayout)}`) /* 菜单项完整显示。 */
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
