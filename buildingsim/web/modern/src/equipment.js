const STATUS_COLORS = { running: 0x00e676, warning: 0xffab00, stopped: 0xff1744 };
const STATUS_DARK   = { running: 0x00994d, warning: 0xaa7200, stopped: 0xaa1030 };
const STATUS_HEX    = { running: '#00e676', warning: '#ffab00', stopped: '#ff1744' };

// === SVG Icon Loader ===
const iconCache = {}; // type -> Promise<HTMLCanvasElement>
const ICON_RES = 128; // icon texture resolution

function loadEquipmentIcon(type, statusColor, noRecolor) {
  const cacheKey = type + ':' + (noRecolor ? 'raw' : statusColor);
  if (iconCache[cacheKey]) return iconCache[cacheKey];

  const promise = fetch(`/api/icons/${type}.svg`)
    .then(r => {
      if (!r.ok) return fetch('/api/icons/generic.svg');
      return r;
    })
    .then(r => r.text())
    .then(svgText => {
      const finalSvg = noRecolor ? svgText :
        svgText.replace(/#ffffff/gi, statusColor).replace(/white/gi, statusColor);
      const canvas = document.createElement('canvas');
      canvas.width = ICON_RES;
      canvas.height = ICON_RES;
      const ctx = canvas.getContext('2d');
      const img = new Image();
      return new Promise(resolve => {
        img.onload = () => {
          ctx.drawImage(img, 0, 0, ICON_RES, ICON_RES);
          resolve(canvas);
        };
        img.onerror = () => resolve(canvas); // empty canvas fallback
        img.src = 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(finalSvg)));
      });
    });

  iconCache[cacheKey] = promise;
  return promise;
}

function createEquipmentSprite(x, y, z, size, canvas) {
  const texture = new THREE.CanvasTexture(canvas);
  texture.minFilter = THREE.LinearFilter;
  const mat = new THREE.SpriteMaterial({ map: texture, transparent: true, depthWrite: false, alphaTest: 0.01 });
  const sprite = new THREE.Sprite(mat);
  sprite.position.set(x, y, z);
  sprite.scale.set(size, size, 1);
  return sprite;
}

// ── occupant count badges ────────────────────────────────────────────────
// The number of people a room holds, taken from the /api/occupancy document
// the viewer already has: fifty occupants in a lecture hall are fifty icons in
// a ring, which reads as "a crowd" but not as "fifty". Purely a rendering
// addition; nothing about the API changes.
//
// Textures are cached per number. The equipment group is rebuilt on every
// occupancy update and clearGroup disposes the materials it finds, but a
// disposed material does not dispose the texture it points at, so the cache
// stays valid and one texture serves every room showing the same number.
const occupantBadgeTextures = new Map();

function occupantBadgeTexture(count) {
  const text = String(count);
  const cached = occupantBadgeTextures.get(text);
  if (cached) return cached;
  const c = texCanvas(128, 64);
  const x = c.getContext('2d');
  x.font = 'bold 34px "Segoe UI", Helvetica, Arial, sans-serif';
  x.textAlign = 'center';
  x.textBaseline = 'middle';
  const w = Math.max(46, Math.min(120, x.measureText(text).width + 26));
  x.fillStyle = 'rgba(8,14,26,0.72)';
  const rx = 64 - w / 2;
  x.beginPath();
  if (x.roundRect) x.roundRect(rx, 14, w, 36, 10); else x.rect(rx, 14, w, 36);
  x.fill();
  x.strokeStyle = 'rgba(0,204,255,0.55)';
  x.lineWidth = 2;
  x.stroke();
  x.fillStyle = '#e8f6ff';
  x.fillText(text, 64, 33);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.minFilter = THREE.LinearFilter;
  t.generateMipmaps = false;
  occupantBadgeTextures.set(text, t);
  return t;
}

function createOccupantCountBadge(wx, wy, count) {
  const sprite = new THREE.Sprite(noTone(new THREE.SpriteMaterial({
    map: occupantBadgeTexture(count), transparent: true, depthWrite: false, depthTest: false,
  })));
  sprite.position.set(wx, wy, 12);
  sprite.scale.set(7, 3.5, 1);
  sprite.renderOrder = 950;
  return sprite;
}

// Keep old function signature for backward compat but it's no longer used
function createCompressorSymbol(x, y, z, size, statusColor, status) {
  const group = new THREE.Group();
  const s = size;
  const darkColor = STATUS_DARK[status] || 0x006633;
  const metalColor = 0x556677;
  const metalDark = 0x3a4a55;
  const highlight = 0x88aacc;

  // === Base plate ===
  const baseMat = new THREE.MeshBasicMaterial({ color: metalDark, side: THREE.DoubleSide });
  const base = new THREE.Mesh(new THREE.PlaneGeometry(s * 1.6, s * 0.9), baseMat);
  base.position.set(0, 0, 0);
  group.add(base);
  // Base border
  const baseBorder = new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(-s*0.8, -s*0.45, 0.05), new THREE.Vector3(s*0.8, -s*0.45, 0.05),
      new THREE.Vector3(s*0.8, s*0.45, 0.05), new THREE.Vector3(-s*0.8, s*0.45, 0.05),
      new THREE.Vector3(-s*0.8, -s*0.45, 0.05)]),
    new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.5 }));
  group.add(baseBorder);

  // === Main cylinder tank (horizontal) ===
  const tankRadius = s * 0.32;
  const tankLen = s * 0.9;
  // Tank body - rounded rect approximation
  const tankShape = new THREE.Shape();
  tankShape.moveTo(-tankLen/2, -tankRadius);
  tankShape.lineTo(tankLen/2, -tankRadius);
  tankShape.quadraticCurveTo(tankLen/2 + tankRadius*0.5, -tankRadius, tankLen/2 + tankRadius*0.5, 0);
  tankShape.quadraticCurveTo(tankLen/2 + tankRadius*0.5, tankRadius, tankLen/2, tankRadius);
  tankShape.lineTo(-tankLen/2, tankRadius);
  tankShape.quadraticCurveTo(-tankLen/2 - tankRadius*0.5, tankRadius, -tankLen/2 - tankRadius*0.5, 0);
  tankShape.quadraticCurveTo(-tankLen/2 - tankRadius*0.5, -tankRadius, -tankLen/2, -tankRadius);
  const tankMesh = new THREE.Mesh(new THREE.ShapeGeometry(tankShape),
    new THREE.MeshBasicMaterial({ color: metalColor, transparent: true, opacity: 0.9, side: THREE.DoubleSide }));
  tankMesh.position.set(0, s*0.05, 0.1);
  group.add(tankMesh);
  // Tank highlight stripe
  const stripe = new THREE.Mesh(new THREE.PlaneGeometry(tankLen * 0.85, tankRadius * 0.2),
    new THREE.MeshBasicMaterial({ color: highlight, transparent: true, opacity: 0.25, side: THREE.DoubleSide }));
  stripe.position.set(0, s*0.05 + tankRadius*0.3, 0.15);
  group.add(stripe);
  // Tank outline
  const tankOutlinePts = [];
  for (let i = 0; i <= 48; i++) {
    const t = i / 48;
    const angle = t * Math.PI * 2;
    // Ellipse for tank
    tankOutlinePts.push(new THREE.Vector3(
      Math.cos(angle) * (tankLen/2 + tankRadius*0.5),
      s*0.05 + Math.sin(angle) * tankRadius, 0.2));
  }
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(tankOutlinePts),
    new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.6 })));

  // === Motor housing (left side) ===
  const motorR = s * 0.22;
  const motorPts = [];
  for (let i = 0; i <= 24; i++) {
    const a = (i / 24) * Math.PI * 2;
    motorPts.push(new THREE.Vector3(
      -s*0.45 + Math.cos(a) * motorR,
      s*0.05 + Math.sin(a) * motorR, 0.25));
  }
  const motorFill = new THREE.Mesh(new THREE.CircleGeometry(motorR, 24),
    new THREE.MeshBasicMaterial({ color: metalDark, side: THREE.DoubleSide }));
  motorFill.position.set(-s*0.45, s*0.05, 0.22);
  group.add(motorFill);
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(motorPts),
    new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.7 })));
  // Motor fan lines
  for (let i = 0; i < 6; i++) {
    const a = (i / 6) * Math.PI * 2;
    const fan = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(-s*0.45, s*0.05, 0.3),
      new THREE.Vector3(-s*0.45 + Math.cos(a)*motorR*0.85, s*0.05 + Math.sin(a)*motorR*0.85, 0.3)]);
    group.add(new THREE.Line(fan, new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.35 })));
  }

  // === Cooling fins (on top of tank) ===
  for (let i = 0; i < 5; i++) {
    const fx = -s*0.15 + i * s*0.12;
    const fin = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(fx, s*0.05 + tankRadius*0.7, 0.2),
      new THREE.Vector3(fx, s*0.05 + tankRadius + s*0.1, 0.2)]);
    group.add(new THREE.Line(fin, new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.4 })));
  }

  // === Outlet pipe (right, going up) ===
  const pipeW = s * 0.06;
  const pipePts = [
    new THREE.Vector3(s*0.55, s*0.05, 0.3),
    new THREE.Vector3(s*0.72, s*0.05, 0.3),
    new THREE.Vector3(s*0.72, s*0.35, 0.3)];
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(pipePts),
    new THREE.LineBasicMaterial({ color: 0x88bbdd })));
  // Parallel pipe line
  const pipePts2 = pipePts.map(p => new THREE.Vector3(p.x + pipeW, p.y, p.z));
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(pipePts2),
    new THREE.LineBasicMaterial({ color: 0x88bbdd })));
  // Pressure arrow
  const arrowPts = [
    new THREE.Vector3(s*0.69, s*0.28, 0.35),
    new THREE.Vector3(s*0.72 + pipeW/2, s*0.38, 0.35),
    new THREE.Vector3(s*0.75 + pipeW, s*0.28, 0.35)];
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(arrowPts),
    new THREE.LineBasicMaterial({ color: statusColor })));

  // === Inlet pipe (left bottom) ===
  const inPts = [
    new THREE.Vector3(-s*0.72, -s*0.15, 0.3),
    new THREE.Vector3(-s*0.55, -s*0.15, 0.3)];
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(inPts),
    new THREE.LineBasicMaterial({ color: 0x6699aa })));
  const inPts2 = inPts.map(p => new THREE.Vector3(p.x, p.y - pipeW, p.z));
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(inPts2),
    new THREE.LineBasicMaterial({ color: 0x6699aa })));

  // === Status LED indicator (glowing dot) ===
  const ledGeo = new THREE.CircleGeometry(s * 0.06, 16);
  const ledMat = new THREE.MeshBasicMaterial({ color: statusColor, side: THREE.DoubleSide });
  const led = new THREE.Mesh(ledGeo, ledMat);
  led.position.set(s*0.35, s*0.05 - tankRadius*0.5, 0.3);
  group.add(led);
  // LED glow
  const glowGeo = new THREE.CircleGeometry(s * 0.12, 16);
  const glowMat = new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide });
  const glow = new THREE.Mesh(glowGeo, glowMat);
  glow.position.set(s*0.35, s*0.05 - tankRadius*0.5, 0.28);
  glow.userData.isLedGlow = true;
  group.add(glow);

  // === Pressure gauge (small circle, right side of tank) ===
  const gaugeR = s * 0.08;
  const gaugePts = [];
  for (let i = 0; i <= 16; i++) {
    const a = (i / 16) * Math.PI * 2;
    gaugePts.push(new THREE.Vector3(s*0.3 + Math.cos(a)*gaugeR, s*0.05 + tankRadius*0.5 + Math.sin(a)*gaugeR, 0.3));
  }
  group.add(new THREE.Mesh(new THREE.CircleGeometry(gaugeR, 16),
    new THREE.MeshBasicMaterial({ color: 0x222233, side: THREE.DoubleSide }))
    .translateX(s*0.3).translateY(s*0.05 + tankRadius*0.5).translateZ(0.28));
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(gaugePts),
    new THREE.LineBasicMaterial({ color: 0xaabbcc })));
  // Gauge needle
  const needleAngle = status === 'running' ? -0.3 : status === 'warning' ? 0.5 : 1.2;
  const needle = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(s*0.3, s*0.05 + tankRadius*0.5, 0.35),
    new THREE.Vector3(s*0.3 + Math.cos(needleAngle)*gaugeR*0.8, s*0.05 + tankRadius*0.5 + Math.sin(needleAngle)*gaugeR*0.8, 0.35)]);
  group.add(new THREE.Line(needle, new THREE.LineBasicMaterial({ color: 0xff4444 })));

  // === Status pulse ring (outer glow) ===
  const pulseGeo = new THREE.RingGeometry(s * 0.85, s * 0.9, 32);
  const pulseMat = new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide });
  const pulse = new THREE.Mesh(pulseGeo, pulseMat);
  pulse.position.z = 0.05;
  pulse.userData.isPulse = true;
  group.add(pulse);

  // === Corner mounting bolts ===
  for (const bx of [-s*0.7, s*0.7]) {
    for (const by of [-s*0.38, s*0.38]) {
      const bolt = new THREE.Mesh(new THREE.CircleGeometry(s*0.03, 8),
        new THREE.MeshBasicMaterial({ color: 0x99aabb, side: THREE.DoubleSide }));
      bolt.position.set(bx, by, 0.1);
      group.add(bolt);
    }
  }

  group.position.set(x, y, z);
  return group;
}

function createCoffeeMachineSymbol(x, y, z, size, statusColor, status) {
  const group = new THREE.Group();
  const s = size;
  const darkColor = STATUS_DARK[status] || 0x006633;
  const bodyColor = 0x2a2024;
  const chrome = 0x99aabb;
  const chromeDark = 0x667788;
  const accent = 0x8b5e3c; // warm coffee brown

  // === Base / drip tray ===
  const baseGeo = new THREE.PlaneGeometry(s * 1.2, s * 0.25);
  const baseMat = new THREE.MeshBasicMaterial({ color: chromeDark, side: THREE.DoubleSide });
  group.add(new THREE.Mesh(baseGeo, baseMat).translateY(-s*0.55).translateZ(0.05));
  // Drip tray grate lines
  for (let i = -2; i <= 2; i++) {
    const grate = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(i * s * 0.12, -s*0.5, 0.1),
      new THREE.Vector3(i * s * 0.12, -s*0.6, 0.1)]);
    group.add(new THREE.Line(grate, new THREE.LineBasicMaterial({ color: chrome, transparent: true, opacity: 0.4 })));
  }

  // === Machine body (tall rectangle) ===
  const bodyShape = new THREE.Shape();
  const bw = s * 0.55, bh = s * 0.65;
  const br = s * 0.06; // corner radius
  bodyShape.moveTo(-bw + br, -bh);
  bodyShape.lineTo(bw - br, -bh);
  bodyShape.quadraticCurveTo(bw, -bh, bw, -bh + br);
  bodyShape.lineTo(bw, bh - br);
  bodyShape.quadraticCurveTo(bw, bh, bw - br, bh);
  bodyShape.lineTo(-bw + br, bh);
  bodyShape.quadraticCurveTo(-bw, bh, -bw, bh - br);
  bodyShape.lineTo(-bw, -bh + br);
  bodyShape.quadraticCurveTo(-bw, -bh, -bw + br, -bh);
  const bodyMesh = new THREE.Mesh(new THREE.ShapeGeometry(bodyShape),
    new THREE.MeshBasicMaterial({ color: bodyColor, transparent: true, opacity: 0.95, side: THREE.DoubleSide }));
  bodyMesh.position.set(0, s*0.05, 0.08);
  group.add(bodyMesh);
  // Body outline
  const bodyOutline = [];
  for (let i = 0; i <= 40; i++) {
    const t = i / 40;
    const angle = t * Math.PI * 2;
    bodyOutline.push(new THREE.Vector3(
      Math.cos(angle) * bw * 1.02,
      s*0.05 + Math.sin(angle) * bh * 1.02, 0.12));
  }
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(bodyOutline),
    new THREE.LineBasicMaterial({ color: chrome, transparent: true, opacity: 0.5 })));

  // === Bean hopper (top) ===
  const hopperShape = new THREE.Shape();
  hopperShape.moveTo(-s*0.3, 0); hopperShape.lineTo(s*0.3, 0);
  hopperShape.lineTo(s*0.2, s*0.3); hopperShape.lineTo(-s*0.2, s*0.3);
  hopperShape.closePath();
  const hopper = new THREE.Mesh(new THREE.ShapeGeometry(hopperShape),
    new THREE.MeshBasicMaterial({ color: 0x3a2a1a, transparent: true, opacity: 0.85, side: THREE.DoubleSide }));
  hopper.position.set(0, s*0.55, 0.15);
  group.add(hopper);
  // Hopper outline
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-s*0.3, s*0.55, 0.2), new THREE.Vector3(s*0.3, s*0.55, 0.2),
    new THREE.Vector3(s*0.2, s*0.85, 0.2), new THREE.Vector3(-s*0.2, s*0.85, 0.2),
    new THREE.Vector3(-s*0.3, s*0.55, 0.2)]),
    new THREE.LineBasicMaterial({ color: accent })));
  // Coffee beans (small dots in hopper)
  for (const bp of [[-0.05,0.68],[0.08,0.72],[0,0.62],[0.12,0.65],[-0.1,0.74]]) {
    const bean = new THREE.Mesh(new THREE.CircleGeometry(s*0.025, 6),
      new THREE.MeshBasicMaterial({ color: 0x5c3a1a, side: THREE.DoubleSide }));
    bean.position.set(bp[0]*s, bp[1]*s, 0.22);
    group.add(bean);
  }

  // === Display screen ===
  const screen = new THREE.Mesh(new THREE.PlaneGeometry(s*0.6, s*0.2),
    new THREE.MeshBasicMaterial({ color: 0x112233, side: THREE.DoubleSide }));
  screen.position.set(0, s*0.3, 0.15);
  group.add(screen);
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-s*0.3, s*0.2, 0.18), new THREE.Vector3(s*0.3, s*0.2, 0.18),
    new THREE.Vector3(s*0.3, s*0.4, 0.18), new THREE.Vector3(-s*0.3, s*0.4, 0.18),
    new THREE.Vector3(-s*0.3, s*0.2, 0.18)]),
    new THREE.LineBasicMaterial({ color: statusColor, transparent: true, opacity: 0.8 })));
  // Screen status text (small colored bar)
  const screenBar = new THREE.Mesh(new THREE.PlaneGeometry(s*0.45, s*0.04),
    new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.7, side: THREE.DoubleSide }));
  screenBar.position.set(0, s*0.3, 0.17);
  screenBar.userData.isScreenBar = true;
  group.add(screenBar);

  // === Dispensing nozzle area ===
  // Nozzle housing
  const nozzle = new THREE.Mesh(new THREE.PlaneGeometry(s*0.25, s*0.12),
    new THREE.MeshBasicMaterial({ color: chromeDark, side: THREE.DoubleSide }));
  nozzle.position.set(0, s*0.05, 0.15);
  group.add(nozzle);
  // Two spouts
  for (const nx of [-s*0.06, s*0.06]) {
    const spout = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(nx, s*0.08, 0.2),
      new THREE.Vector3(nx, -s*0.05, 0.2)]);
    group.add(new THREE.Line(spout, new THREE.LineBasicMaterial({ color: chrome })));
  }
  // Coffee stream (animated via userData)
  if (status === 'running') {
    for (const nx of [-s*0.06, s*0.06]) {
      const stream = new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(nx, -s*0.05, 0.2),
        new THREE.Vector3(nx, -s*0.25, 0.2)]);
      const streamLine = new THREE.Line(stream, new THREE.LineBasicMaterial({
        color: accent, transparent: true, opacity: 0.5 }));
      streamLine.userData.isCoffeeStream = true;
      group.add(streamLine);
    }
  }

  // === Cup (below nozzle) ===
  const cupShape = new THREE.Shape();
  cupShape.moveTo(-s*0.12, 0); cupShape.lineTo(s*0.12, 0);
  cupShape.lineTo(s*0.1, -s*0.18); cupShape.lineTo(-s*0.1, -s*0.18);
  cupShape.closePath();
  const cup = new THREE.Mesh(new THREE.ShapeGeometry(cupShape),
    new THREE.MeshBasicMaterial({ color: 0xeeeeee, transparent: true, opacity: 0.75, side: THREE.DoubleSide }));
  cup.position.set(0, -s*0.25, 0.12);
  group.add(cup);
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-s*0.12, -s*0.25, 0.18), new THREE.Vector3(s*0.12, -s*0.25, 0.18),
    new THREE.Vector3(s*0.1, -s*0.43, 0.18), new THREE.Vector3(-s*0.1, -s*0.43, 0.18),
    new THREE.Vector3(-s*0.12, -s*0.25, 0.18)]),
    new THREE.LineBasicMaterial({ color: chrome, transparent: true, opacity: 0.7 })));
  // Cup handle
  const handlePts = [];
  for (let i = 0; i <= 10; i++) {
    const a = -Math.PI/2 + (i/10) * Math.PI;
    handlePts.push(new THREE.Vector3(s*0.12 + Math.cos(a)*s*0.06, -s*0.32 + Math.sin(a)*s*0.07, 0.18));
  }
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(handlePts),
    new THREE.LineBasicMaterial({ color: chrome, transparent: true, opacity: 0.6 })));
  // Coffee in cup
  if (status === 'running') {
    const coffeeFill = new THREE.Mesh(new THREE.PlaneGeometry(s*0.18, s*0.08),
      new THREE.MeshBasicMaterial({ color: accent, transparent: true, opacity: 0.6, side: THREE.DoubleSide }));
    coffeeFill.position.set(0, -s*0.3, 0.14);
    group.add(coffeeFill);
  }

  // === Steam wisps (above cup, for running status) ===
  if (status === 'running') {
    for (const sx of [-s*0.04, s*0.04]) {
      const steamPts = [
        new THREE.Vector3(sx, -s*0.22, 0.25),
        new THREE.Vector3(sx + s*0.02, -s*0.16, 0.25),
        new THREE.Vector3(sx - s*0.02, -s*0.1, 0.25)];
      const steamLine = new THREE.Line(new THREE.BufferGeometry().setFromPoints(steamPts),
        new THREE.LineBasicMaterial({ color: 0xcccccc, transparent: true, opacity: 0.3 }));
      steamLine.userData.isSteam = true;
      group.add(steamLine);
    }
  }

  // === Side buttons (3 dots) ===
  for (let i = 0; i < 3; i++) {
    const btn = new THREE.Mesh(new THREE.CircleGeometry(s*0.025, 8),
      new THREE.MeshBasicMaterial({ color: i === 0 ? statusColor : chrome, side: THREE.DoubleSide }));
    btn.position.set(s*0.42, s*0.15 - i*s*0.1, 0.15);
    group.add(btn);
  }

  // === Status LED ===
  const led = new THREE.Mesh(new THREE.CircleGeometry(s*0.04, 12),
    new THREE.MeshBasicMaterial({ color: statusColor, side: THREE.DoubleSide }));
  led.position.set(-s*0.42, s*0.42, 0.15);
  group.add(led);
  const ledGlow = new THREE.Mesh(new THREE.CircleGeometry(s*0.09, 12),
    new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide }));
  ledGlow.position.set(-s*0.42, s*0.42, 0.13);
  ledGlow.userData.isLedGlow = true;
  group.add(ledGlow);

  // === Pulse ring ===
  const pulseGeo = new THREE.RingGeometry(s * 0.8, s * 0.85, 32);
  const pulseMat = new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide });
  const pulse = new THREE.Mesh(pulseGeo, pulseMat);
  pulse.position.z = 0.03;
  pulse.userData.isPulse = true;
  group.add(pulse);

  group.position.set(x, y, z);
  return group;
}

function createAirConditioningSymbol(x, y, z, size, statusColor, status) {
  const group = new THREE.Group();
  const s = size;
  const darkColor = STATUS_DARK[status] || 0x006633;
  const bodyColor = 0xdde8f0;
  const metalColor = 0xc8d8e8;
  const metalDark = 0x4a5a6a;
  const highlight = 0x88aacc;
  const coolBlue = 0x44aadd;
  const coolDark = 0x2288bb;

  // === Wall mount bracket ===
  const bracketMat = new THREE.MeshBasicMaterial({ color: metalDark, side: THREE.DoubleSide });
  const bracketL = new THREE.Mesh(new THREE.PlaneGeometry(s * 0.08, s * 0.5), bracketMat);
  bracketL.position.set(-s*0.65, s*0.15, -0.02);
  group.add(bracketL);
  const bracketR = new THREE.Mesh(new THREE.PlaneGeometry(s * 0.08, s * 0.5), bracketMat);
  bracketR.position.set(s*0.65, s*0.15, -0.02);
  group.add(bracketR);

  // === Main body (sleek rounded rectangle - wall unit) ===
  const bw = s * 0.75, bh = s * 0.35;
  const bodyShape = new THREE.Shape();
  const br = s * 0.08;
  bodyShape.moveTo(-bw + br, -bh);
  bodyShape.lineTo(bw - br, -bh);
  bodyShape.quadraticCurveTo(bw, -bh, bw, -bh + br);
  bodyShape.lineTo(bw, bh - br);
  bodyShape.quadraticCurveTo(bw, bh, bw - br, bh);
  bodyShape.lineTo(-bw + br, bh);
  bodyShape.quadraticCurveTo(-bw, bh, -bw, bh - br);
  bodyShape.lineTo(-bw, -bh + br);
  bodyShape.quadraticCurveTo(-bw, -bh, -bw + br, -bh);
  const bodyMesh = new THREE.Mesh(new THREE.ShapeGeometry(bodyShape),
    new THREE.MeshBasicMaterial({ color: 0xdde8f0, transparent: true, opacity: 0.95, side: THREE.DoubleSide }));
  bodyMesh.position.set(0, 0, 0.05);
  group.add(bodyMesh);
  // Body outline
  const bodyOutlinePts = [];
  const cornerPts = [[-bw+br,-bh],[ bw-br,-bh],[bw,-bh+br],[bw,bh-br],[bw-br,bh],[-bw+br,bh],[-bw,bh-br],[-bw,-bh+br]];
  for (const p of cornerPts) bodyOutlinePts.push(new THREE.Vector3(p[0], p[1], 0.1));
  bodyOutlinePts.push(bodyOutlinePts[0].clone());
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(bodyOutlinePts),
    new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.7 })));

  // === Top accent stripe (brand line) ===
  const accentStripe = new THREE.Mesh(new THREE.PlaneGeometry(bw * 1.8, s * 0.04),
    new THREE.MeshBasicMaterial({ color: coolBlue, transparent: true, opacity: 0.6, side: THREE.DoubleSide }));
  accentStripe.position.set(0, bh - s*0.06, 0.12);
  group.add(accentStripe);

  // === Air outlet vanes (horizontal louvers) ===
  const vaneCount = 5;
  const vaneTop = -s*0.02, vaneBot = -bh + s*0.06;
  for (let i = 0; i < vaneCount; i++) {
    const vy = vaneBot + (vaneTop - vaneBot) * (i / (vaneCount - 1));
    // Each vane is a slightly angled line
    const vaneAngle = status === 'running' ? 0.15 * Math.sin(i * 0.5) : 0;
    const vl = bw * 1.5;
    const vaneLine = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(-vl/2, vy - vaneAngle * s * 0.02, 0.15),
      new THREE.Vector3(0, vy + vaneAngle * s * 0.02, 0.15),
      new THREE.Vector3(vl/2, vy - vaneAngle * s * 0.02, 0.15)]);
    group.add(new THREE.Line(vaneLine, new THREE.LineBasicMaterial({
      color: status === 'running' ? coolBlue : 0x8899aa, transparent: true, opacity: 0.5 })));
  }

  // === Air flow indicators (when running) ===
  if (status === 'running') {
    for (const fx of [-s*0.35, 0, s*0.35]) {
      const flowPts = [];
      for (let i = 0; i <= 8; i++) {
        const t = i / 8;
        flowPts.push(new THREE.Vector3(
          fx + Math.sin(t * Math.PI * 2) * s * 0.04,
          -bh - t * s * 0.35,
          0.2));
      }
      const flowLine = new THREE.Line(new THREE.BufferGeometry().setFromPoints(flowPts),
        new THREE.LineBasicMaterial({ color: coolBlue, transparent: true, opacity: 0.3 }));
      flowLine.userData.isAirFlow = true;
      group.add(flowLine);
    }
    // Downward arrows
    for (const fx of [-s*0.2, s*0.2]) {
      const arrowPts = [
        new THREE.Vector3(fx - s*0.05, -bh - s*0.12, 0.2),
        new THREE.Vector3(fx, -bh - s*0.22, 0.2),
        new THREE.Vector3(fx + s*0.05, -bh - s*0.12, 0.2)];
      const arrow = new THREE.Line(new THREE.BufferGeometry().setFromPoints(arrowPts),
        new THREE.LineBasicMaterial({ color: coolBlue, transparent: true, opacity: 0.4 }));
      arrow.userData.isAirFlow = true;
      group.add(arrow);
    }
  }

  // === Display panel (right side) ===
  const dispW = s * 0.25, dispH = s * 0.14;
  const display = new THREE.Mesh(new THREE.PlaneGeometry(dispW, dispH),
    new THREE.MeshBasicMaterial({ color: 0x112233, side: THREE.DoubleSide }));
  display.position.set(s*0.42, bh - s*0.18, 0.12);
  group.add(display);
  // Display border
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(s*0.42-dispW/2, bh-s*0.18-dispH/2, 0.14),
    new THREE.Vector3(s*0.42+dispW/2, bh-s*0.18-dispH/2, 0.14),
    new THREE.Vector3(s*0.42+dispW/2, bh-s*0.18+dispH/2, 0.14),
    new THREE.Vector3(s*0.42-dispW/2, bh-s*0.18+dispH/2, 0.14),
    new THREE.Vector3(s*0.42-dispW/2, bh-s*0.18-dispH/2, 0.14)]),
    new THREE.LineBasicMaterial({ color: statusColor, transparent: true, opacity: 0.7 })));
  // Temperature readout bar
  const tempBar = new THREE.Mesh(new THREE.PlaneGeometry(dispW * 0.7, dispH * 0.25),
    new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.6, side: THREE.DoubleSide }));
  tempBar.position.set(s*0.42, bh - s*0.18, 0.15);
  tempBar.userData.isScreenBar = true;
  group.add(tempBar);

  // === Snowflake icon (center top - cooling indicator) ===
  const sfx = -s*0.42, sfy = bh - s*0.18;
  const sfR = s * 0.08;
  // 6 arms of snowflake
  for (let i = 0; i < 6; i++) {
    const a = (i / 6) * Math.PI * 2;
    const arm = new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(sfx, sfy, 0.18),
      new THREE.Vector3(sfx + Math.cos(a)*sfR, sfy + Math.sin(a)*sfR, 0.18)]);
    group.add(new THREE.Line(arm, new THREE.LineBasicMaterial({
      color: status === 'running' ? coolBlue : 0x667788 })));
    // Small branches on each arm
    const bx = sfx + Math.cos(a)*sfR*0.6, by = sfy + Math.sin(a)*sfR*0.6;
    const ba1 = a + 0.6, ba2 = a - 0.6;
    for (const ba of [ba1, ba2]) {
      const branch = new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(bx, by, 0.18),
        new THREE.Vector3(bx + Math.cos(ba)*sfR*0.35, by + Math.sin(ba)*sfR*0.35, 0.18)]);
      group.add(new THREE.Line(branch, new THREE.LineBasicMaterial({
        color: status === 'running' ? coolBlue : 0x667788, transparent: true, opacity: 0.6 })));
    }
  }

  // === Fan turbine (visible through grille, center of body) ===
  const fanR = s * 0.18;
  const fanFill = new THREE.Mesh(new THREE.CircleGeometry(fanR, 24),
    new THREE.MeshBasicMaterial({ color: 0xccd8e4, transparent: true, opacity: 0.4, side: THREE.DoubleSide }));
  fanFill.position.set(0, -s*0.04, 0.08);
  group.add(fanFill);
  // Fan blades (4 curved blades)
  for (let i = 0; i < 4; i++) {
    const a = (i / 4) * Math.PI * 2;
    const bladePts = [];
    for (let j = 0; j <= 8; j++) {
      const t = j / 8;
      const r = fanR * t;
      const curve = a + t * 0.8; // blade curvature
      bladePts.push(new THREE.Vector3(
        Math.cos(curve) * r,
        -s*0.04 + Math.sin(curve) * r,
        0.12));
    }
    const blade = new THREE.Line(new THREE.BufferGeometry().setFromPoints(bladePts),
      new THREE.LineBasicMaterial({ color: metalDark, transparent: true, opacity: 0.7 }));
    blade.userData.isFanBlade = true;
    group.add(blade);
  }
  // Fan center hub
  const hub = new THREE.Mesh(new THREE.CircleGeometry(s*0.035, 12),
    new THREE.MeshBasicMaterial({ color: metalDark, side: THREE.DoubleSide }));
  hub.position.set(0, -s*0.04, 0.14);
  group.add(hub);
  // Fan outline ring
  const fanRingPts = [];
  for (let i = 0; i <= 32; i++) {
    const a = (i / 32) * Math.PI * 2;
    fanRingPts.push(new THREE.Vector3(Math.cos(a)*fanR, -s*0.04 + Math.sin(a)*fanR, 0.13));
  }
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(fanRingPts),
    new THREE.LineBasicMaterial({ color: highlight, transparent: true, opacity: 0.4 })));

  // === Refrigerant pipes (left side, going to wall) ===
  const pipeY1 = s*0.1, pipeY2 = -s*0.05;
  // Supply pipe (thicker, cool)
  const supplyPts = [
    new THREE.Vector3(-bw, pipeY1, 0.1),
    new THREE.Vector3(-bw - s*0.2, pipeY1, 0.1),
    new THREE.Vector3(-bw - s*0.2, s*0.4, 0.1)];
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(supplyPts),
    new THREE.LineBasicMaterial({ color: coolBlue })));
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(
    supplyPts.map(p => new THREE.Vector3(p.x + s*0.04, p.y, p.z))),
    new THREE.LineBasicMaterial({ color: coolBlue })));
  // Return pipe (warm)
  const returnPts = [
    new THREE.Vector3(-bw, pipeY2, 0.1),
    new THREE.Vector3(-bw - s*0.12, pipeY2, 0.1),
    new THREE.Vector3(-bw - s*0.12, s*0.4, 0.1)];
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(returnPts),
    new THREE.LineBasicMaterial({ color: 0xcc7744 })));
  group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(
    returnPts.map(p => new THREE.Vector3(p.x + s*0.04, p.y, p.z))),
    new THREE.LineBasicMaterial({ color: 0xcc7744 })));

  // === Temperature gauge (small thermometer, right of display) ===
  const thX = s*0.62, thY = bh - s*0.18;
  // Thermometer bulb
  const bulb = new THREE.Mesh(new THREE.CircleGeometry(s*0.035, 12),
    new THREE.MeshBasicMaterial({ color: status === 'stopped' ? 0xcc4444 : coolBlue, side: THREE.DoubleSide }));
  bulb.position.set(thX, thY - s*0.06, 0.15);
  group.add(bulb);
  // Thermometer tube
  const tubeGeo = new THREE.PlaneGeometry(s*0.02, s*0.1);
  const tubeMat = new THREE.MeshBasicMaterial({ color: 0xdddddd, side: THREE.DoubleSide });
  group.add(new THREE.Mesh(tubeGeo, tubeMat).translateX(thX).translateY(thY + s*0.01).translateZ(0.15));
  // Mercury level
  const mercH = status === 'running' ? s*0.04 : status === 'warning' ? s*0.07 : s*0.09;
  const merc = new THREE.Mesh(new THREE.PlaneGeometry(s*0.014, mercH),
    new THREE.MeshBasicMaterial({ color: status === 'running' ? coolBlue : status === 'warning' ? 0xffaa00 : 0xcc4444, side: THREE.DoubleSide }));
  merc.position.set(thX, thY - s*0.06 + mercH/2 + s*0.02, 0.16);
  group.add(merc);

  // === Status LED ===
  const ledGeo = new THREE.CircleGeometry(s * 0.05, 16);
  const ledMat = new THREE.MeshBasicMaterial({ color: statusColor, side: THREE.DoubleSide });
  const led = new THREE.Mesh(ledGeo, ledMat);
  led.position.set(s*0.2, bh - s*0.06, 0.15);
  group.add(led);
  // LED glow
  const glowGeo = new THREE.CircleGeometry(s * 0.1, 16);
  const glowMat = new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide });
  const glow = new THREE.Mesh(glowGeo, glowMat);
  glow.position.set(s*0.2, bh - s*0.06, 0.13);
  glow.userData.isLedGlow = true;
  group.add(glow);

  // === Power/WiFi indicator dots ===
  for (let i = 0; i < 3; i++) {
    const dotSize = s * 0.02;
    const dot = new THREE.Mesh(new THREE.CircleGeometry(dotSize, 6),
      new THREE.MeshBasicMaterial({ color: i === 0 ? statusColor : highlight, side: THREE.DoubleSide }));
    dot.position.set(s*0.08 + i*s*0.06, bh - s*0.06, 0.15);
    group.add(dot);
  }

  // === Status pulse ring ===
  const pulseGeo = new THREE.RingGeometry(s * 0.85, s * 0.9, 32);
  const pulseMat = new THREE.MeshBasicMaterial({ color: statusColor, transparent: true, opacity: 0.3, side: THREE.DoubleSide });
  const pulse = new THREE.Mesh(pulseGeo, pulseMat);
  pulse.position.z = 0.02;
  pulse.userData.isPulse = true;
  group.add(pulse);

  // === Corner mounting screws ===
  for (const bx of [-bw + s*0.06, bw - s*0.06]) {
    for (const by of [-bh + s*0.06, bh - s*0.06]) {
      const screw = new THREE.Mesh(new THREE.CircleGeometry(s*0.025, 8),
        new THREE.MeshBasicMaterial({ color: 0x99aabb, side: THREE.DoubleSide }));
      screw.position.set(bx, by, 0.1);
      group.add(screw);
    }
  }

  group.position.set(x, y, z);
  return group;
}

function buildEquipmentForLevel(level) {
  const fg = floorGroups[level];
  if (!fg) return;
  const data = floorData[level];
  if (!data) return;
  const pw = data.page.width, ph = data.page.height;
  const levelKey = level.split('/').pop();

  if (equipmentGroups[level]) {
    fg.container.remove(equipmentGroups[level]);
    clearGroup(equipmentGroups[level]);
  }
  const eqGroup = new THREE.Group();
  equipmentGroups[level] = eqGroup;

  const levelEquip = equipment.filter(e => e.level === levelKey);
  const iconPromises = [];

  // Group equipment by room
  const byRoom = {};
  for (const eq of levelEquip) {
    if (!byRoom[eq.room]) byRoom[eq.room] = [];
    byRoom[eq.room].push(eq);
  }

  // Also add rooms that have occupancy but no equipment
  const globalOccAll = window._globalOccupancy || {};
  for (const room of data.rooms) {
    const roomKey = `${levelKey}/${room.name}`;
    if ((globalOccAll[roomKey] || globalOccAll[room.name]) && !byRoom[room.name]) {
      byRoom[room.name] = []; // empty equipment list, occupants will be added below
    }
  }

  for (const [roomName, roomEquips] of Object.entries(byRoom)) {
    const room = data.rooms.find(r => r.name === roomName);
    if (!room) continue;
    const [wx, wy] = pdfToWorld(room.center[0], room.center[1], pw, ph);

    // === Build combined list: equipment + occupants ===
    const allItems = roomEquips.map(eq => ({
      iconType: eq.type, name: eq.name, colorHex: STATUS_HEX[eq.status] || '#00e676',
      noRecolor: false, eqData: eq, isOccupant: false, position: eq.position || null,
      height: Number.isFinite(eq.height) && eq.height > 0 ? eq.height : 6,
    }));

    // Add occupants from global occupancy
    const globalOcc = window._globalOccupancy || {};
    const roomOcc = globalOcc[`${levelKey}/${room.name}`] || globalOcc[room.name];
    if (roomOcc) {
      if (roomOcc.persons) {
        for (const p of roomOcc.persons) {
          allItems.push({ iconType: p.icon || 'man', name: p.name, colorHex: null, noRecolor: true, isOccupant: true, position: p.position || null, height: 6 });
        }
      }
      if (roomOcc.aliens) {
        for (const a of roomOcc.aliens) {
          allItems.push({ iconType: 'alien', name: 'Alien', colorHex: null, noRecolor: true, isOccupant: true, position: null, height: 6 });
        }
      }
    }

    const totalItems = allItems.length;

    // === One badge per room with the number of people in it ===
    const occupantCount = roomOcc
      ? (roomOcc.persons ? roomOcc.persons.length : 0) + (roomOcc.aliens ? roomOcc.aliens.length : 0)
      : 0;
    if (occupantCount > 0) {
      const badge = createOccupantCountBadge(wx, wy, occupantCount);
      badge.userData.eqRoom = roomName;
      badge.userData.eqMode = 'count';
      eqGroup.add(badge);
    }

    // === Expanded icons (same layout in 2D and 3D) ===
    for (let idx = 0; idx < totalItems; idx++) {
      const item = allItems[idx];
      const size = 3;

      const p = loadEquipmentIcon(item.iconType, item.colorHex, item.noRecolor).then(canvas => {
        // Arrange in a circle around the room centroid
        const angle = (idx / totalItems) * Math.PI * 2 - Math.PI / 2;
        const radius = Math.max(5, totalItems * 1.2);
        const exact = Array.isArray(item.position)
          ? pdfToWorld(item.position[0], item.position[1], pw, ph) : null;
        const ex = exact ? exact[0] : wx + Math.cos(angle) * radius;
        const ey = exact ? exact[1] : wy + Math.sin(angle) * radius;
        const ez = item.height || 6;

        const expandedSprite = createEquipmentSprite(ex, ey, ez, size, canvas);
        if (item.eqData) expandedSprite.userData.equipment = item.eqData;
        expandedSprite.userData.eqRoom = roomName;
        expandedSprite.userData.eqMode = 'expanded';
        expandedSprite.visible = false;
        eqGroup.add(expandedSprite);

        // Label under the sprite
        const lc = document.createElement('canvas');
        lc.width = 256; lc.height = 32;
        const ctx = lc.getContext('2d');
        const labelColor = item.isOccupant ? (item.iconType === 'alien' ? '#ff2222' : '#00ccff') : (item.colorHex || '#88aacc');
        ctx.font = 'bold 14px monospace'; ctx.fillStyle = labelColor; ctx.textAlign = 'center';
        ctx.fillText(item.name, 128, 16);
        const labelSprite = new THREE.Sprite(new THREE.SpriteMaterial({
          map: new THREE.CanvasTexture(lc), transparent: true, depthWrite: false }));
        labelSprite.position.set(ex, ey - size, ez + 1);
        labelSprite.scale.set(8, 1.2, 1);
        if (item.eqData) labelSprite.userData.equipment = item.eqData;
        labelSprite.userData.eqRoom = roomName;
        labelSprite.userData.eqMode = 'expanded';
        labelSprite.visible = false;
        eqGroup.add(labelSprite);
      });
      iconPromises.push(p);
    }
  }

  Promise.all(iconPromises).then(() => { filterEquipment(); render(); });
  eqGroup.visible = true;
  fg.container.add(eqGroup);
}

function buildAllEquipment() {
  LEVELS.forEach(l => buildEquipmentForLevel(l));
  filterEquipment();
}

let _eqFilterRoom = null;
let _lastSearchedRoom = null; // {name, level} of last searched room
