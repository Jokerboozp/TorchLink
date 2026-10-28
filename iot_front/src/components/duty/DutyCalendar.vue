<script setup>
// 排班日历：按月或按周展示已加载的排班；数据由页面按日期范围分批读取后传入。
import { computed } from 'vue'
import { NDatePicker } from 'naive-ui'
import { ChevronLeft, ChevronRight, Plus } from '@lucide/vue'
import { assignmentsForDay, calendarDays, dateLabel, toDateInput } from '../../fireSafety'
import StatusDot from '../layout/StatusDot.vue'

const anchor = defineModel('anchor', { type: String, required: true })
const mode = defineModel('mode', { type: String, default: 'month' })
const props = defineProps({
  rows: { type: Array, default: () => [] },
  // [开始, 结束) 毫秒时间戳，与页面的查询范围一致。
  range: { type: Array, required: true },
  total: { type: Number, default: 0 },
  complete: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  // 是否按消防站或关键字筛选，用于摘要文字。
  filtered: { type: Boolean, default: false },
  createDisabled: { type: Boolean, default: false },
  stationName: { type: Function, required: true },
  shiftName: { type: Function, required: true },
  personnelNames: { type: Function, required: true }
})
// move(±1) 切换周期，change 在日期或视图变化后重新加载，more 继续加载，create(日期) 与 open(排班) 打开排班弹窗。
const emit = defineEmits(['move', 'today', 'change', 'more', 'create', 'open'])

const days = computed(() => calendarDays(props.range).map(day => ({ ...day, rows: assignmentsForDay(props.rows, day) })))
const calendarTitle = computed(() =>
  mode.value === 'week'
    ? `${dateLabel(toDateInput(props.range[0]))} — ${dateLabel(toDateInput(props.range[1] - 1))}`
    : `${anchor.value.slice(0, 4)}年${Number(anchor.value.slice(5, 7))}月`
)
const leadingDays = computed(() => (mode.value === 'month' ? (days.value[0]?.weekday + 6) % 7 : 0))
const calendarPeople = computed(() => new Set(props.rows.flatMap(row => row.personnelIds || [])).size)
const timeLabel = value => new Date(value).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })
</script>

<template>
  <section class="duty-calendar" aria-label="排班日历">
    <header class="duty-calendar__toolbar">
      <div class="duty-calendar__period">
        <ui-button size="small" aria-label="上一周期" @click="emit('move', -1)"><ChevronLeft /></ui-button>
        <h2>{{ calendarTitle }}</h2>
        <ui-button size="small" aria-label="下一周期" @click="emit('move', 1)"><ChevronRight /></ui-button>
      </div>
      <div class="duty-calendar__controls">
        <NDatePicker
          v-model:formatted-value="anchor"
          size="small"
          type="date"
          value-format="yyyy-MM-dd"
          format="yyyy-MM-dd"
          :clearable="false"
          @update:formatted-value="emit('change')"
        /><ui-button size="small" @click="emit('today')">今天</ui-button
        ><ui-radio-group v-model="mode" size="small" @change="emit('change')"
          ><ui-radio-button value="month">月</ui-radio-button><ui-radio-button value="week">周</ui-radio-button></ui-radio-group
        >
      </div>
    </header>
    <div class="duty-calendar__summary">
      <span
        >{{ mode === 'month' ? '当月' : '当周' }}{{ filtered ? '筛选范围' : '' }}：已加载 {{ rows.length }} / {{ total }} 个排班 ·
        {{ calendarPeople }} 名人员</span
      ><StatusDot :tone="complete ? 'success' : 'warning'" :label="loading ? '加载中' : complete ? '已加载全部排班' : '尚未加载完整'" />
    </div>
    <slot />
    <div class="duty-calendar__scroll" :aria-busy="loading">
      <div class="duty-calendar__weekdays">
        <span v-for="name in ['周一', '周二', '周三', '周四', '周五', '周六', '周日']" :key="name">{{ name }}</span>
      </div>
      <div class="duty-calendar__grid" :class="{ 'duty-calendar__grid--week': mode === 'week' }">
        <div v-for="n in leadingDays" :key="`empty-${n}`" class="duty-calendar__blank" aria-hidden="true" />
        <section
          v-for="day in days"
          :key="day.date"
          class="duty-calendar__day"
          :class="{ 'duty-calendar__day--today': day.date === toDateInput() }"
          :aria-label="dateLabel(day.date)"
        >
          <div class="duty-calendar__day-head">
            <strong>{{ day.day }}</strong
            ><ui-button
              v-permission="'POST /api/v1/duty/assignments'"
              size="small"
              text
              :aria-label="`${dateLabel(day.date)}新增排班`"
              :disabled="createDisabled"
              @click="emit('create', day.date)"
              ><Plus
            /></ui-button>
          </div>
          <button v-for="row in day.rows" :key="row.id" class="duty-calendar__assignment" type="button" @click="emit('open', row)">
            <strong>{{ shiftName(row.shiftId) }}</strong
            ><span>{{ stationName(row.stationId) }}</span
            ><span
              >{{ row.startAt < day.startAt ? '前日 ' : '' }}{{ timeLabel(row.startAt) }} — {{ row.endAt > day.endAt ? '次日 ' : ''
              }}{{ timeLabel(row.endAt) }}</span
            ><small>{{ personnelNames(row) }}</small>
          </button>
          <span v-if="!day.rows.length" class="duty-calendar__empty">{{ complete ? '未排班' : '待核对' }}</span>
        </section>
      </div>
    </div>
    <footer v-if="!complete" class="duty-calendar__footer">
      <span>当前日历及人数只包含已加载记录，完整加载后可核对覆盖情况。</span
      ><ui-button :loading="loading" @click="emit('more')">继续加载排班</ui-button>
    </footer>
  </section>
</template>

<style scoped>
.duty-calendar {
  min-width: 0;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}
.duty-calendar__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 16px;
}
.duty-calendar__period,
.duty-calendar__controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.duty-calendar__period h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.duty-calendar__controls :deep(.n-date-picker) {
  width: 150px;
}
.duty-calendar__summary {
  display: flex;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 12px;
}
.duty-calendar__scroll {
  overflow-x: auto;
}
.duty-calendar__weekdays,
.duty-calendar__grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  min-width: 840px;
}
.duty-calendar__weekdays {
  color: var(--text-muted);
  background: var(--surface-muted);
  border-top: 1px solid var(--border);
  font-size: 12px;
  text-align: center;
}
.duty-calendar__weekdays span {
  padding: 9px;
}
.duty-calendar__day,
.duty-calendar__blank {
  min-height: 155px;
  padding: 8px;
  border-top: 1px solid var(--border);
  border-right: 1px solid var(--border);
}
.duty-calendar__day:nth-child(7n),
.duty-calendar__blank:nth-child(7n) {
  border-right: 0;
}
.duty-calendar__grid--week .duty-calendar__day {
  min-height: 280px;
}
.duty-calendar__blank {
  background: var(--surface-muted);
}
.duty-calendar__day-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 24px;
  margin-bottom: 8px;
}
.duty-calendar__day-head strong {
  display: grid;
  place-items: center;
  min-width: 24px;
  height: 24px;
  font-size: 12px;
}
.duty-calendar__day--today .duty-calendar__day-head strong {
  color: var(--primary-text);
  background: var(--primary-soft);
  border-radius: 50%;
}
.duty-calendar__assignment {
  display: flex;
  flex-direction: column;
  gap: 3px;
  width: 100%;
  margin: 0 0 7px;
  padding: 8px;
  color: var(--text);
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: 5px;
  font: inherit;
  font-size: 11px;
  text-align: left;
  cursor: pointer;
  overflow-wrap: anywhere;
}
.duty-calendar__assignment:hover {
  background: var(--primary-soft);
  border-color: var(--primary);
}
.duty-calendar__assignment strong {
  font-size: 12px;
}
.duty-calendar__assignment small {
  font-size: 11px;
  color: var(--text-muted);
}
.duty-calendar__empty {
  font-size: 11px;
  color: var(--text-muted);
}
.duty-calendar__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  color: var(--text-muted);
  font-size: 12px;
}
@media (max-width: 767px) {
  .duty-calendar__toolbar,
  .duty-calendar__summary {
    align-items: flex-start;
    flex-direction: column;
  }
  .duty-calendar__controls {
    width: 100%;
  }
  .duty-calendar__controls :deep(.n-date-picker) {
    flex: 1;
    min-width: 130px;
  }
  .duty-calendar__period h2 {
    font-size: 13px;
  }
}
</style>
