package config

import (
	"context"
	"fmt"
	"os"

	vault "github.com/hashicorp/vault/api"
	"go.uber.org/zap"
)

// SecretsManager handles secret retrieval from Vault
type SecretsManager struct {
	client *vault.Client
	config *VaultConfig
}

// NewSecretsManager creates a new secrets manager
func NewSecretsManager(cfg *VaultConfig) (*SecretsManager, error) {
	if cfg.Addr == "" {
		// Vault not configured, return nil manager (will use env vars)
		return &SecretsManager{client: nil, config: cfg}, nil
	}

	vaultConfig := vault.DefaultConfig()
	vaultConfig.Address = cfg.Addr

	client, err := vault.NewClient(vaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	if cfg.Token != "" {
		client.SetToken(cfg.Token)
	}

	return &SecretsManager{
		client: client,
		config: cfg,
	}, nil
}

// GetSecret retrieves a secret from Vault
func (sm *SecretsManager) GetSecret(ctx context.Context, path string, key string) (string, error) {
	if sm.client == nil {
		// Fallback to environment variable
		envKey := fmt.Sprintf("SECRET_%s_%s", path, key)
		if value := os.Getenv(envKey); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("vault not configured and environment variable %s not set", envKey)
	}

	secret, err := sm.client.Logical().ReadWithContext(ctx, path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret from vault: %w", err)
	}

	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("secret not found at path: %s", path)
	}

	// Handle KV v2 engine format
	data := secret.Data
	if data["data"] != nil {
		// KV v2 returns data wrapped in "data" field
		if dataMap, ok := data["data"].(map[string]interface{}); ok {
			data = dataMap
		}
	}

	if value, ok := data[key].(string); ok {
		return value, nil
	}

	return "", fmt.Errorf("key %s not found in secret at path %s", key, path)
}

// GetAWSCredentials retrieves AWS credentials from Vault
func (sm *SecretsManager) GetAWSCredentials(ctx context.Context) (accessKeyID, secretAccessKey string, err error) {
	accessKeyID, err = sm.GetSecret(ctx, "secret/data/aws", "access_key_id")
	if err != nil {
		return "", "", fmt.Errorf("failed to get AWS access key: %w", err)
	}

	secretAccessKey, err = sm.GetSecret(ctx, "secret/data/aws", "secret_access_key")
	if err != nil {
		return "", "", fmt.Errorf("failed to get AWS secret key: %w", err)
	}

	return accessKeyID, secretAccessKey, nil
}

// GetKubernetesConfig retrieves Kubernetes config from Vault
func (sm *SecretsManager) GetKubernetesConfig(ctx context.Context) (string, error) {
	return sm.GetSecret(ctx, "secret/data/kubernetes", "kubeconfig")
}

// HealthCheck verifies Vault connectivity
func (sm *SecretsManager) HealthCheck(ctx context.Context) error {
	if sm.client == nil {
		zap.L().Info("Vault not configured, skipping health check")
		return nil
	}

	health, err := sm.client.Sys().Health()
	if err != nil {
		return fmt.Errorf("vault health check failed: %w", err)
	}

	if !health.Initialized {
		return fmt.Errorf("vault is not initialized")
	}

	if health.Sealed {
		return fmt.Errorf("vault is sealed")
	}

	return nil
}

