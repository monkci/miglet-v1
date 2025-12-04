#!/bin/bash
# GCP Startup Script for MIGlet
set -e

MIGLET_DIR="/opt/miglet"
LOG_FILE="/var/log/miglet-startup.log"

exec > >(tee -a "$LOG_FILE") 2>&1

echo "$(date): Starting MIGlet installation..."

# Ensure directories exist
mkdir -p "$MIGLET_DIR/bin"
mkdir -p "$MIGLET_DIR/configs"
mkdir -p /etc/miglet


if ! command -v go &> /dev/null; then
    echo "Installing Go..."
    wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
    tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz
    export PATH=$PATH:/usr/local/go/bin
    rm go1.21.5.linux-amd64.tar.gz
fi


echo "Downloading MIGlet binary from GCS..."

# ------------------------------
# ✅ OPTION: Download MIGlet binary from GCS bucket
# ------------------------------
# Required:
# - VM service account has roles/storage.objectViewer on your bucket
# - Replace BUCKET_NAME/path with your actual location

GCS_BUCKET="gs://monkci-artifacts/miglet/miglet-latest"
LOCAL_BIN_PATH="$MIGLET_DIR/bin/miglet"

# Fetch binary
gsutil cp "$GCS_BUCKET" "$LOCAL_BIN_PATH"
chmod +x "$LOCAL_BIN_PATH"

echo "MIGlet binary downloaded and marked executable."

# Read metadata from instance attributes
POOL_ID=$(curl -s -H "Metadata-Flavor: Google" \
    http://metadata.google.internal/computeMetadata/v1/instance/attributes/pool-id || echo "")
VM_ID=$(curl -s -H "Metadata-Flavor: Google" \
    http://metadata.google.internal/computeMetadata/v1/instance/name || echo "")
CONTROLLER_ENDPOINT="http://controller-service.monkci.local"   # FIX the double quote issue

# Create env file
cat > /etc/miglet/miglet.env <<EOF
MIGLET_POOL_ID="$POOL_ID"
MIGLET_VM_ID="$VM_ID"
MIGLET_CONTROLLER_ENDPOINT="$CONTROLLER_ENDPOINT"
MIGLET_LOGGING_LEVEL="info"
MIGLET_LOGGING_FORMAT="text"
EOF

echo "Environment file created."

# Start MIGlet service
if [ -f "$MIGLET_DIR/deploy/systemd/miglet.service" ]; then
    cp "$MIGLET_DIR/deploy/systemd/miglet.service" /etc/systemd/system/miglet.service
    systemctl daemon-reload
    systemctl enable miglet.service
    systemctl start miglet.service
    echo "$(date): MIGlet systemd service started."
else
    nohup "$LOCAL_BIN_PATH" > /var/log/miglet.log 2>&1 &
    echo "$(date): MIGlet started in background (no systemd service found)."
fi

echo "$(date): MIGlet startup script completed."
