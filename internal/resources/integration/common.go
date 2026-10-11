package integration

import (
	"context"
	"time"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Common holds the envelope attributes every integration resource shares.
// Type models embed it.
type Common struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	TeamIDs        types.Set    `tfsdk:"team_ids"`
	OrganizationID types.String `tfsdk:"organization_id"`
	Version        types.Int64  `tfsdk:"version"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (c *Common) common() *Common { return c }

// commonOf returns the Common embedded in a type model.
func commonOf[M any](m *M) *Common {
	return any(m).(interface{ common() *Common }).common()
}

func commonAttributes() map[string]schema.Attribute {
	stable := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	return map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true, PlanModifiers: stable, MarkdownDescription: "Integration ID."},
		"name": schema.StringAttribute{Required: true, MarkdownDescription: "Integration name."},
		"team_ids": schema.SetAttribute{
			Optional:            true,
			Computed:            true,
			ElementType:         types.StringType,
			PlanModifiers:       []planmodifier.Set{setplanmodifier.UseStateForUnknown()},
			MarkdownDescription: "Team IDs with access. Omit to keep the current teams; an empty set clears them.",
		},
		"organization_id": schema.StringAttribute{Computed: true, PlanModifiers: stable, MarkdownDescription: "Organization owning the integration."},
		"version":         schema.Int64Attribute{Computed: true, MarkdownDescription: "Integration version used for optimistic locking."},
		"created_at":      schema.StringAttribute{Computed: true, PlanModifiers: stable, MarkdownDescription: "Creation timestamp."},
		"updated_at":      schema.StringAttribute{Computed: true, MarkdownDescription: "Last update timestamp."},
	}
}

func setCommon(ctx context.Context, c *Common, remote *client.Integration, diags *diag.Diagnostics) {
	c.ID = types.StringValue(remote.Id)
	c.Name = types.StringValue(remote.Name)
	c.OrganizationID = types.StringValue(remote.OrganizationId)
	c.Version = types.Int64Value(int64(remote.Version))
	c.CreatedAt = timestamp(remote.CreatedAt)
	c.UpdatedAt = timestamp(remote.UpdatedAt)
	teams := []string{}
	if remote.TeamIds != nil {
		teams = *remote.TeamIds
	}
	v, ds := types.SetValueFrom(ctx, types.StringType, teams)
	diags.Append(ds...)
	c.TeamIDs = v
}

func timestamp(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339Nano))
}

// teamsFor resolves the team set to write. An omitted set is planned as the
// current teams, or is unknown on create, which gives no teams.
func teamsFor(ctx context.Context, planned types.Set, diags *diag.Diagnostics) []string {
	result := []string{}
	if !planned.IsNull() && !planned.IsUnknown() {
		diags.Append(planned.ElementsAs(ctx, &result, false)...)
	}
	return result
}

// secretsNote ends each resource description that has secrets.
const secretsNote = "Secrets are written through dedicated endpoints after the integration is saved. They are sensitive but stored in Terraform state. They are write-only: the API never returns them, so a change made outside Terraform is not detected. Removing a secret from the configuration stops managing it without deleting it remotely. If a secret is rejected while the integration is being created, Terraform marks the integration tainted; run `terraform untaint` and apply again to retry the secret without replacing the integration."

func secretAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: description}
}

// Settings the server fills in when they are omitted. Omitting one keeps the
// integration's current value rather than re-stating the server's default.

func defaultedString(description string) schema.StringAttribute {
	return schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, MarkdownDescription: description + " Removing it from the configuration keeps the current value."}
}

func defaultedBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{Optional: true, Computed: true, PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}, MarkdownDescription: description + " Removing it from the configuration keeps the current value."}
}
