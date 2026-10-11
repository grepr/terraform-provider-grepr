package integration

import (
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type SplunkModel struct {
	Common
	SplunkHost    types.String `tfsdk:"splunk_host"`
	IngestHost    types.String `tfsdk:"ingest_host"`
	IngestPort    types.String `tfsdk:"ingest_port"`
	RestHost      types.String `tfsdk:"rest_host"`
	RestPort      types.String `tfsdk:"rest_port"`
	WebHost       types.String `tfsdk:"web_host"`
	WebPort       types.String `tfsdk:"web_port"`
	Secure        types.Bool   `tfsdk:"secure"`
	AuthToken     types.String `tfsdk:"auth_token"`
	RestAuthToken types.String `tfsdk:"rest_auth_token"`
}

var NewSplunkResource = newResource(Kind[SplunkModel, generated.Splunk]{
	Type:        "splunk",
	Description: "Manages a Splunk integration. Set either `splunk_host`, or `ingest_host` and `web_host` for separate endpoints. Tokens are always validated against the configured Splunk endpoints when saved. " + secretsNote,
	Attributes: map[string]schema.Attribute{
		"splunk_host":     schema.StringAttribute{Optional: true, MarkdownDescription: "Host serving ingestion, REST and web traffic. Use instead of the separate hosts."},
		"ingest_host":     schema.StringAttribute{Optional: true, MarkdownDescription: "Host for HTTP Event Collector ingestion. Set together with `web_host`."},
		"ingest_port":     defaultedString("Ingestion port. The server defaults it to `8088`."),
		"rest_host":       schema.StringAttribute{Optional: true, MarkdownDescription: "Host for the REST API."},
		"rest_port":       defaultedString("REST API port. The server defaults it to `8089`."),
		"web_host":        schema.StringAttribute{Optional: true, MarkdownDescription: "Host for the Splunk web UI, used to link to backfilled logs. Set together with `ingest_host`."},
		"web_port":        defaultedString("Web UI port. The server defaults it to `443`."),
		"secure":          defaultedBool("Verify the Splunk endpoints' TLS certificates. Requests always use HTTPS; `false` accepts self-signed certificates. The server defaults it to `true`."),
		"auth_token":      secretAttribute("HTTP Event Collector token, validated against the ingestion endpoint."),
		"rest_auth_token": secretAttribute("REST API token, validated against the REST endpoint."),
	},
	ClearReadOnly: func(p *generated.Splunk) {
		p.AuthToken, p.RestAuthToken, p.Exceptions = nil, nil, nil
	},
	SecretAttributes: []string{"auth_token", "rest_auth_token"},
	ToPayload: func(m SplunkModel, p *generated.Splunk) {
		setString(&p.SplunkHost, m.SplunkHost)
		setString(&p.IngestHost, m.IngestHost)
		setString(&p.IngestPort, m.IngestPort)
		setString(&p.RestHost, m.RestHost)
		setString(&p.RestPort, m.RestPort)
		setString(&p.WebHost, m.WebHost)
		setString(&p.WebPort, m.WebPort)
		setBool(&p.Secure, m.Secure)
	},
	FromPayload: func(p generated.Splunk, m *SplunkModel) {
		m.SplunkHost = stringValue(p.SplunkHost)
		m.IngestHost = stringValue(p.IngestHost)
		m.IngestPort = stringValue(p.IngestPort)
		m.RestHost = stringValue(p.RestHost)
		m.RestPort = stringValue(p.RestPort)
		m.WebHost = stringValue(p.WebHost)
		m.WebPort = stringValue(p.WebPort)
		m.Secure = boolValue(p.Secure)
	},
	WriteSecrets: func(w *secretWriter, plan SplunkModel, next *SplunkModel) {
		w.put("auth-token", plan.AuthToken, &next.AuthToken)
		w.put("rest-auth-token", plan.RestAuthToken, &next.RestAuthToken)
	},
})
