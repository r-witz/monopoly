// Package server runs the rooms: who sits where, and the game they play (package engine), with
// its timers.
//
// Concurrency: every room owns its state behind its own mutex. Rooms are ticked by a few shard
// goroutines, so they run in parallel and a quiet room costs nothing; messages are handled on
// the goroutine of the connection they came from. The hub only keeps the registries.
// Lock order is Room.mu, then Hub.mu: never take a room lock while holding the hub lock.
package server

import (
	"cmp"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"quartiers/internal/engine"
)

type Config struct {
	MaxRooms   int
	TrustProxy bool // read the client IP from X-Forwarded-For (one reverse proxy in front)
}

const (
	maxFailures   = 10 // distinct unknown room codes per IP...
	failureWindow = 5 * time.Minute
	sessionTTL    = 2 * time.Minute // a player outside any room is forgotten this long after leaving
)

// Session is a player, identified by the secret token its browser tab generated. It outlives its
// connection so a refresh or a network blip brings the player back into the room.
type Session struct {
	id      uint32
	conn    atomic.Pointer[Conn]
	room    atomic.Pointer[Room]
	profile atomic.Pointer[profile]
	goneAt  atomic.Int64 // hub time (ns) its last connection dropped
}

// profile is what a player shows of itself. One never changes once a session points to it:
// whoever reads it needs no lock.
type profile struct {
	name  string
	color int // an index in colors: the one it likes, which a room may already have given
}

func (s *Session) connected() bool { return s.conn.Load() != nil }

func (s *Session) send(wire []byte) {
	if c := s.conn.Load(); c != nil {
		c.send(wire)
	}
}

// failures are the unknown codes an IP tried. The same code again does not count: every tab of a
// browser asks again for the room it was in after a restart, and trying a code twice finds nothing new.
type failures struct {
	codes []string
	until time.Time
}

type Hub struct {
	cfg      Config
	board    *engine.Board
	start    time.Time
	upgrader websocket.Upgrader
	shards   []*shard

	mu       sync.Mutex
	rooms    map[string]*Room
	list     []byte    // the public rooms, as sent
	listAt   time.Time // when it was made
	sessions map[string]*Session
	failed   map[string]*failures
	nextID   uint32
}

// NewHub starts a hub whose rooms play on board.
func NewHub(cfg Config, board *engine.Board) *Hub {
	h := &Hub{
		cfg:      cfg,
		board:    board,
		start:    time.Now(),
		upgrader: websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 256, WriteBufferPool: &sync.Pool{}},
		rooms:    map[string]*Room{},
		sessions: map[string]*Session{},
		failed:   map[string]*failures{},
	}
	n := runtime.GOMAXPROCS(0)
	for i := 0; i < n; i++ {
		s := &shard{hub: h, kick: make(chan struct{}, 1), offset: tickFast * time.Duration(i) / time.Duration(n)}
		h.shards = append(h.shards, s)
		go s.run()
	}
	go h.loop()
	return h
}

// now is the hub clock, in seconds.
func (h *Hub) now() float64 { return time.Since(h.start).Seconds() }

// loop pings every connection and forgets stale sessions and failures.
func (h *Hub) loop() {
	var all []*Session
	for {
		time.Sleep(pingEvery)
		now, hubNow := time.Now(), time.Since(h.start)
		all = all[:0]
		h.mu.Lock()
		for token, s := range h.sessions {
			if !s.connected() && s.room.Load() == nil && hubNow-time.Duration(s.goneAt.Load()) > sessionTTL {
				delete(h.sessions, token)
			} else {
				all = append(all, s)
			}
		}
		for ip, f := range h.failed {
			if now.After(f.until) {
				delete(h.failed, ip)
			}
		}
		h.mu.Unlock()
		// a connection that does not answer (or never said hello) hits its read deadline
		for _, s := range all {
			s.send(pingFrame)
		}
	}
}

func (h *Hub) clientIP(r *http.Request) string {
	if h.cfg.TrustProxy {
		// the last entry is the one our own proxy appended; anything before it is client-supplied
		parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Close tells every connected player that the server is going away and coming back: their
// client reconnects, a little later each time it fails.
func (h *Hub) Close() {
	var conns []*Conn
	h.mu.Lock()
	for _, s := range h.sessions {
		if c := s.conn.Load(); c != nil {
			conns = append(conns, c)
		}
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.close(closeRestart)
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Conn{hub: h, ws: ws, ip: h.clientIP(r)}
	if sc, ok := ws.NetConn().(syscall.Conn); ok {
		if raw, err := sc.SyscallConn(); err == nil {
			c.direct = newDirectWriter(raw)
		}
	}
	// Not on this goroutine: returning lets net/http free the request, its two 4 kB buffers and
	// its stack, which is most of what an idle connection would otherwise hold.
	go c.readLoop()
}

func (h *Hub) disconnect(c *Conn) {
	c.close(0)
	s := c.sess
	if s == nil || !s.conn.CompareAndSwap(c, nil) {
		return // never said hello, or already replaced by a newer connection
	}
	s.goneAt.Store(int64(time.Since(h.start)))
	if room := s.room.Load(); room != nil {
		room.do(func() { room.left(s) })
	}
}

func (h *Hub) handle(c *Conn, m *clientMsg) {
	s := c.sess
	if s == nil {
		if m.T == "hello" {
			h.hello(c, m.Token)
		}
		return
	}
	room := s.room.Load()
	switch {
	case m.T == "profile":
		old := s.profile.Load()
		p := profile{clean(m.Name, maxName), old.color}
		if m.Color >= 0 && m.Color < len(colors) {
			p.color = m.Color
		}
		if p == *old {
			return // nothing new for the room
		}
		s.profile.Store(&p)
		if room != nil {
			room.do(func() { room.profiled(s) })
		}
	case room != nil && m.T == "leave":
		room.do(func() { room.remove(s, "left") })
	case room != nil:
		room.handle(s, m)
	case m.T == "create":
		h.create(c, s, m)
	case m.T == "join":
		h.join(c, s, strings.ToUpper(strings.TrimSpace(m.Code)), m.Listed)
	case m.T == "rooms":
		c.send(h.roomList())
	}
}

// roomList is the public rooms, those one can still sit in first. It is made again a second
// after the last time at the earliest, however many ask.
func (h *Hub) roomList() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if now := time.Now(); h.list == nil || now.Sub(h.listAt) > time.Second {
		rooms := []roomEntry{}
		for _, r := range h.rooms {
			if e := r.listing.Load(); e != nil {
				rooms = append(rooms, *e)
			}
		}
		// those waiting for players, then those playing, then the full ones; the liveliest first
		order := func(e roomEntry) int {
			switch {
			case e.Players >= e.Max:
				return 2
			case e.Playing:
				return 1
			}
			return 0
		}
		slices.SortFunc(rooms, func(a, b roomEntry) int {
			return cmp.Or(cmp.Compare(order(a), order(b)), cmp.Compare(b.Players, a.Players), cmp.Compare(a.Code, b.Code))
		})
		rooms = rooms[:min(len(rooms), maxListed)]
		h.list, h.listAt = jsonFrame(struct {
			T     string      `json:"t"`
			Rooms []roomEntry `json:"rooms"`
		}{"rooms", rooms}), now
	}
	return h.list
}

func (h *Hub) hello(c *Conn, token string) {
	if len(token) < 8 || len(token) > 64 {
		return
	}
	h.mu.Lock()
	s := h.sessions[token]
	if s == nil {
		h.nextID++
		s = &Session{id: h.nextID}
		s.profile.Store(&profile{color: int(s.id-1) % len(colors)})
		h.sessions[token] = s
	}
	old := s.conn.Swap(c)
	h.mu.Unlock()
	if old != nil {
		old.close(closeReplaced)
	}
	c.sess = s
	p := s.profile.Load()
	c.send(jsonFrame(welcomeMsg{"welcome", s.id, p.name, p.color, colors, h.board}))
	if room := s.room.Load(); room != nil {
		var refusal []byte
		room.do(func() { refusal = room.admit(s) }) // back in its seat, if it is still there
		if refusal != nil {
			c.send(refusal)
		}
	}
}

func (h *Hub) create(c *Conn, s *Session, m *clientMsg) {
	// its shard is set before anybody else can find the room
	r := &Room{hub: h, name: clean(m.Name, maxRoomName), settings: defaults(), shard: h.shardFor()}
	if m.Settings != nil {
		m.Settings.clean()
		r.settings = *m.Settings
	}
	h.mu.Lock()
	if len(h.rooms) >= h.cfg.MaxRooms {
		h.mu.Unlock()
		c.send(errFrame("busy"))
		return
	}
	for r.code = randomCode(); h.rooms[r.code] != nil; r.code = randomCode() {
	}
	h.rooms[r.code] = r
	h.mu.Unlock()
	r.do(func() { r.admit(s) })
	r.shard.add(r)
}

// join seats s in the room of that code. listed: the player picked it in the list of rooms, and it
// may have gone private since: then it stays out.
func (h *Hub) join(c *Conn, s *Session, code string, listed bool) {
	if len(code) != codeLen { // no room has it: not worth a failure to remember
		c.send(errFrame("notfound"))
		return
	}
	h.mu.Lock()
	room, f := h.rooms[code], h.failed[c.ip]
	if f != nil && time.Now().After(f.until) {
		f = nil
	}
	locked := f != nil && len(f.codes) >= maxFailures
	if room == nil && !locked {
		// unknown codes count, so that rooms cannot be found by trying codes
		if f == nil {
			f = &failures{until: time.Now().Add(failureWindow)}
			h.failed[c.ip] = f
		}
		if !slices.Contains(f.codes, code) {
			f.codes = append(f.codes, code)
		}
	}
	h.mu.Unlock()

	var refusal []byte
	switch {
	case locked:
		refusal = errFrame("locked")
	case room == nil || !room.do(func() {
		if listed && !room.settings.Public && room.member(s.id) == nil {
			refusal = errFrame("private")
		} else {
			refusal = room.admit(s)
		}
	}):
		refusal = errFrame("notfound") // unknown, or it closed meanwhile
	}
	if refusal != nil {
		c.send(refusal)
	}
}
