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

// firstLapHouses is how many houses a street may have before its owner has passed the start.
const firstLapHouses = 2

// reach is the highest level p may build a street at level up to, on landing there (or buying
// it, from 0): houses, up to firstLapHouses before p has passed the start; the tower only on a
// street already at 3 houses.
func (g *Game) reach(p, level int) int {
	switch top := g.Board.MaxLevel; {
	case level >= top-1:
		return top
	case g.Players[p].Lapped:
		return top - 1
	default:
		return min(firstLapHouses, top-1)
	}
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

// build builds street sq of p up to level, p standing on it: see Apply.
func (g *Game) build(p, sq, level int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseRoll && g.Phase != PhaseEnd {
		return ErrPhase
	}
	if err := g.ownedBy(p, sq); err != nil || sq != g.here {
		return ErrInvalid
	}
	d := &g.Deeds[sq]
	if d.Mortgaged || level <= d.Level || level > g.reach(p, d.Level) {
		return ErrInvalid
	}
	cost := g.Board.builtCost(sq, level) - g.Board.builtCost(sq, d.Level)
	if g.Players[p].Cash < cost {
		return ErrCash
	}
	g.transfer(p, -1, cost)
	d.Level, g.here = level, -1
	g.emit(Event{Kind: "built", Who: g.id(p), Square: sq, Level: level, Amount: cost})
	return nil
}

func (g *Game) sell(p, sq int) error {
	if err := g.ownedBy(p, sq); err != nil {
		return err
	}
	if g.Deeds[sq].Level == 0 {
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

// mortgageable says whether sq, owned, can be mortgaged: not already, and no building on it.
func (g *Game) mortgageable(sq int) bool {
	d := g.Deeds[sq]
	return !d.Mortgaged && d.Level == 0
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

// nextToLiquidate is the street whose building p sells next, else the square p mortgages next,
// else -1.
func (g *Game) nextToLiquidate(p int) int {
	for sq, d := range g.Deeds {
		if d.Owner == p && d.Level > 0 {
			return sq
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
