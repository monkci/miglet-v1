# Phase 1 Testing Guide

## Overview
Phase 1 implements the foundational components:
- ✅ Go module setup
- ✅ Structured logger (JSON/text output)
- ✅ Configuration management (env vars, config file)
- ✅ Basic CLI entry point
- ✅ Signal handling

## Local Testing

### 1. Build MIGlet
```bash
go build -o bin/miglet ./cmd/miglet
```

### 2. Test Version
```bash
./bin/miglet --version
```

### 3. Test with Environment Variables
```bash
export MIGLET_POOL_ID="test-pool-123"
export MIGLET_VM_ID="test-vm-456"
export MIGLET_ORG_ID="test-org-789"
export MIGLET_CONTROLLER_ENDPOINT="https://test-controller.example.com"
export MIGLET_GITHUB_ORG="testorg"
export MIGLET_LOGGING_LEVEL="debug"
export MIGLET_LOGGING_FORMAT="text"

./bin/miglet
```

### 4. Test with Config File
```bash
# Copy and edit the example config
cp configs/miglet.yaml.example /tmp/miglet.yaml
# Edit /tmp/miglet.yaml with your values

./bin/miglet --config /tmp/miglet.yaml
```

### 5. Run Test Script
```bash
./scripts/test-phase1.sh
```

## GCP VM Testing

### Prerequisites
- GCP VM with Go 1.21+ installed
- SSH access to the VM

### Deployment Steps

#### Option 1: Build on VM
```bash
# SSH into your GCP VM
gcloud compute ssh <instance-name> --zone <zone>

# Clone or transfer the code
git clone <your-repo> miglet-v1
cd miglet-v1

# Build
go build -o bin/miglet ./cmd/miglet
```

#### Option 2: Build Locally and Transfer
```bash
# Build locally
go build -o bin/miglet ./cmd/miglet

# Transfer to VM
gcloud compute scp bin/miglet <instance-name>:~/miglet --zone <zone>
gcloud compute scp configs/miglet.yaml.example <instance-name>:~/miglet.yaml --zone <zone>
```

### Testing on GCP VM

#### 1. Test with Environment Variables
```bash
# On the VM
export MIGLET_POOL_ID="gcp-pool-001"
export MIGLET_VM_ID="gcp-vm-$(hostname)"
export MIGLET_ORG_ID="gcp-org-001"
export MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io"
export MIGLET_GITHUB_ORG="your-org"
export MIGLET_LOGGING_LEVEL="info"
export MIGLET_LOGGING_FORMAT="json"

./miglet
```

#### 2. Test with Config File
```bash
# Create config file
cat > miglet.yaml <<EOF
pool_id: "gcp-pool-001"
vm_id: "gcp-vm-$(hostname)"
org_id: "gcp-org-001"
controller:
  endpoint: "https://controller.monkci.io"
  auth:
    type: "bearer"
    token_path: "/var/run/secrets/controller-token"
github:
  org: "your-org"
  runner_group: "default"
  labels: ["self-hosted", "linux", "x64"]
logging:
  level: "info"
  format: "json"
EOF

./miglet --config miglet.yaml
```

#### 3. Verify Logging Output
You should see structured JSON logs like:
```json
{"level":"info","msg":"MIGlet starting","time":"2024-01-15T10:30:00Z","version":"dev"}
{"level":"info","component":"miglet","msg":"Configuration loaded successfully","pool_id":"gcp-pool-001","vm_id":"gcp-vm-xxx","org_id":"gcp-org-001","time":"2024-01-15T10:30:00Z"}
{"level":"info","component":"miglet","msg":"MIGlet initialized with context","org_id":"gcp-org-001","pool_id":"gcp-pool-001","vm_id":"gcp-vm-xxx","time":"2024-01-15T10:30:00Z"}
```

#### 4. Test Signal Handling
```bash
# Start MIGlet in background
./miglet --config miglet.yaml &
PID=$!

# Wait a moment
sleep 2

# Send SIGTERM
kill $PID

# Verify graceful shutdown in logs
```

### Expected Behavior

✅ **Success Criteria:**
- MIGlet starts without errors
- Configuration loads correctly (from env or file)
- Structured JSON logs are emitted
- Logs include VM context (vm_id, pool_id, org_id)
- Graceful shutdown on SIGTERM/SIGINT
- Version flag works

❌ **Failure Cases to Test:**
- Missing required config (should fail with clear error)
- Invalid config file (should fail with clear error)
- Invalid log level (should default to info)

## Next Steps (Phase 2)

After Phase 1 is verified:
1. Implement MIG Controller HTTP client
2. Add registration token request flow
3. Test controller communication

## Troubleshooting

### Build Errors
- Ensure Go 1.21+ is installed: `go version`
- Check dependencies: `go mod tidy`

### Config Loading Errors
- Verify environment variable names: `env | grep MIGLET`
- Check config file syntax: `cat miglet.yaml`
- Enable debug logging: `MIGLET_LOGGING_LEVEL=debug`

### Logging Issues
- Test with text format: `--log-format text`
- Check stdout/stderr redirection
- Verify JSON format is valid

