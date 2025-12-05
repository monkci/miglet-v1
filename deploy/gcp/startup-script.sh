#!/bin/bash
# GCP Startup Script for MIGlet
set -euo pipefail

LOG_FILE="/var/log/miglet-startup.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "$(date): MIGlet startup script started"

### ------------------------------------------------------------
### 1. Create required directories
### ------------------------------------------------------------
INSTALL_DIR="/opt/miglet"
BIN_DIR="$INSTALL_DIR/bin"
CONFIG_DIR="/etc/miglet"

mkdir -p "$BIN_DIR"
mkdir -p "$CONFIG_DIR"

echo "Created MIGlet directories."

### ------------------------------------------------------------
### 2. Download MIGlet binary from GCS
### ------------------------------------------------------------
GCS_BINARY="gs://miglet-v1/releases/miglet-v1.0.0"
TARGET_BIN="$BIN_DIR/miglet"

echo "Downloading MIGlet binary from $GCS_BINARY"

gsutil cp "$GCS_BINARY" "$TARGET_BIN"
chmod +x "$TARGET_BIN"

echo "MIGlet binary installed at $TARGET_BIN"

### ------------------------------------------------------------
### 3. Read VM metadata
### ------------------------------------------------------------
METADATA_URL="http://metadata.google.internal/computeMetadata/v1/instance/attributes"

POOL_ID=$(curl -s -H "Metadata-Flavor: Google" "$METADATA_URL/pool-id" || true)
VM_ID=$(curl -s -H "Metadata-Flavor: Google" \
        http://metadata.google.internal/computeMetadata/v1/instance/name || true)

CONTROLLER_ENDPOINT="http://controller-service.monkci.local"

if [[ -z "$POOL_ID" ]]; then
    echo "ERROR: pool-id metadata attribute is missing."
    echo "Set metadata: --metadata=pool-id=<value>"
    exit 1
fi

echo "Metadata loaded: pool_id=$POOL_ID vm_id=$VM_ID"

### ------------------------------------------------------------
### 4. Write environment file
### ------------------------------------------------------------
cat > "$CONFIG_DIR/miglet.env" <<EOF
MIGLET_POOL_ID="$POOL_ID"
MIGLET_VM_ID="$VM_ID"
MIGLET_CONTROLLER_ENDPOINT="$CONTROLLER_ENDPOINT"
MIGLET_LOGGING_LEVEL="info"
MIGLET_LOGGING_FORMAT="text"
EOF

echo "Environment file written to $CONFIG_DIR/miglet.env"

### ------------------------------------------------------------
### 5. Create systemd service
### ------------------------------------------------------------
SERVICE_FILE="/etc/systemd/system/miglet.service"

cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=MIGlet - MonkCI VM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/opt/miglet
ExecStart=/opt/miglet/bin/miglet
EnvironmentFile=/etc/miglet/miglet.env
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

echo "Systemd service file created at $SERVICE_FILE"

### ------------------------------------------------------------
### 6. Enable + start MIGlet service
### ------------------------------------------------------------
systemctl daemon-reload
systemctl enable miglet
systemctl restart miglet

echo "$(date): MIGlet systemd service started"
echo "$(date): MIGlet startup script completed successfully"
