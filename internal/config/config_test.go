package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Unset all env vars to test defaults
	envVars := []string{
		"DOCKER_TIMEOUT", "DOCKER_HOST", "LOG_LEVEL", "LOG_FILE",
		"REFRESH_INTERVAL", "LOG_HISTORY", "MAX_LOG_LINES",
		"MAX_CONCURRENT_ACTIONS", "CHANNEL_TIMEOUT",
		"INITIAL_CONTAINER_MAP_SIZE", "LOG_LINE_BUFFER_SIZE",
	}
	for _, v := range envVars {
		t.Setenv(v, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DockerTimeout != 30*time.Second {
		t.Errorf("DockerTimeout = %v, want %v", cfg.DockerTimeout, 30*time.Second)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFile != "app.log" {
		t.Errorf("LogFile = %q, want %q", cfg.LogFile, "app.log")
	}
	if cfg.RefreshInterval != 1*time.Second {
		t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, 1*time.Second)
	}
	if cfg.LogHistory != 1*time.Hour {
		t.Errorf("LogHistory = %v, want %v", cfg.LogHistory, 1*time.Hour)
	}
	if cfg.MaxLogLines != 1000 {
		t.Errorf("MaxLogLines = %d, want %d", cfg.MaxLogLines, 1000)
	}
	if cfg.MaxConcurrentActions != 3 {
		t.Errorf("MaxConcurrentActions = %d, want %d", cfg.MaxConcurrentActions, 3)
	}
	if cfg.ChannelTimeout != 5*time.Second {
		t.Errorf("ChannelTimeout = %v, want %v", cfg.ChannelTimeout, 5*time.Second)
	}
	if cfg.InitialContainerMapSize != 256 {
		t.Errorf("InitialContainerMapSize = %d, want %d", cfg.InitialContainerMapSize, 256)
	}
	if cfg.LogLineBufferSize != 100 {
		t.Errorf("LogLineBufferSize = %d, want %d", cfg.LogLineBufferSize, 100)
	}
}

func TestLoad_CustomEnv(t *testing.T) {
	t.Setenv("DOCKER_TIMEOUT", "10s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FILE", "custom.log")
	t.Setenv("REFRESH_INTERVAL", "500ms")
	t.Setenv("MAX_LOG_LINES", "500")
	t.Setenv("CHANNEL_TIMEOUT", "3s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DockerTimeout != 10*time.Second {
		t.Errorf("DockerTimeout = %v, want %v", cfg.DockerTimeout, 10*time.Second)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.LogFile != "custom.log" {
		t.Errorf("LogFile = %q, want %q", cfg.LogFile, "custom.log")
	}
	if cfg.RefreshInterval != 500*time.Millisecond {
		t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, 500*time.Millisecond)
	}
	if cfg.MaxLogLines != 500 {
		t.Errorf("MaxLogLines = %d, want %d", cfg.MaxLogLines, 500)
	}
	if cfg.ChannelTimeout != 3*time.Second {
		t.Errorf("ChannelTimeout = %v, want %v", cfg.ChannelTimeout, 3*time.Second)
	}
}

func TestLoad_InvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("DOCKER_TIMEOUT", "invalid")
	t.Setenv("MAX_LOG_LINES", "not-a-number")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// Invalid values should fall back to defaults
	if cfg.DockerTimeout != 30*time.Second {
		t.Errorf("DockerTimeout = %v, want default %v", cfg.DockerTimeout, 30*time.Second)
	}
	if cfg.MaxLogLines != 1000 {
		t.Errorf("MaxLogLines = %d, want default %d", cfg.MaxLogLines, 1000)
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "invalid")
	// Clear other env vars
	for _, v := range []string{"DOCKER_TIMEOUT", "REFRESH_INTERVAL", "LOG_HISTORY",
		"MAX_LOG_LINES", "MAX_CONCURRENT_ACTIONS", "CHANNEL_TIMEOUT",
		"INITIAL_CONTAINER_MAP_SIZE", "LOG_LINE_BUFFER_SIZE"} {
		t.Setenv(v, "")
	}

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error for invalid log level, got nil")
	}
}

func TestValidate_AllFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				DockerTimeout:           30 * time.Second,
				LogLevel:                "info",
				LogFile:                 "app.log",
				RefreshInterval:         time.Second,
				LogHistory:              time.Hour,
				MaxLogLines:             1000,
				MaxConcurrentActions:    3,
				ChannelTimeout:          5 * time.Second,
				InitialContainerMapSize: 256,
				LogLineBufferSize:       100,
			},
			wantErr: false,
		},
		{
			name: "zero DockerTimeout",
			config: Config{
				DockerTimeout:           0,
				LogLevel:                "info",
				RefreshInterval:         time.Second,
				LogHistory:              time.Hour,
				MaxLogLines:             1000,
				MaxConcurrentActions:    3,
				ChannelTimeout:          5 * time.Second,
				InitialContainerMapSize: 256,
				LogLineBufferSize:       100,
			},
			wantErr: true,
		},
		{
			name: "negative RefreshInterval",
			config: Config{
				DockerTimeout:           30 * time.Second,
				LogLevel:                "info",
				RefreshInterval:         -1,
				LogHistory:              time.Hour,
				MaxLogLines:             1000,
				MaxConcurrentActions:    3,
				ChannelTimeout:          5 * time.Second,
				InitialContainerMapSize: 256,
				LogLineBufferSize:       100,
			},
			wantErr: true,
		},
		{
			name: "zero MaxLogLines",
			config: Config{
				DockerTimeout:           30 * time.Second,
				LogLevel:                "info",
				RefreshInterval:         time.Second,
				LogHistory:              time.Hour,
				MaxLogLines:             0,
				MaxConcurrentActions:    3,
				ChannelTimeout:          5 * time.Second,
				InitialContainerMapSize: 256,
				LogLineBufferSize:       100,
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			config: Config{
				DockerTimeout:           30 * time.Second,
				LogLevel:                "verbose",
				RefreshInterval:         time.Second,
				LogHistory:              time.Hour,
				MaxLogLines:             1000,
				MaxConcurrentActions:    3,
				ChannelTimeout:          5 * time.Second,
				InitialContainerMapSize: 256,
				LogLineBufferSize:       100,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetEnvString(t *testing.T) {
	// Test with env var set
	key := "TEST_GET_ENV_STRING"
	t.Setenv(key, "custom")
	if got := getEnvString(key, "default"); got != "custom" {
		t.Errorf("getEnvString() = %q, want %q", got, "custom")
	}

	// Test with empty env var (should return default)
	t.Setenv(key, "")
	if got := getEnvString(key, "default"); got != "default" {
		t.Errorf("getEnvString() = %q, want %q", got, "default")
	}
}

func TestGetEnvDuration(t *testing.T) {
	key := "TEST_GET_ENV_DURATION"

	// Valid duration
	t.Setenv(key, "5s")
	if got := getEnvDuration(key, time.Second); got != 5*time.Second {
		t.Errorf("getEnvDuration() = %v, want %v", got, 5*time.Second)
	}

	// Invalid duration falls back to default
	t.Setenv(key, "invalid")
	if got := getEnvDuration(key, time.Second); got != time.Second {
		t.Errorf("getEnvDuration() = %v, want default %v", got, time.Second)
	}

	// Empty falls back to default
	t.Setenv(key, "")
	if got := getEnvDuration(key, time.Second); got != time.Second {
		t.Errorf("getEnvDuration() = %v, want default %v", got, time.Second)
	}
}

func TestGetEnvInt(t *testing.T) {
	key := "TEST_GET_ENV_INT"

	// Valid int
	t.Setenv(key, "42")
	if got := getEnvInt(key, 10); got != 42 {
		t.Errorf("getEnvInt() = %d, want %d", got, 42)
	}

	// Invalid int falls back to default
	t.Setenv(key, "not-a-number")
	if got := getEnvInt(key, 10); got != 10 {
		t.Errorf("getEnvInt() = %d, want default %d", got, 10)
	}

	// Empty falls back to default
	t.Setenv(key, "")
	if got := getEnvInt(key, 10); got != 10 {
		t.Errorf("getEnvInt() = %d, want default %d", got, 10)
	}
}

func TestDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://localhost:2375")
	// Clear other vars that might fail validation
	for _, v := range []string{"LOG_LEVEL", "DOCKER_TIMEOUT", "REFRESH_INTERVAL", "LOG_HISTORY",
		"MAX_LOG_LINES", "MAX_CONCURRENT_ACTIONS", "CHANNEL_TIMEOUT",
		"INITIAL_CONTAINER_MAP_SIZE", "LOG_LINE_BUFFER_SIZE"} {
		os.Unsetenv(v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	if cfg.DockerHost != "tcp://localhost:2375" {
		t.Errorf("DockerHost = %q, want %q", cfg.DockerHost, "tcp://localhost:2375")
	}
}
