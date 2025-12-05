# Command Communication Options: Controller → MIGlet

## Current State
- **MIGlet polls** controller: `GET /api/v1/vms/{vm_id}/commands`
- Controller responds with pending commands
- MIGlet processes commands and continues polling

## Requirement
- **Controller pushes** commands to MIGlet
- Real-time command delivery
- Support for multiple command types (register_runner, drain, shutdown, update_config, set_log_level)
- Reliable delivery and acknowledgment

---

## Option 1: WebSocket (Recommended for Real-Time)

### Architecture
```
MIGlet ←→ WebSocket ←→ Controller
  ↑                        ↓
  └── Commands pushed in real-time
```

### Implementation
- **MIGlet**: Opens WebSocket connection on startup
- **Controller**: Maintains WebSocket connections per VM
- **Commands**: Pushed immediately when available
- **Acknowledgment**: Via WebSocket message

### Pros
✅ **Real-time delivery** - Commands arrive instantly  
✅ **Bidirectional** - Can send events/heartbeats over same connection  
✅ **Efficient** - No polling overhead  
✅ **Standard protocol** - Well-supported in Go  
✅ **Connection state** - Controller knows if VM is online/offline  

### Cons
❌ **Connection management** - Need to handle reconnections, timeouts  
❌ **Stateful** - Controller must maintain connection state  
❌ **Load balancer complexity** - Sticky sessions required  
❌ **Firewall/NAT issues** - Some networks block WebSocket  
❌ **Resource usage** - Long-lived connections consume resources  

### Go Libraries
- `gorilla/websocket` (most popular)
- `nhooyr.io/websocket` (modern, efficient)

### Example Flow
```go
// MIGlet side
conn, _, err := websocket.DefaultDialer.Dial("ws://controller:8080/api/v1/vms/vm-123/commands", nil)
for {
    var cmd Command
    err := conn.ReadJSON(&cmd)
    // Process command
    conn.WriteJSON(AckResponse{CommandID: cmd.ID, Status: "processed"})
}

// Controller side
hub := NewHub() // Manages connections
hub.Register(vmID, conn)
hub.SendCommand(vmID, command)
```

---

## Option 2: Server-Sent Events (SSE)

### Architecture
```
MIGlet ←─── SSE Stream ─── Controller
  ↑                           ↓
  └── Commands pushed via SSE
```

### Implementation
- **MIGlet**: Opens SSE connection (`GET /api/v1/vms/{vm_id}/commands/stream`)
- **Controller**: Streams commands as they become available
- **Commands**: Sent as `data:` lines in SSE format
- **Acknowledgment**: Via separate HTTP POST endpoint

### Pros
✅ **Simple** - HTTP-based, easier than WebSocket  
✅ **Unidirectional push** - Perfect for command delivery  
✅ **Auto-reconnect** - Browsers/HTTP clients handle reconnection  
✅ **Works through proxies** - Standard HTTP  
✅ **Less overhead** - Simpler than WebSocket  

### Cons
❌ **One-way only** - MIGlet still needs HTTP POST for events/heartbeats  
❌ **Text-only** - Must JSON encode commands  
❌ **Connection limits** - Some HTTP servers limit concurrent connections  
❌ **No built-in ack** - Need separate endpoint for acknowledgments  

### Go Libraries
- Standard `net/http` (SSE is just HTTP with `text/event-stream`)
- `github.com/r3labs/sse` (helper library)

### Example Flow
```go
// MIGlet side
resp, _ := http.Get("http://controller:8080/api/v1/vms/vm-123/commands/stream")
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    if strings.HasPrefix(scanner.Text(), "data: ") {
        var cmd Command
        json.Unmarshal([]byte(scanner.Text()[6:]), &cmd)
        // Process command
        http.Post("http://controller:8080/api/v1/vms/vm-123/commands/ack", ...)
    }
}

// Controller side
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
fmt.Fprintf(w, "data: %s\n\n", jsonCommand)
w.(http.Flusher).Flush()
```

---

## Option 3: HTTP Long Polling

### Architecture
```
MIGlet ──→ GET /commands (long poll) ──→ Controller
  ↑                                        ↓
  └── Holds request until command ready ──┘
```

### Implementation
- **MIGlet**: Makes `GET /api/v1/vms/{vm_id}/commands` request
- **Controller**: Holds request open (e.g., 30-60 seconds)
- **Commands**: Returned immediately when available, or timeout if none
- **MIGlet**: Immediately makes new request after receiving response/timeout

### Pros
✅ **Simple** - Standard HTTP, no special protocols  
✅ **Works everywhere** - No firewall issues  
✅ **Stateless** - Each request is independent  
✅ **Easy to implement** - Minimal changes from current polling  
✅ **Load balancer friendly** - No sticky sessions needed  

### Cons
❌ **Not truly push** - Still requires MIGlet to initiate request  
❌ **Resource usage** - Controller holds connections open  
❌ **Latency** - Up to timeout duration if no commands  
❌ **Connection limits** - Many open connections can be an issue  

### Example Flow
```go
// MIGlet side
for {
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    req, _ := http.NewRequestWithContext(ctx, "GET", "/commands", nil)
    resp, _ := client.Do(req)
    // Process commands from response
    cancel()
}

// Controller side
func handleCommands(w http.ResponseWriter, r *http.Request) {
    // Wait for command or timeout
    select {
    case cmd := <-commandChannel:
        json.NewEncoder(w).Encode(CommandsResponse{Commands: []Command{cmd}})
    case <-time.After(60 * time.Second):
        json.NewEncoder(w).Encode(CommandsResponse{Commands: []Command{}})
    }
}
```

---

## Option 4: Message Queue (Pub/Sub)

### Architecture
```
MIGlet ←─── Subscribe ─── Message Queue ←─── Controller
  ↑                                            ↓
  └── Commands via queue ──────────────────────┘
```

### Implementation
- **Controller**: Publishes commands to queue (Redis Pub/Sub, RabbitMQ, etc.)
- **MIGlet**: Subscribes to queue for its VM ID
- **Commands**: Delivered via queue subscription
- **Acknowledgment**: Via queue ACK or separate HTTP endpoint

### Pros
✅ **Scalable** - Queue handles many VMs efficiently  
✅ **Reliable** - Queue provides delivery guarantees  
✅ **Decoupled** - Controller and MIGlet don't need direct connection  
✅ **Persistent** - Commands can be queued if VM is offline  
✅ **Multi-consumer** - Easy to add monitoring/logging consumers  

### Cons
❌ **Infrastructure dependency** - Need to deploy and manage queue  
❌ **Complexity** - Additional component to maintain  
❌ **Latency** - Slight overhead from queue  
❌ **Configuration** - Need queue credentials/endpoints  

### Options
- **Redis Pub/Sub** - Simple, fast, in-memory
- **RabbitMQ** - Full-featured, persistent queues
- **NATS** - Lightweight, cloud-native
- **GCP Pub/Sub** - Managed service (if on GCP)

### Example Flow (Redis)
```go
// MIGlet side
pubsub := redisClient.Subscribe(ctx, "vm:vm-123:commands")
ch := pubsub.Channel()
for msg := range ch {
    var cmd Command
    json.Unmarshal([]byte(msg.Payload), &cmd)
    // Process command
}

// Controller side
redisClient.Publish(ctx, "vm:vm-123:commands", jsonCommand)
```

---

## Option 5: gRPC Streaming

### Architecture
```
MIGlet ←─── gRPC Stream ─── Controller
  ↑                           ↓
  └── Bidirectional streaming
```

### Implementation
- **MIGlet**: Opens gRPC stream to controller
- **Controller**: Sends commands via stream
- **MIGlet**: Can send events/heartbeats via same stream
- **Acknowledgment**: Via stream message

### Pros
✅ **Efficient** - Binary protocol, lower overhead  
✅ **Bidirectional** - Commands and events on same stream  
✅ **Type-safe** - Protocol buffers provide strong typing  
✅ **Modern** - Industry standard for microservices  

### Cons
❌ **Complexity** - Need to define protobuf schemas  
❌ **HTTP/2 required** - Some environments don't support  
❌ **Less common** - Fewer examples/documentation  
❌ **Tooling** - Need protoc compiler  

---

## Comparison Matrix

| Feature | WebSocket | SSE | Long Polling | Message Queue | gRPC |
|---------|-----------|-----|--------------|---------------|------|
| **Real-time** | ✅ Excellent | ✅ Good | ⚠️ Good (with timeout) | ✅ Excellent | ✅ Excellent |
| **Bidirectional** | ✅ Yes | ❌ No | ❌ No | ⚠️ Via separate channel | ✅ Yes |
| **Simplicity** | ⚠️ Medium | ✅ Simple | ✅ Simple | ❌ Complex | ❌ Complex |
| **Infrastructure** | ✅ None | ✅ None | ✅ None | ❌ Queue needed | ✅ None |
| **Firewall friendly** | ⚠️ Sometimes | ✅ Yes | ✅ Yes | ⚠️ Depends | ⚠️ HTTP/2 |
| **Load balancer** | ⚠️ Sticky sessions | ✅ Standard | ✅ Standard | ✅ Standard | ✅ Standard |
| **Offline queuing** | ❌ No | ❌ No | ❌ No | ✅ Yes | ❌ No |
| **Resource usage** | ⚠️ Medium | ⚠️ Medium | ⚠️ Medium | ✅ Low | ✅ Low |
| **Go support** | ✅ Excellent | ✅ Good | ✅ Excellent | ✅ Good | ✅ Excellent |

---

## Recommendation

### For Your Use Case

**Primary Recommendation: WebSocket**

**Why:**
1. **Real-time delivery** - Commands arrive immediately when controller decides
2. **Bidirectional** - Can send events/heartbeats over same connection (optional optimization)
3. **Standard** - Well-supported, mature libraries
4. **Connection state** - Controller knows VM online/offline status
5. **No infrastructure** - No additional services needed

**Implementation Strategy:**
1. **Phase 1**: WebSocket for commands only
   - MIGlet opens WebSocket on startup
   - Controller pushes commands via WebSocket
   - Keep HTTP POST for events/heartbeats (simpler, works well)
   
2. **Phase 2** (Optional): Move events/heartbeats to WebSocket
   - Reduce HTTP overhead
   - Single connection for all communication

### Alternative: SSE (If WebSocket is problematic)

**Why SSE:**
- Simpler than WebSocket
- Works through all firewalls/proxies
- Standard HTTP, no special handling needed
- Good enough for command delivery

**Trade-off:**
- Still need HTTP POST for events/heartbeats (not a big deal)
- Slightly more latency than WebSocket

---

## Implementation Plan

### WebSocket Approach

#### MIGlet Side
1. Add WebSocket client to `pkg/controller/client.go`
2. Open connection in `Ready` state (and keep open)
3. Listen for commands in goroutine
4. Process commands and send acknowledgment
5. Handle reconnection logic

#### Controller Side
1. Add WebSocket handler: `GET /api/v1/vms/{vm_id}/commands/ws`
2. Maintain connection registry (map of VM ID → WebSocket connection)
3. Push commands to connections when available
4. Handle connection lifecycle (connect, disconnect, reconnect)

#### Command Flow
```
1. MIGlet → Controller: WebSocket connection established
2. Controller: Registers connection for VM
3. Controller: Has command ready → pushes via WebSocket
4. MIGlet: Receives command → processes it
5. MIGlet → Controller: Sends acknowledgment via WebSocket
6. Controller: Marks command as acknowledged
```

---

## Questions to Consider

1. **Scale**: How many VMs will connect simultaneously?
   - < 100: WebSocket is fine
   - 100-1000: WebSocket with connection pooling
   - > 1000: Consider message queue

2. **Network**: Are VMs behind firewalls/NAT?
   - If yes: SSE or Long Polling might be easier
   - If no: WebSocket works great

3. **Infrastructure**: Can you deploy additional services?
   - If yes: Message queue provides best reliability
   - If no: WebSocket or SSE

4. **Latency requirements**: How fast must commands arrive?
   - < 1 second: WebSocket or Message Queue
   - < 5 seconds: SSE or Long Polling
   - > 5 seconds: Any option works

---

## Next Steps

1. **Decide on approach** (recommend WebSocket)
2. **Design WebSocket protocol** (message format, ack mechanism)
3. **Implement MIGlet WebSocket client**
4. **Implement Controller WebSocket handler**
5. **Add reconnection logic**
6. **Test with sample controller**
7. **Update documentation**

What do you think? Which approach fits your infrastructure and requirements best?

