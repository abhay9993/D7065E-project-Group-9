function switchToFloor(level) {
  deselectRoom(); updateInfo();
  currentLevel = level;
  is3DView = false;
  activeCamera = camera2D;
  orbitControls.enabled = false;

  LEVELS.forEach(l => {
    const fg = floorGroups[l];
    if (!fg) return;
    fg.container.visible = (l === level);
    fg.container.position.set(0, 0, 0);
  });
  stairConnectors.visible = false;
  elevatorConnectors.visible = false;
  crossFloorGraphGroup.visible = false;

  if (editModeEnabled) document.getElementById('edit-tools').style.display = 'flex';

  // Re-render route with correct 2D z-positions
  if (lastRouteResult) {
    while(routeGroup.children.length>0){const c=routeGroup.children[0];routeGroup.remove(c);c.geometry.dispose();c.material.dispose();}
    renderSmoothRoute(lastRouteResult, lastRouteLevelKey);
  }

  // If there's a last searched room on this floor, animate zoom-in to it
  if (_lastSearchedRoom && _lastSearchedRoom.level === level) {
    const sData = floorData[level];
    if (sData) {
      const sRoom = sData.rooms.find(r => r.name === _lastSearchedRoom.name);
      if (sRoom) {
        const pw = sData.page.width, ph = sData.page.height;
        const [swx, swy] = pdfToWorld(sRoom.center[0], sRoom.center[1], pw, ph);

        // Start zoomed out, animate to room
        frustumSize = Math.max(pw, ph) * 1.1;
        panOffset.set(0, 0);
        updateCamera2D();

        _eqFilterRoom = _lastSearchedRoom.name;
        filterEquipment();

        // Highlight the room
        const fg = floorGroups[level];
        if (fg) {
          const mesh = fg.meshes.find(m => m.userData.name === _lastSearchedRoom.name);
          if (mesh) {
            mesh.userData._levelIdx = LEVELS.indexOf(level);
            mesh.material.color.setHex(0x00ff88);
            mesh.material.opacity = 0.7;
            highlightedMesh = mesh;
          }
        }

        // Animate zoom-in
        animateCamera2D(swx, swy, 80, 1500);
        return;
      }
    }
  }

  render();
}

let stairConnectors = new THREE.Group();
let elevatorConnectors = new THREE.Group();
scene.add(stairConnectors, elevatorConnectors);

function createStairSymbol(x, y, z, size) {
  // Stair: zigzag lines (like steps seen from above)
  const group = new THREE.Group();
  const steps = 4;
  const pts = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    pts.push(new THREE.Vector3(-size/2 + t*size, -size/2 + (i%2)*size, 0));
    if (i < steps) pts.push(new THREE.Vector3(-size/2 + (i+1)/steps*size, -size/2 + (i%2)*size, 0));
  }
  const geom = new THREE.BufferGeometry().setFromPoints(pts);
  group.add(new THREE.Line(geom, new THREE.LineBasicMaterial({ color: 0xffaa00, linewidth: 2 })));
  // Border square
  const border = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-size/2,-size/2,0), new THREE.Vector3(size/2,-size/2,0),
    new THREE.Vector3(size/2,size/2,0), new THREE.Vector3(-size/2,size/2,0),
    new THREE.Vector3(-size/2,-size/2,0)]);
  group.add(new THREE.Line(border, new THREE.LineBasicMaterial({ color: 0xffaa00 })));
  group.position.set(x, y, z);
  return group;
}

function createElevatorSymbol(x, y, z, size) {
  // Elevator: square with up/down arrows
  const group = new THREE.Group();
  // Border square
  const border = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-size/2,-size/2,0), new THREE.Vector3(size/2,-size/2,0),
    new THREE.Vector3(size/2,size/2,0), new THREE.Vector3(-size/2,size/2,0),
    new THREE.Vector3(-size/2,-size/2,0)]);
  group.add(new THREE.Line(border, new THREE.LineBasicMaterial({ color: 0x44ccff })));
  // Up arrow
  const up = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(-size*0.15, -size*0.1, 0), new THREE.Vector3(-size*0.15, size*0.3, 0),
    new THREE.Vector3(-size*0.3, size*0.15, 0), new THREE.Vector3(-size*0.15, size*0.3, 0),
    new THREE.Vector3(0, size*0.15, 0)]);
  group.add(new THREE.Line(up, new THREE.LineBasicMaterial({ color: 0x44ccff })));
  // Down arrow
  const dn = new THREE.BufferGeometry().setFromPoints([
    new THREE.Vector3(size*0.15, size*0.1, 0), new THREE.Vector3(size*0.15, -size*0.3, 0),
    new THREE.Vector3(0, -size*0.15, 0), new THREE.Vector3(size*0.15, -size*0.3, 0),
    new THREE.Vector3(size*0.3, -size*0.15, 0)]);
  group.add(new THREE.Line(dn, new THREE.LineBasicMaterial({ color: 0x44ccff })));
  group.position.set(x, y, z);
  return group;
}

function clearGroup(g) {
  while (g.children.length > 0) {
    const c = g.children[0]; g.remove(c);
    if (c.traverse) c.traverse(o => { if(o.geometry) o.geometry.dispose(); if(o.material) o.material.dispose(); });
  }
}

function buildVerticalConnectors() {
  clearGroup(stairConnectors);
  clearGroup(elevatorConnectors);

  const pw = floorData[LEVELS[0]]?.page?.width || 595;
  const ph = floorData[LEVELS[0]]?.page?.height || 842;
  const symSize = 6;

  // Stairs
  for (const stair of STAIRS) {
    const avgX = stair.positions.reduce((s,p) => s+p.x, 0) / stair.positions.length;
    const avgY = stair.positions.reduce((s,p) => s+p.y, 0) / stair.positions.length;
    const [wx, wy] = pdfToWorld(avgX, avgY, pw, ph);
    const minL = Math.min(...stair.positions.map(p => p.l));
    const maxL = Math.max(...stair.positions.map(p => p.l));
    for (const pos of stair.positions) {
      stairConnectors.add(createStairSymbol(wx, wy, pos.l * FLOOR_SPACING + 3, symSize));
    }
    stairConnectors.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(wx,wy,minL*FLOOR_SPACING), new THREE.Vector3(wx,wy,maxL*FLOOR_SPACING)]),
      new THREE.LineBasicMaterial({ color: 0xffaa00, transparent: true, opacity: 0.6 })));
  }

  // Elevators
  for (const elev of ELEVATORS) {
    const avgX = elev.positions.reduce((s,p) => s+p.x, 0) / elev.positions.length;
    const avgY = elev.positions.reduce((s,p) => s+p.y, 0) / elev.positions.length;
    const [wx, wy] = pdfToWorld(avgX, avgY, pw, ph);
    const minL = Math.min(...elev.positions.map(p => p.l));
    const maxL = Math.max(...elev.positions.map(p => p.l));
    for (const pos of elev.positions) {
      elevatorConnectors.add(createElevatorSymbol(wx, wy, pos.l * FLOOR_SPACING + 3, symSize));
    }
    elevatorConnectors.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(wx,wy,minL*FLOOR_SPACING), new THREE.Vector3(wx,wy,maxL*FLOOR_SPACING)]),
      new THREE.LineBasicMaterial({ color: 0x44ccff, transparent: true, opacity: 0.6 })));
  }
}

let crossFloorGraphGroup = new THREE.Group();
scene.add(crossFloorGraphGroup);

function buildCrossFloorGraphEdges() {
  clearGroup(crossFloorGraphGroup);
  if (!crossFloorEdges.length) return;

  for (const edge of crossFloorEdges) {
    const fromLevel = edge.from_level;
    const toLevel = edge.to_level;
    const fromData = floorData[fromLevel];
    const toData = floorData[toLevel];
    if (!fromData || !toData) continue;

    // Match by name since IDs change on save
    const fromRoom = fromData.rooms.find(r => r.name === edge.from_name);
    const toRoom = toData.rooms.find(r => r.name === edge.to_name);
    if (!fromRoom || !toRoom) continue;

    const fromIdx = LEVELS.indexOf(fromLevel);
    const toIdx = LEVELS.indexOf(toLevel);
    const pw = fromData.page.width, ph = fromData.page.height;

    const [x1, y1] = pdfToWorld(fromRoom.center[0], fromRoom.center[1], pw, ph);
    const [x2, y2] = pdfToWorld(toRoom.center[0], toRoom.center[1], toData.page.width, toData.page.height);

    const pts = [
      new THREE.Vector3(x1, y1, fromIdx * FLOOR_SPACING + 4),
      new THREE.Vector3(x2, y2, toIdx * FLOOR_SPACING + 4),
    ];
    crossFloorGraphGroup.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints(pts),
      new THREE.LineBasicMaterial({ color: 0xff66ff, transparent: true, opacity: 0.7 })));

    for (const p of pts) {
      const dot = new THREE.Mesh(new THREE.CircleGeometry(1.5, 8),
        new THREE.MeshBasicMaterial({ color: 0xff66ff, side: THREE.DoubleSide }));
      dot.position.copy(p);
      crossFloorGraphGroup.add(dot);
    }
  }

  crossFloorGraphGroup.visible = document.getElementById('showGraph').checked;
}
