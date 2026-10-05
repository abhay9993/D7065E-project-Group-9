// photocam.js — standing where the photographer stood.
//
// The exterior was built against a survey of A-huset: twenty-seven photographs
// with their standpoints marked on the level plan. This puts the render camera
// on those standpoints, at eye height above the local ground, looking the way
// the survey arrow points, through a lens the width of a phone camera — so a
// render and its photograph can be laid side by side and judged.
//
// It is a development aid, reachable as window.buildsim.photoCam(n) or ?photocam=N,
// and it changes nothing about how the viewer behaves otherwise.

// x, y are the standpoints in plan coordinates; heading is degrees measured
// counter-clockwise from +x (plan east). Standpoints came from the numbered
// plan; headings from the arrows where the plan gives one, and otherwise from
// which way the elevation runs in the photograph.
const PHOTO_POSITIONS = {
  1:  { x: 138, y: -172, hdg: 62, back: 16, note: 'SE corner, entrance under the zinc box' },
  2:  { x: -155, y: 190, hdg: -20, note: 'north elevation, looking east' },
  3:  { x: -223, y: 87,  hdg: 0,   note: 'west plaza' },
  4:  { x: 199, y: 128,  hdg: -102, back: 22, note: 'east alley, looking south' },
  5:  { x: 152, y: -13,  hdg: 25,  note: 'east side, Alfa entrance' },
  6:  { x: 36,  y: -180, hdg: 90,  note: 'south elevation' },
  7:  { x: 54,  y: -191, hdg: 62,  note: 'south entrance, zinc box' },
  8:  { x: -230, y: 19,  hdg: 62,  note: 'west lawn — the suterräng section' },
  9:  { x: 168, y: -62,  hdg: 128, back: 16, note: 'east lawn by the university sign' },
  10: { x: 66,  y: -34,  hdg: 64, back: 24, note: 'the plaza front, main entrance' },
  11: { x: -172, y: -191, hdg: 48, note: 'south-west, the preschool wing' },
  13: { x: 202, y: 152,  hdg: -128, back: 8, note: 'east elevation, looking south' },
  14: { x: 19,  y: -191, hdg: 90,  note: 'Universitetsvägen 1' },
  15: { x: 92,  y: 200,  hdg: -24, back: 30, note: 'north elevation by the glass gallery' },
  16: { x: -51, y: -116, hdg: 1,   back: 12, note: 'inner yard, looking east' },
  17: { x: -200, y: 194, hdg: -14, back: 30, note: 'north-west corner, looking east' },
  18: { x: -76, y: -89,  hdg: 22,  note: 'inner yard, looking north-east' },
  19: { x: -225, y: -32, hdg: 12,  note: 'west plaza and the embankment' },
  21: { x: 102, y: -191, hdg: 100, note: 'south-east elevation' },
  22: { x: 196, y: 196,  hdg: -138, back: 30, note: 'north-east glass corner' },
  23: { x: -96, y: -87,  hdg: -133, note: 'inner yard, looking south-west' },
  24: { x: -211, y: -184, hdg: 39, note: 'south-west corner' },
  26: { x: -227, y: 170, hdg: -2,  back: 14, note: 'north-west glass corner, head on' },
  27: { x: -76, y: -200, hdg: 93,  note: 'south yard between the wings' },
  28: { x: -8,  y: -185, hdg: 178, back: 12, note: 'south elevation, looking west' },
  // Synthetic vantages used while judging the massing as a whole.
  90: { x: 200, y: -230, hdg: 118, back: 210, up: 90, tilt: -30, note: 'aerial from the south-east' },
  91: { x: -230, y: 240, hdg: -55, back: 210, up: 90, tilt: -30, note: 'aerial from the north-west' },
  92: { x: 240, y: 190,  hdg: -155, back: 150, up: 30, tilt: 0, note: 'north-east corner, three-quarter' },
  93: { x: -240, y: 60,  hdg: 10,  back: 90,  up: 12, tilt: 6, note: 'the west slope' },
};

// A phone camera is about 26 mm equivalent; on the 4:3 frame these photographs
// use that is a vertical field of view of roughly 46 degrees.
const PHOTO_FOV = 46;

function photoCam(n, opts) {
  const p = PHOTO_POSITIONS[n];
  if (!p) { console.warn('[photocam] no standpoint ' + n); return null; }
  const o = opts || {};
  if (!is3DView) switchTo3D();
  if (typeof setPeel === 'function') setPeel(0);
  if (typeof viewMode !== 'undefined') viewMode = 'exterior';
  const a = (p.hdg * Math.PI) / 180;
  // The plan marks the standpoints against the wall; the photographs are taken
  // from where you actually stand, a bus width further back down the same axis.
  const back = o.back === undefined ? (p.back === undefined ? 20 : p.back) : o.back;
  const ex = p.x - Math.cos(a) * back, ey = p.y - Math.sin(a) * back;
  const g = groundZ(ex, ey);
  const eye = new THREE.Vector3(ex, ey, g + (p.up || 0) + EYE_H);
  // aim a little above the horizon, the way you tilt a phone up at a building
  const look = new THREE.Vector3(
    ex + Math.cos(a) * 70, ey + Math.sin(a) * 70,
    g + (p.up || 0) + EYE_H + (p.tilt === undefined ? 15 : p.tilt));
  // The orbit controller keeps the camera from dropping below its target, so
  // asking it to look *up* at a building would silently lift the camera to the
  // target's height — the difference between standing on the pavement and
  // hovering at first-floor level. Take the controller out of the loop instead.
  if (typeof lockCamera === 'function') lockCamera(true);
  camera3D.fov = o.fov || PHOTO_FOV;
  camera3D.near = 0.4;
  camera3D.updateProjectionMatrix();
  camera3D.position.copy(eye);
  camera3D.up.set(0, 0, 1);
  camera3D.lookAt(look);
  orbitControls.target.copy(
    new THREE.Vector3(ex + Math.cos(a) * 70, ey + Math.sin(a) * 70, eye.z));
  requestShadowUpdate();
  render();
  return p;
}

// A free vantage point, for judging a render against a marked-up screenshot
// whose camera was not one of the survey standpoints.
function freeCam(cfg) {
  const o = cfg || {};
  if (!is3DView) switchTo3D();
  if (typeof setPeel === 'function') setPeel(o.peel || 0);
  if (typeof viewMode !== 'undefined') viewMode = 'exterior';
  if (typeof lockCamera === 'function') lockCamera(true);
  camera3D.fov = o.fov || 50;
  camera3D.near = 0.4;
  camera3D.updateProjectionMatrix();
  camera3D.position.set(o.pos[0], o.pos[1], o.pos[2]);
  camera3D.up.set(0, 0, 1);
  camera3D.lookAt(o.look[0], o.look[1], o.look[2]);
  orbitControls.target.set(o.look[0], o.look[1], o.look[2]);
  requestShadowUpdate();
  render();
  return o;
}

window.buildsim = Object.assign(window.buildsim || {}, {
  photoCam,
  freecam: freeCam,
  // step the transformation by hand, for judging it frame by frame
  setPeel: (t) => { setPeel(t); render(); return t; },
  positions: PHOTO_POSITIONS,
  groundAt: (x, y) => groundZ(x, y),
  ready: () => shellBuilt,
  // handles the render harness uses to check what the generator actually built
  rings: () => levelRings.map(r => r.map(x => {
    const b = ringBBox(x.pts);
    return { n: x.pts.length, area: Math.round(x.area),
             bb: [b.minX, b.minY, b.maxX, b.maxY].map(v => Math.round(v)) };
  })),
  solid: (i, x, y) => !!(levelMasks[i] && levelMasks[i].solid(x, y)),
  // the outer wall run, so a standpoint on a marked-up screenshot can be turned
  // into the wall segment it actually points at
  outerRing: () => baseRings[0].pts.map(p => [+p[0].toFixed(1), +p[1].toFixed(1)]),
  // the same, for any storey's ring — a wall judged on an aerial belongs to the
  // storey it is seen on, not always to the ground floor
  ringPts: (i, j) => ((levelRings[i] || [])[j || 0] || { pts: [] })
    .pts.map(p => [+p[0].toFixed(1), +p[1].toFixed(1)]),
  skin: (i, x, y, nx, ny) => skinFor(i, x, y, nx, ny),
  shell: () => shellGroup,
  // what is under this point of the frame? used when judging a render
  pick: (sx, sy) => {
    const rc = new THREE.Raycaster();
    rc.setFromCamera(new THREE.Vector2(sx, sy), camera3D);
    // The ghost frame is a LineSegments that stays in the scene with its
    // opacity wound down to nothing while the building is closed. It is
    // `visible`, so a raycast hits it and reports a wall where the frame
    // shows sky — which is exactly the wrong answer when the question being
    // asked is "is there a hole in the elevation here?".
    const seen = (h) => h.object.visible && !h.object.isLine &&
      !(h.object.material && h.object.material.transparent &&
        h.object.material.opacity <= 0.02);
    return rc.intersectObjects(scene.children, true)
      .filter(seen)
      .slice(0, 3)
      .map(h => ({ name: h.object.name || h.object.type,
                   parent: h.object.parent && h.object.parent.name,
                   x: +h.point.x.toFixed(1), y: +h.point.y.toFixed(1),
                   z: +h.point.z.toFixed(1), d: +h.distance.toFixed(0) }));
  },
  // what stands over this point of the site? a ray straight down from the sky,
  // so a plan-position judged on a drawing can be turned into what was built
  // there without having to invert a camera.
  probe: (x, y) => {
    const rc = new THREE.Raycaster();
    rc.set(new THREE.Vector3(x, y, 600), new THREE.Vector3(0, 0, -1));
    rc.camera = camera3D;                       // sprites raycast against one
    return rc.intersectObjects(scene.children, true)
      .filter(h => h.object.visible && !h.object.isLine && !h.object.isSprite)
      .slice(0, 10)
      .map(h => ({ name: h.object.name || h.object.type,
                   parent: h.object.parent && h.object.parent.name,
                   z: +h.point.z.toFixed(1) }));
  },
  cam: () => [+camera3D.position.x.toFixed(1), +camera3D.position.y.toFixed(1),
              +camera3D.position.z.toFixed(1)],
  stats: () => renderer.info.render,
});

// The module scope is closed, so a headless regression run cannot reach the
// viewer's own state. Publish exactly what such a run needs to drive and to
// assert, and nothing that changes behaviour.
window.buildsim.dev = {
  get peelT() { return peelT; },
  get peelAnim() { return peelAnim; },
  get peelTarget() { return peelTarget; },
  get viewMode() { return viewMode; },
  get is3DView() { return is3DView; },
  get heatOn() { return heatOn; },
  get heatMetric() { return heatMetric; },
  get peopleOn() { return false; },       // occupant animation was removed
  get levels() { return LEVELS; },
  floorData: () => floorData,
  groups: () => floorGroups,
  routePoints: () => lastRoutePoints,
  counts: () => ({
    beacons: beaconGroup.children.length,
    route: routeGroup.children.length,
    people: 0,
    world: worldGroup.visible, shell: shellGroup.visible,
    spread: Math.round(explodeT * 1000) / 1000,
    floors: floorShown.slice(),
    plates: LEVELS.map(l => !!(floorGroups[l] && floorGroups[l].container.visible)),
    effects: LEVELS.reduce((n, l) => n + (effectGroups[l]?.children.length || 0), 0),
    entities: entityObjects.size,
    doors: LEVELS.reduce((n, l) => n + (semanticDoorGroups[l]?.children.length || 0), 0),
    alerts: decisionAlerts.length,
    lit: LEVELS.reduce((n, l) => n + (floorGroups[l]
      ? floorGroups[l].meshes.filter(m => m.userData.highlighted).length : 0), 0),
  }),
  open: () => new Promise(r => openBuilding({ frame: true, then: r })),
  close: () => new Promise(r => closeBuilding({ then: r })),
  toFloor: (l) => switchToFloor(l),
  frameRoom2D: (roomName, requestedLevel, size) => {
    const level = LEVELS.find(l => l === requestedLevel || l.endsWith('/' + requestedLevel));
    const data = level && floorData[level];
    const room = data && data.rooms.find(r => r.name === roomName);
    if (!room) return false;
    _lastSearchedRoom = { name: roomName, level };
    switchToFloor(level);
    // switchToFloor starts the normal user-facing camera animation. A
    // deterministic test or documentation capture needs its final frame now.
    const [x, y] = pdfToWorld(room.center[0], room.center[1], data.page.width, data.page.height);
    setCamera2DView(x, y, Math.max(20, Math.min(500, Number(size) || 100)));
    return true;
  },
  camera2D: () => ({
    level: currentLevel,
    frustum: frustumSize,
    pan: [panOffset.x, panOffset.y],
    bounds: [camera2D.left, camera2D.right, camera2D.bottom, camera2D.top],
  }),
  to3D: () => switchTo3D(),
  heat: (on, metric) => setHeat(on, metric),
  people: () => false,
  highlights: (h) => applyHighlights(h),
  route: (r) => applyRoute(r),
  camPos: () => camera3D.position.clone(),
  render: () => renderer.info.render,
};

// ?photocam=N puts the viewer straight on that standpoint at load, with the
// controls and the HUD out of the way so the frame is only the building.
(function () {
  const n = parseInt(urlParams.get('photocam') || '', 10);
  if (!n) return;
  const fov = parseFloat(urlParams.get('fov') || '') || undefined;
  const bare = () => {
    const v = document.getElementById('viewer-container');
    if (v) v.style.display = '';
    for (const id of ['hud-bar', 'room-card', 'controls',
                      'findPop', 'routePop', 'floorRail',
                      'buildsim-title', 'liveChip', 'heatLegend']) {
      const e = document.getElementById(id);
      if (e) e.style.display = 'none';
    }
    window.dispatchEvent(new Event('resize'));
  };
  const boot = () => {
    bare();
    if (!shellBuilt) { setTimeout(boot, 150); return; }
    photoCam(n, { fov });
    // one more pass after the textures and the campus have settled
    setTimeout(() => { bare(); photoCam(n, { fov }); window.__photocamReady = true; }, 700);
  };
  setTimeout(boot, 80);
})();
