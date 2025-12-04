# GCP MIG Setup - Quick Visual Guide

## Navigation Path

```
GCP Console → Compute Engine → Instance groups → Create instance group
```

## Step-by-Step UI Flow

### 1. Create Instance Template First

**Path:** `Compute Engine → Instance templates → Create instance template`

#### Basic Configuration Tab:
- **Name:** `miglet-template`
- **Machine type:** `e2-small` or `e2-medium`
- **Boot disk:** 
  - Click **Change**
  - Select **Ubuntu 22.04 LTS**
  - Size: `20 GB`
  - Click **Select**

#### Networking Tab:
- **Network:** `default`
- **External IP:** `Ephemeral` (for testing)

#### Advanced Options → Automation Tab:
- **Startup script:** 
  - Click **Add item**
  - Paste your startup script content
  - OR use: `gs://your-bucket/startup-script.sh`

- **Custom metadata:** Click **Add item** for each:
  ```
  Key: pool-id          Value: test-pool-001
  Key: controller-endpoint  Value: http://YOUR-IP:8080
  Key: github-org       Value: your-org
  Key: mongodb-enabled   Value: true
  Key: mongodb-connection-string  Value: mongodb+srv://...
  ```

- Click **Create** (bottom of page)

### 2. Create Managed Instance Group

**Path:** `Compute Engine → Instance groups → Create instance group`

#### Step 1: New managed instance group
- Select **New managed instance group**
- Select **Stateless**
- Click **Continue**

#### Step 2: Location
- **Single zone**
- Select zone: `us-central1-a` (or your preferred)

#### Step 3: Instance template
- Select the template you created: `miglet-template`

#### Step 4: Autoscaling
- **Autoscaling mode:** `Off` (for testing) or `On`
- If On:
  - Min instances: `1`
  - Max instances: `5`
  - Target CPU: `60%`

#### Step 5: Create
- Click **Create**
- Wait 1-2 minutes

### 3. Verify Setup

#### Check Instances:
1. Click on your MIG name
2. Click **VM instances** tab
3. You should see instances being created

#### Check MIGlet (via SSH):
1. Click on an instance name
2. Click **SSH** button
3. Run:
   ```bash
   sudo systemctl status miglet
   sudo journalctl -u miglet -f
   ```

#### Check Serial Console:
1. In instance details → **Serial port 1 (console)**
2. View startup logs

## Important Metadata Keys

When creating the instance template, add these metadata keys:

| Key | Value Example | Description |
|-----|---------------|-------------|
| `pool-id` | `test-pool-001` | Pool identifier |
| `controller-endpoint` | `http://10.0.0.5:8080` | MIG Controller URL |
| `github-org` | `monkci` | GitHub organization |
| `mongodb-enabled` | `true` | Enable MongoDB storage |
| `mongodb-connection-string` | `mongodb+srv://...` | MongoDB connection |

## Startup Script Options

### Option 1: Paste Directly
- Copy contents of `deploy/gcp/startup-script.sh`
- Paste into "Startup script" field in template

### Option 2: Use GCS Bucket
1. Upload `startup-script.sh` to GCS bucket
2. In template, use: `gs://your-bucket/startup-script.sh`

### Option 3: Build from Source
Modify startup script to:
- Clone repo
- Install Go
- Build MIGlet
- Install systemd service

## Visual Checklist

```
☐ Instance template created
☐ Startup script added to template
☐ Metadata keys added to template
☐ MIG created with template
☐ Instances appearing in MIG
☐ Can SSH into instances
☐ MIGlet service running (systemctl status)
☐ Controller receiving events
☐ MongoDB storing heartbeats (if enabled)
```

## Common Issues

### Issue: Instances stuck in "Creating"
- **Check:** Serial console for errors
- **Fix:** Verify startup script syntax

### Issue: MIGlet not found
- **Check:** `/opt/miglet/bin/miglet` exists
- **Fix:** Startup script needs to download/copy binary

### Issue: Controller connection failed
- **Check:** Firewall rules allow outbound traffic
- **Check:** Controller endpoint in metadata is correct
- **Fix:** Add firewall rule or fix endpoint

### Issue: Runner not installing
- **Check:** Internet connectivity from VM
- **Check:** Disk space available
- **Fix:** Verify network tags and firewall rules

## Quick Test Commands (SSH into instance)

```bash
# Check MIGlet status
sudo systemctl status miglet

# View logs
sudo journalctl -u miglet -n 50

# Check runner installation
ls -la /tmp/miglet-runner/actions-runner/

# Check runner config
cat /tmp/miglet-runner/actions-runner/.runner

# Check runner process
ps aux | grep Runner.Listener

# Test controller connectivity
curl http://YOUR-CONTROLLER-IP:8080/health
```

## Next: Monitor Your MIG

1. **Instance Groups** → Your MIG → **Monitoring** tab
2. View metrics, logs, and health
3. Check **Logs Explorer** for MIGlet logs
4. Query MongoDB for heartbeats

