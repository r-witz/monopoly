package engine

import "slices"

// Properties: building, mortgaging, and what happens to them when their owner goes bankrupt.

// ownsGroup says whether p owns every street of the group.
func (g *Game) ownsGroup(p, group int) bool {
	for sq, s := range g.Board.Squares {
		if s.Group == group && g.Deeds[sq].Owner != p {
			return false
		}
	}
	return true
}

// levels returns the lowest and highest level in the group, and whether any of it is mortgaged.
func (g *Game) levels(group int) (lo, hi int, mortgaged bool) {
	lo = g.Board.MaxLevel
	for sq, s := range g.Board.Squares {
		if s.Group == group {
			d := g.Deeds[sq]
			lo, hi, mortgaged = min(lo, d.Level), max(hi, d.Level), mortgaged || d.Mortgaged
		}
	}
	return lo, hi, mortgaged
}

// spending says whether p may spend cash outside the decision of the turn: not while an auction
// runs (bids are checked against cash) nor while p owes money.
func (g *Game) spending(p int) bool {
	return g.Phase != PhaseAuction && !(g.Phase == PhaseDebt && p == g.Turn)
}

// ownedBy checks that sq is a square p owns.
func (g *Game) ownedBy(p, sq int) error {
	if sq < 0 || sq >= len(g.Deeds) || g.Deeds[sq].Owner != p {
		return ErrInvalid
	}
	return nil
}

func (g *Game) build(p, sq int) error {
	if err := g.ownedBy(p, sq); err != nil {
		return err
	}
	if !g.spending(p) {
		return ErrPhase
	}
	s, d := &g.Board.Squares[sq], &g.Deeds[sq]
	if s.Kind != KindStreet || !g.ownsGroup(p, s.Group) {
		return ErrInvalid
	}
	if lo, _, mortgaged := g.levels(s.Group); mortgaged || d.Level > lo || d.Level == g.Board.MaxLevel {
		return ErrInvalid
	}
	cost := g.Board.levelCost(sq, d.Level+1)
	if g.Players[p].Cash < cost {
		return ErrCash
	}
	g.transfer(p, -1, cost)
	d.Level++
	g.emit(Event{Kind: "built", Who: g.id(p), Square: sq, Level: d.Level, Amount: cost})
	return nil
}

func (g *Game) sell(p, sq int) error {
	if err := g.ownedBy(p, sq); err != nil {
		return err
	}
	s, d := &g.Board.Squares[sq], &g.Deeds[sq]
	if _, hi, _ := g.levels(s.Group); d.Level == 0 || d.Level < hi {
		return ErrInvalid
	}
	g.sellLevel(p, sq)
	return nil
}

func (g *Game) sellLevel(p, sq int) {
	d := &g.Deeds[sq]
	back := g.Board.levelCost(sq, d.Level) / 2
	g.transfer(-1, p, back)
	d.Level--
	g.emit(Event{Kind: "sold", Who: g.id(p), Square: sq, Level: d.Level, Amount: back})
}

// mortgageable says whether sq, owned, can be mortgaged: not already, and no building in its group.
func (g *Game) mortgageable(sq int) bool {
	s := &g.Board.Squares[sq]
	if g.Deeds[sq].Mortgaged {
		return false
	}
	if s.Kind == KindStreet {
		if _, hi, _ := g.levels(s.Group); hi > 0 {
			return false
		}
	}
	return true
}

func (g *Game) mortgage(p, sq int) error {
	if err := g.ownedBy(p, sq); err != nil {
		return err
	}
	if !g.mortgageable(sq) {
		return ErrInvalid
	}
	g.mortgageSquare(p, sq)
	return nil
}

func (g *Game) mortgageSquare(p, sq int) {
	g.Deeds[sq].Mortgaged = true
	m := g.Board.mortgage(sq)
	g.transfer(-1, p, m)
	g.emit(Event{Kind: "mortgaged", Who: g.id(p), Square: sq, Amount: m})
}

func (g *Game) unmortgage(p, sq int) error {
	if err := g.ownedBy(p, sq); err != nil {
		return err
	}
	if !g.Deeds[sq].Mortgaged {
		return ErrInvalid
	}
	if !g.spending(p) {
		return ErrPhase
	}
	cost := g.Board.unmortgage(sq)
	if g.Players[p].Cash < cost {
		return ErrCash
	}
	g.transfer(p, -1, cost)
	g.Deeds[sq].Mortgaged = false
	g.emit(Event{Kind: "unmortgaged", Who: g.id(p), Square: sq, Amount: cost})
	return nil
}

// liquidate sells p's buildings, then mortgages p's properties, until p has amount in cash or
// nothing is left to sell. What the timer does for a player in debt.
func (g *Game) liquidate(p, amount int) {
	for g.Players[p].Cash < amount {
		if sq := g.nextToLiquidate(p); sq >= 0 {
			if g.Deeds[sq].Level > 0 {
				g.sellLevel(p, sq)
			} else {
				g.mortgageSquare(p, sq)
			}
			continue
		}
		return
	}
}

// nextToLiquidate is the square whose building p sells next (the highest one of a group, evenly),
// else the square p mortgages next, else -1.
func (g *Game) nextToLiquidate(p int) int {
	for sq, d := range g.Deeds {
		if d.Owner == p && d.Level > 0 {
			if _, hi, _ := g.levels(g.Board.Squares[sq].Group); d.Level == hi {
				return sq
			}
		}
	}
	for sq, d := range g.Deeds {
		if d.Owner == p && g.mortgageable(sq) {
			return sq
		}
	}
	return -1
}

// bankrupt takes p out of the game. Buildings are sold back to the bank; then cash, properties
// (mortgaged or not) and cards go to the creditor to, or back to the bank (properties free and
// clear) when to is -1.
func (g *Game) bankrupt(p, to int) {
	pl := g.Players[p]
	for sq, d := range g.Deeds {
		if d.Owner == p && d.Level > 0 {
			for l := d.Level; l > 0; l-- {
				g.transfer(-1, p, g.Board.levelCost(sq, l)/2)
			}
			g.Deeds[sq].Level = 0
		}
	}
	g.transfer(p, to, pl.Cash)
	for sq := range g.Deeds {
		if d := &g.Deeds[sq]; d.Owner == p {
			if to >= 0 {
				d.Owner = to
			} else {
				d.Owner, d.Mortgaged = -1, false
			}
		}
	}
	if to >= 0 {
		g.Players[to].JailCards += pl.JailCards
	} else {
		g.returnJailCards(pl.JailCards)
	}
	pl.JailCards, pl.InJail, pl.Bankrupt = 0, false, true
	g.out++
	pl.Out = g.out
	g.emit(Event{Kind: "bankrupt", Who: pl.ID, To: g.id(to)})

	g.Trades = slices.DeleteFunc(g.Trades, func(t *Trade) bool { return t.From == p || t.To == p })
	if d := g.Debt; d != nil {
		if p == g.Turn {
			g.Debt = nil
		} else if d.To == p { // owed to a player who is gone: to the bank instead
			d.To = -1
		}
	}
	if g.checkWin(to) { // the creditor may now own what wins
		return
	}
	if g.alive() <= 1 {
		g.finish(-1, "")
		return
	}
	if a := g.Auction; a != nil {
		a.Passed[p] = true
		if a.Bidder == p { // the bid is void: the auction goes on from nothing
			a.High, a.Bidder = 0, -1
		}
		g.Step++
		g.checkAuction()
		return
	}
	if p == g.Turn {
		g.nextTurn()
	}
}
