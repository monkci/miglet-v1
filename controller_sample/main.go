package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	port              = "8080"
	dataDir           = "./controller_data"
	registrationToken = "BLDTGMLARLL6HEVWUUQPYWLJG2RCE" // Hardcoded test token
)

// Track VMs that have sent vm_started events and are ready for registration
var readyVMs = make(map[string]bool)
var vmRegistrationSent = make(map[string]bool) // Track if we've already sent registration command

func main() {
	// Create data directory
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Create gRPC server
	grpcServer := NewGRPCServer()

	// Start gRPC server in a goroutine
	go func() {
		if err := StartGRPCServer(grpcServer); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// Setup HTTP routes
	http.HandleFunc("/api/v1/vms/", handleVMRequests)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Sample MIG Controller starting:")
	log.Printf("  HTTP server on port %s", port)
	log.Printf("  gRPC server on port %s", grpcPort)
	log.Printf("  Data will be stored in: %s", dataDir)
	log.Printf("  Registration token (hardcoded): %s", registrationToken)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("HTTP server failed: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func handleVMRequests(w http.ResponseWriter, r *http.Request) {
	// Extract VM ID from path: /api/v1/vms/{vm_id}/...
	path := r.URL.Path
	log.Printf("Request: %s %s", r.Method, path)

	// Parse VM ID (simple extraction)
	// Path format: /api/v1/vms/{vm_id}/events or /api/v1/vms/{vm_id}/heartbeat, etc.
	var vmID string
	prefix := "/api/v1/vms/"
	if len(path) > len(prefix) {
		remaining := path[len(prefix):]
		// Find next slash to get VM ID
		if idx := findNextSlash(remaining); idx > 0 {
			vmID = remaining[:idx]
		} else {
			// No slash found, entire remaining is VM ID (shouldn't happen for our endpoints)
			vmID = remaining
		}
	}

	if vmID == "" {
		http.Error(w, "VM ID not found in path", http.StatusBadRequest)
		return
	}

	log.Printf("Extracted VM ID: %s from path: %s", vmID, path)

	// Route based on path suffix
	if strings.HasSuffix(path, "/registration-token") {
		log.Printf("Routing to registration-token handler")
		handleRegistrationToken(w, r, vmID)
	} else if strings.HasSuffix(path, "/events") {
		log.Printf("Routing to events handler")
		handleEvents(w, r, vmID)
	} else if strings.HasSuffix(path, "/heartbeat") {
		log.Printf("Routing to heartbeat handler")
		handleHeartbeat(w, r, vmID)
	} else if strings.HasSuffix(path, "/commands") {
		log.Printf("Routing to commands handler")
		handleCommands(w, r, vmID)
	} else {
		log.Printf("Unknown endpoint: %s (VM ID: %s, HasSuffix /events: %v)", path, vmID, strings.HasSuffix(path, "/events"))
		http.Error(w, fmt.Sprintf("Unknown endpoint: %s", path), http.StatusNotFound)
	}
}

func findNextSlash(s string) int {
	for i, c := range s {
		if c == '/' {
			return i
		}
	}
	return -1
}

// handleRegistrationToken handles registration token requests
func handleRegistrationToken(w http.ResponseWriter, r *http.Request, vmID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Store request
	storeData(vmID, "registration-token-request", body)

	// Parse request (optional, for logging)
	var req map[string]interface{}
	json.Unmarshal(body, &req)
	log.Printf("Registration token request from VM %s: %+v", vmID, req)

	// Send hardcoded response
	response := map[string]interface{}{
		"registration_token": registrationToken,
		"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		"runner_url":         "https://github.com/testorg",
		"runner_group":       "default",
		"labels":             []string{"self-hosted", "linux", "x64"},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	// Store response
	responseData, _ := json.Marshal(response)
	storeData(vmID, "registration-token-response", responseData)
}

// handleEvents handles VM and job events
func handleEvents(w http.ResponseWriter, r *http.Request, vmID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse event to determine type
	var event map[string]interface{}
	if err := json.Unmarshal(body, &event); err != nil {
		log.Printf("Failed to parse event JSON: %v", err)
	}

	eventType, _ := event["type"].(string)
	if eventType == "" {
		eventType = "unknown"
	}

	log.Printf("Event from VM %s: type=%s", vmID, eventType)

	// Store event
	storeData(vmID, fmt.Sprintf("event-%s", eventType), body)

	// Send acknowledgment
	// For vm_started events, explicitly acknowledge (this is what MIGlet waits for)
	if eventType == "vm_started" {
		poolID, _ := event["pool_id"].(string)
		orgID, _ := event["org_id"].(string)
		log.Printf("Acknowledging VM started event - VM: %s, Pool: %s, Org: %s", vmID, poolID, orgID)

			// Mark VM as ready for registration
		readyVMs[vmID] = true
		vmRegistrationSent[vmID] = false // Reset flag for this VM

		// Send explicit acknowledgment for VM started events
		// MIGlet will transition to "ready" state and poll for registration config
		response := map[string]interface{}{
			"status":       "acknowledged", // MIGlet checks for "acknowledged" or "received"
			"acknowledged": true,           // Explicit flag
			"vm_id":        vmID,
			"pool_id":      poolID,
			"org_id":       orgID,
			"message":      "VM started event acknowledged - MIGlet is ready for registration config",
			"timestamp":    time.Now().Format(time.RFC3339),
			// Note: Registration token is NOT sent here - it will be sent via command
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// For other events, send generic acknowledgment
	response := map[string]interface{}{
		"status":       "received",
		"acknowledged": false,
		"vm_id":        vmID,
		"message":      "Event received and stored",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleHeartbeat handles heartbeat requests
func handleHeartbeat(w http.ResponseWriter, r *http.Request, vmID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Store heartbeat with timestamp
	timestamp := time.Now().Format("20060102-150405")
	storeData(vmID, fmt.Sprintf("heartbeat-%s", timestamp), body)

	// Parse heartbeat (optional, for logging)
	var heartbeat map[string]interface{}
	json.Unmarshal(body, &heartbeat)
	log.Printf("Heartbeat from VM %s", vmID)

	// Send acknowledgment
	response := map[string]interface{}{
		"status":  "received",
		"vm_id":   vmID,
		"message": "Heartbeat received",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleCommands handles command requests (MIGlet polling for commands)
func handleCommands(w http.ResponseWriter, r *http.Request, vmID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("Command request from VM %s", vmID)

	// Check if VM is ready and we haven't sent registration command yet
	var commands []map[string]interface{}

	if readyVMs[vmID] && !vmRegistrationSent[vmID] {
		// Send register_runner command with registration token and config
		log.Printf("Sending register_runner command to VM %s", vmID)
		commands = append(commands, map[string]interface{}{
			"id":   fmt.Sprintf("register-%s-%d", vmID, time.Now().Unix()),
			"type": "register_runner",
			"parameters": map[string]interface{}{
				"registration_token": registrationToken,
				"runner_url":         "https://github.com/monkci/miglet-v1",
				"runner_group":       "default",
				"labels":             []string{"self-hosted", "monkci-miglet-tst1", "linux", "x64"},
				"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			"created_at": time.Now().Format(time.RFC3339),
		})
		vmRegistrationSent[vmID] = true // Mark as sent
	}

	response := map[string]interface{}{
		"commands": commands,
		"vm_id":    vmID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// storeData stores incoming data to files
func storeData(vmID, dataType string, data []byte) {
	// Create VM-specific directory
	vmDir := filepath.Join(dataDir, vmID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		log.Printf("Failed to create VM directory: %v", err)
		return
	}

	// Create filename with timestamp
	timestamp := time.Now().Format("20060102-150405.000")
	filename := fmt.Sprintf("%s-%s.json", dataType, timestamp)
	filepath := filepath.Join(vmDir, filename)

	// Write data
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		log.Printf("Failed to write data file: %v", err)
		return
	}

	log.Printf("Stored data: %s", filepath)
}
