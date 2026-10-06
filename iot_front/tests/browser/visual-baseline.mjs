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
const defaultPages = [
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
// IOT_VISUAL_PAGES（逗号分隔的菜单名称）可改为检查其他页面。
const pages = process.env.IOT_VISUAL_PAGES ? process.env.IOT_VISUAL_PAGES.split(',') : defaultPages
// IOT_VISUAL_ACTIONS 另外采集弹窗与抽屉：分号分隔，每项为“菜单>按钮|按钮”，依次点击文字完全相同的按钮或页签，
// 例如 '排班>批量排班;排班>换班申请|审批'；以 = 开头的项（如 '=知识库>检索策略'）不等待弹层，用于采集页签；
// 步骤 @scroll:N 把可滚动区域滚到 N 像素处，用于采集长抽屉的下半部分。
// 只设置它而不设置 IOT_VISUAL_PAGES 时不采集页面。
const actions = (process.env.IOT_VISUAL_ACTIONS || '').split(';').filter(Boolean)
const pageList = process.env.IOT_VISUAL_ACTIONS && !process.env.IOT_VISUAL_PAGES ? [] : pages
const variants = [
  { name: 'light', theme: 'light', width: 1440, height: 900, mobile: false },
  { name: 'dark', theme: 'dark', width: 1440, height: 900, mobile: false },
  { name: 'narrow', theme: 'light', width: 390, height: 844, mobile: true }
]
const [mode, first, second] = process.argv.slice(2)

// Scoped style hashes change when components move between files; they are not structure. Empty
// comments are v-if placeholders that render nothing, and the click wave reflects the last click.
const normalize = html =>
  html
    .replace(/ data-v-[0-9a-f]{8}(="")?/g, '')
    .replace(/<!---->/g, '')
    .replace(/ n-base-wave--active/g, '')
    .replace(/ data-n-id="[^"]*"/g, '')
    .replace(/ (id|for|aria-controls|aria-labelledby|aria-describedby)="[^"]*\d[^"]*"/g, ' $1="#"')
    .replace(/></g, '>\n<')
    .replace(/\n\s*(?=\n)/g, '')

// An open dialog with edits would block the next navigation with a beforeunload prompt; accept it.
const browser = await startBrowser({
  onEvent: message => {
    if (message.method === 'Page.javascriptDialogOpening') browser.call('Page.handleJavaScriptDialog', { accept: true }).catch(() => {})
  },
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

async function login(theme) {
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"), 'login form')
  await evaluate(
    "(() => { const fill = (s, v) => { const i = document.querySelector(s); i.value = v; i.dispatchEvent(new Event('input', { bubbles: true })) }; fill('.login-form #username input, .login-form input#username', 'admin'); fill('.login-form input[type=password]', 'fixture'); document.querySelector('.login-form button[type=submit]').click() })()"
  )
  await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 15"), 'menu')
}

async function openPage(page) {
  if (await evaluate("document.querySelector('.app-topbar__toggle')?.getAttribute('aria-label')==='打开菜单'")) {
    await evaluate("document.querySelector('.app-topbar__toggle').click()")
    await delay(200)
  }
  await evaluate(`document.querySelector('.nav-item[aria-label=${JSON.stringify(page)}]').click()`)
  await until(() => evaluate(`document.querySelector('.app-breadcrumb strong')?.innerText === ${JSON.stringify(page)}`))
  await evaluate('document.activeElement?.blur(); window.scrollTo(0, 0)')
  await settle()
}

// Clicks the first visible, enabled button or tab whose text is exactly the label.
async function clickText(label) {
  await until(
    () =>
      evaluate(`(() => {
      const el = [...document.querySelectorAll('button,.n-tabs-tab,.n-radio-button,[role=tab]')].find(e =>
        e.innerText.trim() === ${JSON.stringify(label)} && e.getClientRects().length && !e.disabled && !e.classList.contains('n-button--disabled'))
      if (!el) return false
      el.click()
      return true
    })()`),
    label
  )
  await settle()
}

async function shoot(dir, file, selector, scroll = 0) {
  // Inner panels (chat history, tables) scroll on their own timing; capture them from the top, or at the requested offset.
  await evaluate(
    `document.querySelectorAll("*").forEach(e => { e.scrollLeft = 0; e.scrollTop = e.scrollHeight > e.clientHeight + 1 && ${scroll} ? ${scroll} : 0 })`
  )
  await delay(100)
  const shot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(dir, `${file}.png`), Buffer.from(shot.data, 'base64'))
  await writeFile(join(dir, `${file}.html`), normalize(await evaluate(`document.querySelector(${JSON.stringify(selector)}).outerHTML`)))
}

async function capture(dir) {
  await mkdir(dir, { recursive: true })
  for (const v of variants) {
    await call('Emulation.setDeviceMetricsOverride', { width: v.width, height: v.height, deviceScaleFactor: 1, mobile: v.mobile })
    const { identifier } = await call('Page.addScriptToEvaluateOnNewDocument', {
      source: `
        localStorage.clear();
        sessionStorage.clear();
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
    if (pageList.length) await login(v.theme)
    for (const page of pageList) {
      await openPage(page)
      await shoot(dir, `${v.name}-${pages.indexOf(page).toString().padStart(2, '0')}-${page}`, '#app')
    }
    for (const [index, action] of actions.entries()) {
      // Each overlay starts from a fresh session so earlier dialogs leave nothing behind.
      const overlay = !action.startsWith('=')
      const [page, steps] = action.replace(/^=/, '').split('>')
      await login(v.theme)
      await openPage(page)
      let scroll = 0
      try {
        for (const step of steps.split('|')) {
          if (step.startsWith('@scroll:')) scroll = Number(step.slice(8))
          else await clickText(step)
        }
      } catch (error) {
        await shoot(dir, `failed-${v.name}-${index}`, 'body')
        throw error
      }
      if (overlay)
        await until(() => evaluate("[...document.querySelectorAll('.n-modal,.n-drawer')].some(e => e.getClientRects().length)"), 'overlay')
      await evaluate('document.activeElement?.blur()')
      await settle()
      await shoot(dir, `${v.name}-action-${index.toString().padStart(2, '0')}-${action.replace(/[=>|/:@]/g, '-')}`, 'body', scroll)
    }
    await call('Page.removeScriptToEvaluateOnNewDocument', { identifier })
  }
  console.log(`已采集 ${variants.length * (pageList.length + actions.length)} 个画面到 ${dir}`)
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
