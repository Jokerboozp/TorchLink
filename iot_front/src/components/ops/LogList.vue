<script setup>
// 日志行列表：级别着色、展开查看字段与标签、复制、查看相邻上下文。
import { ref } from 'vue'
import { ChevronRight, Copy, ListTree } from '@lucide/vue'
import { UiMessage } from '../../ui/feedback.js'
import { entryLevel, logLevelName, logLevelTone, nsToLocal, parseLogFields } from '../../ops/format.js'
import { copyText } from '../../ops/opsApi.js'

const props = defineProps({ entries: { type: Array, default: () => [] }, wrap: { type: Boolean, default: true }, showService: { type: Boolean, default: true }, contextEnabled: { type: Boolean, default: true }, highlight: { type: String, default: '' }, maxHeight: { type: String, default: '' } })
const emit = defineEmits(['context'])
const expanded = ref(new Set())

const key = entry => `${entry.ts}\u0000${entry.line}\u0000${JSON.stringify(entry.labels)}`
function toggle(entry) {
  const next = new Set(expanded.value)
  const k = key(entry)
  next.has(k) ? next.delete(k) : next.add(k)
  expanded.value = next
}
async function copy(text) {
  if (await copyText(text)) UiMessage.success('已复制')
  else UiMessage.error('复制失败，请手动选择文本')
}
function parts(line) {
  if (!props.highlight) return [{ text: line }]
  const needle = props.highlight.toLowerCase()
  const out = []
  let rest = line
  let index = rest.toLowerCase().indexOf(needle)
  while (index >= 0 && needle) {
    out.push({ text: rest.slice(0, index) }, { text: rest.slice(index, index + needle.length), mark: true })
    rest = rest.slice(index + needle.length)
    index = rest.toLowerCase().indexOf(needle)
  }
  out.push({ text: rest })
  return out
}
</script>

<template>
  <div class="log-list" :class="{ 'is-nowrap': !wrap, 'no-service': !showService }" :style="maxHeight ? { maxHeight } : undefined" role="list">
    <article v-for="entry in entries" :key="key(entry)" class="log-row" :class="[`log-row--${logLevelTone[entryLevel(entry)]}`, { 'is-current': entry.current }]" role="listitem">
      <button type="button" class="log-row__main" :aria-expanded="expanded.has(key(entry))" @click="toggle(entry)">
        <ChevronRight class="log-row__chevron" :class="{ 'is-open': expanded.has(key(entry)) }" />
        <time>{{ nsToLocal(entry.ts) }}</time>
        <span class="log-row__level">{{ logLevelName[entryLevel(entry)] }}</span>
        <span v-if="showService" class="log-row__service" :title="entry.labels?.service_name">{{ entry.labels?.service_name || '—' }}</span>
        <span class="log-row__line"><template v-for="(part, i) in parts(entry.line)" :key="i"><mark v-if="part.mark">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template></span>
      </button>
      <div v-if="expanded.has(key(entry))" class="log-detail">
        <div class="log-detail__actions">
          <ui-button size="small" @click="copy(entry.line)"><Copy />复制原文</ui-button>
          <ui-button v-if="contextEnabled" size="small" @click="emit('context', entry)"><ListTree />查看上下文</ui-button>
        </div>
        <dl v-if="Object.keys(parseLogFields(entry.line)).length" class="log-fields">
          <template v-for="(value, name) in parseLogFields(entry.line)" :key="name"><dt>{{ name }}</dt><dd>{{ value }}</dd></template>
        </dl>
        <dl class="log-fields log-fields--labels">
          <template v-for="(value, name) in entry.labels" :key="`l-${name}`"><dt>{{ name }}<small>标签</small></dt><dd>{{ value }}</dd></template>
          <template v-for="(value, name) in entry.metadata || {}" :key="`m-${name}`"><dt>{{ name }}<small>元数据</small></dt><dd>{{ value }}</dd></template>
        </dl>
        <pre class="log-raw">{{ entry.line }}</pre>
      </div>
    </article>
  </div>
</template>

<style scoped>
.log-list { display: grid; min-width: 0; overflow: auto; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-md); font: var(--font-size-xs) var(--font-mono); }
.log-row { border-bottom: 1px solid var(--border); border-left: 3px solid transparent; }
.log-row--danger { border-left-color: var(--danger); }
.log-row--warning { border-left-color: var(--warning); }
.log-row--info { border-left-color: var(--info-border); }
.log-row__main { display: grid; grid-template-columns: 14px 170px 38px minmax(80px, 140px) minmax(0, 1fr); align-items: start; gap: var(--space-2); width: 100%; padding: 6px var(--space-2); color: var(--text); font: inherit; text-align: left; background: none; border: 0; cursor: pointer; }
.no-service .log-row__main { grid-template-columns: 14px 170px 38px minmax(0, 1fr); }
.log-row__main:hover { background: var(--surface-hover); }
.log-row.is-current > .log-row__main { background: var(--primary-soft); }
.log-row__chevron { width: 14px; height: 14px; margin-top: 1px; color: var(--text-muted); transition: transform 0.15s; }
.log-row__chevron.is-open { transform: rotate(90deg); }
.log-row time, .log-row__service { color: var(--text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.log-row__level { font-family: var(--font-sans); }
.log-row--danger .log-row__level { color: var(--danger-text); }
.log-row--warning .log-row__level { color: var(--warning-text); }
.log-row__line { min-width: 0; white-space: pre-wrap; word-break: break-all; }
.is-nowrap .log-row__line { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.log-row mark { color: inherit; background: var(--warning-soft); border-radius: 2px; }
.log-detail { display: grid; gap: var(--space-2); padding: var(--space-2) var(--space-3) var(--space-3) 30px; background: var(--surface-muted); }
.log-detail__actions { display: flex; gap: var(--space-2); font-family: var(--font-sans); }
.log-fields { display: grid; grid-template-columns: minmax(100px, max-content) minmax(0, 1fr); gap: 2px var(--space-3); margin: 0; }
.log-fields dt { color: var(--text-secondary); }
.log-fields dt small { margin-left: 4px; color: var(--text-muted); font-family: var(--font-sans); }
.log-fields dd { margin: 0; word-break: break-all; }
.log-raw { max-height: 240px; margin: 0; padding: var(--space-2); overflow: auto; color: var(--code-text); background: var(--code-bg); border-radius: var(--radius-sm); white-space: pre-wrap; word-break: break-all; }
@media (max-width: 767px) {
  .log-row__main, .no-service .log-row__main { grid-template-columns: 14px minmax(0, 1fr); }
  .log-row__main time { grid-column: 2; }
  .log-row__level, .log-row__service { display: none; }
  .log-row__line { grid-column: 1 / -1; }
}
</style>
