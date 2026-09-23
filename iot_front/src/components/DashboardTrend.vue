<script setup>
import { computed, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { trendGeometry } from '../dashboard.js' /* 引入当前代码需要的依赖。 */
const props = defineProps({ items:{type:Array, default:() => []} }) /* 声明 props。 */
const chart = computed(() => trendGeometry(props.items)) /* 声明 chart。 */
const selected = ref(null) /* 声明 selected。 */
const total = computed(() => props.items.reduce((sum,item) => sum+item.count,0)) /* 声明 total。 */
const selectedPoint = computed(() => props.items.find(item => item.date === selected.value)) /* 声明 selectedPoint。 */
const labelIndexes = computed(() => new Set([0, Math.floor((props.items.length-1)/2), props.items.length-1])) /* 声明 labelIndexes。 */
</script>
<template>
  <div class="trend-caption"><strong>{{ total.toLocaleString() }} <small>条新增告警</small></strong><span aria-live="polite">{{ selectedPoint ? `${selectedPoint.date} · ${selectedPoint.count} 条` : '按首次发生时间统计' }}</span></div> <!-- 渲染 div 界面元素。 -->
  <div v-if="total" class="trend-plot"> <!-- 渲染 div 界面元素。 -->
    <svg viewBox="0 0 700 245" role="group" aria-label="每日新增告警趋势"> <!-- 渲染 svg 界面元素。 -->
      <defs><linearGradient id="dashboard-trend-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="var(--brand-navy)" stop-opacity=".18" /><stop offset="100%" stop-color="var(--brand-navy)" stop-opacity=".01" /></linearGradient></defs> <!-- 趋势面积使用品牌深蓝渐变。 -->
      <g v-for="tick in chart.ticks" :key="tick.value"><line x1="44" x2="672" :y1="tick.y" :y2="tick.y" stroke="var(--border)" stroke-dasharray="3 5" /><text x="30" :y="tick.y+4" text-anchor="end">{{ tick.value }}</text></g> <!-- 渲染 g 界面元素。 -->
      <polygon :points="chart.area" fill="url(#dashboard-trend-fill)" /> <!-- 渲染 polygon 界面元素。 -->
      <polyline :points="chart.line" fill="none" stroke="var(--brand-navy)" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" /> <!-- 趋势线使用品牌深蓝。 -->
      <g v-for="(point,index) in chart.points" :key="point.date"> <!-- 渲染 g 界面元素。 -->
        <circle :cx="point.x" :cy="point.y" :r="selected === point.date ? 5 : 3" fill="white" stroke="var(--brand-navy)" stroke-width="2" /> <!-- 趋势节点与主色一致。 -->
        <circle :cx="point.x" :cy="point.y" r="10" fill="transparent" tabindex="0" role="img" :aria-label="`${point.date}：${point.count} 条新增告警`" @mouseenter="selected=point.date" @mouseleave="selected=null" @focus="selected=point.date" @blur="selected=null"><title>{{ point.date }}：{{ point.count }} 条</title></circle> <!-- 渲染 circle 界面元素。 -->
        <text v-if="labelIndexes.has(index)" :x="point.x" y="233" :text-anchor="index === 0 ? 'start' : index === items.length-1 ? 'end' : 'middle'">{{ point.date.slice(5).replace('-', '/') }}</text> <!-- 渲染 text 界面元素。 -->
      </g> <!-- 结束当前界面区域。 -->
    </svg> <!-- 结束当前界面区域。 -->
  </div> <!-- 结束当前界面区域。 -->
  <ui-empty v-else description="所选时段暂无新增告警" :image-size="76" /> <!-- 渲染 ui-empty 界面元素。 -->
</template>
