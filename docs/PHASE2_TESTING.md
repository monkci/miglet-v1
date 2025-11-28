# Phase 2 Testing Guide - WaitingForControllerAck State

## Overview
Phase 2 implements the `WaitingForControllerAck` state where MIGlet:
1. Sends a "VM started" event to the MIG Controller
2. Waits for acknowledgment with retry logic
3. Transitions to `Idle` state on success, or `Error` state on failure

## What's Implemented

### MIGlet Side
- ✅ State machine with `WaitingForController` state
- ✅ Controller client with `SendVMStartedEvent` method
- ✅ Retry logic with exponential backoff
- ✅ Proper acknowledgment detection
- ✅ State transition on success/failure

### Controller Side (Sample)
- ✅ Receives VM started events
- ✅ Stores events in `controller_data/{vm_id}/`
- ✅ Sends explicit acknowledgment for VM started events
- ✅ Returns proper JSON response with `acknowledged: true`

## Testing Flow

### Step 1: Start the Sample Controller

```bash
cd controller_sample
go run main.go
# or
./controller
```

The controller will:
- Start on port 8080
- Log all incoming requests
- Store data in `controller_data/` directory

### Step 2: Configure MIGlet

Set environment variables:
```bash
export MIGLET_POOL_ID="test-pool-001111"
export MIGLET_VM_ID="test-vm-001111"
export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
export MIGLET_LOGGING_LEVEL="debug"
export MIGLET_LOGGING_FORMAT="text"
```

**Note:** `org_id` and `github.org` are now optional for boot.

### Step 3: Run MIGlet

```bash
./bin/miglet
```

### Expected Behavior

#### MIGlet Logs:
```
[INFO] MIGlet starting
[INFO] Configuration loaded successfully
[INFO] MIGlet initialized with context
[INFO] State machine starting
[INFO] Initializing MIGlet
[INFO] State transition: initializing -> waiting_for_controller
[INFO] Sending VM started event to controller
[DEBUG] Sending VM started event (url: http://localhost:8080/api/v1/vms/test-vm-001/events)
[INFO] Controller acknowledged VM started event
[INFO] State transition: waiting_for_controller -> idle
[INFO] MIGlet ready (Phase 2 complete)
```

#### Controller Logs:
```
2024/01/15 10:30:00 Sample MIG Controller starting on port 8080
2024/01/15 10:30:00 Data will be stored in: ./controller_data
2024/01/15 10:30:05 Request: POST /api/v1/vms/test-vm-001/events
2024/01/15 10:30:05 Event from VM test-vm-001: type=vm_started
2024/01/15 10:30:05 Acknowledging VM started event - VM: test-vm-001, Pool: test-pool-001, Org: 
2024/01/15 10:30:05 Stored data: ./controller_data/test-vm-001/event-vm_started-20240115-103005.000.json
2024/01/15 10:30:05 Sent acknowledgment for VM test-vm-001
```

#### Stored Data:
Check `controller_data/test-vm-001/event-vm_started-*.json`:
```json
{
  "type": "vm_started",
  "timestamp": "2024-01-15T10:30:05Z",
  "vm_id": "test-vm-001",
  "pool_id": "test-pool-001",
  "org_id": "",
  "version": "dev",
  "build_time": "unknown"
}
```

## Testing Scenarios

### Scenario 1: Successful Acknowledgment ✅
- Controller is running
- MIGlet sends event
- Controller responds with `acknowledged: true`
- MIGlet transitions to `Idle`

### Scenario 2: Controller Not Running (Retry)
- Controller is not running
- MIGlet sends event (fails)
- MIGlet retries with exponential backoff
- After max retries, transitions to `Error` state

**Expected logs:**
```
[WARN] Failed to send VM started event: connection refused
[INFO] Retrying VM started event (attempt: 2)
[WARN] Failed to send VM started event: connection refused
...
[ERROR] Failed to get controller acknowledgment after retries
[INFO] State transition: waiting_for_controller -> error
```

### Scenario 3: Controller Returns Error
- Controller is running but returns error status
- MIGlet retries
- After max retries, transitions to `Error` state

### Scenario 4: Controller Returns Non-Acknowledgment
- Controller responds but without `acknowledged: true`
- MIGlet retries
- After max retries, transitions to `Error` state

## Manual Testing

### Test Controller Endpoint Directly

```bash
# Send VM started event
curl -X POST http://localhost:8080/api/v1/vms/test-vm-001/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "vm_started",
    "timestamp": "2024-01-15T10:30:00Z",
    "vm_id": "test-vm-001",
    "pool_id": "test-pool-001",
    "org_id": "test-org-001"
  }'
```

**Expected Response:**
```json
{
  "status": "acknowledged",
  "acknowledged": true,
  "vm_id": "test-vm-001",
  "pool_id": "test-pool-001",
  "org_id": "test-org-001",
  "message": "VM started event acknowledged",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

## Verification Checklist

- [ ] Controller starts successfully
- [ ] MIGlet connects to controller
- [ ] VM started event is sent
- [ ] Event is stored in `controller_data/`
- [ ] Controller sends acknowledgment
- [ ] MIGlet receives acknowledgment
- [ ] MIGlet transitions to `Idle` state
- [ ] Retry logic works when controller is down
- [ ] Error state is reached after max retries

## Troubleshooting

### MIGlet Can't Connect to Controller
- Check controller is running: `curl http://localhost:8080/health`
- Check endpoint in MIGlet config: `MIGLET_CONTROLLER_ENDPOINT`
- Check firewall/network settings

### Controller Not Acknowledging
- Check controller logs for errors
- Verify event is being received
- Check response format matches expected format

### MIGlet Stuck in WaitingForController
- Check if event was sent (controller logs)
- Check if acknowledgment was sent (controller logs)
- Enable debug logging: `MIGLET_LOGGING_LEVEL=debug`

### State Machine Not Transitioning
- Check state machine logs
- Verify acknowledgment format matches what client expects
- Check for errors in state handler

## Next Steps (Phase 3)

After Phase 2 is verified:
1. Implement GitHub runner registration
2. Request registration token from controller
3. Register runner with GitHub
4. Transition to `Idle` state with runner ready
