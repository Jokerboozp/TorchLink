<script setup>
// 告警附件：现场照片（PNG / JPEG）、处置文档（PDF）与短视频（MP4），每条告警最多 10 个、单个 20 MiB。
// 告警关闭后附件只读；文件只在用户点击时下载。
import { computed, ref } from 'vue'
import { api, apiBlob, download, formatTime, notifyError } from '../api'
import { confirmDelete } from '../deleteAction'
import { UiMessage } from '../ui/feedback.js'
import { Download, FileText, Trash2, Upload } from '@lucide/vue'

const props = defineProps({ alarm: { type: Object, required: true } })
const emit = defineEmits(['updated'])
const input = ref(null),
  uploading = ref(false)
const attachments = computed(() => props.alarm.attachments || [])
const editable = computed(() => props.alarm.status !== 'CLOSED')
const base = computed(() => `/api/v1/alarms/${encodeURIComponent(props.alarm.alarmId)}/attachments`)
const size = bytes => (bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`)

async function upload(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  if (file.size > 20 << 20) {
    UiMessage.warning('附件不能超过 20 MiB')
    return
  }
  uploading.value = true
  try {
    const body = new FormData()
    body.append('file', file)
    const updated = await api(base.value, { method: 'POST', body })
    UiMessage.success('附件已上传')
    emit('updated', updated)
  } catch (error) {
    notifyError(error)
  } finally {
    uploading.value = false
  }
}

async function view(att) {
  try {
    const url = URL.createObjectURL(await apiBlob(`${base.value}/${encodeURIComponent(att.id)}?inline=1`))
    window.open(url, '_blank', 'noopener')
    setTimeout(() => URL.revokeObjectURL(url), 60_000)
  } catch (error) {
    notifyError(error)
  }
}

function save(att) {
  download(`${base.value}/${encodeURIComponent(att.id)}`, att.name).catch(notifyError)
}

function remove(att) {
  confirmDelete({
    label: att.name,
    path: `${base.value}/${encodeURIComponent(att.id)}`,
    onDeleted: () => emit('updated', { ...props.alarm, attachments: attachments.value.filter(v => v.id !== att.id) })
  })
}
</script>

<template>
  <ui-card shadow="never" class="top-gap">
    <template #header>
      <div class="card-header">
        <strong>附件</strong>
        <template v-if="editable && attachments.length < 10">
          <input ref="input" type="file" accept="image/png,image/jpeg,application/pdf,video/mp4" hidden @change="upload" />
          <ui-button v-permission="'POST /api/v1/alarms/:id/attachments'" text size="small" :loading="uploading" @click="input?.click()"
            ><Upload />上传</ui-button
          >
        </template>
      </div>
    </template>
    <ui-empty v-if="!attachments.length" description="暂无附件（支持 PNG、JPEG、PDF、MP4，单个不超过 20 MiB）" :image-size="48" />
    <ul v-else class="attachment-list">
      <li v-for="att in attachments" :key="att.id">
        <FileText class="attachment-icon" />
        <div class="attachment-meta">
          <button v-if="att.contentType.startsWith('image/')" type="button" class="attachment-name link" @click="view(att)">
            {{ att.name }}
          </button>
          <span v-else class="attachment-name">{{ att.name }}</span>
          <small>{{ size(att.size) }} · {{ att.uploadedBy }} · {{ formatTime(att.uploadedAt) }}</small>
        </div>
        <ui-button text size="small" title="下载" @click="save(att)"><Download /></ui-button>
        <ui-button
          v-if="editable"
          v-permission="'DELETE /api/v1/alarms/:id/attachments/:attachmentId'"
          text
          size="small"
          type="danger"
          title="删除"
          @click="remove(att)"
          ><Trash2
        /></ui-button>
      </li>
    </ul>
  </ui-card>
</template>

<style scoped>
.attachment-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--space-2);
}
.attachment-list li {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}
.attachment-icon {
  width: 16px;
  height: 16px;
  flex: none;
  color: var(--text-muted);
}
.attachment-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.attachment-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: left;
  color: var(--text-strong);
}
.attachment-name.link {
  background: none;
  border: 0;
  padding: 0;
  cursor: pointer;
  color: var(--primary-text);
}
.attachment-meta small {
  color: var(--text-muted);
}
</style>
