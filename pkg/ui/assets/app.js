'use strict';

const NODE_W = 300;
const NODE_H = 52;
const COL_GAP = 90;
const ROW_GAP = 18;
const PAD = 16;
const SVG_NS = 'http://www.w3.org/2000/svg';

const state = {
  steps: new Map(),
  edges: [],
  dependents: new Map(),
  factory: '',
  stack: '',
  complete: null,
  selected: null,
  runStartedAt: 0,
  es: null,
};

function $(id) {
  return document.getElementById(id);
}

function handleFrame(f) {
  switch (f.kind) {
    case 'graph': initGraph(f); break;
    case 'snapshot': applySnapshot(f); break;
    case 'apply-event': applyEvent(f); break;
    case 'run-complete': finishRun(f); break;
  }
}

function connect() {
  const es = new EventSource('events');
  state.es = es;
  es.onmessage = (e) => handleFrame(JSON.parse(e.data));
  es.onerror = () => {
    if (!state.complete) setStatus('reconnecting...', '');
  };
}

// Output steps are sinks with no display value; hide them and splice
// any edge that passes through one so the visible graph stays
// connected.
function initGraph(f) {
  state.factory = f.factory;
  state.stack = f.stack || '';
  $('title').textContent = state.stack
    ? state.factory + ' · ' + state.stack
    : state.factory;
  state.steps = new Map();
  state.complete = null;
  state.selected = null;
  state.runStartedAt = Date.now();

  const hidden = new Set();
  const depsOf = new Map();
  for (const n of f.steps) {
    depsOf.set(n.address, n['depends-on'] || []);
    if (n.kind === 'output') hidden.add(n.address);
  }
  for (const n of f.steps) {
    if (hidden.has(n.address)) continue;
    state.steps.set(n.address, {
      node: n,
      stage: 'pending',
      decision: n.decision,
      startedAt: 0,
      elapsedMs: 0,
      err: '',
      el: null,
      badgeEl: null,
      tagEl: null,
    });
  }
  state.edges = [];
  state.dependents = new Map([...state.steps.keys()].map((a) => [a, []]));
  for (const addr of state.steps.keys()) {
    for (const dep of visibleDeps(addr, hidden, depsOf)) {
      if (!state.steps.has(dep) || dep === addr) continue;
      state.edges.push({ from: dep, to: addr });
      state.dependents.get(dep).push(addr);
    }
  }
  render();
  updateStatus();
}

function visibleDeps(addr, hidden, depsOf) {
  const out = new Set();
  const seen = new Set();
  const stack = [...(depsOf.get(addr) || [])];
  while (stack.length) {
    const d = stack.pop();
    if (seen.has(d)) continue;
    seen.add(d);
    if (hidden.has(d)) {
      stack.push(...(depsOf.get(d) || []));
    } else {
      out.add(d);
    }
  }
  return out;
}

// Layout via dagre: ranks flow left to right. Dagre reports node
// centers and per-edge waypoints whose endpoints already sit on the
// card borders, with long edges routed around the cards in between.
function layout() {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: 'LR',
    nodesep: ROW_GAP,
    ranksep: COL_GAP,
    marginx: PAD,
    marginy: PAD,
  });
  g.setDefaultEdgeLabel(() => ({}));
  for (const addr of state.steps.keys()) {
    g.setNode(addr, { width: NODE_W, height: NODE_H });
  }
  for (const e of state.edges) g.setEdge(e.from, e.to);
  dagre.layout(g);
  const coords = new Map();
  for (const addr of state.steps.keys()) {
    const n = g.node(addr);
    coords.set(addr, { x: n.x - NODE_W / 2, y: n.y - NODE_H / 2 });
  }
  const edgePoints = state.edges.map((e) => g.edge(e.from, e.to).points);
  const size = g.graph();
  return { coords, edgePoints, width: size.width, height: size.height };
}

function edgePath(points) {
  let d = 'M' + points[0].x + ' ' + points[0].y;
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1];
    const b = points[i];
    const mx = a.x + (b.x - a.x) / 2;
    d += ' C' + mx + ' ' + a.y + ', ' + mx + ' ' + b.y + ', ' + b.x + ' ' + b.y;
  }
  return d;
}

function el(name, attrs) {
  const node = document.createElementNS(SVG_NS, name);
  for (const k in attrs) node.setAttribute(k, attrs[k]);
  return node;
}

function labelText(addr) {
  const max = 36;
  if (addr.length <= max) return addr;
  return '…' + addr.slice(addr.length - max + 1);
}

function render() {
  const svg = $('graph');
  svg.textContent = '';
  if (!state.steps.size) {
    setStatus('nothing to do', 'complete');
    return;
  }
  const defs = el('defs', {});
  const hatch = el('pattern', {
    id: 'hatch', width: 7, height: 7,
    patternUnits: 'userSpaceOnUse', patternTransform: 'rotate(45)',
  });
  hatch.appendChild(el('rect', { width: 7, height: 7, fill: '#1a1814' }));
  hatch.appendChild(el('line', {
    x1: 0, y1: 0, x2: 0, y2: 7, stroke: '#2e261b', 'stroke-width': 3,
  }));
  defs.appendChild(hatch);
  svg.appendChild(defs);

  const { coords, edgePoints, width, height } = layout();
  const edgeLayer = el('g', {});
  state.edges.forEach((e, i) => {
    edgeLayer.appendChild(el('path', {
      class: 'edge',
      d: edgePath(edgePoints[i]),
    }));
  });
  svg.appendChild(edgeLayer);

  for (const [addr, st] of state.steps) {
    const { x, y } = coords.get(addr);
    const g = el('g', { transform: 'translate(' + x + ',' + y + ')' });
    g.appendChild(el('rect', {
      class: 'card', width: NODE_W, height: NODE_H,
      rx: st.node.kind === 'configuration' ? 14 : 6,
    }));
    const tag = el('text', { class: 'tag', x: 12, y: 19 });
    const badge = el('text', {
      class: 'badge', x: NODE_W - 12, y: 19, 'text-anchor': 'end',
    });
    const label = el('text', { class: 'label', x: 12, y: 39 });
    label.textContent = labelText(addr);
    const title = el('title', {});
    title.textContent = addr;
    g.appendChild(tag);
    g.appendChild(badge);
    g.appendChild(label);
    g.appendChild(title);
    g.addEventListener('click', () => select(addr));
    st.el = g;
    st.tagEl = tag;
    st.badgeEl = badge;
    svg.appendChild(g);
    updateStep(addr);
  }
  svg.setAttribute('width', width);
  svg.setAttribute('height', height);
}

const pastWord = {
  'create': 'created',
  'update': 'updated',
  'replace': 'replaced',
  'destroy': 'destroyed',
  'rerun': 'ran',
  'read': 'read',
  'eval': 'evaluated',
  'no-op': 'no change',
  'skip': 'skipped',
};

function fmtDur(ms) {
  if (ms < 1000) return ms + 'ms';
  const s = ms / 1000;
  if (s < 60) return (s < 10 ? s.toFixed(1) : s.toFixed(0)) + 's';
  const m = Math.floor(s / 60);
  const rs = Math.floor(s % 60);
  if (m < 60) return m + 'm' + String(rs).padStart(2, '0') + 's';
  const hh = Math.floor(m / 60);
  return hh + 'h' + String(m % 60).padStart(2, '0') + 'm';
}

function badgeText(st) {
  switch (st.stage) {
    case 'running':
      return fmtDur(Date.now() - st.startedAt);
    case 'done':
      if (st.decision === 'no-op' || st.decision === 'skip') {
        return pastWord[st.decision];
      }
      return (pastWord[st.decision] || st.decision) + ' · ' + fmtDur(st.elapsedMs);
    case 'fail':
      return 'failed · ' + fmtDur(st.elapsedMs);
    case 'blocked':
      return 'blocked';
    default:
      return '';
  }
}

function updateStep(addr) {
  const st = state.steps.get(addr);
  if (!st || !st.el) return;
  const classes = ['step', st.node.kind, st.stage, 'decision-' + st.decision];
  if (st.node.composite) classes.push('composite');
  if (state.selected === addr) classes.push('selected');
  st.el.setAttribute('class', classes.join(' '));
  st.tagEl.textContent = st.node.kind +
    (st.node.composite ? ' · composite' : '') +
    ' · ' + st.decision;
  st.badgeEl.textContent = badgeText(st);
  if (state.selected === addr) fillDetail(st);
}

function applyEvent(f) {
  const st = state.steps.get(f.address);
  if (!st) return;
  if (f.decision) st.decision = f.decision;
  if (f.stage === 'start') {
    st.stage = 'running';
    st.startedAt = Date.now() - (f['elapsed-ms'] || 0);
  } else if (f.stage === 'done') {
    st.stage = 'done';
    st.elapsedMs = f['elapsed-ms'] || 0;
  } else if (f.stage === 'fail') {
    st.stage = 'fail';
    st.elapsedMs = f['elapsed-ms'] || 0;
    st.err = f.err || '';
  }
  updateStep(f.address);
  updateStatus();
}

function applySnapshot(f) {
  for (const addr in f.steps) {
    const st = state.steps.get(addr);
    if (!st) continue;
    const entry = f.steps[addr];
    const elapsed = entry['elapsed-ms'] || 0;
    if (entry.decision) st.decision = entry.decision;
    if (entry.stage === 'start') {
      st.stage = 'running';
      st.startedAt = Date.now() - elapsed;
    } else if (entry.stage === 'done') {
      st.stage = 'done';
      st.elapsedMs = elapsed;
    } else if (entry.stage === 'fail') {
      st.stage = 'fail';
      st.elapsedMs = elapsed;
      st.err = entry.err || '';
    }
    updateStep(addr);
  }
  updateStatus();
}

function finishRun(f) {
  state.complete = f;
  if (!f.ok) {
    const reach = new Set();
    const stack = [];
    for (const [addr, st] of state.steps) {
      if (st.stage === 'fail') stack.push(addr);
    }
    while (stack.length) {
      const a = stack.pop();
      for (const d of state.dependents.get(a) || []) {
        if (!reach.has(d)) {
          reach.add(d);
          stack.push(d);
        }
      }
    }
    for (const [addr, st] of state.steps) {
      if (st.stage === 'pending' && reach.has(addr)) {
        st.stage = 'blocked';
        updateStep(addr);
      }
    }
  }
  updateStatus();
  if (state.es) state.es.close();
}

function counts() {
  let done = 0;
  let failed = 0;
  for (const [, st] of state.steps) {
    if (st.stage === 'done') done++;
    if (st.stage === 'fail') failed++;
  }
  return { done, failed, total: state.steps.size };
}

function setStatus(text, cls) {
  const status = $('status');
  status.textContent = text;
  status.className = cls;
}

function updateStatus() {
  const c = counts();
  const f = state.complete;
  if (!f) {
    const clock = fmtDur(Date.now() - state.runStartedAt);
    setStatus('applying — ' + c.done + '/' + c.total + ' — ' + clock, 'running');
    return;
  }
  const clock = fmtDur(f['elapsed-ms'] || 0);
  if (f.ok) {
    setStatus('complete — ' + f.succeeded + ' step' +
      (f.succeeded === 1 ? '' : 's') + ' — ' + clock, 'complete');
    return;
  }
  let text = 'failed';
  if (f.failed) text += ' — ' + f.failed + ' failed';
  if (f.message) text += ' — ' + f.message;
  if (f['not-run']) text += ' — ' + f['not-run'] + ' not run';
  setStatus(text + ' — ' + clock, 'failed');
}

function fillDetail(st) {
  $('detail-address').textContent = st.node.address;
  $('detail-kind').textContent = st.node.kind + (st.node.composite ? ' (composite)' : '');
  $('detail-decision').textContent = st.decision;
  $('detail-state').textContent = st.stage;
  const elapsed = st.stage === 'running'
    ? fmtDur(Date.now() - st.startedAt)
    : (st.stage === 'done' || st.stage === 'fail' ? fmtDur(st.elapsedMs) : '');
  $('detail-elapsed').textContent = elapsed || '—';
  const errEl = $('detail-error');
  errEl.hidden = !st.err;
  errEl.textContent = st.err;
}

function select(addr) {
  const prev = state.selected;
  state.selected = addr;
  if (prev) updateStep(prev);
  updateStep(addr);
  const st = state.steps.get(addr);
  fillDetail(st);
  $('detail').hidden = false;
}

function tick() {
  for (const [addr, st] of state.steps) {
    if (st.stage === 'running') updateStep(addr);
  }
  if (!state.complete && state.steps.size) updateStatus();
}

$('detail-close').addEventListener('click', () => {
  const prev = state.selected;
  state.selected = null;
  $('detail').hidden = true;
  if (prev) updateStep(prev);
});

connect();
setInterval(tick, 500);
