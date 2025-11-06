package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned"
	"go.uber.org/zap"
)

// KubernetesProvider implements the Provider interface for Kubernetes
type KubernetesProvider struct {
	clientset     *kubernetes.Clientset
	metricsClient *metricsv1beta1.Clientset
	namespace     string
}

// KubernetesConfig holds Kubernetes provider configuration
type KubernetesConfig struct {
	KubeconfigPath string
	Namespace      string
}

// NewKubernetesProvider creates a new Kubernetes provider instance
func NewKubernetesProvider(config interface{}) (Provider, error) {
	var k8sConfig KubernetesConfig
	if cfg, ok := config.(KubernetesConfig); ok {
		k8sConfig = cfg
	} else if cfgMap, ok := config.(map[string]interface{}); ok {
		k8sConfig = KubernetesConfig{
			KubeconfigPath: getStringFromMap(cfgMap, "kubeconfig_path", ""),
			Namespace:      getStringFromMap(cfgMap, "namespace", "default"),
		}
	} else {
		k8sConfig = KubernetesConfig{Namespace: "default"}
	}

	var restConfig *rest.Config
	var err error

	if k8sConfig.KubeconfigPath != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", k8sConfig.KubeconfigPath)
	} else {
		// Try in-cluster config first
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// Fallback to default kubeconfig location
			home := filepath.Join(getHomeDir(), ".kube", "config")
			restConfig, err = clientcmd.BuildConfigFromFlags("", home)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	metricsClient, err := metricsv1beta1.NewForConfig(restConfig)
	if err != nil {
		zap.L().Warn("metrics API not available, resource analysis will be limited", zap.Error(err))
		metricsClient = nil
	}

	return &KubernetesProvider{
		clientset:     clientset,
		metricsClient: metricsClient,
		namespace:     k8sConfig.Namespace,
	}, nil
}

func getHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return os.Getenv("USERPROFILE") // Windows
}

// AnalyzeResourceUsage fetches metrics for a Kubernetes deployment
func (p *KubernetesProvider) AnalyzeResourceUsage(ctx context.Context, resourceID string, region string) (*ResourceMetrics, error) {
	// Parse resource ID: could be "deployment/name" or just "name" (defaults to deployment)
	deploymentName := resourceID
	namespace := p.namespace

	// Check if namespace is specified in resource ID
	if parts := strings.Split(resourceID, "/"); len(parts) == 2 {
		namespace = parts[0]
		deploymentName = parts[1]
	}

	deployment, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	var cpuPercent, memoryPercent, networkMbps, diskIOPS float64

	// Get pod metrics if metrics API is available
	if p.metricsClient != nil {
		pods, err := p.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
		})
		if err == nil && len(pods.Items) > 0 {
			totalCPU, totalMemory := resource.MustParse("0"), resource.MustParse("0")
			requestedCPU, requestedMemory := resource.MustParse("0"), resource.MustParse("0")

			for _, pod := range pods.Items {
				podMetrics, err := p.metricsClient.MetricsV1beta1().PodMetricses(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
				if err != nil {
					continue
				}

				for _, container := range podMetrics.Containers {
					if container.Usage.Cpu() != nil {
						totalCPU.Add(*container.Usage.Cpu())
					}
					if container.Usage.Memory() != nil {
						totalMemory.Add(*container.Usage.Memory())
					}
				}

				// Get resource requests
				for _, container := range pod.Spec.Containers {
					if container.Resources.Requests != nil {
						if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
							requestedCPU.Add(cpu)
						}
						if mem, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
							requestedMemory.Add(mem)
						}
					}
				}
			}

			// Calculate percentages
			if requestedCPU.Value() > 0 {
				cpuPercent = float64(totalCPU.MilliValue()) / float64(requestedCPU.MilliValue()) * 100
			}
			if requestedMemory.Value() > 0 {
				memoryPercent = float64(totalMemory.Value()) / float64(requestedMemory.Value()) * 100
			}
		}
	}

	// Determine health status
	healthStatus := determineHealthStatus(cpuPercent, memoryPercent)

	return &ResourceMetrics{
		CPUPercent:    cpuPercent,
		MemoryPercent: memoryPercent,
		NetworkMbps:   networkMbps,
		DiskIOPS:      diskIOPS,
		HealthStatus:  healthStatus,
		Timestamp:     time.Now(),
	}, nil
}

// ScaleHorizontal scales a Kubernetes deployment
func (p *KubernetesProvider) ScaleHorizontal(ctx context.Context, serviceName string, desiredCount int, maxCount int, dryRun bool) (*ScalingResult, error) {
	namespace := p.namespace

	// Get current deployment
	deployment, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	currentCount := int(*deployment.Spec.Replicas)

	// Validate max count
	if maxCount > 0 && desiredCount > maxCount {
		return nil, fmt.Errorf("desired count %d exceeds max count %d", desiredCount, maxCount)
	}

	if dryRun {
		return &ScalingResult{
			Success:       true,
			Message:       fmt.Sprintf("Dry run: would scale from %d to %d replicas", currentCount, desiredCount),
			PreviousCount: currentCount,
			CurrentCount:  desiredCount,
			EstimatedTime: 30 * time.Second,
			DryRun:        true,
		}, nil
	}

	// Scale deployment
	replicas := int32(desiredCount)
	deployment.Spec.Replicas = &replicas

	_, err = p.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to scale deployment: %w", err)
	}

	return &ScalingResult{
		Success:       true,
		Message:       fmt.Sprintf("Successfully scaled from %d to %d replicas", currentCount, desiredCount),
		PreviousCount: currentCount,
		CurrentCount:  desiredCount,
		EstimatedTime: 30 * time.Second,
		DryRun:        false,
	}, nil
}

// ScaleVertical updates resource requests/limits (requires pod template update)
func (p *KubernetesProvider) ScaleVertical(ctx context.Context, serviceName string, newInstanceType string, dryRun bool) (*ScalingResult, error) {
	namespace := p.namespace

	deployment, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Parse instance type (e.g., "cpu=2,memory=4Gi")
	// For MVP, we'll use a simple mapping
	resourceMap := parseResourceString(newInstanceType)

	if dryRun {
		return &ScalingResult{
			Success: true,
			Message: fmt.Sprintf("Dry run: would update resource requests to %s", newInstanceType),
			DryRun:   true,
		}, nil
	}

	// Update resource requests for all containers
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}

		if cpu, ok := resourceMap["cpu"]; ok {
			container.Resources.Requests[corev1.ResourceCPU] = cpu
			container.Resources.Limits[corev1.ResourceCPU] = cpu
		}
		if memory, ok := resourceMap["memory"]; ok {
			container.Resources.Requests[corev1.ResourceMemory] = memory
			container.Resources.Limits[corev1.ResourceMemory] = memory
		}
	}

	_, err = p.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update deployment: %w", err)
	}

	return &ScalingResult{
		Success:      true,
		Message:      fmt.Sprintf("Successfully updated resource requests to %s", newInstanceType),
		EstimatedTime: 1 * time.Minute,
		DryRun:       false,
	}, nil
}

func parseResourceString(resourceStr string) map[string]resource.Quantity {
	result := make(map[string]resource.Quantity)
	parts := strings.Split(resourceStr, ",")
	for _, part := range parts {
		kv := strings.Split(strings.TrimSpace(part), "=")
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			if qty, err := resource.ParseQuantity(value); err == nil {
				result[key] = qty
			}
		}
	}
	return result
}

// EstimateCost calculates costs for Kubernetes resources (simplified)
func (p *KubernetesProvider) EstimateCost(ctx context.Context, instanceType string, instanceCount int, durationHours int, region string) (*CostEstimate, error) {
	// Kubernetes cost estimation is complex and depends on the underlying infrastructure
	// For MVP, we'll use a simplified model based on resource requests
	// In production, integrate with cloud provider pricing APIs

	// Parse resource string (e.g., "cpu=2,memory=4Gi")
	resourceMap := parseResourceString(instanceType)
	
	var cpuQty, memoryQty resource.Quantity
	if cpu, ok := resourceMap["cpu"]; ok {
		cpuQty = cpu
	}
	if mem, ok := resourceMap["memory"]; ok {
		memoryQty = mem
	}

	// Simplified pricing: $0.01 per CPU core per hour, $0.001 per GB per hour
	cpuCost := float64(cpuQty.MilliValue()) / 1000.0 * 0.01
	memoryGB := float64(memoryQty.Value()) / (1024 * 1024 * 1024)
	memoryCost := memoryGB * 0.001

	hourlyCost := (cpuCost + memoryCost) * float64(instanceCount)
	dailyCost := hourlyCost * 24
	monthlyCost := hourlyCost * 730

	return &CostEstimate{
		HourlyCost:  hourlyCost,
		DailyCost:   dailyCost,
		MonthlyCost: monthlyCost,
		Breakdown: map[string]float64{
			"cpu":    cpuCost * float64(instanceCount),
			"memory": memoryCost * float64(instanceCount),
		},
		Currency: "USD",
	}, nil
}

// GetCurrentInstanceCount returns current replica count
func (p *KubernetesProvider) GetCurrentInstanceCount(ctx context.Context, serviceName string) (int, error) {
	namespace := p.namespace
	deployment, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get deployment: %w", err)
	}

	if deployment.Spec.Replicas == nil {
		return 1, nil
	}

	return int(*deployment.Spec.Replicas), nil
}

// GetInstanceType returns resource requests as a string
func (p *KubernetesProvider) GetInstanceType(ctx context.Context, serviceName string) (string, error) {
	namespace := p.namespace
	deployment, err := p.clientset.AppsV1().Deployments(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get deployment: %w", err)
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "unknown", nil
	}

	container := deployment.Spec.Template.Spec.Containers[0]
	var parts []string
	if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
		parts = append(parts, fmt.Sprintf("cpu=%s", cpu.String()))
	}
	if memory, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
		parts = append(parts, fmt.Sprintf("memory=%s", memory.String()))
	}

	if len(parts) == 0 {
		return "unknown", nil
	}

	return strings.Join(parts, ","), nil
}

// HealthCheck verifies Kubernetes connectivity
func (p *KubernetesProvider) HealthCheck(ctx context.Context) error {
	_, err := p.clientset.CoreV1().Namespaces().Get(ctx, p.namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Kubernetes health check failed: %w", err)
	}
	return nil
}

