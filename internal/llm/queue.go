package llm

import (
	"context"
	"errors"
	"sync"
)

// ErrQueueClosed is returned by Queue.Do when the Queue was closed while the
// call was still waiting to run.
var ErrQueueClosed = errors.New("llm: queue closed")

// Priority orders work waiting on a Queue. Decisions (PriorityHigh) always
// run before any pending memory notes (PriorityLow); order is FIFO within a
// priority. See docs/PLAN.md §8 and §7.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityHigh
)

type queueItem struct {
	ctx  context.Context
	fn   func(context.Context) error
	done chan error
}

// Queue serializes access to a single GPU-bound model (or any shared,
// provider-agnostic resource) so that decisions always jump ahead of pending
// memory notes, while preserving FIFO order within each priority. Queue
// knows nothing about Provider; callers pass arbitrary work via Do.
type Queue struct {
	mu     sync.Mutex
	high   []*queueItem
	low    []*queueItem
	wake   chan struct{}
	closed chan struct{}
}

// NewQueue starts a Queue with the given number of concurrent workers.
// Concurrency below 1 is treated as 1. A single local model can't usefully
// serve more than one request at a time, so concurrency 1 is the normal
// case; it's a parameter mainly so a provider that can genuinely handle
// concurrent requests (e.g. a remote API) isn't artificially serialized.
func NewQueue(concurrency int) *Queue {
	if concurrency < 1 {
		concurrency = 1
	}
	q := &Queue{
		wake:   make(chan struct{}, 1),
		closed: make(chan struct{}),
	}
	for i := 0; i < concurrency; i++ {
		go q.worker()
	}
	return q
}

// Close stops all worker goroutines. Work already queued but not yet
// started is abandoned: any Do call still waiting on it returns
// ErrQueueClosed (or ctx.Err() if that fires first).
func (q *Queue) Close() {
	close(q.closed)
}

// Do enqueues fn at the given priority and blocks until it has run, ctx is
// done, or the Queue is closed — whichever happens first. If ctx is already
// done by the time a worker would run fn, fn is skipped entirely.
func (q *Queue) Do(ctx context.Context, prio Priority, fn func(context.Context) error) error {
	item := q.submit(ctx, prio, fn)
	select {
	case err := <-item.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closed:
		return ErrQueueClosed
	}
}

func (q *Queue) submit(ctx context.Context, prio Priority, fn func(context.Context) error) *queueItem {
	item := &queueItem{ctx: ctx, fn: fn, done: make(chan error, 1)}
	q.mu.Lock()
	if prio == PriorityHigh {
		q.high = append(q.high, item)
	} else {
		q.low = append(q.low, item)
	}
	q.mu.Unlock()
	q.nudge()
	return item
}

func (q *Queue) nudge() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) pop() *queueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.high) > 0 {
		item := q.high[0]
		q.high = q.high[1:]
		return item
	}
	if len(q.low) > 0 {
		item := q.low[0]
		q.low = q.low[1:]
		return item
	}
	return nil
}

func (q *Queue) worker() {
	for {
		item := q.pop()
		if item == nil {
			select {
			case <-q.wake:
				continue
			case <-q.closed:
				return
			}
		}

		if item.ctx.Err() != nil {
			// Caller already gave up while this was queued; don't run fn
			// against a dead context. The send is non-blocking in effect
			// since done is buffered 1 and at most one reader ever reads it.
			item.done <- item.ctx.Err()
			continue
		}
		item.done <- item.fn(item.ctx)
	}
}

// Wrap returns a Provider that runs p.Complete through q at priority prio,
// serializing GPU access across every provider wrapped around the same
// Queue. This is the convenience glue described in docs/PLAN.md §8: decision
// providers wrap with PriorityHigh, memory-note providers with PriorityLow.
func Wrap(p Provider, q *Queue, prio Priority) Provider {
	return &queuedProvider{p: p, q: q, prio: prio}
}

type queuedProvider struct {
	p    Provider
	q    *Queue
	prio Priority
}

func (w *queuedProvider) Name() string { return w.p.Name() }

func (w *queuedProvider) Complete(ctx context.Context, req Request) (Response, error) {
	var resp Response
	err := w.q.Do(ctx, w.prio, func(ctx context.Context) error {
		r, err := w.p.Complete(ctx, req)
		resp = r
		return err
	})
	return resp, err
}
