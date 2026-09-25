<script setup>
// 规则组编辑：保存后由组件（Prometheus 自动重载 / Loki ruler 轮询）加载，平台确认生效或恢复原规则。
import { computed, ref, watch } from 'vue'
import { CheckCircle2, Plus, Trash2 } from '@lucide/vue'
import { UiMessage } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import KeyValueEditor from './KeyValueEditor.vue'

const props = defineProps({ modelValue: Boolean, source: { type: String, required: true }, group: { type: Object, default: null } })
const emit = defineEmits(['update:modelValue', 'saved'])
const form = ref(blank())
const saving = ref(false)
const failure = ref(null)
const checks = ref({})
const isLoki = computed(() => props.source === 'loki')
const base = computed(() => (isLoki.value ? '/api/v1/ops/logs' : '/api/v1/ops/metrics'))
const language = computed(() => (isLoki.value ? 'LogQL' : 'PromQL'))

function blank() {
  return { name: '', interval: '', enabled: true, revision: '', rules: [newRule()] }
}
function newRule(kind = 'alert') {
  return { kind, name: '', expr: '', for: kind === 'alert' ? '5m' : '', keepFiringFor: '', labels: kind === 'alert' ? { severity: 'warning' } : {}, annotations: kind === 'alert' ? { summary: '' } : {} }
}
watch(() => props.modelValue, open => {
  if (!open) return
  failure.value = null
  checks.value = {}
  form.value = props.group ? JSON.parse(JSON.stringify({ name: props.group.name, interval: props.group.interval || '', enabled: props.group.enabled, revision: props.group.revision, rules: props.group.rules.map(r => ({ kind: r.kind, name: r.name, expr: r.expr, for: r.for || '', keepFiringFor: r.keepFiringFor || '', labels: r.labels || {}, annotations: r.annotations || {} })) })) : blank()
})

async function validate(rule, index) {
  checks.value = { ...checks.value, [index]: { pending: true } }
  try {
    await opsGet(`${base.value}/validate`, { query: rule.expr })
    checks.value = { ...checks.value, [index]: { ok: true } }
  } catch (e) { checks.value = { ...checks.value, [index]: { error: opsErrorText(e) } } }
}

async function save() {
  saving.value = true
  failure.value = null
  const body = { ...form.value, rules: form.value.rules.map(r => ({ ...r, annotations: Object.fromEntries(Object.entries(r.annotations || {}).filter(([, v]) => v !== '')) })) }
  try {
    if (props.group) await opsSend('PUT', `${base.value}/rule-groups/${encodeURIComponent(props.group.name)}`, body)
    else await opsSend('POST', `${base.value}/rule-groups`, body)
    UiMessage.success(form.value.enabled ? '规则已生效' : '规则组已保存（停用状态）')
    emit('saved')
    emit('update:modelValue', false)
  } catch (e) {
    failure.value = { message: opsErrorText(e), reason: e.details?.reason || '', rolledBack: e.details?.rolledBack, field: e.details?.field }
  } finally { saving.value = false }
}
</script>

<template>
  <ui-drawer :model-value="modelValue" :title="group ? `编辑规则组：${group.name}` : '新建规则组'" size="min(820px, 96vw)" :close-on-click-modal="!saving" @update:model-value="value => !saving && emit('update:modelValue', value)">
    <div class="rule-editor">
      <ui-alert v-if="failure" type="error" :closable="false" show-icon :title="failure.message">
        <p v-if="failure.reason">{{ failure.reason }}</p>
        <p v-if="failure.rolledBack">原有规则已恢复，组件继续使用修改前的配置。</p>
      </ui-alert>
      <ui-alert v-if="saving" type="info" :closable="false" show-icon :title="`正在写入规则并等待 ${isLoki ? 'Loki ruler' : 'Prometheus'} 加载，通常需要 5～20 秒`" />
      <div class="rule-editor__grid">
        <label>规则组名称<ui-input v-model="form.name" placeholder="例如 平台可用性" maxlength="100" /></label>
        <label>评估间隔<ui-input v-model="form.interval" placeholder="默认使用全局间隔，例如 30s、1m" /></label>
        <label v-if="!group" class="rule-editor__switch">保存后立即启用<ui-switch v-model="form.enabled" /></label>
      </div>
      <section v-for="(rule, index) in form.rules" :key="index" class="rule-card">
        <header>
          <ui-radio-group v-model="rule.kind" size="small" class="segmented-choice-group" :aria-label="`第 ${index + 1} 条规则类型`">
            <ui-radio-button value="alert">告警规则</ui-radio-button>
            <ui-radio-button v-if="!isLoki" value="record">记录规则</ui-radio-button>
          </ui-radio-group>
          <ui-button text size="small" type="danger" :disabled="form.rules.length === 1" @click="form.rules.splice(index, 1)"><Trash2 />删除</ui-button>
        </header>
        <label>{{ rule.kind === 'alert' ? '告警名称（英文标识，如 HighErrorRate）' : '记录的指标名（如 job:up:sum）' }}<ui-input v-model="rule.name" /></label>
        <label>{{ language }} 表达式
          <ui-input v-model="rule.expr" type="textarea" :autosize="{ minRows: 2, maxRows: 8 }" class="mono-input" :placeholder="isLoki ? 'sum(count_over_time({service_name=&quot;platform-api&quot;, level=&quot;error&quot;}[5m])) > 10' : 'sum(rate(parse_failed_total[5m])) > 1'" />
        </label>
        <div class="rule-card__check">
          <ui-button size="small" :loading="checks[index]?.pending" :disabled="!rule.expr" @click="validate(rule, index)">校验表达式</ui-button>
          <span v-if="checks[index]?.ok" class="check-ok"><CheckCircle2 />语法正确</span>
          <span v-else-if="checks[index]?.error" class="check-error">{{ checks[index].error }}</span>
        </div>
        <template v-if="rule.kind === 'alert'">
          <div class="rule-editor__grid">
            <label>持续时间（满足条件多久后触发）<ui-input v-model="rule.for" placeholder="例如 5m" /></label>
            <label>保持触发（恢复后仍保持）<ui-input v-model="rule.keepFiringFor" placeholder="可留空，例如 2m" /></label>
          </div>
          <div class="rule-editor__block"><span>标签（用于分组、路由，如 severity）</span><KeyValueEditor v-model="rule.labels" key-placeholder="标签名" value-placeholder="标签值" add-text="添加标签" /></div>
          <div class="rule-editor__block"><span>注释（summary 为通知摘要，支持 {{ '{' + '{ $labels.job }' + '}' }} 模板）</span><KeyValueEditor v-model="rule.annotations" multiline key-placeholder="注释名" value-placeholder="内容" add-text="添加注释" /></div>
        </template>
        <div v-else class="rule-editor__block"><span>附加标签</span><KeyValueEditor v-model="rule.labels" key-placeholder="标签名" value-placeholder="标签值" add-text="添加标签" /></div>
      </section>
      <ui-button @click="form.rules.push(newRule(isLoki ? 'alert' : 'alert'))"><Plus />添加规则</ui-button>
    </div>
    <template #footer>
      <ui-button :disabled="saving" @click="emit('update:modelValue', false)">取消</ui-button>
      <ui-button type="primary" :loading="saving" @click="save">保存并加载</ui-button>
    </template>
  </ui-drawer>
</template>

<style scoped>
.rule-editor { display: grid; gap: var(--space-4); }
.rule-editor label { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.rule-editor__grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: var(--space-3); }
.rule-editor__switch { align-content: start; }
.rule-editor__block { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.rule-card { display: grid; gap: var(--space-3); padding: var(--space-4); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.rule-card header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); }
.rule-card__check { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); font-size: var(--font-size-xs); }
.check-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--success-text); }
.check-ok svg { width: 14px; height: 14px; }
.check-error { color: var(--danger-text); overflow-wrap: anywhere; }
.mono-input :deep(textarea) { font-family: var(--font-mono); font-size: var(--font-size-sm); }
</style>
