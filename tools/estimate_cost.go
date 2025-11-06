package tools

import (
	"context"
	"fmt"
	"time"

	"scalemind/observability"
	"scalemind/providers"

	"go.uber.org/zap"
)

// EstimateCost calculates cost estimation for a given configuration
func EstimateCost(ctx context.Context, providerType providers.ProviderType, provider providers.Provider, instanceType string, instanceCount int, durationHours int, region string) (string, error) {
	startTime := time.Now()
	traceID := observability.TraceIDFromContext(ctx)
	logger := observability.LoggerWithTraceID(traceID)

	defer func() {
		duration := time.Since(startTime)
		observability.RequestDuration.WithLabelValues("estimate_cost", string(providerType), "success").Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues("estimate_cost", string(providerType), "success").Inc()
	}()

	logger.Info("Estimating cost",
		zap.String("provider", string(providerType)),
		zap.String("instance_type", instanceType),
		zap.Int("instance_count", instanceCount),
		zap.Int("duration_hours", durationHours),
		zap.String("region", region),
	)

	estimate, err := provider.EstimateCost(ctx, instanceType, instanceCount, durationHours, region)
	if err != nil {
		observability.RequestTotal.WithLabelValues("estimate_cost", string(providerType), "error").Inc()
		observability.ErrorRate.WithLabelValues("estimate_cost", string(providerType), "provider_error").Inc()
		observability.CaptureError(err, ctx)
		return "", fmt.Errorf("failed to estimate cost: %w", err)
	}

	response := formatCostEstimate(estimate, instanceType, instanceCount, durationHours, providerType)

	logger.Info("Cost estimation completed",
		zap.Float64("hourly_cost", estimate.HourlyCost),
		zap.Float64("monthly_cost", estimate.MonthlyCost),
	)

	return response, nil
}

func formatCostEstimate(estimate *providers.CostEstimate, instanceType string, instanceCount int, durationHours int, providerType providers.ProviderType) string {
	header := fmt.Sprintf(`Cost Estimate:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Instance Type: %s
Count: %d
Duration: %d hours`,
		instanceType,
		instanceCount,
		durationHours,
	)

	costs := fmt.Sprintf(`
Hourly Cost: $%.4f
Daily Cost: $%.2f
Monthly Cost (sustained): $%.2f`,
		estimate.HourlyCost,
		estimate.DailyCost,
		estimate.MonthlyCost,
	)

	breakdown := ""
	if len(estimate.Breakdown) > 0 {
		breakdown = "\n\nCost Breakdown:"
		for component, cost := range estimate.Breakdown {
			breakdown += fmt.Sprintf("\n  %s: $%.4f/hour", component, cost)
		}
	}

	tips := ""
	if providerType == providers.ProviderAWS {
		tips = "\n\n💡 Tip: Spot instances could save up to 70% on compute costs"
	}

	return header + costs + breakdown + tips
}

