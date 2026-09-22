package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"testing"
	"time"
)

type fakePolicyRuntime struct {
	run func(context.Context, time.Duration, *log.Logger) error
}

func (f fakePolicyRuntime) Run(ctx context.Context, tick time.Duration, logger *log.Logger) error {
	return f.run(ctx, tick, logger)
}

type fakeSynchronizationRuntime struct {
	run func(context.Context, *log.Logger) error
}

func (f fakeSynchronizationRuntime) Run(ctx context.Context, logger *log.Logger) error {
	return f.run(ctx, logger)
}

func TestAgentRuntimeStopsBothComponentsOnCancellation(t *testing.T) {
	var stopped sync.WaitGroup
	stopped.Add(2)
	wait := func(ctx context.Context) error {
		<-ctx.Done()
		stopped.Done()
		return nil
	}
	runtime, err := NewAgentRuntime(
		fakePolicyRuntime{run: func(ctx context.Context, _ time.Duration, _ *log.Logger) error { return wait(ctx) }},
		fakeSynchronizationRuntime{run: func(ctx context.Context, _ *log.Logger) error { return wait(ctx) }},
		time.Second, log.New(testWriter{t}, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stopped.Wait()
}

func TestAgentRuntimeStopsPeerAndReturnsUnexpectedFailure(t *testing.T) {
	want := errors.New("sync failed")
	peerStopped := make(chan struct{})
	runtime, err := NewAgentRuntime(
		fakePolicyRuntime{run: func(ctx context.Context, _ time.Duration, _ *log.Logger) error {
			<-ctx.Done()
			close(peerStopped)
			return nil
		}},
		fakeSynchronizationRuntime{run: func(context.Context, *log.Logger) error { return want }},
		time.Second, log.New(testWriter{t}, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	select {
	case <-peerStopped:
	case <-time.After(time.Second):
		t.Fatal("policy daemon was not stopped")
	}
}

func TestAgentRuntimeRejectsUnexpectedCleanExit(t *testing.T) {
	runtime, err := NewAgentRuntime(
		fakePolicyRuntime{run: func(context.Context, time.Duration, *log.Logger) error { return nil }},
		fakeSynchronizationRuntime{run: func(ctx context.Context, _ *log.Logger) error {
			<-ctx.Done()
			return nil
		}},
		time.Second, log.New(testWriter{t}, "", 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want unexpected-stop error")
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(value []byte) (int, error) {
	w.t.Log(string(value))
	return len(value), nil
}
