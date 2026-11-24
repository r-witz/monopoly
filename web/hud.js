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
    case 'over': return '🏆 ' + w + ' gagne la partie !';
  }
  return e.kind;
}

// --- render, keeping the field being typed in ------------------------------------------------

export function render(app) {
  const f = document.activeElement, fid = f && f.id;
  let caret = null;
  try { caret = [f.selectionStart, f.selectionEnd]; } catch { }
  const inGame = app.room && app.game;
  $('front').hidden = !!inGame || !app.board;
  $('hud').hidden = !inGame;
  if (!app.board) return;
  if (!inGame) $('front').innerHTML = app.room ? roomScreen(app) : homeScreen(app);
  else {
    $('top').innerHTML = top(app);
    $('players').innerHTML = players(app);
    $('actions').innerHTML = actions(app);
    $('deed').innerHTML = deed(app);
    $('deed').hidden = app.selected < 0;
    $('trades').innerHTML = trades(app);
    renderLog(app);
    $('modal').innerHTML = app.modal ? app.modal(app) : '';
    $('modal').hidden = !app.modal;
  }
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

function homeScreen(app) {
  const list = app.rooms == null ? '' : app.rooms.length === 0 ? '<p class="muted">Aucune salle publique pour l\'instant.</p>' :
    app.rooms.map(r => `<div class="row"><span>${esc(r.name || 'Salle ' + r.code)}</span><span class="muted">${r.players}/${r.max}${r.playing ? ' · en jeu' : ''}</span>
      <button class="btn small" data-do="joinListed" data-code="${r.code}">Entrer</button></div>`).join('');
  return `<div class="panel home">
    <h1 class="logo">Quartiers</h1>
    <p class="tagline">Achète, construis, négocie… et ruine tes amis.</p>
    <section><h2>Ton joueur</h2>
      <div class="who"><i class="token big" style="--c:${app.colors[app.myColor] || '#999'}"></i>
      <input id="name" class="field" maxlength="16" value="${esc(app.myName || '')}" placeholder="Ton pseudo"></div>
      <div class="swatches">${app.colors.map((c, i) => `<button class="swatch ${i === app.myColor ? 'on' : ''}" style="--c:${c}" data-do="color" data-i="${i}" aria-label="couleur ${i + 1}"></button>`).join('')}</div>
    </section>
    <section class="play">
      <button class="btn big main" data-do="create">Créer une salle</button>
      <div class="join"><input id="code" class="field code" maxlength="5" placeholder="CODE"><button class="btn big" data-do="join">Rejoindre</button></div>
    </section>
    <section><h2>Salles publiques <button class="btn small" data-do="rooms">↻</button></h2>${list}</section>
  </div>`;
}

function settingsForm(app, owner) {
  const s = owner ? app.draft : app.room.settings;
  const dis = owner ? '' : 'disabled';
  const num = (k, label, step = 1) => `<label><span>${label}</span><input class="field" type="number" step="${step}" id="set-${k}" data-set="${k}" value="${s[k]}" ${dis}></label>`;
  return `<div class="settings">
    <label><span>Fin de partie</span><select class="field" id="set-end" data-set="end" ${dis}>
      ${[['rounds', 'Nombre de tours'], ['time', 'Temps limité'], ['survivor', 'Dernier survivant']].map(([v, l]) =>
        `<option value="${v}" ${s.end === v ? 'selected' : ''}>${l}</option>`).join('')}</select></label>
    ${s.end === 'rounds' ? num('rounds', 'Tours') : s.end === 'time' ? num('minutes', 'Minutes') : ''}
    ${num('startCash', 'Argent de départ', 100)}${num('salary', 'Salaire au Départ', 10)}
    ${num('startProps', 'Rues offertes')}${num('max', 'Joueurs max')}
    ${num('turnSeconds', 'Secondes par décision')}${num('auctionSeconds', 'Secondes par enchère')}
    <label class="check"><input type="checkbox" id="set-public" data-set="public" ${s.public ? 'checked' : ''} ${dis}> Salle publique</label>
    ${owner ? '<button class="btn" data-do="settings">Enregistrer</button>' : ''}
  </div>`;
}

function roomScreen(app) {
  const { nm } = names(app);
  const owner = app.room.owner === app.me;
  const link = location.origin + location.pathname + '#' + app.room.code;
  return `<div class="panel room">
    <h1 class="logo small">Quartiers</h1>
    <div class="code-box"><span class="muted">Code de la salle</span><b>${app.room.code}</b>
      <button class="btn small" data-do="copy" data-text="${esc(link)}">Copier le lien</button></div>
    <div class="cols">
      <section><h2>Joueurs</h2>
        ${app.room.players.map(p => `<div class="row"><i class="token" style="--c:${app.colors[p.color]}"></i>
          <span class="grow">${esc(nm(p.id))}${p.id === app.me ? ' <span class="muted">(toi)</span>' : ''}${p.id === app.room.owner ? ' 👑' : ''}${p.connected ? '' : ' <span class="muted">absent</span>'}</span>
          ${owner && p.id !== app.me ? `<button class="btn small" data-do="kick" data-id="${p.id}">Exclure</button>` : ''}</div>`).join('')}
      </section>
      <section><h2>Règles</h2>${settingsForm(app, owner)}</section>
    </div>
    <div class="bar">
      <button class="btn" data-do="leave">Quitter</button>
      ${owner ? '<button class="btn big main" data-do="start">Lancer la partie</button>' : '<span class="muted">Le créateur 👑 lance la partie.</span>'}
    </div>
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
    ${owner && g.phase !== 'over' ? '<button class="btn small" data-do="stop">Arrêter</button>' : ''}
    <button class="btn small" data-do="leave">Quitter</button>
  </div>`;
}

function players(app) {
  const { nm, colorOf, person } = names(app), g = app.game;
  return g.players.map(p => `<div class="glass pc ${p.id === g.turn && g.phase !== 'over' ? 'turn' : ''} ${p.bankrupt ? 'out' : ''}">
    <i class="token" style="--c:${colorOf(p.id)}"></i>
    <div class="grow"><b>${esc(nm(p.id))}${p.id === app.me ? ' <span class="you">toi</span>' : ''}</b>
      <small>patrimoine ${fmt(p.worth)}${person(p.id)?.connected === false ? ' · absent' : ''}</small></div>
    <div class="right">${money(p.cash)}<span class="badges">${p.inJail ? '🚓' : ''}${'🃏'.repeat(p.jailCards)}${p.bankrupt ? '💥' : ''}</span></div>
  </div>`).join('');
}

function actions(app) {
  const { nm } = names(app), g = app.game, b = app.board;
  const me = g.players.find(p => p.id === app.me);
  const clock = '<span id="clock" class="clock"></span>';
  const btn = (a, label, cls = '') => `<button class="btn big ${cls}" data-do="act" data-a="${a}">${label}</button>`;
  if (g.phase === 'over') {
    return `<div class="glass action mine"><span>🏆 <b>${esc(nm(g.winner))}</b> gagne !</span>
      <button class="btn" data-do="ranking">Classement</button><button class="btn big main" data-do="lobby">Retour au salon</button></div>`;
  }
  if (!me) return `<div class="glass action"><span>Tu regardes la partie. À <b>${esc(nm(g.turn))}</b> de jouer.</span>${clock}</div>`;
  if (me.bankrupt) return `<div class="glass action"><span>Tu as fait faillite… La partie continue sans toi.</span></div>`;
  const mine = g.turn === app.me;
  let body = '';
  switch (g.phase) {
    case 'roll':
      if (mine) body = btn('roll', '🎲 Lancer les dés', 'main') + (me.inJail ? btn('payjail', 'Payer ' + b.jailFine) + (me.jailCards ? btn('jailcard', '🃏 Carte') : '') : '');
      break;
    case 'buy': if (mine) {
      const s = b.squares[me.pos];
      body = `<span>${esc(s.name)}</span>` + btn('buy', 'Acheter ' + money(s.price), 'main') + btn('decline', 'Mettre aux enchères');
    } break;
    case 'auction': {
      const a = g.auction;
      const head = `<span>🔨 <b>${esc(b.squares[a.square].name)}</b> · ${a.high ? money(a.high) + ' par ' + esc(nm(a.bidder)) : 'aucune offre'}</span>`;
      if (a.passed.includes(app.me)) return `<div class="glass action">${head}<span class="muted">Tu as passé.</span>${clock}</div>`;
      if (a.bidder === app.me) return `<div class="glass action">${head}<span class="lead">Tu mènes !</span>${clock}</div>`;
      const quick = [10, 50, 100].map(n => `<button class="btn" data-do="bidQuick" data-n="${n}">+${n}</button>`).join('');
      return `<div class="glass action">${head}${quick}<input id="bid" class="field" type="number" data-bind="bid" value="${app.bid}">
        <button class="btn big main" data-do="bid">Enchérir</button>${btn('pass', 'Passer')}${clock}</div>`;
    }
    case 'debt': if (mine) {
      body = `<span>Tu dois <b>${fmt(g.debt.amount)}</b> à ${esc(nm(g.debt.to))} : vends ou hypothèque (clique tes cases), puis</span>` +
        btn('pay', 'Payer', 'main') + btn('bankrupt', 'Faillite', 'danger');
    } break;
    case 'end': if (mine) body = btn('end', 'Fin du tour ➜', 'main'); break;
  }
  if (!body) body = `<span>À <b>${esc(nm(g.turn))}</b> de jouer…</span>`;
  return `<div class="glass action ${mine ? 'mine' : ''}">${body}${clock}</div>`;
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
      `<tr class="sub"><td>Construction</td><td>${s.buildCost}</td></tr>`;
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
    if (s.kind === 'street') acts += a('build', '🏗 Construire') + a('sell', 'Revendre');
    acts += d.mortgaged ? a('unmortgage', 'Lever l\'hypothèque') : a('mortgage', 'Hypothéquer');
  }
  return `<div class="glass deedcard">
    <div class="deedhead" style="--c:${head}"><b>${esc(s.name)}</b>${s.price ? money(s.price) : ''}<button class="x" data-do="unselect">✕</button></div>
    ${rows ? '<table>' + rows + '</table>' : ''}
    <div class="owner">${d ? `<i class="token" style="--c:${colorOf(d.owner)}"></i> ${esc(nm(d.owner))}${d.mortgaged ? ' · <b>hypothéquée</b>' : ''}` : s.price ? '<span class="muted">À vendre</span>' : ''}</div>
    <div class="acts">${acts}</div>
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
  return `<div class="panel modalbox trade-editor">
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
  const medals = ['🥇', '🥈', '🥉'];
  return `<div class="panel modalbox podium">
    <h1 class="logo small">🏆 ${esc(nm(g.winner))} gagne !</h1>
    ${g.ranking.map((id, i) => { const p = g.players.find(x => x.id === id); return `<div class="row rank"><span class="medal">${medals[i] || i + 1}</span>
      <i class="token" style="--c:${colorOf(id)}"></i><b class="grow">${esc(nm(id))}</b>${p.bankrupt ? '<span class="muted">faillite</span>' : money(p.worth)}</div>`; }).join('')}
    <div class="bar"><button class="btn" data-do="closeModal">Voir le plateau</button><button class="btn big main" data-do="lobby">Retour au salon</button></div>
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
