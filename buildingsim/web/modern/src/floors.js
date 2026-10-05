// floors.js — pulling the stack apart, and putting storeys away.
//
// Two ways to get at a floor that is buried under the ones above it, both
// live only in the open 3D building:
//
//   · spread — the whole stack fans out vertically. Level 0 stays where the
//     building meets the ground, level 1 lifts, level 2 lifts twice as far,
//     so the storeys separate without the plan drifting sideways. It is a
//     drag, not a slider you hunt for: the rail on the right edge, or a
//     vertical drag on any floor plate itself.
//   · filter — an eye chip per storey. Hidden floors leave the scene
//     entirely, so you can isolate level 1 and read it against the site.
//
// Both are pure presentation: they move the tier groups peel.js already
// hangs each floor off, and nothing that computes a coordinate anywhere else
// in the viewer knows they exist. Everything that lives *inside* a floor —
// room meshes, walls, slab, labels, equipment sprites, the heat overlay —
// is parented to that floor's container and rides along for free. The few
// things that span storeys (the route tube, coverage domes, stair and
// elevator connectors) cannot ride two floors at once, so they stand down
// while the stack is spread and come back when it closes.

const EXPLODE_GAP = 45;        // extra height between plates at full spread
const EXPLODE_DRAG = 300;      // px of drag for the full spread
const EXPLODE_TAU = 90;        // ms; how hard the stack chases the target

let explodeT = 0;              // 0 = the storeys at their built spacing
let explodeTarget = 0;
let explodeRAF = 0;
const floorShown = LEVELS.map(() => true);
const tierBase = LEVELS.map(() => 0);   // where the peel wants each tier

// How far storey i is lifted right now. peel.js asks this when it places the
// highlight beacons, which are drawn in world space rather than inside a
// floor, so they have to be lifted by hand.
function floorLift(i) { return i * EXPLODE_GAP * explodeT; }
function floorVisible(i) { return floorShown[i] !== false; }

// ── the stack ────────────────────────────────────────────────────────────
// setPeel owns the tier positions; this rides on top of whatever it decided,
// so opening and closing the building keeps working untouched.
const _fl_setPeel = setPeel;
setPeel = function (t) {
  _fl_setPeel(t);
  for (let i = 0; i < tierGroups.length; i++) {
    if (tierGroups[i]) tierBase[i] = tierGroups[i].position.z;
  }
  layoutFloors();
};

function layoutFloors() {
  if (!tiersReady) return;
  for (let i = 0; i < tierGroups.length; i++) {
    const tg = tierGroups[i];
    if (!tg) continue;
    tg.position.z = tierBase[i] + floorLift(i);
    // A filtered-out storey leaves the scene whole — plates, walls, equipment
    // and all — but only in 3D: the flat plan shows one floor by other means.
    tg.visible = !is3DView || floorVisible(i);
  }
  syncFloorRail();
}

// Anything drawn across two storeys is wrong the moment they move apart, so
// it waits out the spread — except the route, which is rebuilt with each node
// lifted by its own storey (see refreshSpreadRoute), so it stays visible and
// stretches between the separated floors. gateOverlays() is what turns the
// rest back on, so it is also where they get taken away again.
const _fl_gateOverlays = gateOverlays;
gateOverlays = function () {
  _fl_gateOverlays();
  if (is3DView && explodeT > 0.005) {
    if (typeof coverageGroup !== 'undefined') coverageGroup.visible = false;
    if (typeof occupancyGroup !== 'undefined') occupancyGroup.visible = false;
    if (typeof crossFloorGraphGroup !== 'undefined') crossFloorGraphGroup.visible = false;
    if (typeof stairConnectors !== 'undefined') stairConnectors.visible = false;
    if (typeof elevatorConnectors !== 'undefined') elevatorConnectors.visible = false;
  }
  syncFloorRail();
};

// The route tube is one geometry across storeys, so it cannot be translated —
// it is rebuilt from the last result with each node at its lifted height.
// Cheap enough to do per animation tick (a few thousand triangles).
function refreshSpreadRoute() {
  if (typeof lastRouteResult === 'undefined' || !lastRouteResult) return;
  if (typeof routeGroup === 'undefined' || !routeGroup.visible) return;
  while (routeGroup.children.length > 0) {
    const c = routeGroup.children[0];
    routeGroup.remove(c);
    if (c.geometry) c.geometry.dispose();
    if (c.material) c.material.dispose();
  }
  renderSmoothRoute(lastRouteResult, lastRouteLevelKey);
}

function applyExplode() {
  layoutFloors();
  gateOverlays();
  // Beacons are built in world space against the stack's current height.
  if (beaconGroup.children.length) buildHighlightBeacons();
  refreshSpreadRoute();
  render();
}

// The stack always eases toward the target, whether the target came from a
// drag, a chip or a script: a floor that snapped 45 units in one frame reads
// as a glitch rather than as a movement.
function setExplode(t, immediate) {
  const to = clamp01(is3DView ? t : 0);
  explodeTarget = to;
  if (immediate || reducedMotion()) {
    explodeT = to;
    if (explodeRAF) { cancelAnimationFrame(explodeRAF); explodeRAF = 0; }
    applyExplode();
    return;
  }
  if (explodeRAF) return;
  // Time-based smoothing rather than a fixed fraction per frame: the stack
  // then takes the same fifth of a second to settle whether the machine is
  // drawing at 120 fps or at 8.
  let last = performance.now();
  const tick = (now) => {
    const dt = Math.min(120, Math.max(1, now - last));
    last = now;
    const d = explodeTarget - explodeT;
    if (Math.abs(d) < 0.0015) { explodeT = explodeTarget; explodeRAF = 0; applyExplode(); return; }
    explodeT += d * (1 - Math.exp(-dt / EXPLODE_TAU));
    applyExplode();
    explodeRAF = requestAnimationFrame(tick);
  };
  explodeRAF = requestAnimationFrame(tick);
}

// A null list means "the flags have already been set, just apply them".
function setFloorsShown(list) {
  if (Array.isArray(list)) {
    let any = false;
    LEVELS.forEach((l, i) => { if (list[i]) any = true; });
    LEVELS.forEach((l, i) => { floorShown[i] = any ? !!list[i] : true; });
  }
  layoutFloors();
  if (beaconGroup.children.length) buildHighlightBeacons();
  render();
}

// Leaving 3D, closing the building or flying a route all have to land on a
// stack that is whole and closed: a half-spread building with two storeys
// switched off is a state nothing else in the viewer knows how to draw.
function resetFloors(immediate) {
  LEVELS.forEach((l, i) => { floorShown[i] = true; });
  setExplode(0, immediate);
  layoutFloors();
  if (beaconGroup.children.length) buildHighlightBeacons();
}

// ── the rail ─────────────────────────────────────────────────────────────
const railEl = document.getElementById('floorRail');
const trackEl = document.getElementById('explodeTrack');
const fillEl = document.getElementById('explodeFill');
const knobEl = document.getElementById('explodeKnob');
const eyesEl = document.getElementById('floorEyes');
const floorEyeBtns = [];
const KNOB_H = 18;

const EYE_ON = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M1.8 10S4.9 4.9 10 4.9 18.2 10 18.2 10 15.1 15.1 10 15.1 1.8 10 1.8 10z"/><circle cx="10" cy="10" r="2.4"/></svg>';
const EYE_OFF = '<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 4.4 16 15.6"/><path d="M7.4 5.6A8.6 8.6 0 0 1 10 5.2c5.1 0 8.2 5 8.2 5a15 15 0 0 1-3 3.4M5 6.9A15 15 0 0 0 1.8 10.2s3.1 5 8.2 5a8.4 8.4 0 0 0 3-.5"/></svg>';

function buildFloorRail() {
  if (!eyesEl || floorEyeBtns.length) return;
  // Top storey first, so the column reads the way the building stands.
  for (let i = LEVELS.length - 1; i >= 0; i--) {
    const b = document.createElement('button');
    b.className = 'floor-eye on';
    b.dataset.level = String(i);
    b.innerHTML = EYE_ON + '<span>L' + i + '</span>';
    b.title = 'Show or hide level ' + i + ' (at least one stays on)';
    b.addEventListener('click', () => toggleFloorShown(i));
    eyesEl.appendChild(b);
    floorEyeBtns[i] = b;
  }
  wireExplodeRail();
  syncFloorRail();
}

function toggleFloorShown(i) {
  const want = !floorShown[i];
  // At least one storey is always on: with none of them the viewer is an
  // empty site and there is nothing to click your way back from.
  if (!want && floorShown.filter(Boolean).length <= 1) return;
  floorShown[i] = want;
  setFloorsShown(null);
}

function syncFloorRail() {
  if (!railEl) return;
  const open = is3DView && typeof peelT === 'number' && peelT > 0.9
    && !(typeof routeFlightRunning !== 'undefined' && routeFlightRunning);
  railEl.classList.toggle('hidden', !open);
  if (!open) return;
  for (let i = 0; i < floorEyeBtns.length; i++) {
    const b = floorEyeBtns[i];
    if (!b) continue;
    const on = floorVisible(i);
    b.classList.toggle('on', on);
    b.classList.toggle('off', !on);
    const svg = b.querySelector('svg');
    if (svg && b._eye !== on) {
      b.innerHTML = (on ? EYE_ON : EYE_OFF) + '<span>L' + i + '</span>';
      b._eye = on;
    }
  }
  if (trackEl && knobEl && fillEl) {
    const travel = trackEl.clientHeight - KNOB_H;
    knobEl.style.bottom = (explodeT * travel) + 'px';
    fillEl.style.height = (explodeT * travel + KNOB_H / 2) + 'px';
    trackEl.setAttribute('aria-valuenow', Math.round(explodeT * 100));
    railEl.classList.toggle('spread', explodeT > 0.005);
  }
}

function railValue(ev) {
  const r = trackEl.getBoundingClientRect();
  const travel = r.height - KNOB_H;
  if (travel <= 0) return 0;
  return clamp01(1 - ((ev.clientY - r.top) - KNOB_H / 2) / travel);
}

function wireExplodeRail() {
  if (!trackEl) return;
  let dragId = null;
  trackEl.addEventListener('pointerdown', (ev) => {
    ev.preventDefault();
    ev.stopPropagation();
    dragId = ev.pointerId;
    try { trackEl.setPointerCapture(ev.pointerId); } catch (e) { /* mouse without capture */ }
    trackEl.classList.add('dragging');
    setExplode(railValue(ev));
  });
  trackEl.addEventListener('pointermove', (ev) => {
    if (dragId !== ev.pointerId) return;
    setExplode(railValue(ev));
  });
  const end = (ev) => {
    if (dragId !== ev.pointerId) return;
    dragId = null;
    trackEl.classList.remove('dragging');
  };
  trackEl.addEventListener('pointerup', end);
  trackEl.addEventListener('pointercancel', end);
  trackEl.addEventListener('keydown', (ev) => {
    const step = ev.shiftKey ? 0.25 : 0.1;
    if (ev.key === 'ArrowUp' || ev.key === 'ArrowRight') setExplode(explodeTarget + step);
    else if (ev.key === 'ArrowDown' || ev.key === 'ArrowLeft') setExplode(explodeTarget - step);
    else if (ev.key === 'Home') setExplode(1);
    else if (ev.key === 'End') setExplode(0);
    else return;
    ev.preventDefault();
  });
}

// ── dragging the plates themselves ───────────────────────────────────────
// The rail is the visible affordance; this is the one people reach for first.
// A press on a floor plate is held back from OrbitControls until the gesture
// has declared itself: pull up or down and the stack spreads, move sideways
// and the press is handed straight back to the orbiter at the point it went
// down, so a rotation loses nothing but the first five pixels. Pan (right
// button) and zoom (wheel) never come through here at all.
let plateDrag = null;
let synthesising = false;

// Anything you can actually see inside a tier counts as that floor: the slab,
// a room, a wall, a label. Three's raycaster happily reports objects that are
// switched off (the peel's flash planes cover the whole footprint), so the
// hit has to be checked back up its parents before it means anything.
function plateUnderPointer(ev) {
  const rect = canvas.getBoundingClientRect();
  mouse.x = ((ev.clientX - rect.left) / rect.width) * 2 - 1;
  mouse.y = -((ev.clientY - rect.top) / rect.height) * 2 + 1;
  raycaster.setFromCamera(mouse, camera3D);
  const tiers = tierGroups.filter((t, i) => t && t.visible && floorVisible(i));
  if (!tiers.length) return false;
  for (const h of raycaster.intersectObjects(tiers, true)) {
    let o = h.object, on = true;
    while (o) { if (o.visible === false) { on = false; break; } o = o.parent; }
    if (on) return true;
  }
  return false;
}

viewerEl.addEventListener('pointerdown', (ev) => {
  if (synthesising || plateDrag) return;
  if (ev.button !== 0 || ev.pointerType === 'touch') return;
  if (ev.target !== canvas) return;
  if (!is3DView || peelT < 0.9) return;
  if (typeof editModeEnabled !== 'undefined' && editModeEnabled) return;
  if (typeof routeFlightRunning !== 'undefined' && routeFlightRunning) return;
  if (!plateUnderPointer(ev)) return;
  plateDrag = { id: ev.pointerId, x: ev.clientX, y: ev.clientY, from: explodeTarget, mode: null };
  ev.stopPropagation();                    // the orbiter does not see it yet
}, true);

function handBackToOrbit(ev) {
  const d = plateDrag;
  plateDrag = null;
  synthesising = true;
  try {
    canvas.dispatchEvent(new PointerEvent('pointerdown', {
      pointerId: ev.pointerId, pointerType: ev.pointerType || 'mouse', isPrimary: true,
      button: 0, buttons: 1, clientX: d.x, clientY: d.y,
      bubbles: true, cancelable: true, composed: true, view: window,
    }));
  } catch (e) { /* the gesture is simply lost; the next one works */ }
  synthesising = false;
}

window.addEventListener('pointermove', (ev) => {
  if (!plateDrag || ev.pointerId !== plateDrag.id) return;
  const dx = ev.clientX - plateDrag.x, dy = ev.clientY - plateDrag.y;
  if (!plateDrag.mode) {
    if (Math.hypot(dx, dy) < 5) return;
    if (Math.abs(dy) <= Math.abs(dx) * 1.4) { handBackToOrbit(ev); return; }
    plateDrag.mode = 'spread';
    document.body.classList.add('spreading');
  }
  setExplode(plateDrag.from - dy / EXPLODE_DRAG);
});

function endPlateDrag(ev) {
  if (!plateDrag || (ev && ev.pointerId !== plateDrag.id)) return;
  plateDrag = null;
  document.body.classList.remove('spreading');
}
window.addEventListener('pointerup', endPlateDrag);
window.addEventListener('pointercancel', endPlateDrag);

// ── the states that have to be clean ─────────────────────────────────────
const _fl_switchToFloor = switchToFloor;
switchToFloor = function (level) {
  resetFloors(true);
  _fl_switchToFloor(level);
  layoutFloors();
};

const _fl_switchTo3D = switchTo3D;
switchTo3D = function () {
  _fl_switchTo3D();
  layoutFloors();
};

const _fl_closeBuilding = closeBuilding;
closeBuilding = function (opts) {
  LEVELS.forEach((l, i) => { floorShown[i] = true; });
  setExplode(0);
  _fl_closeBuilding(opts);
};

const _fl_flyRoute = flyRoute;
flyRoute = function () {
  resetFloors(true);
  _fl_flyRoute();
};

// The route rides the spread (each node lifts with its own storey), so a
// route drawn into a spread stack is perfectly visible. The old hook that
// collapsed the stack on every draw is gone: refreshSpreadRoute redraws the
// route on every spread tick, and collapsing there made the drag rubber-band
// back to zero.

const _fl_buildHud = buildHud;
buildHud = function () {
  _fl_buildHud();
  buildFloorRail();
};

const _fl_syncHud = syncHud;
syncHud = function () {
  _fl_syncHud();
  syncFloorRail();
};

// ── the console handle ───────────────────────────────────────────────────
window.buildsim.explode = (t) => {
  if (t === undefined) return explodeT;      // where the stack is right now
  setExplode(t);
  return explodeTarget;
};
window.buildsim.floors = (list) => {
  if (list === undefined) return floorShown.slice();
  setFloorsShown(list);
  return floorShown.slice();
};
// buildsim.dev.counts() (photocam.js) reports the spread and filter alongside
// the rest of the scene's state, so a scripted run asserts them in one read.
