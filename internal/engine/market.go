package engine

import "slices"

// Purchases and trades.

// buy buys the square p is on, a street with level houses: see Apply.
func (g *Game) buy(p, level int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseBuy {
		return ErrPhase
	}
	sq := g.Players[p].Pos
	s := &g.Board.Squares[sq]
	if level < 0 || level > 0 && (s.Kind != KindStreet || level > g.reach(p, 0)) {
		return ErrInvalid
	}
	houses := g.Board.builtCost(sq, level)
	if g.Players[p].Cash < s.Price+houses {
		return ErrCash
	}
	g.transfer(p, -1, s.Price+houses)
	g.Deeds[sq].Owner, g.Deeds[sq].Level = p, level
	g.emit(Event{Kind: "bought", Who: g.id(p), Square: sq, Amount: s.Price})
	if level > 0 {
		g.emit(Event{Kind: "built", Who: g.id(p), Square: sq, Level: level, Amount: houses})
	}
	if g.checkWin(p) {
		return nil
	}
	g.settle()
	return nil
}

// decline leaves the square p is on free: nobody buys it.
func (g *Game) decline(p int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseBuy {
		return ErrPhase
	}
	g.emit(Event{Kind: "declined", Who: g.id(p), Square: g.Players[p].Pos})
	g.settle()
	return nil
}

// Offer is a trade as proposed: what the proposer gives and gets, from the player To.
type Offer struct {
	To       uint32 `json:"to"`
	GiveCash int    `json:"giveCash"`
	GetCash  int    `json:"getCash"`
	Give     []int  `json:"give"` // squares of the proposer
	Get      []int  `json:"get"`  // squares of To
}

type Trade struct {
	ID       int
	From, To int // seats
	Offer
}

// check says whether t can be done now. Squares change hands with no building in their group;
// a mortgaged one stays mortgaged. A player who owes money gives none.
func (g *Game) check(t *Trade) error {
	from, to := g.Players[t.From], g.Players[t.To]
	switch {
	case t.From == t.To || to.Bankrupt || from.Bankrupt:
		return ErrInvalid
	case t.GiveCash < 0 || t.GetCash < 0:
		return ErrInvalid
	case t.GiveCash == 0 && t.GetCash == 0 && len(t.Give) == 0 && len(t.Get) == 0:
		return ErrInvalid
	case t.GiveCash > from.Cash || t.GetCash > to.Cash:
		return ErrCash
	case g.Phase == PhaseDebt && (t.From == g.Turn && t.GiveCash > 0 || t.To == g.Turn && t.GetCash > 0):
		return ErrPhase
	}
	all := append(slices.Clone(t.Give), t.Get...)
	if len(all) > len(g.Deeds) || len(slices.Compact(slices.Sorted(slices.Values(all)))) != len(all) {
		return ErrInvalid
	}
	for _, side := range [...]struct {
		squares []int
		owner   int
	}{{t.Give, t.From}, {t.Get, t.To}} {
		for _, sq := range side.squares {
			if g.ownedBy(side.owner, sq) != nil {
				return ErrInvalid
			}
			if g.Deeds[sq].Level > 0 {
				return ErrInvalid
			}
		}
	}
	return nil
}

func (g *Game) offer(p int, o *Offer) error {
	if o == nil {
		return ErrInvalid
	}
	to := g.seat(o.To)
	if to < 0 {
		return ErrInvalid
	}
	t := &Trade{ID: g.nextTrade + 1, From: p, To: to, Offer: *o}
	t.Give, t.Get = slices.Clone(o.Give), slices.Clone(o.Get)
	if err := g.check(t); err != nil {
		return err
	}
	g.nextTrade++
	g.Trades = slices.DeleteFunc(g.Trades, func(o *Trade) bool { return o.From == p })
	g.Trades = append(g.Trades, t)
	g.emit(Event{Kind: "offer", Who: g.id(p), To: g.id(to), Trade: t.ID})
	return nil
}

func (g *Game) trade(id int) *Trade {
	if i := slices.IndexFunc(g.Trades, func(t *Trade) bool { return t.ID == id }); i >= 0 {
		return g.Trades[i]
	}
	return nil
}

func (g *Game) accept(p, id int) error {
	t := g.trade(id)
	if t == nil || t.To != p {
		return ErrInvalid
	}
	if err := g.check(t); err != nil {
		return err
	}
	g.transfer(t.From, t.To, t.GiveCash)
	g.transfer(t.To, t.From, t.GetCash)
	for _, sq := range t.Give {
		g.Deeds[sq].Owner = t.To
	}
	for _, sq := range t.Get {
		g.Deeds[sq].Owner = t.From
	}
	g.Trades = slices.DeleteFunc(g.Trades, func(o *Trade) bool { return o == t })
	g.emit(Event{Kind: "traded", Who: g.id(p), To: g.id(t.From), Trade: id})
	g.checkWin(t.To, t.From)
	return nil
}

func (g *Game) reject(p, id int) error {
	t := g.trade(id)
	if t == nil || t.To != p {
		return ErrInvalid
	}
	g.Trades = slices.DeleteFunc(g.Trades, func(o *Trade) bool { return o == t })
	g.emit(Event{Kind: "rejected", Who: g.id(p), To: g.id(t.From), Trade: id})
	return nil
}

func (g *Game) cancel(p, id int) error {
	t := g.trade(id)
	if t == nil || t.From != p {
		return ErrInvalid
	}
	g.Trades = slices.DeleteFunc(g.Trades, func(o *Trade) bool { return o == t })
	g.emit(Event{Kind: "cancelled", Who: g.id(p), To: g.id(t.To), Trade: id})
	return nil
}
