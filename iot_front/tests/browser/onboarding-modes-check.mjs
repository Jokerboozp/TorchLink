// 添加设备向导的界面验收：共享监听与标准上报两种接入方式，桌面与 390px 手机宽度。
// 只连接本机合成夹具（tests/browser/ui-preview.mjs），接口响应在页面内模拟，不写真实业务数据。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const skipScreenshots = process.env.IOT_UI_SKIP_SCREENSHOTS === '1'
const profile = await mkdtemp(join(tmpdir(), 'iot-onboarding-modes-'))
const child = spawn(browser, ['--headless=new', '--use-mock-keychain', '--password-store=basic', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check, note) { for (let i = 0; i < 100; i++) { const value = await check(); if (value) return value; await delay(100) } throw new Error(`页面等待超时：${note}`) }

// 页面内的接口替身：两个模板，一个共享监听，一个标准上报。
const mocks = `
  localStorage.setItem('iot_token', 'fixture-token');
  localStorage.setItem('iot_tenant', 'fixture');
  localStorage.setItem('iot_user', 'fixture');
  localStorage.setItem('iot_role', 'admin');
  localStorage.setItem('iot_permissions', '["*"]');
  const originalFetch = window.fetch.bind(window);
  const json = body => Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } }));
  const check = (key, label, state, detail) => ({ key, label, state, detail });
  const products = [
    { id:'product-demo', name:'烟雾探测器', category:'smoke', transport:'MQTT', payloadFormat:'json', status:'ENABLED', protocolPackageId:'iot-standard@1.0.0', metadata:{} },
    { id:'product-gateway', name:'用户信息传输装置', category:'gateway', transport:'TCP_UDP', payloadFormat:'hex', status:'ENABLED', protocolPackageId:'protocol-demo@2.0.0', metadata:{} }
  ];
  window.fetch = (input, options) => {
    const path = String(input);
    if (!path.startsWith('/api/')) return originalFetch(input, options);
    if (path === '/api/v1/auth/me') return json({ tenantId:'fixture', role:'admin', permissions:['*'] });
    if (path.startsWith('/api/v1/products?')) return json({ items:products, total:products.length });
    if (path === '/api/v2/protocols') return json({ items:[{ definition:{ id:'protocol-demo', name:'演示消防协议' }, releases:[{ version:'2.0.0', status:'PUBLISHED', transport:'TCP_UDP' }] }] });
    if (path.startsWith('/api/v1/onboarding/preflight?')) {
      const gateway = new URLSearchParams(path.split('?')[1]).get('productId') === 'product-gateway';
      return json(gateway
        ? { product:products[1], plan:{ mode:'listener', connector:'TCP', networks:['tcp','udp'], dial:true, protocol:{ id:'protocol-demo', version:'2.0.0', published:true } }, profiles:[{ id:'gateway-demo', productId:'product-gateway', mode:'listener', network:'tcp', connectionMode:'listen', host:'0.0.0.0', publicHost:'iot.example.com', port:26875, enabled:true, runtimeStatus:'LISTENING' }], checks:[check('product','设备模板','passed','已启用'), check('protocol','通信协议','passed','protocol-demo · 2.0.0'), check('listener','平台接入点','passed','已有可用的平台监听')], ready:true }
        : { product:products[0], plan:{ mode:'standard', connector:'MQTT', dial:false, protocol:{ id:'iot-standard', version:'1.0.0', published:true } }, profiles:[], checks:[check('product','设备模板','passed','已启用'), check('protocol','通信协议','passed','iot-standard · 1.0.0'), check('address','设备端地址','warning','尚未配置平台对外 MQTT 地址，设备保存后仍需管理员补齐')], ready:true });
    }
    if (path === '/api/v1/onboarding' && options?.method === 'POST') {
      const request = window.__enrollRequest = JSON.parse(options.body);
      const listener = request.connection.mode === 'listener';
      const device = { id:request.device.id, name:request.device.name, productId:request.productId, deviceRole:request.device.deviceRole, status:'ENABLED', createdAt:Date.now(), tags:{ connector: listener ? 'TCP' : request.connection.transport } };
      return json(listener
        ? { reused:false, mode:'listener', device, product:{ id:'product-gateway', name:'用户信息传输装置' }, profile:{ id:'gateway-new', mode:'listener', network:request.connection.listener.network, connectionMode:'listen', host:'0.0.0.0', publicHost:request.connection.listener.publicHost, port:request.connection.listener.port, enabled:true, runtimeStatus:'PENDING' } }
        : { reused:false, mode:'standard', device, product:{ id:'product-demo', name:'烟雾探测器' }, credential:{ accessKey:'fixture-access-key', secret:'fixture-device-secret' }, accessInfo:{ kind:'standard', mqttBroker:'', clientId:'device-fixture-access-key', username:'fixture-access-key', upTopic:'/iot/up/fixture/product-demo/' + device.id + '/property', downTopic:'/iot/down/fixture/product-demo/' + device.id + '/command', tokenEndpoint:'/api/v1/device-mqtt/token', sample:{ version:'1.0', data:{ temperature:22 } } } });
    }
    if (path.startsWith('/api/v1/device-registry/') && path.includes('/connection?since=')) return json({ device:{ id:decodeURIComponent(path.split('/')[4]), status:'ENABLED' }, ingest:{ configurationSaved:true, rawReceived:false, parsed:false }, diagnosis:{ stage:'WAITING', tone:'info', title:'等待设备本次上报', nextAction:'按设备端配置完成连接后，页面会自动刷新检查结果。', checks:[check('configuration','配置保存','passed','已保存'), check('service','接收服务','waiting','等待连接配置'), check('raw','收到原始报文','waiting','本次尚未收到'), check('parsed','解析结果','waiting','等待解析'), check('continuous','持续上报','waiting','等待后续上报')] } });
    if (path === '/api/v1/events') return json({ permissions:['*'], alarms:[], devices:[] });
    if (path === '/api/v1/mqtt/token') return json({ websocketUrl:'ws://127.0.0.1:1', username:'fixture', token:'fixture', subscriptions:[] });
    return json({ items:[], total:0 });
  };`

let socket
try {
  const port = await until(async () => { try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null } }, '浏览器调试端口')
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let id = 0
  const pending = new Map()
  const failures = []
  socket.onmessage = event => { const message = JSON.parse(event.data); if (message.method === 'Runtime.exceptionThrown') failures.push(message.params.exceptionDetails?.exception?.description || message.params.exceptionDetails?.text); if (!message.id) return; const request = pending.get(message.id); pending.delete(message.id); message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result) }
  const call = (method, params = {}) => new Promise((resolve, reject) => { const next = ++id; const timer = setTimeout(() => { pending.delete(next); reject(new Error(`CDP ${method} 超过 30 秒未响应`)) }, 30000); pending.set(next, { resolve: value => { clearTimeout(timer); resolve(value) }, reject: error => { clearTimeout(timer); reject(error) } }); socket.send(JSON.stringify({ id: next, method, params })) })
  const evaluate = async expression => { const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (value.exceptionDetails) throw new Error(value.exceptionDetails.exception?.description || value.exceptionDetails.text); return value.result.value }
  const setInput = (label, value) => evaluate(`(() => {const input=document.querySelector('input[aria-label=${JSON.stringify(label)}]');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));return true})()`)
  const button = text => evaluate(`(() => {const item=[...document.querySelectorAll('.app-content button')].find(b=>b.innerText.trim()===${JSON.stringify(text)}&&!b.disabled&&b.getClientRects().length);if(!item)return false;item.click();return true})()`)
  const chooseTemplate = async name => {
    await until(() => evaluate("Boolean([...document.querySelectorAll('.onboarding .n-form-item')].find(item=>item.innerText.includes('设备模板'))?.querySelector('.n-base-selection'))"), '模板选择')
    await evaluate("[...document.querySelectorAll('.onboarding .n-form-item')].find(item=>item.innerText.includes('设备模板')).querySelector('.n-base-selection').click()")
    await until(() => evaluate(`Boolean([...document.querySelectorAll('.n-base-select-option')].find(o=>o.getClientRects().length&&o.innerText.includes(${JSON.stringify(name)})))`), name)
    await evaluate(`[...document.querySelectorAll('.n-base-select-option')].find(o=>o.getClientRects().length&&o.innerText.includes(${JSON.stringify(name)})).click()`)
  }
  const fits = async name => {
    await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
    const layout = await evaluate("(() => {const card=document.querySelector('.onboarding__card').getBoundingClientRect();const clipped=[...document.querySelectorAll('.onboarding button,.onboarding .n-radio-button,.onboarding__option')].filter(e=>e.getClientRects().length&&(e.scrollWidth>e.clientWidth+2)).map(e=>e.innerText.trim());return {document:document.documentElement.scrollWidth,viewport:innerWidth,card:card.right,clipped}})()")
    assert.ok(layout.document <= layout.viewport + 2 && layout.card <= layout.viewport + 1 && !layout.clipped.length, `${name} 布局溢出或文字被裁切：${JSON.stringify(layout)}`)
    if (!skipScreenshots) { const shot = await call('Page.captureScreenshot', { format: 'png' }); await writeFile(join(tmpdir(), `iot-onboarding-modes-${name}.png`), Buffer.from(shot.data, 'base64')) }
  }
  const openWizard = async () => {
    await until(() => evaluate("Boolean([...document.querySelectorAll('.filter-bar button')].find(b=>b.innerText.trim()==='添加设备'))"), '添加设备入口')
    await evaluate("[...document.querySelectorAll('.filter-bar button')].find(b=>b.innerText.trim()==='添加设备').click()")
  }
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Page.addScriptToEvaluateOnNewDocument', { source: mocks })
  for (const [width, height, mobile] of [[1440, 900, false], [390, 844, true]]) {
    const size = mobile ? 'mobile' : 'desktop'
    await call('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile })
    await call('Page.navigate', { url: origin })
    await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 15"), '主导航')
    if (mobile) { await evaluate("document.querySelector('.app-topbar__toggle').click()"); await delay(250) }
    await evaluate("document.querySelector('.nav-item[aria-label=\"设备管理\"]').click()")

    // 共享监听：新建监听端口，设备为主设备，协议设备不显示平台密钥。
    await openWizard()
    await chooseTemplate('用户信息传输装置')
    await until(() => evaluate("document.querySelector('.onboarding__preflight')?.innerText.includes('设备连接平台（TCP / UDP 监听）')"), '监听预检')
    await fits(`${size}-listener-template`)
    await until(() => button('下一步'), '下一步')
    await until(() => evaluate("document.querySelector('.onboarding__title')?.innerText==='设备与连接'"), '设备与连接')
    assert.ok(await evaluate("[...document.querySelectorAll('.onboarding__option')].some(o=>o.innerText.includes('iot.example.com:26875'))"), '未列出模板已有的共享监听')
    assert.ok(await evaluate("[...document.querySelectorAll('.onboarding .n-radio-button--checked')].some(b=>b.innerText.includes('主设备'))"), '网关分类未默认选择主设备')
    await setInput('设备名称', 'A 栋传输装置')
    await setInput('设备编号', 'gb26875_000000000001')
    await evaluate("[...document.querySelectorAll('.onboarding__option')].find(o=>o.innerText.includes('新建共享监听')).click()")
    await until(() => setInput('平台对外地址', 'iot.example.com'), '平台对外地址')
    await setInput('监听端口', '26876')
    await evaluate("document.querySelector('input[aria-label=\"监听端口\"]').blur()")
    await fits(`${size}-listener-device`)
    await until(() => button('保存并生成接入信息'), '保存')
    await until(() => evaluate("document.querySelector('.onboarding__title')?.innerText==='现场配置与验证'"), '验证步骤')
    const listener = await evaluate('window.__enrollRequest')
    assert.deepEqual({ product: listener.productId, role: listener.device.deviceRole, connection: listener.connection }, { product: 'product-gateway', role: 'GATEWAY', connection: { mode: 'listener', listener: { network: 'tcp', host: '', publicHost: 'iot.example.com', port: 26876 } } }, `监听接入请求不正确：${JSON.stringify(listener)}`)
    await until(() => evaluate("document.querySelector('.onboarding')?.innerText.includes('iot.example.com:26876') && (document.querySelector('.onboarding__diagnosis')?.innerText||'').includes('等待设备本次上报')"), '监听设备端信息与诊断')
    assert.ok(await evaluate("!document.querySelector('.onboarding__secret')"), '协议设备不应显示平台密钥')
    await fits(`${size}-listener-verify`)
    await until(() => button('完成'), '完成')

    // 标准上报：平台编号、标签、一次性密钥。
    await openWizard()
    await chooseTemplate('烟雾探测器')
    await until(() => evaluate("document.querySelector('.onboarding__preflight')?.innerText.includes('标准 MQTT / HTTP 上报')"), '标准预检')
    await until(() => button('下一步'), '下一步')
    await until(() => evaluate("document.querySelector('.onboarding__title')?.innerText==='设备与连接'"), '设备与连接')
    await setInput('设备名称', '二层配电间烟感')
    await until(() => button('使用平台编号'), '平台编号')
    await evaluate("[...document.querySelectorAll('.onboarding details summary')].find(s=>s.innerText.includes('安装位置与标签')).click()")
    await until(() => button('添加标签'), '添加标签')
    await until(() => setInput('标签名称', '楼栋'), '标签名称')
    await setInput('标签内容', 'A座')
    await evaluate("document.querySelector('.app-content').scrollTop=document.querySelector('.app-content').scrollHeight")
    await fits(`${size}-standard-device`)
    await until(() => button('保存并生成接入信息'), '保存')
    await until(() => evaluate("(document.querySelector('.onboarding__secret')?.innerText||'').includes('fixture-device-secret')"), '一次性密钥')
    const standard = await evaluate('window.__enrollRequest')
    assert.ok(/^device_[0-9a-f]{12}$/.test(standard.device.id) && standard.device.tags['楼栋'] === 'A座' && standard.connection.mode === 'standard' && standard.connection.transport === 'MQTT', `标准接入请求不正确：${JSON.stringify(standard)}`)
    assert.ok(await evaluate("!Object.values(localStorage).some(value=>String(value).includes('fixture-device-secret'))"), '设备密钥写入了浏览器存储')
    assert.equal(await evaluate("[...document.querySelectorAll('.onboarding__config .onboarding__kv span')].filter(span=>span.innerText==='Secret').length"), 0, '密钥不应在配置列表中重复显示')
    await fits(`${size}-standard-verify`)
    await until(() => button('我已保存'), '我已保存')
    await until(() => evaluate("!document.querySelector('.onboarding__secret') && [...document.querySelectorAll('.onboarding__config .onboarding__kv span')].some(span=>span.innerText==='AccessKey')"), '保存后显示 AccessKey')
    await until(() => button('完成'), '完成')
  }
  assert.deepEqual(failures, [], `页面脚本异常：${failures.join(' | ')}`)
  console.log('PASS: 添加设备向导的共享监听与标准上报在桌面和 390px 宽度下的请求、提示与布局')
} finally {
  socket?.close()
  child.kill()
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }).catch(() => {})
}
