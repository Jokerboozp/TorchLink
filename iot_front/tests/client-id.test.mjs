import test from 'node:test'
import assert from 'node:assert/strict'
import { createClientId } from '../src/clientId.js'

test('HTTP fallback preserves UUID v4 version and variant using random bytes', () => {
  let calls=0
  const provider={getRandomValues(bytes){calls++;return bytes.fill(255)}}
  assert.equal(createClientId(provider),'ffffffff-ffff-4fff-bfff-ffffffffffff')
  assert.equal(calls,1)
  const ids=new Set(Array.from({length:100},()=>createClientId({getRandomValues:bytes=>crypto.getRandomValues(bytes)})))
  assert.equal(ids.size,100)
  for(const id of ids) assert.match(id,/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
})

test('HTTPS keeps the browser native UUID implementation', () => {
  assert.equal(createClientId({randomUUID(){return 'native-id'},getRandomValues(){throw Error('unexpected fallback')}}),'native-id')
})
