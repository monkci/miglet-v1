#!/bin/bash
# Quick test script for the sample controller

echo "=== Testing Sample MIG Controller ==="
echo ""

# Start controller in background
echo "Starting controller..."
./controller &
CONTROLLER_PID=$!
sleep 2

# Test health endpoint
echo "1. Testing health endpoint..."
curl -s http://localhost:8080/health
echo ""
echo ""

# Test registration token request
echo "2. Testing registration token request..."
curl -s -X POST http://localhost:8080/api/v1/vms/test-vm-001/registration-token \
  -H "Content-Type: application/json" \
  -d '{
    "org_id": "test-org",
    "pool_id": "test-pool",
    "runner_group": "default",
    "labels": ["self-hosted", "linux"]
  }' | jq .
echo ""

# Test event
echo "3. Testing event..."
curl -s -X POST http://localhost:8080/api/v1/vms/test-vm-001/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "vm_started",
    "vm_id": "test-vm-001",
    "pool_id": "test-pool",
    "timestamp": "2024-01-15T10:30:00Z"
  }' | jq .
echo ""

# Test heartbeat
echo "4. Testing heartbeat..."
curl -s -X POST http://localhost:8080/api/v1/vms/test-vm-001/heartbeat \
  -H "Content-Type: application/json" \
  -d '{
    "vm_id": "test-vm-001",
    "cpu_load": 0.5,
    "memory_usage": 0.6,
    "runner_state": "idle"
  }' | jq .
echo ""

# Test commands
echo "5. Testing commands..."
curl -s http://localhost:8080/api/v1/vms/test-vm-001/commands | jq .
echo ""

# Show stored data
echo "6. Stored data files:"
ls -la controller_data/test-vm-001/ 2>/dev/null || echo "No data stored yet"
echo ""

# Stop controller
echo "Stopping controller..."
kill $CONTROLLER_PID 2>/dev/null
wait $CONTROLLER_PID 2>/dev/null

echo "=== Test Complete ==="

