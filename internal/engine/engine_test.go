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
		if d.Level > 0 {
			lo, hi, mortgaged := g.levels(s.Group)
			if !g.ownsGroup(d.Owner, s.Group) || mortgaged || hi-lo > 1 {
				t.Fatalf("group %d built unevenly, or not whole, or mortgaged", s.Group)
			}
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
	a := Action{Kind: kinds[r.IntN(len(kinds))], Square: r.IntN(len(g.Deeds)+2) - 1, Amount: r.IntN(g.Players[p].Cash+100) - 10}
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
		rules := Rules{StartCash: 300 + r.IntN(2000), Salary: r.IntN(300), MaxRounds: 5 + r.IntN(40)}
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
	must(t, g, 1, Action{Kind: "build", Square: 1})
	if _, err := g.Apply(1, Action{Kind: "build", Square: 1}); err != ErrInvalid {
		t.Fatalf("uneven build: %v", err)
	}
	must(t, g, 1, Action{Kind: "build", Square: 3})
	must(t, g, 1, Action{Kind: "build", Square: 1})
	if r := g.rent(1); r != s.Rent[2] {
		t.Fatalf("rent at level 2: %d, want %d", r, s.Rent[2])
	}
	if _, err := g.Apply(1, Action{Kind: "mortgage", Square: 3}); err != ErrInvalid {
		t.Fatalf("mortgage with buildings in the group: %v", err)
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
	must(t, g, 0, Action{Kind: "build", Square: 1}) // now square 1 cannot change hands
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
	must(t, g, 1, Action{Kind: "bid", Amount: 40})
	if _, err := g.Apply(1, Action{Kind: "build", Square: 3}); err == nil {
		t.Fatal("built during an auction")
	}
	must(t, g, 2, Action{Kind: "bid", Amount: 41})
	must(t, g, 0, Action{Kind: "pass"})
	ev := must(t, g, 1, Action{Kind: "pass"})
	if !has(ev, "auctioned") || g.Deeds[3].Owner != 2 || g.Players[2].Cash != 1500-41 || g.Phase != PhaseEnd {
		t.Fatalf("owner %d cash %d phase %s", g.Deeds[3].Owner, g.Players[2].Cash, g.Phase)
	}
}
