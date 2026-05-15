// Netquality Dashboard — modern UI matching target design
const stateClass = (s) => {
  if (!s) return 'state-badge state-unknown';
  const k = s.toLowerCase();
  return `state-badge ${k === 'ok' ? 'ok' : k === 'degraded' ? 'degraded' : k === 'down' ? 'down' : 'state-unknown'}`;
};

let statusWarm = true;
let lastStates = [];
let lastRollups = [];
let lastIncidents = [];
let currentTimeWindow = '24h';
let incidentsPage = 1;
const INCIDENTS_PER_PAGE = 8;

const baselineLabel = (mode) => {
  if (mode === 'baseline_active') return 'Baseline active';
  if (mode === 'learning') return 'Learning baseline';
  return mode || 'Unknown';
};

function fmtTs(ts) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function shortTs(ts) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
}

function durationSince(ts) {
  if (!ts) return '';
  const sec = Math.floor(Date.now() / 1000 - ts);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
  return `${Math.floor(sec / 86400)}d ${Math.floor((sec % 86400) / 3600)}h`;
}

function fmtDuration(startTs, endTs) {
  if (!startTs) return '—';
  const end = endTs || Math.floor(Date.now() / 1000);
  const sec = Math.max(0, end - startTs);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
  return `${Math.floor(sec / 86400)}d ${Math.floor((sec % 86400) / 3600)}h`;
}

function setVisible(el, show) {
  if (!el) return;
  el.classList.toggle('hidden', !show);
}

function getStateColor(state) {
  const s = (state || '').toLowerCase();
  if (s === 'ok') return '#22c55e';
  if (s === 'degraded') return '#eab308';
  if (s === 'down') return '#ef4444';
  return '#6b7688';
}

function getIconForDim(dim) {
  const d = (dim || '').toLowerCase();
  if (d === 'dns' || d.startsWith('dns:')) return '🌐';
  if (d === 'gateway') return '🛡️';
  if (d.startsWith('path:') || d.includes('cloudflare')) return '☁️';
  if (d.includes('google')) return 'G';
  return '📡';
}

function countByState(states) {
  let ok = 0, deg = 0, down = 0;
  (states || []).forEach(s => {
    if (s.dimension === 'overall') return;
    const st = (s.state || '').toLowerCase();
    if (st === 'ok') ok++;
    else if (st === 'degraded') deg++;
    else if (st === 'down') down++;
  });
  return { ok, degraded: deg, down };
}

function renderPills(states) {
  const { ok, degraded, down } = countByState(states);
  const container = document.getElementById('status-pills');
  if (!container) return;
  container.innerHTML = `
    <div class="pill pill-ok"><span class="dot"></span><span class="num">${ok}</span> <span class="lbl">OK</span></div>
    <div class="pill pill-degraded"><span class="dot"></span><span class="num">${degraded}</span> <span class="lbl">Degraded</span></div>
    <div class="pill pill-down"><span class="dot"></span><span class="num">${down}</span> <span class="lbl">Down</span></div>
  `;
}

function renderOverall(data) {
  const states = data.states || [];
  const overall = states.find(s => s.dimension === 'overall');
  const stateEl = document.getElementById('overall-state');
  const durEl = document.getElementById('overall-duration');
  const sinceEl = document.getElementById('overall-since');
  const noteEl = document.getElementById('overall-thresholds');

  if (overall && overall.state) {
    const st = overall.state;
    const stLower = st.toLowerCase();
    stateEl.textContent = st.toUpperCase();
    stateEl.className = `overall-state ${stLower === 'ok' ? 'ok' : stLower === 'degraded' ? 'degraded' : 'down'}`;
    durEl.textContent = `for ${durationSince(overall.since_ts)}`;
    sinceEl.textContent = `since ${fmtTs(overall.since_ts)}`;

    const iconWrap = document.getElementById('overall-icon');
    if (iconWrap) {
      iconWrap.className = `card-icon ${stLower}`;
      // keep warning triangle for non-ok; could swap svg for ok but triangle+color is fine
    }
  } else {
    stateEl.textContent = '—';
    stateEl.className = 'overall-state state-unknown';
    durEl.textContent = 'Waiting for data…';
    sinceEl.textContent = '';
    const iconWrap = document.getElementById('overall-icon');
    if (iconWrap) iconWrap.className = 'card-icon';
  }

  if (!data.warm) {
    noteEl.textContent = 'Thresholds: config defaults (baseline not active yet).';
  } else {
    noteEl.textContent = 'Using learned baseline thresholds.';
  }
}

function renderLearning(data) {
  const card = document.getElementById('learning-card');
  const body = document.getElementById('learning-body');
  const ready = document.getElementById('learning-ready');
  const text = document.getElementById('learning-text');
  const bar = document.getElementById('learning-progress');
  const day = document.getElementById('learning-day');
  const pct = document.getElementById('learning-pct');

  if (!data.learning || data.warm) {
    setVisible(body, false);
    setVisible(ready, true);
    ready.textContent = 'Baseline active — using learned thresholds.';
    return;
  }
  setVisible(body, true);
  setVisible(ready, false);

  const L = data.learning;
  const warmup = L.warmup_days || 14;
  text.textContent = 'Monitoring active with config thresholds. Personalized anomaly detection starts after ~14 days of measurements.';

  const progress = Math.min(1, Math.max(0, L.time_progress || 0));
  bar.style.width = `${Math.round(progress * 100)}%`;

  if (!L.first_sample_ts) {
    day.textContent = 'Collecting first samples…';
    pct.textContent = '';
  } else {
    const d = Math.min(warmup, Math.max(1, Math.ceil(L.days_collected || 0)));
    day.textContent = `Day ${d} of ${warmup}`;
    pct.textContent = `${Math.round(progress * 100)}%`;
  }
}

function getRecentRollupsForProbe(rollups, probe, limit = 36) {
  return rollups
    .filter(r => r.probe === probe && r.metric === 'latency_ms')
    .sort((a, b) => a.bucket_ts - b.bucket_ts)
    .slice(-limit);
}

function renderLayerCards(states, rollups) {
  const container = document.getElementById('layer-cards');
  container.innerHTML = '';

  const layerStates = states.filter(s => s.dimension !== 'overall');
  if (layerStates.length === 0) {
    container.innerHTML = '<div class="empty-state" style="grid-column:1/-1">No layer data yet. Waiting for first probe cycle.</div>';
    return;
  }

  // sort: dns, gateway, then others
  layerStates.sort((a, b) => {
    const order = (d) => d === 'dns' ? 0 : d === 'gateway' ? 1 : 2;
    return order(a.dimension) - order(b.dimension) || a.dimension.localeCompare(b.dimension);
  });

  layerStates.forEach(s => {
    const dim = s.dimension;
    const st = s.state || 'unknown';
    const color = getStateColor(st);
    const icon = getIconForDim(dim);
    const recent = getRecentRollupsForProbe(rollups, dim, 42);

    const card = document.createElement('div');
    card.className = 'layer-card';

    let sparkHTML = '';
    if (recent.length > 1) {
      const vals = recent.map(r => r.avg_val);
      const minV = Math.min(...vals);
      const maxV = Math.max(...vals, 1);
      const range = maxV - minV || 1;
      const w = 100;
      const h = 22;
      const pts = recent.map((r, i) => {
        const x = (i / (recent.length - 1)) * w;
        const y = h - ((r.avg_val - minV) / range) * (h - 2);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      }).join(' ');
      sparkHTML = `<svg class="layer-spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none"><polyline points="${pts}" fill="none" stroke="${color}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>`;
    } else {
      sparkHTML = `<div style="height:26px;color:#555;font-size:10px;padding-top:6px">—</div>`;
    }

    card.innerHTML = `
      <div class="layer-head">
        <span class="layer-icon">${icon}</span>
        <span class="layer-name">${dim}</span>
        <span class="layer-state ${st}">${st}</span>
      </div>
      <div class="layer-duration">${durationSince(s.since_ts)}</div>
      ${sparkHTML}
    `;
    // Drill-down affordance: click card to open Analysis filtered to this probe
    card.style.cursor = 'pointer';
    card.title = `View detailed analysis for ${dim}`;
    card.addEventListener('click', () => {
      document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
      const analysisNav = document.getElementById('nav-analysis');
      if (analysisNav) analysisNav.classList.add('active');
      showAnalysisView(dim); // pass probe name for pre-selection
      console.log(`[Netquality] Drill-down requested for layer: ${dim}`);
    });
    container.appendChild(card);
  });
}

function drawLatencyChart(canvas, rollups, states, windowSec) {
  if (!canvas) return;
  const ctx = canvas.getContext('2d', { alpha: true });
  const dpr = window.devicePixelRatio || 1;
  const cssW = canvas.clientWidth || 900;
  const cssH = 280;
  canvas.width = Math.floor(cssW * dpr);
  canvas.height = Math.floor(cssH * dpr);
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, cssW, cssH);

  const latencyRollups = rollups.filter(r => r.metric === 'latency_ms');
  const byProbe = {};
  latencyRollups.forEach(r => {
    if (!byProbe[r.probe]) byProbe[r.probe] = [];
    byProbe[r.probe].push(r);
  });

  const probes = Object.keys(byProbe).sort((a, b) => {
    const oa = (a === 'dns' ? 0 : a === 'gateway' ? 1 : 2);
    const ob = (b === 'dns' ? 0 : b === 'gateway' ? 1 : 2);
    return oa - ob || a.localeCompare(b);
  });
  if (probes.length === 0) {
    ctx.fillStyle = '#6b7688';
    ctx.font = '13px system-ui';
    ctx.fillText('Collecting rollups — chart appears after first 5-minute aggregation.', 24, 40);
    return;
  }

  // state map for color
  const stateMap = {};
  (states || []).forEach(s => { stateMap[s.dimension] = s.state; });

  // time window
  const now = Math.floor(Date.now() / 1000);
  const start = now - windowSec;

  // collect all points in window
  let globalMax = 1;
  const series = probes.map(p => {
    let rows = (byProbe[p] || []).filter(r => r.bucket_ts >= start).sort((a, b) => a.bucket_ts - b.bucket_ts);
    // downsample if too many points
    if (rows.length > 380) {
      const step = Math.ceil(rows.length / 380);
      rows = rows.filter((_, i) => i % step === 0);
    }
    rows.forEach(r => { globalMax = Math.max(globalMax, r.avg_val); });
    return { probe: p, rows, color: getStateColor(stateMap[p]) };
  });

  const padL = 38, padR = 12, padT = 14, padB = 26;
  const chartW = cssW - padL - padR;
  const chartH = cssH - padT - padB;

  // bg grid
  ctx.fillStyle = '#0f141c';
  ctx.fillRect(0, 0, cssW, cssH);
  ctx.strokeStyle = '#222a38';
  ctx.lineWidth = 1;

  // horizontal grid + y labels
  const yTicks = 5;
  ctx.font = '11px system-ui';
  ctx.fillStyle = '#6b7688';
  ctx.textAlign = 'right';
  for (let i = 0; i <= yTicks; i++) {
    const y = padT + (chartH * i / yTicks);
    const val = Math.round(globalMax * (1 - i / yTicks));
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(cssW - padR, y);
    ctx.stroke();
    ctx.fillText(val + ' ms', padL - 6, y + 3);
  }

  // vertical time ticks
  const hours = Math.max(1, Math.round(windowSec / 3600));
  const tickHours = hours > 48 ? 12 : hours > 24 ? 6 : 3;
  ctx.textAlign = 'center';
  for (let t = start; t <= now; t += tickHours * 3600) {
    const x = padL + ((t - start) / windowSec) * chartW;
    if (x < padL || x > cssW - padR) continue;
    ctx.beginPath();
    ctx.moveTo(x, padT);
    ctx.lineTo(x, padT + chartH);
    ctx.stroke();
    const label = new Date(t * 1000).toLocaleTimeString([], { hour: 'numeric', hour12: false });
    ctx.fillText(label, x, cssH - 8);
  }

  // draw series
  series.forEach(({ rows, color }) => {
    if (rows.length < 2) return;
    ctx.strokeStyle = color;
    ctx.fillStyle = color + '33';
    ctx.lineWidth = 2.0;
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';

    ctx.beginPath();
    rows.forEach((r, i) => {
      const x = padL + ((r.bucket_ts - start) / windowSec) * chartW;
      const y = padT + chartH - (Math.min(r.avg_val, globalMax) / globalMax) * chartH;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();

    // small dots at recent points
    ctx.fillStyle = color;
    rows.slice(-6).forEach(r => {
      const x = padL + ((r.bucket_ts - start) / windowSec) * chartW;
      const y = padT + chartH - (Math.min(r.avg_val, globalMax) / globalMax) * chartH;
      ctx.beginPath();
      ctx.arc(x, y, 1.6, 0, Math.PI * 2);
      ctx.fill();
    });
  });

  // legend below is rendered separately
}

function renderChartLegend(states) {
  const el = document.getElementById('chart-legend');
  if (!el) return;
  const layerStates = (states || []).filter(s => s.dimension !== 'overall');
  el.innerHTML = layerStates.map(s => {
    const c = getStateColor(s.state);
    return `<span class="legend-item"><span class="legend-dot" style="background:${c}"></span>${s.dimension}</span>`;
  }).join('');
}

function computeLayerCount(inc) {
  if (!inc.detail_json) return 0;
  try {
    const d = JSON.parse(inc.detail_json);
    const dims = d.dimensions || {};
    return Object.keys(dims).filter(k => k !== 'overall' && dims[k] && dims[k] !== 'ok').length;
  } catch { return 0; }
}

function getAffectedLayers(inc) {
  if (!inc.detail_json) return [];
  try {
    const d = JSON.parse(inc.detail_json);
    const dims = d.dimensions || {};
    return Object.entries(dims)
      .filter(([k, v]) => k !== 'overall' && v && v !== 'ok')
      .map(([k, v]) => ({ name: k, state: v }));
  } catch { return []; }
}

// New helpers for the enriched incident detail (post RGR1 backend change)
function parseIncidentDetail(inc) {
  if (!inc.detail_json) return null;
  try {
    return JSON.parse(inc.detail_json);
  } catch {
    return null;
  }
}

function getTimelineEvents(detail) {
  if (!detail) return [];
  const events = [];
  if (detail.opened_at) events.push({ label: 'Opened', ts: detail.opened_at, state: detail.dimensions ? Object.values(detail.dimensions).find(v => v !== 'ok') : null });
  if (detail.escalated) events.push({ label: 'Escalated', ts: detail.escalated, note: detail.new_state ? `to ${detail.new_state}` : '' });
  if (detail.closed_at) events.push({ label: detail.resolved ? 'Resolved' : 'Closed', ts: detail.closed_at });
  if (detail.escalated_from) events.push({ label: 'Escalated from', note: `#${detail.escalated_from}` });
  return events;
}

function getTriggerDetails(detail) {
  if (!detail || !detail.detail) return [];
  const out = [];
  for (const [dim, info] of Object.entries(detail.detail)) {
    if (dim === 'overall' || !info) continue;
    const state = (detail.dimensions || {})[dim];
    if (!state || state === 'ok') continue;

    const metrics = info.metrics || {};
    const th = info.thresholds || {};
    const proposed = info.proposed || state;

    out.push({
      dimension: dim,
      state,
      proposed,
      metrics: {
        latency: metrics.LatencyMs || metrics.latency_ms,
        loss: metrics.LossPct || metrics.loss_pct,
        jitter: metrics.JitterMs || metrics.jitter_ms,
        fails: metrics.FailCount || metrics.fail_count,
      },
      thresholds: th,
    });
  }
  return out;
}

function renderIncidents(incidents, totalCount = null) {
  const tbody = document.getElementById('incidents-tbody');
  const empty = document.getElementById('incidents-empty');
  const info = document.getElementById('page-info');
  const prev = document.getElementById('page-prev');
  const next = document.getElementById('page-next');

  tbody.innerHTML = '';
  if (!incidents || incidents.length === 0) {
    setVisible(empty, true);
    info.textContent = 'Showing 0 of 0';
    prev.disabled = true;
    next.disabled = true;
    return;
  }
  setVisible(empty, false);

  const total = totalCount != null ? totalCount : incidents.length;
  const startIdx = (incidentsPage - 1) * INCIDENTS_PER_PAGE;
  const pageItems = incidents.slice(startIdx, startIdx + INCIDENTS_PER_PAGE);

  pageItems.forEach(inc => {
    const tr = document.createElement('tr');
    const st = inc.overall_state || '—';
    const layersCnt = computeLayerCount(inc);
    const dur = inc.end_ts ? fmtDuration(inc.start_ts, inc.end_ts) : 'Active';
    const layersBadge = layersCnt > 0
      ? `<span class="layer-count ${st.toLowerCase()}">${layersCnt}</span>`
      : '<span class="layer-count">—</span>';

    tr.innerHTML = `
      <td>${inc.id}</td>
      <td><span class="${stateClass(st)}">${st}</span></td>
      <td>${shortTs(inc.start_ts)}</td>
      <td>${inc.end_ts ? shortTs(inc.end_ts) : '<span style="color:#f59e0b">active</span>'}</td>
      <td>${dur}</td>
      <td>${layersBadge}</td>
      <td><button class="action-btn" data-id="${inc.id}">Details ›</button></td>
    `;
    tbody.appendChild(tr);
  });

  // pagination
  const maxPage = Math.max(1, Math.ceil(incidents.length / INCIDENTS_PER_PAGE));
  info.textContent = `Showing ${startIdx + 1}-${Math.min(startIdx + pageItems.length, incidents.length)} of ${total}`;
  prev.disabled = incidentsPage <= 1;
  next.disabled = incidentsPage >= maxPage;

  // attach detail handlers
  tbody.querySelectorAll('button.action-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = parseInt(btn.dataset.id, 10);
      const inc = incidents.find(i => i.id === id);
      if (inc) showIncidentModal(inc);
    });
  });
}

function updatePagination(incidents) {
  const prev = document.getElementById('page-prev');
  const next = document.getElementById('page-next');
  if (!prev || !next) return;

  prev.onclick = () => {
    if (incidentsPage > 1) {
      incidentsPage--;
      renderIncidents(incidents);
    }
  };
  next.onclick = () => {
    const max = Math.ceil(incidents.length / INCIDENTS_PER_PAGE);
    if (incidentsPage < max) {
      incidentsPage++;
      renderIncidents(incidents);
    }
  };
}

async function showIncidentModal(inc) {
  const modal = document.getElementById('incident-modal');
  if (!modal) return;

  document.getElementById('modal-title').textContent = `Incident #${inc.id}`;
  const stEl = document.getElementById('modal-state');
  stEl.textContent = inc.overall_state || '—';
  stEl.className = `state-badge ${inc.overall_state ? inc.overall_state.toLowerCase() : ''}`;

  document.getElementById('modal-start').textContent = fmtTs(inc.start_ts);
  document.getElementById('modal-end').textContent = inc.end_ts ? fmtTs(inc.end_ts) : 'Active';
  document.getElementById('modal-duration').textContent = inc.end_ts ? fmtDuration(inc.start_ts, inc.end_ts) : 'Ongoing';

  // Affected layers (existing)
  const layersEl = document.getElementById('modal-layers');
  const aff = getAffectedLayers(inc);
  layersEl.innerHTML = aff.length
    ? aff.map(l => `<span class="modal-layer-pill">${l.name} <span class="st ${l.state}">${l.state}</span></span>`).join('')
    : '<span class="modal-layer-pill">No layer details recorded</span>';

  // New rich sections
  const detail = parseIncidentDetail(inc);

  // Timeline
  const timelineEl = document.getElementById('modal-timeline');
  const events = getTimelineEvents(detail);
  timelineEl.innerHTML = events.length
    ? events.map(ev => `<div class="timeline-row"><span class="tl-label">${ev.label}</span> <span class="tl-ts">${ev.ts || ''}</span> ${ev.note ? `<span class="tl-note">${ev.note}</span>` : ''}</div>`).join('')
    : '<span class="muted">No timeline events recorded</span>';

  // Triggering measurements (the key new value from enriched backend detail)
  const triggersEl = document.getElementById('modal-triggers');
  const triggers = getTriggerDetails(detail);
  if (triggers.length) {
    triggersEl.innerHTML = triggers.map(t => {
      const th = t.thresholds || {};
      const latTh = th.latency_ms || th['latency_ms'] || {};
      const lossTh = th.loss_pct || th['loss_pct'] || {};
      return `
        <div class="trigger-row ${t.state}">
          <div class="trigger-head">
            <span class="trig-dim">${t.dimension}</span>
            <span class="state-badge ${t.state}">${t.state}</span>
            <span class="trig-proposed">(proposed ${t.proposed})</span>
          </div>
          <div class="trigger-metrics">
            ${t.metrics.latency != null ? `<span>Latency: <strong>${t.metrics.latency} ms</strong></span>` : ''}
            ${t.metrics.loss != null ? `<span>Loss: <strong>${t.metrics.loss}%</strong></span>` : ''}
            ${t.metrics.jitter != null ? `<span>Jitter: ${t.metrics.jitter} ms</span>` : ''}
            ${t.metrics.fails != null ? `<span>Fails: ${t.metrics.fails}</span>` : ''}
          </div>
          <div class="trigger-th">
            ${latTh.degraded != null ? `<span>Latency th: ${latTh.degraded} / ${latTh.down} <small>(${latTh.source || 'config'})</small></span>` : ''}
            ${lossTh.degraded != null ? `<span>Loss th: ${lossTh.degraded}% / ${lossTh.down}% <small>(config)</small></span>` : ''}
          </div>
        </div>`;
    }).join('');
  } else {
    triggersEl.innerHTML = '<span class="muted">No detailed trigger metrics recorded for this incident.</span>';
  }

  // Raw technical detail (now collapsed)
  let pretty = inc.detail_json || '{}';
  try { pretty = JSON.stringify(JSON.parse(inc.detail_json), null, 2); } catch {}
  document.getElementById('modal-detail').textContent = pretty;

  const exportLink = document.getElementById('modal-export');
  exportLink.href = `/api/v1/incidents/${inc.id}/export`;
  exportLink.download = `incident-${inc.id}.json`;

  // View in Analysis (wired later when Analysis surface exists)
  const viewAnalysisBtn = document.getElementById('modal-view-analysis');
  if (viewAnalysisBtn) {
    viewAnalysisBtn.onclick = () => {
      modal.classList.add('hidden');
      // Pass incident start time + first affected probe (best effort pre-filter)
      const aff = getAffectedLayers(inc);
      const firstProbe = aff.length ? aff[0].name : null;
      document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
      const analysisNav = document.getElementById('nav-analysis');
      if (analysisNav) analysisNav.classList.add('active');
      showAnalysisView(firstProbe, inc.start_ts);
    };
  }

  modal.classList.remove('hidden');

  const close = () => modal.classList.add('hidden');
  document.getElementById('modal-close').onclick = close;
  document.getElementById('modal-close2').onclick = close;
  modal.querySelector('.modal-backdrop').onclick = close;
}

function renderDevice(deviceId) {
  const sel = document.getElementById('device-option');
  if (sel) sel.textContent = deviceId || 'device';
  const picker = document.querySelector('.device-picker select');
  if (picker) picker.title = deviceId || '';
}

function setupNav() {
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', (e) => {
      document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
      item.classList.add('active');

      const section = item.dataset.section;
      if (section === 'analysis') {
        showAnalysisView();
      } else {
        showOverviewView();
      }
    });
  });
}

let analysisVisible = false;

function showAnalysisView(preferredProbe = null, preferredSince = null) {
  analysisVisible = true;

  const content = document.querySelector('.content-area');
  if (content) content.style.display = 'none';

  let panel = document.getElementById('analysis-panel');
  if (!panel) {
    panel = document.createElement('div');
    panel.id = 'analysis-panel';
    panel.className = 'analysis-panel';
    document.querySelector('.main-content').appendChild(panel);
  }
  panel.style.display = 'block';

  // Build rich Analysis UI if not already built
  if (!panel.dataset.built) {
    panel.innerHTML = `
      <div class="section">
        <div class="section-header" style="justify-content:space-between">
          <div><span class="section-icon">🔍</span><h2>Analysis</h2></div>
          <button id="analysis-back" class="btn-secondary">← Back to Overview</button>
        </div>
      </div>

      <div class="card" style="margin-bottom:16px;padding:14px 16px">
        <div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap">
          <div>
            <label style="font-size:11px;color:var(--muted);display:block;margin-bottom:2px">Probe</label>
            <select id="analysis-probe" style="background:var(--card);border:1px solid var(--card-border);color:var(--text);padding:6px 10px;border-radius:6px;font-size:13px"></select>
          </div>
          <div>
            <label style="font-size:11px;color:var(--muted);display:block;margin-bottom:2px">Time range</label>
            <select id="analysis-time" style="background:var(--card);border:1px solid var(--card-border);color:var(--text);padding:6px 10px;border-radius:6px;font-size:13px">
              <option value="24h">Last 24 hours</option>
              <option value="7d">Last 7 days</option>
              <option value="30d">Last 30 days</option>
            </select>
          </div>
          <button id="analysis-refresh" class="btn-secondary" style="margin-top:18px">Refresh</button>
        </div>
      </div>

      <!-- Stats row -->
      <div id="analysis-stats" class="top-cards" style="margin-bottom:16px"></div>

      <!-- Chart -->
      <div class="section">
        <div class="section-header">
          <div><span class="section-icon">📈</span><h2>Latency (avg + p95)</h2></div>
        </div>
        <div class="chart-wrap">
          <canvas id="analysis-chart" width="900" height="260"></canvas>
        </div>
        <div id="analysis-legend" class="chart-legend"></div>
      </div>

      <!-- Raw samples table -->
      <div class="section">
        <div class="section-header">
          <div><span class="section-icon">📋</span><h2>Recent raw samples</h2></div>
        </div>
        <div class="card" style="padding:0;overflow:hidden">
          <table id="analysis-samples-table" style="width:100%;font-size:12px">
            <thead><tr>
              <th style="padding:8px 10px;text-align:left;border-bottom:1px solid var(--card-border)">Time</th>
              <th style="padding:8px 10px;text-align:right;border-bottom:1px solid var(--card-border)">Latency (ms)</th>
              <th style="padding:8px 10px;text-align:right;border-bottom:1px solid var(--card-border)">Loss %</th>
              <th style="padding:8px 10px;text-align:right;border-bottom:1px solid var(--card-border)">Jitter</th>
            </tr></thead>
            <tbody id="analysis-samples-tbody"></tbody>
          </table>
          <div id="analysis-samples-empty" class="empty-state" style="padding:12px">No samples loaded yet.</div>
        </div>
      </div>
    `;
    panel.dataset.built = 'true';

    // Wire controls
    document.getElementById('analysis-back').onclick = () => {
      document.querySelectorAll('.nav-item').forEach(i => i.classList.remove('active'));
      const overviewNav = document.querySelector('.nav-item[data-section="overview"]');
      if (overviewNav) overviewNav.classList.add('active');
      showOverviewView();
    };
    document.getElementById('analysis-refresh').onclick = () => refreshAnalysis();
    document.getElementById('analysis-probe').onchange = () => refreshAnalysis();
    document.getElementById('analysis-time').onchange = () => refreshAnalysis();
  }

  // Populate probe selector from existing rollups
  populateAnalysisProbes();

  // Apply context if coming from layer card or incident
  const probeSel = document.getElementById('analysis-probe');
  const timeSel = document.getElementById('analysis-time');

  if (preferredProbe && probeSel) {
    // Try to select it (will be populated shortly)
    setTimeout(() => {
      if ([...probeSel.options].some(o => o.value === preferredProbe)) {
        probeSel.value = preferredProbe;
      }
    }, 10);
  }
  if (preferredSince && timeSel) {
    const hours = Math.round((Date.now()/1000 - preferredSince) / 3600);
    if (hours > 24*6) timeSel.value = '30d';
    else if (hours > 24) timeSel.value = '7d';
    else timeSel.value = '24h';
  }

  refreshAnalysis();
}

function showOverviewView() {
  analysisVisible = false;
  const content = document.querySelector('.content-area');
  if (content) content.style.display = '';

  const panel = document.getElementById('analysis-panel');
  if (panel) panel.style.display = 'none';

  // Trigger a refresh of the main dashboard when coming back
  if (lastStates.length === 0) {
    refresh();
  }
}

function setupTimeRange() {
  const sel = document.getElementById('time-range');
  if (!sel) return;
  sel.value = currentTimeWindow;
  sel.addEventListener('change', async () => {
    currentTimeWindow = sel.value;
    await loadRollupsAndChart();
  });
}

function setupExportAll() {
  const btn = document.getElementById('export-incidents');
  if (!btn) return;
  btn.addEventListener('click', () => {
    if (!lastIncidents.length) return;
    const rows = [['id', 'state', 'start', 'end', 'duration', 'layers']];
    lastIncidents.forEach(inc => {
      const layers = computeLayerCount(inc);
      rows.push([
        inc.id,
        inc.overall_state || '',
        fmtTs(inc.start_ts),
        inc.end_ts ? fmtTs(inc.end_ts) : 'active',
        inc.end_ts ? fmtDuration(inc.start_ts, inc.end_ts) : 'active',
        layers
      ]);
    });
    const csv = rows.map(r => r.join(',')).join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'netquality-incidents.csv';
    a.click();
    URL.revokeObjectURL(url);
  });
}

function getWindowSeconds() {
  if (currentTimeWindow === '7d') return 7 * 86400;
  if (currentTimeWindow === '30d') return 30 * 86400;
  return 86400;
}

// === Analysis view helpers ===

let analysisProbe = null;
let analysisWindow = '24h';
let analysisRollups = [];
let analysisSamples = [];

function populateAnalysisProbes() {
  const sel = document.getElementById('analysis-probe');
  if (!sel || !lastRollups.length) return;

  const probes = [...new Set(lastRollups.map(r => r.probe))].sort((a,b) => {
    const oa = (a === 'dns' ? 0 : a === 'gateway' ? 1 : 2);
    const ob = (b === 'dns' ? 0 : b === 'gateway' ? 1 : 2);
    return oa - ob || a.localeCompare(b);
  });

  const current = sel.value;
  sel.innerHTML = '';
  probes.forEach(p => {
    const opt = document.createElement('option');
    opt.value = p;
    opt.textContent = p;
    sel.appendChild(opt);
  });
  if (current && probes.includes(current)) sel.value = current;
  else if (probes.length) sel.value = probes[0];
}

async function refreshAnalysis() {
  const probeSel = document.getElementById('analysis-probe');
  const timeSel = document.getElementById('analysis-time');
  if (!probeSel || !timeSel) return;

  analysisProbe = probeSel.value;
  analysisWindow = timeSel.value;

  const nowSec = Math.floor(Date.now() / 1000);
  let sinceSec;
  if (analysisWindow === '7d') sinceSec = nowSec - 7*86400;
  else if (analysisWindow === '30d') sinceSec = nowSec - 30*86400;
  else sinceSec = nowSec - 86400;

  try {
    // Reuse the same rollups endpoint (already loaded in many cases)
    const res = await fetch(`/api/v1/rollups?since=${sinceSec}`);
    const allRollups = await res.json();
    analysisRollups = allRollups.filter(r => r.probe === analysisProbe);

    // Draw richer chart (avg + p95)
    drawAnalysisChart(analysisRollups, sinceSec);

    // Stats
    renderAnalysisStats(analysisRollups);

    // Raw samples
    await loadAnalysisSamples(analysisProbe, sinceSec);
  } catch (e) {
    console.error('analysis refresh', e);
  }
}

function drawAnalysisChart(rollups, startTs) {
  const canvas = document.getElementById('analysis-chart');
  if (!canvas) return;
  const ctx = canvas.getContext('2d', { alpha: true });
  const dpr = window.devicePixelRatio || 1;
  const cssW = canvas.clientWidth || 900;
  const cssH = 260;
  canvas.width = Math.floor(cssW * dpr);
  canvas.height = Math.floor(cssH * dpr);
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, cssW, cssH);

  const latencyRows = rollups.filter(r => r.metric === 'latency_ms').sort((a,b) => a.bucket_ts - b.bucket_ts);
  if (latencyRows.length < 2) {
    ctx.fillStyle = '#6b7688';
    ctx.font = '13px system-ui';
    ctx.fillText('Not enough rollups for this probe in the selected window.', 20, 30);
    return;
  }

  const now = Math.floor(Date.now() / 1000);
  const windowSec = now - startTs;

  let maxVal = 1;
  latencyRows.forEach(r => { maxVal = Math.max(maxVal, r.avg_val, r.p95_val); });

  const padL = 42, padR = 12, padT = 12, padB = 22;
  const w = cssW - padL - padR;
  const h = cssH - padT - padB;

  // background
  ctx.fillStyle = '#0f141c';
  ctx.fillRect(0, 0, cssW, cssH);

  // grid + labels
  ctx.strokeStyle = '#222a38';
  ctx.lineWidth = 1;
  ctx.font = '11px system-ui';
  ctx.fillStyle = '#6b7688';
  ctx.textAlign = 'right';
  for (let i = 0; i <= 4; i++) {
    const y = padT + (h * i / 4);
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(cssW - padR, y);
    ctx.stroke();
    ctx.fillText(Math.round(maxVal * (1 - i/4)) + ' ms', padL - 6, y + 3);
  }

  // time labels
  ctx.textAlign = 'center';
  const hours = Math.max(1, Math.round(windowSec / 3600));
  const step = hours > 48 ? 12 : hours > 12 ? 6 : 3;
  for (let t = startTs; t <= now; t += step * 3600) {
    const x = padL + ((t - startTs) / windowSec) * w;
    if (x < padL || x > cssW - padR) continue;
    ctx.beginPath();
    ctx.moveTo(x, padT);
    ctx.lineTo(x, padT + h);
    ctx.stroke();
    ctx.fillText(new Date(t*1000).toLocaleTimeString([], {hour:'numeric'}), x, cssH - 6);
  }

  // p95 line (lighter)
  ctx.strokeStyle = '#3b82f6';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  latencyRows.forEach((r, i) => {
    const x = padL + ((r.bucket_ts - startTs) / windowSec) * w;
    const y = padT + h - (Math.min(r.p95_val, maxVal) / maxVal) * h;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();

  // avg line (thicker, state color)
  ctx.strokeStyle = '#22c55e';
  ctx.lineWidth = 2.2;
  ctx.beginPath();
  latencyRows.forEach((r, i) => {
    const x = padL + ((r.bucket_ts - startTs) / windowSec) * w;
    const y = padT + h - (Math.min(r.avg_val, maxVal) / maxVal) * h;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();

  // legend
  const legend = document.getElementById('analysis-legend');
  if (legend) {
    legend.innerHTML = `
      <span class="legend-item"><span class="legend-dot" style="background:#22c55e"></span>avg</span>
      <span class="legend-item"><span class="legend-dot" style="background:#3b82f6"></span>p95</span>
    `;
  }
}

function renderAnalysisStats(rollups) {
  const container = document.getElementById('analysis-stats');
  if (!container) return;

  const latency = rollups.filter(r => r.metric === 'latency_ms');
  if (!latency.length) {
    container.innerHTML = '<div class="card" style="padding:12px">No data for stats.</div>';
    return;
  }

  const avgs = latency.map(r => r.avg_val);
  const p95s = latency.map(r => r.p95_val);
  const avg = avgs.reduce((a,b)=>a+b,0) / avgs.length;
  const p95 = Math.max(...p95s);
  const peak = Math.max(...avgs);

  // Get current threshold from lastStates if available
  let thInfo = '—';
  const st = (lastStates || []).find(s => s.dimension === analysisProbe);
  if (st && st.detail && st.detail.thresholds) {
    const t = st.detail.thresholds.latency_ms || st.detail.thresholds['latency_ms'];
    if (t) thInfo = `${t.degraded} / ${t.down} (${t.source})`;
  }

  container.innerHTML = `
    <div class="card"><div style="font-size:11px;color:var(--muted)">Avg latency</div><div style="font-size:22px;font-weight:700;margin-top:2px">${avg.toFixed(0)} ms</div></div>
    <div class="card"><div style="font-size:11px;color:var(--muted)">Peak (avg)</div><div style="font-size:22px;font-weight:700;margin-top:2px">${peak.toFixed(0)} ms</div></div>
    <div class="card"><div style="font-size:11px;color:var(--muted)">P95 (window)</div><div style="font-size:22px;font-weight:700;margin-top:2px">${p95.toFixed(0)} ms</div></div>
    <div class="card"><div style="font-size:11px;color:var(--muted)">Active latency threshold</div><div style="font-size:13px;margin-top:4px">${thInfo}</div></div>
  `;
}

async function loadAnalysisSamples(probe, since) {
  const tbody = document.getElementById('analysis-samples-tbody');
  const empty = document.getElementById('analysis-samples-empty');
  if (!tbody) return;

  try {
    const res = await fetch(`/api/v1/samples?probe=${encodeURIComponent(probe)}&from=${since}`);
    const samples = await res.json();
    analysisSamples = samples.slice(-80); // last 80

    tbody.innerHTML = '';
    if (!analysisSamples.length) {
      empty.style.display = 'block';
      return;
    }
    empty.style.display = 'none';

    analysisSamples.slice(-30).reverse().forEach(s => {
      const tr = document.createElement('tr');
      const ts = new Date(s.ts * 1000).toLocaleString([], {month:'short', day:'numeric', hour:'numeric', minute:'2-digit'});
      tr.innerHTML = `
        <td style="padding:5px 10px;border-bottom:1px solid var(--card-border)">${ts}</td>
        <td style="padding:5px 10px;text-align:right;border-bottom:1px solid var(--card-border)">${s.metric === 'latency_ms' ? s.value.toFixed(1) : '—'}</td>
        <td style="padding:5px 10px;text-align:right;border-bottom:1px solid var(--card-border)">${s.metric === 'loss_pct' ? s.value.toFixed(1) : '—'}</td>
        <td style="padding:5px 10px;text-align:right;border-bottom:1px solid var(--card-border)">${s.metric === 'jitter_ms' ? s.value.toFixed(1) : '—'}</td>
      `;
      tbody.appendChild(tr);
    });
  } catch (e) {
    console.error('samples', e);
    empty.style.display = 'block';
    empty.textContent = 'Failed to load samples.';
  }
}

async function loadRollupsAndChart() {
  const nowSec = Math.floor(Date.now() / 1000);
  const since = nowSec - getWindowSeconds();
  try {
    const res = await fetch(`/api/v1/rollups?since=${since}`);
    lastRollups = await res.json();
    const canvas = document.getElementById('latency-chart');
    drawLatencyChart(canvas, lastRollups, lastStates, getWindowSeconds());
    renderLayerCards(lastStates, lastRollups);
    renderChartLegend(lastStates);
  } catch (e) {
    console.error('rollups', e);
  }
}

async function loadIncidents() {
  try {
    const res = await fetch('/api/v1/incidents?limit=50');
    lastIncidents = await res.json();
    incidentsPage = 1;
    renderIncidents(lastIncidents);
    updatePagination(lastIncidents);
  } catch (e) {
    console.error('incidents', e);
  }
}

async function refresh() {
  try {
    const res = await fetch('/api/v1/status');
    const data = await res.json();

    lastStates = data.states || [];
    statusWarm = !!data.warm;

    renderDevice(data.device_id);
    renderPills(lastStates);
    renderOverall(data);
    renderLearning(data);

    await loadRollupsAndChart();
    await loadIncidents();
  } catch (e) {
    console.error('status refresh failed', e);
  }
}

// Boot
function init() {
  setupNav();
  setupTimeRange();
  setupExportAll();

  // Ensure we start on Overview
  const firstNav = document.querySelector('.nav-item');
  if (firstNav) firstNav.classList.add('active');

  // initial data load (Overview)
  refresh();
  // periodic refresh only affects Overview data; Analysis refreshes on demand
  setInterval(() => {
    if (!analysisVisible) refresh();
  }, 30000);

  // redraw main Overview chart on resize
  let resizeT;
  window.addEventListener('resize', () => {
    clearTimeout(resizeT);
    resizeT = setTimeout(() => {
      if (analysisVisible) return;
      const c = document.getElementById('latency-chart');
      if (c && lastRollups.length) {
        drawLatencyChart(c, lastRollups, lastStates, getWindowSeconds());
      }
    }, 160);
  });

  // keyboard ESC closes modal
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      const m = document.getElementById('incident-modal');
      if (m) m.classList.add('hidden');
    }
  });
}

init();
