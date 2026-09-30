import assert from 'node:assert/strict'
import test from 'node:test'
import { evidenceChart, evidenceRows, findingRows, metricRows, profileDraft, profilePayload, qualityChartSamples, qualityTime, ratioLabel, recordBody, runIsActive, timeMs } from '../src/quality/helpers.js'

const timestamp = Date.parse('2026-10-01T00:00:00Z')
const draft = () => ({ ...profileDraft(), deviceIds: ['b', 'a', 'a'], attributeId: ' pressure ', effectiveFrom: timestamp, scheduleAnchor: timestamp - 3600e3, periodSeconds: 10, toleranceSeconds: 2 })

test('unknown and zero denominators never render invented passing percentages', () => {
  assert.equal(ratioLabel({ state: 'unknown', denominator: 10, numerator: 0, value: 0 }), '证据不足')
  assert.equal(ratioLabel({ state: 'not_applicable', denominator: 0, numerator: 0 }), '不适用')
  assert.equal(ratioLabel({ state: 'partial', denominator: 0, numerator: 0 }), '证据不足')
  assert.equal(ratioLabel({ state: 'assessed', denominator: 4, numerator: 1, value: .25 }), '25.0%')
  assert.equal(ratioLabel({ state: 'partial', denominator: 4, numerator: 1, value: .25 }), '25.0% · 部分数据')
  assert.equal(ratioLabel({ state: 'assessed', denominator: 4, numerator: 1, value: null }), '证据不足')
})

test('profile request keeps millisecond units and preserves a fixed anchor', () => {
  const form = draft(), before = structuredClone(form)
  const result = profilePayload(form)
  assert.deepEqual(result.deviceIds, ['a', 'b'])
  assert.equal(result.body.attributeId, 'pressure')
  assert.equal(result.body.effectiveFrom, timestamp)
  assert.equal(result.body.scheduleAnchor, timestamp - 3600e3)
  assert.equal(result.body.periodMs, 10000)
  assert.equal(result.body.toleranceMs, 2000)
  assert.equal(result.body.minimum, undefined)
  assert.equal(result.body.epsilon, undefined)
  assert.equal(result.body.minimumSamples, 30)
  assert.deepEqual(form, before)
})

test('physical thresholds are not guessed for a new profile', () => {
  const value = profileDraft()
  assert.equal(value.periodSeconds, null)
  assert.equal(value.toleranceSeconds, null)
  assert.equal(value.scheduleAnchor, null)
  assert.equal(value.epsilon, null)
  assert.equal(value.minimum, null)
  assert.equal(value.maximum, null)
  assert.equal(value.maxRate, null)
  assert.equal(value.unitConfirmed, false)
  assert.equal(value.rangeConfirmed, false)
  assert.throws(() => profilePayload(value), /选择设备/)
})

test('periodic tolerance rejects half-period, above-half, negative and finer-than-ms values', () => {
  for (const toleranceSeconds of [5, 6, -1, .0001]) assert.throws(() => profilePayload({ ...draft(), toleranceSeconds }))
  assert.equal(profilePayload({ ...draft(), toleranceSeconds: 4.999 }).body.toleranceMs, 4999)
  assert.throws(() => profilePayload({ ...draft(), periodSeconds: 0 }))
})

test('event profiles omit arbitrary cadence while keeping explicit stable-gap policy', () => {
  const result = profilePayload({ ...draft(), mode: 'event', scheduleAnchor: null, periodSeconds: null, toleranceSeconds: null, maxSequenceGapSeconds: 60 })
  assert.equal(result.body.scheduleAnchor, undefined)
  assert.equal(result.body.periodMs, undefined)
  assert.equal(result.body.toleranceMs, undefined)
  assert.equal(result.body.maxSequenceGapMs, 60000)
})

test('confirmed unit and range, finite values and protected minimum are validated before write', () => {
  assert.throws(() => profilePayload({ ...draft(), rangeConfirmed: true, minimum: 0, maximum: 10 }), /确认单位/)
  assert.throws(() => profilePayload({ ...draft(), rangeConfirmed: true, unitConfirmed: true, minimum: 10, maximum: 0 }), /下限/)
  for (const field of ['epsilon', 'minimum', 'maximum', 'maxRate', 'madMultiplier', 'absoluteDeviation']) assert.throws(() => profilePayload({ ...draft(), [field]: Infinity }))
  assert.throws(() => profilePayload({ ...draft(), minimumSamples: 29 }), /不能低于 30/)
  assert.throws(() => profilePayload({ ...draft(), minimumSamples: 30.5 }), /不能低于 30/)
  assert.throws(() => profilePayload({ ...draft(), epsilon: 0 }), /大于 0/)
  assert.throws(() => profilePayload({ ...draft(), futureToleranceSeconds: -1 }))
})

test('CUSUM and quantile configuration are explicit, versioned and finite', () => {
  assert.throws(() => profilePayload({ ...draft(), cusumEnabled: true, cusumVersion: '', cusumAllowance: .1, cusumThreshold: 1 }))
  assert.throws(() => profilePayload({ ...draft(), cusumEnabled: true, cusumVersion: '1', cusumAllowance: -.1, cusumThreshold: 1 }))
  const result = profilePayload({ ...draft(), cusumEnabled: true, cusumVersion: 'k1', cusumAllowance: .1, cusumThreshold: 1 })
  assert.deepEqual(result.body.cusum, { version: 'k1', allowance: .1, threshold: 1 })
  assert.throws(() => profilePayload({ ...draft(), deviationMethod: 'quantile', quantileLower: .9, quantileUpper: .1 }))
})

test('configuration envelopes preserve revision identity and normalize scope', () => {
  const result = recordBody({ id: 'immutable-revision', resourceId: 'pressure-profile', version: 3, scope: 'SHARED', deviceIds: ['a'], body: { attributeId: 'pressure', mode: 'event', effectiveFrom: timestamp } })
  assert.equal(result.revisionId, 'immutable-revision')
  assert.equal(result.resourceId, 'pressure-profile')
  assert.equal(result.revisionVersion, 3)
  const form = profileDraft(result)
  assert.equal(form.expectedVersion, 3)
  assert.equal(form.scope, 'shared')
  assert.deepEqual(form.deviceIds, ['a'])
})

test('source and algorithm metric envelopes retain device, attribute and parse denominators', () => {
  const metric = { id: 'm', profileId: 'p', format: { state: 'assessed', numerator: 1, denominator: 3 }, window: { start: timestamp, end: timestamp + 10000 } }
  const result = metricRows([{ id: 'series-output', deviceId: 'visible', body: { attributeId: 'pressure', state: 'partial', metrics: [metric], parse: { archived: 8 }, limitations: ['archive expired'] } }])
  assert.equal(result.length, 1)
  assert.equal(result[0].deviceId, 'visible')
  assert.equal(result[0].attributeId, 'pressure')
  assert.equal(result[0].resultState, 'partial')
  assert.equal(result[0].parse.archived, 8)
  assert.equal(result[0].format.denominator, 3)
  const finding = findingRows([{ deviceId: 'visible', body: { id: 'f', metricId: 'm', kind: 'long_stability', attributeId: 'pressure' } }])[0]
  assert.equal(finding.attributeId, 'pressure')
  assert.equal(finding.metricId, 'm')
})

test('evidence retains separate clocks and identifies frozen versus current availability', () => {
  const row = evidenceRows([{ id: 'e', deviceId: 'visible', originalAvailability: 'AVAILABLE_AT_FREEZE', summary: { eventAt: timestamp, receivedAt: timestamp + 1000, availableAt: timestamp + 5000, value: '2026-10-01T00:00:00Z' } }])[0]
  assert.equal(row.availability, 'available_at_freeze')
  assert.equal(row.eventAt, timestamp)
  assert.equal(row.receivedAt, timestamp + 1000)
  assert.equal(row.availableAt, timestamp + 5000)
  assert.equal(row.value, '2026-10-01T00:00:00Z')
  assert.equal(timeMs('0001-01-01T00:00:00Z'), null)
  assert.equal(qualityTime(0), '未知')
  assert.equal(timeMs('2026-10-01T00:00:00Z'), timestamp)
})

test('chart points are sorted, finite, tied deterministically and do not become statistical denominators', () => {
  const source = [{ id: 'b', messageId: 'b', eventAt: timestamp + 1000, value: 3 }, { id: 'a', messageId: 'a', eventAt: timestamp, value: 1 }, { id: 'c', messageId: 'c', eventAt: timestamp, value: 2 }, { id: 'bad', eventAt: timestamp + 2000, value: Infinity }, { id: 'bool', eventAt: timestamp + 3000, value: true }]
  const before = structuredClone(source)
  const value = evidenceChart(source, 1.5, 10)
  assert.deepEqual(value.times, [timestamp, timestamp + 1000])
  assert.deepEqual(value.series.map(row => row.values), [[1, 3], [1.5, 1.5], [10, 10]])
  assert.equal(value.collapsedCount, 1)
  assert.deepEqual(source, before)
  assert.equal(source.length, 5)
})

test('only persistent fact states are treated as running', () => {
  for (const status of ['QUEUED', 'PREPARING', 'RUNNING']) assert.equal(runIsActive({ status }), true)
  for (const status of ['SUCCEEDED', 'PARTIAL', 'FAILED', 'CANCELLED']) assert.equal(runIsActive({ status }), false)
})

test('a clock switch keeps late reception and visibility of the original event members', () => {
  const items = [
    { id: 'late', eventAt: timestamp + 1000, receivedAt: timestamp + 120000, availableAt: timestamp + 3600000, value: 42 },
    { id: 'padding', eventAt: timestamp - 1000, receivedAt: timestamp + 100, value: 41 },
    { id: 'unknown', eventAt: timestamp + 2000, receivedAt: 0, availableAt: 0, value: 43 },
  ]
  const window = { start: timestamp, end: timestamp + 10000 }
  const before = structuredClone(items)
  assert.deepEqual(qualityChartSamples(items, 'receivedAt', window).map(row => [row.id, row.eventAt]), [['late', timestamp + 120000]])
  assert.deepEqual(qualityChartSamples(items, 'availableAt', window).map(row => [row.id, row.eventAt]), [['late', timestamp + 3600000]])
  assert.deepEqual(qualityChartSamples(items, 'eventAt', window).map(row => row.id), ['late', 'unknown'])
  assert.deepEqual(items, before)
})
