// 复制文本到剪贴板；浏览器拒绝时退回选中隐藏文本框复制。返回是否成功。
export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    if (typeof document === 'undefined') return false
    const area = document.createElement('textarea')
    area.value = text
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    area.remove()
    return ok
  }
}
