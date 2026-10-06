<script setup>
// 巡检任务详情：检查项目、整改与复核记录；可执行的操作由页面按任务状态给出。
import { session } from '../../api'
import { dateTimeLabel, inspectionStates, statusLabel, statusTone } from '../../fireSafety'
import { canReviewInspection, extinguisherLabels } from '../../fireSafetyManagement'
import RowActions from '../layout/RowActions.vue'
import StatusDot from '../layout/StatusDot.vue'

// 任务详情；置空即关闭。
const taskDetail = defineModel({ type: Object, default: null })
const props = defineProps({
  // 当前任务可执行的操作（巡检、整改、复核、取消）。
  actions: { type: Array, default: () => [] },
  // /api/v1/fire-safety/options 的结果。
  options: { type: Object, required: true }
})
const { assetName, personName } = extinguisherLabels(props.options)
</script>

<template>
  <ui-drawer
    :model-value="Boolean(taskDetail)"
    :title="taskDetail ? `巡检 · ${assetName(taskDetail.extinguisherId)}` : ''"
    size="min(760px,100vw)"
    @close="taskDetail = null"
  >
    <template v-if="taskDetail"
      ><StatusDot
        :tone="statusTone(taskDetail.status)"
        :label="inspectionStates.find(item => item.value === taskDetail.status)?.label || taskDetail.status"
      />
      <div class="fire-row"><RowActions :actions="actions" :inline="4" /></div>
      <p v-if="taskDetail.status === 'reviewing' && !canReviewInspection(taskDetail, session.user)" class="fire-hint">
        当前整改由你提交，请由其他有复核权限的用户复核。
      </p>
      <dl class="fire-details">
        <div>
          <dt>巡检人员</dt>
          <dd>{{ personName(taskDetail.assigneeId) }}</dd>
        </div>
        <div>
          <dt>截止时间</dt>
          <dd>{{ dateTimeLabel(taskDetail.dueAt) }}</dd>
        </div>
        <div>
          <dt>任务说明</dt>
          <dd>{{ taskDetail.notes || '—' }}</dd>
        </div>
        <div>
          <dt>创建人</dt>
          <dd>{{ taskDetail.createdBy }}</dd>
        </div>
        <div v-if="taskDetail.inspectedAt">
          <dt>巡检时间 / 提交人</dt>
          <dd>{{ dateTimeLabel(taskDetail.inspectedAt) }} · {{ taskDetail.inspectedBy }}</dd>
        </div>
        <div v-if="taskDetail.result">
          <dt>巡检结果</dt>
          <dd>{{ statusLabel(taskDetail.result) }}</dd>
        </div>
        <div v-if="taskDetail.findings">
          <dt>发现的问题</dt>
          <dd>{{ taskDetail.findings }}</dd>
        </div>
        <div v-if="taskDetail.cancelReason">
          <dt>取消原因</dt>
          <dd>{{ taskDetail.cancelReason }}</dd>
        </div>
      </dl>
      <section v-if="taskDetail.checks?.length" class="fire-section">
        <h3>检查项目</h3>
        <div v-for="item in taskDetail.checks" :key="item.name" class="fire-row">
          <span>{{ item.name }}</span
          ><StatusDot :tone="item.passed ? 'success' : 'danger'" :label="item.passed ? '合格' : '不合格'" />
        </div>
      </section>
      <section class="fire-section">
        <h3>整改与复核记录</h3>
        <p v-if="!taskDetail.rectifications?.length" class="fire-hint">暂无整改记录</p>
        <article v-for="(item, index) in taskDetail.rectifications || []" :key="index" class="fire-history">
          <strong>第 {{ index + 1 }} 次整改</strong
          ><StatusDot :tone="statusTone(item.status)" :label="item.status === 'pending' ? '待复核' : statusLabel(item.status)" />
          <p>{{ item.action }}</p>
          <small class="fire-hint">{{ item.submittedBy }} · {{ dateTimeLabel(item.submittedAt) }}</small
          ><template v-if="item.reviewedAt"
            ><p>复核意见：{{ item.reviewNote || '—' }}</p>
            <small class="fire-hint">{{ item.reviewedBy }} · {{ dateTimeLabel(item.reviewedAt) }}</small></template
          >
        </article>
      </section>
    </template>
  </ui-drawer>
</template>

<style scoped src="../../fireSafetyManagement.css"></style>
