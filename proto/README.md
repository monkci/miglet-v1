# Protobuf Definitions

## Generating Go Code

To generate Go code from the protobuf definitions, you need `protoc` installed:

### Install protoc

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

### Generate Code

```bash
./scripts/generate-proto.sh
```

Or manually:
```bash
protoc \
    --go_out=proto/commands \
    --go_opt=paths=source_relative \
    --go-grpc_out=proto/commands \
    --go-grpc_opt=paths=source_relative \
    proto/commands.proto
```

This will generate:
- `proto/commands/commands.pb.go` - Message types
- `proto/commands/commands_grpc.pb.go` - gRPC service interfaces

## Current Status

The proto definitions are in `proto/commands.proto`. The Go code needs to be generated using `protoc`.

For now, the implementation uses placeholder interfaces that match the proto structure. Once the code is generated, update the imports in:
- `pkg/controller/grpc_client.go`
- `controller_sample/grpc_server.go` (to be created)

