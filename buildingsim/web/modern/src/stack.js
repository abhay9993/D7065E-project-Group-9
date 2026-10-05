// stack.js — the classic stacked-plates view.
//
// Before the exterior existed, the viewer was three flat floor plans hanging
// one above the other in the dark: no facade, no slab, no walls, just the
// drawings. It reads a whole building at a glance in a way the modelled
// version cannot, so it is back as a view of its own rather than as history.
//
// It is not a fourth geometry. The plates are exactly where the open building
// puts them (peelT = 1, tier i at i * FLOOR_SPACING), and everything that
// lives inside a floor container — room polygons, linework, labels, equipment,
// the heat overlay — rides along untouched. The view is subtractive: the
// shell, the campus, the slabs and the extruded partitions are switched off,
// and switched back on again on the way out.

let stackOn = false;
let stackApplying = false;      // our own calls must not trip the exit hooks
let stackPrev = null;           // the mode to come back to

function wallsChecked() {
  const e = document.getElementById('showWalls');
  return e ? e.checked : true;
}

// The subtraction, in one place: called on entry, on exit, and again from
// gateOverlays so that nothing which turns the shell back on for its own
// reasons can leave the exterior standing over the plates.
function applyStackVisibility() {
  LEVELS.forEach(l => {
    const fg = floorGroups[l];
    if (!fg) return;
    if (fg.slabGroup) fg.slabGroup.visible = !stackOn;
    if (fg.extrudeGroup) fg.extrudeGroup.visible = stackOn ? false : wallsChecked();
  });
  if (stackOn) {
    if (typeof shellGroup !== 'undefined') shellGroup.visible = false;
    if (typeof worldGroup !== 'undefined') worldGroup.visible = false;
  }
}

// A shot that holds the whole stack: the same shallow three-quarter angle the
// open building uses, which is what makes the plates read as a stack rather
// than as one plan seen from above — but standing further off. A-huset is a
// long plan, and the overview distance is framed for a building with a facade
// to hide its ends; here the drawings run right to the edge of the frame.
const STACK_BACK = 1.45;

function stackShot() {
  const s = overviewShot();
  const look = s.look.clone();
  const dir = s.pos.clone().sub(look);
  const a = viewerW() / Math.max(1, viewerH());
  const back = STACK_BACK * (a < 1 ? 1.6 : a < 1.4 ? 1.2 : 1);
  return { pos: look.clone().add(dir.multiplyScalar(back)), look };
}

function enterStack() {
  stackPrev = {
    is3D: is3DView,
    level: currentLevel,
    closed: peelT < 0.5 || (peelAnim !== null && peelTarget < 0.5),
  };
  stackApplying = true;
  try {
    if (!is3DView) switchTo3D();
    stackOn = true;
    viewMode = 'interior';
    LEVELS.forEach(l => noteWantVisible(l, true));
    // Hide the exterior before the plates move, so the transition is never a
    // glimpse of the building coming apart.
    applyStackVisibility();
    setPeel(1);                       // also re-runs gateOverlays()
    applyStackVisibility();
  } finally { stackApplying = false; }
  const s = stackShot();
  animateCamera3D(s.pos, s.look, 900);
  syncHud();
  render();
}

// restore = true puts the viewer back where it was before the stack (the chip
// switched off by hand); false only takes the view apart, for the commands
// that are on their way somewhere else themselves.
function exitStack(restore) {
  if (!stackOn) return;
  stackOn = false;
  const prev = stackPrev;
  stackPrev = null;
  applyStackVisibility();
  if (typeof worldGroup !== 'undefined') worldGroup.visible = is3DView;
  if (typeof shellGroup !== 'undefined') shellGroup.visible = is3DView;
  stackApplying = true;
  try {
    setPeel(peelT);                   // hands the shell and the overlays back
    if (restore && prev) {
      if (!prev.is3D && prev.level) switchToFloor(prev.level);
      else if (prev.closed) closeBuilding();
      else { const s = stackShot(); animateCamera3D(s.pos, s.look, 700); }
    }
  } finally { stackApplying = false; }
  syncHud();
  render();
}

function setStack(on) {
  const want = on === undefined ? !stackOn : !!on;
  if (want === stackOn) { if (want) { const s = stackShot(); animateCamera3D(s.pos, s.look, 700); } return; }
  if (want) enterStack(); else exitStack(true);
}

// ── keeping the exterior down ────────────────────────────────────────────
// gateOverlays is the one function every path through the viewer ends in, so
// it is where the subtraction is re-asserted.
const _st_gateOverlays = gateOverlays;
gateOverlays = function () {
  _st_gateOverlays();
  if (!stackOn) return;
  applyStackVisibility();
  if (typeof stairConnectors !== 'undefined') stairConnectors.visible = false;
  if (typeof elevatorConnectors !== 'undefined') elevatorConnectors.visible = false;
};

// ── the ways out ─────────────────────────────────────────────────────────
// Building, 3D and the floor chips all mean "somewhere else": the stack comes
// down first and the command then runs exactly as it always did. Route flights
// need the building whole as well.
function leaveStack() { if (stackOn && !stackApplying) exitStack(false); }

const _st_switchTo3D = switchTo3D;
switchTo3D = function () { leaveStack(); _st_switchTo3D(); };

const _st_switchToFloor = switchToFloor;
switchToFloor = function (level) { leaveStack(); _st_switchToFloor(level); };

const _st_closeBuilding = closeBuilding;
closeBuilding = function (opts) { leaveStack(); _st_closeBuilding(opts); };

const _st_flyRoute = flyRoute;
flyRoute = function () { leaveStack(); _st_flyRoute(); };

// openBuilding is deliberately not hooked: the stack *is* open (peelT = 1), so
// heat, highlights and routes can ask for the building to open without the
// view being taken away underneath them.

// ── the chip ─────────────────────────────────────────────────────────────
const STACK_ICON = '<svg viewBox="0 0 20 20" aria-hidden="true">'
  + '<rect x="2.4" y="12.6" width="12.2" height="3.6" rx="1"/>'
  + '<rect x="4.3" y="8" width="12.2" height="3.6" rx="1"/>'
  + '<rect x="6.2" y="3.4" width="12.2" height="3.6" rx="1"/></svg>';

let hudChipStack = null;

const _st_buildHud = buildHud;
buildHud = function () {
  _st_buildHud();
  if (hudChipStack) return;
  const chips = document.getElementById('hudFloors');
  if (!chips) return;
  const b = document.createElement('button');
  b.id = 'btnStack';
  b.className = 'hud-chip icon';
  b.innerHTML = STACK_ICON;
  b.title = 'Stack view: the flat floor plans, without the building';
  b.setAttribute('aria-label', 'Stack view');
  b.addEventListener('click', () => { setStack(); });
  // right after the 3D chip, where the poses of the building live
  if (hudChip3D && hudChip3D.nextSibling) chips.insertBefore(b, hudChip3D.nextSibling);
  else chips.appendChild(b);
  hudChipStack = b;
  syncHud();
};

const _st_syncHud = syncHud;
syncHud = function () {
  _st_syncHud();
  if (hudChipStack) hudChipStack.classList.toggle('on', stackOn);
  // In the stack the building has no pose, so 3D does not claim to be on.
  if (stackOn && hudChip3D) hudChip3D.classList.remove('on');
};

// ── the console handle ───────────────────────────────────────────────────
window.buildsim.stack = (b) => { setStack(b); return stackOn; };
if (window.buildsim.dev) {
  Object.defineProperty(window.buildsim.dev, 'stackOn', { get: () => stackOn });
}
