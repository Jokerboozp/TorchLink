<script setup>
defineProps({
  tone: { type: String, default: 'neutral' },
  label: { type: String, default: '' }
})
</script>

<template>
  <span class="status-dot" :class="`status-dot--${tone}`"><i aria-hidden="true" /><slot>{{ label }}</slot></span>
</template>

<style scoped>
.status-dot { display: inline-flex; align-items: center; gap: 6px; color: var(--text); white-space: nowrap; }
.status-dot i { width: 7px; height: 7px; flex: none; background: var(--neutral-dot); border-radius: 50%; }
.status-dot--success i { background: var(--success); }
.status-dot--warning i { background: var(--warning); }
.status-dot--danger i { background: var(--danger); }
.status-dot--danger { color: var(--danger-text); }
.status-dot--info i { background: var(--info); }
.status-dot--neutral { color: var(--text-secondary); }
</style>
