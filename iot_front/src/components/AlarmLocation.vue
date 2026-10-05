<script setup>
// 告警位置：显示单位、建筑、楼层与点位；有平面图坐标时在图上标出告警位置。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { MapPin } from '@lucide/vue'
import { apiBlob } from '../api'
import { errorMessage } from '../presentation'

const props = defineProps({ alarm: { type: Object, required: true } })
const location = computed(() => props.alarm.location || null)
const text = computed(() =>
  [location.value?.unitName, location.value?.buildingName, location.value?.floorName, location.value?.pointName].filter(Boolean).join(' · ')
)
const planUrl = ref(''),
  loading = ref(false),
  error = ref('')
let version = 0
function release() {
  if (planUrl.value) URL.revokeObjectURL(planUrl.value)
  planUrl.value = ''
}
async function loadPlan() {
  const current = ++version
  release()
  error.value = ''
  if (!location.value?.floorId || location.value.x == null) return
  loading.value = true
  try {
    const blob = await apiBlob(`/api/v1/alarms/${encodeURIComponent(props.alarm.alarmId)}/location-plan`)
    if (current === version) planUrl.value = URL.createObjectURL(blob)
  } catch (e) {
    if (current === version) error.value = errorMessage(e)
  } finally {
    if (current === version) loading.value = false
  }
}
watch(() => `${props.alarm.alarmId}:${location.value?.floorId || ''}:${location.value?.x ?? ''}`, loadPlan, { immediate: true })
onBeforeUnmount(() => {
  version++
  release()
})
</script>

<template>
  <ui-card v-if="location" shadow="never" class="top-gap">
    <template #header
      ><div class="card-header">
        <strong>告警位置</strong><small v-if="location.current" class="location-note">告警发生时设备尚未标注，显示当前标注位置</small>
      </div></template
    >
    <p class="location-text"><MapPin />{{ text || '已关联单位' }}</p>
    <p v-if="loading" class="location-note">正在加载平面图…</p>
    <p v-else-if="error" class="location-note">{{ error }}</p>
    <div v-else-if="planUrl" class="location-plan">
      <img :src="planUrl" alt="告警所在楼层平面图" />
      <span class="location-marker" :style="{ left: `${location.x * 100}%`, top: `${location.y * 100}%` }"><MapPin /></span>
    </div>
  </ui-card>
</template>

<style scoped>
.location-text {
  display: flex;
  gap: 6px;
  align-items: center;
  margin: 0;
  color: var(--text-strong);
}
.location-text svg {
  width: 16px;
  height: 16px;
  color: var(--danger);
}
.location-note {
  color: var(--text-muted);
  font-size: 12px;
}
.location-plan {
  position: relative;
  margin-top: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}
.location-plan img {
  display: block;
  width: 100%;
  height: auto;
}
.location-marker {
  position: absolute;
  transform: translate(-50%, -100%);
  color: var(--danger);
  animation: location-pulse 1.2s ease-in-out infinite;
}
.location-marker svg {
  width: 28px;
  height: 28px;
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.4));
}
@keyframes location-pulse {
  50% {
    transform: translate(-50%, -115%);
  }
}
@media (prefers-reduced-motion: reduce) {
  .location-marker {
    animation: none;
  }
}
</style>
