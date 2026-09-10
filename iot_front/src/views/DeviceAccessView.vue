<script setup>
import { defineAsyncComponent, ref } from 'vue'

const emit = defineEmits(['navigate'])
const IntegrationView = defineAsyncComponent(() => import('./IntegrationView.vue'))
const TestDeviceView = defineAsyncComponent(() => import('./TestDeviceView.vue'))
const activeTab = ref('integration')
try {
  const detail = JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}')
  if (detail?.tab === 'testDevice' && !detail.deviceId) {
    activeTab.value = 'testDevice'
    sessionStorage.removeItem('iot:navigation-detail')
  }
} catch {
  sessionStorage.removeItem('iot:navigation-detail')
}
</script>

<template>
  <el-tabs v-model="activeTab" class="device-access-tabs">
    <el-tab-pane label="设备接入" name="integration" lazy>
      <IntegrationView @navigate="(page, detail) => emit('navigate', page, detail)" />
    </el-tab-pane>
    <el-tab-pane label="测试设备" name="testDevice" lazy>
      <TestDeviceView @navigate="(page, detail) => emit('navigate', page, detail)" />
    </el-tab-pane>
  </el-tabs>
</template>
