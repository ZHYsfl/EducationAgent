package state

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPPTAgentRuntimeStartStop(t *testing.T) {
	r := NewPPTAgentRuntime()

	var started int32
	err := r.Start(func(ctx context.Context) {
		atomic.StoreInt32(&started, 1)
		<-ctx.Done()
	})
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&started) == 1
	}, time.Second, 10*time.Millisecond)

	assert.True(t, r.IsRunning())
	r.Stop()
	r.Wait()
	assert.False(t, r.IsRunning())
}

func TestPPTAgentRuntimeDoubleStart(t *testing.T) {
	r := NewPPTAgentRuntime()
	err := r.Start(func(ctx context.Context) {
		<-ctx.Done()
	})
	assert.NoError(t, err)

	err = r.Start(func(ctx context.Context) {
		<-ctx.Done()
	})
	assert.Error(t, err)

	r.Stop()
	r.Wait()
}

func TestPPTAgentRuntimeStopIdempotent(t *testing.T) {
	r := NewPPTAgentRuntime()
	r.Stop() // should not panic
	err := r.Start(func(ctx context.Context) {
		<-ctx.Done()
	})
	assert.NoError(t, err)
	r.Stop()
	r.Stop() // should not panic
	r.Wait()
}
