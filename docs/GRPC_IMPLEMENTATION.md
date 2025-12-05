# gRPC Bidirectional Streaming Implementation

## Overview

We're implementing gRPC bidirectional streaming for command communication between the MIG Controller and MIGlet. This allows the controller to push commands in real-time to MIGlet, and MIGlet to send events and heartbeats back.

## Architecture

```
MIGlet ←─── gRPC Stream ───→ Controller
  ↑                           ↓
  └── Commands pushed ────────┘
  └── Events/Heartbeats ──────┘
```

## Current Status

### ✅ Completed

1. **Protobuf Definitions** (`proto/commands.proto`)
   - Defined bidirectional streaming service
   - Defined all message types (Command, CommandAck, EventNotification, Heartbeat, etc.)
   - Ready for code generation

2. **Message Types** (`proto/commands/messages.go`)
   - Manually created Go structs matching proto definitions
   - All message types implemented
   - Helper methods for timestamps

3. **Service Interfaces** (`proto/commands/service.go`)
   - Client and server interfaces defined
   - Stream interfaces for bidirectional communication

4. **gRPC Client** (`pkg/controller/grpc_client.go`)
   - Client structure with connection management
   - Reconnection logic
   - Command channel for receiving commands
   - Methods for sending acks, events, heartbeats
   - Stream loop implementation (needs proto-generated client)

### ⚠️ Pending (Requires protoc)

1. **Proto Code Generation**
   - Need to install `protoc` compiler
   - Run `./scripts/generate-proto.sh` to generate:
     - `proto/commands/commands.pb.go` - Message types
     - `proto/commands/commands_grpc.pb.go` - gRPC service client/server

2. **Complete gRPC Client**
   - Update `createStream()` to use generated client
   - Replace placeholder with actual gRPC client call

3. **State Machine Integration**
   - Update `Ready` state to use gRPC instead of HTTP polling
   - Connect to gRPC stream when entering Ready state
   - Process commands from gRPC channel

4. **gRPC Server** (Controller)
   - Implement `CommandServiceServer` interface
   - Handle incoming MIGlet connections
   - Push commands to connected MIGlets
   - Process events and heartbeats from MIGlet

## Installation Steps

### 1. Install protoc

**macOS:**
```bash
brew install protobuf
```

**Ubuntu/Debian:**
```bash
sudo apt-get install protobuf-compiler
```

**Or download from:**
https://grpc.io/docs/protoc-installation/

### 2. Generate Proto Code

```bash
./scripts/generate-proto.sh
```

This will generate:
- `proto/commands/commands.pb.go`
- `proto/commands/commands_grpc.pb.go`

### 3. Update Imports

Once generated, update:
- `pkg/controller/grpc_client.go` - Use generated client
- `controller_sample/grpc_server.go` - Use generated server

## Implementation Details

### Message Flow

1. **Connection:**
   ```
   MIGlet → ConnectRequest → Controller
   Controller → ConnectAck → MIGlet
   ```

2. **Command Delivery:**
   ```
   Controller → Command → MIGlet
   MIGlet → CommandAck → Controller
   ```

3. **Events:**
   ```
   MIGlet → EventNotification → Controller
   ```

4. **Heartbeats:**
   ```
   MIGlet → Heartbeat → Controller
   ```

### Command Types

- `register_runner` - Register GitHub Actions runner
- `drain` - Stop accepting new jobs
- `shutdown` - Shutdown VM
- `update_config` - Update runtime configuration
- `set_log_level` - Change logging verbosity

### Reconnection Logic

- Automatic reconnection on stream errors
- Exponential backoff (5s initial, max 30s)
- Connection state tracking
- Graceful shutdown support

## Next Steps

1. **Install protoc** and generate code
2. **Complete gRPC client** - Update `createStream()` method
3. **Update state machine** - Use gRPC in `Ready` state
4. **Implement gRPC server** - Controller side
5. **Test end-to-end** - Verify command delivery

## Testing

Once implemented, test flow:

1. Start controller with gRPC server on port 50051
2. Start MIGlet - should connect via gRPC
3. Controller sends `register_runner` command
4. MIGlet receives command, processes it, sends ack
5. Verify bidirectional communication works

## Configuration

### MIGlet Config

```yaml
controller:
  endpoint: "http://controller:8080"  # HTTP endpoint (for events/heartbeats)
  grpc_endpoint: "controller:50051"   # gRPC endpoint (for commands)
```

### Controller Config

```yaml
grpc:
  port: 50051
  tls:
    enabled: false  # TODO: Add TLS support
```

## Notes

- Currently using insecure credentials (no TLS)
- Should add TLS support for production
- gRPC endpoint should be separate from HTTP endpoint
- Consider using same port with HTTP/2 for both protocols

