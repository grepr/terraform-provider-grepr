// Package provider implements the Grepr Terraform provider.
//
// The provider handles configuration, authentication, and resource registration.
// It uses OAuth2 client credentials flow via Auth0 to authenticate with the Grepr API.
//
// Configuration can be provided via:
//   - Provider block attributes (host, client_id, client_secret, auth0_domain)
//   - Environment variables (GREPR_HOST, GREPR_CLIENT_ID, GREPR_CLIENT_SECRET, GREPR_AUTH0_DOMAIN)
//
// Environment variables take precedence over provider block attributes.
package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/resources/pipeline"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Compile-time check that GreprProvider implements the provider.Provider interface.
var _ provider.Provider = &GreprProvider{}

// GreprProvider defines the provider implementation.
type GreprProvider struct {
	version string
}

// GreprProviderModel describes the provider data model.
type GreprProviderModel struct {
	Host         types.String `tfsdk:"host"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Auth0Domain  types.String `tfsdk:"auth0_domain"`
}

// New creates a new provider instance.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &GreprProvider{
			version: version,
		}
	}
}

// Metadata returns the provider type name.
func (p *GreprProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "grepr"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration.
func (p *GreprProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Grepr provider is used to manage Grepr pipelines using the Grepr API.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				MarkdownDescription: "The Grepr API host URL (e.g., `https://myorg.app.grepr.ai/api`). Can also be set via the `GREPR_HOST` environment variable.",
				Optional:            true,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "The OAuth client ID for authentication. Can also be set via the `GREPR_CLIENT_ID` environment variable.",
				Optional:            true,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "The OAuth client secret for authentication. Can also be set via the `GREPR_CLIENT_SECRET` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"auth0_domain": schema.StringAttribute{
				MarkdownDescription: "The Auth0 domain for OAuth authentication. Defaults to `grepr-prod.us.auth0.com`. Can also be set via the `GREPR_AUTH0_DOMAIN` environment variable.",
				Optional:            true,
			},
		},
	}
}

// Configure prepares the Grepr API client for data sources and resources.
func (p *GreprProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config GreprProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	host := getConfigValue(config.Host, "GREPR_HOST")
	clientID := getConfigValue(config.ClientID, "GREPR_CLIENT_ID")
	clientSecret := getConfigValue(config.ClientSecret, "GREPR_CLIENT_SECRET")
	auth0Domain := getConfigValue(config.Auth0Domain, "GREPR_AUTH0_DOMAIN")

	if host == "" {
		resp.Diagnostics.AddError(
			"Missing Host Configuration",
			"The provider requires a host to be configured. Set the `host` attribute or the `GREPR_HOST` environment variable.",
		)
	}

	// Validate host URL format
	if host != "" {
		u, err := url.Parse(host)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("host"),
				"Invalid Host URL",
				fmt.Sprintf("Host URL is invalid: %s", err.Error()),
			)
		} else if u.Scheme != "http" && u.Scheme != "https" {
			resp.Diagnostics.AddAttributeError(
				path.Root("host"),
				"Invalid Host URL Scheme",
				fmt.Sprintf("Host must start with http:// or https://, got: %s", host),
			)
		} else if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			// Redundant check but keeps the prefix logic consistent if url.Parse is lenient
			resp.Diagnostics.AddAttributeError(
				path.Root("host"),
				"Invalid Host URL",
				fmt.Sprintf("Host must start with http:// or https://, got: %s", host),
			)
		}
	}

	if clientID == "" {
		resp.Diagnostics.AddError(
			"Missing Client ID Configuration",
			"The provider requires a client_id to be configured. Set the `client_id` attribute or the `GREPR_CLIENT_ID` environment variable.",
		)
	}

	if clientSecret == "" {
		resp.Diagnostics.AddError(
			"Missing Client Secret Configuration",
			"The provider requires a client_secret to be configured. Set the `client_secret` attribute or the `GREPR_CLIENT_SECRET` environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	c := client.NewClient(client.Config{
		Host:         host,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Auth0Domain:  auth0Domain,
	})

	resp.DataSourceData = c
	resp.ResourceData = c
}

// Resources defines the resources implemented by the provider.
func (p *GreprProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		pipeline.NewPipelineResource,
	}
}

// DataSources defines the data sources implemented by the provider.
func (p *GreprProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// getConfigValue returns the config value if set, otherwise falls back to the environment variable.
func getConfigValue(configValue types.String, envVar string) string {
	if !configValue.IsNull() && !configValue.IsUnknown() {
		return configValue.ValueString()
	}
	return os.Getenv(envVar)
}
