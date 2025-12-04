# Setting Up GCP Managed Instance Group (MIG) for MIGlet Testing

This guide walks you through creating a MIG in GCP Console to test MIGlet.

## Prerequisites

- GCP project with billing enabled
- Compute Engine API enabled
- Basic understanding of GCP Console

## Step-by-Step: Create MIG in GCP Console

### Step 1: Navigate to Instance Groups

1. Go to [GCP Console](https://console.cloud.google.com)
2. Select your project
3. Navigate to **Compute Engine** → **Instance groups** (in left sidebar)
4. Click **Create instance group** button

### Step 2: Choose MIG Type

1. Select **New managed instance group**
2. Choose **Stateless** (since MIGlet is ephemeral)
3. Click **Continue**

### Step 3: Configure Basic Settings

**Name:**
- `miglet-test-group` (or your preferred name)

**Location:**
- **Single zone** (recommended for testing)
- Select a zone (e.g., `us-central1-a`)

**Instance template:** 
- Click **Create new instance template** (we'll configure this next)

### Step 4: Create Instance Template

#### 4.1 Basic Configuration

**Name:**
- `miglet-template`

**Machine type:**
- `e2-medium` or `e2-small` (sufficient for testing)

**Boot disk:**
- **Image:** Ubuntu 22.04 LTS (or latest)
- **Boot disk type:** Standard persistent disk
- **Size:** 20 GB (minimum)

#### 4.2 Networking

**Network:**
- `default` (or your VPC)

**Network tags:**
- Add tags if needed for firewall rules

**External IP:**
- **Ephemeral** (for testing) or **None** (for production)

#### 4.3 Advanced Options → Automation

**Startup script:**
- Click **Add item**
- Paste the contents of `deploy/gcp/startup-script.sh`
- Or reference a GCS bucket: `gs://your-bucket/startup-script.sh`

**Custom metadata:**
Click **Add item** for each:

1. **Key:** `pool-id`
   **Value:** `test-pool-001`

2. **Key:** `controller-endpoint`
   **Value:** `http://YOUR-CONTROLLER-IP:8080` (or your controller URL)

3. **Key:** `github-org`
   **Value:** `your-github-org`

4. **Key:** `mongodb-connection-string`
   **Value:** `mongodb+srv://monkci:Youtubes1@monkcicluster.hoogley.mongodb.net/monkci`

5. **Key:** `mongodb-enabled`
   **Value:** `true`

#### 4.4 Service Account (Optional)

- Create or select a service account with minimal permissions
- Or use default compute service account

#### 4.5 Create Template

- Click **Create** at the bottom
- Wait for template creation to complete

### Step 5: Complete MIG Configuration

**Back in MIG creation:**

1. **Instance template:** Select the template you just created

2. **Autoscaling:**
   - **Autoscaling mode:** Off (for testing) or On (for auto-scaling)
   - If On:
     - **Minimum instances:** 1
     - **Maximum instances:** 5
     - **Target CPU utilization:** 60%

3. **Autohealing (Optional):**
   - Enable if you want automatic health checks
   - **Health check:** Create new or use existing
   - **Initial delay:** 60 seconds

4. **Update policy:**
   - **Type:** Opportunistic (for testing) or Proactive (for production)
   - **Maximum surge:** 1
   - **Maximum unavailable:** 0

### Step 6: Deploy MIG

1. Click **Create**
2. Wait for MIG creation (1-2 minutes)
3. You'll see the MIG in the instance groups list

### Step 7: Verify Instances

1. Click on your MIG name
2. Go to **VM instances** tab
3. You should see instances being created
4. Click on an instance name to see details

### Step 8: Check MIGlet Status

#### Option A: Via SSH

1. In the instance details, click **SSH** button
2. Once connected:
   ```bash
   # Check if MIGlet is running
   sudo systemctl status miglet
   
   # View logs
   sudo journalctl -u miglet -f
   
   # Check if runner is installed
   ls -la /tmp/miglet-runner/actions-runner/
   ```

#### Option B: Via Serial Console

1. In instance details → **Serial port 1 (console)**
2. View console output to see MIGlet startup logs

#### Option C: Via Logs

1. Go to **Logging** → **Logs Explorer**
2. Filter by:
   - Resource: `gce_instance`
   - Instance name: `your-instance-name`
   - Log name: `syslog` or search for "miglet"

### Step 9: Test MIGlet Functionality

1. **Check Controller:**
   - Verify controller receives VM started events
   - Check `controller_data/` directory for stored events

2. **Check MongoDB:**
   - Connect to MongoDB
   - Query `heartbeats` collection:
     ```javascript
     db.heartbeats.find({ vm_id: "your-vm-name" }).sort({ timestamp: -1 })
     ```

3. **Check Runner:**
   - SSH into instance
   - Check if runner is configured:
     ```bash
     ls -la /tmp/miglet-runner/actions-runner/.runner
     ```
   - Check if runner process is running:
     ```bash
     ps aux | grep Runner.Listener
     ```

## Important Notes

### Startup Script Considerations

The startup script needs to:
1. **Download MIGlet binary** - You have options:
   - Upload to GCS bucket and download in script
   - Build from source in script (requires Go)
   - Copy from another source

2. **Install systemd service** - The script should:
   - Copy binary to `/opt/miglet/bin/`
   - Install systemd service
   - Start the service

### Firewall Rules

If your controller is on a different VM, ensure firewall allows:
- Outbound HTTPS (for controller communication)
- Outbound HTTPS (for GitHub API)
- Outbound HTTPS (for MongoDB)

Create firewall rule if needed:
1. **VPC network** → **Firewall**
2. **Create firewall rule**
3. Allow egress on ports 443, 8080 (or your controller port)

### Testing Checklist

- [ ] MIG created successfully
- [ ] Instances are being created
- [ ] Startup script executes (check serial console)
- [ ] MIGlet binary is present at `/opt/miglet/bin/miglet`
- [ ] systemd service is installed and enabled
- [ ] MIGlet service is running (`systemctl status miglet`)
- [ ] Controller receives VM started events
- [ ] Runner is downloaded and installed
- [ ] Runner is configured and running
- [ ] Heartbeats are being sent to controller
- [ ] Heartbeats are stored in MongoDB (if enabled)

## Troubleshooting

### Instances Not Starting

1. Check **Serial port 1** for errors
2. Check startup script syntax
3. Verify metadata values are correct

### MIGlet Not Running

1. SSH into instance
2. Check service status: `sudo systemctl status miglet`
3. Check logs: `sudo journalctl -u miglet -n 100`
4. Verify binary exists: `ls -la /opt/miglet/bin/miglet`
5. Check environment file: `sudo cat /etc/miglet/miglet.env`

### Controller Not Receiving Events

1. Verify controller is running and accessible
2. Check firewall rules allow outbound traffic
3. Check controller endpoint in metadata
4. View MIGlet logs for connection errors

### Runner Not Installing

1. Check internet connectivity: `curl -I https://github.com`
2. Check disk space: `df -h`
3. Check `/tmp/miglet-runner/` directory permissions
4. View startup script logs in serial console

## Next Steps

After MIG is running:
1. Monitor MIGlet logs across instances
2. Test auto-scaling (if enabled)
3. Test instance replacement
4. Verify heartbeats in MongoDB
5. Test GitHub Actions job execution

## Cleanup

To delete the test MIG:
1. Go to **Instance groups**
2. Select your MIG
3. Click **Delete**
4. Confirm deletion
5. Delete the instance template if not needed:
   - **Instance templates** → Select template → **Delete**

