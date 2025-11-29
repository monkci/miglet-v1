package storage

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
)

// MongoDBStorage handles MongoDB storage operations
type MongoDBStorage struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
	connected  bool
}

// NewMongoDBStorage creates a new MongoDB storage client
func NewMongoDBStorage(connectionString, databaseName, collectionName string) (*MongoDBStorage, error) {
	storage := &MongoDBStorage{}

	// Create MongoDB client
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(connectionString)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	storage.client = client
	storage.database = client.Database(databaseName)
	storage.collection = storage.database.Collection(collectionName)
	storage.connected = true

	logger.Get().WithFields(map[string]interface{}{
		"database":   databaseName,
		"collection": collectionName,
	}).Info("Connected to MongoDB")

	return storage, nil
}

// StoreHeartbeat stores a heartbeat event in MongoDB
func (s *MongoDBStorage) StoreHeartbeat(ctx context.Context, heartbeat *events.HeartbeatEvent) error {
	if !s.connected {
		return fmt.Errorf("MongoDB not connected")
	}

	// Create document with additional metadata
	document := map[string]interface{}{
		"type":         heartbeat.Type,
		"timestamp":    heartbeat.Timestamp,
		"vm_id":        heartbeat.VMID,
		"pool_id":      heartbeat.PoolID,
		"org_id":        heartbeat.OrgID,
		"vm_health":    heartbeat.VMHealth,
		"runner_state": heartbeat.RunnerState,
		"created_at":   time.Now(),
	}

	// Add current job if present
	if heartbeat.CurrentJob != nil {
		document["current_job"] = map[string]interface{}{
			"job_id":     heartbeat.CurrentJob.JobID,
			"run_id":      heartbeat.CurrentJob.RunID,
			"repository":  heartbeat.CurrentJob.Repository,
			"started_at":  heartbeat.CurrentJob.StartedAt,
		}
	}

	// Insert document
	_, err := s.collection.InsertOne(ctx, document)
	if err != nil {
		return fmt.Errorf("failed to insert heartbeat: %w", err)
	}

	return nil
}

// StoreEvent stores any event in MongoDB
func (s *MongoDBStorage) StoreEvent(ctx context.Context, event interface{}) error {
	if !s.connected {
		return fmt.Errorf("MongoDB not connected")
	}

	// Convert event to document
	document := map[string]interface{}{
		"event":      event,
		"created_at": time.Now(),
	}

	// Insert document
	_, err := s.collection.InsertOne(ctx, document)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	return nil
}

// Close closes the MongoDB connection
func (s *MongoDBStorage) Close(ctx context.Context) error {
	if s.client != nil {
		s.connected = false
		return s.client.Disconnect(ctx)
	}
	return nil
}

// IsConnected returns whether MongoDB is connected
func (s *MongoDBStorage) IsConnected() bool {
	return s.connected
}

