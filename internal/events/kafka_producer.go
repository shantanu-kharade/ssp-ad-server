package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"

	"github.com/yourusername/ssp-adserver/internal/config"
)

const (
	maxRetries  = 3
	retryBackoff = 100 * time.Millisecond
)

// EventProducer handles publishing events to Kafka.
type EventProducer struct {
	impWriter   *kafka.Writer
	clickWriter *kafka.Writer
	logger      *zap.Logger
}

// NewEventProducer creates a new EventProducer with two Kafka writers.
func NewEventProducer(cfg *config.Config, logger *zap.Logger) (*EventProducer, error) {
	if len(cfg.Kafka.Brokers) == 0 {
		return nil, errors.New("kafka brokers list is empty")
	}

	// In order to implement custom retry logic for the sync writer as requested,
	// we will set Async = false and MaxAttempts = 1 on the kafka-go writer,
	// and handle the retries in the Publish methods.
	createWriter := func(topic string) *kafka.Writer {
		return &kafka.Writer{
			Addr:         kafka.TCP(cfg.Kafka.Brokers...),
			Topic:        topic,
			Balancer:     &kafka.Hash{},
			BatchSize:    cfg.Kafka.WriterBatchSize,
			BatchTimeout: time.Duration(cfg.Kafka.WriterBatchTimeoutMs) * time.Millisecond,
			Async:        false, // Explicitly sync so we can catch and retry errors
			MaxAttempts:  1,     // Disable internal retries in favor of our custom retry loop
		}
	}

	return &EventProducer{
		impWriter:   createWriter(cfg.Kafka.ImpressionTopic),
		clickWriter: createWriter(cfg.Kafka.ClickTopic),
		logger:      logger,
	}, nil
}

// PublishImpressionEvent publishes an impression event to Kafka with retry logic.
func (p *EventProducer) PublishImpressionEvent(ctx context.Context, event ImpressionEvent) error {
	data, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal impression event: %w", err)
	}

	msg := kafka.Message{
		Key:   []byte(event.RequestID), // Route consistently by RequestID
		Value: data,
	}

	return p.writeWithRetry(ctx, p.impWriter, msg, "impression")
}

// PublishClickEvent publishes a click event to Kafka with retry logic.
func (p *EventProducer) PublishClickEvent(ctx context.Context, event ClickEvent) error {
	data, err := event.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal click event: %w", err)
	}

	msg := kafka.Message{
		Key:   []byte(event.RequestID), // Route consistently by RequestID
		Value: data,
	}

	return p.writeWithRetry(ctx, p.clickWriter, msg, "click")
}

// writeWithRetry handles the transient error retry logic.
func (p *EventProducer) writeWithRetry(ctx context.Context, writer *kafka.Writer, msg kafka.Message, eventType string) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = writer.WriteMessages(ctx, msg)
		if err == nil {
			return nil
		}

		// Log transient error and retry
		p.logger.Warn(fmt.Sprintf("failed to publish %s event, retrying", eventType),
			zap.Int("attempt", i+1),
			zap.Error(err),
		)

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during %s event publish: %w", eventType, ctx.Err())
		case <-time.After(retryBackoff):
			// Backoff and retry
		}
	}

	// After max retries, log and return error
	p.logger.Error(fmt.Sprintf("failed to publish %s event after %d attempts", eventType, maxRetries),
		zap.Error(err),
	)
	return fmt.Errorf("exhausted retries for %s event: %w", eventType, err)
}

// Close gracefully shuts down the producers, flushing any pending messages.
func (p *EventProducer) Close() error {
	var errs []error
	if err := p.impWriter.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close impression writer: %w", err))
	}
	if err := p.clickWriter.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close click writer: %w", err))
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("errors closing event producer: %v", errs)
	}
	return nil
}

// Ping checks the health of the Kafka connection by dialing the first broker.
func (p *EventProducer) Ping(ctx context.Context) error {
	// The writer has the address (kafka.TCP(brokers...)).
	// For simplicity, we can just dial the address of the impression writer.
	conn, err := kafka.DialContext(ctx, "tcp", p.impWriter.Addr.String())
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}
