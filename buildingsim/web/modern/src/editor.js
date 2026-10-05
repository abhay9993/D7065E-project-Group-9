function filterEquipment(roomName) {
  if (roomName !== undefined) _eqFilterRoom = roomName;
  const showAll = document.getElementById('showEquipment').checked;
  const showCounts = document.getElementById('showPeopleCount')?.checked !== false;
  const activeRoom = _eqFilterRoom || (selectedRoom ? selectedRoom.name : null);

  // Items live in their rooms, laid out identically in 2D and 3D — show the
  // in-room sprites directly (no collapsed toolbox icon).
  LEVELS.forEach(l => {
    const eg = equipmentGroups[l];
    if (!eg) return;
    eg.children.forEach(sprite => {
      const room = sprite.userData.eqRoom;
      // Occupant counts answer "how many are in there", which is worth seeing
      // without opening a room, so they follow their own checkbox.
      if (sprite.userData.eqMode === 'count') { sprite.visible = showCounts; return; }
      if (showAll) sprite.visible = true;
      else if (activeRoom) sprite.visible = room === activeRoom;
      else sprite.visible = false;
    });
  });
  render();
}

function getEquipmentInRoom(roomName, levelKey) {
  return equipment.filter(e => e.room === roomName && e.level === levelKey);
}

function switchTo3D() {
  deselectRoom(); updateInfo();
  is3DView = true;
  activeCamera = camera3D;
  orbitControls.enabled = true;
  editMode = 'select';
  document.getElementById('btnEditRoom').classList.remove('active');
  document.getElementById('btnEraseWalls').classList.remove('active');

  LEVELS.forEach((l, i) => {
    const fg = floorGroups[l];
    if (!fg) return;
    fg.container.visible = true;
    const align = FLOOR_ALIGN[i] || {x:0,y:0};
    fg.container.position.set(align.x, align.y, i * FLOOR_SPACING);
  });

  buildVerticalConnectors();
  buildCrossFloorGraphEdges();
  stairConnectors.visible = document.getElementById('showStairs').checked;
  elevatorConnectors.visible = document.getElementById('showElevators').checked;

  document.getElementById('edit-tools').style.display = 'none';

  // Re-render route with correct 3D z-positions
  if (lastRouteResult) {
    while(routeGroup.children.length>0){const c=routeGroup.children[0];routeGroup.remove(c);c.geometry.dispose();c.material.dispose();}
    renderSmoothRoute(lastRouteResult, lastRouteLevelKey);
  }

  render();
}

function updateCamera2D() {
  const a = viewerW() / viewerH();
  camera2D.left = -frustumSize*a/2 + panOffset.x;
  camera2D.right = frustumSize*a/2 + panOffset.x;
  camera2D.top = frustumSize/2 + panOffset.y;
  camera2D.bottom = -frustumSize/2 + panOffset.y;
  camera2D.updateProjectionMatrix();
}

function render() { renderer.render(scene, activeCamera); }
let eqAnimTime = 0;
function animate() {
  requestAnimationFrame(animate);
  const nowMs = performance.now();
  if (typeof updateVisualizationAnimations === 'function') updateVisualizationAnimations(nowMs);
  // live layers drive themselves off the same frame clock
  if (typeof updateLabelFade === 'function') updateLabelFade();
  eqAnimTime += 0.02;
  // Animate equipment pulse rings
  const pulseScale = 1.0 + 0.15 * Math.sin(eqAnimTime * 2);
  const pulseOpacity = 0.25 + 0.2 * Math.sin(eqAnimTime * 2);
  for (const level in equipmentGroups) {
    const eg = equipmentGroups[level];
    if (!eg || !eg.visible) continue;
    eg.traverse(obj => {
      if (obj.userData?.isPulse) {
        obj.scale.set(pulseScale, pulseScale, 1);
        obj.material.opacity = pulseOpacity;
      }
      if (obj.userData?.isLedGlow) {
        obj.material.opacity = 0.2 + 0.35 * Math.abs(Math.sin(eqAnimTime * 3));
      }
      if (obj.userData?.isSteam) {
        obj.material.opacity = 0.15 + 0.2 * Math.sin(eqAnimTime * 4 + obj.id);
        obj.position.z = 0.25 + 0.03 * Math.sin(eqAnimTime * 2);
      }
      if (obj.userData?.isCoffeeStream) {
        obj.material.opacity = 0.3 + 0.25 * Math.sin(eqAnimTime * 5);
      }
      if (obj.userData?.isScreenBar) {
        obj.material.opacity = 0.5 + 0.3 * Math.sin(eqAnimTime * 1.5);
      }
      if (obj.userData?.isAirFlow) {
        obj.material.opacity = 0.15 + 0.2 * Math.sin(eqAnimTime * 3 + obj.id * 0.5);
        obj.position.y += Math.sin(eqAnimTime * 4) * 0.002;
      }
      if (obj.userData?.isFanBlade) {
        obj.rotation.z = eqAnimTime * 2;
      }
    });
  }
  const needsRender = Object.values(equipmentGroups).some(g => g && g.visible) ||
    (typeof visualEffects !== 'undefined' && visualEffects.length > 0) ||
    (typeof mobileEntities !== 'undefined' && mobileEntities.length > 0);
  if (is3DView) {
    // the flight camera takes the wheel; the orbiter would fight it
    if (typeof cameraLocked === 'undefined' || !cameraLocked) orbitControls.update();
    // Make stair/elevator symbols face camera
    stairConnectors.children.forEach(c => { if (c.isGroup) c.lookAt(camera3D.position); });
    elevatorConnectors.children.forEach(c => { if (c.isGroup) c.lookAt(camera3D.position); });
    render();
  } else if (needsRender) {
    render();
  }
}
function getWorldPos(e) {
  const rect = canvas.getBoundingClientRect();
  return new THREE.Vector3(((e.clientX-rect.left)/rect.width)*2-1, -((e.clientY-rect.top)/rect.height)*2+1, 0).unproject(activeCamera);
}
function vertexSize() { return frustumSize * 0.005; }

function makeHandle(wx, wy, color, sz) {
  const h = new THREE.Mesh(new THREE.PlaneGeometry(sz*2, sz*2),
    new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide, depthTest: false }));
  h.position.set(wx, wy, 5);
  h.renderOrder = 999;
  return h;
}

function showVertexHandles() {
  hideVertexHandles();
  if (!selectedRoom) return;
  const data = floorData[currentLevel];
  const pw = data.page.width, ph = data.page.height, sz = vertexSize();
  const seenPos = new Set();
  selectedRoom.polygon.forEach((pt, idx) => {
    const [wx,wy] = pdfToWorld(pt[0], pt[1], pw, ph);
    const posKey = `${Math.round(pt[0]*10)},${Math.round(pt[1]*10)}`;
    if (seenPos.has(posKey)) return; // skip duplicate position
    seenPos.add(posKey);
    const h = makeHandle(wx, wy, 0xff6644, sz);
    h.userData = { vertexIndex: idx };
    vertexGroup.add(h); vertexHandles.push(h);
  });
  for (let i = 0; i < selectedRoom.polygon.length; i++) {
    const p1 = selectedRoom.polygon[i], p2 = selectedRoom.polygon[(i+1)%selectedRoom.polygon.length];
    const [wx,wy] = pdfToWorld((p1[0]+p2[0])/2, (p1[1]+p2[1])/2, pw, ph);
    const h = makeHandle(wx, wy, 0x44cc66, sz*0.6);
    h.userData = { midpointAfter: i };
    vertexGroup.add(h); vertexHandles.push(h);
  }
}

function hideVertexHandles() {
  vertexHandles.forEach(h => { h.geometry.dispose(); h.material.dispose(); vertexGroup.remove(h); });
  vertexHandles = [];
}

function selectRoom(mesh) {
  deselectRoom();
  selectedMesh = mesh; selectedRoom = mesh.userData;
  mesh.material.color.setHex(0x4a8fd4); mesh.material.opacity = 0.6;
  document.getElementById('btnRect').disabled = false;
  document.getElementById('btnDelete').disabled = false;
  document.getElementById('btnMerge').disabled = false;
  document.getElementById('btnSplitH').disabled = false;
  document.getElementById('btnSplitV').disabled = false;
  document.getElementById('btnSplitFree').disabled = false;
  document.getElementById('btnRename').disabled = false;
  document.getElementById('btnToggleCorridor').disabled = false;
  document.getElementById('btnWeldVerts').disabled = false;
  document.getElementById('btnSnapWalls').disabled = false;
  document.getElementById('btnNudgeU').disabled = false;
  document.getElementById('btnNudgeD').disabled = false;
  document.getElementById('btnNudgeL').disabled = false;
  document.getElementById('btnNudgeR').disabled = false;
  showVertexHandles(); updateInfo();
  filterEquipment();
}

function deselectRoom() {
  if (selectedMesh) {
    const levelIdx = LEVELS.indexOf(currentLevel);
    selectedMesh.material.color.setHex(FLOOR_COLORS[levelIdx]);
    selectedMesh.material.opacity = 0.4;
  }
  selectedMesh = null; selectedRoom = null; hideVertexHandles();
  _eqFilterRoom = null;
  filterEquipment();
  document.getElementById('btnRect').disabled = true;
  document.getElementById('btnDelete').disabled = true;
  document.getElementById('btnMerge').disabled = true;
  document.getElementById('btnSplitH').disabled = true;
  document.getElementById('btnSplitV').disabled = true;
  document.getElementById('btnSplitFree').disabled = true;
  document.getElementById('btnRename').disabled = true;
  document.getElementById('btnToggleCorridor').disabled = true;
  document.getElementById('btnWeldVerts').disabled = true;
  document.getElementById('btnSnapWalls').disabled = true;
  document.getElementById('btnNudgeU').disabled = true;
  document.getElementById('btnNudgeD').disabled = true;
  document.getElementById('btnNudgeL').disabled = true;
  document.getElementById('btnNudgeR').disabled = true;
}

function updateInfo() {
  if (!selectedRoom) { hideRoomCard(); return; }
  {
    const typeLabel = selectedRoom.type === 'corridor' ? '<span style="color:#cccc44">[CORRIDOR]</span>' : '[ROOM]';
    const levelKey = currentLevel.split('/').pop();
    const roomEquip = getEquipmentInRoom(selectedRoom.name, levelKey);
    let equipHtml = '';
    if (roomEquip.length > 0) {
      equipHtml = '<hr style="border-color:#333;margin:6px 0">';
      for (const eq of roomEquip) {
        const sc = eq.status === 'running' ? '#00e676' : eq.status === 'warning' ? '#ffab00' : '#ff1744';
        const status = String(eq.status || 'unknown').toUpperCase();
        equipHtml += `<div style="margin:4px 0;padding:4px;background:rgba(0,230,118,0.08);border-left:3px solid ${sc};padding-left:8px">`;
        equipHtml += `<b style="color:${sc}">${_escape(eq.name || eq.id)}</b> `;
        equipHtml += `<span style="color:${sc};font-size:10px">${_escape(status)}</span><br>`;
        equipHtml += `<span style="font-size:11px;color:#aaa">${_escape(eq.type || 'equipment')}</span>`;
        const sensors = Array.isArray(eq.sensors) ? eq.sensors : [];
        const actuators = Array.isArray(eq.actuators) ? eq.actuators : [];
        if (sensors.length || actuators.length) {
          equipHtml += `<div style="margin-top:3px;font-size:10px;color:#888">${sensors.length} sensor(s), ${actuators.length} actuator(s)</div>`;
        }
        equipHtml += '</div>';
      }
    }
    setInfoCard(`${typeLabel}<br>Vertices: ${selectedRoom.polygon.length}${equipHtml}<br>
      <span style="color:#ff6644">Drag orange = move</span><br>
      <span style="color:#44cc66">Click green = add vertex</span><br>
      <span style="color:#aaa">Right-click orange = remove</span>`,
      selectedRoom.name || ('Room #' + selectedRoom.id));
  }
}

function rebuildRoom() {
  if (!selectedRoom || !selectedMesh) return;
  const data = floorData[currentLevel];
  const pw = data.page.width, ph = data.page.height, pts = selectedRoom.polygon;
  const shape = new THREE.Shape();
  const [sx,sy] = pdfToWorld(pts[0][0],pts[0][1],pw,ph); shape.moveTo(sx,sy);
  for (let i=1;i<pts.length;i++){const[px,py]=pdfToWorld(pts[i][0],pts[i][1],pw,ph);shape.lineTo(px,py);}
  shape.closePath();
  selectedMesh.geometry.dispose(); selectedMesh.geometry = new THREE.ShapeGeometry(shape);
  const fg = floorGroups[currentLevel];
  fg.roomGroup.children.forEach(c => {
    if (c.userData?.roomOutline && c.userData.roomId === selectedRoom.id) {
      const op = pts.map(p => { const[ox,oy]=pdfToWorld(p[0],p[1],pw,ph); return new THREE.Vector3(ox,oy,2); });
      op.push(op[0].clone()); c.geometry.dispose(); c.geometry = new THREE.BufferGeometry().setFromPoints(op);
    }
  });
}

// Undo
function pushUndo() {
  const data = floorData[currentLevel];
  undoStack.push({ level: currentLevel,
    rooms: JSON.parse(JSON.stringify(data.rooms)),
    walls: JSON.parse(JSON.stringify(data.walls)) });
  if (undoStack.length > MAX_UNDO) undoStack.shift();
  document.getElementById('btnUndo').disabled = false;
}
function doUndo() {
  if (undoStack.length === 0) return;
  const snap = undoStack.pop();
  if (snap.level !== currentLevel) switchToFloor(snap.level);
  const data = floorData[currentLevel];
  data.rooms = snap.rooms; data.walls = snap.walls;
  // Rebuild
  const fg = floorGroups[currentLevel];
  fg.meshes.length = 0;
  const toRm = [...fg.roomGroup.children]; toRm.forEach(c=>{fg.roomGroup.remove(c);if(c.geometry)c.geometry.dispose();if(c.material)c.material.dispose();});
  const wRm = [...fg.wallGroup.children]; wRm.forEach(c=>{fg.wallGroup.remove(c);c.geometry.dispose();c.material.dispose();});
  const pw = data.page.width, ph = data.page.height;
  createLineSegments(data.walls, 0x888888, fg.wallGroup, 0.6, pw, ph);
  createRoomPolygons(data.rooms, fg.roomGroup, fg.meshes, FLOOR_COLORS[LEVELS.indexOf(currentLevel)], pw, ph);
  const lRm = [...fg.labelGroup.children]; lRm.forEach(c=>{fg.labelGroup.remove(c);if(c.geometry)c.geometry.dispose();if(c.material)c.material.dispose();});
  createLabels(data.labels, fg.labelGroup, pw, ph);
  deselectRoom(); updateInfo();
  document.getElementById('btnUndo').disabled = undoStack.length === 0;
  render();
}

// Wall erase
function distPtSeg(px,py,x1,y1,x2,y2) {
  const dx=x2-x1,dy=y2-y1,l2=dx*dx+dy*dy;
  if(l2===0)return Math.hypot(px-x1,py-y1);
  let t=((px-x1)*dx+(py-y1)*dy)/l2; t=Math.max(0,Math.min(1,t));
  return Math.hypot(px-(x1+t*dx),py-(y1+t*dy));
}
function findNearestWall(wx,wy,maxDist) {
  const data = floorData[currentLevel];
  const pw=data.page.width,ph=data.page.height;
  const[px,py]=worldToPdf(wx,wy,pw,ph);
  let best=-1,bd=maxDist;
  for(let i=0;i<data.walls.length;i++){
    const s=data.walls[i];
    const d=distPtSeg(px,py,s[0][0],s[0][1],s[1][0],s[1][1]);
    if(d<bd){bd=d;best=i;}
  }
  return best;
}
function highlightWall(idx) {
  if(eraseHighlight){scene.remove(eraseHighlight);eraseHighlight.geometry.dispose();eraseHighlight.material.dispose();eraseHighlight=null;}
  if(idx<0){eraseHoverIdx=-1;return;}
  eraseHoverIdx=idx;
  const data=floorData[currentLevel]; const s=data.walls[idx];
  const pw=data.page.width,ph=data.page.height;
  const[x1,y1]=pdfToWorld(s[0][0],s[0][1],pw,ph);
  const[x2,y2]=pdfToWorld(s[1][0],s[1][1],pw,ph);
  eraseHighlight=new THREE.Line(new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(x1,y1,6),new THREE.Vector3(x2,y2,6)]),
    new THREE.LineBasicMaterial({color:0xff0000}));
  scene.add(eraseHighlight);
}
function eraseWall(idx) {
  const data=floorData[currentLevel];
  if(idx<0||idx>=data.walls.length)return;
  pushUndo();
  data.walls.splice(idx,1);
  const fg=floorGroups[currentLevel];
  const wRm=[...fg.wallGroup.children];wRm.forEach(c=>{fg.wallGroup.remove(c);c.geometry.dispose();c.material.dispose();});
  createLineSegments(data.walls,0x888888,fg.wallGroup,0.6,data.page.width,data.page.height);
  highlightWall(-1); isDirty=true; render();
}

// The building picker, the floor buttons and the 3D switch all used to live
// in a panel in the corner. There is one building, and the toolbar at the
// bottom of the view is now the single home for the rest.

// Mouse events
canvas.addEventListener('contextmenu', (e) => {
  e.preventDefault();
  if (is3DView || !selectedRoom || editMode !== 'editRoom') return;
  { const r=canvas.getBoundingClientRect(); mouse.x=((e.clientX-r.left)/r.width)*2-1; mouse.y=-((e.clientY-r.top)/r.height)*2+1; }
  raycaster.setFromCamera(mouse, activeCamera);
  const vh=raycaster.intersectObjects(vertexHandles);
  if(vh.length>0 && vh[0].object.userData.vertexIndex!==undefined) {
    if(selectedRoom.polygon.length>3){pushUndo();selectedRoom.polygon.splice(vh[0].object.userData.vertexIndex,1);rebuildRoom();showVertexHandles();updateInfo();isDirty=true;render();}
  }
});

canvas.addEventListener('mousedown', (e) => {
  if (e.button !== 0) return;
  if (is3DView) return; // orbit controls handle 3D
  { const r=canvas.getBoundingClientRect(); mouse.x=((e.clientX-r.left)/r.width)*2-1; mouse.y=-((e.clientY-r.top)/r.height)*2+1; }

  if (editMode==='eraseWalls') { if(eraseHoverIdx>=0) eraseWall(eraseHoverIdx); return; }

  // Draw wall mode
  if (drawWallMode) {
    const wp = getWorldPos(e);
    const data = floorData[currentLevel];
    const [px,py] = worldToPdf(wp.x, wp.y, data.page.width, data.page.height);
    const pt = [Math.round(px*10)/10, Math.round(py*10)/10];

    if (drawWallPoints.length > 0) {
      // Add wall segment from last point to this point
      const prev = drawWallPoints[drawWallPoints.length-1];
      // Snap to H/V if close
      if (Math.abs(pt[0]-prev[0]) < Math.abs(pt[1]-prev[1])*0.3) pt[0] = prev[0]; // vertical
      else if (Math.abs(pt[1]-prev[1]) < Math.abs(pt[0]-prev[0])*0.3) pt[1] = prev[1]; // horizontal

      data.walls.push([prev, pt]);

      // Rebuild wall display
      const fg = floorGroups[currentLevel];
      while(fg.wallGroup.children.length>0){const c=fg.wallGroup.children[0];fg.wallGroup.remove(c);c.geometry.dispose();c.material.dispose();}
      createLineSegments(data.walls, 0x888888, fg.wallGroup, 0.6, data.page.width, data.page.height);
      isDirty = true;
    }

    drawWallPoints.push(pt);

    // Show dot at click point
    if (drawWallPreview) { scene.remove(drawWallPreview); drawWallPreview.geometry.dispose(); drawWallPreview.material.dispose(); }
    const dot = new THREE.Mesh(new THREE.CircleGeometry(0.8, 8), new THREE.MeshBasicMaterial({color:0x00ff00, side:THREE.DoubleSide, depthTest:false}));
    dot.position.set(wp.x, wp.y, 6); dot.renderOrder = 999;
    drawWallPreview = dot; scene.add(dot);

    render();
    return;
  }

  // New room mode
  if (newRoomMode) {
    const wp = getWorldPos(e);
    const data = floorData[currentLevel];
    const [px,py] = worldToPdf(wp.x, wp.y, data.page.width, data.page.height);
    if (!newRoomCorner1) {
      newRoomCorner1 = [px, py];
      document.getElementById('btnNewRoom').textContent = 'Click 2nd corner...';
      setInfoCard('<b>New Room:</b> Click second corner', 'Edit');
    } else {
      // Create room from two corners
      pushUndo();
      const c1 = newRoomCorner1, c2 = [px, py];
      const minX = Math.min(c1[0],c2[0]), maxX = Math.max(c1[0],c2[0]);
      const minY = Math.min(c1[1],c2[1]), maxY = Math.max(c1[1],c2[1]);
      const poly = [[minX,minY],[maxX,minY],[maxX,maxY],[minX,maxY]];

      const fg = floorGroups[currentLevel];
      const newId = data.rooms.length > 0 ? Math.max(...data.rooms.map(r=>r.id)) + 1 : 0;
      const name = prompt('Room name:', 'New_' + newId) || 'New_' + newId;
      const newRoom = {
        id: newId, name: name,
        type: 'room',
        area: Math.round((maxX-minX)*(maxY-minY)),
        center: [Math.round((minX+maxX)/2*100)/100, Math.round((minY+maxY)/2*100)/100],
        polygon: poly,
      };
      data.rooms.push(newRoom);

      const pw = data.page.width, ph = data.page.height;
      const levelIdx = LEVELS.indexOf(currentLevel);
      const shape = new THREE.Shape();
      const [sx,sy] = pdfToWorld(poly[0][0],poly[0][1],pw,ph); shape.moveTo(sx,sy);
      for (let i=1;i<poly.length;i++){const[ppx,ppy]=pdfToWorld(poly[i][0],poly[i][1],pw,ph);shape.lineTo(ppx,ppy);}
      shape.closePath();
      const mesh = new THREE.Mesh(new THREE.ShapeGeometry(shape),
        new THREE.MeshBasicMaterial({color:FLOOR_COLORS[levelIdx],transparent:true,opacity:0.4,side:THREE.DoubleSide}));
      mesh.userData = newRoom; mesh.position.z = 1;
      fg.roomGroup.add(mesh); fg.meshes.push(mesh);
      const op = poly.map(p=>{const[ox,oy]=pdfToWorld(p[0],p[1],pw,ph);return new THREE.Vector3(ox,oy,2);});
      op.push(op[0].clone());
      const outline = new THREE.Line(new THREE.BufferGeometry().setFromPoints(op),
        new THREE.LineBasicMaterial({color:0x4a6fa5,transparent:true,opacity:0.6}));
      outline.userData = {roomOutline:true, roomId:newId};
      fg.roomGroup.add(outline);

      // Clean up
      if (newRoomPreview) { scene.remove(newRoomPreview); newRoomPreview.geometry.dispose(); newRoomPreview.material.dispose(); newRoomPreview = null; }
      newRoomMode = false; newRoomCorner1 = null;
      document.getElementById('btnNewRoom').classList.remove('active');
      document.getElementById('btnNewRoom').textContent = 'New Room';
      canvas.style.cursor = 'default';
      selectRoom(mesh);
      editMode = 'editRoom';
      document.getElementById('btnEditRoom').classList.add('active');
      isDirty = true;
    }
    render();
    return;
  }

  // Freeform split: capture two click points
  if (splitFreeMode && selectedRoom) {
    const wp = getWorldPos(e);
    const data = floorData[currentLevel];
    const [px,py] = worldToPdf(wp.x, wp.y, data.page.width, data.page.height);

    if (!splitPoint1) {
      splitPoint1 = [px, py];
      document.getElementById('btnSplitFree').textContent = 'Click 2nd point...';
      setInfoCard('<b>Split:</b> Click second point on room edge', 'Edit');
      // Show first point marker
      if (splitLine) { scene.remove(splitLine); splitLine.geometry.dispose(); splitLine.material.dispose(); }
      const dot = new THREE.Mesh(new THREE.CircleGeometry(1, 8), new THREE.MeshBasicMaterial({color:0xff0000,side:THREE.DoubleSide}));
      dot.position.set(wp.x, wp.y, 6);
      splitLine = dot;
      scene.add(splitLine);
      render();
    } else {
      // Second point - do the split
      const p2 = [px, py];
      if (splitLine) { scene.remove(splitLine); splitLine.geometry.dispose(); splitLine.material.dispose(); splitLine = null; }
      doFreeformSplit(splitPoint1, p2);
      splitFreeMode = false; splitPoint1 = null;
      document.getElementById('btnSplitFree').classList.remove('active');
      document.getElementById('btnSplitFree').textContent = 'Split Free';
    }
    return;
  }

  if (editMode==='editRoom') {
    if (selectedRoom) {
      raycaster.setFromCamera(mouse, activeCamera);
      const vh=raycaster.intersectObjects(vertexHandles);
      if(vh.length>0){
        const hit=vh[0].object;

        // Weld mode: click two vertices to merge them
        if(weldMode && hit.userData.vertexIndex!==undefined){
          if(weldFirstIdx===null){
            weldFirstIdx=hit.userData.vertexIndex;
            hit.material.color.setHex(0xff0000);
            document.getElementById('btnWeldVerts').textContent='Click 2nd vertex...';
            setInfoCard('<b>Weld:</b> Click second vertex to merge', 'Edit');
            render();
          } else {
            const a=weldFirstIdx, b=hit.userData.vertexIndex;
            if(a!==b){
              pushUndo();
              // Weld: move B to A's position, keep both vertices
              // This creates a concave polygon (pinch at A's position)
              // B's neighbors now route through A, forming L/U shapes
              const pA=selectedRoom.polygon[a];
              selectedRoom.polygon[b]=[pA[0],pA[1]];
              rebuildRoom();showVertexHandles();updateInfo();isDirty=true;
            }
            weldMode=false; weldFirstIdx=null;
            document.getElementById('btnWeldVerts').classList.remove('active');
            document.getElementById('btnWeldVerts').textContent='Weld Vertices';
            render();
          }
          return;
        }

        if(hit.userData.midpointAfter!==undefined){
          pushUndo();
          const i=hit.userData.midpointAfter,p1=selectedRoom.polygon[i],p2=selectedRoom.polygon[(i+1)%selectedRoom.polygon.length];
          selectedRoom.polygon.splice(i+1,0,[Math.round((p1[0]+p2[0])/2*100)/100,Math.round((p1[1]+p2[1])/2*100)/100]);
          rebuildRoom();showVertexHandles();updateInfo();isDirty=true;render();
          const nh=vertexHandles.filter(h=>h.userData.vertexIndex===i+1);
          if(nh.length>0){dragVertex=nh[0];canvas.style.cursor='crosshair';}
          return;
        }
        pushUndo(); dragVertex=hit; canvas.style.cursor='crosshair'; return;
      }
    }
    raycaster.setFromCamera(mouse, activeCamera);
    const fg = floorGroups[currentLevel];
    const rh=raycaster.intersectObjects(fg.meshes);
    if(rh.length>0){
      if(mergeSource){executeMerge(rh[0].object);return;}
      selectRoom(rh[0].object);render();return;
    }
    if(mergeSource){cancelMerge();render();return;}
    if(selectedRoom){deselectRoom();updateInfo();render();return;}
  } else {
    raycaster.setFromCamera(mouse, activeCamera);

    // Click an in-room equipment sprite -> show its info panel
    if (!is3DView) {
      const eg = equipmentGroups[currentLevel];
      if (eg) {
        const expandedSprites = eg.children.filter(s => s.visible && s.isSprite && s.userData && s.userData.equipment);
        const expHits = raycaster.intersectObjects(expandedSprites);
        if (expHits.length > 0) {
          window._showEquipmentInfo(expHits[0].object.userData.equipment);
          return;
        }
      }
    }

    // Click room to show info
    const fg = floorGroups[currentLevel];
    if (fg) {
      const rh = raycaster.intersectObjects(fg.meshes);
      if (rh.length > 0) {
        const room = rh[0].object.userData;
        setInfoCard(`Area: ${Math.round(room.area)} sq units<br>Vertices: ${room.polygon.length}`, room.name || ('Room #' + room.id));
        return;
      }
    }
  }

  isPanning=true; panStart.set(e.clientX,e.clientY); canvas.style.cursor='grabbing';
});

canvas.addEventListener('mousemove', (e) => {
  if (is3DView) return;
  if(dragVertex){
    const wp=getWorldPos(e);
    const data=floorData[currentLevel],pw=data.page.width,ph=data.page.height;
    let[px,py]=worldToPdf(wp.x,wp.y,pw,ph);
    const idx=dragVertex.userData.vertexIndex;
    const poly=selectedRoom.polygon;
    const snap=document.getElementById('snapRight').checked;

    if(snap && poly.length===4){
      // Rectangle drag: moving corner 0 also moves corners 1 and 3
      // Vertices go: 0(TL) 1(TR) 2(BR) 3(BL) or similar order
      // Adjacent vertices share one coordinate with the dragged vertex
      const prevIdx=(idx-1+4)%4, nextIdx=(idx+1)%4, oppIdx=(idx+2)%4;
      // prev shares one axis, next shares the other
      // Figure out which: compare prev to opposite
      if(Math.abs(poly[prevIdx][0]-poly[oppIdx][0]) < Math.abs(poly[prevIdx][1]-poly[oppIdx][1])){
        // prev and opp share X -> prev-to-idx edge is horizontal -> prev gets new Y
        poly[prevIdx][1]=py;
        poly[nextIdx][0]=px;
      } else {
        // prev and opp share Y -> prev-to-idx edge is vertical -> prev gets new X
        poly[prevIdx][0]=px;
        poly[nextIdx][1]=py;
      }
    }

    px=Math.round(px*100)/100; py=Math.round(py*100)/100;
    poly[idx]=[px,py];
    const[snappedWx,snappedWy]=pdfToWorld(px,py,pw,ph);
    dragVertex.position.set(snappedWx,snappedWy,5);
    // Update other vertex handle positions if snap moved them
    if(snap && poly.length===4){
      vertexHandles.forEach(h => {
        if(h.userData.vertexIndex!==undefined && h.userData.vertexIndex!==idx){
          const p=poly[h.userData.vertexIndex];
          const[wx,wy]=pdfToWorld(p[0],p[1],pw,ph);
          h.position.set(wx,wy,5);
        }
      });
    }
    rebuildRoom();isDirty=true;render();return;
  }
  if(isPanning){
    const dx=(e.clientX-panStart.x)/viewerW()*frustumSize*(viewerW()/viewerH());
    const dy=-(e.clientY-panStart.y)/viewerH()*frustumSize;
    panOffset.x-=dx;panOffset.y-=dy;panStart.set(e.clientX,e.clientY);updateCamera2D();render();return;
  }
  { const r=canvas.getBoundingClientRect(); mouse.x=((e.clientX-r.left)/r.width)*2-1; mouse.y=-((e.clientY-r.top)/r.height)*2+1; }
  raycaster.setFromCamera(mouse,activeCamera);

  // Draw wall preview line
  if(drawWallMode && drawWallPoints.length > 0){
    const wp = getWorldPos(e);
    const data = floorData[currentLevel];
    const pw=data.page.width, ph=data.page.height;
    const [px,py] = worldToPdf(wp.x, wp.y, pw, ph);
    const prev = drawWallPoints[drawWallPoints.length-1];
    let snapX = px, snapY = py;
    if (Math.abs(snapX-prev[0]) < Math.abs(snapY-prev[1])*0.3) snapX = prev[0];
    else if (Math.abs(snapY-prev[1]) < Math.abs(snapX-prev[0])*0.3) snapY = prev[1];
    const [wx1,wy1] = pdfToWorld(prev[0], prev[1], pw, ph);
    const [wx2,wy2] = pdfToWorld(snapX, snapY, pw, ph);
    if(drawWallPreview) { scene.remove(drawWallPreview); drawWallPreview.geometry.dispose(); drawWallPreview.material.dispose(); }
    const geom = new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(wx1,wy1,6), new THREE.Vector3(wx2,wy2,6)]);
    drawWallPreview = new THREE.Line(geom, new THREE.LineBasicMaterial({color:0x00ff00, depthTest:false}));
    drawWallPreview.renderOrder = 999;
    scene.add(drawWallPreview);
    canvas.style.cursor = 'crosshair';
    render(); return;
  }
  if(newRoomMode && newRoomCorner1){
    const wp=getWorldPos(e);
    const data=floorData[currentLevel],pw=data.page.width,ph=data.page.height;
    const[px,py]=worldToPdf(wp.x,wp.y,pw,ph);
    const c=newRoomCorner1;
    const[x1,y1]=pdfToWorld(Math.min(c[0],px),Math.min(c[1],py),pw,ph);
    const[x2,y2]=pdfToWorld(Math.max(c[0],px),Math.max(c[1],py),pw,ph);
    if(newRoomPreview){scene.remove(newRoomPreview);newRoomPreview.geometry.dispose();newRoomPreview.material.dispose();}
    newRoomPreview=new THREE.Line(new THREE.BufferGeometry().setFromPoints([
      new THREE.Vector3(x1,y1,6),new THREE.Vector3(x2,y1,6),new THREE.Vector3(x2,y2,6),
      new THREE.Vector3(x1,y2,6),new THREE.Vector3(x1,y1,6)]),
      new THREE.LineBasicMaterial({color:0x00ff00}));
    scene.add(newRoomPreview);render();return;
  }
  if(editMode==='eraseWalls'){
    const wp=getWorldPos(e);const md=frustumSize*0.005;
    highlightWall(findNearestWall(wp.x,wp.y,md));
    canvas.style.cursor=eraseHoverIdx>=0?'crosshair':'default';render();return;
  }
  if(selectedRoom){canvas.style.cursor=raycaster.intersectObjects(vertexHandles).length>0?'crosshair':'default';return;}
  const fg=floorGroups[currentLevel];
  if(fg){
    const hits=raycaster.intersectObjects(fg.meshes);
    // Hover only offers the room; the card is opened by clicking it, so a
    // pass of the mouse can no longer wipe what you asked to see.
    canvas.style.cursor=hits.length>0?'pointer':'default';
  }
});

canvas.addEventListener('mouseup', () => {
  if(dragVertex){dragVertex=null;showVertexHandles();updateInfo();render();return;}
  isPanning=false;canvas.style.cursor='default';
});

canvas.addEventListener('wheel', (e) => {
  if(is3DView)return;
  e.preventDefault();
  frustumSize*=e.deltaY>0?1.1:0.9;
  const maxZoom = Math.max(floorData[LEVELS[0]]?.page?.width||400, floorData[LEVELS[0]]?.page?.height||400) * 1.5;
  frustumSize=Math.max(10,Math.min(maxZoom,frustumSize));
  updateCamera2D();if(selectedRoom)showVertexHandles();render();
}, {passive:false});

// Touch pinch-to-zoom for mobile
let touchDist = 0;
canvas.addEventListener('touchstart', (e) => {
  if (e.touches.length === 2) {
    const dx = e.touches[0].clientX - e.touches[1].clientX;
    const dy = e.touches[0].clientY - e.touches[1].clientY;
    touchDist = Math.sqrt(dx*dx + dy*dy);
  }
}, {passive: true});
canvas.addEventListener('touchmove', (e) => {
  if (is3DView) return;
  if (e.touches.length === 2) {
    e.preventDefault();
    const dx = e.touches[0].clientX - e.touches[1].clientX;
    const dy = e.touches[0].clientY - e.touches[1].clientY;
    const newDist = Math.sqrt(dx*dx + dy*dy);
    if (touchDist > 0) {
      frustumSize *= touchDist / newDist;
      const maxZoom = Math.max(floorData[LEVELS[0]]?.page?.width||400, floorData[LEVELS[0]]?.page?.height||400) * 1.5;
      frustumSize = Math.max(10, Math.min(maxZoom, frustumSize));
      updateCamera2D(); render();
    }
    touchDist = newDist;
  }
}, {passive: false});

// Layer toggles
document.getElementById('showWalls').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{if(floorGroups[l])floorGroups[l].wallGroup.visible=e.target.checked;}); render();
});
document.getElementById('showDoors').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{
    if(floorGroups[l]) floorGroups[l].doorGroup.visible=e.target.checked;
    if(typeof semanticDoorGroups !== 'undefined' && semanticDoorGroups[l]) semanticDoorGroups[l].visible=e.target.checked;
  }); render();
});
document.getElementById('showRooms').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{if(floorGroups[l])floorGroups[l].roomGroup.visible=e.target.checked;}); vertexGroup.visible=e.target.checked; render();
});
document.getElementById('showLabels').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{if(floorGroups[l])floorGroups[l].labelGroup.visible=e.target.checked;}); render();
});
document.getElementById('showGraph').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{if(floorGroups[l])floorGroups[l].graphGroup.visible=e.target.checked;});
  crossFloorGraphGroup.visible = e.target.checked && is3DView;
  render();
});
document.getElementById('showWalkable').addEventListener('change', (e) => {
  LEVELS.forEach(l=>{if(floorGroups[l] && floorGroups[l].walkableGroup) floorGroups[l].walkableGroup.visible=e.target.checked;});
  render();
});
document.getElementById('showStairs').addEventListener('change', (e) => {
  stairConnectors.visible = e.target.checked && is3DView; render();
});
document.getElementById('showElevators').addEventListener('change', (e) => {
  elevatorConnectors.visible = e.target.checked && is3DView; render();
});
document.getElementById('showEquipment').addEventListener('change', () => filterEquipment());
document.getElementById('showPeopleCount').addEventListener('change', () => filterEquipment());

// Edit buttons
document.getElementById('btnUndo').addEventListener('click', ()=>{doUndo();});
document.getElementById('btnRect').addEventListener('click', () => {
  if(!selectedRoom)return; pushUndo();
  const pts=selectedRoom.polygon,xs=pts.map(p=>p[0]),ys=pts.map(p=>p[1]);
  selectedRoom.polygon=[[Math.min(...xs),Math.min(...ys)],[Math.max(...xs),Math.min(...ys)],[Math.max(...xs),Math.max(...ys)],[Math.min(...xs),Math.max(...ys)]];
  rebuildRoom();showVertexHandles();updateInfo();isDirty=true;render();
});
document.getElementById('btnDelete').addEventListener('click', () => {
  if(!selectedRoom)return; pushUndo();
  const data=floorData[currentLevel]; const fg=floorGroups[currentLevel];
  const idx=data.rooms.findIndex(r=>r.id===selectedRoom.id);
  if(idx>=0)data.rooms.splice(idx,1);
  const mi=fg.meshes.indexOf(selectedMesh);if(mi>=0)fg.meshes.splice(mi,1);
  fg.roomGroup.remove(selectedMesh);selectedMesh.geometry.dispose();selectedMesh.material.dispose();
  fg.roomGroup.children.filter(c=>c.userData?.roomOutline&&c.userData.roomId===selectedRoom.id)
    .forEach(c=>{fg.roomGroup.remove(c);c.geometry.dispose();c.material.dispose();});
  deselectRoom();updateInfo();isDirty=true;render();
});
function cancelMerge() {
  if (mergeSourceMesh) {
    const li = LEVELS.indexOf(currentLevel);
    mergeSourceMesh.material.color.setHex(FLOOR_COLORS[li]);
    mergeSourceMesh.material.opacity = 0.4;
  }
  mergeSource = null; mergeSourceMesh = null;
  document.getElementById('btnMerge').classList.remove('active');
  document.getElementById('btnMerge').textContent = 'Merge Rooms';
}

function executeMerge(targetMesh) {
  const targetRoom = targetMesh.userData;
  if (targetRoom.id === mergeSource.id) { cancelMerge(); return; }
  pushUndo();
  const allPts = [...mergeSource.polygon, ...targetRoom.polygon];
  const xs = allPts.map(p=>p[0]), ys = allPts.map(p=>p[1]);
  targetRoom.polygon = [
    [Math.min(...xs),Math.min(...ys)],[Math.max(...xs),Math.min(...ys)],
    [Math.max(...xs),Math.max(...ys)],[Math.min(...xs),Math.max(...ys)]];
  targetRoom.name = mergeSource.name + '+' + targetRoom.name;
  targetRoom.center = [(Math.min(...xs)+Math.max(...xs))/2,(Math.min(...ys)+Math.max(...ys))/2];
  selectedMesh = targetMesh; selectedRoom = targetRoom;
  rebuildRoom();
  // Delete source
  const data = floorData[currentLevel], fg = floorGroups[currentLevel];
  const idx = data.rooms.findIndex(r=>r.id===mergeSource.id);
  if(idx>=0) data.rooms.splice(idx,1);
  const mi = fg.meshes.indexOf(mergeSourceMesh); if(mi>=0) fg.meshes.splice(mi,1);
  fg.roomGroup.remove(mergeSourceMesh); mergeSourceMesh.geometry.dispose(); mergeSourceMesh.material.dispose();
  fg.roomGroup.children.filter(c=>c.userData?.roomOutline&&c.userData.roomId===mergeSource.id)
    .forEach(c=>{fg.roomGroup.remove(c);c.geometry.dispose();c.material.dispose();});
  cancelMerge();
  selectRoom(targetMesh);
  isDirty = true; render();
}

document.getElementById('btnMerge').addEventListener('click', () => {
  if (mergeSource) { cancelMerge(); render(); return; } // click again to cancel
  if (!selectedRoom) return;
  mergeSource = selectedRoom;
  mergeSourceMesh = selectedMesh;
  selectedMesh.material.color.setHex(0xff6644); selectedMesh.material.opacity = 0.6;
  hideVertexHandles();
  document.getElementById('btnMerge').classList.add('active');
  document.getElementById('btnMerge').textContent = 'Click 2nd...';
  setInfoCard('<b>Merge:</b> Click second room to merge with <b>'+mergeSource.name+'</b>', 'Edit');
});
function splitRoom(direction) {
  if (!selectedRoom) return;
  pushUndo();
  const pts = selectedRoom.polygon;
  const xs = pts.map(p=>p[0]), ys = pts.map(p=>p[1]);
  const minX=Math.min(...xs), maxX=Math.max(...xs), minY=Math.min(...ys), maxY=Math.max(...ys);
  const midX = (minX+maxX)/2, midY = (minY+maxY)/2;

  let poly1, poly2;
  if (direction === 'h') {
    // Split horizontally at midY
    poly1 = [[minX,minY],[maxX,minY],[maxX,midY],[minX,midY]];
    poly2 = [[minX,midY],[maxX,midY],[maxX,maxY],[minX,maxY]];
  } else {
    // Split vertically at midX
    poly1 = [[minX,minY],[midX,minY],[midX,maxY],[minX,maxY]];
    poly2 = [[midX,minY],[maxX,minY],[maxX,maxY],[midX,maxY]];
  }

  // Update current room to first half
  selectedRoom.polygon = poly1;
  selectedRoom.center = [poly1.reduce((s,p)=>s+p[0],0)/4, poly1.reduce((s,p)=>s+p[1],0)/4];
  rebuildRoom();

  // Create second room
  const data = floorData[currentLevel];
  const fg = floorGroups[currentLevel];
  const newId = Math.max(...data.rooms.map(r=>r.id)) + 1;
  const newRoom = {
    id: newId,
    name: selectedRoom.name + '_B',
    area: selectedRoom.area / 2,
    center: [poly2.reduce((s,p)=>s+p[0],0)/4, poly2.reduce((s,p)=>s+p[1],0)/4],
    polygon: poly2,
  };
  selectedRoom.name = selectedRoom.name.replace(/_B$/, '') + '_A';
  data.rooms.push(newRoom);

  // Create mesh for new room
  const pw = data.page.width, ph = data.page.height;
  const levelIdx = LEVELS.indexOf(currentLevel);
  const shape = new THREE.Shape();
  const [sx,sy] = pdfToWorld(poly2[0][0],poly2[0][1],pw,ph); shape.moveTo(sx,sy);
  for (let i=1;i<poly2.length;i++){const[px,py]=pdfToWorld(poly2[i][0],poly2[i][1],pw,ph);shape.lineTo(px,py);}
  shape.closePath();
  const mesh = new THREE.Mesh(new THREE.ShapeGeometry(shape),
    new THREE.MeshBasicMaterial({color:FLOOR_COLORS[levelIdx],transparent:true,opacity:0.4,side:THREE.DoubleSide}));
  mesh.userData = newRoom; mesh.position.z = 1;
  fg.roomGroup.add(mesh); fg.meshes.push(mesh);
  // Outline
  const op = poly2.map(p=>{const[ox,oy]=pdfToWorld(p[0],p[1],pw,ph);return new THREE.Vector3(ox,oy,2);});
  op.push(op[0].clone());
  const outline = new THREE.Line(new THREE.BufferGeometry().setFromPoints(op),
    new THREE.LineBasicMaterial({color:0x4a6fa5,transparent:true,opacity:0.6}));
  outline.userData = {roomOutline:true, roomId:newId};
  fg.roomGroup.add(outline);

  showVertexHandles(); updateInfo(); isDirty=true; render();
}

document.getElementById('btnSplitH').addEventListener('click', () => splitRoom('h'));
document.getElementById('btnSplitV').addEventListener('click', () => splitRoom('v'));

function findEdgeIntersection(px, py, polygon) {
  // Find which edge of the polygon is closest to point (px,py) and return the intersection info
  let bestDist = Infinity, bestEdge = -1, bestT = 0;
  for (let i = 0; i < polygon.length; i++) {
    const j = (i+1) % polygon.length;
    const ax=polygon[i][0], ay=polygon[i][1], bx=polygon[j][0], by=polygon[j][1];
    const dx=bx-ax, dy=by-ay, len2=dx*dx+dy*dy;
    if (len2 < 0.001) continue;
    let t = ((px-ax)*dx + (py-ay)*dy) / len2;
    t = Math.max(0, Math.min(1, t));
    const cx = ax+t*dx, cy = ay+t*dy;
    const dist = Math.hypot(px-cx, py-cy);
    if (dist < bestDist) { bestDist = dist; bestEdge = i; bestT = t; }
  }
  return { edge: bestEdge, t: bestT, dist: bestDist };
}

function doFreeformSplit(p1pdf, p2pdf) {
  if (!selectedRoom) return;
  const poly = selectedRoom.polygon;

  // Find which edges p1 and p2 are on
  const hit1 = findEdgeIntersection(p1pdf[0], p1pdf[1], poly);
  const hit2 = findEdgeIntersection(p2pdf[0], p2pdf[1], poly);

  if (hit1.edge === hit2.edge) return; // both on same edge, can't split

  // Insert split points on their edges and create two polygons
  const e1 = hit1.edge, t1 = hit1.t;
  const e2 = hit2.edge, t2 = hit2.t;
  const sp1 = [
    Math.round((poly[e1][0] + t1*(poly[(e1+1)%poly.length][0]-poly[e1][0]))*100)/100,
    Math.round((poly[e1][1] + t1*(poly[(e1+1)%poly.length][1]-poly[e1][1]))*100)/100,
  ];
  const sp2 = [
    Math.round((poly[e2][0] + t2*(poly[(e2+1)%poly.length][0]-poly[e2][0]))*100)/100,
    Math.round((poly[e2][1] + t2*(poly[(e2+1)%poly.length][1]-poly[e2][1]))*100)/100,
  ];

  // Build two polygons by walking around the boundary
  // Poly A: sp1 -> edges e1+1..e2 -> sp2 -> back to sp1
  // Poly B: sp2 -> edges e2+1..e1 -> sp1 -> back to sp2
  const n = poly.length;
  const polyA = [sp1];
  for (let k = (e1+1)%n; k !== (e2+1)%n; k = (k+1)%n) {
    polyA.push([...poly[k]]);
  }
  polyA.push(sp2);

  const polyB = [sp2];
  for (let k = (e2+1)%n; k !== (e1+1)%n; k = (k+1)%n) {
    polyB.push([...poly[k]]);
  }
  polyB.push(sp1);

  if (polyA.length < 3 || polyB.length < 3) return;

  pushUndo();

  // Update selected room to polyA
  selectedRoom.polygon = polyA;
  selectedRoom.center = [polyA.reduce((s,p)=>s+p[0],0)/polyA.length, polyA.reduce((s,p)=>s+p[1],0)/polyA.length];
  selectedRoom.name = selectedRoom.name.replace(/_[AB]$/, '') + '_A';
  rebuildRoom();

  // Create new room for polyB
  const data = floorData[currentLevel];
  const fg = floorGroups[currentLevel];
  const newId = Math.max(...data.rooms.map(r=>r.id)) + 1;
  const newRoom = {
    id: newId,
    name: selectedRoom.name.replace(/_A$/, '_B'),
    area: Math.round(selectedRoom.area / 2),
    center: [polyB.reduce((s,p)=>s+p[0],0)/polyB.length, polyB.reduce((s,p)=>s+p[1],0)/polyB.length],
    polygon: polyB,
  };
  data.rooms.push(newRoom);

  const pw = data.page.width, ph = data.page.height;
  const levelIdx = LEVELS.indexOf(currentLevel);
  const shape = new THREE.Shape();
  const [sx,sy] = pdfToWorld(polyB[0][0],polyB[0][1],pw,ph); shape.moveTo(sx,sy);
  for (let i=1;i<polyB.length;i++){const[px,py]=pdfToWorld(polyB[i][0],polyB[i][1],pw,ph);shape.lineTo(px,py);}
  shape.closePath();
  const mesh = new THREE.Mesh(new THREE.ShapeGeometry(shape),
    new THREE.MeshBasicMaterial({color:FLOOR_COLORS[levelIdx],transparent:true,opacity:0.4,side:THREE.DoubleSide}));
  mesh.userData = newRoom; mesh.position.z = 1;
  fg.roomGroup.add(mesh); fg.meshes.push(mesh);
  const op = polyB.map(p=>{const[ox,oy]=pdfToWorld(p[0],p[1],pw,ph);return new THREE.Vector3(ox,oy,2);});
  op.push(op[0].clone());
  const outline = new THREE.Line(new THREE.BufferGeometry().setFromPoints(op),
    new THREE.LineBasicMaterial({color:0x4a6fa5,transparent:true,opacity:0.6}));
  outline.userData = {roomOutline:true, roomId:newId};
  fg.roomGroup.add(outline);

  showVertexHandles(); updateInfo(); isDirty=true; render();
}

document.getElementById('btnSplitFree').addEventListener('click', () => {
  if (!selectedRoom) return;
  if (splitFreeMode) {
    // Cancel
    splitFreeMode = false; splitPoint1 = null;
    if (splitLine) { scene.remove(splitLine); splitLine.geometry.dispose(); splitLine.material.dispose(); splitLine = null; }
    document.getElementById('btnSplitFree').classList.remove('active');
    document.getElementById('btnSplitFree').textContent = 'Split Free';
    updateInfo();
  } else {
    splitFreeMode = true; splitPoint1 = null;
    document.getElementById('btnSplitFree').classList.add('active');
    document.getElementById('btnSplitFree').textContent = 'Click 1st point...';
    setInfoCard('<b>Split:</b> Click first point on room edge', 'Edit');
  }
  render();
});
document.getElementById('btnNewRoom').addEventListener('click', () => {
  if (newRoomMode) {
    // Cancel
    newRoomMode = false; newRoomCorner1 = null;
    if (newRoomPreview) { scene.remove(newRoomPreview); newRoomPreview.geometry.dispose(); newRoomPreview.material.dispose(); newRoomPreview = null; }
    document.getElementById('btnNewRoom').classList.remove('active');
    document.getElementById('btnNewRoom').textContent = 'New Room';
    updateInfo();
  } else {
    newRoomMode = true; newRoomCorner1 = null;
    deselectRoom(); cancelMerge();
    editMode = 'select';
    document.getElementById('btnEditRoom').classList.remove('active');
    document.getElementById('btnEraseWalls').classList.remove('active');
    document.getElementById('btnNewRoom').classList.add('active');
    document.getElementById('btnNewRoom').textContent = 'Click 1st corner...';
    setInfoCard('<b>New Room:</b> Click first corner', 'Edit');
    canvas.style.cursor = 'crosshair';
  }
  render();
});
document.getElementById('btnWeldVerts').addEventListener('click', () => {
  if (!selectedRoom) return;
  if (weldMode) {
    weldMode = false; weldFirstIdx = null;
    document.getElementById('btnWeldVerts').classList.remove('active');
    document.getElementById('btnWeldVerts').textContent = 'Weld Vertices';
    updateInfo();
  } else {
    weldMode = true; weldFirstIdx = null;
    document.getElementById('btnWeldVerts').classList.add('active');
    document.getElementById('btnWeldVerts').textContent = 'Click 1st vertex...';
    setInfoCard('<b>Weld:</b> Click first vertex (orange)', 'Edit');
  }
});

function nudgeRoom(dir, shrink) {
  if (!selectedRoom) return;
  pushUndo();
  const step = shrink ? -0.2 : 0.2; // 0.2 PDF units per click (~10cm)
  const poly = selectedRoom.polygon;
  const xs = poly.map(p=>p[0]), ys = poly.map(p=>p[1]);
  const minX=Math.min(...xs), maxX=Math.max(...xs), minY=Math.min(...ys), maxY=Math.max(...ys);

  // Move vertices on the target edge
  // Note: PDF Y is inverted (top=small, bottom=large)
  for (let i = 0; i < poly.length; i++) {
    switch(dir) {
      case 'up':    if (Math.abs(poly[i][1]-minY)<0.5) poly[i][1] -= step; break;
      case 'down':  if (Math.abs(poly[i][1]-maxY)<0.5) poly[i][1] += step; break;
      case 'left':  if (Math.abs(poly[i][0]-minX)<0.5) poly[i][0] -= step; break;
      case 'right': if (Math.abs(poly[i][0]-maxX)<0.5) poly[i][0] += step; break;
    }
  }
  rebuildRoom(); showVertexHandles(); isDirty=true; render();
}

document.getElementById('btnNudgeU').addEventListener('click', (e) => nudgeRoom('up', e.shiftKey));
document.getElementById('btnNudgeD').addEventListener('click', (e) => nudgeRoom('down', e.shiftKey));
document.getElementById('btnNudgeL').addEventListener('click', (e) => nudgeRoom('left', e.shiftKey));
document.getElementById('btnNudgeR').addEventListener('click', (e) => nudgeRoom('right', e.shiftKey));

document.getElementById('btnSnapWalls').addEventListener('click', () => {
  if (!selectedRoom) return;
  pushUndo();
  const data = floorData[currentLevel];
  const walls = data.walls;
  const MAX_SNAP = 8.0;
  const SNAP_OFFSET = 0.3;
  const poly = selectedRoom.polygon;

  if (poly.length !== 4) {
    // Non-rectangle: skip for now
    document.getElementById('save-status').textContent = 'Snap works on rectangles only';
    setTimeout(()=>{document.getElementById('save-status').textContent='';},2000);
    return;
  }

  // Get bounding box of current room
  const xs = poly.map(p=>p[0]), ys = poly.map(p=>p[1]);
  let top = Math.min(...ys), bottom = Math.max(...ys);
  let left = Math.min(...xs), right = Math.max(...xs);
  const cx = (left+right)/2, cy = (top+bottom)/2;

  // For each edge, find the nearest parallel wall
  // Top edge (horizontal, y=top): find nearest horizontal wall above center
  // Bottom edge: nearest horizontal wall below center
  // Left edge (vertical, x=left): nearest vertical wall left of center
  // Right edge: nearest vertical wall right of center

  function findNearestWall(edgeVal, isHorizontal, searchDir) {
    // searchDir: -1 = look for wall with smaller value (above/left), +1 = larger (below/right)
    let best = null, bestDist = MAX_SNAP;
    for (const seg of walls) {
      const sx1=seg[0][0], sy1=seg[0][1], sx2=seg[1][0], sy2=seg[1][1];
      const dx=sx2-sx1, dy=sy2-sy1;
      const len = Math.hypot(dx, dy);
      if (len < 2) continue; // skip tiny segments

      if (isHorizontal) {
        // Looking for horizontal walls (small dy relative to dx)
        if (Math.abs(dy) > Math.abs(dx) * 0.3) continue; // not horizontal enough
        const wallY = (sy1+sy2)/2;
        // Must overlap with room in X range
        const wMinX = Math.min(sx1,sx2), wMaxX = Math.max(sx1,sx2);
        if (wMaxX < left+2 || wMinX > right-2) continue; // no X overlap
        // Must be in correct direction from edge
        const dist = (wallY - edgeVal) * searchDir;
        if (dist >= 0 && dist < bestDist) {
          bestDist = dist;
          best = wallY;
        }
      } else {
        // Looking for vertical walls
        if (Math.abs(dx) > Math.abs(dy) * 0.3) continue;
        const wallX = (sx1+sx2)/2;
        const wMinY = Math.min(sy1,sy2), wMaxY = Math.max(sy1,sy2);
        if (wMaxY < top+2 || wMinY > bottom-2) continue;
        const dist = (wallX - edgeVal) * searchDir;
        if (dist >= 0 && dist < bestDist) {
          bestDist = dist;
          best = wallX;
        }
      }
    }
    return best;
  }

  // Snap each edge
  const newTop = findNearestWall(top, true, -1);     // wall above top edge
  const newBottom = findNearestWall(bottom, true, 1); // wall below bottom edge
  const newLeft = findNearestWall(left, false, -1);   // wall left of left edge
  const newRight = findNearestWall(right, false, 1);  // wall right of right edge

  // Also check walls between edge and center (closer walls)
  const newTopIn = findNearestWall(cy, true, -1);     // nearest horiz wall above center
  const newBottomIn = findNearestWall(cy, true, 1);
  const newLeftIn = findNearestWall(cx, false, -1);
  const newRightIn = findNearestWall(cx, false, 1);

  // Use the wall closest to the current edge position
  if (newTop !== null) top = newTop + SNAP_OFFSET;
  else if (newTopIn !== null) top = newTopIn + SNAP_OFFSET;
  if (newBottom !== null) bottom = newBottom - SNAP_OFFSET;
  else if (newBottomIn !== null) bottom = newBottomIn - SNAP_OFFSET;
  if (newLeft !== null) left = newLeft + SNAP_OFFSET;
  else if (newLeftIn !== null) left = newLeftIn + SNAP_OFFSET;
  if (newRight !== null) right = newRight - SNAP_OFFSET;
  else if (newRightIn !== null) right = newRightIn - SNAP_OFFSET;

  // Rebuild rectangle
  selectedRoom.polygon = [
    [Math.round(left*100)/100, Math.round(top*100)/100],
    [Math.round(right*100)/100, Math.round(top*100)/100],
    [Math.round(right*100)/100, Math.round(bottom*100)/100],
    [Math.round(left*100)/100, Math.round(bottom*100)/100],
  ];
  selectedRoom.center = [Math.round((left+right)/2*100)/100, Math.round((top+bottom)/2*100)/100];

  rebuildRoom(); showVertexHandles(); updateInfo(); isDirty = true; render();
});
document.getElementById('btnToggleCorridor').addEventListener('click', () => {
  if (!selectedRoom) return;
  pushUndo();
  selectedRoom.type = selectedRoom.type === 'corridor' ? 'room' : 'corridor';
  // Change color to indicate type
  const levelIdx = LEVELS.indexOf(currentLevel);
  selectedMesh.material.color.setHex(selectedRoom.type === 'corridor' ? 0x5c5c2a : FLOOR_COLORS[levelIdx]);
  updateInfo(); isDirty = true; render();
});
document.getElementById('btnRename').addEventListener('click', () => {
  if (!selectedRoom) return;
  const newName = prompt('Room name:', selectedRoom.name || '');
  if (newName !== null) {
    pushUndo();
    selectedRoom.name = newName;
    updateInfo(); isDirty = true;
  }
});
document.getElementById('btnEditRoom').addEventListener('click', () => {
  if(editMode==='editRoom'){editMode='select';document.getElementById('btnEditRoom').classList.remove('active');deselectRoom();updateInfo();}
  else{editMode='editRoom';document.getElementById('btnEditRoom').classList.add('active');document.getElementById('btnEraseWalls').classList.remove('active');highlightWall(-1);}
  render();
});
document.getElementById('btnDrawWall').addEventListener('click', () => {
  if (drawWallMode) {
    // Finish drawing
    drawWallMode = false;
    drawWallPoints = [];
    if (drawWallPreview) { scene.remove(drawWallPreview); drawWallPreview.geometry.dispose(); drawWallPreview.material.dispose(); drawWallPreview = null; }
    document.getElementById('btnDrawWall').classList.remove('active');
    document.getElementById('btnDrawWall').textContent = 'Draw Wall';
    canvas.style.cursor = 'default';
    hideRoomCard();
  } else {
    drawWallMode = true;
    drawWallPoints = [];
    editMode = 'select';
    deselectRoom();
    document.getElementById('btnEditRoom').classList.remove('active');
    document.getElementById('btnEraseWalls').classList.remove('active');
    document.getElementById('btnDrawWall').classList.add('active');
    document.getElementById('btnDrawWall').textContent = 'Stop Drawing';
    canvas.style.cursor = 'crosshair';
    setInfoCard('<b>Draw Wall:</b> Click points to trace walls. Auto-snaps to H/V. Click button to stop.', 'Edit');
  }
  render();
});
document.getElementById('btnEraseWalls').addEventListener('click', () => {
  if(editMode==='eraseWalls'){editMode='select';document.getElementById('btnEraseWalls').classList.remove('active');highlightWall(-1);canvas.style.cursor='default';}
  else{editMode='eraseWalls';document.getElementById('btnEraseWalls').classList.add('active');document.getElementById('btnEditRoom').classList.remove('active');if(selectedRoom){deselectRoom();updateInfo();}canvas.style.cursor='crosshair';}
  render();
});
document.getElementById('btnSave').addEventListener('click', async () => {
  const st=document.getElementById('save-status');st.textContent='Saving...';
  try{
    // Re-index room IDs and update outline references before saving
    const data = floorData[currentLevel];
    data.rooms.sort((a,b) => (a.name||'').localeCompare(b.name||''));
    const fg = floorGroups[currentLevel];
    data.rooms.forEach((r,i) => {
      const oldId = r.id;
      r.id = i;
      // Update outline references
      if (fg) fg.roomGroup.children.forEach(c => {
        if (c.userData?.roomOutline && c.userData.roomId === oldId) c.userData.roomId = i;
      });
    });
    // Ensure all rooms have type field
    data.rooms.forEach(r => { if (!r.type) r.type = 'room'; });
    console.log('Saving', data.rooms.length, 'rooms to', currentLevel);
    st.textContent = 'Saving ' + data.rooms.length + ' rooms...';
    const body=JSON.stringify(data);
    console.log('Body size:', body.length, 'bytes');
    const levelKey = currentLevel.split('/').pop();
    const r=await fetch(`/api/editor/floors/${encodeURIComponent(levelKey)}`,{method:'POST',headers:{'Content-Type':'application/json'},body});
    if(r.ok){st.textContent='Saved '+currentLevel+'!';st.style.color='#5cb85c';isDirty=false;}
    else{st.textContent='Failed!';st.style.color='#e04040';}
  }catch(e){st.textContent='Error: '+e.message;st.style.color='#e04040';}
  setTimeout(()=>{st.textContent='';},3000);
});


window.addEventListener('keydown', (e) => {if((e.ctrlKey||e.metaKey)&&e.key==='z'){e.preventDefault();doUndo();}});
window.addEventListener('beforeunload', (e) => {if(isDirty){e.preventDefault();e.returnValue='';}});
