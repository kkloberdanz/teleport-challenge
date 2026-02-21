// Package output provides an append-only byte buffer with multiple concurrent
// subscribers, each tracking their own read offset.
package output

import (
	"errors"
	"io"
	"sync"
)

// Buffer is an append-only, thread-safe byte buffer. It implements io.Writer
// so it can be used directly as cmd.Stdout / cmd.Stderr. Subscribers created
// via Subscribe each maintain an independent read offset and block until new
// data is available or the buffer is closed.
type Buffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

// NewBuffer creates new buffer
func NewBuffer() *Buffer {
	b := &Buffer{}

	// cond.L will refer to Buffer.mu.
	// See: https://cs.opensource.google/go/go/+/refs/tags/go1.26.0:src/sync/cond.go;l=48
	b.cond = sync.NewCond(&b.mu)
	return b
}

// ErrClosed is returned by Write when the buffer has already been closed.
var ErrClosed = errors.New("write to closed buffer")

// Write appends bytes to the buffer, then wakes all waiting subscribers.
// Returns ErrClosed if the buffer has been closed.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, ErrClosed
	}
	b.buf = append(b.buf, p...)
	b.cond.Broadcast()
	return len(p), nil
}

// Close marks the buffer as complete. Subsequent subscriber reads that have
// consumed all data will return io.EOF.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	b.cond.Broadcast()
}

// Subscribe returns a new subscriber starting at offset 0. The caller must
// call Close when done reading.
func (b *Buffer) Subscribe() io.ReadCloser {
	return &inMemoryLogSubscriber{buf: b, done: make(chan struct{})}
}

// inMemoryLogSubscriber tracks a per-reader offset into a Buffer. It implements
// io.ReadCloser so it can be used with io.Copy, etc.
// Ideally, we would want to keep the logs in a database. For simplicity, we
// will buffer the logs in memory, which this subscriber can be used to read.
type inMemoryLogSubscriber struct {
	buf       *Buffer
	offset    int
	done      chan struct{}
	closeOnce sync.Once
}

// Read copies available data from the buffer into p, blocking until data is
// available, the buffer is closed (io.EOF), or the subscriber is closed
// (io.ErrClosedPipe).
func (s *inMemoryLogSubscriber) Read(p []byte) (int, error) {
	s.buf.mu.Lock()
	defer s.buf.mu.Unlock()

	for s.offset == len(s.buf.buf) {
		if s.buf.closed {
			return 0, io.EOF
		}
		select {
		case <-s.done:
			return 0, io.ErrClosedPipe
		default:
		}
		s.buf.cond.Wait()
	}

	n := copy(p, s.buf.buf[s.offset:])
	s.offset += n
	return n, nil
}

// Close signals the subscriber to stop reading. Any blocked Read call will
// return io.ErrClosedPipe. Close is safe to call multiple times.
func (s *inMemoryLogSubscriber) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.buf.cond.L.Lock()
		s.buf.cond.Broadcast()
		s.buf.cond.L.Unlock()
	})
	return nil
}
