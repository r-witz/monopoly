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
	Group     int    `json:"group,omitempty"`     // streets: 1 to 8, the set to own whole to build
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

// street builds a street of a group, its rents by level (bare, 1 to 3 houses, the tower) and the
// cost of one level.
//
// The rents are those of Business Tour, in thousands, from a player's notes (its rent of every
// city at every level): our group n takes the cities of its group n in order. A group of 3 for our
// 2 streets drops its middle city; a group of 2 for our 3 streets gives its cheaper city to two
// streets. Its bare rents are then those of the classic game, so prices and build costs stay the
// classic ones.
func street(name string, group, price, build int, rent ...int) Square {
	return Square{Kind: KindStreet, Name: name, Group: group, Price: price, Rent: rent, BuildCost: build}
}

// Classic is the board of Quartiers: 40 squares, a fictional harbour town. Four corners and four
// sides of 9 squares; the streets of a group are all on the same side. Street rents are those of
// Business Tour; prices, build costs, transit, utilities, taxes and fines those of the classic game.
var Classic = &Board{
	Squares: []Square{
		{Kind: KindStart, Name: "Départ"},
		street("Ruelle des Tanneurs", 1, 60, 50, 2, 25, 50, 75, 150),
		{Kind: KindCard, Name: "Rumeur"},
		street("Impasse du Lavoir", 1, 60, 50, 4, 30, 60, 90, 180),
		{Kind: KindTax, Name: "Taxe foncière", Tax: 200},
		{Kind: KindTransit, Name: "Ligne Nord", Price: 200},
		street("Rue des Ateliers", 2, 100, 50, 6, 33, 65, 98, 195),
		{Kind: KindCard, Name: "Rumeur"},
		street("Rue du Marché", 2, 100, 50, 6, 35, 75, 105, 210),
		street("Place des Halles", 2, 120, 50, 8, 38, 75, 113, 225),

		{Kind: KindJail, Name: "Poste de police"},
		street("Quai des Pêcheurs", 3, 140, 100, 10, 70, 140, 210, 385),
		{Kind: KindUtility, Name: "Compagnie des eaux", Price: 150},
		street("Rue du Port", 3, 140, 100, 10, 75, 150, 225, 413),
		street("Avenue du Phare", 3, 160, 100, 12, 80, 160, 240, 440),
		{Kind: KindTransit, Name: "Ligne Est", Price: 200},
		street("Rue des Remparts", 4, 180, 100, 14, 85, 170, 255, 468),
		{Kind: KindCard, Name: "Rumeur"},
		street("Porte Saint-Clair", 4, 180, 100, 14, 85, 170, 255, 468),
		street("Montée du Château", 4, 200, 100, 16, 90, 180, 270, 495),

		{Kind: KindRest, Name: "Jardin public"},
		street("Boulevard des Arts", 5, 220, 150, 18, 113, 225, 338, 619),
		{Kind: KindCard, Name: "Rumeur"},
		street("Rue du Théâtre", 5, 220, 150, 18, 113, 225, 338, 619),
		street("Place de l'Opéra", 5, 240, 150, 20, 120, 240, 360, 660),
		{Kind: KindTransit, Name: "Ligne Sud", Price: 200},
		street("Rue de la Bibliothèque", 6, 260, 150, 22, 128, 255, 383, 701),
		street("Place de l'Université", 6, 260, 150, 22, 135, 270, 405, 743),
		{Kind: KindUtility, Name: "Centrale électrique", Price: 150},
		street("Avenue des Sciences", 6, 280, 150, 24, 143, 285, 428, 784),

		{Kind: KindGoToJail, Name: "Au poste !"},
		street("Cours des Tilleuls", 7, 300, 200, 26, 170, 340, 510, 935),
		street("Avenue des Platanes", 7, 300, 200, 26, 170, 340, 510, 935),
		{Kind: KindCard, Name: "Rumeur"},
		street("Allée des Cèdres", 7, 320, 200, 28, 180, 360, 540, 990),
		{Kind: KindTransit, Name: "Ligne Ouest", Price: 200},
		{Kind: KindCard, Name: "Rumeur"},
		street("Esplanade du Belvédère", 8, 350, 200, 35, 190, 380, 570, 1045),
		{Kind: KindTax, Name: "Contravention", Tax: 100},
		street("Colline des Ambassades", 8, 400, 200, 50, 200, 400, 600, 1100),
	},
	Cards: []Card{
		{Text: "Votre start-up lève des fonds : recevez 150.", Effect: Collect, Amount: 150},
		{Text: "Trop-perçu des impôts : recevez 60.", Effect: Collect, Amount: 60},
		{Text: "Vous gagnez un concours de photo : recevez 30.", Effect: Collect, Amount: 30},
		{Text: "Facture du vétérinaire : payez 80.", Effect: Pay, Amount: 80},
		{Text: "Trottinette sur le trottoir : payez 40.", Effect: Pay, Amount: 40},
		{Text: "Retour au Départ.", Effect: MoveTo, Square: 0},
		{Text: "Balade jusqu'à l'Esplanade du Belvédère.", Effect: MoveTo, Square: 37},
		{Text: "Pique-nique au Jardin public.", Effect: MoveTo, Square: 20},
		{Text: "Prenez la ligne la plus proche.", Effect: NextTransit},
		{Text: "Demi-tour ! Reculez de 3 cases.", Effect: MoveBy, Amount: -3},
		{Text: "Contrôle surprise : direction le poste.", Effect: ToJail},
		{Text: "Une amie avocate vous doit un service : sortez du poste gratuitement.", Effect: JailFree},
		{Text: "C'est votre tournée : payez 20 à chaque joueur.", Effect: PayEach, Amount: 20},
		{Text: "Ravalement de façades : payez 25 par maison et 100 par tour.", Effect: Repairs, PerHouse: 25, PerTower: 100},
	},
	Jail:          10,
	JailFine:      50,
	JailTurns:     3,
	MaxLevel:      4,
	TransitRent:   []int{25, 50, 100, 200},
	UtilityFactor: []int{4, 10},
	MortgageRate:  10,
}
