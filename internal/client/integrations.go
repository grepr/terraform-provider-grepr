package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

// Integration and IntegrationWrite are the envelope every integration type
// shares. The spec generates one per type around a typed payload; the managed
// data warehouse's carries its payload as a JSON map, so it serves every type
// and keeps fields newer than this provider's spec as the server sent them.
type (
	Integration      = generated.ReadDataWarehouse
	IntegrationWrite = generated.WriteDataWarehouse
)

// integrationOperations are the generated calls for one integration type.
type integrationOperations struct {
	create func(*generated.Client, context.Context, string, io.Reader, ...generated.RequestEditorFn) (*http.Response, error)
	get    func(*generated.Client, context.Context, string, ...generated.RequestEditorFn) (*http.Response, error)
	update func(*generated.Client, context.Context, string, string, io.Reader, ...generated.RequestEditorFn) (*http.Response, error)
	delete func(*generated.Client, context.Context, string, ...generated.RequestEditorFn) (*http.Response, error)
}

type api = generated.Client

var integrationRoutes = map[string]integrationOperations{
	"datadog":           {(*api).CreateDatadogIntegrationWithBody, (*api).GetDatadogIntegration, (*api).UpdateDatadogIntegrationWithBody, (*api).DeleteDatadogIntegration},
	"splunk":            {(*api).CreateSplunkIntegrationWithBody, (*api).GetSplunkIntegration, (*api).UpdateSplunkIntegrationWithBody, (*api).DeleteSplunkIntegration},
	"newrelic":          {(*api).CreateNewRelicIntegrationWithBody, (*api).GetNewRelicIntegration, (*api).UpdateNewRelicIntegrationWithBody, (*api).DeleteNewRelicIntegration},
	"sumo":              {(*api).CreateSumoIntegrationWithBody, (*api).GetSumoIntegration, (*api).UpdateSumoIntegrationWithBody, (*api).DeleteSumoIntegration},
	"otlp":              {(*api).CreateOtlpIntegrationWithBody, (*api).GetOtlpIntegration, (*api).UpdateOtlpIntegrationWithBody, (*api).DeleteOtlpIntegration},
	"data-warehouse":    {(*api).CreateDataWarehouseIntegrationWithBody, (*api).GetDataWarehouseIntegration, (*api).UpdateDataWarehouseIntegrationWithBody, (*api).DeleteDataWarehouseIntegration},
	"s3-data-warehouse": {(*api).CreateS3DataWarehouseIntegrationWithBody, (*api).GetS3DataWarehouseIntegration, (*api).UpdateS3DataWarehouseIntegrationWithBody, (*api).DeleteS3DataWarehouseIntegration},
	"grepr-pipe":        {(*api).CreateGreprPipeIntegrationWithBody, (*api).GetGreprPipeIntegration, (*api).UpdateGreprPipeIntegrationWithBody, (*api).DeleteGreprPipeIntegration},
}

// secretEndpoints are the generated calls that write one secret, by type and endpoint.
var secretEndpoints = map[string]func(*api, context.Context, string, generated.SecretApi, ...generated.RequestEditorFn) (*http.Response, error){
	"datadog/api-key":        (*api).UpsertDatadogApiKey,
	"datadog/app-key":        (*api).UpsertDatadogAppKey,
	"splunk/auth-token":      (*api).UpsertSplunkAuthToken,
	"splunk/rest-auth-token": (*api).UpsertSplunkRestAuthToken,
	"newrelic/license-key":   (*api).UpsertNewRelicLicenseKey,
	"newrelic/api-key":       (*api).UpsertNewRelicApiKey,
	"sumo/endpoint":          (*api).UpsertSumoEndpoint,
}

func operationsFor(route string) (integrationOperations, error) {
	ops, ok := integrationRoutes[route]
	if !ok {
		return ops, fmt.Errorf("unknown integration type %q", route)
	}
	return ops, nil
}

func (c *Client) writeIntegration(body IntegrationWrite, send func(*api, io.Reader) (*http.Response, error)) (*Integration, error) {
	if body.TeamIds == nil {
		body.TeamIds = &[]string{}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var result Integration
	err = c.call(false, func(a *api) (*http.Response, error) { return send(a, bytes.NewReader(encoded)) }, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CreateIntegration(ctx context.Context, route string, body IntegrationWrite) (*Integration, error) {
	ops, err := operationsFor(route)
	if err != nil {
		return nil, err
	}
	return c.writeIntegration(body, func(a *api, payload io.Reader) (*http.Response, error) {
		return ops.create(a, ctx, "application/json", payload)
	})
}

func (c *Client) UpdateIntegration(ctx context.Context, route, id string, body IntegrationWrite) (*Integration, error) {
	ops, err := operationsFor(route)
	if err != nil {
		return nil, err
	}
	return c.writeIntegration(body, func(a *api, payload io.Reader) (*http.Response, error) {
		return ops.update(a, ctx, id, "application/json", payload)
	})
}

func (c *Client) GetIntegration(ctx context.Context, route, id string) (*Integration, error) {
	ops, err := operationsFor(route)
	if err != nil {
		return nil, err
	}
	var result Integration
	if err := c.call(true, func(a *api) (*http.Response, error) { return ops.get(a, ctx, id) }, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) DeleteIntegration(ctx context.Context, route, id string) error {
	ops, err := operationsFor(route)
	if err != nil {
		return err
	}
	return c.call(false, func(a *api) (*http.Response, error) { return ops.delete(a, ctx, id) }, nil)
}

// SetIntegrationSecret writes one secret through its dedicated endpoint, such
// as datadog/{id}/api-key.
func (c *Client) SetIntegrationSecret(ctx context.Context, route, id, endpoint, value string) error {
	upsert, ok := secretEndpoints[route+"/"+endpoint]
	if !ok {
		return fmt.Errorf("unknown secret %s for %s integrations", endpoint, route)
	}
	return c.call(false, func(a *api) (*http.Response, error) {
		return upsert(a, ctx, id, generated.SecretApi{Secret: value})
	}, nil, value)
}

// GetIngestionToken reads an OTLP integration's token in the clear. The server
// requires edit access and audit-logs every call, so callers use it sparingly.
func (c *Client) GetIngestionToken(ctx context.Context, id string) (string, error) {
	var token string
	err := c.call(true, func(a *api) (*http.Response, error) { return a.GetOtlpToken(ctx, id) }, &token)
	return token, err
}

func (c *Client) SetSecretHeader(ctx context.Context, id, name, value string) error {
	return c.call(false, func(a *api) (*http.Response, error) {
		return a.UpsertOtlpSecretHeader(ctx, id, name, generated.SecretApi{Secret: value})
	}, nil, value)
}

func (c *Client) DeleteSecretHeader(ctx context.Context, id, name string) error {
	return c.call(false, func(a *api) (*http.Response, error) { return a.DeleteOtlpSecretHeader(ctx, id, name) }, nil)
}

// SetDatadogAPIKeys replaces a Datadog integration's whole API key set.
func (c *Client) SetDatadogAPIKeys(ctx context.Context, id string, keys generated.SetApiKeysRequest) (*Integration, error) {
	var values []string
	for _, key := range keys.Keys {
		if key.Value != nil {
			values = append(values, *key.Value)
		}
	}
	var result Integration
	err := c.call(false, func(a *api) (*http.Response, error) { return a.SetDatadogApiKeys(ctx, id, keys) }, &result, values...)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
