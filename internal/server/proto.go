package server

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"strings"
	"unicode"

	"quartiers/internal/engine"
)

// Wire protocol. The server owns every rule and every number of the game: a client draws what
// it is sent and turns the player's clicks into actions.
//
// Client -> server, JSON text frames:
//
//	{"t":"hello","token":"..."}                first message; the token is the player across reconnections
//	{"t":"profile","name":"Lea","color":3}     who the player is, at any time: a name (or none) and the
//	                                           index of one of the colors of "welcome"
//	{"t":"rooms"}                              outside a room: ask for the public rooms
//	{"t":"create","name":"...","settings":{}}  open a room, named or not, and sit in it as its owner; the
//	                                           settings are those of "room" (none: the defaults)
//	{"t":"join","code":"ABCDE","listed":true}  listed: picked in the list of rooms, refused if it went private
//	{"t":"leave"}                              during a game: the player resigns, their properties go back to the bank
//	{"t":"settings","name":"...","settings":{}} the room's name and settings (the owner, at any time: a game
//	                                           keeps those it started with)
//	{"t":"start"}                              start a game with the connected players (the owner, 2 at least)
//	{"t":"stop"}                               end the game, nobody wins, back to the lobby (the owner)
//	{"t":"kick","id":7}                        take a player out of the room (the owner): during a game they
//	                                           resign; they may come back with the link, to watch
//	{"t":"act","a":"roll",...}                 a move of the game: engine.Action's fields ("a", "square",
//	                                           "squares", "level"), see engine.Game.Apply
//
// Server -> client, JSON text frames:
//
//	{"t":"welcome","id":1,"name":"Lea","color":3,"colors":["#e63946",...],"board":{...}}   board: engine.Board
//	{"t":"rooms","rooms":[{"code":"ABCDE","name":"...","players":3,"max":6,"playing":false}]}
//	{"t":"room",...}                           the room and who is in it, see roomMsg
//	{"t":"game",...}                           the game: its whole state and what just happened, see gameMsg;
//	                                           sent on every change, and to whoever comes in
//	{"t":"left","reason":"left"}               or "closed", "kicked"
//	{"t":"stopped"}                            the owner ended the game: a "room" follows
//	{"t":"err","code":"notfound"}              or "full", "private" (went private since the list was sent),
//	                                           "locked" (too many wrong codes), "busy" (too many rooms),
//	                                           "players" (fewer than 2 to start), "playing" (a "start" during a game)
//	{"t":"err","code":"act","reason":"cash"}   an "act" that was refused: reason is one of engine's errors
//	                                           ("over", "notplaying", "notyours", "phase", "cash", "invalid",
//	                                           "unknown"), or "watching" for a player not in the game

type clientMsg struct {
	T        string    `json:"t"`
	Token    string    `json:"token"`
	Name     string    `json:"name"` // of the player (profile) or of the room (create, settings)
	Color    int       `json:"color"`
	Code     string    `json:"code"`
	Listed   bool      `json:"listed"`
	Settings *settings `json:"settings"`
	ID       uint32    `json:"id"` // of a player (kick)
	engine.Action
}

const (
	opText   = 1
	opBinary = 2
	opClose  = 8
	opPing   = 9
)

// wsFrame builds a server-to-client frame: final fragment, no mask, no extension. Messages are
// encoded once as frames and shared by all their recipients.
func wsFrame(op byte, payload []byte) []byte {
	return frameInPlace(op, append(make([]byte, frameHead, frameHead+len(payload)), payload...))
}

var pingFrame = wsFrame(opPing, nil)

// frameHead is the room a message leaves before its payload for frameInPlace to write the header.
const frameHead = 10

// frameInPlace turns buf, frameHead free bytes then a payload, into a frame.
func frameInPlace(op byte, buf []byte) []byte {
	switch n := len(buf) - frameHead; {
	case n < 126:
		buf[8], buf[9] = 0x80|op, byte(n)
		return buf[8:]
	case n < 1<<16:
		buf[6], buf[7] = 0x80|op, 126
		binary.BigEndian.PutUint16(buf[8:], uint16(n))
		return buf[6:]
	default:
		buf[0], buf[1] = 0x80|op, 127
		binary.BigEndian.PutUint64(buf[2:], uint64(n))
		return buf
	}
}

func textFrame(payload []byte) []byte { return wsFrame(opText, payload) }

func jsonFrame(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err) // only plain structs are marshalled
	}
	return textFrame(data)
}

func errFrame(code string) []byte { return textFrame([]byte(`{"t":"err","code":"` + code + `"}`)) }

type welcomeMsg struct {
	T      string        `json:"t"`
	ID     uint32        `json:"id"`
	Name   string        `json:"name"` // what the server remembers of this player
	Color  int           `json:"color"`
	Colors []string      `json:"colors"`
	Board  *engine.Board `json:"board"`
}

// The colors of the pawns. A player asks for one by its index; two players of a room do not get
// the same one as long as there are enough.
var colors = []string{"#e63946", "#ff8a1f", "#ffd23f", "#2ec4f1", "#8b5cf6", "#14b8a6", "#ff6fb5", "#8d99ae"}

const (
	maxName     = 16 // runes
	maxRoomName = 24
	maxListed   = 50 // public rooms a client is told of
)

// clean makes user text fit to keep and to show: no control or format characters (invisible ones,
// those that turn the text around), single spaces, at most max runes. Names are optional: a
// client shows its own words for a player or a room without one.
func clean(s string, max int) string {
	var b strings.Builder
	n, space := 0, false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = n > 0
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if space {
			if n+1 >= max {
				break
			}
			b.WriteByte(' ')
			n++
			space = false
		}
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// roomEntry is a public room in the list of those one can walk into.
type roomEntry struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Players int    `json:"players"` // connected
	Max     int    `json:"max"`
	Playing bool   `json:"playing"`
}

type playerInfo struct {
	ID        uint32 `json:"id"`
	Name      string `json:"name"`
	Color     int    `json:"color"` // in this room
	Connected bool   `json:"connected"`
	Playing   bool   `json:"playing"` // has a seat in the game in progress (or the last one); false: watching
}

// roomMsg is the room and its people, pushed to its members whenever it changes.
type roomMsg struct {
	T        string       `json:"t"`
	Code     string       `json:"code"`
	Name     string       `json:"name"`
	Owner    uint32       `json:"owner"` // the player who sets and starts the games
	Settings settings     `json:"settings"`
	Playing  bool         `json:"playing"` // a game is in progress
	Players  []playerInfo `json:"players"`
}

// gameMsg is the game in progress, or the last one, whole, with what just happened.
type gameMsg struct {
	T     string      `json:"t"`
	Seq   int         `json:"seq"` // the game's number in the room
	State engine.View `json:"state"`
	// Events is what happened since the last "game" (empty for one sent to a player coming in).
	Events []engine.Event `json:"events"`
	// Deadline is in how many ms the timer plays for whoever must decide (see engine.Game.Timeout),
	// 0 for none.
	Deadline int64 `json:"deadline"`
	// EndsIn is in how many ms a game with a time limit ends, 0 for none.
	EndsIn int64 `json:"endsIn"`
}

const (
	codeChars = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLen   = 5
)

func randomCode() string {
	var b [codeLen]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = codeChars[int(b[i])%len(codeChars)]
	}
	return string(b[:])
}
