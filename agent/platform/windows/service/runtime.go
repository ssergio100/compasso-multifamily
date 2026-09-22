package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type policyRuntime interface {
	Run(context.Context, time.Duration, *log.Logger) error
}

type synchronizationRuntime interface {
	Run(context.Context, *log.Logger) error
}

// AgentRuntime owns the two long-running parts of the installed agent. A
// failure in either part stops the other one so SCM never reports a partially
// functioning service as healthy.
type AgentRuntime struct {
	policy          policyRuntime
	synchronization synchronizationRuntime
	tick            time.Duration
	logger          *log.Logger
}

func NewAgentRuntime(policy policyRuntime, synchronization synchronizationRuntime, tick time.Duration, logger *log.Logger) (*AgentRuntime, error) {
	if policy == nil || synchronization == nil {
		return nil, errors.New("policy daemon and synchronizer are required")
	}
	if tick <= 0 {
		return nil, errors.New("agent tick interval must be positive")
	}
	if logger == nil {
		logger = log.Default()
	}
	return &AgentRuntime{policy: policy, synchronization: synchronization, tick: tick, logger: logger}, nil
}

type runtimeResult struct {
	component string
	err       error
}

func (r *AgentRuntime) Run(ctx context.Context) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan runtimeResult, 2)
	go func() {
		results <- runtimeResult{component: "policy daemon", err: r.policy.Run(runContext, r.tick, r.logger)}
	}()
	go func() {
		results <- runtimeResult{component: "synchronizer", err: r.synchronization.Run(runContext, r.logger)}
	}()

	first := <-results
	requestedStop := ctx.Err() != nil
	cancel()
	second := <-results

	var failures []error
	for _, result := range []runtimeResult{first, second} {
		if result.err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", result.component, result.err))
		}
	}
	if len(failures) > 0 {
		err := errors.Join(failures...)
		r.logger.Printf("agent runtime stopped: %v", err)
		return err
	}
	if !requestedStop {
		err := fmt.Errorf("%s stopped unexpectedly", first.component)
		r.logger.Printf("agent runtime stopped: %v", err)
		return err
	}
	return nil
}
