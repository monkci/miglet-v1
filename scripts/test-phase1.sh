#!/bin/bash
# Test script for Phase 1 - Basic setup
# This script helps test MIGlet in a GCP VM

set -e

echo "=== MIGlet Phase 1 Test ==="
echo ""

# Build MIGlet
echo "Building MIGlet..."
go build -o bin/miglet ./cmd/miglet
echo "✓ Build successful"
echo ""

# Test 1: Version flag
echo "Test 1: Version flag"
./bin/miglet --version
echo ""

# Test 2: Help/usage
echo "Test 2: Help/usage"
./bin/miglet --help || true
echo ""

# Test 3: Run with minimal config (env vars)
echo "Test 3: Run with environment variables"
export MIGLET_POOL_ID="test-pool-123"
export MIGLET_VM_ID="test-vm-456"
export MIGLET_ORG_ID="test-org-789"
export MIGLET_CONTROLLER_ENDPOINT="https://test-controller.example.com"
export MIGLET_GITHUB_ORG="testorg"
export MIGLET_LOGGING_LEVEL="debug"
export MIGLET_LOGGING_FORMAT="text"

echo "Starting MIGlet with env vars (will run for 3 seconds)..."
timeout 3 ./bin/miglet || true
echo ""

# Test 4: Run with config file
echo "Test 4: Run with config file"
if [ -f "configs/miglet.yaml.example" ]; then
    # Create a test config
    cp configs/miglet.yaml.example /tmp/test-miglet.yaml
    sed -i 's/pool-123/test-pool-123/g' /tmp/test-miglet.yaml
    sed -i 's/vm-456/test-vm-456/g' /tmp/test-miglet.yaml
    sed -i 's/org-789/test-org-789/g' /tmp/test-miglet.yaml
    
    echo "Starting MIGlet with config file (will run for 3 seconds)..."
    timeout 3 ./bin/miglet --config /tmp/test-miglet.yaml || true
    echo ""
fi

# Test 5: Missing required config
echo "Test 5: Missing required configuration (should fail)"
unset MIGLET_POOL_ID
./bin/miglet 2>&1 | head -5 || true
echo ""

echo "=== Phase 1 Tests Complete ==="
echo ""
echo "Next steps:"
echo "1. Deploy to GCP VM"
echo "2. Test with real metadata server"
echo "3. Verify logging output format"

