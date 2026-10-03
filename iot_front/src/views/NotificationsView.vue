<script setup>
// 告警通知：配置通知渠道（机器人、Webhook、邮件）和按等级逐级升级的通知策略。
import { computed, onMounted, reactive, ref } from 'vue'
import { Plus, RefreshCw, Send, Trash2 } from '@lucide/vue'
import { api, notifyError } from '../api'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { alarmLevels, alarmTypes } from '../labels'
import { channelForm, channelPayload, channelTypes, blankStage, policyForm, policyPayload, stageSummary } from '../notifications'
import DataTableCard from '../components/layout/DataTableCard.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
defineEmits(['navigate'])

const tab = ref('policies')
const channels = ref([]), policies = ref([]), loading = ref(false), loadError = ref('')
const options = reactive({ users: [], roles: [], stations: [] })
const dialog = ref(''), saving = ref(false), saveError = ref('')
const channel = reactive(channelForm()), policy = reactive(policyForm())
const testing = ref(''), testTarget = reactive({ emails: '', mobiles: '' })
const typeInfo = computed(() => channelTypes.find(item => item.value === channel.type) || channelTypes[0])
const channelName = id => channels.value.find(item => item.id === id)?.name || id
const typeLabel = type => channelTypes.find(item => item.value === type)?.label || type

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [c, p, o] = await Promise.all([api('/api/v1/notifications/channels'), api('/api/v1/notifications/policies'), api('/api/v1/notifications/options')])
    channels.value = c.items || []
    policies.value = p.items || []
    Object.assign(options, o)
  } catch (e) {
    loadError.value = e.message || '读取告警通知配置失败'
  } finally {
    loading.value = false
  }
}

function openChannel(value) { Object.assign(channel, channelForm(value)); saveError.value = ''; dialog.value = 'channel' }
function openPolicy(value) { Object.assign(policy, policyForm(value)); saveError.value = ''; dialog.value = 'policy' }

async function save() {
  if (saving.value) return
  saving.value = true
  saveError.value = ''
  try {
    const isChannel = dialog.value === 'channel'
    const value = isChannel ? channelPayload(channel) : policyPayload(policy)
    const base = isChannel ? '/api/v1/notifications/channels' : '/api/v1/notifications/policies'
    await api(value.version ? `${base}/${encodeURIComponent(value.id)}` : base, { method: value.version ? 'PUT' : 'POST', body: JSON.stringify(value) })
    dialog.value = ''
    UiMessage.success('已保存')
    await load()
  } catch (e) {
    saveError.value = e.message || '保存失败'
  } finally {
    saving.value = false
  }
}

async function remove(kind, row) {
  try { await UiMessageBox.confirm(`确认删除“${row.name}”？`, '删除确认', { type: 'warning' }) } catch { return }
  try {
    await api(`/api/v1/notifications/${kind}/${encodeURIComponent(row.id)}`, { method: 'DELETE' })
    await load()
  } catch (e) { notifyError(e) }
}

function openTest(row) { testing.value = row.id; testTarget.emails = ''; testTarget.mobiles = ''; dialog.value = 'test' }
async function sendTest() {
  saving.value = true
  saveError.value = ''
  try {
    const split = v => v.split(/[,，\s]+/).map(item => item.trim()).filter(Boolean)
    await api(`/api/v1/notifications/channels/${encodeURIComponent(testing.value)}/test`, { method: 'POST', body: JSON.stringify({ emails: split(testTarget.emails), mobiles: split(testTarget.mobiles) }) })
    dialog.value = ''
    UiMessage.success('测试消息已发送，请在接收端确认')
  } catch (e) {
    saveError.value = e.message || '发送失败'
  } finally {
    saving.value = false
  }
}

function addStage() {
  const last = policy.stages[policy.stages.length - 1]
  policy.stages.push(blankStage((last?.delaySeconds || 0) + 300))
}

const channelActions = row => [
  { key: 'test', label: '测试', permission: 'POST /api/v1/notifications/channels/:id/test', onClick: () => openTest(row) },
  { key: 'edit', label: '编辑', permission: 'PUT /api/v1/notifications/channels/:id', onClick: () => openChannel(row) },
  { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/notifications/channels/:id', onClick: () => remove('channels', row) }
]
const policyActions = row => [
  { key: 'edit', label: '编辑', permission: 'PUT /api/v1/notifications/policies/:id', onClick: () => openPolicy(row) },
  { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/notifications/policies/:id', onClick: () => remove('policies', row) }
]
onMounted(load)
</script>

<template>
  <div class="notify-page">
    <ui-tabs v-model="tab">
      <ui-tab-pane name="policies" label="通知策略" />
      <ui-tab-pane name="channels" label="通知渠道" />
    </ui-tabs>
    <div class="notify-toolbar">
      <span class="notify-hint">{{ tab === 'policies' ? '告警首次上报时按策略立即通知；超过等待时间仍未确认则逐级升级，确认、恢复或关闭后停止。' : '凭据加密保存且不会回显；短信、语音可由外部系统通过 Webhook 转发。' }}</span>
      <div class="notify-actions">
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-if="tab === 'policies'" v-permission="'POST /api/v1/notifications/policies'" type="primary" :disabled="!channels.length" @click="openPolicy()"><Plus />新增策略</ui-button>
        <ui-button v-else v-permission="'POST /api/v1/notifications/channels'" type="primary" @click="openChannel()"><Plus />新增渠道</ui-button>
      </div>
    </div>
    <ui-alert v-if="tab === 'policies' && !loading && !channels.length && !loadError" type="info" :closable="false" title="请先在“通知渠道”中添加至少一个渠道" />

    <DataTableCard v-if="tab === 'policies'" :title="`通知策略 · ${policies.length} 条`" :error="loadError" @retry="load">
      <ui-table :data="policies" :loading="loading" empty-text="暂无通知策略">
        <ui-table-column label="策略" min-width="160"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="notify-subline">{{ row.levels?.length ? row.levels.map(l => alarmLevels[l] || l).join('、') : '全部等级' }}{{ row.alarmTypes?.length ? ` · ${row.alarmTypes.length} 种类型` : '' }}</small></template></ui-table-column>
        <ui-table-column label="通知步骤" min-width="320"><template #default="{ row }"><ol class="notify-stages"><li v-for="(stage, index) in row.stages" :key="index">{{ stageSummary(stage, index, channelName) }}</li></ol></template></ui-table-column>
        <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '启用' : '停用'" /></template></ui-table-column>
        <ui-table-column label="操作" width="140" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="policyActions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>
    <DataTableCard v-else :title="`通知渠道 · ${channels.length} 个`" :error="loadError" @retry="load">
      <ui-table :data="channels" :loading="loading" empty-text="暂无通知渠道">
        <ui-table-column label="渠道" min-width="180"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="notify-subline">{{ typeLabel(row.type) }}</small></template></ui-table-column>
        <ui-table-column label="地址" min-width="220"><template #default="{ row }">{{ row.type === 'smtp' ? `${row.config.host}:${row.config.port} · ${row.config.from}` : (row.config.urlHint || '—') }}</template></ui-table-column>
        <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '启用' : '停用'" /></template></ui-table-column>
        <ui-table-column label="操作" width="180" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="channelActions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog :model-value="dialog === 'channel'" :title="channel.version ? '编辑通知渠道' : '新增通知渠道'" width="min(640px,94vw)" :close-on-click-modal="!saving" @update:model-value="v => { if (!v && !saving) dialog = '' }">
      <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="notify-gap" />
      <ui-form label-position="top" :disabled="saving">
        <div class="notify-grid">
          <ui-form-item label="名称" required><ui-input v-model="channel.name" maxlength="64" placeholder="例如：消防值班群" /></ui-form-item>
          <ui-form-item label="类型" required><ui-select v-model="channel.type" :disabled="Boolean(channel.version)"><ui-option v-for="item in channelTypes" :key="item.value" :value="item.value" :label="item.label" /></ui-select></ui-form-item>
        </div>
        <template v-if="channel.type === 'smtp'">
          <div class="notify-grid">
            <ui-form-item label="SMTP 服务器" required><ui-input v-model="channel.config.host" placeholder="smtp.example.com" /></ui-form-item>
            <ui-form-item label="端口" required><ui-input-number v-model="channel.config.port" :min="1" :max="65535" /></ui-form-item>
            <ui-form-item label="加密方式"><ui-select v-model="channel.config.security"><ui-option value="tls" label="TLS（465）" /><ui-option value="starttls" label="STARTTLS（587）" /><ui-option value="none" label="不加密（仅内网中继）" /></ui-select></ui-form-item>
            <ui-form-item label="发件人邮箱" required><ui-input v-model="channel.config.from" placeholder="alarm@example.com" /></ui-form-item>
            <ui-form-item label="登录用户名"><ui-input v-model="channel.config.username" autocomplete="off" /></ui-form-item>
            <ui-form-item label="登录密码"><ui-input v-model="channel.secret.password" type="password" show-password autocomplete="new-password" :placeholder="channel.secretSet ? '已保存，留空保持不变' : ''" /></ui-form-item>
          </div>
        </template>
        <template v-else>
          <ui-form-item :label="channel.type === 'webhook' ? 'Webhook 地址' : '机器人地址'" :required="!channel.version"><ui-input v-model="channel.secret.url" autocomplete="off" :placeholder="channel.secretSet ? `已保存（${channel.config.urlHint}），留空保持不变` : typeInfo.urlHint" /></ui-form-item>
          <ui-form-item v-if="typeInfo.sign" label="签名密钥"><ui-input v-model="channel.secret.signSecret" type="password" show-password autocomplete="new-password" :placeholder="channel.secretSet ? '留空保持不变' : '机器人开启“加签”时填写'" /></ui-form-item>
        </template>
        <div class="notify-switch"><span>启用</span><ui-switch v-model="channel.enabled" /></div>
      </ui-form>
      <template #footer><ui-button :disabled="saving" @click="dialog = ''">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存</ui-button></template>
    </ui-dialog>

    <ui-dialog :model-value="dialog === 'policy'" :title="policy.version ? '编辑通知策略' : '新增通知策略'" width="min(820px,96vw)" :close-on-click-modal="!saving" @update:model-value="v => { if (!v && !saving) dialog = '' }">
      <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="notify-gap" />
      <ui-form label-position="top" :disabled="saving">
        <div class="notify-grid">
          <ui-form-item label="策略名称" required><ui-input v-model="policy.name" maxlength="64" placeholder="例如：火警逐级通知" /></ui-form-item>
          <ui-form-item label="告警等级（不选表示全部）"><ui-select v-model="policy.levels" multiple clearable><ui-option v-for="(name, value) in alarmLevels" :key="value" :value="value" :label="name" /></ui-select></ui-form-item>
          <ui-form-item label="告警类型（不选表示全部）"><ui-select v-model="policy.alarmTypes" multiple clearable filterable allow-create><ui-option v-for="(name, value) in alarmTypes" :key="value" :value="value" :label="name" /></ui-select></ui-form-item>
          <div class="notify-switches"><div class="notify-switch"><span>启用策略</span><ui-switch v-model="policy.enabled" /></div><div class="notify-switch"><span>告警恢复时通知第 1 级</span><ui-switch v-model="policy.notifyRecovery" /></div></div>
        </div>
        <section v-for="(stage, index) in policy.stages" :key="index" class="notify-stage">
          <header><strong>第 {{ index + 1 }} 级</strong>
            <span v-if="index === 0">告警首次上报后立即发送</span>
            <span v-else class="notify-delay">未确认 <ui-input-number v-model="stage.delaySeconds" :min="60" :max="86400" :step="60" size="small" /> 秒后发送</span>
            <ui-button v-if="index > 0" text size="small" type="danger" @click="policy.stages.splice(index, 1)"><Trash2 />删除</ui-button>
          </header>
          <div class="notify-grid">
            <ui-form-item label="通知渠道" required><ui-select v-model="stage.channelIds" multiple><ui-option v-for="item in channels" :key="item.id" :value="item.id" :label="`${item.name}（${typeLabel(item.type)}）`" /></ui-select></ui-form-item>
            <ui-form-item label="平台用户（须能查看该设备告警）"><ui-select v-model="stage.users" multiple filterable clearable><ui-option v-for="item in options.users" :key="item.id" :value="item.id" :label="item.contact ? item.name : `${item.name}（未填写联系方式）`" /></ui-select></ui-form-item>
            <ui-form-item label="角色"><ui-select v-model="stage.roles" multiple clearable><ui-option v-for="item in options.roles" :key="item.id" :value="item.id" :label="item.name" /></ui-select></ui-form-item>
            <ui-form-item label="当班人员（来自排班）"><div class="notify-duty"><ui-switch v-model="stage.onDuty" /><ui-select v-if="stage.onDuty" v-model="stage.stationIds" multiple clearable placeholder="全部消防站"><ui-option v-for="item in options.stations" :key="item.id" :value="item.id" :label="item.name" /></ui-select></div></ui-form-item>
            <ui-form-item label="其他邮箱（逗号分隔）"><ui-input :model-value="Array.isArray(stage.emails) ? stage.emails.join(', ') : stage.emails" @update:model-value="v => stage.emails = v" /></ui-form-item>
            <ui-form-item label="其他手机号（机器人 @ 提醒）"><ui-input :model-value="Array.isArray(stage.mobiles) ? stage.mobiles.join(', ') : stage.mobiles" @update:model-value="v => stage.mobiles = v" /></ui-form-item>
          </div>
        </section>
        <ui-button v-if="policy.stages.length < 8" @click="addStage"><Plus />添加升级级别</ui-button>
      </ui-form>
      <template #footer><ui-button :disabled="saving" @click="dialog = ''">取消</ui-button><ui-button type="primary" :loading="saving" @click="save">保存</ui-button></template>
    </ui-dialog>

    <ui-dialog :model-value="dialog === 'test'" title="发送测试消息" width="min(520px,94vw)" @update:model-value="v => { if (!v && !saving) dialog = '' }">
      <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" class="notify-gap" />
      <ui-form label-position="top" :disabled="saving">
        <ui-form-item label="测试邮箱（邮件渠道必填）"><ui-input v-model="testTarget.emails" placeholder="多个以逗号分隔" /></ui-form-item>
        <ui-form-item label="提醒手机号（可选）"><ui-input v-model="testTarget.mobiles" placeholder="机器人 @ 提醒" /></ui-form-item>
      </ui-form>
      <template #footer><ui-button :disabled="saving" @click="dialog = ''">取消</ui-button><ui-button type="primary" :loading="saving" @click="sendTest"><Send />发送</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.notify-page { display: grid; gap: var(--space-4); min-width: 0; }
.notify-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.notify-actions { display: flex; gap: var(--space-2); }
.notify-hint, .notify-subline { color: var(--text-muted); font-size: var(--font-size-xs); }
.notify-subline { display: block; }
.notify-stages { margin: 0; padding-left: 18px; }
.notify-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 0 var(--space-4); }
.notify-stage { display: grid; gap: var(--space-2); margin-bottom: var(--space-3); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.notify-stage header { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.notify-stage header span { color: var(--text-secondary); font-size: var(--font-size-sm); }
.notify-delay { display: inline-flex; align-items: center; gap: var(--space-2); }
.notify-switch { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); min-height: 32px; }
.notify-switches { display: grid; gap: var(--space-2); align-content: end; padding-bottom: var(--space-3); }
.notify-duty { display: flex; align-items: center; gap: var(--space-2); width: 100%; }
.notify-gap { margin-bottom: var(--space-3); }
</style>
