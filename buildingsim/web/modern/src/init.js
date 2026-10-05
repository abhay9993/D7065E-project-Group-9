async function init() {
  // Each milestone is reported to the loading screen as it lands (boot.js);
  // the bar is the real progress of this function, not a timer.
  await Promise.all(LEVELS.map((l, i) => loadFloor(l).then(r => {
    bootProgress(1, 'Floor plan ' + (i === 0 ? 'ground' : i));
    return r;
  })));
  // Load cross-floor edges
  try {
    const ceResp = await fetch(`/api/building/cross-floor-edges?t=${Date.now()}`);
    if (!ceResp.ok) throw new Error(`HTTP ${ceResp.status}`);
    crossFloorEdges = await ceResp.json();
  } catch(e) { console.warn('No cross-floor edges:', e); }
  // Load equipment
  try {
    const eqResp = await fetch(`/api/equipment?t=${Date.now()}`);
    if (!eqResp.ok) throw new Error(`HTTP ${eqResp.status}`);
    equipment = await eqResp.json();
    recordInitialEquipmentVersion();
  } catch(e) { console.warn('No equipment data:', e); }
  bootProgress(1, 'Stairs, lifts and equipment');

  const pw = floorData[LEVELS[0]].page.width, ph = floorData[LEVELS[0]].page.height;
  frustumSize = Math.max(pw, ph) * 1.1;
  updateCamera2D();
  buildAllEquipment();
  filterEquipment();
  switchTo3D();
  render();
  bootProgress(1, 'Assembling the storeys');

  // Load independently published runtime snapshots. These are renderer state;
  // student services remain responsible for simulation and decision-making.
  fetchAndApplyOccupancy();
  fetchAndApplyCoverage();
  await fetchAllVisualizations();

  // === Check edit mode ===
  try {
    const cfgResp = await fetch('/api/config');
    if (!cfgResp.ok) throw new Error(`HTTP ${cfgResp.status}`);
    const cfg = await cfgResp.json();
    if (cfg.edit_mode) {
      editModeEnabled = true;
      if (editModeEnabled) document.getElementById('edit-tools').style.display = 'flex';
    }
  } catch(e) {}

  // === Session & WebSocket ===
  // Not awaited: the boot should get the building on screen first; the
  // session connects in the background and pushes state when it is ready.
  initSession();
}
