// Package config provides configuration management for c8s.
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration values for c8s.
type Config struct {
	// Docker configuration
	DockerTimeout time.Duration
	DockerHost    string

	// Logging configuration
	LogLevel string
	LogFile  string

	// Monitoring configuration
	RefreshInterval time.Duration
	LogHistory      time.Duration
	MaxLogLines     int

	// TUI configuration
	MaxConcurrentActions int
	ChannelTimeout       time.Duration

	// Performance tuning
	InitialContainerMapSize int
	LogLineBufferSize       int
}

// Load loads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		// Docker defaults
		DockerTimeout: getEnvDuration("DOCKER_TIMEOUT", 30*time.Second),
		DockerHost:    os.Getenv("DOCKER_HOST"),

		// Logging defaults
		LogLevel: getEnvString("LOG_LEVEL", "info"),
		LogFile:  getEnvString("LOG_FILE", "app.log"),

		// Monitoring defaults
		RefreshInterval: getEnvDuration("REFRESH_INTERVAL", 1*time.Second),
		LogHistory:      getEnvDuration("LOG_HISTORY", 1*time.Hour),
		MaxLogLines:     getEnvInt("MAX_LOG_LINES", 1000),

		// TUI defaults
		MaxConcurrentActions: getEnvInt("MAX_CONCURRENT_ACTIONS", 3),
		ChannelTimeout:       getEnvDuration("CHANNEL_TIMEOUT", 5*time.Second),

		// Performance defaults
		InitialContainerMapSize: getEnvInt("INITIAL_CONTAINER_MAP_SIZE", 256),
		LogLineBufferSize:       getEnvInt("LOG_LINE_BUFFER_SIZE", 100),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Validate checks that the configuration values are valid.
func (c *Config) Validate() error {
	if c.DockerTimeout <= 0 {
		return fmt.Errorf("DOCKER_TIMEOUT must be positive")
	}
	if c.RefreshInterval <= 0 {
		return fmt.Errorf("REFRESH_INTERVAL must be positive")
	}
	if c.LogHistory <= 0 {
		return fmt.Errorf("LOG_HISTORY must be positive")
	}
	if c.MaxLogLines <= 0 {
		return fmt.Errorf("MAX_LOG_LINES must be positive")
	}
	if c.MaxConcurrentActions <= 0 {
		return fmt.Errorf("MAX_CONCURRENT_ACTIONS must be positive")
	}
	if c.ChannelTimeout <= 0 {
		return fmt.Errorf("CHANNEL_TIMEOUT must be positive")
	}
	if c.InitialContainerMapSize <= 0 {
		return fmt.Errorf("INITIAL_CONTAINER_MAP_SIZE must be positive")
	}
	if c.LogLineBufferSize <= 0 {
		return fmt.Errorf("LOG_LINE_BUFFER_SIZE must be positive")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}

	return nil
}

// Helper functions for reading environment variables with defaults

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			log.Printf("WARNING: invalid value %q for %s, using default %v: %v", value, key, defaultValue, err)
			return defaultValue
		}
		return duration
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		intValue, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("WARNING: invalid value %q for %s, using default %d: %v", value, key, defaultValue, err)
			return defaultValue
		}
		return intValue
	}
	return defaultValue
}
