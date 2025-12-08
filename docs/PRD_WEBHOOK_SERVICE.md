# Webhook Service Product Requirements Document (PRD)

## Document Information

| Field | Value |
|-------|-------|
| Product Name | MonkCI Webhook Service |
| Version | 1.0.0 |
| Last Updated | December 2024 |
| Status | Planning |
| Author | MonkCI Team |

---

## 1. Executive Summary

The Webhook Service is the entry point for all incoming workflow job events from GitHub Actions. It serves as the first component in the MonkCI execution pipeline, responsible for receiving, validating, parsing, persisting, and publishing GitHub webhook events for downstream processing.

This service is designed to be stateless, highly available, and horizontally scalable. It operates on a single principle: "accept → validate → persist → enqueue" — nothing more.

The Webhook Service does not make scheduling decisions, manage VMs, or interact with GitHub runners. Its sole purpose is reliable, fast ingestion of workflow events with guaranteed at-least-once delivery.

---

## 2. Problem Statement

### 2.1 Current Challenges

1. **Unreliable Event Ingestion**: Without proper webhook handling, GitHub events can be lost during network issues or service restarts.

2. **Duplicate Processing**: GitHub retries webhook delivery on failures, potentially causing duplicate job creation without idempotency handling.

3. **Security Vulnerabilities**: Unvalidated webhooks expose the system to forged requests and replay attacks.

4. **Bottleneck Potential**: Webhook processing must be fast to avoid GitHub timeouts and ensure reliable acknowledgment.

5. **Lack of Observability**: Without proper logging and metrics, debugging webhook issues becomes impossible at scale.

### 2.2 Target Users

- **Platform Engineers**: Building and maintaining the MonkCI infrastructure
- **Operations Teams**: Monitoring webhook health and troubleshooting issues
- **Security Teams**: Ensuring webhook authenticity and preventing attacks

---

## 3. Goals and Objectives

### 3.1 Primary Goals

| Goal ID | Goal | Description |
|---------|------|-------------|
| G1 | Reliable Event Reception | Accept webhook events from GitHub with zero loss |
| G2 | Authentication & Validation | Validate and authenticate all incoming events using GitHub signatures |
| G3 | Metadata Extraction | Parse job information and extract runner labels, pool info, and machine type |
| G4 | Persistent Storage | Store job metadata in database with initial state for tracking |
| G5 | Event Publishing | Publish validated events to job scheduling message bus |
| G6 | Idempotent Processing | Ensure at-least-once delivery without creating duplicate jobs |
| G7 | Low Latency | Process events with average latency under 200ms |

### 3.2 Success Metrics

| Metric | Target |
|--------|--------|
| Average processing time | < 200ms |
| P90 latency | < 500ms |
| P99 latency | < 1000ms |
| Sustained throughput | 5,000 webhooks/minute |
| Uptime | 99.9% |
| Duplicate prevention rate | 100% |
| Signature validation accuracy | 100% |

---

## 4. Non-Goals and Exclusions

The Webhook Service explicitly does NOT:

| Exclusion | Responsibility |
|-----------|----------------|
| Assign VMs or start MIG instances | MIG Controller |
| Communicate with GitHub runners | MIGlet |
| Perform scheduling decisions | Scheduler Service |
| Manage VM lifecycle or capacity | MIG Controller |
| Generate registration tokens | MIG Controller |
| Interact with GCP APIs | MIG Controller |
| Manage job execution state beyond RECEIVED | Scheduler / MIG Controller |

The Webhook Service is intentionally limited in scope to ensure simplicity, reliability, and horizontal scalability.

---

## 5. System Context

### 5.1 Position in Architecture

```
GitHub ──webhook──→ [Webhook Service] ──publish──→ Message Queue ──→ [Scheduler]
                           │
                           ▼
                      [Database]
```

### 5.2 Upstream Dependencies

| Dependency | Description |
|------------|-------------|
| GitHub Webhooks | Source of workflow_job and workflow_run events |
| GitHub App | Provides webhook secret for signature validation |

### 5.3 Downstream Dependencies

| Dependency | Description |
|------------|-------------|
| Database | Stores job metadata and state |
| Message Queue | Receives published job events for scheduling |
| Scheduler Service | Consumes events from queue |

---

## 6. Inputs and Event Types

### 6.1 Supported GitHub Events

| Event Type | Usage | Priority |
|------------|-------|----------|
| `workflow_job` | Primary trigger for creating execution requests | P0 - Required |
| `workflow_run` | Advanced analytics and UX features | P1 - Future |

### 6.2 Event Payload Fields

#### 6.2.1 Required Fields

| Field | Source Path | Description |
|-------|-------------|-------------|
| `installation_id` | `installation.id` | Identifies the GitHub App installation (customer/org) |
| `job_id` | `workflow_job.id` | Unique GitHub job identifier |
| `run_id` | `workflow_job.run_id` | Workflow run identifier |
| `repository` | `repository.full_name` | Organization/repository slug |
| `labels` | `workflow_job.labels` | Runner selector labels (determines pool) |
| `action` | `action` | Event action (queued, in_progress, completed) |
| `created_at` | `workflow_job.created_at` | Event timestamp |

#### 6.2.2 Optional Fields

| Field | Source Path | Description |
|-------|-------------|-------------|
| `workflow_name` | `workflow_job.workflow_name` | Name of the workflow |
| `job_name` | `workflow_job.name` | Name of the job |
| `sender` | `sender.login` | GitHub actor (for audit/billing) |
| `head_sha` | `workflow_job.head_sha` | Commit SHA |
| `head_branch` | `workflow_job.head_branch` | Branch name |
| `runner_group_id` | `workflow_job.runner_group_id` | Runner group if specified |

#### 6.2.3 Derived Fields

| Field | Derivation Logic | Description |
|-------|------------------|-------------|
| `os` | Parsed from labels (default: linux) | Operating system |
| `arch` | Parsed from labels (default: amd64) | CPU architecture |
| `machine_type` | Mapped from labels | Pool/machine type identifier |
| `pool_id` | Label mapping table | Target pool for scheduling |

---

## 7. High-Level Processing Flow

### 7.1 Request Flow

1. **Receive Request**: GitHub sends POST request to `/v1/webhook/github`

2. **Validate Signature**: Verify HMAC signature using webhook secret
   - Reject if signature missing or invalid
   - Log verification result

3. **Parse Payload**: Extract and validate JSON payload
   - Reject if malformed
   - Identify event type

4. **Filter Events**: Check if event type is supported
   - Ignore unsupported events (return 200)
   - Only process `workflow_job` with `action=queued`

5. **Extract Metadata**: Parse key fields into JobRequest structure
   - Normalize data formats
   - Derive additional fields (OS, arch, pool)

6. **Check Idempotency**: Query database for existing job
   - Key: `(installation_id, job_id)`
   - If exists: Return 200, update last_seen timestamp
   - If new: Continue processing

7. **Persist to Database**: Create job_runs entry
   - State: `RECEIVED`
   - Include all extracted metadata
   - Record server timestamp

8. **Publish to Queue**: Send `JOB_RECEIVED` message
   - Topic: `jobs.incoming`
   - Wait for publish acknowledgment

9. **Respond to GitHub**: Return 200 OK
   - Only after successful publish
   - Include request ID for correlation

### 7.2 Action Filtering

| Action | Processing |
|--------|------------|
| `queued` | Full processing - create job, publish event |
| `in_progress` | Update existing job state (future) |
| `completed` | Update existing job state (future) |
| `waiting` | Log and ignore (future consideration) |

---

## 8. Functional Requirements

### 8.1 API Endpoint

#### F1.1 Endpoint Specification

| Attribute | Value |
|-----------|-------|
| Method | POST |
| Path | `/v1/webhook/github` |
| Protocol | HTTPS only |
| Content-Type | application/json |
| Authentication | GitHub webhook signature |

#### F1.2 Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-GitHub-Event` | Yes | Event type (workflow_job, workflow_run) |
| `X-GitHub-Delivery` | Yes | Unique delivery ID |
| `X-Hub-Signature-256` | Yes | HMAC-SHA256 signature |
| `Content-Type` | Yes | Must be application/json |
| `User-Agent` | No | GitHub-Hookshot/* |

#### F1.3 Response Codes

| Code | Condition |
|------|-----------|
| 200 | Event processed successfully (including duplicates) |
| 400 | Malformed payload (logged, not retried) |
| 401 | Invalid or missing signature |
| 500 | Internal error (GitHub will retry) |

### 8.2 Event Validation

#### F2.1 Signature Verification

- Compute HMAC-SHA256 of raw request body
- Compare with `X-Hub-Signature-256` header
- Use timing-safe comparison to prevent timing attacks
- Reject if verification fails

#### F2.2 Rejection Conditions

| Condition | Response | Action |
|-----------|----------|--------|
| Missing signature headers | 401 | Log, reject |
| Invalid signature | 401 | Log, reject |
| Malformed JSON | 400 | Log, reject |
| Unsupported event type | 200 | Log, ignore |
| Missing required fields | 200 | Log, ignore |
| Known duplicate event | 200 | Log, no-op |

### 8.3 Metadata Extraction and Parsing

#### F3.1 Label Parsing

Labels are parsed to determine:

| Label Pattern | Derived Value |
|---------------|---------------|
| `self-hosted` | Required for MonkCI runners |
| `linux`, `macos`, `windows` | Operating system |
| `x64`, `arm64` | CPU architecture |
| `monkci-*` | Custom pool/machine type |
| `small`, `medium`, `large` | Machine size |

#### F3.2 Pool Mapping

| Input Labels | Pool ID |
|--------------|---------|
| `[self-hosted, linux, x64, medium]` | `linux-x64-medium` |
| `[self-hosted, linux, arm64, large]` | `linux-arm64-large` |
| `[self-hosted, monkci-gpu]` | `gpu-pool` |

Mapping table is configurable and stored in service configuration.

#### F3.3 Data Normalization

| Field | Normalization |
|-------|---------------|
| Repository | Lowercase, trim whitespace |
| Labels | Lowercase, deduplicate, sort |
| Timestamps | Convert to UTC, ISO 8601 format |
| IDs | Store as strings for consistency |

### 8.4 Idempotency Handling

#### F4.1 Identity Key

- Primary key: `(installation_id, job_id)`
- Must be unique across all job entries
- Used for duplicate detection

#### F4.2 Duplicate Handling

| Scenario | Action |
|----------|--------|
| New job | Create entry, publish event |
| Duplicate (same action) | Return 200, update last_seen |
| Duplicate (different action) | Update state if applicable |

#### F4.3 Idempotency Window

- Check within last 24 hours for performance
- Older duplicates assumed impossible (GitHub behavior)

### 8.5 Database Persistence

#### F5.1 Job Runs Table Schema

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | Internal unique identifier |
| `job_id` | String | GitHub job ID |
| `run_id` | String | GitHub run ID |
| `installation_id` | String | GitHub App installation ID |
| `repository` | String | Full repository name |
| `labels` | Array[String] | Runner labels |
| `machine_type` | String | Resolved machine type |
| `pool_id` | String | Target pool identifier |
| `os` | String | Operating system |
| `arch` | String | CPU architecture |
| `state` | Enum | Current job state |
| `workflow_name` | String | Workflow name (optional) |
| `job_name` | String | Job name (optional) |
| `head_sha` | String | Commit SHA (optional) |
| `head_branch` | String | Branch name (optional) |
| `sender` | String | GitHub actor (optional) |
| `time_received` | Timestamp | Server receive time |
| `time_github` | Timestamp | GitHub event time |
| `last_seen` | Timestamp | Last duplicate receive time |
| `payload_hash` | String | Hash of payload for debugging |
| `created_at` | Timestamp | Record creation time |
| `updated_at` | Timestamp | Record update time |

#### F5.2 Initial State

All new jobs are created with:

| Field | Value |
|-------|-------|
| `state` | `RECEIVED` |
| `time_received` | Current server timestamp |
| `last_seen` | Current server timestamp |

#### F5.3 Payload Storage

| Option | Description | Recommendation |
|--------|-------------|----------------|
| Full payload | Store complete JSON | Development/debugging only |
| Payload hash | SHA256 hash only | Production (lower storage) |
| Redacted payload | Remove sensitive fields | Compliance environments |

### 8.6 Queue Publishing

#### F6.1 Message Format

| Field | Description |
|-------|-------------|
| `message_id` | Unique message identifier |
| `event_type` | `JOB_RECEIVED` |
| `job_id` | GitHub job ID |
| `run_id` | GitHub run ID |
| `installation_id` | GitHub App installation ID |
| `repository` | Repository full name |
| `pool_id` | Target pool identifier |
| `machine_type` | Resolved machine type |
| `labels` | Runner labels array |
| `os` | Operating system |
| `arch` | CPU architecture |
| `time_received` | Server receive timestamp |
| `time_github` | GitHub event timestamp |
| `correlation_id` | Request correlation ID |

#### F6.2 Publishing Guarantees

| Guarantee | Implementation |
|-----------|----------------|
| At-least-once delivery | Wait for publish acknowledgment |
| Ordering | Not required (jobs are independent) |
| Durability | Message persisted before ack |

#### F6.3 Topic Configuration

| Topic | Purpose |
|-------|---------|
| `jobs.incoming` | All incoming job requests |

Future consideration: Per-priority topics (e.g., `jobs.incoming.high`, `jobs.incoming.low`).

### 8.7 Error Handling

#### F7.1 Error Response Matrix

| Error Type | HTTP Code | GitHub Behavior | Internal Action |
|------------|-----------|-----------------|-----------------|
| Transient DB failure | 500 | Retry | Log, alert, retry internally |
| Transient queue failure | 500 | Retry | Log, alert, retry internally |
| Invalid payload | 200 | No retry | Log, discard |
| Duplicate event | 200 | No retry | Log, no-op |
| Unsupported event | 200 | No retry | Log, ignore |
| Rate limited | 429 | Retry with backoff | Log, apply backpressure |

#### F7.2 Retry Logic

- Internal retries for transient failures: 3 attempts
- Exponential backoff: 100ms, 500ms, 2s
- Circuit breaker for sustained failures
- Dead letter queue for unprocessable messages

#### F7.3 Failure Isolation

- Database failures do not crash service
- Queue failures do not crash service
- Individual request failures do not affect others

---

## 9. Observability Requirements

### 9.1 Structured Logging

#### F8.1 Required Log Fields

| Field | Description |
|-------|-------------|
| `request_id` | Unique request identifier |
| `github_delivery_id` | GitHub's delivery ID |
| `installation_id` | GitHub App installation |
| `job_id` | GitHub job ID |
| `run_id` | GitHub run ID |
| `event_type` | GitHub event type |
| `action` | Event action |
| `processing_result` | new / duplicate / ignored / error |
| `signature_valid` | Signature verification result |
| `processing_time_ms` | Total processing duration |
| `db_write_time_ms` | Database write duration |
| `queue_publish_time_ms` | Queue publish duration |

#### F8.2 Log Levels

| Level | Usage |
|-------|-------|
| DEBUG | Detailed processing steps |
| INFO | Successful event processing |
| WARN | Duplicates, ignored events, retries |
| ERROR | Failures, validation errors |

### 9.2 Metrics

#### F8.3 Counter Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `webhook_requests_total` | event_type, action, result | Total requests received |
| `webhook_signature_validations_total` | result (valid/invalid) | Signature checks |
| `webhook_db_writes_total` | result (success/failure) | Database operations |
| `webhook_queue_publishes_total` | result (success/failure) | Queue operations |
| `webhook_duplicates_total` | - | Duplicate events detected |

#### F8.4 Histogram Metrics

| Metric | Buckets | Description |
|--------|---------|-------------|
| `webhook_processing_duration_seconds` | 0.01, 0.05, 0.1, 0.2, 0.5, 1 | End-to-end latency |
| `webhook_db_write_duration_seconds` | 0.001, 0.01, 0.05, 0.1, 0.5 | DB write latency |
| `webhook_queue_publish_duration_seconds` | 0.001, 0.01, 0.05, 0.1, 0.5 | Queue publish latency |

#### F8.5 Gauge Metrics

| Metric | Description |
|--------|-------------|
| `webhook_active_requests` | Currently processing requests |
| `webhook_queue_depth` | Pending messages in queue |

### 9.3 Health Endpoints

| Endpoint | Purpose | Checks |
|----------|---------|--------|
| `/healthz` | Readiness probe | DB connected, queue connected |
| `/livez` | Liveness probe | Service running |
| `/metrics` | Prometheus metrics | All metrics above |

### 9.4 Alerting Thresholds

| Condition | Severity | Threshold |
|-----------|----------|-----------|
| High error rate | Critical | > 1% of requests failing |
| High latency | Warning | P95 > 500ms |
| Queue backlog | Warning | > 1000 messages |
| Signature failures spike | Critical | > 10/minute |
| Service unavailable | Critical | Any instance down |

---

## 10. Performance and Scale Requirements

### 10.1 Latency Targets

| Percentile | Target |
|------------|--------|
| P50 | < 100ms |
| P90 | < 200ms |
| P95 | < 500ms |
| P99 | < 1000ms |

### 10.2 Throughput Targets

| Metric | Target |
|--------|--------|
| Sustained throughput | 5,000 webhooks/minute |
| Peak throughput | 10,000 webhooks/minute |
| Per-instance capacity | 500 webhooks/minute |
| Horizontal scale factor | 10-20 instances |

### 10.3 Resource Limits

| Resource | Limit |
|----------|-------|
| Memory per instance | 512 MB |
| CPU per instance | 1 vCPU |
| Connections per instance | 100 DB, 50 queue |
| Request timeout | 30 seconds |

### 10.4 Data Durability

| Requirement | Implementation |
|-------------|----------------|
| Job record durability | Strong consistency (synchronous write) |
| Message durability | Persistent queue with acknowledgment |
| Zero job loss | Ack only after DB + queue success |

---

## 11. Security Requirements

### 11.1 Authentication

| Requirement | Implementation |
|-------------|----------------|
| Webhook signature verification | HMAC-SHA256 with shared secret |
| Timing-safe comparison | Prevent timing attacks |
| Signature algorithm | SHA-256 only (reject SHA-1) |

### 11.2 Transport Security

| Requirement | Implementation |
|-------------|----------------|
| HTTPS only | Reject HTTP requests |
| TLS version | TLS 1.2 minimum |
| Certificate validation | Valid CA-signed certificates |

### 11.3 Data Protection

| Requirement | Implementation |
|-------------|----------------|
| No secrets in logs | Redact sensitive fields |
| Payload encryption | Encrypt at rest in database |
| Token handling | No GitHub tokens stored |

### 11.4 Rate Limiting

| Limit | Value |
|-------|-------|
| Per-IP rate limit | 1000 requests/minute |
| Per-installation rate limit | 500 requests/minute |
| Global rate limit | 10,000 requests/minute |

### 11.5 Access Control

| Requirement | Implementation |
|-------------|----------------|
| IP allowlist | Optional for enterprise |
| Webhook origin validation | GitHub IP ranges |
| Admin endpoints | Separate authentication |

---

## 12. Deployment and Operations

### 12.1 Deployment Model

| Aspect | Specification |
|--------|---------------|
| Packaging | Container (Docker) |
| Orchestration | Kubernetes |
| Scaling | Horizontal Pod Autoscaler |
| Load balancing | Cloud load balancer |
| Regions | Multi-region for HA |

### 12.2 Scaling Triggers

| Metric | Scale Up | Scale Down |
|--------|----------|------------|
| CPU usage | > 70% | < 30% |
| Request rate | > 400/min/pod | < 100/min/pod |
| Latency P95 | > 300ms | N/A |

### 12.3 Rolling Updates

| Requirement | Implementation |
|-------------|----------------|
| Zero downtime | Rolling deployment |
| Rollback capability | Instant rollback |
| Health checks | Wait for healthy before routing |
| Drain timeout | 30 seconds |

### 12.4 Configuration Management

| Configuration | Source |
|---------------|--------|
| Webhook secret | Secret manager (Vault, GCP Secret Manager) |
| Database connection | Environment variable |
| Queue connection | Environment variable |
| Feature flags | Config map |
| Label mappings | Config map |

---

## 13. Failure Scenarios and Recovery

### 13.1 Failure Matrix

| Scenario | Detection | Impact | Recovery |
|----------|-----------|--------|----------|
| GitHub sends duplicate webhooks | Idempotency check | None | Automatic (return 200) |
| Database unavailable | Health check failure | New jobs not persisted | Retry until recovery, GitHub retries |
| Queue unreachable | Publish failure | Jobs not scheduled | Block ack, retry, GitHub retries |
| Malformed payload | Parse error | Single request dropped | Log and discard safely |
| Traffic spike | Rate limit hit | Some requests delayed | Auto-scale, backpressure |
| Signature verification failure | Validation check | Request rejected | Log and investigate |
| Instance crash | Health check | Requests to other instances | Auto-restart, load balancer reroutes |

### 13.2 Data Recovery

| Scenario | Recovery Procedure |
|----------|-------------------|
| Missed webhooks | GitHub automatic retry for non-200 |
| Database corruption | Restore from backup, replay from queue |
| Queue message loss | Re-process from database (idempotent) |

### 13.3 Incident Response

| Severity | Response Time | Escalation |
|----------|---------------|------------|
| Critical (service down) | 5 minutes | On-call engineer |
| High (degraded performance) | 15 minutes | On-call engineer |
| Medium (elevated errors) | 1 hour | Engineering team |
| Low (minor issues) | 24 hours | Engineering team |

---

## 14. Testing Requirements

### 14.1 Unit Tests

| Component | Coverage Target |
|-----------|-----------------|
| Signature verification | 100% |
| Payload parsing | 100% |
| Idempotency logic | 100% |
| Error handling | 100% |

### 14.2 Integration Tests

| Test Scenario | Description |
|---------------|-------------|
| End-to-end webhook flow | GitHub payload to queue publish |
| Database persistence | Verify job creation and updates |
| Queue publishing | Verify message format and delivery |
| Duplicate handling | Multiple identical webhooks |

### 14.3 Load Tests

| Test | Target |
|------|--------|
| Sustained load | 5,000 req/min for 1 hour |
| Spike test | 10,000 req/min burst |
| Soak test | 1,000 req/min for 24 hours |

### 14.4 Chaos Tests

| Test | Description |
|------|-------------|
| Database failure | Simulate DB unavailability |
| Queue failure | Simulate queue unavailability |
| Network partition | Simulate network issues |
| Instance termination | Kill random instances |

---

## 15. Dependencies

### 15.1 External Dependencies

| Dependency | Purpose | Criticality |
|------------|---------|-------------|
| GitHub Webhooks | Event source | Critical |
| GitHub App | Webhook secret | Critical |

### 15.2 Internal Dependencies

| Dependency | Purpose | Criticality |
|------------|---------|-------------|
| Database (PostgreSQL/MongoDB) | Job persistence | Critical |
| Message Queue (Pub/Sub/Kafka) | Event publishing | Critical |
| Secret Manager | Webhook secret storage | Critical |
| Metrics System | Observability | Important |

### 15.3 Technology Stack

| Component | Technology |
|-----------|------------|
| Language | Go |
| Web Framework | Standard net/http or Fiber |
| Database Driver | pgx or mongo-driver |
| Queue Client | Cloud Pub/Sub or Kafka client |
| Metrics | Prometheus |
| Logging | Structured JSON (logrus/zap) |

---

## 16. Open Decisions

### 16.1 Pending Decisions

| Decision | Options | Considerations |
|----------|---------|----------------|
| Payload storage | Full / Hash only / Redacted | Cost vs. debuggability |
| Label-to-pool mapping location | Webhook Service / Scheduler | Simplicity vs. flexibility |
| Timestamp source | GitHub vs. Server | Ordering vs. accuracy |
| Write timing | Sync (wait for DB) / Async (ack early) | Reliability vs. latency |
| Multi-region strategy | Active-active / Active-passive | Complexity vs. availability |

### 16.2 Future Considerations

| Feature | Description | Priority |
|---------|-------------|----------|
| Workflow_run events | Support for run-level analytics | P1 |
| Webhook replay | Admin endpoint to replay webhooks | P2 |
| Custom label parsers | Plugin system for label mapping | P3 |
| Webhook transformation | Modify events before publishing | P3 |
