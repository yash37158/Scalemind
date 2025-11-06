package tools

import (
	"context"
	"fmt"
	"time"

	"scalemind/observability"
	"scalemind/providers"

	"go.uber.org/zap"
)

// AnalyzeResourceUsage analyzes resource usage for a given resource
func AnalyzeResourceUsage(ctx context.Context, providerType providers.ProviderType, provider providers.Provider, resourceID string, region string) (string, error) {
	startTime := time.Now()
	traceID := observability.TraceIDFromContext(ctx)
	logger := observability.LoggerWithTraceID(traceID)

	defer func() {
		duration := time.Since(startTime)
		observability.RequestDuration.WithLabelValues("analyze_resource_usage", string(providerType), "success").Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues("analyze_resource_usage", string(providerType), "success").Inc()
	}()

	logger.Info("Analyzing resource usage",
		zap.String("provider", string(providerType)),
		zap.String("resource_id", resourceID),
		zap.String("region", region),
	)

	metrics, err := provider.AnalyzeResourceUsage(ctx, resourceID, region)
	if err != nil {
		observability.RequestTotal.WithLabelValues("analyze_resource_usage", string(providerType), "error").Inc()
		observability.ErrorRate.WithLabelValues("analyze_resource_usage", string(providerType), "provider_error").Inc()
		observability.CaptureError(err, ctx)
		return "", fmt.Errorf("failed to analyze resource: %w", err)
	}

	// Format response
	response := formatResourceAnalysis(resourceID, metrics)

	logger.Info("Resource analysis completed",
		zap.Float64("cpu_percent", metrics.CPUPercent),
		zap.Float64("memory_percent", metrics.MemoryPercent),
		zap.String("health_status", metrics.HealthStatus),
	)

	return response, nil
}

func formatResourceAnalysis(resourceID string, metrics *providers.ResourceMetrics) string {
	recommendation := generateRecommendation(metrics)

	return fmt.Sprintf(`Resource Analysis for %s:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
CPU: %.1f%%
Memory: %.1f%%
Network: %.2f Mbps
Disk I/O: %.0f IOPS

Status: %s

Recommendation: %s
Timestamp: %s`,
		resourceID,
		metrics.CPUPercent,
		metrics.MemoryPercent,
		metrics.NetworkMbps,
		metrics.DiskIOPS,
		metrics.HealthStatus,
		recommendation,
		metrics.Timestamp.Format(time.RFC3339),
	)
}

func generateRecommendation(metrics *providers.ResourceMetrics) string {
	if metrics.CPUPercent >= 90 || metrics.MemoryPercent >= 90 {
		return "🔴 CRITICAL - Immediate scaling required"
	}
	if metrics.CPUPercent >= 70 || metrics.MemoryPercent >= 70 {
		return "🟡 HIGH - Consider scaling up soon"
	}
	if metrics.CPUPercent >= 50 || metrics.MemoryPercent >= 50 {
		return "🟢 MODERATE - Monitor closely"
	}
	return "🟢 HEALTHY - No action needed"
}

