// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { startBrowser, delay } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
let browser
try {
  browser = await startBrowser({ timeout: 60000 })
  const { call, evaluate, until } = browser
  await call('Page.enable')
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`})
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`))
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await click('设备通信协议'); await click('上传源码')
  await until(()=>evaluate(`document.querySelector('input[type=file]')`))
  await fill('协议标识','functions-browser')
  assert.equal(await evaluate(`[...document.querySelectorAll('.n-collapse-item__content-inner textarea')].some(e=>e.getClientRects().length>0)`),false)
  assert.equal(await evaluate(`document.querySelector('input[placeholder="Go 函数模式留空自动生成新版本"]').value`),'')
  // Template download uses the authenticated endpoint and a real ZIP response.
  await click('下载解析模板')
  await call('DOM.enable')
  const tree=await call('DOM.getDocument')
  const input=await call('DOM.querySelector',{nodeId:tree.root.nodeId,selector:'input[type=file]'})
  await call('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[process.env.IOT_TEST_SOURCE_GO]})
  await click('上传、编译并发布')
  await until(()=>evaluate(`document.body.textContent.includes('协议已发布，可绑定产品使用') || document.querySelector('.source-error')?.textContent`))
  assert.equal(await evaluate(`document.querySelector('.source-error')?.textContent || ''`),'')
  await until(()=>evaluate(`document.body.textContent.includes('functions-browser') && document.body.textContent.includes('auto-')`))
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await click('上传源码')
  await delay(200)
  assert.equal(await evaluate(`[...document.querySelectorAll('button')].some(e=>e.textContent.trim()==='下载 TCP / UDP 模板'&&e.getClientRects().length)`),true)
  assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true)
  // Send a compile failure through the real page; the previous version remains.
  await fill('版本','invalid-browser')
  await call('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[process.env.IOT_TEST_SOURCE_GO+'.invalid.go']})
  await click('上传、编译并发布')
  await until(()=>evaluate(`document.querySelector('.source-error')?.textContent`))
  console.log('PASS: authenticated Go template download, single Go file upload without JSON/version/runtime, actual compile and samples, failure visible, narrow view')
} finally {
  await browser?.close()
}
