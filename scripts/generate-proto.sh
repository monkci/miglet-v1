#!/bin/bash
# Generate Go code from protobuf definitions

set -e

PROTO_DIR="proto"
OUT_DIR="proto/commands"

# Ensure GOPATH/bin is in PATH for protoc plugins
export PATH="$PATH:$(go env GOPATH)/bin"

# Create output directory
mkdir -p "$OUT_DIR"

# Check if protoc is installed
if ! command -v protoc &> /dev/null; then
    echo "Error: protoc is not installed"
    echo "Install it from: https://grpc.io/docs/protoc-installation/"
    echo ""
    echo "Or on macOS: brew install protobuf"
    echo "Or on Ubuntu: sudo apt-get install protobuf-compiler"
    exit 1
fi

# Check if protoc-gen-go is installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Error: protoc-gen-go is not installed"
    echo "Install it with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
fi

# Check if protoc-gen-go-grpc is installed
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Error: protoc-gen-go-grpc is not installed"
    echo "Install it with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    exit 1
fi

# Generate Go code
echo "Generating Go code from protobuf..."
echo "Using protoc: $(which protoc)"
echo "Using protoc-gen-go: $(which protoc-gen-go)"
echo "Using protoc-gen-go-grpc: $(which protoc-gen-go-grpc)"
echo ""

protoc \
    --go_out="$OUT_DIR" \
    --go_opt=paths=source_relative \
    --go-grpc_out="$OUT_DIR" \
    --go-grpc_opt=paths=source_relative \
    "$PROTO_DIR/commands.proto"

echo ""
echo "✅ Generated files in $OUT_DIR/"
ls -la "$OUT_DIR/"

