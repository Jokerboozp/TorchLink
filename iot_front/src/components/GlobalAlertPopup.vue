<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { AlertTriangle, BellRing, Clock3, Volume2, X } from '@lucide/vue'
import { formatTime, session } from '../api'
import { alarmType } from '../labels'
import {
  alertKeys,
  alertTagType,
  DEFAULT_ALERT_SETTINGS,
  isWithinQuietHours,
  loadAlertSettings,
  normalizeAlertSettings,
  parseRealtimeAlert,
  playAlarmTone,
  saveAlertSettings
} from '../globalAlert'

const emit = defineEmits(['navigate'])

const popupAlerts = ref([])
const settingsVisible = ref(false)
const settings = reactive({ ...DEFAULT_ALERT_SETTINGS })
const settingsDraft = reactive({ ...DEFAULT_ALERT_SETTINGS })
const storageIdentity = { tenant: session.tenant, user: session.user }
const seenAlerts = new Map()

const quietHoursLabel = computed(() => {
  if (!settings.quietStart || !settings.quietEnd || settings.quietStart === settings.quietEnd) return '未设置'
  return `${settings.quietStart} - ${settings.quietEnd}`
})

function currentQuietHours() {
  return isWithinQuietHours(new Date(), settings)
}

function clearExpiredAlertKeys() {
  const now = Date.now()
  for (const [key, expiresAt] of seenAlerts) {
    if (expiresAt <= now) seenAlerts.delete(key)
  }
}

function isDuplicate(alert) {
  clearExpiredAlertKeys()
  const keys = alertKeys(alert)
  if (keys.some(key => seenAlerts.has(key))) return true
  for (const key of keys) seenAlerts.set(key, Date.now() + 10 * 60 * 1000)
  return false
}

function handleRealtime(event) {
  const alert = parseRealtimeAlert(event?.detail?.topic, event?.detail?.payload)
  if (!alert || isDuplicate(alert)) return

  const quiet = currentQuietHours()
  if (settings.soundEnabled && !quiet) void playAlarmTone()
  if (!settings.popupEnabled || quiet) return
  popupAlerts.value = [alert, ...popupAlerts.value].slice(0, 3)
}

function dismissAlert(id) {
  popupAlerts.value = popupAlerts.value.filter(item => item.id !== id)
}

function alertContent(item) {
  return item?.detail || (item?.kind === 'fault' ? '检测到设备故障，请及时处理。' : '检测到设备异常报警，请及时处理。')
}

function viewAlert(alert) {
  dismissAlert(alert.id)
  if (alert.alarmId) {
    emit('navigate', 'alarms', { alarmId: alert.alarmId })
    return
  }
  if (alert.messageId) {
    emit('navigate', 'raw', { messageId: alert.messageId, deviceId: alert.deviceId })
    return
  }
  emit('navigate', 'alarms')
}

function openSettings() {
  Object.assign(settingsDraft, settings)
  settingsVisible.value = true
}

function saveSettingsForm() {
  const next = normalizeAlertSettings(settingsDraft)
  Object.assign(settings, next)
  saveAlertSettings(window.localStorage, storageIdentity, next)
  if (!next.popupEnabled || currentQuietHours()) popupAlerts.value = []
  settingsVisible.value = false
  UiMessage.success('告警提醒设置已保存')
}

async function testSound() {
  const played = await playAlarmTone()
  if (played) UiMessage.success('警报声试听已播放')
  else UiMessage.warning('当前浏览器不支持声音播放，请检查浏览器权限')
}

defineExpose({ openSettings })

onMounted(() => {
  Object.assign(settings, loadAlertSettings(window.localStorage, storageIdentity))
  window.addEventListener('iot:realtime', handleRealtime)
})

onBeforeUnmount(() => {
  window.removeEventListener('iot:realtime', handleRealtime)
})
</script>

<template>
  <TransitionGroup v-if="popupAlerts.length" name="global-alert" tag="section" class="global-alert-popups" aria-label="实时报警通知">
    <article v-for="item in popupAlerts" :key="item.id" class="global-alert-popup" :class="`is-${item.kind}`" role="alertdialog" aria-live="assertive">
      <div class="global-alert-head">
        <div class="global-alert-icon"><component :is="item.kind === 'fault' ? AlertTriangle : BellRing" /></div>
        <div class="global-alert-title">
          <span class="global-alert-kicker">实时通知</span>
          <h3>{{ item.kind === 'fault' ? '设备故障' : '发现报警' }}</h3>
        </div>
        <button class="global-alert-close" type="button" aria-label="关闭报警提示" @click="dismissAlert(item.id)"><X /></button>
      </div>

      <div class="global-alert-facts">
        <div class="global-alert-fact">
          <span>设备名称</span>
          <strong>{{ item.deviceName || '未知设备' }}</strong>
        </div>
        <div class="global-alert-fact global-alert-fact-content">
          <span>报警内容</span>
          <strong>{{ alertContent(item) }}</strong>
        </div>
        <div class="global-alert-fact">
          <span>报警类型</span>
          <strong>{{ item.alarmTypeLabel || alarmType(item.alarmType) }}</strong>
        </div>
        <div class="global-alert-fact">
          <span>报警时间</span>
          <strong>{{ formatTime(item.timestamp) }}</strong>
        </div>
        <div class="global-alert-fact">
          <span>报警等级</span>
          <ui-tag :type="alertTagType(item.alarmLevel)" round>{{ item.alarmLevelLabel }}</ui-tag>
        </div>
      </div>
      <div class="global-alert-actions">
        <ui-button size="small" type="danger" plain @click="viewAlert(item)">{{ item.alarmId ? '查看告警详情' : '查看原始报文' }}</ui-button>
        <ui-button size="small" @click="dismissAlert(item.id)">关闭提示</ui-button>
      </div>
    </article>
  </TransitionGroup>

  <ui-dialog v-model="settingsVisible" title="告警提醒设置" width="min(540px, calc(100vw - 24px))">
    <div class="alert-settings">
      <div class="alert-setting-row">
        <div class="alert-setting-copy">
          <strong>显示报警弹窗</strong>
          <span>关闭后不显示右下角弹窗，告警仍会保留在告警中心。</span>
        </div>
        <ui-switch v-model="settingsDraft.popupEnabled" active-text="开启" inactive-text="关闭" />
      </div>
      <div class="alert-setting-row alert-setting-row-stack">
        <div class="alert-setting-copy">
          <strong>弹窗静默时段</strong>
          <span>每天该时段不显示弹窗，也不播放警报声；留空表示不设置。</span>
        </div>
        <div class="alert-quiet-times">
          <ui-time-picker v-model="settingsDraft.quietStart" value-format="HH:mm" format="HH:mm" placeholder="开始时间" clearable />
          <span>至</span>
          <ui-time-picker v-model="settingsDraft.quietEnd" value-format="HH:mm" format="HH:mm" placeholder="结束时间" clearable />
        </div>
      </div>
      <div class="alert-setting-row">
        <div class="alert-setting-copy">
          <strong>播放警报声</strong>
          <span>新报警或故障到达时播放提示音，静默时段除外。</span>
        </div>
        <div class="alert-sound-setting"><ui-switch v-model="settingsDraft.soundEnabled" active-text="开启" inactive-text="关闭" /><ui-button size="small" plain @click="testSound"><Volume2 />试听</ui-button></div>
      </div>
      <ui-alert type="info" :closable="false" show-icon>
        <template #title>当前静默时段：{{ quietHoursLabel }}</template>
      </ui-alert>
      <div class="alert-settings-note"><Clock3 /> 设置仅保存在当前浏览器的当前租户和用户下。</div>
    </div>
    <template #footer>
      <ui-button @click="settingsVisible = false">取消</ui-button>
      <ui-button type="primary" @click="saveSettingsForm">保存设置</ui-button>
    </template>
  </ui-dialog>
</template>

<style>
/* 弹窗与设置对话框传送到 body，样式以 global-alert- / alert- 前缀限定。 */
.global-alert-popups { position: fixed; z-index: 1200; right: 22px; bottom: 22px; display: grid; gap: 10px; width: min(410px, calc(100vw - 32px)); pointer-events: none; }
.global-alert-popup { padding: 15px; color: var(--text); background: var(--surface); border: 1px solid var(--danger-border); border-left: 4px solid var(--danger); border-radius: var(--radius-xl); box-shadow: var(--shadow-lg); pointer-events: auto; }
.global-alert-popup.is-fault { border-color: var(--warning-border); border-left-color: var(--warning); }
.global-alert-head { display: grid; grid-template-columns: 34px minmax(0, 1fr) 24px; align-items: start; gap: 10px; }
.global-alert-icon { display: grid; place-items: center; width: 34px; height: 34px; color: var(--danger); background: var(--danger-soft); border-radius: var(--radius-lg); }
.global-alert-icon svg { width: 18px; height: 18px; }
.is-fault .global-alert-icon { color: var(--warning-text); background: var(--warning-soft); }
.global-alert-title { min-width: 0; }
.global-alert-kicker { display: block; color: var(--text-muted); font-size: var(--font-size-xs); font-weight: var(--font-weight-semibold); letter-spacing: 0.08em; }
.global-alert-title h3 { margin: 3px 0 2px; overflow: hidden; color: var(--text-strong); font-size: 15px; line-height: var(--line-height-tight); text-overflow: ellipsis; white-space: nowrap; }
.global-alert-close { display: grid; place-items: center; width: 24px; height: 24px; color: var(--text-disabled); background: transparent; border: 0; border-radius: var(--radius-md); cursor: pointer; }
.global-alert-close:hover { color: var(--danger); background: var(--danger-soft); }
.global-alert-close svg { width: 15px; height: 15px; }
.global-alert-facts { display: grid; gap: var(--space-2); margin-top: var(--space-3); }
.global-alert-fact { display: grid; grid-template-columns: 64px minmax(0, 1fr); align-items: start; gap: 9px; min-width: 0; }
.global-alert-fact > span { color: var(--text-muted); font-size: var(--font-size-xs); line-height: var(--line-height-normal); }
.global-alert-fact > strong { min-width: 0; color: var(--text); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); line-height: var(--line-height-normal); overflow-wrap: anywhere; }
.global-alert-fact-content > strong { padding: 7px var(--space-2); color: var(--text-secondary); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-md); font-weight: var(--font-weight-medium); }
.global-alert-fact .ui-tag { justify-self: start; }
.global-alert-actions { display: flex; justify-content: flex-end; gap: 7px; margin-top: 13px; }
.global-alert-actions .ui-button { min-width: 74px; }
.global-alert-enter-active,
.global-alert-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.global-alert-enter-from,
.global-alert-leave-to { opacity: 0; transform: translateY(10px); }
.alert-settings { display: grid; gap: var(--space-3); }
.alert-setting-row { display: flex; align-items: center; justify-content: space-between; gap: 18px; min-height: 58px; padding: var(--space-3); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.alert-setting-row-stack { flex-direction: column; align-items: flex-start; gap: 10px; }
.alert-setting-copy { display: grid; gap: var(--space-1); min-width: 0; }
.alert-setting-copy strong { color: var(--text-strong); font-size: var(--font-size-sm); }
.alert-setting-copy span { color: var(--text-muted); font-size: var(--font-size-xs); line-height: 1.55; }
.alert-quiet-times { display: flex; align-items: center; gap: var(--space-2); width: 100%; }
.alert-quiet-times .ui-time-picker { width: 145px; }
.alert-quiet-times > span { color: var(--text-muted); font-size: var(--font-size-xs); }
.alert-sound-setting { display: flex; align-items: center; gap: 9px; }
.alert-settings-note { display: flex; align-items: center; gap: 5px; color: var(--text-muted); font-size: var(--font-size-xs); }
.alert-settings-note svg { width: 14px; height: 14px; }
@media (max-width: 767px) {
  .global-alert-popups { right: 12px; bottom: 12px; width: calc(100vw - 24px); }
  .global-alert-popup { padding: 13px; }
  .alert-setting-row:not(.alert-setting-row-stack) { flex-direction: column; align-items: flex-start; gap: 10px; }
  .alert-quiet-times .ui-time-picker { flex: 1; min-width: 0; width: 0; }
  .alert-sound-setting { justify-content: space-between; width: 100%; }
}
</style>
