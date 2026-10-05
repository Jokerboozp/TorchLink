import test from 'node:test'
import assert from 'node:assert/strict'
import { pointFraction, siteTree } from '../src/sites.js'

test('site tree nests buildings and floors and counts device-level points per unit', () => {
  const tree = siteTree({
    units: [
      { id: 'u1', name: '单位' },
      { id: 'u2', name: '空单位' }
    ],
    buildings: [{ id: 'b1', unitId: 'u1', name: '1 号楼' }],
    floors: [
      { id: 'f1', buildingId: 'b1', name: '1F', level: 1 },
      { id: 'f3', buildingId: 'b1', name: '3F', level: 3 },
      { id: 'b', buildingId: 'b1', name: 'B1', level: -1 }
    ],
    points: [
      { deviceId: 'panel', unitId: 'u1' },
      { deviceId: 'panel', componentId: 'c1', unitId: 'u1' },
      { deviceId: 'pump', unitId: 'u1' }
    ]
  })
  assert.equal(tree[0].deviceCount, 2)
  assert.deepEqual(
    tree[0].buildings[0].floors.map(f => f.name),
    ['3F', '1F', 'B1']
  )
  assert.equal(tree[1].deviceCount, 0)
})

test('plan positions are fractions of the image clamped to its bounds', () => {
  const rect = { left: 100, top: 50, width: 400, height: 200 }
  assert.deepEqual(pointFraction({ clientX: 300, clientY: 100 }, rect), { x: 0.5, y: 0.25 })
  assert.deepEqual(pointFraction({ clientX: 50, clientY: 400 }, rect), { x: 0, y: 1 })
})

test('saving a listed point drops display-only fields', async () => {
  const { sitePayload } = await import('../src/sites.js')
  assert.deepEqual(sitePayload({ id: 'p', deviceId: 'd', deviceName: '名称', x: 0.1 }), { id: 'p', deviceId: 'd', x: 0.1 })
})
