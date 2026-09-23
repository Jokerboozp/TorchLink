<script setup>
import { computed } from 'vue'
const props = defineProps({ rows: { type: Array, required: true }, parserType: String })
const emit = defineEmits(['change', 'add', 'remove'])
const modbus = computed(() => props.parserType?.startsWith('modbus_'))
const json = computed(() => props.parserType === 'configurable_json_parser')
const types = computed(() => json.value ? ['', 'number', 'integer', 'boolean', 'string', 'json'] : modbus.value
  ? ['bool', 'bits', 'uint16', 'int16', 'uint32', 'int32', 'float32', 'uint64', 'int64', 'float64', 'string']
  : ['uint8', 'int8', 'uint16', 'int16', 'uint32', 'int32', 'float32', 'hex', 'ascii'])
function changeType(row, value) {
  row[json.value || !modbus.value ? 'type' : 'dataType'] = value
  if (modbus.value && value !== 'string') row.registerCount = { bool: 1, bits: 1, uint16: 1, int16: 1, uint32: 2, int32: 2, float32: 2, uint64: 4, int64: 4, float64: 4 }[value]
  if (!json.value && !modbus.value && !['hex', 'ascii'].includes(value)) row.length = { uint8: 1, int8: 1, uint16: 2, int16: 2, uint32: 4, int32: 4, float32: 4 }[value]
  emit('change')
}
</script>

<template>
  <section class="mapping-editor" aria-label="编辑字段映射">
    <div class="section-toolbar"><strong>编辑字段映射</strong><el-button size="small" @click="emit('add')">添加字段</el-button></div>
    <p class="muted-text">对照原始字段填写映射，修改后请重新解析预览。{{ modbus ? '地址从 0 开始；展开每行可修改倍率、单位和字节序。' : '展开每行可修改倍率等参数。' }}</p>
    <el-table :data="rows" max-height="440" size="small" empty-text="暂无字段，请添加字段">
      <el-table-column type="expand">
        <template #default="{ row, $index }">
          <div class="mapping-options form-grid">
            <el-form-item label="倍率"><el-input-number v-model="row.scale" :aria-label="`第${$index + 1}行倍率`" @change="emit('change')" /></el-form-item>
            <template v-if="modbus">
              <el-form-item label="点位名称"><el-input v-model="row.name" @input="emit('change')" /></el-form-item>
              <el-form-item label="寄存器数量"><el-input-number v-model="row.registerCount" :min="1" :max="125" :precision="0" :disabled="row.dataType !== 'string'" @change="emit('change')" /></el-form-item>
              <el-form-item label="数值偏移"><el-input-number v-model="row.offset" @change="emit('change')" /></el-form-item>
              <el-form-item label="单位"><el-input v-model="row.unit" @input="emit('change')" /></el-form-item>
              <el-form-item label="字序"><el-input v-model="row.wordOrder" placeholder="ABCD / CDAB / ABCDEFGH" @input="emit('change')" /></el-form-item>
              <el-form-item label="位索引（可选）"><el-input-number v-model="row.bit" :min="0" :max="15" :precision="0" @change="emit('change')" /></el-form-item>
              <el-form-item label="轮询周期（秒）"><el-input-number v-model="row.pollIntervalSec" :min="1" :precision="0" @change="emit('change')" /></el-form-item>
              <el-form-item label="说明"><el-input v-model="row.description" @input="emit('change')" /></el-form-item>
            </template>
            <el-form-item v-if="!json" label="字节序"><el-select v-model="row[modbus ? 'byteOrder' : 'endian']" @change="emit('change')"><el-option label="大端（big）" value="big" /><el-option label="小端（little）" value="little" /></el-select></el-form-item>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="原始字段 / 来源" min-width="210"><template #default="{ row }"><span class="mapping-source">{{ row.source || '新增字段' }}</span></template></el-table-column>
      <el-table-column label="映射字段标识" min-width="170"><template #default="{ row, $index }"><el-input v-model="row[modbus ? 'identifier' : 'name']" :aria-label="`第${$index + 1}行字段标识`" placeholder="例如 temperature" @input="emit('change')" /></template></el-table-column>
      <el-table-column v-if="json" label="JSON 路径" min-width="220"><template #default="{ row, $index }"><el-input v-model="row.path" :aria-label="`第${$index + 1}行JSON路径`" placeholder="$.data.temperature" @input="emit('change')" /></template></el-table-column>
      <template v-else>
        <el-table-column v-if="modbus" label="功能码" min-width="170"><template #default="{ row }"><el-select v-model="row.functionCode" @change="emit('change')"><el-option v-for="(label, i) in ['01 读线圈', '02 读离散输入', '03 读保持寄存器', '04 读输入寄存器']" :key="i" :label="label" :value="i + 1" /></el-select></template></el-table-column>
        <el-table-column :label="modbus ? '地址（从 0 开始）' : '字节偏移'" min-width="165"><template #default="{ row, $index }"><el-input-number v-model="row[modbus ? 'address' : 'offset']" :aria-label="`第${$index + 1}行地址或偏移`" :min="0" :max="modbus ? 65535 : undefined" :precision="0" controls-position="right" @change="emit('change')" /></template></el-table-column>
        <el-table-column v-if="!modbus" label="字节长度" min-width="150"><template #default="{ row }"><el-input-number v-model="row.length" :min="1" :precision="0" controls-position="right" @change="emit('change')" /></template></el-table-column>
      </template>
      <el-table-column label="数据类型" min-width="150"><template #default="{ row, $index }"><el-select :model-value="row[modbus ? 'dataType' : 'type']" :aria-label="`第${$index + 1}行数据类型`" @update:model-value="changeType(row, $event)"><el-option v-for="type in types" :key="type" :value="type" :label="type || '保持原值'" /></el-select></template></el-table-column>
      <el-table-column label="操作" width="75"><template #default="{ $index }"><el-button link type="danger" :aria-label="`删除第${$index + 1}行字段`" @click="emit('remove', $index)">删除</el-button></template></el-table-column>
    </el-table>
  </section>
</template>

<style scoped>
.mapping-editor { min-width: 0; margin-bottom: 20px; }
.mapping-editor p { margin: 8px 0 12px; font-size: 13px; }
.mapping-editor :deep(.el-input-number) { width: 100%; }
.mapping-options { padding: 16px; max-width: 850px; }
.mapping-source { white-space: normal; overflow-wrap: anywhere; }
</style>
