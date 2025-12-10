package token

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/pkg/logger"
)

// RegistrationToken represents a GitHub Actions runner registration token
type RegistrationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// InstallationToken represents a GitHub App installation access token
type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Service handles GitHub App authentication and token generation
type Service struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client

	// Cache for installation tokens
	tokenCache     map[int64]*InstallationToken
	tokenCacheLock sync.RWMutex
}

// NewService creates a new token service
func NewService(cfg *config.GitHubAppConfig) (*Service, error) {
	log := logger.WithComponent("token_service")

	var privateKeyBytes []byte
	var err error

	// Load private key from path or direct value
	if cfg.PrivateKey != "" {
		privateKeyBytes = []byte(cfg.PrivateKey)
	} else if cfg.PrivateKeyPath != "" {
		privateKeyBytes, err = os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no private key configured")
	}

	// Parse RSA private key
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	log.WithField("app_id", cfg.AppID).Info("Token service initialized")

	return &Service{
		appID:      cfg.AppID,
		privateKey: privateKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokenCache: make(map[int64]*InstallationToken),
	}, nil
}

// GetRegistrationToken generates a runner registration token
func (s *Service) GetRegistrationToken(ctx context.Context, installationID int64, repoOrOrg string, isOrg bool) (*RegistrationToken, error) {
	log := logger.WithComponent("token_service").WithFields(map[string]interface{}{
		"installation_id": installationID,
		"target":          repoOrOrg,
		"is_org":          isOrg,
	})

	// Get installation access token
	accessToken, err := s.getInstallationToken(ctx, installationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get installation token: %w", err)
	}

	// Create registration token
	var url string
	if isOrg {
		url = fmt.Sprintf("https://api.github.com/orgs/%s/actions/runners/registration-token", repoOrOrg)
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/actions/runners/registration-token", repoOrOrg)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request registration token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create registration token: %s - %s", resp.Status, string(body))
	}

	var tokenResp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	expiresAt, _ := time.Parse(time.RFC3339, tokenResp.ExpiresAt)

	log.Info("Registration token created successfully")

	return &RegistrationToken{
		Token:     tokenResp.Token,
		ExpiresAt: expiresAt,
	}, nil
}

// getInstallationToken gets or refreshes an installation access token
func (s *Service) getInstallationToken(ctx context.Context, installationID int64) (*InstallationToken, error) {
	// Check cache first
	s.tokenCacheLock.RLock()
	cached, exists := s.tokenCache[installationID]
	s.tokenCacheLock.RUnlock()

	if exists && time.Until(cached.ExpiresAt) > 5*time.Minute {
		return cached, nil
	}

	// Generate new token
	jwt, err := s.generateAppJWT()
	if err != nil {
		return nil, fmt.Errorf("failed to generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create installation token: %s - %s", resp.Status, string(body))
	}

	var tokenResp struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	expiresAt, _ := time.Parse(time.RFC3339, tokenResp.ExpiresAt)

	token := &InstallationToken{
		Token:     tokenResp.Token,
		ExpiresAt: expiresAt,
	}

	// Cache the token
	s.tokenCacheLock.Lock()
	s.tokenCache[installationID] = token
	s.tokenCacheLock.Unlock()

	return token, nil
}

// generateAppJWT generates a JWT for GitHub App authentication
func (s *Service) generateAppJWT() (string, error) {
	now := time.Now()

	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // Issued at (60s in past for clock skew)
		"exp": now.Add(10 * time.Minute).Unix(),  // Expires in 10 minutes
		"iss": s.appID,                           // Issuer (App ID)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(s.privateKey)
}

// GetRunnerURL returns the URL for runner registration
func GetRunnerURL(repoOrOrg string, isOrg bool) string {
	if isOrg {
		return fmt.Sprintf("https://github.com/%s", repoOrOrg)
	}
	return fmt.Sprintf("https://github.com/%s", repoOrOrg)
}

