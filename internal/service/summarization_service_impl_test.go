package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestErrgroup_JobsRunConcurrently(t *testing.T) {
	svc := NewSummarizationService(context.Background(), nil, nil, nil, nil, zap.NewNop().Sugar())

	var count atomic.Int32
	n := 5

	for i := 0; i < n; i++ {
		svc.group.Go(func() error {
			count.Add(1)
			return nil
		})
	}

	svc.Shutdown()

	if got := count.Load(); got != int32(n) {
		t.Errorf("expected %d jobs done, got %d", n, got)
	}
}

func TestErrgroup_ShutdownWaitsForJobs(t *testing.T) {
	svc := NewSummarizationService(context.Background(), nil, nil, nil, nil, zap.NewNop().Sugar())

	var finished atomic.Bool

	svc.group.Go(func() error {
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
		return nil
	})

	svc.Shutdown()

	if !finished.Load() {
		t.Error("Shutdown returned before job finished")
	}
}

func TestErrgroup_ContextCancelledOnShutdown(t *testing.T) {
	svc := NewSummarizationService(context.Background(), nil, nil, nil, nil, zap.NewNop().Sugar())

	started := make(chan struct{})
	svc.group.Go(func() error {
		close(started)
		<-svc.groupCtx.Done()
		return nil
	})

	<-started
	svc.Shutdown()

	if svc.groupCtx.Err() == nil {
		t.Error("expected context to be cancelled after Shutdown")
	}
}
