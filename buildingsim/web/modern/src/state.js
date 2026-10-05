const canvas = document.getElementById('canvas');
const viewerEl = document.getElementById('viewer-container');
function viewerW() { return viewerEl.clientWidth; }
function viewerH() { return viewerEl.clientHeight; }
const renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
renderer.setSize(viewerW(), viewerH());
renderer.setPixelRatio(window.devicePixelRatio);
renderer.setClearColor(0x1a1a2e);

const scene = new THREE.Scene();

// Cameras
const aspect = viewerW() / viewerH();
let frustumSize = 500;
const camera2D = new THREE.OrthographicCamera(
  -frustumSize*aspect/2, frustumSize*aspect/2, frustumSize/2, -frustumSize/2, 0.1, 1000);
camera2D.position.set(0, 0, 100);
camera2D.lookAt(0, 0, 0);

const camera3D = new THREE.PerspectiveCamera(50, aspect, 1, 2000);
camera3D.position.set(300, -300, 400);
camera3D.up.set(0, 0, 1); // Z is up (like a real building)
camera3D.lookAt(0, 0, 0);

let activeCamera = camera3D;
let is3DView = true;
let orbitControls = new OrbitControls(camera3D, canvas);
// The viewpoint must never dive below the terrain: clamp the orbit camera
// (and its target) a little above the local ground on every control change.
orbitControls.addEventListener('change', () => {
  if (typeof groundZ === 'function') {
    const minC = groundZ(camera3D.position.x, camera3D.position.y) + 2.5;
    if (camera3D.position.z < minC) camera3D.position.z = minC;
    const minT = groundZ(orbitControls.target.x, orbitControls.target.y) + 0.5;
    if (orbitControls.target.z < minT) orbitControls.target.z = minT;
  }
});
orbitControls.enabled = true;
orbitControls.maxPolarAngle = Math.PI / 2; // can't go below ground
orbitControls.minPolarAngle = 0.1;         // can't go directly overhead
orbitControls.enableDamping = true;
orbitControls.dampingFactor = 0.1;
orbitControls.zoomSpeed = 8.0;

const raycaster = new THREE.Raycaster();
const mouse = new THREE.Vector2();

// Floor data
// Building config from URL: ?building=abuilding
const BUILDINGS = {
  'abuilding': { name: 'A-Building (LTU)', levels: ['level0','level1','level2'], labels: ['Floor 0','Floor 1','Floor 2'] },
};
const urlParams = new URLSearchParams(window.location.search);
const requestedBuilding = urlParams.get('building') || 'abuilding';
const currentBuilding = BUILDINGS[requestedBuilding] ? requestedBuilding : 'abuilding';
const BLDG = BUILDINGS[currentBuilding];
const LEVELS = BLDG.levels.map(l => currentBuilding + '/' + l);
const FLOOR_COLORS = [0x2e4468, 0x3a4272, 0x2d5a66];
// Circulation reads warm against the cool rooms.
const CORRIDOR_COLOR = 0x7a6636;
const FLOOR_SPACING = 25; // vertical spacing in 3D view
// Floor alignment: coordinates already aligned in the data (top-left corner anchoring)
const FLOOR_ALIGN = {
  0: { x: 0, y: 0 },
  1: { x: 0, y: 0 },
  2: { x: 0, y: 0 },
};

// Stairs and elevators (to be populated for plant)
// Vertical connections (stairs and elevators)
const STAIRS = [
  { name: 'TRAPPA A', positions: [{l:0,x:306.5,y:32.2},{l:1,x:306.3,y:44.0},{l:2,x:306.4,y:44.0}] },
  { name: 'TRAPPA B', positions: [{l:0,x:227.8,y:19.9},{l:1,x:228.2,y:18.5},{l:2,x:228.5,y:18.5}] },
  { name: 'TRAPPA C', positions: [{l:0,x:127.1,y:20.1},{l:1,x:126.4,y:20.3},{l:2,x:126.5,y:20.3}] },
  { name: 'TRAPPA D', positions: [{l:1,x:16.8,y:27.4},{l:2,x:17.3,y:27.6}] },
  { name: 'TRAPPA E', positions: [{l:0,x:7.2,y:127.0},{l:1,x:9.9,y:126.9},{l:2,x:10.7,y:128.2}] },
  { name: 'TRAPPA F', positions: [{l:0,x:111.0,y:126.8},{l:1,x:112.8,y:128.2},{l:2,x:113.1,y:128.2}] },
  { name: 'TRAPPA G', positions: [{l:0,x:212.7,y:127.2},{l:1,x:214.7,y:128.0},{l:2,x:214.9,y:128.0}] },
  { name: 'TRAPPA H', positions: [{l:0,x:314.9,y:126.8},{l:1,x:316.0,y:126.6},{l:2,x:314.2,y:126.0}] },
  { name: 'TRAPPA I', positions: [{l:0,x:371.4,y:119.6},{l:1,x:370.6,y:118.2}] },
  { name: 'A119/A219', positions: [{l:0,x:328.6,y:330.7},{l:1,x:319.9,y:322.2}] },
  { name: 'A183/A283', positions: [{l:0,x:127.0,y:323.9},{l:1,x:126.4,y:324.4}] },
  { name: 'A303/A403/A503', positions: [{l:0,x:192.8,y:282.4},{l:1,x:192.6,y:281.4},{l:2,x:192.2,y:281.5}] },
  { name: '1575/2604', positions: [{l:0,x:102.4,y:330.5},{l:1,x:101.8,y:329.7}] },
  { name: '1568/2580/3454', positions: [{l:0,x:11.3,y:333.0},{l:1,x:11.0,y:332.8},{l:2,x:11.1,y:332.9}] },
  { name: '1541/2541/3421', positions: [{l:0,x:33.3,y:179.8},{l:1,x:30.6,y:178.1},{l:2,x:32.7,y:178.3}] },
  { name: 'A104/A204', positions: [{l:0,x:325.6,y:226.2},{l:1,x:320.2,y:222.9}] },
];
const ELEVATORS = [
  { name: 'HISS 12', positions: [{l:0,x:133.4,y:17.4},{l:1,x:133.0,y:16.4},{l:2,x:133.1,y:16.6}] },
  { name: 'HISS 13', positions: [{l:0,x:318.0,y:38.1},{l:1,x:317.7,y:37.2},{l:2,x:317.8,y:37.2}] },
  { name: 'HISS 14', positions: [{l:0,x:126.1,y:160.1},{l:1,x:125.8,y:159.2},{l:2,x:125.9,y:159.3}] },
];
let floorData = {};   // level -> json data
let floorGroups = {};  // level -> { wallGroup, doorGroup, roomGroup, meshes[] }
let currentLevel = 'level0';

let selectedRoom = null, selectedMesh = null;
let vertexHandles = [], vertexGroup = new THREE.Group();
scene.add(vertexGroup);
let dragVertex = null, editMode = 'select', isDirty = false;
let eraseHoverIdx = -1, eraseHighlight = null;
let mergeSource = null, mergeSourceMesh = null;
let splitFreeMode = false, splitPoint1 = null, splitLine = null;
let weldMode = false, weldFirstIdx = null;
let newRoomMode = false, newRoomCorner1 = null, newRoomPreview = null;
let drawWallMode = false, drawWallPoints = [], drawWallPreview = null;
let undoStack = [];
const MAX_UNDO = 50;

let panOffset = new THREE.Vector2(0, 0);
let isPanning = false, panStart = new THREE.Vector2();

function pdfToWorld(x, y, pw, ph) { return [x - pw/2, -(y - ph/2)]; }
function worldToPdf(wx, wy, pw, ph) { return [wx + pw/2, -(wy - ph/2)]; }
