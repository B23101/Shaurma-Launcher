// Перевірка TS-нормалізації handle на спільних векторах.
//   node --experimental-strip-types scripts/check-handle.mjs   (з каталогу frontend)
import assert from 'node:assert/strict'
import { normalizeHandle, parseFriendInput } from '../src/lib/friends/handle.ts'
import { HANDLE_VECTORS } from '../src/lib/friends/handle.vectors.ts'

for (const [i, o] of HANDLE_VECTORS) assert.equal(normalizeHandle(i), o, JSON.stringify(i))
const id = '0a1b2c3d-1111-4222-8333-444455556666'
assert.deepEqual(parseFriendInput(` ${id.toUpperCase()} `), { kind: 'id', id })
assert.deepEqual(parseFriendInput('@Bob_1'), { kind: 'handle', handle: 'bob_1' })
assert.deepEqual(parseFriendInput('deadbeef'), { kind: 'handle', handle: 'deadbeef' }) // без дефісів — handle
assert.equal(parseFriendInput('ab'), null)
assert.equal(parseFriendInput(''), null)
console.log('handle: ок')
