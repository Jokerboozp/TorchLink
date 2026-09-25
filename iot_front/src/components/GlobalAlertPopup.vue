<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { AlertTriangle, BellRing, Clock3, Volume2, X } from '@lucide/vue' /* 引入当前代码需要的依赖。 */
import { formatTime, session } from '../api' /* 引入当前代码需要的依赖。 */
import { alarmType } from '../labels' /* 引入当前代码需要的依赖。 */
import { /* 引入当前代码需要的依赖。 */
  alertKeys, /* 执行当前语句并推进处理流程。 */
  alertTagType, /* 执行当前语句并推进处理流程。 */
  DEFAULT_ALERT_SETTINGS, /* 执行当前语句并推进处理流程。 */
  isWithinQuietHours, /* 执行当前语句并推进处理流程。 */
  loadAlertSettings, /* 执行当前语句并推进处理流程。 */
  normalizeAlertSettings, /* 执行当前语句并推进处理流程。 */
  parseRealtimeAlert, /* 执行当前语句并推进处理流程。 */
  playAlarmTone, /* 执行当前语句并推进处理流程。 */
  saveAlertSettings /* 执行当前语句并推进处理流程。 */
} from '../globalAlert' /* 结束当前表达式或代码块。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */

const popupAlerts = ref([]) /* 声明 popupAlerts。 */
const settingsVisible = ref(false) /* 声明 settingsVisible。 */
const settings = reactive({ ...DEFAULT_ALERT_SETTINGS }) /* 声明 settings。 */
const settingsDraft = reactive({ ...DEFAULT_ALERT_SETTINGS }) /* 声明 settingsDraft。 */
const storageIdentity = { tenant: session.tenant, user: session.user } /* 声明 storageIdentity。 */
const seenAlerts = new Map() /* 声明 seenAlerts。 */

const quietHoursLabel = computed(() => { /* 声明 quietHoursLabel。 */
  if (!settings.quietStart || !settings.quietEnd || settings.quietStart === settings.quietEnd) return '未设置' /* 判断条件并选择处理分支。 */
  return `${settings.quietStart} - ${settings.quietEnd}` /* 返回当前处理结果。 */
}) /* 结束当前表达式或代码块。 */

function currentQuietHours() { /* 定义 currentQuietHours 函数。 */
  return isWithinQuietHours(new Date(), settings) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function clearExpiredAlertKeys() { /* 定义 clearExpiredAlertKeys 函数。 */
  const now = Date.now() /* 声明 now。 */
  for (const [key, expiresAt] of seenAlerts) { /* 循环处理当前数据。 */
    if (expiresAt <= now) seenAlerts.delete(key) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function isDuplicate(alert) { /* 定义 isDuplicate 函数。 */
  clearExpiredAlertKeys() /* 执行当前语句并推进处理流程。 */
  const keys = alertKeys(alert) /* 声明 keys。 */
  if (keys.some(key => seenAlerts.has(key))) return true /* 判断条件并选择处理分支。 */
  for (const key of keys) seenAlerts.set(key, Date.now() + 10 * 60 * 1000) /* 循环处理当前数据。 */
  return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function handleRealtime(event) { /* 定义 handleRealtime 函数。 */
  const alert = parseRealtimeAlert(event?.detail?.topic, event?.detail?.payload) /* 声明 alert。 */
  if (!alert || isDuplicate(alert)) return /* 判断条件并选择处理分支。 */

  const quiet = currentQuietHours() /* 声明 quiet。 */
  if (settings.soundEnabled && !quiet) void playAlarmTone() /* 判断条件并选择处理分支。 */
  if (!settings.popupEnabled || quiet) return /* 判断条件并选择处理分支。 */
  popupAlerts.value = [alert, ...popupAlerts.value].slice(0, 3) /* 更新 popupAlerts.value 的值。 */
} /* 结束当前表达式或代码块。 */

function dismissAlert(id) { /* 定义 dismissAlert 函数。 */
  popupAlerts.value = popupAlerts.value.filter(item => item.id !== id) /* 更新 popupAlerts.value 的值。 */
} /* 结束当前表达式或代码块。 */

function alertContent(item) { /* 定义 alertContent 函数。 */
  return item?.detail || (item?.kind === 'fault' ? '检测到设备故障，请及时处理。' : '检测到设备异常报警，请及时处理。') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function viewAlert(alert) { /* 定义 viewAlert 函数。 */
  dismissAlert(alert.id) /* 执行当前语句并推进处理流程。 */
  if (alert.alarmId) { /* 判断条件并选择处理分支。 */
    emit('navigate', 'alarms', { alarmId: alert.alarmId }) /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (alert.messageId) { /* 判断条件并选择处理分支。 */
    emit('navigate', 'raw', { messageId: alert.messageId, deviceId: alert.deviceId }) /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  emit('navigate', 'alarms') /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function openSettings() { /* 定义 openSettings 函数。 */
  Object.assign(settingsDraft, settings) /* 执行当前语句并推进处理流程。 */
  settingsVisible.value = true /* 更新 settingsVisible.value 的值。 */
} /* 结束当前表达式或代码块。 */

function saveSettingsForm() { /* 定义 saveSettingsForm 函数。 */
  const next = normalizeAlertSettings(settingsDraft) /* 声明 next。 */
  Object.assign(settings, next) /* 执行当前语句并推进处理流程。 */
  saveAlertSettings(window.localStorage, storageIdentity, next) /* 执行当前语句并推进处理流程。 */
  if (!next.popupEnabled || currentQuietHours()) popupAlerts.value = [] /* 判断条件并选择处理分支。 */
  settingsVisible.value = false /* 更新 settingsVisible.value 的值。 */
  UiMessage.success('告警提醒设置已保存') /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

async function testSound() { /* 定义 testSound 函数。 */
  const played = await playAlarmTone() /* 声明 played。 */
  if (played) UiMessage.success('警报声试听已播放') /* 判断条件并选择处理分支。 */
  else UiMessage.warning('当前浏览器不支持声音播放，请检查浏览器权限') /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

defineExpose({ openSettings }) /* 执行当前语句并推进处理流程。 */

onMounted(() => { /* 执行当前语句并推进处理流程。 */
  Object.assign(settings, loadAlertSettings(window.localStorage, storageIdentity)) /* 执行当前语句并推进处理流程。 */
  window.addEventListener('iot:realtime', handleRealtime) /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */

onBeforeUnmount(() => { /* 执行当前语句并推进处理流程。 */
  window.removeEventListener('iot:realtime', handleRealtime) /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
</script>

<template>
  <TransitionGroup v-if="popupAlerts.length" name="global-alert" tag="section" class="global-alert-popups" aria-label="实时报警通知"> <!-- 渲染 TransitionGroup 界面元素。 -->
    <article v-for="item in popupAlerts" :key="item.id" class="global-alert-popup" :class="`is-${item.kind}`" role="alertdialog" aria-live="assertive"> <!-- 渲染 article 界面元素。 -->
      <div class="global-alert-head"> <!-- 渲染 div 界面元素。 -->
        <div class="global-alert-icon"><component :is="item.kind === 'fault' ? AlertTriangle : BellRing" /></div> <!-- 渲染 div 界面元素。 -->
        <div class="global-alert-title"> <!-- 渲染 div 界面元素。 -->
          <span class="global-alert-kicker">实时通知</span> <!-- 渲染 span 界面元素。 -->
          <h3>{{ item.kind === 'fault' ? '设备故障' : '发现报警' }}</h3> <!-- 渲染 h3 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <button class="global-alert-close" type="button" aria-label="关闭报警提示" @click="dismissAlert(item.id)"><X /></button> <!-- 渲染 button 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->

      <div class="global-alert-facts"> <!-- 渲染 div 界面元素。 -->
        <div class="global-alert-fact"> <!-- 渲染 div 界面元素。 -->
          <span>设备名称</span> <!-- 渲染 span 界面元素。 -->
          <strong>{{ item.deviceName || '未知设备' }}</strong> <!-- 渲染 strong 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="global-alert-fact global-alert-fact-content"> <!-- 渲染 div 界面元素。 -->
          <span>报警内容</span> <!-- 渲染 span 界面元素。 -->
          <strong>{{ alertContent(item) }}</strong> <!-- 渲染 strong 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="global-alert-fact"> <!-- 渲染 div 界面元素。 -->
          <span>报警类型</span> <!-- 渲染 span 界面元素。 -->
          <strong>{{ item.alarmTypeLabel || alarmType(item.alarmType) }}</strong> <!-- 渲染 strong 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="global-alert-fact"> <!-- 渲染 div 界面元素。 -->
          <span>报警时间</span> <!-- 渲染 span 界面元素。 -->
          <strong>{{ formatTime(item.timestamp) }}</strong> <!-- 渲染 strong 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="global-alert-fact"> <!-- 渲染 div 界面元素。 -->
          <span>报警等级</span> <!-- 渲染 span 界面元素。 -->
          <ui-tag :type="alertTagType(item.alarmLevel)" round>{{ item.alarmLevelLabel }}</ui-tag> <!-- 渲染 ui-tag 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
      </div> <!-- 结束当前界面区域。 -->
      <div class="global-alert-actions"> <!-- 渲染 div 界面元素。 -->
        <ui-button size="small" type="danger" plain @click="viewAlert(item)">{{ item.alarmId ? '查看告警详情' : '查看原始报文' }}</ui-button> <!-- 渲染 ui-button 界面元素。 -->
        <ui-button size="small" @click="dismissAlert(item.id)">关闭提示</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
    </article> <!-- 结束当前界面区域。 -->
  </TransitionGroup> <!-- 结束当前界面区域。 -->

  <ui-dialog v-model="settingsVisible" title="告警提醒设置" width="min(540px, calc(100vw - 24px))"> <!-- 渲染 ui-dialog 界面元素。 -->
    <div class="alert-settings"> <!-- 渲染 div 界面元素。 -->
      <div class="alert-setting-row"> <!-- 渲染 div 界面元素。 -->
        <div class="alert-setting-copy"> <!-- 渲染 div 界面元素。 -->
          <strong>显示报警弹窗</strong> <!-- 渲染 strong 界面元素。 -->
          <span>关闭后不显示右下角弹窗，告警仍会保留在告警中心。</span> <!-- 渲染 span 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <ui-switch v-model="settingsDraft.popupEnabled" active-text="开启" inactive-text="关闭" /> <!-- 渲染 ui-switch 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <div class="alert-setting-row alert-setting-row-stack"> <!-- 渲染 div 界面元素。 -->
        <div class="alert-setting-copy"> <!-- 渲染 div 界面元素。 -->
          <strong>弹窗静默时段</strong> <!-- 渲染 strong 界面元素。 -->
          <span>每天该时段不显示弹窗，也不播放警报声；留空表示不设置。</span> <!-- 渲染 span 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="alert-quiet-times"> <!-- 渲染 div 界面元素。 -->
          <ui-time-picker v-model="settingsDraft.quietStart" value-format="HH:mm" format="HH:mm" placeholder="开始时间" clearable /> <!-- 渲染 ui-time-picker 界面元素。 -->
          <span>至</span> <!-- 渲染 span 界面元素。 -->
          <ui-time-picker v-model="settingsDraft.quietEnd" value-format="HH:mm" format="HH:mm" placeholder="结束时间" clearable /> <!-- 渲染 ui-time-picker 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
      </div> <!-- 结束当前界面区域。 -->
      <div class="alert-setting-row"> <!-- 渲染 div 界面元素。 -->
        <div class="alert-setting-copy"> <!-- 渲染 div 界面元素。 -->
          <strong>播放警报声</strong> <!-- 渲染 strong 界面元素。 -->
          <span>新报警或故障到达时播放提示音，静默时段除外。</span> <!-- 渲染 span 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="alert-sound-setting"><ui-switch v-model="settingsDraft.soundEnabled" active-text="开启" inactive-text="关闭" /><ui-button size="small" plain @click="testSound"><Volume2 />试听</ui-button></div> <!-- 渲染 div 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <ui-alert type="info" :closable="false" show-icon> <!-- 渲染 ui-alert 界面元素。 -->
        <template #title>当前静默时段：{{ quietHoursLabel }}</template>
      </ui-alert> <!-- 结束当前界面区域。 -->
      <div class="alert-settings-note"><Clock3 /> 设置仅保存在当前浏览器的当前租户和用户下。</div> <!-- 渲染 div 界面元素。 -->
    </div> <!-- 结束当前界面区域。 -->
    <template #footer>
      <ui-button @click="settingsVisible = false">取消</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button type="primary" @click="saveSettingsForm">保存设置</ui-button> <!-- 渲染 ui-button 界面元素。 -->
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
