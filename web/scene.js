// The 3D board: an island in the sea, the board on it, the tiles drawn from the server's board,
// the pawns, the dice and the buildings. Everything is made here from simple shapes, in toon
// shading with ink outlines. The scene shows a state (sync) and plays what happened (rollDice,
// movePawn…): main.js decides when.
import * as THREE from './vendor/three.module.min.js';

export const GROUP_COLORS = ['', '#a0603a', '#2ec4f1', '#ff5fa2', '#ff8a1f', '#e63946', '#3a5bd9'];
const INK = 0x1d2a3a;
const PAL = {
  cream: 0xfff8e8, board: 0xf6ecd2, wood: 0xb07a4f, wood2: 0x8a5a3c, grass: 0x7ccf6a, grass2: 0x5bb85a,
  leaf: 0x3fae5a, leaf2: 0x2f9a55, water: 0x47c3ee, sea: 0x2f9fd6, sand: 0xf3dfaa, sky: 0x7fd0f2, horizon: 0xfff2da,
  stone: 0xc9d1da, steel: 0x58657a, gold: 0xffc93c, white: 0xffffff, red: 0xe63946,
};

// --- materials -------------------------------------------------------------------------------

const ramp = values => {
  const t = new THREE.DataTexture(new Uint8Array(values), values.length, 1, THREE.RedFormat);
  t.minFilter = t.magFilter = THREE.NearestFilter;
  t.needsUpdate = true;
  return t;
};
const TONES = ramp([150, 215, 255]), SOFT = ramp([215, 240, 255]);
const toon = (color, more = {}) => new THREE.MeshToonMaterial({ color, gradientMap: TONES, ...more });
const soft = (color, more = {}) => new THREE.MeshToonMaterial({ color, gradientMap: SOFT, ...more });
const HULL = new THREE.MeshBasicMaterial({ color: INK, side: THREE.BackSide });

// outline gives a mesh an ink outline: its back faces, a little bigger, in ink
function outline(mesh, by = 1.07) {
  const hull = new THREE.Mesh(mesh.geometry, HULL);
  hull.scale.setScalar(by);
  hull.raycast = () => { };
  mesh.add(hull);
  return mesh;
}

function mesh(geometry, material, { shadow = true, receive = false } = {}) {
  const m = new THREE.Mesh(geometry, material);
  m.castShadow = shadow;
  m.receiveShadow = receive;
  return m;
}

// --- geometry of the board -------------------------------------------------------------------

const CORNER = 1.6, TILE_W = 1, TILE_D = 1.6;
const HALF = CORNER + 3.5 * TILE_W; // half the side of the board
const TOP = 0.16; // height of a tile's face above the board

// frame places square i: the frame of side 0 (the bottom edge, from right to left, outward +z)
// turned to its side. Corners are the first square of each side.
function frame(i) {
  const side = Math.floor(i / 8), k = i % 8;
  const rot = [0, -Math.PI / 2, Math.PI, Math.PI / 2][side];
  const x = k === 0 ? HALF - CORNER / 2 : HALF - CORNER - (k - 0.5) * TILE_W;
  const z = k === 0 ? HALF - CORNER / 2 : HALF - TILE_D / 2;
  const c = Math.cos(rot), s = Math.sin(rot);
  return { x: x * c + z * s, z: -x * s + z * c, rot, corner: k === 0 };
}

// --- textures --------------------------------------------------------------------------------

const DISPLAY = "'DynaPuff', 'Nunito', system-ui, sans-serif";
const BODY = "'Nunito', system-ui, sans-serif";
const EMOJI = "'Noto Color Emoji', 'Apple Color Emoji', 'Segoe UI Emoji', sans-serif";

function canvas(w, h) {
  const c = document.createElement('canvas');
  c.width = w; c.height = h;
  return [c, c.getContext('2d')];
}

// wrap draws text centered on x, from y, on lines of at most width, shrinking it to fit lines
function wrap(ctx, text, x, y, width, size, maxLines, font = DISPLAY, weight = 600) {
  for (; size > 10; size -= 2) {
    ctx.font = `${weight} ${size}px ${font}`;
    const lines = [];
    let line = '';
    for (const word of text.split(' ')) {
      const next = line ? line + ' ' + word : word;
      if (ctx.measureText(next).width > width && line) { lines.push(line); line = word; } else line = next;
    }
    lines.push(line);
    if (lines.length <= maxLines && lines.every(l => ctx.measureText(l).width <= width)) {
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      const top = y - (lines.length - 1) * size * 0.55;
      lines.forEach((l, i) => ctx.fillText(l, x, top + i * size * 1.1));
      return;
    }
  }
}

function coin(ctx, x, y, r) {
  ctx.fillStyle = '#ffc93c';
  ctx.strokeStyle = '#b8860b';
  ctx.lineWidth = r * 0.25;
  ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2); ctx.fill(); ctx.stroke();
}

function price(ctx, value, x, y, size) {
  ctx.font = `800 ${size}px ${BODY}`;
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  const w = ctx.measureText(String(value)).width + size * 1.1;
  coin(ctx, x - w / 2 + size * 0.4, y, size * 0.38);
  ctx.fillStyle = '#1d2a3a';
  ctx.fillText(String(value), x - w / 2 + size * 1.0, y + size * 0.04);
}

function icon(ctx, glyph, x, y, size) {
  ctx.font = `${size}px ${EMOJI}`;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(glyph, x, y);
}

const ICONS = { transit: '🚋', card: '❓', tax: '💸', 'Compagnie des eaux': '💧', 'Centrale électrique': '⚡' };

function tileTexture(sq, renderer) {
  const corner = ['start', 'jail', 'rest', 'gotojail'].includes(sq.kind);
  const [c, ctx] = corner ? canvas(512, 512) : canvas(320, 512);
  const w = c.width, h = c.height;
  ctx.fillStyle = '#fff8e8';
  ctx.fillRect(0, 0, w, h);
  ctx.strokeStyle = 'rgba(29,42,58,.18)';
  ctx.lineWidth = 6;
  ctx.strokeRect(3, 3, w - 6, h - 6);
  ctx.fillStyle = '#1d2a3a';
  if (sq.kind === 'street') {
    ctx.fillStyle = GROUP_COLORS[sq.group];
    ctx.fillRect(0, 0, w, h * 0.22);
    ctx.fillStyle = 'rgba(255,255,255,.25)';
    ctx.fillRect(0, h * 0.22 - 10, w, 10);
    ctx.fillStyle = '#1d2a3a';
    wrap(ctx, sq.name.toUpperCase(), w / 2, h * 0.5, w - 36, 46, 3);
    price(ctx, sq.price, w / 2, h * 0.86, 48);
  } else if (corner) {
    ctx.save();
    ctx.translate(w / 2, h / 2);
    ctx.rotate(-Math.PI / 4);
    const big = { start: '🏁', jail: '🚓', rest: '🌳', gotojail: '👮' }[sq.kind];
    icon(ctx, big, 0, -40, 120);
    ctx.fillStyle = sq.kind === 'gotojail' ? '#e63946' : '#1d2a3a';
    wrap(ctx, sq.name.toUpperCase(), 0, 90, 330, 54, 2);
    if (sq.kind === 'start') {
      ctx.fillStyle = '#e63946';
      ctx.font = `800 60px ${DISPLAY}`;
      ctx.fillText('➜', 0, 160);
    }
    ctx.restore();
  } else {
    icon(ctx, ICONS[sq.name] || ICONS[sq.kind] || '★', w / 2, h * 0.3, 110);
    ctx.fillStyle = '#1d2a3a';
    wrap(ctx, sq.name.toUpperCase(), w / 2, h * 0.6, w - 36, 42, 2);
    if (sq.price) price(ctx, sq.price, w / 2, h * 0.86, 48);
    if (sq.tax) {
      ctx.fillStyle = '#e63946';
      ctx.font = `800 48px ${BODY}`;
      ctx.fillText('− ' + sq.tax, w / 2, h * 0.86);
    }
  }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  t.anisotropy = renderer.capabilities.getMaxAnisotropy();
  return t;
}

function textSprite(text, color, size = 64) {
  const [c, ctx] = canvas(512, 128);
  ctx.font = `800 ${size}px ${DISPLAY}`;
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.lineWidth = 14;
  ctx.strokeStyle = '#1d2a3a';
  ctx.lineJoin = 'round';
  ctx.strokeText(text, 256, 64);
  ctx.fillStyle = color;
  ctx.fillText(text, 256, 64);
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  const s = new THREE.Sprite(new THREE.SpriteMaterial({ map: t, depthTest: false, transparent: true }));
  s.scale.set(2, 0.5, 1);
  s.renderOrder = 10;
  return s;
}

function diceFace(n) {
  const [c, ctx] = canvas(128, 128);
  ctx.fillStyle = '#fffdf6';
  ctx.fillRect(0, 0, 128, 128);
  ctx.strokeStyle = 'rgba(29,42,58,.25)';
  ctx.lineWidth = 8;
  ctx.strokeRect(4, 4, 120, 120);
  const at = { 1: [[64, 64]], 2: [[36, 36], [92, 92]], 3: [[34, 34], [64, 64], [94, 94]], 4: [[36, 36], [92, 36], [36, 92], [92, 92]],
    5: [[34, 34], [94, 34], [64, 64], [34, 94], [94, 94]], 6: [[36, 30], [92, 30], [36, 64], [92, 64], [36, 98], [92, 98]] }[n];
  ctx.fillStyle = n === 1 ? '#e63946' : '#1d2a3a';
  for (const [x, y] of at) { ctx.beginPath(); ctx.arc(x, y, n === 1 ? 18 : 12, 0, Math.PI * 2); ctx.fill(); }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

// --- props -----------------------------------------------------------------------------------

function tree(scale = 1, round = false) {
  const g = new THREE.Group();
  g.add(outline(mesh(new THREE.CylinderGeometry(0.05, 0.07, 0.3, 6), toon(PAL.wood2))));
  g.children[0].position.y = 0.15;
  const top = round ? new THREE.IcosahedronGeometry(0.28, 0) : new THREE.ConeGeometry(0.26, 0.6, 7);
  const leaves = outline(mesh(top, toon(Math.random() < 0.5 ? PAL.leaf : PAL.leaf2)), 1.06);
  leaves.position.y = round ? 0.5 : 0.6;
  g.add(leaves);
  g.scale.setScalar(scale);
  g.rotation.y = Math.random() * Math.PI;
  return g;
}

function palm(scale = 1) {
  const g = new THREE.Group();
  const trunk = outline(mesh(new THREE.CylinderGeometry(0.05, 0.08, 1, 6), toon(PAL.wood)));
  trunk.position.y = 0.5;
  trunk.rotation.z = 0.15;
  g.add(trunk);
  for (let i = 0; i < 5; i++) {
    const leaf = outline(mesh(new THREE.ConeGeometry(0.12, 0.7, 4), toon(PAL.leaf)), 1.05);
    leaf.position.set(0.07, 1, 0);
    leaf.rotation.set(0, (i / 5) * Math.PI * 2, Math.PI / 2.4);
    leaf.geometry.translate(0, 0.35, 0);
    g.add(leaf);
  }
  g.scale.setScalar(scale);
  return g;
}

function lighthouse() {
  const g = new THREE.Group();
  for (let i = 0; i < 4; i++) {
    const r0 = 0.32 - i * 0.04, r1 = 0.28 - i * 0.04;
    const ring = outline(mesh(new THREE.CylinderGeometry(r1, r0, 0.35, 16), toon(i % 2 ? PAL.white : PAL.red)), 1.04);
    ring.position.y = 0.175 + i * 0.35;
    g.add(ring);
  }
  const lamp = mesh(new THREE.CylinderGeometry(0.15, 0.15, 0.22, 12), new THREE.MeshBasicMaterial({ color: 0xfff3a0 }));
  lamp.position.y = 1.52;
  const cap = outline(mesh(new THREE.ConeGeometry(0.22, 0.25, 12), toon(PAL.red)));
  cap.position.y = 1.75;
  g.add(lamp, cap);
  return g;
}

function cloud() {
  const g = new THREE.Group();
  const m = soft(PAL.white);
  for (let i = 0; i < 5; i++) {
    const s = mesh(new THREE.IcosahedronGeometry(1 + Math.random() * 0.8, 1), m, { shadow: false });
    s.position.set(i * 1.3 - 2.6, Math.random() * 0.6, Math.random() * 0.8);
    g.add(s);
  }
  return g;
}

function house(group) {
  const g = new THREE.Group();
  const body = outline(mesh(new THREE.BoxGeometry(0.2, 0.18, 0.2), toon(PAL.cream)));
  body.position.y = 0.09;
  const roof = outline(mesh(new THREE.ConeGeometry(0.18, 0.15, 4), toon(new THREE.Color(GROUP_COLORS[group]))));
  roof.position.y = 0.255;
  roof.rotation.y = Math.PI / 4;
  g.add(body, roof);
  return g;
}

let windowTexture;
function tower(group) {
  if (!windowTexture) {
    const [c, ctx] = canvas(64, 128);
    ctx.fillStyle = '#eef2f6';
    ctx.fillRect(0, 0, 64, 128);
    ctx.fillStyle = '#6fb7e8';
    for (let y = 10; y < 120; y += 22) for (let x = 8; x < 60; x += 20) ctx.fillRect(x, y, 10, 12);
    windowTexture = new THREE.CanvasTexture(c);
    windowTexture.colorSpace = THREE.SRGBColorSpace;
  }
  const g = new THREE.Group();
  const body = outline(mesh(new THREE.BoxGeometry(0.34, 0.62, 0.3), toon(PAL.white, { map: windowTexture })), 1.05);
  body.position.y = 0.31;
  const top = outline(mesh(new THREE.BoxGeometry(0.4, 0.08, 0.36), toon(new THREE.Color(GROUP_COLORS[group]))), 1.05);
  top.position.y = 0.66;
  const flag = mesh(new THREE.CylinderGeometry(0.01, 0.01, 0.25), toon(INK));
  flag.position.y = 0.82;
  g.add(body, top, flag);
  return g;
}

function pawnShape() {
  const pts = [[0, 0], [0.17, 0], [0.17, 0.05], [0.13, 0.08], [0.08, 0.15], [0.06, 0.3], [0.1, 0.34], [0.1, 0.37], [0.04, 0.39], [0, 0.39]]
    .map(([x, y]) => new THREE.Vector2(x, y));
  return new THREE.LatheGeometry(pts, 24);
}
const PAWN_BODY = pawnShape(), PAWN_HEAD = new THREE.SphereGeometry(0.105, 20, 14);

// --- easing and timing -----------------------------------------------------------------------

const ease = t => 1 - Math.pow(1 - t, 3);
const easeInOut = t => t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
const back = t => { const c = 1.7; return 1 + (c + 1) * Math.pow(t - 1, 3) + c * Math.pow(t - 1, 2); };

export class Scene {
  constructor(canvasEl, board) {
    this.board = board;
    this.anims = new Set();
    this.fast = false;
    const r = this.renderer = new THREE.WebGLRenderer({ canvas: canvasEl, antialias: true });
    r.setPixelRatio(Math.min(devicePixelRatio, 2));
    r.shadowMap.enabled = true;
    r.shadowMap.type = THREE.PCFSoftShadowMap;
    const scene = this.scene = new THREE.Scene();
    scene.fog = new THREE.Fog(PAL.horizon, 40, 110);
    this.camera = new THREE.PerspectiveCamera(38, 1, 0.1, 400);

    const sun = new THREE.DirectionalLight(0xffffff, 2.1);
    sun.position.set(-7, 14, 8);
    sun.castShadow = true;
    sun.shadow.mapSize.set(2048, 2048);
    Object.assign(sun.shadow.camera, { left: -9, right: 9, top: 9, bottom: -9, near: 1, far: 40 });
    sun.shadow.bias = -0.0005;
    scene.add(new THREE.HemisphereLight(0xffffff, 0x9fd0e6, 1.35), sun);

    this.buildWorld();
    this.buildBoard();
    this.buildDice();
    this.pawns = new Map(); // id -> {g, seat, pos, jail, color, out}
    this.deeds = new Map(); // square -> {owner, level, mortgaged, marker, buildings, veil}
    this.cash = new Map();

    // camera: orbit around a target that follows the action
    this.view = { az: 0, polar: 0.92, dist: 17.5, target: new THREE.Vector3() };
    this.goal = { az: 0, polar: 0.92, dist: 17.5, target: new THREE.Vector3() };
    this.idle = true;
    this.controls(canvasEl);
    this.resize();
    addEventListener('resize', () => this.resize());
    this.last = performance.now();
    this.renderer.setAnimationLoop(now => this.frame(now));
  }

  resize() {
    const w = innerWidth, h = innerHeight;
    this.renderer.setSize(w, h, false);
    this.camera.aspect = w / h;
    // a narrow screen backs off to keep the board in view
    this.fit = Math.max(1, 1.25 / this.camera.aspect);
    this.camera.updateProjectionMatrix();
  }

  // --- world ---

  buildWorld() {
    const s = this.scene;
    // sky: a dome from the horizon to deep blue
    const sky = new THREE.Mesh(new THREE.SphereGeometry(200, 32, 16), new THREE.ShaderMaterial({
      side: THREE.BackSide, depthWrite: false, fog: false,
      uniforms: { top: { value: new THREE.Color(0x4fb4ea) }, bottom: { value: new THREE.Color(PAL.horizon) } },
      vertexShader: 'varying float h; void main() { h = normalize(position).y; gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0); }',
      fragmentShader: 'uniform vec3 top, bottom; varying float h; void main() { gl_FragColor = vec4(mix(bottom, top, smoothstep(-0.05, 0.5, h)), 1.0); }',
    }));
    s.add(sky);
    // the sea, with a ring of foam around the island
    const sea = mesh(new THREE.CircleGeometry(160, 64), soft(PAL.sea), { shadow: false, receive: true });
    sea.rotation.x = -Math.PI / 2;
    sea.position.y = -1.2;
    s.add(sea);
    this.foam = mesh(new THREE.RingGeometry(11.2, 12, 64), new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.6 }), { shadow: false });
    this.foam.rotation.x = -Math.PI / 2;
    this.foam.position.y = -1.18;
    s.add(this.foam);
    // the island: sand, then grass, the board on it
    const sand = mesh(new THREE.CylinderGeometry(11, 11.8, 1, 48), soft(PAL.sand), { receive: true });
    sand.position.y = -1.2;
    const grass = mesh(new THREE.CylinderGeometry(9.4, 10.2, 0.5, 48), toon(PAL.grass2), { receive: true });
    grass.position.y = -0.55;
    s.add(sand, grass);
    for (let i = 0; i < 14; i++) {
      const a = (i / 14) * Math.PI * 2 + Math.random() * 0.2;
      if (i % 2) {
        const p = palm(0.9 + Math.random() * 0.4);
        p.position.set(Math.cos(a) * 10.4, -0.7, Math.sin(a) * 10.4);
        p.rotation.y = -a;
        s.add(p);
      } else {
        const t = tree(1 + Math.random() * 0.6, Math.random() < 0.5);
        t.position.set(Math.cos(a) * 8.6, -0.3, Math.sin(a) * 8.6);
        s.add(t);
      }
    }
    for (let i = 0; i < 9; i++) {
      const rock = outline(mesh(new THREE.DodecahedronGeometry(0.3 + Math.random() * 0.4, 0), toon(PAL.stone)), 1.05);
      const a = Math.random() * Math.PI * 2;
      rock.position.set(Math.cos(a) * 11.6, -0.9, Math.sin(a) * 11.6);
      rock.rotation.set(Math.random(), Math.random(), Math.random());
      s.add(rock);
    }
    this.clouds = [];
    for (let i = 0; i < 7; i++) {
      const c = cloud();
      const a = (i / 7) * Math.PI * 2;
      c.position.set(Math.cos(a) * (35 + Math.random() * 20), 8 + Math.random() * 8, Math.sin(a) * (35 + Math.random() * 20));
      c.scale.setScalar(1.2 + Math.random());
      c.userData.speed = 0.01 + Math.random() * 0.02;
      this.clouds.push(c);
      s.add(c);
    }
  }

  buildBoard() {
    const s = this.scene;
    const base = mesh(new THREE.BoxGeometry(HALF * 2 + 0.5, 0.5, HALF * 2 + 0.5), toon(PAL.wood), { receive: true });
    base.position.y = -0.25;
    const top = mesh(new THREE.BoxGeometry(HALF * 2 + 0.2, 0.06, HALF * 2 + 0.2), soft(PAL.board), { receive: true, shadow: false });
    top.position.y = 0.03;
    s.add(outline(base, 1.005), top);

    // the middle: a park with a pond, trees, the lighthouse and the name of the game
    const inner = HALF - TILE_D - 0.1;
    const park = mesh(new THREE.BoxGeometry(inner * 2, 0.1, inner * 2), toon(PAL.grass), { receive: true, shadow: false });
    park.position.y = 0.08;
    s.add(park);
    const pond = mesh(new THREE.CylinderGeometry(0.9, 0.9, 0.04, 32), soft(PAL.water), { shadow: false, receive: true });
    pond.scale.set(1.3, 1, 0.8);
    pond.position.set(-2.1, 0.14, -2);
    s.add(pond);
    const lh = lighthouse();
    lh.position.set(2.5, 0.13, -2.4);
    s.add(lh);
    for (const [x, z, sc] of [[-2.8, 0.6, 0.9], [-2.6, 1.6, 0.7], [2.7, 1.2, 0.8], [2.2, 2.7, 0.7], [-1.2, -2.8, 0.75], [0.4, -2.9, 0.65], [2.9, -0.9, 0.7], [-2.9, 2.8, 0.8]]) {
      const t = tree(sc, Math.random() < 0.5);
      t.position.set(x, 0.13, z);
      s.add(t);
    }
    // the name, written on the lawn
    const [c, ctx] = canvas(1024, 256);
    ctx.font = `700 150px ${DISPLAY}`;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.lineWidth = 22;
    ctx.lineJoin = 'round';
    ctx.strokeStyle = '#1d2a3a';
    ctx.strokeText('Quartiers', 512, 128);
    ctx.fillStyle = '#ffd23f';
    ctx.fillText('Quartiers', 512, 128);
    const tex = new THREE.CanvasTexture(c);
    tex.colorSpace = THREE.SRGBColorSpace;
    tex.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
    const title = new THREE.Mesh(new THREE.PlaneGeometry(5, 1.25), new THREE.MeshBasicMaterial({ map: tex, transparent: true }));
    title.rotation.x = -Math.PI / 2;
    title.position.set(0, 0.14, -0.6);
    s.add(title);

    // the squares
    this.tiles = [];
    const sideMat = soft(PAL.cream);
    this.board.squares.forEach((sq, i) => {
      const f = frame(i);
      const w = f.corner ? CORNER : TILE_W, d = f.corner ? CORNER : TILE_D;
      const g = new THREE.Group();
      g.position.set(f.x, 0, f.z);
      g.rotation.y = f.rot;
      const block = mesh(new THREE.BoxGeometry(w - 0.05, TOP - 0.06, d - 0.05), sideMat, { receive: true });
      block.position.y = 0.06 + (TOP - 0.06) / 2;
      const face = mesh(new THREE.PlaneGeometry(w - 0.06, d - 0.06), toon(PAL.white, { map: tileTexture(sq, this.renderer), emissive: 0x000000 }), { shadow: false, receive: true });
      face.rotation.x = -Math.PI / 2;
      face.position.y = TOP + 0.001;
      face.userData.square = i;
      block.userData.square = i;
      g.add(block, face);
      if (sq.kind === 'jail') g.add(this.bars());
      s.add(g);
      this.tiles.push({ g, face, w, d, corner: f.corner, lift: 0 });
    });
    this.pickables = this.tiles.flatMap(t => [t.face]);

    // the marker under the current player, and the one on the selected square
    this.ring = mesh(new THREE.RingGeometry(0.19, 0.26, 32), new THREE.MeshBasicMaterial({ color: 0xffffff, transparent: true, opacity: 0.9, side: THREE.DoubleSide }), { shadow: false });
    this.ring.rotation.x = -Math.PI / 2;
    this.ring.visible = false;
    s.add(this.ring);
  }

  bars() {
    const g = new THREE.Group();
    const m = toon(PAL.steel);
    const post = new THREE.CylinderGeometry(0.02, 0.02, 0.42, 6);
    for (let i = 0; i <= 8; i++) {
      const t = -0.8 + i * 0.1;
      for (const [x, z] of [[t, 0], [0, t]]) {
        const b = mesh(post, m);
        b.position.set(x, TOP + 0.21, z);
        g.add(b);
      }
    }
    for (const [x, z, ry] of [[-0.4, 0, 0], [0, -0.4, Math.PI / 2]]) {
      const rail = mesh(new THREE.BoxGeometry(0.82, 0.04, 0.04), m);
      rail.position.set(x, TOP + 0.42, z);
      rail.rotation.y = ry;
      g.add(rail);
    }
    return g;
  }

  buildDice() {
    // faces of a BoxGeometry: +x, -x, +y, -y, +z, -z
    const values = [2, 5, 1, 6, 3, 4];
    const mats = values.map(v => toon(PAL.white, { map: diceFace(v) }));
    // the turn that puts each value on top
    const up = {
      1: new THREE.Euler(0, 0, 0), 6: new THREE.Euler(Math.PI, 0, 0), 2: new THREE.Euler(0, 0, Math.PI / 2),
      5: new THREE.Euler(0, 0, -Math.PI / 2), 3: new THREE.Euler(-Math.PI / 2, 0, 0), 4: new THREE.Euler(Math.PI / 2, 0, 0),
    };
    this.faceUp = v => new THREE.Quaternion().setFromEuler(up[v]);
    this.dice = [0, 1].map(() => {
      const d = outline(mesh(new THREE.BoxGeometry(0.46, 0.46, 0.46), mats), 1.05);
      d.visible = false;
      this.scene.add(d);
      return d;
    });
  }

  // --- input and camera ---

  controls(el) {
    let drag = null;
    this.pointer = new THREE.Vector2();
    this.ray = new THREE.Raycaster();
    el.addEventListener('pointerdown', e => { drag = { x: e.clientX, y: e.clientY, moved: 0 }; el.setPointerCapture(e.pointerId); });
    el.addEventListener('pointermove', e => {
      this.pointer.set((e.clientX / innerWidth) * 2 - 1, -(e.clientY / innerHeight) * 2 + 1);
      this.hover = this.pick();
      el.style.cursor = this.hover >= 0 ? 'pointer' : drag ? 'grabbing' : 'grab';
      if (!drag) return;
      const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
      drag.moved += Math.abs(dx) + Math.abs(dy);
      drag.x = e.clientX; drag.y = e.clientY;
      this.goal.az -= dx * 0.006;
      this.goal.polar = Math.min(1.25, Math.max(0.35, this.goal.polar - dy * 0.004));
      this.manualAt = performance.now();
    });
    el.addEventListener('pointerup', () => {
      if (drag && drag.moved < 6 && this.onPick) this.onPick(this.pick());
      drag = null;
    });
    el.addEventListener('wheel', e => {
      e.preventDefault();
      this.goal.dist = Math.min(30, Math.max(7, this.goal.dist * (1 + Math.sign(e.deltaY) * 0.1)));
      this.manualAt = performance.now();
    }, { passive: false });
  }

  pick() {
    this.ray.setFromCamera(this.pointer, this.camera);
    const hit = this.ray.intersectObjects(this.pickables, false)[0];
    return hit ? hit.object.userData.square : -1;
  }

  // focus points the camera at a point, closer; overview shows the whole board
  focus(p, dist = 11) {
    this.goal.target.set(p.x, 0, p.z);
    this.goal.dist = dist;
  }

  overview() {
    if (performance.now() - (this.manualAt || -1e9) < 4000) return; // the player is looking around
    this.goal.target.set(0, 0, 0);
    this.goal.dist = 17.5;
  }

  // home stops the slow turn of the lobby and faces the start, from the nearest way round
  home() {
    this.idle = false;
    this.goal.az = Math.round(this.view.az / (Math.PI * 2)) * Math.PI * 2;
    this.goal.polar = 0.92;
    this.goal.target.set(0, 0, 0);
    this.goal.dist = 17.5;
  }

  // --- the loop ---

  frame(now) {
    const dt = Math.min(0.1, (now - this.last) / 1000);
    this.last = now;
    for (const a of this.anims) {
      const t = Math.min(1, (now - a.start) / a.dur);
      a.fn(t);
      if (t >= 1) { this.anims.delete(a); a.done(); }
    }
    if (this.idle) this.goal.az += dt * 0.08;
    const k = 1 - Math.pow(0.002, dt);
    const v = this.view, g = this.goal;
    v.az += (g.az - v.az) * k;
    v.polar += (g.polar - v.polar) * k;
    v.dist += (g.dist * (this.idle ? 1.1 : 1) - v.dist) * k;
    v.target.lerp(g.target, k);
    const d = v.dist * this.fit;
    this.camera.position.set(v.target.x + d * Math.sin(v.polar) * Math.sin(v.az), d * Math.cos(v.polar), v.target.z + d * Math.sin(v.polar) * Math.cos(v.az));
    this.camera.lookAt(v.target);

    for (const c of this.clouds) {
      const a = Math.atan2(c.position.z, c.position.x) + dt * c.userData.speed;
      const r = Math.hypot(c.position.x, c.position.z);
      c.position.x = Math.cos(a) * r;
      c.position.z = Math.sin(a) * r;
    }
    const wave = Math.sin(now / 900);
    this.foam.scale.setScalar(1 + wave * 0.02);
    this.foam.material.opacity = 0.45 + wave * 0.15;
    if (this.ring.visible) {
      this.ring.scale.setScalar(1 + 0.15 * Math.sin(now / 200));
      const p = this.pawns.get(this.current);
      if (p) this.ring.position.set(p.g.position.x, p.g.position.y + 0.01, p.g.position.z);
    }
    this.tiles.forEach((t, i) => {
      const want = i === this.selected ? 0.12 : i === this.hover ? 0.05 : 0;
      t.lift += (want - t.lift) * k;
      t.g.position.y = t.lift;
      t.face.material.emissive.setHex(i === this.hover ? 0x222222 : 0);
    });
    this.renderer.render(this.scene, this.camera);
  }

  // tween runs fn(t) for t from 0 to 1 over secs; in fast mode at once
  tween(secs, fn) {
    if (this.fast || secs <= 0 || document.hidden) { fn(1); return Promise.resolve(); }
    return new Promise(done => this.anims.add({ start: performance.now(), dur: secs * 1000, fn, done }));
  }

  // --- pawns ---

  // slot is where a pawn stands on square sq: its seat's place on the tile
  slot(sq, seat, jailed) {
    const t = this.tiles[sq];
    let x, z;
    if (!t.corner) {
      x = -0.33 + (seat % 4) * 0.22;
      z = 0.05 + Math.floor(seat / 4) * 0.36;
    } else if (jailed) {
      x = -0.62 + (seat % 3) * 0.22;
      z = -0.62 + Math.floor(seat / 3) * 0.22;
    } else if (this.board.squares[sq].kind === 'jail') {
      [x, z] = [[0.5, -0.55], [0.5, -0.2], [0.5, 0.15], [0.5, 0.5], [0.15, 0.5], [-0.2, 0.5], [-0.55, 0.5], [0.2, 0.2]][seat % 8];
    } else {
      x = -0.4 + (seat % 3) * 0.4;
      z = -0.4 + Math.floor(seat / 3) * 0.4;
    }
    t.g.updateMatrixWorld();
    const p = new THREE.Vector3(x, TOP, z).applyMatrix4(t.g.matrixWorld);
    p.y = TOP;
    return p;
  }

  pawn(id, seat, color) {
    let p = this.pawns.get(id);
    if (!p) {
      const g = new THREE.Group();
      const m = toon(new THREE.Color(color));
      const body = outline(mesh(PAWN_BODY, m), 1.08);
      const head = outline(mesh(PAWN_HEAD, m), 1.12);
      head.position.y = 0.47;
      g.add(body, head);
      g.scale.setScalar(0.95);
      this.scene.add(g);
      p = { g, seat, pos: -1, jail: false, mat: m, out: false };
      this.pawns.set(id, p);
    }
    p.seat = seat;
    p.mat.color.set(color);
    return p;
  }

  place(p, sq, jail) {
    p.pos = sq; p.jail = jail;
    p.g.position.copy(this.slot(sq, p.seat, jail));
  }

  // hop moves a pawn one square on, in an arc
  async hop(p, to, secs, height = 0.45) {
    const from = p.g.position.clone(), dest = this.slot(to, p.seat, false);
    await this.tween(secs, t => {
      p.g.position.lerpVectors(from, dest, t);
      p.g.position.y = TOP + Math.sin(t * Math.PI) * height;
      const squash = t > 0.85 ? 1 - (1 - t) * 0.6 : 1;
      p.g.scale.set(0.95 / Math.sqrt(squash), 0.95 * squash, 0.95 / Math.sqrt(squash));
    });
    p.g.scale.setScalar(0.95);
    p.pos = to;
  }

  // movePawn walks pawn id to square to, square by square (backwards with back), and the camera follows
  async movePawn(id, to, back = false) {
    const p = this.pawns.get(id);
    if (!p || p.pos < 0) return;
    const n = this.board.squares.length;
    const steps = back ? (p.pos - to + n) % n : (to - p.pos + n) % n;
    if (steps === 0) return;
    const secs = Math.max(0.09, Math.min(0.22, 2.4 / steps));
    for (let i = 0; i < steps; i++) {
      const next = (p.pos + (back ? -1 : 1) + n) % n;
      this.focus(this.slot(next, p.seat, false), 12);
      await this.hop(p, next, secs);
    }
    p.jail = false;
  }

  // jump sends a pawn straight to a square (to jail: into the cell)
  async jump(id, to, jail) {
    const p = this.pawns.get(id);
    if (!p) return;
    const from = p.g.position.clone(), dest = this.slot(to, p.seat, jail);
    this.focus(dest, 12);
    await this.tween(0.7, t => {
      p.g.position.lerpVectors(from, dest, easeInOut(t));
      p.g.position.y = TOP + Math.sin(t * Math.PI) * 2.2;
      p.g.rotation.y = t * Math.PI * 2;
    });
    p.pos = to; p.jail = jail;
  }

  setCurrent(id) {
    this.current = id;
    this.ring.visible = !!this.pawns.get(id);
  }

  // --- dice ---

  async rollDice([a, b]) {
    const q = [a, b].map(v => this.faceUp(v).premultiply(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), Math.random() * Math.PI * 2)));
    const ends = [new THREE.Vector3(-0.45, 0.4, 1.2 + Math.random() * 0.3), new THREE.Vector3(0.5, 0.4, 1.4 + Math.random() * 0.3)];
    const cam = this.camera.position.clone().multiplyScalar(0.45);
    const starts = ends.map((e, i) => new THREE.Vector3(cam.x + (i - 0.5), 3.5, cam.z));
    const axes = [0, 1].map(() => new THREE.Vector3(Math.random() - 0.5, Math.random() - 0.5, Math.random() - 0.5).normalize());
    this.goal.target.set(0, 0, 0.8);
    this.goal.dist = 13;
    this.dice.forEach(d => { d.visible = true; });
    await this.tween(1.15, t => {
      const e = ease(t);
      this.dice.forEach((d, i) => {
        d.position.lerpVectors(starts[i], ends[i], e);
        d.position.y = 0.4 + Math.abs(Math.cos(t * Math.PI * 2.2)) * Math.pow(1 - t, 1.6) * 3;
        const spin = new THREE.Quaternion().setFromAxisAngle(axes[i], (1 - e) * 14);
        d.quaternion.copy(spin).multiply(q[i]);
      });
    });
    this.dice.forEach((d, i) => { d.position.copy(ends[i]); d.quaternion.copy(q[i]); });
  }

  // --- properties ---

  // setDeed shows square sq owned by color (null: the bank), at level, mortgaged or not
  async setDeed(sq, owner, color, level, mortgaged, animate) {
    const t = this.tiles[sq], group = this.board.squares[sq].group;
    let d = this.deeds.get(sq);
    if (!d) { d = { owner: 0, level: 0, mortgaged: false, marker: null, buildings: [], veil: null }; this.deeds.set(sq, d); }
    const jobs = [];
    if (d.owner !== owner) {
      if (d.marker) { t.g.remove(d.marker); d.marker = null; }
      if (owner) {
        // a flag on the tile's outer corner, in the owner's color
        const m = new THREE.Group();
        const pole = mesh(new THREE.CylinderGeometry(0.015, 0.015, 0.4, 6), toon(INK));
        pole.position.y = 0.2;
        const flag = outline(mesh(new THREE.BoxGeometry(0.2, 0.12, 0.02), toon(new THREE.Color(color))), 1.1);
        flag.position.set(0.1, 0.33, 0);
        m.add(pole, flag);
        m.position.set(t.w / 2 - 0.12, TOP, t.d / 2 - 0.12);
        // and a strip along its outer edge
        const strip = mesh(new THREE.BoxGeometry(t.w - 0.1, 0.02, 0.06), toon(new THREE.Color(color)), { shadow: false });
        strip.position.set(-(t.w / 2) + 0.12, 0.01, 0.04);
        m.add(strip);
        t.g.add(m);
        d.marker = m;
        if (animate) jobs.push(this.tween(0.5, k => m.scale.setScalar(Math.max(0.001, back(k)))));
      }
      d.owner = owner;
    }
    if (d.level !== level) {
      const grow = level > d.level;
      for (const b of d.buildings) t.g.remove(b);
      d.buildings = [];
      if (level === this.board.maxLevel) {
        d.buildings.push(tower(group));
      } else {
        for (let i = 0; i < level; i++) d.buildings.push(house(group));
      }
      const n = d.buildings.length;
      d.buildings.forEach((b, i) => {
        b.position.set(n === 1 ? 0 : -0.3 + i * 0.3, TOP, -t.d / 2 + 0.2);
        t.g.add(b);
        if (animate && grow && i === n - 1) {
          jobs.push(this.tween(0.6, k => { b.scale.setScalar(Math.max(0.001, back(k))); b.position.y = TOP + (1 - ease(k)) * 1.2; }));
        }
      });
      if (animate) jobs.push(this.puff(sq));
      d.level = level;
    }
    if (d.mortgaged !== mortgaged) {
      if (d.veil) { t.g.remove(d.veil); d.veil.material.map.dispose(); d.veil.material.dispose(); d.veil.geometry.dispose(); d.veil = null; }
      if (mortgaged) {
        const cw = 256, ch = Math.round(256 * t.d / t.w);
        const [c, ctx] = canvas(cw, ch);
        ctx.fillStyle = 'rgba(29,42,58,.55)';
        ctx.fillRect(0, 0, cw, ch);
        ctx.translate(cw / 2, ch / 2);
        ctx.rotate(-Math.atan2(ch, cw));
        ctx.font = `800 34px ${DISPLAY}`;
        ctx.textAlign = 'center';
        ctx.fillStyle = '#ffd23f';
        ctx.fillText('HYPOTHÈQUE', 0, 12);
        const tex = new THREE.CanvasTexture(c);
        tex.colorSpace = THREE.SRGBColorSpace;
        const veil = new THREE.Mesh(new THREE.PlaneGeometry(t.w - 0.06, t.d - 0.06), new THREE.MeshBasicMaterial({ map: tex, transparent: true }));
        veil.rotation.x = -Math.PI / 2;
        veil.position.y = TOP + 0.004;
        t.g.add(veil);
        d.veil = veil;
      }
      d.mortgaged = mortgaged;
    }
    await Promise.all(jobs);
  }

  // puff: a few little clouds where something was built or sold
  puff(sq) {
    const t = this.tiles[sq];
    const parts = [];
    for (let i = 0; i < 6; i++) {
      const p = mesh(new THREE.IcosahedronGeometry(0.07, 0), soft(PAL.white), { shadow: false });
      p.position.set((Math.random() - 0.5) * 0.6, TOP + 0.1, -t.d / 2 + 0.2 + (Math.random() - 0.5) * 0.3);
      p.userData.v = new THREE.Vector3((Math.random() - 0.5) * 0.8, 0.6 + Math.random() * 0.4, (Math.random() - 0.5) * 0.8);
      t.g.add(p);
      parts.push(p);
    }
    return this.tween(0.7, k => {
      for (const p of parts) {
        p.position.addScaledVector(p.userData.v, 0.016);
        p.scale.setScalar(1 + k * 1.5);
        if (k >= 1) t.g.remove(p);
      }
    });
  }

  // popup shows text over a pawn (or a square), rising and fading
  popup(at, text, color) {
    const s = textSprite(text, color);
    s.position.set(at.x, at.y + 1, at.z);
    this.scene.add(s);
    return this.tween(1.6, t => {
      s.position.y = at.y + 1 + t * 0.9;
      s.material.opacity = t < 0.7 ? 1 : 1 - (t - 0.7) / 0.3;
      if (t >= 1) { this.scene.remove(s); s.material.map.dispose(); s.material.dispose(); }
    });
  }

  async sink(id) {
    const p = this.pawns.get(id);
    if (!p || p.out) return;
    p.out = true;
    await this.tween(0.8, t => { p.g.position.y = TOP - t * 0.8; p.g.rotation.z = t * 0.6; });
    p.g.visible = false;
  }

  removePawn(id) {
    const p = this.pawns.get(id);
    if (p) { this.scene.remove(p.g); this.pawns.delete(id); }
  }

  select(sq) { this.selected = sq; }

  // sync shows a game state: the pawns where they are, who owns what. animate: show what changed.
  async sync(state, colorOf, animate) {
    const jobs = [];
    const ids = new Set(state.players.map(p => p.id));
    for (const id of [...this.pawns.keys()]) if (!ids.has(id)) this.removePawn(id);
    state.players.forEach((pl, seat) => {
      const p = this.pawn(pl.id, seat, colorOf(pl.id));
      if (pl.bankrupt) {
        if (!p.out) animate ? jobs.push(this.sink(pl.id)) : (p.out = true, p.g.visible = false);
        return;
      }
      p.out = false;
      p.g.visible = true;
      if (p.pos !== pl.pos || p.jail !== pl.inJail) this.place(p, pl.pos, pl.inJail);
      const before = this.cash.get(pl.id);
      if (animate && before !== undefined && before !== pl.cash) {
        const diff = pl.cash - before;
        jobs.push(this.popup(p.g.position, (diff > 0 ? '+' : '−') + Math.abs(diff), diff > 0 ? '#7CFC8a' : '#ff6b6b'));
      }
      this.cash.set(pl.id, pl.cash);
    });
    const owned = new Map(state.deeds.map(d => [d.square, d]));
    this.board.squares.forEach((sq, i) => {
      if (!sq.price) return;
      const d = owned.get(i);
      jobs.push(this.setDeed(i, d ? d.owner : 0, d ? colorOf(d.owner) : null, d ? d.level : 0, d ? d.mortgaged : false, animate));
    });
    this.setCurrent(state.phase === 'over' ? 0 : state.turn);
    await Promise.all(jobs);
  }

  reset() {
    for (const id of [...this.pawns.keys()]) this.removePawn(id);
    this.cash.clear();
    for (const [sq, d] of this.deeds) {
      const t = this.tiles[sq];
      for (const o of [d.marker, d.veil, ...d.buildings]) if (o) t.g.remove(o);
    }
    this.deeds.clear();
    this.dice.forEach(d => { d.visible = false; });
    this.ring.visible = false;
  }
}
