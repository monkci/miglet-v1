# Local Testing Guide: MIGlet + Sample Controller

This guide explains how to test MIGlet with the sample controller on the same VM.

---

## Prerequisites

### 1. Build Both Binaries

```bash
cd /path/to/miglet-v1

# Build MIGlet
go build -o bin/miglet ./cmd/miglet

# Build Sample Controller
cd controller_sample
go build -o controller .
cd ..
```

### 2. Verify Builds

```bash
ls -la bin/miglet
ls -la controller_sample/controller
```

---

## Testing Steps

### Step 1: Start the Sample Controller

Open **Terminal 1** and start the controller:

```bash
cd /path/to/miglet-v1/controller_sample
./controller
```

Expected output:
```
Sample MIG Controller starting:
  HTTP server on port 8080
  gRPC server on port 50051
  Data will be stored in: ./controller_data
  Registration token (hardcoded): BUS6FEZFNVKF4XNUHMWZRNTJFGCHW
gRPC server starting on port 50051
```

The controller is now listening on:
- **HTTP**: `localhost:8080` (for initial VM started event)
- **gRPC**: `localhost:50051` (for bidirectional streaming)

---

### Step 2: Start MIGlet

Open **Terminal 2** and set environment variables:

```bash
cd /path/to/miglet-v1

# Required configuration
export MIGLET_POOL_ID="test-pool-001"
export MIGLET_VM_ID="test-vm-001"
export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"

# Optional: Set logging
export MIGLET_LOGGING_LEVEL="debug"
export MIGLET_LOGGING_FORMAT="text"

# Start MIGlet
./bin/miglet
```

---

### Step 3: Observe the Flow

#### In Terminal 1 (Controller), you should see:

```
# HTTP: VM started event received
Request: POST /api/v1/vms/test-vm-001/events
Extracted VM ID: test-vm-001 from path: /api/v1/vms/test-vm-001/events
Routing to events handler
Event from VM test-vm-001: type=vm_started
Acknowledging VM started event - VM: test-vm-001, Pool: test-pool-001, Org: 

# gRPC: VM connecting
VM test-vm-001 (Pool: test-pool-001, Org: ) connecting via gRPC
VM test-vm-001 added to connection pool (total: 1)
Connection accepted for VM test-vm-001

# gRPC: Registration command sent
Register runner command sent to VM test-vm-001

# gRPC: Command acknowledgment received
Received command ack from VM test-vm-001: command_id=register-test-vm-001-..., success=true

# gRPC: Events and heartbeats
Received event from VM test-vm-001: type=runner_registered
Received heartbeat from VM test-vm-001: runner_state=idle, cpu=...%, memory=...%
```

#### In Terminal 2 (MIGlet), you should see:

```
INFO  MIGlet starting
INFO  Configuration loaded successfully
INFO  MIGlet initialized with context
INFO  State machine starting

# Initializing state
INFO  Initializing MIGlet
INFO  Installing GitHub Actions runner
INFO  State transition: initializing → waiting_for_controller

# Waiting for controller
INFO  Sending VM started event to controller
INFO  Controller acknowledged VM started event
INFO  State transition: waiting_for_controller → ready

# Ready state - gRPC connection
INFO  MIGlet is ready - connecting to controller via gRPC for commands
INFO  Connecting to controller via gRPC
INFO  gRPC connection established, waiting for commands

# Registration command received
INFO  Received command from controller via gRPC: type=register_runner
INFO  Registration config received, transitioning to registering runner
INFO  State transition: ready → registering_runner

# Runner registration
INFO  Starting GitHub Actions runner registration
INFO  Configuring runner with token
INFO  Starting runner process
INFO  GitHub Actions runner started successfully
INFO  State transition: registering_runner → idle

# Idle state - heartbeats
DEBUG Heartbeat sent to controller via gRPC successfully
```

---

### Step 4: Verify Data Storage

Check the data stored by the controller:

```bash
ls -la controller_sample/controller_data/test-vm-001/
```

You should see files like:
```
event-vm_started-20241205-123456.789.json
grpc-event-runner_registered-20241205-123457.123.json
grpc-heartbeat-20241205-123500.456.json
grpc-heartbeat-20241205-123530.789.json
...
```

View an event:
```bash
cat controller_sample/controller_data/test-vm-001/event-vm_started-*.json
```

View a heartbeat:
```bash
cat controller_sample/controller_data/test-vm-001/grpc-heartbeat-*.json | head -50
```

---

### Step 5: Test Graceful Shutdown

In Terminal 2, press `Ctrl+C` to stop MIGlet.

Expected behavior:
- MIGlet receives shutdown signal
- Sends shutdown event (if configured)
- Closes gRPC connection
- Exits gracefully

In Terminal 1, you should see:
```
Stream closed for VM test-vm-001: ...
VM test-vm-001 removed from connection pool (total: 0)
```

---

## Testing Scenarios

### Scenario 1: Controller Not Running

Start MIGlet without the controller:

```bash
export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
./bin/miglet
```

Expected: MIGlet retries sending VM started event, eventually transitions to error state.

### Scenario 2: Controller Restart

1. Start controller
2. Start MIGlet
3. Stop controller (`Ctrl+C`)
4. Observe MIGlet reconnection attempts
5. Restart controller
6. Observe MIGlet reconnects and continues

### Scenario 3: Multiple MIGlets

Run multiple MIGlet instances with different VM IDs:

**Terminal 2:**
```bash
export MIGLET_VM_ID="test-vm-001"
./bin/miglet
```

**Terminal 3:**
```bash
export MIGLET_VM_ID="test-vm-002"
./bin/miglet
```

Controller should show both VMs connected.

---

## Troubleshooting

### Issue: "connection refused" on gRPC

**Cause**: gRPC server not running or wrong port.

**Solution**: 
- Verify controller is running
- Check gRPC server started on port 50051
- Verify no firewall blocking

### Issue: "config validation failed: pool_id is required"

**Cause**: Environment variables not set.

**Solution**:
```bash
export MIGLET_POOL_ID="test-pool-001"
export MIGLET_VM_ID="test-vm-001"
```

### Issue: VM started event returns 404

**Cause**: HTTP routing issue in controller.

**Solution**: Check controller logs for the actual path being requested.

### Issue: Runner registration fails

**Cause**: GitHub Actions runner binary not installed or invalid token.

**Solution**: 
- Check if runner was installed during initialization
- Verify the hardcoded token in controller matches what's expected
- Check runner logs in MIGlet output

### Issue: No heartbeats received

**Cause**: gRPC stream not established or MIGlet stuck in a state.

**Solution**:
- Check MIGlet logs for current state
- Verify gRPC connection was accepted
- Check for errors in stream loop

---

## Quick Test Script

Create a test script:

```bash
#!/bin/bash
# test-local.sh

# Terminal 1: Start controller
# Run this in a separate terminal:
# cd controller_sample && ./controller

# Wait for controller to start
sleep 2

# Set environment
export MIGLET_POOL_ID="test-pool-001"
export MIGLET_VM_ID="test-vm-$(date +%s)"
export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
export MIGLET_LOGGING_LEVEL="debug"
export MIGLET_LOGGING_FORMAT="text"

# Start MIGlet
./bin/miglet
```

---

## Expected State Flow

```
1. Initializing
   - Load configuration
   - Download and install runner binary
   - Transition to WaitingForController

2. WaitingForController
   - Send vm_started event via HTTP
   - Receive acknowledgment
   - Transition to Ready

3. Ready
   - Connect to gRPC server
   - Send ConnectRequest
   - Receive ConnectAck
   - Wait for commands
   - Receive register_runner command
   - Transition to RegisteringRunner

4. RegisteringRunner
   - Configure runner with token
   - Start runner process
   - Send runner_registered event
   - Transition to Idle

5. Idle
   - Send periodic heartbeats
   - Monitor runner process
   - Wait for jobs (in real scenario)
```

---

## Ports Reference

| Service | Port | Protocol | Purpose |
|---------|------|----------|---------|
| Controller HTTP | 8080 | HTTP | VM started events, fallback |
| Controller gRPC | 50051 | gRPC | Bidirectional streaming |

---

## Log Levels

For debugging, use:
```bash
export MIGLET_LOGGING_LEVEL="debug"
```

For production-like output:
```bash
export MIGLET_LOGGING_LEVEL="info"
```

---

## Clean Up

To reset and test again:

```bash
# Stop MIGlet (Ctrl+C)
# Stop Controller (Ctrl+C)

# Clear stored data
rm -rf controller_sample/controller_data/*

# Clear runner installation (if needed)
rm -rf /tmp/miglet-runner

# Restart testing
```

