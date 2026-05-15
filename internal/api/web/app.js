const stateClass = (s) => {
  if (!s) return 'state-badge state-unknown';
  return `state-badge state-${s}`;
};

let statusWarm = true;

const baselineLabel = (mode) => {
  if (mode === 'baseline_active') return 'Baseline active';
  if (mode === 'learning') return 'Learning baseline';
  return mode || 'Unknown';
};

function fmtTs(ts) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString();
}

function durationSince(ts) {
  if (!ts) return '';
  const sec = Math.floor(Date.now() / 1000 - ts);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
}

function setVisible(el, show) {
  if (!el) return;
  el.classList.toggle('hidden', !show);
}

function renderLearningBanner(data) {
  const banner = document.getElementById('learning-banner');
  if (!data.learning || data.warm) {
    setVisible(banner, false);
    return;
  }
  const L = data.learning;
  const warmupDays = L.warmup_days || 14;
  setVisible(banner, true);

  document.getElementById('learning-copy').textContent =
    `Monitoring is active with config thresholds. Personalized anomaly detection starts after about ${warmupDays} days of measurements.`;

  const progressEl = document.getElementById('learning-progress');
  const pct = Math.round((L.time_progress || 0) * 100);
  progressEl.style.width = `${pct}%`;
  progressEl.setAttribute('aria-valuenow', String(pct));

  const labelEl = document.getElementById('learning-progress-label');
  if (!L.first_sample_ts) {
    labelEl.textContent = 'Waiting for first samples';
  } else {
    const day = Math.min(warmupDays, Math.max(1, Math.ceil(L.days_collected || 0)));
    labelEl.textContent = `Day ${day} of ${warmupDays}`;
  }

  const secondary = document.getElementById('learning-secondary');
  if (L.time_progress >= 1 && !L.baselines_ready) {
    secondary.textContent =
      'Enough time has elapsed — still collecting hourly baseline samples.';
    setVisible(secondary, true);
  } else {
    setVisible(secondary, false);
  }
}

function renderOverallLearningNote(data) {
  const note = document.getElementById('overall-learning-note');
  if (!data.warm) {
    note.textContent = 'Thresholds: config defaults (baseline not active yet).';
    setVisible(note, true);
  } else {
    setVisible(note, false);
  }
}

function applyStatus(data) {
  statusWarm = !!data.warm;
  document.getElementById('meta').textContent =
    `${data.device_id} · ${baselineLabel(data.baseline_mode)}`;

  renderLearningBanner(data);
  renderOverallLearningNote(data);

  const states = data.states || [];
  const overall = states.find((s) => s.dimension === 'overall');
  const el = document.getElementById('overall-state');
  if (overall && overall.state) {
    el.textContent = overall.state;
    el.className = stateClass(overall.state);
    document.getElementById('overall-since').textContent =
      `for ${durationSince(overall.since_ts)} (since ${fmtTs(overall.since_ts)})`;
  } else {
    el.textContent = '—';
    el.className = 'state-badge state-unknown';
    document.getElementById('overall-since').textContent =
      'Waiting for first evaluation cycle…';
  }

  const grid = document.getElementById('dimensions');
  grid.innerHTML = '';
  states
    .filter((s) => s.dimension !== 'overall')
    .forEach((s) => {
      const d = document.createElement('div');
      d.className = 'dim';
      const state = s.state || '—';
      d.innerHTML = `
        <div class="dim-name">${s.dimension}</div>
        <div class="${stateClass(s.state)}">${state}</div>
        <div class="since">${durationSince(s.since_ts)}</div>
      `;
      grid.appendChild(d);
    });
}

async function loadRollups() {
  const nowSec = Math.floor(Date.now() / 1000);
  const windowStart = nowSec - 86400;
  const spanSec = Math.max(1, nowSec - windowStart);
  const res = await fetch(`/api/v1/rollups?since=${windowStart}`);
  const rollups = await res.json();
  const byProbe = {};
  rollups
    .filter((r) => r.metric === 'latency_ms')
    .forEach((r) => {
      if (!byProbe[r.probe]) byProbe[r.probe] = [];
      byProbe[r.probe].push(r);
    });

  const container = document.getElementById('sparklines');
  const emptyEl = document.getElementById('sparklines-empty');
  container.innerHTML = '';
  const probes = Object.keys(byProbe).sort();
  if (probes.length === 0) {
    setVisible(emptyEl, true);
    emptyEl.textContent =
      'Collecting measurements — charts appear after the first 5-minute rollup.';
    return;
  }
  setVisible(emptyEl, false);
  probes.forEach((probe) => {
    const rows = byProbe[probe].sort((a, b) => a.bucket_ts - b.bucket_ts);
    const max = Math.max(...rows.map((r) => r.avg_val), 1);
    const bars = rows
      .map((r) => {
        const pctRaw = ((r.bucket_ts - windowStart) / spanSec) * 100;
        const pct = Math.min(99.75, Math.max(0.25, pctRaw));
        const h = Math.max(2, (r.avg_val / max) * 24);
        const tip = `${r.avg_val.toFixed(0)} ms · ${fmtTs(r.bucket_ts)}`;
        return `<span class="spark-bar" style="left:${pct}%;height:${h}px" title="${tip}"></span>`;
      })
      .join('');
    const row = document.createElement('div');
    row.className = 'sparkline-row';
    row.innerHTML = `
      <span class="sparkline-label">${probe}</span>
      <div class="sparkline-track" role="img" aria-label="Average latency by time, last 24 hours (recent toward the right).">${bars}</div>`;
    container.appendChild(row);
  });
}

async function loadIncidents() {
  const res = await fetch('/api/v1/incidents?limit=20');
  const incidents = await res.json();
  const tbody = document.querySelector('#incidents tbody');
  const emptyEl = document.getElementById('incidents-empty');
  tbody.innerHTML = '';
  if (!incidents.length) {
    setVisible(emptyEl, true);
    emptyEl.textContent = statusWarm
      ? 'No incidents yet.'
      : 'No incidents yet. Status uses default thresholds until the baseline is ready.';
    return;
  }
  setVisible(emptyEl, false);
  incidents.forEach((inc) => {
    const tr = document.createElement('tr');
    const state = inc.overall_state || '—';
    tr.innerHTML = `
      <td>${inc.id}</td>
      <td><span class="${stateClass(inc.overall_state)}">${state}</span></td>
      <td>${fmtTs(inc.start_ts)}</td>
      <td>${inc.end_ts ? fmtTs(inc.end_ts) : 'active'}</td>
      <td><a href="/api/v1/incidents/${inc.id}/export" download>Export</a></td>
    `;
    tbody.appendChild(tr);
  });
}

async function refresh() {
  try {
    const res = await fetch('/api/v1/status');
    const data = await res.json();
    applyStatus(data);
    await Promise.all([loadRollups(), loadIncidents()]);
  } catch (e) {
    console.error(e);
  }
}

refresh();
setInterval(refresh, 30000);
