# MIG Controller Environment Variables

All environment variables use the `CONTROLLER_` prefix.

## Quick Start (Minimum Required)

```bash
# Required configuration
export CONTROLLER_POOL_ID="pool-2vcpu-linux"
export CONTROLLER_GCP_PROJECT_ID="your-gcp-project"
export CONTROLLER_GCP_ZONE="us-central1-a"
export CONTROLLER_GCP_MIG_NAME="monkci-runners-2vcpu"
export CONTROLLER_GITHUB_APP_ID="12345"
export CONTROLLER_GITHUB_APP_PRIVATE_KEY_PATH="/etc/controller/github-app.pem"
export CONTROLLER_REDIS_JOBS_HOST="redis.internal"
export CONTROLLER_REDIS_VM_HOST="redis.internal"
export CONTROLLER_PUBSUB_PROJECT_ID="your-gcp-project"
export CONTROLLER_PUBSUB_SUBSCRIPTION="jobs-2vcpu-sub"
```

---

## Complete Reference

### Server Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_GRPC_PORT` | gRPC server port | `50051` |
| `CONTROLLER_HTTP_PORT` | HTTP server port | `8080` |
| `CONTROLLER_TLS_ENABLED` | Enable TLS | `false` |
| `CONTROLLER_TLS_CERT_PATH` | Path to TLS certificate | - |
| `CONTROLLER_TLS_KEY_PATH` | Path to TLS private key | - |
| `CONTROLLER_TLS_CA_PATH` | Path to CA certificate (mTLS) | - |

### Pool Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_POOL_ID` | Unique pool identifier | - | ✅ |
| `CONTROLLER_POOL_NAME` | Human-readable pool name | - | |
| `CONTROLLER_POOL_TYPE` | Machine type (2vcpu, 4vcpu, 8vcpu) | - | |
| `CONTROLLER_POOL_OS` | Operating system | `linux` | |
| `CONTROLLER_POOL_ARCH` | Architecture | `x64` | |
| `CONTROLLER_POOL_REGION` | GCP region | - | |
| `CONTROLLER_POOL_RUNNER_GROUP` | GitHub runner group | `default` | |
| `CONTROLLER_POOL_LABELS` | Runner labels (comma-separated) | `self-hosted` | |

### GCP Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_GCP_PROJECT_ID` | GCP project ID | - | ✅ |
| `CONTROLLER_GCP_ZONE` | GCP zone | - | ✅ |
| `CONTROLLER_GCP_MIG_NAME` | Managed Instance Group name | - | ✅ |
| `CONTROLLER_GCP_NETWORK_PROJECT` | Network project (Shared VPC) | - | |
| `CONTROLLER_GCP_NETWORK` | VPC network name | `default` | |
| `CONTROLLER_GCP_SUBNETWORK` | Subnetwork name | - | |
| `CONTROLLER_GCP_SERVICE_ACCOUNT_PATH` | Path to SA key file | - | |

### GitHub App Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_GITHUB_APP_ID` | GitHub App ID | - | ✅ |
| `CONTROLLER_GITHUB_APP_PRIVATE_KEY_PATH` | Path to private key PEM | - | ✅* |
| `CONTROLLER_GITHUB_APP_PRIVATE_KEY` | Private key content | - | ✅* |
| `CONTROLLER_GITHUB_WEBHOOK_SECRET` | Webhook secret | - | |
| `CONTROLLER_GITHUB_BASE_URL` | API base URL (for GHES) | `https://api.github.com` | |

> *Either `PRIVATE_KEY_PATH` or `PRIVATE_KEY` is required

### Redis - Jobs Queue

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_REDIS_JOBS_HOST` | Redis host | - | ✅ |
| `CONTROLLER_REDIS_JOBS_PORT` | Redis port | `6379` | |
| `CONTROLLER_REDIS_JOBS_PASSWORD` | Redis password | - | |
| `CONTROLLER_REDIS_JOBS_DB` | Redis database number | `0` | |
| `CONTROLLER_REDIS_JOBS_TLS` | Enable TLS | `false` | |

### Redis - VM Status

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_REDIS_VM_HOST` | Redis host | - | ✅ |
| `CONTROLLER_REDIS_VM_PORT` | Redis port | `6379` | |
| `CONTROLLER_REDIS_VM_PASSWORD` | Redis password | - | |
| `CONTROLLER_REDIS_VM_DB` | Redis database number | `1` | |
| `CONTROLLER_REDIS_VM_TLS` | Enable TLS | `false` | |

### Pub/Sub Configuration

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `CONTROLLER_PUBSUB_PROJECT_ID` | Pub/Sub project ID | - | ✅ |
| `CONTROLLER_PUBSUB_SUBSCRIPTION` | Subscription name | - | ✅ |
| `CONTROLLER_PUBSUB_TOPIC_ID` | Topic for events | - | |

### Scheduler Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_SCHEDULER_POLL_INTERVAL` | Job queue poll interval | `1s` |
| `CONTROLLER_SCHEDULER_ASSIGNMENT_TIMEOUT` | VM ready timeout | `5m` |
| `CONTROLLER_SCHEDULER_MAX_CONCURRENT` | Max parallel assignments | `10` |
| `CONTROLLER_SCHEDULER_MAX_RETRIES` | Max job retries | `3` |

### VM Manager Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_VM_POLL_INTERVAL` | GCloud sync interval | `30s` |
| `CONTROLLER_VM_HEARTBEAT_TIMEOUT` | Unhealthy threshold | `60s` |
| `CONTROLLER_VM_MAX_SCALE_UP` | Max VMs created per minute | `5` |
| `CONTROLLER_VM_MIN_READY` | Warm pool size | `1` |
| `CONTROLLER_VM_MAX_VMS` | Maximum VMs in MIG | `50` |
| `CONTROLLER_VM_IDLE_TIMEOUT` | Stop VM after idle | `10m` |
| `CONTROLLER_VM_BOOT_TIMEOUT` | Max VM boot time | `5m` |

### MIGlet Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_MIGLET_COMMAND_TIMEOUT` | Command timeout | `30s` |
| `CONTROLLER_MIGLET_HEARTBEAT_INTERVAL` | Expected heartbeat | `15s` |
| `CONTROLLER_MIGLET_RUNNER_VERSION` | Runner version | `2.329.0` |

### Logging Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `CONTROLLER_LOG_FORMAT` | Log format (json/text) | `json` |
| `CONTROLLER_LOG_OUTPUT` | Output path or stdout | `stdout` |
| `CONTROLLER_LOG_REDACT_SECRETS` | Redact secrets in logs | `true` |

### Metrics Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_METRICS_ENABLED` | Enable metrics endpoint | `true` |
| `CONTROLLER_METRICS_PORT` | Metrics port | `9090` |
| `CONTROLLER_METRICS_PUSH_GATEWAY` | Prometheus PushGateway URL | - |

### Alerts Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `CONTROLLER_ALERTS_ENABLED` | Enable alerting | `false` |
| `CONTROLLER_ALERTS_SLACK_WEBHOOK` | Slack webhook URL | - |
| `CONTROLLER_ALERTS_PAGERDUTY_KEY` | PagerDuty key | - |

---

## Example Configurations

### 2 vCPU Pool

```bash
export CONTROLLER_POOL_ID="pool-2vcpu-linux-us-central1"
export CONTROLLER_POOL_TYPE="2vcpu"
export CONTROLLER_POOL_LABELS="self-hosted,linux,x64,2vcpu"
export CONTROLLER_GCP_MIG_NAME="monkci-runners-2vcpu"
export CONTROLLER_PUBSUB_SUBSCRIPTION="jobs-2vcpu-sub"
export CONTROLLER_VM_MIN_READY="2"
export CONTROLLER_VM_MAX_VMS="20"
```

### 8 vCPU Pool (GPU Workloads)

```bash
export CONTROLLER_POOL_ID="pool-8vcpu-linux-gpu"
export CONTROLLER_POOL_TYPE="8vcpu"
export CONTROLLER_POOL_LABELS="self-hosted,linux,x64,8vcpu,gpu"
export CONTROLLER_GCP_MIG_NAME="monkci-runners-8vcpu-gpu"
export CONTROLLER_PUBSUB_SUBSCRIPTION="jobs-8vcpu-sub"
export CONTROLLER_VM_MIN_READY="1"
export CONTROLLER_VM_MAX_VMS="10"
export CONTROLLER_VM_IDLE_TIMEOUT="5m"  # Shorter for expensive VMs
```

### Production Settings

```bash
export CONTROLLER_LOG_LEVEL="info"
export CONTROLLER_LOG_FORMAT="json"
export CONTROLLER_LOG_REDACT_SECRETS="true"
export CONTROLLER_METRICS_ENABLED="true"
export CONTROLLER_ALERTS_ENABLED="true"
export CONTROLLER_ALERTS_SLACK_WEBHOOK="https://hooks.slack.com/..."
export CONTROLLER_TLS_ENABLED="true"
export CONTROLLER_TLS_CERT_PATH="/etc/controller/tls.crt"
export CONTROLLER_TLS_KEY_PATH="/etc/controller/tls.key"
```

### Development Settings

```bash
export CONTROLLER_LOG_LEVEL="debug"
export CONTROLLER_LOG_FORMAT="text"
export CONTROLLER_VM_MIN_READY="0"
export CONTROLLER_SCHEDULER_POLL_INTERVAL="5s"
```

---

## Kubernetes Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: controller-config
data:
  CONTROLLER_POOL_ID: "pool-2vcpu-linux"
  CONTROLLER_POOL_TYPE: "2vcpu"
  CONTROLLER_GCP_PROJECT_ID: "your-project"
  CONTROLLER_GCP_ZONE: "us-central1-a"
  CONTROLLER_GCP_MIG_NAME: "runners-2vcpu"
  CONTROLLER_REDIS_JOBS_HOST: "redis.default.svc"
  CONTROLLER_REDIS_VM_HOST: "redis.default.svc"
  CONTROLLER_PUBSUB_PROJECT_ID: "your-project"
  CONTROLLER_PUBSUB_SUBSCRIPTION: "jobs-2vcpu-sub"
---
apiVersion: v1
kind: Secret
metadata:
  name: controller-secrets
type: Opaque
stringData:
  CONTROLLER_GITHUB_APP_ID: "12345"
  CONTROLLER_GITHUB_APP_PRIVATE_KEY: |
    -----BEGIN RSA PRIVATE KEY-----
    ...
    -----END RSA PRIVATE KEY-----
  CONTROLLER_REDIS_JOBS_PASSWORD: "secret"
  CONTROLLER_REDIS_VM_PASSWORD: "secret"
```

