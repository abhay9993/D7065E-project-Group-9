function createLineSegments(lines, color, group, opacity, pw, ph) {
  if (!lines || lines.length === 0) return;
  const positions = new Float32Array(lines.length * 6);
  let idx = 0;
  for (const seg of lines) {
    const [x1,y1] = pdfToWorld(seg[0][0], seg[0][1], pw, ph);
    const [x2,y2] = pdfToWorld(seg[1][0], seg[1][1], pw, ph);
    positions[idx++]=x1; positions[idx++]=y1; positions[idx++]=0;
    positions[idx++]=x2; positions[idx++]=y2; positions[idx++]=0;
  }
  const geom = new THREE.BufferGeometry();
  geom.setAttribute('position', new THREE.BufferAttribute(positions.slice(0,idx), 3));
  group.add(new THREE.LineSegments(geom, new THREE.LineBasicMaterial({ color, transparent: true, opacity })));
}

function createRoomPolygons(rooms, group, meshes, color, pw, ph) {
  for (const room of rooms) {
    const pts = room.polygon;
    if (pts.length < 3) continue;
    const shape = new THREE.Shape();
    const [sx,sy] = pdfToWorld(pts[0][0], pts[0][1], pw, ph);
    shape.moveTo(sx, sy);
    for (let i = 1; i < pts.length; i++) {
      const [px,py] = pdfToWorld(pts[i][0], pts[i][1], pw, ph);
      shape.lineTo(px, py);
    }
    shape.closePath();
    const roomColor = room.type === 'corridor' ? CORRIDOR_COLOR : color;
    // Unlit on purpose: a requested highlight colour must
    // get that colour, not that colour under a sun.
    const mesh = new THREE.Mesh(new THREE.ShapeGeometry(shape),
      noTone(new THREE.MeshBasicMaterial({ color: roomColor, transparent: true, opacity: 0.4, side: THREE.DoubleSide })));
    mesh.userData = room;
    mesh.position.z = 1;
    group.add(mesh);
    meshes.push(mesh);
    // Outline
    const op = pts.map(p => { const [ox,oy] = pdfToWorld(p[0],p[1],pw,ph); return new THREE.Vector3(ox,oy,2); });
    op.push(op[0].clone());
    const outline = new THREE.Line(new THREE.BufferGeometry().setFromPoints(op),
      noTone(new THREE.LineBasicMaterial({ color: 0x6f93c8, transparent: true, opacity: 0.55 })));
    outline.userData = { roomOutline: true, roomId: room.id };
    group.add(outline);
  }
}

function createLabels(labels, group, pw, ph) {
  if (!labels) return;
  for (const label of labels) {
    const [wx,wy] = pdfToWorld(label.x, label.y, pw, ph);
    group.add(makeLabelSprite(label.text, wx, wy, WALL_H + 1.2));
  }
}

async function loadFloor(level) {
  const levelID = level.split('/').pop();
  const resp = await fetch(`/api/building/floors/${levelID}?t=${Date.now()}`);
  if (!resp.ok) {
    throw new Error(`Could not load ${levelID}: HTTP ${resp.status}`);
  }
  const data = await resp.json();
  floorData[level] = data;

  const pw = data.page.width, ph = data.page.height;
  const container = new THREE.Group();
  const wallGroup = new THREE.Group();
  const doorGroup = new THREE.Group();
  const roomGroup = new THREE.Group();
  const meshes = [];

  const levelIdx = LEVELS.indexOf(level);

  // Background image (raster floor plan)
  if (data.background) {
    const loader = new THREE.TextureLoader();
    loader.load(data.background, (texture) => {
      texture.minFilter = THREE.LinearFilter;
      const bgGeom = new THREE.PlaneGeometry(pw, ph);
      const bgMat = new THREE.MeshBasicMaterial({ map: texture, side: THREE.DoubleSide, transparent: true, opacity: 0.3 });
      const bgMesh = new THREE.Mesh(bgGeom, bgMat);
      bgMesh.position.set(0, 0, -0.5);
      container.add(bgMesh);
      render();
    });
  }

  // Walls in the flat plan carry the same blue-grey as the extruded partitions
  // in the 3D interior, a shade under full brightness so the linework reads as
  // built fabric rather than as highlight.
  createLineSegments(data.walls || [], 0x8d97a8, wallGroup, 0.62, pw, ph);
  createLineSegments(data.red_lines || [], 0xe0704a, doorGroup, 0.75, pw, ph);
  createRoomPolygons(data.rooms, roomGroup, meshes, FLOOR_COLORS[levelIdx] || 0x2a3a5c, pw, ph);
  const labelGroup = new THREE.Group();
  labelGroup.visible = false;
  createLabels(data.labels || [], labelGroup, pw, ph);

  // Build graph layer
  const graphGroup = new THREE.Group();
  graphGroup.visible = false;
  if (data.graph) {
    const g = data.graph;
    // Edges
    for (const edge of g.edges) {
      const nA = g.nodes.find(n => n.id === edge.from);
      const nB = g.nodes.find(n => n.id === edge.to);
      if (!nA || !nB) continue;
      const [x1,y1] = pdfToWorld(nA.x, nA.y, pw, ph);
      const [x2,y2] = pdfToWorld(nB.x, nB.y, pw, ph);
      const geom = new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(x1,y1,4), new THREE.Vector3(x2,y2,4)]);
      graphGroup.add(new THREE.Line(geom, new THREE.LineBasicMaterial({color:0xff66ff, transparent:true, opacity:0.4})));
    }
    // Nodes
    for (const node of g.nodes) {
      const [wx,wy] = pdfToWorld(node.x, node.y, pw, ph);
      const dot = new THREE.Mesh(new THREE.CircleGeometry(1, 8),
        new THREE.MeshBasicMaterial({color:0xff66ff, side:THREE.DoubleSide}));
      dot.position.set(wx, wy, 4.5);
      graphGroup.add(dot);
    }
  }

  // Build walkable graph layer
  const walkableGroup = new THREE.Group();
  walkableGroup.visible = false;
  if (data.walkable_graph) {
    const wg = data.walkable_graph;
    const nodeMap = {};
    for (const node of wg.nodes) nodeMap[node.id] = node;
    // Edges
    for (const edge of wg.edges) {
      const [x1,y1] = edge.x1 !== undefined
        ? pdfToWorld(edge.x1, edge.y1, pw, ph)
        : pdfToWorld(nodeMap[edge.from]?.x||0, nodeMap[edge.from]?.y||0, pw, ph);
      const [x2,y2] = edge.x2 !== undefined
        ? pdfToWorld(edge.x2, edge.y2, pw, ph)
        : pdfToWorld(nodeMap[edge.to]?.x||0, nodeMap[edge.to]?.y||0, pw, ph);
      const geom = new THREE.BufferGeometry().setFromPoints([
        new THREE.Vector3(x1,y1,4), new THREE.Vector3(x2,y2,4)]);
      walkableGroup.add(new THREE.Line(geom, new THREE.LineBasicMaterial({color:0x00ffaa, transparent:true, opacity:0.5})));
    }
    // Nodes
    for (const node of wg.nodes) {
      const [wx,wy] = pdfToWorld(node.x, node.y, pw, ph);
      const color = node.type === 'corridor' ? 0x00ffaa : node.type === 'entry' ? 0xffaa00 : 0x4488ff;
      const size = node.type === 'corridor' ? 0.8 : 0.6;
      const dot = new THREE.Mesh(new THREE.CircleGeometry(size, 8),
        new THREE.MeshBasicMaterial({color, side:THREE.DoubleSide}));
      dot.position.set(wx, wy, 4.5);
      walkableGroup.add(dot);
    }
  }

  container.add(wallGroup, doorGroup, roomGroup, labelGroup, graphGroup, walkableGroup);
  scene.add(container);

  floorGroups[level] = { container, wallGroup, doorGroup, roomGroup, labelGroup, graphGroup, walkableGroup, meshes };
  return data;
}

let crossFloorEdges = [];
let equipment = [];
let equipmentGroups = {}; // level -> THREE.Group
let pathGroup = new THREE.Group();
scene.add(pathGroup);
let occupancyGroup = new THREE.Group();
scene.add(occupancyGroup);
let coverageGroup = new THREE.Group();
scene.add(coverageGroup);
