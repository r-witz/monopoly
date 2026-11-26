package engine

// Purchases.

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
