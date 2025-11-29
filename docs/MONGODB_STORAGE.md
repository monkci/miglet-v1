# MongoDB Storage for Heartbeats

MIGlet can store heartbeats directly in MongoDB for persistence and analytics.

## Configuration

### Environment Variables

```bash
export MIGLET_STORAGE_MONGODB_ENABLED="true"
export MIGLET_STORAGE_MONGODB_CONNECTION_STRING="mongodb+srv://monkci:Youtubes1@monkcicluster.hoogley.mongodb.net/monkci"
export MIGLET_STORAGE_MONGODB_DATABASE="monkci"
export MIGLET_STORAGE_MONGODB_COLLECTION="heartbeats"
```

### Config File

```yaml
storage:
  mongodb:
    enabled: true
    connection_string: "mongodb+srv://monkci:Youtubes1@monkcicluster.hoogley.mongodb.net/monkci"
    database: "monkci"
    collection: "heartbeats"
```

## Features

- **Non-blocking**: MongoDB writes happen in a goroutine, so they don't block heartbeats
- **Automatic retry**: If MongoDB is unavailable, heartbeats still go to controller
- **Graceful degradation**: If MongoDB connection fails at startup, MIGlet continues without it
- **Connection management**: Properly closes connection on shutdown

## Heartbeat Document Structure

Each heartbeat is stored as a document with the following structure:

```json
{
  "type": "job_heartbeat",
  "timestamp": "2024-01-15T10:30:00Z",
  "vm_id": "test-vm-001111",
  "pool_id": "test-pool-001111",
  "org_id": "org-789",
  "vm_health": {
    "cpu_load": 0.5,
    "memory_used": 512,
    "memory_total": 2048,
    "disk_used": 10,
    "disk_total": 100
  },
  "runner_state": "idle",
  "current_job": {
    "job_id": "job-123",
    "run_id": "run-456",
    "repository": "monkci/miglet-v1",
    "started_at": "2024-01-15T10:25:00Z"
  },
  "created_at": "2024-01-15T10:30:00Z"
}
```

## Usage

1. **Enable MongoDB storage** via environment variables or config file
2. **Run MIGlet** - it will automatically connect to MongoDB on startup
3. **Heartbeats are stored** every `heartbeat.interval` (default 15s)
4. **Check logs** for MongoDB connection status and storage confirmations

## Logs

When MongoDB is enabled, you'll see:
```
INFO  [08:18:00] Initializing MongoDB storage
INFO  [08:18:00] Connected to MongoDB database=monkci collection=heartbeats
INFO  [08:18:00] MongoDB storage initialized successfully
DEBUG [08:18:15] Heartbeat stored in MongoDB successfully
```

If MongoDB is unavailable:
```
WARN  [08:18:00] Failed to initialize MongoDB storage, continuing without it error="connection timeout"
```

## Querying Heartbeats

You can query heartbeats in MongoDB:

```javascript
// Get all heartbeats for a VM
db.heartbeats.find({ vm_id: "test-vm-001111" })

// Get heartbeats when runner was running a job
db.heartbeats.find({ runner_state: "running" })

// Get recent heartbeats
db.heartbeats.find().sort({ timestamp: -1 }).limit(100)

// Get heartbeats for a specific pool
db.heartbeats.find({ pool_id: "test-pool-001111" })
```

## Notes

- MongoDB writes are **non-blocking** - they happen in background goroutines
- If MongoDB write fails, it's logged but doesn't affect controller heartbeats
- Connection is established once at startup and reused
- Connection is properly closed on MIGlet shutdown

