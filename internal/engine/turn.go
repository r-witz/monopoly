package engine

// The current player's turn: the dice, the move and what the square does.

func (g *Game) roll(p int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseRoll {
		return ErrPhase
	}
	g.fly = false // rolling, the flight is given up
	g.Dice = g.dice()
	pl, sum, double := g.Players[p], g.Dice[0]+g.Dice[1], g.Dice[0] == g.Dice[1]
	g.emit(Event{Kind: "rolled", Who: pl.ID, Dice: g.Dice[:]})
	if pl.InJail {
		g.again = false // a double out of jail moves, and that is all
		switch {
		case double:
			g.free(p, 0)
			g.advance(p, sum)
		case pl.JailTurns+1 >= g.Board.JailTurns:
			// out of tries: the fine is due anyway, then the move
			g.free(p, g.Board.JailFine)
			g.charge(p, g.Board.JailFine, -1, func() { g.advance(p, sum) })
		default:
			pl.JailTurns++
			g.settle()
		}
		return nil
	}
	g.again = double
	if double {
		if g.Doubles++; g.Doubles == 3 {
			g.jail(p)
			g.settle()
			return nil
		}
	}
	g.advance(p, sum)
	return nil
}

// flyTo flies p from the airport to square sq: see Apply.
func (g *Game) flyTo(p, sq int) error {
	switch {
	case p != g.Turn:
		return ErrNotYours
	case g.Phase != PhaseRoll || !g.fly:
		return ErrPhase
	case sq < 0 || sq >= len(g.Board.Squares) || sq == g.Players[p].Pos || g.Board.Squares[sq].Kind == KindStart:
		return ErrInvalid
	case g.Players[p].Cash < g.Board.FlightCost:
		return ErrCash
	}
	pl := g.Players[p]
	g.fly, g.here, g.anywhere = false, -1, false
	g.transfer(p, -1, g.Board.FlightCost)
	g.emit(Event{Kind: "flew", Who: pl.ID, Square: sq, Amount: g.Board.FlightCost})
	pl.Pos = sq // over the board: no salary, no lap
	g.emit(Event{Kind: "moved", Who: pl.ID, Square: sq})
	g.land(p)
	return nil
}

func (g *Game) payJail(p int) error {
	if err := g.canLeaveJail(p); err != nil {
		return err
	}
	if g.Players[p].Cash < g.Board.JailFine {
		return ErrCash
	}
	g.transfer(p, -1, g.Board.JailFine)
	g.free(p, g.Board.JailFine)
	return nil
}

func (g *Game) useJailCard(p int) error {
	if err := g.canLeaveJail(p); err != nil {
		return err
	}
	pl := g.Players[p]
	if pl.JailCards == 0 {
		return ErrInvalid
	}
	pl.JailCards--
	g.returnJailCards(1)
	g.free(p, 0)
	return nil
}

func (g *Game) canLeaveJail(p int) error {
	switch {
	case p != g.Turn:
		return ErrNotYours
	case g.Phase != PhaseRoll:
		return ErrPhase
	case !g.Players[p].InJail:
		return ErrInvalid
	}
	return nil
}

// returnJailCards puts n "leave the police station" cards back under the deck.
func (g *Game) returnJailCards(n int) {
	for i, c := range g.Board.Cards {
		if c.Effect == JailFree && n > 0 {
			g.deck = append(g.deck, i)
			n--
		}
	}
}

func (g *Game) jail(p int) {
	pl := g.Players[p]
	pl.Pos, pl.InJail, pl.JailTurns = g.Board.Jail, true, 0
	g.again, g.here, g.anywhere, g.extra, g.fly = false, -1, false, 0, false
	g.emit(Event{Kind: "jailed", Who: pl.ID, Square: pl.Pos})
}

// free lets p out of the police station, having paid fine.
func (g *Game) free(p, fine int) {
	pl := g.Players[p]
	pl.InJail, pl.JailTurns = false, 0
	g.emit(Event{Kind: "freed", Who: pl.ID, Amount: fine})
}

// advance moves p by steps (backwards when negative), with the salary when passing or landing
// on the start going forward (p has then made a lap), and plays the square reached.
func (g *Game) advance(p, steps int) {
	pl, n := g.Players[p], len(g.Board.Squares)
	g.here, g.anywhere = -1, false
	if steps > 0 && pl.Pos+steps >= n {
		pl.Lapped = true
		g.transfer(-1, p, g.Rules.Salary)
		g.emit(Event{Kind: "salary", Who: pl.ID, Amount: g.Rules.Salary})
	}
	pl.Pos = ((pl.Pos+steps)%n + n) % n
	g.emit(Event{Kind: "moved", Who: pl.ID, Square: pl.Pos})
	g.land(p)
}

// forward moves p forward to square sq.
func (g *Game) forward(p, sq int) {
	n := len(g.Board.Squares)
	g.advance(p, ((sq-g.Players[p].Pos)%n+n)%n)
}

func (g *Game) land(p int) {
	pl := g.Players[p]
	sq := &g.Board.Squares[pl.Pos]
	switch sq.Kind {
	case KindStreet, KindTransit, KindUtility:
		d := g.Deeds[pl.Pos]
		switch {
		case d.Owner < 0:
			g.setPhase(PhaseBuy)
		case d.Owner == p:
			if sq.Kind == KindStreet && g.reach(p, d.Level) > d.Level {
				g.here = pl.Pos
			}
			g.settle()
		default:
			rent := g.rent(pl.Pos)
			g.emit(Event{Kind: "rent", Who: pl.ID, To: g.id(d.Owner), Square: pl.Pos, Amount: rent})
			g.charge(p, rent, d.Owner, g.settle)
		}
	case KindCard:
		g.draw(p)
	case KindTax:
		g.emit(Event{Kind: "tax", Who: pl.ID, Square: pl.Pos, Amount: sq.Tax})
		g.charge(p, sq.Tax, -1, g.settle)
	case KindGoToJail:
		g.jail(p)
		g.settle()
	case KindStart:
		g.startBonus(p)
	default:
		g.settle()
	}
}

// startBonus is the luck of landing right on the start, by the dice or a card: heads p rolls
// once more, tails p may build on a street of theirs, as if landing on it.
func (g *Game) startBonus(p int) {
	by := BonusRoll
	if g.coin() {
		g.extra++
	} else {
		by, g.anywhere = BonusBuild, true
	}
	g.emit(Event{Kind: "bonus", Who: g.id(p), Square: g.Players[p].Pos, By: by})
	g.settle()
}

// settle is the end of the current player's move: they roll again after a double, then for
// each roll won on the start, else they may still build before ending their turn, else the
// turn passes.
func (g *Game) settle() {
	switch pl := g.Players[g.Turn]; {
	case pl.Bankrupt:
		g.nextTurn()
	case g.again && !pl.InJail:
		g.setPhase(PhaseRoll)
	case g.extra > 0 && !pl.InJail:
		g.extra--
		g.Doubles = 0 // a new run: its doubles count from none
		g.setPhase(PhaseRoll)
	case g.canBuild():
		g.setPhase(PhaseEnd)
	default:
		g.nextTurn()
	}
}

// rent is what landing on square sq costs, owned by someone else. A street
// of a group owned whole: twice its bare rent, or 1.5 times its rent with buildings (rounded,
// half up).
func (g *Game) rent(sq int) int {
	s, d := &g.Board.Squares[sq], g.Deeds[sq]
	switch s.Kind {
	case KindStreet:
		rent := s.Rent[d.Level]
		switch {
		case !g.ownsGroup(d.Owner, s.Group):
			return rent
		case d.Level == 0:
			return 2 * rent
		default:
			return (3*rent + 1) / 2
		}
	case KindTransit:
		return g.Board.TransitRent[g.owned(d.Owner, KindTransit)-1]
	case KindUtility:
		return g.Board.UtilityFactor[g.owned(d.Owner, KindUtility)-1] * (g.Dice[0] + g.Dice[1])
	}
	return 0
}

// owned is how many squares of that kind p owns.
func (g *Game) owned(p int, k Kind) (n int) {
	for sq, d := range g.Deeds {
		if d.Owner == p && g.Board.Squares[sq].Kind == k {
			n++
		}
	}
	return n
}

// charge makes p, the current player, pay amount to seat to (-1: the bank), then goes on with
// then; when p cannot, the game waits in PhaseDebt.
func (g *Game) charge(p, amount, to int, then func()) {
	if g.Players[p].Cash >= amount {
		g.transfer(p, to, amount)
		then()
		return
	}
	g.Debt = &Debt{Amount: amount, To: to, then: then}
	g.setPhase(PhaseDebt)
	g.emit(Event{Kind: "debt", Who: g.id(p), To: g.id(to), Amount: amount})
}

func (g *Game) payDebt(p int) error {
	if g.Phase != PhaseDebt {
		return ErrPhase
	}
	if p != g.Turn {
		return ErrNotYours
	}
	d := g.Debt
	if g.Players[p].Cash < d.Amount {
		return ErrCash
	}
	g.transfer(p, d.To, d.Amount)
	g.Debt = nil
	g.emit(Event{Kind: "paid", Who: g.id(p), To: g.id(d.To), Amount: d.Amount})
	d.then()
	return nil
}

func (g *Game) draw(p int) {
	c := g.deck[0]
	g.deck = g.deck[1:]
	card := g.Board.Cards[c]
	if card.Effect != JailFree { // that one stays with the player until used
		g.deck = append(g.deck, c)
	}
	pl := g.Players[p]
	g.emit(Event{Kind: "card", Who: pl.ID, Card: c})
	switch card.Effect {
	case Collect:
		g.transfer(-1, p, card.Amount)
		g.settle()
	case Pay:
		g.charge(p, card.Amount, -1, g.settle)
	case MoveTo:
		g.forward(p, card.Square)
	case MoveBy:
		g.advance(p, card.Amount)
	case NextTransit:
		n := len(g.Board.Squares)
		for i := 1; i <= n; i++ {
			if sq := (pl.Pos + i) % n; g.Board.Squares[sq].Kind == KindTransit {
				g.forward(p, sq)
				return
			}
		}
		g.settle()
	case ToJail:
		g.jail(p)
		g.settle()
	case JailFree:
		pl.JailCards++
		g.settle()
	case PayEach:
		var others []int
		for i, o := range g.Players {
			if i != p && !o.Bankrupt {
				others = append(others, i)
			}
		}
		var next func(i int)
		next = func(i int) {
			switch {
			case i == len(others):
				g.settle()
			case g.Players[others[i]].Bankrupt: // left while p was raising cash
				next(i + 1)
			default:
				g.charge(p, card.Amount, others[i], func() { next(i + 1) })
			}
		}
		next(0)
	case Repairs:
		total := 0
		for _, d := range g.Deeds {
			switch {
			case d.Owner != p:
			case d.Level == g.Board.MaxLevel:
				total += card.PerTower
			default:
				total += d.Level * card.PerHouse
			}
		}
		g.charge(p, total, -1, g.settle)
	default:
		g.settle()
	}
}
