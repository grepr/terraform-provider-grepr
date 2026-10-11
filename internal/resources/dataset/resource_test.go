package dataset

import (
	"context"
	"strconv"
	"testing"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type datasetAPI struct {
	dataset generated.DatasetRead
	err     error
}

func (a *datasetAPI) CreateDataset(_ context.Context, body generated.DatasetCreate) (*generated.DatasetRead, error) {
	a.dataset = generated.DatasetRead{Id: "dataset", Name: body.Name, IntegrationId: body.IntegrationId, OrganizationId: "org", TeamIds: body.TeamIds}
	return &a.dataset, a.err
}
func (a *datasetAPI) GetDataset(context.Context, string) (*generated.DatasetRead, error) {
	return &a.dataset, a.err
}
func (a *datasetAPI) UpdateDataset(_ context.Context, _ string, body generated.DatasetUpdate) (*generated.DatasetRead, error) {
	a.dataset.Name = body.Name
	a.dataset.TeamIds = body.TeamIds
	return &a.dataset, a.err
}
func (a *datasetAPI) DeleteDataset(context.Context, string) error { a.dataset.Id = ""; return a.err }

func stateFor(t *testing.T, m Model) tfsdk.State {
	t.Helper()
	if m.TeamIDs.IsNull() {
		m.TeamIDs = types.SetNull(types.StringType)
	}
	s := tfsdk.State{Schema: Schema()}
	if d := s.Set(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return s
}
func modelFrom(t *testing.T, s tfsdk.State) Model {
	t.Helper()
	var m Model
	if d := s.Get(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return m
}
func TestDatasetCreateRenameAndDelete(t *testing.T) {
	ctx := context.Background()
	api := &datasetAPI{}
	r := &DatasetResource{client: api}
	m := Model{Name: types.StringValue("logs"), IntegrationID: types.StringValue("warehouse"), TeamIDs: types.SetUnknown(types.StringType)}
	planned := stateFor(t, m)
	created := resource.CreateResponse{State: tfsdk.State{Schema: Schema()}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: Schema(), Raw: planned.Raw}}, &created)
	if created.Diagnostics.HasError() {
		t.Fatal(created.Diagnostics)
	}
	got := modelFrom(t, created.State)
	if got.ID.ValueString() != "dataset" || got.TeamIDs.IsUnknown() {
		t.Fatalf("invalid created state: %+v", got)
	}
	got.Name = types.StringValue("renamed")
	next := stateFor(t, got)
	updated := resource.UpdateResponse{State: created.State}
	r.Update(ctx, resource.UpdateRequest{State: created.State, Plan: tfsdk.Plan{Schema: Schema(), Raw: next.Raw}}, &updated)
	if updated.Diagnostics.HasError() {
		t.Fatal(updated.Diagnostics)
	}
	if api.dataset.Name != "renamed" || modelFrom(t, updated.State).ID.ValueString() != "dataset" {
		t.Fatal("rename replaced or failed to update dataset")
	}
	deleted := resource.DeleteResponse{State: updated.State}
	r.Delete(ctx, resource.DeleteRequest{State: updated.State}, &deleted)
	// The framework removes the resource from state after a successful delete.
	if deleted.Diagnostics.HasError() {
		t.Fatalf("delete failed: %v", deleted.Diagnostics)
	}
}
func TestDatasetReadDistinguishesNotFoundAndForbidden(t *testing.T) {
	for _, code := range []int{404, 403, 500} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			r := &DatasetResource{client: &datasetAPI{err: &client.APIError{StatusCode: code}}}
			state := stateFor(t, Model{ID: types.StringValue("dataset")})
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if code == 404 {
				if !resp.State.Raw.IsNull() || resp.Diagnostics.HasError() {
					t.Fatal(resp.Diagnostics)
				}
			} else if !resp.Diagnostics.HasError() || resp.State.Raw.IsNull() {
				t.Fatal("lost resource on read failure")
			}
		})
	}
}
func TestDatasetImportAndTeams(t *testing.T) {
	ctx := context.Background()
	api := &datasetAPI{dataset: generated.DatasetRead{Id: "id", Name: "existing", IntegrationId: "warehouse", OrganizationId: "org", TeamIds: &[]string{"team"}}}
	r := &DatasetResource{client: api}
	imported := resource.ImportStateResponse{State: stateFor(t, Model{})}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "id"}, &imported)
	if imported.Diagnostics.HasError() {
		t.Fatal(imported.Diagnostics)
	}
	resp := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	m := modelFrom(t, resp.State)
	if m.Name.ValueString() != "existing" || len(m.TeamIDs.Elements()) != 1 {
		t.Fatal("import did not read metadata and teams")
	}
	m.TeamIDs = types.SetValueMust(types.StringType, nil)
	p := stateFor(t, m)
	u := resource.UpdateResponse{State: resp.State}
	r.Update(ctx, resource.UpdateRequest{State: resp.State, Plan: tfsdk.Plan{Schema: Schema(), Raw: p.Raw}}, &u)
	if u.Diagnostics.HasError() || len(*api.dataset.TeamIds) != 0 {
		t.Fatalf("explicit empty teams not applied: %v", u.Diagnostics)
	}
}
