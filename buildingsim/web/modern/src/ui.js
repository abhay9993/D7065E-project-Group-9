// === Equipment search ===
document.getElementById('searchEquipment').addEventListener('input', (e) => {
  const q = e.target.value.trim().toLowerCase();
  const results = document.getElementById('eqSearchResults');
  if (q.length < 1) { results.innerHTML = ''; return; }

  let matches = equipment.filter(eq =>
    (eq.name && eq.name.toLowerCase().includes(q)) ||
    (eq.type && eq.type.toLowerCase().includes(q)) ||
    (eq.room && eq.room.toLowerCase().includes(q)) ||
    (eq.status && eq.status.toLowerCase().includes(q)) ||
    (eq.id && eq.id.toLowerCase().includes(q))
  ).slice(0, 10);

  const items = matches.map(eq => {
    const sc = eq.status === 'running' ? '#00e676' : eq.status === 'warning' ? '#ffab00' : '#ff1744';
    const levelKey = eq.level;
    const fullLevel = LEVELS.find(l => l.endsWith(levelKey));
    const levelIdx = fullLevel ? LEVELS.indexOf(fullLevel) : 0;
    const item = document.createElement('div');
    item.className = 'pop-item';
    item.style.borderLeft = `3px solid ${sc}`;
    const name = document.createElement('span');
    name.style.color = sc;
    name.textContent = eq.name || eq.id || 'Equipment';
    const location = document.createElement('span');
    location.className = 'pop-dim';
    location.textContent = `${eq.room || '?'} · F${levelIdx}`;
    item.append(name, location);
    item.addEventListener('click', () => window._eqZoom(eq.room, fullLevel || '', eq.id));
    return item;
  });
  results.replaceChildren(...items);
});

document.getElementById('searchEquipment').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    const first = document.getElementById('eqSearchResults').querySelector('div');
    if (first) first.click();
  }
});

function _escape(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

window._renderEquipmentInfo = function(eq) {
  if (!eq) return;
  const sc = eq.status === 'running' ? '#00e676' : eq.status === 'warning' ? '#ffab00' : '#ff1744';
  let html = `<b style="color:${sc}">${_escape(eq.name)}</b> <span style="color:${sc}">${_escape((eq.status||'').toUpperCase())}</span>`;
  html += `<br><span style="color:#aaa">Room: ${_escape(eq.level)}/${_escape(eq.room)} | ${_escape(eq.type)}</span>`;
  html += `<br><span style="color:#777">ID: ${_escape(eq.id)}</span>`;

  const sensors = Array.isArray(eq.sensors) ? eq.sensors : [];
  if (sensors.length) {
    html += '<div style="margin-top:7px"><b style="color:#8ab4f8">Sensors</b></div>';
    for (const sensor of sensors) {
      const value = sensor.data_type === 'binary' ? String(Boolean(sensor.binary_value)) : sensor.value;
      html += `<div style="font-size:10px;color:#ddd">${_escape(sensor.name || sensor.id)}: ` +
        `<span style="color:#fff">${_escape(value)}${sensor.unit ? ' ' + _escape(sensor.unit) : ''}</span></div>`;
    }
  }

  const actuators = Array.isArray(eq.actuators) ? eq.actuators : [];
  if (actuators.length) {
    html += '<div style="margin-top:7px"><b style="color:#8ab4f8">Actuators</b></div>';
    for (const actuator of actuators) {
      html += `<div style="font-size:10px;color:#ddd">${_escape(actuator.name || actuator.id)}: ` +
        `<span style="color:#fff">${_escape(actuator.state)}</span></div>`;
    }
  }

  setInfoCard(html, eq.name);
};

window._refreshEquipmentInfo = async function(eqId) {
  try {
    const resp = await fetch(`/api/equipment/${encodeURIComponent(eqId)}?t=${Date.now()}`);
    if (!resp.ok) return;
    const eq = await resp.json();
    // Keep local cache in sync
    const idx = equipment.findIndex(e => e.id === eqId);
    if (idx >= 0) equipment[idx] = eq;
    window._renderEquipmentInfo(eq);
  } catch (e) { console.warn('refresh equipment failed', e); }
};

window._eqZoom = function(roomName, level, eqId) {
  if (level) searchAndZoom(roomName, level);
  window._refreshEquipmentInfo(eqId);
};

window._showEquipmentInfo = function(eq) {
  if (!eq) return;
  window._renderEquipmentInfo(eq);
  window._refreshEquipmentInfo(eq.id);
};

// === Click equipment sprite on map to open info panel ===
(() => {
  let downPos = null;
  canvas.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    downPos = { x: e.clientX, y: e.clientY, t: Date.now() };
  });
  canvas.addEventListener('mouseup', (e) => {
    if (e.button !== 0 || !downPos) return;
    const dx = e.clientX - downPos.x, dy = e.clientY - downPos.y;
    const moved = Math.hypot(dx, dy);
    const elapsed = Date.now() - downPos.t;
    downPos = null;
    if (moved > 5 || elapsed > 400) return; // treat as drag, not click

    // 2D clicks on equipment are handled by editor.js mousedown handler
    if (!is3DView) return;

    const rect = canvas.getBoundingClientRect();
    mouse.x = ((e.clientX - rect.left) / rect.width) * 2 - 1;
    mouse.y = -((e.clientY - rect.top) / rect.height) * 2 + 1;
    raycaster.setFromCamera(mouse, activeCamera);

    // Gather all clickable equipment sprites (have attached equipment userData and are visible)
    const sprites = [];
    for (const level in equipmentGroups) {
      const g = equipmentGroups[level];
      if (!g || !g.visible) continue;
      for (const s of g.children) {
        if (!s.visible || !s.isSprite) continue;
        if (!s.userData || !s.userData.equipment) continue;
        sprites.push(s);
      }
    }
    if (sprites.length === 0) return;
    const hits = raycaster.intersectObjects(sprites);
    if (hits.length === 0) return;
    const eq = hits[0].object.userData.equipment;
    window._showEquipmentInfo(eq);
    e.stopPropagation();
  });
})();

window.addEventListener('resize', () => {
  renderer.setSize(viewerW(),viewerH(),false);
  camera3D.aspect=viewerW()/viewerH();camera3D.updateProjectionMatrix();
  updateCamera2D();render();
});

init().catch((error) => {
  console.error('BuildSim viewer failed to start:', error);
  if (bootLabelEl) bootLabelEl.textContent = 'Could not load building data';
  setTimeout(bootFinish, 1500);
});
animate();
