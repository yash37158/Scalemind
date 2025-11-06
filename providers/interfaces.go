package providers

import (
	"context"
	"time"
)

// ResourceMetrics represents resource utilization metrics
type ResourceMetrics struct {
	CPUPercent    float64
	MemoryPercent float64
	NetworkMbps   float64
	DiskIOPS      float64
	HealthStatus  string // 🟢, 🟡, or 🔴
	Timestamp     time.Time
}

// ScalingResult represents the result of a scaling operation
type ScalingResult struct {
	Success        bool
	Message        string
	PreviousCount  int
	CurrentCount   int
	EstimatedTime  time.Duration
	CostDelta      float64
	DryRun         bool
}

// CostEstimate represents cost calculation results
type CostEstimate struct {
	HourlyCost  float64
	DailyCost   float64
	MonthlyCost float64
	Breakdown   map[string]float64
	Currency    string
}

// Provider defines the interface for cloud providers
type Provider interface {
	// AnalyzeResourceUsage fetches metrics for a specific resource
	AnalyzeResourceUsage(ctx context.Context, resourceID string, region string) (*ResourceMetrics, error)

	// ScaleHorizontal scales resources horizontally (add/remove instances)
	ScaleHorizontal(ctx context.Context, serviceName string, desiredCount int, maxCount int, dryRun bool) (*ScalingResult, error)

	// ScaleVertical changes instance type (upgrade/downgrade)
	ScaleVertical(ctx context.Context, serviceName string, newInstanceType string, dryRun bool) (*ScalingResult, error)

	// EstimateCost calculates costs for a given configuration
	EstimateCost(ctx context.Context, instanceType string, instanceCount int, durationHours int, region string) (*CostEstimate, error)

	// GetCurrentInstanceCount returns the current number of instances for a service
	GetCurrentInstanceCount(ctx context.Context, serviceName string) (int, error)

	// GetInstanceType returns the current instance type for a service
	GetInstanceType(ctx context.Context, serviceName string) (string, error)

	// HealthCheck verifies provider connectivity
	HealthCheck(ctx context.Context) error
}

// ProviderType represents the type of cloud provider
type ProviderType string

const (
	ProviderAWS        ProviderType = "aws"
	ProviderKubernetes ProviderType = "kubernetes"
	ProviderGCP        ProviderType = "gcp"
)

// NewProvider creates a new provider instance based on type
func NewProvider(providerType ProviderType, config interface{}) (Provider, error) {
	switch providerType {
	case ProviderAWS:
		return NewAWSProvider(config)
	case ProviderKubernetes:
		return NewKubernetesProvider(config)
	case ProviderGCP:
		// TODO: Implement in Phase 2
		return nil, ErrProviderNotImplemented
	default:
		return nil, ErrInvalidProvider
	}
}

// Common errors
var (
	ErrInvalidProvider        = &ProviderError{Message: "invalid provider type"}
	ErrProviderNotImplemented = &ProviderError{Message: "provider not yet implemented"}
	ErrResourceNotFound       = &ProviderError{Message: "resource not found"}
	ErrPermissionDenied       = &ProviderError{Message: "permission denied"}
	ErrScalingFailed          = &ProviderError{Message: "scaling operation failed"}
)

// ProviderError represents a provider-specific error
type ProviderError struct {
	Message string
	Code    string
}

func (e *ProviderError) Error() string {
	return e.Message
}

