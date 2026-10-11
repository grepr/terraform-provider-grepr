package integration

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// secretWriter writes secrets through their dedicated endpoints after the
// integration's metadata. Each write that succeeds is recorded in the next
// state immediately; the first failure stops the sequence, so unwritten secrets
// keep their previous state and are retried on the next apply.
type secretWriter struct {
	ctx   context.Context
	api   API
	route string
	id    string
	diags *diag.Diagnostics
	// attempted records that a write was sent, which bumps the integration's version.
	attempted bool
}

// put writes one secret when it is configured and differs from state. A
// secret removed from configuration is relinquished: the remote value stays.
func (w *secretWriter) put(endpoint string, desired types.String, current *types.String) {
	if w.diags.HasError() {
		return
	}
	if desired.IsNull() || desired.Equal(*current) {
		*current = desired
		return
	}
	w.attempted = true
	if err := w.api.SetIntegrationSecret(w.ctx, w.route, w.id, endpoint, desired.ValueString()); err != nil {
		w.diags.AddError("Unable to save integration secret", fmt.Sprintf("Failed to save %s: %s", endpoint, err))
		return
	}
	*current = desired
}
