package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func tagsValue(t *testing.T, tags map[string]string) types.Map {
	t.Helper()
	if tags == nil {
		return types.MapNull(types.StringType)
	}
	value, diags := types.MapValueFrom(context.Background(), types.StringType, tags)
	if diags.HasError() {
		t.Fatalf("failed to build tags: %v", diags)
	}
	return value
}

// Terraform accepts a planned value that differs from the config only when the
// config is null. Otherwise the plan must keep the configured tags, so a change
// has to fail with an error that tells the user what to configure.
func TestCreateOnlyTags(t *testing.T) {
	existingPipeline := tftypes.NewValue(tftypes.Object{}, map[string]tftypes.Value{})
	uiManaged := map[string]string{"grepr-ui-managed": "true"}
	partlyUnknownTags := types.MapValueMust(types.StringType, map[string]attr.Value{"team": types.StringUnknown()})

	tests := []struct {
		name       string
		created    bool
		state      types.Map
		config     types.Map
		wantPlan   types.Map
		wantDetail string
	}{
		{
			name:     "create takes the configured tags",
			state:    types.MapNull(types.StringType),
			config:   tagsValue(t, map[string]string{"team": "a"}),
			wantPlan: tagsValue(t, map[string]string{"team": "a"}),
		},
		{
			name:     "unset tags keep the existing tags",
			created:  true,
			state:    tagsValue(t, uiManaged),
			config:   types.MapNull(types.StringType),
			wantPlan: tagsValue(t, uiManaged),
		},
		{
			name:     "matching tags plan no change",
			created:  true,
			state:    tagsValue(t, uiManaged),
			config:   tagsValue(t, uiManaged),
			wantPlan: tagsValue(t, uiManaged),
		},
		{
			name:     "empty tags match a pipeline without tags",
			created:  true,
			state:    types.MapNull(types.StringType),
			config:   tagsValue(t, map[string]string{}),
			wantPlan: tagsValue(t, map[string]string{}),
		},
		{
			name:     "unknown tags wait for apply",
			created:  true,
			state:    types.MapNull(types.StringType),
			config:   types.MapUnknown(types.StringType),
			wantPlan: types.MapUnknown(types.StringType),
		},
		{
			name:     "partly unknown tags wait for apply",
			created:  true,
			state:    tagsValue(t, map[string]string{"team": "a"}),
			config:   partlyUnknownTags,
			wantPlan: partlyUnknownTags,
		},
		{
			name:       "tags on a pipeline without tags",
			created:    true,
			state:      types.MapNull(types.StringType),
			config:     tagsValue(t, map[string]string{"team": "a"}),
			wantDetail: "The pipeline has no tags.",
		},
		{
			name:       "changed tags",
			created:    true,
			state:      tagsValue(t, uiManaged),
			config:     tagsValue(t, map[string]string{"team": "a"}),
			wantDetail: `The pipeline has these tags: { "grepr-ui-managed" = "true" }.`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.MapRequest{
				Path:        path.Root("tags"),
				Plan:        tfsdk.Plan{Raw: existingPipeline},
				StateValue:  tc.state,
				ConfigValue: tc.config,
				PlanValue:   tc.config,
			}
			if tc.created {
				req.State = tfsdk.State{Raw: existingPipeline}
			}
			resp := &planmodifier.MapResponse{PlanValue: req.PlanValue}

			createOnlyTags{}.PlanModifyMap(context.Background(), req, resp)

			if tc.wantDetail != "" {
				if !resp.Diagnostics.HasError() {
					t.Fatalf("expected an error, got plan %s", resp.PlanValue)
				}
				if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, tc.wantDetail) {
					t.Errorf("error detail %q does not contain %q", detail, tc.wantDetail)
				}
				return
			}
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected error: %v", resp.Diagnostics)
			}
			if !resp.PlanValue.Equal(tc.wantPlan) {
				t.Errorf("planned %s, want %s", resp.PlanValue, tc.wantPlan)
			}
		})
	}
}

// Adoption runs this check in Create, because a new resource has no state to
// compare against at plan time.
func TestCheckTagsUnchanged(t *testing.T) {
	uiManaged := map[string]string{"grepr-ui-managed": "true"}

	tests := []struct {
		name       string
		configured map[string]string
		existing   map[string]string
		wantError  bool
	}{
		{name: "unset tags keep the existing tags", configured: nil, existing: uiManaged},
		{name: "matching tags", configured: map[string]string{"grepr-ui-managed": "true"}, existing: uiManaged},
		{name: "empty tags match no tags", configured: map[string]string{}, existing: nil},
		{name: "empty tags on a pipeline with tags", configured: map[string]string{}, existing: uiManaged, wantError: true},
		{name: "changed tags", configured: map[string]string{"team": "a"}, existing: uiManaged, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var diags diag.Diagnostics
			checkTagsUnchanged(&diags, tc.configured, tc.existing)
			if diags.HasError() != tc.wantError {
				t.Errorf("got error %v, want %v: %v", diags.HasError(), tc.wantError, diags)
			}
		})
	}
}

func TestNoReservedTagPrefix(t *testing.T) {
	tests := []struct {
		key       string
		wantError bool
	}{
		{key: "team"},
		{key: "grepr-ui-managed"},
		{key: "__grepr__owner", wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			req := validator.StringRequest{Path: path.Root("tags"), ConfigValue: types.StringValue(tc.key)}
			resp := &validator.StringResponse{}
			noReservedTagPrefix{}.ValidateString(context.Background(), req, resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Errorf("got error %v, want %v: %v", resp.Diagnostics.HasError(), tc.wantError, resp.Diagnostics)
			}
		})
	}
}
