// Berthkeep UI: plain ES module, talks to the local API.
const token = document.querySelector('meta[name="berthkeep-token"]').content;
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

const state = { sessions: [], browsers: [], config: null, home: '', shell: false, label: '', open: null, snippet: 'playwright' };

// API

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (method !== 'GET') opts.headers['X-Berthkeep-Token'] = token;
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  const type = res.headers.get('Content-Type') || '';
  const data = type.includes('json') ? await res.json() : await res.text();
  if (!res.ok) throw new Error((data && data.error) || `${res.status} ${res.statusText}`);
  return data;
}

function toast(msg, isErr = false) {
  const el = document.createElement('div');
  el.className = 'toast' + (isErr ? ' err' : '');
  el.textContent = msg;
  $('#toasts').append(el);
  setTimeout(() => el.remove(), isErr ? 6000 : 2500);
}

async function run(fn, done) {
  try {
    const r = await fn();
    if (done) toast(done);
    return r;
  } catch (e) {
    toast(e.message, true);
  }
}

// Helpers

const isRunning = (s) => s.status && (s.status.state === 'running' || s.status.state === 'running-unverified');
const livePort = (s) => (s.status && s.status.port) || s.port || 0;
const endpointOf = (s) => `http://127.0.0.1:${livePort(s)}`;
const stateText = {
  running: 'Running', 'running-unverified': 'Running', stopped: 'Stopped',
  busy: 'Profile in use', 'port-taken': 'Port taken',
};
const persistText = (s) => (s.autoRemove ? 'Quick temp' : s.persistence === 'ephemeral' ? 'Clean every start' : 'Kept');

function el(tag, attrs = {}, ...kids) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') n.className = v;
    else if (k === 'text') n.textContent = v;
    else if (k.startsWith('on')) n.addEventListener(k.slice(2), v);
    else n.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) if (kid !== null && kid !== undefined && kid !== false) n.append(kid);
  return n;
}

async function copy(text, what) {
  try {
    await navigator.clipboard.writeText(text);
    toast(`Copied ${what}`);
  } catch {
    toast('Copy failed: select the text and copy it by hand', true);
  }
}

// Sessions view

function upsert(view) {
  const i = state.sessions.findIndex((s) => s.id === view.id);
  if (i >= 0) state.sessions[i] = view; else state.sessions.push(view);
}

function render() {
  const q = $('#search').value.trim().toLowerCase();
  const only = $('#filter-state').value;
  const list = state.sessions.filter((s) => {
    if (state.label && !(s.labels || []).includes(state.label)) return false;
    if (only === 'running' && !isRunning(s)) return false;
    if (only === 'stopped' && isRunning(s)) return false;
    if (!q) return true;
    return [s.title, s.slug, ...(s.labels || []), String(livePort(s))].some((x) => (x || '').toLowerCase().includes(q));
  });
  $('#berths').replaceChildren(...list.map(berth));
  $('#empty').hidden = state.sessions.length > 0;
  renderLabels();
  if (state.open) {
    const s = state.sessions.find((x) => x.id === state.open);
    if (s) fillDrawerHead(s); else closeDrawer();
  }
}

function renderLabels() {
  const labels = [...new Set(state.sessions.flatMap((s) => s.labels || []))].sort();
  $('#label-filter').replaceChildren(...labels.map((l) => el('button', {
    class: 'chip' + (state.label === l ? ' on' : ''), text: l, 'aria-pressed': String(state.label === l),
    onclick: () => { state.label = state.label === l ? '' : l; render(); },
  })));
}

function berth(s) {
  const running = isRunning(s);
  const fixed = s.port;
  const shown = running ? s.status.port : fixed;
  const conflict = (s.conflicts || []).length > 0;
  const open = () => openDrawer(s.id);
  return el('li', { class: 'berth' + (conflict ? ' conflict' : ''), 'data-state': s.status.state },
    el('div', { class: 'plate', onclick: open, title: fixed ? 'Fixed port' : 'Port assigned on start' },
      el('span', { class: 'plate-num' + (shown ? '' : ' auto'), text: shown || 'auto' }),
      el('span', { class: 'plate-sub', text: fixed ? 'fixed' : running ? 'auto' : 'next free' })),
    el('div', { class: 'berth-main', onclick: open },
      el('p', { class: 'berth-title', text: s.title }),
      el('div', { class: 'berth-meta' },
        el('span', { class: 'state', text: stateText[s.status.state] || s.status.state }),
        el('span', { text: s.slug }),
        el('span', { text: persistText(s) }),
        ...(s.labels || []).map((l) => el('span', { class: 'label', text: l }))),
      conflict && el('div', { class: 'warning', text: s.conflicts.join('; ') }),
      s.status.holder && el('div', { class: 'warning', text: s.status.holder })),
    el('div', { class: 'berth-actions' },
      el('div', { class: 'toggle', role: 'group', 'aria-label': 'Window mode' },
        el('button', { 'aria-pressed': String(s.mode === 'headed'), text: 'Window', onclick: () => setMode(s, 'headed') }),
        el('button', { 'aria-pressed': String(s.mode === 'headless'), text: 'Headless', onclick: () => setMode(s, 'headless') })),
      running && el('button', { class: 'quiet', text: 'Copy endpoint', onclick: () => copy(endpointOf(s), 'endpoint') }),
      running
        ? el('button', { text: 'Stop', onclick: () => stopSession(s) })
        : el('button', { class: 'primary', text: 'Start', onclick: () => startSession(s) })));
}

async function startSession(s) {
  const v = await run(() => api('POST', `/api/sessions/${s.id}/start`, {}), `Started ${s.slug}`);
  if (v) { upsert(v); render(); }
}

async function stopSession(s) {
  const r = await run(() => api('POST', `/api/sessions/${s.id}/stop`), `Stopped ${s.slug}`);
  if (!r) return;
  if (r.removed) state.sessions = state.sessions.filter((x) => x.id !== s.id); else upsert(r);
  render();
}

async function setMode(s, mode) {
  if (s.mode === mode) return;
  if (isRunning(s) && !confirm(`Restart ${s.title} ${mode === 'headless' ? 'headless' : 'with a window'}? Open tabs are reopened; unsaved page state is lost.`)) return;
  const v = await run(() => api('POST', `/api/sessions/${s.id}/mode`, { mode }), mode === 'headless' ? 'Switched to headless' : 'Switched to a window');
  if (v) { upsert(v); render(); }
}

// Drawer

const snippets = {
  playwright: ['Playwright', (u) => `import { chromium } from 'playwright';\nconst browser = await chromium.connectOverCDP('${u}');\nconst page = browser.contexts()[0].pages()[0] ?? await browser.contexts()[0].newPage();`],
  python: ['Playwright (Python)', (u) => `from playwright.sync_api import sync_playwright\nwith sync_playwright() as p:\n    browser = p.chromium.connect_over_cdp("${u}")\n    page = browser.contexts[0].pages[0]`],
  puppeteer: ['Puppeteer', (u) => `import puppeteer from 'puppeteer-core';\nconst browser = await puppeteer.connect({ browserURL: '${u}' });`],
  devtools: ['chrome-devtools-mcp', (u) => `{\n  "command": "npx",\n  "args": ["chrome-devtools-mcp@latest", "--browserUrl", "${u}"]\n}`],
  pwmcp: ['Playwright MCP', (u) => `{\n  "command": "npx",\n  "args": ["@playwright/mcp@latest", "--cdp-endpoint", "${u}"]\n}`],
  cli: ['CLI', (_, s) => `berthkeep endpoint ${s.slug} --ensure`],
};

function openDrawer(id) {
  state.open = id;
  const s = state.sessions.find((x) => x.id === id);
  if (!s) return;
  $('#drawer').hidden = false;
  fillDrawerHead(s);
  fillEdit(s);
  loadTabs();
  loadLog();
}

function closeDrawer() {
  state.open = null;
  $('#drawer').hidden = true;
}

function fillDrawerHead(s) {
  $('#d-title').textContent = s.title;
  $('#d-sub').textContent = `${s.slug}, ${stateText[s.status.state] || s.status.state}, ${persistText(s).toLowerCase()}`;
  const ep = $('#d-endpoint');
  if (isRunning(s)) {
    const http = endpointOf(s);
    const ws = s.runtime && s.runtime.wsPath ? `ws://127.0.0.1:${s.status.port}${s.runtime.wsPath}` : '';
    ep.replaceChildren(
      el('div', {}, el('code', { text: http }), el('button', { class: 'quiet', text: 'Copy', onclick: () => copy(http, 'endpoint') })),
      ws && el('div', {}, el('code', { text: ws }), el('button', { class: 'quiet', text: 'Copy', onclick: () => copy(ws, 'WebSocket URL') })));
  } else {
    ep.replaceChildren(el('p', { class: 'muted', text: 'Start the session to get its endpoint. Scripts can also run the CLI snippet, which starts it on demand.' }));
  }
  const u = isRunning(s) ? endpointOf(s) : `http://127.0.0.1:${s.port || '<port>'}`;
  $('#d-snippet-tabs').replaceChildren(...Object.entries(snippets).map(([key, [name]]) => el('button', {
    role: 'tab', 'aria-selected': String(state.snippet === key), text: name,
    onclick: () => { state.snippet = key; fillDrawerHead(s); },
  })));
  $('#d-snippet').textContent = snippets[state.snippet][1](u, s);
  const managed = s.profileDir && !/^([a-z]:|[\\/])/i.test(s.profileDir);
  $('#d-remove-hint').textContent = managed
    ? (s.persistence === 'ephemeral' ? 'The profile is wiped.' : 'The profile moves to the trash in the data folder.')
    : 'This profile lives outside Berthkeep; it stays where it is.';
}

function fillEdit(s) {
  const f = $('#d-edit');
  f.title.value = s.title;
  f.labels.value = (s.labels || []).join(', ');
  f.port.value = s.port || '';
  f.startUrls.value = (s.startUrls || []).join(' ');
  f.notes.value = s.notes || '';
  $('#d-profile').textContent = s.profileAbs || s.profileDir;
}

async function loadTabs() {
  const s = state.sessions.find((x) => x.id === state.open);
  const list = $('#d-tabs');
  if (!s || !isRunning(s)) {
    list.replaceChildren(el('li', { class: 'muted', text: 'Not running.' }));
    return;
  }
  const tabs = await run(() => api('GET', `/api/sessions/${s.id}/targets`));
  if (!tabs) return;
  list.replaceChildren(...tabs.map((t) => {
    const peek = el('img', { alt: `Screenshot of ${t.title || t.url}`, hidden: true });
    return el('li', {},
      el('div', { class: 'tab-title', text: t.title || '(untitled)' }),
      el('div', { class: 'tab-url', text: t.url }),
      el('div', { class: 'tab-actions' },
        el('button', { class: 'quiet', text: 'Peek', onclick: () => { peek.src = `/api/sessions/${s.id}/targets/${t.id}/screenshot?ts=${Date.now()}`; peek.hidden = false; } }),
        el('button', { class: 'quiet', text: 'Bring to front', onclick: () => run(() => api('POST', `/api/sessions/${s.id}/targets/${t.id}/activate`)) }),
        el('button', { class: 'quiet', text: 'Close tab', onclick: async () => { await run(() => api('DELETE', `/api/sessions/${s.id}/targets/${t.id}`)); loadTabs(); } })),
      peek);
  }));
  if (!tabs.length) list.replaceChildren(el('li', { class: 'muted', text: 'No open tabs.' }));
}

async function loadLog() {
  const s = state.sessions.find((x) => x.id === state.open);
  if (!s) return;
  const text = await run(() => api('GET', `/api/sessions/${s.id}/log?lines=120`));
  $('#d-log').textContent = text || 'Nothing logged yet.';
}

// New session dialog

function fillSelect(select, options, value) {
  select.replaceChildren(...options.map(([v, label]) => el('option', { value: v, text: label, selected: v === value })));
}

function openNew(preset = {}) {
  const f = $('#new-form');
  f.reset();
  $('#new-error').hidden = true;
  fillSelect(f.browser, [['auto', 'First installed'], ...state.browsers.map((b) => [b.id, b.name])], state.config?.defaults?.browser || 'auto');
  fillSelect(f.cloneFrom, state.sessions.map((s) => [s.id, s.title]), preset.cloneFrom);
  f.mode.value = state.config?.defaults?.mode || 'headed';
  if (preset.cloneFrom) {
    f.source.value = 'clone';
    f.title.value = `${state.sessions.find((s) => s.id === preset.cloneFrom)?.title || ''} copy`;
  }
  f.querySelector('input[value=clone]').disabled = state.sessions.length === 0;
  syncSource();
  $('#port-hint').textContent = '';
  $('#new-dialog').showModal();
  f.title.focus();
}

function syncSource() {
  const f = $('#new-form');
  $('#adopt-row').hidden = f.source.value !== 'adopt';
  $('#pick-folder').hidden = !state.shell;
  f.cloneFrom.disabled = f.source.value !== 'clone';
}

let portTimer;
function checkPort() {
  clearTimeout(portTimer);
  const f = $('#new-form');
  const v = f.port.value.trim();
  const hint = $('#port-hint');
  if (!v || v === 'auto') { hint.textContent = 'Picked from the range on start'; return; }
  portTimer = setTimeout(async () => {
    try {
      const r = await api('GET', `/api/ports/check?port=${encodeURIComponent(v)}`);
      hint.textContent = r.reservedBy.length ? `Also used by ${r.reservedBy.join(', ')}` : r.free ? 'Free' : 'In use by another program';
    } catch (e) { hint.textContent = e.message; }
  }, 250);
}

async function submitNew(ev) {
  ev.preventDefault();
  if (ev.submitter && ev.submitter.value === 'cancel') { $('#new-dialog').close(); return; }
  const f = $('#new-form');
  const port = f.port.value.trim();
  if (port && port !== 'auto' && !/^\d+$/.test(port)) { showNewError('Port must be a number or "auto".'); return; }
  const body = {
    title: f.title.value.trim(), persistence: f.persistence.value, browser: f.browser.value, mode: f.mode.value,
    labels: splitList(f.labels.value), startUrls: f.url.value ? [f.url.value] : [], start: f.start.checked,
    port: port && port !== 'auto' ? Number(port) : 0,
  };
  let path = '/api/sessions';
  if (f.source.value === 'clone') {
    const src = state.sessions.find((s) => s.id === f.cloneFrom.value);
    if (src && isRunning(src)) {
      body.cloneLive = !confirm(`${src.title} is running. Stop it briefly for a consistent copy? Choose Cancel to copy it while it runs (locked files are skipped).`);
    }
    path = `/api/sessions/${f.cloneFrom.value}/clone`;
  }
  if (f.source.value === 'adopt') {
    if (!f.adoptDir.value.trim()) { showNewError('Choose the folder to adopt.'); return; }
    body.adoptDir = f.adoptDir.value.trim();
  }
  try {
    const v = await api('POST', path, body);
    upsert(v);
    render();
    $('#new-dialog').close();
    toast(`Created ${v.slug}`);
  } catch (e) { showNewError(e.message); }
}

function showNewError(msg) {
  const e = $('#new-error');
  e.textContent = msg;
  e.hidden = false;
}

const splitList = (v) => v.split(/[,\s]+/).map((x) => x.trim()).filter(Boolean);

// Detected

async function loadDetected() {
  const list = await run(() => api('GET', '/api/detect'));
  if (!list) return;
  const ul = $('#detected');
  if (!list.length) {
    ul.replaceChildren(el('li', { class: 'muted', text: 'No browser with a debugging port is running.' }));
    return;
  }
  ul.replaceChildren(...list.map((d) => el('li', {},
    el('span', { class: 'plate-num', text: d.port || 'auto' }),
    el('code', { class: 'path', text: d.userDataDir || '(default profile)' }),
    d.session
      ? el('span', { class: 'muted', text: `Managed as ${d.session}` })
      : el('button', { class: 'primary', text: 'Adopt', onclick: () => adopt(d) }))));
}

async function adopt(d) {
  const title = prompt('Name for this session', (d.userDataDir || '').split(/[\\/]/).filter(Boolean).pop() || `port ${d.port}`);
  if (title === null) return;
  const v = await run(() => api('POST', '/api/adopt', { port: d.port, pid: d.pid, title }), 'Adopted');
  if (v) { upsert(v); render(); loadDetected(); }
}

// Settings

function fillSettings() {
  const c = state.config;
  if (!c) return;
  const f = $('#settings');
  f.rangeStart.value = c.ports.rangeStart;
  f.rangeEnd.value = c.ports.rangeEnd;
  fillSelect(f.browser, [['auto', 'First installed'], ...state.browsers.map((b) => [b.id, b.name])], c.defaults.browser);
  f.mode.value = c.defaults.mode;
  f.persistence.value = c.defaults.persistence;
  f.graceMs.value = c.stop.graceMs;
  f.skipCaches.checked = c.clone.skipCaches;
  $('#home-path').textContent = state.home;
}

async function saveSettings(ev) {
  ev.preventDefault();
  const f = $('#settings');
  const c = structuredClone(state.config);
  c.ports.rangeStart = Number(f.rangeStart.value);
  c.ports.rangeEnd = Number(f.rangeEnd.value);
  c.defaults.browser = f.browser.value;
  c.defaults.mode = f.mode.value;
  c.defaults.persistence = f.persistence.value;
  c.stop.graceMs = Number(f.graceMs.value);
  c.clone.skipCaches = f.skipCaches.checked;
  const saved = await run(() => api('PUT', '/api/config', c), 'Settings saved');
  if (saved) state.config = saved;
}

// Views and wiring

function showView(name) {
  for (const b of $$('.view')) {
    const on = b.dataset.view === name;
    b.classList.toggle('on', on);
    if (on) b.setAttribute('aria-current', 'page'); else b.removeAttribute('aria-current');
  }
  for (const p of $$('.pane')) p.hidden = p.id !== `view-${name}`;
  if (name === 'detected') loadDetected();
  if (name === 'settings') fillSettings();
}

async function load() {
  const [st, browsers, config] = await Promise.all([api('GET', '/api/state'), api('GET', '/api/browsers'), api('GET', '/api/config')]);
  state.sessions = st.sessions;
  state.home = st.home;
  state.shell = st.shell;
  state.browsers = browsers || [];
  state.config = config;
  render();
}

function listen() {
  const es = new EventSource(`/api/events?t=${encodeURIComponent(token)}`);
  es.addEventListener('session.status', (e) => { upsert(JSON.parse(e.data)); render(); });
  es.addEventListener('session.removed', (e) => {
    const d = JSON.parse(e.data);
    state.sessions = state.sessions.filter((s) => s.id !== d.id && s.id !== d.ref && s.slug !== d.ref);
    render();
  });
  es.addEventListener('sessions.changed', () => load().catch(() => {}));
}

function wire() {
  $$('.view').forEach((b) => b.addEventListener('click', () => showView(b.dataset.view)));
  $('#new-session').addEventListener('click', () => openNew());
  $('[data-action=new]').addEventListener('click', () => openNew());
  $('#search').addEventListener('input', render);
  $('#filter-state').addEventListener('change', render);
  $('#new-form').addEventListener('submit', submitNew);
  $('#new-form').addEventListener('change', (e) => { if (e.target.name === 'source') syncSource(); });
  $('#new-form').port.addEventListener('input', checkPort);
  $('#pick-folder').addEventListener('click', async () => {
    const r = await run(() => api('POST', '/api/shell/pick-folder', { title: 'Choose a browser profile folder' }));
    if (r && r.path) $('#new-form').adoptDir.value = r.path;
  });
  $('[data-action=close-drawer]').addEventListener('click', closeDrawer);
  $('[data-action=refresh-tabs]').addEventListener('click', loadTabs);
  $('[data-action=refresh-log]').addEventListener('click', loadLog);
  $('#d-copy-snippet').addEventListener('click', () => copy($('#d-snippet').textContent, 'snippet'));
  $('#d-open').addEventListener('submit', async (e) => {
    e.preventDefault();
    const url = e.target.url.value;
    if (await run(() => api('POST', `/api/sessions/${state.open}/targets`, { url }), 'Tab opened')) { e.target.reset(); loadTabs(); }
  });
  $('#d-edit').addEventListener('submit', async (e) => {
    e.preventDefault();
    const f = e.target;
    const port = f.port.value.trim();
    if (port && port !== 'auto' && !/^\d+$/.test(port)) { toast('Port must be a number or "auto".', true); return; }
    const v = await run(() => api('PATCH', `/api/sessions/${state.open}`, {
      title: f.title.value.trim(), labels: splitList(f.labels.value), notes: f.notes.value,
      port: port && port !== 'auto' ? Number(port) : 0, startUrls: splitList(f.startUrls.value),
    }), 'Details saved');
    if (v) { upsert(v); render(); }
  });
  $('[data-action=delete]').addEventListener('click', async () => {
    const s = state.sessions.find((x) => x.id === state.open);
    if (!s || !confirm(`Delete ${s.title}? ${$('#d-remove-hint').textContent}`)) return;
    if (await run(() => api('DELETE', `/api/sessions/${s.id}`), `Deleted ${s.slug}`)) {
      state.sessions = state.sessions.filter((x) => x.id !== s.id);
      closeDrawer();
      render();
    }
  });
  $('#settings').addEventListener('submit', saveSettings);
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName) || $('#new-dialog').open;
    if (e.key === 'Escape' && state.open && !$('#new-dialog').open) closeDrawer();
    if (typing || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === '/') { e.preventDefault(); showView('sessions'); $('#search').focus(); }
    if (e.key === 'n') { e.preventDefault(); openNew(); }
  });
}

wire();
load().then(listen).catch((e) => toast(`Cannot reach the Berthkeep service: ${e.message}`, true));
