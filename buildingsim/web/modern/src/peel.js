// peel.js — opening the building.
//
// The viewer has two poses of the same model. Closed, the floor plates are
// stacked at their true storey height inside the shell and you see A-huset
// from the plaza. Open, the shell has dissolved to a wireframe and the plates
// have separated into the exploded floor stack the rest of the viewer has
// always worked with.
//
// The whole transition is one number, peelT ∈ [0,1]. At peelT = 1 every
// coordinate is exactly what it was before this file existed, so routes,
// highlights, equipment, occupancy and coverage keep their geometry untouched;
// they are simply held back until the building is open.

let peelT = 1;                 // 1 = open (the historical layout)
let peelTarget = 1;
let peelAnim = null;
let _peelAnimId = 0;
const tierGroups = [];
let tiersReady = false;

function clamp01(v) { return v < 0 ? 0 : v > 1 ? 1 : v; }

// Each floor plate hangs off a tier so it can be slid vertically without
// touching any of the coordinates the rest of the viewer computes.
function setupTiers() {
  if (tiersReady) return;
  LEVELS.forEach((l, i) => {
    const tg = new THREE.Group();
    tg.name = 'tier' + i;
    scene.add(tg);
    tierGroups[i] = tg;
    const fg = floorGroups[l];
    if (fg && fg.container) tg.add(fg.container);
  });
  tiersReady = true;
}

// The floor plates are the last act: once the shell is off them they fan out
// downwards, the lowest first, each with a short glow as it reaches its place.
// tierProgress(i, t) is 0 at t = 0 and 1 at t = 1 for every tier, so the two
// end states are bit-for-bit the ones the rest of the viewer expects.
function tierProgress(i, t) {
  const a = 0.44 + i * 0.09;
  const b = Math.min(0.995, a + 0.40);
  const u = clamp01((t - a) / (b - a));
  return u * u * u * (u * (u * 6 - 15) + 10);
}

const tierPulses = [];
function tierPulse(i) {
  if (tierPulses[i]) return tierPulses[i];
  if (!baseRings || !baseRings.length) return null;
  const bb = ringBBox(baseRings[0].pts);
  const m = noTone(new THREE.MeshBasicMaterial({
    color: 0x9fd8ff, transparent: true, opacity: 0, depthWrite: false,
    blending: THREE.AdditiveBlending, side: THREE.DoubleSide }));
  const p = new THREE.Mesh(
    new THREE.PlaneGeometry((bb.maxX - bb.minX) * 1.04, (bb.maxY - bb.minY) * 1.04), m);
  p.position.set(bb.cx, bb.cy, i * FLOOR_SPACING - 0.6);
  p.renderOrder = 900;
  p.visible = false;
  tierGroups[i].add(p);
  tierPulses[i] = p;
  return p;
}

function setPeel(t) {
  peelT = clamp01(t);
  const squeeze = FLOOR_SPACING - EXT_STOREY;
  for (let i = 0; i < tierGroups.length; i++) {
    if (!tierGroups[i]) continue;
    const p = tierProgress(i, peelT);
    tierGroups[i].position.z = -(1 - p) * i * squeeze;
    const pulse = tierPulse(i);
    if (pulse) {
      const g = clamp01((p - 0.70) / 0.30);
      const v = 4 * g * (1 - g) * 0.30;
      pulse.material.opacity = v;
      pulse.visible = v > 0.005 && is3DView;
    }
  }
  setShellPeel(peelT);

  // Hide the plates entirely while the building is shut: they are invisible
  // behind the facade anyway, and dropping ~1000 draw calls keeps the exterior
  // shot smooth on integrated graphics.
  const platesOn = peelT > 0.035 || !is3DView;
  LEVELS.forEach(l => {
    const fg = floorGroups[l];
    if (!fg || !fg.container) return;
    fg.container.visible = fg.container.userData._wantVisible !== false && platesOn;
  });
  gateOverlays();
}

// Overlays are only meaningful once the plates are in their final positions,
// so they sit out the transition and come back at the end.
function gateOverlays() {
  const open = peelT > 0.965;
  const chk = id => { const e = document.getElementById(id); return e ? e.checked : false; };
  if (typeof routeGroup !== 'undefined') routeGroup.visible = open;
  if (typeof occupancyGroup !== 'undefined') occupancyGroup.visible = open;
  if (typeof coverageGroup !== 'undefined') coverageGroup.visible = open;
  if (typeof crossFloorGraphGroup !== 'undefined') crossFloorGraphGroup.visible = open && chk('showGraph');
  if (typeof stairConnectors !== 'undefined') stairConnectors.visible = open && is3DView && chk('showStairs');
  if (typeof elevatorConnectors !== 'undefined') elevatorConnectors.visible = open && is3DView && chk('showElevators');
  if (typeof beaconGroup !== 'undefined') beaconGroup.visible = open && is3DView;
  updateBuildingButton();
}

// switchTo3D / switchToFloor set container visibility for their own reasons;
// remember what they wanted so the peel gate can restore it.
function noteWantVisible(level, v) {
  const fg = floorGroups[level];
  if (fg && fg.container) fg.container.userData._wantVisible = v;
}

const reducedMotion = () => {
  try { return window.matchMedia('(prefers-reduced-motion: reduce)').matches; }
  catch (e) { return false; }
};

function animatePeel(target, duration, onDone) {
  const from = peelT;
  const to = clamp01(target);
  if (Math.abs(to - from) < 0.001) { setPeel(to); if (onDone) onDone(); return; }
  if (reducedMotion()) {                       // an instant cut, no gesture
    peelAnim = ++_peelAnimId;
    peelTarget = to;
    setPeel(to);
    peelAnim = null;
    render();
    if (onDone) onDone();
    return;
  }
  const t0 = performance.now();
  // Deliberately NOT _animId: that counter belongs to the camera tweens, and
  // bumping it here cancelled the very camera move the peel is choreographed
  // with, leaving the building to open in front of a motionless camera.
  const id = ++_peelAnimId;
  peelAnim = id;
  peelTarget = to;
  function tick(now) {
    if (peelAnim !== id) return;              // superseded
    const raw = Math.min((now - t0) / duration, 1);
    // easeInOutQuint — slow to commit, slow to settle, quick in between
    const e = raw < 0.5 ? 16 * raw * raw * raw * raw * raw
                        : 1 - Math.pow(-2 * raw + 2, 5) / 2;
    setPeel(from + (to - from) * e);
    render();
    if (raw < 1) requestAnimationFrame(tick);
    else { peelAnim = null; if (onDone) onDone(); }
  }
  requestAnimationFrame(tick);
}

// ── camera poses ─────────────────────────────────────────────────────────
function buildingCenter() {
  if (baseRings && baseRings.length) {
    const bb = ringBBox(baseRings[0].pts);
    return new THREE.Vector3(bb.cx, bb.cy, 0);
  }
  return new THREE.Vector3(0, 0, 0);
}

function overviewShot() {
  const c = buildingCenter();
  const zTop = (LEVELS.length - 1) * FLOOR_SPACING;
  const span = baseRings && baseRings.length
    ? (() => { const b = ringBBox(baseRings[0].pts); return Math.max(b.maxX - b.minX, b.maxY - b.minY); })()
    : 400;
  const dist = span * 1.02;
  // A shallow angle so the plates read as a stack rather than as one plan.
  const dir = new THREE.Vector3(0.50, -0.80, 0.42).normalize();
  const look = new THREE.Vector3(c.x, c.y, zTop * 0.5);
  return { pos: look.clone().addScaledVector(dir, dist), look };
}

function roomShot(level, roomName, tightness) {
  const data = floorData[level];
  if (!data) return null;
  const room = data.rooms.find(r => r.name === roomName);
  if (!room) return null;
  const [wx, wy] = pdfToWorld(room.center[0], room.center[1], data.page.width, data.page.height);
  const zi = LEVELS.indexOf(level) * FLOOR_SPACING;
  const d = tightness || 110;
  return {
    pos: new THREE.Vector3(wx + d * 0.62, wy - d * 0.72, zi + d * 0.62),
    look: new THREE.Vector3(wx, wy, zi + 2),
  };
}

// ── the two gestures ─────────────────────────────────────────────────────
let viewMode = 'exterior';     // 'exterior' | 'interior'

function openBuilding(opts) {
  const o = opts || {};
  if (!is3DView) {                 // the flat floor view is always "open"
    if (o.then) o.then();
    return;
  }
  // "already open" has to mean open and staying open: a close that is still
  // playing has to be answered with a real animation back, or the running
  // gesture carries on and shuts the building behind us.
  const wasClosed = peelT < 0.9 || (peelAnim !== null && peelTarget < 0.5);
  viewMode = 'interior';
  if (!wasClosed) {
    setPeel(1);
    if (o.then) o.then();
    else if (o.frame) { const s = overviewShot(); animateCamera3D(s.pos, s.look, 900); }
    return;
  }
  const dur = o.duration || 2300;
  const shot = o.shot || overviewShot();
  if (!o.still) swingCamera(shot, dur);
  animatePeel(1, dur, () => {
    gateOverlays();
    if (o.then) o.then();
  });
  updateBuildingButton();
}

// The camera takes a short arc round the building while it transforms, rather
// than sliding to the new pose in a straight line: the swing is what makes the
// opening read as the building turning itself inside out. It is one extra leg,
// and it is abandoned the moment another peel is asked for.
function swingCamera(shot, dur) {
  if (reducedMotion()) {
    animateCamera3D(shot.pos, shot.look, Math.max(200, dur * 0.4));
    return;
  }
  const c = buildingCenter();
  const from = camera3D.position.clone();
  const mid = from.clone().sub(c);
  const a = 0.42;                                  // ~24° of orbit
  const mx = mid.x * Math.cos(a) - mid.y * Math.sin(a);
  const my = mid.x * Math.sin(a) + mid.y * Math.cos(a);
  mid.set(mx, my, mid.z);
  const midPos = c.clone().add(mid.multiplyScalar(1.06));
  midPos.z = from.z + (shot.pos.z - from.z) * 0.45 + 26;
  const midLook = orbitControls.target.clone().lerp(shot.look, 0.45);
  const id = _peelAnimId + 1;                      // the peel about to be armed
  animateCamera3D(midPos, midLook, dur * 0.5);
  setTimeout(() => {
    if (_peelAnimId !== id) return;                // a newer gesture took over
    animateCamera3D(shot.pos, shot.look, dur * 0.52);
  }, dur * 0.46);
}

function closeBuilding(opts) {
  const o = opts || {};
  if (!is3DView) switchTo3D();
  viewMode = 'exterior';
  const shot = heroCameraShot();
  const dur = o.duration || 2000;
  if (!o.still) swingCamera(shot, dur);
  animatePeel(0, dur * 0.92, () => { gateOverlays(); if (o.then) o.then(); });
  updateBuildingButton();
}

function toggleBuilding() {
  // Manual toggle: the building transforms, the viewpoint stays put.
  if (peelT > 0.5) closeBuilding({ still: true }); else openBuilding({ still: true });
}

function updateBuildingButton() {
  const b = document.getElementById('btnBuilding');
  if (!b) return;
  const closed = peelT < 0.5;
  b.classList.toggle('on', closed);
  b.title = closed ? 'Open the building and show the floor plates'
                   : 'Close the building and view it from the plaza';
}

// ── hook the existing behaviours ─────────────────────────────────────────
// Everything below keeps the original function's semantics and only decides
// whether the building has to open first.

const _peel_applyViewport = applyViewport;
applyViewport = function (vp) {
  if (!vp) return;
  if (vp.mode === '2d' || (!is3DView && vp.mode !== '3d')) { _peel_applyViewport(vp); return; }
  if (peelT > 0.9) { _peel_applyViewport(vp); return; }
  // Opening straight onto the requested room reads as one continuous move.
  let shot = null;
  if (vp.room) {
    for (const l of LEVELS) {
      const s = roomShot(l, vp.room, 150 / (vp.zoom || 1));
      if (s) { shot = s; break; }
    }
  }
  openBuilding({ shot: shot || overviewShot(), then: () => _peel_applyViewport(vp) });
};

const _peel_applyHighlights = applyHighlights;
applyHighlights = function (h) {
  const draw = () => {
    _peel_applyHighlights(h);
    buildHighlightBeacons();
    if (h && h.length && is3DView) frameHighlights();
  };
  if (h && h.length && is3DView && peelT < 0.9) { openBuilding({ then: draw }); return; }
  draw();
};

// In the stacked view a highlighted room on a lower floor sits under the plate
// above it, so colouring the floor alone can leave a requested room invisible.
// Each highlight also gets a marker standing clear of the whole stack with a
// thread down to the room it belongs to.
const beaconGroup = new THREE.Group();
beaconGroup.name = 'highlight-beacons';
scene.add(beaconGroup);
let beaconPinTex = null;

function beaconTexture() {
  if (beaconPinTex) return beaconPinTex;
  const c = texCanvas(64, 64);
  const x = c.getContext('2d');
  const g = x.createRadialGradient(32, 32, 1, 32, 32, 31);
  g.addColorStop(0, 'rgba(255,255,255,1)');
  g.addColorStop(0.35, 'rgba(255,255,255,0.75)');
  g.addColorStop(1, 'rgba(255,255,255,0)');
  x.fillStyle = g; x.fillRect(0, 0, 64, 64);
  beaconPinTex = new THREE.CanvasTexture(c);
  return beaconPinTex;
}

function clearBeacons() {
  while (beaconGroup.children.length) {
    const c = beaconGroup.children[0];
    beaconGroup.remove(c);
    if (c.geometry) c.geometry.dispose();
    if (c.material) c.material.dispose();
  }
}

function highlightedRooms() {
  const out = [];
  LEVELS.forEach((l, li) => {
    const fg = floorGroups[l], data = floorData[l];
    if (!fg || !data) return;
    // A storey switched off in the floor filter has no beacon either.
    if (typeof floorVisible === 'function' && is3DView && !floorVisible(li)) return;
    for (const m of fg.meshes) {
      if (!m.userData.highlighted) continue;
      const r = data.rooms.find(rr => rr.id === m.userData.id) || m.userData;
      if (!r || !r.center) continue;
      const [wx, wy] = pdfToWorld(r.center[0], r.center[1], data.page.width, data.page.height);
      // floorLift is the floor filter's spread: a beacon is drawn in world
      // space rather than inside the floor, so it has to be lifted by hand.
      const lift = typeof floorLift === 'function' && is3DView ? floorLift(li) : 0;
      out.push({ x: wx, y: wy, z: li * FLOOR_SPACING + lift, level: l, li,
                 color: m.material.color.clone(), name: r.name });
    }
  });
  return out;
}

function buildHighlightBeacons() {
  clearBeacons();
  if (!is3DView) { render(); return; }
  const hs = highlightedRooms();
  if (!hs.length) { render(); return; }
  const topLift = typeof floorLift === 'function' && is3DView ? floorLift(LEVELS.length - 1) : 0;
  const topZ = (LEVELS.length - 1) * FLOOR_SPACING + topLift + 26;
  for (const h of hs) {
    const mat = noTone(new THREE.LineBasicMaterial({
      color: h.color, transparent: true, opacity: 0.55, depthTest: false }));
    const geo = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(h.x, h.y, h.z + 2), new THREE.Vector3(h.x, h.y, topZ)]);
    const line = new THREE.Line(geo, mat);
    line.renderOrder = 1200;
    beaconGroup.add(line);
    const pin = new THREE.Sprite(noTone(new THREE.SpriteMaterial({
      map: beaconTexture(), color: h.color, transparent: true,
      depthTest: false, depthWrite: false, opacity: 0.95 })));
    pin.position.set(h.x, h.y, topZ);
    pin.scale.set(8, 8, 1);
    pin.renderOrder = 1201;
    beaconGroup.add(pin);
  }
  render();
}

function frameHighlights() {
  const hs = highlightedRooms();
  if (!hs.length) return;
  // Pad the cloud so a row of rooms doesn't collapse to a sliver and pull the
  // camera in on top of them.
  const pts = hs.map(h => new THREE.Vector3(h.x, h.y, h.z));
  let cx = 0, cy = 0, cz = 0;
  for (const p of pts) { cx += p.x; cy += p.y; cz += p.z; }
  cx /= pts.length; cy /= pts.length; cz /= pts.length;
  const pad = 44;
  pts.push(new THREE.Vector3(cx + pad, cy + pad, cz + 14));
  pts.push(new THREE.Vector3(cx - pad, cy - pad, cz - 10));
  fitCameraToRoute(pts);
}

const _peel_applyRoute = applyRoute;
applyRoute = function (route) {
  const draw = () => {
    _peel_applyRoute(route);
    if (route && route.path && route.path.length > 2) offerRouteFlight();
  };
  if (route && route.path && route.path.length > 1 && is3DView && peelT < 0.9) {
    openBuilding({ then: draw });
  } else draw();
};

const _peel_switchTo3D = switchTo3D;
switchTo3D = function () {
  _peel_switchTo3D();
  LEVELS.forEach(l => noteWantVisible(l, true));
  setWorldVisible(true);
  if (typeof shellGroup !== 'undefined') shellGroup.visible = true;
  if (tiersReady) setPeel(peelT);
  syncHudSafe();
};

const _peel_switchToFloor = switchToFloor;
switchToFloor = function (level) {
  // A single flat floor is the fully-open state by definition.
  if (tiersReady) setPeel(1);
  viewMode = 'interior';
  _peel_switchToFloor(level);
  LEVELS.forEach(l => noteWantVisible(l, l === level));
  // grass, trees and sky have no place behind a drawing
  setWorldVisible(false);
  if (tiersReady) setPeel(1);
  // after setPeel: it decides shell visibility from the peel value, and a flat
  // floor plan wants no ghost wireframe of the exterior over it
  if (typeof shellGroup !== 'undefined') shellGroup.visible = false;
  syncHudSafe();
};

function syncHudSafe() { if (typeof syncHud === 'function') syncHud(); }

const _peel_searchAndZoom = searchAndZoom;
searchAndZoom = function (roomName, level) {
  if (is3DView && peelT < 0.9) {
    const shot = roomShot(level, roomName, 120);
    openBuilding({ shot: shot || overviewShot(), then: () => _peel_searchAndZoom(roomName, level) });
    return;
  }
  _peel_searchAndZoom(roomName, level);
};

// ── boot ─────────────────────────────────────────────────────────────────
const _peel_init = init;
init = async function () {
  // The first thing on screen must be the building, not the naked floor
  // plates it is compiled from: keep the canvas dark until the shell is
  // closed, then let the finished frame fade in. The loading screen owns the
  // dark, and hands it over on the same fade — one moment, not two.
  renderer.domElement.style.opacity = '0';
  try {
    await _peel_init();
    try {
      buildShell();
    } catch (e) { console.warn('[shell] build failed:', e); }
    bootProgress(1, 'Raising the facade');
    setupTiers();
    LEVELS.forEach(l => noteWantVisible(l, true));
    buildInteriorDetail();
    startLiveLayers();
    buildHud();
    bootProgress(1, 'Fitting out the interior');
    // Open on the plaza, the way you meet the building in person.
    const shot = heroCameraShot();
    camera3D.position.copy(shot.pos);
    orbitControls.target.copy(shot.look);
    orbitControls.minDistance = 6;
    orbitControls.maxDistance = 1600;
    orbitControls.update();
    setPeel(0);
    viewMode = 'exterior';
    updateBuildingButton();
    requestShadowUpdate();
    render();
    bootProgress(1, 'A-huset');
  } finally {
    // Whatever happened above, the screen is handed over: a loader that
    // sticks is worse than a half-built building you can still orbit.
    renderer.domElement.style.transition = 'opacity 420ms ease';
    renderer.domElement.style.opacity = '1';
    bootFinish();
  }
};
