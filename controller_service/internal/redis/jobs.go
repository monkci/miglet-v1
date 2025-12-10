package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/pkg/logger"
)

// JobStatus represents the status of a job
type JobStatus string

const (
	JobStatusQueued    JobStatus = "QUEUED"
	JobStatusAssigned  JobStatus = "ASSIGNED"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusCompleted JobStatus = "COMPLETED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCancelled JobStatus = "CANCELLED"
)

// Job represents a job in the queue
type Job struct {
	ID             string    `json:"id"`
	OrgID          string    `json:"org_id"`
	OrgName        string    `json:"org_name"`
	InstallationID int64     `json:"installation_id"`
	RepoFullName   string    `json:"repo_full_name"`
	RunID          int64     `json:"run_id"`
	JobID          int64     `json:"job_id"`
	Labels         []string  `json:"labels"`
	PoolID         string    `json:"pool_id"`
	Priority       int       `json:"priority"`
	Status         JobStatus `json:"status"`
	AssignedVMID   string    `json:"assigned_vm_id,omitempty"`
	AssignedAt     time.Time `json:"assigned_at,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	RetryCount     int       `json:"retry_count"`
	MaxRetries     int       `json:"max_retries"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// JobStore handles job persistence in Redis
type JobStore struct {
	client *redis.Client
	poolID string
}

// NewJobStore creates a new job store
func NewJobStore(cfg *config.RedisInstanceConfig, poolID string) (*JobStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log := logger.WithComponent("job_store")
	log.Info("Connected to Jobs Redis")

	return &JobStore{
		client: client,
		poolID: poolID,
	}, nil
}

// Close closes the Redis connection
func (s *JobStore) Close() error {
	return s.client.Close()
}

// Enqueue adds a job to the queue
func (s *JobStore) Enqueue(ctx context.Context, job *Job) error {
	job.Status = JobStatusQueued
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	job.MaxRetries = 3

	// Store job details
	if err := s.saveJob(ctx, job); err != nil {
		return fmt.Errorf("failed to save job: %w", err)
	}

	// Add to queue (sorted set with priority + timestamp score)
	score := float64(job.Priority)*1e12 + float64(job.CreatedAt.UnixNano())
	queueKey := fmt.Sprintf("jobs:queue:%s", s.poolID)

	if err := s.client.ZAdd(ctx, queueKey, redis.Z{
		Score:  score,
		Member: job.ID,
	}).Err(); err != nil {
		return fmt.Errorf("failed to add job to queue: %w", err)
	}

	log := logger.WithJob(job.ID, s.poolID)
	log.Info("Job enqueued")

	return nil
}

// Dequeue removes and returns the highest priority job from the queue
func (s *JobStore) Dequeue(ctx context.Context) (*Job, error) {
	queueKey := fmt.Sprintf("jobs:queue:%s", s.poolID)

	// Pop the job with lowest score (highest priority + oldest)
	result, err := s.client.ZPopMin(ctx, queueKey, 1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to pop job from queue: %w", err)
	}

	if len(result) == 0 {
		return nil, nil // No jobs available
	}

	jobID := result[0].Member.(string)
	return s.Get(ctx, jobID)
}

// Peek returns the next job without removing it
func (s *JobStore) Peek(ctx context.Context) (*Job, error) {
	queueKey := fmt.Sprintf("jobs:queue:%s", s.poolID)

	result, err := s.client.ZRange(ctx, queueKey, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to peek job: %w", err)
	}

	if len(result) == 0 {
		return nil, nil
	}

	return s.Get(ctx, result[0])
}

// Get retrieves a job by ID
func (s *JobStore) Get(ctx context.Context, jobID string) (*Job, error) {
	key := fmt.Sprintf("jobs:details:%s", jobID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}

	return &job, nil
}

// Update updates a job
func (s *JobStore) Update(ctx context.Context, job *Job) error {
	job.UpdatedAt = time.Now()
	return s.saveJob(ctx, job)
}

// AssignToVM assigns a job to a VM
func (s *JobStore) AssignToVM(ctx context.Context, jobID, vmID string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = JobStatusAssigned
	job.AssignedVMID = vmID
	job.AssignedAt = time.Now()

	if err := s.Update(ctx, job); err != nil {
		return err
	}

	// Track job by VM
	vmJobKey := fmt.Sprintf("jobs:by_vm:%s", vmID)
	if err := s.client.Set(ctx, vmJobKey, jobID, 0).Err(); err != nil {
		return fmt.Errorf("failed to track job by VM: %w", err)
	}

	return nil
}

// MarkRunning marks a job as running
func (s *JobStore) MarkRunning(ctx context.Context, jobID string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = JobStatusRunning
	job.StartedAt = time.Now()
	return s.Update(ctx, job)
}

// MarkCompleted marks a job as completed
func (s *JobStore) MarkCompleted(ctx context.Context, jobID string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = JobStatusCompleted
	job.CompletedAt = time.Now()

	if err := s.Update(ctx, job); err != nil {
		return err
	}

	// Clear job from VM tracking
	if job.AssignedVMID != "" {
		vmJobKey := fmt.Sprintf("jobs:by_vm:%s", job.AssignedVMID)
		s.client.Del(ctx, vmJobKey)
	}

	return nil
}

// MarkFailed marks a job as failed
func (s *JobStore) MarkFailed(ctx context.Context, jobID, errorMsg string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = JobStatusFailed
	job.CompletedAt = time.Now()
	job.ErrorMessage = errorMsg

	if err := s.Update(ctx, job); err != nil {
		return err
	}

	// Clear job from VM tracking
	if job.AssignedVMID != "" {
		vmJobKey := fmt.Sprintf("jobs:by_vm:%s", job.AssignedVMID)
		s.client.Del(ctx, vmJobKey)
	}

	return nil
}

// Requeue puts a job back in the queue for retry
func (s *JobStore) Requeue(ctx context.Context, jobID string) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.RetryCount++
	job.Status = JobStatusQueued
	job.AssignedVMID = ""
	job.AssignedAt = time.Time{}
	job.ErrorMessage = ""

	if err := s.Update(ctx, job); err != nil {
		return err
	}

	// Add back to queue
	score := float64(job.Priority)*1e12 + float64(time.Now().UnixNano())
	queueKey := fmt.Sprintf("jobs:queue:%s", s.poolID)

	return s.client.ZAdd(ctx, queueKey, redis.Z{
		Score:  score,
		Member: job.ID,
	}).Err()
}

// GetByVM returns the current job for a VM
func (s *JobStore) GetByVM(ctx context.Context, vmID string) (*Job, error) {
	vmJobKey := fmt.Sprintf("jobs:by_vm:%s", vmID)
	jobID, err := s.client.Get(ctx, vmJobKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return s.Get(ctx, jobID)
}

// QueueLength returns the number of jobs in the queue
func (s *JobStore) QueueLength(ctx context.Context) (int64, error) {
	queueKey := fmt.Sprintf("jobs:queue:%s", s.poolID)
	return s.client.ZCard(ctx, queueKey).Result()
}

// saveJob saves job details to Redis
func (s *JobStore) saveJob(ctx context.Context, job *Job) error {
	key := fmt.Sprintf("jobs:details:%s", job.ID)
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	// Store with 7-day expiry
	return s.client.Set(ctx, key, data, 7*24*time.Hour).Err()
}

