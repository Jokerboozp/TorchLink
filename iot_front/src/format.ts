// 界面上的时间、数量与大小格式，集中在这里，各页面显示一致。

/** A time (epoch milliseconds, ISO string or Date) in local 24-hour form; empty or invalid values show `empty`. */
export function formatTime(value: unknown, empty = '—'): string {
  if (value == null || value === '' || value === 0) return empty
  const date = value instanceof Date ? value : new Date(typeof value === 'string' && !/^\d+$/.test(value) ? value : Number(value))
  return Number.isNaN(date.getTime()) ? empty : date.toLocaleString('zh-CN', { hour12: false })
}

/** A whole count with thousands separators. */
export function formatCount(value: unknown): string {
  return Number(value || 0).toLocaleString('zh-CN')
}

/** A byte size: 字节 below 1 KB, then KB, MB and GB. */
export function formatBytes(value: unknown): string {
  const size = Number(value || 0)
  if (size < 1024) return `${size} 字节`
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 ** 3) return `${(size / 1024 ** 2).toFixed(1)} MB`
  return `${(size / 1024 ** 3).toFixed(2)} GB`
}
