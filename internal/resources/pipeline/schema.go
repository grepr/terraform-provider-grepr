// Package pipeline provides the Terraform resource implementation for Grepr pipelines.
// It defines the schema, data model, and plan modifiers for the grepr_pipeline resource.
package pipeline

import (
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// PipelineResourceModel describes the Terraform state data model for a Grepr pipeline resource.
// This struct maps directly to the HCL attributes defined in PipelineSchema().
//
// The model is divided into three groups:
// 1. Configuration attributes: Set by users in their Terraform config
// 2. Computed attributes: Read-only values populated from the API
// 3. Pipeline status: Nested health and status information
type PipelineResourceModel struct {
	// Configuration attributes
	Name            types.String `tfsdk:"name"`
	JobGraphJSON    types.String `tfsdk:"job_graph_json"`
	DesiredState    types.String `tfsdk:"desired_state"`
	TeamIDs         types.Set    `tfsdk:"team_ids"`
	Tags            types.Map    `tfsdk:"tags"`
	WaitForState    types.Bool   `tfsdk:"wait_for_state"`
	StateTimeout    types.Int64  `tfsdk:"state_timeout"`
	RollbackEnabled types.Bool   `tfsdk:"rollback_enabled"`

	// Computed attributes
	ID             types.String `tfsdk:"id"`
	Version        types.Int64  `tfsdk:"version"`
	State          types.String `tfsdk:"state"`
	OrganizationID types.String `tfsdk:"organization_id"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`

	// Pipeline status (nested)
	PipelineHealth  types.String `tfsdk:"pipeline_health"`
	PipelineMessage types.String `tfsdk:"pipeline_message"`
}

// PipelineSchema returns the complete Terraform schema definition for the grepr_pipeline resource.
//
// The schema defines:
// - Required attributes: name, job_graph_json
// - Optional attributes: desired_state, team_ids, tags, wait_for_state, state_timeout, rollback_enabled
// - Computed attributes: id, version, state, organization_id, created_at, updated_at, pipeline_health, pipeline_message
//
// Plan modifiers are used to:
// - UseStateForUnknown: Preserve values that won't change (id, organization_id, created_at)
// - Static defaults: Provide default values for optional attributes
func PipelineSchema() schema.Schema {
	return schema.Schema{
		MarkdownDescription: "Manages a Grepr pipeline (async streaming job).",

		Attributes: map[string]schema.Attribute{
			// Required configuration attributes
			"name": schema.StringAttribute{
				MarkdownDescription: "The name of the pipeline. Must match the pattern `[a-z0-9_]{1,128}`. Changing this will adopt an existing pipeline with that name or create a new one.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						namePattern,
						"must contain only lowercase letters, numbers, and underscores, and be 1-128 characters long",
					),
				},
			},
			"job_graph_json": schema.StringAttribute{
				MarkdownDescription: "The job graph as a JSON string. Use `jsonencode()` to convert a Terraform object to JSON.",
				Required:            true,
			},

			// Optional configuration
			"desired_state": schema.StringAttribute{
				MarkdownDescription: "The desired state of the pipeline. Valid values are `RUNNING` or `STOPPED`. Defaults to `RUNNING`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("RUNNING"),
				Validators: []validator.String{
					stringvalidator.OneOf("RUNNING", "STOPPED"),
				},
			},
			"team_ids": schema.SetAttribute{
				MarkdownDescription: "Set of team IDs that this pipeline is associated with.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"tags": schema.MapAttribute{
				MarkdownDescription: "Custom tags for the pipeline.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.UseStateForUnknown(),
				},
			},
			"wait_for_state": schema.BoolAttribute{
				MarkdownDescription: "Whether to wait for the pipeline to reach the desired state after create/update operations. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"state_timeout": schema.Int64Attribute{
				MarkdownDescription: "Timeout in seconds for waiting for state transitions. Defaults to `600` (10 minutes).",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(600),
			},
			"rollback_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether to enable automatic rollback on update failures. Defaults to `false`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},

			// Computed attributes (read-only)
			"id": schema.StringAttribute{
				MarkdownDescription: "The unique identifier of the pipeline (TSID format).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "The current version of the pipeline. Increments on updates.",
				Computed:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "The actual current state of the pipeline.",
				Computed:            true,
			},
			"organization_id": schema.StringAttribute{
				MarkdownDescription: "The organization ID that owns this pipeline.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp when the pipeline was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "The timestamp when the pipeline was last updated.",
				Computed:            true,
			},
			"pipeline_health": schema.StringAttribute{
				MarkdownDescription: "The health status of the pipeline. One of `HEALTHY`, `STABILIZING`, `UNHEALTHY`, or `UNKNOWN`.",
				Computed:            true,
			},
			"pipeline_message": schema.StringAttribute{
				MarkdownDescription: "A human-readable message about the pipeline's current status.",
				Computed:            true,
			},
		},
	}
}
