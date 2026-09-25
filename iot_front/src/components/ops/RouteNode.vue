<script setup>
// 通知路由节点（可递归）：匹配条件、接收人、分组与发送间隔、是否继续匹配后续路由。
import { ArrowDown, ArrowUp, Plus, Trash2 } from '@lucide/vue'
import MatcherEditor from './MatcherEditor.vue'

defineOptions({ name: 'RouteNode' })
const props = defineProps({ route: { type: Object, required: true }, receivers: { type: Array, default: () => [] }, labels: { type: Array, default: () => [] }, top: { type: Boolean, default: false }, depth: { type: Number, default: 0 }, disabled: { type: Boolean, default: false } })
const emit = defineEmits(['remove', 'move'])
function addChild() {
  props.route.routes = props.route.routes || []
  props.route.routes.push({ receiver: '', matchers: [{ name: 'severity', op: '=', value: 'critical' }], groupBy: [], continue: false, routes: [] })
}
function move(index, delta) {
  const list = props.route.routes
  const target = index + delta
  if (target < 0 || target >= list.length) return
  ;[list[index], list[target]] = [list[target], list[index]]
}
</script>

<template>
  <div class="route" :class="{ 'is-top': top }">
    <div class="route__head">
      <strong>{{ top ? '默认路由（所有告警从这里开始匹配）' : '子路由' }}</strong>
      <span v-if="!top && !disabled" class="route__actions">
        <ui-button text size="small" aria-label="上移" @click="emit('move', -1)"><ArrowUp /></ui-button>
        <ui-button text size="small" aria-label="下移" @click="emit('move', 1)"><ArrowDown /></ui-button>
        <ui-button text size="small" type="danger" aria-label="删除路由" @click="emit('remove')"><Trash2 /></ui-button>
      </span>
    </div>
    <div v-if="!top" class="route__field"><span>匹配条件</span><MatcherEditor v-model="route.matchers" :labels="labels" add-text="添加条件" :max="10" /></div>
    <div class="route__grid">
      <label>接收人<ui-select v-model="route.receiver" :clearable="!top" :disabled="disabled" :placeholder="top ? '选择默认接收人' : '沿用上级接收人'" aria-label="接收人"><ui-option v-for="name in receivers" :key="name" :value="name" :label="name" /></ui-select></label>
      <label>分组标签<ui-select v-model="route.groupBy" multiple filterable allow-create :disabled="disabled" :placeholder="top ? '例如 alertname、job' : '沿用上级'" aria-label="分组标签"><ui-option v-for="label in ['alertname', 'job', 'instance', 'severity', 'service_name', '...']" :key="label" :value="label" :label="label === '...' ? '...（不分组）' : label" /></ui-select></label>
      <label>首次等待<ui-input v-model="route.groupWait" :disabled="disabled" :placeholder="top ? '30s' : '沿用上级'" /></label>
      <label>组内间隔<ui-input v-model="route.groupInterval" :disabled="disabled" :placeholder="top ? '5m' : '沿用上级'" /></label>
      <label>重复间隔<ui-input v-model="route.repeatInterval" :disabled="disabled" :placeholder="top ? '4h' : '沿用上级'" /></label>
      <label v-if="!top" class="route__check"><ui-checkbox v-model="route.continue" :disabled="disabled">匹配后继续尝试后续路由</ui-checkbox></label>
    </div>
    <p v-if="route.muteTimeIntervals?.length" class="route__muted">静音时段：{{ route.muteTimeIntervals.join('、') }}（保留原配置，平台不编辑）</p>
    <div v-if="route.routes?.length" class="route__children">
      <RouteNode v-for="(child, index) in route.routes" :key="index" :route="child" :receivers="receivers" :labels="labels" :depth="depth + 1" :disabled="disabled" @remove="route.routes.splice(index, 1)" @move="delta => move(index, delta)" />
    </div>
    <ui-button v-if="!disabled && depth < 4" size="small" text type="primary" @click="addChild"><Plus />添加子路由</ui-button>
  </div>
</template>

<style scoped>
.route { display: grid; gap: var(--space-2); padding: var(--space-3); background: var(--surface); border: 1px solid var(--border); border-left: 3px solid var(--primary-border); border-radius: var(--radius-md); }
.route.is-top { border-left-color: var(--primary); }
.route__head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); font-size: var(--font-size-sm); }
.route__actions { display: inline-flex; }
.route__field, .route__grid label { display: grid; gap: 4px; color: var(--text-secondary); font-size: var(--font-size-xs); }
.route__grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: var(--space-2); }
.route__check { align-content: end; }
.route__muted { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.route__children { display: grid; gap: var(--space-2); padding-left: var(--space-3); }
</style>
