import React, { useCallback, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './gc-tokens.css'
import './styles.css'
import { summarizeSnapshots } from './recoveryPoints'
import { readAPIJSON } from './apiResponse'

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
type BackupInfo = {
  restoreEnabled: boolean; mobileUploadEnabled: boolean; readOnly: boolean;
  canSnapshot: boolean; canVerify: boolean; canInspectRemote: boolean
}
type SelectedFileResult = { backedUp: boolean; name: string; bytes: number; sha256: string; note: string }
const maxSelectedBytes = 32 * 1024 * 1024
const isTrustedOrigin = () => window.location.protocol === 'https:' ||
  (window.location.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]', '::1'].includes(window.location.hostname))
const toBase64Url = (input: string) => {
  const bytes = new TextEncoder().encode(input)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
const selectedFileSHA256 = async (file: File) => {
  if (!crypto.subtle) throw new Error('A secure browser context is required for file integrity verification')
  const checksum = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return Array.from(new Uint8Array(checksum), byte => byte.toString(16).padStart(2, '0')).join('')
}
const validSelectedFile = (file: File) => {
  const name = file.name
  const bytes = new TextEncoder().encode(name)
  return file.size > 0 && file.size <= maxSelectedBytes &&
    bytes.length > 0 && bytes.length <= 160 && name.trim() === name &&
    name !== '.' && name !== '..' && !/[\x00-\x1F\x7F-\x9F\/\\]/u.test(name)
}
type RestoreResult = { restored: boolean; snapshotId: string; relativePath: string; note: string }
type Verification = { verified: boolean; filesRead: number; bytesRead: number; note: string }
const api = async <T,>(path: string, token: string, init: RequestInit = {}): Promise<T> => {
  const response = await fetch(path, {
    ...init,
    headers: { 'Authorization': `Bearer ${token}`, ...(init.body ? { 'Content-Type': 'application/json' } : {}) },
    cache: 'no-store',
    redirect: 'error'
  })
  return readAPIJSON<T>(response)
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
  const [readOnly, setReadOnly] = useState(true)
  const [canSnapshot, setCanSnapshot] = useState(false)
  const [canVerify, setCanVerify] = useState(false)
  const [canInspectRemote, setCanInspectRemote] = useState(false)
  const [selectedBackupEnabled, setSelectedBackupEnabled] = useState(false)
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [selectedFileConsent, setSelectedFileConsent] = useState(false)
  const fileInput = useRef<HTMLInputElement | null>(null)
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
      const capabilities = info.status === 'fulfilled' ? info.value : null
      // An unavailable capability response fails closed for privileged UI controls.
      setReadOnly(capabilities?.readOnly ?? true)
      setCanSnapshot(Boolean(capabilities?.canSnapshot))
      setCanVerify(Boolean(capabilities?.canVerify))
      setCanInspectRemote(Boolean(capabilities?.canInspectRemote))
      setRestoreEnabled(Boolean(capabilities?.restoreEnabled))
      setSelectedBackupEnabled(Boolean(capabilities?.mobileUploadEnabled))
      if (shots.status === 'rejected') setLastAction('Snapshot list unavailable; check Kopia connection')
    } catch (cause) {
      setStatus(null)
      setError(cause instanceof Error ? cause.message : 'Connection failed')
    } finally { setBusy(false) }
  }, [])

  const connect = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const next = draftToken.trim()
    if (!isTrustedOrigin()) { setError('Use HTTPS or numeric local loopback before entering a bearer token'); return }
    if (next.length < 32) { setError('Enter your configured 32+ character access token'); return }
    setToken(next)
    setDraftToken('')
    await refresh(next)
  }
  const create = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!canSnapshot) return
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
    if (!canVerify || !verifyConsent || !status?.connected || busy) return
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
    if (!token || !canInspectRemote || capacityBusy || busy) return
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

  const backupSelectedFile = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!selectedBackupEnabled || busy || !status?.connected || !selectedFileConsent || !selectedFile) return
    if (!isTrustedOrigin()) { setError('Only trusted HTTPS or local loopback may send files'); return }
    if (!validSelectedFile(selectedFile)) {
      setError('Choose one nonempty file of at most 32 MiB with a valid filename')
      return
    }
    setBusy(true)
    setError('')
    setLastAction('Uploading only the selected document and creating an encrypted Kopia snapshot. Keep this tab open.')
    try {
      const selectedHash = await selectedFileSHA256(selectedFile)
      const response = await fetch('/api/v1/mobile/files', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${token}`,
          'Content-Type': 'application/octet-stream',
          'X-GCB-Filename-B64': toBase64Url(selectedFile.name),
          'X-GCB-Confirm-Upload': 'true'
        },
        body: selectedFile,
        cache: 'no-store',
        redirect: 'error'
      })
      const payload = await readAPIJSON<SelectedFileResult>(response)
      if (!payload.backedUp || payload.name !== selectedFile.name || payload.bytes !== selectedFile.size ||
          payload.sha256 !== selectedHash) {
        throw new Error('The server did not confirm the expected file size and SHA-256 fingerprint')
      }
      setLastAction(`Encrypted snapshot completed for ${payload.name} (${payload.bytes.toLocaleString()} bytes). Independently verify and restore-test before relying on it.`)
      setSelectedFile(null)
      setSelectedFileConsent(false)
      if (fileInput.current) fileInput.current.value = ''
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Selected-file backup failed')
      setLastAction('Selected-file backup was not confirmed.')
    } finally { setBusy(false) }
  }

  // One privacy-minimized projection drives the timeline and restore selector.
  // Raw Kopia metadata can contain absolute paths, host/user IDs and tags;
  // none of those fields belong in the visual recovery history.
  const recoveryPoints = summarizeSnapshots(snapshots)
  const snapshotIds = recoveryPoints.map(point => point.id)
  const latestPoint = recoveryPoints.find(point => point.dateTime !== null)

  const prepareRestore = (snapshotId: string) => {
    if (!restoreEnabled || busy || !status?.connected) return
    setRestoreId(snapshotId)
    // Picking a recovery point never grants consent or invokes restoration.
    setRestoreConsent(false)
    document.getElementById('restoreSnapshot')?.focus()
    document.getElementById('restore')?.scrollIntoView({ block: 'start' })
  }

  return (
    <div className="layout">
      <aside className="sidebar" aria-label="Primary navigation">
        <div className="brand"><span className="brandmark" aria-hidden="true">◈</span><div>GoreeCloud <strong>Backup</strong><small>Self-hosted workspace</small></div></div>
        <nav aria-label="Sections">
          <a href="#overview" className="nav-current">Overview</a>
          <a href="#snapshots">Recovery points</a>
          <a href="#create">Back up now</a>
          <a href="#restore">Restore files</a>
          <a href="#file-backup">Back up one file</a>
          <a href="#verification">Verify archived files</a>
          <a href="#destinations">Destinations</a>
        </nav>
        <p className="sidebar-note">Local-first · No telemetry<br />Development preview v0.1.0</p>
      </aside>
      <main className="main" id="overview">
        <div className="eyebrow">YOUR BACKUP & RECOVERY WORKSPACE</div>
        <header className="topline">
          <div><h1>Backup & recovery</h1><p className="subtle">Find recovery points, create encrypted backups, and prepare safe restores. A listed snapshot is not proof of integrity.</p></div>
          {token && <button className="secondary" onClick={() => refresh(token)} disabled={busy}>Refresh status</button>}
        </header>
        {!token ? (
          <section className="panel connect">
            <div className="panel-icon" aria-hidden="true">⌁</div>
            <h2>Connect to your backup service</h2>
            <p>Enter your full-access or optional read-only token configured on your self-hosted GoreeCloud Backup server. It is kept only in this tab's memory.</p>
            <form onSubmit={connect} className="connect-form">
              <label htmlFor="token">Access token</label>
              <input id="token" type="password" autoComplete="off" minLength={32} value={draftToken} onChange={e => setDraftToken(e.target.value)} placeholder="Your configured access token" required />
              <button type="submit" disabled={busy}>{busy ? 'Connecting…' : 'Connect securely'}</button>
            </form>
          </section>
        ) : (
          <>
            {readOnly && <p className="hint" role="status">Read-only session: snapshot creation, restoration, uploads, archive verification and remote-provider capacity calls are unavailable.</p>}
            <div className="metrics">
              <section className="metric"><span>Repository</span><strong>{status ? (status.connected ? 'Connected' : 'Unconfigured') : 'Checking…'}</strong><small>Kopia engine</small></section>
              <section className="metric"><span>Recovery points</span><strong>{recoveryPoints.length}</strong><small>Visible, not independently verified</small></section>
              <section className="metric"><span>Latest visible backup</span><strong>{latestPoint?.dateLabel ?? 'Unavailable'}</strong><small>{latestPoint ? latestPoint.timeLabel : 'No dated recovery point'}</small></section>
            </div>
            <section className="panel" id="snapshots" aria-labelledby="recovery-points-title">
              <div className="section-head">
                <div><div className="eyebrow">YOUR RECOVERY HISTORY</div><h2 id="recovery-points-title">Recovery points</h2></div>
                <span className="muted-label">Metadata only · newest first</span>
              </div>
              <p className="subtle">Browse the 50 most recent visible states. Select one to prepare a contained restore. These are listed snapshots, not independently verified recoveries; full source paths, usernames and host details are hidden.</p>
              {recoveryPoints.length ? <ol className="recovery-timeline" aria-label="Saved encrypted recovery points">
                {recoveryPoints.map((point, index) => <li className="recovery-point" key={point.id}>
                  <div className="recovery-point-main">
                    <div className="recovery-point-heading">
                      <strong className="recovery-point-label">{point.label}</strong>
                      {index === 0 && point.dateTime && <span className="recovery-latest">Most recent</span>}
                    </div>
                    <div className="recovery-point-details">
                      {point.dateTime ? <time dateTime={point.dateTime}>{point.dateLabel} · {point.timeLabel}</time>
                        : <span>Date unavailable</span>}
                      <span>Snapshot {point.id.slice(0, 12)}{point.id.length > 12 ? '…' : ''}</span>
                    </div>
                    <span className="recovery-point-caveat">Archived snapshot · restore not tested</span>
                  </div>
                  {restoreEnabled && !readOnly && <button type="button" className="secondary recovery-prepare"
                    disabled={busy || !status?.connected} onClick={() => prepareRestore(point.id)}
                    aria-label={'Prepare a contained restore for ' + point.label + ' from ' + point.dateLabel}>
                    Prepare restore
                  </button>}
                </li>)}
              </ol> : <div className="empty"><span aria-hidden="true">▤</span><strong>No recovery points displayed</strong><p>Connect a Kopia repository and create your first encrypted backup to begin a recovery history.</p></div>}
            </section>
            <section className="panel" id="restore">
              <div className="eyebrow">SAFE RECOVERY</div><h2>Contained restore workspace</h2>
              <p className="subtle">Restore an existing snapshot to a new, private directory on the server. The destination is allocated automatically, separate from the configured source root; the original files are not overwritten.</p>
              {!restoreEnabled ? <p className="hint">{readOnly ? 'This access token cannot restore files.' : 'Restoration is disabled until the server operator configures a separate, private GCB_RESTORE_ROOT directory.'}</p> :
                <form onSubmit={restore} className="restore-form">
                  <label htmlFor="restoreSnapshot">Recovery point</label>
                  <select id="restoreSnapshot" value={restoreId} onChange={e => setRestoreId(e.target.value)} disabled={busy || snapshotIds.length === 0} required>
                    <option value="">Choose a snapshot</option>
                    {recoveryPoints.map(point => <option key={point.id} value={point.id}>{point.label} · {point.dateLabel} · {point.id.slice(0, 12)}</option>)}
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
                <div><label htmlFor="source">Source directory (relative)</label><input id="source" value={source} onChange={e => setSource(e.target.value)} placeholder="Documents" required disabled={busy || !canSnapshot} /></div>
                <button type="submit" disabled={busy || !status?.connected || !canSnapshot}>{busy ? 'Working…' : 'Back up now'}</button>
              </form>
            </section>
            <section className="panel" id="file-backup">
              <div className="eyebrow">USER-SELECTED BACKUP</div><h2>Back up one file from this device</h2>
              <p className="subtle">Choose one document, explicitly approve its transfer, and let your configured server create an encrypted Kopia snapshot. No automatic file access or background upload occurs. Maximum 32 MiB.</p>
              {!selectedBackupEnabled ? <p className="hint">{readOnly ? 'This access token cannot upload files.' : 'This capability is disabled until the operator configures a private GCB_MOBILE_INGEST_ROOT directory on the backup server.'}</p> :
                <form onSubmit={backupSelectedFile} className="selected-file-form">
                  <label htmlFor="selectedBackupFile">Document to back up</label>
                  <input id="selectedBackupFile" ref={fileInput} type="file" disabled={busy} onChange={e => { setSelectedFile(e.target.files?.[0] || null); setSelectedFileConsent(false) }} />
                  {selectedFile && <p className="hint">{selectedFile.name} · {selectedFile.size.toLocaleString()} bytes {validSelectedFile(selectedFile) ? '' : '— unsupported file size or filename'}</p>}
                  <label className="verify-consent"><input type="checkbox" disabled={busy || !selectedFile || !validSelectedFile(selectedFile)} checked={selectedFileConsent} onChange={e => setSelectedFileConsent(e.target.checked)} /> I authorize uploading this one file to my configured server and understand network charges may apply.</label>
                  <button type="submit" disabled={busy || !selectedFile || !selectedFileConsent || !status?.connected || !validSelectedFile(selectedFile)}>{busy ? 'Operation in progress…' : 'Back up selected file'}</button>
                </form>}
              <p className="hint">The upload is protected in transit by HTTPS when accessed remotely, but the server temporarily handles plaintext before Kopia encrypts its snapshot. This is not device-side end-to-end encryption. A process crash may require manual cleanup of protected staging files.</p>
            </section>
            <section className="panel" id="verification">
              <div className="eyebrow">INTEGRITY CHECK</div><h2>Verify archived data</h2>
              <p className="subtle">Ask Kopia to read and verify 100% of archived file contents. This may take a long time and incur provider transfer charges. It is not a substitute for an independent restoration drill.</p>
              <label className="verify-consent"><input type="checkbox" checked={verifyConsent} onChange={e => setVerifyConsent(e.target.checked)} disabled={busy || !canVerify} /> I understand this operation may read all cloud-stored backup data and incur costs.</label>
              <div className="verification-action"><button type="button" onClick={verify} disabled={busy || !verifyConsent || !status?.connected || !canVerify}>{busy ? 'Operation in progress…' : 'Verify all snapshot files'}</button></div>
              {lastVerification && <p className="hint" role="status">Most recent verification in this browser session: {lastVerification.filesRead} files, {lastVerification.bytesRead.toLocaleString()} bytes read. Successful verification does not prove restore readiness.</p>}
            </section>
            <section className="panel" id="destinations">
              <div className="eyebrow">STORAGE CONNECTIONS</div><h2>Rclone remotes</h2>
              {remotes.length ? <div className="remote-list">{remotes.map(name => <div className="remote-line" key={name}><span className="chip">{name}</span><button type="button" className="secondary" disabled={busy || capacityBusy || !canInspectRemote} onClick={() => inspectRemote(name)}>Check capacity</button></div>)}</div> : <p className="subtle">No Rclone remotes are available. Configure destinations on the server.</p>}
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
