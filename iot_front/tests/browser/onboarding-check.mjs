// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-')) /* 声明 profile。 */
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' }) /* 声明 child。 */
let socket /* 声明 socket。 */
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms)) /* 声明 delay。 */
async function until(fn){for(let i=0;i<100;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')} /* 定义 until 函数。 */
try { /* 执行当前语句并推进处理流程。 */
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}}) /* 声明 port。 */
  const pages=await (await fetch(`http://127.0.0.1:${port}/json/list`)).json() /* 声明 pages。 */
  socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl) /* 更新 socket 的值。 */
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject}) /* 等待异步操作完成。 */
  let id=0;const pending=new Map() /* 声明 id。 */
  let blockDeviceToken = false, blockedTokenRequests = 0 /* 声明 blockDeviceToken。 */
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}else if(value.method==='Fetch.requestPaused'){ /* 更新 socket.onmessage 的值。 */
    if(blockDeviceToken){blockedTokenRequests++;call('Fetch.failRequest',{requestId:value.params.requestId,errorReason:'InternetDisconnected'}).catch(()=>{})} /* 判断条件并选择处理分支。 */
    else call('Fetch.continueRequest',{requestId:value.params.requestId}).catch(()=>{}) /* 执行当前语句并推进处理流程。 */
  }} /* 结束当前表达式或代码块。 */
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))}) /* 声明 call。 */
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value} /* 声明 evaluate。 */
  await call('Page.enable') /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN}) /* 等待异步操作完成。 */
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`)) /* 声明 click。 */
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}))})()`) /* 声明 fill。 */
  const request = async(path,body) => { /* 声明 request。 */
    const response = await fetch(process.env.IOT_TEST_ORIGIN+path,{method:'POST',headers:{Authorization:`Bearer ${process.env.IOT_TEST_TOKEN}`,'Content-Type':'application/json'},body:JSON.stringify(body)}) /* 声明 response。 */
    const data=await response.json(); assert.ok(response.ok,JSON.stringify(data)); return data /* 声明 data。 */
  } /* 结束当前表达式或代码块。 */
  // Legacy Modbus remains an API fixture; the UI now manages its persisted instance.
  // Modbus 协议版本由 Go 测试预先发布，这里通过统一的添加设备接口登记采集设备。
  await request('/api/v1/onboarding',{requestId:'browser-modbus',productId:'browser-modbus-product',device:{id:'browser-modbus-preview',name:'Modbus 预览'},connection:{mode:'poll',host:'127.0.0.1',port:Number(process.env.IOT_TEST_MODBUS_PORT),unitId:1,timeoutMs:3000}})
  await click('产品管理'); await click('新建产品') /* 等待异步操作完成。 */
  await fill('产品名称','浏览器标准产品'); await fill('产品标识','browser-standard-product') /* 等待异步操作完成。 */
  await click('保存产品') /* 等待异步操作完成。 */
  await until(()=>evaluate(`!document.querySelector('.ui-dialog')?.getClientRects().length`)) /* 等待异步操作完成。 */
  await click('设备管理'); await click('添加设备') /* 等待异步操作完成。 */
  assert.equal(await evaluate(`!!document.querySelector('.ui-dialog .n-steps')`),false) /* 验证实际结果符合预期。 */
  await evaluate(`document.querySelector('.ui-dialog .n-base-selection').click()`) /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='浏览器标准产品'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`)) /* 等待异步操作完成。 */
  await fill('设备名称','浏览器传感器修订'); await fill('设备标识','browser-device') /* 等待异步操作完成。 */
  await click('保存设备') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-dialog .n-descriptions-table-content')?.textContent`)) /* 等待异步操作完成。 */
  const credentials=await evaluate(`Array.from(document.querySelectorAll('.ui-dialog .n-descriptions-table-content')).map(e=>e.textContent.trim())`) /* 声明 credentials。 */
  const deviceCredential=['browser-device','',credentials[0],credentials[1]] /* 声明 deviceCredential。 */
  await click('关闭') /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.textContent.includes('browser-device'));const button=[...(row?.querySelectorAll('button')||[])].find(e=>e.textContent.trim()==='连接详情');if(!button)return false;button.click();return true})()`)) /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-drawer')?.textContent.includes('iot-standard')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`document.querySelector('.ui-drawer').textContent.includes('连接与状态历史')`)) /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`document.querySelector('.ui-drawer').textContent.includes('最近事件')`)) /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`document.querySelector('.ui-drawer').textContent.includes('最近告警')`)) /* 验证实际结果符合预期。 */
  assert.equal(await evaluate(`document.querySelector('.ui-drawer').textContent.includes('设备影子') || document.querySelector('.ui-drawer').textContent.includes('设备孪生与拓扑')`),false) /* 验证实际结果符合预期。 */
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true}) /* 等待异步操作完成。 */
  await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-drawer').getBoundingClientRect().width<=391`)) /* 等待异步操作完成。 */
  await evaluate(`document.querySelector('.n-base-close').click()`) /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.textContent.includes('历史设备'));const button=[...(row?.querySelectorAll('button')||[])].find(e=>e.textContent.trim()==='连接详情');if(!button)return false;button.click();return true})()`)) /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-drawer')?.textContent.includes('检测到多个关联实例')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`![...document.querySelectorAll('.ui-drawer button')].some(e=>e.textContent.trim()==='发送命令')`)) /* 验证实际结果符合预期。 */
  await evaluate(`document.querySelector('.ui-drawer .n-base-selection').click()`) /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='listener-a'&&e.getClientRects().length);if(!option)return false;option.click();return true})()`)) /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-drawer .ui-descriptions')?.textContent.includes('TCP')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`document.querySelector('.ui-drawer .ui-table').textContent.includes('listener-a') && !document.querySelector('.ui-drawer .ui-table').textContent.includes('listener-b')`)) /* 验证实际结果符合预期。 */
  await evaluate(`document.querySelector('.n-base-close').click()`) /* 等待异步操作完成。 */
  await click('接入网关') /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.textContent.includes('listener-a')&&e.getClientRects().length);const expand=row?.querySelector('.n-data-table-expand-trigger');if(!expand)return false;expand.click();return true})()`)) /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.instance-details')?.textContent.includes('127.0.0.1:1234')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`document.querySelector('.instance-details').textContent.includes('历史设备')`)) /* 验证实际结果符合预期。 */
  await click('接入测试') /* 等待异步操作完成。 */
  await until(()=>evaluate(`(()=>{const tab=[...document.querySelectorAll('[role=tab]')].find(e=>e.textContent.trim()==='测试设备');if(!tab)return false;tab.click();return true})()`)) /* 等待异步操作完成。 */
  await until(()=>evaluate(`!!document.querySelector('.standard-commissioning')`)) /* 等待异步操作完成。 */
  await fill('标准设备标识','browser-device');await fill('接入密钥',deviceCredential[2]);await fill('设备密钥','invalid-secret') /* 等待异步操作完成。 */
  await click('发送新消息') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.standard-commissioning .ui-alert')?.textContent.includes('AUTH_FAILED')`)) /* 等待异步操作完成。 */
  await fill('设备密钥',deviceCredential[3]);await click('发送新消息') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.standard-commissioning .ui-table')?.textContent.includes('已解析')`)) /* 等待异步操作完成。 */
  await click('重发同一条消息') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.standard-commissioning .ui-table')?.textContent.includes('平台已去重')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`document.querySelector('.standard-commissioning pre').textContent.includes('temperature')`)) /* 验证实际结果符合预期。 */
  if(process.env.IOT_TEST_MQTT_WEBSOCKET) { /* 判断条件并选择处理分支。 */
    await evaluate(`([...document.querySelectorAll('.standard-commissioning .n-radio')].find(e=>e.textContent.trim()==='MQTT')).click()`) /* 等待异步操作完成。 */
    await fill('设备密钥','invalid-mqtt-secret') /* 等待异步操作完成。 */
    await click('连接 / 重新认证') /* 等待异步操作完成。 */
    await until(()=>evaluate(`document.querySelector('.standard-commissioning')?.textContent.includes('认证拒绝，请检查或重新生成凭据')`)) /* 等待异步操作完成。 */
    assert.ok(await evaluate(`!!localStorage.getItem('iot_token')`)) /* 验证实际结果符合预期。 */
    await fill('设备密钥',deviceCredential[3]) /* 等待异步操作完成。 */
    await click('连接 / 重新认证') /* 等待异步操作完成。 */
    await until(()=>evaluate(`([...document.querySelectorAll('.standard-commissioning p')].some(e=>e.textContent.trim()==='已连接'))`)).catch(async e=>{console.log(await evaluate(`Array.from(document.querySelectorAll('.standard-commissioning p, .standard-commissioning .ui-alert')).map(e=>e.textContent.trim()).filter(x=>!x.includes('密钥'))`));throw e}) /* 等待异步操作完成。 */
    await click('发送新消息') /* 等待异步操作完成。 */
    await until(()=>evaluate(`document.querySelector('.standard-commissioning .ui-table')?.textContent.includes('已解析')`)) /* 等待异步操作完成。 */
    const outageSeconds = Number(process.env.IOT_TEST_BROWSER_OUTAGE_SECONDS || 0) /* 声明 outageSeconds。 */
    if(outageSeconds) { /* 判断条件并选择处理分支。 */
      blockDeviceToken = true /* 更新 blockDeviceToken 的值。 */
      await call('Fetch.enable',{patterns:[{urlPattern:'*/api/v1/device-mqtt/token',requestStage:'Request'}]}) /* 等待异步操作完成。 */
    } /* 结束当前表达式或代码块。 */
    await click('模拟断链并重连') /* 等待异步操作完成。 */
    if(outageSeconds) { /* 判断条件并选择处理分支。 */
      await until(()=>evaluate(`document.querySelector('.standard-commissioning')?.textContent.includes('等待重连')`)) /* 等待异步操作完成。 */
      const outageStarted = Date.now() /* 声明 outageStarted。 */
      let nextProgress = 30000 /* 声明 nextProgress。 */
      while(Date.now()-outageStarted < outageSeconds*1000) { /* 循环处理当前数据。 */
        await delay(Math.min(1000, outageSeconds*1000-(Date.now()-outageStarted))) /* 等待异步操作完成。 */
        if(Date.now()-outageStarted>=nextProgress){console.log(`OUTAGE: ${Math.floor((Date.now()-outageStarted)/1000)}/${outageSeconds}s, ${blockedTokenRequests} failed token requests`);nextProgress+=30000} /* 判断条件并选择处理分支。 */
        assert.ok(await evaluate(`Number(document.querySelector('[data-testid=mqtt-connect-count]')?.textContent)===1`)) /* 验证实际结果符合预期。 */
        assert.ok(await evaluate(`Array.from(document.querySelectorAll('.standard-commissioning button')).find(e=>e.textContent.trim()==='发送新消息').disabled`)) /* 验证实际结果符合预期。 */
      } /* 结束当前表达式或代码块。 */
      assert.ok(blockedTokenRequests>=1) /* 验证实际结果符合预期。 */
      blockDeviceToken = false /* 更新 blockDeviceToken 的值。 */
      await call('Fetch.disable') /* 等待异步操作完成。 */
      // Automatic backoff is capped at 30 seconds. Do not click reconnect.
      for(let attempt=0;attempt<40;attempt++) { /* 循环处理当前数据。 */
        if(await evaluate(`Number(document.querySelector('[data-testid=mqtt-connect-count]')?.textContent)>=2`))break /* 判断条件并选择处理分支。 */
        await delay(1000) /* 等待异步操作完成。 */
      } /* 结束当前表达式或代码块。 */
      assert.ok(await evaluate(`Number(document.querySelector('[data-testid=mqtt-connect-count]')?.textContent)>=2`)) /* 验证实际结果符合预期。 */
      console.log(`PASS: ${outageSeconds}s real elapsed device-token network outage, ${blockedTokenRequests} failed requests, automatic recovery`) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    await until(()=>evaluate(`Number(document.querySelector('[data-testid=mqtt-connect-count]')?.textContent)>=2`)) /* 等待异步操作完成。 */
    await until(()=>evaluate(`([...document.querySelectorAll('.standard-commissioning p')].some(e=>e.textContent.trim()==='已连接'))`)).catch(async e=>{console.log(await evaluate(`Array.from(document.querySelectorAll('.standard-commissioning p, .standard-commissioning .ui-alert')).map(e=>e.textContent.trim()).filter(x=>!x.includes('密钥'))`));throw e}) /* 等待异步操作完成。 */
    await click('重发同一条消息') /* 等待异步操作完成。 */
    await until(()=>evaluate(`([...document.querySelectorAll('.standard-commissioning .n-data-table-tbody .n-data-table-tr')].filter(e=>e.textContent.includes('已解析')).length===2)`)) /* 等待异步操作完成。 */
    console.log('PASS: live Broker WebSocket credential rejection without operator logout, authentication, Raw parsing, injected transport loss, automatic reconnect and retransmission') /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  await evaluate(`(()=>{const original=URL.createObjectURL;URL.createObjectURL=function(blob){window.__commissioningReport=blob.text();return original.call(URL,blob)}})()`) /* 等待异步操作完成。 */
  await click('导出验收记录') /* 等待异步操作完成。 */
  const exported = await evaluate('window.__commissioningReport') /* 声明 exported。 */
  assert.ok(JSON.parse(exported).records.length>=2) /* 验证实际结果符合预期。 */
  assert.equal(exported.includes(deviceCredential[3]), false) /* 验证实际结果符合预期。 */
  await click('设备管理');await click('测试设备') /* 等待异步操作完成。 */
  await until(()=>evaluate(`!!document.querySelector('.standard-commissioning input[type=password]')`)) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.querySelector('.standard-commissioning input[type=password]').value`), '') /* 验证实际结果符合预期。 */
  await click('摄像头映射') /* 等待异步操作完成。 */
  await click('新增摄像头') /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.body.textContent.includes('国标视频目录') || document.querySelector('.ui-dialog').textContent.includes('ONVIF')`), false) /* 验证实际结果符合预期。 */
  await fill('摄像头标识','browser-camera');await fill('摄像头名称','直接登记摄像头') /* 等待异步操作完成。 */
  await click('保存') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.ui-table')?.textContent.includes('直接登记摄像头')`)) /* 等待异步操作完成。 */
  await click('模型管理') /* 等待异步操作完成。 */
  await until(()=>evaluate(`!!document.querySelector('.provider-select .n-base-selection')`)) /* 等待异步操作完成。 */
  await evaluate(`document.querySelector('.provider-select .n-base-selection').click()`) /* 等待异步操作完成。 */
  await until(()=>evaluate(`([...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).length>=3)`)) /* 等待异步操作完成。 */
  const providerNames = await evaluate(`[...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).map(e=>e.textContent.trim())`) /* 声明 providerNames。 */
  assert.deepEqual(providerNames, ['Ollama','DeepSeek','OpenAI 兼容 API']) /* 验证实际结果符合预期。 */
  console.log('PASS: technical provider names are visible; no model test or configuration apply was performed') /* 执行当前语句并推进处理流程。 */
  console.log('PASS: independent product and device registration, legacy Modbus API fixture, connection history, mobile drawer, legacy profile selection, listener sessions and recent devices, HTTP credential rejection and deduplication, report export without secrets, credential cleanup') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  if(socket)socket.close() /* 判断条件并选择处理分支。 */
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)}) /* 声明 exited。 */
  child.kill() /* 执行当前语句并推进处理流程。 */
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000) /* 声明 forceStop。 */
  forceStop.unref() /* 执行当前语句并推进处理流程。 */
  await exited /* 等待异步操作完成。 */
  clearTimeout(forceStop) /* 执行当前语句并推进处理流程。 */
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
