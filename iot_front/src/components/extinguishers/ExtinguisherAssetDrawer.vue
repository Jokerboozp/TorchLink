<script setup>
// 灭火器详情：资料、维护计划与最近巡检，可从这里编辑资料或创建巡检任务。
import { dateLabel, dateTimeLabel, statusLabel, statusTone } from '../../fireSafety'
import { extinguisherLabels } from '../../fireSafetyManagement'
import StatusDot from '../layout/StatusDot.vue'

// 详情对象；置空即关闭。
const assetDetail = defineModel({ type: Object, default: null })
// /api/v1/fire-safety/options 的结果。
const props = defineProps({ options: { type: Object, required: true } })
// edit(灭火器) 编辑资料，task(灭火器) 创建巡检任务。
const emit = defineEmits(['edit', 'task'])
const { stationName, typeName } = extinguisherLabels(props.options)
</script>

<template>
  <ui-drawer
    :model-value="Boolean(assetDetail)"
    :title="assetDetail ? `灭火器 · ${assetDetail.code}` : ''"
    size="min(700px,100vw)"
    @close="assetDetail = null"
  >
    <template v-if="assetDetail"
      ><StatusDot :tone="statusTone(assetDetail.status)" :label="statusLabel(assetDetail.status)" />
      <dl class="fire-details">
        <div
          v-for="(value, label) in {
            所属消防站: stationName(assetDetail.stationId),
            放置位置: assetDetail.location,
            类型: typeName(assetDetail.type),
            规格: assetDetail.specification,
            厂家: assetDetail.manufacturer,
            出厂序列号: assetDetail.serialNumber,
            生产日期: dateLabel(assetDetail.manufacturedOn),
            维护到期: dateLabel(assetDetail.serviceDueOn),
            计划报废: dateLabel(assetDetail.retireOn),
            巡检周期: `${assetDetail.inspectionCycleDays} 天`,
            最近巡检: assetDetail.lastInspectedAt ? dateTimeLabel(assetDetail.lastInspectedAt) : '尚未巡检',
            下次巡检: dateLabel(assetDetail.nextInspectionOn),
            备注: assetDetail.notes
          }"
          :key="label"
        >
          <dt>{{ label }}</dt>
          <dd>{{ value || '—' }}</dd>
        </div>
      </dl>
      <div class="fire-row">
        <ui-button v-permission="'PUT /api/v1/extinguishers/:id'" @click="emit('edit', assetDetail)">编辑资料</ui-button
        ><ui-button
          v-if="assetDetail.status !== 'retired'"
          v-permission="'POST /api/v1/extinguisher-inspections'"
          type="primary"
          @click="emit('task', assetDetail)"
          >创建巡检任务</ui-button
        >
      </div></template
    >
  </ui-drawer>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
