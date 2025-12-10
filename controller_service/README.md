# MIG Controller

A robust Go-based service for orchestrating GitHub Actions self-hosted runners across Google Cloud Managed Instance Groups (MIGs).

## Overview

The MIG Controller manages the lifecycle of VMs in a MIG, coordinates with MIGlet agents, and assigns GitHub Actions jobs to available runners.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Pub/Sub   │────►│    MIG      │────►│    VMs      │
│   (Jobs)    │     │ Controller  │     │  (MIGlet)   │
└─────────────┘     └─────────────┘     └─────────────┘
                           │
                    ┌──────┴──────┐
                    ▼             ▼
              ┌─────────┐   ┌─────────┐
              │  Redis  │   │  GCloud │
              │ (State) │   │  (API)  │
              └─────────┘   └─────────┘
```

## Features

- **Job Queue Management**: Receives jobs via Pub/Sub, queues in Redis
- **Smart Scheduling**: Assigns jobs to ready VMs, starts stopped VMs, or scales up
- **VM Lifecycle**: Manages VMs via GCloud API (start/stop/create)
- **gRPC Communication**: Bidirectional streaming with MIGlet agents
- **GitHub App Integration**: Generates runner registration tokens
- **Warm Pool**: Maintains minimum ready VMs for fast job assignment
- **Cost Optimization**: Stops idle VMs after configurable timeout

## Quick Start

### Prerequisites

- Go 1.21+
- GCP Project with Compute Engine and Pub/Sub APIs enabled
- Redis instances (or Cloud Memorystore)
- GitHub App with runner registration permissions

### Build

```bash
cd controller_service
go build -o bin/controller ./cmd/controller
```

### Configuration

Copy and customize the config file:

```bash
cp configs/controller.yaml.example configs/controller.yaml
# Edit configs/controller.yaml with your settings
```

Or use environment variables:

```bash
# Required
export CONTROLLER_POOL_ID="pool-2vcpu-linux"
export CONTROLLER_GCP_PROJECT_ID="your-project"
export CONTROLLER_GCP_ZONE="us-central1-a"
export CONTROLLER_GCP_MIG_NAME="monkci-runners-2vcpu"
export GITHUB_APP_ID="12345"
export GITHUB_APP_PRIVATE_KEY_PATH="/etc/controller/github-app-key.pem"
export REDIS_JOBS_HOST="localhost"
export REDIS_VM_HOST="localhost"
export PUBSUB_PROJECT_ID="your-project"
export PUBSUB_SUBSCRIPTION="jobs-pool-2vcpu-sub"

# Optional
export CONTROLLER_LOG_LEVEL="info"
export CONTROLLER_LOG_FORMAT="json"
```

### Run

```bash
./bin/controller --config configs/controller.yaml
```

## Architecture

### Components

| Component | Description |
|-----------|-------------|
| **Message Handler** | Parses Pub/Sub messages, enqueues jobs |
| **Scheduler** | Assigns jobs to VMs, handles provisioning |
| **gRPC Server** | Bidirectional streaming with MIGlets |
| **VM Manager** | Manages VM lifecycle via GCloud API |
| **Token Service** | Generates GitHub runner registration tokens |
| **Job Store** | Redis-backed job queue and state |
| **VM Status Store** | Redis-backed VM state tracking |

### Job Assignment Flow

```
1. Pub/Sub Message → Job Queue (Redis)
2. Scheduler picks job
3. Find ready VM → Assign immediately
   OR Start stopped VM → Wait for ready → Assign
   OR Scale up MIG → Wait for new VM → Assign
4. Send register_runner command via gRPC
5. MIGlet registers runner, runs job
6. Job completion event → Update status
```

### VM States

| Effective State | Description | Can Accept Job? |
|-----------------|-------------|-----------------|
| STOPPED | VM is terminated | No (start first) |
| STARTING | VM is booting | No (wait) |
| BOOTING | MIGlet initializing | No (wait) |
| CONNECTING | MIGlet connecting | No (wait) |
| READY | Ready for registration | **Yes** |
| IDLE | Runner is idle | **Yes** |
| BUSY | Running a job | No |
| ERROR | Something failed | No |

## Configuration Reference

### Pool Configuration

```yaml
pool:
  id: "pool-2vcpu-linux"      # Unique pool identifier
  type: "2vcpu"               # Machine type identifier
  os: "linux"                 # Operating system
  arch: "x64"                 # Architecture
  labels:                     # Runner labels
    - "self-hosted"
    - "linux"
    - "x64"
```

### Scheduler Configuration

```yaml
scheduler:
  poll_interval: "1s"                    # How often to check for jobs
  assignment_timeout: "5m"               # Max time to wait for VM ready
  max_concurrent_assignments: 10         # Parallel assignments
```

### VM Manager Configuration

```yaml
vm_manager:
  poll_interval: "30s"           # How often to sync with GCloud
  heartbeat_timeout: "60s"       # Mark unhealthy if no heartbeat
  min_ready_vms: 2               # Warm pool size
  max_vms: 50                    # Hard limit
  idle_timeout: "10m"            # Stop VM after this idle time
  max_scale_up_per_minute: 5     # Rate limiting
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `CONTROLLER_POOL_ID` | Pool identifier |
| `CONTROLLER_POOL_TYPE` | Machine type (2vcpu, 4vcpu, etc.) |
| `CONTROLLER_GCP_PROJECT_ID` | GCP project |
| `CONTROLLER_GCP_ZONE` | GCP zone |
| `CONTROLLER_GCP_MIG_NAME` | MIG name |
| `GITHUB_APP_ID` | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY_PATH` | Path to private key |
| `GITHUB_APP_PRIVATE_KEY` | Private key content (alternative) |
| `REDIS_JOBS_HOST` | Jobs Redis host |
| `REDIS_JOBS_PASSWORD` | Jobs Redis password |
| `REDIS_VM_HOST` | VM Status Redis host |
| `REDIS_VM_PASSWORD` | VM Status Redis password |
| `PUBSUB_PROJECT_ID` | Pub/Sub project |
| `PUBSUB_SUBSCRIPTION` | Pub/Sub subscription name |

## API Endpoints

### Health & Monitoring

- `GET /health` - Health check (returns 200 if healthy)
- `GET /ready` - Readiness check
- `GET /stats` - Scheduler and Pub/Sub statistics

### gRPC Service

```protobuf
service CommandService {
  rpc StreamCommands(stream MIGletMessage) returns (stream ControllerMessage);
}
```

## Deployment

### Multiple Pool Types

Deploy one controller per pool type:

```bash
# 2 vCPU pool
CONTROLLER_POOL_ID="pool-2vcpu" \
CONTROLLER_GCP_MIG_NAME="runners-2vcpu" \
PUBSUB_SUBSCRIPTION="jobs-2vcpu-sub" \
./controller

# 4 vCPU pool
CONTROLLER_POOL_ID="pool-4vcpu" \
CONTROLLER_GCP_MIG_NAME="runners-4vcpu" \
PUBSUB_SUBSCRIPTION="jobs-4vcpu-sub" \
./controller
```

minimum required enc var

```bash
# Pool
export CONTROLLER_POOL_ID="pool-2vcpu-linux"

# GCP
export CONTROLLER_GCP_PROJECT_ID="your-project"
export CONTROLLER_GCP_ZONE="us-central1-a"
export CONTROLLER_GCP_MIG_NAME="runners-2vcpu"

# GitHub App
export CONTROLLER_GITHUB_APP_ID="12345"
export CONTROLLER_GITHUB_APP_PRIVATE_KEY_PATH="/path/to/key.pem"

# Redis
export CONTROLLER_REDIS_JOBS_HOST="redis.internal"
export CONTROLLER_REDIS_VM_HOST="redis.internal"

# Pub/Sub
export CONTROLLER_PUBSUB_PROJECT_ID="your-project"
export CONTROLLER_PUBSUB_SUBSCRIPTION="jobs-2vcpu-sub"
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mig-controller-2vcpu
spec:
  replicas: 1  # Only one per pool type
  template:
    spec:
      containers:
      - name: controller
        image: your-registry/mig-controller:latest
        env:
        - name: CONTROLLER_POOL_ID
          value: "pool-2vcpu-linux"
        # ... other env vars
```

## Metrics

| Metric | Description |
|--------|-------------|
| `queue_length` | Jobs waiting in queue |
| `assigned_jobs` | Total jobs assigned |
| `failed_jobs` | Total failed assignments |
| `started_vms` | VMs started from stopped |
| `created_vms` | New VMs created |
| `connected_vms` | Active MIGlet connections |

## Directory Structure

```
controller_service/
├── cmd/controller/        # Main entry point
├── internal/
│   ├── config/           # Configuration loading
│   ├── grpc/             # gRPC server for MIGlets
│   ├── pubsub/           # Pub/Sub subscriber
│   ├── redis/            # Redis clients (jobs + VM status)
│   ├── scheduler/        # Job scheduling logic
│   ├── token/            # GitHub token generation
│   └── vm/               # VM lifecycle management
├── pkg/logger/           # Structured logging
├── proto/commands/       # gRPC protobuf definitions
├── configs/              # Configuration examples
└── deploy/               # Deployment configs
```

## License

MIT

