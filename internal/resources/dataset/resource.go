// Package dataset implements the grepr_dataset resource.
package dataset

import (
	"context"
	"fmt"
	"strings"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type API interface {
	CreateDataset(context.Context, generated.DatasetCreate) (*generated.DatasetRead, error)
	GetDataset(context.Context, string) (*generated.DatasetRead, error)
	UpdateDataset(context.Context, string, generated.DatasetUpdate) (*generated.DatasetRead, error)
	DeleteDataset(context.Context, string) error
}
type DatasetResource struct{ client API }

var (
	_ API                              = (*client.Client)(nil)
	_ resource.Resource                = &DatasetResource{}
	_ resource.ResourceWithConfigure   = &DatasetResource{}
	_ resource.ResourceWithImportState = &DatasetResource{}
)

func NewDatasetResource() resource.Resource { return &DatasetResource{} }
func (r *DatasetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dataset"
}
func (r *DatasetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = Schema()
}
func (r *DatasetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(API)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider client", fmt.Sprintf("Expected the Grepr API client, got %T", req.ProviderData))
		return
	}
	r.client = c
}
func (r *DatasetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	teams := []string{}
	if !m.TeamIDs.IsUnknown() && !m.TeamIDs.IsNull() {
		resp.Diagnostics.Append(m.TeamIDs.ElementsAs(ctx, &teams, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.CreateDataset(ctx, generated.DatasetCreate{Name: m.Name.ValueString(), IntegrationId: m.IntegrationID.ValueString(), TeamIds: &teams})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create dataset", err.Error())
		return
	}
	updateModel(ctx, &m, d, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *DatasetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m Model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.GetDataset(ctx, m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read dataset", err.Error())
		return
	}
	updateModel(ctx, &m, d, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *DatasetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, old Model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &old)...)
	if resp.Diagnostics.HasError() {
		return
	}
	teams := []string{}
	resp.Diagnostics.Append(m.TeamIDs.ElementsAs(ctx, &teams, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.client.UpdateDataset(ctx, old.ID.ValueString(), generated.DatasetUpdate{Name: m.Name.ValueString(), TeamIds: &teams})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update dataset", err.Error())
		return
	}
	updateModel(ctx, &m, d, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *DatasetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m Model
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteDataset(ctx, m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete dataset", err.Error())
	}
}
func (r *DatasetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Invalid dataset ID", "Import using the dataset ID.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
func updateModel(ctx context.Context, m *Model, d *generated.DatasetRead, diags *diag.Diagnostics) {
	m.ID = types.StringValue(d.Id)
	m.Name = types.StringValue(d.Name)
	m.IntegrationID = types.StringValue(d.IntegrationId)
	m.OrganizationID = types.StringValue(d.OrganizationId)
	teams := []string{}
	if d.TeamIds != nil {
		teams = *d.TeamIds
	}
	v, ds := types.SetValueFrom(ctx, types.StringType, teams)
	diags.Append(ds...)
	m.TeamIDs = v
}
