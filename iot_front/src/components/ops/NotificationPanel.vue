<script setup>
// 通知路由与接收人：编辑 Alertmanager 配置中的 route 与 receivers（Webhook、邮件）。
// 保存时平台校验、写入并让 Alertmanager 重新加载，失败自动恢复原配置；未识别的配置原样保留。
import { computed, onMounted, ref } from 'vue'
import { Plus, RefreshCw, Send, Trash2 } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { relativeTime } from '../../ops/format.js'
import RouteNode from './RouteNode.vue'
import SecretField from './SecretField.vue'

const props = defineProps({ labels: { type: Array, default: () => [] } })
const config = ref(null)
const draft = ref(null)
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const saveError = ref('')
const testing = ref('')
const canSave = computed(() => config.value?.writable && can('PUT /api/v1/ops/notifications'))
const canTest = computed(() => can('POST /api/v1/ops/notifications/receivers/:name/test'))
const receiverNames = computed(() => (draft.value?.receivers || []).map(r => r.name).filter(Boolean))
const dirty = computed(() => draft.value && JSON.stringify(toApi(draft.value)) !== JSON.stringify(toApi(fromApi(config.value))))
const toOp = m => (m.isEqual ? (m.isRegex ? '=~' : '=') : m.isRegex ? '!~' : '!=')
const fromOp = m => ({ name: m.name, value: m.value, isRegex: m.op.endsWith('~'), isEqual: !m.op.startsWith('!') })

function routeFromApi(r) { return { ...r, groupBy: r.groupBy || [], matchers: (r.matchers || []).map(m => ({ name: m.name, op: toOp(m), value: m.value })), routes: (r.routes || []).map(routeFromApi) } }
function routeToApi(r) {
  const out = { receiver: r.receiver || '', groupBy: r.groupBy?.length ? r.groupBy : undefined, groupWait: r.groupWait || undefined, groupInterval: r.groupInterval || undefined, repeatInterval: r.repeatInterval || undefined, matchers: (r.matchers || []).filter(m => m.name).map(fromOp), continue: Boolean(r.continue), muteTimeIntervals: r.muteTimeIntervals, routes: (r.routes || []).map(routeToApi) }
  if (!out.matchers.length) delete out.matchers
  if (!out.routes.length) delete out.routes
  return out
}
// originalName 让后端找到原接收人，保留未编辑的字段和未修改的凭据。
function fromApi(c) { return c ? { deviceAlarmReceiver: c.deviceAlarmReceiver || '', route: routeFromApi(c.route || {}), receivers: JSON.parse(JSON.stringify(c.receivers || [])).map(r => ({ ...r, originalName: r.name })) } : null }
function toApi(d) { return { deviceAlarmReceiver: d.deviceAlarmReceiver || '', route: routeToApi(d.route), receivers: d.receivers } }

async function load() {
  loading.value = true
  try {
    config.value = await opsGet('/api/v1/ops/notifications')
    draft.value = fromApi(config.value)
    error.value = ''
    saveError.value = ''
  } catch (e) { error.value = opsErrorText(e) } finally { loading.value = false }
}
// 接收人改名后同步更新引用它的路由；在输入框失焦时处理，避免输入过程中的临时重名。
let nameAtFocus = ''
function renameReceiver(receiver) {
  const from = nameAtFocus
  const to = receiver.name.trim()
  if (!from || from === to || draft.value.receivers.filter(r => r.name.trim() === to).length > 1) return
  const walk = route => { if (route.receiver === from) route.receiver = to; (route.routes || []).forEach(walk) }
  walk(draft.value.route)
  if (draft.value.deviceAlarmReceiver === from) draft.value.deviceAlarmReceiver = to
}
function addReceiver() { draft.value.receivers.push({ name: `receiver-${draft.value.receivers.length + 1}`, webhooks: [], emails: [] }) }
function addWebhook(receiver) { receiver.webhooks.push({ url: { set: false, mode: 'replace', value: '' }, bearerToken: { set: false }, sendResolved: true, maxAlerts: 0, timeout: '' }) }
function addEmail(receiver) { receiver.emails.push({ to: '', from: '', smarthost: '', authUsername: '', authPassword: { set: false }, sendResolved: false }) }
function addDeviceEmail() {
  let name = '设备告警邮件'
  let suffix = 2
  while (draft.value.receivers.some(r => r.name === name)) name = `设备告警邮件-${suffix++}`
  const receiver = { name, webhooks: [], emails: [] }
  addEmail(receiver)
  receiver.emails[0].smarthost = 'smtp.163.com:465'
  receiver.emails[0].requireTLS = true
  draft.value.receivers.push(receiver)
  draft.value.deviceAlarmReceiver = name
}
function use163(mail) {
  mail.smarthost = 'smtp.163.com:465'
  mail.requireTLS = true
  if (mail.from) mail.authUsername = mail.from
}
const deviceReceiverOptions = computed(() => [
  { label: '关闭设备告警邮件', value: '' },
  ...(draft.value?.receivers || []).filter(r => r.name && r.emails?.length && !r.webhooks?.length && !r.readOnly?.length).map(r => ({ label: r.name, value: r.name })),
])
function removeReceiver(index) {
  const name = draft.value.receivers[index].name
  const used = draft.value.deviceAlarmReceiver === name || JSON.stringify(routeToApi(draft.value.route)).includes(`"receiver":${JSON.stringify(name)}`)
  if (used) { UiMessage.warning(`接收人“${name}”仍被路由使用，请先修改路由`); return }
  draft.value.receivers.splice(index, 1)
}
async function save() {
  saving.value = true
  saveError.value = ''
  try {
    const body = { revision: config.value.revision, ...toApi(draft.value) }
    config.value = await opsSend('PUT', '/api/v1/ops/notifications', body)
    draft.value = fromApi(config.value)
    UiMessage.success('通知配置已保存，Alertmanager 已重新加载')
  } catch (e) {
    if (e.status === 409 && e.code === 'OPS_CONFLICT') saveError.value = '配置已被他人修改，请刷新后重新编辑'
    else saveError.value = [opsErrorText(e), e.details?.reason, e.details?.rolledBack ? '已恢复原配置' : ''].filter(Boolean).join('：')
  } finally { saving.value = false }
}
async function test(receiver) {
  try {
    await UiMessageBox.confirm(`将通过 Alertmanager 向接收人“${receiver.name}”的真实渠道发送一条测试告警（TorchLinkNotificationTest，5 分钟后自动恢复）。请确认该渠道是测试目标或已获得授权。`, '发送测试通知', { confirmButtonText: '发送' })
  } catch { return }
  testing.value = receiver.name
  try {
    await opsSend('POST', `/api/v1/ops/notifications/receivers/${encodeURIComponent(receiver.name)}/test`)
    UiMessage.success('测试告警已提交到 Alertmanager，约数秒后由对应渠道发送')
  } catch (e) { UiMessage.error(opsErrorText(e)) } finally { testing.value = '' }
}
const savedNames = computed(() => new Set((config.value?.receivers || []).map(r => r.name)))
onMounted(load)
</script>

<template>
  <div class="notify">
    <div class="notify__toolbar">
      <p>监控通知和设备告警邮件由 Alertmanager 统一发送；设备告警的处理状态仍由平台告警中心管理。<span v-if="config?.updatedBy">最近由 {{ config.updatedBy }} {{ relativeTime(config.updatedAt) }}修改。</span></p>
      <div>
        <ui-button size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-if="canSave" size="small" :disabled="!dirty || saving" @click="draft = fromApi(config)">还原</ui-button>
        <ui-button v-if="canSave" size="small" type="primary" :loading="saving" :disabled="!dirty" @click="save">保存并重新加载</ui-button>
      </div>
    </div>
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" show-icon />
    <ui-alert v-if="config && !config.writable" type="warning" :closable="false" :title="`当前配置包含平台不能安全编辑的内容，只能查看：${(config.preserved || []).join('、') || '未配置 Alertmanager 配置文件'}`" />
    <p v-else-if="config?.preserved?.length" class="muted">以下配置由平台原样保留、不在此编辑：{{ config.preserved.join('、') }}<template v-if="config.inhibitRules">；抑制规则 {{ config.inhibitRules }} 条</template></p>

    <template v-if="draft">
      <section class="notify__section">
        <h3>设备告警逐条邮件</h3>
        <p class="muted">启用后，每次设备报警上报都单独发送邮件，包含设备、部件、类型、等级、时间及告警内容。同一活动告警再次上报也会通知，无需先恢复；相同报文重试不重复发信，启用前的历史上报不补发。此设置覆盖全平台设备，请使用授权的运维邮箱。</p>
        <div class="device-notification-controls">
          <ui-select v-model="draft.deviceAlarmReceiver" :disabled="!canSave" size="small" placeholder="关闭设备告警邮件" aria-label="设备告警邮件接收人"><ui-option v-for="option in deviceReceiverOptions" :key="option.value" :value="option.value" :label="option.label" /></ui-select>
          <ui-button v-if="canSave" size="small" @click="addDeviceEmail"><Plus />新增设备告警邮件</ui-button>
        </div>
        <p class="muted">设备告警邮件接收人需关闭“发送恢复通知”。保存后生效；确认、恢复和关闭仍在告警中心处理。</p>
      </section>
      <section class="notify__section">
        <h3>监控通知路由</h3>
        <RouteNode :route="draft.route" :receivers="receiverNames" :labels="labels" top :disabled="!canSave" />
      </section>
      <section class="notify__section">
        <div class="section-head"><h3>接收人</h3><ui-button v-if="canSave" size="small" text type="primary" @click="addReceiver"><Plus />添加接收人</ui-button></div>
        <article v-for="(receiver, index) in draft.receivers" :key="index" class="receiver">
          <header>
            <ui-input v-model="receiver.name" size="small" :disabled="!canSave" class="receiver__name" aria-label="接收人名称" @focus="nameAtFocus = receiver.name" @change="renameReceiver(receiver)" />
            <small v-if="!receiver.webhooks.length && !receiver.emails.length && !receiver.readOnly?.length">未配置渠道（告警会被丢弃）</small>
            <span class="receiver__actions">
              <ui-button v-if="canTest && savedNames.has(receiver.name) && (receiver.webhooks.length || receiver.emails.length)" size="small" :loading="testing === receiver.name" :disabled="dirty" :title="dirty ? '请先保存修改' : ''" @click="test(receiver)"><Send />测试</ui-button>
              <ui-button v-if="canSave" text size="small" type="danger" aria-label="删除接收人" @click="removeReceiver(index)"><Trash2 /></ui-button>
            </span>
          </header>
          <p v-if="receiver.readOnly?.length" class="muted">另有 {{ receiver.readOnly.join('、') }} 渠道，平台原样保留、不编辑。</p>
          <div v-for="(hook, i) in receiver.webhooks" :key="`w${i}`" class="channel">
            <div class="channel__head"><strong>Webhook</strong><ui-button v-if="canSave" text size="small" type="danger" @click="receiver.webhooks.splice(i, 1)">移除</ui-button></div>
            <label>地址<SecretField v-model="hook.url" :allow-clear="false" label="Webhook 地址" placeholder="https://…（地址可能包含令牌，保存后不回显）" /></label>
            <label>Bearer 令牌<SecretField v-model="hook.bearerToken" label="Bearer 令牌" placeholder="可选" /></label>
            <div class="channel__row">
              <ui-checkbox v-model="hook.sendResolved" :disabled="!canSave">发送恢复通知</ui-checkbox>
              <label>单次最多告警数<ui-input-number v-model="hook.maxAlerts" size="small" :min="0" :max="1000" :disabled="!canSave" placeholder="0 表示不限" /></label>
              <label>超时<ui-input v-model="hook.timeout" size="small" :disabled="!canSave" placeholder="例如 10s" /></label>
            </div>
          </div>
          <div v-for="(mail, i) in receiver.emails" :key="`e${i}`" class="channel">
            <div class="channel__head"><strong>邮件</strong><ui-button v-if="canSave" text size="small" type="danger" @click="receiver.emails.splice(i, 1)">移除</ui-button></div>
            <div class="channel__grid">
              <label>收件人<ui-input v-model="mail.to" size="small" :disabled="!canSave" placeholder="ops@example.com，多个用逗号分隔" /></label>
              <label>发件人<ui-input v-model="mail.from" size="small" :disabled="!canSave" placeholder="留空使用全局配置" /></label>
              <label>SMTP 服务器<ui-input v-model="mail.smarthost" size="small" :disabled="!canSave" placeholder="smtp.example.com:587，留空使用全局配置" /></label>
              <label>SMTP 用户名<ui-input v-model="mail.authUsername" size="small" :disabled="!canSave" autocomplete="off" /></label>
            </div>
            <label>SMTP 密码 / 客户端授权码<SecretField v-model="mail.authPassword" label="SMTP 密码 / 客户端授权码" placeholder="163 邮箱填写客户端授权码" /></label>
            <div class="channel__row"><ui-button v-if="canSave" size="small" text @click="use163(mail)">使用 163 邮箱配置</ui-button><p class="muted">163：smtp.163.com:465（SSL/TLS），用户名为完整发件邮箱。请在邮箱“设置 → POP/SMTP/IMAP”开启 SMTP，填写客户端授权码。</p></div>
            <ui-checkbox v-model="mail.sendResolved" :disabled="!canSave">发送恢复通知</ui-checkbox>
          </div>
          <div v-if="canSave" class="receiver__add"><ui-button size="small" text type="primary" @click="addWebhook(receiver)"><Plus />Webhook</ui-button><ui-button size="small" text type="primary" @click="addEmail(receiver)"><Plus />邮件</ui-button></div>
        </article>
      </section>
    </template>
    <ui-skeleton v-else-if="loading" :rows="4" animated />
  </div>
</template>

<style scoped>
.notify { display: grid; gap: var(--space-4); min-width: 0; }
.notify__toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.notify__toolbar p, .muted { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.notify__toolbar > div { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.notify__section { display: grid; gap: var(--space-2); }
.notify__section h3 { margin: 0; color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.section-head { display: flex; align-items: center; justify-content: space-between; }
.receiver { display: grid; gap: var(--space-2); padding: var(--space-3); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-md); }
.receiver header { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.receiver__name { width: 240px; }
.receiver header small { color: var(--warning-text); font-size: var(--font-size-xs); }
.receiver__actions { display: inline-flex; gap: var(--space-2); margin-left: auto; }
.channel { display: grid; gap: var(--space-2); padding: var(--space-2) var(--space-3); background: var(--surface-muted); border-radius: var(--radius-sm); }
.channel label { display: grid; gap: 4px; color: var(--text-secondary); font-size: var(--font-size-xs); }
.channel__head { display: flex; align-items: center; justify-content: space-between; font-size: var(--font-size-sm); }
.channel__row { display: flex; align-items: end; flex-wrap: wrap; gap: var(--space-3); }
.channel__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-2); }
.device-notification-controls { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.device-notification-controls > :first-child { width: min(100%, 320px); }
.receiver__add { display: flex; gap: var(--space-2); }
@media (max-width: 767px) {
  .channel__grid { grid-template-columns: minmax(0, 1fr); }
  .receiver__name { width: 100%; }
}
</style>
