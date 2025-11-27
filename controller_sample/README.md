# Sample MIG Controller

This is a simple, sketchy controller for testing MIGlet communication. It stores all incoming data and sends hardcoded responses.

## Features

- ✅ Receives and stores VM events
- ✅ Receives and stores heartbeats
- ✅ Receives registration token requests and sends hardcoded token
- ✅ Responds to command polling (returns empty for now)
- ✅ Stores all data in `controller_data/` directory

## Usage

### Start the Controller

```bash
cd controller_sample
go run main.go
```

The controller will start on port `8080` by default.

### Test Endpoints

- **Health Check**: `GET http://localhost:8080/health`
- **Registration Token**: `POST http://localhost:8080/api/v1/vms/{vm_id}/registration-token`
- **Events**: `POST http://localhost:8080/api/v1/vms/{vm_id}/events`
- **Heartbeat**: `POST http://localhost:8080/api/v1/vms/{vm_id}/heartbeat`
- **Commands**: `GET http://localhost:8080/api/v1/vms/{vm_id}/commands`

### Hardcoded Responses

**Registration Token Response:**
```json
{
  "registration_token": "AHTXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
  "expires_at": "2024-01-15T11:30:00Z",
  "runner_url": "https://github.com/testorg",
  "runner_group": "default",
  "labels": ["self-hosted", "linux", "x64"]
}
```

**Event/Heartbeat Response:**
```json
{
  "status": "received",
  "vm_id": "vm-123",
  "message": "Event received and stored"
}
```

**Commands Response (empty):**
```json
{
  "commands": [],
  "vm_id": "vm-123"
}
```

## Data Storage

All incoming data is stored in `controller_data/{vm_id}/` directory:

```
controller_data/
├── vm-123/
│   ├── registration-token-request-20240115-103000.000.json
│   ├── registration-token-response-20240115-103000.000.json
│   ├── event-vm_started-20240115-103001.000.json
│   ├── heartbeat-20240115-103015.000.json
│   └── ...
└── vm-456/
    └── ...
```

## Testing with MIGlet

1. Start the controller:
   ```bash
   cd controller_sample
   go run main.go
   ```

2. Configure MIGlet to point to the controller:
   ```bash
   export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
   export MIGLET_POOL_ID="test-pool"
   export MIGLET_VM_ID="test-vm-001"
   ```

3. Run MIGlet (when Phase 2 is implemented)

## Notes

- This is a **test/sketchy** controller - not production ready
- No authentication (for testing only)
- Hardcoded responses
- Simple file-based storage
- No validation or error handling
- Single-threaded (for simplicity)

## Future Enhancements

- Add authentication
- Add command queue (send drain/shutdown commands)
- Add web UI to view stored data
- Add filtering/search for events
- Add metrics endpoint

