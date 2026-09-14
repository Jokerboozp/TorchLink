<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, notifyError } from '../api'
const props = defineProps({ product: { type: Object, required: true } })
const emit = defineEmits(['close', 'saved'])
const canRollback = ref(false)
const protocols = ref([]), switching = ref(false), loading = ref(true)
const binding = reactive({ protocolId: '', version: '' })
const publishedReleases = computed(() => protocols.value.find(item => item.definition.id === binding.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED') || [])
onMounted(async () => {
  try {
    const [catalog, current] = await Promise.all([api('/api/v2/protocols'), api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding`).catch(error => { if (error.status === 404) return {}; throw error })])
    protocols.value = catalog.items || []
    if (props.product.protocolPackageId === 'iot-standard@1.0.0' && !protocols.value.some(p => p.definition.id === 'iot-standard')) protocols.value.unshift({definition:{id:'iot-standard',name:'标准设备上报'},releases:[{version:'1.0.0',status:'PUBLISHED'}]})
    binding.protocolId = current.protocolId || (props.product.protocolPackageId === 'iot-standard@1.0.0' ? 'iot-standard' : ''); binding.version = current.version || (binding.protocolId === 'iot-standard' ? '1.0.0' : '')
    canRollback.value = Boolean(current.previousVersion)
  } catch (error) { notifyError(error) } finally { loading.value = false }
})
async function switchBinding(rollback = false) {
  if (switching.value) return
  if (!rollback && (!binding.protocolId || !binding.version)) return ElMessage.warning('请选择协议和已发布版本')
  switching.value = true
  try {
    await api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding${rollback ? '/rollback' : ''}`, { method:'POST', body:JSON.stringify(rollback ? {} : binding) })
    ElMessage.success(rollback ? '产品协议已回滚' : '产品协议已更新')
    emit('saved'); emit('close')
  } catch (error) { notifyError(error) } finally { switching.value = false }
}
</script>
<template>
  <el-dialog :model-value="true" :title="`${product.name} · 协议版本`" width="min(520px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!switching" :show-close="!switching" @close="emit('close')">
    <el-form label-position="top" :disabled="loading || switching" v-loading="loading">
      <el-form-item label="协议"><el-select v-model="binding.protocolId" filterable @change="binding.version = ''"><el-option v-for="p in protocols" :key="p.definition.id" :label="p.definition.name" :value="p.definition.id" /></el-select></el-form-item>
      <el-form-item label="已发布版本"><el-select v-model="binding.version"><el-option v-for="release in publishedReleases" :key="release.version" :label="release.version" :value="release.version" /></el-select></el-form-item>
    </el-form>
    <template #footer><el-button v-permission="'POST /api/v2/products/:id/protocol-binding/rollback'" :disabled="loading || switching || !canRollback" @click="switchBinding(true)">回滚上一版本</el-button><el-button v-permission="'POST /api/v2/products/:id/protocol-binding'" type="primary" :disabled="loading" :loading="switching" @click="switchBinding(false)">绑定协议</el-button></template>
  </el-dialog>
</template>
