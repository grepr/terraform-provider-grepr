package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type NewRelicModel struct {
	Common
	Site                     types.String `tfsdk:"site"`
	PrivateLink              types.Bool   `tfsdk:"private_link"`
	AccountID                types.String `tfsdk:"account_id"`
	DashboardFreshnessWindow types.String `tfsdk:"dashboard_freshness_window"`
	LicenseKey               types.String `tfsdk:"license_key"`
	APIKey                   types.String `tfsdk:"api_key"`
}

var NewNewRelicResource = newResource(Kind[NewRelicModel, generated.NewRelic]{
	Type:        "newrelic",
	Description: "Manages a New Relic integration. Keys are always validated with New Relic when saved. " + secretsNote,
	Attributes: map[string]schema.Attribute{
		"site":                       schema.StringAttribute{Required: true, MarkdownDescription: "New Relic site. Must be one of the organization's allowed sites."},
		"private_link":               defaultedBool("Connect through AWS PrivateLink. The server defaults it to `false`."),
		"account_id":                 schema.StringAttribute{Optional: true, MarkdownDescription: "New Relic account ID."},
		"dashboard_freshness_window": defaultedString("Import only dashboards viewed within this ISO-8601 duration, written in the server's form such as `PT672H` for 28 days, its default; another spelling shows as a change."),
		"license_key":                secretAttribute("License key used to send logs to New Relic."),
		"api_key":                    secretAttribute("User API key used to read from New Relic."),
	},
	ClearReadOnly: func(p *generated.NewRelic) {
		p.LicenseKey, p.ApiKey, p.Exceptions = nil, nil, nil
	},
	SecretAttributes: []string{"license_key", "api_key"},
	ToPayload: func(m NewRelicModel, p *generated.NewRelic) {
		p.Site = m.Site.ValueString()
		setBool(&p.PrivateLink, m.PrivateLink)
		setString(&p.AccountId, m.AccountID)
		setString(&p.DashboardFreshnessWindow, m.DashboardFreshnessWindow)
	},
	FromPayload: func(p generated.NewRelic, m *NewRelicModel) {
		m.Site = types.StringValue(p.Site)
		m.PrivateLink = boolValue(p.PrivateLink)
		m.AccountID = stringValue(p.AccountId)
		m.DashboardFreshnessWindow = stringValue(p.DashboardFreshnessWindow)
	},
	WriteSecrets: func(w *secretWriter, plan NewRelicModel, next *NewRelicModel) {
		w.put("license-key", plan.LicenseKey, &next.LicenseKey)
		w.put("api-key", plan.APIKey, &next.APIKey)
	},
})
