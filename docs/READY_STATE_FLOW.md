# Ready State Flow

## Overview

MIGlet now uses a two-phase registration flow:
1. **WaitingForController** - Sends VM started event and waits for acknowledgment
2. **Ready** - Polls controller for registration config command
3. **RegisteringRunner** - Uses config from command to register runner

## State Flow

```
Initializing
    ↓
WaitingForController (sends vm_started event)
    ↓ (receives ack)
Ready (polls for register_runner command)
    ↓ (receives command with token/config)
RegisteringRunner (configures and starts runner)
    ↓
Idle (runner ready, waiting for jobs)
```

## Changes

### 1. WaitingForController State
- **Before**: Sent VM started event, received ack + registration token immediately
- **Now**: Sends VM started event, receives only acknowledgment (no token)
- **Transition**: Moves to `Ready` state after acknowledgment

### 2. New Ready State
- **Purpose**: Wait for controller to send registration config via command
- **Behavior**: Polls `/api/v1/vms/{vm_id}/commands` endpoint every 5 seconds
- **Command Type**: Looks for `register_runner` command
- **Command Parameters**:
  ```json
  {
    "id": "register-vm-123-1234567890",
    "type": "register_runner",
    "parameters": {
      "registration_token": "AHTXXXXXXXXXXXXXXXXXXXX",
      "runner_url": "https://github.com/org/repo",
      "runner_group": "default",
      "labels": ["self-hosted", "linux", "x64"],
      "expires_at": "2024-01-15T11:30:00Z"
    },
    "created_at": "2024-01-15T10:30:00Z"
  }
  ```
- **Transition**: Moves to `RegisteringRunner` when command received

### 3. RegisteringRunner State
- **Before**: Used token from VM started event acknowledgment
- **Now**: Uses token and config from `register_runner` command
- **Behavior**: Same as before - configures and starts runner

## API Changes

### Controller Endpoints

#### 1. VM Started Event (POST /api/v1/vms/{vm_id}/events)
**Request:**
```json
{
  "type": "vm_started",
  "vm_id": "vm-123",
  "pool_id": "pool-001",
  "org_id": "org-001"
}
```

**Response (Updated):**
```json
{
  "status": "acknowledged",
  "acknowledged": true,
  "vm_id": "vm-123",
  "pool_id": "pool-001",
  "org_id": "org-001",
  "message": "VM started event acknowledged - MIGlet is ready for registration config"
}
```
**Note**: No `registration_token` in response anymore.

#### 2. Commands Polling (GET /api/v1/vms/{vm_id}/commands)
**Request:** None (GET request)

**Response:**
```json
{
  "commands": [
    {
      "id": "register-vm-123-1234567890",
      "type": "register_runner",
      "parameters": {
        "registration_token": "AHTXXXXXXXXXXXXXXXXXXXX",
        "runner_url": "https://github.com/leaffyAdmin/django_repo",
        "runner_group": "default",
        "labels": ["self-hosted", "linux", "x64"],
        "expires_at": "2024-01-15T11:30:00Z"
      },
      "created_at": "2024-01-15T10:30:00Z"
    }
  ],
  "vm_id": "vm-123"
}
```

## Controller Implementation

### Sample Controller Updates

The `controller_sample/main.go` has been updated:

1. **VM Started Event Handler**:
   - Removed `registration_token` from acknowledgment response
   - Marks VM as "ready" in internal tracking

2. **Commands Handler**:
   - Checks if VM has sent `vm_started` event
   - Returns `register_runner` command with token and config
   - Only sends command once per VM (tracks with `vmRegistrationSent` map)

### Controller Logic

```go
// Track VMs ready for registration
var readyVMs = make(map[string]bool)
var vmRegistrationSent = make(map[string]bool)

// In handleEvents (vm_started):
readyVMs[vmID] = true
vmRegistrationSent[vmID] = false

// In handleCommands:
if readyVMs[vmID] && !vmRegistrationSent[vmID] {
    // Send register_runner command
    vmRegistrationSent[vmID] = true
}
```

## Benefits

1. **Separation of Concerns**: Controller decides when to send registration config
2. **Flexibility**: Controller can delay registration or send different configs
3. **Better Control**: Controller can validate VM state before sending token
4. **Scalability**: Controller can batch or prioritize registration commands

## Testing

### Test Flow

1. Start sample controller:
   ```bash
   cd controller_sample
   go run main.go
   ```

2. Start MIGlet:
   ```bash
   export MIGLET_POOL_ID="test-pool-001"
   export MIGLET_VM_ID="test-vm-001"
   export MIGLET_CONTROLLER_ENDPOINT="http://localhost:8080"
   ./bin/miglet
   ```

3. Expected behavior:
   - MIGlet sends `vm_started` event
   - Controller acknowledges (no token)
   - MIGlet transitions to `Ready` state
   - MIGlet polls `/commands` endpoint
   - Controller returns `register_runner` command
   - MIGlet transitions to `RegisteringRunner` state
   - Runner gets configured and started

### Verification

Check logs for:
- `"Controller acknowledgment received, transitioning to ready state"`
- `"MIGlet is ready - polling controller for registration config"`
- `"Received register_runner command from controller"`
- `"Registration config received, transitioning to registering runner"`

## Migration Notes

If you have existing MIGlets or controllers:
- **MIGlet**: Already updated to use new flow
- **Controller**: Must be updated to:
  1. Remove `registration_token` from VM started event ack
  2. Implement command polling endpoint
  3. Send `register_runner` command when VM is ready

