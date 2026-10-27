// Перевірка TS-нормалізації біо/кольору на спільних векторах з Go.
//   node --experimental-strip-types scripts/check-profile.mjs   (з каталогу frontend)
import assert from 'node:assert/strict'
import { normalizeBio, safeColor, safeBio, shortId, bioLength } from '../src/lib/friends/profileStyle.ts'
import { BIO_VECTORS } from '../src/lib/friends/profileStyle.vectors.ts'

for (const [name, input, out, ok] of BIO_VECTORS) {
  const r = normalizeBio(input)
  assert.equal(r.ok, ok, name)
  assert.equal(r.text, out, name)
}
assert.equal(bioLength('😀😀я'), 3)
assert.equal(safeBio('x'.repeat(500)), '')
assert.equal(safeBio('  ok\n'), 'ok')
assert.equal(safeColor('#FF8800'), '#ff8800')
for (const bad of ['red', '#fff', 'url(x)', '#ff8800;x', 'var(--a)', '']) assert.equal(safeColor(bad), '', bad)
assert.equal(shortId('0a1b2c3d-1111-4222-8333-444455556666'), '0a1b2c3d…6666')
console.log('profile: ок')
