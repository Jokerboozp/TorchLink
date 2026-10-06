import { api, notifyError } from './api'
import { UiMessage, UiMessageBox } from './ui/feedback.js'

// confirmed 弹出确认框：确认返回 true，取消或关闭返回 false（不抛出）。参数与 UiMessageBox.confirm 相同。
export async function confirmed(message, title, options) {
  try {
    await UiMessageBox.confirm(message, title, options)
    return true
  } catch {
    return false
  }
}

// confirmAction 先确认再执行：确认后运行 action，成功时提示 success，返回是否已执行。
// 冲突（409）显示 conflictHint，其他失败统一提示；取消不提示。
export async function confirmAction({ message, title, options, action, success = '', conflictHint = '' }) {
  if (!(await confirmed(message, title, options))) return false
  try {
    const result = await action()
    const text = typeof success === 'function' ? success(result) : success
    if (text) UiMessage.success(text)
    return true
  } catch (error) {
    if (error?.status === 409 && conflictHint) UiMessage.warning(conflictHint)
    else notifyError(error)
    return false
  }
}

// confirmDelete 是删除操作的 confirmAction：确认后发送 DELETE，删除后调用 onDeleted。
export function confirmDelete({
  label,
  path,
  onDeleted,
  warning = '删除后无法恢复。',
  blockedHint = '存在关联或仍在使用，请先解除关联或关闭活动告警。'
}) {
  return confirmAction({
    message: `确定删除“${label}”？${warning}`,
    title: '删除确认',
    options: { type: 'warning', confirmButtonText: '确定删除', cancelButtonText: '取消' },
    action: async () => {
      const result = await api(path, { method: 'DELETE' })
      await onDeleted?.()
      return result
    },
    success: result => (result?.deleting ? '已提交删除，清理将在后台完成' : '删除成功'),
    conflictHint: blockedHint
  })
}
