<script setup>
import { computed } from 'vue'
import { MoreHorizontal } from '@lucide/vue'
import { can } from '../../permissions'

// actions: [{ key, label, onClick, permission?, type?: 'danger', disabled?, hidden?, loading? }]
const props = defineProps({
  actions: { type: Array, default: () => [] },
  inline: { type: Number, default: 2 }
})
const visible = computed(() => props.actions.filter(action => !action.hidden && (!action.permission || can(action.permission))))
const shown = computed(() => visible.value.length <= props.inline + 1 ? visible.value : visible.value.slice(0, props.inline))
const folded = computed(() => visible.value.slice(shown.value.length))

function run(key) {
  const action = folded.value.find(item => item.key === key)
  if (action && !action.disabled) action.onClick?.()
}
</script>

<template>
  <div class="row-actions">
    <ui-button
      v-for="action in shown"
      :key="action.key"
      size="small"
      text
      :type="action.type === 'danger' ? 'danger' : 'primary'"
      :disabled="action.disabled"
      :loading="action.loading"
      @click="action.onClick?.()"
    >{{ action.label }}</ui-button>
    <ui-dropdown v-if="folded.length" @command="run">
      <ui-button size="small" text aria-label="更多操作" title="更多操作"><MoreHorizontal /></ui-button>
      <template #dropdown>
        <ui-dropdown-menu>
          <ui-dropdown-item v-for="action in folded" :key="action.key" :command="action.key" :disabled="action.disabled">
            <span :class="{ 'row-actions__danger': action.type === 'danger' }">{{ action.label }}</span>
          </ui-dropdown-item>
        </ui-dropdown-menu>
      </template>
    </ui-dropdown>
  </div>
</template>

<style scoped>
.row-actions { display: inline-flex; align-items: center; justify-content: flex-end; flex-wrap: nowrap; gap: var(--space-3); white-space: nowrap; }
.row-actions__danger { color: var(--danger-text); }
</style>
