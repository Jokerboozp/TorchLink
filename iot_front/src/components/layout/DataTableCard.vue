<script setup>
defineProps({
  title: { type: String, default: '' },
  total: { type: Number, default: 0 },
  page: { type: Number, default: null },
  pageSize: { type: Number, default: 20 },
  pageSizes: { type: Array, default: () => [20, 50, 100] },
  error: { type: String, default: '' }
})
const emit = defineEmits(['update:page', 'update:pageSize', 'retry'])
</script>

<template>
  <section class="data-table-card">
    <header v-if="title || $slots.header" class="data-table-card__header">
      <slot name="header"><h2>{{ title }}</h2></slot>
    </header>
    <div v-if="error" class="data-table-card__error" role="alert">
      <span>{{ error }}</span>
      <ui-button size="small" @click="emit('retry')">重新加载</ui-button>
    </div>
    <slot />
    <footer v-if="page != null && total > 0" class="data-table-card__footer">
      <span>共 {{ total }} 条</span>
      <ui-pagination
        :current-page="page"
        :page-size="pageSize"
        :total="total"
        :page-sizes="pageSizes"
        layout="sizes, prev, pager, next"
        @update:current-page="value => emit('update:page', value)"
        @update:page-size="value => emit('update:pageSize', value)"
      />
    </footer>
  </section>
</template>

<style scoped>
.data-table-card { min-width: 0; overflow: hidden; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); box-shadow: var(--shadow-xs); }
.data-table-card__header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border); }
.data-table-card__header h2 { margin: 0; color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.data-table-card__error { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-3) var(--space-4); color: var(--danger-text); background: var(--danger-soft); border-bottom: 1px solid var(--danger-border); font-size: var(--font-size-sm); }
.data-table-card__footer { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); padding: var(--space-3) var(--space-4); border-top: 1px solid var(--border); }
.data-table-card__footer > span { color: var(--text-muted); font-size: var(--font-size-sm); }
.data-table-card :deep(.n-data-table .n-data-table-th) { white-space: nowrap; }
@media (max-width: 767px) {
  .data-table-card__footer { justify-content: center; }
  .data-table-card__footer > span { display: none; }
}
</style>
