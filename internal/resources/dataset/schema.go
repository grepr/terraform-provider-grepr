package dataset

import (
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type Model struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	IntegrationID  types.String `tfsdk:"integration_id"`
	OrganizationID types.String `tfsdk:"organization_id"`
	TeamIDs        types.Set    `tfsdk:"team_ids"`
}

func Schema() schema.Schema {
	return schema.Schema{MarkdownDescription: "Manages a Grepr dataset and its backing tables. Table settings made outside Terraform, such as attribute promotion, are left alone. Deleting it drops the tables and is refused while pipelines use it; add `lifecycle { prevent_destroy = true }` to protect stored data.", Attributes: map[string]schema.Attribute{
		"id":              schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Dataset ID."},
		"name":            schema.StringAttribute{Required: true, MarkdownDescription: "Dataset name. Can be updated in place."},
		"integration_id":  schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Managed or S3 warehouse integration ID. Changing it replaces the dataset: the new one starts without the old one's data or table settings, such as its promotion policy."},
		"organization_id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Organization owning the dataset."},
		"team_ids":        schema.SetAttribute{Optional: true, Computed: true, ElementType: types.StringType, PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}, MarkdownDescription: "Team IDs. Omit to preserve existing teams; use an empty set to clear them."},
	}}
}
