// live.js — scalar room layers published by simulators and analytics services.
//
// A room layer is state or an estimate (temperature, smoke risk, infection
// risk, energy use, ...). Equipment sensors remain observations. If no layer
// has been published, the two legacy sensor-derived temperature/CO2 views are
// still available for backwards compatibility.

let heatOn = false;
let heatMetric = 'temperature';
let roomLayers = [];
const heatMeshes = {};
let heatBuilt = false;

const LEGACY_LAYERS = {
  temperature: {
    id: 'temperature', label: 'Temperature observations', unit: '°C',
    source: 'sensor', minimum: 18, maximum: 27, opacity: 0.72,
    palette: ['#3894f2', '#2ec76b', '#f5c733', '#e63a29'],
  },
  co2: {
    id: 'co2', label: 'CO₂ observations', unit: 'ppm',
    source: 'sensor', minimum: 420, maximum: 1400, opacity: 0.72,
    palette: ['#3894f2', '#2ec76b', '#f5c733', '#e63a29'],
  },
};

function activeRoomLayers() {
  const result = [...roomLayers];
  for (const legacy of Object.values(LEGACY_LAYERS)) {
    if (!result.some(layer => layer.id === legacy.id)) result.push(legacy);
  }
  return result;
}

function selectedRoomLayer() {
  const layers = activeRoomLayers();
  return layers.find(layer => layer.id === heatMetric) || layers[0] || LEGACY_LAYERS.temperature;
}

function colorForLayer(layer, value, out) {
  const palette = Array.isArray(layer.palette) && layer.palette.length >= 2
    ? layer.palette : LEGACY_LAYERS.temperature.palette;
  const span = Math.max(1e-9, Number(layer.maximum) - Number(layer.minimum));
  const scaled = Math.max(0, Math.min(1, (value - Number(layer.minimum)) / span));
  const p = scaled * (palette.length - 1);
  const i = Math.min(palette.length - 2, Math.floor(p));
  const f = p - i;
  const a = new THREE.Color(palette[i]);
  const b = new THREE.Color(palette[i + 1]);
  out[0] = a.r + (b.r - a.r) * f;
  out[1] = a.g + (b.g - a.g) * f;
  out[2] = a.b + (b.b - a.b) * f;
  return out;
}

function sensorRoomMetric(level, room, metric) {
  const levelKey = level.split('/').pop();
  for (const eq of equipment) {
    if (eq.level !== levelKey || eq.room !== room.name) continue;
    for (const sensor of Array.isArray(eq.sensors) ? eq.sensors : []) {
      const kind = String(sensor.type || sensor.name || sensor.id || '').toLowerCase();
      const wanted = metric === 'co2' ? ['co2', 'carbon'] : ['temp'];
      if (!wanted.some(token => kind.includes(token))) continue;
      const value = typeof sensor.value === 'number' ? sensor.value : parseFloat(sensor.value);
      if (Number.isFinite(value)) return value;
    }
  }
  return null;
}

function roomLayerValue(level, room, layer) {
  if (layer.values) {
    const key = `${level.split('/').pop()}/${room.name}`;
    const value = layer.values[key];
    return Number.isFinite(value) ? value : null;
  }
  return sensorRoomMetric(level, room, layer.id);
}

function buildHeatLayers() {
  if (heatBuilt) return;
  heatBuilt = true;
  for (const level of LEVELS) {
    const fg = floorGroups[level];
    if (!fg || !fg.meshes.length) continue;
    const parts = [];
    const ranges = [];
    let total = 0;
    for (const roomMesh of fg.meshes) {
      const geometry = roomMesh.geometry.index ? roomMesh.geometry.toNonIndexed() : roomMesh.geometry.clone();
      const positions = geometry.attributes.position;
      ranges.push({ start: total, count: positions.count, room: roomMesh.userData });
      total += positions.count;
      parts.push(positions.array);
      if (geometry !== roomMesh.geometry) geometry.dispose();
    }
    const positions = new Float32Array(total * 3);
    let offset = 0;
    for (const part of parts) { positions.set(part, offset); offset += part.length; }
    const colors = new Float32Array(total * 3);
    const geometry = new THREE.BufferGeometry();
    geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3));
    geometry.computeBoundingSphere();
    const material = noTone(new THREE.MeshBasicMaterial({
      vertexColors: true, transparent: true, opacity: 0.72,
      side: THREE.DoubleSide, depthWrite: false,
    }));
    const mesh = new THREE.Mesh(geometry, material);
    mesh.position.z = 1.45;
    mesh.renderOrder = 400;
    mesh.visible = false;
    fg.container.add(mesh);
    heatMeshes[level] = { mesh, ranges, colors: geometry.attributes.color };
  }
}

const _layerColor = [0, 0, 0];
function updateHeatmap() {
  if (!heatOn) return;
  const layer = selectedRoomLayer();
  for (const level of LEVELS) {
    const heat = heatMeshes[level];
    if (!heat || !heat.mesh.visible) continue;
    heat.mesh.material.opacity = Number.isFinite(layer.opacity) ? layer.opacity : 0.72;
    const colors = heat.colors.array;
    for (const range of heat.ranges) {
      const value = roomLayerValue(level, range.room, layer);
      if (value === null) {
        _layerColor[0] = 0.17; _layerColor[1] = 0.19; _layerColor[2] = 0.23;
      } else {
        colorForLayer(layer, value, _layerColor);
      }
      for (let i = 0; i < range.count; i++) {
        const offset = (range.start + i) * 3;
        colors[offset] = _layerColor[0];
        colors[offset + 1] = _layerColor[1];
        colors[offset + 2] = _layerColor[2];
      }
    }
    heat.colors.needsUpdate = true;
  }
}

function setHeat(on, metric) {
  buildHeatLayers();
  if (metric && activeRoomLayers().some(layer => layer.id === metric)) heatMetric = metric;
  heatOn = !!on;
  for (const level of LEVELS) {
    if (heatMeshes[level]) heatMeshes[level].mesh.visible = heatOn;
  }
  if (heatOn) {
    if (is3DView) openBuilding({ frame: peelT < 0.5 });
    updateHeatmap();
  }
  renderRoomLayerButtons();
  updateHeatLegend();
  render();
}

function renderRoomLayerButtons() {
  const container = document.getElementById('roomLayerButtons');
  if (!container) return;
  container.replaceChildren();

  const addButton = (label, layerID, title) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'room-layer-btn';
    button.textContent = label;
    button.title = title;
    button.dataset.layerId = layerID;
    const selected = layerID ? heatOn && heatMetric === layerID : !heatOn;
    button.classList.toggle('on', selected);
    button.setAttribute('aria-pressed', String(selected));
    button.addEventListener('click', () => {
      setHeat(Boolean(layerID), layerID || undefined);
      if (typeof syncHud === 'function') syncHud();
    });
    container.appendChild(button);
  };

  addButton('Off', '', 'Hide room heatmaps');
  for (const layer of activeRoomLayers()) {
    const label = layer.label || layer.id;
    const source = layer.source ? ` — ${layer.source}` : '';
    addButton(label, layer.id, `${label}${source}`);
  }
}

function updateHeatLegend() {
  const element = document.getElementById('heatLegend');
  if (!element) return;
  element.style.display = heatOn ? 'block' : 'none';
  if (!heatOn) return;
  const layer = selectedRoomLayer();
  const palette = Array.isArray(layer.palette) ? layer.palette : LEGACY_LAYERS.temperature.palette;
  element.replaceChildren();
  const title = document.createElement('div');
  title.className = 'hud-legend-title';
  title.textContent = layer.label || layer.id;
  const source = document.createElement('span');
  source.className = 'hud-legend-source';
  source.textContent = layer.source || 'published state';
  const ramp = document.createElement('div');
  ramp.className = 'hud-ramp';
  ramp.style.background = `linear-gradient(to right, ${palette.join(', ')})`;
  const scale = document.createElement('div');
  scale.className = 'hud-legend-scale';
  const low = document.createElement('span');
  low.textContent = `${layer.minimum}${layer.unit || ''}`;
  const high = document.createElement('span');
  high.textContent = `${layer.maximum}${layer.unit || ''}`;
  scale.append(low, high);
  element.append(title, source, ramp, scale);
}

async function fetchAndApplyRoomLayers() {
  try {
    const response = await fetch('/api/room-layers');
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    roomLayers = await response.json();
    if (!activeRoomLayers().some(layer => layer.id === heatMetric)) {
      heatMetric = activeRoomLayers()[0]?.id || 'temperature';
    }
    if (heatOn) updateHeatmap();
    renderRoomLayerButtons();
    if (typeof syncHud === 'function') syncHud();
    render();
  } catch (error) {
    console.warn('Failed to fetch room layers:', error);
  }
}

function hideLiveChip() {
  const element = document.getElementById('liveChip');
  if (element) element.style.display = 'none';
}

function startLiveLayers() {
  buildHeatLayers();
  hideLiveChip();
  const requestedLayer = urlParams.get('layer');
  const requestedFloor = urlParams.get('floor');
  const requestedRoom = urlParams.get('room');
  window._buildsimDiagnostics.deepLink = {
    layer: requestedLayer || '', floor: requestedFloor || '', room: requestedRoom || '', fired: false,
  };
  // The shell boot deliberately establishes its plaza view after this hook.
  // Apply deep-link state on the next task so it is the final requested view.
  if (requestedLayer || requestedFloor || requestedRoom) {
    setTimeout(() => {
      window._buildsimDiagnostics.deepLink.fired = true;
      window._buildsimDiagnostics.deepLink.before3D = is3DView;
      if (requestedFloor || requestedRoom) {
        let level = requestedFloor ? fullVisualLevel(requestedFloor) : null;
        if (!level && requestedRoom) {
          level = LEVELS.find(candidate => floorData[candidate]?.rooms.some(room => room.name === requestedRoom));
        }
        if (level) {
          window._buildsimDiagnostics.deepLink.resolvedLevel = level;
          if (requestedRoom) _lastSearchedRoom = { name: requestedRoom, level };
          switchToFloor(level);
          if (requestedRoom) {
            const data = floorData[level];
            const room = data?.rooms.find(candidate => candidate.name === requestedRoom);
            if (room) {
              const [x, y] = pdfToWorld(room.center[0], room.center[1], data.page.width, data.page.height);
              // A URL is a reproducible view, not a gesture. Land directly on
              // its final frame even while the initial WebGL scene is busy.
              setCamera2DView(x, y, 100);
            }
          }
          window._buildsimDiagnostics.deepLink.afterFloor3D = is3DView;
          if (typeof syncHud === 'function') syncHud();
        }
      }
      if (requestedLayer) setHeat(true, requestedLayer);
      window._buildsimDiagnostics.deepLink.afterHeat3D = is3DView;
    }, 500);
  }
  setInterval(() => { if (heatOn) { updateHeatmap(); render(); } }, 2000);
}
