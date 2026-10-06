// 界面截图与 DOM 基线：重构前后各采集一次再对比，确认样式、类名与结构没有变化。
//   采集：IOT_TEST_HEADFUL=1 node tests/browser/visual-baseline.mjs capture <输出目录>
//   对比：IOT_TEST_HEADFUL=1 node tests/browser/visual-baseline.mjs compare <目录A> <目录B>
// 采集前先 npm run build，并以 IOT_UI_PREVIEW_NOW=1790000000000 启动 tests/browser/ui-preview.mjs。
import { startBrowser, delay } from '../helpers/browser.mjs'
import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const fixedNow = 1790000000000
const noisePixels = 16
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
]
const variants = [
  { name: 'light', theme: 'light', width: 1440, height: 900, mobile: false },
  { name: 'dark', theme: 'dark', width: 1440, height: 900, mobile: false },
  { name: 'narrow', theme: 'light', width: 390, height: 844, mobile: true }
]
const [mode, first, second] = process.argv.slice(2)

// Scoped style hashes change when components move between files; they are not structure.
const normalize = html =>
  html
    .replace(/ data-v-[0-9a-f]{8}(="")?/g, '')
    .replace(/ data-n-id="[^"]*"/g, '')
    .replace(/ (id|for|aria-controls|aria-labelledby|aria-describedby)="[^"]*\d[^"]*"/g, ' $1="#"')
    .replace(/></g, '>\n<')

const browser = await startBrowser({
  args: [
    '--use-mock-keychain',
    '--password-store=basic',
    '--disable-background-timer-throttling',
    '--disable-renderer-backgrounding',
    '--hide-scrollbars'
  ]
})
const { call, evaluate, until } = browser
try {
  await call('Page.enable')
  await call('Runtime.enable')
  if (mode === 'capture') await capture(first)
  else if (mode === 'compare') await compare(first, second)
  else throw new Error('用法：capture <目录> 或 compare <目录A> <目录B>')
} finally {
  await browser.close()
}

async function settle() {
  let previous = ''
  for (let i = 0; i < 40; i++) {
    await delay(150)
    const html = await evaluate(
      "document.querySelector('.ui-loading,.n-data-table--loading,.n-skeleton,.n-spin-container .n-spin') ? '' : document.body.innerHTML"
    )
    if (html && html === previous) return
    previous = html
  }
}

async function capture(dir) {
  await mkdir(dir, { recursive: true })
  for (const v of variants) {
    await call('Emulation.setDeviceMetricsOverride', { width: v.width, height: v.height, deviceScaleFactor: 1, mobile: v.mobile })
    const { identifier } = await call('Page.addScriptToEvaluateOnNewDocument', {
      source: `
        localStorage.clear();
        localStorage.setItem('iot:theme', ${JSON.stringify(v.theme)});
        const RealDate = Date;
        class FixedDate extends RealDate { constructor(...a) { super(...(a.length ? a : [${fixedNow}])) } static now() { return ${fixedNow} } }
        window.Date = FixedDate;
        document.addEventListener('DOMContentLoaded', () => {
          const style = document.createElement('style');
          style.textContent = '*,*::before,*::after{transition:none!important;animation:none!important;caret-color:transparent!important}';
          document.head.appendChild(style);
        });`
    })
    await call('Page.navigate', { url: origin })
    await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"))
    await evaluate(
      "(() => { const fill = (s, v) => { const i = document.querySelector(s); i.value = v; i.dispatchEvent(new Event('input', { bubbles: true })) }; fill('.login-form #tenant-id input, .login-form input#tenant-id', 'fixture'); fill('.login-form #username input, .login-form input#username', 'admin'); fill('.login-form input[type=password]', 'fixture'); document.querySelector('.login-form button[type=submit]').click() })()"
    )
    await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 15"))
    for (const page of pages) {
      if (await evaluate("document.querySelector('.app-topbar__toggle')?.getAttribute('aria-label')==='打开菜单'")) {
        await evaluate("document.querySelector('.app-topbar__toggle').click()")
        await delay(200)
      }
      await evaluate(`document.querySelector('.nav-item[aria-label=${JSON.stringify(page)}]').click()`)
      await until(() => evaluate(`document.querySelector('.app-breadcrumb strong')?.innerText === ${JSON.stringify(page)}`))
      await evaluate('document.activeElement?.blur(); window.scrollTo(0, 0)')
      await settle()
      // Inner panels (chat history, tables) scroll on their own timing; capture them from the top.
      await evaluate(
        'document.querySelectorAll("*").forEach(e => { if (e.scrollTop) e.scrollTop = 0; if (e.scrollLeft) e.scrollLeft = 0 })'
      )
      await delay(100)
      const shot = await call('Page.captureScreenshot', { format: 'png' })
      const file = `${v.name}-${pages.indexOf(page).toString().padStart(2, '0')}-${page}`
      await writeFile(join(dir, `${file}.png`), Buffer.from(shot.data, 'base64'))
      await writeFile(join(dir, `${file}.html`), normalize(await evaluate("document.querySelector('#app').outerHTML")))
    }
    await call('Page.removeScriptToEvaluateOnNewDocument', { identifier })
  }
  console.log(`已采集 ${variants.length * pages.length} 个画面到 ${dir}`)
}

async function compare(a, b) {
  await call('Page.navigate', { url: 'about:blank' })
  const files = (await readdir(a)).filter(name => name.endsWith('.png')).sort()
  let failed = 0
  for (const name of files) {
    const base = name.slice(0, -4)
    const [htmlA, htmlB] = await Promise.all([
      readFile(join(a, `${base}.html`), 'utf8'),
      readFile(join(b, `${base}.html`), 'utf8').catch(() => '')
    ])
    const [pngA, pngB] = await Promise.all([readFile(join(a, name)), readFile(join(b, name)).catch(() => null)])
    let pixels = -1
    if (pngB) {
      pixels = await evaluate(`(async () => {
        const load = src => new Promise(r => { const i = new Image(); i.onload = () => r(i); i.src = 'data:image/png;base64,' + src });
        const [x, y] = await Promise.all([load(${JSON.stringify(pngA.toString('base64'))}), load(${JSON.stringify(pngB.toString('base64'))})]);
        if (x.width !== y.width || x.height !== y.height) return x.width * x.height;
        const data = img => { const c = document.createElement('canvas'); c.width = img.width; c.height = img.height; const g = c.getContext('2d'); g.drawImage(img, 0, 0); return g.getImageData(0, 0, img.width, img.height).data };
        const p = data(x), q = data(y); let n = 0, x0 = 1e9, y0 = 1e9, x1 = -1, y1 = -1;
        for (let i = 0; i < p.length; i += 4) if (p[i] !== q[i] || p[i + 1] !== q[i + 1] || p[i + 2] !== q[i + 2]) {
          n++; const px = (i / 4) % x.width, py = Math.floor(i / 4 / x.width);
          x0 = Math.min(x0, px); y0 = Math.min(y0, py); x1 = Math.max(x1, px); y1 = Math.max(y1, py);
        }
        return n ? n + ' 区域 ' + x0 + ',' + y0 + '-' + x1 + ',' + y1 : 0;
      })()`)
    }
    const domSame = htmlA === htmlB
    // A handful of pixels differ between identical runs (GPU anti-aliasing); structure is checked by the DOM.
    const changed = pixels === -1 || (typeof pixels === 'string' && Number.parseInt(pixels) > noisePixels)
    if (changed || !domSame) {
      failed++
      let firstDiff = ''
      if (!domSame) {
        const la = htmlA.split('\n'),
          lb = htmlB.split('\n')
        const i = la.findIndex((line, k) => line !== lb[k])
        firstDiff = `\n    A: ${(la[i] || '').slice(0, 220)}\n    B: ${(lb[i] || '').slice(0, 220)}`
      }
      console.log(`差异 ${base}: 像素 ${pixels}，DOM ${domSame ? '一致' : '不同'}${firstDiff}`)
    }
  }
  console.log(failed ? `${failed}/${files.length} 个画面不同` : `${files.length} 个画面一致`)
  process.exitCode = failed ? 1 : 0
}
