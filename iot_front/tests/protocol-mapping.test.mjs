import test from 'node:test'
import assert from 'node:assert/strict'
import { reactive } from 'vue'
import { mappingRows, mappingConfig } from '../src/protocolMapping.js'

test('JSON object mappings retain defaults, type conversions and unrelated settings', () => {
  const config = reactive({ properties: { temperature: { path: '$.t', type: 'number', scale: 0.1, default: 0 } }, timestampPath: '$.time', tags: { room: '$.room' } })
  const rows = mappingRows(config, 'configurable_json_parser')
  rows[0].name = 'heat'; rows[0].path = '$.data.t'
  const result = mappingConfig(config, 'configurable_json_parser', rows)
  assert.deepEqual(result.properties.heat, { path: '$.data.t', type: 'number', scale: 0.1, default: 0 })
  assert.equal(result.timestampPath, '$.time'); assert.deepEqual(result.tags, { room: '$.room' })
  assert.equal(config.properties.temperature.path, '$.t')
})

test('Modbus edits preserve alarm mappings and do not mutate the original point table', () => {
  const config = { points: [{ identifier: 'temp', name: '温度', functionCode: 3, address: 10, registerCount: 1, dataType: 'uint16', alarmMapping: { 1: 'FIRE' }, scale: 0.1 }], blocks: [{ startAddress: 10 }] }
  const rows = mappingRows(config, 'modbus_tcp_parser_v2')
  rows[0].address = 12; rows[0].identifier = 'temperature'
  const result = mappingConfig(config, 'modbus_tcp_parser_v2', rows)
  assert.equal(result.points[0].address, 12); assert.equal(result.points[0].identifier, 'temperature')
  assert.deepEqual(result.points[0].alarmMapping, { 1: 'FIRE' })
  assert.equal(Object.hasOwn(result.points[0], 'source'), false)
  assert.equal(config.points[0].address, 10)
})

test('HEX edits retain framing and checksum settings and reject invalid offsets', () => {
  const config = { startHex: 'AA', checksum: 'sum8', fields: [{ name: 'temperature', offset: 1, length: 2, type: 'uint16', endian: 'big' }] }
  const rows = mappingRows(config, 'configurable_hex_parser'); rows[0].offset = 3
  const result = mappingConfig(config, 'configurable_hex_parser', rows)
  assert.equal(result.fields[0].offset, 3); assert.equal(result.startHex, 'AA'); assert.equal(result.checksum, 'sum8')
  rows[0].offset = -1
  assert.throws(() => mappingConfig(config, 'configurable_hex_parser', rows), /无效/)
})

test('legacy JSON mapping key survives and deleting every field cannot enable implicit passthrough', () => {
  const config = { propertyMappings: { temperature: '$.t' } }
  assert.deepEqual(mappingConfig(config, 'configurable_json_parser', mappingRows(config, 'configurable_json_parser')), config)
  assert.throws(() => mappingConfig(config, 'configurable_json_parser', []), /至少保留/)
})
