#!/bin/bash
# GCP Startup Script for MIGlet
# This script runs when the VM boots up

set -e

MIGLET_DIR="/opt/miglet"
LOG_FILE="/var/log/miglet-startup.log"

# Log everything
exec > >(tee -a "$LOG_FILE") 2>&1

echo "$(date): Starting MIGlet installation..."

# Install Go if not present (for building)
if ! command -v go &> /dev/null; then
    echo "Installing Go..."
    wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    rm go1.21.5.linux-amd64.tar.gz
fi

# Create MIGlet directory
mkdir -p "$MIGLET_DIR/bin"
mkdir -p "$MIGLET_DIR/configs"

# Get MIGlet binary (options):
# Option 1: Download from artifact repository
# wget -O "$MIGLET_DIR/bin/miglet" https://your-artifact-repo/miglet
# chmod +x "$MIGLET_DIR/bin/miglet"

# Option 2: Build from source (if code is available)
# cd /tmp
# git clone https://github.com/monkci/miglet-v1.git
# cd miglet-v1
# go build -o "$MIGLET_DIR/bin/miglet" ./cmd/miglet

# Option 3: Copy from GCS bucket
# gsutil cp gs://your-bucket/miglet "$MIGLET_DIR/bin/miglet"
# chmod +x "$MIGLET_DIR/bin/miglet"

# Set environment variables from metadata
# GCP metadata can be accessed via: curl -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/KEY
POOL_ID=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/pool-id || echo "")
VM_ID=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/name || echo "")
CONTROLLER_ENDPOINT=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/controller-endpoint || echo "http://localhost:8080")

# Create environment file
cat > /etc/miglet/miglet.env <<EOF
MIGLET_POOL_ID="$POOL_ID"
MIGLET_VM_ID="$VM_ID"
MIGLET_CONTROLLER_ENDPOINT="$CONTROLLER_ENDPOINT"
MIGLET_LOGGING_LEVEL="info"
MIGLET_LOGGING_FORMAT="text"
EOF

# Install systemd service
if [ -f "$MIGLET_DIR/deploy/systemd/miglet.service" ]; then
    cp "$MIGLET_DIR/deploy/systemd/miglet.service" /etc/systemd/system/miglet.service
    systemctl daemon-reload
    systemctl enable miglet.service
    systemctl start miglet.service
    echo "$(date): MIGlet service started"
else
    # Fallback: Run directly (not recommended for production)
    nohup "$MIGLET_DIR/bin/miglet" > /var/log/miglet.log 2>&1 &
    echo "$(date): MIGlet started in background"
fi

echo "$(date): MIGlet startup script completed"

