// Copyright 2014 The Go Authors.
// See https://code.google.com/p/go/source/browse/CONTRIBUTORS
// Licensed under the same terms as Go itself:
// https://code.google.com/p/go/source/browse/LICENSE

// Flow control

package http2

import "log"

var _ = log.Println
// flow is the flow control window's size.
type flow struct {
	// n is the number of DATA bytes we're allowed to send.
	// A flow is kept both on a conn and a per-stream.
	n int32

	// conn points to the shared connection-level flow that is
	// shared by all streams on that conn. It is nil for the flow
	// that's on the conn directly.
	conn *flow
}

func (f *flow) setConnFlow(cf *flow) { f.conn = cf }

func (f *flow) available() int32 {
	n := f.n
    if f.conn != nil {
       //log.Println("conn", f.conn.n)
    }
	if f.conn != nil && f.conn.n < n {
		n = f.conn.n
	}
	return n
}

func (f *flow) take(n int32) {
	//log.Println("take = ", n, f.available())
	if n > f.available() {
		panic("internal error: took too much")
	}
	f.n -= n
	if f.conn != nil {
		f.conn.n -= n
	}
}

// add adds n bytes (positive or negative) to the flow control window.
// It returns false if the sum would exceed 2^31-1.
func (f *flow) add(n int32) bool {
	//log.Println("add = ", n, f.available())
	remain := (1<<31 - 1) - f.n
	if n > remain {
		return false
	}
	f.n += n
    if f.conn != nil {
	    remain = (1<<31 - 1) - f.conn.n
	    if n > remain {
		    return false
	    }
        f.conn.n += n
    }
	return true
}
