package output_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"go.uber.org/goleak"

	"github.com/kkloberdanz/teleworker/output"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestWriteAndRead(t *testing.T) {
	buf := output.NewBuffer()
	data := []byte("hello world")
	if _, err := buf.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	buf.Close()

	sub := buf.Subscribe()
	got := make([]byte, 64)
	n, err := sub.Read(t.Context(), got)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(got[:n]) != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", string(got[:n]))
	}

	// Next read should return EOF.
	_, err = sub.Read(t.Context(), got)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestSubscriberReadsFromBeginning(t *testing.T) {
	buf := output.NewBuffer()
	buf.Write([]byte("first "))
	buf.Write([]byte("second"))
	buf.Close()

	sub := buf.Subscribe()
	got := make([]byte, 64)
	n, err := sub.Read(t.Context(), got)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(got[:n]) != "first second" {
		t.Fatalf("expected %q, got %q", "first second", string(got[:n]))
	}
}

func TestReadReturnsEOFOnClose(t *testing.T) {
	buf := output.NewBuffer()
	sub := buf.Subscribe()
	buf.Close()

	got := make([]byte, 64)
	_, err := sub.Read(t.Context(), got)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	buf := output.NewBuffer()
	sub := buf.Subscribe()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := make([]byte, 64)
	_, err := sub.Read(ctx, got)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestContextCancellationUnblocksWaitingRead verifies that cancelling the
// context wakes a subscriber that is blocked waiting for data. This exercises
// the AfterFunc callback that broadcasts on the condition variable while
// holding the lock, preventing a lost wakeup between the ctx.Err() check
// and cond.Wait().
func TestContextCancellationUnblocksWaitingRead(t *testing.T) {
	buf := output.NewBuffer()
	sub := buf.Subscribe()

	ctx, cancel := context.WithCancel(t.Context())

	result := make(chan error, 1)
	go func() {
		p := make([]byte, 64)
		_, err := sub.Read(ctx, p)
		result <- err
	}()

	cancel()

	err := <-result
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// The subscriber should still be usable with a fresh context.
	buf.Write([]byte("after cancel"))
	buf.Close()

	got := make([]byte, 64)
	n, err := sub.Read(t.Context(), got)
	if err != nil {
		t.Fatalf("Read after cancel failed: %v", err)
	}
	if string(got[:n]) != "after cancel" {
		t.Fatalf("expected %q, got %q", "after cancel", string(got[:n]))
	}
}

// TestConcurrentCancelWithWriters verifies that many subscribers can recover
// from a context cancellation while concurrent writes are happening, and that
// every subscriber eventually sees the complete output.
func TestConcurrentCancelWithWriters(t *testing.T) {
	buf := output.NewBuffer()

	const numSubscribers = 20
	const numWrites = 50
	const chunk = "line\n"

	var want string
	for range numWrites {
		want += chunk
	}

	subs := make([]output.Subscriber, numSubscribers)
	for i := range subs {
		subs[i] = buf.Subscribe()
	}

	ctx, cancel := context.WithCancel(t.Context())

	// Ensure all subscribers are blocked in Read before any writes begin.
	ready := make(chan struct{}, numSubscribers)

	// Each subscriber reads until its context is cancelled, then continues
	// reading with a fresh context until EOF.
	var wg sync.WaitGroup
	results := make([]string, numSubscribers)
	for i, sub := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var all []byte
			p := make([]byte, 64)
			readCtx := ctx
			ready <- struct{}{}
			for {
				n, err := sub.Read(readCtx, p)
				if n > 0 {
					all = append(all, p[:n]...)
				}
				if err == context.Canceled {
					// Switch to the test context and keep reading.
					readCtx = t.Context()
					continue
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("subscriber error %d: %v", i, err)
					return
				}
			}
			results[i] = string(all)
		}()
	}

	// Wait for all subscribers to be ready before writing.
	for range numSubscribers {
		<-ready
	}

	// Cancel the context while subscribers are blocked, then write data.
	// This exercises the AfterFunc broadcast waking blocked readers
	// concurrently with new writes arriving.
	cancel()

	for range numWrites {
		buf.Write([]byte(chunk))
	}
	buf.Close()

	wg.Wait()

	for i, r := range results {
		if r != want {
			t.Errorf("subscriber %d: got %d bytes, want %d bytes", i, len(r), len(want))
		}
	}
}

func TestMultipleConcurrentSubscribers(t *testing.T) {
	buf := output.NewBuffer()
	const numSubscribers = 5
	const payload = "concurrent data"

	subs := make([]output.Subscriber, numSubscribers)
	for i := range subs {
		subs[i] = buf.Subscribe()
	}

	buf.Write([]byte(payload))
	buf.Close()

	var wg sync.WaitGroup
	results := make([]string, numSubscribers)
	for i, sub := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := make([]byte, 64)
			n, err := sub.Read(t.Context(), got)
			if err != nil {
				t.Errorf("subscriber %d: Read failed: %v", i, err)
				return
			}
			results[i] = string(got[:n])
		}()
	}
	wg.Wait()

	for i, r := range results {
		if r != payload {
			t.Errorf("subscriber %d: expected %q, got %q", i, payload, r)
		}
	}
}

// TestManyConcurrentSubscribersWithIncrementalWrites verifies that many
// subscribers can read from a buffer while writes are happening concurrently,
// and that every subscriber sees the complete output. Run with -race.
func TestManyConcurrentSubscribersWithIncrementalWrites(t *testing.T) {
	buf := output.NewBuffer()
	const numSubscribers = 50
	const numWrites = 100
	const chunk = "data chunk\n"

	want := ""
	for range numWrites {
		want += chunk
	}

	subs := make([]output.Subscriber, numSubscribers)
	for i := range subs {
		subs[i] = buf.Subscribe()
	}

	// Read all output from each subscriber concurrently.
	var wg sync.WaitGroup
	results := make([]string, numSubscribers)
	for i, sub := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var all []byte
			p := make([]byte, 64)
			for {
				n, err := sub.Read(t.Context(), p)
				if n > 0 {
					all = append(all, p[:n]...)
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Errorf("subscriber %d: Read error: %v", i, err)
					return
				}
			}
			results[i] = string(all)
		}()
	}

	// Write incrementally from a separate goroutine.
	go func() {
		for range numWrites {
			buf.Write([]byte(chunk))
		}
		buf.Close()
	}()

	wg.Wait()

	for i, r := range results {
		if r != want {
			t.Errorf("subscriber %d: got %d bytes, want %d bytes", i, len(r), len(want))
		}
	}
}
