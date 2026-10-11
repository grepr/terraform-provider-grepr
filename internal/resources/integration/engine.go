// Package integration implements one Terraform resource per Grepr integration
// type on a shared lifecycle engine.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// API is the part of the Grepr client the integration resources use.
type API interface {
	CreateIntegration(ctx context.Context, route string, body client.IntegrationWrite) (*client.Integration, error)
	GetIntegration(ctx context.Context, route, id string) (*client.Integration, error)
	UpdateIntegration(ctx context.Context, route, id string, body client.IntegrationWrite) (*client.Integration, error)
	DeleteIntegration(ctx context.Context, route, id string) error
	SetIntegrationSecret(ctx context.Context, route, id, endpoint, value string) error
	SetSecretHeader(ctx context.Context, id, name, value string) error
	DeleteSecretHeader(ctx context.Context, id, name string) error
	GetIngestionToken(ctx context.Context, id string) (string, error)
	SetDatadogAPIKeys(ctx context.Context, id string, keys generated.SetApiKeysRequest) (*client.Integration, error)
}

// Kind declares one integration type. The engine owns the lifecycle; a kind
// owns its attributes, its secrets, and how its settings map to the payload.
// M is the type's model, which embeds Common; P is the payload type generated
// from the API spec.
type Kind[M, P any] struct {
	// Type is the resource name infix, the envelope type, and the API route, e.g. "datadog".
	Type        string
	Description string
	Attributes  map[string]schema.Attribute
	// ClearReadOnly removes the fields the server fills on reads and rejects on
	// writes. Optional.
	ClearReadOnly func(*P)
	// SecretAttributes are the attributes WriteSecrets owns. Every other
	// attribute takes its planned value after a successful write.
	SecretAttributes []string
	// ToPayload sets the settings this kind manages. Fields with unknown planned
	// values are left alone so the server derives them.
	ToPayload func(M, *P)
	// FromPayload copies the remote settings into the model.
	FromPayload func(P, *M)
	// WriteSecrets writes changed secrets in order and records each success in next.
	WriteSecrets func(w *secretWriter, plan M, next *M)
	// OnCreate captures values that only the create response carries. Optional.
	OnCreate func(created P, m *M)
	// OnRead fills values a read of the integration does not carry. Optional.
	OnRead func(ctx context.Context, api API, id string, m *M) diag.Diagnostics
}

type integrationResource[M, P any] struct {
	kind   Kind[M, P]
	client API
}

var (
	_ API                              = (*client.Client)(nil)
	_ resource.ResourceWithConfigure   = &integrationResource[DatadogModel, generated.Datadog]{}
	_ resource.ResourceWithImportState = &integrationResource[DatadogModel, generated.Datadog]{}
)

func newResource[M, P any](kind Kind[M, P]) func() resource.Resource {
	return func() resource.Resource { return &integrationResource[M, P]{kind: kind} }
}

// payloadKey names the private state entry holding the integration's last
// payload as the server returned it. Updates are written over it, so fields
// this provider version does not model are sent back unchanged.
const payloadKey = "payload"

// privateState is a resource's private state, as a response carries it.
type privateState interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func (r *integrationResource[M, P]) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + typeNameSuffix(r.kind.Type)
}

func (r *integrationResource[M, P]) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := commonAttributes()
	maps.Copy(attributes, r.kind.Attributes)
	resp.Schema = schema.Schema{MarkdownDescription: r.kind.Description, Attributes: attributes}
}

func (r *integrationResource[M, P]) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *integrationResource[M, P]) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k := r.kind
	planned := commonOf(&plan)
	teams := teamsFor(ctx, planned.TeamIDs, &resp.Diagnostics)
	// Secrets start empty and are recorded one by one as they are written, so a
	// failed write leaves state describing exactly what exists remotely. This is
	// prepared before the integration exists so nothing can fail between its
	// creation and the state that records it.
	next := r.planWithSecrets(ctx, req.Plan, nil, &resp.Diagnostics)
	var settings P
	k.ToPayload(plan, &settings)
	payload, err := encodePayload(settings, nil)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration settings", err.Error())
	}
	if resp.Diagnostics.HasError() {
		return
	}
	remote, err := r.client.CreateIntegration(ctx, k.Type, client.IntegrationWrite{Name: planned.Name.ValueString(), Type: generated.WriteDataWarehouseType(k.Type), Payload: payload, TeamIds: &teams})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create integration", err.Error())
		return
	}
	setCommon(ctx, commonOf(&next), remote, &resp.Diagnostics)
	created := r.remember(ctx, resp.Private, remote, &resp.Diagnostics)
	if k.OnCreate != nil {
		k.OnCreate(created, &next)
	}
	r.fillDerivedSettings(ctx, req.Plan, &next, created, &resp.Diagnostics)
	r.writeSecrets(ctx, resp.Private, plan, &next, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *integrationResource[M, P]) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m M
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := commonOf(&m).ID.ValueString()
	remote, err := r.client.GetIntegration(ctx, r.kind.Type, id)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read integration", err.Error())
		return
	}
	setCommon(ctx, commonOf(&m), remote, &resp.Diagnostics)
	// Secrets are write-only: reads return them masked, so state keeps what
	// Terraform wrote.
	r.kind.FromPayload(r.remember(ctx, resp.Private, remote, &resp.Diagnostics), &m)
	if r.kind.OnRead != nil {
		resp.Diagnostics.Append(r.kind.OnRead(ctx, r.client, id, &m)...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *integrationResource[M, P]) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior M
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k := r.kind
	current := commonOf(&prior)
	planned := commonOf(&plan)
	teams := teamsFor(ctx, planned.TeamIDs, &resp.Diagnostics)
	next := r.planWithSecrets(ctx, req.Plan, &req.State, &resp.Diagnostics)
	// The configuration is written over the payload last read, and the version
	// read with it makes the server reject the write if the integration has
	// changed since.
	var remote map[string]any
	last, diags := req.Private.GetKey(ctx, payloadKey)
	resp.Diagnostics.Append(diags...)
	if len(last) == 0 {
		// Writing without it would reset every setting this provider does not model.
		resp.Diagnostics.AddError("Integration settings not read yet", "Refresh the integration before updating it, for example by running terraform apply without -refresh=false.")
	} else if err := decodeJSON(last, &remote); err != nil {
		resp.Diagnostics.AddError("Unable to read the stored integration settings", err.Error())
	}
	if resp.Diagnostics.HasError() {
		return
	}
	settings, err := decodePayload[P](remote)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read the stored integration settings", err.Error())
	}
	if resp.Diagnostics.HasError() {
		return
	}
	k.ToPayload(plan, &settings)
	payload, err := encodePayload(settings, remote)
	if err != nil {
		resp.Diagnostics.AddError("Unable to encode integration settings", err.Error())
		return
	}
	write := client.IntegrationWrite{Name: planned.Name.ValueString(), Type: generated.WriteDataWarehouseType(k.Type), Version: int32(current.Version.ValueInt64() + 1), Payload: payload, TeamIds: &teams}
	updated, err := r.client.UpdateIntegration(ctx, k.Type, current.ID.ValueString(), write)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update integration", err.Error())
		return
	}
	setCommon(ctx, commonOf(&next), updated, &resp.Diagnostics)
	r.fillDerivedSettings(ctx, req.Plan, &next, r.remember(ctx, resp.Private, updated, &resp.Diagnostics), &resp.Diagnostics)
	r.writeSecrets(ctx, resp.Private, plan, &next, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

func (r *integrationResource[M, P]) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m M
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteIntegration(ctx, r.kind.Type, commonOf(&m).ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete integration", err.Error())
	}
}

func (r *integrationResource[M, P]) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Invalid integration ID", "Import using the integration ID.")
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// writeSecrets writes the changed secrets, then, because those writes bump the
// integration's version without returning it, reads the integration again for
// its version and payload. Secrets keep the values written: a server replica can
// serve secrets from a cache older than the write. Problems with that read are
// warnings, since the writes landed; the next refresh reads again.
func (r *integrationResource[M, P]) writeSecrets(ctx context.Context, private privateState, plan M, next *M, diags *diag.Diagnostics) {
	id := commonOf(next).ID.ValueString()
	writer := &secretWriter{ctx: ctx, api: r.client, route: r.kind.Type, id: id, diags: diags}
	r.kind.WriteSecrets(writer, plan, next)
	if !writer.attempted {
		return
	}
	var problems diag.Diagnostics
	remote, err := r.client.GetIntegration(ctx, r.kind.Type, id)
	if err != nil {
		problems.AddError("Unable to read the integration after writing its secrets", err.Error())
	} else {
		setCommon(ctx, commonOf(next), remote, &problems)
		r.remember(ctx, private, remote, &problems)
	}
	for _, problem := range problems {
		diags.AddWarning(problem.Summary(), problem.Detail())
	}
}

// remember decodes a payload the server returned and stores it, without its
// read-only fields, in private state for the next update.
func (r *integrationResource[M, P]) remember(ctx context.Context, private privateState, remote *client.Integration, diags *diag.Diagnostics) P {
	settings, err := decodePayload[P](remote.Payload)
	if err != nil {
		diags.AddError("Unable to read integration settings", err.Error())
		return settings
	}
	writable := settings
	if r.kind.ClearReadOnly != nil {
		r.kind.ClearReadOnly(&writable)
	}
	payload, err := encodePayload(writable, remote.Payload)
	if err == nil {
		var stored []byte
		if stored, err = json.Marshal(payload); err == nil {
			diags.Append(private.SetKey(ctx, payloadKey, stored)...)
		}
	}
	if err != nil {
		diags.AddError("Unable to store integration settings", err.Error())
	}
	return settings
}

// planWithSecrets returns the planned model with the kind's secret attributes
// taken from prior state, or null when there is no prior state.
func (r *integrationResource[M, P]) planWithSecrets(ctx context.Context, plan tfsdk.Plan, prior *tfsdk.State, diags *diag.Diagnostics) M {
	var result M
	values := map[string]tftypes.Value{}
	if err := plan.Raw.As(&values); err != nil {
		diags.AddError("Unable to read plan", err.Error())
		return result
	}
	priorValues := map[string]tftypes.Value{}
	if prior != nil {
		if err := prior.Raw.As(&priorValues); err != nil {
			diags.AddError("Unable to read state", err.Error())
			return result
		}
	}
	attributeTypes := plan.Raw.Type().(tftypes.Object).AttributeTypes
	for _, name := range r.kind.SecretAttributes {
		if previous, ok := priorValues[name]; ok {
			values[name] = previous
		} else {
			values[name] = tftypes.NewValue(attributeTypes[name], nil)
		}
	}
	state := tfsdk.State{Schema: plan.Schema, Raw: tftypes.NewValue(plan.Raw.Type(), values)}
	diags.Append(state.Get(ctx, &result)...)
	return result
}

// fillDerivedSettings replaces settings left unknown by the plan (omitted
// optional/computed attributes) with the values the server chose.
func (r *integrationResource[M, P]) fillDerivedSettings(ctx context.Context, plan tfsdk.Plan, m *M, remote P, diags *diag.Diagnostics) {
	observed := *m
	r.kind.FromPayload(remote, &observed)
	current, chosen := tfsdk.State{Schema: plan.Schema}, tfsdk.State{Schema: plan.Schema}
	diags.Append(current.Set(ctx, m)...)
	diags.Append(chosen.Set(ctx, &observed)...)
	if diags.HasError() {
		return
	}
	values, chosenValues := map[string]tftypes.Value{}, map[string]tftypes.Value{}
	if err := current.Raw.As(&values); err != nil {
		diags.AddError("Unable to read state", err.Error())
		return
	}
	if err := chosen.Raw.As(&chosenValues); err != nil {
		diags.AddError("Unable to read state", err.Error())
		return
	}
	for _, name := range sortedKeys(r.kind.Attributes) {
		if !values[name].IsKnown() && !isSecret(r.kind.SecretAttributes, name) {
			values[name] = chosenValues[name]
		}
	}
	current.Raw = tftypes.NewValue(current.Raw.Type(), values)
	diags.Append(current.Get(ctx, m)...)
}
