<script setup>
// 懒加载页面的加载中与加载失败占位：慢网络时显示加载提示；
// 平台升级后旧标签页取不到旧版本页面文件时，提示刷新而不是留下空白。
defineProps({ failed: Boolean })
const reload = () => window.location.reload()
</script>

<template>
  <div class="page-load-state" :role="failed ? 'alert' : 'status'">
    <template v-if="failed">
      <strong>页面加载失败</strong>
      <span>网络中断或平台已升级，刷新后即可继续使用。</span>
      <ui-button size="small" type="primary" @click="reload">刷新页面</ui-button>
    </template>
    <span v-else class="page-load-state__loading">正在加载页面…</span>
  </div>
</template>

<style scoped>
.page-load-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-2);
  padding: 48px 16px;
  color: var(--text-secondary);
  text-align: center;
}
.page-load-state strong {
  color: var(--text-strong);
}
</style>
