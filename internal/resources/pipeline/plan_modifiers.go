package pipeline

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// createOnlyTags keeps the tags already on the pipeline in the plan. The update
// API carries no tags field, so tags are fixed once the pipeline exists. Without
// this the provider plans a removal that the API cannot perform, the next read
// brings the tags back, and the plan never converges.
//
// Terraform rejects a plan that replaces a configured value, so configured tags
// that differ from the pipeline's tags are an error.
type createOnlyTags struct{}

func (m createOnlyTags) Description(_ context.Context) string {
	return "Tags are set when the pipeline is created and cannot be changed afterwards."
}

func (m createOnlyTags) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m createOnlyTags) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// On create there is no prior state, so the configured tags are the plan.
	if req.State.Raw.IsNull() {
		return
	}

	// On destroy there is nothing to keep.
	if req.Plan.Raw.IsNull() {
		return
	}

	// Terraform plans again at apply time, when the value is known.
	if !fullyKnown(req.ConfigValue) {
		return
	}

	if req.ConfigValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}

	// Compare contents, so a null map and an empty map both mean no tags.
	configured := map[string]string{}
	resp.Diagnostics.Append(req.ConfigValue.ElementsAs(ctx, &configured, false)...)
	existing := map[string]string{}
	if !req.StateValue.IsNull() {
		resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &existing, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	checkTagsUnchanged(&resp.Diagnostics, configured, existing)
}

func fullyKnown(tags types.Map) bool {
	if tags.IsUnknown() {
		return false
	}
	for _, value := range tags.Elements() {
		if value.IsUnknown() {
			return false
		}
	}
	return true
}

// checkTagsUnchanged reports configured tags that differ from the tags the
// pipeline already has. Nil configured tags keep the existing tags.
func checkTagsUnchanged(diags *diag.Diagnostics, configured, existing map[string]string) {
	if configured == nil || maps.Equal(configured, existing) {
		return
	}
	addTagsChangeError(diags, existing)
}

func addTagsChangeError(diags *diag.Diagnostics, existing map[string]string) {
	detail := "Grepr sets the tags of a pipeline only when it creates the pipeline, because the update API " +
		"does not accept tags. "
	if len(existing) == 0 {
		detail += "The pipeline has no tags. Remove the tags argument."
	} else {
		pairs := make([]string, 0, len(existing))
		for _, key := range slices.Sorted(maps.Keys(existing)) {
			pairs = append(pairs, fmt.Sprintf("%q = %q", key, existing[key]))
		}
		detail += fmt.Sprintf(
			"The pipeline has these tags: { %s }. Set tags to the same values, or remove the tags "+
				"argument to keep them.",
			strings.Join(pairs, ", "),
		)
	}
	diags.AddAttributeError(path.Root("tags"), "Tags cannot be changed after creation", detail)
}

// reservedTagPrefix matches GREPR_INTERNAL_JOB_TAG_PREFIX on the server, which
// drops tags with this prefix when it creates a pipeline.
const reservedTagPrefix = "__grepr__"

// noReservedTagPrefix rejects tag keys that the server drops. Otherwise the
// create succeeds and every later plan fails because the tags differ.
type noReservedTagPrefix struct{}

func (v noReservedTagPrefix) Description(_ context.Context) string {
	return fmt.Sprintf("tag keys must not start with %q", reservedTagPrefix)
}

func (v noReservedTagPrefix) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v noReservedTagPrefix) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.HasPrefix(req.ConfigValue.ValueString(), reservedTagPrefix) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Reserved tag key",
			fmt.Sprintf("Grepr reserves tag keys that start with %q and drops them. Use a different key.", reservedTagPrefix),
		)
	}
}
