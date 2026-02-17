package testutil

import (
	"testing"
	"time"
)

// PollUntil calls condition every 10ms until it returns true or 5 seconds
// elapse, in which case the test is failed with the given message.
func PollUntil(t *testing.T, msg string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}
