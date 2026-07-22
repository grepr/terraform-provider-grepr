// Package pipeline implements the grepr_pipeline Terraform resource.
//
// The pipeline resource manages Grepr async streaming jobs, which are data
// processing pipelines that continuously process log data from sources
// (Datadog, Splunk, etc.) through operations (parsing, filtering, etc.)
// to sinks (data warehouse, Iceberg tables, etc.).
//
// Key features:
//   - Adoption: If a pipeline with the same name exists, it will be adopted
//     rather than failing with a conflict error
//   - State waiting: By default, operations wait for the pipeline to reach
//     a stable state (RUNNING or STOPPED) before completing
//   - Optimistic locking: Updates use version numbers to prevent conflicts
//   - Import: Existing pipelines can be imported by ID or name
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Compile-time checks that PipelineResource implements required interfaces
var (
	_ resource.Resource                = &PipelineResource{}
	_ resource.ResourceWithConfigure   = &PipelineResource{}
	_ resource.ResourceWithImportState = &PipelineResource{}

	// namePattern enforces pipeline naming rules: lowercase alphanumeric and underscores only
	namePattern = regexp.MustCompile(`^[a-z0-9_]{1,128}$`)
)

// PipelineResource defines the resource implementation.
type PipelineResource struct {
	client *client.Client
}

// NewPipelineResource creates a new pipeline resource.
func NewPipelineResource() resource.Resource {
	return &PipelineResource{}
}

// Metadata returns the resource type name.
func (r *PipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

// Schema returns the resource schema.
func (r *PipelineResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = PipelineSchema()
}

// Configure sets up the resource with the provider client.
func (r *PipelineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData),
		)
		return
	}

	r.client = c
}

// Create creates a new pipeline or adopts an existing one.
//
// Adoption behavior: If an active (non-terminal) pipeline with the same name already
// exists in Grepr, this resource will "adopt" it instead of failing. This allows users to:
//   - Import existing pipelines into Terraform management
//   - Re-run terraform apply after manual creation in the UI
//   - Recover from partial failures where the resource was created but not tracked
//
// After adoption, if the plan differs from the existing pipeline's configuration,
// an update will be performed to reconcile them.
//
// If the only pipeline with that name is in a terminal state (DELETED, FAILED, FINISHED,
// or CANCELLED), the name is free to reuse, so a brand new pipeline is created rather than
// adopting the dead one.
func (r *PipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	tflog.Debug(ctx, "Creating pipeline", map[string]interface{}{"name": name})

	// Check if a pipeline with this name already exists (for adoption)
	existingJob, err := r.client.GetJobByName(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to check for existing pipeline", err.Error())
		return
	}

	var job *client.Job
	var jobGraphJSONToPreserve string
	var tagsToPreserve map[string]string

	if existingJob != nil && !client.IsTerminal(existingJob.State) {
		// Adopt the existing active pipeline
		tflog.Info(ctx, "Adopting existing pipeline", map[string]interface{}{
			"name": name,
			"id":   existingJob.Id,
		})
		job = existingJob

		// Apply any differences as an update
		needsUpdate := r.needsUpdate(ctx, plan, existingJob)
		if needsUpdate {
			updateReq, err := r.buildUpdateRequest(ctx, plan, existingJob)
			if err != nil {
				resp.Diagnostics.AddError("Failed to build update request", err.Error())
				return
			}

			// Preserve the original plan values
			tagsToPreserve, err = r.extractTags(ctx, plan.Tags)
			if err != nil {
				resp.Diagnostics.AddError("Failed to extract tags", err.Error())
				return
			}
			jobGraphJSONToPreserve = plan.JobGraphJSON.ValueString()

			updatedJob, err := r.client.UpdateJob(ctx, existingJob.Id, *updateReq, plan.RollbackEnabled.ValueBool())
			if err != nil {
				if apiErr, ok := err.(*client.APIError); ok && apiErr.IsConflict() {
					resp.Diagnostics.AddError(
						"Pipeline Name Conflict",
						fmt.Sprintf(
							"A pipeline named %q already exists and could not be adopted because it was "+
								"modified by another process. Re-run 'terraform apply' to retry, or bring the "+
								"existing pipeline under Terraform management with "+
								"'terraform import grepr_pipeline.<resource_name> %s'.",
							name, name,
						),
					)
					return
				}
				resp.Diagnostics.AddError("Failed to update adopted pipeline", err.Error())
				return
			}
			job = updatedJob
		} else {
			// No update needed, preserve plan values
			tagsToPreserve, err = r.extractTags(ctx, plan.Tags)
			if err != nil {
				resp.Diagnostics.AddError("Failed to extract tags", err.Error())
				return
			}
			jobGraphJSONToPreserve = plan.JobGraphJSON.ValueString()
		}
	} else {
		// Create a new pipeline
		createReq, tags, err := r.buildCreateRequest(ctx, plan)
		if err != nil {
			resp.Diagnostics.AddError("Failed to build create request", err.Error())
			return
		}

		jobGraphJSONToPreserve = plan.JobGraphJSON.ValueString()
		tagsToPreserve = tags

		newJob, err := r.client.CreateAsyncJob(ctx, *createReq)
		if err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.IsConflict() {
				resp.Diagnostics.AddError(
					"Pipeline Name Conflict",
					fmt.Sprintf(
						"A pipeline named %q already exists. Pipeline names must be unique within an "+
							"organization. Choose a different name, or bring the existing pipeline under "+
							"Terraform management with 'terraform import grepr_pipeline.<resource_name> %s'.",
						name, name,
					),
				)
				return
			}
			resp.Diagnostics.AddError("Failed to create pipeline", err.Error())
			return
		}
		job = newJob
	}

	// Wait for stable state if requested
	if plan.WaitForState.ValueBool() {
		timeout := time.Duration(plan.StateTimeout.ValueInt64()) * time.Second
		desiredState := client.JobState(plan.DesiredState.ValueString())

		stableJob, err := r.client.WaitForState(ctx, job.Id, desiredState, timeout)
		if err != nil {
			resp.Diagnostics.AddError(
				"Pipeline did not reach desired state",
				fmt.Sprintf("Pipeline created but did not reach state %s: %s", desiredState, err.Error()),
			)
		}
		if stableJob != nil {
			job = stableJob
		}
	}

	// Update state from the job, but preserve the original request for job_graph_json, tags, and desired state
	r.updateModelFromJob(ctx, &plan, job, &originalJobData{
		JobGraphJSON: jobGraphJSONToPreserve,
		Tags:         tagsToPreserve,
		DesiredState: plan.DesiredState.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *PipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	if id == "" {
		// Try to look up by name
		name := state.Name.ValueString()
		job, err := r.client.GetJobByName(ctx, name)
		if err != nil {
			resp.Diagnostics.AddError("Failed to read pipeline", err.Error())
			return
		}
		if job == nil {
			resp.State.RemoveResource(ctx)
			return
		}
		r.updateModelFromJob(ctx, &state, job, nil)
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	job, err := r.client.GetJob(ctx, id)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.IsNotFound() {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read pipeline", err.Error())
		return
	}

	r.updateModelFromJob(ctx, &state, job, nil)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the pipeline.
func (r *PipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PipelineResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state PipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Read the current state from the API to get the latest version
	currentJob, err := r.client.GetJob(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read current pipeline state", err.Error())
		return
	}

	updateReq, err := r.buildUpdateRequest(ctx, plan, currentJob)
	if err != nil {
		resp.Diagnostics.AddError("Failed to build update request", err.Error())
		return
	}

	// Extract tags from plan to preserve in state
	tags, err := r.extractTags(ctx, plan.Tags)
	if err != nil {
		resp.Diagnostics.AddError("Failed to extract tags", err.Error())
		return
	}

	tflog.Debug(ctx, "Updating pipeline", map[string]interface{}{
		"id":          id,
		"fromVersion": currentJob.Version,
	})

	job, err := r.client.UpdateJob(ctx, id, *updateReq, plan.RollbackEnabled.ValueBool())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.IsConflict() {
			resp.Diagnostics.AddError(
				"Version Conflict",
				"The pipeline was modified by another process. Please run terraform refresh and try again.",
			)
			return
		}
		resp.Diagnostics.AddError("Failed to update pipeline", err.Error())
		return
	}

	// Wait for stable state if requested
	if plan.WaitForState.ValueBool() {
		timeout := time.Duration(plan.StateTimeout.ValueInt64()) * time.Second
		desiredState := client.JobState(plan.DesiredState.ValueString())

		stableJob, err := r.client.WaitForState(ctx, job.Id, desiredState, timeout)
		if err != nil {
			resp.Diagnostics.AddError(
				"Pipeline did not reach desired state",
				fmt.Sprintf("Pipeline updated but did not reach state %s: %s", desiredState, err.Error()),
			)
		}
		if stableJob != nil {
			job = stableJob
		}
	}

	r.updateModelFromJob(ctx, &plan, job, &originalJobData{
		JobGraphJSON: plan.JobGraphJSON.ValueString(),
		Tags:         tags,
		DesiredState: plan.DesiredState.ValueString(),
	})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete deletes the pipeline.
func (r *PipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	tflog.Debug(ctx, "Deleting pipeline", map[string]interface{}{"id": id})

	err := r.client.DeleteJob(ctx, id)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.IsNotFound() {
			// Already deleted
			return
		}
		resp.Diagnostics.AddError("Failed to delete pipeline", err.Error())
		return
	}

	// Wait for deletion if requested
	if state.WaitForState.ValueBool() {
		timeout := time.Duration(state.StateTimeout.ValueInt64()) * time.Second
		if err := r.client.WaitForDeletion(ctx, id, timeout); err != nil {
			resp.Diagnostics.AddError(
				"Pipeline deletion may not be complete",
				fmt.Sprintf("Delete request accepted but pipeline may still be deleting: %s", err.Error()),
			)
		}
	}
}

// ImportState imports an existing pipeline by ID or name.
func (r *PipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	idOrName := req.ID

	// First try to get by ID
	job, err := r.client.GetJob(ctx, idOrName)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.IsNotFound() {
			// Try by name
			job, err = r.client.GetJobByName(ctx, idOrName)
			if err != nil {
				resp.Diagnostics.AddError("Failed to import pipeline", err.Error())
				return
			}
			if job == nil {
				resp.Diagnostics.AddError(
					"Pipeline not found",
					fmt.Sprintf("No pipeline found with ID or name: %s", idOrName),
				)
				return
			}
		} else {
			resp.Diagnostics.AddError("Failed to import pipeline", err.Error())
			return
		}
	}

	// Set the ID for import
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), job.Id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), job.Name)...)
}

// buildCreateRequest builds a CreateJobRequest from the plan.
// Returns the request and the extracted tags map for state preservation.
func (r *PipelineResource) buildCreateRequest(ctx context.Context, plan PipelineResourceModel) (*client.CreateJobRequest, map[string]string, error) {
	jobGraph, err := r.parseJobGraph(plan.JobGraphJSON.ValueString())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse job_graph_json: %w", err)
	}

	tags, err := r.extractTags(ctx, plan.Tags)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract tags: %w", err)
	}

	teamIDs, err := r.extractTeamIDs(ctx, plan.TeamIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to extract team_ids: %w", err)
	}

	return &client.CreateJobRequest{
		Name:       plan.Name.ValueString(),
		Execution:  generated.CreateJobExecutionASYNCHRONOUS,
		Processing: generated.CreateJobProcessingSTREAMING,
		JobGraph:   *jobGraph,
		Tags:       mapToCreateJobTags(tags),
		TeamIds:    teamIDs,
	}, tags, nil
}

// buildUpdateRequest builds an UpdateJobRequest from the plan and current job.
func (r *PipelineResource) buildUpdateRequest(ctx context.Context, plan PipelineResourceModel, currentJob *client.Job) (*client.UpdateJobRequest, error) {
	jobGraph, err := r.parseJobGraph(plan.JobGraphJSON.ValueString())
	if err != nil {
		return nil, fmt.Errorf("failed to parse job_graph_json: %w", err)
	}

	teamIDs, err := r.extractTeamIDs(ctx, plan.TeamIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to extract team_ids: %w", err)
	}

	return &client.UpdateJobRequest{
		FromVersion:  currentJob.Version,
		DesiredState: generated.UpdateJobDesiredState(plan.DesiredState.ValueString()),
		JobGraph:     *jobGraph,
		TeamIds:      teamIDs,
	}, nil
}

// parseJobGraph parses a JSON string into a JobGraph.
func (r *PipelineResource) parseJobGraph(jsonStr string) (*client.JobGraph, error) {
	var jobGraph client.JobGraph
	if err := json.Unmarshal([]byte(jsonStr), &jobGraph); err != nil {
		return nil, err
	}
	return &jobGraph, nil
}

// extractTags extracts tags from the Terraform types.Map.
func (r *PipelineResource) extractTags(ctx context.Context, tagsAttr types.Map) (map[string]string, error) {
	if tagsAttr.IsNull() || tagsAttr.IsUnknown() {
		return nil, nil
	}

	tags := make(map[string]string)
	diags := tagsAttr.ElementsAs(ctx, &tags, false)
	if diags.HasError() {
		return nil, fmt.Errorf("failed to extract tags")
	}
	return tags, nil
}

// extractTeamIDs extracts team IDs from the Terraform types.Set.
func (r *PipelineResource) extractTeamIDs(ctx context.Context, teamIDsAttr types.Set) (*[]string, error) {
	if teamIDsAttr.IsNull() || teamIDsAttr.IsUnknown() {
		return nil, nil
	}

	var teamIDs []string
	diags := teamIDsAttr.ElementsAs(ctx, &teamIDs, false)
	if diags.HasError() {
		return nil, fmt.Errorf("failed to extract team_ids")
	}
	return &teamIDs, nil
}

// needsUpdate checks if an update is needed based on the plan vs current state.
func (r *PipelineResource) needsUpdate(ctx context.Context, plan PipelineResourceModel, currentJob *client.Job) bool {
	// Check desired state
	if plan.DesiredState.ValueString() != string(currentJob.DesiredState) {
		return true
	}

	// Check job graph - compare JSON
	planGraph, err := r.parseJobGraph(plan.JobGraphJSON.ValueString())
	if err != nil {
		return true
	}

	planGraphJSON, err := json.Marshal(planGraph)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal plan job graph", map[string]interface{}{"error": err.Error()})
		return true
	}

	currentGraphJSON, err := json.Marshal(currentJob.JobGraph)
	if err != nil {
		tflog.Error(ctx, "Failed to marshal current job graph", map[string]interface{}{"error": err.Error()})
		return true
	}

	if string(planGraphJSON) != string(currentGraphJSON) {
		return true
	}

	return false
}

// originalJobData holds values from the original Terraform plan that should be
// preserved in state rather than using the API response values.
//
// This is important because:
//   - job_graph_json: The API may add default fields or reorder JSON keys, causing
//     spurious diffs on subsequent plans
//   - tags: The API may add system tags that the user didn't specify
//   - desired_state: We want to track what the user requested, not the current state
type originalJobData struct {
	JobGraphJSON string
	Tags         map[string]string
	DesiredState string
}

// updateModelFromJob updates the Terraform model from an API job response.
//
// This method populates computed fields (id, version, state, timestamps) from
// the API response while optionally preserving user-specified values for fields
// that may differ between the request and response (job_graph_json, tags).
//
// The originalData parameter, when provided, ensures that Terraform state matches
// what the user specified in their configuration, avoiding spurious diffs.
func (r *PipelineResource) updateModelFromJob(ctx context.Context, model *PipelineResourceModel, job *client.Job, originalData *originalJobData) {
	model.ID = types.StringValue(job.Id)
	model.Name = types.StringValue(job.Name)
	model.Version = types.Int64Value(job.Version)
	model.State = types.StringValue(string(job.State))

	// Preserve the original desired state if provided
	if originalData != nil && originalData.DesiredState != "" {
		model.DesiredState = types.StringValue(originalData.DesiredState)
	} else {
		model.DesiredState = types.StringValue(string(job.DesiredState))
	}

	model.OrganizationID = types.StringValue(job.OrganizationId)
	model.CreatedAt = types.StringValue(job.CreatedAt.Format(time.RFC3339))
	model.UpdatedAt = types.StringValue(job.UpdatedAt.Format(time.RFC3339))

	// Pipeline status is not in the OpenAPI spec, so set to null
	model.PipelineHealth = types.StringNull()
	model.PipelineMessage = types.StringNull()

	// Use the original request's job graph JSON if provided, otherwise use the API response
	// This avoids inconsistencies from server-added default fields and JSON field ordering
	if originalData != nil && originalData.JobGraphJSON != "" {
		model.JobGraphJSON = types.StringValue(originalData.JobGraphJSON)
	} else if model.JobGraphJSON.IsUnknown() || model.JobGraphJSON.IsNull() {
		jobGraphJSON, err := json.Marshal(job.JobGraph)
		if err == nil {
			model.JobGraphJSON = types.StringValue(string(jobGraphJSON))
		}
	}

	// Use the original request's tags if provided, otherwise use the API response
	// This avoids inconsistencies from server-added system tags
	if originalData != nil {
		if len(originalData.Tags) > 0 {
			model.Tags, _ = types.MapValueFrom(ctx, types.StringType, originalData.Tags)
		} else {
			model.Tags = types.MapNull(types.StringType)
		}
	} else if model.Tags.IsUnknown() || model.Tags.IsNull() {
		tags := readJobTagsToMap(job.Tags)
		if len(tags) > 0 {
			model.Tags, _ = types.MapValueFrom(ctx, types.StringType, tags)
		} else {
			model.Tags = types.MapNull(types.StringType)
		}
	}

	// Convert team IDs
	if job.TeamIds != nil && len(*job.TeamIds) > 0 {
		model.TeamIDs, _ = types.SetValueFrom(ctx, types.StringType, *job.TeamIds)
	} else {
		model.TeamIDs = types.SetNull(types.StringType)
	}
}

// mapToCreateJobTags converts a map[string]string to *map[string]string for CreateJob.
// Returns nil when the map is nil or empty so the field is omitted from the JSON payload.
func mapToCreateJobTags(m map[string]string) *map[string]string {
	if len(m) == 0 {
		return nil
	}
	return &m
}

// readJobTagsToMap converts map[string]string from ReadJob to map[string]string
func readJobTagsToMap(tags map[string]string) map[string]string {
	return tags
}
