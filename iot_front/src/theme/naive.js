import source from './tokens.css?raw'
import { createThemeOverrides } from './naiveTheme.js'

// 构建时读取 tokens.css，生成与页面样式同源的 Naive UI 主题。
export const themeOverrides = createThemeOverrides(source)
