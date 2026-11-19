import React, { useCallback, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './gc-tokens.css'
import './styles.css'

type Status = { connected: boolean; message: string }
type Remotes = { remotes: string[] }
type Capacity = { remote: string; quota: Partial<Record<'total'|'used'|'free'|'trashed'|'other'|'objects', number>>; note: string }
const formatCapacity = (amount: number, isObjects: boolean) => {
  if (!Number.isFinite(amount) || amount < 0) return 'Unavailable'
  if (isObjects) return Math.round(amount).toLocaleString()
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let size = amount
  let index = 0
  while (size >= 1024 && index < units.length - 1) { size /= 1024; index++ }
  return `${size.toFixed(index ? 1 : 0)} ${units[index]}`
}
type BackupInfo = { restoreEnabled: boolean }
type RestoreResult = { restored: boolean; snapshotId: string; relativePath: string; note: string }
type Verification = { verified: boolean; filesRead: number; bytesRead: number; note: string }
type ServerError = { error?: string }
const api = async <T,>(path: string, token: string, init: RequestInit = {}): Promise<T> => {
  const response = await fetch(path, {
    ...init,
    headers: { 'Authorization': `Bearer ${token}`, ...(init.body ? { 'Content-Type': 'application/json' } : {}) },
    cache: 'no-store'
  })
  const body = (await response.json()) as T & ServerError
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`)
  return body
}

function App() {
  // Credentials deliberately remain in process memory only; never localStorage/sessionStorage.
  const [draftToken, setDraftToken] = useState('')
  const [token, setToken] = useState('')
  const [status, setStatus] = useState<Status | null>(null)
  const [snapshots, setSnapshots] = useState<unknown[]>([])
  const [remotes, setRemotes] = useState<string[]>([])
  const [capacity, setCapacity] = useState<Capacity | null>(null)
  const [capacityError, setCapacityError] = useState('')
  const [capacityBusy, setCapacityBusy] = useState(false)
  const [source, setSource] = useState('')
  const [restoreEnabled, setRestoreEnabled] = useState(false)
  const [restoreId, setRestoreId] = useState('')
  const [restoreConsent, setRestoreConsent] = useState(false)
  const [lastRestore, setLastRestore] = useState<RestoreResult | null>(null)
  const [verifyConsent, setVerifyConsent] = useState(false)
  const [lastVerification, setLastVerification] = useState<Verification | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastAction, setLastAction] = useState('')

  const refresh = useCallback(async (key: string) => {
    setError('')
    setBusy(true)
    try {
      const s = await api<Status>('/api/v1/status', key)
      setStatus(s)
      const [shots, destinations, info] = await Promise.allSettled([
        api<unknown>('/api/v1/snapshots', key),
        api<Remotes>('/api/v1/remotes', key),
        api<BackupInfo>('/api/v1/info', key)
      ])
      setSnapshots(shots.status === 'fulfilled' && Array.isArray(shots.value) ? shots.value : [])
      setRemotes(destinations.status === 'fulfilled' ? destinations.value.remotes : [])
      setCapacity(null)
      setCapacityError('')
      setRestoreEnabled(info.status === 'fulfilled' && Boolean(info.value.restoreEnabled))
      if (shots.status === 'rejected') setLastAction('Snapshot list unavailable; check Kopia connection')
    } catch (cause) {
      setStatus(null)
      setError(cause instanceof Error ? cause.message : 'Connection failed')
    } finally { setBusy(false) }
  }, [])

  const connect = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const next = draftToken.trim()
    if (next.length < 32) { setError('Enter your configured 32+ character access token'); return }
    setToken(next)
    setDraftToken('')
    await refresh(next)
  }
  const create = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!source.trim()) { setError('Choose a directory below the configured source root'); return }
    setBusy(true)
    setError('')
    setLastAction('Running an encrypted Kopia snapshot. Do not close this tab.')
    try {
      await api('/api/v1/snapshots', token, { method: 'POST', body: JSON.stringify({ source: source.trim() }) })
      setLastAction('Snapshot command completed. Verify it in your repository before relying on it.')
      setSource('')
      await refresh(token)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Snapshot failed')
      setBusy(false)
    }
  }

  const verify = async () => {
    if (!verifyConsent || !status?.connected || busy) return
    setBusy(true)
    setError('')
    setLastAction('Reading and verifying all snapshot file contents. This can be slow and consume storage-provider bandwidth.')
    try {
      const result = await api<Verification>('/api/v1/verify', token, { method: 'POST' })
      setLastVerification(result)
      setLastAction(`Verified ${result.filesRead} archived files (${result.bytesRead.toLocaleString()} bytes). A restore drill is still necessary.`)
      setVerifyConsent(false)
    } catch (cause) {
      setLastVerification(null)
      setError(cause instanceof Error ? cause.message : 'Verification failed')
    } finally { setBusy(false) }
  }


  const restore = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!restoreEnabled || !restoreId || !restoreConsent || busy) return
    setBusy(true)
    setError('')
    setLastRestore(null)
    setLastAction('Restoring a snapshot into a private, new directory. The original source is not modified.')
    try {
      const result = await api<RestoreResult>('/api/v1/restore', token, {
        method: 'POST', body: JSON.stringify({ snapshotId: restoreId, acknowledge: true })
      })
      setLastRestore(result)
      setLastAction('A contained restore completed. Inspect recovered files and compare with expected data.')
      setRestoreConsent(false)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Restore failed')
    } finally { setBusy(false) }
  }

  const inspectRemote = async (remote: string) => {
    if (!token || capacityBusy || busy) return
    setCapacityBusy(true)
    setCapacity(null)
    setCapacityError('')
    try {
      const result = await api<Capacity>(`/api/v1/remotes/about?remote=${encodeURIComponent(remote)}`, token)
      setCapacity(result)
    } catch (cause) {
      setCapacityError(cause instanceof Error ? cause.message : 'Provider capacity is unavailable')
    } finally { setCapacityBusy(false) }
  }

  const snapshotIds = snapshots.map(item => {
    if (item && typeof item === 'object' && 'id' in item && typeof item.id === 'string') return item.id
    return ''
  }).filter(Boolean)

  return (
    <div className="layout">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand"><span className="brandmark" aria-hidden="true">◈</span><div>GoreeCloud <strong>Backup</strong><small>Self-hosted workspace</small></div></div>
        <nav aria-label="Sections">
          <a href="#overview" className="nav-current">Overview</a>
          <a href="#snapshots">Snapshots</a>
          <a href="#verification">Verify integrity</a>
          <a href="#restore">Restore workspace</a>
          <a href="#destinations">Destinations</a>
        </nav>
        <p className="sidebar-note">Local-first · No telemetry<br />Development preview v0.1.0</p>
      </aside>
      <main className="main" id="overview">
        <div className="eyebrow">PROTECT WHAT MATTERS</div>
        <header className="topline">
          <div><h1>Backup overview</h1><p className="subtle">Encrypted snapshots and storage connectivity in one workspace.</p></div>
          {token && <button className="secondary" onClick={() => refresh(token)} disabled={busy}>Refresh status</button>}
        </header>
        {!token ? (
          <section className="panel connect">
            <div className="panel-icon" aria-hidden="true">⌁</div>
            <h2>Connect to your backup service</h2>
            <p>Enter the access token configured on your self-hosted GoreeCloud Backup server. It is kept only in this tab's memory.</p>
            <form onSubmit={connect} className="connect-form">
              <label htmlFor="token">Access token</label>
              <input id="token" type="password" autoComplete="off" minLength={32} value={draftToken} onChange={e => setDraftToken(e.target.value)} placeholder="Your GCB_API_TOKEN" required />
              <button type="submit" disabled={busy}>{busy ? 'Connecting…' : 'Connect securely'}</button>
            </form>
          </section>
        ) : (
          <>
            <div className="metrics">
              <section className="metric"><span>Repository</span><strong>{status ? (status.connected ? 'Connected' : 'Unconfigured') : 'Checking…'}</strong><small>Kopia engine</small></section>
              <section className="metric"><span>Snapshots visible</span><strong>{snapshots.length}</strong><small>Current repository listing</small></section>
              <section className="metric"><span>Rclone remotes</span><strong>{remotes.length}</strong><small>Configured destinations</small></section>
            </div>
            <section className="panel" id="snapshots">
              <div className="section-head"><div><div className="eyebrow">RECOVERY POINTS</div><h2>Snapshots</h2></div><span className="muted-label">View only</span></div>
              {snapshots.length ? <ul className="snapshot-list">{snapshots.slice(0, 50).map((snapshot, index) => <li key={index}><span className="snapshot-icon" aria-hidden="true">▣</span><code>{JSON.stringify(snapshot).slice(0, 360)}</code></li>)}</ul>
                : <div className="empty"><span aria-hidden="true">▤</span><strong>No snapshots displayed</strong><p>Connect a Kopia repository and create your first protected snapshot.</p></div>}
            </section>
            <section className="panel" id="restore">
              <div className="eyebrow">SAFE RECOVERY</div><h2>Contained restore workspace</h2>
              <p className="subtle">Restore an existing snapshot to a new, private directory on the server. The destination is allocated automatically, separate from the configured source root; the original files are not overwritten.</p>
              {!restoreEnabled ? <p className="hint">Restoration is disabled until the server operator configures a separate, private GCB_RESTORE_ROOT directory.</p> :
                <form onSubmit={restore} className="restore-form">
                  <label htmlFor="restoreSnapshot">Recovery point</label>
                  <select id="restoreSnapshot" value={restoreId} onChange={e => setRestoreId(e.target.value)} disabled={busy || snapshotIds.length === 0} required>
                    <option value="">Choose a snapshot</option>
                    {snapshotIds.map(id => <option key={id} value={id}>{id}</option>)}
                  </select>
                  <label className="verify-consent"><input type="checkbox" checked={restoreConsent} onChange={e => setRestoreConsent(e.target.checked)} disabled={busy} /> I understand this operation may download cloud backup data, use server disk space, and must be inspected before recovering original files.</label>
                  <button type="submit" disabled={busy || !restoreConsent || !restoreId || !status?.connected}>{busy ? 'Working…' : 'Restore to new private workspace'}</button>
                </form>}
              {lastRestore && <p className="hint" role="status">Restore completed inside the operator-controlled restore root: <code>{lastRestore.relativePath}</code>. Verify its contents before use. No files were written to the original source.</p>}
            </section>
            <section className="panel" id="create">
              <div className="eyebrow">NEW RECOVERY POINT</div><h2>Create a snapshot</h2>
              <p className="subtle">Enter an existing subdirectory of the source root configured on the server. Symlink escapes and parent traversal are rejected.</p>
              <form onSubmit={create} className="create-form">
                <div><label htmlFor="source">Source directory (relative)</label><input id="source" value={source} onChange={e => setSource(e.target.value)} placeholder="Documents" required disabled={busy} /></div>
                <button type="submit" disabled={busy || !status?.connected}>{busy ? 'Working…' : 'Back up now'}</button>
              </form>
            </section>
            <section className="panel" id="verification">
              <div className="eyebrow">INTEGRITY CHECK</div><h2>Verify archived data</h2>
              <p className="subtle">Ask Kopia to read and verify 100% of archived file contents. This may take a long time and incur provider transfer charges. It is not a substitute for an independent restoration drill.</p>
              <label className="verify-consent"><input type="checkbox" checked={verifyConsent} onChange={e => setVerifyConsent(e.target.checked)} disabled={busy} /> I understand this operation may read all cloud-stored backup data and incur costs.</label>
              <div className="verification-action"><button type="button" onClick={verify} disabled={busy || !verifyConsent || !status?.connected}>{busy ? 'Operation in progress…' : 'Verify all snapshot files'}</button></div>
              {lastVerification && <p className="hint" role="status">Most recent verification in this browser session: {lastVerification.filesRead} files, {lastVerification.bytesRead.toLocaleString()} bytes read. Successful verification does not prove restore readiness.</p>}
            </section>
            <section className="panel" id="destinations">
              <div className="eyebrow">STORAGE CONNECTIONS</div><h2>Rclone remotes</h2>
              {remotes.length ? <div className="remote-list">{remotes.map(name => <div className="remote-line" key={name}><span className="chip">{name}</span><button type="button" className="secondary" disabled={busy || capacityBusy} onClick={() => inspectRemote(name)}>Check capacity</button></div>)}</div> : <p className="subtle">No Rclone remotes are available. Configure destinations on the server.</p>}
              {capacityBusy && <p role="status" className="hint">Contacting the selected remote provider…</p>}
              {capacityError && <p role="alert" className="error">{capacityError}</p>}
              {capacity && <div className="remote-capacity" role="status"><strong>Provider-reported capacity for {capacity.remote}</strong><dl>{Object.entries(capacity.quota).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{formatCapacity(value as number, key === 'objects')}</dd></div>)}</dl><p className="hint">{capacity.note}</p></div>}
              <p className="hint">Capacity checks are opt-in and may contact a remote provider. Some Rclone backends do not report quota. Always encrypt using Kopia before storing backups remotely; destination setup and transfers are not managed from this interface.</p>
            </section>
          </>
        )}
        <div role="status" aria-live="polite" className="statusline">{lastAction}</div>
        {error && <div role="alert" className="error">{error}</div>}
        <footer>GoreeCloud UI · GoreeShield privacy · Wardveil security · Mesh integration planned</footer>
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(<React.StrictMode><App /></React.StrictMode>)
