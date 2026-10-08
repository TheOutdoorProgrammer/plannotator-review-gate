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
		return &Decision{
			Permission:        "allow",
			Reason:            "approved with notes",
			AdditionalContext: "non-blocking review note",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make([]coordinatedReview, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := range results {
		go func(i int) {
			defer wg.Done()
			results[i] = coordinateReview(ctx, ev, review)
			if results[i].Primary {
				if err := markReviewResponded(results[i].Entry); err != nil {
					t.Errorf("markReviewResponded: %v", err)
				}
			}
		}(i)
	}
	<-started
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("review ran %d times, want once", got)
	}
	primaries := 0
	for i, result := range results {
		if result.Primary {
			primaries++
		}
		if result.Decision == nil || result.Decision.Permission != "allow" ||
			result.Decision.AdditionalContext != "non-blocking review note" {
			t.Errorf("result %d = %+v", i, result)
		}
	}
	if primaries != 1 {
		t.Fatalf("got %d primary responses, want one", primaries)
	}

	context, err := claimReviewContext(ev)
	if err != nil {
		t.Fatal(err)
	}
	if context != "non-blocking review note" {
		t.Fatalf("claimed context = %q", context)
	}
	context, err = claimReviewContext(ev)
	if err != nil {
		t.Fatal(err)
	}
	if context != "" {
		t.Fatalf("context was delivered twice: %q", context)
	}
}
