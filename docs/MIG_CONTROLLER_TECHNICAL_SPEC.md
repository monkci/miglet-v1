# MIG Controller Technical Specification

## 1. Overview

The MIG Controller is a Go-based service that orchestrates GitHub Actions self-hosted runners across a fleet of VMs managed by Google Cloud Managed Instance Groups (MIGs). It receives job requests via Pub/Sub, manages VM lifecycle, and coordinates with MIGlet agents running inside each VM.

## 2. Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              External Services                                   │
├─────────────────┬─────────────────┬─────────────────┬───────────────────────────┤
│   Pub/Sub       │    GitHub API   │   GCloud API    │         Redis             │
│ (jobs.pool.*)   │   (App Auth)    │   (MIG Mgmt)    │  (Jobs + VM Status)       │
└────────┬────────┴────────┬────────┴────────┬────────┴─────────────┬─────────────┘
         │                 │                 │                     │
         ▼                 ▼                 ▼                     ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              MIG Controller                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐        │
│  │   Message    │  │   MIGlet     │  │  VM Status   │  │  Scheduler   │        │
│  │   Handler    │  │   Handler    │  │   Watcher    │  │              │        │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘        │
│         │                 │                 │                 │                 │
│         │                 │    ┌────────────┴────────────┐    │                 │
│         │                 │    │    State Manager        │    │                 │
│         │                 │    └─────────────────────────┘    │                 │
│         │                 │                                   │                 │
│         ▼                 ▼                                   ▼                 │
│  ┌─────────────────────────────────────────────────────────────────────────┐   │
│  │                         Core Services                                    │   │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌────────────┐         │   │
│  │  │ Token      │  │ VM         │  │ gRPC       │  │ Job        │         │   │
│  │  │ Service    │  │ Manager    │  │ Server     │  │ Queue      │         │   │
│  │  └────────────┘  └────────────┘  └────────────┘  └────────────┘         │   │
│  └─────────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────────┘
         │
         │ gRPC (bidirectional streaming)
         ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              MIG (Managed Instance Group)                        │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐                  │
│  │  VM Instance    │  │  VM Instance    │  │  VM Instance    │   ...            │
│  │  ┌───────────┐  │  │  ┌───────────┐  │  │  ┌───────────┐  │                  │
│  │  │  MIGlet   │  │  │  │  MIGlet   │  │  │  │  MIGlet   │  │                  │
│  │  └───────────┘  │  │  └───────────┘  │  │  └───────────┘  │                  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘                  │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## 3. Components

### 3.1 Message Handler

**Responsibility:** Parse incoming Pub/Sub messages and enqueue jobs.

```go
type PubSubMessage struct {
    OrgID          string   `json:"org_id"`
    OrgName        string   `json:"org_name"`
    InstallationID int64    `json:"installation_id"`
    RepoFullName   string   `json:"repo_full_name"`
    RunID          int64    `json:"run_id"`
    JobID          int64    `json:"job_id"`
    Labels         []string `json:"labels"`        // e.g., ["self-hosted", "linux", "x64"]
    PoolID         string   `json:"pool_id"`       // Derived from labels or explicit
    Priority       int      `json:"priority"`      // Job priority (optional)
    ReceivedAt     int64    `json:"received_at"`
}
```

**Flow:**
1. Subscribe to `jobs.pool.<pool_id>` topic
2. Parse and validate message
3. Create `Job` record in Redis with status `QUEUED`
4. Acknowledge Pub/Sub message after Redis write succeeds

### 3.2 MIGlet Handler

**Responsibility:** Manage gRPC connections with MIGlet agents.

```go
type MIGletConnection struct {
    VMID        string
    PoolID      string
    OrgID       string
    Stream      commands.CommandService_StreamCommandsServer
    MigletState string    // initializing, connecting, ready, idle, job_running, etc.
    RunnerState string    // idle, running, offline
    LastSeen    time.Time
    CurrentJob  *JobInfo
}

type MIGletHandler struct {
    connections sync.Map           // map[vmID]*MIGletConnection
    stateStore  *VMStateStore      // Redis-backed state store
}
```

**Responsibilities:**
- Accept gRPC connections from MIGlets
- Track connection state per VM
- Forward `register_runner` commands to MIGlets
- Receive heartbeats and update VM state
- Handle connection lifecycle (connect, disconnect, reconnect)

### 3.3 VM Status Watcher

**Responsibility:** Monitor VM infrastructure state from GCloud.

```go
type VMInfraState string

const (
    VMInfraRunning    VMInfraState = "RUNNING"
    VMInfraStopped    VMInfraState = "TERMINATED"
    VMInfraStaging    VMInfraState = "STAGING"
    VMInfraStopping   VMInfraState = "STOPPING"
    VMInfraProvisioning VMInfraState = "PROVISIONING"
)

type VMInstance struct {
    Name         string
    Zone         string
    InfraState   VMInfraState  // From GCloud API
    MigletState  string        // From MIGlet heartbeat
    RunnerState  string        // From MIGlet heartbeat
    LastHeartbeat time.Time
    CurrentJobID string
    CreatedAt    time.Time
}
```

**Polling Logic:**
```
Every 30 seconds:
  1. Call compute.instanceGroupManagers.listManagedInstances()
  2. For each instance:
     - Update infra state in Redis
     - Compare with MIGlet heartbeat data
     - Detect stuck/unhealthy VMs
  3. Clean up stale entries for deleted VMs
```

### 3.4 Scheduler

**Responsibility:** Assign jobs to VMs based on availability.

```go
type Scheduler struct {
    jobQueue    *JobQueue
    vmStore     *VMStateStore
    migletHandler *MIGletHandler
    vmManager   *VMManager
    tokenService *TokenService
}
```

**Scheduling Algorithm:**

```
func (s *Scheduler) ProcessNextJob() error {
    // 1. Pick job from queue
    job := s.jobQueue.Pop()
    if job == nil {
        return nil // No jobs
    }

    // 2. Find suitable VM
    vm := s.findAvailableVM(job.PoolID)
    
    switch {
    case vm != nil && vm.MigletState == "ready":
        // Case A: Ready VM available
        return s.assignJobToVM(job, vm)
        
    case vm != nil && vm.InfraState == "TERMINATED":
        // Case B: Stopped VM exists - start it
        return s.startVMAndAssign(job, vm)
        
    default:
        // Case C: No VMs available - create new
        return s.createVMAndAssign(job)
    }
}
```

## 4. Data Models

### 4.1 Redis Schema

#### Jobs Redis

```
# Job Queue (sorted set by priority + timestamp)
KEY: jobs:queue:{pool_id}
SCORE: priority * 1000000000 + timestamp
VALUE: job_id

# Job Details (hash)
KEY: jobs:details:{job_id}
FIELDS:
  - status: QUEUED | ASSIGNED | RUNNING | COMPLETED | FAILED
  - org_id
  - org_name
  - installation_id
  - repo_full_name
  - run_id
  - labels (JSON array)
  - pool_id
  - assigned_vm_id
  - assigned_at
  - started_at
  - completed_at
  - error_message
  - created_at

# Job by VM (for quick lookup)
KEY: jobs:by_vm:{vm_id}
VALUE: job_id (current job)
```

#### VM Status Redis

```
# VM Details (hash)
KEY: vms:{pool_id}:{vm_id}
FIELDS:
  - infra_state: RUNNING | TERMINATED | STAGING | ...
  - miglet_state: initializing | connecting | ready | idle | job_running | ...
  - runner_state: idle | running | offline
  - last_heartbeat: timestamp
  - current_job_id
  - cpu_usage
  - memory_usage
  - created_at
  - zone

# VMs by State (sets for quick filtering)
KEY: vms:by_state:{pool_id}:ready
MEMBERS: vm_id, vm_id, ...

KEY: vms:by_state:{pool_id}:stopped
MEMBERS: vm_id, vm_id, ...

KEY: vms:by_state:{pool_id}:idle
MEMBERS: vm_id, vm_id, ...

# Pool Stats (hash)
KEY: pools:stats:{pool_id}
FIELDS:
  - total_vms
  - running_vms
  - ready_vms
  - busy_vms
  - stopped_vms
  - queued_jobs
```

### 4.2 State Transitions

#### Combined VM State Matrix

| Infra State | MIGlet State | Runner State | Effective State | Can Accept Job? |
|-------------|--------------|--------------|-----------------|-----------------|
| TERMINATED  | -            | -            | `STOPPED`       | No (start first)|
| STAGING     | -            | -            | `STARTING`      | No (wait)       |
| RUNNING     | initializing | -            | `BOOTING`       | No (wait)       |
| RUNNING     | connecting   | -            | `CONNECTING`    | No (wait)       |
| RUNNING     | ready        | offline      | `READY`         | **YES**         |
| RUNNING     | idle         | idle         | `IDLE`          | **YES**         |
| RUNNING     | job_running  | running      | `BUSY`          | No              |
| RUNNING     | error        | -            | `ERROR`         | No (investigate)|
| STOPPING    | -            | -            | `STOPPING`      | No              |

## 5. Core Services

### 5.1 Token Service

**Responsibility:** Generate GitHub runner registration tokens.

```go
type TokenService struct {
    githubAppID    int64
    privateKeyPath string
    privateKey     *rsa.PrivateKey
}

type RegistrationToken struct {
    Token     string    `json:"token"`
    ExpiresAt time.Time `json:"expires_at"`
}

func (t *TokenService) GetRegistrationToken(installationID int64, repoOrOrg string) (*RegistrationToken, error) {
    // 1. Generate JWT from GitHub App credentials
    jwt := t.generateAppJWT()
    
    // 2. Get installation access token
    accessToken := t.getInstallationToken(jwt, installationID)
    
    // 3. Create runner registration token
    token := t.createRegistrationToken(accessToken, repoOrOrg)
    
    return token, nil
}
```

### 5.2 VM Manager

**Responsibility:** Manage VM lifecycle via GCloud API.

```go
type VMManager struct {
    computeService *compute.Service
    projectID      string
    zone           string
    migName        string
}

// Start a stopped VM
func (m *VMManager) StartVM(vmName string) error {
    _, err := m.computeService.Instances.Start(m.projectID, m.zone, vmName).Do()
    return err
}

// Create new VM in MIG (resize)
func (m *VMManager) ScaleUp(count int) error {
    mig, _ := m.computeService.InstanceGroupManagers.Get(m.projectID, m.zone, m.migName).Do()
    newSize := mig.TargetSize + int64(count)
    
    _, err := m.computeService.InstanceGroupManagers.Resize(
        m.projectID, m.zone, m.migName, newSize,
    ).Do()
    return err
}

// Stop a VM (for cost savings)
func (m *VMManager) StopVM(vmName string) error {
    _, err := m.computeService.Instances.Stop(m.projectID, m.zone, vmName).Do()
    return err
}
```

### 5.3 gRPC Server

**Responsibility:** Handle bidirectional streaming with MIGlets.

```go
type GRPCServer struct {
    commands.UnimplementedCommandServiceServer
    migletHandler *MIGletHandler
    vmStore       *VMStateStore
}

func (s *GRPCServer) StreamCommands(stream commands.CommandService_StreamCommandsServer) error {
    var vmID, poolID string
    
    for {
        msg, err := stream.Recv()
        if err != nil {
            s.migletHandler.HandleDisconnect(vmID)
            return err
        }
        
        switch m := msg.Message.(type) {
        case *commands.MIGletMessage_Connect:
            vmID = m.Connect.VmId
            poolID = m.Connect.PoolId
            s.migletHandler.HandleConnect(vmID, poolID, stream)
            
        case *commands.MIGletMessage_Heartbeat:
            s.handleHeartbeat(vmID, m.Heartbeat)
            
        case *commands.MIGletMessage_CommandAck:
            s.handleCommandAck(vmID, m.CommandAck)
            
        case *commands.MIGletMessage_Event:
            s.handleEvent(vmID, m.Event)
        }
    }
}
```

## 6. Scheduling Flow

### 6.1 Job Assignment Flow

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           Job Assignment Flow                                    │
└─────────────────────────────────────────────────────────────────────────────────┘

1. NEW JOB ARRIVES (Pub/Sub)
   │
   ▼
2. INSERT INTO REDIS QUEUE
   jobs:queue:{pool_id} → job_id (with priority score)
   jobs:details:{job_id} → { status: "QUEUED", ... }
   │
   ▼
3. SCHEDULER PICKS JOB
   │
   ├──────────────────────────────────────────────────────────────┐
   │                                                              │
   ▼                                                              ▼
4a. READY VM EXISTS?                                    4b. STOPPED VM EXISTS?
    │                                                             │
    │ YES                                                         │ YES
    ▼                                                             ▼
5a. SEND register_runner COMMAND                       5b. START VM (gcloud API)
    │                                                             │
    ▼                                                             ▼
6a. WAIT FOR CommandAck                                6b. WAIT FOR MIGlet CONNECT
    │                                                             │
    │                                                             ▼
    │                                                  6c. WAIT FOR "ready" STATE
    │                                                             │
    │                                                             ▼
    │                                                  6d. SEND register_runner
    │                                                             │
    ▼                                                             ▼
7. UPDATE JOB STATUS → "ASSIGNED"                     7. UPDATE JOB STATUS → "ASSIGNED"
   │                                                             │
   └─────────────────────────┬───────────────────────────────────┘
                             │
                             ▼
8. WAIT FOR runner_registered EVENT
   │
   ▼
9. WAIT FOR job_started EVENT → UPDATE STATUS → "RUNNING"
   │
   ▼
10. WAIT FOR job_completed EVENT → UPDATE STATUS → "COMPLETED"
    │
    ▼
11. VM RETURNS TO "idle" STATE (ready for next job)

                    ─────────────────────────────────
                    
4c. NO VMs AVAILABLE AT ALL?
    │
    │ YES
    ▼
5c. SCALE UP MIG (create new VM)
    │
    ▼
6c. WAIT FOR VM PROVISIONING
    │
    ▼
7c. WAIT FOR MIGlet CONNECT + "ready" STATE
    │
    ▼
8c. SEND register_runner COMMAND
    │
    ▼
9c. CONTINUE FROM STEP 7...
```

### 6.2 VM State Management Flow

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         VM State Management                                      │
└─────────────────────────────────────────────────────────────────────────────────┘

               GCloud API                    MIGlet Heartbeat
                   │                              │
                   ▼                              ▼
        ┌─────────────────┐            ┌─────────────────┐
        │  VM Status      │            │  MIGlet         │
        │  Watcher        │            │  Handler        │
        └────────┬────────┘            └────────┬────────┘
                 │                              │
                 │   infra_state                │   miglet_state
                 │                              │   runner_state
                 │                              │   current_job
                 ▼                              ▼
        ┌─────────────────────────────────────────────────┐
        │              VM State Store (Redis)             │
        │                                                 │
        │  vms:{pool_id}:{vm_id} = {                     │
        │    infra_state: "RUNNING",                     │
        │    miglet_state: "ready",                      │
        │    runner_state: "idle",                       │
        │    last_heartbeat: 1702214456,                 │
        │    current_job_id: null                        │
        │  }                                              │
        └────────────────────┬────────────────────────────┘
                             │
                             │ State change triggers
                             ▼
        ┌─────────────────────────────────────────────────┐
        │              State Change Handlers              │
        │                                                 │
        │  - ready → Notify scheduler (VM available)     │
        │  - error → Alert + investigate                 │
        │  - offline (timeout) → Mark unhealthy          │
        │  - job_running → Update job status             │
        └─────────────────────────────────────────────────┘
```

## 7. Configuration

### 7.1 Controller Configuration

```yaml
# config/controller.yaml

server:
  grpc_port: 50051
  http_port: 8080
  
gcp:
  project_id: "monkci-prod"
  zone: "us-central1-a"
  mig_name: "monkci-runners-2vcpu"
  
github_app:
  app_id: 12345
  private_key_path: "/etc/controller/github-app-key.pem"
  
redis:
  jobs:
    host: "redis-jobs.internal:6379"
    password_env: "REDIS_JOBS_PASSWORD"
    db: 0
  vm_status:
    host: "redis-vmstatus.internal:6379"
    password_env: "REDIS_VM_PASSWORD"
    db: 0

pubsub:
  project_id: "monkci-prod"
  subscription: "jobs-pool-2vcpu-sub"

scheduler:
  poll_interval: "1s"
  assignment_timeout: "5m"
  max_concurrent_assignments: 10
  
vm_manager:
  poll_interval: "30s"
  heartbeat_timeout: "60s"
  max_scale_up_per_minute: 5
  min_ready_vms: 2      # Always keep 2 VMs ready (warm pool)
  max_vms: 50           # Hard limit on MIG size

logging:
  level: "info"
  format: "json"
```

### 7.2 Environment Variables

```bash
# GCP Authentication
GOOGLE_APPLICATION_CREDENTIALS="/etc/controller/gcp-sa-key.json"

# GitHub App
GITHUB_APP_ID="12345"
GITHUB_APP_PRIVATE_KEY_PATH="/etc/controller/github-app-key.pem"

# Redis
REDIS_JOBS_HOST="redis-jobs.internal:6379"
REDIS_JOBS_PASSWORD="secret123"
REDIS_VM_HOST="redis-vmstatus.internal:6379"
REDIS_VM_PASSWORD="secret456"

# Pub/Sub
PUBSUB_PROJECT_ID="monkci-prod"
PUBSUB_SUBSCRIPTION="jobs-pool-2vcpu-sub"

# GCP MIG
GCP_PROJECT_ID="monkci-prod"
GCP_ZONE="us-central1-a"
GCP_MIG_NAME="monkci-runners-2vcpu"

# Controller
GRPC_PORT="50051"
HTTP_PORT="8080"
```

## 8. API Endpoints

### 8.1 gRPC Service (for MIGlets)

```protobuf
service CommandService {
  rpc StreamCommands(stream MIGletMessage) returns (stream ControllerMessage);
}
```

### 8.2 HTTP Endpoints (for Admin/Monitoring)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |
| `/metrics` | GET | Prometheus metrics |
| `/api/v1/pools/{pool_id}/stats` | GET | Pool statistics |
| `/api/v1/pools/{pool_id}/vms` | GET | List VMs in pool |
| `/api/v1/pools/{pool_id}/jobs` | GET | List jobs in pool |
| `/api/v1/vms/{vm_id}` | GET | VM details |
| `/api/v1/vms/{vm_id}/drain` | POST | Drain VM (finish job, don't accept new) |
| `/api/v1/jobs/{job_id}` | GET | Job details |
| `/api/v1/jobs/{job_id}/cancel` | POST | Cancel job |

## 9. Error Handling

### 9.1 Failure Scenarios

| Scenario | Detection | Response |
|----------|-----------|----------|
| MIGlet disconnects | gRPC stream error | Mark VM unhealthy, reassign job after timeout |
| Heartbeat timeout | No heartbeat for 60s | Mark VM unhealthy, investigate |
| Job stuck | No completion after max_duration | Cancel job, restart VM |
| GCloud API failure | API error | Retry with backoff, alert after N failures |
| Redis unavailable | Connection error | Fail-closed, reject new jobs |
| Pub/Sub unavailable | Connection error | Messages queue in Pub/Sub |
| Token generation fails | GitHub API error | Retry with backoff, fail job after N retries |

### 9.2 Job Failure Recovery

```go
func (s *Scheduler) HandleJobFailure(job *Job, reason string) {
    // 1. Update job status
    job.Status = "FAILED"
    job.ErrorMessage = reason
    s.jobStore.Update(job)
    
    // 2. Check retry policy
    if job.RetryCount < job.MaxRetries {
        job.RetryCount++
        job.Status = "QUEUED"
        s.jobQueue.Push(job)
        return
    }
    
    // 3. Alert on final failure
    s.alertService.JobFailed(job)
}
```

## 10. Scaling Considerations

### 10.1 Warm Pool Strategy

Maintain a minimum number of ready VMs to reduce job latency:

```go
func (m *VMManager) EnsureWarmPool(poolID string, minReady int) {
    readyCount := m.vmStore.CountByState(poolID, "ready")
    deficit := minReady - readyCount
    
    if deficit > 0 {
        // First try to start stopped VMs
        stoppedVMs := m.vmStore.GetByState(poolID, "stopped")
        toStart := min(len(stoppedVMs), deficit)
        
        for i := 0; i < toStart; i++ {
            m.StartVM(stoppedVMs[i].Name)
        }
        
        // If still need more, scale up MIG
        stillNeeded := deficit - toStart
        if stillNeeded > 0 {
            m.ScaleUp(stillNeeded)
        }
    }
}
```

### 10.2 Cost Optimization

```go
func (m *VMManager) IdleVMCleanup() {
    idleVMs := m.vmStore.GetIdleVMs(idleThreshold)
    
    for _, vm := range idleVMs {
        // Keep minimum warm pool
        if m.vmStore.CountReady() > m.config.MinReadyVMs {
            m.StopVM(vm.Name)
        }
    }
}
```

## 11. Metrics & Observability

### 11.1 Key Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `controller_jobs_queued` | Gauge | Jobs waiting in queue |
| `controller_jobs_running` | Gauge | Jobs currently executing |
| `controller_jobs_completed_total` | Counter | Total completed jobs |
| `controller_jobs_failed_total` | Counter | Total failed jobs |
| `controller_vms_total` | Gauge | Total VMs in pool |
| `controller_vms_ready` | Gauge | VMs ready for jobs |
| `controller_vms_busy` | Gauge | VMs running jobs |
| `controller_vms_stopped` | Gauge | Stopped VMs |
| `controller_job_assignment_duration` | Histogram | Time to assign job |
| `controller_job_wait_duration` | Histogram | Time job waits in queue |
| `controller_vm_startup_duration` | Histogram | Time for VM to become ready |
| `controller_grpc_connections` | Gauge | Active MIGlet connections |

### 11.2 Logging

```json
{
  "level": "info",
  "ts": "2025-12-10T12:34:56Z",
  "component": "scheduler",
  "event": "job_assigned",
  "job_id": "job-123",
  "vm_id": "vm-456",
  "pool_id": "pool-2vcpu",
  "queue_time_ms": 150,
  "assignment_time_ms": 25
}
```

## 12. Directory Structure

```
mig-controller/
├── cmd/
│   └── controller/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── pubsub/
│   │   ├── subscriber.go
│   │   └── message.go
│   ├── scheduler/
│   │   ├── scheduler.go
│   │   └── algorithm.go
│   ├── grpc/
│   │   ├── server.go
│   │   └── handlers.go
│   ├── vm/
│   │   ├── manager.go
│   │   ├── watcher.go
│   │   └── state.go
│   ├── token/
│   │   ├── service.go
│   │   └── github.go
│   ├── redis/
│   │   ├── jobs.go
│   │   └── vmstatus.go
│   └── handlers/
│       ├── message.go
│       └── miglet.go
├── pkg/
│   └── logger/
│       └── logger.go
├── proto/
│   └── commands/
│       └── commands.proto
├── configs/
│   └── controller.yaml
├── deploy/
│   ├── kubernetes/
│   └── systemd/
├── scripts/
│   └── generate-proto.sh
└── go.mod
```

## 13. Deployment

### 13.1 Prerequisites

- GCP Project with:
  - Compute Engine API enabled
  - Pub/Sub API enabled
  - Service Account with roles:
    - `roles/compute.instanceAdmin.v1`
    - `roles/pubsub.subscriber`
- Redis instances (or Cloud Memorystore)
- GitHub App with runner registration permissions

### 13.2 Deployment Options

1. **GKE Deployment** (Recommended for production)
2. **Compute Engine VM** (Simpler for single-pool setup)
3. **Cloud Run** (Stateless, but need external Redis)

## 14. Security

- gRPC: Use TLS for MIGlet connections
- GitHub App: Store private key securely (Secret Manager)
- Redis: Use AUTH and TLS
- GCP: Use Workload Identity for GKE deployments
- Rate limit job submissions per org
- Validate Pub/Sub message signatures

