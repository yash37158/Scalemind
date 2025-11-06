# ScaleMind - AI-Powered Infrastructure Autoscaling MCP Server

ScaleMind is an intelligent autoscaling MCP (Model Context Protocol) server that enables DevOps engineers to manage cloud infrastructure scaling through natural language conversations in Cursor IDE.

## Features

- **Resource Analysis**: Fetch real-time CPU, memory, network, and disk metrics
- **Horizontal Scaling**: Add or remove instances/pods with cost estimation
- **Vertical Scaling**: Upgrade/downgrade instance types
- **Cost Estimation**: Calculate costs before scaling operations
- **Smart Recommendations**: AI-driven scaling strategy recommendations
- **Multi-Cloud Support**: AWS, Kubernetes (GCP coming in Phase 2)

## Prerequisites

- Go 1.24+ (for building from source)
- AWS credentials configured (for AWS provider)
- Kubernetes cluster access (for Kubernetes provider)
- Cursor IDE with MCP support

## Installation

### Build from Source

1. **Clone the repository:**
```bash
git clone <repository-url>
cd ScaleMind
```

2. **Install dependencies:**
```bash
go mod download
```

3. **Build the application:**
```bash
go build -o scalemind main.go
```

4. **Verify the build:**
```bash
./scalemind --help  # Or check if binary exists
ls -lh scalemind
```

### Docker

**Build the Docker image:**
```bash
docker build -t scalemind:latest .
```

**Run the container:**
```bash
docker run -it --rm \
  -e AWS_REGION=us-east-1 \
  -e KUBECONFIG=/path/to/kubeconfig \
  scalemind:latest
```

## MCP Configuration

ScaleMind communicates with Cursor IDE via the Model Context Protocol (MCP) over STDIO. Configure Cursor to use ScaleMind as an MCP server.

### Step 1: Locate MCP Configuration File

The MCP configuration file location varies by operating system:

| OS | Configuration File Path |
|---|---|
| **macOS** | `~/Library/Application Support/Cursor/User/globalStorage/mcp.json` |
| **Linux** | `~/.config/Cursor/User/globalStorage/mcp.json` |
| **Windows** | `%APPDATA%\Cursor\User\globalStorage\mcp.json` |

> **Note**: If the file doesn't exist, create it with the directory structure.

### Step 2: Configure ScaleMind

Open or create the MCP configuration file and add ScaleMind server configuration.

#### AWS Configuration

```json
{
  "mcpServers": {
    "scalemind": {
      "command": "/absolute/path/to/scalemind",
      "args": [],
      "env": {
        "AWS_REGION": "us-east-1",
        "LOG_LEVEL": "info"
      }
    }
  }
}
```

**AWS Credentials Setup:**

ScaleMind uses AWS SDK's default credential chain:
1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. `~/.aws/credentials` file
3. IAM role (if running on EC2)

**Required IAM Permissions:**
- `ec2:DescribeInstances`, `ec2:DescribeRegions`
- `autoscaling:DescribeAutoScalingGroups`, `autoscaling:SetDesiredCapacity`
- `cloudwatch:GetMetricStatistics`
- `pricing:GetProducts` (for cost estimation)

#### Kubernetes Configuration

```json
{
  "mcpServers": {
    "scalemind": {
      "command": "/absolute/path/to/scalemind",
      "args": [],
      "env": {
        "KUBECONFIG": "/path/to/kubeconfig",
        "LOG_LEVEL": "info"
      }
    }
  }
}
```

**Kubernetes Access Setup:**

If `KUBECONFIG` is not set, ScaleMind will try:
1. In-cluster configuration (if running as a pod)
2. `~/.kube/config` (default location)

**Required RBAC Permissions:**
- `apps/deployments: get, list, update`
- `pods: get, list`
- `metrics.k8s.io: get, list` (optional, for resource metrics)

#### Combined AWS + Kubernetes Configuration

```json
{
  "mcpServers": {
    "scalemind": {
      "command": "/absolute/path/to/scalemind",
      "args": [],
      "env": {
        "AWS_REGION": "us-east-1",
        "KUBECONFIG": "/path/to/kubeconfig",
        "LOG_LEVEL": "info"
      }
    }
  }
}
```

### Step 3: Get Absolute Path to Binary

**macOS/Linux:**
```bash
# If built from source
realpath scalemind

# If installed globally
which scalemind
```

**Windows:**
```powershell
Resolve-Path .\scalemind.exe
```

### Step 4: Verify Configuration

1. **Check JSON syntax:**
```bash
# macOS/Linux
cat ~/Library/Application\ Support/Cursor/User/globalStorage/mcp.json | python3 -m json.tool

# Windows (PowerShell)
Get-Content $env:APPDATA\Cursor\User\globalStorage\mcp.json | ConvertFrom-Json | ConvertTo-Json
```

2. **Test the binary:**
```bash
# Should wait for STDIO input (no output)
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}' | ./scalemind
```

### Step 5: Restart Cursor IDE

1. **Completely quit Cursor IDE** (not just close the window)
2. **Restart Cursor IDE**
3. **Verify MCP connection** by asking Cursor about infrastructure tools

## Configuration

### Environment Variables

ScaleMind can be configured via environment variables in the MCP configuration or via a `.env` file.

#### AWS Configuration

```bash
AWS_REGION=us-east-1  # Default AWS region (required for AWS provider)
```

AWS credentials are handled via AWS SDK default chain (see AWS Configuration section above).

#### Kubernetes Configuration

```bash
KUBECONFIG=/path/to/kubeconfig  # Path to kubeconfig file (optional)
```

If not set, ScaleMind will try in-cluster config or `~/.kube/config`.

#### Observability

```bash
LOG_LEVEL=info                    # Log level: debug, info, warn, error
SENTRY_DSN=https://xxx@sentry.io/xxx  # Sentry error tracking (optional)
PROMETHEUS_PORT=8080             # Prometheus metrics port (optional)
```

#### Optional: HashiCorp Vault

```bash
VAULT_ADDR=https://vault.example.com    # Vault server address
VAULT_TOKEN=your-vault-token             # Vault authentication token
```

> **Note**: If Vault is configured, secrets will be retrieved from Vault. Otherwise, environment variables are used.

## MCP Tools

ScaleMind exposes 5 MCP tools for infrastructure management:

### 1. analyze_resource_usage

Analyze resource utilization for a specific resource.

**Parameters:**
- `provider` (required): `"aws"` or `"kubernetes"`
- `resource_id` (required): 
  - AWS: EC2 Instance ID (e.g., `"i-1234567890"`)
  - Kubernetes: Deployment name or `"namespace/deployment-name"`
- `region` (optional): AWS region (defaults to configured `AWS_REGION`)

**Example:**
```json
{
  "name": "analyze_resource_usage",
  "arguments": {
    "provider": "aws",
    "resource_id": "i-1234567890",
    "region": "us-east-1"
  }
}
```

### 2. scale_horizontal

Scale resources horizontally (add/remove instances).

**Parameters:**
- `provider` (required): `"aws"` or `"kubernetes"`
- `service_name` (required): 
  - AWS: Auto Scaling Group name
  - Kubernetes: Deployment name
- `desired_count` (required): Target instance/replica count
- `max_count` (optional): Safety limit to prevent over-scaling
- `dry_run` (optional): `true` to simulate without executing

**Example:**
```json
{
  "name": "scale_horizontal",
  "arguments": {
    "provider": "kubernetes",
    "service_name": "my-app",
    "desired_count": 5,
    "max_count": 10,
    "dry_run": true
  }
}
```

### 3. estimate_cost

Calculate costs before scaling operations.

**Parameters:**
- `provider` (required): `"aws"` or `"kubernetes"`
- `instance_type` (required): 
  - AWS: Instance type (e.g., `"t3.medium"`, `"m5.large"`)
  - Kubernetes: Resource string (e.g., `"cpu=2,memory=4Gi"`)
- `instance_count` (required): Number of instances
- `duration_hours` (required): Duration in hours (1, 24, or 730 for monthly)
- `region` (optional): Cloud region

**Example:**
```json
{
  "name": "estimate_cost",
  "arguments": {
    "provider": "aws",
    "instance_type": "t3.medium",
    "instance_count": 3,
    "duration_hours": 730,
    "region": "us-east-1"
  }
}
```

### 4. recommend_scaling_strategy

Get AI-driven scaling recommendations based on current conditions.

**Parameters:**
- `provider` (required): `"aws"` or `"kubernetes"`
- `current_load_percent` (required): CPU/memory utilization percentage (0-100)
- `budget_limit_usd` (required): Monthly budget ceiling in USD
- `current_instance_count` (required): Current number of replicas/instances
- `workload_type` (optional): `"cpu_intensive"`, `"memory_intensive"`, `"io_intensive"`, or `"balanced"`
- `time_horizon` (optional): `"next 1h"`, `"1d"`, or `"1w"`

**Example:**
```json
{
  "name": "recommend_scaling_strategy",
  "arguments": {
    "provider": "aws",
    "current_load_percent": 85.5,
    "budget_limit_usd": 500.0,
    "current_instance_count": 3,
    "workload_type": "cpu_intensive"
  }
}
```

### 5. scale_vertical

Upgrade or downgrade instance type.

**Parameters:**
- `provider` (required): `"aws"` or `"kubernetes"`
- `service_name` (required): Service/deployment name
- `new_instance_type` (required): 
  - AWS: New instance type (e.g., `"t3.large"`, `"m5.xlarge"`)
  - Kubernetes: Resource string (e.g., `"cpu=4,memory=8Gi"`)
- `dry_run` (optional): `true` to simulate without executing

**Example:**
```json
{
  "name": "scale_vertical",
  "arguments": {
    "provider": "aws",
    "service_name": "my-asg",
    "new_instance_type": "t3.large",
    "dry_run": true
  }
}
```

> **⚠️ Warning**: Vertical scaling may cause brief downtime during instance replacement in AWS.

## Usage Examples

### Testing Prompts for AWS

#### Resource Analysis
```
Analyze the resource usage for my AWS EC2 instance i-1234567890abcde in us-east-1
```

```
Can you check the CPU and network metrics for instance i-0987654321fedcb in us-west-2?
```

#### Horizontal Scaling
```
Show me what would happen if I scale my Auto Scaling Group 'production-web-asg' to 5 instances (dry run)
```

```
Scale my AWS Auto Scaling Group 'staging-api-asg' to 3 instances with a max limit of 5
```

```
What would happen if I scale 'my-app-asg' from 2 to 8 instances? Show me the dry run results
```

#### Cost Estimation
```
How much would it cost to run 3 t3.medium instances in us-east-1 for a month?
```

```
Estimate the cost for 5 m5.large instances in eu-west-1 for 24 hours
```

```
What's the monthly cost for 10 c5.xlarge instances in ap-southeast-1?
```

#### Scaling Recommendations
```
My AWS instance is running at 85% CPU. I have a budget of $500/month and currently have 3 instances. 
What scaling strategy do you recommend for a cpu_intensive workload?
```

```
I'm seeing 92% load on my infrastructure with 2 instances. Budget is $300/month. 
What should I do? Consider a 1 week time horizon.
```

#### Vertical Scaling
```
Show me what would happen if I change the instance type for my ASG 'my-service-asg' to t3.large (dry run)
```

### Testing Prompts for Kubernetes

#### Resource Analysis
```
Analyze the resource usage for my Kubernetes deployment 'my-app' in the default namespace
```

```
Can you check the CPU and memory metrics for deployment 'api-server' in namespace 'production'?
```

```
What's the current resource utilization for deployment 'frontend'?
```

#### Horizontal Scaling
```
Show me what would happen if I scale my Kubernetes deployment 'my-app' to 5 replicas (dry run)
```

```
Scale my Kubernetes deployment 'api-server' in namespace 'production' to 3 replicas with max limit of 10
```

```
What would happen if I scale deployment 'worker' from 2 to 6 replicas? Show me the dry run
```

#### Cost Estimation
```
How much would it cost to run 3 pods with cpu=2,memory=4Gi for a month?
```

```
Estimate the cost for 5 pods with cpu=1,memory=2Gi for 24 hours
```

```
What's the monthly cost for 10 pods with cpu=4,memory=8Gi?
```

#### Scaling Recommendations
```
My Kubernetes deployment 'my-app' is running at 85% CPU. I have a budget of $500/month and currently have 3 replicas. 
What scaling strategy do you recommend for a memory_intensive workload?
```

```
I'm seeing 90% load on my deployment with 2 replicas. Budget is $300/month. 
What should I do? Consider a 1 day time horizon.
```

#### Vertical Scaling
```
Update the resource requests for my Kubernetes deployment 'my-app' to cpu=4,memory=8Gi
```

```
Show me what would happen if I change resources for deployment 'api-server' to cpu=2,memory=4Gi (dry run)
```

```
Scale up the resources for deployment 'worker' to cpu=8,memory=16Gi
```

### General Testing Prompts

#### List Available Tools
```
What MCP tools are available for infrastructure management?
```

```
What can you help me with regarding AWS and Kubernetes scaling?
```

#### Combined Operations
```
Analyze my AWS instance i-1234567890, then estimate the cost if I scale the ASG to 5 instances
```

```
Check my Kubernetes deployment 'my-app' metrics, then recommend a scaling strategy with a $400 budget
```

## Architecture

```
┌─────────────────────────────────────┐
│          Cursor IDE                 │
│     (MCP Client)                    │
└──────────────────┬──────────────────┘
                   │
                   │ JSON-RPC 2.0 (STDIO)
                   ▼
┌──────────────────────────────────────┐
│     ScaleMind MCP Server             │
│  - Tools Layer                       │
│  - Provider Abstraction              │
│  - Observability                     │
└──────────┬───────────────────────────┘
           │
    ┌──────┴──────┬──────────┐
    ▼             ▼          ▼
  AWS API    Kubernetes   GCP API
```

## Security

- Secrets should be stored in HashiCorp Vault (not environment variables in production)
- All scaling operations require explicit user approval
- Input validation on all tool parameters
- Structured logging with trace IDs for audit trails

## Troubleshooting

### MCP Server Not Connecting

1. Verify MCP configuration file path and JSON syntax
2. Ensure absolute path to `scalemind` binary is correct
3. Check file permissions: `chmod +x scalemind`
4. Restart Cursor IDE completely (quit and reopen)
5. Check Cursor logs for MCP errors

### Provider Initialization Failed

**AWS:**
```bash
# Verify credentials
aws sts get-caller-identity
```

**Kubernetes:**
```bash
# Verify cluster access
kubectl cluster-info
```

### No Metrics Available

**Kubernetes:** Ensure metrics-server is installed:
```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

## License

[Your License Here]

## Support

[Support Information]
