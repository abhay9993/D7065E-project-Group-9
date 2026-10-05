let editModeEnabled = false;
let sessionId = null;
let sessionWs = null;
let sessionClosing = false;
let localEquipmentVersion = 0;
let localOccupancyVersion = 0;
let globalOccupancyState = {};
let sessionOccupancyState = {};
let globalCoverageState = [];
let sessionCoverageState = [];
const EQUIPMENT_REFRESH_INTERVAL_MS = 100;
let equipmentRefreshTimer = null;
let equipmentRefreshInFlight = false;
let equipmentRefreshPending = false;

window._buildsimDiagnostics = {
  equipmentNotifications: 0,
  equipmentRefreshes: 0,
  lastNotifiedEquipmentVersion: 0,
  lastAppliedEquipmentVersion: 0,
  equipmentCount: 0,
  sensorValues: {},
  websocketState: 'disconnected',
};

async function initSession() {
  try {
    const resp = await fetch('/api/sessions', { method: 'POST' });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    const sess = await resp.json();
    sessionId = sess.id;
    window._viewerSessionId = sessionId;
    updateSessionChip(sessionId);
    sessionClosing = false;
    console.log('Session created:', sessionId);
    connectWebSocket();
  } catch(e) {
    console.log('Failed to create session:', e);
    updateSessionChip(null, 'unavailable');
  }
}

function updateSessionChip(id, status) {
  const chip = document.getElementById('sessionChip');
  const label = document.getElementById('sessionIdLabel');
  if (!chip || !label) return;
  if (id) {
    label.textContent = id.slice(0, 8);
    chip.title = `Session ${id} — click to copy`;
    chip.disabled = false;
  } else {
    label.textContent = status || 'connecting…';
    chip.disabled = true;
  }
}

async function copySessionId() {
  if (!sessionId) return;
  let copied = false;
  try {
    await navigator.clipboard.writeText(sessionId);
    copied = true;
  } catch (_) {
    const input = document.createElement('textarea');
    input.value = sessionId;
    input.setAttribute('readonly', '');
    input.style.position = 'fixed';
    input.style.opacity = '0';
    document.body.appendChild(input);
    input.select();
    copied = document.execCommand('copy');
    input.remove();
  }
  if (!copied) return;
  const label = document.getElementById('sessionIdLabel');
  if (!label) return;
  label.textContent = 'copied';
  setTimeout(() => {
    if (sessionId) label.textContent = sessionId.slice(0, 8);
  }, 1200);
}

document.getElementById('sessionChip')?.addEventListener('click', copySessionId);

function connectWebSocket() {
  if (!sessionId || sessionClosing) return;
  const id = sessionId;
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  sessionWs = new WebSocket(`${proto}//${location.host}/ws/${id}`);
  let wasConnected = false;

  sessionWs.onopen = () => {
    wasConnected = true;
    window._buildsimDiagnostics.websocketState = 'connected';
    console.log('WebSocket connected to session:', id);
  };
  sessionWs.onclose = () => {
    window._buildsimDiagnostics.websocketState = 'disconnected';
    if (!sessionClosing && sessionId === id && wasConnected) {
      console.log('WebSocket disconnected, reconnecting in 3s...');
      setTimeout(connectWebSocket, 3000);
    } else if (!wasConnected) {
      console.log('WebSocket failed to connect (session may not exist)');
    }
  };
  sessionWs.onerror = () => {}; // suppress console error, onclose handles it
  sessionWs.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      handleSessionMessage(msg);
    } catch(e) {
      console.log('Bad WS message:', e);
    }
  };
}

function handleSessionMessage(msg) {
  switch (msg.type) {
    case 'viewport':
      // A viewer creates a fresh session whose version-0 viewport is only a
      // server-side default. Reapplying it after boot can overwrite a URL
      // deep link. Real remote viewport commands start at version 1.
      if ((msg.version || 0) > 0) applyViewport(msg.data);
      break;
    case 'highlights':
      applyHighlights(msg.data);
      break;
    case 'route':
      // A remotely selected route is meant to be shown, not just drawn.
      if (typeof _routeFromRemote !== 'undefined') _routeFromRemote = true;
      applyRoute(msg.data);
      break;
    case 'coverage':
      if (Object.prototype.hasOwnProperty.call(msg, 'data')) {
        sessionCoverageState = Array.isArray(msg.data) ? msg.data : [];
        renderCombinedCoverage();
      } else {
        fetchAndApplyCoverage();
      }
      break;
    case 'occupancy':
      if (Object.prototype.hasOwnProperty.call(msg, 'data')) {
        sessionOccupancyState = msg.data || {};
        renderCombinedOccupancy();
      } else {
        fetchAndApplyOccupancy();
      }
      break;
    case 'equipment':
      if (msg.version > localEquipmentVersion) {
        scheduleEquipmentRefresh(msg.version);
      }
      break;
    case 'room_layers':
    case 'effects':
    case 'entities':
    case 'room_appearance':
    case 'alerts':
    case 'doors':
      scheduleVisualizationRefresh(msg.type);
      break;
  }
}

function applyViewport(vp) {
  if (!vp) return;

  // Find target room center if room name specified
  let targetX = null, targetY = null, targetLevel = null;
  if (vp.room) {
    const requestedLevel = vp.floor
      ? LEVELS.find(l => l === vp.floor || l.endsWith('/' + vp.floor))
      : null;
    const levels = requestedLevel ? [requestedLevel] : LEVELS;
    for (const l of levels) {
      const data = floorData[l];
      if (!data) continue;
      const room = data.rooms.find(r => r.name === vp.room);
      if (room) {
        const [wx, wy] = pdfToWorld(room.center[0], room.center[1], data.page.width, data.page.height);
        targetX = wx;
        targetY = wy;
        targetLevel = l;
        break;
      }
    }
    if (targetX === null) { console.log('Room not found:', vp.room); return; }
  }

  if (vp.mode === '3d') {
    if (!is3DView) switchTo3D();
    if (targetX !== null) {
      const levelIdx = LEVELS.indexOf(targetLevel);
      const tz = levelIdx * FLOOR_SPACING;
      const dist = 150 / (vp.zoom || 1);
      const targetPos = new THREE.Vector3(targetX + dist, targetY - dist, tz + dist);
      const targetLookAt = new THREE.Vector3(targetX, targetY, tz);
      animateCamera3D(targetPos, targetLookAt, 1500);
    }
  } else {
    const floor = vp.floor || (targetLevel ? targetLevel.split('/').pop() : 'level0');
    const level = currentBuilding + '/' + floor;
    if (is3DView || currentLevel !== level) switchToFloor(level);
    const page = floorData[level]?.page || floorData[LEVELS[0]]?.page || { width: 595, height: 842 };
    const pw = page.width;
    const ph = page.height;
    const targetZoom = (vp.zoom && vp.zoom > 0) ? Math.max(pw, ph) * 1.1 / vp.zoom : frustumSize;
    const targetPanX = targetX !== null ? targetX : panOffset.x;
    const targetPanY = targetY !== null ? targetY : panOffset.y;
    animateCamera2D(targetPanX, targetPanY, targetZoom, 1500);
  }
}

let _animId = 0;
function animateCamera3D(targetPos, targetLookAt, duration) {
  const id = ++_animId;
  const startPos = camera3D.position.clone();
  const startTarget = orbitControls.target.clone();
  const startTime = performance.now();

  function tick(now) {
    if (id !== _animId) return;
    const t = Math.max(0, Math.min((now - startTime) / duration, 1));
    const ease = t < 0.5 ? 4*t*t*t : 1 - Math.pow(-2*t+2, 3)/2; // easeInOutCubic

    camera3D.position.lerpVectors(startPos, targetPos, ease);
    orbitControls.target.lerpVectors(startTarget, targetLookAt, ease);
    orbitControls.update();
    render();

    if (t < 1) requestAnimationFrame(tick);
  }
  requestAnimationFrame(tick);
}

function animateCamera2D(targetPanX, targetPanY, targetFrustum, duration) {
  if (duration <= 0) {
    setCamera2DView(targetPanX, targetPanY, targetFrustum);
    return;
  }
  const id = ++_animId;
  const startPanX = panOffset.x, startPanY = panOffset.y;
  const startFrustum = frustumSize;
  const startTime = performance.now();

  function tick(now) {
    if (id !== _animId) return;
    const t = Math.max(0, Math.min((now - startTime) / duration, 1));
    const ease = t < 0.5 ? 4*t*t*t : 1 - Math.pow(-2*t+2, 3)/2;

    panOffset.x = startPanX + (targetPanX - startPanX) * ease;
    panOffset.y = startPanY + (targetPanY - startPanY) * ease;
    frustumSize = startFrustum + (targetFrustum - startFrustum) * ease;
    updateCamera2D();
    render();

    if (t < 1) requestAnimationFrame(tick);
  }
  requestAnimationFrame(tick);
}

function setCamera2DView(targetPanX, targetPanY, targetFrustum) {
  _animId++;
  panOffset.set(targetPanX, targetPanY);
  frustumSize = targetFrustum;
  updateCamera2D();
  render();
}

function applyHighlights(highlights) {
  if (!highlights) return;
  // Remove previous highlights
  LEVELS.forEach(l => {
    const fg = floorGroups[l];
    if (!fg) return;
    fg.meshes.forEach(m => {
      if (m.userData.highlighted) {
        m.material.color.setHex(m.userData.originalColor !== undefined
          ? m.userData.originalColor
          : (m.userData.type === 'corridor' ? CORRIDOR_COLOR : FLOOR_COLORS[LEVELS.indexOf(l)] || FLOOR_COLORS[0]));
        m.material.opacity = m.userData.originalOpacity !== undefined ? m.userData.originalOpacity : 0.4;
        m.userData.highlighted = false;
      }
    });
  });

  // Apply new highlights
  for (const h of highlights) {
    LEVELS.forEach(l => {
      const levelKey = l.split('/').pop();
      if (h.level && h.level !== l && h.level !== levelKey) return;
      const fg = floorGroups[l];
      if (!fg) return;
      const meshes = fg.meshes.filter(m => m.userData.id === h.room_id || m.userData.name === h.room);
      for (const mesh of meshes) {
        if (mesh.userData.originalColor === undefined) {
          mesh.userData.originalColor = mesh.material.color.getHex();
          mesh.userData.originalOpacity = mesh.material.opacity;
        }
        mesh.material.color.setStyle(h.color);
        mesh.material.opacity = h.opacity || 0.8;
        mesh.userData.highlighted = true;
      }
    });
  }

  // Highlights can span multiple floors, but the 2D viewer shows ONE floor at
  // a time — a highlight on a hidden floor is colored but invisible (the
  // "highlighted 6 rooms, only 3 show" bug: rooms split across level0/level1).
  // Surface cross-floor highlights two ways:
  //   1. badge each floor button with its highlight count, so the user knows
  //      which floors have highlighted rooms;
  //   2. if the CURRENT floor has NONE but another floor does, switch to the
  //      floor with the most so the user sees something rather than nothing.
  const perFloor = {};
  LEVELS.forEach(l => {
    const fg = floorGroups[l];
    perFloor[l] = fg ? fg.meshes.filter(m => m.userData.highlighted).length : 0;
  });
  updateFloorHighlightBadges(perFloor);
  // Only auto-switch floors in 2D — in 3D every floor is visible at once, so
  // switching is needless and switchToFloor() would drop us out of 3D,
  // clobbering a 3D viewport that was just applied (e.g. navigate to a room).
  if (!is3DView && (perFloor[currentLevel] || 0) === 0) {
    let best = null, bestN = 0;
    LEVELS.forEach(l => { if ((perFloor[l] || 0) > bestN) { best = l; bestN = perFloor[l]; } });
    if (best && bestN > 0 && best !== currentLevel) {
      switchToFloor(best);
    }
  }
  render();
}

// updateFloorHighlightBadges appends a "⬤N" count to each floor chip in the
// toolbar that has N highlighted rooms (and restores the bare label when N is
// 0). Chip index i corresponds to LEVELS[i] / BLDG.labels[i] (built in hud.js).
function updateFloorHighlightBadges(perFloor) {
  LEVELS.forEach((l, i) => {
    const chip = hudChipFloor[i];
    if (!chip) return;
    const base = (typeof BLDG !== 'undefined' && BLDG.labels && BLDG.labels[i] != null)
      ? BLDG.labels[i].replace('Floor ', 'F') : ('F' + i);
    const n = perFloor[l] || 0;
    chip.textContent = n > 0 ? `${base} ⬤${n}` : base;
  });
}

function applyRoute(route) {
  if (!route || !route.path || route.path.length < 2) { clearRoute(); render(); return; }

  // Use the same smooth route renderer as the interactive route finder
  clearRoute();

  // Determine which floor the route is on
  const levels = new Set(route.path.map(n => n.level).filter(Boolean));
  const isMultiFloor = levels.size > 1;

  // If no level info on nodes, try to find the floor from the first room name
  let levelKey = null;
  if (!isMultiFloor) {
    const firstName = route.path[0].name || '';
    for (const l of LEVELS) {
      const data = floorData[l];
      if (!data) continue;
      const lk = l.split('/').pop();
      if (data.rooms.find(r => r.name === firstName)) { levelKey = lk; break; }
    }
    if (!levelKey) levelKey = 'level1'; // fallback
  }

  // Cross-floor routes only make sense in 3D (the 2D view shows one floor).
  if (isMultiFloor && !is3DView) switchTo3D();

  renderSmoothRoute(route, isMultiFloor ? null : levelKey, true);
  render();
}

async function fetchAndApplyOccupancy() {
  try {
    const resp = await fetch('/api/occupancy');
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    globalOccupancyState = await resp.json();
    renderCombinedOccupancy();
  } catch(e) { console.log('Failed to fetch occupancy:', e); }
}

function renderCombinedOccupancy() {
  window._globalOccupancy = { ...(globalOccupancyState || {}), ...(sessionOccupancyState || {}) };
  buildAllEquipment();
  filterEquipment();
}

async function fetchAndApplyCoverage() {
  try {
    const resp = await fetch('/api/coverage');
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    globalCoverageState = await resp.json();
    renderCombinedCoverage();
  } catch(e) { console.log('Failed to fetch coverage:', e); }
}

function renderCombinedCoverage() {
  const byID = new Map();
  for (const zone of globalCoverageState || []) byID.set(zone.id, zone);
  for (const zone of sessionCoverageState || []) byID.set(zone.id, zone);
  applyCoverage([...byID.values()]);
}

function renderOccupancy(occupancy) {
  // Clear old occupancy sprites
  while (occupancyGroup.children.length > 0) {
    const c = occupancyGroup.children[0];
    occupancyGroup.remove(c);
    if (c.material && c.material.map) c.material.map.dispose();
    if (c.material) c.material.dispose();
  }

  if (!occupancy) { render(); return; }

  const iconPromises = [];

  for (const [roomKey, occ] of Object.entries(occupancy)) {
    const personCount = occ.persons ? occ.persons.length : 0;
    const alienCount = occ.aliens ? occ.aliens.length : 0;
    if (personCount === 0 && alienCount === 0) continue;

    // Canonical keys are "level/room-name". Legacy unique numeric IDs and
    // room names are still accepted by the server for compatibility.
    const slash = roomKey.indexOf('/');
    const keyLevel = slash >= 0 ? roomKey.slice(0, slash) : '';
    const roomToken = slash >= 0 ? roomKey.slice(slash + 1) : roomKey;
    const roomID = /^\d+$/.test(roomToken) ? Number(roomToken) : null;
    let roomCenter = null, roomPw = 0, roomPh = 0, roomLevel = null;
    for (const l of LEVELS) {
      if (keyLevel && l !== keyLevel && !l.endsWith('/' + keyLevel)) continue;
      const data = floorData[l];
      if (!data) continue;
      const room = data.rooms.find(r => r.name === roomToken || (roomID !== null && r.id === roomID));
      if (room) {
        roomPw = data.page.width;
        roomPh = data.page.height;
        roomCenter = room.center;
        roomLevel = l;
        break;
      }
    }
    if (!roomCenter) continue;

    const [wx, wy] = pdfToWorld(roomCenter[0], roomCenter[1], roomPw, roomPh);
    const levelIdx = LEVELS.indexOf(roomLevel);
    const zBase = is3DView ? levelIdx * FLOOR_SPACING + 3 : 12;
    const iconSize = 3;
    const spacing = iconSize * 1.3;
    const yOffset = is3DView ? 0 : iconSize * 5; // offset upward in 2D to clear equipment icons

    // Collect all entities to render
    const entities = [];
    if (occ.persons) {
      for (const person of occ.persons) {
        entities.push({ type: person.icon || 'man', name: person.name, id: person.id });
      }
    }
    if (occ.aliens) {
      for (const alien of occ.aliens) {
        entities.push({ type: 'alien', name: 'Alien', id: alien.id });
      }
    }

    // Spread entities horizontally, centered on room
    const totalWidth = (entities.length - 1) * spacing;
    const startX = wx - totalWidth / 2;

    for (let ei = 0; ei < entities.length; ei++) {
      const ent = entities[ei];
      const ex = startX + ei * spacing;

      const p = loadEquipmentIcon(ent.type, null, true).then(canvas => {
        const sprite = createEquipmentSprite(ex, wy + yOffset, zBase, iconSize, canvas);
        occupancyGroup.add(sprite);

        // Name label below icon
        const lc = document.createElement('canvas');
        lc.width = 128; lc.height = 32;
        const ctx = lc.getContext('2d');
        ctx.font = 'bold 14px monospace';
        ctx.fillStyle = ent.type === 'alien' ? '#ff2222' : '#00ccff';
        ctx.textAlign = 'center';
        ctx.fillText(ent.name || ent.type, 64, 18);
        const label = new THREE.Sprite(new THREE.SpriteMaterial({
          map: new THREE.CanvasTexture(lc), transparent: true, depthWrite: false }));
        label.position.set(ex, wy + yOffset - iconSize * 1.2, zBase);
        label.scale.set(6, 1.5, 1);
        occupancyGroup.add(label);
      });
      iconPromises.push(p);
    }
  }

  Promise.all(iconPromises).then(() => render());
}

function applyCoverage(zones) {
  // Clear old coverage
  while (coverageGroup.children.length > 0) {
    const c = coverageGroup.children[0];
    coverageGroup.remove(c);
    if (c.geometry) c.geometry.dispose();
    if (c.material) c.material.dispose();
  }

  if (!zones || zones.length === 0) { render(); return; }

  const defaultData = floorData[LEVELS[0]];
  if (!defaultData) return;
  const pw = defaultData.page.width, ph = defaultData.page.height;

  // Build clipping planes from building room bounds
  const allRooms = [];
  for (const l of LEVELS) {
    const fd = floorData[l];
    if (fd) allRooms.push(...fd.rooms);
  }
  const allPts = allRooms.flatMap(r => r.polygon);
  const minX = Math.min(...allPts.map(p => p[0]));
  const maxX = Math.max(...allPts.map(p => p[0]));
  const minY = Math.min(...allPts.map(p => p[1]));
  const maxY = Math.max(...allPts.map(p => p[1]));

  const [wMinX, wMaxY] = pdfToWorld(minX, minY, pw, ph);
  const [wMaxX, wMinY] = pdfToWorld(maxX, maxY, pw, ph);

  const maxFloorZ = (LEVELS.length - 1) * FLOOR_SPACING + 10;
  const clipPlanes = [
    new THREE.Plane(new THREE.Vector3(1, 0, 0), -wMinX),    // left
    new THREE.Plane(new THREE.Vector3(-1, 0, 0), wMaxX),    // right
    new THREE.Plane(new THREE.Vector3(0, 1, 0), -wMinY),    // front
    new THREE.Plane(new THREE.Vector3(0, -1, 0), wMaxY),    // back
    new THREE.Plane(new THREE.Vector3(0, 0, 1), 0),         // below ground
    new THREE.Plane(new THREE.Vector3(0, 0, -1), maxFloorZ), // above top floor
  ];
  renderer.localClippingEnabled = true;

  for (const zone of zones) {
    // Resolve room name to center if specified
    let cx = zone.center[0], cy = zone.center[1];
    let resolvedLevel = zone.level || '';
    let coordinatePage = defaultData.page;
    if (zone.room) {
      let found = false;
      for (const l of LEVELS) {
        const levelKey = l.split('/').pop();
        if (resolvedLevel && resolvedLevel !== l && resolvedLevel !== levelKey) continue;
        const fd = floorData[l];
        if (!fd) continue;
        const room = fd.rooms.find(r => r.name === zone.room);
        if (room) {
          cx = room.center[0]; cy = room.center[1];
          coordinatePage = fd.page;
          if (!resolvedLevel) resolvedLevel = levelKey;
          found = true;
          break;
        }
      }
      if (!found) continue;
    } else if (resolvedLevel) {
      const level = LEVELS.find(l => l === resolvedLevel || l.endsWith('/' + resolvedLevel));
      if (level && floorData[level]) coordinatePage = floorData[level].page;
    }
    const [wx, wy] = pdfToWorld(cx, cy, coordinatePage.width, coordinatePage.height);
    const radius = zone.radius || 30;
    const color = new THREE.Color(zone.color || '#00aaff');
    const opacity = zone.opacity || 0.15;
    const height = zone.height || 0;

    // Determine which floors to render on
    let floors = [];
    if (resolvedLevel) {
      const li = LEVELS.findIndex(l => l === resolvedLevel || l.endsWith('/' + resolvedLevel));
      if (li >= 0) floors.push(li);
    } else {
      // All floors
      for (let i = 0; i < LEVELS.length; i++) floors.push(i);
    }

    if (is3DView) {
      // 3D: render as translucent sphere, centered on the floor(s)
      const minFloorZ = Math.min(...floors) * FLOOR_SPACING;
      const maxFloorZ = Math.max(...floors) * FLOOR_SPACING;
      const centerZ = (minFloorZ + maxFloorZ) / 2 + 5;
      const scaleZ = height > 0 ? height / (radius * 2) : 0.5; // flatten vertically

      const geom = new THREE.SphereGeometry(radius, 32, 24);
      const mat = new THREE.MeshBasicMaterial({
        color, transparent: true, opacity,
        side: THREE.DoubleSide, depthWrite: false,
        clippingPlanes: clipPlanes
      });
      const mesh = new THREE.Mesh(geom, mat);
      mesh.position.set(wx, wy, centerZ);
      mesh.scale.set(1, 1, scaleZ);
      mesh.renderOrder = 500;
      coverageGroup.add(mesh);

      // Inner glow (smaller, brighter sphere)
      const innerGeom = new THREE.SphereGeometry(radius * 0.5, 24, 16);
      const innerMat = new THREE.MeshBasicMaterial({
        color, transparent: true, opacity: opacity * 1.5,
        side: THREE.DoubleSide, depthWrite: false,
        clippingPlanes: clipPlanes
      });
      const inner = new THREE.Mesh(innerGeom, innerMat);
      inner.position.set(wx, wy, centerZ);
      inner.scale.set(1, 1, scaleZ);
      inner.renderOrder = 501;
      coverageGroup.add(inner);
    } else {
      // 2D: flat disc per floor
      for (const fi of floors) {
        const z = 2;
        const geom = new THREE.CircleGeometry(radius, 48);
        const mat = new THREE.MeshBasicMaterial({
          color, transparent: true, opacity,
          side: THREE.DoubleSide, depthWrite: false,
          clippingPlanes: clipPlanes
        });
        const mesh = new THREE.Mesh(geom, mat);
        mesh.position.set(wx, wy, z);
        mesh.renderOrder = 500;
        coverageGroup.add(mesh);

        // Soft edge ring
        const ringGeom = new THREE.RingGeometry(radius * 0.85, radius, 48);
        const ringMat = new THREE.MeshBasicMaterial({
          color, transparent: true, opacity: opacity * 0.4,
          side: THREE.DoubleSide, depthWrite: false,
          clippingPlanes: clipPlanes
        });
        const ring = new THREE.Mesh(ringGeom, ringMat);
        ring.position.set(wx, wy, z + 0.1);
        ring.renderOrder = 499;
        coverageGroup.add(ring);
      }
    }
  }

  render();
}

function recordInitialEquipmentVersion() {
  const version = equipment.reduce((highest, item) => Math.max(highest, Number(item.version) || 0), 0);
  localEquipmentVersion = Math.max(localEquipmentVersion, version);
  recordEquipmentSnapshot(localEquipmentVersion);
}

function recordEquipmentSnapshot(version) {
  const diagnostics = window._buildsimDiagnostics;
  diagnostics.lastAppliedEquipmentVersion = Math.max(
    diagnostics.lastAppliedEquipmentVersion,
    version,
    ...equipment.map(item => Number(item.version) || 0),
  );
  diagnostics.equipmentCount = equipment.length;
  diagnostics.sensorValues = {};
  for (const item of equipment) {
    for (const sensor of item.sensors || []) diagnostics.sensorValues[sensor.id] = sensor.value;
  }
}

// A sensor storm may create hundreds of WebSocket notifications in a second.
// Coalesce them into bounded snapshots: render at most once per interval, then
// perform one trailing refresh if a newer version arrived during the request.
function scheduleEquipmentRefresh(version) {
  const numericVersion = Number(version) || 0;
  localEquipmentVersion = Math.max(localEquipmentVersion, numericVersion);
  equipmentRefreshPending = true;
  window._buildsimDiagnostics.equipmentNotifications++;
  window._buildsimDiagnostics.lastNotifiedEquipmentVersion = localEquipmentVersion;
  if (equipmentRefreshTimer === null && !equipmentRefreshInFlight) {
    equipmentRefreshTimer = setTimeout(flushEquipmentRefresh, EQUIPMENT_REFRESH_INTERVAL_MS);
  }
}

async function flushEquipmentRefresh() {
  equipmentRefreshTimer = null;
  if (equipmentRefreshInFlight || !equipmentRefreshPending) return;
  equipmentRefreshPending = false;
  equipmentRefreshInFlight = true;
  const targetVersion = localEquipmentVersion;
  try {
    await fetchAndApplyEquipment(targetVersion);
  } finally {
    equipmentRefreshInFlight = false;
    if (equipmentRefreshPending && equipmentRefreshTimer === null) {
      equipmentRefreshTimer = setTimeout(flushEquipmentRefresh, EQUIPMENT_REFRESH_INTERVAL_MS);
    }
  }
}

async function fetchAndApplyEquipment(targetVersion = localEquipmentVersion) {
  window._buildsimDiagnostics.equipmentRefreshes++;
  try {
    const resp = await fetch(`/api/equipment?t=${Date.now()}`);
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    equipment = await resp.json();
    buildAllEquipment();
    render();
    recordEquipmentSnapshot(targetVersion);
  } catch(e) { console.log('Failed to fetch equipment:', e); }
}

// Clean up session on page unload
window.addEventListener('beforeunload', () => {
  sessionClosing = true;
  window._buildsimDiagnostics.websocketState = 'closing';
  if (sessionWs) sessionWs.close();
  if (sessionId) {
    fetch(`/api/sessions/${sessionId}`, { method: 'DELETE', keepalive: true });
  }
});
