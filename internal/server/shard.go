package server

import (
	"sync"
	"time"
)

const shardRooms = 64 // rooms a shard takes before the next one is used

// shard ticks a set of rooms from a single goroutine, on a common beat: one wake-up serves all
// the rooms that are due (a timer per room would wake a thread for every room tick), and a shard
// whose rooms are all quiet sleeps until the next one wants a tick.
type shard struct {
	hub    *Hub
	kick   chan struct{}
	offset time.Duration // of its beat, so that the shards do not all wake up at once

	mu    sync.Mutex
	rooms []*Room
}

func (s *shard) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *shard) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rooms)
}

func (s *shard) run() {
	timer := time.NewTimer(time.Hour)
	var rooms []*Room
	for {
		select {
		case <-timer.C:
		case <-s.kick:
		}
		s.mu.Lock()
		rooms = append(rooms[:0], s.rooms...)
		s.mu.Unlock()

		soonest := time.Duration(1<<63 - 1)
		closed := false
		for _, r := range rooms {
			due := time.Duration(r.due.Load())
			if now := time.Since(s.hub.start); due <= now {
				alive := r.do(func() {
					// rounded to the beat, also after an extra tick asked by poke
					due = (now + r.tick(now.Seconds()) + tickFast/2 - s.offset).Truncate(tickFast) + s.offset
					r.due.Store(int64(due)) // under the room lock: a poke after this tick is never lost
				})
				if !alive {
					closed = true
					continue
				}
			}
			soonest = min(soonest, due)
		}
		if closed {
			s.mu.Lock()
			kept := s.rooms[:0]
			for _, r := range s.rooms {
				if !r.gone.Load() {
					kept = append(kept, r)
				}
			}
			clear(s.rooms[len(kept):])
			s.rooms = kept
			s.mu.Unlock()
		}
		timer.Reset(max(0, min(soonest-time.Since(s.hub.start), time.Hour)))
	}
}

// shardFor picks the shard of a new room: the first one with room left, so a quiet server ticks
// all its rooms from one goroutine, and the least loaded one when they are all busy.
func (h *Hub) shardFor() *shard {
	best, least := h.shards[0], int(^uint(0)>>1)
	for _, s := range h.shards {
		n := s.count()
		if n < shardRooms {
			return s
		}
		if n < least {
			best, least = s, n
		}
	}
	return best
}

func (s *shard) add(r *Room) {
	s.mu.Lock()
	s.rooms = append(s.rooms, r)
	s.mu.Unlock()
	s.wake()
}
