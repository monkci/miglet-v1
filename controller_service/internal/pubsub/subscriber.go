package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/uuid"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/internal/redis"
	"github.com/monkci/mig-controller/pkg/logger"
)

// JobMessage represents a job message from Pub/Sub
type JobMessage struct {
	OrgID          string   `json:"org_id"`
	OrgName        string   `json:"org_name"`
	InstallationID int64    `json:"installation_id"`
	RepoFullName   string   `json:"repo_full_name"`
	RunID          int64    `json:"run_id"`
	JobID          int64    `json:"job_id"`
	Labels         []string `json:"labels"`
	PoolID         string   `json:"pool_id"`
	Priority       int      `json:"priority"`
	ReceivedAt     int64    `json:"received_at"`
}

// Subscriber handles Pub/Sub message consumption
type Subscriber struct {
	cfg      *config.Config
	client   *pubsub.Client
	sub      *pubsub.Subscription
	jobStore *redis.JobStore

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	receivedMessages int64
	processedJobs    int64
	failedMessages   int64
}

// NewSubscriber creates a new Pub/Sub subscriber
func NewSubscriber(cfg *config.Config, jobStore *redis.JobStore) (*Subscriber, error) {
	ctx := context.Background()

	client, err := pubsub.NewClient(ctx, cfg.PubSub.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	sub := client.Subscription(cfg.PubSub.Subscription)

	// Configure subscription settings
	sub.ReceiveSettings.MaxOutstandingMessages = 100
	sub.ReceiveSettings.MaxOutstandingBytes = 10 * 1024 * 1024 // 10MB
	sub.ReceiveSettings.NumGoroutines = 10

	log := logger.WithComponent("pubsub_subscriber")
	log.WithFields(map[string]interface{}{
		"project":      cfg.PubSub.ProjectID,
		"subscription": cfg.PubSub.Subscription,
	}).Info("Pub/Sub subscriber initialized")

	subscriberCtx, cancel := context.WithCancel(context.Background())

	return &Subscriber{
		cfg:      cfg,
		client:   client,
		sub:      sub,
		jobStore: jobStore,
		ctx:      subscriberCtx,
		cancel:   cancel,
	}, nil
}

// Start starts consuming messages
func (s *Subscriber) Start() {
	log := logger.WithComponent("pubsub_subscriber")
	log.Info("Starting Pub/Sub subscriber")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.receiveMessages()
	}()
}

// Stop stops the subscriber
func (s *Subscriber) Stop() error {
	log := logger.WithComponent("pubsub_subscriber")
	log.Info("Stopping Pub/Sub subscriber")

	s.cancel()
	s.wg.Wait()

	if err := s.client.Close(); err != nil {
		return fmt.Errorf("failed to close pubsub client: %w", err)
	}

	log.Info("Pub/Sub subscriber stopped")
	return nil
}

// receiveMessages continuously receives messages from Pub/Sub
func (s *Subscriber) receiveMessages() {
	log := logger.WithComponent("pubsub_subscriber")

	err := s.sub.Receive(s.ctx, func(ctx context.Context, msg *pubsub.Message) {
		s.receivedMessages++

		if err := s.processMessage(ctx, msg); err != nil {
			log.WithError(err).Warn("Failed to process message")
			s.failedMessages++
			// Nack to retry later
			msg.Nack()
			return
		}

		s.processedJobs++
		msg.Ack()
	})

	if err != nil && s.ctx.Err() == nil {
		log.WithError(err).Error("Pub/Sub receive error")
	}
}

// processMessage processes a single Pub/Sub message
func (s *Subscriber) processMessage(ctx context.Context, msg *pubsub.Message) error {
	log := logger.WithComponent("pubsub_subscriber")

	// Parse message
	var jobMsg JobMessage
	if err := json.Unmarshal(msg.Data, &jobMsg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Validate message
	if err := s.validateMessage(&jobMsg); err != nil {
		log.WithError(err).Warn("Invalid message, dropping")
		return nil // Don't retry invalid messages
	}

	log.WithFields(map[string]interface{}{
		"org_id":          jobMsg.OrgID,
		"repo":            jobMsg.RepoFullName,
		"job_id":          jobMsg.JobID,
		"installation_id": jobMsg.InstallationID,
	}).Info("Received job message")

	// Check for duplicate (idempotency)
	existingJobID := fmt.Sprintf("%d-%d", jobMsg.InstallationID, jobMsg.JobID)
	existingJob, err := s.jobStore.Get(ctx, existingJobID)
	if err == nil && existingJob != nil {
		log.WithField("job_id", existingJobID).Info("Duplicate job, skipping")
		return nil
	}

	// Create job record
	job := &redis.Job{
		ID:             existingJobID,
		OrgID:          jobMsg.OrgID,
		OrgName:        jobMsg.OrgName,
		InstallationID: jobMsg.InstallationID,
		RepoFullName:   jobMsg.RepoFullName,
		RunID:          jobMsg.RunID,
		JobID:          jobMsg.JobID,
		Labels:         jobMsg.Labels,
		PoolID:         s.cfg.Pool.ID,
		Priority:       jobMsg.Priority,
	}

	// Enqueue job
	if err := s.jobStore.Enqueue(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	log.WithField("job_id", job.ID).Info("Job enqueued")
	return nil
}

// validateMessage validates a job message
func (s *Subscriber) validateMessage(msg *JobMessage) error {
	if msg.InstallationID == 0 {
		return fmt.Errorf("installation_id is required")
	}
	if msg.JobID == 0 {
		return fmt.Errorf("job_id is required")
	}
	if msg.RepoFullName == "" {
		return fmt.Errorf("repo_full_name is required")
	}
	return nil
}

// GetStats returns subscriber statistics
func (s *Subscriber) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"received_messages": s.receivedMessages,
		"processed_jobs":    s.processedJobs,
		"failed_messages":   s.failedMessages,
	}
}

// PublishTestMessage publishes a test message (for testing only)
func PublishTestMessage(ctx context.Context, projectID, topicID string, msg *JobMessage) error {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return err
	}
	defer client.Close()

	topic := client.Topic(topicID)

	if msg.ReceivedAt == 0 {
		msg.ReceivedAt = time.Now().Unix()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	result := topic.Publish(ctx, &pubsub.Message{
		Data: data,
		Attributes: map[string]string{
			"message_id": uuid.New().String(),
		},
	})

	_, err = result.Get(ctx)
	return err
}

