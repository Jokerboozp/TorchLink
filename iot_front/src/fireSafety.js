export const stationTypes = [
  { value:'micro', label:'微型消防站' },
  { value:'professional', label:'专职消防站' },
  { value:'volunteer', label:'志愿消防站' }
]
export const extinguisherTypes = [
  { value:'dry_powder', label:'干粉' }, { value:'co2', label:'二氧化碳' },
  { value:'foam', label:'泡沫' }, { value:'water', label:'水基' }, { value:'other', label:'其他' }
]
export const inspectionStates = [
  { value:'pending', label:'待巡检' }, { value:'rectifying', label:'待整改' },
  { value:'reviewing', label:'待复核' }, { value:'completed', label:'已完成' }, { value:'cancelled', label:'已取消' }
]
export const dispatchTypes = [
  { value:'fire', label:'灭火' }, { value:'rescue', label:'救援' },
  { value:'drill', label:'演练' }, { value:'other', label:'其他' }
]
const labels = {
  active:'在用', ready:'可用', maintenance:'维护中', retired:'已报废', pending:'待处理',
  approved:'已通过', rejected:'已驳回', rectifying:'待整改', reviewing:'待复核',
  completed:'已完成', cancelled:'已取消', dispatched:'已出动', returned:'已归队', pass:'合格', fail:'不合格'
}
const tones = {
  active:'success', ready:'success', approved:'success', completed:'success', returned:'success', pass:'success',
  maintenance:'warning', pending:'warning', rectifying:'danger', reviewing:'warning', dispatched:'warning',
  rejected:'danger', fail:'danger', retired:'neutral', cancelled:'neutral'
}
export const statusLabel = value => labels[String(value || '').toLowerCase()] || value || '—'
export const statusTone = value => tones[String(value || '').toLowerCase()] || 'neutral'
const pad = value => String(value).padStart(2, '0')

export function toDateInput(value = Date.now()) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}
export function dateTimeLabel(value) {
  if (value == null || value === '' || !Number(value)) return '—'
  const date = new Date(Number(value))
  if (!Number.isFinite(date.getTime())) return '—'
  return date.toLocaleString('zh-CN', { hour12:false, year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit' })
}
export function dateLabel(value) {
  if (!value) return '—'
  try {
    const date = localDate(value)
    return `${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日`
  } catch { return value }
}
// Construct dates in local calendar time. Parsing YYYY-MM-DD with Date would
// use UTC and move the visible date for people west of Greenwich.
function localDate(value) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(value || ''))
  if (!match) throw new Error('请选择有效日期')
  const year = Number(match[1]), month = Number(match[2]) - 1, day = Number(match[3])
  const date = new Date(year, month, day)
  if (date.getFullYear() !== year || date.getMonth() !== month || date.getDate() !== day) throw new Error('请选择有效日期')
  return date
}
export function shiftRange(day, startTime, endTime) {
  const parseTime = value => {
    const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(String(value || ''))
    if (!match) throw new Error('班次时间须为有效的 HH:mm')
    return [Number(match[1]), Number(match[2])]
  }
  const start = localDate(day), end = localDate(day)
  const [startHour, startMinute] = parseTime(startTime), [endHour, endMinute] = parseTime(endTime)
  start.setHours(startHour, startMinute, 0, 0)
  end.setHours(endHour, endMinute, 0, 0)
  if (end <= start) end.setDate(end.getDate() + 1)
  return [start.getTime(), end.getTime()]
}
// 批量排班：日期区间内每天一个班次，人员按组逐日轮换（第 1 天第 1 组，第 2 天第 2 组……）。
export const maxBatchDays = 62
export function batchAssignments({ stationId, shift, from, to, groups, notes = '' }) {
  const rotation = (groups || []).map(group => [...new Set(group || [])]).filter(group => group.length)
  if (!stationId || !shift) throw new Error('请选择消防站和班次模板')
  if (!rotation.length) throw new Error('请至少设置一组值班人员')
  const first = localDate(from), last = localDate(to)
  if (last < first) throw new Error('结束日期不能早于开始日期')
  const out = []
  for (const day = new Date(first); day <= last; day.setDate(day.getDate() + 1)) {
    if (out.length >= maxBatchDays) throw new Error(`一次最多安排 ${maxBatchDays} 天`)
    const [startAt, endAt] = shiftRange(toDateInput(day), shift.startTime, shift.endTime)
    out.push({ stationId, shiftId:shift.id, personnelIds:[...rotation[out.length % rotation.length]], startAt, endAt, notes:String(notes || '').trim() })
  }
  return out
}
export function monthRange(day) {
  const date = localDate(day)
  return [new Date(date.getFullYear(), date.getMonth(), 1).getTime(), new Date(date.getFullYear(), date.getMonth() + 1, 1).getTime()]
}
export function weekRange(day) {
  const start = localDate(day)
  start.setDate(start.getDate() - (start.getDay() + 6) % 7)
  const end = new Date(start)
  end.setDate(end.getDate() + 7)
  return [start.getTime(), end.getTime()]
}
export function calendarDays(range) {
  const [fromAt, toAt] = range
  const days = []
  for (const date = new Date(fromAt); date.getTime() < toAt; date.setDate(date.getDate() + 1)) {
    const next = new Date(date)
    next.setDate(next.getDate() + 1)
    days.push({ date:toDateInput(date), day:date.getDate(), weekday:date.getDay(), startAt:date.getTime(), endAt:next.getTime() })
  }
  return days
}
export function assignmentsForDay(rows, day) {
  return rows.filter(row => row.startAt < day.endAt && row.endAt > day.startAt).sort((a, b) => a.startAt - b.startAt || a.id.localeCompare(b.id))
}
export function moveCalendar(day, mode, direction) {
  const date = localDate(day)
  if (mode === 'week') date.setDate(date.getDate() + direction * 7)
  else { date.setDate(1); date.setMonth(date.getMonth() + direction) }
  return toDateInput(date)
}
