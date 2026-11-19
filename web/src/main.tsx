import React, { useCallback, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './gc-tokens.css'
import './styles.css'

type Status = { connected: boolean; message: string }
type Remotes = { remotes: string[] }
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
  const [source, setSource] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastAction, setLastAction] = useState('')

  const refresh = useCallback(async (key: string) => {
    setError('')
    setBusy(true)
    try {
      const s = await api<Status>('/api/v1/status', key)
      setStatus(s)
      const [shots, destinations] = await Promise.allSettled([
        api<unknown>('/api/v1/snapshots', key),
        api<Remotes>('/api/v1/remotes', key)
      ])
      setSnapshots(shots.status === 'fulfilled' && Array.isArray(shots.value) ? shots.value : [])
      setRemotes(destinations.status === 'fulfilled' ? destinations.value.remotes : [])
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

  return (
    <div className="layout">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand"><span className="brandmark" aria-hidden="true">◈</span><div>GoreeCloud <strong>Backup</strong><small>Self-hosted workspace</small></div></div>
        <nav aria-label="Sections">
          <a href="#overview" className="nav-current">Overview</a>
          <a href="#snapshots">Snapshots</a>
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
            <section className="panel" id="create">
              <div className="eyebrow">NEW RECOVERY POINT</div><h2>Create a snapshot</h2>
              <p className="subtle">Enter an existing subdirectory of the source root configured on the server. Symlink escapes and parent traversal are rejected.</p>
              <form onSubmit={create} className="create-form">
                <div><label htmlFor="source">Source directory (relative)</label><input id="source" value={source} onChange={e => setSource(e.target.value)} placeholder="Documents" required disabled={busy} /></div>
                <button type="submit" disabled={busy || !status?.connected}>{busy ? 'Working…' : 'Back up now'}</button>
              </form>
            </section>
            <section className="panel" id="destinations">
              <div className="eyebrow">STORAGE CONNECTIONS</div><h2>Rclone remotes</h2>
              {remotes.length ? <div className="chip-list">{remotes.map(name => <span className="chip" key={name}>{name}</span>)}</div> : <p className="subtle">No Rclone remotes are available. Configure destinations on the server.</p>}
              <p className="hint">Always use Kopia encryption before storing backups remotely. Remote configuration and synchronization are not yet managed from this interface.</p>
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
