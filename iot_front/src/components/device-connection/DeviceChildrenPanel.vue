<script setup>
// 子设备：列出主设备下已登记的子设备，并按子设备类型和地址添加。
import { reactive, ref } from 'vue'
import { api, formatTime, notifyError } from '../../api'
import { UiMessage } from '../../ui/feedback.js'
import { businessStatuses, label } from '../../labels'

const props = defineProps({
  // 子设备分页列表：items、total、page、loading、error。
  list: { type: Object, required: true },
  // 接入点配置的子设备类型。
  childTypes: { type: Array, default: () => [] },
  // 主设备的接口路径，例如 /api/v1/device-registry/<id>。
  base: { type: String, required: true }
})
// page(页码) 翻页，added 添加成功，device(编号) 查看子设备。
const emit = defineEmits(['page', 'added', 'device'])
const childDialog = ref(false),
  childSaving = ref(false)
const childForm = reactive({ type: '', address: '', name: '' })
function openChildDialog() {
  Object.assign(childForm, { type: props.childTypes[0]?.type || '', address: '', name: '' })
  childDialog.value = true
}
async function addChild() {
  if (childSaving.value) return
  if (!childForm.type || !childForm.address.trim()) return UiMessage.warning('请选择子设备类型并填写地址')
  childSaving.value = true
  try {
    const result = await api(`${props.base}/children`, {
      method: 'POST',
      body: JSON.stringify({ type: childForm.type, address: childForm.address.trim(), name: childForm.name.trim() })
    })
    childDialog.value = false
    UiMessage.success(result.reused ? '该地址的子设备此前已登记' : '子设备已添加')
    emit('added')
  } catch (cause) {
    notifyError(cause)
  } finally {
    childSaving.value = false
  }
}
</script>

<template>
  <section class="connection-section device-children" v-loading="list.loading">
    <div class="section-heading">
      <h3>子设备（{{ list.total }}）</h3>
      <ui-button v-if="childTypes.length" v-permission="'POST /api/v1/device-registry/:id/children'" size="small" @click="openChildDialog"
        >添加子设备</ui-button
      >
    </div>
    <p v-if="!childTypes.length">接入点尚未配置子设备类型。请在设备模板的“公共连接”中添加子设备映射后，再按地址添加子设备。</p>
    <ui-alert v-if="list.error" title="子设备加载失败" :description="list.error" type="error" :closable="false" />
    <ui-table v-else :data="list.items" border empty-text="暂无子设备，等待主设备上报登记信息">
      <ui-table-column prop="device.name" label="名称" min-width="140" /><ui-table-column
        prop="device.childAddress"
        label="地址"
        min-width="90"
      />
      <ui-table-column prop="productName" label="设备模板" min-width="130" />
      <ui-table-column label="协议" min-width="150"
        ><template #default="{ row }"
          >{{ row.binding?.protocolId || '未配置' }} · {{ row.binding?.version || '—' }}</template
        ></ui-table-column
      >
      <ui-table-column label="最近上报" min-width="170"
        ><template #default="{ row }">{{ formatTime(row.runtimeState?.lastSeenAt) }}</template></ui-table-column
      >
      <ui-table-column label="状态" min-width="95"
        ><template #default="{ row }">{{ label(businessStatuses, row.runtimeState?.businessStatus) || '未知' }}</template></ui-table-column
      >
      <ui-table-column label="操作" width="110" fixed="right"
        ><template #default="{ row }"
          ><ui-button link type="primary" @click="emit('device', row.device.id)">查看子设备</ui-button></template
        ></ui-table-column
      >
    </ui-table>
    <ui-pagination
      v-if="list.total > 20"
      :current-page="list.page"
      :page-size="20"
      :total="list.total"
      layout="prev, pager, next"
      @update:current-page="page => emit('page', page)"
    />
  </section>
  <ui-dialog
    v-model="childDialog"
    title="添加子设备"
    width="min(480px, 94vw)"
    :close-on-click-modal="false"
    :close-on-press-escape="!childSaving"
    :show-close="!childSaving"
  >
    <ui-form label-position="top" :disabled="childSaving" @submit.prevent="addChild">
      <ui-form-item label="子设备类型" required
        ><ui-select v-model="childForm.type" aria-label="子设备类型"
          ><ui-option
            v-for="item in childTypes"
            :key="item.type"
            :value="item.type"
            :label="`${item.type} · 模板 ${item.productId}`" /></ui-select
      ></ui-form-item>
      <ui-form-item label="子设备地址" required
        ><ui-input v-model="childForm.address" placeholder="主设备协议中的子设备地址" aria-label="子设备地址"
      /></ui-form-item>
      <ui-form-item label="名称"
        ><ui-input v-model="childForm.name" maxlength="256" placeholder="留空时按类型和地址生成" aria-label="子设备名称"
      /></ui-form-item>
    </ui-form>
    <p class="child-dialog-hint">子设备沿用主设备的连接，由主设备协议按地址识别。</p>
    <template #footer
      ><ui-button :disabled="childSaving" @click="childDialog = false">取消</ui-button
      ><ui-button type="primary" :loading="childSaving" @click="addChild">添加</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped src="./connection-section.css"></style>
<style scoped>
.child-dialog-hint {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
</style>
