// world.js — the world the building stands in.
//
// Sets up the renderer for a physically-plausible Nordic daylight look (ACES
// tone mapping, soft shadows), builds the sky, the campus ground with its
// plaza paving, birches and lamp posts, and provides the geometry helpers the
// exterior shell is built from: a footprint extractor that turns the room
// polygons of a floor into clean closed outlines (outer ring + courtyards).
//
// Everything here is procedural — no external assets, no network fetches.

// ── scale ────────────────────────────────────────────────────────────────
// Floor plans are in PDF points. A room of median area 74 units² reads as a
// ~18 m² office, which puts one world unit at roughly 0.45 m.
const U_PER_M   = 2.2;          // world units per metre
const EXT_STOREY = 9.6;         // exterior storey height (~4.4 m, floor to floor)
const PARAPET_H = 1.15;         // the metal coping along a roof edge
const EYE_H     = 1.6 * U_PER_M;

// ── the suterräng grade ──────────────────────────────────────────────────
// A-huset stands on a slope. At the north end the ground is a whole storey
// above the level-0 floor, so level 0 is a buried basement and the building
// reads as two floors (photos 2, 15, 17, 22, 26). Walk south or west and the
// ground falls away, the basement surfaces as a brick base course with its own
// windows and doors, and the same building reads as three floors over a plinth
// (photos 8, 18, 19, 23). Everything that needs to know where the ground is —
// the facade generator, the terrain mesh, the trees, the lamp posts — asks
// groundZ(), so the grade is stated exactly once.
const GRADE_Y0 = 116;           // south of this the ground is at the level-0 floor
const GRADE_Y1 = 178;           // north of this it is a full storey higher
function smooth01(t) { t = t < 0 ? 0 : t > 1 ? 1 : t; return t * t * (3 - 2 * t); }

// Filled in once the footprint is known; the defaults keep groundZ sane before
// that (the shell is built before the first render, so it never shows).
let siteBB = { minX: -195, maxX: 185, minY: -175, maxY: 185 };

// The three courtyards of the north block are enclosed lawns sitting on the
// level-1 slab: their basement windows are under ground, which is what the
// survey shows and what the owner's review asked for. They are ringed by the
// building on all four sides, so the step up happens inside the wall and is
// never visible as a cliff.
const COURTYARDS = [
  { x0: -160, y0: 42, x1: -90, y1: 154 },
  { x0: -58,  y0: 42, x1: 12,  y1: 154 },
  { x0: 44,   y0: 42, x1: 114, y1: 154 },
];
function courtyardFrac(x, y) {
  let f = 0;
  for (const c of COURTYARDS) {
    const d = Math.min(x - c.x0, c.x1 - x, y - c.y0, c.y1 - y);
    if (d > 0) f = Math.max(f, smooth01(d / 6));
  }
  return f;
}

// Parts of the level-0 plan that are cellar: the plan draws rooms there, but on
// site the yard runs straight over them. Nothing is built above ground inside
// these and the terrain is not holed for them.
const CELLARS = [
  // The vault under the south-west yard: the level-0 plan draws a wing running
  // south into the yard between x -86.25 and -50.5, from y 9.25 down to -42.5,
  // and on site the yard runs straight over it.
  //
  // The rectangle has to sit *just* outside that wing and nowhere near anything
  // else. Every wall run whose midpoint falls inside it is dropped, so a
  // rectangle drawn generously — the way this one used to be — eats the real
  // south elevation either side of the wing and leaves a slot of open sky
  // through the building, which is the "structure is broken" of the owner's
  // first slide. North edge 9.0 keeps the elevation at y 9.25 and the level-1
  // elevation at y 11.25; the sides clear the wing's own faces by three
  // quarters of a metre and no more.
  { x0: -87, y0: -44, x1: -50, y1: 9 },
  // The needle at the plaza. The diagonal wing is drawn running out to a point
  // at about (109, -21), and the last dozen metres of it are two or three
  // metres wide: a knife of plan that the site does not have — the plaza paving
  // runs across it. Built, it comes out as a single panel of brick with a
  // window and a coping standing clear of everything around it, which is what
  // the owner circled on slide 1 and struck out again as "still not fixed".
  // The rectangle stops short of the glazed plaza wing's west face at x 129 and
  // short of the wing proper at y -27, so only the needle goes.
  { x0: 103, y0: -27, x1: 124, y1: -12 },
];
function inCellar(x, y) {
  for (const c of CELLARS) {
    if (x > c.x0 && x < c.x1 && y > c.y0 && y < c.y1) return true;
  }
  return false;
}

// How deeply the level-0 storey is buried at (x,y): 0 = floor at grade,
// 1 = a full storey underground.
function buriedFrac(x, y) {
  return Math.max(smooth01((y - GRADE_Y0) / (GRADE_Y1 - GRADE_Y0)), courtyardFrac(x, y));
}

function groundZ(x, y) {
  let z = EXT_STOREY * buriedFrac(x, y);
  // Beyond the building the ground keeps falling away to the west and south —
  // the lawn in photo 8 and the embankment above the plaza in photo 19. The
  // fall is gentle: photo 8 shows the whole basement storey standing clear of
  // a lawn that is very nearly level along the west elevation.
  const outW = Math.max(0, siteBB.minX - 60 - x);
  const outS = Math.max(0, siteBB.minY - 60 - y);
  z -= Math.min(7, 0.030 * outW + 0.026 * outS);
  // a very soft roll so the lawn is never a dead plane
  z += 0.9 * Math.sin(x * 0.0075 + 1.3) * Math.cos(y * 0.0062 - 0.4);
  return z;
}

// ── renderer ─────────────────────────────────────────────────────────────
// The viewer starts hidden, so the very first setSize() wrote a 0px inline
// height onto the canvas — which then kept the container at zero height for
// good. Let the stylesheet own the element's size and never write it back.
canvas.style.width = '';
canvas.style.height = '';
renderer.setSize(viewerW(), viewerH(), false);
renderer.setPixelRatio(Math.min(window.devicePixelRatio, 1.75));
// The sky is a dome far outside the building; the original 2000-unit far
// plane sliced straight through it and left a black band above the roofline.
camera3D.far = 9000;
camera3D.updateProjectionMatrix();
renderer.toneMapping = THREE.ACESFilmicToneMapping;
renderer.toneMappingExposure = 1.05;
renderer.shadowMap.enabled = true;
renderer.shadowMap.type = THREE.PCFSoftShadowMap;
renderer.shadowMap.autoUpdate = false;   // shadows are static; refreshed on demand
renderer.setClearColor(0x0d1220);

// Semantic overlays (room fills, routes, coverage, labels, equipment) must keep
// their exact requested colour, so they opt out of tone mapping.
function noTone(m) {
  if (!m) return m;
  if (Array.isArray(m)) { m.forEach(noTone); return m; }
  m.toneMapped = false;
  return m;
}

function requestShadowUpdate() { renderer.shadowMap.needsUpdate = true; }

// ── sky ──────────────────────────────────────────────────────────────────
// A dome with a gradient and thin cirrus, matching the high pale-blue Nordic
// sky in the reference photograph. Cheap: only sky pixels pay for it.
const skyUniforms = {
  uTop:     { value: new THREE.Color(0x12101f) },
  uMid:     { value: new THREE.Color(0x1a1a2e) },
  uHorizon: { value: new THREE.Color(0x3a2547) },
  uSunDir:  { value: new THREE.Vector3(0.45, 0.55, 0.72).normalize() },
  uCloud:   { value: 0.0 },
};
const skyMat = new THREE.ShaderMaterial({
  side: THREE.BackSide, depthWrite: false, fog: false,
  uniforms: skyUniforms,
  vertexShader: `
    varying vec3 vDir;
    void main() {
      vDir = normalize(position);
      gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
    }`,
  fragmentShader: `
    varying vec3 vDir;
    uniform vec3 uTop, uMid, uHorizon, uSunDir;
    uniform float uCloud;
    float hash(vec2 p){ return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
    float vnoise(vec2 p){
      vec2 i = floor(p), f = fract(p);
      f = f * f * (3.0 - 2.0 * f);
      return mix(mix(hash(i), hash(i + vec2(1,0)), f.x),
                 mix(hash(i + vec2(0,1)), hash(i + vec2(1,1)), f.x), f.y);
    }
    float fbm(vec2 p){
      float v = 0.0, a = 0.5;
      for (int i = 0; i < 4; i++) { v += a * vnoise(p); p *= 2.03; a *= 0.5; }
      return v;
    }
    void main() {
      vec3 d = normalize(vDir);
      float h = clamp(d.z, 0.0, 1.0);
      vec3 col = mix(uHorizon, uMid, pow(h, 0.55));
      col = mix(col, uTop, pow(h, 1.7));
      // sun bloom
      float s = max(dot(d, normalize(uSunDir)), 0.0);
      col += vec3(1.0, 0.94, 0.82) * pow(s, 180.0) * 1.4;
      col += vec3(1.0, 0.92, 0.80) * pow(s, 8.0) * 0.11;
      // thin cirrus, flattened onto the dome
      if (d.z > 0.02) {
        vec2 uv0 = d.xy / (d.z + 0.22);
        vec2 uv = uv0;
        float band = fbm(vec2(uv.x * 0.9, uv.y * 3.2));
        float n = fbm(vec2(uv.x * 1.7, uv.y * 5.0) + vec2(band * 1.6, 0.0));
        n = smoothstep(0.40, 0.80, n * (0.80 + 0.45 * band));
        float streak = smoothstep(0.01, 0.16, d.z);
        col = mix(col, vec3(1.0, 0.995, 0.99), n * uCloud * streak);
      }
      gl_FragColor = vec4(col, 1.0);
      #include <tonemapping_fragment>
      #include <colorspace_fragment>
    }`,
});
const skyDome = new THREE.Mesh(new THREE.SphereGeometry(3400, 40, 24), skyMat);
skyDome.rotation.x = Math.PI / 2;          // scene is Z-up
skyDome.renderOrder = -1000;
skyDome.frustumCulled = false;
scene.add(skyDome);

// Aerial haze that only bites well beyond the building, so the ground meets
// the sky instead of ending in a hard line.
scene.fog = new THREE.Fog(0x231d33, 1500, 3300);

// ── lighting ─────────────────────────────────────────────────────────────
// The survey was shot under a high overcast, and the elevations were matched
// to it: sampling the same brick off photograph and render showed the walls
// turned away from the sun coming out 40 % too dark. The sky now carries most
// of the light and the sun is a soft key on top of it, which is both what
// Luleå looks like in August and what makes a render comparable to a photo.
const hemiLight = new THREE.HemisphereLight(0xd6e6f6, 0x7d7856, 3.24);
scene.add(hemiLight);
// A little flat fill so interiors are never pitch black when the roof is on.
const ambLight = new THREE.AmbientLight(0xb4c6d6, 0.88);
scene.add(ambLight);

const keyLight = new THREE.DirectionalLight(0xfff4e6, 0.50);
keyLight.position.set(300, 250, 460);
keyLight.castShadow = true;
keyLight.shadow.mapSize.set(3072, 3072);
keyLight.shadow.camera.near = 60;
keyLight.shadow.camera.far = 1800;
keyLight.shadow.camera.left = -430;
keyLight.shadow.camera.right = 430;
keyLight.shadow.camera.top = 430;
keyLight.shadow.camera.bottom = -430;
keyLight.shadow.bias = -0.0012;
keyLight.shadow.normalBias = 0.6;
scene.add(keyLight);
scene.add(keyLight.target);

const fillLight = new THREE.DirectionalLight(0xd6e6ff, 1.55);
fillLight.position.set(-320, -240, 180);
scene.add(fillLight);

// ── procedural texture helpers ───────────────────────────────────────────
function texCanvas(w, h) {
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  return c;
}
function finishTex(canvas, repeatX, repeatY, aniso) {
  const t = new THREE.CanvasTexture(canvas);
  t.wrapS = t.wrapT = THREE.RepeatWrapping;
  t.repeat.set(repeatX || 1, repeatY || 1);
  t.anisotropy = Math.min(aniso || 8, renderer.capabilities.getMaxAnisotropy());
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}
// Deterministic RNG so the campus looks the same on every load.
function mulberry(seed) {
  let a = seed >>> 0;
  return function () {
    a |= 0; a = (a + 0x6D2B79F5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// ── footprint extraction ─────────────────────────────────────────────────
// Rooms are thousands of little polygons; the building's outline is their
// union. Rasterise the rooms onto a 1-unit grid, morphologically close it so
// wall thicknesses don't leave slivers, trace the filled/empty boundary into
// closed loops, then simplify. Returns rings in world coordinates with the
// outer ring first (CCW) and courtyards after it (CW).
function extractFootprint(level, opts) {
  const o = opts || {};
  const cell = o.cell || 1.0;
  const closeR = o.close === undefined ? 3 : o.close;
  const eps = o.eps === undefined ? 1.7 : o.eps;
  const minArea = o.minArea === undefined ? 90 : o.minArea;
  const data = floorData[level];
  if (!data || !data.rooms || !data.rooms.length) return [];
  const pw = data.page.width, ph = data.page.height;

  // world-space bounds
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const polys = [];
  for (const r of data.rooms) {
    if (!r.polygon || r.polygon.length < 3) continue;
    const p = r.polygon.map(pt => pdfToWorld(pt[0], pt[1], pw, ph));
    polys.push(p);
    for (const q of p) {
      if (q[0] < minX) minX = q[0];
      if (q[0] > maxX) maxX = q[0];
      if (q[1] < minY) minY = q[1];
      if (q[1] > maxY) maxY = q[1];
    }
  }
  if (!polys.length) return [];
  const pad = (closeR + 3) * cell;
  minX -= pad; minY -= pad; maxX += pad; maxY += pad;
  const W = Math.ceil((maxX - minX) / cell) + 1;
  const H = Math.ceil((maxY - minY) / cell) + 1;
  if (W * H > 4e6) return [];

  const grid = new Uint8Array(W * H);
  // scanline polygon fill
  for (const p of polys) {
    let pyMin = Infinity, pyMax = -Infinity;
    for (const q of p) { if (q[1] < pyMin) pyMin = q[1]; if (q[1] > pyMax) pyMax = q[1]; }
    const y0 = Math.max(0, Math.floor((pyMin - minY) / cell));
    const y1 = Math.min(H - 1, Math.ceil((pyMax - minY) / cell));
    for (let gy = y0; gy <= y1; gy++) {
      const wy = minY + (gy + 0.5) * cell;
      const xs = [];
      for (let i = 0, n = p.length; i < n; i++) {
        const a = p[i], b = p[(i + 1) % n];
        if ((a[1] <= wy && b[1] > wy) || (b[1] <= wy && a[1] > wy)) {
          xs.push(a[0] + (wy - a[1]) / (b[1] - a[1]) * (b[0] - a[0]));
        }
      }
      if (xs.length < 2) continue;
      xs.sort((m, n2) => m - n2);
      for (let k = 0; k + 1 < xs.length; k += 2) {
        const gx0 = Math.max(0, Math.round((xs[k] - minX) / cell - 0.5));
        const gx1 = Math.min(W - 1, Math.round((xs[k + 1] - minX) / cell - 0.5));
        for (let gx = gx0; gx <= gx1; gx++) grid[gy * W + gx] = 1;
      }
    }
  }

  const closed = closeR > 0 ? morphClose(grid, W, H, closeR) : grid;
  const loops = traceLoops(closed, W, H);
  const rings = [];
  for (const loop of loops) {
    const simple = simplifyClosed(loop, eps);
    if (simple.length < 4) continue;
    const world = flattenBows(
      straightenJogs(simple.map(p => [minX + p[0] * cell, minY + p[1] * cell]), 2.5));
    const a = signedArea(world);
    if (Math.abs(a) < minArea) continue;
    rings.push({ pts: world, area: a });
  }
  rings.sort((a, b) => Math.abs(b.area) - Math.abs(a.area));
  return rings;
}

// Take the shallow jogs out of a long elevation.
//
// The plan is drawn room by room and the rasterised outline inherits every
// half-metre the drawing wanders: two long runs of the same wall arrive a
// metre or two apart and the trace joins them with a short step. The elevation
// builder chops runs into bays and gives each one its own quad, so that step
// comes out as a single bay of facade set back behind its neighbours — the
// owner's "the wall is not flat, one of the bricks is pushed into the
// building". It also leaves the odd needle of wall at the tip of a wing where
// two such steps meet, which is the free-standing slab of slide 1.
//
// So wherever two long runs are all but parallel and the step between them is
// shallower than `maxOff`, drop the step and let one run continue into the
// other. A real set-back — a recessed entrance bay, the corner where the south
// front steps in — is deeper than that and is left exactly as drawn.
function straightenJogs(pts, maxOff) {
  let out = pts.slice();
  for (let pass = 0; pass < 8; pass++) {
    const n = out.length;
    if (n < 6) break;
    let cut = -1;
    for (let i = 0; i < n; i++) {
      const a = out[(i - 1 + n) % n], b = out[i];
      const c = out[(i + 1) % n], d = out[(i + 2) % n];
      const e1x = b[0] - a[0], e1y = b[1] - a[1], l1 = Math.hypot(e1x, e1y);
      const e2 = Math.hypot(c[0] - b[0], c[1] - b[1]);
      const e3x = d[0] - c[0], e3y = d[1] - c[1], l3 = Math.hypot(e3x, e3y);
      // both neighbours have to be real elevations, and the step between them
      // short — otherwise this is a corner and not a wobble
      if (l1 < 7 || l3 < 7 || e2 > 6 || l1 < 1e-6 || l3 < 1e-6) continue;
      const u1x = e1x / l1, u1y = e1y / l1;
      if ((u1x * e3x + u1y * e3y) / l3 < 0.94) continue;   // not the same wall
      // how far the second run stands off the line of the first
      const off = Math.abs(u1x * (c[1] - a[1]) - u1y * (c[0] - a[0]));
      if (off > maxOff) continue;
      cut = i;
      break;
    }
    if (cut < 0) break;
    const n2 = out.length;
    const drop = new Set([cut, (cut + 1) % n2]);
    out = out.filter((_, k) => !drop.has(k));
  }
  return out;
}

// Take the bow out of a long wall.
//
// `straightenJogs` above deals with one short step between two long runs. What
// it cannot see is the other shape the tracing leaves: a long straight wall
// interrupted by a *chain* of short facets that wander off the line and come
// back to it. The diagonal wing on the plaza is the case the owner drew a
// circle round — the ground floor leaves its 113 m elevation at (108.7, -20.5),
// bulges twelve metres in over eight little vertices and rejoins it at
// (34.7, -106.5), while the two storeys above it, traced from their own
// drawings, run the same wall as a single edge. From the air the ground floor
// reads as a curve, and because the upper rings are snapped to the base one the
// curve is carried all the way up: "the building wall should not be curved like
// this. Just a straight wall."
//
// So: two long runs pointing the same way, a short chain between them, and the
// chain collapses onto the line the two runs share.
//
// A real articulated elevation must survive this, and most of what sits between
// two long parallel runs is real: the recess in the west wall that the skywalk
// crosses, the glazed bays that step out of the north front, a recessed
// entrance. Two things tell those from a bow.
//
//   · A bow turns one way and then back — two changes of hand at most. The
//     zig-zag bays on the south front alternate, four or more, and stay.
//   · Every facet of a bow is *small* and *travels along the wall*: the trace
//     is walking from one end of the run to the other and merely wandering off
//     the line while it does it, so each little segment is a few metres of
//     stair and still points broadly the way the chord between the two ends
//     points. A piece of building does neither — the recess in the west wall
//     turns out of the elevation at a right angle and comes back at one, the
//     re-entrant corner of the east yard is fifty metres of wall — and the
//     moment one facet does either the chain is left exactly as drawn.
function flattenBows(pts, maxOff, minRun) {
  const off = maxOff === undefined ? 13 : maxOff;
  const long = minRun === undefined ? 24 : minRun;
  const short = 10;                       // the lesser of the two runs
  const facet = 20;                       // longer than this and it is a wall
  const alongCos = 0.5;                   // 60° off the chord and it is a return
  let out = pts.slice();
  for (let pass = 0; pass < 8; pass++) {
    const n = out.length;
    if (n < 8) break;
    let found = null;
    for (let i = 0; i < n && !found; i++) {
      const a = out[i], b = out[(i + 1) % n];        // the run that arrives
      const l1 = Math.hypot(b[0] - a[0], b[1] - a[1]);
      if (l1 < short) continue;
      const u1x = (b[0] - a[0]) / l1, u1y = (b[1] - a[1]) / l1;
      // walk forward over the chain, looking for the run that leaves
      for (let k = 3; k <= 9 && !found; k++) {
        const c = out[(i + k) % n], d = out[(i + k + 1) % n];
        const l3 = Math.hypot(d[0] - c[0], d[1] - c[1]);
        if (l3 < short) continue;
        if (Math.max(l1, l3) < long) continue;
        if ((u1x * (d[0] - c[0]) + u1y * (d[1] - c[1])) / l3 < 0.995) continue;
        // b and c must be the two ends of one line, and the chain between them
        // must stay close to it and on one side of it
        const cl = Math.hypot(c[0] - b[0], c[1] - b[1]);
        if (cl < 1e-6 || cl > Math.min(l1, l3) * 1.4) continue;
        const vx = (c[0] - b[0]) / cl, vy = (c[1] - b[1]) / cl;
        let lo = 0, hi = 0, ok = true;
        for (let j = 2; j < k; j++) {
          const p = out[(i + j) % n];
          const e = vx * (p[1] - b[1]) - vy * (p[0] - b[0]);
          if (Math.abs(e) > off) { ok = false; break; }
          if (e < lo) lo = e;
          if (e > hi) hi = e;
        }
        if (!ok) continue;
        if (Math.min(-lo, hi) > 2.5) continue;        // a zig-zag, not a bow
        // every facet has to be short and going the way the chord goes; a long
        // one, or one that turns out of the wall, is a piece of the building
        for (let j = 1; j < k && ok; j++) {
          const p = out[(i + j) % n], q = out[(i + j + 1) % n];
          const sx = q[0] - p[0], sy = q[1] - p[1];
          const sl = Math.hypot(sx, sy);
          if (sl > facet) ok = false;
          else if (sl > 1e-6 && (vx * sx + vy * sy) / sl < alongCos) ok = false;
        }
        if (!ok) continue;
        // and it has to turn like a bow: at most two changes of hand
        let flips = 0, prev = 0;
        for (let j = 0; j <= k; j++) {
          const p = out[(i + j) % n], q = out[(i + j + 1) % n];
          const r = out[(i + j + 2) % n];
          const cr = (q[0] - p[0]) * (r[1] - q[1]) - (q[1] - p[1]) * (r[0] - q[0]);
          const s = Math.abs(cr) < 1e-6 ? 0 : (cr > 0 ? 1 : -1);
          if (s && prev && s !== prev) flips++;
          if (s) prev = s;
        }
        if (flips > 2) continue;
        found = { i, k };
      }
    }
    if (!found) break;
    const n2 = out.length;
    const drop = new Set();          // everything strictly between b and c
    for (let j = 2; j < found.k; j++) drop.add((found.i + j) % n2);
    out = out.filter((_, idx) => !drop.has(idx));
  }
  return out;
}

// Separable box dilate/erode — a cheap approximation of a disc structuring
// element, which is all we need to bridge wall-thickness gaps.
function morphClose(src, W, H, r) {
  const dil = boxMorph(src, W, H, r, 1);
  return boxMorph(dil, W, H, r, 0);
}
function boxMorph(src, W, H, r, want) {
  const tmp = new Uint8Array(W * H);
  const out = new Uint8Array(W * H);
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      let v = want ? 0 : 1;
      for (let d = -r; d <= r; d++) {
        const xx = x + d;
        if (xx < 0 || xx >= W) { if (!want) { v = 0; break; } continue; }
        const s = src[y * W + xx];
        if (want) { if (s) { v = 1; break; } }
        else if (!s) { v = 0; break; }
      }
      tmp[y * W + x] = v;
    }
  }
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      let v = want ? 0 : 1;
      for (let d = -r; d <= r; d++) {
        const yy = y + d;
        if (yy < 0 || yy >= H) { if (!want) { v = 0; break; } continue; }
        const s = tmp[yy * W + x];
        if (want) { if (s) { v = 1; break; } }
        else if (!s) { v = 0; break; }
      }
      out[y * W + x] = v;
    }
  }
  return out;
}

// Boundary tracing: emit one directed unit edge per filled/empty cell face,
// oriented so the filled side is on the left, then chain them into loops.
function traceLoops(g, W, H) {
  const key = (x, y) => y * (W + 1) + x;
  const next = new Map();
  const push = (a, b) => {
    const k = key(a[0], a[1]);
    const l = next.get(k);
    if (l) l.push(b); else next.set(k, [b]);
  };
  const on = (x, y) => x >= 0 && y >= 0 && x < W && y < H && g[y * W + x];
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) {
      if (!g[y * W + x]) continue;
      if (!on(x, y - 1)) push([x, y], [x + 1, y]);
      if (!on(x + 1, y)) push([x + 1, y], [x + 1, y + 1]);
      if (!on(x, y + 1)) push([x + 1, y + 1], [x, y + 1]);
      if (!on(x - 1, y)) push([x, y + 1], [x, y]);
    }
  }
  const loops = [];
  let guard = 0;
  while (next.size && guard++ < 100000) {
    const startKey = next.keys().next().value;
    const sy = Math.floor(startKey / (W + 1)), sx = startKey % (W + 1);
    const start = [sx, sy];
    const loop = [start];
    let cur = start;
    for (let i = 0; i < 400000; i++) {
      const k = key(cur[0], cur[1]);
      const lst = next.get(k);
      if (!lst || !lst.length) break;
      const nxt = lst.shift();
      if (!lst.length) next.delete(k);
      loop.push(nxt);
      cur = nxt;
      if (cur[0] === start[0] && cur[1] === start[1]) break;
    }
    if (loop.length > 6) loops.push(loop);
  }
  return loops;
}

function signedArea(p) {
  let a = 0;
  for (let i = 0, n = p.length; i < n; i++) {
    const q = p[i], r = p[(i + 1) % n];
    a += q[0] * r[1] - r[0] * q[1];
  }
  return a / 2;
}

function simplifyClosed(loop, eps) {
  // drop the duplicated closing vertex, split the ring at its two extremes so
  // Douglas–Peucker has well-defined endpoints, then rejoin.
  const p = loop.slice(0, loop.length - 1);
  if (p.length < 8) return p;
  let iMin = 0, iMax = 0;
  for (let i = 1; i < p.length; i++) {
    if (p[i][0] + p[i][1] < p[iMin][0] + p[iMin][1]) iMin = i;
    if (p[i][0] + p[i][1] > p[iMax][0] + p[iMax][1]) iMax = i;
  }
  const a = Math.min(iMin, iMax), b = Math.max(iMin, iMax);
  const s1 = dp(p.slice(a, b + 1), eps);
  const s2 = dp(p.slice(b).concat(p.slice(0, a + 1)), eps);
  return s1.slice(0, -1).concat(s2.slice(0, -1));
}
function dp(pts, eps) {
  if (pts.length < 3) return pts.slice();
  const a = pts[0], b = pts[pts.length - 1];
  const dx = b[0] - a[0], dy = b[1] - a[1];
  const L = Math.hypot(dx, dy);
  let best = -1, bi = 0;
  for (let i = 1; i < pts.length - 1; i++) {
    const d = L < 1e-9
      ? Math.hypot(pts[i][0] - a[0], pts[i][1] - a[1])
      : Math.abs((pts[i][0] - a[0]) * dy - (pts[i][1] - a[1]) * dx) / L;
    if (d > best) { best = d; bi = i; }
  }
  if (best > eps) {
    const l = dp(pts.slice(0, bi + 1), eps);
    const r = dp(pts.slice(bi), eps);
    return l.slice(0, -1).concat(r);
  }
  return [a, b];
}

function pointInRing(pt, ring) {
  let inside = false;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const xi = ring[i][0], yi = ring[i][1], xj = ring[j][0], yj = ring[j][1];
    if ((yi > pt[1]) !== (yj > pt[1]) &&
        pt[0] < (xj - xi) * (pt[1] - yi) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

// Shapes from a ring set, for roof slabs, floor plates and ceilings.
//
// A floor is not one polygon with holes: the plan can have several separate
// solid pieces (a detached wing) as well as courtyards punched through them.
// Rings that wind the same way as the largest one are solids; the rest are
// holes, each assigned to the smallest solid that contains it. Feeding a
// separate solid to the triangulator as if it were a hole is what produced
// the black wedge across the plate.
function shapesFromRings(rings) {
  if (!rings || !rings.length) return [];
  const sgn = Math.sign(rings[0].area) || 1;
  const solids = [], holes = [];
  for (const r of rings) (Math.sign(r.area) === sgn ? solids : holes).push(r);
  if (!solids.length) return [];

  const shapes = solids.map(s => {
    const shape = new THREE.Shape();
    shape.moveTo(s.pts[0][0], s.pts[0][1]);
    for (let i = 1; i < s.pts.length; i++) shape.lineTo(s.pts[i][0], s.pts[i][1]);
    shape.closePath();
    return { shape, ring: s };
  });

  for (const h of holes) {
    let host = null;
    for (const s of shapes) {
      if (!pointInRing(h.pts[0], s.ring.pts)) continue;
      if (!host || Math.abs(s.ring.area) < Math.abs(host.ring.area)) host = s;
    }
    if (!host) continue;                       // stray loop, drop it
    const path = new THREE.Path();
    path.moveTo(h.pts[0][0], h.pts[0][1]);
    for (let i = 1; i < h.pts.length; i++) path.lineTo(h.pts[i][0], h.pts[i][1]);
    path.closePath();
    host.shape.holes.push(path);
  }
  return shapes.map(s => s.shape);
}
function shapeFromRings(rings) {
  const s = shapesFromRings(rings);
  return s.length ? s : null;
}

// Vertical wall band along a ring, textured by arc length so panel modules
// keep a constant real-world width all the way round the building. The face
// normal is the edge's right-hand side, which for a CCW outer ring points out
// of the building and for a CW courtyard ring points into the courtyard —
// exactly the two surfaces anyone can see.
function ringWallGeometry(ring, z0, z1, uPerUnit, invert) {
  const n = ring.length;
  const pos = [], nrm = [], uv = [];
  let run = 0;
  const s = invert ? -1 : 1;
  for (let i = 0; i < n; i++) {
    const a = ring[i], b = ring[(i + 1) % n];
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const len = Math.hypot(dx, dy);
    if (len < 1e-6) continue;
    const nx = s * dy / len, ny = -s * dx / len;
    const u0 = run * uPerUnit, u1 = (run + len) * uPerUnit;
    run += len;
    const A = [a[0], a[1], z0], B = [b[0], b[1], z0];
    const C = [b[0], b[1], z1], D = [a[0], a[1], z1];
    const tri = (p, q, r, uvp, uvq, uvr) => {
      pos.push(...p, ...q, ...r);
      nrm.push(nx, ny, 0, nx, ny, 0, nx, ny, 0);
      uv.push(...uvp, ...uvq, ...uvr);
    };
    if (!invert) {
      tri(A, B, C, [u0, 0], [u1, 0], [u1, 1]);
      tri(A, C, D, [u0, 0], [u1, 1], [u0, 1]);
    } else {
      tri(A, C, B, [u0, 0], [u1, 1], [u1, 0]);
      tri(A, D, C, [u0, 0], [u0, 1], [u1, 1]);
    }
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  g.setAttribute('normal', new THREE.Float32BufferAttribute(nrm, 3));
  g.setAttribute('uv', new THREE.Float32BufferAttribute(uv, 2));
  g.computeBoundingSphere();
  return g;
}

// ── ground & campus ──────────────────────────────────────────────────────
const worldGroup = new THREE.Group();
worldGroup.name = 'campus';
scene.add(worldGroup);

function groundTexture() {
  const c = texCanvas(512, 512);
  const x = c.getContext('2d');
  x.fillStyle = '#6d8442';
  x.fillRect(0, 0, 512, 512);
  const rnd = mulberry(7);
  for (let i = 0; i < 14000; i++) {
    const r = rnd();
    x.fillStyle = r > 0.80 ? 'rgba(146,176,88,0.55)'
      : r > 0.54 ? 'rgba(84,106,50,0.5)'
      : r > 0.24 ? 'rgba(112,140,66,0.45)' : 'rgba(150,150,96,0.32)';
    const s = 1.5 + rnd() * 7;
    x.fillRect(rnd() * 512, rnd() * 512, s, s * (0.4 + rnd()));
  }
  // gravel drifts
  for (let i = 0; i < 12; i++) {
    x.globalAlpha = 0.13;
    x.fillStyle = '#9aa072';
    x.beginPath();
    x.ellipse(rnd() * 512, rnd() * 512, 30 + rnd() * 70, 18 + rnd() * 44, rnd() * 3, 0, Math.PI * 2);
    x.fill();
    x.globalAlpha = 1;
  }
  return finishTex(c, 34, 34, 4);
}

// Concrete slab paving: 0.9 m slabs laid in a grid with the darker banded
// courses the campus uses (photos 6, 10, 14, 19).
function paveTexture() {
  const c = texCanvas(512, 512);
  const x = c.getContext('2d');
  x.fillStyle = '#a49e93';
  x.fillRect(0, 0, 512, 512);
  const rnd = mulberry(19);
  const S = 64;
  for (let gy = 0; gy < 8; gy++) {
    for (let gx = 0; gx < 8; gx++) {
      const px = gx * S, py = gy * S;
      const v = 0.95 + rnd() * 0.09;
      // every fourth course is a dark red-brown band
      const band = (gx % 4 === 3) || (gy % 4 === 3);
      const base = band ? [160, 147, 133] : [166, 161, 151];
      x.fillStyle = `rgb(${Math.round(base[0] * v)},${Math.round(base[1] * v)},${Math.round(base[2] * v)})`;
      x.fillRect(px + 1, py + 1, S - 2, S - 2);
      x.fillStyle = 'rgba(255,255,255,0.055)';
      x.fillRect(px + 1, py + 1, S - 2, 2);
      x.fillStyle = 'rgba(0,0,0,0.10)';
      x.fillRect(px + 1, py + S - 3, S - 2, 2);
    }
  }
  x.globalAlpha = 0.16;
  for (let i = 0; i < 2600; i++) {
    x.fillStyle = rnd() > 0.5 ? '#ffffff' : '#54514c';
    x.fillRect(rnd() * 512, rnd() * 512, 2, 2);
  }
  return finishTex(c, 1, 1, 8);
}

// The red-brown clay tile of the inner courtyards (photos 3, 19, 27, 28).
function tileTexture() {
  const c = texCanvas(512, 512);
  const x = c.getContext('2d');
  x.fillStyle = '#9d6248';
  x.fillRect(0, 0, 512, 512);
  const rnd = mulberry(23);
  const S = 51.2;
  for (let gy = 0; gy < 10; gy++) {
    for (let gx = 0; gx < 10; gx++) {
      const v = 0.88 + rnd() * 0.22;
      x.fillStyle = `rgb(${Math.round(178 * v)},${Math.round(112 * v)},${Math.round(84 * v)})`;
      x.fillRect(gx * S + 1, gy * S + 1, S - 2, S - 2);
    }
  }
  x.globalAlpha = 0.14;
  for (let i = 0; i < 2200; i++) {
    x.fillStyle = rnd() > 0.5 ? '#ffffff' : '#4b2f24';
    x.fillRect(rnd() * 512, rnd() * 512, 2, 2);
  }
  return finishTex(c, 1, 1, 8);
}

const groundMat = new THREE.MeshStandardMaterial({
  map: groundTexture(), roughness: 0.98, metalness: 0.0, color: 0xffffff,
});
const paveMat = new THREE.MeshStandardMaterial({
  map: paveTexture(), roughness: 0.90, metalness: 0.0,
});
const tileMat = new THREE.MeshStandardMaterial({
  map: tileTexture(), roughness: 0.88, metalness: 0.0,
});
paveMat.map.repeat.set(1 / 15, 1 / 15);
tileMat.map.repeat.set(1 / 13, 1 / 13);
groundMat.map.repeat.set(1 / 26, 1 / 26);

// The site is presented as a bounded piece of ground — the building, its lawns
// and plazas, path stubs that run out, and nothing beyond. There is no distant
// apron: the terrain ends at the site edge and drops away as a soil rim, the
// way a model of the block would sit on its base.
const SITE_PAD = 104;           // how far the ground reaches past the building
const RIM_DROP = 15;            // how deep the cut edge of the site reads
const soilMat = new THREE.MeshStandardMaterial({ color: 0x6a5442, roughness: 0.99 });

// ── graded terrain ───────────────────────────────────────────────────────
// One mesh per surface (lawn / slab paving / courtyard tile) sharing a single
// grid of vertices, so the grade is continuous across a material change.
// Quads that fall inside the building are dropped — the footprint is a hole,
// which is what lets the floor plates show when the shell peels open — but the
// hole is eroded a couple of metres so the terrain always tucks under the wall.

// Rasterise the footprint rings into a mask, then erode, so "is this point
// inside the building" is one array lookup rather than a polygon crossing test
// against a thousand-vertex ring.
function footprintMask(rings, cell, erode) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const r of rings) for (const p of r.pts) {
    if (p[0] < minX) minX = p[0];
    if (p[0] > maxX) maxX = p[0];
    if (p[1] < minY) minY = p[1];
    if (p[1] > maxY) maxY = p[1];
  }
  const pad = (erode + 16) * cell;
  minX -= pad; minY -= pad; maxX += pad; maxY += pad;
  const W = Math.ceil((maxX - minX) / cell) + 1;
  const H = Math.ceil((maxY - minY) / cell) + 1;
  // Even–odd scanline fill. Against every ring at once it gives the solid
  // building (courtyards punched out); against the outer ring alone it gives
  // the site block, and the difference between them is the courtyards.
  function fill(rs) {
    const grid = new Uint8Array(W * H);
    for (let gy = 0; gy < H; gy++) {
      const wy = minY + (gy + 0.5) * cell;
      const xs = [];
      for (const r of rs) {
        const p = r.pts;
        for (let i = 0, n = p.length; i < n; i++) {
          const a = p[i], b = p[(i + 1) % n];
          if ((a[1] <= wy && b[1] > wy) || (b[1] <= wy && a[1] > wy)) {
            xs.push(a[0] + (wy - a[1]) / (b[1] - a[1]) * (b[0] - a[0]));
          }
        }
      }
      if (xs.length < 2) continue;
      xs.sort((m, n) => m - n);
      for (let k = 0; k + 1 < xs.length; k += 2) {
        const g0 = Math.max(0, Math.ceil((xs[k] - minX) / cell - 0.5));
        const g1 = Math.min(W - 1, Math.floor((xs[k + 1] - minX) / cell - 0.5));
        for (let gx = g0; gx <= g1; gx++) grid[gy * W + gx] = 1;
      }
    }
    return grid;
  }
  const solid = fill(rings);
  const block = fill([rings[0]]);
  const eroded = erode > 0 ? boxMorph(solid, W, H, erode, 0) : solid;
  const near = boxMorph(solid, W, H, Math.round(30 / cell), 1);
  const at = (g) => (x, y) => {
    const gx = Math.round((x - minX) / cell - 0.5);
    const gy = Math.round((y - minY) / cell - 0.5);
    if (gx < 0 || gy < 0 || gx >= W || gy >= H) return false;
    return !!g[gy * W + gx];
  };
  return { eroded: at(eroded), solid: at(solid), block: at(block), near: at(near) };
}

// The site grid: a plain rectangle around the building, evenly divided, so the
// ground ends on a straight cut rather than trailing off to the horizon.
function terrainAxis(lo, hi, step) {
  const n = Math.max(2, Math.round((hi - lo) / step));
  const out = [];
  for (let i = 0; i <= n; i++) out.push(lo + (hi - lo) * i / n);
  return out;
}

// The roads and footpaths are stubs: they come in off the site, run past the
// building and taper away before the edge, so the eye is never led out of the
// scene. Each is a segment with a half-width that closes to nothing at both
// ends (x0, y0, x1, y1, halfWidth, surface).
let PATHS = [];
function buildPaths() {
  const { minX, maxX, minY, maxY } = siteBB;
  PATHS = [
    // the walk along the north front, out of photo 2 and photo 15
    [minX - 40, maxY + 26, maxX + 34, maxY + 26, 13, 'pave'],
    // the east alley of photos 4, 12 and 13
    [maxX + 30, maxY - 10, maxX + 30, minY + 40, 12, 'pave'],
    // the south approach — red clay tile, photos 14, 21 and 28
    [minX - 30, minY - 16, maxX + 10, minY - 16, 15, 'tile'],
    // the path down the west slope to the plaza, photo 8
    [minX - 46, 96, minX - 46, -80, 9, 'pave'],
  ];
}
function pathSurfaceAt(x, y) {
  for (const [x0, y0, x1, y1, hw, surf] of PATHS) {
    const dx = x1 - x0, dy = y1 - y0;
    const l2 = dx * dx + dy * dy;
    let t = l2 ? ((x - x0) * dx + (y - y0) * dy) / l2 : 0;
    if (t < 0 || t > 1) continue;
    const px = x0 + dx * t, py = y0 + dy * t;
    // taper: full width over the middle, closing to a point at each end
    const w = hw * smooth01(Math.min(t, 1 - t) / 0.22);
    if (w > 0.5 && Math.hypot(x - px, y - py) < w) return surf;
  }
  return null;
}

// Which surface is underfoot. The photographs are consistent: slab paving all
// round the south, east and north approaches, lawn on the west slope and along
// the north-east edge, clay tile on the south approach and in the yards, and
// mown grass in the three enclosed courtyards.
function surfaceAt(x, y, mask) {
  if (courtyardFrac(x, y) > 0.5) return 'grass';                    // a courtyard lawn
  if (mask.block(x, y)) return 'tile';                              // the inner yards
  const p = pathSurfaceAt(x, y);
  if (p) return p;
  if (x < siteBB.minX + 10 && y > -74 && y < 128) return 'grass';   // the west lawn
  if (x > siteBB.maxX + 6 && y < siteBB.maxY - 4) return 'grass';   // the east flank lawn
  if (!mask.near(x, y)) return 'grass';
  return 'pave';
}

function buildTerrain(rings) {
  const mask = footprintMask(rings, 2.0, 2);
  buildPaths();
  const xs = terrainAxis(siteBB.minX - SITE_PAD, siteBB.maxX + SITE_PAD, 5.6);
  const ys = terrainAxis(siteBB.minY - SITE_PAD, siteBB.maxY + SITE_PAD, 5.6);
  const NX = xs.length, NY = ys.length;
  const z = new Float32Array(NX * NY);
  for (let j = 0; j < NY; j++) {
    for (let i = 0; i < NX; i++) z[j * NX + i] = groundZ(xs[i], ys[j]);
  }
  const buckets = { grass: [], pave: [], tile: [] };
  for (let j = 0; j + 1 < NY; j++) {
    for (let i = 0; i + 1 < NX; i++) {
      const x0 = xs[i], x1 = xs[i + 1], y0 = ys[j], y1 = ys[j + 1];
      const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2;
      // the building itself is a hole in the terrain, eroded a little so the
      // ground always tucks in under the wall rather than stopping short of it
      if (!inCellar(cx, cy) && mask.eroded(x0, y0) && mask.eroded(x1, y0) &&
          mask.eroded(x0, y1) && mask.eroded(x1, y1)) continue;
      const b = buckets[surfaceAt(cx, cy, mask)];
      const z00 = z[j * NX + i], z10 = z[j * NX + i + 1];
      const z01 = z[(j + 1) * NX + i], z11 = z[(j + 1) * NX + i + 1];
      b.push(x0, y0, z00, x1, y0, z10, x1, y1, z11,
             x0, y0, z00, x1, y1, z11, x0, y1, z01);
    }
  }
  const mats = { grass: groundMat, pave: paveMat, tile: tileMat };
  for (const k of ['grass', 'pave', 'tile']) {
    const arr = buckets[k];
    if (!arr.length) continue;
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(arr, 3));
    // planar UVs, so the texture scale is the same on every surface
    const uv = new Float32Array((arr.length / 3) * 2);
    for (let i = 0, k2 = 0; i < arr.length; i += 3) {
      uv[k2++] = arr[i]; uv[k2++] = arr[i + 1];
    }
    g.setAttribute('uv', new THREE.BufferAttribute(uv, 2));
    g.computeVertexNormals();
    g.computeBoundingSphere();
    const m = new THREE.Mesh(g, mats[k]);
    m.receiveShadow = true;
    m.name = 'terrain-' + k;
    worldGroup.add(m);
  }
  buildSiteRim(xs, ys, z, NX, NY);
  return mask;
}

// The cut edge of the site: a soil skirt hanging under the rim of the ground
// so the bounded piece of campus reads as a solid block rather than as a sheet
// that stops in mid-air.
function buildSiteRim(xs, ys, z, NX, NY) {
  const pos = [];
  const quad = (ax, ay, az, bx, by, bz) => {
    const a0 = az - RIM_DROP, b0 = bz - RIM_DROP;
    pos.push(ax, ay, az, bx, by, bz, bx, by, b0,
             ax, ay, az, bx, by, b0, ax, ay, a0);
  };
  const zAt = (i, j) => z[j * NX + i];
  for (let i = 0; i + 1 < NX; i++) {                       // south and north edges
    quad(xs[i + 1], ys[0], zAt(i + 1, 0), xs[i], ys[0], zAt(i, 0));
    quad(xs[i], ys[NY - 1], zAt(i, NY - 1), xs[i + 1], ys[NY - 1], zAt(i + 1, NY - 1));
  }
  for (let j = 0; j + 1 < NY; j++) {                       // west and east edges
    quad(xs[0], ys[j], zAt(0, j), xs[0], ys[j + 1], zAt(0, j + 1));
    quad(xs[NX - 1], ys[j + 1], zAt(NX - 1, j + 1), xs[NX - 1], ys[j], zAt(NX - 1, j));
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  g.computeVertexNormals();
  g.computeBoundingSphere();
  const m = new THREE.Mesh(g, soilMat);
  m.name = 'site-rim';
  worldGroup.add(m);
}

// Birch: a slim white trunk with the characteristic dark scars, and a light
// canopy built from crossed alpha billboards so it stays cheap.
function birchBarkTexture() {
  const c = texCanvas(128, 512);
  const x = c.getContext('2d');
  const g = x.createLinearGradient(0, 0, 128, 0);
  g.addColorStop(0, '#b9b4aa'); g.addColorStop(0.35, '#f2efe8');
  g.addColorStop(0.7, '#e6e1d6'); g.addColorStop(1, '#a9a49a');
  x.fillStyle = g; x.fillRect(0, 0, 128, 512);
  const rnd = mulberry(41);
  for (let i = 0; i < 130; i++) {
    x.fillStyle = `rgba(40,36,32,${0.28 + rnd() * 0.5})`;
    const w = 6 + rnd() * 40, h = 2 + rnd() * 5;
    x.fillRect(rnd() * 128, rnd() * 512, w, h);
  }
  for (let i = 0; i < 40; i++) {
    x.fillStyle = `rgba(120,110,96,${0.12 + rnd() * 0.2})`;
    x.fillRect(0, rnd() * 512, 128, 1 + rnd() * 2);
  }
  return finishTex(c, 1, 1, 4);
}
function leafTexture() {
  const N = 512;
  const c = texCanvas(N, N);
  const x = c.getContext('2d');
  x.clearRect(0, 0, N, N);
  const rnd = mulberry(97);
  // a soft, slightly ragged crown: dense in the middle, thinning at the edge,
  // built from small leaves rather than from big translucent discs
  for (let i = 0; i < 5200; i++) {
    const a = rnd() * Math.PI * 2;
    const r = Math.pow(rnd(), 0.42) * (N * 0.47);
    const px = N / 2 + Math.cos(a) * r;
    const py = N / 2 + Math.sin(a) * r * 0.94;
    const edge = r / (N * 0.47);
    if (rnd() < edge * edge * edge * 0.9) continue;   // ragged silhouette
    const s = 2.4 + rnd() * 4.4;
    const g = 108 + rnd() * 78;
    const sh = 0.62 + 0.38 * (1 - edge);
    x.fillStyle = `rgba(${Math.round(g * 0.52 * sh)},${Math.round(g * sh)},${Math.round(g * 0.34 * sh)},${0.72 + rnd() * 0.28})`;
    x.beginPath();
    x.ellipse(px, py, s, s * 0.7, a, 0, Math.PI * 2);
    x.fill();
  }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.anisotropy = 4;
  return t;
}
const barkTex = birchBarkTexture();
const leafTex = leafTexture();
const barkMat = new THREE.MeshStandardMaterial({ map: barkTex, roughness: 0.9 });
const leafMat = new THREE.MeshStandardMaterial({
  map: leafTex, transparent: true, alphaTest: 0.38, side: THREE.DoubleSide,
  roughness: 0.95, color: 0xa8c47e,
});

// Trees are collected as placements and planted at the end as five instanced
// meshes for the whole campus. One birch used to be five draw calls; a hundred
// and thirty of them were most of the frame's cost on an integrated GPU.
const treeSpots = [];
function plantBirch(x, y, h, rot) { treeSpots.push({ x, y, h, rot }); }

const TRUNK_GEO = new THREE.CylinderGeometry(0.010, 0.024, 0.92, 6, 1)
  .rotateX(Math.PI / 2).translate(0, 0, 0.46);
const LEAF_GEO = new THREE.PlaneGeometry(1, 1).rotateX(Math.PI / 2);

function plantAllTrees() {
  const n = treeSpots.length;
  if (!n) return;
  const mk = (geo, mat) => {
    const im = new THREE.InstancedMesh(geo, mat, n);
    im.castShadow = true;
    im.frustumCulled = false;
    worldGroup.add(im);
    return im;
  };
  const trunks = mk(TRUNK_GEO, barkMat);
  const crowns = [mk(LEAF_GEO, leafMat), mk(LEAF_GEO, leafMat),
                  mk(LEAF_GEO, leafMat), mk(LEAF_GEO, leafMat)];
  const m = new THREE.Matrix4(), q = new THREE.Quaternion();
  const up = new THREE.Vector3(0, 0, 1), pos = new THREE.Vector3(), sc = new THREE.Vector3();
  for (let i = 0; i < n; i++) {
    const t = treeSpots[i];
    const z = groundZ(t.x, t.y);
    q.setFromAxisAngle(up, t.rot);
    m.compose(pos.set(t.x, t.y, z), q, sc.set(t.h, t.h, t.h));
    trunks.setMatrixAt(i, m);
    const cw = t.h * 0.52, base = t.h * 0.52 + cw * 0.46;
    for (let k = 0; k < 3; k++) {
      q.setFromAxisAngle(up, t.rot + (k / 3) * Math.PI);
      m.compose(pos.set(t.x, t.y, z + base + (k - 1) * t.h * 0.02), q, sc.set(cw, 1, cw));
      crowns[k].setMatrixAt(i, m);
    }
    q.setFromAxisAngle(new THREE.Vector3(1, 0, 0), Math.PI / 2);
    m.compose(pos.set(t.x, t.y, z + t.h * 0.52 + cw * 0.88), q, sc.set(cw * 0.85, cw * 0.85, 1));
    crowns[3].setMatrixAt(i, m);
  }
  trunks.instanceMatrix.needsUpdate = true;
  crowns.forEach(c => { c.instanceMatrix.needsUpdate = true; });
}

// The birches are instanced and all one shape, which is right for a wood and
// wrong for the one tree that matters: the big rowan standing on the west plaza
// in photo 19, wider than it is tall and dark against the sheet metal. It is
// built on its own — a handful of meshes, planted by hand.
const broadBarkMat = new THREE.MeshStandardMaterial({ color: 0x6a5945, roughness: 0.96 });
const broadLeafMat = new THREE.MeshStandardMaterial({
  map: leafTex, transparent: true, alphaTest: 0.34, side: THREE.DoubleSide,
  roughness: 0.95, color: 0x88a95e,
});
function makeBroadleaf(h) {
  const g = new THREE.Group();
  const trunkH = h * 0.30;                          // short bole, crown low down
  const trunk = new THREE.Mesh(
    new THREE.CylinderGeometry(h * 0.020, h * 0.040, trunkH * 1.6, 8), broadBarkMat);
  trunk.rotation.x = Math.PI / 2;
  trunk.position.z = trunkH * 0.8;
  trunk.castShadow = true;
  g.add(trunk);
  const cw = h * 0.98;                              // a crown broader than tall
  for (let k = 0; k < 4; k++) {
    const p = new THREE.Mesh(LEAF_GEO, broadLeafMat);
    p.scale.set(cw, 1, cw * 0.80);
    p.rotation.z = (k / 4) * Math.PI;
    p.position.z = trunkH + cw * 0.30 + (k % 2) * cw * 0.04;
    p.castShadow = true;
    g.add(p);
  }
  const cap = new THREE.Mesh(LEAF_GEO, broadLeafMat);
  cap.rotation.x = Math.PI / 2;
  cap.scale.set(cw * 0.86, cw * 0.86, 1);
  cap.position.z = trunkH + cw * 0.52;
  g.add(cap);
  return g;
}

// Campus lamp: a black tapered pole with a white acorn globe, the fitting
// standing all round the site (photos 8, 16, 19).
const lampPoleMat = new THREE.MeshStandardMaterial({ color: 0x22262a, roughness: 0.5, metalness: 0.45 });
const lampHeadMat = new THREE.MeshStandardMaterial({
  color: 0xf6f2e6, emissive: 0xffeec4, emissiveIntensity: 0.35, roughness: 0.35,
});
function makeLamp(h) {
  const g = new THREE.Group();
  const pole = new THREE.Mesh(new THREE.CylinderGeometry(0.30, 0.52, h, 10), lampPoleMat);
  pole.rotation.x = Math.PI / 2; pole.position.z = h / 2;
  pole.castShadow = true;
  g.add(pole);
  const base = new THREE.Mesh(new THREE.CylinderGeometry(0.72, 0.95, 1.5, 10), lampPoleMat);
  base.rotation.x = Math.PI / 2; base.position.z = 0.75;
  g.add(base);
  const globe = new THREE.Mesh(new THREE.SphereGeometry(0.82, 12, 10), lampHeadMat);
  globe.position.z = h + 0.75;
  g.add(globe);
  const collar = new THREE.Mesh(new THREE.CylinderGeometry(0.52, 0.36, 0.6, 10), lampPoleMat);
  collar.rotation.x = Math.PI / 2; collar.position.z = h + 0.1;
  g.add(collar);
  return g;
}

const benchMat = new THREE.MeshStandardMaterial({ color: 0x8a6a44, roughness: 0.85 });
function makeBench() {
  const g = new THREE.Group();
  const seat = new THREE.Mesh(new THREE.BoxGeometry(9, 2.4, 0.5), benchMat);
  seat.position.z = 1.6; seat.castShadow = true;
  g.add(seat);
  for (const sx of [-3.4, 3.4]) {
    const leg = new THREE.Mesh(new THREE.BoxGeometry(0.5, 2.2, 1.6), lampPoleMat);
    leg.position.set(sx, 0, 0.8);
    g.add(leg);
  }
  return g;
}

// Low clipped hedges and shrub beds — they line the alley in photos 4, 12, 16
// and 21, and they are what stops the lawn from reading as a golf course.
const shrubMat = new THREE.MeshStandardMaterial({ color: 0x4f6b34, roughness: 0.97, flatShading: true });
const SHRUB_GEO = new THREE.IcosahedronGeometry(1, 1);
const shrubSpots = [];
function makeShrub(r, rnd) {
  return { r, sx: 1 + rnd() * 0.4, sy: 1 + rnd() * 0.4, sz: 0.42 + rnd() * 0.22,
           rot: rnd() * 3.1, position: { set(x, y, z) { this.x = x; this.y = y; this.z = z; } } };
}
function addShrub(s) {
  shrubSpots.push(s);
}
function plantAllShrubs() {
  if (!shrubSpots.length) return;
  const im = new THREE.InstancedMesh(SHRUB_GEO, shrubMat, shrubSpots.length);
  im.frustumCulled = false;
  const m = new THREE.Matrix4(), q = new THREE.Quaternion();
  const up = new THREE.Vector3(0, 0, 1), p = new THREE.Vector3(), sc = new THREE.Vector3();
  for (let i = 0; i < shrubSpots.length; i++) {
    const s = shrubSpots[i];
    q.setFromAxisAngle(up, s.rot);
    m.compose(p.set(s.position.x, s.position.y, s.position.z), q,
              sc.set(s.r * s.sx, s.r * s.sy, s.r * s.sz));
    im.setMatrixAt(i, m);
  }
  im.instanceMatrix.needsUpdate = true;
  worldGroup.add(im);
}

// Stone slabs set into the lawn, the path across the west slope in photo 8.
function slabPath(x0, y0, x1, y1, n, mat) {
  const g = new THREE.Group();
  for (let i = 0; i < n; i++) {
    const t = (i + 0.5) / n;
    const px = x0 + (x1 - x0) * t, py = y0 + (y1 - y0) * t;
    const s = new THREE.Mesh(new THREE.BoxGeometry(2.8, 2.4, 0.24), mat);
    s.position.set(px, py, groundZ(px, py) - 0.02);
    s.rotation.z = Math.atan2(y1 - y0, x1 - x0);
    g.add(s);
  }
  return g;
}

// Populate the campus once the footprint is known. Everything is placed on the
// graded terrain, so nothing floats above the slope or sinks into it.
let campusBuilt = false;
let siteMask = null;
function buildCampus(baseRings, entrance) {
  if (campusBuilt || !baseRings || !baseRings.length) return;
  campusBuilt = true;
  const outer = baseRings[0].pts;
  const rnd = mulberry(2026);

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const p of outer) {
    minX = Math.min(minX, p[0]); maxX = Math.max(maxX, p[0]);
    minY = Math.min(minY, p[1]); maxY = Math.max(maxY, p[1]);
  }
  siteBB = { minX, minY, maxX, maxY };
  siteMask = buildTerrain(baseRings);

  const clear = (x, y, m) => {
    if (siteMask.solid(x, y)) return false;
    for (const p of outer) if (Math.hypot(p[0] - x, p[1] - y) < m) return false;
    return true;
  };

  // Distance to the nearest level-0 wall EDGE (all rings: outer and
  // courtyards). clear() only measures to ring vertices, so on a long wall a
  // planting could sit a metre from the facade — or with its foliage through
  // it, sticking into a room (the shrub inside A109). Anything with a crown
  // has to keep its whole extent outside the walls.
  const distToWalls = (x, y) => {
    let d2 = Infinity;
    for (const r of baseRings) {
      const pts = r.pts;
      for (let i = 0; i < pts.length; i++) {
        const a = pts[i], b = pts[(i + 1) % pts.length];
        const vx = b[0] - a[0], vy = b[1] - a[1];
        const L2 = vx * vx + vy * vy || 1;
        let t = ((x - a[0]) * vx + (y - a[1]) * vy) / L2;
        t = t < 0 ? 0 : t > 1 ? 1 : t;
        const dx = x - (a[0] + vx * t), dy = y - (a[1] + vy * t);
        const dd = dx * dx + dy * dy;
        if (dd < d2) d2 = dd;
      }
    }
    return Math.sqrt(d2);
  };

  // Generous planting: A-huset sits in a wooded campus and every photograph of
  // it has birches in the frame. They are instanced, so the count is cheap.
  const inSite = (x, y) => x > minX - SITE_PAD + 8 && x < maxX + SITE_PAD - 8 &&
                           y > minY - SITE_PAD + 8 && y < maxY + SITE_PAD - 8;
  let trees = 0;
  for (let i = 0; i < 9000 && trees < 84; i++) {
    const x = minX - SITE_PAD + rnd() * (maxX - minX + SITE_PAD * 2);
    const y = minY - SITE_PAD + rnd() * (maxY - minY + SITE_PAD * 2);
    if (!inSite(x, y)) continue;
    if (!clear(x, y, 34 + rnd() * 30)) continue;
    if (distToWalls(x, y) < 7) continue;      // canopy stays off the facades
    if (surfaceAt(x, y, siteMask) !== 'grass') continue;
    if (entrance && Math.hypot(x - entrance.x - entrance.nx * 45,
                               y - entrance.y - entrance.ny * 45) < 52) continue;
    plantBirch(x, y, 17 + rnd() * 13, rnd() * Math.PI);
    trees++;
  }

  // A belt of birch along the boundary, thickening the edge of the site so it
  // closes the view instead of opening onto empty ground.
  for (let i = 0; i < 130; i++) {
    const a = (i / 130) * Math.PI * 2 + rnd() * 0.05;
    const rx = (maxX - minX) / 2 + SITE_PAD - 16 - rnd() * 34;
    const ry = (maxY - minY) / 2 + SITE_PAD - 16 - rnd() * 34;
    const x = (minX + maxX) / 2 + Math.cos(a) * rx;
    const y = (minY + maxY) / 2 + Math.sin(a) * ry;
    if (!inSite(x, y) || !clear(x, y, 20)) continue;
    if (surfaceAt(x, y, siteMask) !== 'grass') continue;
    plantBirch(x, y, 20 + rnd() * 14, rnd() * Math.PI);
  }

  // A row of small limes down the east alley, as in photos 4 and 12 — set well
  // back from the elevation so they frame it instead of hiding it.
  for (let i = 0; i < 9; i++) {
    const x = maxX + 48, y = maxY - 40 - i * 30;
    if (!clear(x, y, 22)) continue;
    plantBirch(x, y, 13 + rnd() * 3, rnd() * Math.PI);
  }

  let lamps = 0;
  for (let i = 0; i < 2600 && lamps < 10; i++) {
    const x = minX - 80 + rnd() * (maxX - minX + 160);
    const y = minY - 80 + rnd() * (maxY - minY + 160);
    if (!clear(x, y, 34)) continue;
    if (siteMask.solid(x, y)) continue;
    if (surfaceAt(x, y, siteMask) === 'grass') continue;
    const l = makeLamp(13);
    l.position.set(x, y, groundZ(x, y));
    worldGroup.add(l);
    lamps++;
  }

  let shrubs = 0;
  for (let i = 0; i < 12000 && shrubs < 150; i++) {
    const x = minX - 60 + rnd() * (maxX - minX + 120);
    const y = minY - 60 + rnd() * (maxY - minY + 120);
    if (!clear(x, y, 10)) continue;
    if (!siteMask.near(x, y)) continue;
    if (surfaceAt(x, y, siteMask) === 'grass' && rnd() > 0.35) continue;
    const s = makeShrub(1.0 + rnd() * 1.4, rnd);
    // The whole crown stays outside the walls, not just the trunk point.
    if (distToWalls(x, y) < s.r * Math.max(s.sx, s.sy) + 0.6) continue;
    s.position.set(x, y, groundZ(x, y) + 0.3);
    addShrub(s);
    shrubs++;
  }

  buildKerbs(outer, rnd);
  buildKindergarten(rnd);
  buildUniversitySign();

  // The rowan of photo 19, on the bank in front of the sheet-metal block just
  // north of the link, with a smaller one behind it.
  for (const [tx, ty, th, rot] of [[-207, 33, 31, 0.7], [-229, 66, 18, 2.2]]) {
    const t = makeBroadleaf(th);
    t.position.set(tx, ty, groundZ(tx, ty));
    t.rotation.z = rot;
    worldGroup.add(t);
  }

  const slabMat = new THREE.MeshStandardMaterial({ color: 0x8b8880, roughness: 0.95 });
  worldGroup.add(slabPath(minX - 54, 58, minX - 16, 2, 22, slabMat));

  if (entrance) {
    for (let i = 0; i < 3; i++) {
      const b = makeBench();
      const t = (i - 1) * 34;
      const bx = entrance.x + entrance.nx * 48 - entrance.ny * t;
      const by = entrance.y + entrance.ny * 48 + entrance.nx * t;
      b.position.set(bx, by, groundZ(bx, by));
      b.rotation.z = Math.atan2(entrance.ny, entrance.nx) + Math.PI / 2;
      worldGroup.add(b);
    }
  }
  plantAllTrees();
  plantAllShrubs();
  requestShadowUpdate();
}

// The low granite retaining edge that runs along the foot of the brick
// elevations, with its bed of shrubs behind (photos 12, 16, 21).
function buildKerbs(outer, rnd) {
  const kerbMat = new THREE.MeshStandardMaterial({ color: 0x8a5a48, roughness: 0.95 });
  const geo = new THREE.BoxGeometry(1, 2.3, 1.6);
  const spots = [];
  for (let i = 0; i < outer.length; i++) {
    const a = outer[i], b = outer[(i + 1) % outer.length];
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const len = Math.hypot(dx, dy);
    if (len < 26) continue;
    const nx = dy / len, ny = -dx / len;
    const mx = (a[0] + b[0]) / 2, my = (a[1] + b[1]) / 2;
    if (my > -30 && mx < 130) continue;              // sheet-metal block: no kerb
    if (groundZ(mx, my) > EXT_STOREY * 0.4) continue; // only where the ground is low
    const step = 5.6;
    for (let d = 6; d < len - 6; d += step) {
      const t = d / len;
      const px = a[0] + dx * t + nx * 8, py = a[1] + dy * t + ny * 8;
      if (siteMask.solid(px, py)) continue;
      spots.push([px, py, Math.atan2(dy, dx)]);
    }
  }
  if (!spots.length) return;
  const inst = new THREE.InstancedMesh(geo, kerbMat, spots.length);
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), up = new THREE.Vector3(0, 0, 1);
  for (let i = 0; i < spots.length; i++) {
    const [px, py, ang] = spots[i];
    q.setFromAxisAngle(up, ang);
    m.compose(new THREE.Vector3(px, py, groundZ(px, py) + 0.5), q,
              new THREE.Vector3(5.9, 1, 1));
    inst.setMatrixAt(i, m);
  }
  inst.instanceMatrix.needsUpdate = true;
  inst.castShadow = true; inst.receiveShadow = true;
  worldGroup.add(inst);
  // the bed behind the kerb
  for (const [px, py, ang] of spots) {
    if (rnd() > 0.55) continue;
    const bx = px - Math.sin(ang) * -2.6, by = py + Math.cos(ang) * -2.6;
    const sh = makeShrub(1.6 + rnd() * 1.4, rnd);
    sh.position.set(bx, by, groundZ(bx, by) + 1.1);
    addShrub(sh);
  }
}

// ── Kideus förskola and its playground ───────────────────────────────────
// The preschool occupies the south-west end of the building: a two-storey
// glazed room stepping out of the brick wing under a pergola on red steel
// posts (photos 11 and 24), with the fenced play yard beside it and a second,
// larger yard in the corner behind (photos 23 and 28).
const kgGlassMat = new THREE.MeshStandardMaterial({
  color: 0x9fb2b8, roughness: 0.12, metalness: 0.2,
  transparent: true, opacity: 0.72, side: THREE.DoubleSide,
});
const kgFrameMat = new THREE.MeshStandardMaterial({ color: 0xb9bdbc, roughness: 0.42, metalness: 0.55 });
const kgPostMat  = new THREE.MeshStandardMaterial({ color: 0x9c5b52, roughness: 0.62, metalness: 0.15 });
const kgSignMat  = new THREE.MeshStandardMaterial({ color: 0xe8efe6, roughness: 0.8 });
const woodMat    = new THREE.MeshStandardMaterial({ color: 0x87664a, roughness: 0.94 });
const playRedMat = new THREE.MeshStandardMaterial({ color: 0xa8342c, roughness: 0.8 });
const playBlueMat = new THREE.MeshStandardMaterial({ color: 0x2f5f8f, roughness: 0.8 });
const playYellowMat = new THREE.MeshStandardMaterial({ color: 0xc8992e, roughness: 0.8 });
const fenceMat   = new THREE.MeshStandardMaterial({ color: 0x6d5a44, roughness: 0.95 });

function buildKindergarten(rnd) {
  const g = new THREE.Group();
  g.name = 'kindergarten';
  // the glazed room, stepping out of the south face of the south-west wing
  const X0 = -172, X1 = -132, Y0 = -178, Y1 = -160;
  const cx = (X0 + X1) / 2, cy = (Y0 + Y1) / 2;
  const gz = groundZ(cx, cy);
  const W = X1 - X0, D = Y1 - Y0, H = EXT_STOREY * 1.9;
  const glass = new THREE.Mesh(new THREE.BoxGeometry(W, D, H - 2.4), kgGlassMat);
  glass.position.set(cx, cy, gz + (H - 2.4) / 2);
  g.add(glass);
  for (const z of [gz + 0.4, gz + EXT_STOREY * 0.92, gz + H - 1.6]) {
    const band = new THREE.Mesh(new THREE.BoxGeometry(W + 0.8, D + 0.8, 1.5), kgFrameMat);
    band.position.set(cx, cy, z);
    band.castShadow = true;
    g.add(band);
  }
  // the printed banner over the ground floor
  const sign = new THREE.Mesh(new THREE.BoxGeometry(W * 0.86, 0.5, 3.2), kgSignMat);
  sign.position.set(cx, Y0 - 0.4, gz + EXT_STOREY * 0.72);
  g.add(sign);
  const roof = new THREE.Mesh(new THREE.BoxGeometry(W + 1.6, D + 1.6, 1.0), kgFrameMat);
  roof.position.set(cx, cy, gz + H);
  roof.castShadow = true;
  g.add(roof);

  // the pergola: red posts carrying a light slatted canopy over the entrance
  const PY = Y0 - 11;
  const canopy = new THREE.Mesh(new THREE.BoxGeometry(W + 10, 12, 0.6), kgFrameMat);
  canopy.position.set(cx, PY + 1.5, gz + EXT_STOREY * 1.45);
  canopy.castShadow = true;
  g.add(canopy);
  for (const sx of [-1, -0.33, 0.33, 1]) {
    const post = new THREE.Mesh(new THREE.CylinderGeometry(0.55, 0.55, EXT_STOREY * 1.45, 8), kgPostMat);
    post.rotation.x = Math.PI / 2;
    post.position.set(cx + sx * (W / 2 + 3.4), PY - 3.2, gz + EXT_STOREY * 0.72);
    post.castShadow = true;
    g.add(post);
  }
  worldGroup.add(g);

  // the two play yards
  buildPlayground(-186, -206, -118, -182, rnd);        // in front of the preschool
  buildPlayground(-136, -128, -96, -54, rnd);          // the corner yard, photo 23
}

// A fenced play yard: wooden fence, a climbing frame with a red roof, a shed,
// a sandpit and benches. Everything is a box or a cylinder — it only has to
// read as a preschool yard from across the lawn.
function buildPlayground(x0, y0, x1, y1, rnd) {
  const g = new THREE.Group();
  g.name = 'playground';
  const post = (x, y, h, mat) => {
    const p = new THREE.Mesh(new THREE.BoxGeometry(0.5, 0.5, h), mat || fenceMat);
    p.position.set(x, y, groundZ(x, y) + h / 2);
    g.add(p);
    return p;
  };
  // fence: rails and pales round the perimeter
  const run = (ax, ay, bx, by) => {
    const len = Math.hypot(bx - ax, by - ay);
    const n = Math.max(2, Math.round(len / 1.7));
    for (let i = 0; i <= n; i++) {
      const t = i / n;
      post(ax + (bx - ax) * t, ay + (by - ay) * t, 4.2);
    }
    const mx = (ax + bx) / 2, my = (ay + by) / 2;
    for (const z of [2.2, 3.7]) {
      const rail = new THREE.Mesh(new THREE.BoxGeometry(len, 0.4, 0.5), fenceMat);
      rail.position.set(mx, my, groundZ(mx, my) + z);
      rail.rotation.z = Math.atan2(by - ay, bx - ax);
      g.add(rail);
    }
  };
  run(x0, y0, x1, y0); run(x1, y0, x1, y1); run(x1, y1, x0, y1); run(x0, y1, x0, y0);

  const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2, gz = groundZ(cx, cy);
  // climbing frame: a blue and yellow box under a red pitched roof
  const body = new THREE.Mesh(new THREE.BoxGeometry(9, 7, 7), playBlueMat);
  body.position.set(cx, cy, gz + 3.5); body.castShadow = true; g.add(body);
  const panel = new THREE.Mesh(new THREE.BoxGeometry(9.4, 0.5, 4.4), playYellowMat);
  panel.position.set(cx, cy - 3.6, gz + 4.4); g.add(panel);
  const roofA = new THREE.Mesh(new THREE.ConeGeometry(7.4, 5.2, 4), playRedMat);
  roofA.rotation.x = Math.PI / 2; roofA.rotation.z = Math.PI / 4;
  roofA.position.set(cx, cy, gz + 9.4); roofA.castShadow = true; g.add(roofA);
  const slide = new THREE.Mesh(new THREE.BoxGeometry(3.2, 9, 0.5), playYellowMat);
  slide.position.set(cx + 6, cy + 4, gz + 3.4);
  slide.rotation.x = -0.5; g.add(slide);
  // the little wooden shed of photo 23
  const shed = new THREE.Mesh(new THREE.BoxGeometry(11, 8, 6), woodMat);
  shed.position.set(x1 - 9, y0 + 7, gz + 3); shed.castShadow = true; g.add(shed);
  const shedRoof = new THREE.Mesh(new THREE.BoxGeometry(13, 10, 0.7), lampPoleMat);
  shedRoof.position.set(x1 - 9, y0 + 7, gz + 6.5); g.add(shedRoof);
  // sandpit and a couple of tables
  const sand = new THREE.Mesh(new THREE.BoxGeometry(11, 9, 0.6),
    new THREE.MeshStandardMaterial({ color: 0xcbb98d, roughness: 0.99 }));
  sand.position.set(x0 + 9, cy + 6, gz + 0.3); g.add(sand);
  for (let i = 0; i < 3; i++) {
    const b = makeBench();
    b.scale.set(0.55, 0.55, 0.55);
    const bx = x0 + 7 + i * 9, by = y1 - 6;
    b.position.set(bx, by, groundZ(bx, by));
    g.add(b);
  }
  worldGroup.add(g);
}

// The Luleå tekniska universitet monolith: a brushed steel slab on a concrete
// plinth, standing on the lawn at the edge of the east plaza (photo 9).
function buildUniversitySign() {
  const g = new THREE.Group();
  g.name = 'university-sign';
  // Out on the grass beside the east walk, where the owner's arrow puts it and
  // where the photograph of it was taken — a stone-footed steel monument, not
  // the blue pylon that used to stand against the doors.
  // The group carries the position, so its parts are built about their own
  // origin: turning a group whose children hold world coordinates would swing
  // the whole monument round the middle of the site instead of about itself.
  const x = 214, y = 4;
  g.position.set(x, y, groundZ(x, y));
  // Turned a quarter round from where it stood: the east walk runs north–south
  // past it, so broadside to the walk put the monument edge-on to everyone
  // using it. Square to the walk it reads from either end, which is the owner's
  // "rotate 90 degree".
  g.rotation.z = 0;
  const base = new THREE.Mesh(new THREE.BoxGeometry(11, 5.2, 1.8),
    new THREE.MeshStandardMaterial({ color: 0xada79c, roughness: 0.9 }));
  base.position.set(0, 0, 0.9);
  base.castShadow = true;
  g.add(base);
  const slab = new THREE.Mesh(new THREE.BoxGeometry(9.6, 1.5, 12),
    new THREE.MeshStandardMaterial({ color: 0xb9bbb7, roughness: 0.32, metalness: 0.72 }));
  slab.position.set(0, 0, 7.6);
  slab.castShadow = true;
  g.add(slab);
  // The mark on both faces. Each one is its own mesh laid in the slab's own
  // plane and turned to look outwards, so the artwork reads the right way up
  // and the right way round on whichever side you walk past — a single
  // double-sided plane would show one of them mirrored.
  const markMat = noTone(new THREE.MeshBasicMaterial({
    map: universityMarkTexture(), transparent: true,
  }));
  for (const s of [-1, 1]) {
    const mark = new THREE.Mesh(new THREE.PlaneGeometry(6.4, 5.0), markMat);
    mark.rotation.x = Math.PI / 2;
    if (s > 0) mark.rotation.y = Math.PI;
    mark.position.set(0, s * 0.80, 8.4);
    g.add(mark);
  }
  worldGroup.add(g);
}

function universityMarkTexture() {
  const c = texCanvas(256, 200);
  const x = c.getContext('2d');
  x.clearRect(0, 0, 256, 200);
  x.fillStyle = '#1b3f8f';
  x.font = 'bold 150px Georgia, serif';
  x.textAlign = 'center';
  x.fillText('L', 168, 132);
  x.font = '22px Georgia, serif';
  x.fillText('LULEÅ', 92, 78);
  x.fillText('TEKNISKA', 92, 104);
  x.fillText('UNIVERSITET', 92, 130);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

function setWorldVisible(v) {
  worldGroup.visible = v;
  skyDome.visible = v;
}
