<script setup>
import { computed, ref } from 'vue'
import { ringSegments } from '../dashboard.js'

const props = defineProps({
  items:{type:Array, default:() => []},
  title:{type:String, required:true},
  unit:{type:String, default:'条'},
  variant:{type:String, default:'ring'},
  available:Boolean,
  loading:Boolean,
})
const selected = ref('')
const segments = computed(() => ringSegments(props.items))
const total = computed(() => props.items.reduce((sum,item) => sum+item.count,0))
const maximum = computed(() => Math.max(1,...props.items.map(item => item.count)))
const focusItem = computed(() => segments.value.find(item => item.key === selected.value))
const summary = computed(() => `${props.title}：${segments.value.map(item => `${item.name} ${item.count} ${props.unit}`).join('，')}`)
</script>

<template>
  <div class="distribution-chart">
    <ui-skeleton v-if="!available && loading" :rows="4" animated />
    <ui-empty v-else-if="!available || !total" :description="!available ? '尚未获取统计数据' : unit === '台' ? '暂无已登记设备' : '所选时段暂无新增告警'" :image-size="65" />
    <template v-else>
      <div v-if="variant === 'ring'" class="ring-layout">
        <div class="summary-ring">
          <svg viewBox="0 0 160 160" role="img" :aria-label="summary">
            <circle cx="80" cy="80" r="64" fill="none" stroke="var(--surface-hover)" stroke-width="14" />
            <circle v-for="item in segments.filter(item => item.count)" :key="item.key" cx="80" cy="80" r="64" fill="none" :stroke="item.color" stroke-width="14" pathLength="100" :stroke-dasharray="`${item.percent} ${100-item.percent}`" :stroke-dashoffset="-item.offset" transform="rotate(-90 80 80)" :opacity="selected && selected !== item.key ? .3 : 1"><title>{{ item.name }}：{{ item.count }} {{ unit }}</title></circle>
          </svg>
          <div class="ring-center" aria-live="polite"><strong>{{ (focusItem?.count ?? total).toLocaleString() }}</strong><small>{{ focusItem?.name || `合计 / ${unit}` }}</small></div>
        </div>
        <div class="distribution-legend">
          <div v-for="item in segments" :key="item.key" class="legend-row" tabindex="0" @mouseenter="selected=item.key" @mouseleave="selected=''" @focus="selected=item.key" @blur="selected=''">
            <i :style="{background:item.color}" /><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }}</b><small>{{ item.percent.toFixed(1) }}%</small>
          </div>
        </div>
      </div>
      <template v-else-if="variant === 'stack'">
        <div class="stack-total"><strong>{{ total.toLocaleString() }}</strong><span>{{ unit }}已登记设备</span></div>
        <div class="stack-chart" role="img" :aria-label="summary"><i v-for="item in segments.filter(item => item.count)" :key="item.key" :style="{width:`${item.percent}%`,background:item.color}" :title="`${item.name}：${item.count} ${unit}（${item.percent.toFixed(1)}%）`" /></div>
        <div class="distribution-legend"><div v-for="item in segments" :key="item.key" class="legend-row"><i :style="{background:item.color}" /><span>{{ item.name }}</span><b>{{ item.count.toLocaleString() }}</b><small>{{ item.percent.toFixed(1) }}%</small></div></div>
      </template>
      <div v-else class="ranking-chart" role="img" :aria-label="summary">
        <div v-for="(item,index) in segments" :key="item.key" class="ranking-row">
          <div><span class="rank">{{ index+1 }}</span><span class="rank-name" :title="item.name">{{ item.name }}</span><b>{{ item.count.toLocaleString() }} <small>{{ unit }}</small></b><small>{{ item.percent.toFixed(1) }}%</small></div>
          <div class="ranking-track"><i :style="{width:`${item.count/maximum*100}%`,background:item.color}" /></div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.distribution-chart { container-type:inline-size; min-height:240px; display:flex; flex-direction:column; justify-content:center; gap:18px; }
.ring-layout { display:flex; align-items:center; gap:16px; }
.summary-ring { position:relative; width:112px; flex:0 0 112px; }
.summary-ring svg { display:block; width:100%; }
.summary-ring circle { transition:opacity .15s; }
.ring-center { position:absolute; inset:0; display:flex; flex-direction:column; justify-content:center; align-items:center; pointer-events:none; }
.ring-center strong,.stack-total strong { font-size:28px; line-height:1.2; color:var(--text-strong); font-weight:600; font-variant-numeric:tabular-nums; }
.ring-center small { margin-top:6px; color:var(--text-muted); font-size:12px; }
.distribution-legend { display:grid; align-content:center; gap:12px; flex:1; min-width:0; }
.legend-row { display:grid; grid-template-columns:8px minmax(0,1fr) auto 44px; align-items:center; gap:7px; font-size:12px; border-radius:4px; }
.legend-row:focus-visible { outline:2px solid var(--primary); outline-offset:4px; }
.legend-row i { height:8px; width:8px; border-radius:50%; }
.legend-row span { color:var(--text-secondary); overflow-wrap:anywhere; }
.legend-row b,.ranking-row b { font-weight:500; color:var(--text-strong); font-variant-numeric:tabular-nums; }
.legend-row small { text-align:right; }
.legend-row small,.ranking-row small,.stack-total span { color:var(--text-muted); font-size:12px; }
.stack-total { display:flex; align-items:baseline; gap:10px; }
.stack-chart { display:flex; height:24px; overflow:hidden; border-radius:5px; background:var(--surface-hover); gap:2px; }
.stack-chart i { min-width:0; }
.ranking-chart { display:grid; gap:15px; }
.ranking-row > div:first-child { display:flex; align-items:center; gap:9px; margin-bottom:7px; font-size:13px; }
.rank { width:18px; color:var(--text-muted); font-size:11px; }
.rank-name { flex:1; min-width:0; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; color:var(--text-secondary); }
.ranking-row b { white-space:nowrap; }
.ranking-row > div > small { width:45px; text-align:right; }
.ranking-track { height:7px; background:var(--surface-hover); border-radius:5px; margin-left:27px; overflow:hidden; }
.ranking-track i { display:block; height:100%; border-radius:inherit; }
@container (min-width:400px) {
  .summary-ring { width:136px; flex-basis:136px; }
}
@container (max-width:280px) {
  .ring-layout { flex-direction:column; gap:14px; }
  .summary-ring { width:112px; flex-basis:112px; }
  .distribution-legend { width:100%; }
  .legend-row { font-size:12px; }
}
@media (max-width:767px) { .distribution-chart { min-height:180px; } }
</style>
