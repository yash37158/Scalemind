package tools

import (
	"context"
	"fmt"
	"time"

	"scalemind/observability"
	"scalemind/providers"

	"go.uber.org/zap"
)

// ScaleHorizontal scales resources horizontally
func ScaleHorizontal(ctx context.Context, providerType providers.ProviderType, provider providers.Provider, serviceName string, desiredCount int, maxCount int, dryRun bool) (string, error) {
	startTime := time.Now()
	traceID := observability.TraceIDFromContext(ctx)
	logger := observability.LoggerWithTraceID(traceID)

	defer func() {
		duration := time.Since(startTime)
		status := "success"
		if dryRun {
			status = "dry_run"
		}
		observability.RequestDuration.WithLabelValues("scale_horizontal", string(providerType), status).Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues("scale_horizontal", string(providerType), status).Inc()
	}()

	logger.Info("Scaling horizontally",
		zap.String("provider", string(providerType)),
		zap.String("service_name", serviceName),
		zap.Int("desired_count", desiredCount),
		zap.Int("max_count", maxCount),
		zap.Bool("dry_run", dryRun),
	)

	result, err := provider.ScaleHorizontal(ctx, serviceName, desiredCount, maxCount, dryRun)
	if err != nil {
		observability.RequestTotal.WithLabelValues("scale_horizontal", string(providerType), "error").Inc()
		observability.ErrorRate.WithLabelValues("scale_horizontal", string(providerType), "provider_error").Inc()
		observability.CaptureError(err, ctx)
		return "", fmt.Errorf("failed to scale: %w", err)
	}

	if !result.DryRun {
		observability.ScalingOperations.WithLabelValues(string(providerType), "horizontal", "success").Inc()
	}

	response := formatScalingResult(result, serviceName)

	logger.Info("Horizontal scaling completed",
		zap.Int("previous_count", result.PreviousCount),
		zap.Int("current_count", result.CurrentCount),
		zap.Bool("dry_run", result.DryRun),
	)

	return response, nil
}

func formatScalingResult(result *providers.ScalingResult, serviceName string) string {
	if result.DryRun {
		return fmt.Sprintf(`Horizontal Scaling (DRY RUN) for %s:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Status: %s
Previous Count: %d
Desired Count: %d
Estimated Time: %s

⚠️  This was a dry run. No changes were made.`,
			serviceName,
			result.Message,
			result.PreviousCount,
			result.CurrentCount,
			result.EstimatedTime,
		)
	}

	costInfo := ""
	if result.CostDelta != 0 {
		costInfo = fmt.Sprintf("\nCost Impact: $%.2f/month", result.CostDelta)
	}

	return fmt.Sprintf(`Horizontal Scaling for %s:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Status: ✅ %s
Previous Count: %d
Current Count: %d
Estimated Time to Complete: %s%s`,
		serviceName,
		result.Message,
		result.PreviousCount,
		result.CurrentCount,
		result.EstimatedTime,
		costInfo,
	)
}

