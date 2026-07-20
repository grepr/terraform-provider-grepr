// Package client provides unit tests for the Grepr API client.
// These tests use httptest to mock the Grepr API server and verify
// that the client makes correct HTTP requests and handles responses properly.
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

// setupTestServer creates a test HTTP server and a client configured to use it.
// The server uses the provided handler to respond to requests.
// The returned client has a pre-set access token with a 1-hour expiry to avoid OAuth calls.
func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	server := httptest.NewServer(handler)

	c := &Client{
		httpClient:  server.Client(),
		host:        server.URL,
		accessToken: "test-token",
		tokenExpiry: time.Now().Add(time.Hour),
	}

	return server, c
}

// TestClient_GetJob verifies that GetJob() correctly fetches a job by ID
// and properly deserializes the API response.
func TestClient_GetJob(t *testing.T) {
	now := time.Now()
	expectedJob := Job{
		Id:             "test-id-123",
		Version:        1,
		OrganizationId: "test-org",
		Name:           "test_pipeline",
		Execution:      generated.ReadJobExecutionASYNCHRONOUS,
		Processing:     generated.ReadJobProcessingSTREAMING,
		State:          JobStateRunning,
		DesiredState:   generated.ReadJobDesiredStateRUNNING,
		JobGraph: JobGraph{
			Vertices: []generated.Operation{},
			Edges:    []string{},
		},
		Tags:      map[string]string{"env": "test"},
		TeamIds:   &[]string{"team-1"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs/test-id-123" {
			t.Errorf("expected /api/v1/jobs/test-id-123, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedJob)
	})
	defer server.Close()

	job, err := client.GetJob(context.Background(), "test-id-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Id != expectedJob.Id {
		t.Errorf("expected Id %s, got %s", expectedJob.Id, job.Id)
	}
	if job.Name != expectedJob.Name {
		t.Errorf("expected Name %s, got %s", expectedJob.Name, job.Name)
	}
	if job.State != expectedJob.State {
		t.Errorf("expected State %s, got %s", expectedJob.State, job.State)
	}
}

// TestClient_GetJob_NotFound verifies that GetJob() returns an APIError with
// IsNotFound() == true when the server returns 404.
func TestClient_GetJob_NotFound(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "job not found"}`))
	})
	defer server.Close()

	_, err := client.GetJob(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !apiErr.IsNotFound() {
		t.Errorf("expected IsNotFound() to be true")
	}
}

// TestClient_GetJobByName verifies that GetJobByName() correctly fetches a job
// by name using the name query parameter.
func TestClient_GetJobByName(t *testing.T) {
	expectedJob := Job{
		Id:   "test-id-123",
		Name: "my_pipeline",
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs" {
			t.Errorf("expected /api/v1/jobs, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("name") != "my_pipeline" {
			t.Errorf("expected name=my_pipeline, got %s", r.URL.Query().Get("name"))
		}

		items := []Job{expectedJob}
		resp := JobsResponse{Items: &items}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	job, err := client.GetJobByName(context.Background(), "my_pipeline")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Id != expectedJob.Id {
		t.Errorf("expected Id %s, got %s", expectedJob.Id, job.Id)
	}
	if job.Name != expectedJob.Name {
		t.Errorf("expected Name %s, got %s", expectedJob.Name, job.Name)
	}
}

// TestClient_GetJobByName_NotFound verifies that GetJobByName() returns nil
// (not an error) when no job with the given name exists.
func TestClient_GetJobByName_NotFound(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		items := []Job{}
		resp := JobsResponse{Items: &items}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	job, err := client.GetJobByName(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job != nil {
		t.Errorf("expected nil job, got %+v", job)
	}
}

// TestClient_CreateAsyncJob verifies that CreateAsyncJob() sends a properly
// formatted POST request to create a new async pipeline.
func TestClient_CreateAsyncJob(t *testing.T) {
	expectedJob := Job{
		Id:           "new-job-id",
		Version:      0,
		Name:         "new_pipeline",
		State:        JobStatePending,
		DesiredState: generated.ReadJobDesiredStateRUNNING,
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs/async" {
			t.Errorf("expected /api/v1/jobs/async, got %s", r.URL.Path)
		}

		var req CreateJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Name != "new_pipeline" {
			t.Errorf("expected name new_pipeline, got %s", req.Name)
		}
		if req.Execution != generated.CreateJobExecutionASYNCHRONOUS {
			t.Errorf("expected execution ASYNCHRONOUS, got %s", req.Execution)
		}
		if req.Processing != generated.CreateJobProcessingSTREAMING {
			t.Errorf("expected processing STREAMING, got %s", req.Processing)
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedJob)
	})
	defer server.Close()

	req := CreateJobRequest{
		Name:       "new_pipeline",
		Execution:  generated.CreateJobExecutionASYNCHRONOUS,
		Processing: generated.CreateJobProcessingSTREAMING,
		JobGraph: JobGraph{
			Vertices: []generated.Operation{},
			Edges:    []string{},
		},
	}

	job, err := client.CreateAsyncJob(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Id != expectedJob.Id {
		t.Errorf("expected Id %s, got %s", expectedJob.Id, job.Id)
	}
	if job.State != JobStatePending {
		t.Errorf("expected State PENDING, got %s", job.State)
	}
}

// TestClient_UpdateJob verifies that UpdateJob() sends a properly formatted
// PUT request with version and rollback parameters.
func TestClient_UpdateJob(t *testing.T) {
	expectedJob := Job{
		Id:           "test-id-123",
		Version:      2,
		Name:         "test_pipeline",
		State:        JobStateUpdating,
		DesiredState: generated.ReadJobDesiredStateSTOPPED,
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs/test-id-123" {
			t.Errorf("expected /api/v1/jobs/test-id-123, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("rollbackEnabled") != "true" {
			t.Errorf("expected rollbackEnabled=true, got %s", r.URL.Query().Get("rollbackEnabled"))
		}

		var req UpdateJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.FromVersion != 1 {
			t.Errorf("expected fromVersion 1, got %d", req.FromVersion)
		}
		if req.DesiredState != generated.UpdateJobDesiredStateSTOPPED {
			t.Errorf("expected desiredState STOPPED, got %s", req.DesiredState)
		}

		w.WriteHeader(http.StatusAccepted)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expectedJob)
	})
	defer server.Close()

	req := UpdateJobRequest{
		FromVersion:  1,
		DesiredState: generated.UpdateJobDesiredStateSTOPPED,
		JobGraph: JobGraph{
			Vertices: []generated.Operation{},
			Edges:    []string{},
		},
	}

	job, err := client.UpdateJob(context.Background(), "test-id-123", req, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Version != 2 {
		t.Errorf("expected Version 2, got %d", job.Version)
	}
}

// TestClient_UpdateJob_Conflict verifies that UpdateJob() returns an APIError
// with IsConflict() == true when the server returns 409 (e.g., version mismatch).
func TestClient_UpdateJob_Conflict(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error": "version conflict"}`))
	})
	defer server.Close()

	req := UpdateJobRequest{
		FromVersion:  1,
		DesiredState: generated.UpdateJobDesiredStateSTOPPED,
		JobGraph: JobGraph{
			Vertices: []generated.Operation{},
			Edges:    []string{},
		},
	}

	_, err := client.UpdateJob(context.Background(), "test-id-123", req, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !apiErr.IsConflict() {
		t.Errorf("expected IsConflict() to be true")
	}
}

// TestClient_DeleteJob verifies that DeleteJob() sends a properly formatted
// DELETE request and handles the 202 Accepted response.
func TestClient_DeleteJob(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/jobs/test-id-123" {
			t.Errorf("expected /api/v1/jobs/test-id-123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusAccepted)
	})
	defer server.Close()

	err := client.DeleteJob(context.Background(), "test-id-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestClient_DeleteJob_NotFound verifies that DeleteJob() returns an APIError
// with IsNotFound() == true when the server returns 404.
func TestClient_DeleteJob_NotFound(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "job not found"}`))
	})
	defer server.Close()

	err := client.DeleteJob(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !apiErr.IsNotFound() {
		t.Errorf("expected IsNotFound() to be true")
	}
}

// TestClient_WaitForState_Timeout verifies that WaitForState() returns an error
// when the timeout is exceeded before the job reaches the desired state.
func TestClient_WaitForState_Timeout(t *testing.T) {
	// Reduce poll interval for testing
	originalPollInterval := pollInterval
	pollInterval = 10 * time.Millisecond
	defer func() { pollInterval = originalPollInterval }()

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Always return PENDING state
		job := Job{
			Id:    "test-id-123",
			State: JobStatePending,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	})
	defer server.Close()

	// Short timeout for testing
	_, err := client.WaitForState(context.Background(), "test-id-123", JobStateRunning, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedMsg := "timeout waiting for job test-id-123 to reach state RUNNING"
	if err.Error() != expectedMsg {
		t.Errorf("expected timeout error message %q, got %q", expectedMsg, err.Error())
	}
}

// TestClient_WaitForState_Success verifies that WaitForState() returns the job
// when it reaches the desired state.
func TestClient_WaitForState_Success(t *testing.T) {
	// Reduce poll interval for testing
	originalPollInterval := pollInterval
	pollInterval = 10 * time.Millisecond
	defer func() { pollInterval = originalPollInterval }()

	attempts := 0
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		state := JobStatePending
		if attempts > 2 {
			state = JobStateRunning
		}

		job := Job{
			Id:    "test-id-123",
			State: state,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	})
	defer server.Close()

	// Timeout long enough to allow polling
	job, err := client.WaitForState(context.Background(), "test-id-123", JobStateRunning, 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.State != JobStateRunning {
		t.Errorf("expected state RUNNING, got %s", job.State)
	}
}
