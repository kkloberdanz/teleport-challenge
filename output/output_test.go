package output_test

import (
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
	n, err := sub.Read(got)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(got[:n]) != "hello world" {
		t.Fatalf("expected %q, got %q", "hello world", string(got[:n]))
	}

	// Next read should return EOF.
	_, err = sub.Read(got)
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
	n, err := sub.Read(got)
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
	_, err := sub.Read(got)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestSubscriberClose(t *testing.T) {
	buf := output.NewBuffer()
	sub := buf.Subscribe()
	sub.Close()

	got := make([]byte, 64)
	_, err := sub.Read(got)
	if err != io.ErrClosedPipe {
		t.Fatalf("expected io.ErrClosedPipe, got %v", err)
	}
}

func TestMultipleConcurrentSubscribers(t *testing.T) {
	buf := output.NewBuffer()
	const numSubscribers = 5
	const payload = "concurrent data"

	subs := make([]io.ReadCloser, numSubscribers)
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
			n, err := sub.Read(got)
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

	subs := make([]io.ReadCloser, numSubscribers)
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
				n, err := sub.Read(p)
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
