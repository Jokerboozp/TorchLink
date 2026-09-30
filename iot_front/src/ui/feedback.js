import { createDiscreteApi } from 'naive-ui'

const { message, dialog } = createDiscreteApi(['message', 'dialog'])

export const UiMessage = {
  success: content => message.success(String(content)),
  info: content => message.info(String(content)),
  warning: content => message.warning(String(content)),
  error: content => message.error(String(content))
}

export const UiMessageBox = {
  confirm(content, title = '请确认', options = {}) {
    return new Promise((resolve, reject) => {
      let settled = false
      const cancel = reason => { if (!settled) { settled = true; reject(reason) } }
      dialog.warning({
        title,
        content: String(content),
        positiveText: options.confirmButtonText || '确定',
        negativeText: options.cancelButtonText || '取消',
        onPositiveClick: () => { settled = true; resolve('confirm') },
        onNegativeClick: () => cancel('cancel'),
        onClose: () => cancel('close'),
        onMaskClick: () => cancel('close')
      })
    })
  }
}
