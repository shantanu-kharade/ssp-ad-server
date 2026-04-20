// Package metrics provides in-memory atomic counters for server observability.
// Counters are safe for concurrent access and designed to be recorded from
// middleware and read via the /metrics HTTP endpoint.
package metrics

import (
	"sync/atomic"
	"time"
)

// Counters holds all server-wide atomic counters for request tracking.
type Counters struct {
	// TotalRequests is the total number of HTTP requests received.
	TotalRequests atomic.Int64
	// SuccessfulBids is the number of requests that returned a 2xx status (excluding 204).
	SuccessfulBids atomic.Int64
	// NoBids is the number of requests that returned 204 No Content.
	NoBids atomic.Int64
	// Errors is the number of requests that returned 4xx or 5xx status codes.
	Errors atomic.Int64
	// TotalLatencyMs is the cumulative latency in milliseconds across all requests.
	TotalLatencyMs atomic.Int64
	// startTime records when the counters were initialized for uptime calculation.
	startTime time.Time
}

// GlobalCounters is the singleton metrics counter instance used across the server.
var GlobalCounters = NewCounters()

// NewCounters creates a new Counters instance with the start time set to now.
func NewCounters() *Counters {
	return &Counters{
		startTime: time.Now(),
	}
}

// Record increments the appropriate counter based on the HTTP status code
// and adds the request latency to the cumulative total.
func (c *Counters) Record(statusCode int, latencyMs int64) {
	c.TotalRequests.Add(1)
	c.TotalLatencyMs.Add(latencyMs)

	switch {
	case statusCode == 204:
		c.NoBids.Add(1)
	case statusCode >= 200 && statusCode < 300:
		c.SuccessfulBids.Add(1)
	case statusCode >= 400:
		c.Errors.Add(1)
	}
}

// Snapshot returns a map of all current counter values suitable for JSON
// serialization. It includes computed fields like avg_latency_ms and
// uptime_seconds.
func (c *Counters) Snapshot() map[string]interface{} {
	total := c.TotalRequests.Load()
	successful := c.SuccessfulBids.Load()
	noBids := c.NoBids.Load()
	errors := c.Errors.Load()
	totalLatency := c.TotalLatencyMs.Load()

	var avgLatency float64
	if total > 0 {
		avgLatency = float64(totalLatency) / float64(total)
	}

	uptime := time.Since(c.startTime).Seconds()

	return map[string]interface{}{
		"total_requests":  total,
		"successful_bids": successful,
		"no_bids":         noBids,
		"errors":          errors,
		"avg_latency_ms":  avgLatency,
		"uptime_seconds":  uptime,
	}
}

// Reset zeroes all counters. This is called when the /metrics endpoint
// receives the reset=true query parameter.
func (c *Counters) Reset() {
	c.TotalRequests.Store(0)
	c.SuccessfulBids.Store(0)
	c.NoBids.Store(0)
	c.Errors.Store(0)
	c.TotalLatencyMs.Store(0)
}
