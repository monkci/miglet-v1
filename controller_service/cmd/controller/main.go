package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/monkci/mig-controller/internal/config"
	grpcserver "github.com/monkci/mig-controller/internal/grpc"
	"github.com/monkci/mig-controller/internal/pubsub"
	"github.com/monkci/mig-controller/internal/redis"
	"github.com/monkci/mig-controller/internal/scheduler"
	"github.com/monkci/mig-controller/internal/token"
	"github.com/monkci/mig-controller/internal/vm"
	"github.com/monkci/mig-controller/pkg/logger"
	"github.com/monkci/mig-controller/proto/commands"
)

var (
	configPath = flag.String("config", "", "Path to config file")
	version    = "dev"
	buildTime  = "unknown"
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger.Init(cfg.Logging.Level, cfg.Logging.Format)
	log := logger.WithComponent("main")

	log.WithFields(map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
		"pool_id":    cfg.Pool.ID,
		"pool_type":  cfg.Pool.Type,
	}).Info("MIG Controller starting")

	// Initialize Redis stores
	jobStore, err := redis.NewJobStore(&cfg.Redis.Jobs, cfg.Pool.ID)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize job store")
	}
	defer jobStore.Close()

	vmStore, err := redis.NewVMStatusStore(&cfg.Redis.VMStatus, cfg.Pool.ID)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize VM status store")
	}
	defer vmStore.Close()

	// Initialize token service
	tokenService, err := token.NewService(&cfg.GitHubApp)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize token service")
	}

	// Initialize VM manager
	vmManager, err := vm.NewManager(cfg, vmStore)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize VM manager")
	}
	defer vmManager.Close()

	// Initialize gRPC server
	grpcServer := grpcserver.NewServer(cfg, vmStore)

	// Initialize scheduler
	sched := scheduler.NewScheduler(cfg, jobStore, vmStore, vmManager, grpcServer, tokenService)

	// Set up event handlers
	grpcServer.SetEventCallback(func(vmID string, event *commands.EventNotification) {
		sched.HandleJobEvent(vmID, event)
	})

	// Initialize Pub/Sub subscriber
	subscriber, err := pubsub.NewSubscriber(cfg, jobStore)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize Pub/Sub subscriber")
	}

	// Start components
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start gRPC server
	go func() {
		if err := grpcServer.Start(cfg.Server.GRPCPort); err != nil {
			log.WithError(err).Fatal("gRPC server failed")
		}
	}()

	// Start Pub/Sub subscriber
	subscriber.Start()

	// Start scheduler
	sched.Start()

	// Start HTTP server for health checks and metrics
	go startHTTPServer(cfg, sched, subscriber)

	// Initial VM list refresh
	if err := vmManager.RefreshVMList(ctx); err != nil {
		log.WithError(err).Warn("Initial VM refresh failed")
	}

	log.WithFields(map[string]interface{}{
		"grpc_port": cfg.Server.GRPCPort,
		"http_port": cfg.Server.HTTPPort,
		"pool_id":   cfg.Pool.ID,
	}).Info("MIG Controller started successfully")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	log.WithField("signal", sig).Info("Shutdown signal received")

	// Graceful shutdown
	sched.Stop()
	subscriber.Stop()
	vmManager.Close()

	log.Info("MIG Controller shutdown complete")
}

// startHTTPServer starts the HTTP server for health checks and metrics
func startHTTPServer(cfg *config.Config, sched *scheduler.Scheduler, subscriber *pubsub.Subscriber) {
	log := logger.WithComponent("http_server")

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Readiness check
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Ready"))
	})

	// Metrics/stats endpoint
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := map[string]interface{}{
			"scheduler": sched.GetStats(),
			"pubsub":    subscriber.GetStats(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%+v", stats)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	log.WithField("addr", addr).Info("HTTP server starting")

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.WithError(err).Error("HTTP server failed")
	}
}

