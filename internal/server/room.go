package server

import (
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"quartiers/internal/engine"
)

const (
	tickFast = time.Second / 20 // the beat rooms are woken on
	tickIdle = time.Second      // a room sleeps this long at most: housekeeping

	keptSeats = 8 // places kept for those who dropped, on top of the players: the oldest one goes to a newcomer

	profileDelay = 100 * time.Millisecond // profile changes are told to the room at most this often
)

type member struct {
	s      *Session
	color  int // of its pawn, an index in colors
	leftAt float64
	seat   int // in the room's game, -1: watching
}

// Room is one group of players and the game they play. mu guards everything below it.
type Room struct {
	hub     *Hub
	code    string
	shard   *shard
	due     atomic.Int64              // hub time (ns) of its next tick
	gone    atomic.Bool               // closed, for those who cannot take the lock
	listing atomic.Pointer[roomEntry] // what the list of rooms says of it, for the hub

	mu       sync.Mutex
	closed   bool
	name     string // as its owner called it, or nothing
	settings settings
	members  []*member // in joining order
	owner    *member   // starts the games: who opened the room, then who has been there the longest

	// The game in progress, or the last one, over, until the next starts; nil before the first
	// and once stopped. It plays with the settings it started with, play.
	game     *engine.Game
	play     settings
	seq      int     // games started in this room
	step     int     // the game's Step the deadline was set for
	deadline float64 // hub time the timer plays for whoever must decide
	endAt    float64 // hub time a game with a time limit ends
	dirty    bool    // a profile changed since the last push

	emptySince float64
	lastGC     float64
}

// do runs fn with the room locked and reports whether the room is still open afterwards.
// A panic closes the room instead of taking the server down.
func (r *Room) do(fn func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() {
		if e := recover(); e != nil {
			log.Printf("room %s crashed, closing it: %v\n%s", r.code, e, debug.Stack())
			r.close()
		}
	}()
	if !r.closed {
		fn()
	}
	return !r.closed
}

// poke asks for a tick right now.
func (r *Room) poke() {
	r.due.Store(0)
	r.shard.wake()
}

// later asks for a tick within d at the latest: what waits for it (dirty) is told once for a
// whole burst of changes.
func (r *Room) later(d time.Duration) {
	due := int64(time.Since(r.hub.start) + d)
	for cur := r.due.Load(); cur > due && !r.due.CompareAndSwap(cur, due); cur = r.due.Load() {
	}
	r.shard.wake()
}

func (r *Room) member(id uint32) *member {
	for _, m := range r.members {
		if m.s.id == id {
			return m
		}
	}
	return nil
}

func (r *Room) connected() (n int) {
	for _, m := range r.members {
		if m.s.connected() {
			n++
		}
	}
	return n
}

func (r *Room) broadcast(wire []byte) {
	for _, m := range r.members {
		m.s.send(wire)
	}
}

// playing says whether a game is in progress.
func (r *Room) playing() bool { return r.game != nil && r.game.Phase != engine.PhaseOver }

// seated says whether m is still in the game in progress: their seat is kept for them, whatever
// the room's limits.
func (r *Room) seated(m *member) bool {
	return r.playing() && m.seat >= 0 && !r.game.Players[m.seat].Bankrupt
}

// push sends the room and its people to its members.
func (r *Room) push() {
	r.dirty = false
	playing := r.playing()
	st := roomMsg{T: "room", Code: r.code, Name: r.name, Owner: r.owner.s.id, Settings: r.settings, Playing: playing,
		Players: make([]playerInfo, len(r.members))}
	if e := (roomEntry{r.code, r.name, r.connected(), r.settings.Max, playing}); !r.settings.Public {
		r.listing.Store(nil)
	} else if r.listing.Load() == nil || *r.listing.Load() != e {
		r.listing.Store(&e)
	}
	for i, m := range r.members {
		st.Players[i] = playerInfo{ID: m.s.id, Name: m.s.profile.Load().name, Color: m.color, Connected: m.s.connected(),
			Playing: r.game != nil && m.seat >= 0}
	}
	r.broadcast(jsonFrame(st))
}

// admit seats s, or gives it its seat back, or says why not. Whoever comes in during a game
// watches it.
func (r *Room) admit(s *Session) (refusal []byte) {
	m := r.member(s.id)
	if m == nil {
		if r.connected() >= r.settings.Max {
			return errFrame("full")
		}
		if !s.room.CompareAndSwap(nil, r) {
			return nil // already in another room
		}
		if len(r.members) >= r.settings.Max+keptSeats {
			// fewer than Max are here: the place of whoever left first goes, unless they are in the game
			var away *member
			for _, o := range r.members {
				if !o.s.connected() && !r.seated(o) && (away == nil || o.leftAt < away.leftAt) {
					away = o
				}
			}
			if away == nil {
				s.room.CompareAndSwap(r, nil)
				return errFrame("full")
			}
			r.remove(away.s, "")
		}
		m = &member{s: s, seat: -1}
		r.members = append(r.members, m)
		if r.owner == nil {
			r.owner = m
		}
		r.paint(m, s.profile.Load().color)
	} else if !r.seated(m) && r.connected() > r.settings.Max {
		// back after the room filled up (it counts itself): there is no place left for it
		r.remove(s, "")
		return errFrame("full")
	} else if !r.free(m.color, m, false) {
		r.paint(m, s.profile.Load().color) // its color went to someone who came in meanwhile
	}
	m.leftAt = 0
	r.push()
	if r.game != nil {
		s.send(r.gameFrame(nil, r.hub.now()))
	}
	return nil
}

// left is called when a member's connection drops: it keeps its seat for a while.
func (r *Room) left(s *Session) {
	if m := r.member(s.id); m != nil && !s.connected() {
		m.leftAt = r.hub.now()
		r.push()
	}
}

// profiled is called when a member changed its name or the color it likes.
func (r *Room) profiled(s *Session) {
	if m := r.member(s.id); m != nil {
		if c := s.profile.Load().color; r.free(c, m, true) {
			m.color = c
		}
		r.dirty = true
		r.later(profileDelay)
	}
}

// free says whether no member other than m has that color: among all the members with all set,
// among those connected otherwise.
func (r *Room) free(color int, m *member, all bool) bool {
	for _, o := range r.members {
		if o != m && o.color == color && (all || o.s.connected()) {
			return false
		}
	}
	return true
}

// paint gives m the color its player wants, or the first one nobody here has, or failing that
// (absent players keep theirs) the first one no connected player has. With more players than
// colors, some share one: then it gets the one it wants.
func (r *Room) paint(m *member, want int) {
	for _, all := range [2]bool{true, false} {
		if r.free(want, m, all) {
			m.color = want
			return
		}
		for c := range colors {
			if r.free(c, m, all) {
				m.color = c
				return
			}
		}
	}
	m.color = want
}

func (r *Room) remove(s *Session, reason string) {
	var gone *member
	for i, m := range r.members {
		if m.s == s {
			gone = m
			r.members = append(r.members[:i], r.members[i+1:]...)
			break
		}
	}
	s.room.CompareAndSwap(r, nil)
	if reason != "" {
		s.send(textFrame(fmt.Appendf(nil, `{"t":"left","reason":%q}`, reason)))
	}
	if len(r.members) == 0 {
		r.close()
		return
	}
	if r.owner.s == s {
		// the room goes to who has been there the longest, among those who are there if any
		r.owner = r.members[0]
		for _, m := range r.members {
			if m.s.connected() {
				r.owner = m
				break
			}
		}
	}
	r.push()
	if gone != nil && r.seated(gone) { // out of the room, out of the game
		r.publish(r.game.Resign(gone.seat), r.hub.now())
	}
}

// kick takes a member out of the room, there or not: it may come back with the link.
func (r *Room) kick(id uint32) {
	if o := r.member(id); o != nil && o != r.owner {
		r.remove(o.s, "kicked")
	}
}

func (r *Room) close() {
	if r.closed {
		return
	}
	r.closed = true
	r.gone.Store(true)
	for _, m := range r.members {
		if m.s.room.CompareAndSwap(r, nil) {
			m.s.send(textFrame([]byte(`{"t":"left","reason":"closed"}`)))
		}
	}
	r.members = nil
	r.hub.mu.Lock()
	delete(r.hub.rooms, r.code)
	r.hub.mu.Unlock()
}

// housekeeping closes a deserted room and frees the places of players who are not coming back.
// A player of the game in progress has its seat for longer, the timer playing for it meanwhile.
func (r *Room) housekeeping(t float64) {
	emptyFor := 30.0
	if r.playing() {
		emptyFor = 90
	}
	if r.connected() > 0 {
		r.emptySince = 0
	} else {
		if r.emptySince == 0 {
			r.emptySince = t
		}
		if t-r.emptySince > emptyFor {
			r.close()
			return
		}
	}
	for _, m := range append([]*member(nil), r.members...) {
		seatFor := 20.0
		if r.seated(m) {
			seatFor = 300
		}
		if !m.s.connected() && m.leftAt != 0 && t-m.leftAt > seatFor {
			if r.remove(m.s, ""); r.closed {
				return
			}
		}
	}
}

func (r *Room) handle(s *Session, msg *clientMsg) {
	r.do(func() {
		m := r.member(s.id)
		t := r.hub.now()
		switch {
		case m == nil:
		case msg.T == "act":
			r.act(m, msg.Action, t)
		case msg.T == "start" && m == r.owner:
			if code := r.startGame(t); code != "" {
				s.send(errFrame(code))
			}
		case msg.T == "settings" && m == r.owner && msg.Settings != nil:
			msg.Settings.clean()
			r.name, r.settings = clean(msg.Name, maxRoomName), *msg.Settings
			r.push()
		case msg.T == "stop" && m == r.owner && r.playing():
			r.stop()
		case msg.T == "kick" && m == r.owner:
			r.kick(msg.ID)
		}
	})
	r.poke()
}
