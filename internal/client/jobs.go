package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

var pollInterval = 5 * time.Second

// CreateAsyncJob creates a new async streaming job (pipeline).
//
// The job is created in CREATED state and will automatically transition through
// PENDING -> STARTING -> RUNNING (or STOPPED if desired_state is STOPPED).
// Use WaitForState or WaitForStableState to wait for the job to be ready.
func (c *Client) CreateAsyncJob(ctx context.Context, req CreateJobRequest) (*Job, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, EndpointJobsAsync, req)
	if err != nil {
		return nil, err
	}

	var job Job
	if err := handleResponse(resp, &job); err != nil {
		return nil, err
	}

	return &job, nil
}

// GetJob retrieves a job by ID.
func (c *Client) GetJob(ctx context.Context, id string) (*Job, error) {
	path := fmt.Sprintf(EndpointJob, url.PathEscape(id))

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var job Job
	if err := handleResponse(resp, &job); err != nil {
		return nil, err
	}

	return &job, nil
}

// GetJobByName retrieves the latest version of a job by name.
//
// Returns nil (not an error) if no job with the given name exists.
// This is used for adoption - checking if a pipeline with a given name already exists.
func (c *Client) GetJobByName(ctx context.Context, name string) (*Job, error) {
	path := fmt.Sprintf("%s?name=%s&latest=true", EndpointJobs, url.QueryEscape(name))

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var jobsResp JobsResponse
	if err := handleResponse(resp, &jobsResp); err != nil {
		return nil, err
	}

	if jobsResp.Items == nil || len(*jobsResp.Items) == 0 {
		return nil, nil
	}

	return &(*jobsResp.Items)[0], nil
}

// UpdateJob updates an existing job.
//
// The request must include fromVersion (the current version of the job) for
// optimistic locking. If the job has been modified by another process since
// it was read, the API will return a 409 Conflict error.
//
// Set rollbackEnabled to true to automatically rollback to the previous version
// if the update fails (e.g., if the new configuration is invalid).
func (c *Client) UpdateJob(ctx context.Context, id string, req UpdateJobRequest, rollbackEnabled bool) (*Job, error) {
	path := fmt.Sprintf(EndpointJob+"?rollbackEnabled=%t", url.PathEscape(id), rollbackEnabled)

	resp, err := c.doRequest(ctx, http.MethodPut, path, req)
	if err != nil {
		return nil, err
	}

	var job Job
	if err := handleResponse(resp, &job); err != nil {
		return nil, err
	}

	return &job, nil
}

// DeleteJob deletes a job by ID.
func (c *Client) DeleteJob(ctx context.Context, id string) error {
	path := fmt.Sprintf(EndpointJob, url.PathEscape(id))

	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	return handleResponse(resp, nil)
}

// WaitForState polls the job until it reaches the desired state or a terminal state.
//
// This method is used after Create/Update operations to wait for the job to
// transition to the desired state (typically RUNNING or STOPPED).
//
// Returns an error if:
//   - The timeout is exceeded
//   - The job reaches a terminal state that is not the desired state
//   - The context is cancelled
//
// Special case: if desiredState is DELETED and the job returns 404, this is
// considered success (the job was deleted).
func (c *Client) WaitForState(ctx context.Context, id string, desiredState JobState, timeout time.Duration) (*Job, error) {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for job %s to reach state %s", id, desiredState)
		}

		job, err := c.GetJob(ctx, id)
		if err != nil {
			if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
				// 404 is success when waiting for deletion
				if desiredState == JobStateDeleted {
					return nil, nil
				}
				return nil, err
			}
			return nil, err
		}

		if job.State == desiredState {
			return job, nil
		}

		// If we hit a terminal state that's not what we wanted, fail fast
		if IsTerminal(job.State) && job.State != desiredState {
			return job, fmt.Errorf("job %s reached terminal state %s instead of %s", id, job.State, desiredState)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// WaitForStableState polls the job until it reaches a stable state.
func (c *Client) WaitForStableState(ctx context.Context, id string, timeout time.Duration) (*Job, error) {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for job %s to reach a stable state", id)
		}

		job, err := c.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}

		if IsStable(job.State) {
			return job, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// WaitForDeletion polls until the job is deleted or returns 404.
func (c *Client) WaitForDeletion(ctx context.Context, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for job %s to be deleted", id)
		}

		job, err := c.GetJob(ctx, id)
		if err != nil {
			if apiErr, ok := err.(*APIError); ok && apiErr.IsNotFound() {
				return nil
			}
			return err
		}

		if job.State == JobStateDeleted {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
