import { createDiscreteApi } from 'naive-ui' /* 使用 Naive UI 的独立反馈 API，供现有业务模块调用。 */

const { message, dialog } = createDiscreteApi(['message', 'dialog']) /* 页面外的异步回调也能显示消息与确认框。 */

export const UiMessage = { /* 统一业务消息入口，避免页面依赖具体提供器。 */
  success: content => message.success(String(content)), /* 显示成功结果。 */
  info: content => message.info(String(content)), /* 显示提示信息。 */
  warning: content => message.warning(String(content)), /* 显示操作提醒。 */
  error: content => message.error(String(content)) /* 显示错误原因。 */
} /* 结束消息入口。 */

export const UiMessageBox = { /* 将已有 Promise 式确认流程接到 Naive UI 对话框。 */
  confirm(content, title = '请确认', options = {}) { /* 展示不可逆业务操作前的确认框。 */
    return new Promise((resolve, reject) => { /* 保持调用方使用 await 与取消异常的语义。 */
      let settled = false /* 防止关闭和取消事件重复结算。 */
      const cancel = reason => { if (!settled) { settled = true; reject(reason) } } /* 取消时不继续业务请求。 */
      dialog.warning({ /* 使用 Naive UI 的警告对话框。 */
        title, /* 保留业务给出的标题。 */
        content: String(content), /* 保留业务给出的说明。 */
        positiveText: options.confirmButtonText || '确定', /* 保留自定义确定文案。 */
        negativeText: options.cancelButtonText || '取消', /* 保留自定义取消文案。 */
        onPositiveClick: () => { settled = true; resolve('confirm') }, /* 确认后继续原有流程。 */
        onNegativeClick: () => cancel('cancel'), /* 取消时返回现有调用方识别的标记。 */
        onClose: () => cancel('close'), /* 关闭按钮与遮罩关闭统一取消。 */
        onMaskClick: () => cancel('close') /* 点击遮罩不执行被确认的操作。 */
      }) /* 结束确认框配置。 */
    }) /* 结束确认 Promise。 */
  } /* 结束确认操作。 */
} /* 结束对话框入口。 */
