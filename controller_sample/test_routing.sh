#!/bin/bash
# Quick test to verify controller routing

echo "Testing controller routing..."
echo ""

# Test events endpoint
echo "1. Testing /events endpoint:"
curl -X POST http://localhost:8080/api/v1/vms/test-vm-123/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "vm_started",
    "vm_id": "test-vm-123",
    "pool_id": "test-pool"
  }' 2>&1
echo ""
echo ""

# Test health endpoint
echo "2. Testing /health endpoint:"
curl http://localhost:8080/health 2>&1
echo ""
echo ""

echo "Check controller logs above to see routing decisions"

