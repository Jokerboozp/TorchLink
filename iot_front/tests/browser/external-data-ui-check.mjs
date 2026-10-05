// Real browser interactions against the real Vue page, with isolated synthetic API responses.
// This verifies rendering and interaction; it does not claim backend ingestion acceptance.
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { startBrowser } from '../helpers/browser.mjs'
import { blankEndpoint } from '../../src/externalData.js'

const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:5173'
const endpoint = { ...blankEndpoint('source-1'), id: 'endpoint-1', revision: 1, name: '视频分析告警', enabled: false }
let browser
try {
  browser = await startBrowser({ args: ['--use-mock-keychain', '--password-store=basic'] })
  const { call, evaluate, until, errors } = browser
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source: `
    localStorage.clear();
    for (const [key,value] of Object.entries({iot_token:'fixture',iot_tenant:'fixture',iot_user:'operator',iot_role:'admin',iot_permissions:'["*"]'})) localStorage.setItem(key,value);
    window.__externalRequests=[];
    const rows={sources:[{id:'source-1',revision:1,name:'视频分析平台',username:'operator',enabled:false,allowedHosts:['api.example.test:443'],auth:{type:'bearer',secretSet:true}}],endpoints:[${JSON.stringify(endpoint)}],bindings:[{id:'binding-1',revision:1,sourceId:'source-1',externalId:'camera-01',kind:'camera',targetId:'platform-camera-1'}],records:[{id:'record-1',revision:1,sourceId:'source-1',endpointId:'endpoint-1',status:'WAITING_BINDING',createdAt:Date.now(),body:{raw:{eventCode:'fire-1',camera:'camera-01'},event:{id:'fire-1',objectId:'camera-01',timestamp:Date.now()},configRevision:1,error:'外部编号尚未关联平台对象',attempts:1}}],jobs:[{id:'job-1',revision:1,sourceId:'source-1',endpointId:'endpoint-1',status:'COMPLETED',createdAt:Date.now(),body:{manual:true,from:Date.now()-3600000,to:Date.now(),pages:3,received:27,attempts:0}}]};
    const originalFetch=window.fetch.bind(window);
    window.fetch=async(input,options={})=>{
      const url=new URL(String(input),location.origin),path=url.pathname,method=options.method||'GET';
      if(!path.startsWith('/api/'))return originalFetch(input,options);
      let body={items:[],total:0};
      if(path==='/api/v1/auth/me')body={tenantId:'fixture',username:'operator',role:'admin',permissions:['*']};
      if(path==='/api/v1/events')body={permissions:['*'],alarms:[],devices:[]};
      if(path==='/api/v1/mqtt/token')body={websocketUrl:'ws://127.0.0.1:1',username:'fixture',token:'fixture',subscriptions:[]};
      if(path.startsWith('/api/v1/external-data/')){
        const [kind,id,action]=path.slice('/api/v1/external-data/'.length).split('/');
        const payload=options.body?JSON.parse(options.body):null;
        window.__externalRequests.push({kind,id,action,method,payload});
        if(method==='GET'&&!id)body={items:rows[kind]||[],total:rows[kind]?.length||0};
        else if(kind==='records'&&method==='GET')body={entry:rows.records[0],receipt:{id:'receipt-1'}};
        else if(action==='test')body={items:[{raw:payload.sample,event:{id:'preview-1',objectId:'camera-01'},filtered:false}]};
        else if(action==='retry')body={status:'PENDING'};
        else if(action==='rotate-key')body={key:'fixture-receiver-key',receiveUrl:location.origin+'/api/external/fixture/endpoint-1'};
        else if(method==='POST'||method==='PUT'){const row={...payload,id:id||'created-source',revision:2};rows[kind]=rows[kind].filter(x=>x.id!==row.id).concat(row);body=row;}
      }
      return new Response(JSON.stringify(body),{headers:{'Content-Type':'application/json'}});
    };
  `
  })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="外部数据接入"]'))`), 'external menu')
  await evaluate(`document.querySelector('.nav-item[aria-label="外部数据接入"]').click()`)
  await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('视频分析平台')`), 'source list')
  const click = async text => {
    const found = await evaluate(
      `(() => {const b=[...document.querySelectorAll('button')].find(b=>b.getClientRects().length&&b.innerText.trim()===${JSON.stringify(text)});if(!b)return false;b.click();return true})()`
    )
    assert.equal(found, true, `button ${text}`)
  }
  const fill = async (label, value) => {
    const found = await evaluate(
      `(() => {const item=[...document.querySelectorAll('.n-form-item')].find(e=>e.getClientRects().length&&e.querySelector('.n-form-item-label')?.innerText.includes(${JSON.stringify(label)}));const input=item?.querySelector('input,textarea');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));return true})()`
    )
    assert.equal(found, true, `input ${label}`)
  }
  await click('编辑')
  await until(() => evaluate(`Boolean(document.querySelector('.external-editor'))`))
  await fill('来源名称', '已编辑的视频来源')
  await click('保存配置')
  await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('已编辑的视频来源')`))
  const saved = await evaluate(`window.__externalRequests.find(x=>x.kind==='sources'&&x.method==='PUT').payload`)
  assert.equal(saved.revision, 1)
  assert.equal(saved.auth.secretSet, true)
  assert.ok(!saved.auth.secret)
  await click('接入接口')
  await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('视频分析告警')`))
  await click('编辑')
  await until(() => evaluate(`Boolean(document.querySelector('.editor-steps'))`))
  await click('下一步')
  await until(() => evaluate(`document.querySelector('.external-editor')?.innerText.includes('添加字段')`))
  assert.ok(await evaluate(`document.querySelectorAll('.field-rule').length >= 3`))
  await click('下一步')
  await click('保存配置')
  await until(() => evaluate(`!document.querySelector('.external-editor')?.getClientRects().length`))
  await click('接收记录')
  await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('等待编号绑定')`))
  await click('查看详情')
  await until(() => evaluate(`Boolean(document.querySelector('.raw-grid'))`))
  assert.equal(
    await evaluate(
      `document.querySelector('.raw-grid').innerText.includes('eventCode') && document.querySelector('.raw-grid').innerText.includes('objectId')`
    ),
    true
  )
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: false })
  await until(
    () => evaluate(`getComputedStyle(document.querySelector('.raw-grid')).gridTemplateColumns.split(' ').length===1`),
    'stacked narrow detail'
  )
  await until(
    () =>
      evaluate(
        `(() => {const modal=document.querySelector('.n-modal');if(!modal)return false;const r=modal.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1&&getComputedStyle(modal).opacity==='1'})()`
      ),
    'narrow dialog remains within viewport'
  )
  await evaluate(`new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))`)
  const overflow = await evaluate(`document.documentElement.scrollWidth > innerWidth + 2`)
  assert.equal(overflow, false, 'no whole-page horizontal overflow')
  const screenshot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile('/tmp/torchlink-external-data-narrow.png', Buffer.from(screenshot.data, 'base64'))
  await evaluate(`document.querySelector('.n-modal .n-card-header .n-base-close')?.click()`)
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await click('拉取任务')
  await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('27 条')`))
  assert.deepEqual(errors, [])
  console.log(
    JSON.stringify({
      result: 'passed',
      checks: [
        'source edit retains revision and saved secret',
        'three-step endpoint editor',
        'record raw/event comparison',
        '390px responsive modal',
        'actual job progress'
      ],
      backend: 'synthetic responses; no backend acceptance claim',
      screenshot: '/tmp/torchlink-external-data-narrow.png'
    })
  )
} finally {
  await browser?.close()
}
