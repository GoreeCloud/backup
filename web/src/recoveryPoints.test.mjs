import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { summarizeSnapshots } from './recoveryPoints.ts'

test('renders newest recovery points first without exposing private source or host metadata', () => {
  const raw = [
    { id: 'older-0001', startTime: '2026-11-15T10:00:00Z',
      source: { path: '/home/private-user/Customer Photos', host: 'secret-host', user: 'secret-user' },
      rootEntry: { obj: 'secret-object' }, tags: { sensitive: 'secret-label' } },
    { id: 'newer-0002', startTime: '2026-11-19T10:00:00Z',
      source: { path: 'C:\\Users\\private-user\\Grüße-東京' }, host: 'private-host' }
  ]
  const points = summarizeSnapshots(raw)
  assert.deepEqual(points.map(item => item.id), ['newer-0002', 'older-0001'])
  assert.equal(points[0].label, 'Grüße-東京')
  assert.equal(points[1].label, 'Customer Photos')
  assert.equal(points[0].dateTime, '2026-11-19T10:00:00.000Z')
  for (const sensitive of ['private-user', 'secret-host', 'secret-user', 'secret-object', 'secret-label', '/home/', 'C:\\Users']) {
    assert.equal(JSON.stringify(points).includes(sensitive), false)
  }
})

test('deduplicates IDs and excludes traversal-like and malformed IDs', () => {
  const input = [
    { id: 'valid-0001', source: { path: 'Projects' } },
    { id: 'valid-0001', source: { path: 'duplicated' } },
    { id: '../escape', source: { path: 'invalid' } },
    { id: 'x', source: { path: 'invalid' } },
    { id: null },
    { id: 'valid_0002' },
    { id: 'valid-0003', source: { path: '\n' } }
  ]
  const points = summarizeSnapshots(input)
  assert.equal(points.length, 3)
  assert.equal(points[0].label, 'Projects')
  assert.equal(points[1].label, 'Protected backup')
  assert.equal(points[2].label, 'Protected backup')
  assert.equal(points[0].dateTime, null)
  assert.equal(points[0].dateLabel, 'Date unavailable')
})

test('bounded ordering, stable ties and no full paths in a displayed summary', () => {
  const input = Array.from({ length: 150 }, (_, index) => ({
    id: 'point-' + String(index).padStart(5, '0'),
    startTime: '2026-11-19T05:00:00Z',
    source: { path: '/private/backups/collection' + index }
  }))
  assert.equal(summarizeSnapshots(input).length, 50)
  assert.deepEqual(summarizeSnapshots(input, 2).map(row => row.id), ['point-00000', 'point-00001'])
  assert.deepEqual(summarizeSnapshots(input, 101), [])
  assert.deepEqual(summarizeSnapshots(input, -1), [])
  assert.deepEqual(summarizeSnapshots({}), [])
})

test('unsafe metadata never becomes a displayed path or executable markup', () => {
  const data = [
    { id: 'point-0001', startTime: 'yesterday', source: { path: 'C:' } },
    { id: 'point-0002', startTime: '2026-11-19T11:00:00Z',
      source: { path: '/server/private/\x00Hidden\\Sensitive' } },
    { id: 'point-0003', startTime: '2026-11-19T11:00:00Z',
      source: { path: '/path/<script>alert("x")</script>' } },
    { id: 'point-0004', startTime: '2026-02-30T11:00:00Z', source: { path: 'Database' } },
    { id: 'point-0005', source: { path: '/private/\u202eInvisible-label' } }
  ]
  const points = summarizeSnapshots(data)
  assert.equal(points.find(x => x.id === 'point-0001').label, 'Protected backup')
  assert.equal(points.find(x => x.id === 'point-0002').label, 'Sensitive')
  assert.equal(points.find(x => x.id === 'point-0003').label, 'Protected backup')
  assert.equal(points.find(x => x.id === 'point-0001').dateTime, null)
  assert.equal(points.find(x => x.id === 'point-0004').dateTime, null)
  assert.equal(points.find(x => x.id === 'point-0005').label, 'Protected backup')
})


test('stylesheet stays CSS-only and retains the purpose-built backup timeline', () => {
  const css = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')
  assert.match(css, /\.layout\s*\{/)
  assert.match(css, /\.recovery-timeline\s*\{/)
  assert.match(css, /var\(--gc-brand\)/)
  assert.doesNotMatch(css, /^\s*(?:import\s+React|function\s+App|type\s+Status|createRoot\()/m)
})
