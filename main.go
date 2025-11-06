package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"scalemind/config"
	"scalemind/handlers"
	"scalemind/observability"

	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := observability.InitializeLogger("info")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Initialize metrics
	observability.InitializeMetrics()

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	if err := cfg.Validate(); err != nil {
		logger.Warn("Configuration validation warnings", zap.Error(err))
	}

	// Initialize Sentry if configured
	if cfg.Obs.SentryDSN != "" {
		if err := observability.InitializeSentry(cfg.Obs.SentryDSN, os.Getenv("ENV")); err != nil {
			logger.Warn("Failed to initialize Sentry", zap.Error(err))
		}
	}

	logger.Info("ScaleMind MCP Server starting",
		zap.String("server_name", cfg.Server.Name),
		zap.String("protocol_version", cfg.Server.ProtocolVersion),
	)

	// Create MCP handler (non-fatal if providers fail)
	mcpHandler, err := handlers.NewMCPHandler(cfg)
	if err != nil {
		logger.Error("Failed to create MCP handler", zap.Error(err))
		// Continue anyway - tools can still be listed
		mcpHandler = nil
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start MCP server
	// If handler is nil, server will still respond to initialize and tools/list
	go runMCPServer(ctx, mcpHandler, logger)

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutting down ScaleMind MCP Server")
	cancel()
}

// runMCPServer runs the MCP server using STDIO transport
func runMCPServer(ctx context.Context, handler *handlers.MCPHandler, logger *zap.Logger) {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	// CRITICAL: Don't buffer stdout - MCP needs immediate output
	encoder.SetIndent("", "")

	// Don't log to stdout - it will corrupt the MCP protocol
	// Logs go to stderr via logger configuration

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Recover from any panics to keep server running
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Panic recovered in MCP server",
							zap.Any("panic", r),
							zap.Stack("stack"))
					}
				}()

				var request map[string]interface{}
				if err := decoder.Decode(&request); err != nil {
					if err.Error() == "EOF" {
						return
					}
					// Log errors to stderr only
					logger.Error("Failed to decode request", zap.Error(err))
					
					// Send error response if we have an ID
					if id, ok := request["id"]; ok && id != nil {
						errorResponse := map[string]interface{}{
							"jsonrpc": "2.0",
							"id":      id,
							"error": map[string]interface{}{
								"code":    -32700,
								"message": "Parse error",
							},
						}
						_ = encoder.Encode(errorResponse)
					}
					return
				}

				// Generate trace ID
				traceID := observability.NewTraceID()
				reqCtx := observability.WithTraceID(ctx, traceID)

				// Handle request with error recovery
				response := handleRequest(reqCtx, handler, request, logger)

				// Send response (if it's not a notification)
				if response != nil {
					if err := encoder.Encode(response); err != nil {
						// Log errors to stderr only
						logger.Error("Failed to encode response", zap.Error(err))
					} else {
						// CRITICAL: Flush stdout immediately for MCP protocol
						_ = os.Stdout.Sync()
					}
				}
			}()
		}
	}
}

func handleRequest(ctx context.Context, handler *handlers.MCPHandler, request map[string]interface{}, logger *zap.Logger) map[string]interface{} {
	// Safely extract method, params, and id
	method, _ := request["method"].(string)
	params, _ := request["params"].(map[string]interface{})
	id := request["id"]

	// Handle notifications (no id) differently
	if id == nil {
		// This is a notification, handle silently
		if method == "initialized" {
			logger.Info("MCP client initialized")
			return nil
		}
		return nil
	}

	// Create base response
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
	}

	// Handle different MCP methods with error recovery
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in handleRequest",
				zap.String("method", method),
				zap.Any("panic", r),
				zap.Stack("stack"))
			response["error"] = map[string]interface{}{
				"code":    -32603,
				"message": "Internal error",
			}
		}
	}()

	switch method {
	case "initialize":
		// MCP protocol requires initialize handshake first
		// Return full capabilities including tools support
		response["result"] = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "ScaleMind",
				"version": "1.0.0",
			},
		}
	case "tools/list":
		// Always return tools list, even if providers fail
		response["result"] = getToolsList()
	case "tools/call":
		// Handle tool calls with proper error handling
		if handler == nil {
			response["error"] = map[string]interface{}{
				"code":    -32603,
				"message": "Server not initialized",
			}
			break
		}
		
		result, err := handleToolCall(ctx, handler, params)
		if err != nil {
			response["error"] = map[string]interface{}{
				"code":    -32000,
				"message": err.Error(),
			}
		} else {
			response["result"] = map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": result,
					},
				},
			}
		}
	default:
		response["error"] = map[string]interface{}{
			"code":    -32601,
			"message": fmt.Sprintf("Method not found: %s", method),
		}
	}

	return response
}

func getToolsList() map[string]interface{} {
	return map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "analyze_resource_usage",
				"description": "Fetch real-time CPU, memory, network, and disk metrics for a resource",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"aws", "kubernetes"},
							"description": "Cloud provider (aws or kubernetes)",
						},
						"resource_id": map[string]interface{}{
							"type":        "string",
							"description": "Instance ID (AWS) or deployment name (Kubernetes)",
						},
						"region": map[string]interface{}{
							"type":        "string",
							"description": "AWS region (optional, defaults to us-east-1)",
						},
					},
					"required": []string{"provider", "resource_id"},
				},
			},
			{
				"name":        "scale_horizontal",
				"description": "Add or remove instances/pods horizontally",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"aws", "kubernetes"},
							"description": "Cloud provider",
						},
						"service_name": map[string]interface{}{
							"type":        "string",
							"description": "Service/deployment name",
						},
						"desired_count": map[string]interface{}{
							"type":        "integer",
							"description": "Target instance count",
						},
						"max_count": map[string]interface{}{
							"type":        "integer",
							"description": "Safety limit (optional)",
						},
						"dry_run": map[string]interface{}{
							"type":        "boolean",
							"description": "Show what would happen without executing",
						},
					},
					"required": []string{"provider", "service_name", "desired_count"},
				},
			},
			{
				"name":        "estimate_cost",
				"description": "Calculate costs before scaling",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"aws", "kubernetes"},
							"description": "Cloud provider",
						},
						"instance_type": map[string]interface{}{
							"type":        "string",
							"description": "Instance type (e.g., t3.medium, m5.large)",
						},
						"instance_count": map[string]interface{}{
							"type":        "integer",
							"description": "Number of instances",
						},
						"duration_hours": map[string]interface{}{
							"type":        "integer",
							"description": "Duration in hours (1, 24, or 730 for monthly)",
						},
						"region": map[string]interface{}{
							"type":        "string",
							"description": "Cloud region (optional)",
						},
					},
					"required": []string{"provider", "instance_type", "instance_count", "duration_hours"},
				},
			},
			{
				"name":        "recommend_scaling_strategy",
				"description": "Recommend horizontal vs vertical scaling",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"aws", "kubernetes"},
							"description": "Cloud provider",
						},
						"current_load_percent": map[string]interface{}{
							"type":        "number",
							"description": "CPU/memory utilization percentage",
						},
						"budget_limit_usd": map[string]interface{}{
							"type":        "number",
							"description": "Monthly budget ceiling",
						},
						"current_instance_count": map[string]interface{}{
							"type":        "integer",
							"description": "Current replicas",
						},
						"workload_type": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"cpu_intensive", "memory_intensive", "io_intensive", "balanced"},
							"description": "Workload type (optional)",
						},
						"time_horizon": map[string]interface{}{
							"type":        "string",
							"description": "Time horizon: next 1h, 1d, 1w (optional)",
						},
					},
					"required": []string{"provider", "current_load_percent", "budget_limit_usd", "current_instance_count"},
				},
			},
			{
				"name":        "scale_vertical",
				"description": "Upgrade/downgrade instance type",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"provider": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"aws", "kubernetes"},
							"description": "Cloud provider",
						},
						"service_name": map[string]interface{}{
							"type":        "string",
							"description": "Service name",
						},
						"new_instance_type": map[string]interface{}{
							"type":        "string",
							"description": "New instance type (e.g., t3.large, m5.xlarge)",
						},
						"dry_run": map[string]interface{}{
							"type":        "boolean",
							"description": "Show what would happen without executing",
						},
					},
					"required": []string{"provider", "service_name", "new_instance_type"},
				},
			},
		},
	}
}

func handleToolCall(ctx context.Context, handler *handlers.MCPHandler, params map[string]interface{}) (string, error) {
	if params == nil {
		return "", fmt.Errorf("missing params")
	}

	toolName, ok := params["name"].(string)
	if !ok || toolName == "" {
		return "", fmt.Errorf("missing or invalid tool name")
	}

	arguments, _ := params["arguments"].(map[string]interface{})
	if arguments == nil {
		arguments = make(map[string]interface{})
	}

	switch toolName {
	case "analyze_resource_usage":
		provider, _ := arguments["provider"].(string)
		resourceID, _ := arguments["resource_id"].(string)
		region, _ := arguments["region"].(string)
		return handler.HandleAnalyzeResourceUsage(ctx, provider, resourceID, region)

	case "scale_horizontal":
		provider, _ := arguments["provider"].(string)
		serviceName, _ := arguments["service_name"].(string)
		desiredCount := int(arguments["desired_count"].(float64))
		maxCount := 0
		if mc, ok := arguments["max_count"].(float64); ok {
			maxCount = int(mc)
		}
		dryRun, _ := arguments["dry_run"].(bool)
		return handler.HandleScaleHorizontal(ctx, provider, serviceName, desiredCount, maxCount, dryRun)

	case "estimate_cost":
		provider, _ := arguments["provider"].(string)
		instanceType, _ := arguments["instance_type"].(string)
		instanceCount := int(arguments["instance_count"].(float64))
		durationHours := int(arguments["duration_hours"].(float64))
		region, _ := arguments["region"].(string)
		return handler.HandleEstimateCost(ctx, provider, instanceType, instanceCount, durationHours, region)

	case "recommend_scaling_strategy":
		provider, _ := arguments["provider"].(string)
		currentLoadPercent := arguments["current_load_percent"].(float64)
		budgetLimitUSD := arguments["budget_limit_usd"].(float64)
		currentInstanceCount := int(arguments["current_instance_count"].(float64))
		workloadType, _ := arguments["workload_type"].(string)
		timeHorizon, _ := arguments["time_horizon"].(string)
		return handler.HandleRecommendScalingStrategy(ctx, provider, currentLoadPercent, budgetLimitUSD, currentInstanceCount, workloadType, timeHorizon)

	case "scale_vertical":
		provider, _ := arguments["provider"].(string)
		serviceName, _ := arguments["service_name"].(string)
		newInstanceType, _ := arguments["new_instance_type"].(string)
		dryRun, _ := arguments["dry_run"].(bool)
		return handler.HandleScaleVertical(ctx, provider, serviceName, newInstanceType, dryRun)

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

