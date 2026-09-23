import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { reactive } from 'vue' /* 引入当前代码需要的依赖。 */
import { mappingRows, mappingConfig } from '../src/protocolMapping.js' /* 引入当前代码需要的依赖。 */

test('JSON object mappings retain defaults, type conversions and unrelated settings', () => { /* 执行当前语句并推进处理流程。 */
  const config = reactive({ properties: { temperature: { path: '$.t', type: 'number', scale: 0.1, default: 0 } }, timestampPath: '$.time', tags: { room: '$.room' } }) /* 声明 config。 */
  const rows = mappingRows(config, 'configurable_json_parser') /* 声明 rows。 */
  rows[0].name = 'heat'; rows[0].path = '$.data.t' /* 更新 rows[0].name 的值。 */
  const result = mappingConfig(config, 'configurable_json_parser', rows) /* 声明 result。 */
  assert.deepEqual(result.properties.heat, { path: '$.data.t', type: 'number', scale: 0.1, default: 0 }) /* 验证实际结果符合预期。 */
  assert.equal(result.timestampPath, '$.time'); assert.deepEqual(result.tags, { room: '$.room' }) /* 验证实际结果符合预期。 */
  assert.equal(config.properties.temperature.path, '$.t') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('Modbus edits preserve alarm mappings and do not mutate the original point table', () => { /* 执行当前语句并推进处理流程。 */
  const config = { points: [{ identifier: 'temp', name: '温度', functionCode: 3, address: 10, registerCount: 1, dataType: 'uint16', alarmMapping: { 1: 'FIRE' }, scale: 0.1 }], blocks: [{ startAddress: 10 }] } /* 声明 config。 */
  const rows = mappingRows(config, 'modbus_tcp_parser_v2') /* 声明 rows。 */
  rows[0].address = 12; rows[0].identifier = 'temperature' /* 更新 rows[0].address 的值。 */
  const result = mappingConfig(config, 'modbus_tcp_parser_v2', rows) /* 声明 result。 */
  assert.equal(result.points[0].address, 12); assert.equal(result.points[0].identifier, 'temperature') /* 验证实际结果符合预期。 */
  assert.deepEqual(result.points[0].alarmMapping, { 1: 'FIRE' }) /* 验证实际结果符合预期。 */
  assert.equal(Object.hasOwn(result.points[0], 'source'), false) /* 验证实际结果符合预期。 */
  assert.equal(config.points[0].address, 10) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('HEX edits retain framing and checksum settings and reject invalid offsets', () => { /* 执行当前语句并推进处理流程。 */
  const config = { startHex: 'AA', checksum: 'sum8', fields: [{ name: 'temperature', offset: 1, length: 2, type: 'uint16', endian: 'big' }] } /* 声明 config。 */
  const rows = mappingRows(config, 'configurable_hex_parser'); rows[0].offset = 3 /* 声明 rows。 */
  const result = mappingConfig(config, 'configurable_hex_parser', rows) /* 声明 result。 */
  assert.equal(result.fields[0].offset, 3); assert.equal(result.startHex, 'AA'); assert.equal(result.checksum, 'sum8') /* 验证实际结果符合预期。 */
  rows[0].offset = -1 /* 更新 rows[0].offset 的值。 */
  assert.throws(() => mappingConfig(config, 'configurable_hex_parser', rows), /无效/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('legacy JSON mapping key survives and deleting every field cannot enable implicit passthrough', () => { /* 执行当前语句并推进处理流程。 */
  const config = { propertyMappings: { temperature: '$.t' } } /* 声明 config。 */
  assert.deepEqual(mappingConfig(config, 'configurable_json_parser', mappingRows(config, 'configurable_json_parser')), config) /* 验证实际结果符合预期。 */
  assert.throws(() => mappingConfig(config, 'configurable_json_parser', []), /至少保留/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
