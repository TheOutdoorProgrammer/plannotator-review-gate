package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinateReviewSharesOneVerdict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ev := &Event{SessionID: "session", ToolUseID: "tool-call"}

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	review := func() *Decision {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return &Decision{Permission: "deny", Reason: "review feedback"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make([]*Decision, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i] = coordinateReview(ctx, ev, review)
		}(i)
	}
	<-started
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("review ran %d times, want once", got)
	}
	for i, decision := range results {
		if decision == nil || decision.Permission != "deny" || decision.Reason != "review feedback" {
			t.Errorf("result %d = %+v", i, decision)
		}
	}
}
