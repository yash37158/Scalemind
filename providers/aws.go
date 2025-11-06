package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"go.uber.org/zap"
)

// AWSProvider implements the Provider interface for AWS
type AWSProvider struct {
	cfg               aws.Config
	ec2Client         *ec2.Client
	autoscalingClient *autoscaling.Client
	cloudwatchClient  *cloudwatch.Client
	pricingClient     *pricing.Client
	region            string
}

// AWSConfig holds AWS provider configuration
type AWSConfig struct {
	Region string
}

// NewAWSProvider creates a new AWS provider instance
func NewAWSProvider(config interface{}) (Provider, error) {
	awsConfig, ok := config.(AWSConfig)
	if !ok {
		if cfgMap, ok := config.(map[string]interface{}); ok {
			awsConfig = AWSConfig{
				Region: getStringFromMap(cfgMap, "region", "us-east-1"),
			}
		} else {
			awsConfig = AWSConfig{Region: "us-east-1"}
		}
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(awsConfig.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Pricing API uses us-east-1 or ap-south-1 region
	pricingRegion := "us-east-1"
	if awsConfig.Region == "ap-south-1" {
		pricingRegion = "ap-south-1"
	}
	
	// Create separate config for pricing API
	pricingCfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(pricingRegion),
	)
	if err != nil {
		zap.L().Warn("Failed to create pricing config, will use default region", zap.Error(err))
		pricingCfg = cfg
	}

	return &AWSProvider{
		cfg:               cfg,
		ec2Client:         ec2.NewFromConfig(cfg),
		autoscalingClient: autoscaling.NewFromConfig(cfg),
		cloudwatchClient:  cloudwatch.NewFromConfig(cfg),
		pricingClient:     pricing.NewFromConfig(pricingCfg),
		region:            awsConfig.Region,
	}, nil
}

func getStringFromMap(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return defaultValue
}

// AnalyzeResourceUsage fetches metrics for an EC2 instance
func (p *AWSProvider) AnalyzeResourceUsage(ctx context.Context, resourceID string, region string) (*ResourceMetrics, error) {
	if region == "" {
		region = p.region
	}

	// Get instance details
	describeOutput, err := p.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{resourceID},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance: %w", err)
	}

	if len(describeOutput.Reservations) == 0 || len(describeOutput.Reservations[0].Instances) == 0 {
		return nil, ErrResourceNotFound
	}

	_ = describeOutput.Reservations[0].Instances[0] // instance variable for future use
	endTime := time.Now()
	startTime := endTime.Add(-5 * time.Minute)

	// Fetch CloudWatch metrics
	cpuMetric, err := p.getCloudWatchMetric(ctx, "AWS/EC2", "CPUUtilization", resourceID, startTime, endTime)
	if err != nil {
		zap.L().Warn("failed to get CPU metric", zap.Error(err))
		cpuMetric = 0
	}

	memoryMetric, err := p.getCloudWatchMetric(ctx, "AWS/EC2", "NetworkIn", resourceID, startTime, endTime)
	if err != nil {
		zap.L().Warn("failed to get network metric", zap.Error(err))
		memoryMetric = 0
	}

	// Convert bytes to Mbps
	networkMbps := memoryMetric / (1024 * 1024) * 8

	diskMetric, err := p.getCloudWatchMetric(ctx, "AWS/EC2", "DiskReadOps", resourceID, startTime, endTime)
	if err != nil {
		zap.L().Warn("failed to get disk metric", zap.Error(err))
		diskMetric = 0
	}

	// Determine health status
	healthStatus := determineHealthStatus(cpuMetric, 0) // Memory not directly available from EC2

	// For memory, we'll use a heuristic based on instance type
	// In production, you'd query CloudWatch agent metrics
	memoryPercent := 0.0

	return &ResourceMetrics{
		CPUPercent:    cpuMetric,
		MemoryPercent: memoryPercent,
		NetworkMbps:   networkMbps,
		DiskIOPS:      diskMetric,
		HealthStatus:  healthStatus,
		Timestamp:     time.Now(),
	}, nil
}

func (p *AWSProvider) getCloudWatchMetric(ctx context.Context, namespace, metricName, instanceID string, startTime, endTime time.Time) (float64, error) {
	input := &cloudwatch.GetMetricStatisticsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(metricName),
		Dimensions: []cloudwatchtypes.Dimension{
			{
				Name:  aws.String("InstanceId"),
				Value: aws.String(instanceID),
			},
		},
		StartTime:  aws.Time(startTime),
		EndTime:    aws.Time(endTime),
		Period:     aws.Int32(300), // 5 minutes
		Statistics: []cloudwatchtypes.Statistic{cloudwatchtypes.StatisticAverage},
	}

	result, err := p.cloudwatchClient.GetMetricStatistics(ctx, input)
	if err != nil {
		return 0, err
	}

	if len(result.Datapoints) == 0 {
		return 0, fmt.Errorf("no datapoints found")
	}

	// Return the most recent datapoint
	latest := result.Datapoints[0]
	for _, dp := range result.Datapoints {
		if dp.Timestamp.After(*latest.Timestamp) {
			latest = dp
		}
	}

	if latest.Average != nil {
		return *latest.Average, nil
	}

	return 0, fmt.Errorf("no average value in datapoint")
}

func determineHealthStatus(cpuPercent, memoryPercent float64) string {
	if cpuPercent >= 90 || memoryPercent >= 90 {
		return "🔴"
	}
	if cpuPercent >= 70 || memoryPercent >= 70 {
		return "🟡"
	}
	return "🟢"
}

// ScaleHorizontal scales an Auto Scaling Group
func (p *AWSProvider) ScaleHorizontal(ctx context.Context, serviceName string, desiredCount int, maxCount int, dryRun bool) (*ScalingResult, error) {
	// Get current ASG state
	describeOutput, err := p.autoscalingClient.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{serviceName},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe ASG: %w", err)
	}

	if len(describeOutput.AutoScalingGroups) == 0 {
		return nil, ErrResourceNotFound
	}

	asg := describeOutput.AutoScalingGroups[0]
	currentCount := int(*asg.DesiredCapacity)

	// Validate max count
	if maxCount > 0 && desiredCount > maxCount {
		return nil, fmt.Errorf("desired count %d exceeds max count %d", desiredCount, maxCount)
	}

	if dryRun {
		return &ScalingResult{
			Success:       true,
			Message:       fmt.Sprintf("Dry run: would scale from %d to %d instances", currentCount, desiredCount),
			PreviousCount: currentCount,
			CurrentCount:  desiredCount,
			EstimatedTime: 2 * time.Minute,
			DryRun:        true,
		}, nil
	}

	// Update desired capacity
	_, err = p.autoscalingClient.SetDesiredCapacity(ctx, &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(serviceName),
		DesiredCapacity:       aws.Int32(int32(desiredCount)),
		HonorCooldown:         aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set desired capacity: %w", err)
	}

	return &ScalingResult{
		Success:       true,
		Message:       fmt.Sprintf("Successfully scaled from %d to %d instances", currentCount, desiredCount),
		PreviousCount: currentCount,
		CurrentCount:  desiredCount,
		EstimatedTime: 2 * time.Minute,
		DryRun:        false,
	}, nil
}

// ScaleVertical changes instance type (requires ASG launch template update)
func (p *AWSProvider) ScaleVertical(ctx context.Context, serviceName string, newInstanceType string, dryRun bool) (*ScalingResult, error) {
	// This is a complex operation that requires updating launch templates
	// For MVP, we'll return a placeholder
	if dryRun {
		return &ScalingResult{
			Success: true,
			Message: fmt.Sprintf("Dry run: would change instance type to %s (requires launch template update)", newInstanceType),
			DryRun:   true,
		}, nil
	}

	return nil, fmt.Errorf("vertical scaling not fully implemented in MVP")
}

// EstimateCost calculates costs using AWS Pricing API
func (p *AWSProvider) EstimateCost(ctx context.Context, instanceType string, instanceCount int, durationHours int, region string) (*CostEstimate, error) {
	if region == "" {
		region = p.region
	}

	// Try to get real pricing from AWS Pricing API
	hourlyRate, err := p.getPricingFromAPI(ctx, instanceType, region)
	if err != nil {
		zap.L().Warn("Failed to fetch pricing from API, using fallback", 
			zap.String("instance_type", instanceType),
			zap.Error(err))
		
		// Fallback to static pricing map
		hourlyRate = p.getStaticPricing(instanceType)
	}

	hourlyCost := hourlyRate * float64(instanceCount)
	dailyCost := hourlyCost * 24
	monthlyCost := hourlyCost * 730

	return &CostEstimate{
		HourlyCost:  hourlyCost,
		DailyCost:   dailyCost,
		MonthlyCost: monthlyCost,
		Breakdown: map[string]float64{
			"compute": hourlyCost,
		},
		Currency: "USD",
	}, nil
}

// getPricingFromAPI fetches real pricing from AWS Pricing API
func (p *AWSProvider) getPricingFromAPI(ctx context.Context, instanceType string, region string) (float64, error) {
	// AWS Pricing API requires specific service code and filters
	serviceCode := "AmazonEC2"
	
	// Build filter for on-demand Linux instance pricing
	filters := []pricingtypes.Filter{
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("ServiceCode"),
			Value: aws.String(serviceCode),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("location"),
			Value: aws.String(p.getPricingLocation(region)),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("instanceType"),
			Value: aws.String(instanceType),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("tenancy"),
			Value: aws.String("Shared"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("operatingSystem"),
			Value: aws.String("Linux"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("preInstalledSw"),
			Value: aws.String("NA"),
		},
		{
			Type:  pricingtypes.FilterTypeTermMatch,
			Field: aws.String("capacitystatus"),
			Value: aws.String("Used"),
		},
	}

	input := &pricing.GetProductsInput{
		ServiceCode: aws.String(serviceCode),
		Filters:     filters,
		MaxResults:  aws.Int32(1),
	}

	result, err := p.pricingClient.GetProducts(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("failed to get pricing from API: %w", err)
	}

	if len(result.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing found for instance type %s in region %s", instanceType, region)
	}

	// Parse the JSON price list (AWS Pricing API returns JSON string)
	// Extract the on-demand price
	priceListJSON := result.PriceList[0]
	
	// Unmarshal the JSON string to map
	var priceList map[string]interface{}
	if err := json.Unmarshal([]byte(priceListJSON), &priceList); err != nil {
		return 0, fmt.Errorf("failed to unmarshal price list JSON: %w", err)
	}
	
	hourlyPrice, err := p.parsePriceFromJSON(priceList)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %w", err)
	}

	return hourlyPrice, nil
}

// parsePriceFromJSON extracts hourly price from AWS Pricing API JSON response
// AWS Pricing API returns complex nested JSON, this is a simplified parser
func (p *AWSProvider) parsePriceFromJSON(priceListJSON map[string]interface{}) (float64, error) {
	// Navigate through the nested JSON structure
	terms, ok := priceListJSON["terms"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("terms not found in price list")
	}

	onDemand, ok := terms["OnDemand"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("OnDemand terms not found")
	}

	// Get first (and typically only) OnDemand term
	for _, termData := range onDemand {
		term, ok := termData.(map[string]interface{})
		if !ok {
			continue
		}

		priceDimensions, ok := term["priceDimensions"].(map[string]interface{})
		if !ok {
			continue
		}

		// Get first price dimension
		for _, priceDim := range priceDimensions {
			dim, ok := priceDim.(map[string]interface{})
			if !ok {
				continue
			}

			pricePerUnit, ok := dim["pricePerUnit"].(map[string]interface{})
			if !ok {
				continue
			}

			usdPrice, ok := pricePerUnit["USD"].(string)
			if !ok {
				continue
			}

			var price float64
			if _, err := fmt.Sscanf(usdPrice, "%f", &price); err != nil {
				return 0, fmt.Errorf("failed to parse price string: %w", err)
			}

			return price, nil
		}
	}

	return 0, fmt.Errorf("could not extract price from JSON")
}

// getPricingLocation converts AWS region to Pricing API location format
func (p *AWSProvider) getPricingLocation(region string) string {
	locationMap := map[string]string{
		"us-east-1":      "US East (N. Virginia)",
		"us-east-2":      "US East (Ohio)",
		"us-west-1":      "US West (N. California)",
		"us-west-2":      "US West (Oregon)",
		"eu-west-1":      "Europe (Ireland)",
		"eu-central-1":   "Europe (Frankfurt)",
		"ap-southeast-1": "Asia Pacific (Singapore)",
		"ap-south-1":     "Asia Pacific (Mumbai)",
		"ap-northeast-1": "Asia Pacific (Tokyo)",
	}

	if location, ok := locationMap[region]; ok {
		return location
	}

	// Default fallback
	return "US East (N. Virginia)"
}

// getStaticPricing returns static pricing as fallback
func (p *AWSProvider) getStaticPricing(instanceType string) float64 {
	pricingMap := map[string]float64{
		"t3.micro":   0.0104,
		"t3.small":   0.0208,
		"t3.medium":  0.0416,
		"t3.large":   0.0832,
		"t3.xlarge":  0.1664,
		"m5.large":   0.096,
		"m5.xlarge":  0.192,
		"m5.2xlarge": 0.384,
		"c5.large":   0.085,
		"c5.xlarge":  0.17,
	}

	if rate, ok := pricingMap[instanceType]; ok {
		return rate
	}

	// Default fallback
	zap.L().Warn("instance type not in pricing map, using default", zap.String("instance_type", instanceType))
	return 0.05
}

// GetCurrentInstanceCount returns current ASG desired capacity
func (p *AWSProvider) GetCurrentInstanceCount(ctx context.Context, serviceName string) (int, error) {
	describeOutput, err := p.autoscalingClient.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{serviceName},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to describe ASG: %w", err)
	}

	if len(describeOutput.AutoScalingGroups) == 0 {
		return 0, ErrResourceNotFound
	}

	return int(*describeOutput.AutoScalingGroups[0].DesiredCapacity), nil
}

// GetInstanceType returns instance type from ASG launch template
func (p *AWSProvider) GetInstanceType(ctx context.Context, serviceName string) (string, error) {
	describeOutput, err := p.autoscalingClient.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{serviceName},
	})
	if err != nil {
		return "", fmt.Errorf("failed to describe ASG: %w", err)
	}

	if len(describeOutput.AutoScalingGroups) == 0 {
		return "", ErrResourceNotFound
	}

	asg := describeOutput.AutoScalingGroups[0]
	if asg.MixedInstancesPolicy != nil && asg.MixedInstancesPolicy.LaunchTemplate != nil {
		// Handle mixed instances policy
		return "mixed", nil
	}

	if asg.LaunchTemplate != nil {
		// Get launch template details
		ltOutput, err := p.ec2Client.DescribeLaunchTemplateVersions(ctx, &ec2.DescribeLaunchTemplateVersionsInput{
			LaunchTemplateId: asg.LaunchTemplate.LaunchTemplateId,
		})
		if err == nil && len(ltOutput.LaunchTemplateVersions) > 0 {
			if lt := ltOutput.LaunchTemplateVersions[0].LaunchTemplateData; lt != nil && lt.InstanceType != "" {
				return string(lt.InstanceType), nil
			}
		}
	}

	return "unknown", nil
}

// HealthCheck verifies AWS connectivity
func (p *AWSProvider) HealthCheck(ctx context.Context) error {
	_, err := p.ec2Client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return fmt.Errorf("AWS health check failed: %w", err)
	}
	return nil
}

