// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Pipe adapter to connect code expecting an io.Reader
// with code expecting an io.Writer.

package http2

import (
	"errors"
	"sync"
	"sync/atomic"
    "bytes"
    "io"
)

// ErrClosedPipe is the error used for read or write operations on a closed pipe.
var ErrClosedPipe = errors.New("io: read/write on closed pipe")

type pipeResult struct {
	n   int
	err error
}

// A pipe is the shared pipe structure underlying PipeReader and PipeWriter.
type pipe struct {
	rl    sync.Mutex // gates readers one at a time
	wl    sync.Mutex // gates writers one at a time
	l     sync.Mutex // protects remaining fields
	data  *bytes.Buffer     // data remaining in pending write
	rwait sync.Cond  // waiting reader
	rerr  error      // if reader closed, error to give writes
	werr  error      // if writer closed, error to give reads
}

func (p *pipe) read(b []byte) (n int, err error) {
	// One reader at a time.
	p.rl.Lock()
	defer p.rl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	for {
		if p.rerr != nil {
			return 0, ErrClosedPipe
		}
		if p.data.Len() != 0 {
			break
		}
		if p.werr != nil {
			return 0, p.werr
		}
		p.rwait.Wait()
	}
    n, err = p.data.Read(b)
	return
}

var zero [0]byte

func (p *pipe) write(b []byte) (n int, err error) {
	// pipe uses nil to mean not available
	if b == nil {
		b = zero[:]
	}

	// One writer at a time.
	p.wl.Lock()
	defer p.wl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	if p.rerr != nil {
        err = p.rerr
        return
    }
	if p.werr != nil {
		err = ErrClosedPipe
		return
	}
	n, err = p.data.Write(b)
	p.rwait.Signal()
	return
}

func (p *pipe) rclose(err error) {
	if err == nil {
		err = ErrClosedPipe
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.rerr = err
	p.rwait.Signal()
}

func (p *pipe) wclose(err error) {
	if err == nil {
		err = io.EOF
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.werr = err
	p.rwait.Signal()
}

// A PipeReader is the read half of a pipe.
type PipeReader struct {
	p *pipe
    n int64
}

// Read implements the standard Read interface:
// it reads data from the pipe, blocking until a writer
// arrives or the write end is closed.
// If the write end is closed with an error, that error is
// returned as err; otherwise err is EOF.
func (r *PipeReader) Read(data []byte) (n int, err error) {
	n, err = r.p.read(data)
    atomic.AddInt64(&r.n, int64(n))
    return
}

func (r *PipeReader) GetReadCount() int64 {
    return atomic.LoadInt64(&r.n)
}

func (r *PipeReader) ResetReadCount() {
    atomic.StoreInt64(&r.n, 0)
}

// Close closes the reader; subsequent writes to the
// write half of the pipe will return the error ErrClosedPipe.
func (r *PipeReader) Close() error {
	return r.CloseWithError(nil)
}

// CloseWithError closes the reader; subsequent writes
// to the write half of the pipe will return the error err.
func (r *PipeReader) CloseWithError(err error) error {
	r.p.rclose(err)
	return nil
}

// A PipeWriter is the write half of a pipe.
type PipeWriter struct {
	p *pipe
}

// Write implements the standard Write interface:
// it writes data to the pipe, blocking until readers
// have consumed all the data or the read end is closed.
// If the read end is closed with an error, that err is
// returned as err; otherwise err is ErrClosedPipe.
func (w *PipeWriter) Write(data []byte) (n int, err error) {
	return w.p.write(data)
}

// Close closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and EOF.
func (w *PipeWriter) Close() error {
	return w.CloseWithError(nil)
}

// CloseWithError closes the writer; subsequent reads from the
// read half of the pipe will return no bytes and the error err,
// or EOF if err is nil.
//
// CloseWithError always returns nil.
func (w *PipeWriter) CloseWithError(err error) error {
	w.p.wclose(err)
	return nil
}

// Pipe creates a synchronous in-memory pipe.
// It can be used to connect code expecting an io.Reader
// with code expecting an io.Writer.
// Reads on one end are matched with writes on the other,
// copying data directly between the two; there is no internal buffering.
// It is safe to call Read and Write in parallel with each other or with
// Close. Close will complete once pending I/O is done. Parallel calls to
// Read, and parallel calls to Write, are also safe:
// the individual calls will be gated sequentially.
//make a 0 len and 64K cap size byte array
func Pipe() (*PipeReader, *PipeWriter) {
	p := new(pipe)
    p.data = bytes.NewBuffer(make([]byte, 0, 1 << 16))
	p.rwait.L = &p.l
	r := &PipeReader{p, 0}
	w := &PipeWriter{p}
	return r, w
}

type pipe2 struct {
    pr *PipeReader
    pw *PipeWriter
    err error
}

func newpipe2() *pipe2 {
    r := &pipe2{}
    r.pr , r.pw = Pipe()
    return r
}

// Read waits until data is available and copies bytes
// from the buffer into p.
func (r *pipe2) Read(p []byte) (n int, err error) {
	return r.pr.Read(p)
}

// Write copies bytes from p into the buffer and wakes a reader.
// It is an error to write more data than the buffer can hold.
func (w *pipe2) Write(p []byte) (n int, err error) {
	return w.pw.Write(p)
}

func (c *pipe2) Close(err error) {
    c.err = err
	c.pr.Close()
    c.pw.Close()
}
