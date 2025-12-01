# MIGlet Deployment Guide

This guide covers different methods to deploy and auto-start MIGlet on VMs.

## Method 1: systemd Service (Recommended for Linux)

### Installation

1. **Copy files to VM:**
   ```bash
   scp -r deploy/systemd/ user@vm:/tmp/
   scp bin/miglet user@vm:/tmp/
   ```

2. **On the VM, run installation:**
   ```bash
   cd /tmp/systemd
   sudo ./install.sh
   ```

3. **Configure environment:**
   ```bash
   sudo nano /etc/miglet/miglet.env
   # Set your configuration variables
   ```

4. **Start the service:**
   ```bash
   sudo systemctl start miglet
   sudo systemctl status miglet
   ```

### Service Management

```bash
# Start
sudo systemctl start miglet

# Stop
sudo systemctl stop miglet

# Restart
sudo systemctl restart miglet

# Check status
sudo systemctl status miglet

# View logs
sudo journalctl -u miglet -f

# Enable on boot (already done by install.sh)
sudo systemctl enable miglet

# Disable on boot
sudo systemctl disable miglet
```

### Configuration

Edit `/etc/miglet/miglet.env`:
```bash
MIGLET_POOL_ID="pool-123"
MIGLET_VM_ID="vm-456"
MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io"
MIGLET_STORAGE_MONGODB_ENABLED="true"
MIGLET_STORAGE_MONGODB_CONNECTION_STRING="mongodb+srv://..."
```

After editing, restart the service:
```bash
sudo systemctl restart miglet
```

## Method 2: GCP Startup Script

### Using GCP Console

1. Go to Compute Engine → VM instances
2. Create or edit VM
3. Under "Automation" → "Startup script"
4. Paste the contents of `deploy/gcp/startup-script.sh`
5. Or reference a GCS bucket: `gs://your-bucket/startup-script.sh`

### Using gcloud CLI

```bash
gcloud compute instances create miglet-vm \
  --zone=us-central1-a \
  --metadata-from-file startup-script=deploy/gcp/startup-script.sh \
  --metadata pool-id=pool-123,controller-endpoint=https://controller.monkci.io
```

### Using Instance Metadata

Set custom metadata that the startup script will read:
```bash
gcloud compute instances add-metadata INSTANCE_NAME \
  --zone=ZONE \
  --metadata pool-id=pool-123,controller-endpoint=https://controller.monkci.io
```

## Method 3: Cloud-Init (Multi-Cloud)

Create a `cloud-init.yaml`:

```yaml
#cloud-config
packages:
  - wget
  - curl

write_files:
  - path: /opt/miglet/bin/miglet
    permissions: '0755'
    owner: root:root
    content: |
      # Binary content here (base64 encoded or download)

runcmd:
  - |
    cat > /etc/systemd/system/miglet.service <<EOF
    [Unit]
    Description=MIGlet
    After=network-online.target
    
    [Service]
    Type=simple
    ExecStart=/opt/miglet/bin/miglet
    Restart=always
    Environment="MIGLET_POOL_ID=pool-123"
    Environment="MIGLET_VM_ID=$(hostname)"
    
    [Install]
    WantedBy=multi-user.target
    EOF
  - systemctl daemon-reload
  - systemctl enable miglet
  - systemctl start miglet
```

## Method 4: Docker Container

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o miglet ./cmd/miglet

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /build/miglet /usr/local/bin/miglet
ENTRYPOINT ["/usr/local/bin/miglet"]
```

### docker-compose.yml

```yaml
version: '3.8'
services:
  miglet:
    build: .
    restart: unless-stopped
    environment:
      - MIGLET_POOL_ID=${POOL_ID}
      - MIGLET_VM_ID=${VM_ID}
      - MIGLET_CONTROLLER_ENDPOINT=${CONTROLLER_ENDPOINT}
    volumes:
      - /tmp/miglet-runner:/tmp/miglet-runner
```

### Run with Docker

```bash
docker run -d \
  --name miglet \
  --restart unless-stopped \
  -e MIGLET_POOL_ID="pool-123" \
  -e MIGLET_VM_ID="vm-456" \
  -e MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io" \
  -v /tmp/miglet-runner:/tmp/miglet-runner \
  miglet:latest
```

## Method 5: Manual Background Process (Not Recommended)

```bash
# Create a simple wrapper script
cat > /usr/local/bin/miglet-wrapper.sh <<'EOF'
#!/bin/bash
cd /opt/miglet
exec /opt/miglet/bin/miglet "$@"
EOF

chmod +x /usr/local/bin/miglet-wrapper.sh

# Add to crontab for @reboot
(crontab -l 2>/dev/null; echo "@reboot /usr/local/bin/miglet-wrapper.sh") | crontab -
```

## Recommended Approach

**For GCP VMs:**
1. Use **systemd service** (Method 1) for production
2. Use **GCP startup script** (Method 2) for initial deployment automation
3. Combine both: startup script installs systemd service

**For other clouds:**
- Use **systemd service** (Method 1)
- Use **cloud-init** (Method 3) for initial setup

## Verification

After deployment, verify MIGlet is running:

```bash
# Check process
ps aux | grep miglet

# Check systemd status
sudo systemctl status miglet

# Check logs
sudo journalctl -u miglet -n 50

# Test connectivity
curl http://localhost:8080/health  # If controller is local
```

## Troubleshooting

### Service won't start
```bash
# Check service status
sudo systemctl status miglet

# Check logs
sudo journalctl -u miglet -n 100

# Check configuration
sudo cat /etc/miglet/miglet.env
```

### Binary not found
```bash
# Verify binary exists
ls -la /opt/miglet/bin/miglet

# Check permissions
sudo chmod +x /opt/miglet/bin/miglet
```

### Environment variables not loaded
```bash
# Check if EnvironmentFile is uncommented in service file
sudo cat /etc/systemd/system/miglet.service | grep EnvironmentFile

# Reload systemd after changes
sudo systemctl daemon-reload
sudo systemctl restart miglet
```

