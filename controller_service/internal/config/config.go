package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all MIG Controller configuration
type Config struct {
	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Pool configuration (identifies which pool this controller manages)
	Pool PoolConfig `mapstructure:"pool"`

	// GCP configuration
	GCP GCPConfig `mapstructure:"gcp"`

	// GitHub App configuration
	GitHubApp GitHubAppConfig `mapstructure:"github_app"`

	// Redis configuration
	Redis RedisConfig `mapstructure:"redis"`

	// Pub/Sub configuration
	PubSub PubSubConfig `mapstructure:"pubsub"`

	// Scheduler configuration
	Scheduler SchedulerConfig `mapstructure:"scheduler"`

	// VM Manager configuration
	VMManager VMManagerConfig `mapstructure:"vm_manager"`

	// MIGlet configuration (for communicating with agents)
	MIGlet MIGletConfig `mapstructure:"miglet"`

	// Logging configuration
	Logging LoggingConfig `mapstructure:"logging"`

	// Metrics configuration
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Alerts configuration
	Alerts AlertsConfig `mapstructure:"alerts"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	GRPCPort          int           `mapstructure:"grpc_port"`
	HTTPPort          int           `mapstructure:"http_port"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	MaxConnectionAge  time.Duration `mapstructure:"max_connection_age"`
	KeepaliveInterval time.Duration `mapstructure:"keepalive_interval"`
	KeepaliveTimeout  time.Duration `mapstructure:"keepalive_timeout"`
	TLS               TLSConfig     `mapstructure:"tls"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertPath string `mapstructure:"cert_path"`
	KeyPath  string `mapstructure:"key_path"`
	CAPath   string `mapstructure:"ca_path"` // For mTLS
}

// PoolConfig identifies the pool this controller manages
type PoolConfig struct {
	ID          string   `mapstructure:"id"`           // Unique identifier (e.g., "pool-2vcpu-linux-us-central1")
	Name        string   `mapstructure:"name"`         // Human-readable name
	Type        string   `mapstructure:"type"`         // Machine type (e.g., "2vcpu", "4vcpu", "8vcpu")
	OS          string   `mapstructure:"os"`           // Operating system (linux, windows)
	Arch        string   `mapstructure:"arch"`         // Architecture (x64, arm64)
	Region      string   `mapstructure:"region"`       // GCP region
	Labels      []string `mapstructure:"labels"`       // Default labels for runners
	RunnerGroup string   `mapstructure:"runner_group"` // GitHub runner group
}

// GCPConfig holds GCP-specific configuration
type GCPConfig struct {
	ProjectID          string `mapstructure:"project_id"`
	Zone               string `mapstructure:"zone"`
	MIGName            string `mapstructure:"mig_name"`
	NetworkProject     string `mapstructure:"network_project"`      // For shared VPC
	Network            string `mapstructure:"network"`              // VPC network name
	Subnetwork         string `mapstructure:"subnetwork"`           // Subnetwork name
	ServiceAccountPath string `mapstructure:"service_account_path"` // Path to SA key (if not using default)
}

// GitHubAppConfig holds GitHub App configuration
type GitHubAppConfig struct {
	AppID          int64  `mapstructure:"app_id"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
	PrivateKey     string `mapstructure:"private_key"` // Direct key value (for K8s secrets)
	WebhookSecret  string `mapstructure:"webhook_secret"`
	BaseURL        string `mapstructure:"base_url"` // For GitHub Enterprise
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Jobs     RedisInstanceConfig `mapstructure:"jobs"`
	VMStatus RedisInstanceConfig `mapstructure:"vm_status"`
}

// RedisInstanceConfig holds configuration for a single Redis instance
type RedisInstanceConfig struct {
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	Password       string        `mapstructure:"password"`
	DB             int           `mapstructure:"db"`
	TLS            bool          `mapstructure:"tls"`
	MaxRetries     int           `mapstructure:"max_retries"`
	PoolSize       int           `mapstructure:"pool_size"`
	MinIdleConns   int           `mapstructure:"min_idle_conns"`
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
}

// PubSubConfig holds Pub/Sub configuration
type PubSubConfig struct {
	ProjectID              string        `mapstructure:"project_id"`
	Subscription           string        `mapstructure:"subscription"`
	TopicID                string        `mapstructure:"topic_id"` // For publishing events
	MaxOutstandingMessages int           `mapstructure:"max_outstanding_messages"`
	MaxOutstandingBytes    int           `mapstructure:"max_outstanding_bytes"`
	NumGoroutines          int           `mapstructure:"num_goroutines"`
	AckDeadline            time.Duration `mapstructure:"ack_deadline"`
}

// SchedulerConfig holds scheduler configuration
type SchedulerConfig struct {
	PollInterval             time.Duration `mapstructure:"poll_interval"`
	AssignmentTimeout        time.Duration `mapstructure:"assignment_timeout"`
	MaxConcurrentAssignments int           `mapstructure:"max_concurrent_assignments"`
	RetryInterval            time.Duration `mapstructure:"retry_interval"`
	MaxRetries               int           `mapstructure:"max_retries"`
	JobTimeout               time.Duration `mapstructure:"job_timeout"` // Max job duration
}

// VMManagerConfig holds VM manager configuration
type VMManagerConfig struct {
	PollInterval        time.Duration `mapstructure:"poll_interval"`
	HeartbeatTimeout    time.Duration `mapstructure:"heartbeat_timeout"`
	MaxScaleUpPerMinute int           `mapstructure:"max_scale_up_per_minute"`
	MinReadyVMs         int           `mapstructure:"min_ready_vms"`
	MaxVMs              int           `mapstructure:"max_vms"`
	IdleTimeout         time.Duration `mapstructure:"idle_timeout"`
	BootTimeout         time.Duration `mapstructure:"boot_timeout"`  // Max time for VM to boot
	DrainTimeout        time.Duration `mapstructure:"drain_timeout"` // Max time to wait for job completion on drain
	DeleteDelay         time.Duration `mapstructure:"delete_delay"`  // Delay before deleting stopped VMs
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`
}

// MIGletConfig holds configuration for MIGlet communication
type MIGletConfig struct {
	CommandTimeout    time.Duration `mapstructure:"command_timeout"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	ReconnectInterval time.Duration `mapstructure:"reconnect_interval"`
	MaxReconnectDelay time.Duration `mapstructure:"max_reconnect_delay"`
	RunnerInstallPath string        `mapstructure:"runner_install_path"`
	RunnerVersion     string        `mapstructure:"runner_version"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level         string `mapstructure:"level"`
	Format        string `mapstructure:"format"`
	OutputPath    string `mapstructure:"output_path"` // File path or "stdout"
	RedactSecrets bool   `mapstructure:"redact_secrets"`
}

// MetricsConfig holds metrics/observability configuration
type MetricsConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	Port         int           `mapstructure:"port"`
	Path         string        `mapstructure:"path"`
	PushGateway  string        `mapstructure:"push_gateway"` // Prometheus push gateway URL
	PushInterval time.Duration `mapstructure:"push_interval"`
}

// AlertsConfig holds alerting configuration
type AlertsConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	SlackWebhook   string        `mapstructure:"slack_webhook"`
	PagerDutyKey   string        `mapstructure:"pagerduty_key"`
	EmailRecipient string        `mapstructure:"email_recipient"`
	AlertCooldown  time.Duration `mapstructure:"alert_cooldown"`
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read from environment variables
	v.SetEnvPrefix("CONTROLLER")
	v.AutomaticEnv()

	// Explicit environment variable bindings
	bindEnvVars(v)

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

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.grpc_port", 50051)
	v.SetDefault("server.http_port", 8080)
	v.SetDefault("server.shutdown_timeout", "30s")
	v.SetDefault("server.max_connection_age", "30m")
	v.SetDefault("server.keepalive_interval", "10s")
	v.SetDefault("server.keepalive_timeout", "3s")
	v.SetDefault("server.tls.enabled", false)

	// Pool defaults
	v.SetDefault("pool.os", "linux")
	v.SetDefault("pool.arch", "x64")
	v.SetDefault("pool.runner_group", "default")
	v.SetDefault("pool.labels", []string{"self-hosted"})

	// GCP defaults
	v.SetDefault("gcp.network", "default")

	// GitHub App defaults
	v.SetDefault("github_app.base_url", "https://api.github.com")

	// Redis defaults
	v.SetDefault("redis.jobs.port", 6379)
	v.SetDefault("redis.jobs.db", 0)
	v.SetDefault("redis.jobs.max_retries", 3)
	v.SetDefault("redis.jobs.pool_size", 10)
	v.SetDefault("redis.jobs.min_idle_conns", 2)
	v.SetDefault("redis.jobs.connect_timeout", "5s")
	v.SetDefault("redis.jobs.read_timeout", "3s")
	v.SetDefault("redis.jobs.write_timeout", "3s")

	v.SetDefault("redis.vm_status.port", 6379)
	v.SetDefault("redis.vm_status.db", 1)
	v.SetDefault("redis.vm_status.max_retries", 3)
	v.SetDefault("redis.vm_status.pool_size", 10)
	v.SetDefault("redis.vm_status.min_idle_conns", 2)
	v.SetDefault("redis.vm_status.connect_timeout", "5s")
	v.SetDefault("redis.vm_status.read_timeout", "3s")
	v.SetDefault("redis.vm_status.write_timeout", "3s")

	// Pub/Sub defaults
	v.SetDefault("pubsub.max_outstanding_messages", 100)
	v.SetDefault("pubsub.max_outstanding_bytes", 10485760) // 10MB
	v.SetDefault("pubsub.num_goroutines", 10)
	v.SetDefault("pubsub.ack_deadline", "60s")

	// Scheduler defaults
	v.SetDefault("scheduler.poll_interval", "1s")
	v.SetDefault("scheduler.assignment_timeout", "5m")
	v.SetDefault("scheduler.max_concurrent_assignments", 10)
	v.SetDefault("scheduler.retry_interval", "30s")
	v.SetDefault("scheduler.max_retries", 3)
	v.SetDefault("scheduler.job_timeout", "6h")

	// VM Manager defaults
	v.SetDefault("vm_manager.poll_interval", "30s")
	v.SetDefault("vm_manager.heartbeat_timeout", "60s")
	v.SetDefault("vm_manager.max_scale_up_per_minute", 5)
	v.SetDefault("vm_manager.min_ready_vms", 1)
	v.SetDefault("vm_manager.max_vms", 50)
	v.SetDefault("vm_manager.idle_timeout", "10m")
	v.SetDefault("vm_manager.boot_timeout", "5m")
	v.SetDefault("vm_manager.drain_timeout", "30m")
	v.SetDefault("vm_manager.delete_delay", "1h")
	v.SetDefault("vm_manager.health_check_interval", "1m")

	// MIGlet defaults
	v.SetDefault("miglet.command_timeout", "30s")
	v.SetDefault("miglet.heartbeat_interval", "15s")
	v.SetDefault("miglet.reconnect_interval", "5s")
	v.SetDefault("miglet.max_reconnect_delay", "5m")
	v.SetDefault("miglet.runner_install_path", "/tmp/miglet-runner")
	v.SetDefault("miglet.runner_version", "2.329.0")

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.output_path", "stdout")
	v.SetDefault("logging.redact_secrets", true)

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("metrics.push_interval", "30s")

	// Alerts defaults
	v.SetDefault("alerts.enabled", false)
	v.SetDefault("alerts.alert_cooldown", "5m")
}

func bindEnvVars(v *viper.Viper) {
	// Server config
	bindEnv(v, "server.grpc_port", "GRPC_PORT")
	bindEnv(v, "server.http_port", "HTTP_PORT")
	bindEnv(v, "server.tls.enabled", "TLS_ENABLED")
	bindEnv(v, "server.tls.cert_path", "TLS_CERT_PATH")
	bindEnv(v, "server.tls.key_path", "TLS_KEY_PATH")
	bindEnv(v, "server.tls.ca_path", "TLS_CA_PATH")

	// Pool config
	bindEnv(v, "pool.id", "POOL_ID")
	bindEnv(v, "pool.name", "POOL_NAME")
	bindEnv(v, "pool.type", "POOL_TYPE")
	bindEnv(v, "pool.os", "POOL_OS")
	bindEnv(v, "pool.arch", "POOL_ARCH")
	bindEnv(v, "pool.region", "POOL_REGION")
	bindEnv(v, "pool.runner_group", "POOL_RUNNER_GROUP")
	bindEnvStringSlice(v, "pool.labels", "POOL_LABELS")

	// GCP config
	bindEnv(v, "gcp.project_id", "GCP_PROJECT_ID")
	bindEnv(v, "gcp.zone", "GCP_ZONE")
	bindEnv(v, "gcp.mig_name", "GCP_MIG_NAME")
	bindEnv(v, "gcp.network_project", "GCP_NETWORK_PROJECT")
	bindEnv(v, "gcp.network", "GCP_NETWORK")
	bindEnv(v, "gcp.subnetwork", "GCP_SUBNETWORK")
	bindEnv(v, "gcp.service_account_path", "GCP_SERVICE_ACCOUNT_PATH")

	// GitHub App config
	bindEnvInt64(v, "github_app.app_id", "GITHUB_APP_ID")
	bindEnv(v, "github_app.private_key_path", "GITHUB_APP_PRIVATE_KEY_PATH")
	bindEnv(v, "github_app.private_key", "GITHUB_APP_PRIVATE_KEY")
	bindEnv(v, "github_app.webhook_secret", "GITHUB_WEBHOOK_SECRET")
	bindEnv(v, "github_app.base_url", "GITHUB_BASE_URL")

	// Redis - Jobs
	bindEnv(v, "redis.jobs.host", "REDIS_JOBS_HOST")
	bindEnvInt(v, "redis.jobs.port", "REDIS_JOBS_PORT")
	bindEnv(v, "redis.jobs.password", "REDIS_JOBS_PASSWORD")
	bindEnvInt(v, "redis.jobs.db", "REDIS_JOBS_DB")
	bindEnvBool(v, "redis.jobs.tls", "REDIS_JOBS_TLS")

	// Redis - VM Status
	bindEnv(v, "redis.vm_status.host", "REDIS_VM_HOST")
	bindEnvInt(v, "redis.vm_status.port", "REDIS_VM_PORT")
	bindEnv(v, "redis.vm_status.password", "REDIS_VM_PASSWORD")
	bindEnvInt(v, "redis.vm_status.db", "REDIS_VM_DB")
	bindEnvBool(v, "redis.vm_status.tls", "REDIS_VM_TLS")

	// Pub/Sub config
	bindEnv(v, "pubsub.project_id", "PUBSUB_PROJECT_ID")
	bindEnv(v, "pubsub.subscription", "PUBSUB_SUBSCRIPTION")
	bindEnv(v, "pubsub.topic_id", "PUBSUB_TOPIC_ID")

	// Scheduler config
	bindEnv(v, "scheduler.poll_interval", "SCHEDULER_POLL_INTERVAL")
	bindEnv(v, "scheduler.assignment_timeout", "SCHEDULER_ASSIGNMENT_TIMEOUT")
	bindEnvInt(v, "scheduler.max_concurrent_assignments", "SCHEDULER_MAX_CONCURRENT")
	bindEnvInt(v, "scheduler.max_retries", "SCHEDULER_MAX_RETRIES")

	// VM Manager config
	bindEnv(v, "vm_manager.poll_interval", "VM_POLL_INTERVAL")
	bindEnv(v, "vm_manager.heartbeat_timeout", "VM_HEARTBEAT_TIMEOUT")
	bindEnvInt(v, "vm_manager.max_scale_up_per_minute", "VM_MAX_SCALE_UP")
	bindEnvInt(v, "vm_manager.min_ready_vms", "VM_MIN_READY")
	bindEnvInt(v, "vm_manager.max_vms", "VM_MAX_VMS")
	bindEnv(v, "vm_manager.idle_timeout", "VM_IDLE_TIMEOUT")
	bindEnv(v, "vm_manager.boot_timeout", "VM_BOOT_TIMEOUT")

	// MIGlet config
	bindEnv(v, "miglet.command_timeout", "MIGLET_COMMAND_TIMEOUT")
	bindEnv(v, "miglet.heartbeat_interval", "MIGLET_HEARTBEAT_INTERVAL")
	bindEnv(v, "miglet.runner_version", "MIGLET_RUNNER_VERSION")

	// Logging
	bindEnv(v, "logging.level", "LOG_LEVEL")
	bindEnv(v, "logging.format", "LOG_FORMAT")
	bindEnv(v, "logging.output_path", "LOG_OUTPUT")
	bindEnvBool(v, "logging.redact_secrets", "LOG_REDACT_SECRETS")

	// Metrics
	bindEnvBool(v, "metrics.enabled", "METRICS_ENABLED")
	bindEnvInt(v, "metrics.port", "METRICS_PORT")
	bindEnv(v, "metrics.push_gateway", "METRICS_PUSH_GATEWAY")

	// Alerts
	bindEnvBool(v, "alerts.enabled", "ALERTS_ENABLED")
	bindEnv(v, "alerts.slack_webhook", "ALERTS_SLACK_WEBHOOK")
	bindEnv(v, "alerts.pagerduty_key", "ALERTS_PAGERDUTY_KEY")
}

// Helper functions for environment variable binding
func bindEnv(v *viper.Viper, key, envKey string) {
	if val := os.Getenv("CONTROLLER_" + envKey); val != "" {
		v.Set(key, val)
	}
}

func bindEnvInt(v *viper.Viper, key, envKey string) {
	if val := os.Getenv("CONTROLLER_" + envKey); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			v.Set(key, intVal)
		}
	}
}

func bindEnvInt64(v *viper.Viper, key, envKey string) {
	if val := os.Getenv("CONTROLLER_" + envKey); val != "" {
		if intVal, err := strconv.ParseInt(val, 10, 64); err == nil {
			v.Set(key, intVal)
		}
	}
}

func bindEnvBool(v *viper.Viper, key, envKey string) {
	if val := os.Getenv("CONTROLLER_" + envKey); val != "" {
		v.Set(key, val == "true" || val == "1" || val == "yes")
	}
}

func bindEnvStringSlice(v *viper.Viper, key, envKey string) {
	if val := os.Getenv("CONTROLLER_" + envKey); val != "" {
		v.Set(key, strings.Split(val, ","))
	}
}

func validate(cfg *Config) error {
	// Required fields
	if cfg.Pool.ID == "" {
		return fmt.Errorf("pool.id is required (CONTROLLER_POOL_ID)")
	}
	if cfg.GCP.ProjectID == "" {
		return fmt.Errorf("gcp.project_id is required (CONTROLLER_GCP_PROJECT_ID)")
	}
	if cfg.GCP.Zone == "" {
		return fmt.Errorf("gcp.zone is required (CONTROLLER_GCP_ZONE)")
	}
	if cfg.GCP.MIGName == "" {
		return fmt.Errorf("gcp.mig_name is required (CONTROLLER_GCP_MIG_NAME)")
	}
	if cfg.GitHubApp.AppID == 0 {
		return fmt.Errorf("github_app.app_id is required (CONTROLLER_GITHUB_APP_ID)")
	}
	if cfg.GitHubApp.PrivateKeyPath == "" && cfg.GitHubApp.PrivateKey == "" {
		return fmt.Errorf("github_app.private_key_path or github_app.private_key is required")
	}
	if cfg.Redis.Jobs.Host == "" {
		return fmt.Errorf("redis.jobs.host is required (CONTROLLER_REDIS_JOBS_HOST)")
	}
	if cfg.Redis.VMStatus.Host == "" {
		return fmt.Errorf("redis.vm_status.host is required (CONTROLLER_REDIS_VM_HOST)")
	}
	if cfg.PubSub.ProjectID == "" {
		return fmt.Errorf("pubsub.project_id is required (CONTROLLER_PUBSUB_PROJECT_ID)")
	}
	if cfg.PubSub.Subscription == "" {
		return fmt.Errorf("pubsub.subscription is required (CONTROLLER_PUBSUB_SUBSCRIPTION)")
	}

	// Validate pool type
	validTypes := map[string]bool{"2vcpu": true, "4vcpu": true, "8vcpu": true, "16vcpu": true, "custom": true}
	if cfg.Pool.Type != "" && !validTypes[cfg.Pool.Type] {
		return fmt.Errorf("invalid pool.type: %s (valid: 2vcpu, 4vcpu, 8vcpu, 16vcpu, custom)", cfg.Pool.Type)
	}

	// Validate VM limits
	if cfg.VMManager.MinReadyVMs < 0 {
		return fmt.Errorf("vm_manager.min_ready_vms must be >= 0")
	}
	if cfg.VMManager.MaxVMs < cfg.VMManager.MinReadyVMs {
		return fmt.Errorf("vm_manager.max_vms must be >= min_ready_vms")
	}

	return nil
}

// GetRedisJobsAddr returns the Redis jobs address
func (c *Config) GetRedisJobsAddr() string {
	return fmt.Sprintf("%s:%d", c.Redis.Jobs.Host, c.Redis.Jobs.Port)
}

// GetRedisVMStatusAddr returns the Redis VM status address
func (c *Config) GetRedisVMStatusAddr() string {
	return fmt.Sprintf("%s:%d", c.Redis.VMStatus.Host, c.Redis.VMStatus.Port)
}

// GetPoolLabels returns the pool labels with defaults
func (c *Config) GetPoolLabels() []string {
	labels := c.Pool.Labels
	if len(labels) == 0 {
		labels = []string{"self-hosted"}
	}
	// Add pool-specific labels
	if c.Pool.OS != "" {
		labels = append(labels, c.Pool.OS)
	}
	if c.Pool.Arch != "" {
		labels = append(labels, c.Pool.Arch)
	}
	if c.Pool.Type != "" {
		labels = append(labels, c.Pool.Type)
	}
	return labels
}

// IsProduction returns true if running in production mode
func (c *Config) IsProduction() bool {
	return c.Logging.Level != "debug" && c.Logging.Format == "json"
}

// String returns a sanitized string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Pool: %s, Type: %s, GCP: %s/%s/%s, GitHub App: %d}",
		c.Pool.ID, c.Pool.Type, c.GCP.ProjectID, c.GCP.Zone, c.GCP.MIGName, c.GitHubApp.AppID)
}
