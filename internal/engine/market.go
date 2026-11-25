package engine

import "slices"

// Auctions and trades.

func (g *Game) buy(p int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseBuy {
		return ErrPhase
	}
	sq := g.Players[p].Pos
	price := g.Board.Squares[sq].Price
	if g.Players[p].Cash < price {
		return ErrCash
	}
	g.transfer(p, -1, price)
	g.Deeds[sq].Owner = p
	g.emit(Event{Kind: "bought", Who: g.id(p), Square: sq, Amount: price})
	if g.checkWin(p) {
		return nil
	}
	g.settle()
	return nil
}

func (g *Game) decline(p int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseBuy {
		return ErrPhase
	}
	a := &Auction{Square: g.Players[p].Pos, Bidder: -1, Passed: make([]bool, len(g.Players))}
	for i, o := range g.Players {
		a.Passed[i] = o.Bankrupt
	}
	g.Auction = a
	g.setPhase(PhaseAuction)
	g.emit(Event{Kind: "auction", Who: g.id(p), Square: a.Square})
	return nil
}

// minBid is the lowest bid an auction on sq takes: half the price of the square.
func (g *Game) minBid(sq int) int { return (g.Board.Squares[sq].Price + 1) / 2 }

func (g *Game) bid(p, amount int) error {
	a := g.Auction
	switch {
	case g.Phase != PhaseAuction:
		return ErrPhase
	case a.Passed[p] || a.Bidder == p || amount <= a.High || amount < g.minBid(a.Square):
		return ErrInvalid
	case amount > g.Players[p].Cash:
		return ErrCash
	}
	a.High, a.Bidder = amount, p
	g.Step++ // the clock starts again
	g.emit(Event{Kind: "bid", Who: g.id(p), Square: a.Square, Amount: amount})
	g.checkAuction()
	return nil
}

func (g *Game) pass(p int) error {
	a := g.Auction
	switch {
	case g.Phase != PhaseAuction:
		return ErrPhase
	case a.Passed[p] || a.Bidder == p:
		return ErrInvalid
	}
	a.Passed[p] = true
	g.emit(Event{Kind: "pass", Who: g.id(p), Square: a.Square})
	g.checkAuction()
	return nil
}

// checkAuction closes the auction once nobody is left to outbid the highest bidder.
func (g *Game) checkAuction() {
	a := g.Auction
	for i := range g.Players {
		if !a.Passed[i] && i != a.Bidder {
			return
		}
	}
	g.closeAuction()
}

// closeAuction sells the square to the highest bidder, who still has the cash: nobody can spend
// while an auction runs. With no bid the square stays with the bank.
func (g *Game) closeAuction() {
	a := g.Auction
	g.Auction = nil
	if a.Bidder >= 0 {
		g.transfer(a.Bidder, -1, a.High)
		g.Deeds[a.Square].Owner = a.Bidder
	}
	g.emit(Event{Kind: "auctioned", Who: g.id(a.Bidder), Square: a.Square, Amount: a.High})
	if g.checkWin(a.Bidder) {
		return
	}
	g.settle()
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
			if s := &g.Board.Squares[sq]; s.Kind == KindStreet {
				if _, hi, _ := g.levels(s.Group); hi > 0 {
					return ErrInvalid
				}
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
	if g.Phase == PhaseAuction {
		return ErrPhase
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
