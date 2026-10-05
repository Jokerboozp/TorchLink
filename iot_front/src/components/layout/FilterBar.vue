<script setup>
import { ref } from 'vue'
import { SlidersHorizontal } from '@lucide/vue'
import { useMediaQuery } from '../../composables/useMediaQuery'

// 窄屏时筛选条件默认收起，点击“筛选”展开，操作按钮始终可见。
const narrow = useMediaQuery('(max-width: 767px)')
const open = ref(false)
</script>

<template>
  <div class="filter-bar">
    <ui-button v-if="narrow" class="filter-bar__toggle" :aria-expanded="open ? 'true' : 'false'" @click="open = !open"
      ><SlidersHorizontal />{{ open ? '收起筛选' : '筛选' }}</ui-button
    >
    <div v-show="!narrow || open" class="filter-bar__filters"><slot /></div>
    <div v-if="$slots.actions" class="filter-bar__actions"><slot name="actions" /></div>
  </div>
</template>

<style scoped>
.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.filter-bar__filters {
  display: flex;
  flex: 1 1 auto;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}
.filter-bar__actions {
  display: flex;
  flex: none;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.filter-bar__filters :deep(.ui-select),
.filter-bar__filters :deep(.ui-input) {
  width: 200px;
}
@media (max-width: 767px) {
  .filter-bar {
    align-items: stretch;
    flex-direction: column;
  }
  .filter-bar__filters :deep(.ui-select),
  .filter-bar__filters :deep(.ui-input) {
    flex: 1 1 160px;
    width: auto;
  }
  .filter-bar__actions > * {
    flex: 1 1 auto;
  }
}
</style>
