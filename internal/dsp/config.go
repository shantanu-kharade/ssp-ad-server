package dsp

import (
	"encoding/json"
	"fmt"
)

// Config represents the configuration for a single DSP endpoint.
type Config struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	TimeoutMs int    `json:"timeout_ms"`
	Weight    int    `json:"weight"`
}

// ParseConfig parses a JSON array string into a slice of Config objects.
func ParseConfig(configJSON string) ([]Config, error) {
	var configs []Config
	if configJSON == "" {
		return configs, nil
	}
	
	if err := json.Unmarshal([]byte(configJSON), &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DSP config: %w", err)
	}
	
	return configs, nil
}
