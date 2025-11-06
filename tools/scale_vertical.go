package tools

import (
	"context"
	"fmt"
	"time"

	"scalemind/observability"
	"scalemind/providers"

	"go.uber.org/zap"
)

// ScaleVertical scales resources vertically (upgrade/downgrade instance type)
func ScaleVertical(ctx context.Context, providerType providers.ProviderType, provider providers.Provider, serviceName string, newInstanceType string, dryRun bool) (string, error) {
	startTime := time.Now()
	traceID := observability.TraceIDFromContext(ctx)
	logger := observability.LoggerWithTraceID(traceID)

	defer func() {
		duration := time.Since(startTime)
		status := "success"
		if dryRun {
			status = "dry_run"
		}
		observability.RequestDuration.WithLabelValues("scale_vertical", string(providerType), status).Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues("scale_vertical", string(providerType), status).Inc()
	}()

	logger.Info("Scaling vertically",
		zap.String("provider", string(providerType)),
		zap.String("service_name", serviceName),
		zap.String("new_instance_type", newInstanceType),
		zap.Bool("dry_run", dryRun),
	)

	result, err := provider.ScaleVertical(ctx, serviceName, newInstanceType, dryRun)
	if err != nil {
		observability.RequestTotal.WithLabelValues("scale_vertical", string(providerType), "error").Inc()
		observability.ErrorRate.WithLabelValues("scale_vertical", string(providerType), "provider_error").Inc()
		observability.CaptureError(err, ctx)
		return "", fmt.Errorf("failed to scale vertically: %w", err)
	}

	if !result.DryRun {
		observability.ScalingOperations.WithLabelValues(string(providerType), "vertical", "success").Inc()
	}

	response := formatVerticalScalingResult(result, serviceName, newInstanceType)

	logger.Info("Vertical scaling completed",
		zap.String("new_instance_type", newInstanceType),
		zap.Bool("dry_run", result.DryRun),
	)

	return response, nil
}

func formatVerticalScalingResult(result *providers.ScalingResult, serviceName string, newInstanceType string) string {
	if result.DryRun {
		return fmt.Sprintf(`Vertical Scaling (DRY RUN) for %s:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Status: %s
New Instance Type: %s
Estimated Time: %s

⚠️  This was a dry run. No changes were made.
⚠️  Note: Vertical scaling may cause brief downtime during instance replacement.`,
			serviceName,
			result.Message,
			newInstanceType,
			result.EstimatedTime,
		)
	}

	costInfo := ""
	if result.CostDelta != 0 {
		sign := "+"
		if result.CostDelta < 0 {
			sign = ""
		}
		costInfo = fmt.Sprintf("\nCost Impact: %s$%.2f/month", sign, result.CostDelta)
	}

	return fmt.Sprintf(`Vertical Scaling for %s:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Status: ✅ %s
New Instance Type: %s
Estimated Time to Complete: %s%s

⚠️  Warning: Vertical scaling may cause brief downtime during instance replacement.`,
		serviceName,
		result.Message,
		newInstanceType,
		result.EstimatedTime,
		costInfo,
	)
}

