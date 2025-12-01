#!/bin/bash
# Installation script for MIGlet systemd service

set -e

MIGLET_DIR="/opt/miglet"
SERVICE_FILE="miglet.service"
SYSTEMD_DIR="/etc/systemd/system"

echo "Installing MIGlet systemd service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "Please run as root (use sudo)"
    exit 1
fi

# Create MIGlet directory
mkdir -p "$MIGLET_DIR/bin"
mkdir -p "$MIGLET_DIR/configs"
mkdir -p "/etc/miglet"

# Copy binary (assuming it's in current directory)
if [ -f "./bin/miglet" ]; then
    cp ./bin/miglet "$MIGLET_DIR/bin/miglet"
    chmod +x "$MIGLET_DIR/bin/miglet"
    echo "✓ Binary copied to $MIGLET_DIR/bin/miglet"
else
    echo "Warning: ./bin/miglet not found. Please copy the binary manually."
fi

# Copy service file
cp "$SERVICE_FILE" "$SYSTEMD_DIR/$SERVICE_FILE"
echo "✓ Service file installed to $SYSTEMD_DIR/$SERVICE_FILE"

# Create environment file template
if [ ! -f "/etc/miglet/miglet.env" ]; then
    cat > /etc/miglet/miglet.env <<EOF
# MIGlet Environment Variables
# Uncomment and set values as needed

# MIGLET_POOL_ID="pool-123"
# MIGLET_VM_ID="vm-456"
# MIGLET_ORG_ID="org-789"
# MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io"
# MIGLET_GITHUB_ORG="myorg"
# MIGLET_STORAGE_MONGODB_ENABLED="true"
# MIGLET_STORAGE_MONGODB_CONNECTION_STRING="mongodb+srv://..."
EOF
    echo "✓ Environment file template created at /etc/miglet/miglet.env"
    echo "  Edit this file to set your configuration"
fi

# Update service to use environment file
sed -i 's|# EnvironmentFile=/etc/miglet/miglet.env|EnvironmentFile=/etc/miglet/miglet.env|' "$SYSTEMD_DIR/$SERVICE_FILE"

# Reload systemd
systemctl daemon-reload
echo "✓ Systemd daemon reloaded"

# Enable service (but don't start yet - user should configure first)
systemctl enable miglet.service
echo "✓ MIGlet service enabled (will start on boot)"

echo ""
echo "Installation complete!"
echo ""
echo "Next steps:"
echo "1. Edit /etc/miglet/miglet.env and set your configuration"
echo "2. Start the service: sudo systemctl start miglet"
echo "3. Check status: sudo systemctl status miglet"
echo "4. View logs: sudo journalctl -u miglet -f"

