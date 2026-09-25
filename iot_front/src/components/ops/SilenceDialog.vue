<script setup>
// 新建或编辑 Alertmanager 静默：匹配条件、开始与结束时间、原因，并实时显示当前会被静默的告警数。
import { computed, ref, watch } from 'vue'
import { UiMessage } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import MatcherEditor from './MatcherEditor.vue'

const props = defineProps({ modelValue: Boolean, silence: { type: Object, default: null }, labels: { type: Array, default: () => [] } })
const emit = defineEmits(['update:modelValue', 'saved'])
const matchers = ref([])
const startNow = ref(true)
const starts = ref(null)
const duration = ref(2 * 3600e3)
const ends = ref(null)
const comment = ref('')
const saving = ref(false)
const error = ref('')
const affected = ref(null)
let timer = null
const durations = [{ value: 3600e3, label: '1 小时' }, { value: 2 * 3600e3, label: '2 小时' }, { value: 4 * 3600e3, label: '4 小时' }, { value: 12 * 3600e3, label: '12 小时' }, { value: 86400e3, label: '1 天' }, { value: 3 * 86400e3, label: '3 天' }, { value: 7 * 86400e3, label: '7 天' }, { value: 0, label: '自定义结束时间' }]
const editing = computed(() => Boolean(props.silence?.id))
const toOp = m => (m.isEqual ? (m.isRegex ? '=~' : '=') : m.isRegex ? '!~' : '!=')

watch(() => props.modelValue, open => {
  if (!open) return
  const s = props.silence || {}
  matchers.value = (s.matchers || []).map(m => ('op' in m ? { ...m } : { name: m.name, op: toOp(m), value: m.value }))
  if (!matchers.value.length) matchers.value = [{ name: 'alertname', op: '=', value: '' }]
  const start = s.startsAt ? Date.parse(s.startsAt) : Date.now()
  startNow.value = !s.startsAt || start <= Date.now()
  starts.value = start
  if (s.endsAt && Date.parse(s.endsAt) > Date.now()) { duration.value = 0; ends.value = Date.parse(s.endsAt) } else { duration.value = 2 * 3600e3; ends.value = null }
  comment.value = s.comment || ''
  error.value = ''
  preview()
})
watch(matchers, () => { clearTimeout(timer); timer = setTimeout(preview, 400) }, { deep: true })

async function preview() {
  const valid = matchers.value.filter(m => m.name && m.value !== undefined)
  if (!valid.length) { affected.value = null; return }
  try { affected.value = ((await opsGet('/api/v1/ops/alerts', { matchers: valid, silenced: true, inhibited: true })).items || []).length } catch { affected.value = null }
}
async function save() {
  const startAt = startNow.value ? Date.now() : starts.value
  const endAt = duration.value ? startAt + duration.value : ends.value
  if (!endAt) { error.value = '请选择结束时间'; return }
  saving.value = true
  error.value = ''
  try {
    const body = { matchers: matchers.value.filter(m => m.name), startsAt: startNow.value ? '' : new Date(startAt).toISOString().replace(/\.\d{3}Z$/, 'Z'), endsAt: new Date(endAt).toISOString().replace(/\.\d{3}Z$/, 'Z'), comment: comment.value }
    if (editing.value) await opsSend('PUT', `/api/v1/ops/silences/${encodeURIComponent(props.silence.id)}`, body)
    else await opsSend('POST', '/api/v1/ops/silences', body)
    UiMessage.success(editing.value ? '静默已更新（Alertmanager 会生成新的静默编号）' : '静默已创建')
    emit('saved')
    emit('update:modelValue', false)
  } catch (e) { error.value = opsErrorText(e) } finally { saving.value = false }
}
</script>

<template>
  <ui-dialog :model-value="modelValue" :title="editing ? '编辑静默' : '新建静默'" width="min(640px, 96vw)" @update:model-value="value => emit('update:modelValue', value)">
    <div class="silence-form">
      <div class="field"><span>匹配条件</span><MatcherEditor v-model="matchers" :labels="labels" add-text="添加条件" :max="20" /><small>静默只影响通知，告警仍在 Alertmanager 与规则中可见。至少需要一个非空的“等于”条件。</small></div>
      <p class="affected" :class="{ 'is-zero': affected === 0 }">{{ affected == null ? '' : affected ? `当前有 ${affected} 条告警匹配这些条件` : '当前没有告警匹配这些条件（静默会作用于之后触发的告警）' }}</p>
      <div class="two">
        <label>开始<ui-select v-model="startNow" aria-label="开始时间"><ui-option :value="true" label="立即开始" /><ui-option :value="false" label="指定时间" /></ui-select></label>
        <label>时长<ui-select v-model="duration" aria-label="时长"><ui-option v-for="d in durations" :key="d.value" :value="d.value" :label="d.label" /></ui-select></label>
      </div>
      <div v-if="!startNow || !duration" class="two">
        <label v-if="!startNow">开始时间<ui-date-time v-model="starts" disable-past aria-label="开始时间" /></label>
        <label v-if="!duration">结束时间<ui-date-time v-model="ends" disable-past aria-label="结束时间" /></label>
      </div>
      <label>原因<ui-input v-model="comment" type="textarea" :rows="2" maxlength="500" placeholder="例如：计划内维护 backup-service" /></label>
      <ui-alert v-if="error" type="error" :title="error" :closable="false" />
    </div>
    <template #footer><ui-button @click="emit('update:modelValue', false)">取消</ui-button><ui-button type="primary" :loading="saving" :disabled="!comment.trim()" @click="save">{{ editing ? '更新静默' : '创建静默' }}</ui-button></template>
  </ui-dialog>
</template>

<style scoped>
.silence-form { display: grid; gap: var(--space-3); }
.silence-form label, .field { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.field small { color: var(--text-muted); font-size: var(--font-size-xs); }
.two { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); }
.affected { margin: 0; color: var(--warning-text); font-size: var(--font-size-xs); }
.affected.is-zero { color: var(--text-muted); }
@media (max-width: 767px) {
  .two { grid-template-columns: minmax(0, 1fr); }
}
</style>
