package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// Config holds all application configuration
type Config struct {
	Server ServerConfig
	AWS    AWSConfig
	K8s    KubernetesConfig
	Auth   AuthConfig
	Obs    ObservabilityConfig
	Vault  VaultConfig
}

// ServerConfig holds MCP server configuration
type ServerConfig struct {
	Name            string
	ProtocolVersion string
	Port            int
}

// AWSConfig holds AWS-specific configuration
type AWSConfig struct {
	Region string
}

// KubernetesConfig holds Kubernetes-specific configuration
type KubernetesConfig struct {
	KubeconfigPath string
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecretKey      string
	OAuthClientID     string
	OAuthClientSecret string
	RateLimitPerHour  int
}

// ObservabilityConfig holds observability settings
type ObservabilityConfig struct {
	SentryDSN      string
	LogLevel       string
	PrometheusPort int
	EnableTracing  bool
}

// VaultConfig holds HashiCorp Vault configuration
type VaultConfig struct {
	Addr  string
	Token string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	// Load .env file if it exists (for local development)
	_ = godotenv.Load()

	cfg := &Config{
		Server: ServerConfig{
			Name:            getEnv("MCP_SERVER_NAME", "ScaleMind"),
			ProtocolVersion: getEnv("MCP_PROTOCOL_VERSION", "1.0"),
			Port:            getEnvAsInt("SERVER_PORT", 8080),
		},
		AWS: AWSConfig{
			Region: getEnv("AWS_REGION", "us-east-1"),
		},
		K8s: KubernetesConfig{
			KubeconfigPath: getEnv("KUBECONFIG", ""),
		},
		Auth: AuthConfig{
			JWTSecretKey:      getEnv("JWT_SECRET_KEY", ""),
			OAuthClientID:     getEnv("OAUTH_CLIENT_ID", ""),
			OAuthClientSecret: getEnv("OAUTH_CLIENT_SECRET", ""),
			RateLimitPerHour:  getEnvAsInt("RATE_LIMIT_PER_HOUR", 1000),
		},
		Obs: ObservabilityConfig{
			SentryDSN:      getEnv("SENTRY_DSN", ""),
			LogLevel:       getEnv("LOG_LEVEL", "info"),
			PrometheusPort: getEnvAsInt("PROMETHEUS_PORT", 8080),
			EnableTracing:  getEnvAsBool("ENABLE_TRACING", true),
		},
		Vault: VaultConfig{
			Addr:  getEnv("VAULT_ADDR", ""),
			Token: getEnv("VAULT_TOKEN", ""),
		},
	}

	return cfg, nil
}

// Validate checks if required configuration is present
func (c *Config) Validate() error {
	// In production, secrets should come from Vault, not env vars
	// For MVP, we'll allow empty values but warn
	if c.Auth.JWTSecretKey == "" {
		zap.L().Warn("JWT_SECRET_KEY not set, authentication may not work")
	}
	if c.Vault.Addr == "" {
		zap.L().Warn("VAULT_ADDR not set, secrets management may not work")
	}
	return nil
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetRequestTimeout returns a default timeout for API requests
func GetRequestTimeout() time.Duration {
	return 30 * time.Second
}

