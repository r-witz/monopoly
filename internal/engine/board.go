// Package engine holds the rules of the game: a pure state machine. No clock, no I/O, no locks:
// its owner (a room) serialises every call and runs the timers, calling Timeout when the player
// who must decide takes too long.
package engine

// Kind is what a square does.
type Kind string

const (
	KindStart    Kind = "start"    // collect the salary when passing or landing
	KindStreet   Kind = "street"   // buildable, rent by level
	KindTransit  Kind = "transit"  // rent by how many lines the owner has
	KindUtility  Kind = "utility"  // rent: the dice times a factor by how many the owner has
	KindCard     Kind = "card"     // draw a card
	KindTax      Kind = "tax"      // pay the bank
	KindJail     Kind = "jail"     // the police station: just visiting, or held there
	KindRest     Kind = "rest"     // nothing happens
	KindGoToJail Kind = "gotojail" // straight to the police station
)

type Square struct {
	Kind      Kind   `json:"kind"`
	Name      string `json:"name"`
	Group     int    `json:"group,omitempty"`     // streets: 1 to 6, the set to own whole to build
	Price     int    `json:"price,omitempty"`     // streets, transit, utilities
	Rent      []int  `json:"rent,omitempty"`      // streets: by level, 0 (bare) to MaxLevel
	BuildCost int    `json:"buildCost,omitempty"` // streets: one level; sold back for half
	Tax       int    `json:"tax,omitempty"`
}

func (s *Square) ownable() bool {
	return s.Kind == KindStreet || s.Kind == KindTransit || s.Kind == KindUtility
}

// Effect is what a card does to the player who draws it, and only to that player: nobody else
// can end up owing money because of a card.
type Effect string

const (
	Collect     Effect = "collect"     // Amount from the bank
	Pay         Effect = "pay"         // Amount to the bank
	MoveTo      Effect = "moveTo"      // forward to Square, salary when passing the start
	MoveBy      Effect = "moveBy"      // Amount squares, backwards when negative (no salary)
	NextTransit Effect = "nextTransit" // forward to the next transit line, normal rent
	ToJail      Effect = "toJail"
	JailFree    Effect = "jailFree" // kept until used to leave the police station, then back in the deck
	PayEach     Effect = "payEach"  // Amount to every other player still in the game
	Repairs     Effect = "repairs"  // PerHouse per level below MaxLevel, PerTower per street at MaxLevel
)

type Card struct {
	Text     string `json:"text"`
	Effect   Effect `json:"effect"`
	Amount   int    `json:"amount,omitempty"`
	Square   int    `json:"square"`
	PerHouse int    `json:"perHouse,omitempty"`
	PerTower int    `json:"perTower,omitempty"`
}

// Board is everything about the game that never changes during a game. Clients get it whole once.
type Board struct {
	Squares       []Square `json:"squares"`
	Cards         []Card   `json:"cards"`
	Jail          int      `json:"jail"`          // the square of the police station
	JailFine      int      `json:"jailFine"`      // to leave it
	JailTurns     int      `json:"jailTurns"`     // failed rolls before the fine is due anyway
	MaxLevel      int      `json:"maxLevel"`      // levels 1 to MaxLevel-1 are houses, MaxLevel a tower
	TransitRent   []int    `json:"transitRent"`   // by lines owned, 1 to 4
	UtilityFactor []int    `json:"utilityFactor"` // times the dice, by utilities owned, 1 to 2
	MortgageRate  int      `json:"mortgageRate"`  // percent of the mortgage paid back on top of it to lift it
}

// mortgage is what mortgaging a square gives; lifting it costs that plus MortgageRate percent.
func (b *Board) mortgage(sq int) int { return b.Squares[sq].Price / 2 }

func (b *Board) unmortgage(sq int) int {
	m := b.mortgage(sq)
	return m + (m*b.MortgageRate+99)/100
}

// street builds a street of a group: its rent at each level comes from its price, the same
// curve for every street.
func street(name string, group, price int) Square {
	mult := [...]int{10, 50, 140, 300, 420} // percent of the price, by level
	rent := make([]int, len(mult))
	for i, m := range mult {
		rent[i] = (price*m/100 + 2) / 5 * 5 // to the nearest 5
	}
	return Square{Kind: KindStreet, Name: name, Group: group, Price: price, Rent: rent, BuildCost: (price/2 + 5) / 10 * 10}
}

// Classic is the board of Quartiers: 32 squares, a fictional harbour town.
var Classic = &Board{
	Squares: []Square{
		{Kind: KindStart, Name: "Départ"},
		street("Ruelle des Tanneurs", 1, 70),
		{Kind: KindCard, Name: "Rumeur"},
		street("Impasse du Lavoir", 1, 90),
		{Kind: KindTax, Name: "Taxe foncière", Tax: 120},
		{Kind: KindTransit, Name: "Ligne Nord", Price: 180},
		street("Rue des Ateliers", 2, 120),
		street("Rue du Marché", 2, 120),
		{Kind: KindJail, Name: "Poste de police"},
		street("Place des Halles", 2, 140),
		{Kind: KindUtility, Name: "Compagnie des eaux", Price: 140},
		street("Quai des Pêcheurs", 3, 180),
		street("Rue du Port", 3, 180),
		{Kind: KindTransit, Name: "Ligne Est", Price: 180},
		{Kind: KindCard, Name: "Rumeur"},
		street("Avenue du Phare", 3, 200),
		{Kind: KindRest, Name: "Jardin public"},
		street("Boulevard des Arts", 4, 240),
		{Kind: KindCard, Name: "Rumeur"},
		street("Rue du Théâtre", 4, 240),
		street("Place de l'Opéra", 4, 260),
		{Kind: KindTransit, Name: "Ligne Sud", Price: 180},
		street("Cours des Tilleuls", 5, 300),
		{Kind: KindUtility, Name: "Centrale électrique", Price: 140},
		{Kind: KindGoToJail, Name: "Au poste !"},
		street("Avenue des Platanes", 5, 300),
		street("Allée des Cèdres", 5, 320),
		{Kind: KindTransit, Name: "Ligne Ouest", Price: 180},
		{Kind: KindCard, Name: "Rumeur"},
		street("Esplanade du Belvédère", 6, 380),
		{Kind: KindTax, Name: "Contravention", Tax: 80},
		street("Colline des Ambassades", 6, 420),
	},
	Cards: []Card{
		{Text: "Votre start-up lève des fonds : recevez 150.", Effect: Collect, Amount: 150},
		{Text: "Trop-perçu des impôts : recevez 60.", Effect: Collect, Amount: 60},
		{Text: "Vous gagnez un concours de photo : recevez 30.", Effect: Collect, Amount: 30},
		{Text: "Facture du vétérinaire : payez 80.", Effect: Pay, Amount: 80},
		{Text: "Trottinette sur le trottoir : payez 40.", Effect: Pay, Amount: 40},
		{Text: "Retour au Départ.", Effect: MoveTo, Square: 0},
		{Text: "Balade jusqu'à l'Esplanade du Belvédère.", Effect: MoveTo, Square: 29},
		{Text: "Pique-nique au Jardin public.", Effect: MoveTo, Square: 16},
		{Text: "Prenez la ligne la plus proche.", Effect: NextTransit},
		{Text: "Demi-tour ! Reculez de 3 cases.", Effect: MoveBy, Amount: -3},
		{Text: "Contrôle surprise : direction le poste.", Effect: ToJail},
		{Text: "Une amie avocate vous doit un service : sortez du poste gratuitement.", Effect: JailFree},
		{Text: "C'est votre tournée : payez 20 à chaque joueur.", Effect: PayEach, Amount: 20},
		{Text: "Ravalement de façades : payez 25 par maison et 100 par tour.", Effect: Repairs, PerHouse: 25, PerTower: 100},
	},
	Jail:          8,
	JailFine:      60,
	JailTurns:     3,
	MaxLevel:      4,
	TransitRent:   []int{30, 60, 110, 180},
	UtilityFactor: []int{5, 12},
	MortgageRate:  10,
}
