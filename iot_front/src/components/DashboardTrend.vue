<script setup>
import { computed, ref } from 'vue'
import { compactCount, trendGeometry } from '../dashboard.js'
const props = defineProps({ items: { type: Array, default: () => [] } })
const chart = computed(() => trendGeometry(props.items))
const selected = ref(null)
const total = computed(() => props.items.reduce((sum, item) => sum + item.count, 0))
const selectedPoint = computed(() => props.items.find(item => item.date === selected.value))
const labelIndexes = computed(() => new Set([0, Math.floor((props.items.length - 1) / 2), props.items.length - 1]))
</script>
<template>
  <div class="trend-caption">
    <strong :title="`${total.toLocaleString()} 条新增告警`">{{ compactCount(total) }} <small>条新增告警</small></strong
    ><span aria-live="polite">{{
      selectedPoint ? `${selectedPoint.date}，${selectedPoint.count.toLocaleString()} 条` : '按首次发生时间统计'
    }}</span>
  </div>
  <div v-if="total" class="trend-plot">
    <svg viewBox="0 0 700 245" role="group" aria-label="每日新增告警趋势">
      <g v-for="tick in chart.ticks" :key="tick.value">
        <line :x1="chart.left" x2="672" :y1="tick.y" :y2="tick.y" stroke="var(--border)" stroke-width="0.5" />
        <text :x="chart.left - 14" :y="tick.y + 4" text-anchor="end">{{ tick.label }}</text>
      </g>
      <polygon :points="chart.area" fill="var(--primary-soft)" />
      <polyline :points="chart.line" fill="none" stroke="var(--primary)" stroke-width="0.5" stroke-linejoin="round" stroke-linecap="round" />
      <g v-for="(point, index) in chart.points" :key="point.date">
        <circle
          :cx="point.x"
          :cy="point.y"
          :r="selected === point.date ? 5 : 4"
          fill="var(--surface)"
          stroke="var(--primary)"
          stroke-width="0.5"
        />
        <!-- 趋势节点与主色一致。 -->
        <circle
          :cx="point.x"
          :cy="point.y"
          r="10"
          fill="transparent"
          tabindex="0"
          role="img"
          :aria-label="`${point.date}：${point.count} 条新增告警`"
          @mouseenter="selected = point.date"
          @mouseleave="selected = null"
          @focus="selected = point.date"
          @blur="selected = null"
        >
          <title>{{ point.date }}：{{ point.count }} 条</title>
        </circle>
        <text
          v-if="labelIndexes.has(index)"
          :x="point.x"
          y="233"
          :text-anchor="index === 0 ? 'start' : index === items.length - 1 ? 'end' : 'middle'"
        >
          {{ point.date.slice(5).replace('-', '/') }}
        </text>
      </g>
    </svg>
  </div>
  <ui-empty v-else description="所选时段暂无新增告警" :image-size="76" />
</template>

<style scoped>
.trend-caption {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.trend-caption > strong {
  white-space: nowrap;
  color: var(--text-strong);
  font-size: 18px;
  font-weight: var(--font-weight-semibold);
  font-variant-numeric: tabular-nums;
}
.trend-caption small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  font-weight: 400;
}
.trend-caption > span {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.trend-plot svg {
  display: block;
  width: 100%;
  min-height: 200px;
  overflow: visible;
}
.trend-plot text {
  fill: var(--text-muted);
  font: var(--font-size-xs) var(--font-sans);
}
.trend-plot circle[tabindex] {
  cursor: pointer;
}
.trend-plot circle[tabindex]:focus {
  outline: none;
  stroke: var(--primary);
  stroke-width: 0.5;
}
@media (max-width: 767px) {
  .trend-caption {
    flex-wrap: wrap;
    gap: 6px;
  }
  .trend-plot svg {
    min-height: 150px;
  }
  .trend-plot text {
    font-size: 18px;
  }
}
</style>
