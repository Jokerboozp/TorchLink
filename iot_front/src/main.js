import { createApp } from 'vue'
import App from './App.vue'
import './styles.css'
import { permissionDirective } from './permissions'
import { installUi } from './ui'
import { UiMessage } from './ui/feedback.js'
import { pinia } from './stores/index.ts'
import { router } from './router/index.ts'

// 升级后，旧页面按需加载的分块可能已不存在：提示后自动刷新一次以加载新版本。
// 一分钟内再次失败不再刷新，避免循环；此时由页面的加载失败提示处理。
const RELOAD_KEY = 'iot:reload-after-update'
window.addEventListener('vite:preloadError', event => {
  let last = 0
  try {
    last = Number(sessionStorage.getItem(RELOAD_KEY) || 0)
  } catch {
    /* 会话存储不可用时按未刷新处理。 */
  }
  if (Date.now() - last < 60000) return
  event.preventDefault()
  try {
    sessionStorage.setItem(RELOAD_KEY, String(Date.now()))
  } catch {
    /* 无法记录时仍刷新一次。 */
  }
  UiMessage.info('平台已更新，正在重新加载页面')
  setTimeout(() => window.location.reload(), 800)
})

const app = createApp(App)
app.use(pinia)
app.use(router)
installUi(app)
app.directive('permission', permissionDirective).mount('#app')
