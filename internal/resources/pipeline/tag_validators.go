package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// reservedTagPrefix matches GREPR_INTERNAL_JOB_TAG_PREFIX on the server, which
// drops tags with this prefix from every public request.
const reservedTagPrefix = "__grepr__"

// noReservedTagPrefix rejects tag keys that the server drops. Otherwise the
// apply succeeds and every later plan shows the dropped tags as a change.
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
