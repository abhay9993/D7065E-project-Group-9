// visualization.js — renderer-only state for physical effects, room lighting,
// moving people/robots, decision alerts, and semantic doors/entrances.

const effectGroups = {};
const entityGroups = {};
const appearanceGroups = {};
const semanticDoorGroups = {};
const entityObjects = new Map();
let visualEffects = [];
let mobileEntities = [];
let roomAppearance = [];
let decisionAlerts = [];
let semanticDoors = [];

function fullVisualLevel(level) {
  return LEVELS.find(candidate => candidate === level || candidate.endsWith('/' + level)) || null;
}

function ensureVisualizationGroups() {
  for (const level of LEVELS) {
    const floor = floorGroups[level];
    if (!floor) continue;
    const ensure = (collection, name) => {
      if (!collection[level]) {
        collection[level] = new THREE.Group();
        collection[level].name = name;
        floor.container.add(collection[level]);
      }
    };
    ensure(effectGroups, 'simulation-effects');
    ensure(entityGroups, 'mobile-entities');
    ensure(appearanceGroups, 'room-appearance');
    ensure(semanticDoorGroups, 'semantic-doors');
  }
}

function disposeVisualObject(object) {
  object.traverse(child => {
    if (child.geometry) child.geometry.dispose();
    if (child.material) {
      const materials = Array.isArray(child.material) ? child.material : [child.material];
      for (const material of materials) {
        if (material.map) material.map.dispose();
        material.dispose();
      }
    }
  });
}

function emptyVisualGroup(group) {
  if (!group) return;
  while (group.children.length) {
    const child = group.children[0];
    group.remove(child);
    disposeVisualObject(child);
  }
}

function visualLocation(item) {
  const level = fullVisualLevel(item.level);
  const floor = level ? floorData[level] : null;
  if (!floor) return null;
  let position = Array.isArray(item.position) ? item.position : null;
  let room = null;
  if (item.room) room = floor.rooms.find(candidate => candidate.name === item.room);
  if (!position && room) position = room.center;
  if (!position) return null;
  const [x, y] = pdfToWorld(position[0], position[1], floor.page.width, floor.page.height);
  return { level, floor, room, x, y };
}

function labelSprite(text, color, width = 256) {
  const canvas = document.createElement('canvas');
  canvas.width = width; canvas.height = 38;
  const context = canvas.getContext('2d');
  context.font = '600 15px system-ui, sans-serif';
  context.textAlign = 'center';
  context.fillStyle = color;
  context.shadowColor = '#07101c';
  context.shadowBlur = 4;
  context.fillText(text, width / 2, 22);
  const sprite = new THREE.Sprite(new THREE.SpriteMaterial({
    map: new THREE.CanvasTexture(canvas), transparent: true, depthWrite: false,
  }));
  sprite.scale.set(14, 2.1, 1);
  sprite.renderOrder = 740;
  return sprite;
}

function translucent(color, opacity) {
  return noTone(new THREE.MeshBasicMaterial({
    color, transparent: true, opacity, depthWrite: false, side: THREE.DoubleSide,
  }));
}

function createFireEffect(effect, radius, height) {
  const group = new THREE.Group();
  const outer = new THREE.Mesh(new THREE.ConeGeometry(radius * 0.62, height, 16), translucent(effect.color || '#ff5a1f', 0.76));
  outer.rotation.x = Math.PI / 2;
  outer.position.z = height / 2;
  outer.userData.flame = true;
  outer.userData.phase = 0;
  const inner = new THREE.Mesh(new THREE.ConeGeometry(radius * 0.34, height * 0.72, 14), translucent('#ffd54a', 0.9));
  inner.rotation.x = Math.PI / 2;
  inner.position.z = height * 0.36;
  inner.userData.flame = true;
  inner.userData.phase = 1.7;
  const glow = new THREE.Mesh(new THREE.CircleGeometry(radius, 32), translucent('#ff3d00', 0.25));
  glow.position.z = 0.15;
  group.add(glow, outer, inner);
  return group;
}

function createSmokeEffect(effect, radius, height) {
  const group = new THREE.Group();
  const color = effect.color || '#687386';
  for (let i = 0; i < 7; i++) {
    const size = radius * (0.35 + (i % 3) * 0.12);
    const puff = new THREE.Mesh(new THREE.SphereGeometry(size, 12, 8), translucent(color, 0.18 + effect.intensity * 0.12));
    const angle = i * 2.4;
    puff.position.set(Math.cos(angle) * radius * 0.35, Math.sin(angle) * radius * 0.35, 2 + (i / 6) * height);
    puff.userData.smoke = true;
    puff.userData.baseX = puff.position.x;
    puff.userData.baseY = puff.position.y;
    puff.userData.baseZ = puff.position.z;
    puff.userData.phase = i * 0.83;
    group.add(puff);
  }
  return group;
}

function createVolumeEffect(effect, radius, height) {
  const color = effect.color || (effect.type === 'water' ? '#27a7ff' : '#6ee7a2');
  const geometry = height > 0
    ? new THREE.CylinderGeometry(radius, radius, height, 36, 1, true)
    : new THREE.CircleGeometry(radius, 36);
  const mesh = new THREE.Mesh(geometry, translucent(color, 0.16 + effect.intensity * 0.18));
  if (height > 0) {
    mesh.rotation.x = Math.PI / 2;
    mesh.position.z = height / 2;
  } else {
    mesh.position.z = 0.3;
  }
  const ring = new THREE.Mesh(new THREE.RingGeometry(radius * 0.88, radius, 40), translucent(color, 0.8));
  ring.position.z = 0.5;
  ring.userData.warningPulse = true;
  mesh.add(ring);
  return mesh;
}

function createSprinklerEffect(effect, radius, height) {
  const group = new THREE.Group();
  const cone = new THREE.Mesh(
    new THREE.ConeGeometry(radius, height, 24, 1, true),
    new THREE.MeshBasicMaterial({ color: effect.color || '#55c7ff', wireframe: true, transparent: true, opacity: 0.38 }),
  );
  cone.rotation.x = Math.PI / 2;
  cone.position.z = height / 2;
  const head = new THREE.Mesh(new THREE.SphereGeometry(1.2, 12, 8), translucent('#c7e9ff', 0.95));
  head.position.z = height;
  group.add(cone, head);
  return group;
}

function createWarningEffect(effect, radius) {
  const group = new THREE.Group();
  for (let i = 0; i < 3; i++) {
    const ring = new THREE.Mesh(new THREE.RingGeometry(radius * (0.25 + i * 0.23), radius * (0.31 + i * 0.23), 36), translucent(effect.color || '#ffca28', 0.85 - i * 0.18));
    ring.position.z = 0.4 + i * 0.08;
    ring.userData.warningPulse = true;
    ring.userData.phase = i * 0.8;
    group.add(ring);
  }
  return group;
}

function applyEffects(effects) {
  visualEffects = Array.isArray(effects) ? effects : [];
  ensureVisualizationGroups();
  for (const level of LEVELS) emptyVisualGroup(effectGroups[level]);
  for (const effect of visualEffects) {
    const location = visualLocation(effect);
    if (!location) continue;
    const radius = Number(effect.radius) || 8;
    const height = Number(effect.height) || radius * 1.5;
    let object;
    if (effect.type === 'fire') object = createFireEffect(effect, radius, height);
    else if (effect.type === 'smoke') object = createSmokeEffect(effect, radius, height);
    else if (effect.type === 'sprinkler') object = createSprinklerEffect(effect, radius, height);
    else if (effect.type === 'warning') object = createWarningEffect(effect, radius);
    else object = createVolumeEffect(effect, radius, effect.height || 0);
    object.position.set(location.x, location.y, 2);
    object.userData.visualEffect = effect;
    if (effect.label) {
      const label = labelSprite(effect.label, '#ffe4ca');
      label.position.set(0, -radius - 2, Math.max(5, height * 0.65));
      object.add(label);
    }
    effectGroups[location.level].add(object);
  }
  render();
}

function roomShape(room, floor) {
  if (!room || room.polygon.length < 3) return null;
  const shape = new THREE.Shape();
  room.polygon.forEach((point, index) => {
    const [x, y] = pdfToWorld(point[0], point[1], floor.page.width, floor.page.height);
    if (index === 0) shape.moveTo(x, y); else shape.lineTo(x, y);
  });
  shape.closePath();
  return shape;
}

function applyRoomAppearance(items) {
  roomAppearance = Array.isArray(items) ? items : [];
  ensureVisualizationGroups();
  for (const level of LEVELS) emptyVisualGroup(appearanceGroups[level]);
  let lights = 0;
  for (const item of roomAppearance) {
    const location = visualLocation(item);
    if (!location || !location.room) continue;
    const shape = roomShape(location.room, location.floor);
    if (!shape) continue;
    const brightness = Number(item.brightness) || 0;
    const overlay = new THREE.Mesh(new THREE.ShapeGeometry(shape), translucent(item.color || '#ffd36a', 0.05 + brightness * 0.48));
    overlay.position.z = 1.7;
    overlay.renderOrder = 420;
    appearanceGroups[location.level].add(overlay);
    if (brightness > 0.05 && lights++ < 32) {
      const light = new THREE.PointLight(item.color || '#ffd36a', brightness * 0.8, Math.max(20, Math.sqrt(location.room.area || 100) * 5), 2);
      light.position.set(location.x, location.y, 9);
      appearanceGroups[location.level].add(light);
    }
  }
  render();
}

function entityIconType(type) {
  if (type === 'cleaning_robot') return 'cleaning_robot';
  return ['man', 'woman', 'group', 'robot'].includes(type) ? type : 'generic';
}

function createEntityObject(entity) {
  const group = new THREE.Group();
  group.userData.entityType = entity.type;
  const markerColor = entity.type.includes('robot') ? 0x7dd3fc : 0x38bdf8;
  const marker = new THREE.Mesh(new THREE.CylinderGeometry(1.7, 1.7, 0.7, 18), translucent(markerColor, 0.65));
  marker.rotation.x = Math.PI / 2;
  marker.position.z = 0.5;
  group.add(marker);
  const label = labelSprite(entity.name || entity.id, '#bfe9ff');
  label.position.set(0, -3.7, 5.8);
  group.add(label);
  loadEquipmentIcon(entityIconType(entity.type), null, true).then(canvas => {
    if (!entityObjects.has(entity.id)) return;
    const sprite = createEquipmentSprite(0, 0, 5, 5, canvas);
    sprite.userData.mobileEntity = entity.id;
    group.add(sprite);
    render();
  });
  return group;
}

function applyEntities(entities) {
  mobileEntities = Array.isArray(entities) ? entities : [];
  ensureVisualizationGroups();
  const wanted = new Set(mobileEntities.map(entity => entity.id));
  for (const [id, record] of entityObjects) {
    if (wanted.has(id)) continue;
    if (record.group.parent) record.group.parent.remove(record.group);
    disposeVisualObject(record.group);
    entityObjects.delete(id);
  }
  const now = performance.now();
  for (const entity of mobileEntities) {
    const location = visualLocation(entity);
    if (!location) continue;
    const target = new THREE.Vector3(location.x, location.y, 2);
    let record = entityObjects.get(entity.id);
    if (!record || record.group.userData.entityType !== entity.type) {
      if (record) {
        if (record.group.parent) record.group.parent.remove(record.group);
        disposeVisualObject(record.group);
      }
      const group = createEntityObject(entity);
      group.position.copy(target);
      entityGroups[location.level].add(group);
      record = { group, level: location.level, from: target.clone(), to: target.clone(), start: now, duration: 0 };
      entityObjects.set(entity.id, record);
    } else {
      if (record.level !== location.level) {
        if (record.group.parent) record.group.parent.remove(record.group);
        entityGroups[location.level].add(record.group);
        record.level = location.level;
      }
      record.from = record.group.position.clone();
      record.to = target;
      record.start = now;
      record.duration = Math.max(0, Number(entity.transition_ms) || 0);
    }
    record.group.rotation.z = -(Number(entity.heading) || 0) * Math.PI / 180;
    record.group.userData.mobileEntity = entity;
  }
  render();
}

function doorColor(door) {
  if (door.lock_state === 'jammed') return '#ff8a35';
  if (door.blocked || door.lock_state === 'locked') return '#ff405d';
  if (door.state === 'open') return '#55e6a5';
  return door.kind === 'entrance' || door.kind === 'fire_exit' ? '#4dd9ff' : '#75a7ff';
}

function createDoorObject(door) {
  const color = doorColor(door);
  const group = new THREE.Group();
  const frame = new THREE.Mesh(new THREE.BoxGeometry(5.2, 1.1, 7), translucent(color, 0.88));
  frame.position.z = 3.5;
  group.add(frame);
  if (door.kind === 'entrance' || door.kind === 'fire_exit') {
    const cap = new THREE.Mesh(new THREE.BoxGeometry(7.4, 1.3, 0.8), translucent(color, 0.95));
    cap.position.z = 7.2;
    group.add(cap);
  }
  if (door.lock_state !== 'unlocked') {
    const body = new THREE.Mesh(new THREE.BoxGeometry(2.5, 0.9, 2.1), translucent('#f8fafc', 0.95));
    body.position.set(0, -0.8, 5.2);
    const shackle = new THREE.Mesh(new THREE.TorusGeometry(1.0, 0.28, 8, 18, Math.PI), translucent('#f8fafc', 0.95));
    shackle.rotation.x = Math.PI / 2;
    shackle.position.set(0, -0.8, 6.2);
    group.add(body, shackle);
  }
  if (door.blocked) {
    const ring = new THREE.Mesh(new THREE.RingGeometry(4.2, 4.8, 32), translucent(color, 0.72));
    ring.position.z = 0.5;
    ring.userData.warningPulse = true;
    group.add(ring);
  }
  const label = labelSprite(`${door.kind === 'door' ? '' : door.kind.replace('_', ' ') + ' · '}${door.name}`, color);
  label.position.set(0, -5.2, 7.8);
  group.add(label);
  group.userData.semanticDoor = door;
  return group;
}

function applyDoors(doors) {
  semanticDoors = Array.isArray(doors) ? doors : [];
  ensureVisualizationGroups();
  for (const level of LEVELS) emptyVisualGroup(semanticDoorGroups[level]);
  for (const door of semanticDoors) {
    const location = visualLocation(door);
    if (!location) continue;
    const object = createDoorObject(door);
    object.position.set(location.x, location.y, 2);
    semanticDoorGroups[location.level].add(object);
  }
  const checkbox = document.getElementById('showDoors');
  if (checkbox) for (const level of LEVELS) semanticDoorGroups[level].visible = checkbox.checked;
  render();
}

function applyAlerts(alerts) {
  decisionAlerts = Array.isArray(alerts) ? alerts : [];
  const stack = document.getElementById('alert-stack');
  if (!stack) return;
  stack.replaceChildren();
  for (const alert of decisionAlerts) {
    const card = document.createElement('button');
    card.type = 'button';
    card.className = `decision-alert ${alert.severity}`;
    const eyebrow = document.createElement('span');
    eyebrow.className = 'decision-alert-kind';
    eyebrow.textContent = `${alert.severity}${alert.room ? ' · ' + alert.room : ''}`;
    const title = document.createElement('strong');
    title.textContent = alert.title;
    const message = document.createElement('span');
    message.textContent = alert.message || '';
    card.append(eyebrow, title, message);
    if (alert.room && alert.level) {
      card.title = `Show ${alert.room}`;
      card.addEventListener('click', () => {
        const level = fullVisualLevel(alert.level);
        if (!level) return;
        _lastSearchedRoom = { name: alert.room, level };
        switchToFloor(level);
        syncHud();
      });
    }
    stack.appendChild(card);
  }
}

function updateVisualizationAnimations(now) {
  for (const record of entityObjects.values()) {
    if (record.duration <= 0) continue;
    const t = Math.min(1, (now - record.start) / record.duration);
    const smooth = t * t * (3 - 2 * t);
    record.group.position.lerpVectors(record.from, record.to, smooth);
    if (t >= 1) record.duration = 0;
  }
  for (const level of LEVELS) {
    const group = effectGroups[level];
    if (group) group.traverse(object => {
      if (object.userData.flame) {
        const pulse = 1 + 0.12 * Math.sin(now * 0.008 + object.userData.phase);
        object.scale.set(pulse, pulse, 0.9 + 0.16 * Math.sin(now * 0.011 + object.userData.phase));
      }
      if (object.userData.smoke) {
        const phase = object.userData.phase;
        object.position.x = object.userData.baseX + Math.sin(now * 0.0008 + phase) * 1.4;
        object.position.y = object.userData.baseY + Math.cos(now * 0.00065 + phase) * 1.0;
        object.position.z = object.userData.baseZ + Math.sin(now * 0.0012 + phase) * 1.2;
      }
      if (object.userData.warningPulse) {
        const pulse = 1 + 0.12 * Math.sin(now * 0.006 + (object.userData.phase || 0));
        object.scale.set(pulse, pulse, 1);
        if (object.material) object.material.opacity = 0.35 + 0.35 * Math.abs(Math.sin(now * 0.005));
      }
    });
    const doors = semanticDoorGroups[level];
    if (doors) doors.traverse(object => {
      if (object.userData.warningPulse) {
        const pulse = 1 + 0.1 * Math.sin(now * 0.006);
        object.scale.set(pulse, pulse, 1);
      }
    });
  }
}

async function fetchVisualCollection(endpoint, apply) {
  try {
    const response = await fetch(endpoint);
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    apply(await response.json());
  } catch (error) {
    console.warn(`Failed to fetch ${endpoint}:`, error);
  }
}

const visualRefresh = {
  room_layers: () => fetchAndApplyRoomLayers(),
  effects: () => fetchVisualCollection('/api/effects', applyEffects),
  entities: () => fetchVisualCollection('/api/entities', applyEntities),
  room_appearance: () => fetchVisualCollection('/api/room-appearance', applyRoomAppearance),
  alerts: () => fetchVisualCollection('/api/alerts', applyAlerts),
  doors: () => fetchVisualCollection('/api/doors', applyDoors),
};
const visualRefreshState = {};

function scheduleVisualizationRefresh(kind) {
  if (!visualRefresh[kind]) return;
  window._buildsimDiagnostics.visualNotifications = (window._buildsimDiagnostics.visualNotifications || 0) + 1;
  const state = visualRefreshState[kind] || (visualRefreshState[kind] = { timer: null, active: false, pending: false });
  state.pending = true;
  if (state.timer || state.active) return;
  state.timer = setTimeout(async () => {
    state.timer = null;
    if (state.active || !state.pending) return;
    state.pending = false;
    state.active = true;
    window._buildsimDiagnostics.visualRefreshes = (window._buildsimDiagnostics.visualRefreshes || 0) + 1;
    await visualRefresh[kind]();
    state.active = false;
    if (state.pending) scheduleVisualizationRefresh(kind);
  }, 50);
}

async function fetchAllVisualizations() {
  ensureVisualizationGroups();
  await Promise.all(Object.values(visualRefresh).map(fetcher => fetcher()));
}

window.buildsim = Object.assign(window.buildsim || {}, {
  effects: () => [...visualEffects],
  entities: () => [...mobileEntities],
  doors: () => [...semanticDoors],
  alerts: () => [...decisionAlerts],
});
