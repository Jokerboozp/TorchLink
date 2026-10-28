<script setup>
// 健康信号：按最近一天的上报数据计算的异常迹象，供巡检与研判参考。
import { formatTime } from '../../api'

defineProps({ signals: { type: Array, default: () => [] } })
const signalNames = {
  STUCK_VALUE: '数值长时间不变',
  REPORT_DRIFT: '上报周期偏离',
  OUT_OF_RANGE: '数值超出有效范围',
  PEER_OUTLIER: '与同型号设备差异显著'
}
</script>

<template>
  <section class="connection-section device-signals">
    <h3>健康信号 <small>按最近一天的上报数据计算，供巡检与研判参考</small></h3>
    <ul class="signal-list">
      <li v-for="item in signals" :key="`${item.signalType}:${item.property}`">
        <ui-tag size="small" :type="item.strength >= 0.5 ? 'warning' : 'info'">{{ signalNames[item.signalType] || item.signalType }}</ui-tag
        ><span>{{ item.property || '整机' }}</span
        ><small>强度 {{ Math.round(item.strength * 100) }}% · {{ formatTime(item.windowEnd) }}</small>
      </li>
    </ul>
  </section>
</template>

<style scoped src="./connection-section.css"></style>
<style scoped>
.signal-list {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.signal-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.signal-list small {
  color: var(--text-muted);
}
</style>
