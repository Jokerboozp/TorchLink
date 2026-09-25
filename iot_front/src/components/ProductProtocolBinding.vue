<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, formatTime, notifyError } from '../api'

// 设备模板详情中的“协议版本”：切换或回滚模板使用的已发布协议版本。
const props = defineProps({ product: { type: Object, required: true } })
const emit = defineEmits(['saved'])
const protocols = ref([]), switching = ref(false), loading = ref(true)
const current = ref({})
const binding = reactive({ protocolId: '', version: '' })
const standard = computed(() => props.product.protocolPackageId === 'iot-standard@1.0.0')
const publishedReleases = computed(() => protocols.value.find(item => item.definition.id === binding.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED') || [])
const changed = computed(() => binding.protocolId !== (current.value.protocolId || '') || binding.version !== (current.value.version || ''))

async function load() {
  loading.value = true
  try {
    const [catalog, active] = await Promise.all([api('/api/v2/protocols'), api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding`).catch(error => { if (error.status === 404) return {}; throw error })])
    protocols.value = catalog.items || []
    current.value = active || {}
    binding.protocolId = active.protocolId || ''
    binding.version = active.version || ''
  } catch (error) { notifyError(error) } finally { loading.value = false }
}
async function switchBinding(rollback = false) {
  if (switching.value) return
  if (!rollback && (!binding.protocolId || !binding.version)) return UiMessage.warning('请选择协议和已发布版本')
  switching.value = true
  try {
    await api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding${rollback ? '/rollback' : ''}`, { method: 'POST', body: JSON.stringify(rollback ? {} : binding) })
    UiMessage.success(rollback ? '已回滚到上一版本' : '协议版本已切换')
    emit('saved')
    await load()
  } catch (error) { notifyError(error) } finally { switching.value = false }
}
onMounted(load)
</script>

<template>
  <section class="protocol-binding" :class="{ 'ui-loading': loading }">
    <p v-if="standard" class="protocol-binding__note">该模板使用内置标准设备上报协议（MQTT / HTTP），无需绑定版本。</p>
    <template v-else>
      <dl class="protocol-binding__current">
        <div><dt>当前协议</dt><dd>{{ current.protocolId || '未绑定' }}</dd></div>
        <div><dt>当前版本</dt><dd>{{ current.version || '—' }}</dd></div>
        <div><dt>上一版本</dt><dd>{{ current.previousVersion ? `${current.previousProtocolId || current.protocolId} · ${current.previousVersion}` : '—' }}</dd></div>
        <div><dt>切换时间</dt><dd>{{ formatTime(current.updatedAt) }}</dd></div>
      </dl>
      <ui-form label-position="top" :disabled="loading || switching" class="protocol-binding__form">
        <ui-form-item label="协议"><ui-select v-model="binding.protocolId" filterable placeholder="选择协议" @change="binding.version = ''"><ui-option v-for="item in protocols" :key="item.definition.id" :label="item.definition.name || item.definition.id" :value="item.definition.id" /></ui-select></ui-form-item>
        <ui-form-item label="已发布版本"><ui-select v-model="binding.version" placeholder="选择版本"><ui-option v-for="release in publishedReleases" :key="release.version" :label="release.version" :value="release.version" /></ui-select></ui-form-item>
      </ui-form>
      <p class="protocol-binding__note">切换后，新上报的数据使用新版本解析；共享监听和子设备映射会校验新版本是否仍支持现有接入方式。</p>
      <div class="protocol-binding__actions">
        <ui-button v-permission="'POST /api/v2/products/:id/protocol-binding/rollback'" :disabled="loading || switching || !current.previousVersion" @click="switchBinding(true)">回滚上一版本</ui-button>
        <ui-button v-permission="'POST /api/v2/products/:id/protocol-binding'" type="primary" :disabled="loading || !changed" :loading="switching" @click="switchBinding(false)">切换版本</ui-button>
      </div>
    </template>
  </section>
</template>

<style scoped>
.protocol-binding { position: relative; display: grid; gap: var(--space-4); }
.protocol-binding__current { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-3); margin: 0; }
.protocol-binding__current > div { min-width: 0; padding: var(--space-3); background: var(--surface-muted); border-radius: var(--radius-md); }
.protocol-binding__current dt { color: var(--text-muted); font-size: var(--font-size-xs); }
.protocol-binding__current dd { margin: 2px 0 0; color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); overflow-wrap: anywhere; }
.protocol-binding__form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 var(--space-4); }
.protocol-binding__note { margin: 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.protocol-binding__actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); }
@media (max-width: 767px) {
  .protocol-binding__current, .protocol-binding__form { grid-template-columns: 1fr 1fr; }
  .protocol-binding__form { grid-template-columns: 1fr; }
}
</style>
