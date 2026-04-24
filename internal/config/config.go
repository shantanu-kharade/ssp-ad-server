// Package config handles loading and validating application configuration
// from environment variables. It uses godotenv for .env file support and
// envconfig for struct-based parsing.
package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Config holds all runtime configuration for the SSP ad server.
// Values are populated from environment variables with the specified prefixes.
type Config struct {
	// Server contains HTTP server settings.
	Server ServerConfig
	// Log contains logging configuration.
	Log LogConfig
	// Kafka contains Kafka event streaming configuration.
	Kafka KafkaConfig
}

// ServerConfig holds HTTP server-specific settings.
type ServerConfig struct {
	// Port is the TCP port the server listens on.
	Port int `envconfig:"SERVER_PORT" default:"8080"`
	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration `envconfig:"SERVER_READ_TIMEOUT" default:"5s"`
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout time.Duration `envconfig:"SERVER_WRITE_TIMEOUT" default:"10s"`
	// RedisURL is the connection string for Redis.
	RedisURL string `envconfig:"REDIS_URL" default:"redis://localhost:6379"`
	// AdminAPIKey is the static key required for /admin endpoints.
	AdminAPIKey string `envconfig:"ADMIN_API_KEY" default:"secret-admin-key"`
	// DSPEndpoints is a JSON array string containing DSP configurations.
	DSPEndpoints string `envconfig:"DSP_ENDPOINTS" default:"[{\"name\":\"dsp-mock\",\"url\":\"http://localhost:8081/bid\",\"timeout_ms\":50,\"weight\":1}]"`
	// HouseAdsEnabled controls whether house ads are served as fallbacks.
	HouseAdsEnabled bool `envconfig:"HOUSE_ADS_ENABLED" default:"true"`
	// HouseAdCampaignID is the campaign ID of the fallback house ad.
	HouseAdCampaignID string `envconfig:"HOUSE_AD_CAMPAIGN_ID" default:"house-camp-001"`
	// HouseAdCreativeID is the creative ID of the fallback house ad.
	HouseAdCreativeID string `envconfig:"HOUSE_AD_CREATIVE_ID" default:"house-cr-001"`
	// HouseAdMarkup is the actual ad markup to serve.
	HouseAdMarkup string `envconfig:"HOUSE_AD_MARKUP" default:"<!-- house ad -->"`
	// HouseAdPrice is the clearing price of the fallback house ad.
	HouseAdPrice float64 `envconfig:"HOUSE_AD_PRICE" default:"0.01"`
	// DBURL is the PostgreSQL connection string.
	DBURL string `envconfig:"DB_URL" default:"postgres://ssp:ssp@localhost:5433/ssp_db?sslmode=disable"`
}

// LogConfig holds logging-specific settings.
type LogConfig struct {
	// Level sets the minimum logging level (debug, info, warn, error).
	Level string `envconfig:"LOG_LEVEL" default:"info"`
}

// KafkaConfig holds Kafka event streaming settings.
type KafkaConfig struct {
	// Brokers is a list of Kafka broker addresses.
	Brokers []string `envconfig:"KAFKA_BROKERS" default:"localhost:9092"`
	// ImpressionTopic is the topic for impression events.
	ImpressionTopic string `envconfig:"KAFKA_IMPRESSION_TOPIC" default:"impression_events"`
	// ClickTopic is the topic for click events.
	ClickTopic string `envconfig:"KAFKA_CLICK_TOPIC" default:"click_events"`
	// WriterBatchSize is the number of messages to batch before sending.
	WriterBatchSize int `envconfig:"KAFKA_WRITER_BATCH_SIZE" default:"100"`
	// WriterBatchTimeoutMs is the maximum time to wait before sending a batch.
	WriterBatchTimeoutMs int `envconfig:"KAFKA_WRITER_BATCH_TIMEOUT_MS" default:"10"`
	// WriterAsync controls if the Kafka writer operates asynchronously.
	WriterAsync bool `envconfig:"KAFKA_WRITER_ASYNC" default:"true"`
}

// Load reads configuration from a .env file (if present) and then from
// environment variables, returning a fully populated and validated Config.
// It returns an error wrapped with context if any step fails.
func Load() (*Config, error) {
	// Load .env file if it exists; ignore error if file is missing
	// since production typically uses real environment variables.
	_ = godotenv.Load()

	var serverCfg ServerConfig
	if err := envconfig.Process("", &serverCfg); err != nil {
		return nil, fmt.Errorf("failed to process server config from environment: %w", err)
	}

	var logCfg LogConfig
	if err := envconfig.Process("", &logCfg); err != nil {
		return nil, fmt.Errorf("failed to process log config from environment: %w", err)
	}

	var kafkaCfg KafkaConfig
	if err := envconfig.Process("", &kafkaCfg); err != nil {
		return nil, fmt.Errorf("failed to process kafka config from environment: %w", err)
	}

	cfg := &Config{
		Server: serverCfg,
		Log:    logCfg,
		Kafka:  kafkaCfg,
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// validate performs semantic validation on the loaded configuration values.
func validate(cfg *Config) error {
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("SERVER_PORT must be between 1 and 65535, got %d", cfg.Server.Port)
	}

	if cfg.Server.ReadTimeout <= 0 {
		return fmt.Errorf("SERVER_READ_TIMEOUT must be positive, got %s", cfg.Server.ReadTimeout)
	}

	if cfg.Server.WriteTimeout <= 0 {
		return fmt.Errorf("SERVER_WRITE_TIMEOUT must be positive, got %s", cfg.Server.WriteTimeout)
	}

	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.Log.Level] {
		return fmt.Errorf("LOG_LEVEL must be one of [debug, info, warn, error], got %q", cfg.Log.Level)
	}

	return nil
}
