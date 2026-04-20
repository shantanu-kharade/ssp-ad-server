package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDSPCircuitBreaker(t *testing.T) {
	log := zap.NewNop()
	timeout := 100 * time.Millisecond
	cb := newDSPCircuitBreakerWithTimeout("test-dsp", log, timeout)

	ctx := context.Background()
	dummyErr := errors.New("dummy error")

	runFailure := func() error {
		_, err := cb.Execute(ctx, func() (interface{}, error) {
			return nil, dummyErr
		})
		return err
	}

	runSuccess := func() error {
		_, err := cb.Execute(ctx, func() (interface{}, error) {
			return "ok", nil
		})
		return err
	}

	// 1. Initial state should be closed. We need 10 requests to evaluate ReadyToTrip.
	// We'll generate 10 consecutive failures.
	for i := 0; i < 10; i++ {
		err := runFailure()
		if err != dummyErr && !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("expected dummyErr or ErrCircuitOpen, got %v", err)
		}
	}

	// 2. Circuit should now be open. Next request should fast-fail.
	err := runSuccess()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v (state: %s)", err, cb.State())
	}

	// 3. Wait for timeout to transition to half-open
	time.Sleep(timeout + 50*time.Millisecond)

	// 4. Send 5 successful requests to probe and close the circuit (since MaxRequests=5)
	for i := 0; i < 5; i++ {
		err = runSuccess()
		if err != nil {
			t.Fatalf("expected success on half-open probe, got %v", err)
		}
	}

	// 5. Next request should be allowed (closed state)
	err = runSuccess()
	if err != nil {
		t.Fatalf("expected success on closed state, got %v", err)
	}

	if cb.State() != "closed" {
		t.Fatalf("expected closed state, got %s", cb.State())
	}
}
