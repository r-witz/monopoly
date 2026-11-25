package engine

import (
	"errors"
	"math/rand/v2"
	"slices"
)

// Phase is the decision the game waits for. Everything else a player may do (manage their
// properties, trade) happens around it.
type Phase string

const (
	PhaseRoll    Phase = "roll"    // the current player rolls (in jail: or pays, or uses a card)
	PhaseBuy     Phase = "buy"     // the current player buys the square they are on, or declines: an auction
	PhaseAuction Phase = "auction" // every player still in may bid until all but the highest bidder passed
	PhaseDebt    Phase = "debt"    // the current player raises the cash they owe, then pays, or goes bankrupt
	PhaseEnd     Phase = "end"     // the current player may still manage, then ends the turn
	PhaseOver    Phase = "over"
)

// Rules are the numbers a room chooses for a game.
type Rules struct {
	StartCash int // each player's cash at the start, their only asset
	Salary    int // when passing or landing on the start
	MaxRounds int // the game ends after this many rounds, the richest winning; 0: no limit
	// Instant wins: owning these ends the game at once, whatever the mortgages.
	WinTransit bool // every transit line
	WinGroups  bool // 3 whole groups
	WinSide    bool // every street of a side of the board
}

// Why an instant win was won (Game.WinBy).
const (
	WinByTransit = "transit"
	WinByGroups  = "groups"
	WinBySide    = "side"
)

// Action is a player's move. Which fields count depends on Kind.
type Action struct {
	Kind   string `json:"a"`      // see Apply
	Square int    `json:"square"` // build, sell, mortgage, unmortgage
	Amount int    `json:"amount"` // bid
	Offer  *Offer `json:"offer"`  // offer
	Trade  int    `json:"trade"`  // accept, reject, cancel: the trade's ID
}

// Errors of Apply. Nothing changed when one is returned.
var (
	ErrOver       = errors.New("over")       // the game is over
	ErrNotPlaying = errors.New("notplaying") // no such player, or bankrupt
	ErrNotYours   = errors.New("notyours")   // another player's decision
	ErrPhase      = errors.New("phase")      // not now
	ErrCash       = errors.New("cash")       // not enough cash
	ErrInvalid    = errors.New("invalid")    // not a move of the rules
	ErrUnknown    = errors.New("unknown")    // no such action
)

type Player struct {
	ID        uint32
	Cash      int
	Pos       int
	InJail    bool
	JailTurns int // failed rolls in jail
	JailCards int
	Bankrupt  bool
	Out       int // the order in which the player went bankrupt, from 1; 0: still in
}

// Deed is the state of a square that can be owned. Owner is a seat, -1 for the bank.
type Deed struct {
	Owner     int
	Level     int
	Mortgaged bool
}

type Auction struct {
	Square int
	High   int    // the highest bid, 0 for none
	Bidder int    // its seat, -1 for none
	Passed []bool // by seat: out of this auction
}

// Debt is what the current player owes and cannot pay yet. To is a seat, -1 for the bank.
type Debt struct {
	Amount int
	To     int
	then   func() // what the turn does once it is paid
}

// Game is one game. Players are addressed by seat, their index in Players.
type Game struct {
	Board   *Board
	Rules   Rules
	Players []*Player
	Deeds   []Deed // by square; Owner -1 for squares nobody can own
	Turn    int    // the seat of the current player
	Round   int    // from 1
	Phase   Phase
	Dice    [2]int // the last roll
	Doubles int    // in a row, this turn
	Auction *Auction
	Debt    *Debt
	Trades  []*Trade
	Winner  int    // seat, -1 before the end (and when the game was stopped with nobody in)
	WinBy   string // an instant win (WinByTransit…), "" when the richest won
	// Step changes whenever the decision the game waits for changes (and on every bid): a timer
	// started for one step is void once Step moved on.
	Step int

	again     bool // the current player rolls again (a double, not out of jail)
	deck      []int
	nextTrade int
	out       int // players bankrupt so far
	bank      int // money the bank put into the game, net: sum of cash = players*StartCash + bank
	rng       *rand.Rand
	dice      func() [2]int
	events    []Event
}

// New starts a game for these players, in this order, the first one to play. seed fixes every
// roll and every shuffle.
func New(b *Board, r Rules, ids []uint32, seed uint64) *Game {
	g := &Game{Board: b, Rules: r, Winner: -1, Round: 1, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
	g.dice = func() [2]int { return [2]int{g.rng.IntN(6) + 1, g.rng.IntN(6) + 1} }
	for _, id := range ids {
		g.Players = append(g.Players, &Player{ID: id, Cash: r.StartCash})
	}
	g.Deeds = make([]Deed, len(b.Squares))
	for i := range g.Deeds {
		g.Deeds[i].Owner = -1
	}
	g.deck = g.rng.Perm(len(b.Cards))
	g.setPhase(PhaseRoll)
	g.emit(Event{Kind: "turn", Who: g.id(0)})
	return g
}

// Events returns what New did, for the first message of a game.
func (g *Game) Events() []Event { return g.flush() }

func (g *Game) flush() []Event {
	ev := g.events
	g.events = nil
	return ev
}

func (g *Game) setPhase(p Phase) {
	g.Phase = p
	g.Step++
}

func (g *Game) id(seat int) uint32 {
	if seat < 0 {
		return 0
	}
	return g.Players[seat].ID
}

func (g *Game) seat(id uint32) int {
	return slices.IndexFunc(g.Players, func(p *Player) bool { return p.ID == id })
}

// Apply plays one action of the player at seat p and returns what happened. On an error nothing
// changed. The actions:
//
//	roll                     the current player, PhaseRoll
//	payjail, jailcard        leave the police station before rolling: pay the fine, or use a card
//	buy, decline             PhaseBuy: the square the current player is on; decline starts an auction
//	bid (amount), pass       PhaseAuction: any player still in it; a bid beats the highest, is at least half the price, within
//	                         the bidder's cash; who passed is out of this auction
//	build, sell (square)     one level, at any time, evenly across the group (the whole group owned,
//	                         none of it mortgaged); not while an auction runs or the builder owes
//	mortgage, unmortgage     the same times; a street only with no building in its group
//	pay, bankrupt            PhaseDebt: pay what is owed, or give up: everything goes to the creditor
//	end                      PhaseEnd: the next player's turn
//	offer (offer)            propose a trade, replacing the proposer's open one; at any time. A
//	                         counter-offer is a reject, then an offer of one's own
//	accept, reject (trade)   the player the trade was offered to; accept checks it again
//	cancel (trade)           the proposer
func (g *Game) Apply(p int, a Action) ([]Event, error) {
	g.events = nil
	if g.Phase == PhaseOver {
		return nil, ErrOver
	}
	if p < 0 || p >= len(g.Players) || g.Players[p].Bankrupt {
		return nil, ErrNotPlaying
	}
	var err error
	switch a.Kind {
	case "roll":
		err = g.roll(p)
	case "payjail":
		err = g.payJail(p)
	case "jailcard":
		err = g.useJailCard(p)
	case "buy":
		err = g.buy(p)
	case "decline":
		err = g.decline(p)
	case "bid":
		err = g.bid(p, a.Amount)
	case "pass":
		err = g.pass(p)
	case "build":
		err = g.build(p, a.Square)
	case "sell":
		err = g.sell(p, a.Square)
	case "mortgage":
		err = g.mortgage(p, a.Square)
	case "unmortgage":
		err = g.unmortgage(p, a.Square)
	case "pay":
		err = g.payDebt(p)
	case "bankrupt":
		if g.Phase != PhaseDebt || p != g.Turn {
			err = ErrPhase
		} else {
			g.bankrupt(p, g.Debt.To)
		}
	case "end":
		err = g.endTurn(p)
	case "offer":
		err = g.offer(p, a.Offer)
	case "accept":
		err = g.accept(p, a.Trade)
	case "reject":
		err = g.reject(p, a.Trade)
	case "cancel":
		err = g.cancel(p, a.Trade)
	default:
		err = ErrUnknown
	}
	if err != nil {
		g.events = nil
		return nil, err
	}
	return g.flush(), nil
}

// Timeout plays what the rules do when the player who must decide does not: roll, decline,
// close the auction, raise the cash owed by selling and mortgaging (or go bankrupt), end the
// turn. It always moves the game on.
func (g *Game) Timeout() []Event {
	g.events = nil
	switch g.Phase {
	case PhaseRoll:
		g.roll(g.Turn)
	case PhaseBuy:
		g.decline(g.Turn)
	case PhaseAuction:
		g.closeAuction()
	case PhaseDebt:
		g.liquidate(g.Turn, g.Debt.Amount)
		if g.payDebt(g.Turn) != nil {
			g.bankrupt(g.Turn, g.Debt.To)
		}
	case PhaseEnd:
		g.nextTurn()
	}
	return g.flush()
}

// Resign takes the player at seat p out of the game: bankrupt, their properties back to the bank
// (or to whoever they owe, when they are in debt). For a player who leaves.
func (g *Game) Resign(p int) []Event {
	g.events = nil
	if g.Phase != PhaseOver && !g.Players[p].Bankrupt {
		to := -1
		if g.Phase == PhaseDebt && p == g.Turn {
			to = g.Debt.To
		}
		g.bankrupt(p, to)
	}
	return g.flush()
}

// Finish ends the game now: the richest player wins.
func (g *Game) Finish() []Event {
	g.events = nil
	if g.Phase != PhaseOver {
		g.finish(-1, "")
	}
	return g.flush()
}

// finish ends the game: winner wins by an instant win (by), or with -1 the richest wins.
func (g *Game) finish(winner int, by string) {
	g.Auction, g.Debt, g.Trades = nil, nil, nil
	g.setPhase(PhaseOver)
	g.Winner, g.WinBy = winner, by
	if winner < 0 {
		if r := g.Ranking(); len(r) > 0 {
			g.Winner = r[0]
		}
	}
	g.emit(Event{Kind: "over", Who: g.id(g.Winner), By: by})
}

// checkWin ends the game when one of the players ps, in that order, owns what an instant win of
// the rules asks for, and says whether it did. A player who owes money cannot win before paying
// it (payDebt checks again then): winning would wipe the debt out.
func (g *Game) checkWin(ps ...int) bool {
	for _, p := range ps {
		if p < 0 || g.Players[p].Bankrupt || g.Debt != nil && p == g.Turn {
			continue
		}
		if by := g.instantWin(p); by != "" {
			g.finish(p, by)
			return true
		}
	}
	return false
}

// instantWin is the instant win p owns, "" for none.
func (g *Game) instantWin(p int) string {
	r, b := g.Rules, g.Board
	if r.WinTransit {
		lines := 0
		for _, s := range b.Squares {
			if s.Kind == KindTransit {
				lines++
			}
		}
		if lines > 0 && g.owned(p, KindTransit) == lines {
			return WinByTransit
		}
	}
	if !r.WinGroups && !r.WinSide {
		return ""
	}
	whole := map[int]bool{} // by group: owned whole by p
	side := len(b.Squares) / 4
	sides := make([][]int, 4) // the groups of each side
	for sq, s := range b.Squares {
		if s.Kind != KindStreet {
			continue
		}
		if _, ok := whole[s.Group]; !ok {
			whole[s.Group] = g.ownsGroup(p, s.Group)
			sides[min(sq/side, 3)] = append(sides[min(sq/side, 3)], s.Group)
		}
	}
	if r.WinGroups {
		n := 0
		for _, w := range whole {
			if w {
				n++
			}
		}
		if n >= 3 {
			return WinByGroups
		}
	}
	if r.WinSide {
		for _, groups := range sides {
			all := len(groups) > 0
			for _, gr := range groups {
				all = all && whole[gr]
			}
			if all {
				return WinBySide
			}
		}
	}
	return ""
}

// Worth is what a player has: cash, properties at their price (half when mortgaged), buildings
// at their cost.
func (g *Game) Worth(p int) int {
	w := g.Players[p].Cash
	for sq, d := range g.Deeds {
		if d.Owner != p {
			continue
		}
		s := &g.Board.Squares[sq]
		w += s.Price + d.Level*s.BuildCost
		if d.Mortgaged {
			w -= g.Board.mortgage(sq)
		}
	}
	return w
}

// Ranking is the seats of the players still in, richest first, then those who went bankrupt,
// the last one to go first.
func (g *Game) Ranking() []int {
	seats := make([]int, len(g.Players))
	key := make([]int, len(g.Players)) // higher ranks first
	for i, p := range g.Players {
		seats[i] = i
		if p.Bankrupt {
			key[i] = p.Out - len(g.Players) - 1 // below any worth, which is never negative
		} else {
			key[i] = g.Worth(i)
		}
	}
	slices.SortStableFunc(seats, func(a, b int) int { return key[b] - key[a] })
	if g.WinBy != "" { // an instant win: the winner first, whatever the worth
		i := slices.Index(seats, g.Winner)
		seats = append(append([]int{g.Winner}, seats[:i]...), seats[i+1:]...)
	}
	return seats
}

func (g *Game) alive() (n int) {
	for _, p := range g.Players {
		if !p.Bankrupt {
			n++
		}
	}
	return n
}

// transfer moves cash between seats, -1 being the bank. The payer has it.
func (g *Game) transfer(from, to, amount int) {
	if from >= 0 {
		g.Players[from].Cash -= amount
	} else {
		g.bank += amount
	}
	if to >= 0 {
		g.Players[to].Cash += amount
	} else {
		g.bank -= amount
	}
}

// nextTurn hands the turn to the next player still in, or ends the game.
func (g *Game) nextTurn() {
	g.Doubles, g.again, g.Debt = 0, false, nil
	if g.alive() <= 1 {
		g.finish(-1, "")
		return
	}
	i := g.Turn
	for {
		if i++; i == len(g.Players) {
			i = 0
			g.Round++
		}
		if !g.Players[i].Bankrupt {
			break
		}
	}
	if g.Rules.MaxRounds > 0 && g.Round > g.Rules.MaxRounds {
		g.Round = g.Rules.MaxRounds
		g.finish(-1, "")
		return
	}
	g.Turn = i
	g.setPhase(PhaseRoll)
	g.emit(Event{Kind: "turn", Who: g.id(i)})
}

func (g *Game) endTurn(p int) error {
	if p != g.Turn {
		return ErrNotYours
	}
	if g.Phase != PhaseEnd {
		return ErrPhase
	}
	g.nextTurn()
	return nil
}
