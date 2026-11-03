(() => {
  'use strict';
  let secret = '';
  const $ = (id) => document.getElementById(id);
  const auth = $('auth-dialog');
  const label = (text, cls) => { const node = document.createElement('span'); node.textContent = text; if (cls) node.className = cls; return node; };
  const tabs = new Set(['overview','operations','history']);

  function navigate(tab) {
    if (!tabs.has(tab)) tab = 'overview';
    for (const id of tabs) $(id).hidden = id !== tab;
    for (const link of document.querySelectorAll('[data-tab]')) {
      const selected = link.dataset.tab === tab;
      link.classList.toggle('active', selected);
      if (selected) link.setAttribute('aria-current','page'); else link.removeAttribute('aria-current');
    }
    location.hash = tab;
    if (tab === 'history' && secret) refresh();
  }

  async function api(endpoint, options = {}) {
    const headers = {Authorization: `Bearer ${secret}`};
    if (options.body) headers['Content-Type'] = 'application/json';
    const response = await fetch('/api/v1' + endpoint, {...options, headers, cache: 'no-store'});
    let data;
    try { data = await response.json(); } catch (_) { throw new Error('Server returned an invalid response'); }
    if (!response.ok) throw new Error(data.error || `Request failed (${response.status})`);
    return data;
  }

  function status(text, success) {
    const item = $('form-status');
    item.textContent = text;
    item.classList.toggle('success', !!success);
  }
  function updateFields() {
    const type = $('operation').value;
    $('source-field').hidden = ['snapshots','verify-structure','verify-full','restore'].includes(type);
    $('destination-field').hidden = ['backup','snapshots','verify-structure','verify-full'].includes(type);
    $('snapshot-field').hidden = type !== 'restore';
    $('confirm-field').hidden = !['restore','sync','verify-full'].includes(type);
    const phrase = type === 'sync' ? 'DELETE DESTINATION EXTRAS' : type === 'verify-full' ? 'VERIFY ALL BACKUPS' : 'RESTORE';
    $('confirm-hint').textContent = `Enter exactly: ${phrase}`;
    $('confirm-label').textContent = 'Typed confirmation';
    status(type === 'sync' ? 'Warning: sync deletes destination-only files. Complete and review a sync-preview of the same paths first.' : type === 'verify-full' ? 'Full verification downloads/decrypts stored data and may incur cloud transfer costs. Allow enough time for the check to finish.' : type === 'verify-structure' ? 'Checks snapshot structures and referenced blobs; use full verification to check every stored file content.' : '', false);
  }
  async function refresh() {
    if (!secret) return;
    try {
      const [health, recent] = await Promise.all([api('/health'), api('/jobs')]);
      const jobs = recent.jobs;
      $('total-count').textContent = jobs.length;
      $('completed-count').textContent = jobs.filter(j => j.status === 'completed').length;
      $('running-count').textContent = jobs.filter(j => ['queued','running','cancelling'].includes(j.status)).length;
      $('service-state').textContent = 'Connected';
      for (const engine of ['kopia','rclone']) {
        const el = $(engine+'-state');
        el.textContent = health.engines[engine] ? 'Available' : 'Not installed';
        el.classList.toggle('ready', health.engines[engine]);
      }
      $('engine-summary').textContent = `${Object.values(health.engines).filter(Boolean).length} / 2 available`;
      $('session-state').replaceChildren(label('●','status-dot'), document.createTextNode(' Connected'));
      renderJobs(jobs);
    } catch (err) {
      $('service-state').textContent = 'Unavailable';
      $('session-state').textContent = 'Connection lost';
      if (String(err.message).includes('authentication')) { secret = ''; auth.hidden = false; }
    }
  }
  function renderJobs(jobs) {
    const root = $('jobs');
    root.replaceChildren();
    if (!jobs.length) { const empty = document.createElement('div'); empty.className = 'empty-state'; empty.textContent = 'No operations yet. Run your first backup or transfer.'; root.append(empty); return; }
    for (const job of jobs) {
      const item = document.createElement('article'); item.className = 'job';
      const head = document.createElement('div'); head.className = 'job-head';
      head.append(label(job.type.replaceAll('-',' '),'job-title'), label(job.status,'job-status '+job.status));
      item.append(head);
      if (['queued','running'].includes(job.status)) {
        const cancelButton = document.createElement('button');
        cancelButton.type = 'button';
        cancelButton.className = 'button minimal cancel-job';
        cancelButton.textContent = 'Cancel operation';
        cancelButton.setAttribute('aria-label', `Cancel ${job.type} operation ${job.id}`);
        cancelButton.addEventListener('click', async () => {
          cancelButton.disabled = true;
          cancelButton.textContent = 'Requesting cancellation…';
          try { await api(`/jobs/${encodeURIComponent(job.id)}/cancel`, {method:'POST'}); await refresh(); }
          catch (err) { cancelButton.textContent = err.message; cancelButton.disabled = false; }
        });
        item.append(cancelButton);
      }
      const time = document.createElement('div'); time.className='job-time';
      time.textContent = `${new Date(job.started_at).toLocaleString()} · ${job.id}`; item.append(time);
      if (job.error || job.output) {
        const pre = document.createElement('pre');
        pre.textContent = [job.error,job.output].filter(Boolean).join('\n');
        item.append(pre);
      }
      root.append(item);
    }
  }
  $('auth-form').addEventListener('submit', async (e) => {
    e.preventDefault(); $('auth-error').textContent = '';
    secret = $('token').value;
    try {
      await api('/health'); auth.hidden = true; $('token').value = ''; await refresh();
    } catch (err) { secret = ''; $('auth-error').textContent = err.message; }
  });
  $('reconnect').addEventListener('click', () => { secret = ''; auth.hidden = false; $('token').focus(); });
  $('operation').addEventListener('change', updateFields);
  $('job-form').addEventListener('submit', async (e) => {
    e.preventDefault(); if (!secret) { auth.hidden = false; return; }
    const type = $('operation').value;
    const payload = {type,source:$('source').value.trim(),destination:$('destination').value.trim(),snapshot_id:$('snapshot-id').value.trim()};
    if (['restore','sync','verify-full'].includes(type)) payload.confirm = $('confirm').value.trim();
    $('submit').disabled = true;
    status('Submitting operation…', true);
    try {
      const result = await api('/jobs',{method:'POST',body:JSON.stringify(payload)});
      status(`Operation queued · ${result.id}`,true); $('confirm').value = '';
      navigate('history'); await refresh();
    } catch (err) { status(err.message, false); }
    finally { $('submit').disabled = false; }
  });
  $('refresh').addEventListener('click', refresh);
  for (const element of document.querySelectorAll('[data-go]')) element.addEventListener('click', () => navigate(element.dataset.go));
  window.addEventListener('hashchange', () => navigate(location.hash.slice(1)));
  updateFields(); navigate(location.hash.slice(1));
  setInterval(() => { if (secret) refresh(); }, 4000);
})();
