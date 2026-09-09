import { alarmType, alarmLevels, label } from './labels.js'

export const ALERT_SETTINGS_STORAGE_PREFIX = 'iot:alert-settings:'
export const DEFAULT_ALERT_SETTINGS = Object.freeze({
  popupEnabled: true,
  soundEnabled: true,
  quietStart: '',
  quietEnd: ''
})

const TIME_PATTERN = /^(?:[01]\d|2[0-3]):[0-5]\d$/
const QUIET_TOKEN_PATTERN = /(FAULT|故障|MALFUNCTION|MALFUNCTIONING|ERROR|ERR)/i

function resolveStorage(storage) {
  if (storage) return storage
  if (typeof localStorage !== 'undefined') return localStorage
  return null
}

function storageIdentity(identity = {}) {
  const tenant = encodeURIComponent(String(identity.tenant || 'default'))
  const user = encodeURIComponent(String(identity.user || 'default'))
  return `${ALERT_SETTINGS_STORAGE_PREFIX}${tenant}:${user}`
}

function validTime(value) {
  return typeof value === 'string' && TIME_PATTERN.test(value) ? value : ''
}

export function normalizeAlertSettings(value = {}) {
  return {
    popupEnabled: value.popupEnabled !== false,
    soundEnabled: value.soundEnabled !== false,
    quietStart: validTime(value.quietStart),
    quietEnd: validTime(value.quietEnd)
  }
}

export function loadAlertSettings(storage, identity) {
  const target = resolveStorage(storage)
  if (!target) return normalizeAlertSettings(DEFAULT_ALERT_SETTINGS)
  try {
    return normalizeAlertSettings(JSON.parse(target.getItem(storageIdentity(identity)) || '{}'))
  } catch {
    return normalizeAlertSettings(DEFAULT_ALERT_SETTINGS)
  }
}

export function saveAlertSettings(storage, identity, value) {
  const normalized = normalizeAlertSettings(value)
  const target = resolveStorage(storage)
  if (target) {
    try {
      target.setItem(storageIdentity(identity), JSON.stringify(normalized))
    } catch {
      // A private browsing context may reject localStorage writes. The live
      // settings still apply for this session even when persistence fails.
    }
  }
  return normalized
}

function timeToMinutes(value) {
  if (!TIME_PATTERN.test(value || '')) return null
  const [hours, minutes] = value.split(':').map(Number)
  return hours * 60 + minutes
}

export function isWithinQuietHours(date = new Date(), settings = {}) {
  const start = timeToMinutes(settings.quietStart)
  const end = timeToMinutes(settings.quietEnd)
  if (start == null || end == null || start === end) return false
  const current = date instanceof Date ? date.getHours() * 60 + date.getMinutes() : 0
  if (start < end) return current >= start && current < end
  return current >= start || current < end
}

function asObject(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : null
}

function parsePayload(payload) {
  if (typeof payload !== 'string') return asObject(payload)
  try {
    return asObject(JSON.parse(payload))
  } catch {
    return null
  }
}

function upper(value) {
  return String(value ?? '').trim().toUpperCase()
}

function textValue(value) {
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return String(value)
  return ''
}

function firstText(...values) {
  for (const value of values) {
    const text = textValue(value).trim()
    if (text) return text
  }
  return ''
}

function timestampOf(data) {
  const value = Number(data.lastTriggeredAt || data.triggeredAt || data.eventTime || data.reportedAt || data.receivedAt || data.createdAt || Date.now())
  if (!Number.isFinite(value) || value <= 0) return Date.now()
  return value < 100000000000 ? value * 1000 : value
}

function isTruthyFlag(value) {
  return value === true || value === 1 || upper(value) === 'TRUE' || upper(value) === 'YES'
}

function hasFaultEvent(data) {
  const event = asObject(data.event)
  const eventTokens = [
    data.messageType,
    data.type,
    data.eventType,
    event?.type,
    event?.eventType,
    event?.code,
    event?.name,
    event?.alarmType,
    event?.status
  ]
  if (eventTokens.some(value => QUIET_TOKEN_PATTERN.test(textValue(value)))) return true

  const properties = asObject(data.properties)
  const raw = asObject(data.raw)
  const flags = [
    data.fault,
    data.error,
    properties?.fault,
    properties?.powerFault,
    properties?.sensorFault,
    properties?.error,
    event?.fault,
    event?.error,
    raw?.fault,
    raw?.error
  ]
  return flags.some(isTruthyFlag)
}

function detailText(data, kind) {
  if (data.componentId) return [data.componentName || data.componentId, data.componentLocation, alarmType(data.alarmType)].filter(Boolean).join(' · ')
  const event = asObject(data.event)
  const details = asObject(data.details)
  const nested = nestedMessage(data)
  const nestedEvent = asObject(nested?.event)
  const videoEvent = asObject(details?.videoEvent)
  const candidate = kind === 'fault'
    ? firstText(event?.message, event?.description, event?.name, event?.type, data.message, data.description, data.alarmContent, data.content, data.alarmName, details?.description, details?.reason, nestedEvent?.message, nestedEvent?.description, nestedEvent?.name, nestedEvent?.type, videoEvent?.alarmName, videoEvent?.description)
    : firstText(data.alarmContent, data.alarm_content, data.content, data.message, data.description, data.reason, data.alarmReason, data.alarmName, details?.description, details?.reason, details?.ruleName, event?.message, event?.description, nestedEvent?.message, nestedEvent?.description, nestedEvent?.name, nestedEvent?.type, videoEvent?.alarmName, videoEvent?.description)
  return candidate && !['FAULT', 'ALARM'].includes(upper(candidate)) ? candidate : ''
}

function nestedMessage(data) {
  const details = asObject(data.details)
  return asObject(data.message) || asObject(details?.message) || asObject(data.standardMessage)
}

function normalizeAlert(data, kind, topic) {
  const alarmLevel = upper(data.alarmLevel || data.level || (kind === 'fault' ? 'HIGH' : 'HIGH')) || 'HIGH'
  const alarmTypeValue = upper(data.alarmType || data.alarm_type || (kind === 'fault' ? 'DEVICE_FAULT' : 'MANUAL_ALARM')) || (kind === 'fault' ? 'DEVICE_FAULT' : 'MANUAL_ALARM')
  const alarmId = firstText(data.alarmId, data.id)
  const nested = nestedMessage(data)
  const standardMessageId = firstText(data.messageId, data.message_id, data.triggerId, data.trigger_id, nested?.messageId, nested?.message_id)
  const rawMessageId = firstText(data.rawMessageId, data.raw_message_id, nested?.rawMessageId, nested?.raw_message_id)
  const messageId = rawMessageId || standardMessageId
  const deviceId = firstText(data.deviceId, data.cameraId)
  const baseId = alarmId || messageId || `${kind}:${topic}:${timestampOf(data)}`
  return {
    id: baseId,
    kind,
    alarmId,
    triggerId: firstText(data.triggerId),
    messageId,
    rawMessageId,
    standardMessageId,
    tenantId: firstText(data.tenantId),
    productId: firstText(data.productId),
    deviceId,
    componentId: firstText(data.componentId),
    deviceName: firstText(data.deviceName, data.device_name, data.cameraName, nested?.deviceName, nested?.device_name),
    alarmType: alarmTypeValue,
    alarmTypeLabel: alarmType(alarmTypeValue),
    alarmLevel,
    alarmLevelLabel: label(alarmLevels, alarmLevel, '高'),
    status: upper(data.status || 'ACTIVE') || 'ACTIVE',
    source: firstText(data.source),
    timestamp: timestampOf(data),
    detail: detailText(data, kind) || (kind === 'fault' ? '检测到设备故障，请及时处理。' : '检测到设备异常报警，请及时处理。'),
    raw: data,
    topic
  }
}

export function parseRealtimeAlert(topic, payload) {
  const data = parsePayload(payload)
  const value = String(topic || '')
  if (!data) return null

  if (/\/iot\/alarm\/raised(?:\/|$)/.test(value)) return normalizeAlert(data, 'alarm', value)
  if (!value.includes('/iot/parsed/')) return null
  // The server emits separate authoritative alarms for component observations.
  if (Array.isArray(data.event?.components)) return null

  const messageType = upper(data.messageType || data.type)
  if (messageType === 'ALARM_REPORT' || messageType === 'ALARM') return normalizeAlert(data, 'alarm', value)
  if (messageType === 'EVENT_REPORT' && hasFaultEvent(data)) return normalizeAlert(data, 'fault', value)
  if (!messageType && hasFaultEvent(data)) return normalizeAlert(data, 'fault', value)
  return null
}

export function alertKeys(alert) {
  if (alert?.componentId && alert?.alarmId) return [String(alert.alarmId)]
  return [...new Set([alert?.alarmId, alert?.triggerId, alert?.messageId, alert?.rawMessageId, alert?.standardMessageId, alert?.id].filter(Boolean).map(String))]
}

export function alertTagType(level) {
  return ['CRITICAL', 'HIGH'].includes(upper(level)) ? 'danger' : ['MEDIUM', 'ACKED'].includes(upper(level)) ? 'warning' : 'info'
}

let audioContext

export async function playAlarmTone() {
  if (typeof window === 'undefined') return false
  const AudioContext = window.AudioContext || window.webkitAudioContext
  if (!AudioContext) return false
  try {
    audioContext ||= new AudioContext()
    if (audioContext.state === 'suspended') await audioContext.resume()
    const start = audioContext.currentTime
    for (const [offset, frequency] of [[0, 880], [0.16, 660], [0.32, 880]]) {
      const oscillator = audioContext.createOscillator()
      const gain = audioContext.createGain()
      oscillator.type = 'sine'
      oscillator.frequency.setValueAtTime(frequency, start + offset)
      gain.gain.setValueAtTime(0.0001, start + offset)
      gain.gain.exponentialRampToValueAtTime(0.18, start + offset + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, start + offset + 0.12)
      oscillator.connect(gain)
      gain.connect(audioContext.destination)
      oscillator.start(start + offset)
      oscillator.stop(start + offset + 0.13)
    }
    return true
  } catch {
    return false
  }
}
