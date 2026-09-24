import { api, notifyError } from './api'
import { UiMessage, UiMessageBox } from './ui/feedback.js'

export async function confirmDelete({ label, path, onDeleted, warning = '删除后无法恢复。', blockedHint = '存在关联或仍在使用，请先解除关联或关闭活动告警。' }) {
  try {
    await UiMessageBox.confirm(`确定删除“${label}”？${warning}`, '删除确认', { type:'warning', confirmButtonText:'确定删除', cancelButtonText:'取消' })
    await api(path, { method:'DELETE' })
    UiMessage.success('删除成功')
    await onDeleted?.()
    return true
  } catch (error) {
    if (error === 'cancel' || error === 'close') return false
    if (error?.status === 409) UiMessage.warning(blockedHint)
    else notifyError(error)
    return false
  }
}
