// shell.js — the building as it looks from the outside.
//
// A-huset is a suterräng building on a slope, and everything about how it
// reads from outside follows from that plus three materials:
//
//   · a red-brown standing-seam sheet-metal skin on the original block —
//     the north and west elevations and the walls facing the inner yards
//     (photos 2, 8, 15, 18, 19, 23);
//   · red-orange face brick in running bond on the east block and on every
//     wing south of the main block (photos 5, 6, 10, 13, 16, 21, 27, 28),
//     over a painted brick base course wherever the ground has dropped far
//     enough for the basement to surface (photo 8);
//   · a frosted screen printed with mathematics on the level-2 volumes that
//     cantilever out over the north elevation, carried on round concrete
//     columns (photos 15, 17, 22, 26).
//
// None of that is hand-placed. The three floor plans give three footprints;
// the walls of each storey are built from its own outline, a storey is capped
// with a parapet only where nothing stands on it, level 2 is glass exactly
// where it oversails level 1 on the north side, and every window band starts
// above the local ground height that world.js computes. Change the plans and
// the elevations follow.
//
// Nothing is loaded: every surface is a canvas drawn at boot.

const shellGroup = new THREE.Group();
shellGroup.name = 'shell';
scene.add(shellGroup);

let shellBuilt = false;
let baseRings = null, topRings = null, entranceInfo = null;
let levelRings = [];         // rings per level, index = storey
let levelMasks = [];         // rasterised footprint per storey
let baseSupport = null;      // the ground floor, asked whether it carries a storey
let shellFadeParts = [];     // { mat, base } — faded out as the building opens
let shellLiftParts = [];     // { obj, z0, lift } — roof & glass rise on opening
let ghostEdges = null;

// One texture tile spans three structural bays of facade and one storey.
const BAY_W       = 8.6;                 // ~3.9 m, the module the photos show
const FACADE_TILE = BAY_W * 3;
const GLASS_TILE  = BAY_W * 3;
const FOUNDATION  = -16;                 // walls run this far below the datum
const CHUNK       = 11;                  // wall runs are classified this finely
const GLASS_DROP  = 2.9;                 // the glass box hangs below its floor
const GLASS_RISE  = 1.3;                 // and stands proud of the roof line
const GLASS_LEAN  = 0.75;                // its head leans out over its foot

function storeyZ(i) { return i * EXT_STOREY; }

// ── shared canvas helpers ────────────────────────────────────────────────
// A pane of glass: dark room behind, a raking sky reflection, a blind or a
// curtain in some of them. Used by every skin, so all the glazing on the
// building belongs to one family.
function drawPane(x, px, py, w, h, rnd, opt) {
  const o = opt || {};
  const g = x.createLinearGradient(px, py, px + w * 0.45, py + h);
  if (o.shop) {
    g.addColorStop(0, '#c3d4de');
    g.addColorStop(0.34, '#8ba3b4');
    g.addColorStop(0.70, '#5a7182');
    g.addColorStop(1, '#7d95a4');
  } else {
    g.addColorStop(0, o.pale ? '#c2d3dd' : '#a8c0cf');
    g.addColorStop(0.30, '#6d8698');
    g.addColorStop(0.66, '#47606f');
    g.addColorStop(1, '#5f7889');
  }
  x.fillStyle = g;
  x.fillRect(px, py, w, h);
  x.save();
  x.beginPath(); x.rect(px, py, w, h); x.clip();
  // sky raking across the pane
  x.fillStyle = 'rgba(230,242,252,0.22)';
  x.beginPath();
  x.moveTo(px - 10, py + h * 0.86); x.lineTo(px + w + 10, py + h * 0.10);
  x.lineTo(px + w + 10, py - 10); x.lineTo(px - 10, py + h * 0.46);
  x.closePath(); x.fill();
  const r = rnd();
  if (r > 0.55) {                       // a venetian blind, half down
    const bh = h * (0.35 + rnd() * 0.5);
    x.fillStyle = 'rgba(226,226,222,0.82)';
    x.fillRect(px, py, w, bh);
    x.strokeStyle = 'rgba(120,124,126,0.55)';
    x.lineWidth = 1;
    for (let by = py + 3; by < py + bh; by += 4.5) {
      x.beginPath(); x.moveTo(px, by); x.lineTo(px + w, by); x.stroke();
    }
  } else if (r > 0.34) {                // a pale curtain at one side
    x.fillStyle = 'rgba(236,234,226,0.72)';
    x.fillRect(px + (rnd() > 0.5 ? 0 : w * 0.55), py, w * 0.45, h);
  } else if (r > 0.42) {                // a lit room
    x.fillStyle = 'rgba(255,236,196,0.20)';
    x.fillRect(px, py + h * 0.15, w, h * 0.85);
  }
  x.restore();
  x.strokeStyle = 'rgba(24,30,34,0.35)';
  x.lineWidth = 1.4;
  x.strokeRect(px + 0.7, py + 0.7, w - 1.4, h - 1.4);
}

// Aluminium frame around an opening, with the sill projecting past the reveal.
function drawFrame(x, px, py, w, h, col, lw, sill) {
  x.strokeStyle = col;
  x.lineWidth = lw;
  x.strokeRect(px - lw / 2, py - lw / 2, w + lw, h + lw);
  if (sill) {
    x.fillStyle = sill;
    x.fillRect(px - lw * 1.7, py + h + lw / 2, w + lw * 3.4, lw * 1.25);
    x.fillStyle = 'rgba(0,0,0,0.28)';
    x.fillRect(px - lw * 1.7, py + h + lw / 2 + lw * 1.25, w + lw * 3.4, lw * 0.5);
  }
}

// One window band for the whole building.
//
// A storey is a storey whichever material is in front of it: the floor slab is
// at the same level behind the brick, behind the sheet metal and behind the
// painted base course, so the windows in all three have to be at the same
// height. They were not — each skin had picked its own head and sill — and
// because the skins change partway along an elevation rather than at a corner,
// the row of windows stepped up or down in the middle of a wall wherever one
// gave way to the next. Every skin that punches a window into a normal storey
// now takes its band from here.
const BAND_HEAD = 0.21;                 // head, as a fraction of the storey
const BAND_H = 0.37;                    // and how deep the opening is

// ── the red-brown standing-seam sheet ────────────────────────────────────
// Photo 8 is the reference: 55 cm panels, a raised seam between every pair,
// wide punched windows with wood-orange frames on the main floor and a band of
// small windows and louvres at the top.
function metalSkin(storey) {
  const W = 1024;
  const H = Math.round(W * EXT_STOREY / FACADE_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(4100 + storey * 17);
  const bay = W / 3;

  // Colour sampled off the survey: the reddish clusters of photos 2, 8 and 26
  // sit at rgb(155,90,80)–rgb(176,113,105) under an overcast sky. The sheet is
  // a deep muted red-brown, not the orange it used to be.
  const g = x.createLinearGradient(0, 0, 0, H);
  g.addColorStop(0, '#b26c5e');
  g.addColorStop(0.55, '#a86255');
  g.addColorStop(1, '#9f5b4c');
  x.fillStyle = g;
  x.fillRect(0, 0, W, H);

  // standing seams — measured off photo 8 at about 0.55 m, so 1.2 world units
  // apart, each with its own thin shadow and highlight
  const seamStep = W * 1.2 / FACADE_TILE;
  for (let px = 0; px < W + seamStep; px += seamStep) {
    const v = 0.985 + rnd() * 0.03;             // panel-to-panel variation
    x.fillStyle = `rgba(255,255,255,${(v - 1) * 0.7 + 0.012})`;
    x.fillRect(px, 0, seamStep, H);
    x.fillStyle = 'rgba(64,26,16,0.20)';
    x.fillRect(px - 1.2, 0, 2.4, H);
    x.fillStyle = 'rgba(255,232,214,0.14)';
    x.fillRect(px + 1.2, 0, 1.3, H);
  }
  // weathering: faint, and along the seams rather than across the sheet
  x.globalAlpha = 0.028;
  for (let i = 0; i < 130; i++) {
    x.fillStyle = rnd() > 0.5 ? '#33130a' : '#e8cdbe';
    x.fillRect(Math.round(rnd() * W / seamStep) * seamStep + rnd() * seamStep,
               rnd() * H, 1 + rnd() * 2, 40 + rnd() * 180);
  }
  x.globalAlpha = 1;

  const WOOD = '#b06a45', ZINC = '#c9cbc7';
  if (storey === 2) {
    // top band: a small square window and a louvred panel per bay
    for (let i = 0; i < 3; i++) {
      const bx = i * bay;
      const w = bay * 0.22, h = H * 0.21, px = bx + bay * 0.09, py = H * 0.17;
      drawPane(x, px, py, w, h, rnd);
      drawFrame(x, px, py, w, h, WOOD, 4, ZINC);
      const lw = bay * 0.44, lx = bx + bay * 0.40;
      x.fillStyle = '#9c5a4a';
      x.fillRect(lx, py, lw, h);
      x.strokeStyle = 'rgba(74,26,12,0.42)';
      x.lineWidth = 2;
      for (let ly = py + 4; ly < py + h; ly += 7) {
        x.beginPath(); x.moveTo(lx, ly); x.lineTo(lx + lw, ly); x.stroke();
      }
      x.strokeStyle = 'rgba(255,220,200,0.14)';
      x.lineWidth = 1.4;
      for (let ly = py + 6.4; ly < py + h; ly += 7) {
        x.beginPath(); x.moveTo(lx, ly); x.lineTo(lx + lw, ly); x.stroke();
      }
      drawFrame(x, lx, py, lw, h, '#ab6047', 3, null);
    }
  } else {
    // Main floor: one wide window per bay, wood-orange frame, zinc sill.
    //
    // Head and sill are BAND_HEAD/BAND_H and not this skin's own taste, because
    // the sheet metal does not wrap the building — it stops partway along an
    // elevation and the brick carries on. Where it stopped, the metal's windows
    // used to start a tenth of a storey higher and run half a metre lower than
    // the brick windows beside them, so the elevation changed level in the
    // middle of a run: the owner's "improve window alignment / transition".
    // The bay module is already continuous (u runs along the whole ring), so
    // sharing the band is all it takes for the two to read as one row.
    for (let i = 0; i < 3; i++) {
      const w = bay * 0.74, px = i * bay + bay * 0.13;
      const py = H * BAND_HEAD, h = H * BAND_H;
      // the light sill the brick carries, so the line runs through the joint
      x.fillStyle = '#c9cbc8';
      x.fillRect(px - 3, py - H * 0.052, w + 6, H * 0.048);
      drawPane(x, px, py, w, h, rnd);
      x.strokeStyle = WOOD; x.lineWidth = 3;
      x.beginPath();
      x.moveTo(px + w / 2, py); x.lineTo(px + w / 2, py + h);
      x.moveTo(px, py + h * 0.30); x.lineTo(px + w, py + h * 0.30);
      x.stroke();
      drawFrame(x, px, py, w, h, WOOD, 5, ZINC);
      // and the dark spandrel under it, down to the floor line
      const sy = py + h + 5;
      x.fillStyle = '#3a2b26';
      x.fillRect(px - 2, sy, w + 4, H * 0.16);
      x.fillStyle = 'rgba(0,0,0,0.30)';
      x.fillRect(px - 2, sy + H * 0.16 - 3, w + 4, 3);
    }
  }
  return finishTex(c, 1, 1, 8);
}

// ── the painted brick base course ────────────────────────────────────────
// Where the ground falls away the basement surfaces as a salmon-painted brick
// plinth with its own row of windows and the occasional door (photo 8).
function plinthSkin() {
  const W = 1024;
  const H = Math.round(W * EXT_STOREY / FACADE_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(6100);
  const bay = W / 3;

  // Painted brick sampled off photos 2 and 8: rgb(153,92,76)–rgb(194,138,119),
  // a dusty salmon over a red-brown, never the pink it used to be.
  x.fillStyle = '#a26a58';
  x.fillRect(0, 0, W, H);
  const cw = Math.max(9, W * (0.25 * U_PER_M) / FACADE_TILE);
  const ch = Math.max(4.2, H * (0.077 * U_PER_M) / EXT_STOREY);   // ~13 courses/m
  for (let row = 0, py = 0; py < H; row++, py += ch) {
    const off = (row % 2) * cw / 2;
    for (let px = -cw; px < W + cw; px += cw) {
      const v = 0.93 + rnd() * 0.14;
      x.fillStyle = `rgb(${Math.round(179 * v)},${Math.round(118 * v)},${Math.round(100 * v)})`;
      x.fillRect(px + off + 0.7, py + 0.7, cw - 1.4, ch - 1.4);
    }
  }
  x.globalAlpha = 0.10;
  for (let i = 0; i < 700; i++) {
    x.fillStyle = rnd() > 0.5 ? '#fff' : '#6d4130';
    x.fillRect(rnd() * W, rnd() * H, 3, 3);
  }
  x.globalAlpha = 1;

  // Same band as the brick and the sheet metal: on the west slope the base
  // course stands at grade next to brick that stands at grade, and a row of
  // windows that changed height where the two met is what the transition note
  // is about.
  for (let i = 0; i < 3; i++) {
    const w = bay * 0.70, px = i * bay + bay * 0.15;
    const py = H * BAND_HEAD, h = H * BAND_H;
    drawPane(x, px, py, w, h, rnd);
    x.strokeStyle = '#b06a45'; x.lineWidth = 3;
    x.beginPath();
    x.moveTo(px + w / 2, py); x.lineTo(px + w / 2, py + h); x.stroke();
    drawFrame(x, px, py, w, h, '#b06a45', 5, '#c9cbc7');
  }
  // damp course at the very bottom
  x.fillStyle = 'rgba(70,40,28,0.28)';
  x.fillRect(0, H * 0.93, W, H * 0.07);
  return finishTex(c, 1, 1, 8);
}

// ── red-orange face brick ────────────────────────────────────────────────
// Running bond, grey aluminium windows, a dark spandrel panel under each and
// a roller-blind box above — the elevation in photos 6, 13, 21 and 28.
function brickSkin(storey) {
  const W = 1024;
  const H = Math.round(W * EXT_STOREY / FACADE_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(7700 + storey * 31);
  const bay = W / 3;

  // Face brick sampled off photos 5, 13, 18 and 21: the dominant cluster is
  // rgb(167,108,87) with a lighter rgb(190,138,120) in the light and a light
  // grey-beige joint. 250 mm brick on a 77 mm course — thirteen to the metre.
  const cw = W * (0.25 * U_PER_M) / FACADE_TILE;    // 250 mm brick
  const ch = H * (0.077 * U_PER_M) / EXT_STOREY;    // 77 mm course
  x.fillStyle = '#bfb4a3';                          // mortar
  x.fillRect(0, 0, W, H);
  for (let row = 0, py = 0; py < H + ch; row++, py += ch) {
    const off = (row % 2) * cw / 2;
    for (let px = -cw; px < W + cw; px += cw) {
      const v = 0.90 + rnd() * 0.20;
      const warm = rnd() > 0.86 ? 1.06 : 1;
      x.fillStyle = `rgb(${Math.round(178 * v * warm)},${Math.round(112 * v)},${Math.round(91 * v)})`;
      x.fillRect(px + off + 0.9, py + 0.9, cw - 1.8, ch - 1.8);
    }
  }
  x.globalAlpha = 0.07;
  for (let i = 0; i < 900; i++) {
    x.fillStyle = rnd() > 0.5 ? '#ffffff' : '#4a1d12';
    x.fillRect(rnd() * W, rnd() * H, 3, 3);
  }
  x.globalAlpha = 1;

  const ALU = '#5c6367';
  const py = H * BAND_HEAD, h = H * BAND_H;
  for (let i = 0; i < 3; i++) {
    for (let j = 0; j < 2; j++) {                 // two windows to a bay
      const w = bay * 0.375;
      const px = i * bay + bay * 0.10 + j * bay * 0.42;
      x.fillStyle = '#c9cbc8';
      x.fillRect(px - 3, py - H * 0.052, w + 6, H * 0.048);
      x.fillStyle = 'rgba(0,0,0,0.22)';
      x.fillRect(px - 3, py - H * 0.006, w + 6, H * 0.006);
      drawPane(x, px, py, w, h, rnd);
      drawFrame(x, px, py, w, h, ALU, 4.5, null);
      // dark spandrel panel below the window, down to the floor line
      const sy = py + h + 3;
      x.fillStyle = '#33383b';
      x.fillRect(px - 2, sy, w + 4, H * 0.16);
      x.fillStyle = 'rgba(255,255,255,0.05)';
      x.fillRect(px - 2, sy, w + 4, 3);
      x.fillStyle = 'rgba(0,0,0,0.35)';
      x.fillRect(px - 2, sy + H * 0.16 - 3, w + 4, 3);
    }
  }
  return finishTex(c, 1, 1, 8);
}

// ── the shopfront storey under the glass ─────────────────────────────────
// Brick piers with a tall window between each pair and a red-brown frame, the
// ground floor that carries the mathematics glass in photos 12, 17, 22 and 26.
function shopSkin() {
  const W = 1024;
  const H = Math.round(W * EXT_STOREY / FACADE_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(5150);
  const bay = W / 3;

  const cw = W * (0.25 * U_PER_M) / FACADE_TILE;
  const ch = H * (0.077 * U_PER_M) / EXT_STOREY;
  x.fillStyle = '#bfb4a3';
  x.fillRect(0, 0, W, H);
  for (let row = 0, py = 0; py < H + ch; row++, py += ch) {
    const off = (row % 2) * cw / 2;
    for (let px = -cw; px < W + cw; px += cw) {
      const v = 0.90 + rnd() * 0.20;
      x.fillStyle = `rgb(${Math.round(178 * v)},${Math.round(112 * v)},${Math.round(91 * v)})`;
      x.fillRect(px + off + 0.9, py + 0.9, cw - 1.8, ch - 1.8);
    }
  }
  const FRAME = '#a4633f';
  for (let i = 0; i < 3; i++) {
    const w = bay * 0.70, px = i * bay + bay * 0.15;
    const py = H * 0.13, h = H * 0.68;
    drawPane(x, px, py, w, h, rnd, { pale: true, shop: true });
    x.strokeStyle = FRAME; x.lineWidth = 4;
    x.beginPath();
    x.moveTo(px + w / 2, py); x.lineTo(px + w / 2, py + h);
    x.moveTo(px, py + h * 0.24); x.lineTo(px + w, py + h * 0.24);
    x.stroke();
    drawFrame(x, px, py, w, h, FRAME, 7, null);
    // granite kerb under the glazing
    x.fillStyle = '#7d7a74';
    x.fillRect(px - 6, py + h + 4, w + 12, H * 0.06);
  }
  return finishTex(c, 1, 1, 8);
}

// ── the glazed gallery and the glazed link ───────────────────────────────
// Photo 10: a curtain wall of tall panes in slim silver mullions, with a band
// of brushed spandrel panels at the floor line. Photo 27's link across the
// yard is the same wall with a deeper metal band top and bottom.
function curtainWall(opt) {
  const o = opt || {};
  const W = 1024;
  const H = Math.round(W * EXT_STOREY / FACADE_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(o.seed || 3300);
  const band = o.band === undefined ? 0.17 : o.band;
  x.fillStyle = '#b9bebd';
  x.fillRect(0, 0, W, H);
  // spandrel bands: brushed aluminium sheets with a fine horizontal grain
  const drawBand = (py, h) => {
    const g = x.createLinearGradient(0, py, 0, py + h);
    g.addColorStop(0, '#d3d7d6');
    g.addColorStop(0.45, '#bcc1c0');
    g.addColorStop(1, '#a7adab');
    x.fillStyle = g;
    x.fillRect(0, py, W, h);
    x.globalAlpha = 0.14;
    for (let i = 0; i < 120; i++) {
      x.fillStyle = rnd() > 0.5 ? '#ffffff' : '#6f7474';
      x.fillRect(rnd() * W, py + rnd() * h, 26 + rnd() * 80, 1.5);
    }
    x.globalAlpha = 1;
  };
  const bh = H * band;
  drawBand(0, bh);
  drawBand(H - bh, bh);
  // the glass between them
  const gy = bh, gh = H - bh * 2;
  const pane = W / 12;
  for (let i = 0; i < 12; i++) {
    drawPane(x, i * pane + 1.6, gy + 1.6, pane - 3.2, gh - 3.2, rnd,
             { pale: true, shop: o.bright !== false });
  }
  // mullions and a transom across the middle of the glass
  x.fillStyle = '#c6cac9';
  for (let i = 0; i <= 12; i++) x.fillRect(i * pane - 2.2, gy, 4.4, gh);
  x.fillRect(0, gy + gh * (o.transom === undefined ? 0.46 : o.transom) - 2, W, 4);
  x.fillStyle = 'rgba(255,255,255,0.35)';
  for (let i = 0; i <= 12; i++) x.fillRect(i * pane - 2.2, gy, 1.2, gh);
  return finishTex(c, 1, 1, 8);
}

// ── the projecting glazed bay ────────────────────────────────────────────
// Where level 2 oversails level 1 by only a metre or two the plans are showing
// the zinc-clad glazed boxes that step out of the north front (photo 2).
function baySkin() {
  const W = 512;
  const H = Math.round(W * EXT_STOREY / (FACADE_TILE / 2));
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(2450);
  x.fillStyle = '#c2c6c4'; x.fillRect(0, 0, W, H);
  for (let px = 0; px < W; px += 42) {
    x.fillStyle = `rgba(255,255,255,${0.05 + rnd() * 0.10})`;
    x.fillRect(px, 0, 40, H);
    x.fillStyle = 'rgba(74,80,82,0.32)'; x.fillRect(px - 1.4, 0, 2.8, H);
    x.fillStyle = 'rgba(255,255,255,0.28)'; x.fillRect(px + 1.4, 0, 1.5, H);
  }
  const py = H * 0.16, h = H * 0.60;
  drawPane(x, 10, py, W - 20, h, rnd, { pale: true });
  x.strokeStyle = '#3d4548'; x.lineWidth = 5;
  for (let i = 1; i < 4; i++) {
    const mx = 10 + (W - 20) * i / 4;
    x.beginPath(); x.moveTo(mx, py); x.lineTo(mx, py + h); x.stroke();
  }
  x.beginPath(); x.moveTo(10, py + h * 0.62); x.lineTo(W - 10, py + h * 0.62); x.stroke();
  drawFrame(x, 10, py, W - 20, h, '#4a5254', 7, null);
  return finishTex(c, 1, 1, 8);
}

// ── the mathematics glass ────────────────────────────────────────────────
// The signature volume: a frosted screen printed with faded formulas,
// geometric diagrams and a field of hexagons.
function mathGlassTexture() {
  const W = 2048, H = Math.round(W * EXT_STOREY / GLASS_TILE);
  const c = texCanvas(W, H);
  const x = c.getContext('2d');
  const rnd = mulberry(31337);

  const g = x.createLinearGradient(0, 0, 0, H);
  g.addColorStop(0, '#dbe6e8');
  g.addColorStop(0.40, '#c1d5da');
  g.addColorStop(0.58, '#9ab6c8');
  g.addColorStop(0.80, '#b5ccd4');
  g.addColorStop(1, '#cdddde');
  x.fillStyle = g;
  x.fillRect(0, 0, W, H);
  // the darker band where the room behind shows through the clear vision strip
  x.fillStyle = 'rgba(96,120,140,0.22)';
  x.fillRect(0, H * 0.44, W, H * 0.20);

  // Anything near an edge is drawn three times so the tile is seamless.
  function wrapped(fn) {
    for (const off of [-W, 0, W]) {
      x.save(); x.translate(off, 0); fn(x); x.restore();
    }
  }

  wrapped(cx => {
    cx.fillStyle = 'rgba(120,138,150,0.14)';
    for (let i = 0; i < 26; i++) {
      const px = rnd() * W, ph = 40 + rnd() * 90;
      cx.beginPath();
      cx.ellipse(px, H * 0.62 - ph * 0.2, 11 + rnd() * 7, ph * 0.4, 0, 0, Math.PI * 2);
      cx.fill();
    }
  });

  function hexField(cx0, cy0, cols, rows, R, alpha) {
    wrapped(cx => {
      cx.lineWidth = 2.2;
      for (let r = 0; r < rows; r++) {
        for (let q = 0; q < cols; q++) {
          const px = cx0 + q * R * 1.5;
          const py = cy0 + r * R * Math.sqrt(3) + (q % 2) * R * Math.sqrt(3) / 2;
          const a = alpha * (0.35 + rnd() * 0.85);
          cx.strokeStyle = `rgba(104,120,134,${a})`;
          cx.beginPath();
          for (let k = 0; k < 6; k++) {
            const ang = Math.PI / 180 * (60 * k);
            const vx = px + R * Math.cos(ang), vy = py + R * Math.sin(ang);
            k ? cx.lineTo(vx, vy) : cx.moveTo(vx, vy);
          }
          cx.closePath();
          cx.stroke();
          if (rnd() > 0.86) { cx.fillStyle = `rgba(148,166,180,${a * 0.3})`; cx.fill(); }
        }
      }
    });
  }
  hexField(W * 0.50, -H * 0.10, 15, 8, H * 0.105, 1.55);
  hexField(W * 0.04, H * 0.30, 9, 5, H * 0.058, 0.95);

  function globe(px, py, r, a) {
    wrapped(cx => {
      cx.strokeStyle = `rgba(92,110,128,${a})`;
      cx.lineWidth = 1.7;
      cx.beginPath(); cx.arc(px, py, r, 0, Math.PI * 2); cx.stroke();
      for (let i = 1; i < 6; i++) {
        const rr2 = r * Math.cos((i / 6) * Math.PI / 2 + 0.0001);
        cx.beginPath(); cx.ellipse(px, py, r, r * Math.abs(1 - i / 3), 0, 0, Math.PI * 2); cx.stroke();
        cx.beginPath(); cx.ellipse(px, py, Math.abs(rr2), r, 0, 0, Math.PI * 2); cx.stroke();
      }
    });
  }
  function dial(px, py, r, a) {
    wrapped(cx => {
      cx.strokeStyle = `rgba(92,110,128,${a})`;
      cx.lineWidth = 1.6;
      cx.beginPath(); cx.arc(px, py, r, 0, Math.PI * 2); cx.stroke();
      cx.beginPath(); cx.arc(px, py, r * 0.82, 0, Math.PI * 2); cx.stroke();
      for (let i = 0; i < 48; i++) {
        const ang = i / 48 * Math.PI * 2;
        const l = (i % 4 === 0) ? r * 0.16 : r * 0.07;
        cx.beginPath();
        cx.moveTo(px + Math.cos(ang) * r * 0.82, py + Math.sin(ang) * r * 0.82);
        cx.lineTo(px + Math.cos(ang) * (r * 0.82 + l), py + Math.sin(ang) * (r * 0.82 + l));
        cx.stroke();
      }
    });
  }
  function waveform(px, py, w, amp, a) {
    wrapped(cx => {
      cx.strokeStyle = `rgba(92,110,128,${a})`;
      cx.lineWidth = 1.8;
      cx.beginPath();
      for (let i = 0; i <= w; i += 3) {
        const t = i / w * Math.PI * 4;
        const vy = py - Math.sin(t) * amp * Math.exp(-i / w * 0.8);
        i ? cx.lineTo(px + i, vy) : cx.moveTo(px + i, vy);
      }
      cx.stroke();
      cx.lineWidth = 1.1;
      cx.beginPath(); cx.moveTo(px - 8, py); cx.lineTo(px + w + 8, py); cx.stroke();
    });
  }
  function inscribed(px, py, r, sides, a) {
    wrapped(cx => {
      cx.strokeStyle = `rgba(92,110,128,${a})`;
      cx.lineWidth = 1.6;
      cx.beginPath(); cx.arc(px, py, r, 0, Math.PI * 2); cx.stroke();
      cx.beginPath();
      for (let k = 0; k <= sides; k++) {
        const ang = k / sides * Math.PI * 2 - Math.PI / 2;
        const vx = px + Math.cos(ang) * r, vy = py + Math.sin(ang) * r;
        k ? cx.lineTo(vx, vy) : cx.moveTo(vx, vy);
      }
      cx.stroke();
    });
  }
  function axes3d(px, py, s, a) {
    wrapped(cx => {
      cx.strokeStyle = `rgba(92,110,128,${a})`;
      cx.lineWidth = 1.6;
      for (const [dx, dy] of [[0, -s], [s * 0.95, s * 0.42], [-s * 0.95, s * 0.42]]) {
        cx.beginPath(); cx.moveTo(px, py); cx.lineTo(px + dx, py + dy); cx.stroke();
      }
      cx.lineWidth = 1.3;
      cx.beginPath();
      for (let i = 0; i <= 60; i++) {
        const t = i / 60;
        const vx = px - s * 0.9 + t * s * 1.8;
        const vy = py + s * 0.1 - Math.sin(t * Math.PI * 2) * s * 0.34;
        i ? cx.lineTo(vx, vy) : cx.moveTo(vx, vy);
      }
      cx.stroke();
    });
  }

  globe(W * 0.21, H * 0.36, H * 0.24, 0.5);
  globe(W * 0.78, H * 0.60, H * 0.15, 0.36);
  dial(W * 0.905, H * 0.28, H * 0.18, 0.44);
  waveform(W * 0.34, H * 0.72, W * 0.14, H * 0.12, 0.44);
  inscribed(W * 0.07, H * 0.22, H * 0.18, 7, 0.4);
  axes3d(W * 0.65, H * 0.44, H * 0.22, 0.36);

  const formulas = [
    'e^{iπ} + 1 = 0', '∫ f(x) dx = F(b) − F(a)', '∇ × E = − ∂B/∂t',
    'Σ 1/n² = π²/6', 'lim (1 + 1/n)^n = e', 'a² + b² = c²',
    'ψ(x,t) = A e^{i(kx − ωt)}', 'det(A − λI) = 0', 'P(A|B) = P(B|A)P(A)/P(B)',
    'dS ≥ δQ/T', 'sin²θ + cos²θ = 1', '∂²u/∂t² = c² ∇²u',
    'F = G m₁m₂ / r²', '∮ B · dl = μ₀ I', 'f(x) = Σ aₙ (x − x₀)ⁿ',
    'λ = h / p', 'E = mc²', '∇ · D = ρ', 'y′′ + ω² y = 0',
    'log(xy) = log x + log y', 'K(x,y) = exp(−‖x−y‖² / 2σ²)',
    'arg min ‖Ax − b‖²', 'ζ(s) = Σ n^{−s}', 'τ = 2π', '√2 ≈ 1.41421',
    'π = 3.14159265', 'φ = (1 + √5)/2', 'i² = −1', 'n! = n(n−1)!',
    '⟨ψ|φ⟩', 'Δx Δp ≥ ħ/2', 'x = (−b ± √(b²−4ac)) / 2a',
    'P[a ≤ X ≤ b] = ∫ W(x) dx', 'S = π r²', 'cos = B/A',
  ];
  const fonts = ['italic 30px Georgia, "Times New Roman", serif',
                 'italic 22px Georgia, "Times New Roman", serif',
                 'italic 40px Georgia, "Times New Roman", serif',
                 'italic 26px Georgia, "Times New Roman", serif'];
  for (let i = 0; i < 170; i++) {
    const s = formulas[Math.floor(rnd() * formulas.length)];
    const px = rnd() * W, py = 40 + rnd() * (H - 70);
    const a = 0.22 + rnd() * 0.42;
    const rot = (rnd() - 0.5) * 0.05;
    wrapped(cx => {
      cx.save();
      cx.translate(px, py); cx.rotate(rot);
      cx.font = fonts[Math.floor(rnd() * fonts.length)];
      cx.fillStyle = `rgba(78,96,114,${a})`;
      cx.fillText(s, 0, 0);
      cx.restore();
    });
  }
  for (const [s, px, py, sz, a] of [['751', W * 0.30, H * 0.24, 60, 0.32],
                                    ['901', W * 0.62, H * 0.42, 58, 0.30],
                                    ['2,7', W * 0.845, H * 0.80, 52, 0.26],
                                    ['3,14', W * 0.545, H * 0.90, 48, 0.26]]) {
    wrapped(cx => {
      cx.font = `italic ${sz}px Georgia, serif`;
      cx.fillStyle = `rgba(82,100,118,${a})`;
      cx.fillText(s, px, py);
    });
  }

  x.globalAlpha = 0.05;
  for (let i = 0; i < 5000; i++) {
    x.fillStyle = rnd() > 0.5 ? '#fff' : '#8fa2b0';
    x.fillRect(rnd() * W, rnd() * H, 2, 2);
  }
  x.globalAlpha = 1;
  // dark aluminium mullions, one every 1.4 m, plus a transom at mid height
  const bays = Math.round(GLASS_TILE / (1.35 * U_PER_M));
  for (let i = 0; i < bays; i++) {
    const px = i * W / bays;
    x.fillStyle = 'rgba(38,46,50,0.88)';
    x.fillRect(px - 4, 0, 8, H);
    x.fillStyle = 'rgba(255,255,255,0.20)';
    x.fillRect(px + 4, 0, 2.5, H);
  }
  for (const v of [0.30, 0.56, 0.82]) {
    x.fillStyle = 'rgba(38,46,50,0.80)';
    x.fillRect(0, H * v, W, 7);
    x.fillStyle = 'rgba(255,255,255,0.16)';
    x.fillRect(0, H * v + 7, W, 2);
  }
  x.fillStyle = 'rgba(214,218,216,0.98)'; x.fillRect(0, 0, W, 12);
  x.fillStyle = 'rgba(120,138,152,0.55)'; x.fillRect(0, H - 10, W, 10);
  return finishTex(c, 1, 1, 8);
}

// ── parapet, roof, zinc ──────────────────────────────────────────────────
function parapetTexture() {
  const c = texCanvas(128, 128);
  const x = c.getContext('2d');
  const g = x.createLinearGradient(0, 0, 0, 128);
  g.addColorStop(0, '#e6e7e3');
  g.addColorStop(0.16, '#d3d5d1');
  g.addColorStop(0.9, '#bcbeb9');
  g.addColorStop(1, '#93958f');
  x.fillStyle = g; x.fillRect(0, 0, 128, 128);
  const rnd = mulberry(311);
  x.globalAlpha = 0.10;
  for (let i = 0; i < 260; i++) {
    x.fillStyle = rnd() > 0.5 ? '#fff' : '#5c5e5a';
    x.fillRect(rnd() * 128, rnd() * 128, 2, 6 + rnd() * 20);
  }
  return finishTex(c, 1, 1, 4);
}

// Brushed aluminium sheet — the skin of the glazed links, and of nothing else.
// It is deliberately a *different* texture from the zinc: the zinc's panel
// joints every sixty-four pixels are what makes a large tray read as a run of
// precast concrete panels, which is the owner's "not concrete". This one is
// pale, finely ribbed and highly metallic, so a bridge fascia reads as a
// pressed aluminium edge whatever size it is cut to.
function alumTexture() {
  const c = texCanvas(256, 128);
  const x = c.getContext('2d');
  x.fillStyle = '#d5dade'; x.fillRect(0, 0, 256, 128);
  const rnd = mulberry(1717);
  for (let px = 0; px < 256; px += 8) {
    x.fillStyle = `rgba(255,255,255,${0.16 + rnd() * 0.12})`;
    x.fillRect(px, 0, 3.2, 128);
    x.fillStyle = 'rgba(120,130,136,0.20)';
    x.fillRect(px + 4.4, 0, 1.2, 128);
  }
  x.globalAlpha = 0.08;
  for (let i = 0; i < 220; i++) {
    x.fillStyle = rnd() > 0.5 ? '#fff' : '#8d979c';
    x.fillRect(rnd() * 256, rnd() * 128, 1.5, 4 + rnd() * 14);
  }
  return finishTex(c, 1, 1, 3);
}

function zincTexture() {
  const c = texCanvas(512, 256);
  const x = c.getContext('2d');
  x.fillStyle = '#bcc0bf'; x.fillRect(0, 0, 512, 256);
  const rnd = mulberry(909);
  for (let px = 0; px < 512; px += 64) {
    x.fillStyle = `rgba(255,255,255,${0.05 + rnd() * 0.09})`;
    x.fillRect(px, 0, 62, 256);
    x.fillStyle = 'rgba(70,76,78,0.35)';
    x.fillRect(px - 1.5, 0, 3, 256);
    x.fillStyle = 'rgba(255,255,255,0.30)';
    x.fillRect(px + 1.5, 0, 1.6, 256);
  }
  x.globalAlpha = 0.10;
  for (let i = 0; i < 500; i++) {
    x.fillStyle = rnd() > 0.5 ? '#fff' : '#6a6f70';
    x.fillRect(rnd() * 512, rnd() * 256, 3, 3 + rnd() * 22);
  }
  return finishTex(c, 1, 1, 6);
}

function roofTexture() {
  const c = texCanvas(256, 256);
  const x = c.getContext('2d');
  x.fillStyle = '#6e7274'; x.fillRect(0, 0, 256, 256);
  const rnd = mulberry(555);
  for (let i = 0; i < 2400; i++) {
    x.fillStyle = rnd() > 0.5 ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.07)';
    x.fillRect(rnd() * 256, rnd() * 256, 3, 3);
  }
  x.strokeStyle = 'rgba(0,0,0,0.12)'; x.lineWidth = 2;
  for (let i = 0; i < 256; i += 32) {
    x.beginPath(); x.moveTo(i, 0); x.lineTo(i, 256); x.stroke();
  }
  return finishTex(c, 26, 26, 4);
}

// The blue information pylon that stands beside every entrance (photos 5, 7,
// 14 and 15): the university mark at the top, the house and street under it,
// and a block of small print below.
function signTexture() {
  const c = texCanvas(256, 1024);
  const x = c.getContext('2d');
  x.fillStyle = '#1d3f7a'; x.fillRect(0, 0, 256, 1024);
  const g = x.createLinearGradient(0, 0, 256, 0);
  g.addColorStop(0, 'rgba(255,255,255,0.14)');
  g.addColorStop(1, 'rgba(0,0,0,0.20)');
  x.fillStyle = g; x.fillRect(0, 0, 256, 1024);
  x.fillStyle = '#eef2f6';
  x.textAlign = 'center';
  x.font = 'bold 96px Georgia, serif';
  x.fillText('L', 128, 130);
  x.font = '26px "Segoe UI", Helvetica, Arial, sans-serif';
  x.fillText('Hus A', 128, 210);
  x.font = '20px "Segoe UI", Helvetica, Arial, sans-serif';
  x.fillText('Universitetsvägen 1', 128, 246);
  x.globalAlpha = 0.75;
  const rnd = mulberry(1717);
  for (let i = 0; i < 26; i++) {
    x.fillRect(46, 320 + i * 22, 60 + rnd() * 130, 7);
  }
  x.globalAlpha = 1;
  return finishTex(c, 1, 1, 8);
}

// A pylon standing on the pavement beside the doors.
//
// The sign face is a plane of its own rather than a side of a box: a box maps
// its two long faces with opposite v, so whichever way the pylon was turned one
// side of it always read upside down — which is what the owner's review caught
// on the south front. A plane laid in the XZ plane carries the artwork the one
// way up, and the far side is the same plane turned about its own axis.
function buildPylonSign(frame, ox, oy) {
  const H = EXT_STOREY * 0.82, W = 3.4, D = 0.9;
  const body = new THREE.Mesh(new THREE.BoxGeometry(W, D, H), signBodyMat);
  body.castShadow = true;
  body.position.set(ox, oy, H / 2 + 1.0);
  frame.add(body);
  for (const s of [-1, 1]) {
    const face = new THREE.Mesh(new THREE.PlaneGeometry(W - 0.12, H - 0.12), signMat);
    face.rotation.x = Math.PI / 2;
    if (s > 0) face.rotation.y = Math.PI;
    face.position.set(ox, oy + s * (D / 2 + 0.02), H / 2 + 1.0);
    frame.add(face);
  }
  const foot = new THREE.Mesh(new THREE.BoxGeometry(W + 1.2, 2.2, 1.0), concreteMat);
  foot.position.set(ox, oy, 0.5);
  frame.add(foot);
}

// ── materials ────────────────────────────────────────────────────────────
let skinMats = {};
let roofMat, trimMat, signMat, signBodyMat, doorMat, soffitMat, zincMat, concreteMat,
    pipeMat, steelMat, frameMat, roomGlassMat, linkAlumMat, linkGlassMat, linkMullMat,
    linkGlazeMat, linkPanelMat, linkFasciaMat, linkRoofMat;

function buildShellMaterials() {
  const mk = (t, rough, metal) => new THREE.MeshStandardMaterial({
    map: t, roughness: rough, metalness: metal, color: 0xffffff,
  });
  skinMats = {
    metal1:  mk(metalSkin(1), 0.60, 0.18),
    metal2:  mk(metalSkin(3), 0.60, 0.18),
    metalTop: mk(metalSkin(2), 0.60, 0.18),
    brick1:  mk(brickSkin(1), 0.93, 0.02),
    brick2:  mk(brickSkin(2), 0.93, 0.02),
    plinth:  mk(plinthSkin(), 0.90, 0.02),
    shop:    Object.assign(mk(shopSkin(), 0.88, 0.03),
                          { emissive: new THREE.Color(0x4a453c), emissiveIntensity: 1 }),
    bay:     mk(baySkin(), 0.34, 0.55),
    gallery: mk(curtainWall({ seed: 3300, band: 0.15 }), 0.22, 0.42),
    link:    mk(curtainWall({ seed: 4400, band: 0.27, transom: 0.5 }), 0.24, 0.46),
    glass:   new THREE.MeshStandardMaterial({
      map: mathGlassTexture(), roughness: 0.10, metalness: 0.14,
      transparent: true, opacity: 0.90, side: THREE.DoubleSide,
      emissive: 0xbdd2e0, emissiveIntensity: 0.02,
    }),
  };
  roofMat = mk(roofTexture(), 0.95, 0.0);
  trimMat = mk(parapetTexture(), 0.50, 0.18);
  // The coping is the only thing that stands *above* the roof plate, so it is
  // the only thing that tells the eye where a roof stops. Built one-sided it is
  // visible from outside and invisible from the roof itself: look across a
  // large flat roof from any high oblique and its far edge is a razor line with
  // the ground showing beyond it — a grey slab lying over the site with nothing
  // holding it up, which is exactly the "the building is really broken" of the
  // owner's aerial. The elevation under that edge faces away and cannot help.
  // Two-sided, every roof reads as a tray with an upstand all round it, from
  // above as well as from the ground.
  trimMat.side = THREE.DoubleSide;
  // Behind the plaza link's glass there must be no brick: the link is a glazed
  // volume, not glazing hung on a brick wall. The wall that closes the space
  // behind it becomes a dark interior surface, so the glass reads as depth
  // instead of a pane laid on masonry — and there is still no hole to see
  // through into the sky.
  // "glass all the way to the ground, on both sides. So no bricks on the
  // skywalk": the far side of the link is its second glazed wall. Kept opaque
  // on purpose — a truly transparent inner pane would look straight through
  // the building and out at the sky, since nothing inside is drawn in the
  // exterior view. Dark, smooth and a little metallic reads as glazing with a
  // dim interior behind it, which is what the photograph shows.
  skinMats.linkback = new THREE.MeshStandardMaterial({
    color: 0x39454f, roughness: 0.12, metalness: 0.58,
    emissive: 0x121a22, emissiveIntensity: 1,
  });
  skinMats.trim = trimMat;
  skinMats.trimdk = new THREE.MeshStandardMaterial({
    map: parapetTexture(), color: 0x8d8880, roughness: 0.55, metalness: 0.10,
    side: THREE.DoubleSide,          // an upstand, seen from the roof as well
  });
  zincMat = mk(zincTexture(), 0.36, 0.55);
  // ── the skywalks' own materials ────────────────────────────────────────
  // "Made of glass/aluminium. Not concrete." The bridges are the only things
  // on this building that wear these three, and they are handed to the meshes
  // that buildSkywalk/buildWestSkywalk create, by name, at construction time.
  // Nothing looks a bridge up by where it is, so no wall can ever pick them up
  // and no shift in the ring geometry can ever make a bridge lose them.
  // Metalness without an environment map only makes a surface dark — the sheen
  // has to come from the texture and a light colour, not from the number.
  linkAlumMat = mk(alumTexture(), 0.32, 0.42);
  Object.assign(linkAlumMat, {
    emissive: new THREE.Color(0x9aa4a9), emissiveIntensity: 0.16,
  });
  linkGlassMat = new THREE.MeshStandardMaterial({
    color: 0xbcd2dc, roughness: 0.06, metalness: 0.22,
    emissive: 0x9fbcc9, emissiveIntensity: 0.30,
    transparent: true, opacity: 0.52, side: THREE.DoubleSide,
  });
  linkMullMat = new THREE.MeshStandardMaterial({
    color: 0xc6ced2, roughness: 0.34, metalness: 0.45,
    emissive: 0x8f989c, emissiveIntensity: 0.14,
  });
  // The plaza link is a *wall* of glass, not a bridge crossing thin air: what is
  // behind it is the brick of the building, and photo 10 reads that brick and
  // the stair inside straight through the glazing. So it takes a clearer glass
  // than the two bridges do — theirs is read against the sky, and at this
  // opacity a bridge would vanish.
  linkGlazeMat = new THREE.MeshStandardMaterial({
    color: 0xb6cbd3, roughness: 0.05, metalness: 0.20,
    emissive: 0x86a6b4, emissiveIntensity: 0.07,
    transparent: true, opacity: 0.28, side: THREE.DoubleSide,
    depthWrite: false,
  });
  // The spandrel panels: pressed sheet, a shade darker than the mullions so the
  // band reads as one line of metal across the glass and not as a white stripe.
  linkPanelMat = mk(alumTexture(), 0.40, 0.26);
  Object.assign(linkPanelMat, {
    color: new THREE.Color(0xd0d5d6),
    emissive: new THREE.Color(0x97a0a4), emissiveIntensity: 0.16,
  });
  // The eave. In the photograph the roof edge is one dark line a hand deep with
  // a pale soffit under it; anything heavier is the precast the owner struck.
  linkFasciaMat = new THREE.MeshStandardMaterial({
    color: 0x6f7679, roughness: 0.52, metalness: 0.35,
  });
  // The roof sheet. Not the bright aluminium of the fascia and the spandrels:
  // seen from above it is the only large flat surface the link has, and in the
  // pale metal it read as a white plank lying across the yard — the same
  // mistake, in plan, that the slab skin made in elevation. It is a grey sheet
  // roof, a shade lighter than the flat roofs it runs into.
  linkRoofMat = mk(alumTexture(), 0.62, 0.20);
  linkRoofMat.color = new THREE.Color(0x8e9597);
  signMat = new THREE.MeshStandardMaterial({
    map: signTexture(), roughness: 0.55, emissive: 0x2a4a86, emissiveIntensity: 0.12,
  });
  signBodyMat = new THREE.MeshStandardMaterial({ color: 0x1d3f7a, roughness: 0.55 });
  doorMat = new THREE.MeshStandardMaterial({
    color: 0x5f767c, roughness: 0.16, metalness: 0.35,
    emissive: 0x1e2a2b, emissiveIntensity: 1.0,
    transparent: true, opacity: 0.86,
  });
  // The glass of the hung-out rooms: pale and reflective, never the bottle
  // green a door leaf is (photos 1 and 27).
  roomGlassMat = new THREE.MeshStandardMaterial({
    color: 0xa9bcc4, roughness: 0.07, metalness: 0.22,
    emissive: 0x8fa6b2, emissiveIntensity: 0.25,
    transparent: true, opacity: 0.68, side: THREE.DoubleSide,
  });
  frameMat = new THREE.MeshStandardMaterial({ color: 0x2c4a3c, roughness: 0.5, metalness: 0.2 });
  soffitMat = new THREE.MeshStandardMaterial({
    color: 0xd7d9da, roughness: 0.72, emissive: 0xfff0d4, emissiveIntensity: 0.22,
  });
  concreteMat = new THREE.MeshStandardMaterial({ color: 0xb6b3ad, roughness: 0.82 });
  pipeMat = new THREE.MeshStandardMaterial({ color: 0xa8adab, roughness: 0.48, metalness: 0.50 });
  steelMat = new THREE.MeshStandardMaterial({ color: 0x9aa0a2, roughness: 0.44, metalness: 0.60 });
}

// ── the glazed pieces the survey shows and the plan cannot ───────────────
// The plaza front carries a glazed gallery either side of the main entrance
// (photo 10), and the passage between the south wings is bridged by a glazed
// link — the skywalk of photo 27, which the plan draws as an ordinary
// first-floor slab over an open ground-floor passage.
//
// *One* storey of it. This band used to run from storey 1 to the parapet, and
// two storeys of aluminium-spandrel curtain wall the whole width of the plaza
// front is the biggest single piece of metal on the building — it is what fills
// the plaza in every render taken from the yard, and it is what "no aluminium
// on the main building, only on the skywalk" is about. Photo 10 is plain: the
// plaza front is brick with punched windows, and the glazing is one band at
// first-floor level set into it, over brick and under brick.
const GALLERY = { x0: 42, x1: 134, y0: -8, y1: 28, cx: 88, half: 17 };
function inGallery(mx, my, ny) {
  return ny < -0.5 && mx > GALLERY.x0 && mx < GALLERY.x1 &&
         my > GALLERY.y0 && my < GALLERY.y1 &&
         Math.abs(mx - GALLERY.cx) > GALLERY.half;
}
// ── the glazed link on the plaza (photo 10) ──────────────────────────────
// The wall that closes the plaza on its east side is the long low glazed link
// of photo 10 — the piece the owner has now sent back four times.
//
// Three passes tried to say it with a *skin*: classify the wall runs down x 129
// as curtain wall and let the facade generator paint them. That cannot say what
// the photograph says. A skinned wall is the height of its storey, it is capped
// by the same coping as every other wall, and its glass is a picture printed on
// an opaque quad — so what came out was a flat-topped two-storey slab of pale
// grey the whole length of the plaza, indistinguishable from precast. "Made of
// glass/aluminium. Not concrete."
//
// What the photograph shows is a *structure*, and it is now built as one, by
// buildPlazaLink below: real aluminium mullions at two-and-a-half metre centres
// running the full height, one band of pressed spandrel panels at the first
// floor, clear glass between them with the brick of the building read straight
// through it, and — the thing no skin can carry — a shallow monopitch roof on a
// thin fascia over a pale soffit, low where the link meets the brick and rising
// away from it. The wall behind is the brick the plan draws, which is exactly
// what you see through the glass in the photograph.
//
// The zone below is *geometry only*: it says where the link stands, so the
// storey-1 coping can be left off under its eave. No material is chosen by it.
//
// And it is glazed on *both* faces. The link is six metres of gallery between
// the plaza and the east lawn, and the outer ring draws both its walls: the
// plaza face down x 129 and the courtyard face back up x 135. Building the
// screen on one of them only left the other reading as brick with a glass
// roof over it — "it needs the same glass structure on the other side" — so
// the same screen is instanced on both, and one sheet of roof spans between
// them so the two read as one volume and not as two loose panes.
const PLAZA_LINK = {
  // the plaza face, taken off the outer ring: north end at the three-storey
  // block, south end at the brick corner that closes the plaza
  face: [[129.7, 9.5], [128.7, -31.5]],
  // the courtyard face, the same ring's return: south end where the link runs
  // into the south-east block, north end at the three-storey block. Wound so
  // that a→b keeps the wall's outward normal on the same hand as the plaza
  // face's, which is what lets one function build either screen.
  back: [[135.7, -24.5], [134.7, 9.5]],
  out: 1.15,            // how far the screen stands proud of the wall
  over: 1.15,           // how far the eave oversails the glass
  parapet: { x0: 122, x1: 137.5, y0: -34, y1: 11 },
};
// Wall runs that stand behind the link's glass — on either face now, since
// both are glazed. Deliberately narrower than the parapet box above: only the
// two runs of the link's own gallery qualify, so no neighbouring elevation
// loses its brick.
function behindPlazaLink(storey, mx, my, nx) {
  if (storey < 0) return false;                // ground storey included
  if (my > 10.5) return false;
  if (nx < -0.5) return mx > 126 && mx < 133 && my > -33;    // the plaza face
  if (nx > 0.5) return mx > 133 && mx < 138 && my > -26.5;   // the courtyard face
  return false;
}
function underPlazaLink(storey, mx, my) {
  if (storey < 1) return false;
  const p = PLAZA_LINK.parapet;
  return mx > p.x0 && mx < p.x1 && my > p.y0 && my < p.y1;
}
// The west elevation is two buildings, not one: the brick block that carries
// the west entrance, and the red sheet-metal block north of it. The seam
// between them is at y 10 — it is where the base skin changes — and the plan
// itself already draws the gap: north of y 12 the upper storeys stand on
// x -188, between y 0 and 10 they are set six metres back.
//
// What crosses that gap is a *small* skywalk and nothing more. A previous pass
// read the owner's note as a whole glazed link and built a two-storey
// aluminium volume the width of four bays across this elevation; his review
// struck it out — "there is no such big glassstructure" — so the skin band that
// carried it is gone and the elevation is brick and sheet metal again.
const WEST_LINK = { x0: -210, x1: -168, y0: 10, y1: 26 };
// The gap the bridge crosses, measured off the level-1/2 masks: north of y 12
// the upper storeys stand on x -188, south of y 0 they stand on x -186, and
// between the two the plan cuts them back to x -182. So the gap in the west
// elevation runs north-south for a dozen metres, and a bridge across it runs
// the same way — along y, near the wall line, not out of the back of the
// recess. A pass that ran it the other way stood a glass box on a stalk in
// mid-air, which is what the owner struck out twice.
const WEST_BRIDGE = { y0: -0.5, y1: 12.5, xFace: -187.4, xDeep: -183.0 };
// The extent of the south bridge. This is *geometry* and nothing else: it says
// where buildSkywalk puts its box, and no material anywhere is chosen by asking
// whether a point falls inside it.
const SKYWALK = { x0: -92, y0: -158, x1: -66, y1: -141 };
// The south-east corner used to be skinned in standing-seam aluminium here, on
// the reading that the photograph's metal was cladding. It is not: in the
// photograph the metal is the *collar of one glazed room*, a fascia and an
// apron a metre and a half deep either side of a single sheet of glass, and
// every wall around it is brick. Skinning two storeys of both elevations in it
// is what the owner struck out — "no aluminium on the main building, only on
// the skywalk" — so the skin is gone and the collar belongs to the box that
// buildEntrance hangs over the doors.
//
// There used to be an `inSkywalk(mx, my)` here that answered "is this wall on
// the bridge?" from a bounding box. Two passes were lost to it: first it said
// yes to a dozen metres of the wings' own brick and leaked aluminium onto the
// main building, then — once the ring geometry near the gap moved — it stopped
// saying yes to anything and the bridge silently fell back to the default
// skin and came out looking like concrete. A test that can miss without
// telling anyone is the wrong instrument. The bridges now carry their own
// materials, handed to their own meshes in buildSkywalk and buildWestSkywalk,
// and there is no zone test left to go wrong.

// ── which skin goes where ────────────────────────────────────────────────
// The main block — everything north of the inner yards and west of the east
// block — keeps its original sheet metal. The east block and every wing to the
// south are the brick building. Under a cantilevered level-2 glass box the
// storey below is brick too, which is what photos 17, 22 and 26 show.
function baseSkinAt(mx, my) {
  // Everything up to the west link is the brick building. North of the link the
  // sheet-metal block begins: photo 19 shows brick right up to the aluminium
  // joint, which is what the owner's "wrong texture" note is about.
  if (my < WEST_LINK.y0) return 'brick';              // south wings and the west entrance block
  if (mx > 138) return 'brick';                       // the east block
  // The plaza front — the south face of the north block — is brick as well:
  // photo 10 shows brick either side of the entrance, with the glazed gallery
  // set into it. Only the west and north elevations are sheet metal.
  if (my < 30 && mx > -60) return 'brick';
  return 'metal';
}

// Which storey the ground meets. On the sheet-metal block the storey standing
// at grade is always the painted brick base course — level 0 on the west slope
// where the basement surfaces, level 1 on the north where it is buried. That
// one rule is what makes photo 2 and photo 8 the same building.
function gradeStorey(x, y) {
  return Math.round(groundZ(x, y) / EXT_STOREY);
}

function skinFor(storey, mx, my, nx, ny) {
  const base = baseSkinAt(mx, my);
  // The skywalk between the south wings is its own object — a zinc tray, a head
  // and the blades between them, built by buildSkywalk. It is *not* a skin the
  // elevations wear. Classifying every wall run whose midpoint fell in the
  // bridge's bounding box as 'link' clad the two wings' own brick returns in
  // the bridge's aluminium for a dozen metres either side of the gap, which is
  // the owner's "the aluminium has leaked onto the main building". The box was
  // never a description of the wall; it only ever described the crossing.
  if (behindPlazaLink(storey, mx, my, nx)) return 'linkback';
  if (storey === 1 && inGallery(mx, my, ny)) return 'gallery';
  if (storey === 2) {
    const m1 = levelMasks[1];
    if (my > 138 && m1) {
      // a deep oversail is a mathematics-glass box; a shallow one is one of the
      // zinc-clad glazed bays that step out of the north front
      if (!m1.solid(mx - nx * 5.4, my - ny * 5.4)) return 'glass';
      if (!m1.solid(mx - nx * 2.6, my - ny * 2.6)) return 'bay';
    }
    if (base === 'brick') return 'brick2';
    const up = 2 - gradeStorey(mx, my);
    return up <= 0 ? 'plinth' : up >= 2 ? 'metalTop' : 'metal2';
  }
  if (storey === 1) {
    const m1 = levelMasks[1], m2 = levelMasks[2];
    if (my > 138 && m1 && m2) {
      for (const d of [5, 9]) {
        const ox = mx + nx * d, oy = my + ny * d;
        if (m2.solid(ox, oy) && !m1.solid(ox, oy)) return 'shop';
      }
    }
    if (base === 'brick') return 'brick1';
    return gradeStorey(mx, my) >= 1 ? 'plinth' : 'metal1';
  }
  return base === 'brick' ? 'brick1' : 'plinth';
}

// ── wall assembly ────────────────────────────────────────────────────────
// One vertex buffer per skin for the whole building: six draw calls for every
// elevation, which is what keeps this smooth on integrated graphics.
function makeSink() {
  const s = {};
  return {
    push(skin, ax, ay, az, bx, by, bz, cx2, cy2, cz, nx, ny, uva, uvb, uvc) {
      let b = s[skin];
      if (!b) b = s[skin] = { p: [], n: [], u: [] };
      b.p.push(ax, ay, az, bx, by, bz, cx2, cy2, cz);
      b.n.push(nx, ny, 0, nx, ny, 0, nx, ny, 0);
      b.u.push(uva[0], uva[1], uvb[0], uvb[1], uvc[0], uvc[1]);
    },
    quad(skin, a, b, c, d, nx, ny, u0, u1, v0, v1) {
      this.push(skin, a[0], a[1], a[2], b[0], b[1], b[2], c[0], c[1], c[2],
                nx, ny, [u0, v0], [u1, v0], [u1, v1]);
      this.push(skin, a[0], a[1], a[2], c[0], c[1], c[2], d[0], d[1], d[2],
                nx, ny, [u0, v0], [u1, v1], [u0, v1]);
    },
    buckets: s,
  };
}

function sinkToMeshes(sink, group, tileW, cast) {
  for (const key of Object.keys(sink.buckets)) {
    const b = sink.buckets[key];
    if (!b.p.length) continue;
    const skin = key.split('#')[0];
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(b.p, 3));
    g.setAttribute('normal', new THREE.Float32BufferAttribute(b.n, 3));
    g.setAttribute('uv', new THREE.Float32BufferAttribute(b.u, 2));
    g.computeBoundingSphere();
    const m = new THREE.Mesh(g, skinMats[skin] || trimMat);
    m.castShadow = !!cast;
    m.receiveShadow = true;
    m.name = 'facade-' + key;
    (sectionOf(key) || group).add(m);
  }
}

// ── the elevation, cut into sections that can move on their own ──────────
// The walls are bucketed by which way they face and which storey they are on,
// so the transition can take the building apart a section at a time instead of
// dissolving it all at once. Each section is a group whose origin is the
// building's own origin, so nothing about the geometry changes: at rest every
// section sits at (0,0,0) and the elevation is exactly what it was.
const wallSections = [];
const _sectionByKey = new Map();
function sectionOf(key) {
  const parts = key.split('#');
  if (parts.length < 3) return null;
  const face = +parts[1], storey = +parts[2];
  const id = face + ':' + storey;
  let s = _sectionByKey.get(id);
  if (!s) {
    const grp = new THREE.Group();
    grp.name = 'wall-' + id;
    const ang = face * Math.PI / 2;               // 0:+x 1:+y 2:-x 3:-y
    s = { grp, nx: Math.cos(ang), ny: Math.sin(ang), face, storey };
    _sectionByKey.set(id, s);
    wallSections.push(s);
  }
  return s.grp;
}
// Which of the four faces a wall run belongs to, from its outward normal.
function faceOf(nx, ny) {
  return Math.abs(nx) >= Math.abs(ny) ? (nx >= 0 ? 0 : 2) : (ny >= 0 ? 1 : 3);
}

// Walk a ring, chop every edge into short runs, classify each run and drop its
// quad into the right bucket. `u` keeps running along the whole ring so the
// panel module never restarts at a corner.
// At a corner two leaning faces would pull apart, so the head of the glass is
// pushed out along the mitre of the two walls that meet there.
function miterOffsets(pts) {
  const n = pts.length, out = [];
  for (let i = 0; i < n; i++) {
    const a = pts[(i - 1 + n) % n], b = pts[i], c = pts[(i + 1) % n];
    const d1x = b[0] - a[0], d1y = b[1] - a[1], l1 = Math.hypot(d1x, d1y) || 1;
    const d2x = c[0] - b[0], d2y = c[1] - b[1], l2 = Math.hypot(d2x, d2y) || 1;
    const n1x = d1y / l1, n1y = -d1x / l1;
    const n2x = d2y / l2, n2y = -d2x / l2;
    let mx = n1x + n2x, my = n1y + n2y;
    const m = Math.hypot(mx, my);
    if (m < 1e-4) { out.push([n2x, n2y]); continue; }
    mx /= m; my /= m;
    const k = Math.min(1.8, 1 / Math.max(0.55, mx * n2x + my * n2y));
    out.push([mx * k, my * k]);
  }
  return out;
}

// Nothing on an upper storey may stand where the ground floor has nothing
// under it.
//
// The three storeys are traced from three separate drawings, and where a wing
// runs out to a point — the tip of the diagonal wing on the plaza, for one —
// the three outlines disagree by a couple of metres. Level 1 then keeps a bay
// of elevation that level 0 does not carry, and what it leaves is a panel of
// brick with a window and a coping standing in the air over the paving with
// nothing under it or behind it. That panel is the free-standing slab of the
// owner's slide 1, and the same disagreement leaves smaller ones elsewhere.
//
// So an upper-storey wall run has to be able to find ground-floor building
// behind it. It is asked at three depths, because a wall stands on the outline
// and the mask is coarse. The north front is exempt: there the level-2 glass
// boxes oversail level 1 on purpose, which is the whole point of photos 17,
// 22 and 26, and `cantileverOnly` builds the soffit that carries them.
function carriedFromBelow(storey, mx, my, nx, ny) {
  if (storey < 1) return true;
  const m0 = baseSupport;
  if (!m0) return true;
  if (my > 130) return true;                    // the cantilevered north front
  // Two and a half metres of slack for the tracing, and five and a half for a
  // storey that really does step out over the one below. Eight was too generous:
  // at the south-east corner it kept a single bay of first-floor brick standing
  // seven metres clear of the ground floor, which read from the pavement as a
  // wedge of wall poking up out of nothing beside the glazed box.
  for (const d of [1.5, 3.5, 5.5]) {
    if (m0.solid(mx - nx * d, my - ny * d)) return true;
  }
  return false;
}

// The same question asked of a piece of roof plate, which has no facing
// direction: is there ground floor near it? A plate left hanging where its
// walls have gone is the slab seen from above.
//
// It has to be exactly as far-sighted as `carriedFromBelow` is, and in every
// direction rather than in one. A wall run is kept when it finds ground floor
// up to five and a half units behind it, so a plate piece that far behind the
// same wall has to be kept too: ask it a shorter question and the elevation
// stands with a bite of sky taken out of the roof behind it. Two and a half was
// that shorter question, and what it left, once the plate was cut fine enough
// for the answer to show, was a rash of five-unit pits along the south range.
const PLATE_REACH = (() => {
  const o = [[0, 0]];
  for (const r of [2.5, 5.5]) {
    for (let k = 0; k < 8; k++) {
      const a = k * Math.PI / 4;
      o.push([r * Math.cos(a), r * Math.sin(a)]);
    }
  }
  return o;
})();
function plateCarried(storey, x, y) {
  if (storey < 1) return true;
  const m0 = baseSupport;
  if (!m0) return true;
  if (y > 130) return true;
  for (const [dx, dy] of PLATE_REACH) {
    if (m0.solid(x + dx, y + dy)) return true;
  }
  return false;
}

// Where along a→b does it cross the edge of a cellar rectangle? Returns the
// parameters, 0 and 1 included, in order — the places the elevation has to be
// broken so that no bay of it is half over a cellar and half over building.
function cellarBreaks(a, b) {
  const ts = [0, 1];
  const dx = b[0] - a[0], dy = b[1] - a[1];
  for (const c of CELLARS) {
    if (Math.abs(dx) > 1e-9) {
      for (const v of [c.x0, c.x1]) {
        const t = (v - a[0]) / dx;
        if (t > 1e-6 && t < 1 - 1e-6) ts.push(t);
      }
    }
    if (Math.abs(dy) > 1e-9) {
      for (const v of [c.y0, c.y1]) {
        const t = (v - a[1]) / dy;
        if (t > 1e-6 && t < 1 - 1e-6) ts.push(t);
      }
    }
  }
  ts.sort((m, n) => m - n);
  return ts;
}

function buildStoreyWalls(storey, rings, sink, parapetSink) {
  const z0 = storeyZ(storey), z1 = z0 + EXT_STOREY;
  const above = levelMasks[storey + 1];
  for (const ring of rings) {
    const pts = ring.pts;
    const mit = miterOffsets(pts);
    let run = 0;
    for (let i = 0; i < pts.length; i++) {
      const a = pts[i], b = pts[(i + 1) % pts.length];
      const dx = b[0] - a[0], dy = b[1] - a[1];
      const len = Math.hypot(dx, dy);
      if (len < 1e-6) continue;
      const nx = dy / len, ny = -dx / len;
      // Cut the elevation on the cellar rectangles before chopping it into
      // bays. The plate is cut exactly on those lines, so the wall has to be:
      // a bay that straddles one goes whole whichever way it is judged, and it
      // takes with it either a strip of elevation the cellar never asked for —
      // one bay of brick with a single window standing on the roof, the owner's
      // slide 1 — or the two and a half metres of wall in front of the plate
      // the cellar left standing, which opens a slot at the plaza corner that
      // you can see the rooms through. Split first and every bay is wholly in
      // or wholly out, so its midpoint answers for it.
      const brk = cellarBreaks(a, b);
      const bays = [];
      for (let k = 0; k + 1 < brk.length; k++) {
        const seg = len * (brk[k + 1] - brk[k]);
        if (seg < 1e-6) continue;
        const n = Math.max(1, Math.ceil(seg / CHUNK));
        for (let s = 0; s < n; s++) {
          bays.push([brk[k] + (brk[k + 1] - brk[k]) * (s / n),
                     brk[k] + (brk[k + 1] - brk[k]) * ((s + 1) / n)]);
        }
      }
      for (let s = 0; s < bays.length; s++) {
        const t0 = bays[s][0], t1 = bays[s][1];
        const p0 = [a[0] + dx * t0, a[1] + dy * t0];
        const p1 = [a[0] + dx * t1, a[1] + dy * t1];
        const mx = (p0[0] + p1[0]) / 2, my = (p0[1] + p1[1]) / 2;
        if (inCellar(mx, my)) continue;              // nothing above ground
        if (!carriedFromBelow(storey, mx, my, nx, ny)) continue;
        const skin = skinFor(storey, mx, my, nx, ny);
        const base = baseSkinAt(mx, my);
        const u0 = (run + len * t0) / FACADE_TILE;
        const u1 = (run + len * t1) / FACADE_TILE;
        const glass = skin === 'glass' || skin === 'bay';
        // Storeys overlap by a hair. Butted exactly, the seam between two bands
        // showed as a thread of sky the length of the elevation.
        const LAP = 0.30;
        const bot = glass ? z0 - GLASS_DROP : storey === 0 ? FOUNDATION : z0 - LAP;
        const top = glass ? z1 + GLASS_RISE : z1;
        const vb = glass ? 0 : storey === 0 ? (FOUNDATION - z0) / EXT_STOREY
                                            : -LAP / EXT_STOREY;
        if (top < groundZ(mx, my) + 0.4) continue;   // wholly underground
        const lean = skin === 'glass' ? GLASS_LEAN : 0;
        const o0 = (t0 <= 1e-9) ? mit[i] : [nx, ny];
        const o1 = (t1 >= 1 - 1e-9) ? mit[(i + 1) % pts.length] : [nx, ny];
        const bucket = skin + '#' + faceOf(nx, ny) + '#' + storey;
        sink.quad(bucket,
          [p0[0], p0[1], bot], [p1[0], p1[1], bot],
          [p1[0] + o1[0] * lean, p1[1] + o1[1] * lean, top],
          [p0[0] + o0[0] * lean, p0[1] + o0[1] * lean, top],
          nx, ny, u0, u1, vb, 1);
        // parapet wherever nothing stands on this storey — except under the
        // plaza link, whose own sloping fascia is the head of that elevation.
        // A coping band left there stands a hand's width behind the glass and
        // reads as the flat top of a box, which is the whole complaint.
        const hasAbove = glass || (above && above.solid(mx - nx * 5, my - ny * 5));
        if (!hasAbove && !underPlazaLink(storey, mx, my)) {
          parapetSink.quad((base === 'brick' ? 'trim' : 'trimdk') +
            '#' + faceOf(nx, ny) + '#' + storey,
            [p0[0], p0[1], z1 - 0.30], [p1[0], p1[1], z1 - 0.30],
            [p1[0], p1[1], z1 + PARAPET_H], [p0[0], p0[1], z1 + PARAPET_H],
            nx, ny, u0 * 6, u1 * 6, 0, 1);
        }
      }
      run += len;
    }
  }
}

// Wherever the roof plate is cut — over a cellar the yard runs across, or where
// the slope has swallowed a storey — the cut has to be given a face. Without
// one the elevation stops in mid-air and you look straight through the building
// into the sky, which is the "looks very broken" of the owner's first slide.
// The cut edges come out of the plate itself, so the wall lands exactly on the
// line the roof ends on however coarsely the plate was triangulated.
function buildCutWalls(cuts, storey, sink, parapetSink) {
  const z1 = storeyZ(storey) + EXT_STOREY;
  const z0 = storey === 0 ? FOUNDATION : storeyZ(storey);
  const vb = storey === 0 ? (FOUNDATION - storeyZ(0)) / EXT_STOREY : 0;
  for (const [ax0, ay0, bx0, by0, nx, ny] of cuts) {
    // Run each face a little past both ends: neighbouring cut edges meet at an
    // angle, and a hairline of sky between them reads as badly as the hole did.
    const l0 = Math.hypot(bx0 - ax0, by0 - ay0) || 1;
    const ex = (bx0 - ax0) / l0 * 0.9, ey = (by0 - ay0) / l0 * 0.9;
    const ax = ax0 - ex, ay = ay0 - ey, bx = bx0 + ex, by = by0 + ey;
    const len = Math.hypot(bx - ax, by - ay);
    const steps = Math.max(1, Math.ceil(len / CHUNK));
    for (let s = 0; s < steps; s++) {
      const t0 = s / steps, t1 = (s + 1) / steps;
      const p0 = [ax + (bx - ax) * t0, ay + (by - ay) * t0];
      const p1 = [ax + (bx - ax) * t1, ay + (by - ay) * t1];
      const mx = (p0[0] + p1[0]) / 2, my = (p0[1] + p1[1]) / 2;
      if (z1 < groundZ(mx, my) + 0.4) continue;      // the cut is underground
      // No support test here, and there must not be one. A cut edge exists only
      // where a plate triangle survived, and a plate triangle now survives only
      // where the storey below carries every corner of it — `aboveGrade` asks
      // `plateCarried` the same question `carriedFromBelow` asks of a wall. So
      // the support question has already been answered, on the plate, before
      // this edge was ever produced. Asking it again here can only disagree
      // with that answer, and when it did — the plate kept, its closing wall
      // refused — what it left was roof standing on nothing, which is the
      // defect this pass is about. Every cut the plate opens is closed.
      const ix = mx - nx * 2.5, iy = my - ny * 2.5;  // the side that still stands
      const skin = skinFor(storey, ix, iy, nx, ny);
      const base = baseSkinAt(ix, iy);
      const face = faceOf(nx, ny);
      const u0 = len * t0 / FACADE_TILE, u1 = len * t1 / FACADE_TILE;
      sink.quad(skin + '#' + face + '#' + storey,
        [p0[0], p0[1], z0], [p1[0], p1[1], z0],
        [p1[0], p1[1], z1], [p0[0], p0[1], z1],
        nx, ny, u0, u1, vb, 1);
      if (underPlazaLink(storey, mx, my)) continue;
      parapetSink.quad((base === 'brick' ? 'trim' : 'trimdk') + '#' + face + '#' + storey,
        [p0[0], p0[1], z1 - 0.30], [p1[0], p1[1], z1 - 0.30],
        [p1[0], p1[1], z1 + PARAPET_H], [p0[0], p0[1], z1 + PARAPET_H],
        nx, ny, u0 * 6, u1 * 6, 0, 1);
    }
  }
}

// ── build ────────────────────────────────────────────────────────────────
function buildShell() {
  if (shellBuilt) return;
  if (!floorData[LEVELS[0]]) return;
  const t0 = performance.now();
  buildShellMaterials();

  levelRings = LEVELS.map((l, i) => extractFootprint(l, {
    cell: 1.0, close: 3, eps: 1.7, minArea: i === 2 ? 200 : 120,
  }));
  baseRings = levelRings[0];
  if (!baseRings.length) return;
  // Put the upper storeys back on the elevation the ground floor draws.
  //
  // Each storey is traced from its own drawing and simplified on its own, so
  // the same wall comes out a metre or two apart on the three levels. Built as
  // traced, the storeys step in and out over one another: on the plaza the
  // level-1 wall stands two metres proud of the level-0 wall below it and
  // shows its returns, which is the panel of brick the owner reads as a
  // free-standing slab and, from the ground, as "one of the bricks pushed into
  // the building". Anything within `SNAP` of the ground-floor outline is that
  // same wall and is moved onto it; a real set-back is deeper than that and is
  // left where the plan puts it.
  const SNAP = 3.0;
  for (let i = 1; i < levelRings.length; i++) {
    snapRingsToBase(levelRings[i], baseRings, SNAP);
    // And nothing on an upper storey may stand where the ground floor is not.
    //
    // Three units is the right reach for a wall that is really the same wall
    // drawn twice, and the wrong one for the places where the level-1 trace
    // simply wanders off the site: it runs six units east of the ground floor
    // at (157.8, -62) and six units south of it at the south-east corner. Left
    // there, the wall is culled for want of anything to stand on, a bay or two
    // of it survives where the support mask happens to reach, and what is left
    // beside the east walkway is a pair of brick slivers standing on the paving
    // with the roof stepping in and out behind them — the owner's orphans and
    // his torn eastern rim. A point already over the ground floor is a real
    // set-back and is left exactly where the plan puts it; only a point with
    // nothing under it is pulled back onto the outline.
    snapRingsToBase(levelRings[i], baseRings, 10, true);
  }
  topRings = levelRings[2] || [];

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const p of baseRings[0].pts) {
    minX = Math.min(minX, p[0]); maxX = Math.max(maxX, p[0]);
    minY = Math.min(minY, p[1]); maxY = Math.max(maxY, p[1]);
  }
  siteBB = { minX, minY, maxX, maxY };

  levelMasks = levelRings.map(r => (r && r.length ? footprintMask(r, 2.0, 0) : null));
  // Close the slots that only one storey has.
  //
  // Between two storeys that are both solid, the middle one sometimes carries a
  // narrow re-entrant slot — eight metres across and ten deep on the south face
  // of the north block, at x -68…-61 — that the plan draws and the building does
  // not have. Built as drawn it takes a bite out of the middle band of the
  // elevation and you look straight through it at the floor plate and the far
  // wall inside: the "hole in the wall" of the owner's slide 5. A light-well is
  // cut through every storey; a slot in one storey alone is the plan.
  for (let i = 1; i + 1 < levelRings.length; i++) {
    if (!levelRings[i] || !levelRings[i].length) continue;
    fillSandwichedSlots(levelRings[i], levelMasks[i - 1], levelMasks[i + 1], 16);
  }
  levelMasks = levelRings.map(r => (r && r.length ? footprintMask(r, 2.0, 0) : null));
  // finer than the storey masks: it decides whether a wall run has ground floor
  // behind it, and two metres of slack there is two metres of slab in the air
  baseSupport = footprintMask(levelRings[0], 1.0, 0);
  entranceInfo = findEntrance(baseRings);

  const track = (mesh, opts) => {
    shellGroup.add(mesh);
    const o = opts || {};
    if (o.fade !== false) {
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
      for (const m of mats) {
        if (!shellFadeParts.some(p => p.mat === m)) {
          m.transparent = true;
          shellFadeParts.push({ mat: m, base: m.opacity, ghost: o.ghost === undefined ? 0.075 : o.ghost });
        }
      }
    }
    if (o.lift) shellLiftParts.push({ obj: mesh, z0: mesh.position.z, lift: o.lift });
    return mesh;
  };

  // ── elevations ──
  const wallGroup = new THREE.Group();
  const glassGroup = new THREE.Group();
  const sink = makeSink(), parapetSink = makeSink(), glassSink = makeSink();
  for (let i = 0; i < levelRings.length; i++) {
    if (!levelRings[i] || !levelRings[i].length) continue;
    buildStoreyWalls(i, levelRings[i], i === 2 ? glassSink : sink, parapetSink);
  }
  // the faces of every cut in a roof plate, collected as the plates are built
  const cutSink = makeSink(), cutParapet = makeSink();
  for (let i = 0; i < levelRings.length; i++) {
    if (!levelRings[i] || !levelRings[i].length) continue;
    const shape = shapeFromRings(levelRings[i]);
    if (!shape) continue;
    const cuts = [];
    aboveGrade(new THREE.ShapeGeometry(shape, 6), storeyZ(i) + EXT_STOREY + 0.03,
               cuts, i);
    buildCutWalls(cuts, i, cutSink, cutParapet);
  }
  for (const k of Object.keys(cutSink.buckets)) {
    sink.buckets[k] = sink.buckets[k] || { p: [], n: [], u: [] };
    const a = sink.buckets[k], b = cutSink.buckets[k];
    a.p.push(...b.p); a.n.push(...b.n); a.u.push(...b.u);
  }
  for (const k of Object.keys(cutParapet.buckets)) {
    parapetSink.buckets[k] = parapetSink.buckets[k] || { p: [], n: [], u: [] };
    const a = parapetSink.buckets[k], b = cutParapet.buckets[k];
    a.p.push(...b.p); a.n.push(...b.n); a.u.push(...b.u);
  }
  // the glazed part of level 2 is split out so it can lift away on peel
  {
    const gk = Object.keys(glassSink.buckets).filter(k => k.startsWith('glass#'));
    if (gk.length) {
      const b = {};
      for (const k of gk) { b['glass#' + k.split('#').slice(1).join('#') + '#free'] = glassSink.buckets[k]; delete glassSink.buckets[k]; }
      // keys without a section suffix land straight in the glass group
      const plain = {};
      for (const k of Object.keys(b)) plain['glass'] = plain['glass'] || { p: [], n: [], u: [] };
      for (const k of Object.keys(b)) {
        plain['glass'].p.push(...b[k].p); plain['glass'].n.push(...b[k].n); plain['glass'].u.push(...b[k].u);
      }
      sinkToMeshes({ buckets: plain }, glassGroup, 0, true);
    }
  }
  sinkToMeshes(glassSink, wallGroup, 0, true);
  sinkToMeshes(sink, wallGroup, 0, true);
  sinkToMeshes(parapetSink, wallGroup, 0, true);
  for (const s of wallSections) wallGroup.add(s.grp);
  shellGroup.add(wallGroup);
  for (const k of Object.keys(skinMats)) {
    const m = skinMats[k];
    m.transparent = true;
    shellFadeParts.push({ mat: m, base: m.opacity, ghost: k === 'glass' ? 0 : 0.07 });
  }
  trimMat.transparent = true;
  shellFadeParts.push({ mat: trimMat, base: 1, ghost: 0.06 });

  // ── roofs ──
  for (let i = 0; i < levelRings.length; i++) {
    if (!levelRings[i] || !levelRings[i].length) continue;
    const shape = shapeFromRings(levelRings[i]);
    if (!shape) continue;
    const z = storeyZ(i) + EXT_STOREY + 0.03;
    const g = aboveGrade(new THREE.ShapeGeometry(shape, 6), z, null, i);
    if (!g) continue;
    const r = new THREE.Mesh(g, roofMat);
    r.name = 'roof-' + i;
    r.position.z = z;
    r.receiveShadow = true;
    track(r, { lift: i === 2 ? 46 : 34 });
  }
  roofMat.transparent = true;

  // ── roof plant ──
  {
    const rnd = mulberry(88);
    const plantMat = new THREE.MeshStandardMaterial({ color: 0x8d9295, roughness: 0.8 });
    const roofPlant = new THREE.Group();
    const inset = (m, px, py) => m.solid(px + 15, py) && m.solid(px - 15, py) &&
                                 m.solid(px, py + 15) && m.solid(px, py - 15);
    for (let i = 0; i < 1200 && roofPlant.children.length < 12; i++) {
      const px = minX + rnd() * (maxX - minX);
      const py = minY + rnd() * (maxY - minY);
      const m1 = levelMasks[1], m2 = levelMasks[2];
      if (!m1 || !inset(m1, px, py)) continue;
      if (m2 && m2.solid(px, py)) continue;
      const bw = 5 + rnd() * 7, bh = 1.8 + rnd() * 2.2;
      const b = new THREE.Mesh(new THREE.BoxGeometry(bw, bw * (0.5 + rnd()), bh), plantMat);
      b.position.set(px, py, storeyZ(2) + bh / 2);
      b.castShadow = true;
      roofPlant.add(b);
    }
    shellGroup.add(roofPlant);
    plantMat.transparent = true;
    shellFadeParts.push({ mat: plantMat, base: 1, ghost: 0 });
    shellLiftParts.push({ obj: roofPlant, z0: 0, lift: 34 });
  }

  // ── the glass volumes and what carries them ──
  // The mathematics glass is the piece the eye is on, so it is the first thing
  // to move: it rises out of the north front, drifts clear of the building and
  // turns a few degrees as it goes.
  shellGroup.add(glassGroup);
  shellLiftParts.push({ obj: glassGroup, z0: 0, lift: 62, early: true,
                        drift: [0.35, 0.94], spin: 0.09 });
  buildGlassSupports(glassGroup);

  // ── details that read at eye level ──
  buildDownpipes();
  buildEntrances();
  buildSkywalk();
  buildWestSkywalk();
  buildPlazaLink();
  buildWestCanopy();

  // ── ghost frame, visible once the building is open ──
  ghostEdges = buildGhostEdges(baseRings, topRings);
  shellGroup.add(ghostEdges);

  buildCampus(baseRings, entranceInfo);
  shellBuilt = true;
  requestShadowUpdate();
  console.log(`[shell] built in ${Math.round(performance.now() - t0)}ms — ` +
    levelRings.map((r, i) => `L${i}:${r.length}`).join(' '));
}

// Keep only the triangles of the level-2 plate that hang out past level 1 on
// the north front — the underside you can actually stand beneath.
function cantileverOnly(geo, m1) {
  const src = geo.getAttribute('position');
  const idx = geo.getIndex();
  const n = idx ? idx.count : src.count;
  const out = [];
  for (let t = 0; t < n; t += 3) {
    const a = idx ? idx.getX(t) : t;
    const b = idx ? idx.getX(t + 1) : t + 1;
    const c = idx ? idx.getX(t + 2) : t + 2;
    const cx = (src.getX(a) + src.getX(b) + src.getX(c)) / 3;
    const cy = (src.getY(a) + src.getY(b) + src.getY(c)) / 3;
    if (cy < 138 || m1.solid(cx, cy)) continue;
    for (const k of [a, b, c]) out.push(src.getX(k), src.getY(k), 0);
  }
  geo.dispose();
  if (!out.length) return null;
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(out, 3));
  g.computeVertexNormals();
  g.computeBoundingSphere();
  return g;
}

// Keep only the triangles of a flat slab that stand clear of the ground. When
// `cuts` is passed it is filled with the interior edges the cull opened up —
// the edges with a surviving triangle on one side and nothing on the other —
// each as [ax, ay, bx, by, nx, ny] with the normal pointing into the void.
//
// Two different things remove a triangle. Ground swallowing a storey leaves a
// real cut: the plate stops in the slope. A cellar is a wing the plan draws
// and the site does not have. Both leave the same thing behind — an edge of
// plate with a storey on one side and nothing on the other — and both want the
// same thing done about it, which is a wall. A pass that faced the first and
// refused the second is what left the elevation open beside the plaza link:
// the walls of the needle were dropped, the plate over them was not, and the
// cut it left was never closed, so you looked straight through the building.
//
// What the refusal was really guarding against is a cut face built where the
// storey below carries nothing — a slab of wall in the air, the "structure is
// broken" of the owner's slide 1. That is a support question and it is asked
// as one, by `carriedFromBelow` in buildCutWalls, exactly as it is asked of
// every other wall run. Nothing here needs to know what kind of cut it is.
//
// A triangulator's triangles are the wrong size to ask any of this of, and the
// pass that asked it of their three corners is the defect this pass is about.
// Earcut gives the level-1 plate as about a hundred triangles for the whole
// building, and one of them spans the entire diagonal wing on the plaza: the
// corners (108.7, -20.5), (35.5, -105.6) and (121, -30.9), fifty-one metres of
// building in one triangle. Its first corner sits inside the twenty-metre
// cellar rectangle at the plaza needle, so the whole triangle went, and with it
// the roof off the whole wing. The elevation is built from the ring, so it
// stayed; `buildCutWalls` then closed the hole with a second wall eight metres
// behind the first; and what that leaves, read from the air, is the wing as two
// parallel planes of brick with roof showing between them — the owner's double
// wall, and the court beside it apparently roofed over at the wrong level.
//
// So the plate is cut up before it is judged: exactly along the cellar
// rectangles, which have to come out of it with a straight edge, and then on a
// world grid, which bounds every piece to `PLATE_CELL` across. The grid is
// fixed in world space rather than fitted to each triangle so that two pieces
// either side of a shared edge cut it in the same places and no crack is left
// between them. Every question is then asked once, of a centroid that stands
// for nothing further away than a metre and a half.
const PLATE_CELL = 5;

// Clip a convex polygon to one side of an axis-aligned line, exactly. The
// interpolation is always run from whichever end of an edge sorts first, so the
// two pieces that share an edge compute the point on it to the same bit.
function clipToSide(poly, axis, c, sgn) {
  const out = [];
  for (let i = 0, n = poly.length; i < n; i++) {
    const p = poly[i], q = poly[(i + 1) % n];
    const sp = (p[axis] - c) * sgn, sq = (q[axis] - c) * sgn;
    if (sp >= 0) out.push(p);
    if ((sp > 0 && sq < 0) || (sp < 0 && sq > 0)) {
      const flip = q[0] < p[0] || (q[0] === p[0] && q[1] < p[1]);
      const a = flip ? q : p, b = flip ? p : q;
      const t = (c - a[axis]) / (b[axis] - a[axis]);
      const r = [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
      r[axis] = c;
      out.push(r);
    }
  }
  return out;
}

function splitPolys(polys, axis, c) {
  const out = [];
  for (const p of polys) {
    let lo = false, hi = false;
    for (const q of p) {
      if (q[axis] < c - 1e-9) lo = true;
      else if (q[axis] > c + 1e-9) hi = true;
    }
    if (!lo || !hi) { out.push(p); continue; }
    const a = clipToSide(p, axis, c, -1), b = clipToSide(p, axis, c, 1);
    if (a.length >= 3) out.push(a);
    if (b.length >= 3) out.push(b);
  }
  return out;
}

// One plate triangle into the pieces the tests can be asked of.
function platePieces(tri, out) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const p of tri) {
    if (p[0] < minX) minX = p[0];
    if (p[0] > maxX) maxX = p[0];
    if (p[1] < minY) minY = p[1];
    if (p[1] > maxY) maxY = p[1];
  }
  let polys = [tri];
  const xs = [], ys = [];
  for (const c of CELLARS) {
    for (const v of [c.x0, c.x1]) if (v > minX && v < maxX) xs.push(v);
    for (const v of [c.y0, c.y1]) if (v > minY && v < maxY) ys.push(v);
  }
  for (let g = Math.ceil(minX / PLATE_CELL) * PLATE_CELL; g < maxX; g += PLATE_CELL) xs.push(g);
  for (let g = Math.ceil(minY / PLATE_CELL) * PLATE_CELL; g < maxY; g += PLATE_CELL) ys.push(g);
  for (const v of xs) polys = splitPolys(polys, 0, v);
  for (const v of ys) polys = splitPolys(polys, 1, v);
  for (const p of polys) {
    for (let i = 1; i + 1 < p.length; i++) out.push([p[0], p[i], p[i + 1]]);
  }
}

// Keep only the pieces of a flat slab that stand clear of the ground, are not
// over a cellar, and have a storey under them. When `cuts` is passed it is
// filled with the interior edges the cull opened up — the edges with a
// surviving piece on one side and nothing on the other — each as
// [ax, ay, bx, by, nx, ny] with the normal pointing into the void.
//
// Two different things remove a piece. Ground swallowing a storey leaves a real
// cut: the plate stops in the slope. A cellar is a wing the plan draws and the
// site does not have. Both leave the same thing behind — an edge of plate with
// a storey on one side and nothing on the other — and both want the same thing
// done about it, which is a wall. A pass that faced the first and refused the
// second is what left the elevation open beside the plaza link: the walls of
// the needle were dropped, the plate over them was not, and the cut it left was
// never closed, so you looked straight through the building.
function aboveGrade(geo, z, cuts, storey) {
  const src = geo.getAttribute('position');
  const idx = geo.getIndex();
  const n = idx ? idx.count : src.count;
  const out = [];
  const tris = [];
  for (let t = 0; t < n; t += 3) {
    const a = idx ? idx.getX(t) : t;
    const b = idx ? idx.getX(t + 1) : t + 1;
    const c = idx ? idx.getX(t + 2) : t + 2;
    platePieces([[src.getX(a), src.getY(a)],
                 [src.getX(b), src.getY(b)],
                 [src.getX(c), src.getY(c)]], tris);
  }
  const st = storey === undefined ? 0 : storey;
  for (const tri of tris) {
    const cx = (tri[0][0] + tri[1][0] + tri[2][0]) / 3;
    const cy = (tri[0][1] + tri[1][1] + tri[2][1]) / 3;
    tri.keep = !(groundZ(cx, cy) > z - EXT_STOREY * 0.55 || inCellar(cx, cy)) &&
               plateCarried(st, cx, cy);
    if (!tri.keep) continue;
    for (const p of tri) out.push(p[0], p[1], 0);
  }
  if (cuts) {
    const vk = (p) => p[0].toFixed(3) + ',' + p[1].toFixed(3);
    const m = new Map();
    for (const tri of tris) {
      for (let e = 0; e < 3; e++) {
        const i = tri[e], j = tri[(e + 1) % 3], o = tri[(e + 2) % 3];
        const ka = vk(i), kb = vk(j);
        if (ka === kb) continue;                   // a clipped-away sliver
        const k = ka < kb ? ka + '|' + kb : kb + '|' + ka;
        let r = m.get(k);
        if (!r) m.set(k, r = { i, j, all: 0, kept: 0, o: null });
        r.all++;
        if (tri.keep) { r.kept++; r.i = i; r.j = j; r.o = o; }
      }
    }
    for (const r of m.values()) {
      if (r.all !== 2 || r.kept !== 1) continue;
      let ax = r.i[0], ay = r.i[1];
      let bx = r.j[0], by = r.j[1];
      const len = Math.hypot(bx - ax, by - ay);
      if (len < 0.05) continue;
      // The quad is wound so that its face is on the right of a→b, so turning
      // the normal round means turning the edge round with it — otherwise the
      // closing wall is built inside out and cannot be seen from the void.
      const ox = r.o[0] - (ax + bx) / 2, oy = r.o[1] - (ay + by) / 2;
      if (((by - ay) * ox - (bx - ax) * oy) > 0) {
        const tx = ax, ty = ay; ax = bx; ay = by; bx = tx; by = ty;
      }
      cuts.push([ax, ay, bx, by, (by - ay) / len, -(bx - ax) / len]);
    }
  }
  geo.dispose();
  if (!out.length) return null;
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(out, 3));
  g.computeVertexNormals();
  g.computeBoundingSphere();
  return g;
}

// Is (x, y) inside a set of rings? Even–odd across all of them at once, so the
// outer ring counts the point in and a courtyard ring counts it back out.
function insideRings(rings, x, y) {
  let hit = false;
  for (const r of rings) {
    const p = r.pts;
    for (let i = 0, j = p.length - 1; i < p.length; j = i++) {
      if ((p[i][1] > y) !== (p[j][1] > y) &&
          x < (p[j][0] - p[i][0]) * (y - p[i][1]) / (p[j][1] - p[i][1]) + p[i][0]) hit = !hit;
    }
  }
  return hit;
}

// Move every point of `rings` that is within `maxD` of the ground-floor outline
// onto it, so the storeys of one wall share one plane. Points further off are
// left alone: those are the places the building really does step.
//
// With `outsideOnly` the same move is made, at a longer reach, but only for a
// point that has no ground floor under it at all. That is not a step in the
// building — nothing can stand there — and it is what leaves an upper storey
// hanging off the side of the one below.
function snapRingsToBase(rings, base, maxD, outsideOnly) {
  for (const r of rings) {
    for (const p of r.pts) {
      // The level-2 glass boxes on the north front oversail level 1 on purpose,
      // and `cantileverOnly` builds the soffit that carries them.
      if (outsideOnly && (p[1] > 130 || insideRings(base, p[0], p[1]))) continue;
      let bx = 0, by = 0, bd = Infinity;
      for (const br of base) {
        const q = br.pts;
        for (let i = 0; i < q.length; i++) {
          const a = q[i], b = q[(i + 1) % q.length];
          const dx = b[0] - a[0], dy = b[1] - a[1];
          const l2 = dx * dx + dy * dy;
          if (l2 < 1e-9) continue;
          let t = ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / l2;
          t = t < 0 ? 0 : t > 1 ? 1 : t;
          const cx = a[0] + dx * t, cy = a[1] + dy * t;
          const d = Math.hypot(cx - p[0], cy - p[1]);
          if (d < bd) { bd = d; bx = cx; by = cy; }
        }
      }
      if (bd <= maxD) { p[0] = bx; p[1] = by; }
    }
  }
  for (const r of rings) r.area = signedArea(r.pts);
}

// Fill the slots that this storey has and the storeys either side of it do not.
//
// A slot shows in the ring as a short chord with a long detour hung off it: two
// or three vertices that run into the building and back out again, arriving
// within `maxSpan` of where they left. Splicing the detour out is only right if
// the ground it gives back is building on both neighbouring storeys — otherwise
// it is a real light-well, a real set-back, or the recess a bridge crosses, and
// every one of those has to stay. The area is sampled rather than trusted to a
// centroid: a U is not convex and its centroid can fall outside it.
function fillSandwichedSlots(rings, below, above, maxSpan) {
  if (!below || !above) return rings;
  const inside = (poly, x, y) => {
    let hit = false;
    for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
      const a = poly[i], b = poly[j];
      if ((a[1] > y) !== (b[1] > y) &&
          x < (b[0] - a[0]) * (y - a[1]) / (b[1] - a[1]) + a[0]) hit = !hit;
    }
    return hit;
  };
  for (const r of rings) {
    for (let guard = 0; guard < 12; guard++) {
      const pts = r.pts, n = pts.length;
      let cut = null;
      for (let i = 0; i < n && !cut; i++) {
        for (let k = 2; k <= 6 && n - k >= 4; k++) {
          const a = pts[i], b = pts[(i + k) % n];
          const span = Math.hypot(b[0] - a[0], b[1] - a[1]);
          if (span > maxSpan) continue;
          let run = 0;
          const loop = [];
          for (let m = 0; m <= k; m++) {
            const p = pts[(i + m) % n];
            loop.push(p);
            if (m) run += Math.hypot(p[0] - loop[m - 1][0], p[1] - loop[m - 1][1]);
          }
          if (run < span * 2.2) continue;            // a corner, not a slot
          // The detour has to be an indentation — splicing it out must give
          // ground back to the building. On the outer ring, traced anti-
          // clockwise, that shows as the signed area growing; on a courtyard
          // ring, traced the other way round, the same thing shows as a
          // negative area growing towards zero. One test covers both.
          const spliced = pts.filter((_, m) => {
            for (let q = 1; q < k; q++) if (m === (i + q) % n) return false;
            return true;
          });
          if (signedArea(spliced) <= r.area + 1) continue;
          // and the ground it gives back has to be built on, above and below
          const bb = ringBBox(loop);
          let seen = 0, bad = 0;
          for (let sx = bb.minX + 1; sx < bb.maxX && !bad; sx += 1.5) {
            for (let sy = bb.minY + 1; sy < bb.maxY && !bad; sy += 1.5) {
              if (!inside(loop, sx, sy)) continue;
              seen++;
              if (!below.solid(sx, sy) || !above.solid(sx, sy)) bad = 1;
            }
          }
          if (bad || seen < 3) continue;
          cut = spliced;
          break;
        }
      }
      if (!cut) break;
      r.pts = cut;
      r.area = signedArea(cut);
    }
  }
  return rings;
}

function ringBBox(pts) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  for (const p of pts) {
    minX = Math.min(minX, p[0]); maxX = Math.max(maxX, p[0]);
    minY = Math.min(minY, p[1]); maxY = Math.max(maxY, p[1]);
  }
  return { minX, minY, maxX, maxY, cx: (minX + maxX) / 2, cy: (minY + maxY) / 2 };
}

// Round in-situ concrete columns under the cantilevered glass, and the zinc
// fascia that wraps the soffit — the detail in photos 1, 17, 22, 26 and 27.
function buildGlassSupports(group) {
  if (!topRings.length || !levelMasks[1]) return;
  const m1 = levelMasks[1];
  const colGeo = new THREE.CylinderGeometry(0.95, 1.12, storeyZ(2) - GLASS_DROP - 1.6, 14);
  const spots = [];
  const fascia = makeSink();
  for (const r of topRings) {
    const pts = r.pts;
    let run = 0, acc = 0;
    for (let i = 0; i < pts.length; i++) {
      const a = pts[i], b = pts[(i + 1) % pts.length];
      const dx = b[0] - a[0], dy = b[1] - a[1];
      const len = Math.hypot(dx, dy);
      if (len < 1e-6) continue;
      const nx = dy / len, ny = -dx / len;
      const steps = Math.max(1, Math.ceil(len / CHUNK));
      for (let s = 0; s < steps; s++) {
        const t0 = s / steps, t1 = (s + 1) / steps;
        const p0 = [a[0] + dx * t0, a[1] + dy * t0];
        const p1 = [a[0] + dx * t1, a[1] + dy * t1];
        const mx = (p0[0] + p1[0]) / 2, my = (p0[1] + p1[1]) / 2;
        if (my < 138 || m1.solid(mx - nx * 5.4, my - ny * 5.4)) continue;
        const u0 = (run + len * t0) / 12, u1 = (run + len * t1) / 12;
        // zinc apron under the glass, a metre and a half deep
        fascia.quad('trim',
          [p0[0], p0[1], storeyZ(2) - GLASS_DROP - 1.6], [p1[0], p1[1], storeyZ(2) - GLASS_DROP - 1.6],
          [p1[0], p1[1], storeyZ(2) - GLASS_DROP], [p0[0], p0[1], storeyZ(2) - GLASS_DROP],
          nx, ny, u0, u1, 0, 1);
        acc += len / steps;
        if (acc > 24) { acc = 0; spots.push([mx - nx * 1.7, my - ny * 1.7]); }
      }
      run += len;
    }
  }
  // The soffit belongs only to what actually oversails: the underside of the
  // glass boxes on the north front, not the whole level-2 plate.
  const topShape = shapeFromRings(topRings);
  if (topShape) {
    const raw = new THREE.ShapeGeometry(topShape, 6);
    const geo = cantileverOnly(raw, m1);
    if (geo) {
      const sof = new THREE.Mesh(geo, zincMat);
      sof.position.z = storeyZ(2) - GLASS_DROP - 1.6;
      sof.rotation.y = Math.PI;             // face downwards
      group.add(sof);
    }
  }
  const zg = new THREE.Group();
  sinkToMeshes(fascia, zg, 0, true);
  zg.traverse(o => { if (o.isMesh) o.material = zincMat; });
  group.add(zg);
  zincMat.transparent = true;
  shellFadeParts.push({ mat: zincMat, base: 1, ghost: 0 });

  if (spots.length) {
    const inst = new THREE.InstancedMesh(colGeo, concreteMat, spots.length);
    const mtx = new THREE.Matrix4();
    const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(1, 0, 0), Math.PI / 2);
    const sc = new THREE.Vector3(1, 1, 1);
    for (let i = 0; i < spots.length; i++) {
      mtx.compose(new THREE.Vector3(spots[i][0], spots[i][1],
                  (storeyZ(2) - GLASS_DROP - 1.6) / 2), q, sc);
      inst.setMatrixAt(i, mtx);
    }
    inst.instanceMatrix.needsUpdate = true;
    inst.castShadow = true;
    shellGroup.add(inst);
    concreteMat.transparent = true;
    shellFadeParts.push({ mat: concreteMat, base: 1, ghost: 0.05 });
  }
}

// Galvanised rainwater pipes, one every 26 units of elevation. They are the
// single most recognisable thing on this building at close range.
function buildDownpipes() {
  const pts = [];
  const outer = baseRings[0].pts;
  let run = 0, acc = 0;
  for (let i = 0; i < outer.length; i++) {
    const a = outer[i], b = outer[(i + 1) % outer.length];
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const len = Math.hypot(dx, dy);
    if (len < 1e-6) continue;
    const nx = dy / len, ny = -dx / len;
    acc += len;
    while (acc > 38) {
      acc -= 38;
      const t = (len - acc) / len;
      pts.push([a[0] + dx * t + nx * 0.32, a[1] + dy * t + ny * 0.32]);
    }
    run += len;
  }
  if (!pts.length) return;
  const geo = new THREE.CylinderGeometry(0.24, 0.24, 1, 6);
  const inst = new THREE.InstancedMesh(geo, pipeMat, pts.length);
  const mtx = new THREE.Matrix4();
  const q = new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(1, 0, 0), Math.PI / 2);
  for (let i = 0; i < pts.length; i++) {
    const [px, py] = pts[i];
    const g0 = groundZ(px, py);
    const m1 = levelMasks[1], m2 = levelMasks[2];
    const top = (m2 && m2.solid(px, py)) ? storeyZ(3)
              : (m1 && m1.solid(px, py)) ? storeyZ(2) : storeyZ(1);
    const h = Math.max(3, top - g0);
    mtx.compose(new THREE.Vector3(px, py, g0 + h / 2), q,
                new THREE.Vector3(1, h, 1));
    inst.setMatrixAt(i, mtx);
  }
  inst.instanceMatrix.needsUpdate = true;
  inst.castShadow = true;
  shellGroup.add(inst);
  pipeMat.transparent = true;
  shellFadeParts.push({ mat: pipeMat, base: 1, ghost: 0 });
}

// ── entrances ────────────────────────────────────────────────────────────
// The real building's main entrance faces east onto the plaza; the others are
// the two under the glass corners and the doors off the south approach.
function findEntrance(rings) {
  if (!rings || !rings.length) return null;
  const outer = rings[0].pts;
  let best = null, bestScore = -Infinity;
  for (let i = 0; i < outer.length; i++) {
    const a = outer[i], b = outer[(i + 1) % outer.length];
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const len = Math.hypot(dx, dy);
    if (len < 22) continue;
    const nx = dy / len, ny = -dx / len;
    if (nx < 0.6) continue;                   // must face east
    const cy = (a[1] + b[1]) / 2;
    const score = Math.max(a[0], b[0]) * 1.2 - Math.abs(cy - 26) * 0.7 + Math.min(len, 60) * 0.3;
    if (score > bestScore) {
      bestScore = score;
      best = { x: (a[0] + b[0]) / 2, y: cy, nx, ny, dx: dx / len, dy: dy / len, len };
    }
  }
  if (!best) {
    for (let i = 0; i < outer.length; i++) {
      const a = outer[i], b = outer[(i + 1) % outer.length];
      const len = Math.hypot(b[0] - a[0], b[1] - a[1]);
      if (len < 12) continue;
      const cy = (a[1] + b[1]) / 2;
      if (!best || cy > best.y) {
        best = { x: (a[0] + b[0]) / 2, y: cy, nx: 0, ny: 1,
                 dx: (b[0] - a[0]) / len, dy: (b[1] - a[1]) / len, len };
      }
    }
  }
  return best;
}

// The outer-ring segment nearest a nominal point, so an entrance can be asked
// for by where it is on the plan rather than by index.
//
// The distance is measured to the segment itself and not to the inset point the
// door will finally sit on. Measuring the inset one is what silently moved the
// south entrance: asked for x 41, between the two recessed bays, every segment
// short enough to be there was skipped and the long wall beyond the second bay
// won on a projection clamped twenty-odd metres along itself. The door then
// stood where nothing had asked for it, and no amount of nudging the nominal
// point fixed it — the owner's slide 6 is that bug seen from the plaza.
//
// `minLen` is how short a wall run may be and still take a door. The default
// keeps doors off the little returns; a caller that means a recessed bay says so.
// How wide the doorway on a wall of this length comes out. `buildEntrance` and
// `wallNear` have to agree about it — one of them decides where the middle of
// the screen goes and the other how far the screen reaches from that middle.
function entranceWidth(len) {
  return Math.min(21, len - 1.2, Math.max(14, len * 0.32));
}

function wallNear(px, py, minLen) {
  const outer = baseRings[0].pts;
  const lim = minLen || 14;
  let best = null, bestD = Infinity;
  for (let i = 0; i < outer.length; i++) {
    const a = outer[i], b = outer[(i + 1) % outer.length];
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const len = Math.hypot(dx, dy);
    if (len < lim) continue;
    const traw = ((px - a[0]) * dx + (py - a[1]) * dy) / (len * len);
    const tc = Math.max(0, Math.min(1, traw));
    const d = Math.hypot(a[0] + dx * tc - px, a[1] + dy * tc - py);
    if (d < bestD) {
      bestD = d;
      // Keep the whole doorway on the wall. The old clamp was 0.15–0.85 of the
      // run whatever the doorway was going to be: on the twenty-one metre wall
      // at the back of the north-east recess that put the middle of a fourteen
      // metre screen three metres from the end of it, and four metres of glass
      // and steel came out through the brick return and stood in the open. The
      // margin below is half the width `buildEntrance` will choose, so the
      // reveal always has wall either side of it.
      const half = entranceWidth(len) / 2 + 0.6;
      const m = Math.min(0.45, half / len);
      const t = Math.max(m, Math.min(1 - m, traw));
      best = { x: a[0] + dx * t, y: a[1] + dy * t, nx: dy / len, ny: -dx / len,
               dx: dx / len, dy: dy / len, len };
    }
  }
  return best;
}

function buildEntrances() {
  // Reviewed against the survey: the main way in is the recessed glazed
  // entrance in its brick frame on the plaza front (photo 10) — not a canopy
  // with a banner, which the owner's review struck out. The south front's
  // entrance is the green-framed pair of doors under a light steel canopy
  // where the diagonal wing lands (photo 14), and there is no entrance in the
  // yard between the south wings.
  //
  // The two doors that used to stand on the plaza — the recessed glazed
  // entrance in its brick frame and its twin across the yard in the plaza wing
  // — are gone. The owner's review rings both of them and says "remove this
  // entrances"; the yard is served from the east recess and from the doors on
  // the north side of it, and nothing on the plaza fronts is a way in.
  const wanted = [
    // Under the north-east glass corner (photo 22). "Move entrance to the
    // other side of the wall", asked twice now.
    //
    // The first reading of it slid the doors along the twenty-one metre wall
    // at the back of the recess — from (157, 175.5) to (150, 175.5) — and the
    // owner sent the same note back with the same wall circled. Sliding a door
    // to the far end of a wall does not put it on the other side of that wall:
    // it is the same face, looking the same way, seven metres over. What he is
    // pointing at is the corner. The recess turns at (164.7, 175.5), and the
    // nine-metre return that runs south from it to (164.7, 166.5) is the same
    // piece of wall seen from the other side — the east elevation, facing the
    // plaza, instead of the north-facing back of a recess.
    //
    // That is also how its twin is already built: the door under the
    // north-*west* glass corner is on the west face, out on the elevation, not
    // tucked into the notch beside it. The two corners now read alike.
    [167, 171, '', 8],
    [-196, 172, ''],         // its west face, the door in photo 26
    // Universitetsvägen 1 (photos 7, 14). Measured off the owner's slide 6
    // rather than guessed at. The south front zig-zags: the outer run reaches
    // the corner (57.7, -164.5), and west of it the wall steps back to
    // (52.7, -158.5) and runs level to (44.7, -158.5) — the recess the doors
    // stand in. Laying the slide's blue arrow back over a render taken from
    // the same standpoint puts it about five metres west of that corner, at
    // the east end of the recess and not in the middle of it, so the doors go
    // as far along the recess as they will fit.
    [51.5, -158.5, 'steel', 7],
    [-196, -40, 'canopy'],   // the west plaza (photo 19)
    // The doors with the porthole. They stood two-thirds of the way up the east
    // elevation, on the long straight run at x 151.7, because the point they
    // were asked for fell nearest that wall — and there is nothing there on the
    // plan. The owner's review settles it with the floor plan itself: his arrow
    // lands on the chamfer that closes the east recess, between (149.7, -36.5)
    // and (135.7, -24.5), where the plan draws the little square vestibule just
    // inside the wall. That is the Alfa entrance of photo 5.
    [144.5, -32, 'port'],
    // The south-east corner, and the one place on this building where a room is
    // hung out over the pavement: photo 1 (the owner's slide 7) is taken looking
    // straight up at it. Brick below with the porthole and the green doors under
    // a soffit on a single round concrete column, one storey of glass above it
    // held between an apron and a fascia of sheet metal, and brick again either
    // side. It goes on the ten-metre return that faces the south approach, which
    // is the wall his circle is drawn round.
    [146.7, -146.5, 'box', 9],
    // There used to be one more here, asked for at (176, -18) as "the east
    // side, photo 5". There is no wall at that point: the nearest run long
    // enough to take a door was the one at y 9.5 that faces *south* into the
    // east yard, twenty-nine metres away, and that is where the doors went —
    // a way in on a blank elevation beside the university sign, which is what
    // the owner's slide circles and says to remove. The east side already has
    // its entrance: the Alfa doors on the chamfer above.
  ];
  for (const [px, py, kind, minLen] of wanted) {
    const w = wallNear(px, py, minLen);
    if (!w) continue;
    buildEntrance(w, {
      canopy: kind === 'canopy', steel: kind === 'steel',
      box: kind === 'box', port: kind === 'port' || kind === 'box',
    });
  }
}

function buildEntrance(e, opt) {
  const o = opt || {};
  const g = new THREE.Group();
  const frame = new THREE.Group();
  const gz = groundZ(e.x, e.y);
  frame.position.set(e.x, e.y, gz);
  frame.rotation.z = Math.atan2(e.dy, e.dx);
  const outIsMinusY = (e.nx * -e.dy + e.ny * e.dx) < 0;
  if (outIsMinusY) frame.rotation.z += Math.PI;
  g.add(frame);

  // Never wider than the wall it is set into. A door asked for in a recessed
  // bay used to be given the width of a door on a long elevation and burst out
  // of the reveal on both sides, which read as the wall bulging forward.
  const W = entranceWidth(e.len);
  const D = o.canopy ? 8.5 : 4.6;
  const Z = EXT_STOREY * 0.72;
  const at = (obj, ax, ay, az) => { obj.position.set(ax, ay, az); frame.add(obj); return obj; };

  // glazed screen with door leaves, set into the facade
  const gh = Z - 0.5;
  const glass = new THREE.Mesh(new THREE.PlaneGeometry(W * 0.86, gh), doorMat);
  glass.rotation.x = Math.PI / 2;
  glass.rotation.y = Math.PI;
  at(glass, 0, 0.5, gh / 2);
  const jambMat = steelMat;
  for (const s of [-3, -1, 1, 3]) {
    const mull = new THREE.Mesh(new THREE.BoxGeometry(0.4, 0.5, gh), jambMat);
    at(mull, s * W * 0.10, 0.62, gh / 2);
  }
  at(new THREE.Mesh(new THREE.BoxGeometry(W * 0.88, 0.6, 0.7), jambMat), 0, 0.62, gh);
  // the dark-green door frame the building uses everywhere
  const leaf = new THREE.Mesh(new THREE.BoxGeometry(W * 0.30, 0.30, gh * 0.86), doorMat);
  at(leaf, 0, 0.80, gh * 0.43);
  for (const s2 of [-1, 1]) {
    at(new THREE.Mesh(new THREE.BoxGeometry(0.45, 0.5, gh * 0.86), frameMat),
       s2 * W * 0.15, 0.85, gh * 0.43);
  }
  at(new THREE.Mesh(new THREE.BoxGeometry(W * 0.32, 0.5, 0.5), frameMat), 0, 0.85, gh * 0.86);

  if (o.canopy) {
    const slab = new THREE.Mesh(new THREE.BoxGeometry(W, D, 0.40), trimMat);
    slab.castShadow = true;
    at(slab, 0, D / 2, Z + 0.3);
    at(new THREE.Mesh(new THREE.BoxGeometry(W - 1.6, D - 1.6, 0.22), soffitMat), 0, D / 2, Z + 0.02);
    for (const s of [-1, 0, 1]) {
      const col = new THREE.Mesh(new THREE.CylinderGeometry(0.35, 0.35, Z, 8), steelMat);
      col.rotation.x = Math.PI / 2;
      col.castShadow = true;
      at(col, s * (W / 2 - 2.4), D - 1.6, Z / 2);
    }
  }

  // The south front's doors (photo 14): a pair of dark-green leaves in the
  // brick, under a light steel canopy carried on slim galvanised posts.
  if (o.steel) {
    const cd = 8.0, ch = EXT_STOREY * 0.86;
    const cano = new THREE.Mesh(new THREE.BoxGeometry(W + 6, cd, 0.28), steelMat);
    cano.castShadow = true;
    at(cano, 0, cd / 2 - 0.4, ch);
    for (const s of [-1, -0.34, 0.34, 1]) {
      const post = new THREE.Mesh(new THREE.CylinderGeometry(0.30, 0.30, ch, 8), steelMat);
      post.rotation.x = Math.PI / 2;
      post.castShadow = true;
      at(post, s * (W / 2 + 2.2), cd - 1.2, ch / 2);
    }
    for (const s of [-1, 1]) {                       // the beam over the doors
      const beam = new THREE.Mesh(new THREE.BoxGeometry(0.5, cd, 0.5), steelMat);
      at(beam, s * (W / 2 + 2.2), cd / 2 - 0.4, ch - 0.4);
    }
    // the address pylon, on the side of the doors photo 14 shows it
    buildPylonSign(frame, -(W / 2 + 6.2), cd + 1.4);
  }
  // The one room this building hangs out over its pavement, and the owner's
  // slide 7 is a photograph of it taken from directly underneath (photo 1).
  // Read off that photograph rather than invented:
  //
  //   • it is ONE storey — the first floor and nothing above it. The passes
  //     the owner struck out ran it up two and three floors, which is what he
  //     meant by "no such big glass structure";
  //   • the glass is a single sheet either side of one mullion, turning the
  //     corner on the left of the picture and dying into brick on the right;
  //   • the metal is only the collar: a deep apron under the glass and a
  //     shallower fascia over it, in the same standing-seam sheet as the
  //     skywalk. Every wall around the box is brick;
  //   • it stands on one round concrete column at the outer corner, and the
  //     soffit between column and wall is the flat concrete of the picture;
  //   • the doors and the porthole are under it, in the brick.
  if (o.box) {
    const BW = Math.min(W + 4.0, e.len + 0.5);       // a little wider than the doors,
    const BD = 5.6;                                  // how far it stands out
    const bz = EXT_STOREY * 1.04;                    // the soffit, just over the doors
    const BH = EXT_STOREY * 0.92;                    // one storey and no more
    const APRON = 2.0, FASCIA = 1.9;
    const collar = (h0, h1) => {
      const m = new THREE.Mesh(new THREE.BoxGeometry(BW, BD, h1 - h0), zincMat);
      m.castShadow = true;
      at(m, 0, BD / 2 - 0.4, bz + (h0 + h1) / 2);
    };
    collar(0, APRON);                                // the apron under the glass
    collar(BH - FASCIA, BH);                         // the fascia over it
    const ghh = BH - APRON - FASCIA;
    const gl = new THREE.Mesh(new THREE.BoxGeometry(BW - 0.7, BD - 0.7, ghh), roomGlassMat);
    at(gl, 0, (BD - 0.7) / 2 - 0.4, bz + APRON + ghh / 2);
    const mull = new THREE.Mesh(new THREE.BoxGeometry(0.34, 0.45, ghh), frameMat);
    at(mull, 0, BD - 0.78, bz + APRON + ghh / 2);    // the one mullion in the picture
    for (const s of [-1, 1]) {                       // and the corner posts
      // Square, not deep: a post as deep as the box turns the whole return into
      // a dark green panel from any angle off the axis, and in the photograph
      // the return is glass all the way back to the brick.
      const post = new THREE.Mesh(new THREE.BoxGeometry(0.34, 0.34, ghh), frameMat);
      at(post, s * (BW / 2 - 0.35), BD - 0.78, bz + APRON + ghh / 2);
    }
    // the concrete soffit, and the single round column that carries it
    const sof = new THREE.Mesh(new THREE.BoxGeometry(BW, BD - 0.5, 0.32), concreteMat);
    at(sof, 0, (BD - 0.5) / 2 - 0.4, bz - 0.18);
    const col = new THREE.Mesh(new THREE.CylinderGeometry(0.50, 0.60, bz - 0.34, 14), concreteMat);
    col.rotation.x = Math.PI / 2;
    col.castShadow = true;
    at(col, -BW * 0.38, BD - 1.3, (bz - 0.34) / 2);
  }

  // The porthole beside the door — the one in photo 1, and the one the owner's
  // slide 3 circles on the east elevation before asking for it to be moved.
  if (o.port) {
    const pxo = -W * 0.30;
    const port = new THREE.Mesh(new THREE.CircleGeometry(1.15, 20), doorMat);
    port.rotation.x = Math.PI / 2; port.rotation.y = Math.PI;
    at(port, pxo, 0.45, EXT_STOREY * 0.52);
    const ring = new THREE.Mesh(new THREE.TorusGeometry(1.25, 0.18, 6, 20), frameMat);
    ring.rotation.x = Math.PI / 2;
    at(ring, pxo, 0.5, EXT_STOREY * 0.52);
  }

  // granite steps up to the threshold
  for (let i = 0; i < 3; i++) {
    at(new THREE.Mesh(new THREE.BoxGeometry(W * 0.8 + i * 2.4, 1.9, 0.30), concreteMat),
       0, 1.4 + i * 1.85, -0.15 - i * 0.30);
  }

  shellGroup.add(g);
  shellLiftParts.push({ obj: g, z0: 0, lift: 18 });
  for (const m of [signMat, signBodyMat, doorMat, soffitMat, steelMat, frameMat, roomGlassMat]) {
    if (!shellFadeParts.some(p => p.mat === m)) {
      m.transparent = true;
      shellFadeParts.push({ mat: m, base: m.opacity, ghost: 0 });
    }
  }
}

// ── the skywalk ──────────────────────────────────────────────────────────
// The two halves of the south range are joined over the passage by a glazed
// bridge (photo 27). The plan already carries the first floor across; what it
// cannot say is what the crossing is made of, and the owner has now said it
// twice with a photograph beside it: glass and aluminium, not concrete.
//
// So it is built the way the photograph is glazed — a slim pressed-aluminium
// tray at the sill and a slim one at the head, one band of aluminium spandrel
// over the floor, and everything else glass in aluminium mullions. The heavy
// slabs and the four deep blades that used to face it were what read as
// precast concrete: a metre and a half of grey tray under a metre and a
// quarter of grey head, with the glass between them all but hidden.
//
// Every mesh here is handed its material explicitly. There is no zone test.
function buildSkywalk() {
  const { x0, y0, x1, y1 } = SKYWALK;
  const w = x1 - x0, d = y1 - y0;
  const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2;
  const z0 = storeyZ(1), z1 = storeyZ(2);
  const g = new THREE.Group();
  g.name = 'skywalk';
  g.userData.link = true;                            // aluminium and glass, by name

  const TRAY = 0.55, CAP = 0.5, SPAN = 1.35;         // sill, head, spandrel band
  const tray = new THREE.Mesh(new THREE.BoxGeometry(w + 0.9, d + 0.9, TRAY), linkAlumMat);
  tray.position.set(cx, cy, z0 - TRAY / 2 + 0.1);
  tray.castShadow = true;
  g.add(tray);
  const cap = new THREE.Mesh(new THREE.BoxGeometry(w + 1.1, d + 1.1, CAP), linkAlumMat);
  cap.position.set(cx, cy, z1 + CAP / 2 + 0.15);
  cap.castShadow = true;
  g.add(cap);
  // the spandrel: the one opaque band in the photograph, just over the floor
  const span = new THREE.Mesh(new THREE.BoxGeometry(w + 0.5, d + 0.5, SPAN), linkAlumMat);
  span.position.set(cx, cy, z0 + SPAN / 2 + 0.1);
  g.add(span);
  // and the glass above it, the full height of the crossing
  const gh = (z1 + 0.15) - (z0 + SPAN + 0.1);
  const box = new THREE.Mesh(new THREE.BoxGeometry(w + 0.2, d + 0.2, gh), linkGlassMat);
  box.position.set(cx, cy, z0 + SPAN + 0.1 + gh / 2);
  g.add(box);
  // aluminium mullions down both long faces, and a transom across them
  const n = Math.max(3, Math.round(w / 2.6));
  for (const s of [-1, 1]) {
    for (let i = 0; i <= n; i++) {
      const m = new THREE.Mesh(new THREE.BoxGeometry(0.26, 0.34, gh), linkMullMat);
      m.position.set(x0 + (w * i) / n, cy + s * (d / 2 + 0.16), z0 + SPAN + 0.1 + gh / 2);
      g.add(m);
    }
    const tr = new THREE.Mesh(new THREE.BoxGeometry(w + 0.3, 0.30, 0.26), linkMullMat);
    tr.position.set(cx, cy + s * (d / 2 + 0.16), z0 + SPAN + 0.1 + gh * 0.56);
    g.add(tr);
  }
  shellGroup.add(g);
  shellLiftParts.push({ obj: g, z0: 0, lift: 40 });
  registerLinkFade();
}

// The link materials fade with the rest of the shell when the building opens.
function registerLinkFade() {
  for (const m of [linkAlumMat, linkGlassMat, linkMullMat,
                   linkGlazeMat, linkPanelMat, linkFasciaMat, linkRoofMat]) {
    if (!m || shellFadeParts.some(p => p.mat === m)) continue;
    m.transparent = true;
    shellFadeParts.push({ mat: m, base: m.opacity, ghost: 0 });
  }
}

// ── the glazed link on the plaza ─────────────────────────────────────────
// Photo 10, read off the print rather than remembered:
//
//   · two storeys of glass, sill to eave, nothing opaque but one band;
//   · slim silver mullions at about two and a half metres, and a transom grid
//     across the glass between them;
//   · at the first floor a band of *individual* pressed panels, one to a bay,
//     standing a little proud of the glass line — not a continuous sheet;
//   · a shallow monopitch roof over the lot, low where the link meets the
//     brick and rising away from it, on a thin dark fascia with a pale soffit
//     showing under its overhang;
//   · and the brick of the building read straight through the glass.
//
// It is a lean, transparent thing. The two numbers that matter are that it is
// distinctly *lower* than the three-storey block it runs into, and that its
// head is a sloping line and not a coping.
function buildPlazaLink() {
  const [a, b] = PLAZA_LINK.face;
  const [c, d] = PLAZA_LINK.back;
  const g = new THREE.Group();
  g.name = 'plaza-link';
  g.userData.link = true;                 // glass and aluminium, by name
  // Eaves. The low end is the corner where the link meets the brick that closes
  // the plaza; the high end is where it runs into the three-storey block, whose
  // own parapet stands eight metres higher still. A shallow pitch and nothing
  // more: the first cut ran from 0.07 to 0.36 of a storey and the sheet took off
  // over the yard like a ramp, because the wing behind it has a flat roof and
  // every centimetre of rise is a centimetre of daylight under the sheet. Under
  // four per cent is what the photograph shows.
  //
  // The pitch runs north–south, so the head of *both* screens is one and the
  // same plane: read it off y and the two eaves cannot drift apart, however
  // differently the two ring segments happen to be drawn.
  const eaveHi = storeyZ(2) + EXT_STOREY * 0.190;
  const eaveLo = storeyZ(2) + EXT_STOREY * 0.035;
  const yHi = a[1], yLo = b[1];
  const eaveAt = (y) => eaveLo + ((eaveHi - eaveLo) * (y - yLo)) / (yHi - yLo);
  buildLinkScreen(g, a, b, eaveAt(a[1]), eaveAt(b[1]));
  buildLinkScreen(g, c, d, eaveAt(c[1]), eaveAt(d[1]));
  buildLinkRoof(g, [a, b], [c, d], eaveAt);
  shellGroup.add(g);
  shellLiftParts.push({ obj: g, z0: 0, lift: 30 });
  registerLinkFade();
}

// The line an eave runs along: the wall a→b pushed out by the glass line plus
// the oversail, on the wall's own outward normal.
function linkEaveLine(a, b) {
  const dx = b[0] - a[0], dy = b[1] - a[1], L = Math.hypot(dx, dy) || 1;
  const o = PLAZA_LINK.out + PLAZA_LINK.over;
  const nx = (dy / L) * o, ny = (-dx / L) * o;
  return [[a[0] + nx, a[1] + ny], [b[0] + nx, b[1] + ny]];
}

// One sheet of roof over the whole gallery, from the plaza eave to the
// courtyard eave. It has to be one sheet: two screens each carrying their own
// slab meet somewhere in the middle of a six-metre span and read as two lean-
// tos back to back. The two faces are not quite parallel — the plan splays
// them by a metre over forty — so the sheet is laid as a quadrilateral off the
// two eave lines rather than as a box, and every point of it takes its height
// from the same function of y that the eaves do.
function buildLinkRoof(g, faceA, faceB, eaveAt) {
  const ea = linkEaveLine(faceA[0], faceA[1]);
  const eb = linkEaveLine(faceB[0], faceB[1]);
  // Both edges have to span the same run of the building, so the shorter one
  // is extended along its own line to meet the longer one's ends.
  const at = (line, y) => {
    const [p, q] = line, dy = q[1] - p[1];
    const t = Math.abs(dy) < 1e-6 ? 0 : (y - p[1]) / dy;
    return [p[0] + (q[0] - p[0]) * t, y];
  };
  const yHi = Math.max(ea[0][1], ea[1][1]), yLo = Math.min(ea[0][1], ea[1][1]);
  const T = 0.30;                                   // a hand's breadth thick
  const corners = [at(ea, yHi), at(ea, yLo), at(eb, yLo), at(eb, yHi)];
  const top = corners.map(p => [p[0], p[1], eaveAt(p[1])]);
  const bot = top.map(p => [p[0], p[1], p[2] - T]);
  const pos = [];
  const tri = (p, q, r) => pos.push(...p, ...q, ...r);
  tri(top[0], top[1], top[2]); tri(top[0], top[2], top[3]);
  tri(bot[0], bot[2], bot[1]); tri(bot[0], bot[3], bot[2]);
  for (let i = 0; i < 4; i++) {
    const j = (i + 1) % 4;
    tri(top[i], bot[i], bot[j]); tri(top[i], bot[j], top[j]);
  }
  const geo = new THREE.BufferGeometry();
  geo.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  geo.computeVertexNormals();
  const sheet = new THREE.Mesh(geo, linkRoofMat);
  sheet.castShadow = true;
  g.add(sheet);
}

// One glazed screen, standing on the wall run a→b with its head sloping from
// zA to zB. Built in a local frame: +x runs a→b along the wall, +y points into
// the building, so everything the screen puts in front of the wall has a
// negative y. The wall's own outward normal is (dy, −dx), which is −y here.
function buildLinkScreen(g, a, b, zA, zB) {
  const dx = b[0] - a[0], dy = b[1] - a[1];
  const L = Math.hypot(dx, dy);
  if (L < 4) return;
  const grp = new THREE.Group();
  grp.position.set(a[0], a[1], 0);
  grp.rotation.z = Math.atan2(dy, dx);
  g.add(grp);

  const out = -PLAZA_LINK.out;                       // the glass line, proud of the wall
  const gz = groundZ((a[0] + b[0]) / 2, (a[1] + b[1]) / 2);
  const z0 = gz + 0.08;                              // glass to the ground
  const zb0 = storeyZ(1) - 1.45;                     // spandrel band, at the floor line
  const zb1 = storeyZ(1) + 1.30;
  const eave = (x) => zA + ((zB - zA) * x) / L;
  const HEAD = 0.46;                                 // fascia and soffit take this much
  const n = Math.max(3, Math.round(L / 5.4));        // ≈ 2.4 m bays
  const bw = L / n;

  const add = (mesh, x, y, z) => { mesh.position.set(x, y, z); grp.add(mesh); return mesh; };

  // a slim pressed cill along the foot, the way the photograph starts
  add(new THREE.Mesh(new THREE.BoxGeometry(L, 0.85, 0.20), linkAlumMat),
      L / 2, out + 0.10, gz + 0.02);

  for (let i = 0; i < n; i++) {
    const xc = bw * (i + 0.5), w = bw - 0.34;
    // ground-floor glass
    add(new THREE.Mesh(new THREE.BoxGeometry(w, 0.20, zb0 - z0), linkGlazeMat),
        xc, out, (z0 + zb0) / 2);
    // the one opaque band: a single pressed panel to the bay, set a little proud
    add(new THREE.Mesh(new THREE.BoxGeometry(w - 0.22, 0.40, zb1 - zb0 - 0.26), linkPanelMat),
        xc, out - 0.15, (zb0 + zb1) / 2);
    // first-floor glass, up to the sloping head
    const ht = eave(xc) - HEAD - zb1;
    add(new THREE.Mesh(new THREE.BoxGeometry(w, 0.20, ht), linkGlazeMat),
        xc, out, zb1 + ht / 2);
  }

  // mullions, full height, and the transoms across them
  for (let i = 0; i <= n; i++) {
    const x = Math.min(L - 0.13, Math.max(0.13, bw * i));
    const h = eave(x) - HEAD - z0;
    add(new THREE.Mesh(new THREE.BoxGeometry(0.26, 0.42, h), linkMullMat),
        x, out - 0.05, z0 + h / 2);
  }
  for (const [z, t] of [[zb0, 0.22], [zb1, 0.22], [(z0 + zb0) / 2, 0.18]]) {
    add(new THREE.Mesh(new THREE.BoxGeometry(L, 0.34, t), linkMullMat), L / 2, out - 0.05, z);
  }
  // one transom across the upper glass, following the slope
  {
    const zm0 = zb1 + (zA - HEAD - zb1) * 0.52, zm1 = zb1 + (zB - HEAD - zb1) * 0.52;
    const sl = Math.atan2(zm1 - zm0, L);
    const m = new THREE.Mesh(
      new THREE.BoxGeometry(Math.hypot(L, zm1 - zm0), 0.32, 0.20), linkMullMat);
    m.rotation.y = -sl;
    add(m, L / 2, out - 0.05, (zm0 + zm1) / 2);
  }

  // ── the head ──
  // A fascia and a soffit, and nothing thicker. The sheet itself belongs to
  // neither screen — buildLinkRoof lays one across the gallery — but the eave
  // is part of this elevation and is built with it, so both faces of the link
  // are finished the same way and the head reads as one line all round.
  const dz = zB - zA, Ls = Math.hypot(L, dz), sl = Math.atan2(dz, L);
  const zc = (zA + zB) / 2;
  const OVER = PLAZA_LINK.over;                       // how far the eave oversails
  const fascia = new THREE.Mesh(new THREE.BoxGeometry(Ls, 0.26, 0.42), linkFasciaMat);
  fascia.rotation.y = -sl;
  add(fascia, L / 2, out - OVER + 0.13, zc - 0.36);
  const soffit = new THREE.Mesh(new THREE.BoxGeometry(Ls - 0.2, OVER - 0.34, 0.12), soffitMat);
  soffit.rotation.y = -sl;
  add(soffit, L / 2, out - OVER + 0.26 + (OVER - 0.34) / 2, zc - 0.31);
}

// ── the west skywalk ─────────────────────────────────────────────────────
// The photograph on the owner's slide 10 shows what is actually there: two
// blocks with a gap between them and one short glazed bridge crossing it, high
// up, narrow, and set well back between the two facades.
//
// The pass before this one filled the whole reveal — thirteen metres of glazing
// brought out flush with the wall and standing seven metres proud of the storey
// behind it. From the west lawn that is a glass building cantilevered into the
// air over nothing, and the owner's second review struck it out again: what
// belongs here is *only* a small elevated bridge, and the facade the big volume
// covered has to come back into view.
//
// So the bridge is now one crossing and nothing else: a walkway four metres
// wide carried across the twelve-metre gap at level 2, its face kept a little
// behind the wall line either side so it never oversails the storey below. The
// rest of the recess is left alone — the set-back level-2 wall the plan draws
// is built there already and simply shows again behind the bridge.
const WEST_WALL_X = -188.3;

function buildWestSkywalk() {
  const { y0, y1, xFace, xDeep } = WEST_BRIDGE;
  const cy = (y0 + y1) / 2, len = y1 - y0;           // the span, along the wall
  const D = xDeep - xFace;                           // how wide the walkway is
  const cx = (xFace + xDeep) / 2;
  const g = new THREE.Group();
  g.name = 'west-skywalk';
  // one storey, sitting at the head of the elevation
  const bz0 = storeyZ(2) + EXT_STOREY * 0.16;
  const bh = EXT_STOREY * 0.60;

  // Same rule as its twin over the south passage: glass and aluminium, given
  // to these meshes by name, never looked up from where they happen to stand.
  const tray = new THREE.Mesh(new THREE.BoxGeometry(D, len, 0.55), linkAlumMat);
  tray.position.set(cx, cy, bz0);
  tray.castShadow = true;
  g.add(tray);
  const cap = new THREE.Mesh(new THREE.BoxGeometry(D, len, 0.5), linkAlumMat);
  cap.position.set(cx, cy, bz0 + bh);
  cap.castShadow = true;
  g.add(cap);
  const glass = new THREE.Mesh(new THREE.BoxGeometry(D - 0.3, len - 0.2, bh - 0.55), linkGlassMat);
  glass.position.set(cx, cy, bz0 + bh / 2);
  g.add(glass);
  const n = Math.max(2, Math.round(len / 3.2));      // slim mullions along the span
  for (let i = 0; i <= n; i++) {
    const m = new THREE.Mesh(new THREE.BoxGeometry(0.26, 0.30, bh - 0.55), linkMullMat);
    m.position.set(xFace + 0.20, y0 + (len * i) / n, bz0 + bh / 2);
    g.add(m);
  }
  shellGroup.add(g);
  shellLiftParts.push({ obj: g, z0: 0, lift: 40 });
  registerLinkFade();
}

// The aluminium canopy on the west front does not stop at the doors: photo 19
// runs it the whole way along the elevation to the link and the rowan beyond,
// which is the last of the owner's twelve notes.
function buildWestCanopy() {
  const y0 = -30, y1 = WEST_LINK.y0 + 1;
  const D = 7.2, H = EXT_STOREY * 0.84;
  const cy = (y0 + y1) / 2, len = y1 - y0;
  const g = new THREE.Group();
  g.name = 'west-canopy';
  const gz = groundZ(WEST_WALL_X - D / 2, cy);
  const slab = new THREE.Mesh(new THREE.BoxGeometry(D, len, 0.34), steelMat);
  slab.position.set(WEST_WALL_X - D / 2, cy, gz + H);
  slab.castShadow = true;
  g.add(slab);
  const sof = new THREE.Mesh(new THREE.BoxGeometry(D - 1.3, len - 0.9, 0.22), soffitMat);
  sof.position.set(WEST_WALL_X - D / 2, cy, gz + H - 0.28);
  g.add(sof);
  const n = Math.max(2, Math.round(len / 15));
  for (let i = 0; i <= n; i++) {
    const py = y0 + (len * i) / n;
    const post = new THREE.Mesh(new THREE.CylinderGeometry(0.27, 0.27, H, 8), steelMat);
    post.rotation.x = Math.PI / 2;
    post.position.set(WEST_WALL_X - D + 1.1, py, gz + H / 2);
    post.castShadow = true;
    g.add(post);
  }
  shellGroup.add(g);
  shellLiftParts.push({ obj: g, z0: 0, lift: 18 });
}

// A wireframe of the building's silhouette that stays behind once the shell
// has opened, so the floor plates keep their context.
function buildGhostEdges(base, top) {
  const pts = [];
  const push = (ring, z) => {
    for (let i = 0; i < ring.length; i++) {
      const a = ring[i], b = ring[(i + 1) % ring.length];
      pts.push(a[0], a[1], z, b[0], b[1], z);
    }
  };
  for (const r of base) { push(r.pts, 0.05); push(r.pts, storeyZ(2)); }
  for (const r of top) push(r.pts, storeyZ(3));
  for (const p of base[0].pts) pts.push(p[0], p[1], 0.05, p[0], p[1], storeyZ(2));
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3));
  const m = noTone(new THREE.LineBasicMaterial({
    color: 0x7fb4e8, transparent: true, opacity: 0, depthWrite: false,
  }));
  const l = new THREE.LineSegments(g, m);
  l.userData.ghostMat = m;
  return l;
}

// ── peel driver ──────────────────────────────────────────────────────────
// t = 0 → the solid building. t = 1 → the shell has dissolved to a ghost and
// lifted away, leaving the floor plates.
//
// The whole gesture is choreographed off that one number, in stages that
// overlap: the mathematics-glass volume goes first and drifts aside, the roofs
// slide off after it, and then the elevation opens section by section from the
// top down and from the far side round. Every stage is a pure function of t,
// so the transition plays backwards by running t the other way, an interrupted
// toggle simply picks up the value it is at, and both end states are exactly
// what they were before the choreography existed: at t = 0 every section is at
// its origin and every material at its base opacity; at t = 1 the drifts have
// closed back to zero and only the lift and the ghost opacity remain.
function stage(t, a, b) {
  const u = Math.max(0, Math.min(1, (t - a) / (b - a)));
  return u * u * u * (u * (u * 6 - 15) + 10);      // smootherstep
}
function setShellPeel(t) {
  if (!shellBuilt) return;
  const k = Math.max(0, Math.min(1, t));
  const fade = stage(k, 0.10, 0.76);
  for (const p of shellFadeParts) {
    p.mat.opacity = p.base + (p.ghost - p.base) * fade;
    p.mat.visible = p.mat.opacity > 0.004;
  }
  // the lifts: glass first, then roofs and everything else
  const liftEarly = stage(k, 0.00, 0.34);
  const liftLate = stage(k, 0.16, 0.62);
  for (const p of shellLiftParts) {
    const u = p.early ? liftEarly : liftLate;
    p.obj.position.z = p.z0 + p.lift * u;
    if (p.drift) {
      p.obj.position.x = p.drift[0] * p.lift * u * 0.42;
      p.obj.position.y = p.drift[1] * p.lift * u * 0.42;
      p.obj.rotation.z = p.spin * u;
    }
  }
  // the elevation, section by section: top storey first, and the four faces
  // a beat apart, each pushed a little out of the building as it goes
  const settle = 1 - stage(k, 0.84, 1.0);
  for (const s of wallSections) {
    const start = 0.24 + (2 - s.storey) * 0.07 + ((s.face + 2) % 4) * 0.035;
    const u = stage(k, start, Math.min(0.98, start + 0.34)) * settle;
    s.grp.position.set(s.nx * 26 * u, s.ny * 26 * u, u * 5.5);
  }
  if (ghostEdges) {
    ghostEdges.userData.ghostMat.opacity = 0.34 * stage(k, 0.45, 1.0);
    ghostEdges.visible = k > 0.46;
  }
  shellGroup.visible = k < 0.999 || (ghostEdges && ghostEdges.visible);
}

// The opening shot: standing off the south-east corner on the plaza, near eye
// level and looking slightly up, so the long elevation and the corner that
// turns away are both in frame.
function heroCameraShot() {
  if (!baseRings || !baseRings.length) {
    return { pos: new THREE.Vector3(320, -320, 220), look: new THREE.Vector3(0, 0, 10) };
  }
  const bb = ringBBox(baseRings[0].pts);
  const a = viewerW() / Math.max(1, viewerH());
  const back = a < 1 ? 1.55 : a < 1.4 ? 1.2 : 1;
  const look = new THREE.Vector3(bb.maxX - 40, bb.maxY - 60, storeyZ(3) * 0.5);
  const pos = new THREE.Vector3(
    look.x + 150 * back, look.y + 170 * back, 40 * (back * 0.7 + 0.3));
  return { pos, look };
}

function entranceCameraShot() {
  if (!entranceInfo) return heroCameraShot();
  const e = entranceInfo;
  const gz = groundZ(e.x, e.y);
  return {
    pos: new THREE.Vector3(e.x + e.nx * 62 + e.dx * 26, e.y + e.ny * 62 + e.dy * 26, gz + 14),
    look: new THREE.Vector3(e.x, e.y, gz + EXT_STOREY * 0.7),
  };
}
