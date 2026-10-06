<script setup>
// 设备控制：选择产品定义的命令并填写参数，人工确认后下发；发送结果、应答与 MQTT 命令记录在下方核对。
import { pretty } from '../../api'
import { commandStatuses, label } from '../../labels'
import CommandValueInput from '../CommandValueInput.vue'

// 所选命令标识与参数值。
const commandType = defineModel('type', { type: String, default: '' })
const commandValues = defineModel('values', { type: Object, default: () => ({}) })
defineProps({
  // 设备连接详情。
  data: { type: Object, required: true },
  operations: { type: Array, default: () => [] },
  selectedOperation: { type: Object, default: null },
  // MQTT 命令记录分页列表：items、total、page、error。
  list: { type: Object, required: true },
  canCommand: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  actionBusy: { type: Boolean, default: false },
  // 已有待确认结果的命令，可开始一条新命令。
  pending: { type: Boolean, default: false },
  commandResult: { type: Object, default: null },
  commandReply: { type: Object, default: null }
})
// sendMqtt 与 send 分别经 MQTT 或接入点下发，reset 开始新命令，reply 查看应答报文，page(页码) 翻页命令记录。
const emit = defineEmits(['sendMqtt', 'send', 'reset', 'reply', 'page'])
</script>

<template>
  <section class="connection-section device-commands">
    <h3>设备控制</h3>
    <p>已发送不代表设备执行成功。请核对发送状态和设备应答；结果未知时不要重复发送。</p>
    <template v-if="canCommand">
      <ui-form label-position="top" :disabled="actionBusy || loading">
        <ui-form-item label="设备命令"
          ><ui-select v-model="commandType" aria-label="设备命令" placeholder="选择设备支持的命令"
            ><ui-option v-for="c in operations" :key="c.identifier" :value="c.identifier" :label="c.name || c.identifier" /></ui-select
        ></ui-form-item>
        <ui-form-item
          v-for="field in selectedOperation?.fields || []"
          :key="`${commandType}:${field.identifier}`"
          :label="`${field.name || field.identifier}${field.unit ? `（${field.unit}）` : ''}`"
          :required="field.required"
        >
          <CommandValueInput v-model="commandValues[field.identifier]" :kind="field.dataType" :label="field.name || field.identifier" />
        </ui-form-item>
        <p v-if="selectedOperation && !selectedOperation.fields?.length">此命令无需参数。</p>
      </ui-form>
      <ui-button
        v-permission="'POST /api/v1/device-registry/:id/commands'"
        v-if="data.connector === 'MQTT'"
        :disabled="loading || !data.mqttCommandAvailable || !data.credentialEnabled || !selectedOperation"
        :loading="actionBusy"
        @click="emit('sendMqtt')"
        >执行命令</ui-button
      >
      <ui-button
        v-permission="'POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands'"
        v-else
        :loading="actionBusy"
        :disabled="loading || !data.profile.enabled || !data.sessions?.length || !selectedOperation"
        @click="emit('send')"
        >执行命令</ui-button
      >
      <p v-if="data.connector !== 'MQTT' && (!data.profile.enabled || !data.sessions?.length)">
        当前没有在线会话或接入点已停用，暂时不能下发命令。
      </p>
      <ui-button v-if="pending" class="section-feedback" :disabled="actionBusy" @click="emit('reset')">开始一条新命令</ui-button>
    </template>
    <ui-alert
      v-if="commandResult?.lastError"
      :title="label(commandStatuses, String(commandResult.status || '').toUpperCase())"
      :description="commandResult.lastError"
      type="warning"
      :closable="false"
    />
    <ui-descriptions v-if="commandResult" :column="1" border class="section-feedback"
      ><ui-descriptions-item label="发送状态">{{
        label(commandStatuses, String(commandResult.status || '').toUpperCase())
      }}</ui-descriptions-item
      ><ui-descriptions-item label="设备应答">{{
        commandResult.reply || commandResult.response
          ? pretty(commandResult.reply || commandResult.response)
          : commandResult.rawMessageId
            ? `已收到应答，原始报文：${commandResult.rawMessageId}`
            : '尚无应答内容'
      }}</ui-descriptions-item></ui-descriptions
    >
    <ui-button v-if="commandResult?.rawMessageId" class="section-feedback" @click="emit('reply')">查看应答报文</ui-button>
    <pre v-if="commandReply">{{ pretty(commandReply) }}</pre>
    <template v-if="data.connector === 'MQTT'">
      <ui-alert v-if="list.error" title="命令记录加载失败" :description="list.error" type="error" :closable="false" />
      <ui-table v-else :data="list.items" border empty-text="暂无命令记录"
        ><ui-table-column prop="type" label="命令" min-width="120" /><ui-table-column label="状态" min-width="170"
          ><template #default="{ row }">{{ label(commandStatuses, row.status) }}</template></ui-table-column
        ><ui-table-column label="回执" min-width="180"
          ><template #default="{ row }"
            ><span class="field-value">{{ pretty(row.reply || {}) }}</span></template
          ></ui-table-column
        ></ui-table
      >
      <ui-pagination
        v-if="list.total > 20"
        :current-page="list.page"
        :page-size="20"
        :total="list.total"
        layout="prev, pager, next"
        @update:current-page="page => emit('page', page)"
      />
    </template>
  </section>
</template>

<style scoped src="./connection-section.css"></style>
