package tools

import (
	"context"
	"fmt"
	"time"

	"scalemind/observability"
	"scalemind/providers"

	"go.uber.org/zap"
)

// ScalingStrategy represents a recommended scaling strategy
type ScalingStrategy struct {
	Action              string
	Reasoning           string
	CostImpact          float64
	Priority            string
	ConfidenceScore     float64
	Alternatives        []string
}

// RecommendScalingStrategy recommends a scaling strategy based on current conditions
func RecommendScalingStrategy(ctx context.Context, providerType providers.ProviderType, provider providers.Provider, currentLoadPercent float64, budgetLimitUSD float64, currentInstanceCount int, workloadType string, timeHorizon string) (string, error) {
	startTime := time.Now()
	traceID := observability.TraceIDFromContext(ctx)
	logger := observability.LoggerWithTraceID(traceID)

	defer func() {
		duration := time.Since(startTime)
		observability.RequestDuration.WithLabelValues("recommend_scaling_strategy", string(providerType), "success").Observe(duration.Seconds())
		observability.RequestTotal.WithLabelValues("recommend_scaling_strategy", string(providerType), "success").Inc()
	}()

	logger.Info("Generating scaling recommendation",
		zap.String("provider", string(providerType)),
		zap.Float64("current_load_percent", currentLoadPercent),
		zap.Float64("budget_limit_usd", budgetLimitUSD),
		zap.Int("current_instance_count", currentInstanceCount),
		zap.String("workload_type", workloadType),
		zap.String("time_horizon", timeHorizon),
	)

	// Get current instance type for cost calculation
	currentInstanceType, err := provider.GetInstanceType(ctx, "default")
	if err != nil {
		// Use a default if we can't determine
		currentInstanceType = "t3.medium"
	}

	// Calculate current cost
	currentCostEstimate, err := provider.EstimateCost(ctx, currentInstanceType, currentInstanceCount, 730, "")
	if err != nil {
		logger.Warn("Failed to estimate current cost", zap.Error(err))
		currentCostEstimate = &providers.CostEstimate{MonthlyCost: 100} // Default estimate
	}

	strategy := calculateStrategy(currentLoadPercent, budgetLimitUSD, currentInstanceCount, workloadType, timeHorizon, currentCostEstimate.MonthlyCost)

	response := formatRecommendation(strategy, currentLoadPercent, budgetLimitUSD, currentCostEstimate.MonthlyCost)

	logger.Info("Scaling recommendation generated",
		zap.String("action", strategy.Action),
		zap.String("priority", strategy.Priority),
		zap.Float64("confidence_score", strategy.ConfidenceScore),
	)

	return response, nil
}

func calculateStrategy(currentLoadPercent, budgetLimitUSD float64, currentInstanceCount int, workloadType, timeHorizon string, currentMonthlyCost float64) ScalingStrategy {
	var action string
	var reasoning string
	var costImpact float64
	var priority string
	var confidenceScore float64
	var alternatives []string

	// Determine action based on load
	if currentLoadPercent >= 90 {
		action = "scale_horizontal"
		reasoning = "Critical load detected. Immediate horizontal scaling recommended for redundancy and performance."
		priority = "critical"
		confidenceScore = 0.95
		costImpact = currentMonthlyCost * 0.5 // Assume 50% increase
		alternatives = []string{"scale_vertical", "scale_horizontal + optimize"}
	} else if currentLoadPercent >= 70 {
		action = "scale_horizontal"
		reasoning = "High load detected. Horizontal scaling provides better redundancy and fault tolerance."
		priority = "high"
		confidenceScore = 0.85
		costImpact = currentMonthlyCost * 0.5
		alternatives = []string{"scale_vertical", "scale_down (if temporary spike)"}
	} else if currentLoadPercent >= 50 {
		action = "no_action"
		reasoning = "Moderate load. Monitor closely. Consider scaling if trend continues upward."
		priority = "medium"
		confidenceScore = 0.70
		costImpact = 0
		alternatives = []string{"scale_horizontal (if budget allows)", "optimize current resources"}
	} else if currentLoadPercent <= 30 && currentInstanceCount > 1 {
		action = "scale_down"
		reasoning = "Low load detected. Scaling down can reduce costs while maintaining adequate capacity."
		priority = "low"
		confidenceScore = 0.80
		costImpact = -currentMonthlyCost * 0.3 // 30% cost savings
		alternatives = []string{"no_action (keep current for redundancy)", "scale_down gradually"}
	} else {
		action = "no_action"
		reasoning = "Load is within acceptable range. Current configuration is optimal."
		priority = "low"
		confidenceScore = 0.90
		costImpact = 0
		alternatives = []string{"monitor for changes", "optimize if needed"}
	}

	// Check budget constraints
	if action != "no_action" && action != "scale_down" {
		projectedCost := currentMonthlyCost + costImpact
		if projectedCost > budgetLimitUSD {
			// Adjust recommendation to stay within budget
			if currentLoadPercent >= 90 {
				// Critical - recommend vertical scaling instead (cheaper)
				action = "scale_vertical"
				reasoning = "Budget constraint detected. Vertical scaling recommended over horizontal to stay within budget."
				costImpact = currentMonthlyCost * 0.2 // 20% increase
			} else {
				// High but not critical - recommend optimization
				action = "no_action"
				reasoning = "Budget constraint detected. Recommend optimizing current resources before scaling."
				costImpact = 0
				alternatives = append(alternatives, "scale_vertical (if optimization insufficient)")
			}
		}
	}

	// Adjust for workload type
	if workloadType == "cpu_intensive" && action == "scale_horizontal" {
		reasoning += " Horizontal scaling is ideal for CPU-intensive workloads."
	} else if workloadType == "memory_intensive" && action == "scale_vertical" {
		reasoning += " Vertical scaling may be more cost-effective for memory-intensive workloads."
	}

	return ScalingStrategy{
		Action:          action,
		Reasoning:       reasoning,
		CostImpact:      costImpact,
		Priority:        priority,
		ConfidenceScore:  confidenceScore,
		Alternatives:    alternatives,
	}
}

func formatRecommendation(strategy ScalingStrategy, currentLoadPercent float64, budgetLimitUSD float64, currentMonthlyCost float64) string {
	projectedCost := currentMonthlyCost + strategy.CostImpact

	header := fmt.Sprintf(`Scaling Strategy Recommendation:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Current Load: %.1f%%
Current Monthly Cost: $%.2f
Budget Limit: $%.2f`,
		currentLoadPercent,
		currentMonthlyCost,
		budgetLimitUSD,
	)

	recommendation := fmt.Sprintf(`
Recommended Action: %s
Priority: %s
Confidence: %.0f%%

Reasoning: %s`,
		strategy.Action,
		strategy.Priority,
		strategy.ConfidenceScore*100,
		strategy.Reasoning,
	)

	costInfo := ""
	if strategy.CostImpact != 0 {
		costInfo = fmt.Sprintf(`
Cost Impact: $%.2f/month
Projected Monthly Cost: $%.2f`,
			strategy.CostImpact,
			projectedCost,
		)
	} else {
		costInfo = "\nCost Impact: No change"
	}

	alternatives := ""
	if len(strategy.Alternatives) > 0 {
		alternatives = "\n\nAlternative Options:"
		for i, alt := range strategy.Alternatives {
			alternatives += fmt.Sprintf("\n  %d. %s", i+1, alt)
		}
	}

	return header + recommendation + costInfo + alternatives
}

