package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"quartiers/internal/engine"
)

type inMsg struct {
	T        string         `json:"t"`
	ID       uint32         `json:"id"`
	Code     string         `json:"code"`
	Reason   string         `json:"reason"`
	Playing  bool           `json:"playing"`
	State    *engine.View   `json:"state"`
	Events   []engine.Event `json:"events"`
	Deadline int64          `json:"deadline"`
	EndsIn   int64          `json:"endsIn"`
}

// bot is a player over a real websocket: it plays whatever the game waits from it.
type bot struct {
	t     *testing.T
	url   string
	token string
	ws    *websocket.Conn
	id    uint32
	mu    sync.Mutex
	got   []inMsg // every message, in order
}

func dial(t *testing.T, url, token string) *bot {
	b := &bot{t: t, url: url, token: token}
	b.connect()
	return b
}

func (b *bot) connect() {
	ws, _, err := websocket.DefaultDialer.Dial(b.url, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	b.ws = ws
	b.send(map[string]any{"t": "hello", "token": b.token})
	welcome := b.read()
	if welcome.T != "welcome" {
		b.t.Fatalf("first message %q", welcome.T)
	}
	b.id = welcome.ID
}

func (b *bot) send(v any) {
	if err := b.ws.WriteJSON(v); err != nil {
		b.t.Logf("send: %v", err)
	}
}

func (b *bot) read() inMsg {
	b.ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := b.ws.ReadMessage()
	if err != nil {
		return inMsg{T: "closed"}
	}
	var m inMsg
	if err := json.Unmarshal(data, &m); err != nil {
		b.t.Fatalf("%s: %v", data, err)
	}
	b.mu.Lock()
	b.got = append(b.got, m)
	b.mu.Unlock()
	return m
}

// until reads until a message satisfies ok, or the connection closes (a test error).
func (b *bot) until(ok func(inMsg) bool) inMsg {
	for {
		m := b.read()
		if m.T == "closed" {
			b.t.Error("connection closed while waiting")
			return m
		}
		if ok(m) {
			return m
		}
	}
}

// move is what the bot plays on that state, nil when it is not its turn to decide.
func (b *bot) move(v *engine.View) map[string]any {
	var me *engine.PlayerView
	for i := range v.Players {
		if v.Players[i].ID == b.id {
			me = &v.Players[i]
		}
	}
	if me == nil || me.Bankrupt {
		return nil
	}
	mine := v.Turn == b.id
	switch {
	case v.Phase == engine.PhaseRoll && mine:
		return map[string]any{"t": "act", "a": "roll"}
	case v.Phase == engine.PhaseBuy && mine:
		if me.Cash >= engine.Classic.Squares[me.Pos].Price+100 {
			return map[string]any{"t": "act", "a": "buy"}
		}
		return map[string]any{"t": "act", "a": "decline"}
	case v.Phase == engine.PhaseAuction && v.Auction.Bidder != b.id && !slices.Contains(v.Auction.Passed, b.id):
		if v.Auction.High < v.Auction.Min+30 && me.Cash > 200 {
			return map[string]any{"t": "act", "a": "bid", "amount": max(v.Auction.High+10, v.Auction.Min)}
		}
		return map[string]any{"t": "act", "a": "pass"}
	case v.Phase == engine.PhaseDebt && mine:
		if me.Cash >= v.Debt.Amount {
			return map[string]any{"t": "act", "a": "pay"}
		}
		return map[string]any{"t": "act", "a": "bankrupt"}
	case v.Phase == engine.PhaseEnd && mine:
		return map[string]any{"t": "act", "a": "end"}
	}
	return nil
}

// play answers every game message until the game is over, or until stopAfter game messages.
// It returns the last state.
func (b *bot) play(stopAfter int) *engine.View {
	var last *engine.View
	for n := 0; stopAfter == 0 || n < stopAfter; n++ {
		m := b.until(func(m inMsg) bool { return m.T == "game" })
		if m.T == "closed" {
			return nil
		}
		last = m.State
		if last.Phase == engine.PhaseOver {
			return last
		}
		if a := b.move(last); a != nil {
			b.send(a)
		}
	}
	return last
}

func TestGameOverWebsocket(t *testing.T) {
	hub := NewHub(Config{MaxRooms: 10}, engine.Classic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer hub.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	a, b, c := dial(t, url, "token-alice"), dial(t, url, "token-bruno"), dial(t, url, "token-chloe")
	a.send(map[string]any{"t": "create", "settings": map[string]any{"end": "rounds", "rounds": 8}})
	code := a.until(func(m inMsg) bool { return m.T == "room" }).Code
	for _, p := range []*bot{b, c} {
		p.send(map[string]any{"t": "join", "code": code})
		p.until(func(m inMsg) bool { return m.T == "room" })
	}
	b.send(map[string]any{"t": "start"}) // not the owner: nothing happens
	a.send(map[string]any{"t": "start"})

	var wg sync.WaitGroup
	finals := make([]*engine.View, 3)
	for i, p := range []*bot{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); finals[i] = p.play(0) }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		// chloe drops in the middle of the game, comes back with her token and plays on
		c.play(15)
		c.ws.Close()
		time.Sleep(200 * time.Millisecond)
		c.connect()
		back := c.until(func(m inMsg) bool { return m.T == "game" })
		if back.T == "closed" {
			return
		}
		if !slices.ContainsFunc(back.State.Players, func(p engine.PlayerView) bool { return p.ID == c.id }) {
			t.Error("chloe lost her seat")
		}
		if a := c.move(back.State); a != nil { // the state she came back to may wait for her
			c.send(a)
		}
		finals[2] = c.play(0)
	}()

	// dan comes in during the game: he watches, and cannot play
	d := dial(t, url, "token-dan")
	d.send(map[string]any{"t": "join", "code": code})
	if m := d.until(func(m inMsg) bool { return m.T == "room" }); !m.Playing {
		t.Error("dan should see a game in progress")
	}
	d.until(func(m inMsg) bool { return m.T == "game" })
	d.send(map[string]any{"t": "act", "a": "roll"})
	if m := d.until(func(m inMsg) bool { return m.T == "err" }); m.Reason != "watching" {
		t.Errorf("dan's roll: %+v", m)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the game did not end")
	}
	for i, v := range finals {
		if v == nil || v.Phase != engine.PhaseOver || v.Winner == 0 {
			t.Fatalf("bot %d ended on %+v", i, v)
		}
		if v.Round != 8 && v.WinBy == "" && slices.ContainsFunc(v.Players, func(p engine.PlayerView) bool { return !p.Bankrupt && p.ID != v.Winner }) {
			t.Fatalf("over on round %d with several players in", v.Round)
		}
	}
	t.Logf("winner %d after %d rounds, ranking %v", finals[0].Winner, finals[0].Round, finals[0].Ranking)
}

// The timers play for whoever does not decide, and a game with a time limit ends on time.
func TestTimers(t *testing.T) {
	hub := NewHub(Config{MaxRooms: 10}, engine.Classic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer hub.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	a, b := dial(t, url, "token-alice"), dial(t, url, "token-bruno")
	a.send(map[string]any{"t": "create", "settings": map[string]any{"end": "time", "minutes": 5}})
	code := a.until(func(m inMsg) bool { return m.T == "room" }).Code
	b.send(map[string]any{"t": "join", "code": code})
	b.until(func(m inMsg) bool { return m.T == "room" })
	a.send(map[string]any{"t": "start"})
	first := a.until(func(m inMsg) bool { return m.T == "game" })
	if first.State.Phase != engine.PhaseRoll {
		t.Fatalf("phase %s", first.State.Phase)
	}

	hub.mu.Lock()
	r := hub.rooms[code]
	hub.mu.Unlock()
	r.do(func() { r.tick(r.deadline + 0.01) }) // nobody rolled in time
	m := a.until(func(m inMsg) bool { return m.T == "game" })
	if len(m.Events) == 0 || m.Events[0].Kind != "rolled" {
		t.Fatalf("after the turn timer: %+v", m.Events)
	}
	r.do(func() { r.tick(r.endAt) })
	over := a.until(func(m inMsg) bool { return m.T == "game" && m.State.Phase == engine.PhaseOver })
	if over.State.Winner == 0 || over.Deadline != 0 || over.EndsIn != 0 {
		t.Fatalf("time over: %+v", over)
	}
	if room := a.until(func(m inMsg) bool { return m.T == "room" }); room.Playing {
		t.Fatal("the room still plays")
	}
}

// A trade proposed and accepted over the wire changes hands.
func TestTradeWebsocket(t *testing.T) {
	hub := NewHub(Config{MaxRooms: 10}, engine.Classic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer hub.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	a, b := dial(t, url, "token-alice"), dial(t, url, "token-bruno")
	a.send(map[string]any{"t": "create"})
	code := a.until(func(m inMsg) bool { return m.T == "room" }).Code
	b.send(map[string]any{"t": "join", "code": code})
	b.until(func(m inMsg) bool { return m.T == "room" })
	a.send(map[string]any{"t": "start"})
	st := a.until(func(m inMsg) bool { return m.T == "game" }).State
	cash := func(v *engine.View, id uint32) int {
		return v.Players[slices.IndexFunc(v.Players, func(p engine.PlayerView) bool { return p.ID == id })].Cash
	}
	if len(st.Deeds) != 0 {
		t.Fatalf("players start with properties: %+v", st.Deeds)
	}
	before := cash(st, a.id)
	a.send(map[string]any{"t": "act", "a": "offer", "offer": map[string]any{"to": b.id, "giveCash": 25, "getCash": 10}})
	offered := b.until(func(m inMsg) bool { return m.T == "game" && len(m.State.Trades) > 0 }).State.Trades[0]
	if offered.From != a.id || offered.To != b.id || offered.GiveCash != 25 {
		t.Fatalf("offer: %+v", offered)
	}
	b.send(map[string]any{"t": "act", "a": "accept", "trade": offered.ID})
	after := a.until(func(m inMsg) bool { return m.T == "game" && len(m.State.Trades) == 0 }).State
	if cash(after, a.id) != before-15 {
		t.Fatalf("after the trade: %d, want %d", cash(after, a.id), before-15)
	}
}

// With nobody playing, the shard fires the turn timer on its own (slow: one TurnSeconds).
func TestTurnTimerFires(t *testing.T) {
	if testing.Short() {
		t.Skip("waits 10 s")
	}
	hub := NewHub(Config{MaxRooms: 10}, engine.Classic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer hub.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	a, b := dial(t, url, "token-alice"), dial(t, url, "token-bruno")
	a.send(map[string]any{"t": "create", "settings": map[string]any{"turnSeconds": 10}})
	code := a.until(func(m inMsg) bool { return m.T == "room" }).Code
	b.send(map[string]any{"t": "join", "code": code})
	b.until(func(m inMsg) bool { return m.T == "room" })
	start := time.Now()
	a.send(map[string]any{"t": "start"})
	a.until(func(m inMsg) bool { return m.T == "game" })
	a.ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, data, err := a.ws.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	waited := time.Since(start)
	if !strings.Contains(string(data), `"kind":"rolled"`) || waited < 9*time.Second || waited > 12*time.Second {
		t.Fatalf("after %v: %s", waited, data)
	}
}

// A player who leaves during the game resigns: the others go on without them.
func TestLeaveResigns(t *testing.T) {
	hub := NewHub(Config{MaxRooms: 10}, engine.Classic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer hub.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	a, b := dial(t, url, "token-alice"), dial(t, url, "token-bruno")
	a.send(map[string]any{"t": "create"})
	code := a.until(func(m inMsg) bool { return m.T == "room" }).Code
	b.send(map[string]any{"t": "join", "code": code})
	b.until(func(m inMsg) bool { return m.T == "room" })
	a.send(map[string]any{"t": "start"})
	b.until(func(m inMsg) bool { return m.T == "game" })
	b.send(map[string]any{"t": "leave"})
	if m := b.until(func(m inMsg) bool { return m.T == "left" }); m.Reason != "left" {
		t.Fatalf("left: %+v", m)
	}
	over := a.until(func(m inMsg) bool { return m.T == "game" && m.State.Phase == engine.PhaseOver })
	if over.State.Winner != a.id {
		t.Fatalf("winner %d, want %d", over.State.Winner, a.id)
	}
}
