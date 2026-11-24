package server

import (
	"encoding/json"
	"log"
	"math"
	"runtime/debug"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxMessageSize = 4096
	maxPending     = 1 << 20 // bytes waiting for a slow client before it is dropped
	drainChunk     = 32 << 10
	writeWait      = 10 * time.Second // to get one chunk through to a slow client
	pingEvery      = 10 * time.Second
	pongWait       = 25 * time.Second

	// incoming messages per connection: burst, then sustained rate per second
	msgBurst = 40
	msgRate  = 20

	closeTooSlow  = 1008
	closeRestart  = 1012 // the server stops and comes back: reconnect
	closeReplaced = 4000 // the same player connected from somewhere else: the client must not reconnect
)

// Conn is one websocket. Its goroutine reads; anybody may send.
//
// Writing is where a game server spends its time, so send does the least possible: frames come
// already encoded and go straight to the socket without blocking and without waking a goroutine.
// Only a client that is not keeping up gets a queue and a goroutine to drain it.
type Conn struct {
	hub  *Hub
	ws   *websocket.Conn // used for reading only
	ip   string
	sess *Session // set by hello; only touched by the reader goroutine

	direct *directWriter // nil when the socket cannot be written without blocking

	wmu      sync.Mutex
	closed   bool
	draining bool   // a goroutine is writing pend: everything else queues behind it
	pend     []byte // frames the socket had no room for
}

// send writes one or more encoded frames. It never blocks the caller (usually a room holding its
// lock) and never keeps wire: the caller may reuse it.
func (c *Conn) send(wire []byte) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	if !c.draining && c.direct != nil {
		n, ok := c.direct.try(wire)
		if !ok {
			c.shut(0)
			return
		}
		if wire = wire[n:]; len(wire) == 0 {
			return
		}
	}
	if len(c.pend)+len(wire) > maxPending {
		c.shut(closeTooSlow) // it will reconnect and get a fresh state
		return
	}
	c.pend = append(c.pend, wire...)
	if !c.draining {
		c.draining = true
		go c.drain()
	}
}

// drain writes what send had to queue, waiting for the client this time.
func (c *Conn) drain() {
	nc := c.ws.NetConn()
	for {
		c.wmu.Lock()
		buf := c.pend
		c.pend = nil
		if len(buf) == 0 || c.closed {
			if !c.closed {
				nc.SetWriteDeadline(time.Time{}) // or the next direct write would fail on it
				c.draining = false
			}
			c.wmu.Unlock()
			return
		}
		c.wmu.Unlock()
		// in chunks, so the deadline is about making progress, whatever the size of the backlog
		for len(buf) > 0 {
			chunk := buf[:min(len(buf), drainChunk)]
			nc.SetWriteDeadline(time.Now().Add(writeWait))
			if _, err := nc.Write(chunk); err != nil {
				c.close(0)
				return
			}
			buf = buf[len(chunk):]
		}
	}
}

// close ends the connection, telling the client why when code is set.
func (c *Conn) close(code int) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if !c.closed {
		c.shut(code)
	}
}

func (c *Conn) shut(code int) {
	c.closed, c.pend = true, nil
	if code != 0 && !c.draining && c.direct != nil {
		c.direct.try(wsFrame(opClose, []byte{byte(code >> 8), byte(code)}))
	}
	c.ws.Close() // the reader notices and cleans up
}

func (c *Conn) readLoop() {
	defer c.hub.disconnect(c)
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	// all writes go through send: browsers never ping, and a close is not echoed (the connection
	// ends as soon as ReadMessage reports it)
	c.ws.SetPingHandler(func(string) error { return nil })
	c.ws.SetCloseHandler(func(int, string) error { return nil })
	tokens, last := float64(msgBurst), time.Now()
	for {
		op, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		now := time.Now()
		tokens = math.Min(msgBurst, tokens+now.Sub(last).Seconds()*msgRate)
		last = now
		if tokens < 1 {
			continue // flooding: drop
		}
		tokens--
		var m clientMsg
		if op != websocket.TextMessage || json.Unmarshal(data, &m) != nil {
			continue
		}
		if !c.dispatch(&m) {
			return
		}
	}
}

// dispatch handles one message; a bug in a handler costs this connection, not the server.
func (c *Conn) dispatch(m *clientMsg) (ok bool) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("message %q crashed: %v\n%s", m.T, e, debug.Stack())
			ok = false
		}
	}()
	c.hub.handle(c, m)
	return true
}
