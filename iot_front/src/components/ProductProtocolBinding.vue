<script setup>
import { computed, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
const props = defineProps({ product: { type: Object, required: true } }) /* 声明 props。 */
const emit = defineEmits(['close', 'saved']) /* 声明 emit。 */
const canRollback = ref(false) /* 声明 canRollback。 */
const protocols = ref([]), switching = ref(false), loading = ref(true) /* 声明 protocols。 */
const binding = reactive({ protocolId: '', version: '' }) /* 声明 binding。 */
const publishedReleases = computed(() => protocols.value.find(item => item.definition.id === binding.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED') || []) /* 声明 publishedReleases。 */
onMounted(async () => { /* 执行当前语句并推进处理流程。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [catalog, current] = await Promise.all([api('/api/v2/protocols'), api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding`).catch(error => { if (error.status === 404) return {}; throw error })]) /* 执行当前语句并推进处理流程。 */
    protocols.value = catalog.items || [] /* 更新 protocols.value 的值。 */
    if (props.product.protocolPackageId === 'iot-standard@1.0.0' && !protocols.value.some(p => p.definition.id === 'iot-standard')) protocols.value.unshift({definition:{id:'iot-standard',name:'标准设备上报'},releases:[{version:'1.0.0',status:'PUBLISHED'}]}) /* 判断条件并选择处理分支。 */
    binding.protocolId = current.protocolId || (props.product.protocolPackageId === 'iot-standard@1.0.0' ? 'iot-standard' : ''); binding.version = current.version || (binding.protocolId === 'iot-standard' ? '1.0.0' : '') /* 更新 binding.protocolId 的值。 */
    canRollback.value = Boolean(current.previousVersion) /* 更新 canRollback.value 的值。 */
  } catch (error) { notifyError(error) } finally { loading.value = false } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
async function switchBinding(rollback = false) { /* 定义 switchBinding 函数。 */
  if (switching.value) return /* 判断条件并选择处理分支。 */
  if (!rollback && (!binding.protocolId || !binding.version)) return UiMessage.warning('请选择协议和已发布版本') /* 判断条件并选择处理分支。 */
  switching.value = true /* 更新 switching.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    await api(`/api/v2/products/${encodeURIComponent(props.product.id)}/protocol-binding${rollback ? '/rollback' : ''}`, { method:'POST', body:JSON.stringify(rollback ? {} : binding) }) /* 等待异步操作完成。 */
    UiMessage.success(rollback ? '产品协议已回滚' : '产品协议已更新') /* 执行当前语句并推进处理流程。 */
    emit('saved'); emit('close') /* 执行当前语句并推进处理流程。 */
  } catch (error) { notifyError(error) } finally { switching.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
</script>
<template>
  <ui-dialog :model-value="true" :title="`${product.name} · 协议版本`" width="min(520px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!switching" :show-close="!switching" @close="emit('close')"> <!-- 渲染 ui-dialog 界面元素。 -->
    <ui-form label-position="top" :disabled="loading || switching" v-loading="loading"> <!-- 渲染 ui-form 界面元素。 -->
      <ui-form-item label="协议"><ui-select v-model="binding.protocolId" filterable @change="binding.version = ''"><ui-option v-for="p in protocols" :key="p.definition.id" :label="p.definition.name" :value="p.definition.id" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item label="已发布版本"><ui-select v-model="binding.version"><ui-option v-for="release in publishedReleases" :key="release.version" :label="release.version" :value="release.version" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
    </ui-form> <!-- 结束当前界面区域。 -->
    <template #footer><ui-button v-permission="'POST /api/v2/products/:id/protocol-binding/rollback'" :disabled="loading || switching || !canRollback" @click="switchBinding(true)">回滚上一版本</ui-button><ui-button v-permission="'POST /api/v2/products/:id/protocol-binding'" type="primary" :disabled="loading" :loading="switching" @click="switchBinding(false)">绑定协议</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->
</template>
