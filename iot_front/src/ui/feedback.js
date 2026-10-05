import { computed } from 'vue'
import { createDiscreteApi, darkTheme, dateZhCN, zhCN } from 'naive-ui'
import { darkThemeOverrides, themeOverrides } from '../theme/naive.js'
import { isDark } from '../theme/mode.js'

// 页面外的异步回调也能显示消息与确认框；与页面使用同一套主题、深浅色和中文语言。
const configProviderProps = computed(() => ({
  locale: zhCN,
  dateLocale: dateZhCN,
  theme: isDark.value ? darkTheme : null,
  themeOverrides: isDark.value ? darkThemeOverrides : themeOverrides
}))
const { message, dialog } = createDiscreteApi(['message', 'dialog'], { configProviderProps })

export const UiMessage = {
  success: content => message.success(String(content)),
  info: content => message.info(String(content)),
  warning: content => message.warning(String(content)),
  error: content => message.error(String(content))
}

export const UiMessageBox = {
  confirm(content, title = '请确认', options = {}) {
    return new Promise((resolve, reject) => {
      /* 保持调用方使用 await 与取消异常的语义。 */
      let settled = false /* 防止关闭和取消事件重复结算。 */
      const cancel = reason => {
        if (!settled) {
          settled = true
          reject(reason)
        }
      }
      dialog.warning({
        title,
        content: String(content),
        positiveText: options.confirmButtonText || '确定',
        negativeText: options.cancelButtonText || '取消',
        onPositiveClick: () => {
          settled = true
          resolve('confirm')
        },
        onNegativeClick: () => cancel('cancel') /* 取消时返回现有调用方识别的标记。 */,
        onClose: () => cancel('close') /* 关闭按钮与遮罩关闭统一取消。 */,
        onMaskClick: () => cancel('close')
      })
    })
  }
}
