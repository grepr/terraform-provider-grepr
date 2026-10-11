package integration

import (
	"context"
	"errors"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type OTLPModel struct {
	Common
	Endpoint         types.String `tfsdk:"endpoint"`
	LogsEndpoint     types.String `tfsdk:"logs_endpoint"`
	TracesEndpoint   types.String `tfsdk:"traces_endpoint"`
	Headers          types.List   `tfsdk:"headers"`
	AttributeHeaders types.List   `tfsdk:"attribute_headers"`
	OutputEncoding   types.String `tfsdk:"output_encoding"`
	SecretHeaders    types.Map    `tfsdk:"secret_headers"`
	IngestionToken   types.String `tfsdk:"ingestion_token"`
}

func headerType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"name": types.StringType, "value": types.StringType}}
}

func attributeHeaderType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{"header_name": types.StringType, "attribute_path": types.ListType{ElemType: types.StringType}}}
}

var NewOTLPResource = newResource(Kind[OTLPModel, generated.Otlp]{
	Type:        "otlp",
	Description: "Manages an OTLP integration, used by pipelines to send logs and traces to an OTLP/HTTP endpoint. " + secretsNote,
	Attributes: map[string]schema.Attribute{
		"endpoint":        schema.StringAttribute{Required: true, MarkdownDescription: "Base OTLP/HTTP URL. Logs and traces go to `/v1/logs` and `/v1/traces` under it unless overridden."},
		"logs_endpoint":   schema.StringAttribute{Optional: true, MarkdownDescription: "Full URL for logs, overriding the one derived from `endpoint`."},
		"traces_endpoint": schema.StringAttribute{Optional: true, MarkdownDescription: "Full URL for traces, overriding the one derived from `endpoint`."},
		"headers": schema.ListNestedAttribute{
			Optional:            true,
			MarkdownDescription: "Non-secret HTTP headers sent with every request. A name may repeat to send several values.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"name":  schema.StringAttribute{Required: true, MarkdownDescription: "Header name."},
				"value": schema.StringAttribute{Required: true, MarkdownDescription: "Header value."},
			}},
		},
		"attribute_headers": schema.ListNestedAttribute{
			Optional:            true,
			MarkdownDescription: "Headers whose values come from each event's attributes.",
			NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
				"header_name":    schema.StringAttribute{Required: true, MarkdownDescription: "Header to send."},
				"attribute_path": schema.ListAttribute{Required: true, ElementType: types.StringType, MarkdownDescription: "Attribute path segments, one per nesting level."},
			}},
		},
		"output_encoding": defaultedString("Request encoding, `proto` or `json` in lowercase. The server defaults it to `proto`."),
		"secret_headers": schema.MapAttribute{
			Optional:            true,
			Sensitive:           true,
			ElementType:         types.StringType,
			Validators:          []validator.Map{mapvalidator.NoNullValues()},
			MarkdownDescription: "Secret HTTP headers by exact name, such as `Authorization`. Removing a header from this map deletes it remotely; removing the whole attribute stops managing the headers without deleting them. Headers this resource never managed are left alone.",
		},
		"ingestion_token": schema.StringAttribute{
			Computed:            true,
			Sensitive:           true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			MarkdownDescription: "Token generated at creation that senders present to Grepr. Stored in state. After an import it is read once from an endpoint that requires edit access and is audit-logged.",
		},
	},
	ClearReadOnly: func(p *generated.Otlp) {
		p.Token, p.PlainTextToken, p.SecretHeaders = nil, nil, nil
	},
	SecretAttributes: []string{"secret_headers", "ingestion_token"},
	ToPayload: func(m OTLPModel, p *generated.Otlp) {
		p.Endpoint = m.Endpoint.ValueString()
		setString(&p.LogsEndpoint, m.LogsEndpoint)
		setString(&p.TracesEndpoint, m.TracesEndpoint)
		setString(&p.OutputEncoding, m.OutputEncoding)
		p.Headers = nil
		if !m.Headers.IsNull() {
			headers := []generated.Header{}
			for _, element := range m.Headers.Elements() {
				header := element.(types.Object).Attributes()
				headers = append(headers, generated.Header{Name: header["name"].(types.String).ValueString(), Value: header["value"].(types.String).ValueString()})
			}
			p.Headers = &headers
		}
		p.AttributeHeaders = nil
		if !m.AttributeHeaders.IsNull() {
			mappings := []generated.AttributeHeaderMapping{}
			for _, element := range m.AttributeHeaders.Elements() {
				mapping := element.(types.Object).Attributes()
				segments := []string{}
				for _, segment := range mapping["attribute_path"].(types.List).Elements() {
					segments = append(segments, segment.(types.String).ValueString())
				}
				mappings = append(mappings, generated.AttributeHeaderMapping{HeaderName: mapping["header_name"].(types.String).ValueString(), AttributePath: &segments})
			}
			p.AttributeHeaders = &mappings
		}
	},
	FromPayload: func(p generated.Otlp, m *OTLPModel) {
		m.Endpoint = types.StringValue(p.Endpoint)
		m.LogsEndpoint = stringValue(p.LogsEndpoint)
		m.TracesEndpoint = stringValue(p.TracesEndpoint)
		m.OutputEncoding = stringValue(p.OutputEncoding)
		m.Headers = types.ListNull(headerType())
		if p.Headers != nil {
			headers := []attr.Value{}
			for _, header := range *p.Headers {
				headers = append(headers, types.ObjectValueMust(headerType().AttrTypes, map[string]attr.Value{"name": types.StringValue(header.Name), "value": types.StringValue(header.Value)}))
			}
			m.Headers = types.ListValueMust(headerType(), headers)
		}
		// The server returns an empty list when no attribute headers are set.
		if p.AttributeHeaders == nil || (len(*p.AttributeHeaders) == 0 && m.AttributeHeaders.IsNull()) {
			m.AttributeHeaders = types.ListNull(attributeHeaderType())
			return
		}
		mappings := []attr.Value{}
		for _, mapping := range *p.AttributeHeaders {
			segments := []attr.Value{}
			if mapping.AttributePath != nil {
				for _, segment := range *mapping.AttributePath {
					segments = append(segments, types.StringValue(segment))
				}
			}
			mappings = append(mappings, types.ObjectValueMust(attributeHeaderType().AttrTypes, map[string]attr.Value{
				"header_name":    types.StringValue(mapping.HeaderName),
				"attribute_path": types.ListValueMust(types.StringType, segments),
			}))
		}
		m.AttributeHeaders = types.ListValueMust(attributeHeaderType(), mappings)
	},
	OnCreate: func(created generated.Otlp, m *OTLPModel) {
		// Only the create response carries the token in the clear; without it,
		// the token stays null and the next refresh reads it from the token endpoint.
		if created.PlainTextToken != nil {
			m.IngestionToken = types.StringValue(*created.PlainTextToken)
		}
	},
	WriteSecrets: writeSecretHeaders,
	OnRead:       readIngestionToken,
})

// writeSecretHeaders writes changed headers, then deletes headers removed from
// the map. A header stays in state until its deletion succeeds, so a failed
// deletion is retried. Removing the whole attribute relinquishes the headers.
func writeSecretHeaders(w *secretWriter, plan OTLPModel, next *OTLPModel) {
	if plan.SecretHeaders.IsNull() {
		next.SecretHeaders = plan.SecretHeaders
		return
	}
	desired := plan.SecretHeaders.Elements()
	headers := next.SecretHeaders.Elements()
	for _, name := range sortedKeys(desired) {
		if w.diags.HasError() {
			break
		}
		value := desired[name]
		if value.Equal(headers[name]) {
			continue
		}
		w.attempted = true
		if err := w.api.SetSecretHeader(w.ctx, w.id, name, value.(types.String).ValueString()); err != nil {
			w.diags.AddError("Unable to save OTLP secret header", err.Error())
			break
		}
		headers[name] = value
	}
	for _, name := range sortedKeys(headers) {
		if _, keep := desired[name]; keep || w.diags.HasError() {
			continue
		}
		w.attempted = true
		if err := w.api.DeleteSecretHeader(w.ctx, w.id, name); err != nil && !client.IsNotFound(err) {
			w.diags.AddError("Unable to delete OTLP secret header", err.Error())
			break
		}
		delete(headers, name)
	}
	next.SecretHeaders = types.MapValueMust(types.StringType, headers)
}

// readIngestionToken reads the token when state has none, which only an import
// leaves; creation stores it from the response.
func readIngestionToken(ctx context.Context, api API, id string, m *OTLPModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if !m.IngestionToken.IsNull() && !m.IngestionToken.IsUnknown() {
		return diags
	}
	token, err := api.GetIngestionToken(ctx, id)
	if err != nil {
		detail := err.Error()
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.IsForbidden() {
			detail = "Reading the token requires edit access to the integration. " + detail
		}
		diags.AddError("Unable to read OTLP ingestion token", detail)
		return diags
	}
	m.IngestionToken = types.StringValue(token)
	return diags
}
