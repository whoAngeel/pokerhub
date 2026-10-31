package llm

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestQueue_PriorityOrdering(t *testing.T) {
	q := NewQueue(1)
	defer q.Close()

	gate := make(chan struct{})
	started := make(chan struct{})

	// Occupy the sole worker so nothing else can run until we release gate,
	// giving us full control over what's queued before anything executes.
	blocker := q.submit(context.Background(), PriorityHigh, func(ctx context.Context) error {
		close(started)
		<-gate
		return nil
	})
	<-started

	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context) error {
		return func(ctx context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	low0 := q.submit(context.Background(), PriorityLow, record("low0"))
	low1 := q.submit(context.Background(), PriorityLow, record("low1"))
	high := q.submit(context.Background(), PriorityHigh, record("high"))
	low2 := q.submit(context.Background(), PriorityLow, record("low2"))

	close(gate)

	waitDone(t, blocker)
	waitDone(t, high)
	waitDone(t, low0)
	waitDone(t, low1)
	waitDone(t, low2)

	mu.Lock()
	got := append([]string{}, order...)
	mu.Unlock()

	want := []string{"high", "low0", "low1", "low2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execution order = %v, want %v (high before low, FIFO within a priority)", got, want)
	}
}

func waitDone(t *testing.T, item *queueItem) {
	t.Helper()
	select {
	case <-item.done:
	case <-time.After(2 * time.Second):
		t.Fatal("queue item never completed")
	}
}

func TestQueue_Do_RespectsContextCancellationWhileWaiting(t *testing.T) {
	q := NewQueue(1)
	defer q.Close()

	gate := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = q.Do(context.Background(), PriorityHigh, func(ctx context.Context) error { close(started); <-gate; return nil })
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{}, 1)
	doErr := make(chan error, 1)
	go func() {
		doErr <- q.Do(ctx, PriorityLow, func(ctx context.Context) error {
			ran <- struct{}{}
			return nil
		})
	}()

	cancel()

	select {
	case err := <-doErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do() did not return promptly after ctx cancellation while still queued")
	}

	close(gate)

	select {
	case <-ran:
		t.Fatal("fn ran even though its context was already cancelled before the worker reached it")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestQueue_Do_RunsAndReturnsError(t *testing.T) {
	q := NewQueue(1)
	defer q.Close()

	boom := errors.New("boom")
	err := q.Do(context.Background(), PriorityHigh, func(ctx context.Context) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Do() error = %v, want boom", err)
	}
}

func TestQueue_ClosedQueueAbandonsPendingWork(t *testing.T) {
	q := NewQueue(1)

	gate := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_ = q.Do(context.Background(), PriorityHigh, func(ctx context.Context) error { close(started); <-gate; return nil })
	}()
	<-started

	doErr := make(chan error, 1)
	go func() {
		doErr <- q.Do(context.Background(), PriorityLow, func(ctx context.Context) error { return nil })
	}()

	q.Close()

	select {
	case err := <-doErr:
		if !errors.Is(err, ErrQueueClosed) {
			t.Fatalf("Do() error = %v, want ErrQueueClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do() did not return after Close()")
	}
	close(gate)
}

func TestWrap_ForwardsToProviderThroughQueue(t *testing.T) {
	q := NewQueue(1)
	defer q.Close()

	mock := NewMock(MockResponse{Content: "wrapped"})
	wrapped := Wrap(mock, q, PriorityHigh)

	if wrapped.Name() != "mock" {
		t.Errorf("Name() = %q, want mock", wrapped.Name())
	}

	resp, err := wrapped.Complete(context.Background(), Request{User: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "wrapped" {
		t.Errorf("Content = %q, want wrapped", resp.Content)
	}
	if len(mock.Requests()) != 1 {
		t.Errorf("underlying provider received %d requests, want 1", len(mock.Requests()))
	}
}
