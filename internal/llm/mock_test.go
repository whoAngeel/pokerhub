package llm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMock_ScriptedQueue(t *testing.T) {
	boom := errors.New("boom")
	p := NewMock(
		MockResponse{Content: "first"},
		MockResponse{Content: "second"},
		MockResponse{Err: boom},
	)

	resp, err := p.Complete(context.Background(), Request{User: "a"})
	if err != nil || resp.Content != "first" {
		t.Fatalf("call 1: resp=%+v err=%v, want content=first err=nil", resp, err)
	}

	resp, err = p.Complete(context.Background(), Request{User: "b"})
	if err != nil || resp.Content != "second" {
		t.Fatalf("call 2: resp=%+v err=%v, want content=second err=nil", resp, err)
	}

	_, err = p.Complete(context.Background(), Request{User: "c"})
	if !errors.Is(err, boom) {
		t.Fatalf("call 3: err=%v, want boom", err)
	}

	// queue exhausted: falls back to the deterministic default.
	resp, err = p.Complete(context.Background(), Request{User: "d"})
	if err != nil || resp.Content != "{}" {
		t.Fatalf("call 4 (queue exhausted): resp=%+v err=%v, want default {} content", resp, err)
	}
}

func TestMock_DefaultContent_ActionSchema(t *testing.T) {
	p := NewMock()
	schema := json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"},"amount":{"type":"integer"}}}`)
	resp, err := p.Complete(context.Background(), Request{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"reasoning":{"hand":"mock","odds":"mock","reads":"mock","plan":"mock"},"action":"check","amount":0,"say":""}`
	if resp.Content != want {
		t.Errorf("Content = %q, want %q", resp.Content, want)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &decoded); err != nil {
		t.Errorf("default content is not valid JSON: %v", err)
	}
}

func TestMock_DefaultContent_NoActionSchema(t *testing.T) {
	p := NewMock()
	resp, err := p.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "{}" {
		t.Errorf("Content = %q, want {}", resp.Content)
	}

	schema := json.RawMessage(`{"type":"object","properties":{"foo":{"type":"string"}}}`)
	resp, err = p.Complete(context.Background(), Request{Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "{}" {
		t.Errorf("Content = %q, want {} for a schema without an action property", resp.Content)
	}
}

func TestMock_Func(t *testing.T) {
	called := false
	p := MockFunc(func(req Request) (Response, error) {
		called = true
		return Response{Content: "from-func:" + req.User}, nil
	})
	resp, err := p.Complete(context.Background(), Request{User: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("MockFunc's function was never called")
	}
	if resp.Content != "from-func:hi" {
		t.Errorf("Content = %q, want from-func:hi", resp.Content)
	}
}

func TestMock_DelayRespectsCancellation(t *testing.T) {
	p := NewMock(MockResponse{Content: "too-late", Delay: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.Complete(ctx, Request{})
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Complete took %v, want it to return promptly on ctx timeout", elapsed)
	}
}

func TestMock_RequestsRecordedThreadSafe(t *testing.T) {
	p := NewMock()
	var wg sync.WaitGroup
	const n = 50
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = p.Complete(context.Background(), Request{User: "req"})
		}(i)
	}
	wg.Wait()

	got := p.Requests()
	if len(got) != n {
		t.Fatalf("Requests() returned %d entries, want %d", len(got), n)
	}
}

func TestMock_Name(t *testing.T) {
	if NewMock().Name() != "mock" {
		t.Errorf("Name() = %q, want mock", NewMock().Name())
	}
}
