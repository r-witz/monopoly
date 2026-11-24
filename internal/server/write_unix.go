//go:build unix

package server

import "syscall"

// directWriter writes to a socket without ever waiting. Not safe for concurrent use.
type directWriter struct {
	raw syscall.RawConn
	fn  func(fd uintptr) bool // built once: a closure per write would be an allocation per message
	buf []byte
	n   int
	err error
}

func newDirectWriter(raw syscall.RawConn) *directWriter {
	w := &directWriter{raw: raw}
	w.fn = func(fd uintptr) bool {
		w.n, w.err = syscall.Write(int(fd), w.buf)
		return true // done either way: the caller queues what is left
	}
	return w
}

// try writes as much of b as the socket takes right now. ok is false when the connection is broken.
func (w *directWriter) try(b []byte) (n int, ok bool) {
	w.buf, w.n, w.err = b, 0, nil
	err := w.raw.Write(w.fn)
	w.buf = nil
	return max(w.n, 0), err == nil && (w.err == nil || w.err == syscall.EAGAIN || w.err == syscall.EINTR)
}
