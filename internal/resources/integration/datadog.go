package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type DatadogModel struct {
	Common
	Site                         types.String `tfsdk:"site"`
	DashboardFreshnessWindow     types.String `tfsdk:"dashboard_freshness_window"`
	IncludeNeverViewedDashboards types.Bool   `tfsdk:"include_never_viewed_dashboards"`
	APIKey                       types.String `tfsdk:"api_key"`
	APIKeys                      types.Map    `tfsdk:"api_keys"`
	AppKey                       types.String `tfsdk:"app_key"`
}

var NewDatadogResource = newResource(Kind[DatadogModel, generated.Datadog]{
	Type:        "datadog",
	Description: "Manages a Datadog integration, used by pipelines to receive Datadog agent data and send data back to Datadog. " + secretsNote,
	Attributes: map[string]schema.Attribute{
		"site":                            schema.StringAttribute{Required: true, MarkdownDescription: "Datadog site, such as `datadoghq.com`. Must be one of the organization's allowed sites."},
		"dashboard_freshness_window":      schema.StringAttribute{Optional: true, MarkdownDescription: "Import only dashboards viewed within this ISO-8601 duration, written in the server's form such as `PT720H` for 30 days; another spelling shows as a change. Omit to import every dashboard."},
		"include_never_viewed_dashboards": schema.BoolAttribute{Optional: true, MarkdownDescription: "Also import dashboards that were never viewed."},
		"api_key":                         secretAttribute("API key used to send data to Datadog, validated with Datadog. Saving it rotates the send key and is refused while a pipeline using the integration is running. Conflicts with `api_keys`."),
		"api_keys":                        apiKeysAttribute(),
		"app_key":                         secretAttribute("Application key. Grepr checks its scopes with Datadog, which needs the API key saved first."),
	},
	ClearReadOnly: func(p *generated.Datadog) {
		p.ApiKey, p.ApiKeys, p.AppKey, p.ClientToken, p.Exceptions = nil, nil, nil, nil, nil
	},
	SecretAttributes: []string{"api_key", "api_keys", "app_key"},
	ToPayload: func(m DatadogModel, p *generated.Datadog) {
		p.Site = m.Site.ValueString()
		setString(&p.DashboardFreshnessWindow, m.DashboardFreshnessWindow)
		setBool(&p.IncludeNeverViewedDashboards, m.IncludeNeverViewedDashboards)
	},
	FromPayload: func(p generated.Datadog, m *DatadogModel) {
		m.Site = types.StringValue(p.Site)
		m.DashboardFreshnessWindow = stringValue(p.DashboardFreshnessWindow)
		m.IncludeNeverViewedDashboards = boolValue(p.IncludeNeverViewedDashboards)
	},
	// The application key's validation needs the API key, so keys go first.
	WriteSecrets: func(w *secretWriter, plan DatadogModel, next *DatadogModel) {
		w.apiKeys(plan.APIKeys, &next.APIKeys)
		w.put("api-key", plan.APIKey, &next.APIKey)
		w.put("app-key", plan.AppKey, &next.AppKey)
	},
})
