package engine

// What clients are told. There is nothing to hide but the order of the deck: the whole game goes
// to everybody. Players are named by their ID, 0 being the bank (or nobody).

// Event is one thing that happened. Which fields count depends on Kind:
//
//	turn         who: whose turn it is
//	rolled       who, dice
//	salary       who, amount: passing or landing on the start
//	moved        who, square: where they are now
//	jailed       who, square: sent to the police station
//	freed        who, amount: out of it, having paid amount (0: a double or a card)
//	rent, tax    who, to (rent), square, amount: due; "debt" follows when who cannot pay
//	card         who, card: its index in the board's cards
//	debt         who, to, amount: owed, the game waits for who to raise it
//	paid         who, to, amount: a debt paid
//	bought       who, square, amount (the square alone: "built" follows for the houses bought with it)
//	declined     who, square: not bought, it stays free
//	built, sold  who, square, level (now), amount (paid, or got back)
//	mortgaged, unmortgaged   who, square, amount (got, or paid)
//	offer, traded, rejected, cancelled   who, to, trade: a trade proposed by who, accepted by who,
//	             rejected by who, withdrawn by who
//	bankrupt     who, to (the creditor who gets everything, 0: the bank)
//	over         who: the winner, by: the instant win it won by (WinByTransit…), none for the richest
type Event struct {
	Kind   string `json:"kind"`
	Who    uint32 `json:"who,omitempty"`
	To     uint32 `json:"to,omitempty"`
	Square int    `json:"square"`
	Amount int    `json:"amount,omitempty"`
	Dice   []int  `json:"dice,omitempty"`
	Card   int    `json:"card"`
	Level  int    `json:"level,omitempty"`
	Trade  int    `json:"trade,omitempty"`
	By     string `json:"by,omitempty"`
}

func (g *Game) emit(e Event) { g.events = append(g.events, e) }

type View struct {
	Phase     Phase        `json:"phase"`
	Turn      uint32       `json:"turn"`      // the current player
	Round     int          `json:"round"`     // from 1
	MaxRounds int          `json:"maxRounds"` // 0: none
	Dice      [2]int       `json:"dice"`      // the last roll
	Doubles   int          `json:"doubles"`   // doubles in a row this turn: the player rolls again after one, the third sends them to jail
	Here      int          `json:"here"`      // the street the current player may build on now, having landed on it; -1: none
	Players   []PlayerView `json:"players"`   // in the order they play
	Deeds     []DeedView   `json:"deeds"`     // the squares someone owns
	Debt      *DebtView    `json:"debt,omitempty"`
	Trades    []TradeView  `json:"trades"`
	Ranking   []uint32     `json:"ranking"` // the standings now, final once over
	Winner    uint32       `json:"winner,omitempty"`
	WinBy     string       `json:"winBy,omitempty"` // the instant win the winner won by, "" for the richest
}

type PlayerView struct {
	ID        uint32 `json:"id"`
	Cash      int    `json:"cash"`
	Pos       int    `json:"pos"`
	InJail    bool   `json:"inJail"`
	JailTurns int    `json:"jailTurns"`
	JailCards int    `json:"jailCards"`
	Lapped    bool   `json:"lapped"` // has passed the start once: may have 3 houses
	Bankrupt  bool   `json:"bankrupt"`
	Worth     int    `json:"worth"`
}

type DeedView struct {
	Square    int    `json:"square"`
	Owner     uint32 `json:"owner"`
	Level     int    `json:"level"`
	Mortgaged bool   `json:"mortgaged"`
}

type DebtView struct {
	Who    uint32 `json:"who"`
	Amount int    `json:"amount"`
	To     uint32 `json:"to"` // 0: the bank
}

type TradeView struct {
	ID   int    `json:"id"`
	From uint32 `json:"from"`
	Offer
}

func (g *Game) View() View {
	v := View{Phase: g.Phase, Turn: g.id(g.Turn), Round: g.Round, MaxRounds: g.Rules.MaxRounds, Dice: g.Dice, Doubles: g.Doubles, Here: -1,
		Deeds: []DeedView{}, Trades: []TradeView{}, Winner: g.id(g.Winner), WinBy: g.WinBy}
	if h := g.here; h >= 0 && g.Deeds[h].Owner == g.Turn && !g.Deeds[h].Mortgaged {
		v.Here = h // not once traded or mortgaged since landing
	}
	for i, p := range g.Players {
		v.Players = append(v.Players, PlayerView{p.ID, p.Cash, p.Pos, p.InJail, p.JailTurns, p.JailCards, p.Lapped, p.Bankrupt, g.Worth(i)})
	}
	for sq, d := range g.Deeds {
		if d.Owner >= 0 {
			v.Deeds = append(v.Deeds, DeedView{sq, g.id(d.Owner), d.Level, d.Mortgaged})
		}
	}
	if d := g.Debt; d != nil {
		v.Debt = &DebtView{g.id(g.Turn), d.Amount, g.id(d.To)}
	}
	for _, t := range g.Trades {
		v.Trades = append(v.Trades, TradeView{t.ID, g.id(t.From), t.Offer})
	}
	for _, seat := range g.Ranking() {
		v.Ranking = append(v.Ranking, g.id(seat))
	}
	return v
}
