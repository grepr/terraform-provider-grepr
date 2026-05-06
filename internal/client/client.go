// Package client provides a Go client for the Grepr API.
//
// The client handles OAuth2 authentication via Auth0 and provides methods for
// managing async streaming jobs (pipelines). It includes automatic token caching
// and refresh, as well as helper methods for waiting on job state transitions.
//
// Basic usage:
//
//	c := client.NewClient(client.Config{
//	    Host:         "https://myorg.app.grepr.ai/api",
//	    ClientID:     "your-client-id",
//	    ClientSecret: "your-client-secret",
//	})
//
//	job, err := c.CreateAsyncJob(ctx, createReq)
//	if err != nil {
//	    return err
//	}
//
//	// Wait for the job to reach RUNNING state
//	job, err = c.WaitForState(ctx, job.Id, client.JobStateRunning, 5*time.Minute)
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// defaultAuth0Domain is the production Auth0 domain used for OAuth authentication.
	defaultAuth0Domain = "grepr-prod.us.auth0.com"

	// tokenRefreshBuffer is how long before token expiry we should refresh the in-memory token.
	// We refresh early to avoid race conditions where the token expires mid-request.
	tokenRefreshBuffer = 60 * time.Second

	// diskTokenRefreshBuffer is the expiry buffer applied when evaluating a disk-cached token.
	// A larger buffer (5 minutes) is used for disk-cached tokens to match the CLI's GreprAuth
	// behavior and to account for clock skew across processes that share the same cache file.
	diskTokenRefreshBuffer = 5 * time.Minute
)

// cachedTokenData is the JSON structure written to the disk token cache file at
// ~/.grepr/auth/{clientID}-m2m.json. The ExpiresAt field is epoch milliseconds.
type cachedTokenData struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	ExpiresAt   int64  `json:"expires_at"`
}

// Client is the Grepr API client.
//
// It handles OAuth2 authentication and provides methods for CRUD operations on jobs.
// The client is safe for concurrent use - token caching uses a read-write mutex to
// allow multiple concurrent API calls while ensuring thread-safe token refresh.
type Client struct {
	httpClient   *http.Client
	host         string
	clientID     string
	clientSecret string
	auth0Domain  string

	// Token caching fields. Protected by tokenMu for thread-safe access.
	// We cache the token and refresh it before expiry to minimize Auth0 calls.
	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// Config contains the configuration for creating a new Client.
type Config struct {
	Host         string
	ClientID     string
	ClientSecret string
	Auth0Domain  string
}

// NewClient creates a new Grepr API client.
func NewClient(cfg Config) *Client {
	auth0Domain := cfg.Auth0Domain
	if auth0Domain == "" {
		auth0Domain = defaultAuth0Domain
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		host:         cfg.Host,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		auth0Domain:  auth0Domain,
	}
}

// getToken returns a valid access token, refreshing if necessary.
//
// This method uses a double-checked locking pattern:
// 1. First, acquire a read lock and check if we have a valid cached token
// 2. If not, acquire a write lock and check again (another goroutine may have refreshed)
// 3. If still needed, fetch a new token from Auth0
//
// This allows multiple goroutines to use a cached token concurrently while
// ensuring only one goroutine refreshes the token when needed.
func (c *Client) getToken(ctx context.Context) (string, error) {
	// Fast path: check with read lock if we have a valid cached token
	c.tokenMu.RLock()
	if c.accessToken != "" && time.Now().Add(tokenRefreshBuffer).Before(c.tokenExpiry) {
		token := c.accessToken
		c.tokenMu.RUnlock()
		return token, nil
	}
	c.tokenMu.RUnlock()

	// Slow path: acquire write lock to refresh token
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double-check after acquiring write lock - another goroutine may have refreshed
	if c.accessToken != "" && time.Now().Add(tokenRefreshBuffer).Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	// Check disk cache before calling Auth0 — this avoids a network call when a sibling
	// process has already fetched and persisted a fresh token (e.g., between terraform steps).
	if td, err := c.loadDiskToken(); err == nil && td != nil {
		expiresAt := time.UnixMilli(td.ExpiresAt)
		if time.Now().Add(diskTokenRefreshBuffer).Before(expiresAt) {
			c.accessToken = td.AccessToken
			c.tokenExpiry = expiresAt
			return td.AccessToken, nil
		}
	}

	token, expiresIn, err := c.FetchToken(ctx)
	if err != nil {
		return "", err
	}

	c.accessToken = token
	c.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)

	td := cachedTokenData{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		ExpiresAt:   c.tokenExpiry.UnixMilli(),
	}
	if err := c.saveDiskToken(td); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save M2M token to disk cache: %v\n", err)
	}

	return token, nil
}

// FetchToken fetches a new OAuth token from Auth0.
func (c *Client) FetchToken(ctx context.Context) (string, int, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth/token", c.auth0Domain)

	reqBody := OAuthTokenRequest{
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		Audience:     "service",
		GrantType:    "client_credentials",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to fetch token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("failed to fetch token: status %d", resp.StatusCode)
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("failed to decode token response: %w", err)
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

// diskCachePath returns the path of the M2M token cache file for this client.
// The path is ~/.grepr/auth/{clientID}-m2m.json.
func (c *Client) diskCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".grepr", "auth", fmt.Sprintf("%s-m2m.json", c.clientID)), nil
}

// loadDiskToken reads and decodes the disk token cache. Returns nil, nil when
// the cache file does not exist (a miss is not an error).
func (c *Client) loadDiskToken() (*cachedTokenData, error) {
	cachePath, err := c.diskCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read disk token cache: %w", err)
	}

	var td cachedTokenData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, fmt.Errorf("failed to decode disk token cache: %w", err)
	}

	return &td, nil
}

// saveDiskToken writes the token to the disk cache with restricted permissions.
// The cache directory is created with mode 0700 and the file with mode 0600.
func (c *Client) saveDiskToken(td cachedTokenData) error {
	cachePath, err := c.diskCachePath()
	if err != nil {
		return err
	}

	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.Marshal(td)
	if err != nil {
		return fmt.Errorf("failed to encode token for disk cache: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write disk token cache: %w", err)
	}

	return nil
}

const (
	// maxRetries is the maximum number of retry attempts for retryable errors (5xx).
	maxRetries = 3
	// initialRetryDelay is the initial delay between retries (exponential backoff).
	initialRetryDelay = 100 * time.Millisecond
	// maxRetryDelay is the maximum delay between retries.
	maxRetryDelay = 5 * time.Second
)

// doRequest performs an authenticated HTTP request with retry logic for server errors.
// It will retry up to maxRetries times for 5xx errors with exponential backoff.
// Client errors (4xx) are not retried as they indicate a problem with the request.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var lastErr error
	var jsonBody []byte

	// Marshal body once before retries
	if body != nil {
		var err error
		jsonBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	// Retry loop with exponential backoff
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Get fresh token for each attempt (in case it expired during retries)
		token, err := c.getToken(ctx)
		if err != nil {
			return nil, err
		}

		// Create request body reader
		var reqBody io.Reader
		if jsonBody != nil {
			reqBody = bytes.NewReader(jsonBody)
		}

		url := fmt.Sprintf("%s%s", c.host, path)
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Network errors are retryable
			lastErr = err
			if attempt < maxRetries {
				delay := calculateBackoff(attempt)
				time.Sleep(delay)
				continue
			}
			return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, err)
		}

		// Check if we should retry based on status code
		if resp.StatusCode >= 500 && attempt < maxRetries {
			// Server error - read body for error message, then retry
			bodyBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = &APIError{
				StatusCode: resp.StatusCode,
				Message:    string(bodyBytes),
			}
			delay := calculateBackoff(attempt)
			time.Sleep(delay)
			continue
		}

		// Success or non-retryable error (4xx) - return response
		return resp, nil
	}

	// All retries exhausted
	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
}

// calculateBackoff calculates the retry delay using exponential backoff.
// Formula: min(initialDelay * 2^attempt, maxDelay)
func calculateBackoff(attempt int) time.Duration {
	delay := initialRetryDelay * time.Duration(1<<uint(attempt))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// APIError represents an error from the Grepr API.
// It includes the HTTP status code and response message for detailed error handling.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// IsNotFound returns true if the error is a 404 Not Found error.
func (e *APIError) IsNotFound() bool {
	return e.StatusCode == http.StatusNotFound
}

// IsConflict returns true if the error is a 409 Conflict error.
// Common when there's a version mismatch during updates.
func (e *APIError) IsConflict() bool {
	return e.StatusCode == http.StatusConflict
}

// IsBadRequest returns true if the error is a 400 Bad Request error.
func (e *APIError) IsBadRequest() bool {
	return e.StatusCode == http.StatusBadRequest
}

// IsUnauthorized returns true if the error is a 401 Unauthorized error.
func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized
}

// IsForbidden returns true if the error is a 403 Forbidden error.
func (e *APIError) IsForbidden() bool {
	return e.StatusCode == http.StatusForbidden
}

// IsClientError returns true if the error is a 4xx client error.
// Client errors indicate issues with the request that should not be retried.
func (e *APIError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// IsServerError returns true if the error is a 5xx server error.
// Server errors are transient and may succeed on retry.
func (e *APIError) IsServerError() bool {
	return e.StatusCode >= 500
}

// IsRetryable returns true if the error might succeed on retry.
// Only server errors (5xx) are considered retryable.
func (e *APIError) IsRetryable() bool {
	return e.IsServerError()
}

// handleResponse processes an HTTP response and returns an error if not successful.
func handleResponse(resp *http.Response, result interface{}) error {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
