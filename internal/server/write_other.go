//go:build !unix

package server

import "syscall"

// directWriter has no implementation here: everything goes through the queue.
type directWriter struct{}

func newDirectWriter(syscall.RawConn) *directWriter { return nil }

func (*directWriter) try([]byte) (int, bool) { return 0, true }
