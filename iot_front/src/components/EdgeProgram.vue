<script setup>
import { onMounted, ref } from 'vue'
import { ElMessageBox } from 'element-plus'
import { api, notifyError, formatTime } from '../api'
const props = defineProps({ node: { type: Object, required: true } })
const emit = defineEmits(['close'])
const state = ref(null), version = ref(''), busy = ref(false), loading = ref(false)
const phases = { STARTING: '启动中', RUNNING: '运行中', STAGING: '校验制品', UPDATING: '切换中', ROLLED_BACK: '已回退', FAILED: '失败' }
const path = () => `/api/v1/edge-nodes/${encodeURIComponent(props.node.id)}/program`
async function load() {
  loading.value = true
  try { state.value = await api(path()); version.value = state.value.targetVersion || '' }
  catch (error) { state.value = null; notifyError(error) }
  finally { loading.value = false }
}
async function save() {
  if (!state.value || busy.value) return
  busy.value = true
  try {
    await ElMessageBox.confirm(version.value.trim() ? `将节点 ${props.node.name} 的目标版本设为 ${version.value.trim()}。现场启动器会执行本地信任目录签名的程序，切换时采集短暂停止；启动或认证失败将尝试回退。` : '清除目标版本后，节点保留当前程序。已经开始的切换以现场实际结果为准。', '确认程序版本', { confirmButtonText: '确认设置', cancelButtonText: '取消', type: 'warning' })
    state.value = await api(path(), { method: 'POST', body: JSON.stringify({ version: version.value.trim(), expectedGeneration: state.value.generation, confirmed: true }) })
  } catch (error) { if (error !== 'cancel' && error !== 'close') notifyError(error) }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <el-dialog :model-value="true" :title="`${node.name} · 程序升级`" width="min(600px,96vw)" append-to-body @close="emit('close')">
    <div v-loading="loading">
      <p>需要现场运行独立启动器，并配置可信程序目录。目标版本只表示请求，实际版本以启动器上报为准。</p>
      <template v-if="state">
        <p>目标版本：{{ state.targetVersion || '未指定' }} · 请求版本号：{{ state.generation }}</p>
        <p>实际版本：{{ state.status?.version || '尚未上报' }} · {{ phases[state.status?.phase] || '等待启动器' }}</p>
        <p>状态对应请求：{{ state.status?.generation ?? '—' }} · 最后上报：{{ formatTime(state.status?.lastSeenAt) }}</p>
        <p v-if="state.status?.candidateVersion">待切换版本：{{ state.status.candidateVersion }}</p>
        <el-alert v-if="state.status?.lastError" :title="state.status.lastError" type="error" :closable="false" />
        <el-form label-position="top" @submit.prevent="save"><el-form-item label="目标程序版本"><el-input v-model="version" maxlength="128" placeholder="如 1.2.0，留空清除目标" :disabled="busy" /></el-form-item></el-form>
        <p>失败后修复现场问题，再次确认同一版本会生成新的尝试请求。离线上报不能证明当前进程仍在运行。</p>
      </template>
    </div>
    <template #footer><el-button :disabled="busy" :loading="loading" @click="load">刷新状态</el-button><el-button type="primary" :disabled="!state || loading || node.status !== 'ENABLED'" :loading="busy" @click="save">设置目标版本</el-button></template>
  </el-dialog>
</template>
