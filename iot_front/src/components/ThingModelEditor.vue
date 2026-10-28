<script setup>
import { computed } from 'vue'
import ThingModelFields from './ThingModelFields.vue'
const props = defineProps({ modelValue: Object })
const emit = defineEmits(['update:modelValue'])
const model = computed(() => props.modelValue || { properties: [], events: [], commands: [] })
const field = () => ({ identifier: '', name: '', dataType: 'number', unit: '', required: false, writable: false })
function update(fn) {
  const next = JSON.parse(JSON.stringify(model.value))
  next.properties ||= []
  next.events ||= []
  next.commands ||= []
  fn(next)
  emit('update:modelValue', next)
}
function addOperation(kind) {
  update(next => next[kind].push({ identifier: '', name: '', fields: [] }))
}
const operations = [
  { kind: 'events', label: '事件', help: '事件标识与协议解析后的事件一致。' },
  { kind: 'commands', label: '命令', help: '这里只声明受支持命令与参数；协议必须实现对应编码，实际下发仍需设备控制权限与人工确认。' }
]
</script>
<template>
  <section class="thing-model-editor">
    <h3>数据定义</h3>
    <p>为解析字段设置中文名称、单位，声明设备实际支持的事件与命令。保存声明不会自动新增协议能力。</p>
    <ui-form label-position="top"
      ><details>
        <summary>属性字段 · {{ model.properties?.length || 0 }}</summary>
        <ThingModelFields
          :rows="model.properties || []"
          properties
          @add="update(next => next.properties.push(field()))"
          @remove="index => update(next => next.properties.splice(index, 1))"
        />
      </details>
      <details v-for="group in operations" :key="group.kind">
        <summary>{{ group.label }} · {{ model[group.kind]?.length || 0 }}</summary>
        <p>{{ group.help }}</p>
        <section v-for="(operation, index) in model[group.kind] || []" :key="index" class="model-operation">
          <div class="operation-heading">
            <strong>{{ group.label }} {{ index + 1 }}</strong
            ><ui-button size="small" @click="update(next => next[group.kind].splice(index, 1))">移除{{ group.label }}</ui-button>
          </div>
          <div class="operation-grid">
            <ui-form-item :label="`${group.label}标识`" required><ui-input v-model="operation.identifier" maxlength="128" /></ui-form-item
            ><ui-form-item label="显示名称"><ui-input v-model="operation.name" /></ui-form-item>
          </div>
          <ThingModelFields
            :rows="operation.fields || []"
            @add="update(next => (next[group.kind][index].fields ||= []).push(field()))"
            @remove="fieldIndex => update(next => next[group.kind][index].fields.splice(fieldIndex, 1))"
          />
        </section>
        <ui-button size="small" :disabled="(model[group.kind]?.length || 0) >= 128" @click="addOperation(group.kind)"
          >添加{{ group.label }}</ui-button
        >
      </details></ui-form
    >
  </section>
</template>
<style scoped>
.thing-model-editor {
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid var(--border);
}
.thing-model-editor p {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
details {
  margin: var(--space-3) 0;
}
summary {
  cursor: pointer;
  margin-bottom: var(--space-3);
  font-weight: var(--font-weight-semibold);
}
.model-operation {
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  margin-bottom: var(--space-3);
}
.operation-heading {
  display: flex;
  gap: var(--space-3);
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--space-3);
}
.operation-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 767px) {
  .operation-grid {
    grid-template-columns: 1fr;
  }
}
</style>
