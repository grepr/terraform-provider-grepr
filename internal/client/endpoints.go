// Package client provides API endpoint path constants for the Grepr API.
package client

// API endpoint paths for the Grepr server.
//
// IMPORTANT: The base URL (e.g., "http://org.app.grepr.localhost:7665") should NOT
// include the "/api" prefix - it's already part of these endpoint paths.
//
// Example usage:
//   - List jobs: GET http://org.app.grepr.localhost:7665/api/v1/jobs
//   - Get job:   GET http://org.app.grepr.localhost:7665/api/v1/jobs/{id}
//   - Create:    POST http://org.app.grepr.localhost:7665/api/v1/jobs/async
const (
	// EndpointJobsAsync is the path for creating new async jobs (pipelines)
	EndpointJobsAsync = "/api/v1/jobs/async"

	// EndpointJobs is the path for listing all jobs
	EndpointJobs = "/api/v1/jobs"

	// EndpointJob is the path template for getting/updating/deleting a specific job.
	// Use fmt.Sprintf(EndpointJob, jobID) to construct the full path.
	EndpointJob = "/api/v1/jobs/%s"

	// EndpointJobTags is the path template for replacing a job's tags in place.
	// Use fmt.Sprintf(EndpointJobTags, jobID) to construct the full path.
	EndpointJobTags = "/api/v1/jobs/%s/tags"
)
