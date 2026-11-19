/**
 * Convert raw Kopia snapshot JSON into a privacy-conscious, backup-specific
 * recovery timeline. Never show server host/user fields or full source paths.
 * This is a view model, not a claim that a snapshot was verified/restored.
 */
export type RecoveryPoint = {
  id: string
  label: string
  dateTime: string | null
  dateLabel: string
  timeLabel: string
}

const validSnapshotID = (id: unknown): id is string =>
  typeof id === 'string' && /^[a-zA-Z0-9_-]{8,128}$/.test(id)

const recoveryLabel = (path: unknown): string => {
  if (typeof path !== 'string' || path.length > 4096) return 'Protected backup'
  const leaf = path.replace(/\\/g, '/').replace(/\/+$/g, '').split('/').pop() || ''
  // Reject label tricks; never render the host, username, parent path or
  // markup-looking source components even as inert text.
  if (/[\u202A-\u202E\u2066-\u2069<>]/u.test(leaf)) return 'Protected backup'
  const safe = Array.from(leaf.normalize('NFC')).filter(char => {
    const code = char.codePointAt(0) || 0
    return code > 31 && code !== 127 && (code < 128 || code > 159)
  }).join('').trim()
  if (!safe || safe === '.' || safe === '..' || /^[a-zA-Z]:$/.test(safe)) return 'Protected backup'
  return Array.from(safe).slice(0, 80).join('')
}

const parseRecoveryTime = (value: unknown): Date | null => {
  if (typeof value !== 'string') return null
  const parts = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.exec(value)
  if (!parts) return null
  const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number)
  // Reject impossible calendars and clock times rather than silently
  // normalizing February 30 or hour 25 into another displayed recovery date.
  const calendar = new Date(0)
  calendar.setUTCFullYear(year, month - 1, day)
  calendar.setUTCHours(hour, minute, second, 0)
  if (calendar.getUTCFullYear() !== year || calendar.getUTCMonth() !== month - 1 ||
      calendar.getUTCDate() !== day || calendar.getUTCHours() !== hour ||
      calendar.getUTCMinutes() !== minute || calendar.getUTCSeconds() !== second) return null
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date : null
}

export const summarizeSnapshots = (input: unknown, limit = 50): RecoveryPoint[] => {
  if (!Array.isArray(input) || !Number.isInteger(limit) || limit < 1 || limit > 100) return []
  const seen = new Set<string>()
  const prepared: Array<{ id: string; label: string; date: Date | null; sortTime: number; order: number }> = []
  input.forEach((value, order) => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return
    const entry = value as Record<string, unknown>
    if (!validSnapshotID(entry.id) || seen.has(entry.id)) return
    seen.add(entry.id)
    const source = entry.source && typeof entry.source === 'object' && !Array.isArray(entry.source)
      ? entry.source as Record<string, unknown> : {}
    const date = parseRecoveryTime(entry.startTime)
    prepared.push({
      id: entry.id,
      label: recoveryLabel(source.path),
      date,
      sortTime: date?.getTime() ?? Number.NEGATIVE_INFINITY,
      order
    })
  })
  prepared.sort((a, b) => b.sortTime - a.sortTime || a.order - b.order)
  const latest = prepared.slice(0, limit)
  if (!latest.length) return []
  const dates = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' })
  const times = new Intl.DateTimeFormat(undefined, { timeStyle: 'short' })
  return latest.map(point => ({
    id: point.id,
    label: point.label,
    dateTime: point.date?.toISOString() ?? null,
    dateLabel: point.date ? dates.format(point.date) : 'Date unavailable',
    timeLabel: point.date ? times.format(point.date) : 'Time unavailable'
  }))
}
