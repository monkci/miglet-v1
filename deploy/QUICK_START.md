# Quick Start - Auto-start MIGlet on VM Boot

## Recommended: systemd Service (Linux)

### Step 1: Transfer Files to VM

```bash
# From your local machine
scp bin/miglet user@your-vm:/tmp/miglet
scp -r deploy/systemd/ user@your-vm:/tmp/
```

### Step 2: Install on VM

```bash
# SSH into VM
ssh user@your-vm

# Run installation
cd /tmp/systemd
sudo ./install.sh
```

### Step 3: Configure

```bash
# Edit environment file
sudo nano /etc/miglet/miglet.env
```

Add your configuration:
```bash
MIGLET_POOL_ID="pool-123"
MIGLET_VM_ID="vm-456"
MIGLET_CONTROLLER_ENDPOINT="https://controller.monkci.io"
MIGLET_STORAGE_MONGODB_ENABLED="true"
MIGLET_STORAGE_MONGODB_CONNECTION_STRING="mongodb+srv://monkci:Youtubes1@monkcicluster.hoogley.mongodb.net/monkci"
```

### Step 4: Start Service

```bash
sudo systemctl start miglet
sudo systemctl status miglet
```

### Step 5: Verify

```bash
# Check if running
sudo systemctl status miglet

# View logs
sudo journalctl -u miglet -f

# Verify it starts on boot
sudo systemctl is-enabled miglet
# Should output: enabled
```

## GCP-Specific: Using Startup Script

### Option A: Via gcloud CLI

```bash
gcloud compute instances create miglet-vm \
  --zone=us-central1-a \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --metadata-from-file startup-script=deploy/gcp/startup-script.sh \
  --metadata \
    pool-id=pool-123,\
    controller-endpoint=https://controller.monkci.io
```

### Option B: Via GCP Console

1. Create VM → Advanced options
2. Under "Automation" → Paste startup script
3. Add custom metadata:
   - `pool-id`: your pool ID
   - `controller-endpoint`: your controller URL

## Service Commands Reference

```bash
# Start
sudo systemctl start miglet

# Stop
sudo systemctl stop miglet

# Restart
sudo systemctl restart miglet

# Status
sudo systemctl status miglet

# Logs (follow)
sudo journalctl -u miglet -f

# Logs (last 100 lines)
sudo journalctl -u miglet -n 100

# Enable on boot
sudo systemctl enable miglet

# Disable on boot
sudo systemctl disable miglet
```

## Troubleshooting

### Service fails to start
```bash
# Check detailed status
sudo systemctl status miglet -l

# Check logs
sudo journalctl -u miglet -n 50 --no-pager

# Verify binary exists
ls -la /opt/miglet/bin/miglet
```

### Configuration not loading
```bash
# Check environment file
sudo cat /etc/miglet/miglet.env

# Verify service file loads it
sudo cat /etc/systemd/system/miglet.service | grep EnvironmentFile

# Reload after changes
sudo systemctl daemon-reload
sudo systemctl restart miglet
```

### Network issues
```bash
# Check if network is up
systemctl status network-online.target

# Test connectivity
curl -v https://controller.monkci.io/health
```

