<script setup>
import { computed } from 'vue'
import { permissionSections, featureLevel, applyFeatureLevel } from '../permissionPresets'
const props = defineProps({ modelValue: { type: Array, default: () => [] }, catalog: { type: Array, default: () => [] } })
const emit = defineEmits(['update:modelValue'])
const sections = computed(() => permissionSections.map(section => ({
  ...section,
  groups: section.menus.flatMap(id => {
    const menu = props.catalog.find(item => item.kind === 'menu' && item.menu === id)
    return menu ? [{ ...menu, actions: props.catalog.filter(item => item.kind === 'action' && item.menu === id) }] : []
  })
})))
function choose(group, level) { emit('update:modelValue', applyFeatureLevel(group, props.modelValue, level)) }
function toggle(id, checked) { emit('update:modelValue', checked ? [...new Set([...props.modelValue, id])] : props.modelValue.filter(value => value !== id)) }
</script>
<template>
  <div class="permission-picker">
    <p class="muted-text permission-note">选择功能和使用级别即可。“管理”包含该功能的全部操作（含删除等）；特殊需要可展开细项设置。</p>
    <details v-for="(section, index) in sections" :key="section.name" class="permission-section" :open="index === 0">
      <summary>{{ section.name }}<small>已开放 {{ section.groups.filter(group => modelValue.includes(group.id)).length }} / {{ section.groups.length }} 项功能</small></summary>
      <div v-for="group in section.groups" :key="group.id" class="permission-group">
        <div class="permission-feature">
          <strong>{{ group.name }}</strong>
          <ui-radio-group class="segmented-choice-group" :model-value="featureLevel(group, modelValue)" :aria-label="`${group.name}权限级别`" @update:model-value="choose(group, $event)">
            <ui-radio-button value="none">关闭</ui-radio-button>
            <ui-radio-button value="view">{{ group.menu === 'ai' ? '问答' : '查看' }}</ui-radio-button>
            <ui-radio-button v-if="group.actions.length" value="manage">管理</ui-radio-button>
            <ui-radio-button v-if="featureLevel(group, modelValue) === 'custom'" value="custom" disabled>自定义</ui-radio-button>
          </ui-radio-group>
        </div>
        <details v-if="group.actions.length" class="permission-advanced">
          <summary>细项设置<span v-if="featureLevel(group, modelValue) === 'custom'"> · 保留已有自定义权限</span></summary>
          <ui-checkbox :model-value="modelValue.includes(group.id)" @change="toggle(group.id, $event)">允许访问此功能</ui-checkbox>
          <div class="permission-actions">
            <ui-checkbox v-for="action in group.actions" :key="action.id" :model-value="modelValue.includes(action.id)" @change="toggle(action.id, $event)"><ui-tooltip :content="action.id"><span>{{ action.name }}</span></ui-tooltip></ui-checkbox>
          </div>
        </details>
      </div>
    </details>
  </div>
</template>
<style scoped>
.permission-picker { width:100%; min-width:0; }
.permission-note { font-size:12px; line-height:1.6; }
.permission-section { border-bottom:1px solid var(--border); }
.permission-section > summary { display:flex; align-items:center; justify-content:space-between; padding:14px 0; cursor:pointer; font-weight:600; }
.permission-section > summary::before { content:'▸'; margin-right:8px; }
.permission-section[open] > summary::before { content:'▾'; }
.permission-section > summary small { margin-left:auto; color:var(--text-muted); font-weight:400; }
.permission-group { padding:12px 0; border-top:1px solid var(--border); }
.permission-feature { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:10px; font-size:13px; }
.permission-advanced { margin-top:8px; font-size:12px; color:var(--text-muted); }
.permission-advanced summary { cursor:pointer; padding:4px 0; }
.permission-actions { display:flex; flex-wrap:wrap; gap:4px 14px; padding-top:6px; }
.permission-actions .ui-checkbox { margin-right:0; }
@media(max-width:640px) { .permission-feature { align-items:flex-start; flex-direction:column; } }
</style>
