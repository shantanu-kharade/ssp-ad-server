package resilience

import (
	"context"
	"errors"
	"time"

	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

// DSPCircuitBreaker wraps gobreaker.CircuitBreaker for DSP connections.
type DSPCircuitBreaker struct {
	cb *gobreaker.CircuitBreaker
}

// NewDSPCircuitBreaker creates a new circuit breaker with default settings.
func NewDSPCircuitBreaker(dspName string, log *zap.Logger) *DSPCircuitBreaker {
	return newDSPCircuitBreakerWithTimeout(dspName, log, 30*time.Second)
}

func newDSPCircuitBreakerWithTimeout(dspName string, log *zap.Logger, timeout time.Duration) *DSPCircuitBreaker {
	settings := gobreaker.Settings{
		Name:        dspName,
		MaxRequests: 5,
		Interval:    60 * time.Second,
		Timeout:     timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio > 0.5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Warn("circuit breaker state change",
				zap.String("dsp", name),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			)
		},
	}

	return &DSPCircuitBreaker{
		cb: gobreaker.NewCircuitBreaker(settings),
	}
}

// Execute wraps a function execution in the circuit breaker.
func (d *DSPCircuitBreaker) Execute(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	res, err := d.cb.Execute(func() (interface{}, error) {
		return fn()
	})

	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, ErrCircuitOpen
		}
		return nil, err
	}

	return res, nil
}

// State returns the current state of the circuit breaker as a string.
func (d *DSPCircuitBreaker) State() string {
	return d.cb.State().String()
}
