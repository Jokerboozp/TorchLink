import source from './tokens.css?raw'
import darkSource from './tokens-dark.css?raw'
import { createThemeOverrides } from './naiveTheme.js'

// 构建时读取 tokens.css，生成与页面样式同源的 Naive UI 主题；深色主题在其上叠加 tokens-dark.css。
export const themeOverrides = createThemeOverrides(source)
export const darkThemeOverrides = createThemeOverrides(source + darkSource)
