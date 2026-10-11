package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type SumoModel struct {
	Common
	Endpoint types.String `tfsdk:"endpoint"`
}

var NewSumoResource = newResource(Kind[SumoModel, generated.Sumo]{
	Type:        "sumo",
	Description: "Manages a Sumo Logic integration. " + secretsNote,
	Attributes: map[string]schema.Attribute{
		"endpoint": secretAttribute("HTTP collector URL. It embeds the collector's secret code, so it is handled as a secret; the server validates its format."),
	},
	ClearReadOnly:    func(p *generated.Sumo) { p.Endpoint = nil },
	SecretAttributes: []string{"endpoint"},
	ToPayload:        func(SumoModel, *generated.Sumo) {},
	FromPayload:      func(generated.Sumo, *SumoModel) {},
	WriteSecrets: func(w *secretWriter, plan SumoModel, next *SumoModel) {
		w.put("endpoint", plan.Endpoint, &next.Endpoint)
	},
})
