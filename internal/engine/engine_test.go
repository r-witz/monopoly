package engine

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"
)

var testRules = Rules{StartCash: 1500, Salary: 200}

func newGame(n int, r Rules, seed uint64) *Game {
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = uint32(i + 1)
	}
	return New(Classic, r, ids, seed)
}

// script makes the dice give these rolls, in order.
func script(g *Game, rolls ...[2]int) {
	g.dice = func() [2]int {
		r := rolls[0]
		rolls = rolls[1:]
		return r
	}
}

func must(t *testing.T, g *Game, p int, a Action) []Event {
	t.Helper()
	ev, err := g.Apply(p, a)
	if err != nil {
		t.Fatalf("%s by seat %d: %v (phase %s)", a.Kind, p, err, g.Phase)
	}
	return ev
}

func has(ev []Event, kind string) bool {
	return slices.ContainsFunc(ev, func(e Event) bool { return e.Kind == kind })
}

// invariants checks what must hold after any call, whatever was played.
func invariants(t *testing.T, g *Game) {
	t.Helper()
	sum := 0
	for i, p := range g.Players {
		if p.Cash < 0 {
			t.Fatalf("seat %d has %d", i, p.Cash)
		}
		if p.Bankrupt && (p.Cash != 0 || p.JailCards != 0) {
			t.Fatalf("bankrupt seat %d keeps cash %d or cards %d", i, p.Cash, p.JailCards)
		}
		sum += p.Cash
	}
	if want := len(g.Players)*g.Rules.StartCash + g.bank; sum != want {
		t.Fatalf("cash %d, want %d: money appeared or vanished", sum, want)
	}
	if (g.Phase == PhaseDebt) != (g.Debt != nil) || (g.Phase == PhaseAuction) != (g.Auction != nil) {
		t.Fatalf("phase %s with debt %v auction %v", g.Phase, g.Debt, g.Auction)
	}
	if g.Phase != PhaseOver && g.Phase != PhaseAuction && g.Players[g.Turn].Bankrupt {
		t.Fatalf("phase %s for bankrupt seat %d", g.Phase, g.Turn)
	}
	if d := g.Debt; d != nil && d.To >= 0 && g.Players[d.To].Bankrupt {
		t.Fatalf("debt owed to bankrupt seat %d", d.To)
	}
	for sq, d := range g.Deeds {
		s := &g.Board.Squares[sq]
		switch {
		case d.Owner >= 0 && !s.ownable():
			t.Fatalf("square %d owned", sq)
		case d.Owner >= 0 && g.Players[d.Owner].Bankrupt:
			t.Fatalf("square %d owned by bankrupt seat %d", sq, d.Owner)
		case d.Level < 0 || d.Level > g.Board.MaxLevel:
			t.Fatalf("square %d at level %d", sq, d.Level)
		case d.Level > 0 && s.Kind != KindStreet:
			t.Fatalf("building on square %d", sq)
		}
		if d.Level > 0 && d.Mortgaged {
			t.Fatalf("square %d built and mortgaged", sq)
		}
		if d.Level > firstLapHouses && !g.Players[d.Owner].Lapped {
			t.Fatalf("square %d at level %d, its owner never passed the start", sq, d.Level)
		}
	}
	cards := len(g.deck)
	for _, p := range g.Players {
		cards += p.JailCards
	}
	if cards != len(g.Board.Cards) {
		t.Fatalf("%d cards in the deck and in hands, want %d", cards, len(g.Board.Cards))
	}
}

// randomAction is any action, legal or not, by any seat, with random arguments.
func randomAction(g *Game, r *rand.Rand) (int, Action) {
	kinds := []string{"roll", "roll", "roll", "buy", "buy", "decline", "bid", "pass", "pass", "build", "build", "sell",
		"mortgage", "unmortgage", "pay", "pay", "bankrupt", "end", "end", "end", "offer", "accept", "reject", "cancel", "payjail", "jailcard", "bogus"}
	p := r.IntN(len(g.Players))
	if r.IntN(2) == 0 {
		p = g.Turn
	}
	a := Action{Kind: kinds[r.IntN(len(kinds))], Square: r.IntN(len(g.Deeds)+2) - 1, Level: r.IntN(g.Board.MaxLevel+3) - 1,
		Amount: r.IntN(g.Players[p].Cash+100) - 10}
	if a.Kind == "build" && r.IntN(2) == 0 {
		a.Square = g.here // most random squares are not where the player stands
	}
	if len(g.Trades) > 0 {
		a.Trade = g.Trades[r.IntN(len(g.Trades))].ID
	}
	if a.Kind == "offer" {
		o := &Offer{To: uint32(r.IntN(len(g.Players) + 1)), GiveCash: r.IntN(300), GetCash: r.IntN(300)}
		for sq, d := range g.Deeds {
			if d.Owner == p && r.IntN(3) == 0 {
				o.Give = append(o.Give, sq)
			} else if d.Owner >= 0 && d.Owner != p && r.IntN(3) == 0 {
				o.Get = append(o.Get, sq)
			}
		}
		a.Offer = o
	}
	return p, a
}

func TestRandomGames(t *testing.T) {
	games := 3000
	if testing.Short() {
		games = 300
	}
	for seed := range uint64(games) {
		r := rand.New(rand.NewPCG(seed, 7))
		rules := Rules{StartCash: 300 + r.IntN(2000), Salary: r.IntN(300), MaxRounds: 5 + r.IntN(40),
			WinTransit: r.IntN(2) == 0, WinGroups: r.IntN(2) == 0, WinSide: r.IntN(2) == 0}
		g := newGame(2+r.IntN(7), rules, seed)
		invariants(t, g)
		for step := 0; g.Phase != PhaseOver; step++ {
			if step > 100000 {
				t.Fatalf("seed %d: no end after %d steps (round %d)", seed, step, g.Round)
			}
			switch n := r.IntN(100); {
			case n < 5:
				g.Timeout()
			case n == 5 && r.IntN(20) == 0:
				g.Resign(r.IntN(len(g.Players)))
			case n == 6 && r.IntN(100) == 0:
				g.Finish()
			default:
				p, a := randomAction(g, r)
				before := g.Step
				if _, err := g.Apply(p, a); err != nil && g.Step != before {
					t.Fatalf("seed %d: %s failed (%v) but the game moved on", seed, a.Kind, err)
				}
			}
			invariants(t, g)
			if step%50 == 0 {
				if _, err := json.Marshal(g.View()); err != nil {
					t.Fatal(err)
				}
			}
		}
		if g.Winner < 0 || g.Players[g.Winner].Bankrupt {
			t.Fatalf("seed %d: winner %d", seed, g.Winner)
		}
	}
}

// A game where nobody plays ends on the round limit, moved on by the timer alone.
func TestIdleGameEnds(t *testing.T) {
	g := newGame(6, Rules{StartCash: 1500, Salary: 200, MaxRounds: 15}, 1)
	for i := 0; g.Phase != PhaseOver; i++ {
		if i > 10000 {
			t.Fatal("no end")
		}
		g.Timeout()
		invariants(t, g)
	}
	if g.Round != 15 {
		t.Fatalf("ended on round %d", g.Round)
	}
}

func TestThreeDoublesGoToJail(t *testing.T) {
	g := newGame(2, testRules, 1)
	script(g, [2]int{1, 1}, [2]int{2, 2}, [2]int{3, 3})
	for i := range 3 {
		ev := must(t, g, 0, Action{Kind: "roll"})
		if i < 2 {
			// squares 2 (card) then 6 (street): play them out so the turn is back to rolling
			for g.Phase != PhaseRoll {
				g.Timeout()
			}
			continue
		}
		if !has(ev, "jailed") || !g.Players[0].InJail || g.Phase != PhaseEnd {
			t.Fatalf("after 3 doubles: jail %v phase %s", g.Players[0].InJail, g.Phase)
		}
	}
}

func TestJailThirdFailPaysAndMoves(t *testing.T) {
	g := newGame(2, testRules, 1)
	g.jail(0)
	script(g, [2]int{1, 2}, [2]int{1, 2}, [2]int{1, 3})
	for i := range 3 {
		g.Turn, g.Phase = 0, PhaseRoll
		must(t, g, 0, Action{Kind: "roll"})
		if i < 2 && (!g.Players[0].InJail || g.Players[0].Pos != g.Board.Jail) {
			t.Fatalf("try %d: should still be held", i)
		}
	}
	if p := g.Players[0]; p.InJail || p.Pos != g.Board.Jail+4 || p.Cash != 1500-g.Board.JailFine {
		t.Fatalf("after 3 tries: jail %v pos %d cash %d", p.InJail, p.Pos, p.Cash)
	}
}

func TestRentDoublesOnWholeGroupAndBuildings(t *testing.T) {
	g := newGame(2, testRules, 1)
	g.Deeds[1].Owner = 1
	s := &g.Board.Squares[1]
	if r := g.rent(1); r != s.Rent[0] {
		t.Fatalf("bare rent %d, want %d", r, s.Rent[0])
	}
	g.Deeds[3].Owner = 1
	if r := g.rent(1); r != 2*s.Rent[0] {
		t.Fatalf("whole group rent %d, want %d", r, 2*s.Rent[0])
	}
	g.Deeds[1].Level = 2
	if r, want := g.rent(1), (3*s.Rent[2]+1)/2; r != want {
		t.Fatalf("whole group rent at level 2: %d, want %d", r, want)
	}
	g.Deeds[1].Level = 1 // 25: 37.5, rounded up
	if r := g.rent(1); r != 38 || s.Rent[1] != 25 {
		t.Fatalf("whole group rent at level 1: %d (rent %d), want 38", r, s.Rent[1])
	}
	g.Deeds[3].Owner = 0 // the group no longer whole
	if r := g.rent(1); r != s.Rent[1] {
		t.Fatalf("rent at level 1: %d, want %d", r, s.Rent[1])
	}
	g.Deeds[3].Owner, g.Deeds[1].Level = 1, 2
	if _, err := g.Apply(1, Action{Kind: "mortgage", Square: 1}); err != ErrInvalid {
		t.Fatalf("mortgage with buildings on the street: %v", err)
	}
	must(t, g, 1, Action{Kind: "mortgage", Square: 3}) // nothing on it: its group does not matter
}

// standOn puts seat p on square sq on their turn, as if they had just rolled there.
func standOn(t *testing.T, g *Game, p, sq int) {
	t.Helper()
	g.Turn, g.Phase = p, PhaseRoll
	g.Players[p].Pos = sq - 4
	script(g, [2]int{1, 3})
	must(t, g, p, Action{Kind: "roll"})
}

// On a free street: the bare street, or with 1 or 2 houses; 3 houses once the player has passed
// the start; never the tower.
func TestBuyWithHouses(t *testing.T) {
	g := newGame(2, testRules, 1)
	s := &g.Board.Squares[11]
	standOn(t, g, 0, 11)
	if g.Phase != PhaseBuy {
		t.Fatalf("phase %s", g.Phase)
	}
	for _, level := range []int{-1, 3, 4} {
		if _, err := g.Apply(0, Action{Kind: "buy", Level: level}); err != ErrInvalid {
			t.Fatalf("buy with %d houses before a lap: %v", level, err)
		}
	}
	ev := must(t, g, 0, Action{Kind: "buy", Level: 2})
	if g.Deeds[11].Level != 2 || g.Players[0].Cash != 1500-s.Price-2*s.BuildCost || !has(ev, "built") {
		t.Fatalf("level %d cash %d, events %v", g.Deeds[11].Level, g.Players[0].Cash, ev)
	}
	if g.here != -1 {
		t.Fatal("may build again on the landing it bought on")
	}

	g = newGame(2, testRules, 1)
	g.Players[0].Pos = 39
	script(g, [2]int{6, 6})
	must(t, g, 0, Action{Kind: "roll"}) // passes the start, lands on 11
	if !g.Players[0].Lapped || g.Phase != PhaseBuy || g.Players[0].Pos != 11 {
		t.Fatalf("lapped %v phase %s pos %d", g.Players[0].Lapped, g.Phase, g.Players[0].Pos)
	}
	if _, err := g.Apply(0, Action{Kind: "buy", Level: 4}); err != ErrInvalid {
		t.Fatalf("buy with the tower: %v", err)
	}
	must(t, g, 0, Action{Kind: "buy", Level: 3})

	// transit and utilities: no houses
	g = newGame(2, testRules, 1)
	standOn(t, g, 0, 5)
	if _, err := g.Apply(0, Action{Kind: "buy", Level: 1}); err != ErrInvalid {
		t.Fatalf("a house on a transit line: %v", err)
	}
	invariants(t, g)
}

// Building: standing on one's street, once per landing, whole group or not; the tower on a
// street at 3 houses, a later landing.
func TestBuildOnLanding(t *testing.T) {
	g := newGame(2, testRules, 1)
	s := &g.Board.Squares[21]
	g.Deeds[21].Owner, g.Deeds[21].Level = 0, 1 // group 5, not whole
	if _, err := g.Apply(0, Action{Kind: "build", Square: 21, Level: 2}); err != ErrInvalid {
		t.Fatalf("built away from the street: %v", err)
	}
	standOn(t, g, 0, 21)
	if g.View().Here != 21 {
		t.Fatalf("here %d", g.View().Here)
	}
	for _, level := range []int{1, 3, 4} {
		if _, err := g.Apply(0, Action{Kind: "build", Square: 21, Level: level}); err != ErrInvalid {
			t.Fatalf("up to %d from 1 house, before a lap: %v", level, err)
		}
	}
	cash := g.Players[0].Cash
	must(t, g, 0, Action{Kind: "build", Square: 21, Level: 2})
	if g.Deeds[21].Level != 2 || g.Players[0].Cash != cash-s.BuildCost {
		t.Fatalf("level %d cash %d", g.Deeds[21].Level, g.Players[0].Cash)
	}
	if _, err := g.Apply(0, Action{Kind: "build", Square: 21, Level: 2}); err != ErrInvalid {
		t.Fatalf("built twice on one landing: %v", err)
	}
	must(t, g, 0, Action{Kind: "end"})

	// 2 houses before a lap: nothing to build
	standOn(t, g, 0, 21)
	if g.here != -1 {
		t.Fatal("here with 2 houses before a lap")
	}
	must(t, g, 0, Action{Kind: "end"})

	g.Players[0].Lapped = true
	standOn(t, g, 0, 21)
	if _, err := g.Apply(0, Action{Kind: "build", Square: 21, Level: 4}); err != ErrInvalid {
		t.Fatalf("the tower from 2 houses: %v", err)
	}
	must(t, g, 0, Action{Kind: "build", Square: 21, Level: 3})
	must(t, g, 0, Action{Kind: "end"})

	standOn(t, g, 0, 21)
	cash = g.Players[0].Cash
	must(t, g, 0, Action{Kind: "build", Square: 21, Level: 4})
	if g.Deeds[21].Level != 4 || g.Players[0].Cash != cash-s.TowerCost {
		t.Fatalf("level %d cash %d", g.Deeds[21].Level, g.Players[0].Cash)
	}
	must(t, g, 0, Action{Kind: "end"})

	// at the tower, nothing left to build
	standOn(t, g, 0, 21)
	if g.here != -1 {
		t.Fatal("here at the tower")
	}
	// a mortgaged street of one's own: nothing either
	g.Deeds[23].Owner, g.Deeds[23].Mortgaged = 0, true
	must(t, g, 0, Action{Kind: "end"})
	standOn(t, g, 0, 23)
	if g.here != -1 {
		t.Fatal("here on a mortgaged street")
	}
	invariants(t, g)
}

// Passing the start makes a lap, even with no salary.
func TestLappedWithoutSalary(t *testing.T) {
	g := newGame(2, Rules{StartCash: 1500}, 1)
	g.Players[0].Pos = 38
	script(g, [2]int{1, 2})
	must(t, g, 0, Action{Kind: "roll"})
	if !g.Players[0].Lapped || g.Players[1].Lapped || !g.View().Players[0].Lapped {
		t.Fatal("no lap")
	}
}

func TestDebtTimeoutSellsThenPays(t *testing.T) {
	g := newGame(2, testRules, 1)
	for _, sq := range []int{1, 3} {
		g.Deeds[sq].Owner, g.Deeds[sq].Level = 0, 2
	}
	g.transfer(0, -1, 1490) // 10 left
	g.charge(0, 100, 1, g.settle)
	if g.Phase != PhaseDebt {
		t.Fatalf("phase %s", g.Phase)
	}
	ev := g.Timeout()
	if !has(ev, "sold") || !has(ev, "paid") || g.Phase != PhaseEnd || g.Players[1].Cash != 1600 {
		t.Fatalf("phase %s, creditor %d, events %v", g.Phase, g.Players[1].Cash, ev)
	}
	invariants(t, g)
}

func TestBankruptToCreditor(t *testing.T) {
	g := newGame(3, testRules, 1)
	g.Deeds[5].Owner, g.Deeds[5].Mortgaged = 0, true
	g.transfer(0, -1, 1500)
	g.charge(0, 500, 2, g.settle)
	must(t, g, 0, Action{Kind: "bankrupt"})
	if g.Deeds[5].Owner != 2 || !g.Deeds[5].Mortgaged || !g.Players[0].Bankrupt || g.Turn != 1 || g.Phase != PhaseRoll {
		t.Fatalf("deed %+v, turn %d, phase %s", g.Deeds[5], g.Turn, g.Phase)
	}
	invariants(t, g)
}

func TestTradeCheckedAgainOnAccept(t *testing.T) {
	g := newGame(2, testRules, 1)
	g.Deeds[1].Owner, g.Deeds[3].Owner, g.Deeds[5].Owner = 0, 0, 1
	must(t, g, 0, Action{Kind: "offer", Offer: &Offer{To: 2, Give: []int{1}, Get: []int{5}, GiveCash: 50}})
	g.Deeds[1].Level = 1 // now square 1 cannot change hands
	if _, err := g.Apply(1, Action{Kind: "accept", Trade: 1}); err != ErrInvalid {
		t.Fatalf("accept after a build: %v", err)
	}
	must(t, g, 0, Action{Kind: "sell", Square: 1})
	must(t, g, 1, Action{Kind: "accept", Trade: 1})
	if g.Deeds[1].Owner != 1 || g.Deeds[5].Owner != 0 || g.Players[1].Cash != 1550 || len(g.Trades) != 0 {
		t.Fatalf("after the trade: %+v %+v cash %d", g.Deeds[1], g.Deeds[5], g.Players[1].Cash)
	}
	invariants(t, g)
}

func TestAuction(t *testing.T) {
	g := newGame(3, testRules, 1)
	script(g, [2]int{1, 2})
	must(t, g, 0, Action{Kind: "roll"}) // square 3, a street
	must(t, g, 0, Action{Kind: "decline"})
	if _, err := g.Apply(1, Action{Kind: "bid", Amount: 29}); err == nil {
		t.Fatal("bid under half the price (60)")
	}
	if v := g.View(); v.Auction.Min != 30 {
		t.Fatalf("min bid %d, want 30", v.Auction.Min)
	}
	must(t, g, 1, Action{Kind: "bid", Amount: 30})
	if _, err := g.Apply(1, Action{Kind: "build", Square: 3}); err == nil {
		t.Fatal("built during an auction")
	}
	must(t, g, 2, Action{Kind: "bid", Amount: 31})
	must(t, g, 0, Action{Kind: "pass"})
	ev := must(t, g, 1, Action{Kind: "pass"})
	if !has(ev, "auctioned") || g.Deeds[3].Owner != 2 || g.Players[2].Cash != 1500-31 || g.Phase != PhaseEnd {
		t.Fatalf("owner %d cash %d phase %s", g.Deeds[3].Owner, g.Players[2].Cash, g.Phase)
	}
}

// TestBoardLayout: four corners and four sides of 9 squares, and every group on a single side.
func TestBoardLayout(t *testing.T) {
	b := Classic
	if len(b.Squares) != 40 {
		t.Fatalf("%d squares, want 40", len(b.Squares))
	}
	corners := []Kind{KindStart, KindJail, KindRest, KindGoToJail}
	for side, k := range corners {
		if b.Squares[side*10].Kind != k {
			t.Fatalf("square %d is %s, want the corner %s", side*10, b.Squares[side*10].Kind, k)
		}
	}
	if b.Squares[b.Jail].Kind != KindJail {
		t.Fatal("Jail is not the police station")
	}
	sides := map[int]int{}
	for i, s := range b.Squares {
		if s.Kind != KindStreet {
			continue
		}
		if len(s.Rent) != b.MaxLevel+1 || s.BuildCost <= 0 || s.TowerCost < s.BuildCost {
			t.Fatalf("%s: %d rents (want %d), house %d, tower %d", s.Name, len(s.Rent), b.MaxLevel+1, s.BuildCost, s.TowerCost)
		}
		if side, ok := sides[s.Group]; ok && side != i/10 {
			t.Fatalf("group %d on sides %d and %d", s.Group, side, i/10)
		}
		sides[s.Group] = i / 10
	}
	if len(sides) != 8 {
		t.Fatalf("%d groups, want 8", len(sides))
	}
	for _, c := range b.Cards {
		if c.Effect == MoveTo && b.Squares[c.Square].Kind == KindCard {
			t.Fatalf("card %q moves to a card square", c.Text)
		}
	}
}

var winRules = Rules{StartCash: 1500, Salary: 200, WinTransit: true, WinGroups: true, WinSide: true}

// give hands these squares to seat p.
func give(g *Game, p int, squares ...int) {
	for _, sq := range squares {
		g.Deeds[sq].Owner = p
	}
}

// buyAt makes seat p buy square sq, as if p had just landed on it.
func buyAt(t *testing.T, g *Game, p, sq int) []Event {
	t.Helper()
	g.Turn, g.Phase, g.Players[p].Pos = p, PhaseBuy, sq
	return must(t, g, p, Action{Kind: "buy"})
}

// won checks the game is over, won by seat p by the instant win by, p first in the ranking.
func won(t *testing.T, g *Game, ev []Event, p int, by string) {
	t.Helper()
	if g.Phase != PhaseOver || g.Winner != p || g.WinBy != by || g.Ranking()[0] != p {
		t.Fatalf("phase %s, winner %d by %q, ranking %v; want seat %d by %q", g.Phase, g.Winner, g.WinBy, g.Ranking(), p, by)
	}
	if v := g.View(); v.WinBy != by || v.Ranking[0] != g.id(p) {
		t.Fatalf("view: by %q, ranking %v", v.WinBy, v.Ranking)
	}
	if !slices.ContainsFunc(ev, func(e Event) bool { return e.Kind == "over" && e.By == by && e.Who == g.id(p) }) {
		t.Fatalf("no over event by %q in %+v", by, ev)
	}
}

func TestWinTransit(t *testing.T) {
	g := newGame(2, winRules, 1)
	g.Players[1].Cash += 100000 // richer: the instant win beats the worth
	g.bank += 100000
	give(g, 0, 5, 15, 25)
	won(t, g, buyAt(t, g, 0, 35), 0, WinByTransit)
	invariants(t, g)

	g = newGame(2, Rules{StartCash: 1500, Salary: 200, WinGroups: true, WinSide: true}, 1)
	give(g, 0, 5, 15, 25)
	buyAt(t, g, 0, 35)
	if g.Phase == PhaseOver {
		t.Fatal("won with the transit lines, the rule off")
	}
}

func TestWinGroups(t *testing.T) {
	rules := Rules{StartCash: 1500, Salary: 200, WinGroups: true}
	g := newGame(2, rules, 1)
	give(g, 0, 1, 3, 11, 13, 14, 21, 23) // groups 1 and 3 whole, group 5 but square 24
	give(g, 1, 24)
	must(t, g, 0, Action{Kind: "offer", Offer: &Offer{To: 2, GiveCash: 300, Get: []int{24}}})
	won(t, g, must(t, g, 1, Action{Kind: "accept", Trade: g.Trades[0].ID}), 0, WinByGroups)
	invariants(t, g)

	// 2 whole groups only: the game goes on
	g = newGame(2, rules, 1)
	give(g, 0, 1, 11, 13, 14)
	buyAt(t, g, 0, 3)
	if g.Phase == PhaseOver {
		t.Fatal("won with 2 groups")
	}
}

func TestWinSide(t *testing.T) {
	g := newGame(3, Rules{StartCash: 1500, Salary: 200, WinSide: true}, 1)
	give(g, 0, 1, 3, 6, 8) // the first side, but square 9
	g.Turn, g.Phase, g.Players[0].Pos = 0, PhaseBuy, 9
	must(t, g, 0, Action{Kind: "decline"})
	must(t, g, 0, Action{Kind: "bid", Amount: 60})
	must(t, g, 1, Action{Kind: "pass"})
	won(t, g, must(t, g, 2, Action{Kind: "pass"}), 0, WinBySide)
	invariants(t, g)

	// a whole side with groups of two sides is not a side
	g = newGame(2, Rules{StartCash: 1500, Salary: 200, WinSide: true}, 1)
	give(g, 0, 1, 3, 6, 8, 11, 13)
	buyAt(t, g, 0, 14)
	if g.Phase == PhaseOver {
		t.Fatal("won with a side not whole")
	}
}

// A bankruptcy hands the creditor the last transit line: the creditor wins at once, though
// several players are still in.
func TestWinByBankruptcy(t *testing.T) {
	g := newGame(3, winRules, 1)
	give(g, 1, 5, 15, 25)
	give(g, 0, 35)
	g.Turn, g.Phase, g.Debt = 0, PhaseDebt, &Debt{Amount: 100000, To: 1, then: func() {}}
	won(t, g, must(t, g, 0, Action{Kind: "bankrupt"}), 1, WinByTransit)
	invariants(t, g)
}

// A player in debt who gets the last transit line by a trade does not win before paying: the
// win would wipe the debt out.
func TestNoWinInDebt(t *testing.T) {
	g := newGame(3, winRules, 1)
	give(g, 0, 5, 15, 25)
	give(g, 2, 35)
	g.Turn, g.Phase, g.Debt = 0, PhaseDebt, &Debt{Amount: 200, To: 1, then: g.settle}
	must(t, g, 2, Action{Kind: "offer", Offer: &Offer{To: 1, Give: []int{35}}})
	must(t, g, 0, Action{Kind: "accept", Trade: g.Trades[0].ID})
	if g.Phase != PhaseDebt || g.Deeds[35].Owner != 0 {
		t.Fatalf("won in debt: phase %s, owner %d", g.Phase, g.Deeds[35].Owner)
	}
	won(t, g, must(t, g, 0, Action{Kind: "pay"}), 0, WinByTransit)
	if g.Players[1].Cash != 1500+200 {
		t.Fatalf("creditor cash %d, the debt was not paid", g.Players[1].Cash)
	}
	invariants(t, g)
}
