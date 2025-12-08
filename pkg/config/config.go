package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all MIGlet configuration
type Config struct {
	// Identifiers
	PoolID string `mapstructure:"pool_id"`
	VMID   string `mapstructure:"vm_id"`
	OrgID  string `mapstructure:"org_id"`

	// MIG Controller
	Controller ControllerConfig `mapstructure:"controller"`

	// GitHub Runner (metadata only, no credentials)
	GitHub GitHubConfig `mapstructure:"github"`

	// Behavior
	Heartbeat HeartbeatConfig `mapstructure:"heartbeat"`
	Shutdown  ShutdownConfig  `mapstructure:"shutdown"`

	// Logging
	Logging LoggingConfig `mapstructure:"logging"`

	// Metrics
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Storage
	Storage StorageConfig `mapstructure:"storage"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	MongoDB MongoDBConfig `mapstructure:"mongodb"`
}

// MongoDBConfig holds MongoDB configuration
type MongoDBConfig struct {
	Enabled          bool   `mapstructure:"enabled"`
	ConnectionString string `mapstructure:"connection_string"`
	Database         string `mapstructure:"database"`
	Collection       string `mapstructure:"collection"`
}

// ControllerConfig holds MIG Controller configuration
type ControllerConfig struct {
	Endpoint     string        `mapstructure:"endpoint"`      // HTTP endpoint (legacy, used for gRPC derivation)
	GRPCEndpoint string        `mapstructure:"grpc_endpoint"` // gRPC endpoint (e.g., "localhost:50051")
	Auth         AuthConfig    `mapstructure:"auth"`
	Timeout      time.Duration `mapstructure:"timeout"`
	Retry        RetryConfig   `mapstructure:"retry"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Type      string `mapstructure:"type"`       // "bearer" or "mtls"
	TokenPath string `mapstructure:"token_path"` // Path to token file
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts    int           `mapstructure:"max_attempts"`
	InitialBackoff time.Duration `mapstructure:"initial_backoff"`
	MaxBackoff     time.Duration `mapstructure:"max_backoff"`
}

// GitHubConfig holds GitHub runner configuration (metadata only)
type GitHubConfig struct {
	Org          string        `mapstructure:"org"`
	RunnerGroup  string        `mapstructure:"runner_group"`
	Labels       []string      `mapstructure:"labels"`
	TokenSource  string        `mapstructure:"token_source"`  // "controller" or "metadata"
	MetadataPath string        `mapstructure:"metadata_path"` // If token_source is "metadata"
	Timeout      time.Duration `mapstructure:"registration_timeout"`
}

// HeartbeatConfig holds heartbeat configuration
type HeartbeatConfig struct {
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// ShutdownConfig holds shutdown configuration
type ShutdownConfig struct {
	GracePeriod time.Duration `mapstructure:"grace_period"`
	ForceAfter  time.Duration `mapstructure:"force_after"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level         string `mapstructure:"level"`  // "debug", "info", "warn", "error"
	Format        string `mapstructure:"format"` // "json" or "text"
	RedactSecrets bool   `mapstructure:"redact_secrets"`
}

// MetricsConfig holds metrics collection configuration
type MetricsConfig struct {
	CollectionInterval time.Duration `mapstructure:"collection_interval"`
	IncludeDisk        bool          `mapstructure:"include_disk"`
	IncludeNetwork     bool          `mapstructure:"include_network"`
}

// Load loads configuration from multiple sources (priority order):
// 1. Environment variables (MIGLET_*)
// 2. Config file
// 3. Metadata server (future)
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Load from environment variables
	// Read env vars directly and set in viper to ensure they're picked up
	// This works around viper's AutomaticEnv limitations with Unmarshal
	if val := os.Getenv("MIGLET_POOL_ID"); val != "" {
		v.Set("pool_id", val)
	}
	if val := os.Getenv("MIGLET_VM_ID"); val != "" {
		v.Set("vm_id", val)
	}
	if val := os.Getenv("MIGLET_ORG_ID"); val != "" {
		v.Set("org_id", val)
	}
	if val := os.Getenv("MIGLET_CONTROLLER_ENDPOINT"); val != "" {
		v.Set("controller.endpoint", val)
	}
	if val := os.Getenv("MIGLET_CONTROLLER_AUTH_TYPE"); val != "" {
		v.Set("controller.auth.type", val)
	}
	if val := os.Getenv("MIGLET_CONTROLLER_AUTH_TOKEN_PATH"); val != "" {
		v.Set("controller.auth.token_path", val)
	}
	if val := os.Getenv("MIGLET_CONTROLLER_TIMEOUT"); val != "" {
		v.Set("controller.timeout", val)
	}
	if val := os.Getenv("MIGLET_GITHUB_ORG"); val != "" {
		v.Set("github.org", val)
	}
	if val := os.Getenv("MIGLET_GITHUB_RUNNER_GROUP"); val != "" {
		v.Set("github.runner_group", val)
	}
	if val := os.Getenv("MIGLET_GITHUB_LABELS"); val != "" {
		// Labels can be comma-separated
		labels := strings.Split(val, ",")
		for i, label := range labels {
			labels[i] = strings.TrimSpace(label)
		}
		v.Set("github.labels", labels)
	}
	if val := os.Getenv("MIGLET_GITHUB_TOKEN_SOURCE"); val != "" {
		v.Set("github.token_source", val)
	}
	if val := os.Getenv("MIGLET_GITHUB_METADATA_PATH"); val != "" {
		v.Set("github.metadata_path", val)
	}
	if val := os.Getenv("MIGLET_GITHUB_REGISTRATION_TIMEOUT"); val != "" {
		v.Set("github.registration_timeout", val)
	}
	if val := os.Getenv("MIGLET_LOGGING_LEVEL"); val != "" {
		v.Set("logging.level", val)
	}
	if val := os.Getenv("MIGLET_LOGGING_FORMAT"); val != "" {
		v.Set("logging.format", val)
	}
	if val := os.Getenv("MIGLET_LOGGING_REDACT_SECRETS"); val != "" {
		v.Set("logging.redact_secrets", val == "true" || val == "1")
	}
	if val := os.Getenv("MIGLET_STORAGE_MONGODB_ENABLED"); val != "" {
		v.Set("storage.mongodb.enabled", val == "true" || val == "1")
	}
	if val := os.Getenv("MIGLET_STORAGE_MONGODB_CONNECTION_STRING"); val != "" {
		v.Set("storage.mongodb.connection_string", val)
	}
	if val := os.Getenv("MIGLET_STORAGE_MONGODB_DATABASE"); val != "" {
		v.Set("storage.mongodb.database", val)
	}
	if val := os.Getenv("MIGLET_STORAGE_MONGODB_COLLECTION"); val != "" {
		v.Set("storage.mongodb.collection", val)
	}

	// Load from config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate required fields
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default values for configuration
func setDefaults(v *viper.Viper) {
	// Controller defaults
	v.SetDefault("controller.timeout", "30s")
	v.SetDefault("controller.retry.max_attempts", 5)
	v.SetDefault("controller.retry.initial_backoff", "1s")
	v.SetDefault("controller.retry.max_backoff", "30s")

	// GitHub defaults
	v.SetDefault("github.token_source", "controller")
	v.SetDefault("github.registration_timeout", "60s")

	// Heartbeat defaults
	v.SetDefault("heartbeat.interval", "15s")
	v.SetDefault("heartbeat.timeout", "60s")

	// Shutdown defaults
	v.SetDefault("shutdown.grace_period", "30s")
	v.SetDefault("shutdown.force_after", "5m")

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.redact_secrets", true)

	// Metrics defaults
	v.SetDefault("metrics.collection_interval", "10s")
	v.SetDefault("metrics.include_disk", true)
	v.SetDefault("metrics.include_network", true)

	// Storage defaults
	v.SetDefault("storage.mongodb.enabled", false)
	v.SetDefault("storage.mongodb.database", "monkci")
	v.SetDefault("storage.mongodb.collection", "heartbeats")
}

// validate validates required configuration fields
func validate(cfg *Config) error {
	if cfg.PoolID == "" {
		return fmt.Errorf("pool_id is required")
	}
	if cfg.VMID == "" {
		return fmt.Errorf("vm_id is required")
	}
	// org_id is optional - MIGlet is ephemeral and may not have org context at boot
	// if cfg.OrgID == "" {
	// 	return fmt.Errorf("org_id is required")
	// }

	// Either gRPC endpoint or HTTP endpoint (for derivation) is required
	if cfg.Controller.GRPCEndpoint == "" && cfg.Controller.Endpoint == "" {
		return fmt.Errorf("controller.grpc_endpoint or controller.endpoint is required")
	}

	// github.org is optional - may be provided later via controller
	// if cfg.GitHub.Org == "" {
	// 	return fmt.Errorf("github.org is required")
	// }
	return nil
}

// LoadFromEnv loads configuration primarily from environment variables
// Useful for testing or when config file is not available
func LoadFromEnv() (*Config, error) {
	return Load("")
}
