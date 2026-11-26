// A bare test client for the server: no build, no framework. Each tab is a player (the token is
// kept per tab), so a whole game can be played alone in several tabs.
'use strict';

const token = sessionStorage.token || (sessionStorage.token = crypto.randomUUID());
let ws, me = 0, board = null, colors = [], room = null, game = null, seq = 0;
let deadlineAt = 0, endsAt = 0, selected = -1, log = [], publicRooms = null;
let trade = { to: 0, give: [], get: [], giveCash: 0, getCash: 0 }, draft = null;

const groupColors = ['', '#8d5524', '#2ec4f1', '#ff6fb5', '#9b5de5', '#ff8a1f', '#f2c014', '#e63946', '#3a6ff7'];
const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
const status = s => { document.getElementById('status').textContent = s || ''; };

// --- connection -----------------------------------------------------------------------------

function connect() {
  ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
  ws.onopen = () => send({ t: 'hello', token });
  ws.onmessage = e => receive(JSON.parse(e.data));
  ws.onclose = e => {
    if (e.code === 4000) return status('Ouvert dans un autre onglet.');
    status('Déconnecté, reconnexion…');
    setTimeout(connect, 1000);
  };
}
const send = m => ws && ws.readyState === 1 && ws.send(JSON.stringify(m));
const act = (a, extra = {}) => send({ t: 'act', a, ...extra });

function receive(m) {
  switch (m.t) {
    case 'welcome':
      me = m.id; board = m.board; colors = m.colors;
      status('');
      if (sessionStorage.name) send({ t: 'profile', name: sessionStorage.name, color: -1 });
      if (!room && location.hash.length > 1) send({ t: 'join', code: location.hash.slice(1) });
      break;
    case 'rooms': publicRooms = m.rooms; break;
    case 'room':
      room = m;
      if (!draft) draft = { ...m.settings, name: m.name };
      history.replaceState(null, '', '#' + m.code);
      break;
    case 'game':
      if (m.seq !== seq) { seq = m.seq; log = []; selected = -1; }
      game = m.state;
      deadlineAt = m.deadline ? Date.now() + m.deadline : 0;
      endsAt = m.endsIn ? Date.now() + m.endsIn : 0;
      for (const e of m.events) log.unshift(describe(e));
      break;
    case 'stopped': game = null; log.unshift('La partie a été arrêtée.'); break;
    case 'left':
      room = game = draft = null;
      history.replaceState(null, '', location.pathname);
      status({ kicked: 'Exclu de la salle.', closed: 'Salle fermée.' }[m.reason] || '');
      break;
    case 'err':
      status(m.code === 'act' ? 'Refusé : ' + m.reason : 'Erreur : ' + m.code);
      setTimeout(() => status(''), 3000);
      return;
  }
  render();
}

// --- helpers --------------------------------------------------------------------------------

const person = id => room && room.players.find(p => p.id === id);
const nm = id => id === 0 ? 'la banque' : (person(id)?.name || 'Joueur ' + id);
const colorOf = id => person(id) ? colors[person(id).color] : '#999';
const sqName = i => board.squares[i].name;
const deed = i => game.deeds.find(d => d.square === i);
const mine = () => game && game.players.find(p => p.id === me);

function describe(e) {
  const w = nm(e.who), t = nm(e.to), s = board.squares[e.square]?.name;
  switch (e.kind) {
    case 'turn': return '— Tour de ' + w;
    case 'rolled': return w + ' lance ' + e.dice.join(' + ');
    case 'salary': return w + ' touche ' + e.amount + ' au Départ';
    case 'moved': return w + ' arrive sur ' + s;
    case 'jailed': return w + ' est emmené au poste';
    case 'freed': return w + ' sort du poste' + (e.amount ? ' en payant ' + (e.amount) : '');
    case 'rent': return w + ' doit ' + e.amount + ' de loyer à ' + t + ' (' + s + ')';
    case 'tax': return w + ' doit ' + e.amount + ' (' + s + ')';
    case 'card': return w + ' pioche : « ' + board.cards[e.card].text + ' »';
    case 'debt': return w + ' doit ' + e.amount + ' à ' + t + ' et doit trouver l\'argent';
    case 'paid': return w + ' paie ' + e.amount + ' à ' + t;
    case 'bought': return w + ' achète ' + s + ' pour ' + e.amount;
    case 'declined': return w + ' n\'achète pas ' + s;
    case 'bonus': return w + ' tombe pile sur le Départ : ' + (e.by === 'roll' ? 'un lancer de plus' : 'construction sur la rue de son choix');
    case 'built': return w + ' construit sur ' + s + ' (niveau ' + e.level + ')';
    case 'resold': return w + ' revend ' + s + ' à la banque (+' + e.amount + ')';
    case 'offer': return w + ' propose un échange à ' + t;
    case 'traded': return w + ' accepte l\'échange de ' + t;
    case 'rejected': return w + ' refuse l\'échange de ' + t;
    case 'cancelled': return w + ' retire son offre à ' + t;
    case 'bankrupt': return w + ' fait faillite' + (e.to ? ' au profit de ' + t : '');
    case 'over': return 'Partie terminée : ' + w + ' gagne' + (e.by ? ' (' + { transit: 'toutes les lignes', groups: '3 quartiers complets', side: 'un côté complet' }[e.by] + ')' : '') + ' !';
  }
  return e.kind;
}

// --- rendering ------------------------------------------------------------------------------

function render() {
  // keep the focus and caret of the field being typed in across the redraw
  const f = document.activeElement, fid = f && f.id;
  let caret = null;
  try { caret = [f.selectionStart, f.selectionEnd]; } catch { }
  const app = document.getElementById('app');
  app.innerHTML = !board ? '<p>Connexion…</p>' : !room ? lobby() : game ? gameView() : roomView();
  if (fid && document.getElementById(fid)) {
    const el = document.getElementById(fid);
    el.focus();
    try { if (caret && caret[0] != null) el.setSelectionRange(...caret); } catch { }
  }
  tickTimer();
}

function lobby() {
  const list = publicRooms == null ? '' : publicRooms.length === 0 ? '<p class="muted">Aucune salle publique.</p>' :
    publicRooms.map(r => `<div>${esc(r.name || 'Salle ' + r.code)} — ${r.players}/${r.max} ${r.playing ? '(en jeu)' : ''}
      <button data-do="joinListed" data-code="${r.code}">Rejoindre</button></div>`).join('');
  return `<div class="lobby">
    <div class="box"><h3>Ton nom</h3>
      <input id="name" maxlength="16" value="${esc(sessionStorage.name || '')}" placeholder="Joueur ${me}">
      <button data-do="name">OK</button></div>
    <div class="box"><h3>Nouvelle salle</h3><button class="main" data-do="create">Créer une salle</button></div>
    <div class="box"><h3>Rejoindre par code</h3>
      <input id="code" maxlength="5" placeholder="ABCDE" style="text-transform:uppercase">
      <button data-do="join">Rejoindre</button></div>
    <div class="box"><h3>Salles publiques</h3><button data-do="rooms">Rafraîchir</button>${list}</div>
  </div>`;
}

function settingsForm(owner) {
  const s = owner ? draft : room.settings;
  const dis = owner ? '' : 'disabled';
  const num = (k, label) => `<label>${label} <input type="number" id="set-${k}" data-set="${k}" value="${s[k]}" ${dis}></label>`;
  return `<div class="settings">
    <label><input type="checkbox" id="set-public" data-set="public" ${s.public ? 'checked' : ''} ${dis}> publique</label>
    ${num('max', 'joueurs max')}
    <label>fin <select id="set-end" data-set="end" ${dis}>
      ${[['rounds', 'au nombre de tours'], ['time', 'au temps'], ['survivor', 'dernier survivant']].map(([v, l]) =>
        `<option value="${v}" ${s.end === v ? 'selected' : ''}>${l}</option>`).join('')}</select></label>
    ${num('rounds', 'tours')}${num('minutes', 'minutes')}<br>
    ${num('startCash', 'argent de départ')}${num('salary', 'salaire')}<br>
    ${num('turnSeconds', 's par décision')}<br>
    victoire immédiate :${[['winTransit', '4 lignes'], ['winGroups', '3 quartiers'], ['winSide', 'un côté']].map(([k, l]) =>
      ` <label><input type="checkbox" id="set-${k}" data-set="${k}" ${s[k] ? 'checked' : ''} ${dis}> ${l}</label>`).join('')}
    ${owner ? '<br><button data-do="settings">Appliquer</button>' : ''}
  </div>`;
}

function roomView() {
  const owner = room.owner === me;
  const link = location.origin + location.pathname + '#' + room.code;
  return `<div class="lobby">
    <div class="box"><h3>Salle ${esc(room.name || room.code)} — code <b>${room.code}</b></h3>
      <span class="muted">Lien : ${esc(link)}</span></div>
    <div class="box"><h3>Joueurs</h3>
      ${room.players.map(p => `<div><span class="pawn" style="background:${colors[p.color]}"></span>
        ${esc(nm(p.id))}${p.id === me ? ' (toi)' : ''}${p.id === room.owner ? ' ★' : ''}${p.connected ? '' : ' <span class="muted">(absent)</span>'}
        ${owner && p.id !== me ? `<button data-do="kick" data-id="${p.id}">exclure</button>` : ''}</div>`).join('')}
    </div>
    <div class="box"><h3>Réglages</h3>${settingsForm(owner)}</div>
    ${owner ? '<button class="main" data-do="start">Lancer la partie</button>' : '<p class="muted">Le propriétaire (★) lance la partie.</p>'}
    <button data-do="leave">Quitter la salle</button>
  </div>`;
}

function cell(i) {
  const s = board.squares[i];
  const k = board.squares.length / 4; // squares per side, its corner first
  const [row, col] = i < k ? [k + 1, k + 1 - i] : i < 2 * k ? [k + 1 - (i - k), 1] : i < 3 * k ? [1, i - 2 * k + 1] : [i - 3 * k + 1, k + 1];
  const d = deed(i);
  const pawns = game.players.filter(p => !p.bankrupt && p.pos === i)
    .map(p => `<span class="pawn" style="background:${colorOf(p.id)}" title="${esc(nm(p.id))}"></span>`).join('');
  const lvl = !d || !d.level ? '' : d.level === board.maxLevel ? '🏢' : '🏠'.repeat(d.level);
  return `<div class="sq ${selected === i ? 'sel' : ''}" style="grid-row:${row};grid-column:${col}" data-do="select" data-sq="${i}">
    ${s.group ? `<div class="band" style="background:${groupColors[s.group]}"></div>` : ''}
    <div>${esc(s.name)}</div>${s.price ? `<div class="muted">${s.price}</div>` : ''}<div class="lvl">${lvl}</div>
    <div class="pawns">${pawns}</div>
    ${d ? `<div class="owner" style="background:${colorOf(d.owner)}"></div>` : ''}
  </div>`;
}

function squareDetail() {
  if (selected < 0) return '<p class="muted">Clique une case pour la voir (et la gérer si elle est à toi).</p>';
  const s = board.squares[selected], d = deed(selected);
  let info = '';
  if (s.kind === 'street') info = `Loyer : ${s.rent[0]} (groupe complet : ${2 * s.rent[0]}), puis ${s.rent.slice(1).join(' / ')} (la dernière : tour), × 1,5 avec le groupe complet.
    Construction : ${s.buildCost} la maison, ${s.towerCost} la tour.`;
  if (s.kind === 'transit') info = 'Loyer selon les lignes possédées : ' + board.transitRent.join(' / ') + '.';
  if (s.kind === 'utility') info = 'Loyer : dés × ' + board.utilityFactor.join(' (ou ×') + ' avec les deux).';
  if (s.kind === 'tax') info = 'À payer : ' + s.tax + '.';
  let actions = '';
  if (d && d.owner === me && game.phase === 'debt' && game.turn === me && mine().cash < game.debt.amount) {
    actions = `<button data-do="resell" data-sq="${selected}">Revendre à la banque (+${d.resale})</button>`;
  }
  return `<h3>${esc(s.name)}</h3><div>${s.price ? 'Prix ' + s.price + '. ' : ''}${info}</div>
    <div>${d ? 'À ' + esc(nm(d.owner)) + (d.level ? ', niveau ' + d.level : '') : s.price ? 'Libre.' : ''}</div>${actions}`;
}

function actions() {
  const p = mine();
  if (!p) return '<p class="muted">Tu regardes la partie.</p>';
  if (p.bankrupt) return '<p class="muted">Tu as fait faillite.</p>';
  if (game.phase === 'over') return '';
  const myTurn = game.turn === me, b = (a, l, main) => `<button ${main ? 'class="main"' : ''} data-do="act" data-a="${a}">${l}</button>`;
  // on a street of mine I just landed on, or any street of mine after landing right on the start
  // and drawing building: build up to a level (the tower only from 3 houses)
  const upgradesOf = sq => {
    const s = board.squares[sq], lvl = game.deeds.find(d => d.square === sq).level;
    const top = lvl >= board.maxLevel - 1 ? board.maxLevel : p.lapped ? board.maxLevel - 1 : Math.min(2, board.maxLevel - 1);
    let html = '', cost = 0;
    for (let l = lvl + 1; l <= top; l++) {
      cost += l === board.maxLevel ? s.towerCost : s.buildCost;
      html += `<button data-do="act" data-a="build" data-sq="${sq}" data-level="${l}">${l === board.maxLevel ? 'Tour' : l + ' maison' + (l > 1 ? 's' : '')} (${cost})</button>`;
    }
    return html ? 'Construire sur ' + esc(s.name) + ' : ' + html + '<br>' : '';
  };
  const upgrades = () => {
    if (!myTurn) return '';
    if (game.here >= 0 && game.here === p.pos) return upgradesOf(game.here);
    if (!game.buildAnywhere) return '';
    return 'Départ pile, au choix :<br>' + game.deeds.filter(d => d.owner === me && board.squares[d.square].kind === 'street')
      .map(d => upgradesOf(d.square)).join('');
  };
  switch (game.phase) {
    case 'roll':
      if (!myTurn) break;
      return upgrades() + b('roll', 'Lancer les dés', true) + (p.inJail ? b('payjail', 'Payer ' + board.jailFine) + (p.jailCards ? b('jailcard', 'Utiliser la carte') : '') : '');
    case 'buy': {
      if (!myTurn) break;
      const s = board.squares[p.pos];
      const top = s.kind !== 'street' ? 0 : p.lapped ? board.maxLevel - 1 : Math.min(2, board.maxLevel - 1);
      let buys = '';
      for (let l = 0; l <= top; l++) buys += `<button ${l ? '' : 'class="main"'} data-do="act" data-a="buy" data-level="${l}">Acheter ${esc(s.name)}${l ? ' + ' + l + ' maison' + (l > 1 ? 's' : '') : ''} (${s.price + l * s.buildCost})</button>`;
      return buys + b('decline', 'Ne pas acheter');
    }
    case 'debt':
      if (!myTurn) break;
      return `Tu dois <b>${game.debt.amount}</b> à ${esc(nm(game.debt.to))}. S'il te manque de l'argent, revends (clique tes propriétés) ou échange, puis :<br>` +
        b('pay', 'Payer', true) + b('bankrupt', 'Déclarer faillite');
    case 'end':
      if (!myTurn) break;
      return upgrades() + b('end', 'Finir le tour', true);
  }
  return '<p class="muted">À ' + esc(nm(game.turn)) + ' de jouer.</p>';
}

function trades() {
  const p = mine();
  if (!p || p.bankrupt || game.phase === 'over') return '';
  const list = game.trades.filter(t => t.from === me || t.to === me).map(t => {
    const side = (sq, cash) => [...sq.map(sqName), cash ? cash + ' €' : ''].filter(Boolean).map(esc).join(', ') || 'rien';
    return `<div>${esc(nm(t.from))} donne <b>${side(t.give, t.giveCash)}</b> contre <b>${side(t.get, t.getCash)}</b> de ${esc(nm(t.to))}
      ${t.to === me ? `<button data-do="act" data-a="accept" data-trade="${t.id}">Accepter</button><button data-do="act" data-a="reject" data-trade="${t.id}">Refuser</button>`
        : `<button data-do="act" data-a="cancel" data-trade="${t.id}">Retirer</button>`}</div>`;
  }).join('');
  const others = game.players.filter(o => o.id !== me && !o.bankrupt);
  if (!others.some(o => o.id === trade.to)) trade = { to: others[0]?.id || 0, give: [], get: [], giveCash: 0, getCash: 0 };
  const boxes = (owner, key) => game.deeds.filter(d => d.owner === owner).map(d =>
    `<label><input type="checkbox" data-trade-sq="${key}" value="${d.square}" ${trade[key].includes(d.square) ? 'checked' : ''}> ${esc(sqName(d.square))}</label><br>`).join('') || '<span class="muted">rien</span>';
  return `<div class="box"><h3>Échanges</h3>${list}
    <details ${trade.open ? 'open' : ''} data-do="toggleTrade"><summary>Proposer un échange</summary>
      avec <select id="trade-to" data-bind="tradeTo">${others.map(o => `<option value="${o.id}" ${o.id === trade.to ? 'selected' : ''}>${esc(nm(o.id))}</option>`).join('')}</select>
      <table><tr><th>Je donne</th><th>Je reçois</th></tr><tr><td>${boxes(me, 'give')}</td><td>${boxes(trade.to, 'get')}</td></tr>
      <tr><td><input type="number" id="trade-giveCash" data-bind="giveCash" value="${trade.giveCash}"> €</td>
      <td><input type="number" id="trade-getCash" data-bind="getCash" value="${trade.getCash}"> €</td></tr></table>
      <button data-do="offer">Envoyer l'offre</button>
    </details></div>`;
}

function gameView() {
  const over = game.phase === 'over', owner = room.owner === me;
  const rows = game.players.map(p => `<tr class="${p.id === game.turn && !over ? 'turn' : ''} ${p.bankrupt ? 'out' : ''}">
    <td><span class="pawn" style="background:${colorOf(p.id)}"></span> ${esc(nm(p.id))}${p.id === me ? ' (toi)' : ''}${person(p.id)?.connected === false ? ' <span class="muted">(absent)</span>' : ''}</td>
    <td>${p.cash}</td><td class="muted">${p.worth}</td><td>${p.inJail ? '🚓' : ''}${p.jailCards ? '🃏'.repeat(p.jailCards) : ''}</td></tr>`).join('');
  const center = `<div class="center">
    <div>Tour ${game.round}${game.maxRounds ? ' / ' + game.maxRounds : ''} — phase <b>${game.phase}</b> <span class="timer" id="timer"></span></div>
    <div class="dice">${game.dice[0] ? '🎲 ' + game.dice.join(' ') : ''}</div>
    ${over ? `<h2>🏆 ${esc(nm(game.winner))} gagne !</h2><ol>${game.ranking.map(id => '<li>' + esc(nm(id)) + '</li>').join('')}</ol>
      <button data-do="back">Retour au salon</button>` : ''}
    <hr>${squareDetail()}</div>`;
  return `<div class="game">
    <div class="board">${board.squares.map((_, i) => cell(i)).join('')}${center}</div>
    <aside>
      <div class="box"><table><tr><th>Joueur</th><th>Cash</th><th>Patrimoine</th><th></th></tr>${rows}</table></div>
      <div class="box"><h3>À faire</h3>${actions()}</div>
      ${trades()}
      <div class="box"><h3>Journal</h3><div class="log">${log.map(l => '<div>' + esc(l) + '</div>').join('')}</div></div>
      ${owner && !over ? '<button data-do="stop">Arrêter la partie</button>' : ''}
      <button data-do="leave">Quitter la salle</button>
    </aside></div>`;
}

function tickTimer() {
  const el = document.getElementById('timer');
  if (!el) return;
  const parts = [];
  if (deadlineAt) parts.push('⏱ ' + Math.max(0, Math.ceil((deadlineAt - Date.now()) / 1000)) + ' s');
  if (endsAt) {
    const s = Math.max(0, Math.ceil((endsAt - Date.now()) / 1000));
    parts.push('fin dans ' + Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0'));
  }
  el.textContent = parts.join(' · ');
}
setInterval(tickTimer, 250);

// --- input ----------------------------------------------------------------------------------

const handlers = {
  name() {
    sessionStorage.name = document.getElementById('name').value.trim();
    send({ t: 'profile', name: sessionStorage.name, color: -1 });
  },
  create: () => send({ t: 'create' }),
  join: () => send({ t: 'join', code: document.getElementById('code').value.trim().toUpperCase() }),
  joinListed: d => send({ t: 'join', code: d.code, listed: true }),
  rooms: () => send({ t: 'rooms' }),
  leave() {
    if (game && game.phase !== 'over' && mine() && !mine().bankrupt && !confirm('Quitter pendant la partie = abandonner. Sûr ?')) return;
    send({ t: 'leave' });
  },
  settings() { const { name, ...settings } = draft; send({ t: 'settings', name, settings }); },
  start: () => send({ t: 'start' }),
  stop: () => confirm('Arrêter la partie pour tout le monde ?') && send({ t: 'stop' }),
  kick: d => send({ t: 'kick', id: +d.id }),
  back() { game = null; render(); },
  select(d) { selected = +d.sq; render(); },
  act: d => act(d.a, { square: d.sq ? +d.sq : 0, level: d.level ? +d.level : 0, trade: d.trade ? +d.trade : 0 }),
  resell: d => act('resell', { squares: [+d.sq] }),
  offer() {
    const { to, give, get, giveCash, getCash } = trade;
    act('offer', { offer: { to, give, get, giveCash, getCash } });
    trade = { ...trade, give: [], get: [], giveCash: 0, getCash: 0 };
  },
  toggleTrade() { },
};

document.addEventListener('click', e => {
  const el = e.target.closest('[data-do]');
  if (!el || el.tagName === 'DETAILS') return;
  handlers[el.dataset.do](el.dataset);
});
document.addEventListener('toggle', e => { if (e.target.dataset.do === 'toggleTrade') trade.open = e.target.open; }, true);
document.addEventListener('input', e => {
  const t = e.target, b = t.dataset;
  if (b.bind === 'giveCash' || b.bind === 'getCash') trade[b.bind] = +t.value;
  else if (b.set && draft) draft[b.set] = t.type === 'checkbox' ? t.checked : t.type === 'number' ? +t.value : t.value;
});
document.addEventListener('change', e => {
  const t = e.target, b = t.dataset;
  if (b.bind === 'tradeTo') { trade = { ...trade, to: +t.value, get: [] }; render(); }
  else if (b.tradeSq) {
    const sq = +t.value, list = trade[b.tradeSq];
    trade[b.tradeSq] = t.checked ? [...list, sq] : list.filter(x => x !== sq);
  } else if (b.set && draft) draft[b.set] = t.type === 'checkbox' ? t.checked : t.type === 'number' ? +t.value : t.value;
});
document.addEventListener('keydown', e => {
  if (e.key !== 'Enter') return;
  if (e.target.id === 'name') handlers.name();
  if (e.target.id === 'code') handlers.join();
});

connect();
render();
