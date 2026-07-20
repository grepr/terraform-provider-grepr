package client

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

// Type aliases for generated types.
//
// These aliases simplify imports in other packages by re-exporting the generated
// types under simpler names. The generated types come from the OpenAPI spec at
// https://docs.grepr.ai/openapi.json and are auto-generated using oapi-codegen.
//
// To regenerate the types after an API change:
//
//	make generate
type (
	// Job represents a Grepr job (pipeline) as returned by the API.
	Job = generated.ReadJob

	// JobState represents the current state of a job (e.g., RUNNING, STOPPED, DELETED).
	JobState = generated.ReadJobState

	// CreateJobRequest is the request body for creating a new async job.
	CreateJobRequest = generated.CreateJob

	// UpdateJobRequest is the request body for updating an existing job.
	UpdateJobRequest = generated.UpdateJob

	// JobGraph defines the pipeline's data flow: sources, operations, and sinks.
	JobGraph = generated.GreprJobGraph

	// JobsResponse is the paginated response from the list jobs endpoint.
	JobsResponse = generated.ItemsCollectionReadJob
)

// Job state constants - re-exported from generated package for convenience.
//
// Job states follow this lifecycle:
//
//	CREATED -> PENDING -> STARTING -> RUNNING (stable)
//	                                     |
//	                                     v
//	                                  STOPPING -> STOPPED (stable)
//	                                     |
//	                                     v
//	                   UPDATING/ROLLING_BACK -> RUNNING/STOPPED
//	                                     |
//	                                     v
//	                        FINISHED/FAILED/CANCELLED/DELETED (terminal)
const (
	// Initial states
	JobStateCreated = generated.ReadJobStateCREATED
	JobStatePending = generated.ReadJobStatePENDING

	// Infrastructure states
	JobStateInfraUpdate     = generated.ReadJobStateINFRAUPDATE
	JobStateInfraUpdateWait = generated.ReadJobStateINFRAUPDATEWAIT
	JobStateVerifying       = generated.ReadJobStateVERIFYING
	JobStateWaiting         = generated.ReadJobStateWAITING

	// Stable (running) states
	JobStateRunning = generated.ReadJobStateRUNNING
	JobStateStopped = generated.ReadJobStateSTOPPED

	// Terminal states (job has ended)
	JobStateFinished  = generated.ReadJobStateFINISHED
	JobStateFailed    = generated.ReadJobStateFAILED
	JobStateCancelled = generated.ReadJobStateCANCELLED
	JobStateDeleted   = generated.ReadJobStateDELETED

	// Transitional states
	JobStateUpdating    = generated.ReadJobStateUPDATING
	JobStateStarting    = generated.ReadJobStateSTARTING
	JobStateStopping    = generated.ReadJobStateSTOPPING
	JobStateRollingBack = generated.ReadJobStateROLLINGBACK
)

// IsTerminal returns true if the state is a terminal state.
//
// Terminal states are final - the job will not transition to any other state.
// These are: FINISHED, FAILED, CANCELLED, DELETED.
func IsTerminal(s JobState) bool {
	switch s {
	case JobStateFinished, JobStateFailed, JobStateCancelled, JobStateDeleted:
		return true
	default:
		return false
	}
}

// IsStable returns true if the state is a stable (non-transitional) state.
//
// Stable states are: RUNNING, STOPPED, or any terminal state.
// In stable states, the job is not actively transitioning and can accept new operations.
func IsStable(s JobState) bool {
	switch s {
	case JobStateRunning, JobStateStopped:
		return true
	default:
		return IsTerminal(s)
	}
}

// Execution type constants
const (
	ExecutionAsynchronous = generated.CreateJobExecutionASYNCHRONOUS
)

// Processing type constants
const (
	ProcessingStreaming = generated.CreateJobProcessingSTREAMING
)

// DesiredState constants for UpdateJob
const (
	DesiredStateRunning = generated.UpdateJobDesiredStateRUNNING
	DesiredStateStopped = generated.UpdateJobDesiredStateSTOPPED
)

// OAuthTokenRequest is the request body for obtaining an OAuth token.
type OAuthTokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Audience     string `json:"audience"`
	GrantType    string `json:"grant_type"`
}

// OAuthTokenResponse is the response from the OAuth token endpoint.
type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}
