import { onBeforeUnmount, watch } from 'vue'
import { UiMessageBox } from '../ui/feedback.js'

// 全站未保存内容登记：页面或弹窗登记“是否有未保存修改”的判断函数，
// 切换菜单、规则联动跳转、权限刷新重载页面和关闭浏览器标签前统一检查。
const sources = new Map()
let nextId = 0

export function hasUnsaved() {
  for (const check of sources.values()) {
    try {
      if (check()) return true
    } catch {
      // 判断函数出错时不阻止离开。
    }
  }
  return false
}

// 有未保存内容时询问是否放弃；用户确认放弃或没有未保存内容时返回 true。
export async function confirmDiscard(message = '当前有未保存的修改，离开后将丢失。确定离开？') {
  if (!hasUnsaved()) return true
  try {
    await UiMessageBox.confirm(message, '放弃未保存的修改', { confirmButtonText: '放弃修改', cancelButtonText: '继续编辑' })
    return true
  } catch {
    return false
  }
}

// 只在刷新或关闭浏览器时需要提示的情况（例如进行中的长请求会被中断），切换菜单不受影响。
const unloadSources = new Map()
export function registerUnloadWarning(check) {
  const id = ++nextId
  unloadSources.set(id, check)
  return () => unloadSources.delete(id)
}
const unloadWarned = () =>
  [...unloadSources.values()].some(check => {
    try {
      return check()
    } catch {
      return false
    }
  })

function onBeforeUnload(event) {
  if (!hasUnsaved() && !unloadWarned()) return
  event.preventDefault()
  event.returnValue = ''
}
if (typeof window !== 'undefined') window.addEventListener('beforeunload', onBeforeUnload)

// registerUnsaved 供非组件代码使用，返回注销函数。
export function registerUnsaved(check) {
  const id = ++nextId
  sources.set(id, check)
  return () => sources.delete(id)
}

// useUnsavedGuard 在组件内登记判断函数，组件卸载时自动注销。
export function useUnsavedGuard(check) {
  const unregister = registerUnsaved(check)
  onBeforeUnmount(unregister)
  return unregister
}

// confirmClose 用于弹窗关闭前：表单有修改时先确认，确认放弃或无修改时返回 true。
export async function confirmClose(dirty, message = '表单有未保存的修改，关闭后将丢失。确定关闭？') {
  if (!dirty) return true
  try {
    await UiMessageBox.confirm(message, '放弃未保存的修改', { confirmButtonText: '放弃修改', cancelButtonText: '继续编辑' })
    return true
  } catch {
    return false
  }
}

// formSnapshot 记录表单打开时的内容，dirty() 与当前内容比较，用于判断是否有修改。
export function formSnapshot(getValue) {
  let saved = ''
  const read = () => {
    try {
      return JSON.stringify(getValue())
    } catch {
      return ''
    }
  }
  return {
    reset() {
      saved = read()
    },
    dirty() {
      return saved !== '' && read() !== saved
    },
    clear() {
      saved = ''
    }
  }
}

// trackDialogForm：弹窗打开时记录快照、关闭时清除，并登记到全站未保存检查。
export function trackDialogForm(visible, getValue) {
  const snapshot = formSnapshot(getValue)
  watch(
    visible,
    open => {
      if (open) setTimeout(() => snapshot.reset())
      else snapshot.clear()
    },
    { immediate: true }
  )
  useUnsavedGuard(() => Boolean(visible.value) && snapshot.dirty())
  return snapshot
}
