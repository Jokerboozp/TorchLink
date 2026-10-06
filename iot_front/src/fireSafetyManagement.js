import { extinguisherTypes } from './fireSafety.js'

// Editable fields deliberately exclude reminder projections and audit fields.
const editableFields = {
  stations: ['code', 'name', 'type', 'address', 'longitude', 'latitude', 'contact', 'phone', 'enabled', 'notes'],
  personnel: ['name', 'phone', 'stationId', 'position', 'enabled', 'notes'],
  equipment: ['stationId', 'name', 'category', 'quantity', 'unit', 'status', 'notes'],
  extinguishers: [
    'code',
    'stationId',
    'location',
    'type',
    'specification',
    'manufacturer',
    'serialNumber',
    'manufacturedOn',
    'serviceDueOn',
    'retireOn',
    'inspectionCycleDays',
    'status',
    'notes'
  ]
}
// 必填项逐字段提示：返回 { 字段: 提示 }，为空对象表示通过。
const requiredFields = {
  stations: { code: '请填写消防站编号', name: '请填写消防站名称' },
  personnel: { stationId: '请选择所属消防站', name: '请填写姓名' },
  equipment: { stationId: '请选择所属消防站', name: '请填写器材名称', category: '请填写器材类别', unit: '请填写数量单位' },
  dispatches: {
    stationId: '请选择所属消防站',
    title: '请填写出勤标题',
    location: '请填写出勤地点',
    startedAtInput: '请选择出勤时间',
    personnelIds: '请选择出勤人员'
  },
  extinguishers: { code: '请填写灭火器编号', stationId: '请选择所属消防站', location: '请填写放置位置' }
}
export function requiredFieldErrors(kind, form) {
  const blank = value => (Array.isArray(value) ? !value.length : !String(value ?? '').trim())
  return Object.fromEntries(Object.entries(requiredFields[kind] || {}).filter(([key]) => blank(form[key])))
}
export function managementPayload(kind, form) {
  const payload = Object.fromEntries(editableFields[kind].map(key => [key, form[key]]))
  if (form.id) payload.version = form.version
  return payload
}
export function localDateTimeInput(value = Date.now()) {
  const date = new Date(value),
    pad = part => String(part).padStart(2, '0')
  if (!Number.isFinite(date.getTime())) return ''
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
export function inputTimestamp(value, label = '时间') {
  const timestamp = value ? new Date(value).getTime() : NaN
  if (!Number.isFinite(timestamp) || timestamp <= 0) throw new Error(`请选择有效的${label}`)
  return timestamp
}
export function inspectionPayload(form, version) {
  if (form.checks.some(item => typeof item.passed !== 'boolean')) throw new Error('请为每项检查明确选择合格或不合格')
  const checks = form.checks.map(item => ({ name: item.name.trim(), passed: Boolean(item.passed) }))
  if (!checks.length || checks.some(item => !item.name)) throw new Error('请至少填写一项检查项目，项目名称不能为空')
  if (new Set(checks.map(item => item.name)).size !== checks.length) throw new Error('检查项目名称不能重复')
  const findings = form.findings.trim()
  if (checks.some(item => !item.passed) && !findings) throw new Error('检查不合格时请填写发现的问题')
  return { version, checks, findings }
}
export function canReviewInspection(task, username) {
  const latest = task?.rectifications?.at(-1)
  return task?.status === 'reviewing' && latest?.status === 'pending' && Boolean(username) && latest.submittedBy !== username
}
export function dispatchPayload(form) {
  if (!form.stationId || !form.title.trim() || !form.location.trim() || !form.personnelIds.length)
    throw new Error('请填写消防站、出勤标题、地点并选择出勤人员')
  const equipment = form.equipment.map(item => ({ equipmentId: item.equipmentId, quantity: Number(item.quantity) }))
  if (equipment.some(item => !item.equipmentId || !Number.isInteger(item.quantity) || item.quantity <= 0))
    throw new Error('请选择出勤器材并填写正整数数量')
  if (new Set(equipment.map(item => item.equipmentId)).size !== equipment.length) throw new Error('出勤器材不能重复选择')
  return {
    stationId: form.stationId,
    title: form.title.trim(),
    type: form.type,
    location: form.location.trim(),
    personnelIds: [...form.personnelIds],
    equipment,
    startedAt: inputTimestamp(form.startedAtInput, '出勤时间')
  }
}
export function fireQuery(filters, pagination = {}) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries({ ...filters, ...pagination })) {
    if (value !== '' && value != null) query.set(key, String(value))
  }
  return query.toString()
}

// 灭火器页面的名称查找；options 为 /api/v1/fire-safety/options 的结果，资料被删除时给出说明而不是编号。
export function extinguisherLabels(options) {
  return {
    stationName: id => options.stations.find(item => item.id === id)?.name || '已移除消防站',
    personName: id => options.personnel.find(item => item.id === id)?.name || '已移除人员',
    assetName: id => options.extinguishers.find(item => item.id === id)?.code || '已移除灭火器',
    typeName: value => extinguisherTypes.find(item => item.value === value)?.label || value
  }
}
