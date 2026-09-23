import { alarmType, alarmLevels, label } from './labels.js' /* 引入当前代码需要的依赖。 */

export const ALERT_SETTINGS_STORAGE_PREFIX = 'iot:alert-settings:' /* 执行当前语句并推进处理流程。 */
export const DEFAULT_ALERT_SETTINGS = Object.freeze({ /* 执行当前语句并推进处理流程。 */
  popupEnabled: true, /* 执行当前语句并推进处理流程。 */
  soundEnabled: true, /* 执行当前语句并推进处理流程。 */
  quietStart: '', /* 执行当前语句并推进处理流程。 */
  quietEnd: '' /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */

const TIME_PATTERN = /^(?:[01]\d|2[0-3]):[0-5]\d$/ /* 声明 TIME_PATTERN。 */
const QUIET_TOKEN_PATTERN = /(FAULT|故障|MALFUNCTION|MALFUNCTIONING|ERROR|ERR)/i /* 声明 QUIET_TOKEN_PATTERN。 */

function resolveStorage(storage) { /* 定义 resolveStorage 函数。 */
  if (storage) return storage /* 判断条件并选择处理分支。 */
  if (typeof localStorage !== 'undefined') return localStorage /* 判断条件并选择处理分支。 */
  return null /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function storageIdentity(identity = {}) { /* 定义 storageIdentity 函数。 */
  const tenant = encodeURIComponent(String(identity.tenant || 'default')) /* 声明 tenant。 */
  const user = encodeURIComponent(String(identity.user || 'default')) /* 声明 user。 */
  return `${ALERT_SETTINGS_STORAGE_PREFIX}${tenant}:${user}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function validTime(value) { /* 定义 validTime 函数。 */
  return typeof value === 'string' && TIME_PATTERN.test(value) ? value : '' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function normalizeAlertSettings(value = {}) { /* 执行当前语句并推进处理流程。 */
  return { /* 返回当前处理结果。 */
    popupEnabled: value.popupEnabled !== false, /* 执行当前语句并推进处理流程。 */
    soundEnabled: value.soundEnabled !== false, /* 执行当前语句并推进处理流程。 */
    quietStart: validTime(value.quietStart), /* 执行当前语句并推进处理流程。 */
    quietEnd: validTime(value.quietEnd) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function loadAlertSettings(storage, identity) { /* 执行当前语句并推进处理流程。 */
  const target = resolveStorage(storage) /* 声明 target。 */
  if (!target) return normalizeAlertSettings(DEFAULT_ALERT_SETTINGS) /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    return normalizeAlertSettings(JSON.parse(target.getItem(storageIdentity(identity)) || '{}')) /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    return normalizeAlertSettings(DEFAULT_ALERT_SETTINGS) /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function saveAlertSettings(storage, identity, value) { /* 执行当前语句并推进处理流程。 */
  const normalized = normalizeAlertSettings(value) /* 声明 normalized。 */
  const target = resolveStorage(storage) /* 声明 target。 */
  if (target) { /* 判断条件并选择处理分支。 */
    try { /* 执行当前语句并推进处理流程。 */
      target.setItem(storageIdentity(identity), JSON.stringify(normalized)) /* 执行当前语句并推进处理流程。 */
    } catch { /* 结束当前表达式或代码块。 */
      // A private browsing context may reject localStorage writes. The live
      // settings still apply for this session even when persistence fails.
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  return normalized /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function timeToMinutes(value) { /* 定义 timeToMinutes 函数。 */
  if (!TIME_PATTERN.test(value || '')) return null /* 判断条件并选择处理分支。 */
  const [hours, minutes] = value.split(':').map(Number) /* 执行当前语句并推进处理流程。 */
  return hours * 60 + minutes /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function isWithinQuietHours(date = new Date(), settings = {}) { /* 执行当前语句并推进处理流程。 */
  const start = timeToMinutes(settings.quietStart) /* 声明 start。 */
  const end = timeToMinutes(settings.quietEnd) /* 声明 end。 */
  if (start == null || end == null || start === end) return false /* 判断条件并选择处理分支。 */
  const current = date instanceof Date ? date.getHours() * 60 + date.getMinutes() : 0 /* 声明 current。 */
  if (start < end) return current >= start && current < end /* 判断条件并选择处理分支。 */
  return current >= start || current < end /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function asObject(value) { /* 定义 asObject 函数。 */
  return value && typeof value === 'object' && !Array.isArray(value) ? value : null /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function parsePayload(payload) { /* 定义 parsePayload 函数。 */
  if (typeof payload !== 'string') return asObject(payload) /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    return asObject(JSON.parse(payload)) /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function upper(value) { /* 定义 upper 函数。 */
  return String(value ?? '').trim().toUpperCase() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function textValue(value) { /* 定义 textValue 函数。 */
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value) /* 判断条件并选择处理分支。 */
  return '' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function firstText(...values) { /* 定义 firstText 函数。 */
  for (const value of values) { /* 循环处理当前数据。 */
    const text = textValue(value).trim() /* 声明 text。 */
    if (text) return text /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  return '' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function timestampOf(data) { /* 定义 timestampOf 函数。 */
  const value = Number(data.lastTriggeredAt || data.triggeredAt || data.eventTime || data.reportedAt || data.receivedAt || data.createdAt || Date.now()) /* 声明 value。 */
  if (!Number.isFinite(value) || value <= 0) return Date.now() /* 判断条件并选择处理分支。 */
  return value < 100000000000 ? value * 1000 : value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function isTruthyFlag(value) { /* 定义 isTruthyFlag 函数。 */
  return value === true || value === 1 || upper(value) === 'TRUE' || upper(value) === 'YES' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function hasFaultEvent(data) { /* 定义 hasFaultEvent 函数。 */
  const event = asObject(data.event) /* 声明 event。 */
  const eventTokens = [ /* 声明 eventTokens。 */
    data.messageType, /* 执行当前语句并推进处理流程。 */
    data.type, /* 执行当前语句并推进处理流程。 */
    data.eventType, /* 执行当前语句并推进处理流程。 */
    event?.type, /* 执行当前语句并推进处理流程。 */
    event?.eventType, /* 执行当前语句并推进处理流程。 */
    event?.code, /* 执行当前语句并推进处理流程。 */
    event?.name, /* 执行当前语句并推进处理流程。 */
    event?.alarmType, /* 执行当前语句并推进处理流程。 */
    event?.status /* 执行当前语句并推进处理流程。 */
  ] /* 结束当前表达式或代码块。 */
  if (eventTokens.some(value => QUIET_TOKEN_PATTERN.test(textValue(value)))) return true /* 判断条件并选择处理分支。 */

  const properties = asObject(data.properties) /* 声明 properties。 */
  const raw = asObject(data.raw) /* 声明 raw。 */
  const flags = [ /* 声明 flags。 */
    data.fault, /* 执行当前语句并推进处理流程。 */
    data.error, /* 执行当前语句并推进处理流程。 */
    properties?.fault, /* 执行当前语句并推进处理流程。 */
    properties?.powerFault, /* 执行当前语句并推进处理流程。 */
    properties?.sensorFault, /* 执行当前语句并推进处理流程。 */
    properties?.error, /* 执行当前语句并推进处理流程。 */
    event?.fault, /* 执行当前语句并推进处理流程。 */
    event?.error, /* 执行当前语句并推进处理流程。 */
    raw?.fault, /* 执行当前语句并推进处理流程。 */
    raw?.error /* 执行当前语句并推进处理流程。 */
  ] /* 结束当前表达式或代码块。 */
  return flags.some(isTruthyFlag) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function detailText(data, kind) { /* 定义 detailText 函数。 */
  if (data.componentId) return [data.componentName || data.componentId, data.componentLocation, alarmType(data.alarmType)].filter(Boolean).join(' · ') /* 判断条件并选择处理分支。 */
  const event = asObject(data.event) /* 声明 event。 */
  const details = asObject(data.details) /* 声明 details。 */
  const nested = nestedMessage(data) /* 声明 nested。 */
  const nestedEvent = asObject(nested?.event) /* 声明 nestedEvent。 */
  const videoEvent = asObject(details?.videoEvent) /* 声明 videoEvent。 */
  const candidate = kind === 'fault' /* 声明 candidate。 */
    ? firstText(event?.message, event?.description, event?.name, event?.type, data.message, data.description, data.alarmContent, data.content, data.alarmName, details?.description, details?.reason, nestedEvent?.message, nestedEvent?.description, nestedEvent?.name, nestedEvent?.type, videoEvent?.alarmName, videoEvent?.description) /* 执行当前语句并推进处理流程。 */
    : firstText(data.alarmContent, data.alarm_content, data.content, data.message, data.description, data.reason, data.alarmReason, data.alarmName, details?.description, details?.reason, details?.ruleName, event?.message, event?.description, nestedEvent?.message, nestedEvent?.description, nestedEvent?.name, nestedEvent?.type, videoEvent?.alarmName, videoEvent?.description) /* 执行当前语句并推进处理流程。 */
  return candidate && !['FAULT', 'ALARM'].includes(upper(candidate)) ? candidate : '' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function nestedMessage(data) { /* 定义 nestedMessage 函数。 */
  const details = asObject(data.details) /* 声明 details。 */
  return asObject(data.message) || asObject(details?.message) || asObject(data.standardMessage) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function normalizeAlert(data, kind, topic) { /* 定义 normalizeAlert 函数。 */
  const alarmLevel = upper(data.alarmLevel || data.level || (kind === 'fault' ? 'HIGH' : 'HIGH')) || 'HIGH' /* 声明 alarmLevel。 */
  const alarmTypeValue = upper(data.alarmType || data.alarm_type || (kind === 'fault' ? 'DEVICE_FAULT' : 'MANUAL_ALARM')) || (kind === 'fault' ? 'DEVICE_FAULT' : 'MANUAL_ALARM') /* 声明 alarmTypeValue。 */
  const alarmId = firstText(data.alarmId, data.id) /* 声明 alarmId。 */
  const nested = nestedMessage(data) /* 声明 nested。 */
  const standardMessageId = firstText(data.messageId, data.message_id, data.triggerId, data.trigger_id, nested?.messageId, nested?.message_id) /* 声明 standardMessageId。 */
  const rawMessageId = firstText(data.rawMessageId, data.raw_message_id, nested?.rawMessageId, nested?.raw_message_id) /* 声明 rawMessageId。 */
  const messageId = rawMessageId || standardMessageId /* 声明 messageId。 */
  const deviceId = firstText(data.deviceId, data.cameraId) /* 声明 deviceId。 */
  const baseId = alarmId || messageId || `${kind}:${topic}:${timestampOf(data)}` /* 声明 baseId。 */
  return { /* 返回当前处理结果。 */
    id: baseId, /* 执行当前语句并推进处理流程。 */
    kind, /* 执行当前语句并推进处理流程。 */
    alarmId, /* 执行当前语句并推进处理流程。 */
    triggerId: firstText(data.triggerId), /* 执行当前语句并推进处理流程。 */
    messageId, /* 执行当前语句并推进处理流程。 */
    rawMessageId, /* 执行当前语句并推进处理流程。 */
    standardMessageId, /* 执行当前语句并推进处理流程。 */
    tenantId: firstText(data.tenantId), /* 执行当前语句并推进处理流程。 */
    productId: firstText(data.productId), /* 执行当前语句并推进处理流程。 */
    deviceId, /* 执行当前语句并推进处理流程。 */
    componentId: firstText(data.componentId), /* 执行当前语句并推进处理流程。 */
    deviceName: firstText(data.deviceName, data.device_name, data.cameraName, nested?.deviceName, nested?.device_name), /* 执行当前语句并推进处理流程。 */
    alarmType: alarmTypeValue, /* 执行当前语句并推进处理流程。 */
    alarmTypeLabel: alarmType(alarmTypeValue), /* 执行当前语句并推进处理流程。 */
    alarmLevel, /* 执行当前语句并推进处理流程。 */
    alarmLevelLabel: label(alarmLevels, alarmLevel, '高'), /* 执行当前语句并推进处理流程。 */
    status: upper(data.status || 'ACTIVE') || 'ACTIVE', /* 执行当前语句并推进处理流程。 */
    source: firstText(data.source), /* 执行当前语句并推进处理流程。 */
    timestamp: timestampOf(data), /* 执行当前语句并推进处理流程。 */
    detail: detailText(data, kind) || (kind === 'fault' ? '检测到设备故障，请及时处理。' : '检测到设备异常报警，请及时处理。'), /* 执行当前语句并推进处理流程。 */
    raw: data, /* 执行当前语句并推进处理流程。 */
    topic /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function parseRealtimeAlert(topic, payload) { /* 执行当前语句并推进处理流程。 */
  const data = parsePayload(payload) /* 声明 data。 */
  const value = String(topic || '') /* 声明 value。 */
  if (!data) return null /* 判断条件并选择处理分支。 */

  if (/\/iot\/alarm\/raised(?:\/|$)/.test(value)) return normalizeAlert(data, 'alarm', value) /* 判断条件并选择处理分支。 */
  if (!value.includes('/iot/parsed/')) return null /* 判断条件并选择处理分支。 */
  // The server emits separate authoritative alarms for component observations.
  if (Array.isArray(data.event?.components)) return null /* 判断条件并选择处理分支。 */

  const messageType = upper(data.messageType || data.type) /* 声明 messageType。 */
  if (messageType === 'ALARM_REPORT' || messageType === 'ALARM') return normalizeAlert(data, 'alarm', value) /* 判断条件并选择处理分支。 */
  if (messageType === 'EVENT_REPORT' && hasFaultEvent(data)) return normalizeAlert(data, 'fault', value) /* 判断条件并选择处理分支。 */
  if (!messageType && hasFaultEvent(data)) return normalizeAlert(data, 'fault', value) /* 判断条件并选择处理分支。 */
  return null /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function alertKeys(alert) { /* 执行当前语句并推进处理流程。 */
  if (alert?.componentId && alert?.alarmId) return [String(alert.alarmId)] /* 判断条件并选择处理分支。 */
  return [...new Set([alert?.alarmId, alert?.triggerId, alert?.messageId, alert?.rawMessageId, alert?.standardMessageId, alert?.id].filter(Boolean).map(String))] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function alertTagType(level) { /* 执行当前语句并推进处理流程。 */
  return ['CRITICAL', 'HIGH'].includes(upper(level)) ? 'danger' : ['MEDIUM', 'ACKED'].includes(upper(level)) ? 'warning' : 'info' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

let audioContext /* 声明 audioContext。 */

export async function playAlarmTone() { /* 执行当前语句并推进处理流程。 */
  if (typeof window === 'undefined') return false /* 判断条件并选择处理分支。 */
  const AudioContext = window.AudioContext || window.webkitAudioContext /* 声明 AudioContext。 */
  if (!AudioContext) return false /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    audioContext ||= new AudioContext() /* 执行当前语句并推进处理流程。 */
    if (audioContext.state === 'suspended') await audioContext.resume() /* 判断条件并选择处理分支。 */
    const start = audioContext.currentTime /* 声明 start。 */
    for (const [offset, frequency] of [[0, 880], [0.16, 660], [0.32, 880]]) { /* 循环处理当前数据。 */
      const oscillator = audioContext.createOscillator() /* 声明 oscillator。 */
      const gain = audioContext.createGain() /* 声明 gain。 */
      oscillator.type = 'sine' /* 更新 oscillator.type 的值。 */
      oscillator.frequency.setValueAtTime(frequency, start + offset) /* 执行当前语句并推进处理流程。 */
      gain.gain.setValueAtTime(0.0001, start + offset) /* 执行当前语句并推进处理流程。 */
      gain.gain.exponentialRampToValueAtTime(0.18, start + offset + 0.02) /* 执行当前语句并推进处理流程。 */
      gain.gain.exponentialRampToValueAtTime(0.0001, start + offset + 0.12) /* 执行当前语句并推进处理流程。 */
      oscillator.connect(gain) /* 执行当前语句并推进处理流程。 */
      gain.connect(audioContext.destination) /* 执行当前语句并推进处理流程。 */
      oscillator.start(start + offset) /* 执行当前语句并推进处理流程。 */
      oscillator.stop(start + offset + 0.13) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    return true /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    return false /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
