package handlers

import (
	"context"
	"fmt"

	"scalemind/config"
	"scalemind/providers"
	"scalemind/tools"

	"go.uber.org/zap"
)

// MCPHandler handles MCP tool requests
type MCPHandler struct {
	config    *config.Config
	providers map[providers.ProviderType]providers.Provider
}

// NewMCPHandler creates a new MCP handler
// Provider initialization failures are non-fatal - server can still list tools
func NewMCPHandler(cfg *config.Config) (*MCPHandler, error) {
	handler := &MCPHandler{
		config:    cfg,
		providers: make(map[providers.ProviderType]providers.Provider),
	}

	// Initialize AWS provider (non-blocking)
	// Provider failures should not prevent MCP server from starting
	func() {
		defer func() {
			if r := recover(); r != nil {
				zap.L().Error("Panic initializing AWS provider",
					zap.Any("panic", r),
					zap.Stack("stack"))
			}
		}()
		
		awsProvider, err := providers.NewProvider(providers.ProviderAWS, providers.AWSConfig{
			Region: cfg.AWS.Region,
		})
		if err != nil {
			zap.L().Warn("Failed to initialize AWS provider (non-fatal)", zap.Error(err))
		} else {
			handler.providers[providers.ProviderAWS] = awsProvider
			zap.L().Info("AWS provider initialized successfully")
		}
	}()

	// Initialize Kubernetes provider (non-blocking)
	func() {
		defer func() {
			if r := recover(); r != nil {
				zap.L().Error("Panic initializing Kubernetes provider",
					zap.Any("panic", r),
					zap.Stack("stack"))
			}
		}()
		
		k8sProvider, err := providers.NewProvider(providers.ProviderKubernetes, providers.KubernetesConfig{
			KubeconfigPath: cfg.K8s.KubeconfigPath,
			Namespace:      "default",
		})
		if err != nil {
			zap.L().Warn("Failed to initialize Kubernetes provider (non-fatal)", zap.Error(err))
		} else {
			handler.providers[providers.ProviderKubernetes] = k8sProvider
			zap.L().Info("Kubernetes provider initialized successfully")
		}
	}()

	// Always return handler, even if providers failed
	// Tools can still be listed, but calls will fail gracefully
	return handler, nil
}

// GetProvider returns a provider by type
func (h *MCPHandler) GetProvider(providerType providers.ProviderType) (providers.Provider, error) {
	provider, ok := h.providers[providerType]
	if !ok {
		return nil, fmt.Errorf("provider %s not available", providerType)
	}
	return provider, nil
}

// HandleAnalyzeResourceUsage handles analyze_resource_usage tool
func (h *MCPHandler) HandleAnalyzeResourceUsage(ctx context.Context, providerStr, resourceID, region string) (string, error) {
	providerType := providers.ProviderType(providerStr)
	provider, err := h.GetProvider(providerType)
	if err != nil {
		return "", err
	}

	return tools.AnalyzeResourceUsage(ctx, providerType, provider, resourceID, region)
}

// HandleScaleHorizontal handles scale_horizontal tool
func (h *MCPHandler) HandleScaleHorizontal(ctx context.Context, providerStr, serviceName string, desiredCount int, maxCount int, dryRun bool) (string, error) {
	providerType := providers.ProviderType(providerStr)
	provider, err := h.GetProvider(providerType)
	if err != nil {
		return "", err
	}

	return tools.ScaleHorizontal(ctx, providerType, provider, serviceName, desiredCount, maxCount, dryRun)
}

// HandleEstimateCost handles estimate_cost tool
func (h *MCPHandler) HandleEstimateCost(ctx context.Context, providerStr, instanceType string, instanceCount int, durationHours int, region string) (string, error) {
	providerType := providers.ProviderType(providerStr)
	provider, err := h.GetProvider(providerType)
	if err != nil {
		return "", err
	}

	return tools.EstimateCost(ctx, providerType, provider, instanceType, instanceCount, durationHours, region)
}

// HandleRecommendScalingStrategy handles recommend_scaling_strategy tool
func (h *MCPHandler) HandleRecommendScalingStrategy(ctx context.Context, providerStr string, currentLoadPercent float64, budgetLimitUSD float64, currentInstanceCount int, workloadType string, timeHorizon string) (string, error) {
	providerType := providers.ProviderType(providerStr)
	provider, err := h.GetProvider(providerType)
	if err != nil {
		return "", err
	}

	return tools.RecommendScalingStrategy(ctx, providerType, provider, currentLoadPercent, budgetLimitUSD, currentInstanceCount, workloadType, timeHorizon)
}

// HandleScaleVertical handles scale_vertical tool
func (h *MCPHandler) HandleScaleVertical(ctx context.Context, providerStr, serviceName string, newInstanceType string, dryRun bool) (string, error) {
	providerType := providers.ProviderType(providerStr)
	provider, err := h.GetProvider(providerType)
	if err != nil {
		return "", err
	}

	return tools.ScaleVertical(ctx, providerType, provider, serviceName, newInstanceType, dryRun)
}

