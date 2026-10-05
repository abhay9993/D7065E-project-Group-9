// interior.js — giving the floor plates thickness.
//
// The plan data is flat: polygons and line segments. This turns each floor
// into a plate you can read as a physical thing — a slab with an edge you can
// see, walls with real height, corridors that glow differently from rooms —
// without touching the room meshes themselves, which stay exactly as they were
// so highlight colours and opacities keep their meaning.

const WALL_H = 6.4;            // interior wall height (~2.9 m)
const SLAB_TOP = -0.05;        // just under the plan linework
const SLAB_BOTTOM = -2.0;

const footprintCache = {};
function footprintOf(level) {
  if (footprintCache[level]) return footprintCache[level];
  const r = extractFootprint(level, { cell: 1.0, close: 3, eps: 1.7, minArea: 120 });
  footprintCache[level] = r;
  return r;
}

// Partitions, in the two colours the building actually has. The near-white
// they were before came back from the sun as bare plasterboard: bright enough
// to pull the eye off the plan and to flatten every room into the same glare.
// A muted blue-grey sits inside the dark theme and still reads as a built
// surface; the corridors keep their warmer, sandier wall so circulation is
// legible at a glance without a second overlay.
const wallMat = new THREE.MeshLambertMaterial({
  color: 0x8d97a8, side: THREE.DoubleSide,
});
const corridorWallMat = new THREE.MeshLambertMaterial({
  color: 0xb3a891, side: THREE.DoubleSide,
});
const slabTopMat = new THREE.MeshLambertMaterial({ color: 0x49505e });
const slabEdgeMat = new THREE.MeshLambertMaterial({ color: 0x232a38, side: THREE.DoubleSide });
// Only pulled over the floor you are inside, for route flights.
const ceilingMat = new THREE.MeshLambertMaterial({
  color: 0xc8ccd4, side: THREE.DoubleSide, emissive: 0x2a3140, emissiveIntensity: 0.6,
});
const ceilings = {};
const wallCapMat = noTone(new THREE.LineBasicMaterial({
  color: 0xbfd4ee, transparent: true, opacity: 0.32,
}));

const interiorExtras = [];     // groups toggled with the Walls checkbox

// A merged wall band for a whole floor: one draw call instead of thousands.
function wallBandGeometry(segments, pw, ph, z0, z1) {
  const n = segments.length;
  const pos = new Float32Array(n * 18);
  const nrm = new Float32Array(n * 18);
  let k = 0, kn = 0;
  for (const seg of segments) {
    const [x1, y1] = pdfToWorld(seg[0][0], seg[0][1], pw, ph);
    const [x2, y2] = pdfToWorld(seg[1][0], seg[1][1], pw, ph);
    const dx = x2 - x1, dy = y2 - y1;
    const len = Math.hypot(dx, dy);
    if (len < 1e-5) continue;
    const nx = dy / len, ny = -dx / len;
    const quad = [
      x1, y1, z0, x2, y2, z0, x2, y2, z1,
      x1, y1, z0, x2, y2, z1, x1, y1, z1,
    ];
    for (let i = 0; i < 18; i++) pos[k++] = quad[i];
    for (let i = 0; i < 6; i++) { nrm[kn++] = nx; nrm[kn++] = ny; nrm[kn++] = 0; }
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.BufferAttribute(pos.subarray(0, k), 3));
  g.setAttribute('normal', new THREE.BufferAttribute(nrm.subarray(0, kn), 3));
  g.computeBoundingSphere();
  return g;
}

function wallCapGeometry(segments, pw, ph, z) {
  const pos = new Float32Array(segments.length * 6);
  let k = 0;
  for (const seg of segments) {
    const [x1, y1] = pdfToWorld(seg[0][0], seg[0][1], pw, ph);
    const [x2, y2] = pdfToWorld(seg[1][0], seg[1][1], pw, ph);
    pos[k++] = x1; pos[k++] = y1; pos[k++] = z;
    pos[k++] = x2; pos[k++] = y2; pos[k++] = z;
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.BufferAttribute(pos.subarray(0, k), 3));
  return g;
}

function buildInteriorDetail() {
  LEVELS.forEach((level, li) => {
    const fg = floorGroups[level];
    const data = floorData[level];
    if (!fg || !data || fg.detailBuilt) return;
    fg.detailBuilt = true;
    const pw = data.page.width, ph = data.page.height;

    // ── slab ──
    const slabGroup = new THREE.Group();
    const rings = footprintOf(level);
    if (rings.length) {
      const shape = shapeFromRings(rings);
      if (shape) {
        const top = new THREE.Mesh(new THREE.ShapeGeometry(shape, 6), slabTopMat);
        top.position.z = SLAB_TOP;
        slabGroup.add(top);
        const under = new THREE.Mesh(new THREE.ShapeGeometry(shape, 6), slabEdgeMat);
        under.position.z = SLAB_BOTTOM;
        slabGroup.add(under);
        const ceil = new THREE.Mesh(new THREE.ShapeGeometry(shape, 6), ceilingMat);
        ceil.position.z = WALL_H + 0.5;
        ceil.visible = false;
        fg.container.add(ceil);
        ceilings[level] = ceil;
      }
      for (const r of rings) {
        const edge = new THREE.Mesh(
          ringWallGeometry(r.pts, SLAB_BOTTOM, SLAB_TOP, 0.05, false), slabEdgeMat);
        slabGroup.add(edge);
      }
    }
    fg.container.add(slabGroup);
    fg.slabGroup = slabGroup;

    // ── walls ──
    // Wall segments that border a corridor are lit warmer, so circulation
    // reads differently from the rooms it serves. Classifying every segment
    // exactly would be expensive; sampling the nearest room centre is enough
    // to give the corridors their own light.
    const corridorPts = [];
    for (const r of data.rooms) {
      if (r.type === 'corridor') {
        const [cx, cy] = pdfToWorld(r.center[0], r.center[1], pw, ph);
        corridorPts.push([cx, cy, Math.sqrt(Math.max(r.area, 4)) * 0.75 + 5]);
      }
    }
    const walls = data.walls || [];
    const nearCorridor = [], plain = [];
    for (const seg of walls) {
      const mx = (seg[0][0] + seg[1][0]) / 2, my = (seg[0][1] + seg[1][1]) / 2;
      const [wx, wy] = pdfToWorld(mx, my, pw, ph);
      let hit = false;
      for (const c of corridorPts) {
        const dx = c[0] - wx, dy = c[1] - wy;
        if (dx * dx + dy * dy < c[2] * c[2]) { hit = true; break; }
      }
      (hit ? nearCorridor : plain).push(seg);
    }

    const extrude = new THREE.Group();
    if (plain.length) {
      const m = new THREE.Mesh(wallBandGeometry(plain, pw, ph, 0.05, WALL_H), wallMat);
      extrude.add(m);
    }
    if (nearCorridor.length) {
      const m = new THREE.Mesh(wallBandGeometry(nearCorridor, pw, ph, 0.05, WALL_H * 1.02), corridorWallMat);
      extrude.add(m);
    }
    // a cool line along the top of every wall — reads as the glazed strip
    // above the partitions
    if (walls.length) {
      extrude.add(new THREE.LineSegments(wallCapGeometry(walls, pw, ph, WALL_H + 0.05), wallCapMat));
    }
    fg.container.add(extrude);
    fg.extrudeGroup = extrude;
    interiorExtras.push(extrude);
  });

  const chk = document.getElementById('showWalls');
  if (chk) {
    const apply = () => interiorExtras.forEach(g => { g.visible = chk.checked; });
    chk.addEventListener('change', () => { apply(); render(); });
    apply();
  }
  render();
}

// ── labels ───────────────────────────────────────────────────────────────
// Crisp, high-DPI room labels that fade out with distance so a whole floor of
// them never turns into noise.
const labelSprites = [];

function makeLabelSprite(text, wx, wy, z) {
  const pad = 8;
  const c = texCanvas(256, 64);
  const x = c.getContext('2d');
  x.font = '600 30px "Segoe UI", Helvetica, Arial, sans-serif';
  x.textAlign = 'center';
  x.textBaseline = 'middle';
  const w = Math.min(240, x.measureText(text).width + pad * 2);
  x.fillStyle = 'rgba(10,16,30,0.62)';
  const rx = 128 - w / 2;
  x.beginPath();
  if (x.roundRect) x.roundRect(rx, 16, w, 32, 7); else x.rect(rx, 16, w, 32);
  x.fill();
  x.strokeStyle = 'rgba(140,180,235,0.35)';
  x.lineWidth = 1.5;
  x.stroke();
  x.fillStyle = '#dceaff';
  x.fillText(text, 128, 33);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.minFilter = THREE.LinearFilter;
  t.generateMipmaps = false;
  const sprite = new THREE.Sprite(noTone(new THREE.SpriteMaterial({
    map: t, transparent: true, depthWrite: false, depthTest: false,
  })));
  sprite.position.set(wx, wy, z);
  sprite.scale.set(14, 3.5, 1);
  sprite.renderOrder = 900;
  labelSprites.push(sprite);
  return sprite;
}

function updateLabelFade() {
  if (!labelSprites.length) return;
  const cam = activeCamera.position;
  for (const s of labelSprites) {
    const p = s.parent;
    if (!p || !p.visible) continue;
    s.getWorldPosition(_lblTmp);
    const d = _lblTmp.distanceTo(cam);
    const a = is3DView
      ? clamp01((260 - d) / 130) * clamp01((d - 14) / 20 + 0.2)
      : 1;
    s.material.opacity = a;
    s.visible = a > 0.02;
  }
}
const _lblTmp = new THREE.Vector3();

// Indoor lighting: a soft lamp carried by the camera plus the ceiling plane,
// switched on only when the camera is actually inside a floor. Without it a
// first-person view is a black box — the sun only ever reaches the wall tops.
const headLamp = new THREE.PointLight(0xffeedd, 0, 90, 1.6);
scene.add(headLamp);

function setIndoorLighting(on, level) {
  headLamp.intensity = on ? 42 : 0;
  for (const l of LEVELS) {
    const c = ceilings[l];
    if (c) c.visible = !!(on && (!level || l === level));
  }
  ambLight.intensity = on ? 0.85 : 0.42;
  hemiLight.intensity = on ? 1.6 : 1.25;
}

function moveHeadLamp() {
  if (headLamp.intensity <= 0) return;
  headLamp.position.copy(camera3D.position);
  headLamp.position.z += 1.2;
}
