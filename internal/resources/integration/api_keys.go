package integration

import (
	"context"

	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// APIKey is one entry of a Datadog integration's API key set.
type APIKey struct {
	KeyID   types.String `tfsdk:"key_id"`
	Value   types.String `tfsdk:"value"`
	Label   types.String `tfsdk:"label"`
	SendKey types.Bool   `tfsdk:"send_key"`
}

func apiKeyType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"key_id": types.StringType, "value": types.StringType, "label": types.StringType, "send_key": types.BoolType}}
}

func apiKeysAttribute() schema.Attribute {
	return schema.MapNestedAttribute{
		Optional:            true,
		Validators:          []validator.Map{mapvalidator.NoNullValues(), mapvalidator.ConflictsWith(path.MatchRoot("api_key")), apiKeysValidator{}},
		MarkdownDescription: "The integration's entire API key set, keyed by stable labels; reordering does not rotate keys, but renaming a label re-creates that key under a new ID. Keys added outside Terraform are removed the next time Terraform writes the set. Exactly one entry sets `send_key`. Holds 1–64 entries, and the organization's key quota (10 by default) also applies. Removing the attribute stops managing the set without deleting keys. Conflicts with `api_key`.",
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"key_id":   schema.StringAttribute{Optional: true, Computed: true, Validators: []validator.String{stringvalidator.LengthAtLeast(1)}, PlanModifiers: []planmodifier.String{stringplanmodifier.UseNonNullStateForUnknown()}, MarkdownDescription: "Existing key ID, or the ID the server assigns to a new key. Omit it for a new key."},
			"value":    schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Plaintext key. Required for a new key; omit to keep an existing key by `key_id`."},
			"label":    schema.StringAttribute{Optional: true, MarkdownDescription: "Display label. Omission clears the remote label."},
			"send_key": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false), MarkdownDescription: "Use this key to send data to Datadog."},
		}},
	}
}

func keyValues(ctx context.Context, value types.Map, diags *diag.Diagnostics) map[string]APIKey {
	result := map[string]APIKey{}
	if value.IsNull() || value.IsUnknown() {
		return result
	}
	diags.Append(value.ElementsAs(ctx, &result, true)...)
	return result
}

func keyMap(ctx context.Context, keys map[string]APIKey, diags *diag.Diagnostics) types.Map {
	v, ds := types.MapValueFrom(ctx, apiKeyType(), keys)
	diags.Append(ds...)
	return v
}

// remoteKeys returns the integration's keys that carry an ID, in server order.
func remoteKeys(views *[]generated.IngestKeyView) []generated.IngestKeyView {
	var keys []generated.IngestKeyView
	if views != nil {
		for _, view := range *views {
			if view.KeyId != nil && *view.KeyId != "" {
				keys = append(keys, view)
			}
		}
	}
	return keys
}

// apiKeys replaces the remote key set with the configured one. The endpoint
// replaces the whole set, so retrying after a failure cannot duplicate keys: an
// entry without a key_id is added again and any key the request omits is
// removed.
func (w *secretWriter) apiKeys(desired types.Map, current *types.Map) {
	if w.diags.HasError() {
		return
	}
	if desired.IsNull() {
		*current = desired
		return
	}
	keys := keyValues(w.ctx, desired, w.diags)
	old := keyValues(w.ctx, *current, w.diags)
	if w.diags.HasError() {
		return
	}
	if desired.Equal(*current) {
		return
	}
	names := sortedKeys(keys)
	request := generated.SetApiKeysRequest{Keys: make([]generated.ApiKey, 0, len(names))}
	for _, name := range names {
		key, previous := keys[name], old[name]
		sendKey := key.SendKey.ValueBool()
		entry := generated.ApiKey{SendKey: &sendKey}
		setString(&entry.KeyId, key.KeyID)
		setString(&entry.Label, key.Label)
		// Send a value for new keys, rotations, and labels moved to another key; an
		// unchanged key goes by ID only.
		unchanged := key.KeyID.Equal(previous.KeyID) && key.Value.Equal(previous.Value)
		if entry.KeyId == nil || !unchanged {
			setString(&entry.Value, key.Value)
		}
		request.Keys = append(request.Keys, entry)
	}
	w.attempted = true
	updated, err := w.api.SetDatadogAPIKeys(w.ctx, w.id, request)
	if err != nil {
		w.diags.AddError("Unable to save Datadog API keys", err.Error())
		return
	}
	saved, err := decodePayload[generated.Datadog](updated.Payload)
	if err != nil {
		w.diags.AddError("Unable to read the saved Datadog API keys", err.Error())
		return
	}
	// The server stores keys in request order, which assigns each generated ID
	// to its label.
	views := remoteKeys(saved.ApiKeys)
	if len(views) != len(names) {
		w.diags.AddError("Unable to reconcile Datadog API keys", "The returned key set did not match the request. Refresh and inspect the key set before retrying.")
		return
	}
	resolved := map[string]APIKey{}
	for i, name := range names {
		key := keys[name]
		key.KeyID = types.StringValue(*views[i].KeyId)
		resolved[name] = key
	}
	*current = keyMap(w.ctx, resolved, w.diags)
}

// apiKeysValidator rejects a key set the server would silently reinterpret:
// it picks the send key itself when none or several are marked, which
// Terraform would then report as an inconsistent result.
type apiKeysValidator struct{}

func (apiKeysValidator) Description(context.Context) string {
	return "exactly one key sets send_key"
}
func (v apiKeysValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (apiKeysValidator) ValidateMap(ctx context.Context, req validator.MapRequest, resp *validator.MapResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	sendCount := 0
	for _, element := range req.ConfigValue.Elements() {
		if element.IsNull() || element.IsUnknown() {
			return
		}
		var key APIKey
		resp.Diagnostics.Append(element.(types.Object).As(ctx, &key, basetypes.ObjectAsOptions{})...)
		if key.SendKey.IsUnknown() {
			return
		}
		if key.SendKey.ValueBool() {
			sendCount++
		}
	}
	if sendCount != 1 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid send key", "Exactly one API key must set send_key = true.")
	}
}
