package pipeline

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// createOnlyTags keeps the tags already on the pipeline in the plan. The update
// API carries no tags field, so tags are fixed once the pipeline exists. Without
// this the provider plans a removal that the API cannot perform, the next read
// brings the tags back, and the plan never converges.
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

	resp.PlanValue = req.StateValue

	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if !req.ConfigValue.Equal(req.StateValue) {
		resp.Diagnostics.AddAttributeWarning(
			req.Path,
			"Tags cannot be changed after creation",
			"The Grepr update API does not accept tags, so the configured tags are ignored and the "+
				"pipeline keeps the tags it was created with.",
		)
	}
}
