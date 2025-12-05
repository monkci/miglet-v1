# gRPC Server Implementation in Sample Controller

## Overview

The sample controller now includes a gRPC server that implements bidirectional streaming for command delivery and event/heartbeat reception.

## Architecture

```
MIGlet ←─── gRPC Stream ───→ Controller (gRPC Server)
  ↑                           ↓
  └── Commands pushed ────────┘
  └── Events/Heartbeats ──────┘
```

## Implementation Details

### Server Structure

The gRPC server (`grpc_server.go`) includes:

1. **GRPCServer** - Main server struct that implements `CommandServiceServer`
2. **VMConnection** - Tracks each connected VM's stream and metadata
3. **Connection Management** - Tracks active connections and handles disconnections
4. **Command Queue** - Queues commands for VMs that are temporarily offline

### Key Features

#### 1. Bidirectional Streaming
- MIGlet connects and sends `ConnectRequest`
- Controller responds with `ConnectAck`
- Controller can send commands at any time
- MIGlet can send events, heartbeats, and acks at any time

#### 2. Automatic Command Delivery
- When a VM connects, controller automatically sends `register_runner` command
- Commands are queued if VM is not connected
- Queued commands are sent when VM reconnects

#### 3. Message Processing
- **ConnectRequest**: Registers VM connection, sends ack, triggers registration command
- **CommandAck**: Logs acknowledgment from MIGlet
- **EventNotification**: Stores events to disk, handles `vm_started` events
- **Heartbeat**: Stores heartbeats to disk with metrics
- **ErrorNotification**: Logs errors from MIGlet

#### 4. Data Storage
- Events stored as: `grpc-event-{type}-{timestamp}.json`
- Heartbeats stored as: `grpc-heartbeat-{timestamp}.json`
- All stored in `controller_data/{vm_id}/` directory

## Flow

### Connection Flow

```
1. MIGlet → ConnectRequest
2. Controller → ConnectAck (accepted)
3. Controller → register_runner Command
4. MIGlet → CommandAck
5. MIGlet → EventNotification (runner_registered)
6. MIGlet → Heartbeat (periodic)
```

### Command Delivery

```
1. Controller creates Command
2. Controller.SendCommand(vmID, command)
3. If VM connected: Send immediately via stream
4. If VM offline: Queue command
5. When VM reconnects: Send queued commands
```

## Usage

### Start Controller

```bash
cd controller_sample
go run main.go grpc_server.go
```

Or build:
```bash
go build -o controller .
./controller
```

### Configuration

The controller runs:
- **HTTP server** on port `8080` (for backward compatibility)
- **gRPC server** on port `50051` (for bidirectional streaming)

### Testing with MIGlet

1. Set MIGlet config:
   ```bash
   export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
   ```

2. MIGlet will:
   - Send `vm_started` event via HTTP (for ack)
   - Connect to gRPC server on port `50051`
   - Receive `register_runner` command via gRPC
   - Send all subsequent events/heartbeats via gRPC

## Code Structure

### Main Components

1. **GRPCServer.StreamCommands()** - Handles bidirectional streaming
2. **sendRegisterRunnerCommand()** - Sends registration command to VM
3. **SendCommand()** - Sends any command to a connected VM
4. **queueCommand()** - Queues commands for offline VMs
5. **storeGRPCEvent()** - Stores events to disk
6. **storeGRPCHeartbeat()** - Stores heartbeats to disk

### Connection Tracking

```go
connections map[string]*VMConnection  // vmID -> connection
commandQueue map[string][]*Command    // vmID -> pending commands
```

### Thread Safety

- All connection operations use `sync.RWMutex`
- Command queue uses separate mutex
- Safe for concurrent access

## Example Output

```
Sample MIG Controller starting:
  HTTP server on port 8080
  gRPC server on port 50051
  Data will be stored in: ./controller_data
  Registration token (hardcoded): BUS6FEZFNVKF4XNUHMWZRNTJFGCHW
gRPC server starting on port 50051
VM test-vm-001 (Pool: test-pool-001, Org: ) connecting via gRPC
VM test-vm-001 added to connection pool (total: 1)
Connection accepted for VM test-vm-001
Register runner command sent to VM test-vm-001
Received command ack from VM test-vm-001: command_id=register-test-vm-001-1234567890, success=true
Received event from VM test-vm-001: type=runner_registered
Received heartbeat from VM test-vm-001: runner_state=idle, cpu=2.50%, memory=45.30%
```

## Next Steps

1. **Add TLS support** - Currently using insecure credentials
2. **Add command types** - Implement drain, shutdown, update_config commands
3. **Add metrics** - Track connection count, command delivery rate
4. **Add persistence** - Store connection state across restarts
5. **Add authentication** - Validate VM identity before accepting connections

## Testing

To test the full flow:

1. Start controller:
   ```bash
   cd controller_sample
   go run main.go grpc_server.go
   ```

2. Start MIGlet:
   ```bash
   export MIGLET_POOL_ID="test-pool-001"
   export MIGLET_VM_ID="test-vm-001"
   export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
   ./bin/miglet
   ```

3. Verify:
   - MIGlet connects via gRPC
   - Controller sends `register_runner` command
   - MIGlet registers runner and sends events
   - Heartbeats flow through gRPC stream
   - All data stored in `controller_data/` directory

