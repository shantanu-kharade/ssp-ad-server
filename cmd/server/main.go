// Package main is the entrypoint for the SSP ad server. It initialises
// configuration, sets up structured logging, and starts the HTTP server.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/yourusername/ssp-adserver/internal/cache"
	"github.com/yourusername/ssp-adserver/internal/config"
	"github.com/yourusername/ssp-adserver/internal/db"
	"github.com/yourusername/ssp-adserver/internal/events"
	"github.com/yourusername/ssp-adserver/internal/server"
)

func main() {
	// Load and validate configuration from environment.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	// Initialise the structured logger.
	logger, err := buildLogger(cfg.Log.Level)
	if err != nil {
		log.Fatalf("failed to initialise logger: %v", err)
	}
	defer func() {
		// Flush any buffered log entries on shutdown.
		_ = logger.Sync()
	}()

	tracerShutdown, err := initTracerProvider(context.Background())
	if err != nil {
		logger.Fatal("failed to initialize OpenTelemetry tracer provider", zap.Error(err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracerShutdown(shutdownCtx); err != nil {
			logger.Error("failed to shut down tracer provider", zap.Error(err))
		}
	}()

	// Initialize Redis cache client
	redisClient := cache.NewRedisClient(cfg.Server.RedisURL, logger)
	defer redisClient.Close()

	// Initialize PostgreSQL
	dbPool, err := db.Connect(context.Background(), cfg.Server.DBURL)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer dbPool.Close()

	if err := db.RunMigrations(cfg.Server.DBURL, "internal/db/migrations"); err != nil {
		logger.Fatal("failed to run database migrations", zap.Error(err))
	}

	// Initialize Kafka Producer
	eventProducer, err := events.NewEventProducer(cfg, logger)
	if err != nil {
		logger.Fatal("failed to initialize event producer", zap.Error(err))
	}
	defer func() {
		if err := eventProducer.Close(); err != nil {
			logger.Error("failed to close event producer", zap.Error(err))
		}
	}()

	// Create the Fiber application with all middleware and routes.
	app := server.New(cfg, logger, redisClient, dbPool, eventProducer)

	// Graceful shutdown: listen for OS signals in a separate goroutine.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))

		if err := app.Shutdown(); err != nil {
			logger.Error("server shutdown error", zap.Error(fmt.Errorf("graceful shutdown failed: %w", err)))
		}
	}()

	// Start the server (blocks until shutdown).
	if err := server.Start(app, cfg, logger); err != nil {
		logger.Fatal("server exited with error", zap.Error(err))
	}

	logger.Info("server shut down gracefully")
}

// buildLogger creates a production-ready zap.Logger at the specified level.
// It outputs JSON-formatted logs to stdout.
func buildLogger(level string) (*zap.Logger, error) {
	// Parse the log level string into a zapcore.Level.
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	zapCfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Encoding:         "json",
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "timestamp",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "message",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.MillisDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build zap logger: %w", err)
	}

	return logger, nil
}

func initTracerProvider(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	exp, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("ssp-adserver"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build OpenTelemetry resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
