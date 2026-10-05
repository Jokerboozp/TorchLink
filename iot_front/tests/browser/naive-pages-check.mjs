// 在隔离浏览器中打开全部主页面，检查 Naive UI 迁移后的可见结构。
import { startBrowser, delay } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173' /* 只访问合成数据预览服务。 */
const pages = [
  '运行总览',
  '协议开发',
  '设备模板',
  '设备管理',
  '模拟设备测试',
  '摄像头映射',
  '告警中心',
  '智能巡检',
  '原始报文',
  '告警规则',
  '模型管理',
  '智能助手',
  '知识库',
  '备份中心',
  '用户与权限'
] /* 检查全部主菜单。 */
let browser
try {
  browser = await startBrowser({
    args: [
      '--use-mock-keychain',
      '--password-store=basic',
      '--disable-background-timer-throttling',
      '--disable-renderer-backgrounding',
      '--disable-backgrounding-occluded-windows',
      '--disable-hang-monitor',
      '--disable-features=TabFreezing,IntensiveWakeUpThrottling,HighEfficiencyModeAvailable,BatterySaverModeAvailable'
    ],
    onEvent: message => {
      if (message.method === 'Page.javascriptDialogOpening') console.log('JS 对话框：', JSON.stringify(message.params).slice(0, 300))
    }
  })
  const { call: browserCall, evaluate, until, errors: failures, warnings } = browser
  const skipScreenshots = process.env.IOT_UI_SKIP_SCREENSHOTS === '1'
  const blankPng = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII='
  const call = (method, params = {}) =>
    skipScreenshots && method === 'Page.captureScreenshot' ? Promise.resolve({ data: blankPng }) : browserCall(method, params)
  const auditControls = async rootSelector =>
    evaluate(
      `(() => {const root=[...document.querySelectorAll(${JSON.stringify(rootSelector)})].find(e=>e.getClientRects().length&&getComputedStyle(e).visibility!=='hidden');if(!root)return {missing:true};const visible=e=>{const r=e.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(e).visibility!=='hidden'};const controls=[...root.querySelectorAll('button,.n-form-item-label,.n-base-selection-label,.n-card-header__main')].filter(visible),labeled=controls.filter(e=>e.innerText?.trim());return {clipped:labeled.filter(e=>e.scrollWidth>e.clientWidth+3&&getComputedStyle(e).overflowX!=='visible').map(e=>({text:e.innerText.trim().slice(0,36),width:e.clientWidth,content:e.scrollWidth})),emptyButtons:controls.filter(e=>e.tagName==='BUTTON'&&!e.innerText.trim()&&!e.getAttribute('aria-label')&&!e.getAttribute('title')&&!e.closest('.n-input__suffix')).map(e=>e.outerHTML.slice(0,120))}})()`
    ) /* 对每个可见界面检查文字裁切和无名按钮，略过已由输入框标注的内部步进按钮。 */
  const auditContrast = async rootSelector =>
    evaluate(
      `(() => {const root=[...document.querySelectorAll(${JSON.stringify(rootSelector)})].find(e=>e.getClientRects().length&&getComputedStyle(e).visibility!=='hidden');if(!root)return [];const rgb=s=>{const m=s.match(/rgba?\\(([^)]+)\\)/);return m?m[1].split(',').slice(0,3).map(Number):null};const lum=c=>{const v=c.map(x=>{x/=255;return x<=.04045?x/12.92:((x+.055)/1.055)**2.4});return .2126*v[0]+.7152*v[1]+.0722*v[2]};const ratio=(a,b)=>{const x=lum(a),y=lum(b);return (Math.max(x,y)+.05)/(Math.min(x,y)+.05)};const candidates=[...root.querySelectorAll('small,p,label,.n-form-item-label')].filter(e=>{const r=e.getBoundingClientRect();return r.width>0&&r.height>0&&e.innerText?.trim()&&!e.closest('[aria-hidden=true]')&&!e.closest('.n-button--disabled')});return candidates.map(e=>{let parent=e,bg;while(parent){const color=getComputedStyle(parent).backgroundColor;if(color&&!color.endsWith(', 0)')&&color!=='transparent'){bg=rgb(color);break}parent=parent.parentElement}const fg=rgb(getComputedStyle(e).color);return {text:e.innerText.trim().slice(0,38),contrast:fg&&bg?Math.round(ratio(fg,bg)*10)/10:null,fg:getComputedStyle(e).color,bg:bg?.join(',')}}).filter(x=>x.contrast!==null&&x.contrast<4.5).slice(0,12)})()`
    ) /* 排除带伪元素底色的按钮，按正文 AA 对比度检查。 */

  const clickListAction = async name => {
    /* 行操作最多显示两个，其余在“更多”菜单中。 */
    const direct = await evaluate(
      `(() => {const button=[...document.querySelectorAll('.app-content button')].find(item=>item.getClientRects().length&&item.innerText.trim()===${JSON.stringify(name)});if(!button)return false;button.click();return true})()`
    )
    if (direct) return true
    const opened = await evaluate(
      '(() => {const more=[...document.querySelectorAll(\'.app-content .row-actions button[aria-label="更多操作"]\')].find(item=>item.getClientRects().length);if(!more)return false;more.click();return true})()'
    )
    if (!opened) return false
    await until(() =>
      evaluate(
        `Boolean([...document.querySelectorAll('.n-dropdown-option')].find(option=>option.getClientRects().length&&option.innerText.trim()===${JSON.stringify(name)}))`
      )
    ).catch(() => false)
    return evaluate(
      `(() => {const option=[...document.querySelectorAll('.n-dropdown-option')].find(item=>item.getClientRects().length&&item.innerText.trim()===${JSON.stringify(name)});if(!option)return false;option.querySelector('.n-dropdown-option-body').click();return true})()`
    )
  }
  const openPage = async name => {
    /* 窄屏先打开抽屉导航，再切换页面。 */
    if (await evaluate("document.querySelector('.app-topbar__toggle')?.getAttribute('aria-label')==='打开菜单'")) {
      await evaluate("document.querySelector('.app-topbar__toggle').click()")
      await delay(250)
    }
    await evaluate(`document.querySelector('.nav-item[aria-label=${JSON.stringify(name)}]').click()`)
    await until(() => evaluate(`document.querySelector('.app-breadcrumb strong')?.innerText === ${JSON.stringify(name)}`))
    return until(async () => {
      const value = await evaluate(
        "(() => {const root=document.querySelector('.app-content');if(!root || root.querySelector('.ui-loading,.n-data-table--loading,.n-skeleton'))return '';return [...root.children].filter(node=>!node.classList.contains('page-header')).map(node=>node.innerText || '').join(' ').trim()})()"
      )
      return value.length > name.length ? value : false
    }).catch(async error => {
      throw new Error(
        `${name} 缺少业务内容：${error.message}；异常=${failures.slice(-3).join(' | ')}；警告=${warnings.slice(-3).join(' | ')}`
      )
    }) /* 等待异步页面出现业务内容。 */
  }
  // 登录页不预填租户与账户：夹具登录时三项都要填写。
  const fixtureLogin =
    "(() => { const fill = (selector, value) => { const input = document.querySelector(selector); input.value = value; input.dispatchEvent(new Event('input', { bubbles: true })) }; fill('.login-form #tenant-id input, .login-form input#tenant-id', 'fixture'); fill('.login-form #username input, .login-form input#username', 'admin'); fill('.login-form input[type=password]', 'fixture'); document.querySelector('.login-form button[type=submit]').click() })()"
  const freshSession = async () => {
    /* 重新载入并登录：隔离前面页面累积的状态，避免无头浏览器长时间运行后卡住。 */
    await call('Page.navigate', { url: origin })
    await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"))
    await evaluate(fixtureLogin)
    await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 15"))
  }
  await call('Page.enable') /* 开启导航与截图。 */
  await call('Runtime.enable') /* 收集未处理的脚本异常。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 固定桌面视口。 */
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, options) => {
      const path = String(input);
      if (path === '/api/v1/ai/health-inspection/progress') return Promise.resolve(new Response(JSON.stringify({ message:'暂无巡检任务' }), { status:404, headers:{ 'Content-Type':'application/json' } }));
      const body = path === '/api/v1/auth/login' ? { accessToken: 'fixture-token', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me' ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/events' ? { permissions: ['*'], alarms: [], devices: [] }
        : path === '/api/v1/mqtt/token' ? { websocketUrl:'ws://127.0.0.1:1', username:'fixture', token:'fixture', subscriptions:[] }
        : path === '/api/v2/protocol-source-template' ? { compilerAvailable:true, targetPlatforms:['linux-amd64','linux-arm64','windows-amd64','windows-arm64','darwin-amd64','darwin-arm64'] }
        : path === '/api/v2/protocols' ? { items:[{ definition:{ id:'protocol-demo', name:'演示消防协议', vendor:'炬联' }, releases:[{ version:'2.0.0', status:'PUBLISHED', transport:'MQTT', parserType:'JSON', artifact:{ platform:'linux-amd64' } },{ version:'1.0.0', status:'VALIDATED', transport:'MQTT', parserType:'JSON', artifact:{ platform:'linux-amd64' } }] }] }
        : path === '/api/v2/device-access-profiles' ? { items:[{ id:'gateway-demo', productId:'product-demo', protocolId:'protocol-demo', protocolVersion:'1.0.0', mode:'listener', network:'tcp', connectionMode:'listen', host:'0.0.0.0', port:26875, timeoutMs:5000, enabled:true }] }
        : path.startsWith('/api/v1/products?') ? { items:[{ id:'product-demo', name:'烟雾探测器', category:'smoke', transport:'MQTT', payloadFormat:'json', status:'ENABLED', protocolPackageId:'iot-standard@1.0.0', metadata:{} },{ id:'product-gateway', name:'用户信息传输装置', category:'gateway', transport:'TCP_UDP', payloadFormat:'hex', status:'ENABLED', protocolPackageId:'protocol-demo@2.0.0', metadata:{} }], total:2 }
        : path.startsWith('/api/v1/products/') && path.endsWith('/preparation') ? {revision:1,product:{id:'product-gateway',name:'用户信息传输装置',category:'gateway',transport:'TCP',payloadFormat:'hex',status:'ENABLED',protocolPackageId:'protocol-demo@2.0.0',metadata:{}},candidate:{product:{id:'product-gateway',name:'用户信息传输装置',category:'gateway',transport:'TCP',payloadFormat:'hex',status:'ENABLED',protocolPackageId:'protocol-demo@2.0.0',metadata:{}},protocolId:'protocol-demo',version:'2.0.0',profiles:[],verificationRules:{mode:'periodic',minMessages:2,windowSeconds:300,maxGapSeconds:120}},affectedDevices:0,status:'DRAFT',history:[]}
        : path.startsWith('/api/v1/device-registry?') ? { items:[{ device:{ id:'device-demo', name:'一层走廊烟感', productId:'product-demo', deviceRole:'DIRECT', status:'ENABLED', createdAt:Date.now() }, runtimeState:{ businessStatus:'ONLINE', lastSeenAt:Date.now() }, childCount:0 }], total:1 }
        : path.startsWith('/api/v1/raw-messages?') ? { items: [{ messageId: 'raw-demo', receivedAt: Date.now(), productId: 'product-demo', deviceId: 'device-demo', protocol: 'MQTT', parsed: true, parsedMessageType: 'PROPERTY_REPORT', payloadSize: 4, payloadHash: 'fixture-hash' }], total: 1 }
        : path === '/api/v1/raw-messages/raw-demo' ? { parseStatus: 'PARSED', message: { messageId: 'raw-demo', deviceId: 'device-demo', productId: 'product-demo', payload: 'AA01', receivedAt: Date.now(), protocol: 'MQTT', payloadFormat: 'hex' }, standardMessage: { messageType: 'PROPERTY_REPORT', properties: { temperature: 42 } }, archive: { payloadHash: 'fixture-hash' } }
        : path.startsWith('/api/v1/alarms?') ? { items: [{ alarmId:'alarm-demo', deviceId:'device-demo', deviceName:'测试设备', alarmType:'MANUAL_ALARM', alarmLevel:'HIGH', status:'ACTIVE', source:'device', lastTriggeredAt:Date.now() }], total:1 }
        : path === '/api/v1/alarms/alarm-demo' ? { alarmId:'alarm-demo', deviceId:'device-demo', deviceName:'测试设备', alarmType:'MANUAL_ALARM', alarmLevel:'HIGH', status:'ACTIVE', source:'device', firstTriggeredAt:Date.now(), lastTriggeredAt:Date.now(), triggerCount:1, diagnosticLines:Array.from({length:80},(_,index)=>'第 '+(index+1)+' 条诊断记录') }
        : path === '/api/v1/ai/alarm-analysis/alarm-demo' ? { summary:'设备多次触发故障告警，需要检查现场状态', riskLevel:'MEDIUM', confidence:0.85, possibleReasons:['设备状态异常','通信链路抖动'], suggestions:['检查设备电源和网络','核对告警历史'], model:'fixture', createdAt:Date.now() }
        : path.startsWith('/api/v1/ai/workflows/admin?') ? { items:[{ id:'ops-assistant', name:'内置运维助手', description:'只读配置清单', enabled:true, version:'1.0.0' },{ id:'custom-assistant', name:'示例智能体', description:'可编辑的智能体', enabled:true, version:'1.0.0', persona:'只依据工具结果回答', defaultModel:'deepseek-chat', maxTokens:1024, capabilities:['知识检索'], allowedTools:['mcp__iot__query_knowledge_base'] }], total:2, allowedTools:['mcp__iot__query_alarm_list','mcp__iot__query_knowledge_base'], builtinIds:['ops-assistant'] }
        : path.startsWith('/api/v1/ai/workflows?') ? { items:[{ id:'ops-assistant', name:'内置运维助手', description:'使用当前租户的设备、告警、属性历史和知识库数据辅助故障排查，并提供可复核的运维建议。', enabled:true, capabilities:['告警查询'], allowedTools:['mcp__iot__query_alarm_list'] },{ id:'custom-assistant', name:'示例智能体', description:'检索处置知识', enabled:true, capabilities:['知识检索'], allowedTools:['mcp__iot__query_knowledge_base'] }], total:2, healthy:true }
        : path.startsWith('/api/v1/knowledge/documents?') ? { items:[{ id:'knowledge-demo', filename:'消防处置手册.md', workflowId:'ops-assistant', category:'manual', metadata:{ chunks:2, size:2048 }, createdAt:Date.now() }], total:1, indexMode:'vector', persistentIndex:true }
        : path === '/api/v1/knowledge/documents/knowledge-demo' ? { document:{ id:'knowledge-demo', filename:'消防处置手册.md', workflowId:'ops-assistant', category:'manual', metadata:{ chunks:2, size:2048 }, createdAt:Date.now() }, index:{ mode:'vector', vectorizer:'fixture', chunking:{ strategy:'fixed-window-overlap', size:500, overlap:50 }, extractedChars:950, chunkCount:2 }, chunks:[{ chunkId:'chunk-1', startChar:0, endChar:500, characterCount:500, vectorized:true, content:'设备告警处置步骤' },{ chunkId:'chunk-2', startChar:450, endChar:950, characterCount:500, vectorized:true, content:'现场复核与恢复流程' }] }
        : path.startsWith('/api/v1/rules?') ? { items: [{ id:'rule-demo', name:'演示规则', alarmType:'DEVICE_FAULT', level:'HIGH', enabled:true, conditions:[], actions:[] }], total:1 }
        : path === '/api/v1/access/users' ? { tenantId:'fixture', items:[{ username:'operator-demo', displayName:'操作员', enabled:true, roleIds:[], deviceScope:'none' }] }
        : path === '/api/v1/access/roles' ? { items:[{ id:'viewer', name:'查看员', permissions:[] }] }
        : path === '/api/v1/access/permissions' ? { items:[{ id:'menu:devices', name:'设备管理', kind:'menu', menu:'devices' },{ id:'GET /api/v1/devices', name:'查看设备', kind:'action', menu:'devices' }] }
        : path === '/api/v1/access/device-options' ? { items:[{ id:'device-demo', name:'测试设备' }] }
        : path === '/api/v1/device-registry/device-demo/connection' ? { device:{id:'device-demo',name:'一层走廊烟感',productId:'product-demo',deviceRole:'DIRECT',status:'ENABLED',createdAt:Date.now(),accessKey:'fixture-access-key',connector:'MQTT'},product:{id:'product-demo',name:'烟雾探测器',status:'ENABLED',transport:'MQTT',thingModel:{properties:[],commands:[]}},connector:'MQTT',protocolId:'iot-standard',protocolVersion:'1.0.0',connection:{connectionStatus:'CONNECTED',dataStatus:'FRESH',businessStatus:'ONLINE',lastSeenAt:Date.now()},accessInfo:{mqttBroker:'mqtts://devices.example.test:8883',clientId:'device-demo',username:'fixture-access-key',upTopic:'/iot/up/fixture/product-demo/device-demo/property',downTopic:'/iot/down/fixture/product-demo/device-demo/command',tokenEndpoint:'/api/v1/device-mqtt/token',sample:{temperature:22}},ingest:{configurationSaved:true,rawReceived:false,parsed:false,stage:'WAITING_FOR_DATA'},diagnosis:{stage:'ADDRESS_MISSING',tone:'warning',title:'平台对外地址未配置',nextAction:'请管理员配置设备接入的对外地址后，再按设备端信息连接。',checks:[]},profile:null,profiles:[],sessions:[],latestProperties:[],recentAlarms:[],revocations:[],credentialSupported:true,credentialEnabled:true }
        : path.startsWith('/api/v1/backups?') ? { items:[{ id:'backup-demo', type:'DEVICE_DAILY', status:'COMPLETED', startedAt:Date.now(), completedAt:Date.now() }], total:1 }
        : path === '/api/v1/backups/backup-demo' ? { id:'backup-demo', type:'DEVICE_DAILY', status:'COMPLETED', startedAt:Date.now(), completedAt:Date.now(), details:{}, objectKey:'backup/manifest.json' }
        : path.startsWith('/api/v1/backups/backup-demo/files?') ? { artifacts:[{ component:'原始报文', filename:'raw-messages.jsonl.gz', size:313, checksum:'fixture' }], total:1, components:{ rawMessages:{ records:1 } } }
        : path.startsWith('/api/v1/dashboard?') ? { devices: 3, online: 2, activeAlarms: 2, highAlarms: 1, states: { ONLINE:2, OFFLINE:1 }, products: [{ key:'product-demo', name:'演示烟感', count:3 }], trend: [], levels: { HIGH:1, MEDIUM:1 }, updatedAt: Date.now() } : null;
      return body || path.startsWith('/api/') ? Promise.resolve(new Response(JSON.stringify(body || { items:[], total:0 }), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  `
  }) /* 注入仅供界面检查使用的身份与权限。 */
  await call('Page.navigate', { url: origin }) /* 打开合成数据前端。 */
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))")).catch(async error => {
    throw new Error(
      `${error.message}；表单=${await evaluate("document.querySelector('.login-form')?.innerHTML.slice(0,500)")}；异常=${failures.slice(0, 2).join(' | ')}；警告=${warnings.slice(0, 3).join(' | ')}`
    )
  }) /* 等待登录页并报告首屏脚本异常。 */
  const loginBrand = await evaluate(
    "(() => {const root=getComputedStyle(document.documentElement),button=document.querySelector('.login-submit');return {clay:root.getPropertyValue('--brand-clay').trim(),primary:root.getPropertyValue('--primary').trim(),button:getComputedStyle(button).backgroundColor}})()"
  ) /* 读取最终计算后的登录页主色。 */
  assert.ok(
    loginBrand.clay === '#b8573a' && loginBrand.primary === '#b8573a' && loginBrand.button === 'rgb(184, 87, 58)',
    `登录页未使用品牌陶土主色：${JSON.stringify(loginBrand)}`
  ) /* 登录按钮与主题变量都应采用品牌陶土色。 */
  const loginCapture = await call('Page.captureScreenshot', { format: 'png' }) /* 留存登录页视觉检查截图。 */
  await writeFile(join(tmpdir(), 'iot-brand-login.png'), Buffer.from(loginCapture.data, 'base64')) /* 保存登录页截图。 */
  await evaluate(fixtureLogin) /* 完成夹具登录。 */
  await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 15")) /* 确认全部主菜单可见。 */
  const asideBrand = await evaluate(
    "(() => {const aside=document.querySelector('.app-sidebar'),menu=aside.querySelector('.nav-item:not(.is-active)'),logo=aside.querySelector('.app-sidebar__brand img');return {background:getComputedStyle(aside).backgroundColor,menu:getComputedStyle(menu).color,logo:logo?.naturalWidth||0}})()"
  ) /* 读取实际渲染的导航颜色。 */
  assert.ok(
    asideBrand.background === 'rgb(245, 244, 237)' && asideBrand.menu === 'rgb(61, 61, 58)' && asideBrand.logo > 0,
    `米色侧栏、深色菜单或品牌标识未生效：${JSON.stringify(asideBrand)}`
  ) /* 检查导航可读性及品牌标识。 */
  await evaluate("document.querySelector('.app-topbar__toggle').click()") /* 验证折叠导航。 */
  await delay(300)
  assert.ok(
    await evaluate(
      "(() => {const shell=document.querySelector('.app-shell'),menu=document.querySelector('.nav-item:not(.is-active)');return shell.classList.contains('is-collapsed') && menu.getBoundingClientRect().width>0 && menu.getAttribute('aria-label') && getComputedStyle(menu).color==='rgb(61, 61, 58)'})()"
    ),
    '折叠态导航图标不可见或缺少名称'
  ) /* 折叠后仍保留可读菜单。 */
  await evaluate("document.querySelector('.app-topbar__toggle').click()") /* 恢复完整侧栏。 */
  await delay(300)
  for (const name of pages) {
    /* 逐页检查标题、正文和脚本异常。 */
    const text = await openPage(name)
    assert.ok(text.length > name.length, `${name} 缺少业务内容`) /* 防止页面只显示标题。 */
    assert.ok(
      await evaluate(
        "[...document.querySelectorAll('.app-content .n-tabs-tab')].every(tab=>tab.innerText.trim().length>0 && tab.getBoundingClientRect().width>0)"
      ),
      `${name} 存在空白页签`
    ) /* 所有主页面页签必须有可读标题。 */
    const surfaceAudit = await auditControls('.app-content')
    assert.deepEqual(surfaceAudit.clipped, [], `${name} 有被裁切的控件文字`)
    assert.deepEqual(surfaceAudit.emptyButtons, [], `${name} 有无名称的操作按钮`)
    const contrastAudit = await auditContrast('.app-content')
    assert.deepEqual(contrastAudit, [], `${name} 有对比不足的正文文字`)
    console.log(
      `${name} 按钮：${(await evaluate("[...document.querySelectorAll('.app-content button')].filter(e=>e.getClientRects().length).map(e=>e.innerText.trim()).filter(Boolean).slice(0,35)")).join('、')}`
    )
    if (name === '运行总览') {
      assert.ok(
        await evaluate(
          "(() => {const cards=[...document.querySelectorAll('.stat-card')];const circle=document.querySelector('.device-ring circle:nth-of-type(2)');return cards.length>=4 && new Set(cards.map(card=>getComputedStyle(card).borderTopColor)).size===1 && getComputedStyle(cards[0]).borderTopColor===getComputedStyle(cards[0]).borderLeftColor && (!circle || getComputedStyle(circle).transitionProperty.includes('stroke-dasharray'))})()"
        ),
        '仪表盘统计卡片应为中性样式，圆环过渡需生效'
      )
      assert.ok(!contrastAudit.some(item => item.text === '已登记设备' && item.contrast < 4.5), '总览统计卡片的说明文字对比不足')
    } /* 检查总览主题与说明可读性。 */
    if (name === '备份中心')
      assert.ok(
        await evaluate(
          "(() => {const card=document.querySelector('.backup-stat-grid .n-card-content'),label=card?.querySelector('span'),value=card?.querySelector('strong'),note=card?.querySelector('small');if(!label||!value||!note)return false;const a=label.getBoundingClientRect(),b=value.getBoundingClientRect(),c=note.getBoundingClientRect();return a.bottom<=b.top&&b.bottom<=c.top})()"
        ),
        '备份概览卡片的标题、数值和说明挤在同一行'
      ) /* 卡片内容应按层级纵向排列。 */
    if (name === '设备管理')
      assert.ok(
        await evaluate(
          "(() => {const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(item=>item.innerText.includes('一层走廊烟感'));return row && row.querySelectorAll('.n-tag').length===0 && row.querySelectorAll('.status-dot').length===2 && row.innerText.includes('已启用') && row.querySelectorAll('.row-actions button').length<=3})()"
        ),
        '设备列表状态应以圆点展示，行操作最多两项加“更多”'
      ) /* 状态统一为圆点，操作不换行。 */
    if (name === '模型管理')
      assert.ok(
        await evaluate(
          "(() => {const config=document.querySelector('.ai-provider-config'),scope=document.querySelector('.ai-capability-card'),token=document.querySelector('.config-field-grid .n-input-number'),hint=document.querySelector('.config-field-grid .provider-field-hint');return config&&scope&&token&&hint&&config.getBoundingClientRect().width>scope.getBoundingClientRect().width&&config.querySelectorAll('.config-section').length===3&&hint.getBoundingClientRect().top>=token.getBoundingClientRect().bottom})()"
        ),
        '模型配置未突出主操作，或词元说明与输入框挤在一起'
      )
    if (name === '智能助手')
      assert.ok(
        await evaluate(
          "(() => {const workbench=document.querySelector('.ai-workbench'),chat=document.querySelector('.ai-chat-card'),label=document.querySelector('.chat-workflow-label'),select=document.querySelector('.chat-workflow-select'),actions=document.querySelector('.chat-header-actions'),prompts=document.querySelector('.quick-prompts'),promptLabel=document.querySelector('.quick-prompts-label'),firstPrompt=document.querySelector('.quick-prompts-list button'),log=document.querySelector('.chat-log');if(!workbench||!chat||!label||!select||!actions||!prompts||!promptLabel||!firstPrompt||!log)return false;const l=label.getBoundingClientRect(),s=select.getBoundingClientRect(),a=actions.getBoundingClientRect(),p=promptLabel.getBoundingClientRect(),b=firstPrompt.getBoundingClientRect(),q=prompts.getBoundingClientRect();return !document.querySelector('.control-card')&&chat.getBoundingClientRect().right>=workbench.getBoundingClientRect().right-2&&chat.getBoundingClientRect().width>=workbench.getBoundingClientRect().width*0.6&&s.width>=220&&l.right+8<=s.left&&s.right+8<=a.left&&p.right+8<=b.left&&q.bottom<=log.getBoundingClientRect().top+2&&document.querySelector('.n-card-header').getBoundingClientRect().height<=85})()"
        ),
        '智能助手工具栏或快捷提问排列不清晰'
      )
    if (name === '智能巡检')
      assert.ok(
        await evaluate(
          "(() => {const hero=document.querySelector('.inspection-hero'),card=document.querySelector('.inspection-page>.surface-card'),action=hero?.querySelector('.inspection-hero-actions .n-button--primary-type');return hero&&card&&action&&hero.getBoundingClientRect().width>=card.getBoundingClientRect().width-2&&action.getBoundingClientRect().left>hero.getBoundingClientRect().left+hero.getBoundingClientRect().width/2&&hero.querySelectorAll('.inspection-scope span').length===3})()"
        ),
        '智能巡检说明与主操作没有占满页面或层级不清'
      )
    {
      const capture = await call('Page.captureScreenshot', { format: 'png' })
      await writeFile(join(tmpdir(), `iot-naive-${pages.indexOf(name)}.png`), Buffer.from(capture.data, 'base64'))
    } /* 留存每个主页面的临时截图供逐页复核。 */
  } /* 结束页面遍历。 */
  const overlayCases = [
    ['运行总览', '详情'],
    ['协议开发', '管理版本'],
    ['协议开发', '上传源码'],
    ['协议开发', '协议生成'],
    ['设备管理', '编辑'],
    ['设备管理', '详情'],
    ['摄像头映射', '新增摄像头'],
    ['告警中心', '查看详情'],
    ['原始报文', '详情'],
    ['告警规则', '手动添加规则'],
    ['告警规则', '智能生成规则草稿'],
    ['告警规则', '详情'],
    ['告警规则', '编辑'],
    ['智能助手', '新建智能体', '智能体'],
    ['知识库', '上传知识文档'],
    ['知识库', '查看详情'],
    ['备份中心', '详情 / 文件'],
    ['用户与权限', '添加用户', '用户管理'],
    ['用户与权限', '编辑', '用户管理'],
    ['用户与权限', '重置密码', '用户管理'],
    ['用户与权限', '添加角色', '角色管理'],
    ['用户与权限', '编辑', '角色管理']
  ] /* 覆盖每个页面可直接打开的编辑、详情和管理弹层。 */
  const skippedOverlays = new Set(
    (process.env.IOT_UI_SKIP_OVERLAYS || '').split(',').filter(Boolean)
  ) /* 仅用于定位环境问题：如“用户与权限/编辑”。 */
  let overlayPage = ''
  for (const [pageName, actionName, tabName] of overlayCases) {
    if (skippedOverlays.has(`${pageName}/${actionName}`)) {
      console.log('跳过弹层', pageName, actionName)
      continue
    }
    if (pageName !== overlayPage) {
      await freshSession()
      overlayPage = pageName
    }
    await openPage(pageName)
    await delay(100)
    if (tabName) {
      await until(() =>
        evaluate(
          `Boolean([...document.querySelectorAll('.n-tabs-tab,.n-radio-button')].find(tab=>tab.innerText.includes(${JSON.stringify(tabName)})))`
        )
      )
      await evaluate(
        `[...document.querySelectorAll('.n-tabs-tab,.n-radio-button')].find(tab=>tab.innerText.includes(${JSON.stringify(tabName)})).click()`
      )
      await delay(100)
    }
    console.log('弹层检查', pageName, actionName, tabName || '')
    const found = await clickListAction(actionName)
    assert.ok(found, `${pageName} 缺少“${actionName}”入口`)
    await until(() =>
      evaluate(
        "Boolean([...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length && getComputedStyle(item).visibility!=='hidden'))"
      )
    )
    await delay(550)
    if (pageName === '协议开发' && actionName === '管理版本') {
      assert.ok(
        await evaluate(
          "(() => {const modal=document.querySelector('.protocol-versions-dialog');return modal && modal.innerText.includes('2.0.0') && modal.innerText.includes('1.0.0') && modal.querySelectorAll('.n-data-table-tbody .n-data-table-tr').length===2 && [...modal.querySelectorAll('button')].filter(button=>button.innerText.trim()==='删除').length===2})()"
        ),
        '版本管理弹窗未逐版本展示详情与删除入口'
      )
    }
    if (pageName === '设备管理' && actionName === '详情') {
      assert.ok(
        await evaluate("document.querySelectorAll('.device-connection-drawer .connection-status-grid > div').length===6"),
        '设备连接详情未优先展示六项接入状态'
      )
      const topShot = await call('Page.captureScreenshot', { format: 'png' })
      await writeFile(join(tmpdir(), 'iot-device-connection-top.png'), Buffer.from(topShot.data, 'base64'))
    }
    const overlay = await evaluate(
      `(() => {const m=[...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden'),r=m.getBoundingClientRect(),body=m.querySelector('.n-card-content,.n-drawer-body-content-wrapper'),footer=m.querySelector('.n-card__footer'),b=body?.getBoundingClientRect(),f=footer?.getBoundingClientRect(),fields=[...m.querySelectorAll('.n-form-item,.n-input,.n-select,.n-alert')].filter(e=>e.getClientRects().length),outside=fields.filter(e=>{const x=e.getBoundingClientRect();return x.left<r.left-2||x.right>r.right+2}).map(e=>e.innerText.slice(0,25));let reachable=true;if(body&&body.scrollHeight>body.clientHeight+2){body.scrollTop=body.scrollHeight;reachable=body.scrollTop>0}return {title:m.querySelector('.n-card-header__main,.n-drawer-header__main')?.innerText||'',rect:{left:r.left,right:r.right,top:r.top,bottom:r.bottom},viewport:{width:innerWidth,height:innerHeight},withinViewport:r.left>=-1&&r.right<=innerWidth+1&&r.top>=-1&&r.bottom<=innerHeight+1,footerSeparate:!f||!b||b.bottom<=f.top+2,reachable,outside}})()`
    )
    assert.ok(
      overlay.withinViewport && overlay.footerSeparate && overlay.reachable && !overlay.outside.length,
      `${pageName} / ${actionName} 弹层布局或滚动异常：${JSON.stringify(overlay)}`
    )
    const overlayControls = await auditControls('.n-modal,.n-drawer')
    assert.deepEqual(overlayControls.clipped, [], `${pageName} / ${actionName} 弹层有被裁切的控件文字`)
    assert.deepEqual(overlayControls.emptyButtons, [], `${pageName} / ${actionName} 弹层有无名称的操作按钮`)
    assert.deepEqual(await auditContrast('.n-modal,.n-drawer'), [], `${pageName} / ${actionName} 弹层有对比不足的正文文字`)
    if (pageName === '协议开发' && actionName === '协议生成')
      assert.equal(
        await evaluate("document.querySelectorAll('.protocol-generator .generator-section').length"),
        2,
        '生成协议应区分资料与协议基本信息'
      )
    if (pageName === '设备管理' && actionName === '编辑') {
      await evaluate("document.querySelector('.n-modal .device-advanced .n-collapse-item__header-main').click()")
      await until(() => evaluate("document.querySelector('.n-modal .device-tags')?.getBoundingClientRect().height>0"))
      await evaluate(
        "[...document.querySelectorAll('.n-modal .device-tags button')].find(button=>button.textContent.trim()==='添加标签').click()"
      )
      assert.ok(
        await evaluate(
          "[...document.querySelectorAll('.device-tag-row')].every(row=>[...row.querySelectorAll('input')].filter(input=>input.getAttribute('aria-label')).length===2)"
        ),
        '标签名称和内容应分别标注'
      )
    }
    if (pageName === '告警规则' && ['手动添加规则', '详情', '编辑'].includes(actionName)) {
      assert.equal(
        await evaluate("document.querySelectorAll('.n-modal .rule-editor-section').length"),
        3,
        '规则弹窗应按基本信息、触发条件、联动动作分区'
      )
      assert.ok(
        await evaluate(
          "(() => {const field=document.querySelector('.rule-action-field'),input=field?.querySelector('.n-input'),help=field?.querySelector('small');return input&&help&&help.getBoundingClientRect().top>=input.getBoundingClientRect().bottom})()"
        ),
        '联动动作说明应位于输入框下方'
      )
      assert.ok(await evaluate("!document.querySelector('.rule-field-reference').open"), '字段参考应默认收起')
      await evaluate("document.querySelector('.rule-field-reference summary').click()")
      assert.ok(
        await evaluate(
          "document.querySelector('.rule-field-reference').open && document.querySelectorAll('.rule-field-reference .n-data-table-tr').length>0"
        ),
        '字段参考应能展开查看'
      )
    }
    if (pageName === '摄像头映射' && actionName === '新增摄像头')
      assert.equal(
        await evaluate("document.querySelectorAll('.camera-editor-section').length"),
        3,
        '摄像头弹窗应分开显示身份、位置、关联与状态'
      )
    if (pageName === '用户与权限' && tabName === '角色管理')
      assert.equal(
        await evaluate("document.querySelectorAll('.role-editor-section').length"),
        3,
        '角色弹窗应分开显示角色信息、可用功能与设备范围'
      )
    const capture = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(
      join(
        tmpdir(),
        `iot-overlay-${overlayCases.indexOf(overlayCases.find(item => item[0] === pageName && item[1] === actionName && item[2] === tabName))}.png`
      ),
      Buffer.from(capture.data, 'base64')
    )
    await evaluate(
      "(() => {const overlay=[...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden'),close=[...overlay.querySelectorAll('.n-card__footer button')].find(button=>button.getClientRects().length&&['关闭','取消','关闭详情','关闭弹窗'].includes(button.innerText.trim())&&!button.disabled);(close||overlay.querySelector('.n-base-close'))?.click()})()"
    )
    await delay(200)
    // 检查中改过表单（如设备标签）时，关闭前会先确认放弃未保存的修改。
    await evaluate(
      "[...document.querySelectorAll('.n-dialog button')].find(button=>button.getClientRects().length&&button.innerText.trim()==='放弃修改')?.click()"
    )
    await until(() =>
      evaluate(
        "![...document.querySelectorAll('.n-modal,.n-drawer')].some(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden')"
      )
    )
  }
  console.log(`PASS: ${overlayCases.length} 个弹层布局与滚动检查`)
  await openPage('智能助手')
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-radio-button')].find(b=>b.innerText.trim()==='智能体'))"))
  await evaluate("[...document.querySelectorAll('.n-radio-button')].find(b=>b.innerText.trim()==='智能体').click()")
  await until(() => evaluate("document.querySelector('.agent-manager')?.innerText.includes('示例智能体')"))
  assert.ok(await clickListAction('查看'), '内置智能体缺少查看入口')
  await until(() =>
    evaluate("[...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length&&m.innerText.includes('查看内置智能体'))")
  )
  assert.ok(
    await evaluate(
      "(() => {const m=[...document.querySelectorAll('.n-modal')].find(x=>x.getClientRects().length);return !m.innerText.includes('创建智能体')&&!m.innerText.includes('保存修改')&&[...m.querySelectorAll('input')].some(i=>i.value==='内置运维助手')})()"
    ),
    '内置智能体查看弹窗不是只读或缺少配置'
  )
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)"))
  assert.ok(await clickListAction('编辑'), '自定义智能体缺少编辑入口')
  await until(() =>
    evaluate("[...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length&&m.innerText.includes('编辑智能体'))")
  )
  assert.ok(
    await evaluate(
      "(() => {const m=[...document.querySelectorAll('.n-modal')].find(x=>x.getClientRects().length);return m.innerText.includes('保存修改')&&m.innerText.includes('检索知识库')&&[...m.querySelectorAll('textarea')].some(t=>t.value==='只依据工具结果回答')})()"
    ),
    '自定义智能体编辑弹窗没有加载当前配置'
  )
  await evaluate("[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)"))
  await openPage('用户与权限')
  await until(() => evaluate("Boolean(document.querySelector('.n-tabs-tab[data-name=users]'))"))
  await evaluate("document.querySelector('.n-tabs-tab[data-name=users]').click()")
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button=>button.innerText.includes('添加用户')))"
    )
  )
  await evaluate(
    "[...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button=>button.innerText.includes('添加用户')).click()"
  )
  await until(() => evaluate("Boolean(document.querySelector('.n-modal .user-editor'))"))
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.user-editor').closest('.n-modal'),grid=m.querySelector('.user-editor-grid'),items=[...grid.children],r=e=>e.getBoundingClientRect();return items.length>=4 && items.every((e,i)=>i%2===1||!items[i+1]||r(items[i+1]).top===r(e).top) && !m.querySelector('.user-editor-permissions details').open && m.querySelector('.user-editor-switch [role=switch]')})()"
    ),
    '添加用户账户信息分栏、状态或权限折叠区异常'
  )
  await evaluate("document.querySelector('.user-editor-permissions summary').click()")
  assert.ok(
    await evaluate(
      "document.querySelector('.user-editor-permissions details').open && document.querySelectorAll('.user-editor-permissions .permission-group').length>0"
    ),
    '用户附加权限无法展开'
  )
  await delay(300)
  const userCapture = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-user-add.png'), Buffer.from(userCapture.data, 'base64'))
  await evaluate("document.querySelector('.user-editor').closest('.n-modal').querySelector('.n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal=>modal.getClientRects().length)"))
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText==='编辑').click()")
  await until(() => evaluate("Boolean(document.querySelector('.n-modal .user-editor'))"))
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.user-editor').closest('.n-modal');return m.innerText.includes('重置密码') && m.querySelector('input[placeholder*=用户名]')===null && m.querySelector('.user-editor-grid input')?.disabled})()"
    ),
    '编辑用户表单未说明密码操作或允许修改用户名'
  )
  await evaluate("document.querySelector('.user-editor').closest('.n-modal').querySelector('.n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal=>modal.getClientRects().length)"))
  await openPage('知识库')
  await until(() => evaluate("Boolean([...document.querySelectorAll('button')].find(button=>button.innerText.includes('上传知识文档')))"))
  await evaluate("[...document.querySelectorAll('button')].find(button=>button.innerText.includes('上传知识文档')).click()")
  await until(() => evaluate("Boolean(document.querySelector('.knowledge-upload-dialog'))"))
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.knowledge-upload-dialog'),sections=m.querySelectorAll('.knowledge-upload-section'),select=m.querySelector('.knowledge-upload-form .n-select'),tip=m.querySelector('.field-tip'),r=e=>e.getBoundingClientRect();return sections.length===2 && r(sections[1]).top>=r(sections[0]).bottom && (!tip || (r(tip).top>=r(select).bottom && r(tip).right<=r(m).right))})()"
    ),
    '知识上传步骤或字段说明出现重叠'
  )
  await delay(300)
  const uploadCapture = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-knowledge-upload.png'), Buffer.from(uploadCapture.data, 'base64'))
  await evaluate("document.querySelector('.knowledge-upload-form .n-select .n-base-selection').click()")
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option=>option.getClientRects().length))")
  )
  await evaluate("[...document.querySelectorAll('.n-base-select-option')].find(option=>option.getClientRects().length).click()")
  await until(() =>
    evaluate("document.querySelector('.knowledge-upload-form .n-select .n-base-selection-label')?.innerText.includes('运维助手')")
  )
  await evaluate("document.querySelector('.knowledge-upload-form .n-select .n-base-selection').click()")
  await call('Input.insertText', { text: 'custom-assistant' })
  await call('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
  await call('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
  await until(() =>
    evaluate("document.querySelector('.knowledge-upload-form .n-select .n-base-selection-label')?.innerText.includes('custom-assistant')")
  )
  await evaluate("document.querySelectorAll('.knowledge-upload-form .n-select .n-base-selection')[2].click()")
  await call('Input.insertText', { text: '消防设备' })
  await call('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
  await call('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
  await until(() => evaluate("document.querySelectorAll('.knowledge-upload-form .n-select')[2]?.innerText.includes('消防设备')"))
  await evaluate("document.querySelector('.knowledge-upload-dialog .n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal=>modal.getClientRects().length)"))
  await call('Emulation.setDeviceMetricsOverride', {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true
  }) /* 检查手机底部导航断点。 */
  await delay(250) /* 等待侧栏宽度过渡完成。 */
  const mobileNav = await evaluate(
    "(() => {const aside=document.querySelector('.app-sidebar'),r=aside.getBoundingClientRect();return {right:r.right,inert:aside.inert,toggle:document.querySelector('.app-topbar__toggle')?.getAttribute('aria-label')}})()"
  ) /* 手机端侧栏默认收起为抽屉。 */
  assert.ok(
    mobileNav.right <= 1 && mobileNav.inert && mobileNav.toggle === '打开菜单',
    `手机端导航应默认收起为抽屉：${JSON.stringify(mobileNav)}`
  )
  await evaluate("document.querySelector('.app-topbar__toggle').click()")
  await delay(300)
  const mobileBrand = await evaluate(
    "(() => {const aside=document.querySelector('.app-sidebar'),menu=aside.querySelector('.nav-item:not(.is-active)'),r=aside.getBoundingClientRect();return {left:r.left,right:r.right,viewport:innerWidth,mask:Boolean(document.querySelector('.app-sidebar-mask')),background:getComputedStyle(aside).backgroundColor,menu:getComputedStyle(menu).color}})()"
  ) /* 读取抽屉导航的尺寸与颜色。 */
  assert.ok(
    mobileBrand.left >= -1 &&
      mobileBrand.right <= mobileBrand.viewport &&
      mobileBrand.mask &&
      mobileBrand.background === 'rgb(245, 244, 237)' &&
      mobileBrand.menu === 'rgb(61, 61, 58)',
    `手机抽屉导航未沿用品牌色或布局溢出：${JSON.stringify(mobileBrand)}`
  ) /* 窄屏仍可读取导航入口。 */
  await evaluate("document.querySelector('.app-sidebar-mask').click()")
  await until(() => evaluate("!document.querySelector('.app-sidebar-mask')"))
  await openPage('用户与权限')
  await until(() => evaluate("Boolean(document.querySelector('.n-tabs-tab[data-name=users]'))"))
  await evaluate("document.querySelector('.n-tabs-tab[data-name=users]').click()") /* 默认显示角色管理，先切换到用户管理。 */
  await evaluate(
    "[...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button=>button.innerText.includes('添加用户')).click()"
  )
  await until(() => evaluate("Boolean(document.querySelector('.n-modal .user-editor'))"))
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.user-editor').closest('.n-modal'),r=m.getBoundingClientRect(),items=[...m.querySelector('.user-editor-grid').children],body=m.querySelector('.n-card-content');return r.left>=0&&r.right<=innerWidth+1&&items[1].getBoundingClientRect().top>=items[0].getBoundingClientRect().bottom&&body.scrollHeight>body.clientHeight})()"
    ),
    '窄屏添加用户弹窗溢出或无法滚动'
  )
  await evaluate("document.querySelector('.user-editor').closest('.n-modal').querySelector('.n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal=>modal.getClientRects().length)"))
  await openPage('知识库')
  await until(() => evaluate("Boolean([...document.querySelectorAll('button')].find(button=>button.innerText.includes('上传知识文档')))"))
  await evaluate("[...document.querySelectorAll('button')].find(button=>button.innerText.includes('上传知识文档')).click()")
  await until(() => evaluate("Boolean(document.querySelector('.knowledge-upload-dialog'))"))
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.knowledge-upload-dialog'),r=m.getBoundingClientRect(),fields=[...m.querySelector('.metadata-grid').children];return r.left>=0&&r.right<=innerWidth+1&&fields[1].getBoundingClientRect().top>=fields[0].getBoundingClientRect().bottom})()"
    ),
    '窄屏知识上传元数据字段未换行'
  )
  await evaluate("document.querySelector('.knowledge-upload-dialog .n-base-close').click()")
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal=>modal.getClientRects().length)"))
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  await openPage('协议开发') /* 检查上传源码弹窗的额外目标选择。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button=>button.innerText.includes('上传源码')))"
    )
  ) /* 等待协议工具栏。 */
  await evaluate(
    "[...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button=>button.innerText.includes('上传源码')).click()"
  ) /* 打开源码上传弹窗。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('编译选项')))"
    )
  ) /* 等待编译选项。 */
  const sourceLayout = await evaluate(
    "(() => {const modal=document.querySelector('.source-upload-dialog'),templates=modal.querySelector('.source-template-actions'),footer=modal.querySelector('.source-actions'),r=e=>e.getBoundingClientRect();return {templateButtons:templates.querySelectorAll('button').length,footerButton:footer.querySelector('button')?.innerText,withinModal:r(templates).left>=r(modal).left&&r(templates).right<=r(modal).right,hasBinding:modal.innerText.includes('绑定产品'),hasImmediatePublish:!!modal.querySelector('.n-switch')}})()"
  )
  assert.ok(
    sourceLayout.withinModal &&
      sourceLayout.templateButtons === 2 &&
      sourceLayout.footerButton.includes('构建并校验') &&
      !sourceLayout.hasBinding &&
      !sourceLayout.hasImmediatePublish,
    `源码上传应独立校验并保留模板下载：${JSON.stringify(sourceLayout)}`
  )
  await delay(300) /* 等待弹窗动画结束，截图应呈现最终布局。 */
  const sourceCapture = await call('Page.captureScreenshot', { format: 'png' }) /* 留存弹窗的合成数据截图供布局复核。 */
  await writeFile(join(tmpdir(), 'iot-source-upload-desktop.png'), Buffer.from(sourceCapture.data, 'base64')) /* 保存桌面视口截图。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-collapse-item__header-main').click()"
  ) /* 展开额外目标。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal .n-form-item')].find(item=>item.innerText.includes('额外编译目标') && item.getBoundingClientRect().height>0))"
    )
  ) /* 等待目标选择器真正展开。 */
  await delay(250) /* 等待折叠动画结束再检查滚动尺寸。 */
  assert.ok(
    await evaluate(
      "(() => {const modal=document.querySelector('.source-upload-dialog'),select=modal.querySelector('.n-collapse .n-base-selection'),help=modal.querySelector('.n-collapse small');return select && help && help.getBoundingClientRect().top>=select.getBoundingClientRect().bottom})()"
    ),
    '额外编译目标说明仍与下拉框挤在同一行'
  ) /* 编译说明应位于选择器下方。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal .n-form-item')].find(item=>item.innerText.includes('额外编译目标')).querySelector('.n-base-selection').click()"
  ) /* 展开多选菜单。 */
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option=>option.getClientRects().length))")
  ) /* 等待可见选项。 */
  const targetOption = await evaluate(
    "(() => {const option=[...document.querySelectorAll('.n-base-select-option')].find(item=>item.getClientRects().length),r=option.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;return {outsideModal:!option.closest('.n-modal'),x,y,visible:r.top>=0&&r.bottom<=innerHeight,hit:document.elementFromPoint(x,y)?.closest('.n-base-select-option')===option}})()"
  ) /* 检查菜单未被弹窗遮住。 */
  assert.ok(
    targetOption.outsideModal && targetOption.visible && targetOption.hit,
    `额外编译目标菜单被裁切或遮挡：${JSON.stringify(targetOption)}`
  ) /* 选项必须能接收真实鼠标事件。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mousePressed',
    x: targetOption.x,
    y: targetOption.y,
    button: 'left',
    clickCount: 1
  }) /* 用鼠标选择一个编译目标。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseReleased',
    x: targetOption.x,
    y: targetOption.y,
    button: 'left',
    clickCount: 1
  }) /* 完成点击。 */
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-modal .n-base-selection')].find(item=>item.innerText.includes('Linux')))")
  ) /* 确认实际多选值已更新。 */
  await call('Emulation.setDeviceMetricsOverride', {
    width: 1000,
    height: 600,
    deviceScaleFactor: 1,
    mobile: false
  }) /* 用短视口验证上传表单滚动。 */
  const sourceScroll = await evaluate(
    "(() => {const modal=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length),body=modal.querySelector('.n-card-content');body.scrollTop=250;return {top:body.scrollTop,scrollHeight:body.scrollHeight,clientHeight:body.clientHeight,modalHeight:modal.getBoundingClientRect().height,overflow:getComputedStyle(body).overflowY,modalDisplay:getComputedStyle(modal).display,bodyFlex:getComputedStyle(body).flex,modalMaxHeight:getComputedStyle(modal).maxHeight,children:[...modal.children].map(e=>[e.className,e.getBoundingClientRect().height])}})()"
  ) /* 读取上传弹窗滚动范围。 */
  assert.ok(sourceScroll.top > 0, `额外编译目标展开后上传源码正文无法滚动：${JSON.stringify(sourceScroll)}`) /* 弹窗正文仍可上下滚动。 */
  const sourceWheel = await evaluate(
    "(() => {const body=[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-card-content'),r=body.getBoundingClientRect();body.scrollTop=0;return {x:r.left+20,y:r.top+Math.min(90,r.height/2)}})()"
  ) /* 定位弹窗正文的滚轮测试点。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: sourceWheel.x,
    y: sourceWheel.y,
    deltaX: 0,
    deltaY: 300
  }) /* 验证向下滚动。 */
  await until(() => evaluate("document.querySelector('.source-upload-dialog .n-card-content').scrollTop>0")) /* 等待正文滚动。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: sourceWheel.x,
    y: sourceWheel.y,
    deltaX: 0,
    deltaY: -300
  }) /* 验证向上滚动。 */
  await until(() => evaluate("document.querySelector('.source-upload-dialog .n-card-content').scrollTop===0")) /* 确认返回顶部。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  await call('Emulation.setDeviceMetricsOverride', {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true
  }) /* 检查窄屏模板与提交按钮的换行。 */
  const mobileSourceLayout = await evaluate(
    "(() => {const modal=document.querySelector('.source-upload-dialog'),panel=modal.querySelector('.source-template-actions'),footer=modal.querySelector('.source-actions'),button=footer.querySelector('button'),r=e=>e.getBoundingClientRect();return {viewport:innerWidth,documentWidth:document.documentElement.scrollWidth,modalRight:r(modal).right,panelRight:r(panel).right,buttonRight:r(button).right,footerWidth:r(footer).width,buttonWidth:r(button).width}})()"
  ) /* 读取手机视口中各分组的宽度。 */
  assert.ok(
    mobileSourceLayout.documentWidth <= mobileSourceLayout.viewport + 2 &&
      mobileSourceLayout.modalRight <= mobileSourceLayout.viewport + 1 &&
      mobileSourceLayout.panelRight <= mobileSourceLayout.viewport + 1 &&
      mobileSourceLayout.buttonRight <= mobileSourceLayout.viewport + 1,
    `上传源码窄屏横向溢出：${JSON.stringify(mobileSourceLayout)}`
  ) /* 窄屏仍可完整看到模板与提交按钮。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()"
  ) /* 关闭上传弹窗。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待弹窗关闭。 */
  for (const [page, label] of [
    ['告警中心', '关闭告警'],
    ['告警规则', '删除'],
    ['用户与权限', '删除']
  ]) {
    /* 检查三个列表中的危险操作可见。 */
    await openPage(page) /* 打开目标列表。 */
    await until(() =>
      evaluate(
        `Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button => button.innerText.trim() === ${JSON.stringify(label)}))`
      )
    ) /* 等待目标按钮。 */
    const buttonColor = await evaluate(
      `(() => {const button=[...document.querySelectorAll('.n-data-table-tbody button')].find(item=>item.innerText.trim()===${JSON.stringify(label)}),s=getComputedStyle(button);return {color:s.color,background:s.backgroundColor,disabled:button.disabled,className:button.className}})()`
    ) /* 读取真实颜色。 */
    assert.ok(
      buttonColor.color !== buttonColor.background && buttonColor.color !== 'rgb(255, 255, 255)',
      `${page}的${label}按钮文字不可见：${JSON.stringify(buttonColor)}`
    ) /* 危险按钮文字不能与浅色背景融为一体。 */
  } /* 结束危险操作检查。 */
  await openPage('告警中心') /* 复现告警详情的长报文滚动。 */
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('查看详情')))")
  ) /* 等待告警列表。 */
  await evaluate(
    "[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('查看详情')).click()"
  ) /* 打开告警详情。 */
  await until(() => evaluate("Boolean(document.querySelector('.alarm-detail-dialog details.raw-detail'))"))
  await evaluate("document.querySelector('.alarm-detail-dialog details.raw-detail').open = true") /* 原始数据默认折叠，展开后检查长报文。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal')].find(modal=>modal.getClientRects().length && modal.innerText.includes('diagnosticLines')))"
    )
  ) /* 等待长报文显示。 */
  await until(() =>
    evaluate("Boolean(document.querySelector('.alarm-detail-dialog .analysis-grid'))")
  ) /* 等待内层研判卡片加载，复现滚轮停住的区域。 */
  assert.ok(
    await evaluate(
      "(() => {const modal=document.querySelector('.alarm-detail-dialog'),body=modal.querySelector(':scope > .n-card-content'),inner=modal.querySelector('.top-gap > .n-card-content');return body && inner && getComputedStyle(body).overflowY==='auto' && getComputedStyle(inner).overflowY==='visible'})()"
    ),
    '告警研判卡片错误地成为第二个滚动容器'
  ) /* 详情只允许最外层正文接收滚轮。 */
  const analysisWheel = await evaluate(
    "(() => {const modal=document.querySelector('.alarm-detail-dialog'),body=modal.querySelector(':scope > .n-card-content'),inner=modal.querySelector('.top-gap > .n-card-content');inner.scrollIntoView({block:'center'});const r=inner.getBoundingClientRect();return {x:r.left+Math.min(100,r.width/2),y:Math.max(120,Math.min(innerHeight-120,r.top+Math.min(80,r.height/2))),top:body.scrollTop}})()"
  ) /* 把鼠标放在实际研判内容上。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: analysisWheel.x,
    y: analysisWheel.y,
    deltaX: 0,
    deltaY: 120
  }) /* 第一次滚轮下滑。 */
  await until(() =>
    evaluate(`document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop>${analysisWheel.top}`)
  ) /* 外层正文应响应第一次滚轮。 */
  const analysisFirstTop = await evaluate(
    "document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop"
  ) /* 记录第一次滚动位置。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: analysisWheel.x,
    y: analysisWheel.y,
    deltaX: 0,
    deltaY: 120
  }) /* 在同一位置连续下滑。 */
  await until(() =>
    evaluate(`document.querySelector('.alarm-detail-dialog > .n-card-content').scrollTop>${analysisFirstTop}`)
  ) /* 第二次滚轮不能被内层卡片截断。 */
  assert.ok(
    await evaluate(
      "(() => {const modal=[...document.querySelectorAll('.n-modal')].find(item=>item.getClientRects().length),pre=modal.querySelector('pre');return pre.scrollHeight<=pre.clientHeight+1 && getComputedStyle(pre).overflowY==='visible'})()"
    ),
    '告警详情的 JSON 报文仍单独截取滚轮'
  ) /* 详情只能保留外层正文滚动。 */
  const alarmWheel = await evaluate(
    "(() => {const modal=[...document.querySelectorAll('.n-modal')].find(item=>item.getClientRects().length),body=modal.querySelector('.n-card-content'),pre=modal.querySelector('pre');body.scrollTop=Math.max(0,pre.offsetTop-160);const rect=pre.getBoundingClientRect();return {x:rect.left+Math.min(100,rect.width/2),y:Math.max(120,Math.min(innerHeight-120,rect.top+80)),top:body.scrollTop}})()"
  ) /* 将滚轮定位在报文区域。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: alarmWheel.x,
    y: alarmWheel.y,
    deltaX: 0,
    deltaY: 260
  }) /* 在报文上向下滚动。 */
  await until(() => evaluate(`document.querySelector('.n-modal .n-card-content').scrollTop>${alarmWheel.top}`)) /* 外层详情应继续滚动。 */
  await call('Input.dispatchMouseEvent', {
    type: 'mouseWheel',
    x: alarmWheel.x,
    y: alarmWheel.y,
    deltaX: 0,
    deltaY: -260
  }) /* 在报文上向上滚动。 */
  await until(() => evaluate(`document.querySelector('.n-modal .n-card-content').scrollTop<=${alarmWheel.top}`)) /* 验证双向滚动。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()"
  ) /* 关闭告警详情。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待详情关闭。 */
  await openPage('设备模板')
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.product-name')].find(button=>button.innerText.includes('用户信息传输装置')))")
  )
  await evaluate("[...document.querySelectorAll('.product-name')].find(button=>button.innerText.includes('用户信息传输装置')).click()")
  await until(() => evaluate("document.querySelector('.product-preparation h2')?.innerText==='用户信息传输装置'"))
  await evaluate("document.querySelectorAll('.preparation-steps button')[1].click()")
  await until(() => evaluate("document.querySelector('.product-preparation .n-base-selection')?.innerText.includes('演示消防协议')"))
  await evaluate("document.querySelectorAll('.preparation-steps button')[2].click()")
  await until(() =>
    evaluate(
      "(()=>{const b=[...document.querySelectorAll('.product-preparation button')].find(b=>b.innerText==='添加公共监听');if(!b)return false;b.click();return true})()"
    )
  )
  await until(() => evaluate("Boolean(document.querySelector('.preparation-profile'))"))
  await call('Emulation.setDeviceMetricsOverride', { width: 1000, height: 600, deviceScaleFactor: 1, mobile: false })
  const gatewayLayout = await evaluate(
    "(()=>{const body=document.querySelector('.app-content'),profile=document.querySelector('.preparation-profile');body.scrollTop=300;return {scrollTop:body.scrollTop,right:profile.getBoundingClientRect().right,viewport:innerWidth}})()"
  )
  assert.ok(
    gatewayLayout.scrollTop > 0 && gatewayLayout.right <= gatewayLayout.viewport,
    `模板公共连接表单无法滚动或溢出：${JSON.stringify(gatewayLayout)}`
  )
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
  await openPage('备份中心') /* 检查备份详情说明。 */
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('详情 / 文件')))")
  ) /* 等待备份行。 */
  await evaluate(
    "[...document.querySelectorAll('.n-data-table-tbody button')].find(button=>button.innerText.includes('详情 / 文件')).click()"
  ) /* 打开备份详情。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal .section-heading span')].find(item=>item.innerText.includes('清单中的每个文件')))"
    )
  ) /* 等待说明文字。 */
  assert.ok(
    await evaluate(
      "(() => {const text=[...document.querySelectorAll('.n-modal .section-heading span')].find(item=>item.innerText.includes('清单中的每个文件'));return getComputedStyle(text).color!=='rgb(242, 242, 244)'})()"
    ),
    '备份详情说明文字与白色背景过于接近'
  ) /* 明确使用深色辅助文本。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal')].find(m=>m.getClientRects().length).querySelector('.n-base-close').click()"
  ) /* 关闭备份详情。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(m=>m.getClientRects().length)")) /* 等待详情关闭。 */
  await freshSession()
  await evaluate(
    "(() => {const base='iot:ai-history:v1:'+localStorage.getItem('iot_tenant')+':'+localStorage.getItem('iot_user');for(const [id,text] of [['ops-assistant','A 专属对话'],['custom-assistant','B 专属对话']]){const state={version:1,selectedWorkflowId:id,conversationId:'conversation-'+id,messages:[{id:'message-'+id,role:'user',status:'succeeded',text}],runs:[]};localStorage.setItem(base+':'+encodeURIComponent(id),JSON.stringify(state));if(id==='ops-assistant')localStorage.setItem(base,JSON.stringify(state))}})()"
  )
  await openPage('智能助手') /* 检查智能助手滚动区。 */
  await until(() =>
    evaluate("Boolean(document.querySelector('.chat-workflow-select') && document.querySelector('.chat-log'))")
  ) /* 等待工作流切换与对话区。 */
  await until(() => evaluate("document.querySelector('.chat-log')?.innerText.includes('A 专属对话')"))
  assert.deepEqual(await auditContrast('.message-row.user'), [], '智能助手用户消息文字与气泡背景对比不足')
  {
    const capture = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(join(tmpdir(), 'iot-ai-user-message.png'), Buffer.from(capture.data, 'base64'))
  }
  await evaluate("document.querySelector('.chat-workflow-select .n-base-selection').click()")
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option=>option.innerText.includes('示例智能体')))")
  )
  await evaluate("[...document.querySelectorAll('.n-base-select-option')].find(option=>option.innerText.includes('示例智能体')).click()")
  await until(() => evaluate("document.querySelector('.chat-log')?.innerText.includes('B 专属对话')"))
  assert.ok(
    await evaluate(
      "!document.querySelector('.chat-log').innerText.includes('A 专属对话') && document.querySelector('.quick-prompts').innerText.includes('示例智能体')"
    ),
    '切换工作流后仍显示上一个插件的会话或快捷操作'
  )
  assert.ok(await evaluate("document.querySelector('.chat-workflow-select').getBoundingClientRect().width>=220"), '工作流切换控件过窄')
  assert.ok(
    await evaluate(
      "(() => {const log=document.querySelector('.chat-log');for(let i=0;i<30;i++){const item=document.createElement('p');item.textContent='滚动测试';log.append(item)}log.scrollTop=200;return log.scrollTop>0})()"
    ),
    '智能助手对话记录无法向下滚动'
  ) /* 对话滚动容器需保持有效。 */
  await openPage('设备模板')
  await evaluate(
    "[...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button => button.textContent.includes('新建设备模板')).click()"
  )
  await until(() => evaluate("Boolean(document.querySelector('.product-preparation'))"))
  await evaluate(
    "[...document.querySelectorAll('.product-preparation .n-form-item')].find(item => item.innerText.includes('设备分类')).querySelector('.n-base-selection').click()"
  )
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-base-select-option')].find(option => option.getClientRects().length))")
  )
  await evaluate("[...document.querySelectorAll('.n-base-select-option')].find(option => option.getClientRects().length).click()")
  await openPage('设备管理') /* 打开设备管理检查详情抽屉。 */
  await until(() => evaluate("Boolean(document.querySelector('.n-data-table-tbody .device-name'))")) /* 等待示例设备行。 */
  await evaluate("document.querySelector('.n-data-table-tbody .device-name').click()") /* 打开设备连接详情。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-drawer')].find(drawer => drawer.getClientRects().length && drawer.innerText.includes('设备连接与数据')))"
    )
  ) /* 确认抽屉显示。 */
  assert.ok(
    await evaluate("document.querySelector('.onboarding-diagnosis')?.innerText.includes('平台对外地址未配置')"),
    '设备详情未显示后端诊断结论'
  )
  await evaluate("document.querySelector('.n-drawer .n-base-close').click()") /* 关闭受控抽屉。 */
  await until(() =>
    evaluate("![...document.querySelectorAll('.n-drawer')].some(drawer => drawer.getClientRects().length)")
  ) /* 确认抽屉解除挂载。 */
  assert.ok(await clickListAction('编辑'), '设备行缺少编辑入口') /* 打开设备编辑表单。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('设备角色')))"
    )
  ) /* 确认角色控件显示。 */
  await evaluate("document.querySelector('.n-modal .n-collapse-item__header-main').click()") /* 展开更多设置。 */
  await until(() => evaluate("Boolean(document.querySelector('.n-modal [role=switch]'))")) /* 等待设备状态开关。 */
  const initialSwitch = await evaluate("document.querySelector('.n-modal [role=switch]').getAttribute('aria-checked')") /* 记录原状态。 */
  await evaluate("document.querySelector('.n-modal [role=switch]').click()") /* 切换字符串型设备状态。 */
  await until(() =>
    evaluate(`document.querySelector('.n-modal [role=switch]').getAttribute('aria-checked') !== ${JSON.stringify(initialSwitch)}`)
  ) /* 确认状态反转。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 离开未保存的设备表单。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-dialog button')].find(button=>button.getClientRects().length&&button.innerText.trim()==='放弃修改'))"
    )
  ) /* 有未保存修改时先确认放弃。 */
  await evaluate(
    "[...document.querySelectorAll('.n-dialog button')].find(button=>button.getClientRects().length&&button.innerText.trim()==='放弃修改').click()"
  )
  await until(() =>
    evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")
  ) /* 确认弹窗关闭。 */
  await openPage('原始报文') /* 打开带解析结果的示例报文。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(row => row.innerText.includes('raw-demo')))"
    )
  ) /* 确认列表取得样本。 */
  await evaluate(
    "[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(row => row.innerText.includes('raw-demo')).querySelector('button').click()"
  ) /* 查看报文详情。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('报文详情与解析结果')))"
    )
  ) /* 等待详情弹窗。 */
  assert.ok(
    await evaluate(
      "document.querySelector('.n-modal')?.innerText.includes('标准解析结果') && document.querySelector('.n-modal')?.innerText.includes('原始报文')"
    ),
    '报文详情缺少原文与解析结果标签'
  ) /* 两种记录都须可访问。 */
  assert.ok(
    await evaluate("document.querySelector('.n-modal')?.innerText.includes('temperature')"),
    '报文详情未显示解析后的字段'
  ) /* 默认展示解析内容。 */
  await evaluate(
    "[...document.querySelectorAll('.n-modal .n-tabs-tab')].find(tab => tab.innerText.includes('原始报文')).click()"
  ) /* 切换到原始记录。 */
  await until(() => evaluate("document.querySelector('.n-modal')?.innerText.includes('AA01')")) /* 确认原始载荷可读。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 关闭报文详情。 */
  await openPage('运行总览') /* 检查总览页交互。 */
  await until(() => evaluate("Boolean(document.querySelector('.dashboard-page .n-radio-group'))")) /* 等待趋势切换。 */
  assert.ok(
    await evaluate(
      "(() => {const card=document.querySelector('.stat-card');const label=card.querySelector('span').getBoundingClientRect(),value=card.querySelector('strong').getBoundingClientRect(),note=card.querySelector('small').getBoundingClientRect();return label.bottom<value.top && value.bottom<note.top})()"
    ),
    '总览统计卡片文字相互重叠'
  ) /* 数值和说明必须分行显示。 */
  assert.ok(
    await evaluate("document.querySelector('.app-shell').getBoundingClientRect().bottom <= innerHeight + 1"),
    '工作区超过视口高度，页面无法上下滚动'
  ) /* 工作区必须受视口约束。 */
  assert.ok(
    await evaluate("(() => {const e=document.querySelector('.app-content');e.scrollTop=200;return e.scrollTop>0})()"),
    '总览内容无法纵向滚动'
  ) /* 超长页面须可滚动。 */
  await evaluate("document.querySelector('.app-content').scrollTop=0") /* 恢复首屏。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 1000, y: 650, deltaX: 0, deltaY: 320 }) /* 模拟向下滚动。 */
  await until(() => evaluate("document.querySelector('.app-content').scrollTop > 0")) /* 确认鼠标滚轮可用。 */
  await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 1000, y: 650, deltaX: 0, deltaY: -320 }) /* 模拟向上滚动。 */
  await until(() => evaluate("document.querySelector('.app-content').scrollTop === 0")) /* 确认双向滚动。 */
  if (await evaluate("(() => {const nav=document.querySelector('.app-sidebar__nav');return nav.scrollHeight>nav.clientHeight+1})()")) {
    /* 菜单超出高度时必须能滚动到底部。 */
    await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 100, y: 550, deltaX: 0, deltaY: 300 }) /* 检查侧栏向下滚动。 */
    await until(() => evaluate("document.querySelector('.app-sidebar__nav').scrollTop > 0")) /* 确认底部菜单可达。 */
    await call('Input.dispatchMouseEvent', { type: 'mouseWheel', x: 100, y: 550, deltaX: 0, deltaY: -300 }) /* 检查侧栏向上滚动。 */
    await until(() => evaluate("document.querySelector('.app-sidebar__nav').scrollTop === 0")) /* 确认侧栏双向滚动。 */
  } else
    assert.ok(
      await evaluate(
        "(() => {const items=[...document.querySelectorAll('.nav-item')],nav=document.querySelector('.app-sidebar__nav').getBoundingClientRect();return items.every(item=>item.getBoundingClientRect().bottom<=nav.bottom+1)})()"
      ),
      '侧栏菜单未完整显示且无法滚动'
    )
  assert.ok(
    await evaluate(
      "(() => {const b=[...document.querySelectorAll('.dashboard-page .n-radio-button')];return b.length===2 && getComputedStyle(b[0]).backgroundColor!==getComputedStyle(b[1]).backgroundColor})()"
    ),
    '7 天与 30 天切换按钮缺少清晰的选中样式'
  ) /* 趋势切换状态须明确。 */
  await evaluate("[...document.querySelectorAll('.dashboard-page .n-radio-button')][1].click()") /* 切换到近 30 天。 */
  await until(() =>
    evaluate("[...document.querySelectorAll('.dashboard-page .n-radio-button')][1].classList.contains('n-radio-button--checked')")
  ) /* 确认选中状态同步。 */
  await evaluate('document.querySelector(\'[aria-label="告警提醒设置"]\').click()') /* 检查设置弹窗。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-modal')].find(modal => modal.getClientRects().length && modal.innerText.includes('告警提醒设置')))"
    )
  ) /* 等待设置弹窗。 */
  assert.ok(
    await evaluate("document.querySelector('.n-modal')?.innerText.includes('当前静默时段：')"),
    '告警提醒设置缺少静默时段说明'
  ) /* 提示标题必须显示。 */
  await delay(250) /* 等待弹窗动画结束后检查布局。 */
  const settingsCapture = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-naive-settings.png'), Buffer.from(settingsCapture.data, 'base64')) /* 留存设置弹窗的合成数据截图。 */
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }) /* 检查窄屏设置。 */
  assert.ok(
    await evaluate(
      "(() => {const m=document.querySelector('.n-modal'),r=m.getBoundingClientRect();return r.left>=0 && r.right<=innerWidth+1 && [...m.querySelectorAll('.alert-setting-row,.alert-quiet-times,.n-time-picker')].every(e=>{const c=e.getBoundingClientRect();return c.left>=r.left+6 && c.right<=r.right-6})})()"
    ),
    '窄屏告警设置内容被裁切'
  ) /* 控件须留在弹窗范围内。 */
  await evaluate("document.querySelector('.n-modal .n-base-close').click()") /* 关闭设置弹窗。 */
  await until(() => evaluate("![...document.querySelectorAll('.n-modal')].some(modal => modal.getClientRects().length)")) /* 等待关闭。 */
  assert.ok(
    await evaluate("document.querySelector('.app-shell').getBoundingClientRect().bottom <= innerHeight + 1"),
    '窄屏工作区超过视口高度'
  ) /* 窄屏滚动区域也须留在视口内。 */
  for (const name of pages) {
    await openPage(name)
    await delay(120)
    const width = await evaluate(
      "({document:document.documentElement.scrollWidth,viewport:innerWidth,content:document.querySelector('.app-content').getBoundingClientRect().width})"
    )
    assert.ok(
      width.document <= width.viewport + 2 && width.content <= width.viewport + 2,
      `${name} 手机视图横向溢出：${JSON.stringify(width)}`
    )
    const shot = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(join(tmpdir(), `iot-mobile-${pages.indexOf(name)}.png`), Buffer.from(shot.data, 'base64'))
  }
  console.log(`PASS: ${pages.length} 个主页面手机视图无横向溢出`)
  let mobileOverlayPage = ''
  for (const [pageName, actionName, tabName] of overlayCases) {
    if (skippedOverlays.has(`${pageName}/${actionName}`)) continue
    if (pageName !== mobileOverlayPage) {
      await freshSession()
      mobileOverlayPage = pageName
    }
    await openPage(pageName)
    if (tabName) {
      await until(() =>
        evaluate(
          `Boolean([...document.querySelectorAll('.n-tabs-tab,.n-radio-button')].find(tab=>tab.innerText.includes(${JSON.stringify(tabName)})))`
        )
      )
      await evaluate(
        `[...document.querySelectorAll('.n-tabs-tab,.n-radio-button')].find(tab=>tab.innerText.includes(${JSON.stringify(tabName)})).click()`
      )
    }
    assert.ok(await until(() => clickListAction(actionName)).catch(() => false), `${pageName} 手机视图缺少“${actionName}”入口`)
    await until(() =>
      evaluate(
        "Boolean([...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden'))"
      )
    )
    if (pageName === '告警规则' && actionName === '手动添加规则') {
      await evaluate("document.querySelector('.n-modal .rule-field-reference summary').click()")
      assert.ok(
        await evaluate(
          "document.querySelectorAll('.rule-reference-cards article').length>0 && getComputedStyle(document.querySelector('.rule-reference-cards')).display==='grid'"
        ),
        '手机端字段参考应按卡片逐条阅读'
      )
    }
    await delay(550)
    if (pageName === '设备管理' && actionName === '详情') {
      const topShot = await call('Page.captureScreenshot', { format: 'png' })
      await writeFile(join(tmpdir(), 'iot-device-connection-mobile-top.png'), Buffer.from(topShot.data, 'base64'))
    }
    const layout = await evaluate(
      "(()=>{const m=[...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden'),r=m.getBoundingClientRect(),body=m.querySelector('.n-card-content,.n-drawer-body-content-wrapper'),fields=[...m.querySelectorAll('.n-form-item,.n-input,.n-select,.n-alert')].filter(e=>e.getClientRects().length),outside=fields.filter(e=>{const x=e.getBoundingClientRect();return x.left<r.left-2||x.right>r.right+2}).map(e=>e.innerText.slice(0,25));let reachable=true;if(body&&body.scrollHeight>body.clientHeight+2){body.scrollTop=body.scrollHeight;reachable=body.scrollTop>0}return {rect:{left:r.left,right:r.right,top:r.top,bottom:r.bottom},viewport:{width:innerWidth,height:innerHeight},reachable,outside,bodyOverflow:body?body.scrollWidth>body.clientWidth+2:false}})()"
    )
    assert.ok(
      layout.rect.left >= -1 &&
        layout.rect.right <= layout.viewport.width + 1 &&
        layout.rect.top >= -1 &&
        layout.rect.bottom <= layout.viewport.height + 1 &&
        layout.reachable &&
        !layout.outside.length &&
        !layout.bodyOverflow,
      `${pageName} / ${actionName} 手机弹层溢出：${JSON.stringify(layout)}`
    )
    const shot = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(
      join(
        tmpdir(),
        `iot-mobile-overlay-${overlayCases.findIndex(item => item[0] === pageName && item[1] === actionName && item[2] === tabName)}.png`
      ),
      Buffer.from(shot.data, 'base64')
    )
    await evaluate(
      "[...document.querySelectorAll('.n-modal,.n-drawer')].find(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden')?.querySelector('.n-base-close')?.click()"
    )
    await until(() =>
      evaluate(
        "![...document.querySelectorAll('.n-modal,.n-drawer')].some(item=>item.getClientRects().length&&getComputedStyle(item).visibility!=='hidden')"
      )
    )
  }
  console.log(`PASS: ${overlayCases.length} 个弹层手机视图布局与滚动检查`)
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 560, deviceScaleFactor: 1, mobile: true })
  await openPage('设备模板')
  await evaluate(
    "[...document.querySelectorAll(':is(.page-toolbar,.filter-bar) button')].find(button => button.textContent.includes('新建设备模板')).click()"
  )
  await until(() => evaluate("Boolean(document.querySelector('.product-preparation'))"))
  assert.ok(
    await evaluate(
      "(() => {const body=document.querySelector('.app-content'),form=document.querySelector('.product-preparation');body.scrollTop=200;return body.getBoundingClientRect().bottom<=innerHeight+1 && body.scrollTop>0 && form.getBoundingClientRect().right<=innerWidth+1})()"
    ),
    '窄屏模板准备表单无法上下滚动或横向溢出'
  )
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false }) /* 恢复桌面视口。 */
  // 添加设备向导的各接入方式由 onboarding-modes-check.mjs 单独检查。
  await openPage('用户与权限') /* 校验删除确认共享弹窗的说明与取消操作。 */
  await until(() => evaluate("Boolean(document.querySelector('.n-tabs-tab[data-name=users]'))"))
  await evaluate("document.querySelector('.n-tabs-tab[data-name=users]').click()") /* 默认显示角色管理，先切换到用户管理。 */
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-data-table-tbody button')].find(b=>b.innerText==='删除'))"))
  await evaluate("[...document.querySelectorAll('.n-data-table-tbody button')].find(b=>b.innerText==='删除').click()")
  await until(() =>
    evaluate("Boolean([...document.querySelectorAll('.n-dialog')].find(d=>d.getClientRects().length&&d.innerText.includes('删除确认')))")
  )
  await delay(350)
  assert.ok(
    await evaluate(
      "(() => {const d=[...document.querySelectorAll('.n-dialog')].find(x=>x.getClientRects().length),r=d.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1&&d.innerText.includes('操作员')&&d.innerText.includes('取消')&&d.innerText.includes('确定')})()"
    ),
    '删除用户确认框缺少对象、操作说明或取消入口'
  )
  await evaluate(
    "[...document.querySelectorAll('.n-dialog')].find(d=>d.getClientRects().length).querySelector('.n-dialog__action button:first-child').click()"
  )
  await until(() => evaluate("![...document.querySelectorAll('.n-dialog')].some(d=>d.getClientRects().length)"))
  await evaluate('document.querySelector(\'[aria-label="打开用户菜单"]\').click()') /* 打开账户菜单。 */
  await until(() =>
    evaluate(
      "Boolean([...document.querySelectorAll('.n-dropdown-option')].find(option => option.getClientRects().length && option.innerText.includes('退出登录')))"
    )
  ) /* 确认账户操作可见。 */
  assert.notEqual(
    await evaluate(
      "getComputedStyle([...document.querySelectorAll('.n-dropdown-menu')].find(menu => menu.getClientRects().length)).display"
    ),
    'flex',
    '账户菜单被横向 flex 样式破坏'
  ) /* 菜单须按列表纵向排布。 */
  const logoutLayout = await evaluate(
    "(() => {const label=[...document.querySelectorAll('.ui-dropdown-label')].find(item=>item.getClientRects().length&&item.innerText.includes('退出登录')),icon=label.querySelector('svg').getBoundingClientRect(),node=[...label.childNodes].find(n=>n.nodeType===3&&n.textContent.includes('退出登录')),range=document.createRange();range.selectNodeContents(node);const lines=range.getClientRects(),text=range.getBoundingClientRect();return {lines:lines.length,iconRight:icon.right,textLeft:text.left,iconMid:icon.top+icon.height/2,textTop:text.top,textBottom:text.bottom,display:getComputedStyle(label).display}})()"
  ) /* 读取退出菜单布局：图标与文字的实际位置，不依赖字体宽度。 */
  assert.ok(
    logoutLayout.lines === 1 &&
      logoutLayout.iconRight <= logoutLayout.textLeft + 1 &&
      logoutLayout.iconMid >= logoutLayout.textTop &&
      logoutLayout.iconMid <= logoutLayout.textBottom &&
      logoutLayout.display === 'inline-flex',
    `退出登录图标与文字未排在同一行：${JSON.stringify(logoutLayout)}`
  ) /* 菜单项完整显示。 */
  await evaluate(
    "[...document.querySelectorAll('.n-dropdown-option')].find(option => option.getClientRects().length && option.innerText.includes('退出登录')).querySelector('.n-dropdown-option-body').click()"
  ) /* 执行退出登录。 */
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))")) /* 确认退出后返回登录页。 */
  assert.deepEqual(failures, [], `页面脚本异常：${failures.join(' | ')}`) /* 不接受未处理的页面错误。 */
  if (warnings.length)
    console.log(`页面警告 ${warnings.length} 条：${[...new Set(warnings)].slice(0, 8).join(' | ')}`) /* 输出需继续排查的框架警告。 */
  console.log(`PASS: ${pages.length} 个主页面正常渲染`) /* 报告界面检查结果。 */
} finally {
  await browser?.close()
}
