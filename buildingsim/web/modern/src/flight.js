// flight.js — moving through the building rather than around it.
//
// Route flight: when a route arrives, the camera can run it at head height,
// easing in and out and banking into the turns, then settle back into the
// overview it came from. While it runs the orbit controller stands down and
// this file drives camera3D directly.

let cameraLocked = false;
let flightAbort = null;

function lockCamera(on) {
  cameraLocked = on;
  orbitControls.enabled = !on && is3DView;
}

// ── route flight ─────────────────────────────────────────────────────────
let routeFlightRunning = false;

function offerRouteFlight() {
  const chip = document.getElementById('routeFlightChip');
  if (chip) {
    chip.style.display = 'flex';
    clearTimeout(chip._t);
    chip._t = setTimeout(() => { chip.style.display = 'none'; }, 14000);
  }
  // A remotely selected route is a request to *show* the way: pull back and
  // frame the whole path so the red line reads at a glance. The head-height
  // fly-through stays one click away on the chip — it was disorienting as the
  // automatic response ("it showed walk mode").
  if (_routeFromRemote) {
    _routeFromRemote = false;
    setTimeout(() => { if (lastRoutePoints) frameRoute(); }, 500);
  }
}

// Pull the camera back to a three-quarter view that holds the whole route,
// with a little margin so the endpoints' markers stay inside the frame.
function frameRoute() {
  if (!lastRoutePoints || lastRoutePoints.length < 2) return;
  if (!is3DView) switchTo3D();
  const bb = new THREE.Box3().setFromPoints(lastRoutePoints);
  const c = bb.getCenter(new THREE.Vector3());
  const size = bb.getSize(new THREE.Vector3());
  const span = Math.max(size.x, size.y, size.z, 60);
  const dist = span * 1.25 + 70;
  const dir = new THREE.Vector3(0.42, -0.62, 0.66).normalize();
  animateCamera3D(c.clone().addScaledVector(dir, dist), c, 1600);
}
function hideRouteFlightChip() {
  const chip = document.getElementById('routeFlightChip');
  if (chip) chip.style.display = 'none';
}
let _routeFromRemote = false;

function flyRoute() {
  if (routeFlightRunning || !lastRoutePoints || lastRoutePoints.length < 2) return;
  if (!is3DView) switchTo3D();
  hideRouteFlightChip();
  const pts = lastRoutePoints.map(p => p.clone());
  const curve = new THREE.CatmullRomCurve3(pts, false, 'centripetal', 0.5);
  const total = curve.getLength();
  const back = { pos: camera3D.position.clone(), look: orbitControls.target.clone() };

  routeFlightRunning = true;
  lockCamera(true);
  document.body.classList.add('flying');
  setIndoorLighting(true, null);
  setRouteEyeLevel(true);

  const speed = 26;                        // world units per second
  const dur = Math.min(19000, Math.max(4200, total / speed * 1000));
  const t0 = performance.now();
  const id = ++_animId;
  flightAbort = () => { if (id === _animId) _animId++; endFlight(); };

  const up = new THREE.Vector3(0, 0, 1);
  const eye = new THREE.Vector3(), aim = new THREE.Vector3();
  const d1 = new THREE.Vector3(), d2 = new THREE.Vector3();

  function endFlight() {
    routeFlightRunning = false;
    setRouteEyeLevel(false);
    setIndoorLighting(false);
    document.body.classList.remove('flying');
    camera3D.up.set(0, 0, 1);
    lockCamera(false);
    animateCamera3D(back.pos, back.look, 1500);
  }

  function tick(now) {
    if (id !== _animId) { endFlight(); return; }
    const raw = Math.min((now - t0) / dur, 1);
    // ease in and out so the start and finish don't jerk
    const s = raw < 0.12 ? (raw / 0.12) * (raw / 0.12) * 0.12
            : raw > 0.88 ? 1 - Math.pow((1 - raw) / 0.12, 2) * 0.12
            : raw;
    const t = Math.min(0.999, s);
    curve.getPointAt(t, eye);
    curve.getPointAt(Math.min(0.9995, t + 0.02), aim);
    // Route tubes are drawn ROUTE_Z above the floor so they read over the
    // plan; the camera wants head height above that same floor instead —
    // otherwise it flies out through the ceiling.
    eye.z += EYE_H - ROUTE_Z;
    aim.z += EYE_H * 0.8 - ROUTE_Z;

    // bank: compare the heading a little ahead with the heading a little
    // behind and roll into the difference
    curve.getTangentAt(Math.max(0.0005, t - 0.012), d1);
    curve.getTangentAt(Math.min(0.9985, t + 0.012), d2);
    const turn = Math.atan2(d1.x * d2.y - d1.y * d2.x, d1.x * d2.x + d1.y * d2.y);
    const bank = Math.max(-0.42, Math.min(0.42, turn * 5.5));
    const fwd = d2.clone().normalize();
    up.set(0, 0, 1).applyAxisAngle(fwd, -bank);

    camera3D.position.copy(eye);
    camera3D.up.copy(up);
    camera3D.lookAt(aim);
    orbitControls.target.copy(aim);
    moveHeadLamp();
    render();
    if (raw < 1) requestAnimationFrame(tick);
    else endFlight();
  }
  requestAnimationFrame(tick);
}

// Escape gets you out of the flight, the same key that leaves everything
// else in the viewer.
window.addEventListener('keydown', e => {
  const tag = (e.target && e.target.tagName) || '';
  if (tag === 'INPUT' || tag === 'TEXTAREA') return;
  if (e.key === 'Escape' && routeFlightRunning && flightAbort) flightAbort();
});
