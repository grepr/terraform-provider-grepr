package pipeline

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
