// hud.js — the controls that sit on top of the view.
//
// One bar, thumb-reachable on a phone and unobtrusive on a desktop: which
// pose of the building you're looking at, which floor, and which live layers
// are on. Geometry toggles and explicitly selectable room heatmaps sit behind
// the Layers button.

let hudBuilt = false;

function buildHud() {
  if (hudBuilt) return;
  hudBuilt = true;

  const btn = id => document.getElementById(id);

  // ── floor chips ──
  const chips = btn('hudFloors');
  if (chips) {
    const mk = (label, title, fn) => {
      const b = document.createElement('button');
      b.className = 'hud-chip';
      b.textContent = label;
      b.title = title;
      b.addEventListener('click', fn);
      chips.appendChild(b);
      return b;
    };
    hudChip3D = mk('3D', 'Stacked floor plates', () => { switchTo3D(); openBuilding({ still: true }); syncHud(); });
    BLDG.levels.forEach((l, i) => {
      hudChipFloor[i] = mk(BLDG.labels[i].replace('Floor ', 'F'), BLDG.labels[i],
        () => { switchToFloor(currentBuilding + '/' + l); syncHud(); });
    });
  }

  btn('btnBuilding').addEventListener('click', () => { toggleBuilding(); syncHud(); });
  btn('btnLayers').addEventListener('click', () => toggleHudPop('layers'));
  btn('btnFind').addEventListener('click', () => {
    toggleHudPop('find');
    if (hudPopOpen === 'find') document.getElementById('searchRoom').focus();
  });
  btn('btnRoute').addEventListener('click', () => {
    toggleHudPop('route');
    if (hudPopOpen === 'route') document.getElementById('routeFrom').focus();
  });
  const cardX = document.getElementById('room-card-close');
  if (cardX) cardX.addEventListener('click', () => hideRoomCard());
  const fly = document.getElementById('routeFlightGo');
  if (fly) fly.addEventListener('click', () => flyRoute());
  const flyX = document.getElementById('routeFlightNo');
  if (flyX) flyX.addEventListener('click', () => hideRouteFlightChip());

  syncHud();
}

let hudChip3D = null;
const hudChipFloor = [];

function syncHud() {
  renderRoomLayerButtons();
  if (hudChip3D) hudChip3D.classList.toggle('on', is3DView);
  hudChipFloor.forEach((b, i) => {
    b.classList.toggle('on', !is3DView && currentLevel === LEVELS[i]);
  });
  updateBuildingButton();
  updateHeatLegend();
}

// ── popovers off the bar ─────────────────────────────────────────────────
// Find, Route and Layers each hang a small card just above their button.
// Only one is ever out: they are all the same drawer, and two of them open at
// once is a menu, which is the thing this bar replaced. A click anywhere else
// or Escape puts the drawer back.
const HUD_POPS = {
  find: ['btnFind', 'findPop'],
  route: ['btnRoute', 'routePop'],
  layers: ['btnLayers', 'controls'],
};
let hudPopOpen = null;

function placeHudPop(pop, button) {
  const host = document.getElementById('viewer-container');
  if (!host) return;
  const hr = host.getBoundingClientRect();
  const br = button.getBoundingClientRect();
  const w = pop.offsetWidth || 260;
  const half = Math.min(w, hr.width - 20) / 2;
  let cx = br.left + br.width / 2 - hr.left;
  cx = Math.max(half + 10, Math.min(hr.width - half - 10, cx));
  pop.style.left = cx + 'px';
}

function setHudPop(name) {
  hudPopOpen = name || null;
  // a suggestion list belongs to the popover that raised it
  document.querySelectorAll('.hud-ac').forEach(e => e.remove());
  for (const key in HUD_POPS) {
    const [bid, pid] = HUD_POPS[key];
    const b = document.getElementById(bid), p = document.getElementById(pid);
    if (!b || !p) continue;
    const on = key === hudPopOpen;
    p.classList.toggle('open', on);
    b.classList.toggle('on', on);
    if (on) placeHudPop(p, b);
  }
}

function toggleHudPop(name) { setHudPop(hudPopOpen === name ? null : name); }

document.addEventListener('mousedown', (e) => {
  if (!hudPopOpen) return;
  const [bid, pid] = HUD_POPS[hudPopOpen];
  const t = e.target;
  if (t.closest && (t.closest('#' + pid) || t.closest('#' + bid) || t.closest('.hud-ac'))) return;
  setHudPop(null);
});

window.addEventListener('keydown', (e) => {
  if (e.key !== 'Escape') return;
  if (hudPopOpen) { setHudPop(null); return; }
  if (!document.getElementById('room-card')?.classList.contains('hidden')) hideRoomCard();
});

window.addEventListener('resize', () => {
  if (!hudPopOpen) return;
  const [bid, pid] = HUD_POPS[hudPopOpen];
  const b = document.getElementById(bid), p = document.getElementById(pid);
  if (b && p) placeHudPop(p, b);
});

// ── the details card ─────────────────────────────────────────────────────
// What used to be the bottom half of the old panel. It has no home of its
// own on screen until there is something to say, and then it sits in the
// corner the toolbar leaves free.
function showRoomCard(title) {
  const c = document.getElementById('room-card');
  if (!c) return;
  if (title !== undefined && title !== null) {
    const t = document.getElementById('room-card-title');
    if (t) t.textContent = title;
  }
  c.classList.remove('hidden');
}

function hideRoomCard() {
  const c = document.getElementById('room-card');
  if (c) c.classList.add('hidden');
}

// Every write to the card goes through here so it can never be filled in
// while it is out of sight.
function setInfoCard(html, title) {
  const el = document.getElementById('info-content');
  if (el) el.innerHTML = html;
  showRoomCard(title);
}

// A tiny handle for scripted checks and for driving the view from the console.
window.buildsim = Object.assign(window.buildsim || {}, {
  open: (o) => openBuilding(o || { frame: true }),
  close: () => closeBuilding(),
  peel: () => peelT,
  setPeel: (t) => { setPeel(t); render(); },
  hero: () => { const s = heroCameraShot(); camera3D.position.copy(s.pos); orbitControls.target.copy(s.look); orbitControls.update(); render(); },
  overview: () => { const s = overviewShot(); camera3D.position.copy(s.pos); orbitControls.target.copy(s.look); orbitControls.update(); render(); },
  heat: (on, m) => { setHeat(on !== false, m); syncHud(); },
  people: () => false,
  fly: () => flyRoute(),
  ready: () => shellBuilt && tiersReady,
  stats: () => ({
    calls: renderer.info.render.calls,
    tris: renderer.info.render.triangles,
    geoms: renderer.info.memory.geometries,
    textures: renderer.info.memory.textures,
    peel: peelT, mode: is3DView ? '3d' : '2d', heat: heatOn,
    session: window._viewerSessionId || null,
    live: { ...(window._buildsimDiagnostics || {}) },
  }),
});
