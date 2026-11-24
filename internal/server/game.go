package server

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	mrand "math/rand/v2"
	"time"

	"quartiers/internal/engine"
)

// startGame starts a game with the connected members, in a random order. It returns the error
// code when it cannot, "" when it did.
func (r *Room) startGame(t float64) string {
	if r.playing() {
		return "playing"
	}
	var seated []*member
	for _, m := range r.members {
		m.seat = -1
		if m.s.connected() && len(seated) < maxPlayers {
			seated = append(seated, m)
		}
	}
	if len(seated) < 2 {
		return "players"
	}
	var b [8]byte
	rand.Read(b[:])
	seed := binary.LittleEndian.Uint64(b[:])
	order := seated[:0:0]
	for _, i := range mrand.New(mrand.NewPCG(seed, 0)).Perm(len(seated)) {
		order = append(order, seated[i])
	}
	ids := make([]uint32, len(order))
	for i, m := range order {
		m.seat, ids[i] = i, m.s.id
	}
	r.play = r.settings
	rules := engine.Rules{StartCash: r.play.StartCash, Salary: r.play.Salary, StartProps: r.play.StartProps}
	if r.play.End == "rounds" {
		rules.MaxRounds = r.play.Rounds
	}
	r.game = engine.New(r.hub.board, rules, ids, seed)
	r.seq++
	r.step = -1
	r.endAt = 0
	if r.play.End == "time" {
		r.endAt = t + float64(r.play.Minutes*60)
	}
	r.push()
	r.publish(r.game.Events(), t)
	return ""
}

// act plays a move of member m.
func (r *Room) act(m *member, a engine.Action, t float64) {
	if r.game == nil || m.seat < 0 {
		m.s.send(textFrame([]byte(`{"t":"err","code":"act","reason":"watching"}`)))
		return
	}
	events, err := r.game.Apply(m.seat, a)
	if err != nil {
		m.s.send(textFrame(fmt.Appendf(nil, `{"t":"err","code":"act","reason":%q}`, err.Error())))
		return
	}
	r.publish(events, t)
}

// publish tells everybody what just happened, after starting the timer again when the game waits
// for a new decision.
func (r *Room) publish(events []engine.Event, t float64) {
	g := r.game
	if g.Step != r.step {
		r.step = g.Step
		secs := r.play.TurnSeconds
		if g.Phase == engine.PhaseAuction {
			secs = r.play.AuctionSeconds
		}
		r.deadline = t + float64(secs)
	}
	r.broadcast(r.gameFrame(events, t))
	if g.Phase == engine.PhaseOver {
		r.push() // the room is no longer playing
	}
}

func (r *Room) gameFrame(events []engine.Event, t float64) []byte {
	if events == nil {
		events = []engine.Event{}
	}
	msg := gameMsg{T: "game", Seq: r.seq, State: r.game.View(), Events: events}
	if r.playing() {
		msg.Deadline = ms(r.deadline - t)
		if r.endAt > 0 {
			msg.EndsIn = ms(r.endAt - t)
		}
	}
	return jsonFrame(msg)
}

func ms(s float64) int64 { return max(0, int64(math.Ceil(s*1000))) }

// stop ends the game now, by the owner: nobody wins.
func (r *Room) stop() {
	r.game = nil
	r.broadcast(textFrame([]byte(`{"t":"stopped"}`)))
	r.push()
}

// tick brings the room up to hub time t and returns how long it can sleep before the next tick:
// until a timer runs out, a second at most.
func (r *Room) tick(t float64) time.Duration {
	if t-r.lastGC >= 1 {
		r.lastGC = t
		if r.housekeeping(t); r.closed {
			return 0
		}
	}
	if r.playing() {
		switch {
		case r.endAt > 0 && t >= r.endAt:
			r.publish(r.game.Finish(), t)
		case t >= r.deadline:
			r.publish(r.game.Timeout(), t)
		}
	}
	if r.dirty {
		r.push()
	}
	if !r.playing() {
		return tickIdle
	}
	next := r.deadline
	if r.endAt > 0 {
		next = min(next, r.endAt)
	}
	return max(0, min(tickIdle, time.Duration((next-t)*float64(time.Second))))
}
