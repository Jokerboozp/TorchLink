<script setup>
import { computed, ref } from 'vue'
import { trendGeometry } from '../dashboard.js'
const props = defineProps({ items:{type:Array, default:() => []} })
const chart = computed(() => trendGeometry(props.items))
const selected = ref(null)
const total = computed(() => props.items.reduce((sum,item) => sum+item.count,0))
const selectedPoint = computed(() => props.items.find(item => item.date === selected.value))
const labelIndexes = computed(() => new Set([0, Math.floor((props.items.length-1)/2), props.items.length-1]))
</script>
<template>
  <div class="trend-caption"><strong>{{ total.toLocaleString() }} <small>条新增告警</small></strong><span aria-live="polite">{{ selectedPoint ? `${selectedPoint.date} · ${selectedPoint.count} 条` : '按首次发生时间统计' }}</span></div>
  <div v-if="total" class="trend-plot">
    <svg viewBox="0 0 700 245" role="group" aria-label="每日新增告警趋势">
      <defs><linearGradient id="dashboard-trend-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#0071e3" stop-opacity=".18" /><stop offset="100%" stop-color="#0071e3" stop-opacity=".01" /></linearGradient></defs>
      <g v-for="tick in chart.ticks" :key="tick.value"><line x1="44" x2="672" :y1="tick.y" :y2="tick.y" stroke="var(--border)" stroke-dasharray="3 5" /><text x="30" :y="tick.y+4" text-anchor="end">{{ tick.value }}</text></g>
      <polygon :points="chart.area" fill="url(#dashboard-trend-fill)" />
      <polyline :points="chart.line" fill="none" stroke="#0071e3" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" />
      <g v-for="(point,index) in chart.points" :key="point.date">
        <circle :cx="point.x" :cy="point.y" :r="selected === point.date ? 5 : 3" fill="white" stroke="#0071e3" stroke-width="2" />
        <circle :cx="point.x" :cy="point.y" r="10" fill="transparent" tabindex="0" role="img" :aria-label="`${point.date}：${point.count} 条新增告警`" @mouseenter="selected=point.date" @mouseleave="selected=null" @focus="selected=point.date" @blur="selected=null"><title>{{ point.date }}：{{ point.count }} 条</title></circle>
        <text v-if="labelIndexes.has(index)" :x="point.x" y="233" :text-anchor="index === 0 ? 'start' : index === items.length-1 ? 'end' : 'middle'">{{ point.date.slice(5).replace('-', '/') }}</text>
      </g>
    </svg>
  </div>
  <el-empty v-else description="所选时段暂无新增告警" :image-size="76" />
</template>
