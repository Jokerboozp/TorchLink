import assert from 'node:assert/strict'
import { globSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

// 静态样式规范：颜色与尺寸只来自 tokens.css，外观通过主题调整而不是 !important 覆盖。
const root = new URL('../', import.meta.url)
const read = path => readFile(new URL(path, root), 'utf8')
const tokens = await read('src/theme/tokens.css')
const defined = new Set([...tokens.matchAll(/(--[\w-]+)\s*:/g)].map(match => match[1]))
const sources = await Promise.all(globSync('src/**/*.{vue,css}', { cwd: root }).map(async path => {
  const text = await read(path)
  const styles = path.endsWith('.vue') ? [...text.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(match => match[1]).join('\n') : text
  return { path, text, styles }
}))

test('页面引用的 CSS 变量都在 tokens.css 或本文件中定义', () => {
  const missing = []
  for (const { path, text, styles } of sources) {
    const local = new Set([...styles.matchAll(/(--[\w-]+)\s*:/g)].map(match => match[1]))
    for (const [, name] of text.matchAll(/var\((--[\w-]+)/g)) {
      // --n-* 是 Naive UI 组件的内部变量，允许在样式中局部覆盖。
      if (!defined.has(name) && !local.has(name) && !name.startsWith('--n-')) missing.push(`${path} ${name}`)
    }
  }
  assert.deepEqual(missing, [])
})

test('只有 tokens.css 定义颜色值', () => {
  const colors = sources.filter(({ path }) => !path.endsWith('theme/tokens.css')).flatMap(({ path, styles }) => [...styles.matchAll(/#[0-9a-fA-F]{3,8}\b|\b(?:rgb|rgba|hsl|hsla)\(/g)].map(match => `${path} ${match[0]}`))
  assert.deepEqual(colors, [])
})

test('除动效降级外不使用 !important', () => {
  const important = sources.filter(({ path, styles }) => !path.endsWith('styles/motion.css') && styles.includes('!important')).map(({ path }) => path)
  assert.deepEqual(important, [])
})

test('不保留旧组件库选择器与过渡样式文件', async () => {
  assert.deepEqual(sources.filter(({ styles }) => /\.el-[\w-]/.test(styles)).map(({ path }) => path), [])
  const entry = await read('src/styles.css')
  assert.doesNotMatch(entry, /legacy/, '入口样式不应再引入过渡样式')
})
