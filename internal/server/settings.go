package server

import "encoding/json"

// maxPlayers is the most players a room takes: as many as there are colors.
const maxPlayers = 8

// settings are how a room plays, chosen by its owner, in the lobby or at any time: a game uses
// those it started with (Room.play).
type settings struct {
	Public      bool   `json:"public"`      // in the list of rooms
	Max         int    `json:"max"`         // connected players the room takes, 2 to maxPlayers
	StartCash   int    `json:"startCash"`   // 500 to 5000
	Salary      int    `json:"salary"`      // on passing the start, 0 to 1000
	End         string `json:"end"`         // "rounds": the richest after Rounds; "time": after Minutes; "survivor": the last one standing
	Rounds      int    `json:"rounds"`      // 5 to 200
	Minutes     int    `json:"minutes"`     // 5 to 240
	TurnSeconds int    `json:"turnSeconds"` // to decide, 10 to 300, then the timer plays (engine.Game.Timeout)
	// Instant wins (engine.Rules): owning every transit line, 3 whole groups, or every street of
	// a side of the board ends the game at once. The side is off by default: its 2 groups are cheap
	// (440 for the first side).
	WinTransit bool `json:"winTransit"`
	WinGroups  bool `json:"winGroups"`
	WinSide    bool `json:"winSide"`
}

// defaults are the settings of a room whose creator did not say.
func defaults() settings {
	return settings{Max: 6, StartCash: 2000, Salary: 200, End: "rounds", Rounds: 25, Minutes: 45,
		TurnSeconds: 30, WinTransit: true, WinGroups: true}
}

// UnmarshalJSON reads the settings a client sent over the defaults: what it leaves out keeps its
// default.
func (s *settings) UnmarshalJSON(data []byte) error {
	type plain settings // the same fields, without this method
	p := plain(defaults())
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*s = settings(p)
	return nil
}

// clean makes settings a client sent fit to keep: every number within its bounds.
func (s *settings) clean() {
	clamp := func(v, lo, hi int) int { return max(lo, min(hi, v)) }
	s.Max = clamp(s.Max, 2, maxPlayers)
	s.StartCash = clamp(s.StartCash, 500, 5000)
	s.Salary = clamp(s.Salary, 0, 1000)
	if s.End != "time" && s.End != "survivor" {
		s.End = "rounds"
	}
	s.Rounds = clamp(s.Rounds, 5, 200)
	s.Minutes = clamp(s.Minutes, 5, 240)
	s.TurnSeconds = clamp(s.TurnSeconds, 10, 300)
}
