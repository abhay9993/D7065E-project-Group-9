// How far above a floor the route tube is drawn, so it reads over the plan.
// The first-person flight subtracts it again to sit at head height.
const ROUTE_Z = 5;

// The way through the building, in the colour a fire plan uses for it.
const ROUTE_COLOR = 0xe02020;   // the line itself
const ROUTE_GLOW  = 0xff6a5a;   // the casing just outside it
const ROUTE_START = 0x2bff7a;   // where you are
const ROUTE_END   = 0xffc21e;   // where you are going

// Search
let highlightedMesh = null;
function clearHighlight() {
  if (highlightedMesh) {
    const li = highlightedMesh.userData._levelIdx;
    const type = highlightedMesh.userData.type;
    highlightedMesh.material.color.setHex(type === 'corridor' ? CORRIDOR_COLOR : FLOOR_COLORS[li !== undefined ? li : 0]);
    highlightedMesh.material.opacity = 0.4;
    highlightedMesh = null;
  }
}

function searchAndZoom(roomName, level) {
  clearHighlight();
  const data = floorData[level];
  if (!data) return;
  const room = data.rooms.find(r => r.name === roomName);
  if (!room) return;
  _lastSearchedRoom = { name: roomName, level };

  const levelIdx = LEVELS.indexOf(level);
  const pw = data.page.width, ph = data.page.height;
  const [wx, wy] = pdfToWorld(room.center[0], room.center[1], pw, ph);

  // Highlight the room in bright green
  // Make all floors visible first so we can see it in 3D
  LEVELS.forEach((l, i) => {
    const fg = floorGroups[l];
    if (fg) { const a = FLOOR_ALIGN[i]||{x:0,y:0}; fg.container.visible = true; fg.container.position.set(a.x, a.y, i * FLOOR_SPACING); }
  });

  const fg = floorGroups[level];
  const mesh = fg.meshes.find(m => m.userData.name === roomName);
  if (mesh) {
    mesh.userData._levelIdx = levelIdx;
    mesh.material.color.setHex(0x00ff88);
    mesh.material.opacity = 0.7;
    highlightedMesh = mesh;
  }

  setInfoCard(`<span style="color:#9fb3cc">Floor ${levelIdx}</span>`, roomName);

  if (is3DView) {
    _eqFilterRoom = roomName;
    filterEquipment();

    // 3D fly-in: move camera to look at the room
    const targetPos = new THREE.Vector3(wx + 80, wy - 80, levelIdx * FLOOR_SPACING + 120);
    const targetLook = new THREE.Vector3(wx, wy, levelIdx * FLOOR_SPACING);
    const startPos = camera3D.position.clone();
    const startLook = orbitControls.target.clone();
    const duration = 1500;
    const startTime = performance.now();

    function animate3D(now) {
      const t = Math.min(1, (now - startTime) / duration);
      const ease = t < 0.5 ? 2*t*t : -1+(4-2*t)*t;
      camera3D.position.lerpVectors(startPos, targetPos, ease);
      orbitControls.target.lerpVectors(startLook, targetLook, ease);
      orbitControls.update();
      render();
      if (t < 1) requestAnimationFrame(animate3D);
    }
    requestAnimationFrame(animate3D);
  } else {
    // Switch to floor, show collapsed icon for searched room
    switchToFloor(level);
    _eqFilterRoom = roomName;
    filterEquipment();

    const startFrustum = frustumSize;
    const startPan = panOffset.clone();
    const targetFrustum = 80;
    const targetPan = new THREE.Vector2(wx, wy);
    const duration = 1000;
    const startTime = performance.now();

    function animate2D(now) {
      const t = Math.min(1, (now - startTime) / duration);
      const ease = t < 0.5 ? 2*t*t : -1+(4-2*t)*t;
      frustumSize = startFrustum + (targetFrustum - startFrustum) * ease;
      panOffset.lerpVectors(startPan, targetPan, ease);
      updateCamera2D();
      render();
      if (t < 1) requestAnimationFrame(animate2D);
    }
    requestAnimationFrame(animate2D);
  }
}

document.getElementById('searchRoom').addEventListener('input', (e) => {
  const q = e.target.value.trim().toLowerCase();
  const results = document.getElementById('searchResults');
  if (q.length < 2) { results.innerHTML = ''; return; }

  let matches = [];
  for (const level of LEVELS) {
    const data = floorData[level];
    if (!data) continue;
    for (const room of data.rooms) {
      if (room.name && room.name.toLowerCase().includes(q)) {
        matches.push({ name: room.name, level, levelIdx: LEVELS.indexOf(level) });
      }
    }
  }
  matches = matches.slice(0, 10);

  const items = matches.map(m => {
    const item = document.createElement('div');
    item.className = 'pop-item';
    const name = document.createElement('span');
    name.textContent = m.name;
    const level = document.createElement('span');
    level.className = 'pop-dim';
    level.textContent = `Floor ${m.levelIdx}`;
    item.append(name, level);
    item.addEventListener('click', () => window._searchZoom(m.name, m.level));
    return item;
  });
  results.replaceChildren(...items);
});

window._searchZoom = searchAndZoom;

// === Route finding ===
let routeGroup = new THREE.Group();
scene.add(routeGroup);
let routeHighlightedMeshes = [];
let lastRouteResult = null;
let lastRouteLevelKey = null;
let lastRoutePoints = null;

// Seen from above, the route belongs a little over the plan — ROUTE_Z up —
// so it reads across the whole floor. Seen from inside at eye level it belongs
// on the ground in front of you: at head height a line this thick is a pipe
// through the lens rather than a way through the building. The route flight
// drops the whole group for as long as it runs.
function setRouteEyeLevel(on) {
  routeGroup.position.z = on ? -(ROUTE_Z - 0.35) : 0;
  render();
}

function clearRoute() {
  while(routeGroup.children.length>0){const c=routeGroup.children[0];routeGroup.remove(c);c.geometry.dispose();c.material.dispose();}
  routeHighlightedMeshes.forEach(m=>{
    const li = m.userData._levelIdx;
    const type = m.userData.type;
    m.material.color.setHex(type==='corridor'?CORRIDOR_COLOR:FLOOR_COLORS[li!==undefined?li:0]);
    m.material.opacity=0.4;
  });
  routeHighlightedMeshes=[];
  lastRouteResult = null;
  lastRouteLevelKey = null;
  lastRoutePoints = null;
  if (typeof hideRouteFlightChip === 'function') hideRouteFlightChip();
  document.getElementById('routeInfo').innerHTML='';
  document.getElementById('btnClearRoute').style.display='none';
}

async function findAndShowRoute(fromName, toName, fromLevelHint = '', toLevelHint = '') {
  clearRoute(); clearHighlight();

  // Resolve each endpoint without silently picking the first floor when a
  // label is reused. Autocomplete supplies the floor; manually typed unique
  // labels are still convenient.
  const fromMatches = [], toMatches = [];
  for (const l of LEVELS) {
    const data = floorData[l];
    if (!data) continue;
    const levelKey = l.split('/').pop();
    if (data.rooms.find(r => r.name === fromName) && !fromMatches.includes(levelKey)) fromMatches.push(levelKey);
    if (data.rooms.find(r => r.name === toName) && !toMatches.includes(levelKey)) toMatches.push(levelKey);
  }
  const resolveLevel = (matches, hint) => hint && matches.includes(hint) ? hint : (matches.length === 1 ? matches[0] : null);
  const fromLevel = resolveLevel(fromMatches, fromLevelHint);
  const toLevel = resolveLevel(toMatches, toLevelHint);

  if (!fromLevel || !toLevel) {
    const info = document.getElementById('routeInfo');
    info.style.color = '#e04040';
    const ambiguous = (!fromLevel && fromMatches.length > 1) || (!toLevel && toMatches.length > 1);
    info.textContent = ambiguous ? 'That room name exists on several floors; choose a floor-labelled suggestion.' :
      'Room not found: ' + (!fromLevel ? fromName + ' ' : '') + (!toLevel ? toName : '');
    return;
  }

  // Use a single-floor route when possible and qualified endpoints otherwise.
  try {
    const url = fromLevel === toLevel
      ? `/api/graph/route?from_name=${encodeURIComponent(fromName)}&to_name=${encodeURIComponent(toName)}&level=${fromLevel}&type=walkable`
      : `/api/graph/route?from_name=${encodeURIComponent(fromName)}&from_level=${encodeURIComponent(fromLevel)}&to_name=${encodeURIComponent(toName)}&to_level=${encodeURIComponent(toLevel)}&type=walkable`;
    const resp = await fetch(url);
    if (!resp.ok) {
      const err = await resp.json();
      const info = document.getElementById('routeInfo');
      info.style.color = '#e04040';
      info.textContent = err.error || 'No route found';
      return;
    }
    const result = await resp.json();

    // Check if route spans multiple floors
    const levels = new Set(result.path.map(n => n.level).filter(Boolean));
    const isMultiFloor = levels.size > 1;

    if (isMultiFloor && !is3DView) switchTo3D();

    // Seen from outside, the way through the building is inside a closed box.
    // Open it first, the same as for a remotely selected route, so the
    // line the camera then flies to is actually on screen.
    const draw = () => renderSmoothRoute(result, isMultiFloor ? null : fromLevel, true);
    if (is3DView && typeof openBuilding === 'function' && peelT < 0.9) openBuilding({ then: draw });
    else draw();
  } catch(e) {
    const info = document.getElementById('routeInfo');
    info.style.color = '#e04040';
    info.textContent = 'Error: ' + e.message;
  }
}

// Deterministically frame an entire route: fit the camera to the path's
// bounding box so the whole walk is visible. Honors the current 2D/3D mode.
function fitCameraToRoute(points) {
  if (!points || points.length < 2) return;
  let minX = Infinity, minY = Infinity, minZ = Infinity;
  let maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
  for (const p of points) {
    minX = Math.min(minX, p.x); maxX = Math.max(maxX, p.x);
    minY = Math.min(minY, p.y); maxY = Math.max(maxY, p.y);
    minZ = Math.min(minZ, p.z); maxZ = Math.max(maxZ, p.z);
  }
  const cx = (minX + maxX) / 2, cy = (minY + maxY) / 2, cz = (minZ + maxZ) / 2;
  const aspect = viewerW() / viewerH();

  if (is3DView) {
    // Fit a bounding sphere within both the vertical and horizontal FOV.
    const r = 0.5 * Math.hypot(maxX - minX, maxY - minY, maxZ - minZ);
    const vfov = camera3D.fov * Math.PI / 180;
    const hfov = 2 * Math.atan(Math.tan(vfov / 2) * aspect);
    const dist = Math.max(r / Math.sin(vfov / 2), r / Math.sin(hfov / 2), 60) * 1.25;
    const dir = new THREE.Vector3(0.6, -0.7, 0.85).normalize();
    const pos = new THREE.Vector3(cx, cy, cz).addScaledVector(dir, dist);
    animateCamera3D(pos, new THREE.Vector3(cx, cy, cz), 1200);
  } else {
    // Orthographic: frustumSize is the visible vertical world height.
    const w = maxX - minX, h = maxY - minY;
    const targetFrustum = Math.max(h, w / aspect, 40) * 1.35;
    animateCamera2D(cx, cy, targetFrustum, 1200);
  }
}

function renderSmoothRoute(result, levelKey, fit) {
  lastRouteResult = result;
  lastRouteLevelKey = levelKey;
  const defaultData = floorData[LEVELS[0]];
  if (!defaultData) return;

  // Convert path to world coordinates, handling per-node level
  const rawPoints = result.path.map(n => {
    const nodeLevel = n.level
      ? LEVELS.find(l => l.endsWith(n.level)) || LEVELS[0]
      : (levelKey ? LEVELS.find(l => l.endsWith(levelKey)) || LEVELS[0] : LEVELS[0]);
    const data = floorData[nodeLevel] || defaultData;
    const pw = data.page.width, ph = data.page.height;
    const levelIdx = LEVELS.indexOf(nodeLevel);
    // Ride the floor spread: each node lifts with its own storey, so the
    // route stays attached to its floors and stretches between them.
    const lift = (is3DView && typeof floorLift === 'function') ? floorLift(levelIdx) : 0;
    const z = is3DView ? levelIdx * FLOOR_SPACING + ROUTE_Z + lift : 6;
    const [wx, wy] = pdfToWorld(n.x, n.y, pw, ph);
    const align = is3DView ? (FLOOR_ALIGN[levelIdx] || {x:0,y:0}) : {x:0,y:0};
    return new THREE.Vector3(wx + align.x, wy - align.y, z);
  });

  if (rawPoints.length < 2) return;
  // kept so the camera can fly the same line the tube was built from
  lastRoutePoints = rawPoints;

  // Build rounded-corner path: straight segments with arc fillets at turns
  const cornerRadius = 1.5;
  const smoothPoints = [];

  for (let i = 0; i < rawPoints.length; i++) {
    if (i === 0 || i === rawPoints.length - 1) {
      smoothPoints.push(rawPoints[i].clone());
      continue;
    }
    const prev = rawPoints[i - 1];
    const curr = rawPoints[i];
    const next = rawPoints[i + 1];

    // Vectors to previous and next
    const toPrev = new THREE.Vector3().subVectors(prev, curr).normalize();
    const toNext = new THREE.Vector3().subVectors(next, curr).normalize();

    // Distance to neighbors
    const distPrev = prev.distanceTo(curr);
    const distNext = next.distanceTo(curr);
    const r = Math.min(cornerRadius, distPrev * 0.4, distNext * 0.4);

    // Points before and after the corner
    const before = curr.clone().addScaledVector(toPrev, r);
    const after = curr.clone().addScaledVector(toNext, r);

    smoothPoints.push(before);
    // Add arc points between before and after
    for (let t = 0.25; t <= 0.75; t += 0.25) {
      const p = new THREE.Vector3().lerpVectors(before, after, t);
      // Pull toward the corner point slightly for a rounder curve
      p.lerp(curr, 0.15);
      smoothPoints.push(p);
    }
    smoothPoints.push(after);
  }

  // Build tube from the smooth points.
  //
  // The route has to be findable at a glance from across a 170 m building and
  // from inside a corridor, so it is drawn in signal red and thick enough to
  // hold its width when the whole plan is on screen. A paler red casing sits
  // just outside it: against the dark plan it reads as a glow, and against a
  // pale wall it keeps the line from disappearing into the surface.
  const curve = new THREE.CatmullRomCurve3(smoothPoints, false, 'chordal', 0.5);
  const seg = smoothPoints.length * 4;
  // Two passes. The solid line is depth-tested, so walls and floor plates in
  // front of it genuinely hide it — the route lies *in* the building, on its
  // own storey, instead of being painted over the whole model. Where it is
  // hidden, the ghost pass (no depth test, faint) still lets the eye follow
  // it through the structure. Both share one geometry.
  const tubeGeo = new THREE.TubeGeometry(curve, seg, 1.0, 10, false);
  const tube = new THREE.Mesh(tubeGeo,
    noTone(new THREE.MeshBasicMaterial({ color: ROUTE_COLOR })));
  routeGroup.add(tube);

  const ghost = new THREE.Mesh(tubeGeo,
    noTone(new THREE.MeshBasicMaterial({
      color: ROUTE_GLOW, depthTest: false, depthWrite: false,
      transparent: true, opacity: 0.16 })));
  ghost.renderOrder = 999;
  routeGroup.add(ghost);

  // Start marker (green sphere), same two-pass treatment as the line
  const dotGhost = (mesh, color) => {
    const g = new THREE.Mesh(mesh.geometry,
      noTone(new THREE.MeshBasicMaterial({
        color, depthTest: false, depthWrite: false,
        transparent: true, opacity: 0.30 })));
    g.position.copy(mesh.position);
    g.renderOrder = 1001;
    return g;
  };
  const startDot = new THREE.Mesh(new THREE.SphereGeometry(2.2, 16, 16),
    noTone(new THREE.MeshBasicMaterial({ color: ROUTE_START })));
  startDot.position.copy(rawPoints[0]);
  routeGroup.add(startDot);
  routeGroup.add(dotGhost(startDot, ROUTE_START));

  // End marker — amber, so it stays its own mark on a red line
  const endDot = new THREE.Mesh(new THREE.SphereGeometry(2.2, 16, 16),
    noTone(new THREE.MeshBasicMaterial({ color: ROUTE_END })));
  endDot.position.copy(rawPoints[rawPoints.length - 1]);
  routeGroup.add(endDot);
  routeGroup.add(dotGhost(endDot, ROUTE_END));

  // Highlight start and end rooms
  const firstName = result.path[0]?.name;
  const lastName = result.path[result.path.length - 1]?.name;
  const firstLevel = result.path[0]?.level;
  const lastLevel = result.path[result.path.length - 1]?.level;

  for (const [name, hexColor, lvl] of [[firstName, ROUTE_START, firstLevel], [lastName, ROUTE_END, lastLevel]]) {
    const fl = lvl ? LEVELS.find(l => l.endsWith(lvl)) : (levelKey ? LEVELS.find(l => l.endsWith(levelKey)) : null);
    if (!fl) continue;
    const fg = floorGroups[fl];
    if (!fg) continue;
    const mesh = fg.meshes.find(m => m.userData.name === name);
    if (mesh) {
      mesh.material.color.setHex(hexColor);
      mesh.material.opacity = 0.7;
      routeHighlightedMeshes.push(mesh);
    }
  }

  // Route description
  const levels = new Set(result.path.map(n => n.level).filter(Boolean));
  let desc = `<b style="color:#2bff7a">${firstName}</b> → <b style="color:#ffc21e">${lastName}</b><br>`;
  desc += `${result.path.length} nodes, distance: ${result.distance.toFixed(1)}`;
  if (levels.size > 1) desc += `<br>Floors: ${[...levels].join(' → ')}`;
  document.getElementById('routeInfo').innerHTML = desc;
  document.getElementById('btnClearRoute').style.display = 'block';

  if (fit) fitCameraToRoute(rawPoints);
  render();
}

// Autocomplete for route inputs
function setupRouteAutocomplete(inputId) {
  const input = document.getElementById(inputId);
  let dropdown = null;

  input.addEventListener('input', () => {
    delete input.dataset.routeLevel;
    if(dropdown) dropdown.remove();
    const q = input.value.trim().toLowerCase();
    if(q.length<2) return;

    let matches = [];
    for(const level of LEVELS) {
      const data = floorData[level];
      if(!data) continue;
      const levelKey = level.split('/').pop();
      const levelIdx = LEVELS.indexOf(level);
      for(const r of data.rooms) {
        if(r.name && r.name.toLowerCase().includes(q))
          matches.push({name:r.name, level:levelKey, levelIdx});
      }
    }
    matches = [...new Map(matches.map(m => [`${m.level}:${m.name}`, m])).values()].slice(0,8);
    if(!matches.length) return;

    dropdown = document.createElement('div');
    // .hud-ac keeps the toolbar popover open while you pick from this list.
    dropdown.className = 'hud-ac';
    const rect = input.getBoundingClientRect();
    dropdown.style.left = rect.left+'px';
    dropdown.style.width = rect.width+'px';

    for(const m of matches) {
      const d = document.createElement('div');
      d.textContent = `${m.name} · F${m.levelIdx}`;
      d.className = 'pop-item';
      d.onclick = ()=>{input.value=m.name; input.dataset.routeLevel=m.level; dropdown.remove(); dropdown=null;};
      dropdown.appendChild(d);
    }
    document.body.appendChild(dropdown);
    // These inputs live in a popover at the bottom of the screen, so the list
    // drops upwards — below the field it would land on the popover's own
    // buttons.
    const h = dropdown.offsetHeight;
    dropdown.style.top = (rect.top > window.innerHeight * 0.5
      ? Math.max(4, rect.top - h - 4) : rect.bottom + 2) + 'px';
  });

  input.addEventListener('blur', ()=>{setTimeout(()=>{if(dropdown){dropdown.remove();dropdown=null;}},200);});
}

setupRouteAutocomplete('routeFrom');
setupRouteAutocomplete('routeTo');

document.getElementById('btnFindRoute').addEventListener('click', () => {
  const fromInput = document.getElementById('routeFrom');
  const toInput = document.getElementById('routeTo');
  const from = fromInput.value.trim();
  const to = toInput.value.trim();
  if(!from || !to) { document.getElementById('routeInfo').innerHTML='<span style="color:#e04040">Enter both rooms</span>'; return; }
  try {
    document.getElementById('routeInfo').innerHTML='Finding route...';
    setTimeout(() => findAndShowRoute(from, to, fromInput.dataset.routeLevel, toInput.dataset.routeLevel), 50);
  } catch(e) {
    console.error('Route error:', e);
    const info = document.getElementById('routeInfo');
    info.style.color = '#e04040';
    info.textContent = 'Error: ' + e.message;
  }
});

document.getElementById('btnClearRoute').addEventListener('click', () => {
  clearRoute(); render();
});

// Enter key triggers route finding
document.getElementById('routeTo').addEventListener('keydown', (e) => {
  if(e.key==='Enter') document.getElementById('btnFindRoute').click();
});
document.getElementById('routeFrom').addEventListener('keydown', (e) => {
  if(e.key==='Enter') document.getElementById('routeTo').focus();
});

// Enter takes the first hit — the common case is that you typed the room you
// already know the name of.
document.getElementById('searchRoom').addEventListener('keydown', (e) => {
  if (e.key !== 'Enter') return;
  const first = document.getElementById('searchResults').querySelector('div');
  if (first) first.click();
});
