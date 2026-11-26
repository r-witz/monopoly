package engine

import "slices"

// Properties: building, selling them back to the bank in debt, and what happens to them when
// their owner goes bankrupt.

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
	if err := g.ownedBy(p, sq); err != nil || sq != g.here && !(g.anywhere && g.Board.Squares[sq].Kind == KindStreet) {
		return ErrInvalid
	}
	d := &g.Deeds[sq]
	if level <= d.Level || level > g.reach(p, d.Level) {
		return ErrInvalid
	}
	cost := g.Board.builtCost(sq, level) - g.Board.builtCost(sq, d.Level)
	if g.Players[p].Cash < cost {
		return ErrCash
	}
	g.transfer(p, -1, cost)
	d.Level, g.here, g.anywhere = level, -1, false
	g.emit(Event{Kind: "built", Who: g.id(p), Square: sq, Level: level, Amount: cost})
	return nil
}

// Resale is what selling square sq back to the bank brings: all it cost, its price and every
// level built on it.
func (g *Game) Resale(sq int) int {
	return g.Board.Squares[sq].Price + g.Board.builtCost(sq, g.Deeds[sq].Level)
}

// resell sells squares of p back to the bank, p owing more than their cash: see Apply. Once the
// cash covers the debt, it is paid.
func (g *Game) resell(p int, squares []int) error {
	if g.Phase != PhaseDebt {
		return ErrPhase
	}
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Players[p].Cash >= g.Debt.Amount { // enough to pay: nothing to sell
		return ErrInvalid
	}
	if len(squares) == 0 || len(squares) > len(g.Deeds) || len(slices.Compact(slices.Sorted(slices.Values(squares)))) != len(squares) {
		return ErrInvalid
	}
	for _, sq := range squares {
		if err := g.ownedBy(p, sq); err != nil {
			return err
		}
	}
	for _, sq := range squares {
		g.resellSquare(p, sq)
	}
	if g.Players[p].Cash >= g.Debt.Amount {
		return g.payDebt(p) // cannot fail now
	}
	return nil
}

// resellSquare gives square sq of p back to the bank, its buildings with it, for its resale.
func (g *Game) resellSquare(p, sq int) {
	v := g.Resale(sq)
	g.transfer(-1, p, v)
	g.Deeds[sq] = Deed{Owner: -1}
	g.emit(Event{Kind: "resold", Who: g.id(p), Square: sq, Amount: v})
}

// liquidate resells the properties of p whose resales add up to the least that covers what p is
// missing to have amount in cash: as little as possible goes, the most valuable properties stay.
// All of them when even that is not enough. What the timer does for a player in debt.
func (g *Game) liquidate(p, amount int) {
	missing := amount - g.Players[p].Cash
	if missing <= 0 {
		return
	}
	var squares, values []int
	total := 0
	for sq, d := range g.Deeds {
		if d.Owner == p {
			squares, values = append(squares, sq), append(values, g.Resale(sq))
			total += values[len(values)-1]
		}
	}
	if total <= missing {
		for _, sq := range squares {
			g.resellSquare(p, sq)
		}
		return
	}
	// by[s]: 1 + the index of the property that first made a sale of exactly s, 0 for none yet
	// (by[0]: the empty sale). The properties before it make s minus its value.
	by := make([]int, total+1)
	by[0] = -1
	for i, v := range values {
		for s := total; s >= v; s-- {
			if by[s] == 0 && by[s-v] != 0 {
				by[s] = i + 1
			}
		}
	}
	s := missing
	for by[s] == 0 { // total itself is a sale: the search ends
		s++
	}
	for s > 0 {
		i := by[s] - 1
		g.resellSquare(p, squares[i])
		s -= values[i]
	}
}

// bankrupt takes p out of the game. Buildings are sold back to the bank, for what they cost; then
// cash, properties and cards go to the creditor to, or back to the bank (properties free) when to
// is -1.
func (g *Game) bankrupt(p, to int) {
	pl := g.Players[p]
	for sq, d := range g.Deeds {
		if d.Owner == p && d.Level > 0 {
			g.transfer(-1, p, g.Board.builtCost(sq, d.Level))
			g.Deeds[sq].Level = 0
		}
	}
	g.transfer(p, to, pl.Cash)
	for sq := range g.Deeds {
		if d := &g.Deeds[sq]; d.Owner == p {
			d.Owner = max(to, -1)
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
	if p == g.Turn {
		g.nextTurn()
	}
}
