// The client: the connection, the state, the clicks, and the order in which what the server says
// is shown: a "game" message plays its events one after another (the dice, the pawn walking, the
// card…), then the board and the HUD show its state.
import { Scene } from './scene.js';
import * as hud from './hud.js';

const token = sessionStorage.token || (sessionStorage.token = crypto.randomUUID());
const app = {
  me: 0, board: null, colors: [], room: null, game: null, seq: 0, deadlineAt: 0, endsAt: 0, selected: -1,
  log: [], logOpen: false, rooms: null, draft: null, modal: null, dismissed: 0,
  sale: [], // the squares picked to sell back to the bank, to pay a debt
  myName: sessionStorage.name || '', myColor: sessionStorage.color != null ? +sessionStorage.color : -1,
};
let ws, scene = null;
const queue = [];
let busy = false, cameraTimer = 0;
let epoch = 0; // moves on when the game shown is dropped: what was playing stops showing

// --- connection ------------------------------------------------------------------------------

function connect() {
  ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
  ws.onopen = () => send({ t: 'hello', token });
  ws.onmessage = e => receive(JSON.parse(e.data));
  ws.onclose = e => {
    if (e.code === 4000) return hud.toast('Ouvert dans un autre onglet.');
    hud.toast('Connexion perdue, reconnexion…');
    setTimeout(connect, 1000 + Math.random() * 1000);
  };
}
const send = m => ws && ws.readyState === 1 && ws.send(JSON.stringify(m));
function act(a, extra = {}) {
  document.body.classList.add('waiting'); // until the answer: no second click
  send({ t: 'act', a, square: 0, ...extra });
}

const REFUSALS = { cash: 'Pas assez d\'argent.', phase: 'Pas maintenant.', notyours: 'Ce n\'est pas ton tour.', invalid: 'Action impossible ici.',
  watching: 'Tu regardes la partie.', over: 'La partie est finie.' };

async function receive(m) {
  switch (m.t) {
    case 'welcome':
      app.me = m.id; app.colors = m.colors;
      if (app.myColor < 0) app.myColor = m.color;
      if (app.myName || sessionStorage.color != null) send({ t: 'profile', name: app.myName, color: app.myColor });
      if (!app.board) {
        app.board = m.board;
        await Promise.all(['700 40px DynaPuff', '800 40px Nunito', '600 40px DynaPuff'].map(f => document.fonts.load(f).catch(() => { })));
        scene = new Scene(document.getElementById('view'), app.board);
        scene.onPick = sq => { app.selected = sq; scene.select(sq); hud.render(app); };
      }
      if (!app.room && location.hash.length > 1) send({ t: 'join', code: location.hash.slice(1) });
      else if (!app.room) send({ t: 'rooms' });
      break;
    case 'rooms':
      if (JSON.stringify(m.rooms) === JSON.stringify(app.rooms)) return; // nothing new: the field being typed in is left alone
      app.rooms = m.rooms;
      break;
    case 'room': {
      const wasOwner = app.room?.owner === app.me;
      app.room = m;
      // the owner's settings are theirs, sent as they change them; the others see the room's
      if (m.owner !== app.me || !wasOwner || !app.draft) app.draft = { ...m.settings, name: m.name };
      history.replaceState(null, '', '#' + m.code);
      if (app.game && !busy) scene.sync(app.game, hud.names(app).colorOf, false);
      break;
    }
    case 'game':
      queue.push({ m, at: Date.now() });
      pump();
      return;
    case 'stopped':
      queue.length = 0;
      leaveGame();
      hud.toast('La partie a été arrêtée.');
      break;
    case 'left':
      queue.length = 0;
      leaveGame();
      app.room = app.draft = null;
      history.replaceState(null, '', location.pathname);
      send({ t: 'rooms' });
      if (m.reason === 'kicked') hud.toast('Tu as été exclu de la salle.');
      if (m.reason === 'closed') hud.toast('La salle a fermé.');
      break;
    case 'err':
      document.body.classList.remove('waiting');
      hud.toast(m.code === 'act' ? (REFUSALS[m.reason] || 'Refusé.') : ({ notfound: 'Salle introuvable.', full: 'Salle pleine.',
        players: 'Il faut au moins 2 joueurs.', playing: 'Une partie est déjà en cours.', locked: 'Trop d\'essais, attends un peu.', busy: 'Serveur plein.', private: 'Cette salle est devenue privée.' }[m.code] || m.code));
      return;
  }
  hud.render(app);
}

function leaveGame() {
  epoch++;
  app.game = null; app.modal = null; app.selected = -1; app.seq = 0;
  document.getElementById('card').hidden = true;
  if (scene) { scene.reset(); scene.idle = true; scene.overview(); scene.select(-1); }
}

// --- playing what happened -------------------------------------------------------------------

async function pump() {
  if (busy) return;
  busy = true;
  document.body.classList.add('animating');
  while (queue.length) {
    const item = queue.shift();
    try { await play(item); } catch (err) { console.error(err); }
  }
  busy = false;
  document.body.classList.remove('animating', 'waiting');
  clearTimeout(cameraTimer);
  cameraTimer = setTimeout(() => { if (!busy && scene) scene.overview(); }, 1800);
}

async function play({ m, at }) {
  if (!scene || (m.seq === app.dismissed && m.state.phase === 'over')) return;
  const { colorOf, nm } = hud.names(app);
  const mine = epoch, dropped = () => mine !== epoch;
  scene.fast = queue.length > 2; // falling behind: show the result at once
  if (m.seq !== app.seq || !app.game) {
    // a new game, or one joined in the middle: shown as it is
    if (m.seq !== app.seq) { app.log = []; app.selected = -1; app.modal = null; scene.select(-1); }
    app.seq = m.seq;
    scene.reset();
    scene.home();
    app.game = m.state;
    await scene.sync(m.state, colorOf, false);
    if (dropped()) return;
    if (m.events.length) hud.render(app);
  }
  let card = null, flight = false;
  for (const e of m.events) {
    app.log.unshift(hud.describe(app, e));
    hud.renderLog(app);
    switch (e.kind) {
      case 'turn':
        scene.setCurrent(e.who);
        if (!scene.fast) hud.banner(e.who === app.me ? '🎲 À toi de jouer !' : 'Au tour de <b>' + hud.esc(nm(e.who)) + '</b>', 1.2);
        break;
      case 'rolled': {
        // what was the state before this roll says what the double does
        const before = app.game.players.find(p => p.id === e.who);
        await scene.rollDice(e.dice);
        if (e.dice[0] === e.dice[1] && !scene.fast) {
          const who = e.who === app.me ? 'Tu' : '<b>' + hud.esc(nm(e.who)) + '</b>';
          hud.banner(before?.inJail ? 'Double ! ' + who + ' sor' + (e.who === app.me ? 's' : 't') + ' du poste' :
            app.game.doubles >= 2 ? '3ᵉ double…' : 'Double ! ' + who + ' rejoue' + (e.who === app.me ? 's' : ''), 1.1);
        }
        break;
      }
      case 'flew':
        flight = true;
        if (!scene.fast) hud.banner('✈️ Décollage !', 1.1);
        break;
      case 'moved':
        if (flight) await scene.jump(e.who, e.square, false);
        else await scene.movePawn(e.who, e.square, card && card.effect === 'moveBy' && card.amount < 0);
        card = flight = null;
        break;
      case 'jailed':
        if (!scene.fast) hud.banner('🚓 Au poste !', 1.2);
        await scene.jump(e.who, e.square, true);
        break;
      case 'card':
        card = app.board.cards[e.card];
        if (!scene.fast) await hud.card(app, card.text, e.who);
        break;
      case 'bonus':
        if (!scene.fast) {
          const who = e.who === app.me ? '' : '<b>' + hud.esc(nm(e.who)) + '</b>';
          hud.banner(e.by === 'roll' ? '🎲 Départ pile : ' + (who ? who + ' rejoue' : 'tu rejoues !') :
            '🏗 Départ pile : ' + (who ? who + ' construit sur la rue de son choix' : 'construis sur la rue de ton choix !'), 1.6);
        }
        break;
      case 'bankrupt':
        if (!scene.fast) hud.banner('💥 <b>' + hud.esc(nm(e.who)) + '</b> fait faillite', 1.6);
        break;
    }
    if (dropped()) return;
  }
  app.game = m.state;
  app.deadlineAt = m.deadline ? at + m.deadline : 0;
  app.endsAt = m.endsIn ? at + m.endsIn : 0;
  if (m.state.phase === 'over' && m.events.some(e => e.kind === 'over')) app.modal = hud.overModal;
  // short of cash to pay: what can be sold opens at once, and closes once the debt is gone
  const owing = m.state.phase === 'debt' && m.state.debt?.who === app.me;
  if (owing && m.events.some(e => e.kind === 'debt' && e.who === app.me)) {
    app.sale = [];
    app.modal = hud.saleModal;
  }
  if (app.modal === hud.saleModal && !owing) app.modal = null;
  document.body.classList.remove('waiting');
  hud.render(app);
  await scene.sync(m.state, colorOf, !scene.fast);
  scene.fast = false;
}

setInterval(() => hud.tick(app), 250);
// the public rooms, kept fresh while on the home
const askRooms = () => { if (app.board && !app.room && !document.hidden) send({ t: 'rooms' }); };
setInterval(askRooms, 3000);
document.addEventListener('visibilitychange', askRooms);

// --- clicks and typing -----------------------------------------------------------------------

const handlers = {
  create: () => { saveName(); send({ t: 'create' }); },
  join: () => { saveName(); send({ t: 'join', code: document.getElementById('code').value.trim().toUpperCase() }); },
  joinListed: d => { saveName(); send({ t: 'join', code: d.code, listed: true }); },
  rooms: () => send({ t: 'rooms' }),
  color(d) {
    app.myColor = +d.i;
    sessionStorage.color = d.i;
    send({ t: 'profile', name: app.myName, color: app.myColor });
    hud.render(app);
  },
  copy: d => navigator.clipboard?.writeText(d.text).then(() => hud.toast('Lien copié !'), () => hud.toast(d.text)),
  leave() {
    const me = app.game && app.game.phase !== 'over' && app.game.players.find(p => p.id === app.me && !p.bankrupt);
    if (me && !confirm('Quitter pendant la partie, c\'est abandonner. Sûr ?')) return;
    send({ t: 'leave' });
  },
  settings() { const { name, ...settings } = app.draft; send({ t: 'settings', name, settings }); },
  step(d) {
    const [lo, hi, step] = hud.LIMITS[d.k];
    app.draft[d.k] = Math.max(lo, Math.min(hi, (+app.draft[d.k] || 0) + step * +d.d));
    handlers.settings();
    hud.render(app);
  },
  seg(d) {
    app.draft[d.k] = d.v === 'true' ? true : d.v === 'false' ? false : d.v;
    handlers.settings();
    hud.render(app);
  },
  rules() { app.modal = hud.rulesModal; hud.render(app); },
  start: () => send({ t: 'start' }),
  stop: () => confirm('Arrêter la partie pour tout le monde ?') && send({ t: 'stop' }),
  kick: d => send({ t: 'kick', id: +d.id }),
  act: d => act(d.a, { square: d.sq ? +d.sq : 0, level: d.level ? +d.level : 0 }),
  unselect() { app.selected = -1; scene.select(-1); hud.render(app); },
  pick: d => scene.onPick(+d.sq),
  toggleLog() { app.logOpen = !app.logOpen; hud.renderLog(app); },
  openSale() { app.sale = []; app.modal = hud.saleModal; hud.render(app); },
  sell() {
    const g = app.game;
    const picked = g.deeds.filter(d => d.owner === app.me && app.sale.includes(d.square));
    if (!picked.length || !g.debt) return;
    act('resell', { squares: picked.map(d => d.square) }); // paid with it, when that covers the debt
    app.sale = []; // the window stays open: play() closes it once the debt is gone
    hud.render(app);
  },
  closeModal() { app.modal = null; hud.render(app); },
  lobby() { app.dismissed = app.seq; leaveGame(); hud.render(app); },
  ranking() { app.modal = hud.overModal; hud.render(app); },
};

function saveName() {
  const el = document.getElementById('name');
  if (!el) return;
  app.myName = sessionStorage.name = el.value.trim();
  send({ t: 'profile', name: app.myName, color: app.myColor });
}

document.addEventListener('pointerdown', e => { if (e.target.closest('[data-do]')) hud.hold(true); }, true);
for (const type of ['pointerup', 'pointercancel']) document.addEventListener(type, () => setTimeout(() => hud.hold(false)), true);
document.addEventListener('click', e => {
  const el = e.target.closest('[data-do]');
  if (el && handlers[el.dataset.do]) handlers[el.dataset.do](el.dataset);
});
document.addEventListener('input', e => {
  const t = e.target, b = t.dataset;
  if (t.id === 'name') app.myName = t.value; // kept as typed, sent on change
  else if (t.id === 'code') app.code = t.value;
  else if (b.set && app.draft) app.draft[b.set] = t.type === 'checkbox' ? t.checked : t.type === 'number' ? +t.value : t.value;
});
document.addEventListener('change', e => {
  const t = e.target, b = t.dataset;
  if ('saleSq' in b) {
    const sq = +t.value;
    app.sale = t.checked ? [...app.sale, sq] : app.sale.filter(x => x !== sq);
    hud.render(app);
  } else if (b.set && app.draft) {
    let v = t.type === 'number' ? +t.value : t.value;
    if (hud.LIMITS[b.set]) { const [lo, hi] = hud.LIMITS[b.set]; v = Math.max(lo, Math.min(hi, v || lo)); }
    app.draft[b.set] = v;
    handlers.settings();
    hud.render(app);
  } else if (t.id === 'name') saveName();
});
document.addEventListener('keydown', e => {
  if (e.key === 'Escape' && app.modal) handlers.closeModal();
  if (e.key !== 'Enter') return;
  if (e.target.dataset.set) e.target.blur(); // a setting typed in: sent by its change
  if (e.target.id === 'name') saveName();
  if (e.target.id === 'code') handlers.join();
});

connect();
hud.render(app);
