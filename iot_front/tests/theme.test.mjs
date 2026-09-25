import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createThemeOverrides, parseTokens, resolveToken } from '../src/theme/naiveTheme.js'

const tokensCss = await readFile(new URL('../src/theme/tokens.css', import.meta.url), 'utf8')

test('Naive UI 主题完全由 tokens.css 生成，品牌色与设计变量一致', () => {
  const theme = createThemeOverrides(tokensCss)
  assert.equal(theme.common.primaryColor, '#13386c')
  assert.equal(theme.common.bodyColor, resolveToken(parseTokens(tokensCss), '--bg'))
  const values = JSON.stringify(theme)
  assert.doesNotMatch(values, /var\(/, '主题中不能残留未解析的 CSS 变量')
  assert.doesNotMatch(values, /undefined/)
})

test('缺失的设计变量会直接报错，而不是生成空主题', () => {
  assert.throws(() => resolveToken(parseTokens(':root { --a: var(--missing); }'), '--a'), /未定义/)
})
