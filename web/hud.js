// Everything drawn in HTML over the board: the screens before a game, and during one the players,
// what to do, the square selected, the trades and the log. render(app) draws it all from app.
import { GROUP_COLORS } from './scene.js';

export const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
const $ = id => document.getElementById(id);
const fmt = n => n.toLocaleString('fr-FR');
const money = n => `<span class="money"><i class="coin"></i>${fmt(n)}</span>`;

export function names(app) {
  const person = id => app.room && app.room.players.find(p => p.id === id);
  const nm = id => id === 0 ? 'la banque' : (person(id)?.name || 'Joueur ' + id);
  const colorOf = id => person(id) ? app.colors[person(id).color] : '#9aa5b1';
  return { person, nm, colorOf };
}

export function describe(app, e) {
  const { nm } = names(app);
  const w = `<b>${esc(nm(e.who))}</b>`, t = `<b>${esc(nm(e.to))}</b>`, s = esc(app.board.squares[e.square]?.name || '');
  switch (e.kind) {
    case 'turn': return '<span class="sep">Tour de ' + w + '</span>';
    case 'rolled': return w + ' fait ' + e.dice.join(' + ') + (e.dice[0] === e.dice[1] ? ' <b>double !</b>' : '');
    case 'salary': return w + ' touche ' + e.amount + ' au Départ';
    case 'moved': return w + ' arrive sur ' + s;
    case 'jailed': return w + ' est emmené au poste 🚓';
    case 'freed': return w + ' sort du poste' + (e.amount ? ' (' + e.amount + ')' : '');
    case 'rent': return w + ' doit ' + e.amount + ' de loyer à ' + t;
    case 'tax': return w + ' doit ' + e.amount + ' (' + s + ')';
    case 'card': return w + ' : « ' + esc(app.board.cards[e.card].text) + ' »';
    case 'debt': return w + ' doit trouver ' + e.amount;
    case 'paid': return w + ' paie ' + e.amount + ' à ' + t;
    case 'bought': return w + ' achète ' + s + ' (' + e.amount + ')';
    case 'auction': return 'Enchère sur ' + s;
    case 'bid': return w + ' enchérit ' + e.amount;
    case 'pass': return w + ' passe';
    case 'auctioned': return e.who ? w + ' remporte ' + s + ' (' + e.amount + ')' : s + ' : aucune offre';
    case 'built': return w + ' construit sur ' + s;
    case 'sold': return w + ' revend un niveau sur ' + s;
    case 'mortgaged': return w + ' hypothèque ' + s;
    case 'unmortgaged': return w + ' lève l\'hypothèque de ' + s;
    case 'offer': return w + ' propose un échange à ' + t;
    case 'traded': return w + ' accepte l\'échange de ' + t;
    case 'rejected': return w + ' refuse l\'échange de ' + t;
    case 'cancelled': return w + ' retire son offre';
    case 'bankrupt': return w + ' fait faillite 💥';
    case 'over': return '🏆 ' + w + ' gagne la partie' + (e.by ? ' : ' + WIN_BY[e.by] : '') + ' !';
  }
  return e.kind;
}

// --- render, keeping the field being typed in ------------------------------------------------

// hold keeps the screen as it is while a button is pressed: drawn again under the finger, it
// would never get its click. What came meanwhile is drawn on release.
export function hold(on) {
  render.held = on;
  if (!on && render.pending) { const app = render.pending; render.pending = null; render(app); }
}

export function render(app) {
  if (render.held) { render.pending = app; return; }
  const f = document.activeElement, fid = f && f.id;
  let caret = null;
  try { caret = [f.selectionStart, f.selectionEnd]; } catch { }
  const inGame = app.room && app.game;
  $('front').hidden = !!inGame || !app.board;
  $('hud').hidden = !inGame;
  if (!app.board) return;
  if (!inGame) {
    // drawn again only when it changed: a button being pressed is not swapped under the finger
    const front = app.room ? roomScreen(app) : homeScreen(app);
    if (front !== render.front) $('front').innerHTML = render.front = front;
  } else {
    render.front = '';
    $('top').innerHTML = top(app);
    $('players').innerHTML = players(app);
    $('actions').innerHTML = actions(app);
    $('deed').innerHTML = deed(app);
    $('deed').hidden = app.selected < 0;
    $('trades').innerHTML = trades(app);
    renderLog(app);
  }
  // the window is drawn again only when what it shows changed: its animations and the button
  // being pressed are left alone
  const modal = app.modal ? app.modal(app) : '';
  if (modal !== render.modal) $('modal').innerHTML = render.modal = modal;
  $('modal').hidden = !app.modal;
  if (fid && $(fid)) {
    $(fid).focus();
    try { if (caret && caret[0] != null) $(fid).setSelectionRange(...caret); } catch { }
  }
  tick(app);
}

export function renderLog(app) {
  $('log').innerHTML = app.log.slice(0, app.logOpen ? 60 : 5).map(l => '<div>' + l + '</div>').join('') +
    `<button class="link" data-do="toggleLog">${app.logOpen ? 'réduire' : 'tout le journal'}</button>`;
}

// tick updates the clocks only
export function tick(app) {
  const el = $('clock');
  if (!el) return;
  const left = app.deadlineAt ? Math.max(0, Math.ceil((app.deadlineAt - Date.now()) / 1000)) : 0;
  el.textContent = left ? left + ' s' : '';
  el.classList.toggle('hurry', left > 0 && left <= 5);
  const end = $('endclock');
  if (end && app.endsAt) {
    const s = Math.max(0, Math.ceil((app.endsAt - Date.now()) / 1000));
    end.textContent = Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
  }
}

// --- before a game ---------------------------------------------------------------------------

const ball = (c, cls = '') => `<i class="ball ${cls}" style="--c:${c}"></i>`;

// the name and the color of the player, on the home and in the room: a color another player of the
// room has is not to be taken
function profile(app) {
  const mine = app.room?.players.find(p => p.id === app.me);
  const color = mine ? mine.color : app.myColor;
  const taken = new Set((app.room?.players || []).filter(p => p.id !== app.me).map(p => p.color));
  return `<div class="who">${ball(app.colors[color] || '#999', 'big')}<input id="name" class="field" maxlength="16" value="${esc(app.myName || '')}" placeholder="Ton pseudo" autocomplete="nickname" spellcheck="false"></div>
    <div class="swatches">${app.colors.map((c, i) => `<button class="ball ${i === color ? 'on' : ''} ${taken.has(i) ? 'taken' : ''}" style="--c:${c}" data-do="color" data-i="${i}"
      aria-label="couleur ${i + 1}" ${taken.has(i) ? 'disabled' : ''}></button>`).join('')}</div>`;
}

function homeScreen(app) {
  const list = !app.rooms?.length ? '<p class="empty">Aucune salle publique pour l\'instant.</p>' :
    '<ul class="rooms">' + app.rooms.map(r => `<li><span class="nm">${esc(r.name || 'Salle ' + r.code)}</span><span class="tag ${r.playing ? 'live' : 'open'}">${r.playing ? 'en jeu' : 'au salon'}</span>
      <span class="count">${r.players}/${r.max}</span><button class="btn small sun" data-do="joinListed" data-code="${r.code}">Entrer</button></li>`).join('') + '</ul>';
  return `<div class="home">
    <header class="brand"><h1 class="logo">Quartiers</h1><p class="tagline">Achète, construis, négocie… et ruine tes amis.</p></header>
    <div class="stack">
      <section class="card me"><h2>Ton joueur</h2>${profile(app)}</section>
      <section class="card"><h2>Jouer</h2><div class="stack">
        <button class="btn red big" data-do="create">Créer une salle</button>
        <div class="row"><input id="code" class="field code" maxlength="5" placeholder="CODE" value="${esc(app.code || '')}" autocomplete="off" spellcheck="false"><button class="btn sun" data-do="join">Rejoindre</button></div>
        <button class="link" data-do="rules">Comment on joue ?</button>
      </div></section>
    </div>
    <section class="card list-card"><h2>Salles publiques</h2>${list}</section>
  </div>`;
}

// the bounds of the numbers of the room, and the step of their − and + (the server keeps them in bounds too)
export const LIMITS = { rounds: [5, 200, 5], minutes: [5, 240, 5], startCash: [500, 5000, 100], salary: [0, 1000, 20],
  turnSeconds: [10, 300, 5], auctionSeconds: [5, 60, 1], max: [2, 8, 1] };

// stepper is a number of the settings: − and + around it for the owner, the number alone for the others
function stepper(k, s, owner, unit = '') {
  if (!owner) return `<span class="v">${fmt(s[k])}${unit}</span>`;
  const [lo, hi] = LIMITS[k];
  return `<span class="stepper"><button data-do="step" data-k="${k}" data-d="-1" ${s[k] <= lo ? 'disabled' : ''} aria-label="moins">−</button>
    <span class="val"><input type="number" id="set-${k}" data-set="${k}" value="${s[k]}" min="${lo}" max="${hi}">${unit ? '<span class="unit">' + unit + '</span>' : ''}</span>
    <button data-do="step" data-k="${k}" data-d="1" ${s[k] >= hi ? 'disabled' : ''} aria-label="plus">+</button></span>`;
}

// seg is a choice between a few values, the one taken pressed
function seg(k, options, value) {
  return '<span class="seg">' + options.map(([v, l]) => `<button data-do="seg" data-k="${k}" data-v="${v}" aria-pressed="${v === value}">${l}</button>`).join('') + '</span>';
}

const END = [['rounds', 'Tours'], ['time', 'Temps'], ['survivor', 'Survivant']];

// the instant wins: what owning ends the game at once, by the setting that turns it on
const WINS = [['winTransit', '4 lignes de transport', 'les posséder toutes'], ['winGroups', '3 quartiers complets', 'trois groupes de couleur entiers'],
  ['winSide', 'Un côté complet', "tous les quartiers d'un même côté du plateau"]];
// WIN_BY says how an instant win was won, by the reason the server gives (winBy)
export const WIN_BY = { transit: 'toutes les lignes de transport', groups: '3 quartiers complets', side: "tous les quartiers d'un côté" };
const END_HINT = { rounds: 'le plus riche après N tours de table', time: 'le plus riche quand le temps est écoulé', survivor: 'le dernier à ne pas avoir fait faillite' };

function settingsForm(app, owner) {
  const s = owner ? app.draft : app.room.settings;
  const row = (label, ctl, hint = '') => `<div class="set"><span class="k">${label}${hint ? '<small>' + hint + '</small>' : ''}</span>${ctl}</div>`;
  return [
    row('Fin de partie', owner ? seg('end', END, s.end) : `<span class="v">${END.find(e => e[0] === s.end)[1]}</span>`, END_HINT[s.end]),
    s.end === 'rounds' ? row('Tours de table', stepper('rounds', s, owner)) : s.end === 'time' ? row('Durée', stepper('minutes', s, owner, ' min')) : '',
    row('Argent de départ', stepper('startCash', s, owner)),
    row('Salaire au Départ', stepper('salary', s, owner)),
    row('Temps pour décider', stepper('turnSeconds', s, owner, ' s'), 'puis le jeu joue à ta place'),
    row('Temps par enchère', stepper('auctionSeconds', s, owner, ' s'), 'sans nouvelle offre'),
    ...WINS.map(([k, label, hint]) => row('🏆 ' + label, owner ? seg(k, [[true, 'Oui'], [false, 'Non']], s[k]) : `<span class="v">${s[k] ? 'Oui' : 'Non'}</span>`,
      'victoire immédiate : ' + hint)),
  ].join('');
}

function roomScreen(app) {
  const { nm } = names(app), r = app.room, owner = r.owner === app.me, s = owner ? app.draft : r.settings;
  const link = location.origin + location.pathname + '#' + r.code;
  const here = r.players.filter(p => p.connected).length;
  const seats = r.players.map(p => `<li class="${p.id === app.me ? 'mine' : ''} ${p.connected ? '' : 'away'}">${ball(app.colors[p.color])}
      <span class="nm">${esc(nm(p.id))}</span>${p.connected ? '' : '<small>absent</small>'}${p.id === r.owner ? '<span class="crown" title="crée la partie">👑</span>' : ''}
      ${owner && p.id !== app.me ? `<button class="x" data-do="kick" data-id="${p.id}" title="Exclure ${esc(nm(p.id))}">✕</button>` : ''}</li>`).join('') +
    '<li class="free"></li>'.repeat(Math.max(0, s.max - r.players.length));
  return `<div class="lobby">
    <section class="card top">
      <span class="brand-mini">Quartiers</span>
      ${owner ? `<input id="room-name" class="field name" data-set="name" maxlength="24" value="${esc(s.name || '')}" placeholder="Salle ${r.code}" aria-label="Nom de la salle">`
        : `<h2 class="name">${esc(r.name || 'Salle ' + r.code)}</h2>`}
      ${owner ? seg('public', [[false, '🔒 Privée'], [true, '🌐 Publique']], s.public) : `<span class="state">${r.settings.public ? '🌐 Publique' : '🔒 Privée'}</span>`}
      <div class="ticket"><span>Code</span><b>${r.code}</b></div>
      <button class="btn sun small" data-do="copy" data-text="${esc(link)}">Copier le lien</button>
      <button class="btn small" data-do="leave">Quitter</button>
    </section>
    <div class="side">
      <section class="card me"><h2>Ton joueur</h2>${profile(app)}</section>
      <section class="card players"><div class="phead"><h2>Joueurs ${here}/${s.max}</h2>${owner ? stepper('max', s, true) : ''}</div><ul class="seats">${seats}</ul></section>
    </div>
    <section class="card settings"><h2>La partie</h2>${settingsForm(app, owner)}
      <button class="link" data-do="rules">Comment on joue ?</button>
      <div class="go">${owner ? '<button class="btn red big" data-do="start">Lancer la partie</button>' : '<p class="waiting">👑 Le créateur de la salle lance la partie</p>'}</div>
    </section>
  </div>`;
}

// --- during a game ---------------------------------------------------------------------------

function top(app) {
  const g = app.game, owner = app.room.owner === app.me;
  return `<div class="glass topbar">
    <span class="round">Tour <b>${g.round}</b>${g.maxRounds ? ' / ' + g.maxRounds : ''}</span>
    ${app.endsAt ? '<span class="pill">⏳ <b id="endclock"></b></span>' : ''}
    <span class="pill muted">Salle ${app.room.code}</span>
    <span class="grow"></span>
    <button class="btn small" data-do="rules" title="Comment on joue">?</button>
    ${owner && g.phase !== 'over' ? '<button class="btn small" data-do="stop">Arrêter</button>' : ''}
    <button class="btn small" data-do="leave">Quitter</button>
  </div>`;
}

// the color of a square's deed: its group's for a street
const deedColor = s => s.kind === 'street' ? GROUP_COLORS[s.group] : '#2b2d42';

function players(app) {
  const { nm, colorOf, person } = names(app), g = app.game, b = app.board;
  const props = id => g.deeds.filter(d => d.owner === id).sort((x, y) => x.square - y.square).map(d => `<button class="prop ${d.mortgaged ? 'mort' : ''} ${d.square === app.selected ? 'on' : ''}"
    style="--c:${deedColor(b.squares[d.square])}" data-do="pick" data-sq="${d.square}" title="${esc(b.squares[d.square].name)}"></button>`).join('');
  return g.players.map(p => `<div class="glass pc ${p.id === g.turn && g.phase !== 'over' ? 'turn' : ''} ${p.bankrupt ? 'out' : ''} ${p.id === app.me ? 'me' : ''}">
    <i class="token" style="--c:${colorOf(p.id)}"></i>
    <div class="grow"><b><span class="nm">${esc(nm(p.id))}</span>${p.id === app.me ? '<span class="you">toi</span>' : ''}</b>
      <small title="patrimoine : argent, propriétés et constructions"><span class="lbl">patrimoine </span>${fmt(p.worth)}</small></div>
    <div class="right">${money(p.cash)}<span class="badges">${person(p.id)?.connected === false ? '<span title="absent">💤</span>' : ''}${p.inJail ? '🚓' : ''}${'🃏'.repeat(p.jailCards)}${p.bankrupt ? '💥' : ''}</span></div>
    <div class="props">${props(p.id)}</div>
  </div>`).join('');
}

// The levels of a street: 0 bare, then houses, b.maxLevel the tower.
const LEVELS = ['Terrain', '🏠', '🏠🏠', '🏠🏠🏠', '🏢 Tour'];

// reach is the highest level player me may build a street at level up to, on landing there (or
// buying it, from 0): 2 houses before passing the start, then 3; the tower on a street at 3 houses.
// The same as engine.Game.reach.
function reach(b, me, level) {
  if (level >= b.maxLevel - 1) return b.maxLevel;
  return me.lapped ? b.maxLevel - 1 : Math.min(2, b.maxLevel - 1);
}

// builtCost is what the levels from+1 to to of street s cost together.
function builtCost(b, s, from, to) {
  let c = 0;
  for (let l = from + 1; l <= to; l++) c += l === b.maxLevel ? s.towerCost : s.buildCost;
  return c;
}

// levelButtons are the buttons to take street sq from level from to each level it may reach.
function levelButtons(b, me, sq, a, from, base = 0) {
  const s = b.squares[sq];
  let html = '';
  for (let l = from === 0 && a === 'buy' ? 0 : from + 1; l <= reach(b, me, from); l++) {
    const cost = base + builtCost(b, s, from, l);
    html += `<button class="btn big ${(a === 'buy' ? l === 0 : l === from + 1) ? 'main' : ''}" data-do="act" data-a="${a}" data-sq="${sq}" data-level="${l}" ${me.cash < cost ? 'disabled' : ''}>${LEVELS[l]} ${money(cost)}</button>`;
  }
  return html;
}

function actions(app) {
  const { nm } = names(app), g = app.game, b = app.board;
  const me = g.players.find(p => p.id === app.me);
  const clock = '<span id="clock" class="clock"></span>';
  const btn = (a, label, cls = '') => `<button class="btn big ${cls}" data-do="act" data-a="${a}">${label}</button>`;
  // what is happening, then the buttons: they wrap as one block
  const box = (info, btns = '', cls = '') => `<div class="glass action ${cls}">${info ? '<span class="info">' + info + '</span>' : ''}${btns ? '<div class="btns">' + btns + clock + '</div>' : clock}</div>`;
  if (g.phase === 'over') {
    return box(`🏆 <b>${esc(nm(g.winner))}</b> gagne !`, '<button class="btn" data-do="ranking">Classement</button><button class="btn big main" data-do="lobby">Retour au salon</button>', 'mine');
  }
  if (!me) return box(`Tu regardes la partie. À <b>${esc(nm(g.turn))}</b> de jouer.`);
  if (me.bankrupt) return box('Tu as fait faillite… La partie continue sans toi.');
  const mine = g.turn === app.me;
  // standing on a street of mine I may build on, this landing only
  const build = () => mine && g.here >= 0 && g.here === me.pos ? levelButtons(b, me, g.here, 'build', g.deeds.find(d => d.square === g.here).level) : '';
  const buildInfo = () => build() ? `Construire sur <b>${esc(b.squares[g.here].name)}</b> ?` : '';
  switch (g.phase) {
    case 'roll':
      if (mine) {
        const again = g.doubles > 0 && !me.inJail;
        const info = again ? '<b>Double !</b> Tu relances les dés.' + (g.doubles === 2 ? ' <span class="warn">Encore un double et c\'est le poste de police.</span>' : '') :
          me.inJail ? 'Au poste : fais un double pour sortir, ou paie.' : '';
        return box([info, buildInfo()].filter(Boolean).join(' '), build() + btn('roll', again ? '🎲 Relancer les dés' : '🎲 Lancer les dés', 'main') + (me.inJail ? btn('payjail', 'Payer ' + b.jailFine) + (me.jailCards ? btn('jailcard', '🃏 Carte') : '') : ''), 'mine');
      }
      if (g.doubles > 0) return box(`<b>${esc(nm(g.turn))}</b> a fait un double et relance les dés.`);
      break;
    case 'buy': if (mine) {
      const s = b.squares[me.pos];
      if (s.kind !== 'street') return box(esc(s.name), btn('buy', 'Acheter ' + money(s.price), 'main') + btn('decline', 'Mettre aux enchères'), 'mine');
      const later = me.lapped ? '' : ' <span class="muted">(3 maisons après ton premier passage au Départ)</span>';
      return box(`<b>${esc(s.name)}</b> : le terrain, ou avec des maisons ?${later}`, levelButtons(b, me, me.pos, 'buy', 0, s.price) + btn('decline', 'Mettre aux enchères'), 'mine');
    } break;
    case 'auction': {
      const a = g.auction;
      const head = `🔨 <b>${esc(b.squares[a.square].name)}</b> · ${a.high ? money(a.high) + ' par ' + esc(nm(a.bidder)) : 'mise à prix ' + money(a.min)}`;
      if (a.passed.includes(app.me)) return box(head, '<span class="muted">Tu as passé.</span>');
      if (a.bidder === app.me) return box(head, '<span class="lead">Tu mènes !</span>');
      // With no bid yet, the quick bids start from the minimum: half the price.
      const quick = (a.high ? [10, 50, 100] : [0, 50, 100]).map(n =>
        `<button class="btn" data-do="bidQuick" data-n="${n}">${a.high ? '+' + n : fmt(a.min + n)}</button>`).join('');
      return box(head, `${quick}<input id="bid" class="field" type="number" data-bind="bid" value="${app.bid}">
        <button class="btn big main" data-do="bid">Enchérir</button>${btn('pass', 'Passer')}`);
    }
    case 'debt': if (mine) {
      return box(`Tu dois <b>${fmt(g.debt.amount)}</b> à ${esc(nm(g.debt.to))} : vends ou hypothèque (clique tes propriétés), puis`,
        btn('pay', 'Payer', 'main') + btn('bankrupt', 'Faillite', 'danger'), 'mine');
    } break;
    case 'end': if (mine) return box(buildInfo(), build() + btn('end', 'Fin du tour ➜', build() ? '' : 'main'), 'mine');
  }
  return box(`À <b>${esc(nm(g.turn))}</b> de jouer…`);
}

function deed(app) {
  if (app.selected < 0) return '';
  const { nm, colorOf } = names(app), b = app.board, s = b.squares[app.selected], g = app.game;
  const d = g.deeds.find(x => x.square === app.selected);
  const head = s.kind === 'street' ? GROUP_COLORS[s.group] : '#2b2d42';
  let rows = '';
  if (s.kind === 'street') {
    rows = [['Loyer', s.rent[0]], ['Groupe complet', s.rent[0] * 2], ['🏠', s.rent[1]], ['🏠🏠', s.rent[2]], ['🏠🏠🏠', s.rent[3]], ['🏢 Tour', s.rent[4]]]
      .map(([l, v], i) => `<tr class="${d && (i === 0 ? d.level === 0 : i >= 2 && d.level === i - 1) ? 'cur' : ''}"><td>${l}</td><td>${fmt(v)}</td></tr>`).join('') +
      `<tr class="sub"><td>Maison</td><td>${fmt(s.buildCost)}</td></tr><tr class="sub"><td>Tour</td><td>${fmt(s.towerCost)}</td></tr>`;
  } else if (s.kind === 'transit') {
    rows = b.transitRent.map((v, i) => `<tr><td>${i + 1} ligne${i ? 's' : ''}</td><td>${v}</td></tr>`).join('');
  } else if (s.kind === 'utility') {
    rows = b.utilityFactor.map((v, i) => `<tr><td>${i + 1} compagnie${i ? 's' : ''}</td><td>dés × ${v}</td></tr>`).join('');
  } else if (s.kind === 'tax') rows = `<tr><td>À payer</td><td>${s.tax}</td></tr>`;
  if (s.price) rows += `<tr class="sub"><td>Hypothèque</td><td>${s.price / 2}</td></tr>`;
  let acts = '';
  const me = g.players.find(p => p.id === app.me);
  if (d && d.owner === app.me && me && !me.bankrupt && g.phase !== 'over') {
    const a = (x, l) => `<button class="btn small" data-do="act" data-a="${x}" data-sq="${app.selected}">${l}</button>`;
    if (s.kind === 'street' && d.level > 0) acts += a('sell', 'Revendre un niveau');
    acts += d.mortgaged ? a('unmortgage', 'Lever l\'hypothèque') : a('mortgage', 'Hypothéquer');
  }
  return `<div class="glass deedcard">
    <div class="deedhead" style="--c:${head}"><b>${esc(s.name)}</b>${s.price ? money(s.price) : ''}<button class="x" data-do="unselect">✕</button></div>
    <div class="owner">${d ? `<i class="token" style="--c:${colorOf(d.owner)}"></i> ${esc(nm(d.owner))}${d.mortgaged ? ' · <b>hypothéquée</b>' : ''}` : s.price ? '<span class="muted">À vendre</span>' : ''}</div>
    ${acts ? '<div class="acts">' + acts + '</div>' : ''}
    ${rows ? '<table>' + rows + '</table>' : ''}
  </div>`;
}

function trades(app) {
  const { nm } = names(app), g = app.game, b = app.board;
  const me = g.players.find(p => p.id === app.me);
  if (!me || me.bankrupt || g.phase === 'over') return '';
  const side = (sq, cash) => [...sq.map(i => esc(b.squares[i].name)), cash ? money(cash) : ''].filter(Boolean).join(', ') || 'rien';
  const list = g.trades.filter(t => t.from === app.me || t.to === app.me).map(t => `<div class="trade ${t.to === app.me ? 'forme' : ''}">
    <div><b>${esc(nm(t.from))}</b> donne ${side(t.give, t.giveCash)}</div>
    <div>contre ${side(t.get, t.getCash)} de <b>${esc(nm(t.to))}</b></div>
    <div class="acts">${t.to === app.me
      ? `<button class="btn small main" data-do="act" data-a="accept" data-trade="${t.id}">Accepter</button><button class="btn small" data-do="act" data-a="reject" data-trade="${t.id}">Refuser</button>`
      : `<button class="btn small" data-do="act" data-a="cancel" data-trade="${t.id}">Retirer</button>`}</div></div>`).join('');
  return `<div class="glass trades"><div class="row"><b class="grow">Échanges</b><button class="btn small" data-do="openTrade">🤝 Proposer</button></div>${list}</div>`;
}

// the trade editor, in the modal
export function tradeModal(app) {
  const { nm, colorOf } = names(app), g = app.game, b = app.board, t = app.trade;
  const others = g.players.filter(o => o.id !== app.me && !o.bankrupt);
  if (!others.some(o => o.id === t.to)) Object.assign(t, { to: others[0]?.id || 0, get: [] });
  const me = g.players.find(p => p.id === app.me), them = g.players.find(p => p.id === t.to);
  const boxes = (owner, key) => g.deeds.filter(d => d.owner === owner).map(d => {
    const s = b.squares[d.square];
    return `<label class="chip ${t[key].includes(d.square) ? 'on' : ''}" style="--c:${s.group ? GROUP_COLORS[s.group] : '#2b2d42'}">
      <input type="checkbox" data-trade-sq="${key}" value="${d.square}" ${t[key].includes(d.square) ? 'checked' : ''}>${esc(s.name)}</label>`;
  }).join('') || '<span class="muted">aucune propriété</span>';
  return `<div class="card modalbox trade-editor">
    <h2>Proposer un échange</h2>
    <div class="row">avec ${others.map(o => `<button class="btn small ${o.id === t.to ? 'main' : ''}" data-do="tradeTo" data-id="${o.id}"><i class="token" style="--c:${colorOf(o.id)}"></i> ${esc(nm(o.id))}</button>`).join('')}</div>
    <div class="cols">
      <section><h3>Je donne</h3>${boxes(app.me, 'give')}<label class="cash">+ <input id="trade-giveCash" class="field" type="number" min="0" max="${me.cash}" data-bind="giveCash" value="${t.giveCash}"> ${money(me.cash)} dispo</label></section>
      <section><h3>Je reçois</h3>${them ? boxes(t.to, 'get') : ''}<label class="cash">+ <input id="trade-getCash" class="field" type="number" min="0" data-bind="getCash" value="${t.getCash}"> ${them ? money(them.cash) : ''} chez lui</label></section>
    </div>
    <div class="bar"><button class="btn" data-do="closeModal">Annuler</button><button class="btn big main" data-do="offer">Envoyer l'offre</button></div>
  </div>`;
}

export function overModal(app) {
  const { nm, colorOf } = names(app), g = app.game;
  const p = id => g.players.find(x => x.id === id);
  const worth = id => p(id).bankrupt ? '<span class="muted">faillite</span>' : money(p(id).worth);
  const top = g.ranking.slice(0, 3);
  const step = i => top[i] === undefined ? '' : `<div class="step p${i + 1}"><div class="who">${ball(colorOf(top[i]), 'big')}<b>${esc(nm(top[i]))}</b>${worth(top[i])}</div>
    <div class="block">${i + 1}</div></div>`;
  const rest = g.ranking.slice(3).map((id, i) => `<li><span class="rk">${i + 4}</span>${ball(colorOf(id))}<span class="nm">${esc(nm(id))}</span>${worth(id)}</li>`).join('');
  return `<div class="card modalbox podium">
    <h2 class="title">🏆 ${esc(nm(g.winner))} gagne !</h2>
    ${g.winBy ? `<p class="muted">avec ${WIN_BY[g.winBy]}</p>` : ''}
    <div class="steps">${step(1)}${step(0)}${step(2)}</div>
    ${rest ? '<ol class="rest">' + rest + '</ol>' : ''}
    <div class="bar"><button class="btn" data-do="closeModal">Voir le plateau</button><button class="btn red big" data-do="lobby">Retour au salon</button></div>
  </div>`;
}

// rulesModal is how the game is played, in short
export function rulesModal(app) {
  const s = app.room?.settings, b = app.board;
  const end = !s ? 'au bout d\'un nombre de tours ou d\'un temps choisi, le plus riche gagne ; ou au dernier survivant' :
    s.end === 'rounds' ? 'après ' + s.rounds + ' tours de table, le plus riche gagne (argent, propriétés et constructions)' :
    s.end === 'time' ? 'au bout de ' + s.minutes + ' minutes, le plus riche gagne (argent, propriétés et constructions)' : 'le dernier joueur à ne pas avoir fait faillite gagne';
  const wins = !s ? [] : WINS.filter(([k]) => s[k]).map(([, label]) => label.toLowerCase());
  return `<div class="card modalbox rules">
    <h2>Comment on joue</h2>
    <ul>
      <li><b>À ton tour</b>, lance les dés et avance. Sur une case libre, achète-la, ou elle part aux enchères pour tout le monde.</li>
      <li><b>Un double</b> (les deux dés pareils) : après ton déplacement, tu relances. Au <b>3ᵉ double d'affilée</b>, direction le poste de police, sans bouger.</li>
      <li><b>Au poste</b>, pour sortir : fais un double, paie ${b.jailFine} ou joue une carte. Après ${b.jailTurns} essais, l'amende est due.</li>
      <li><b>Maisons</b> : en achetant une rue, prends-la nue ou avec 1 ou 2 maisons (3 après ton premier passage au Départ). Ensuite, chaque fois que tu retombes sur ta rue, ajoute des maisons, sans avoir besoin de tout le groupe. La tour se construit sur une rue à 3 maisons, en y retombant.</li>
      <li><b>Loyers</b> : doublés sur une rue sans maison quand tu as tout son groupe de couleur.</li>
      <li><b>Le Départ</b> rapporte ${s ? s.salary : 'un salaire'} à chaque passage.</li>
      <li><b>Une dette</b> : vends, hypothèque ou échange pour payer, sinon c'est la faillite.</li>
      <li><b>Échanges</b> : propriétés et argent, à tout moment, avec n'importe qui.</li>
      <li><b>Minuteur</b> : si tu tardes, le jeu joue à ta place.</li>
      <li><b>Fin</b> : ${end}.</li>
      ${wins.length ? `<li><b>Victoire immédiate</b> : ${wins.join(', ou ')}.</li>` : ''}
    </ul>
    <div class="bar"><span></span><button class="btn sun" data-do="closeModal">Compris !</button></div>
  </div>`;
}

// --- flashes ---------------------------------------------------------------------------------

export function toast(text) {
  const el = $('toast');
  el.textContent = text;
  el.classList.add('show');
  clearTimeout(toast.t);
  toast.t = setTimeout(() => el.classList.remove('show'), 2600);
}

// banner shows a big line in the middle of the screen for a moment
export function banner(html, secs = 1.3) {
  const el = $('banner');
  el.innerHTML = html;
  el.classList.remove('show');
  void el.offsetWidth;
  el.classList.add('show');
  return new Promise(r => setTimeout(r, secs * 1000));
}

// card shows a drawn card until clicked, or for a few seconds
export function card(app, text, who) {
  const { nm } = names(app);
  const el = $('card');
  el.innerHTML = `<div class="cardface"><div class="cardhead">❓ Rumeur</div><p>${esc(text)}</p><small>pour ${esc(nm(who))}</small></div>`;
  el.hidden = false;
  return new Promise(r => {
    const done = () => { el.hidden = true; el.onclick = null; clearTimeout(timer); r(); };
    const timer = setTimeout(done, 2600);
    el.onclick = done;
  });
}
