# MIGlet Quick Start - Phase 1

## What's Implemented ✅

Phase 1 includes the foundational components:
- Go module and project structure
- Structured logger (JSON/text formats)
- Configuration management (env vars + YAML file)
- Basic CLI with version, config, and log flags
- Signal handling for graceful shutdown

## Quick Test (Local)

```bash
# 1. Build
go build -o bin/miglet ./cmd/miglet

# 2. Test version
./bin/miglet --version

# 3. Test with env vars
export MIGLET_POOL_ID="test-pool"
export MIGLET_VM_ID="test-vm"
export MIGLET_ORG_ID="test-org"
export MIGLET_CONTROLLER_ENDPOINT="https://test.example.com"
export MIGLET_GITHUB_ORG="testorg"

./bin/miglet --log-format text --log-level debug
```

## Testing in GCP VM

### Step 1: Transfer Files
```bash
# Build locally
go build -o bin/miglet ./cmd/miglet

# Transfer to GCP VM
gcloud compute scp bin/miglet <instance-name>:~/miglet --zone <zone>
```

### Step 2: Test on VM
```bash
# SSH into VM
gcloud compute ssh <instance-name> --zone <zone>

# Set environment variables
export MIGLET_POOL_ID="gcp-pool-001"
export MIGLET_VM_ID="gcp-vm-$(hostname)"
export MIGLET_ORG_ID="gcp-org-001"
export MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io"
export MIGLET_GITHUB_ORG="your-org"
export MIGLET_LOGGING_LEVEL="info"
export MIGLET_LOGGING_FORMAT="json"

# Run MIGlet
./miglet
```

### Step 3: Verify Output
You should see structured JSON logs:
```json
{"level":"info","msg":"MIGlet starting","time":"...","version":"dev"}
{"level":"info","component":"miglet","msg":"Configuration loaded successfully",...}
{"level":"info","component":"miglet","msg":"MIGlet initialized with context",...}
```

Press `Ctrl+C` to test graceful shutdown.

## Configuration Options

### Environment Variables
All config can be set via `MIGLET_*` environment variables:
- `MIGLET_POOL_ID` - Pool identifier (required)
- `MIGLET_VM_ID` - VM identifier (required)
- `MIGLET_ORG_ID` - Organization identifier (required)
- `MIGLET_CONTROLLER_ENDPOINT` - Controller URL (required)
- `MIGLET_GITHUB_ORG` - GitHub organization (required)
- `MIGLET_LOGGING_LEVEL` - Log level: debug, info, warn, error
- `MIGLET_LOGGING_FORMAT` - Log format: json, text

### Config File
See `configs/miglet.yaml.example` for full configuration options.

## What's Next

After Phase 1 is tested and working:
- **Phase 2**: MIG Controller client and registration token flow
- **Phase 3**: GitHub runner registration
- **Phase 4**: Job lifecycle monitoring

See `docs/PHASE1_TESTING.md` for detailed testing instructions.

