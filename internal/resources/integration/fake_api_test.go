package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
)

// fakeAPI imitates the parts of the Grepr integrations API the resources rely
// on: server defaults, masked secret reads, read-only fields rejected on
// writes, null fields left out of reads, duration canonicalization,
// optimistic versions, and the OTLP token returned only by create.
type fakeAPI struct {
	integrations map[string]*client.Integration
	secrets      map[string]map[string]string // integration ID -> payload field -> value
	headers      map[string]map[string]string // integration ID -> header -> value
	keys         map[string][]fakeKey
	sendKeys     map[string]string // integration ID -> send key ID
	nextID       int
	calls        []string
	// failSecret makes writes to this secret endpoint fail.
	failSecret string
	// tokenStatus, when set, is the error status of the token endpoint.
	tokenStatus int
	// staleSecretReads serves secrets the way a replica with an old cache does.
	staleSecretReads bool
}

type fakeKey struct{ id, value, label string }

func newFakeAPI() *fakeAPI {
	return &fakeAPI{integrations: map[string]*client.Integration{}, secrets: map[string]map[string]string{}, headers: map[string]map[string]string{}, keys: map[string][]fakeKey{}, sendKeys: map[string]string{}}
}

var fakeDefaults = map[string]map[string]any{
	"datadog":           {"filterQuery": "", "additionalTags": []any{}, "validateKeysOnSave": true},
	"splunk":            {"secure": true, "ingestPort": "8088", "restPort": "8089", "webPort": "443", "additionalTags": []any{}, "filterQuery": ""},
	"newrelic":          {"privateLink": false, "dashboardFreshnessWindow": "PT672H"},
	"otlp":              {"outputEncoding": "proto", "attributeHeaders": []any{}},
	"s3-data-warehouse": {"assumeRoleArn": "arn:aws:iam::123456789012:role/grepr", "region": "us-east-1", "prefix": ""},
}

var fakeReadOnly = map[string][]string{
	"datadog":  {"apiKey", "apiKeys", "appKey", "clientToken", "exceptions"},
	"splunk":   {"authToken", "restAuthToken", "exceptions"},
	"newrelic": {"licenseKey", "apiKey", "exceptions"},
	"sumo":     {"endpoint"},
	"otlp":     {"token", "plainTextToken", "secretHeaders"},
}

// fakeSecretFields maps secret endpoints to the payload field that masks them.
var fakeSecretFields = map[string]string{"api-key": "apiKey", "app-key": "appKey", "auth-token": "authToken", "rest-auth-token": "restAuthToken", "license-key": "licenseKey", "endpoint": "endpoint"}

func mask(value string) string {
	shown := min(2, len(value)/4)
	return "**********" + value[len(value)-shown:]
}

func badRequest(message string) error {
	return &client.APIError{StatusCode: http.StatusBadRequest, Message: message}
}

func (a *fakeAPI) store(route string, payload map[string]any) (map[string]any, error) {
	for _, field := range fakeReadOnly[route] {
		if _, present := payload[field]; present {
			return nil, badRequest("payload." + field + " must be null")
		}
	}
	stored := maps.Clone(payload)
	for k, v := range fakeDefaults[route] {
		if stored[k] == nil {
			stored[k] = v
		}
	}
	// The server leaves null fields out of its responses.
	for k, v := range stored {
		if v == nil {
			delete(stored, k)
		}
	}
	if encoding, ok := stored["outputEncoding"].(string); ok {
		stored["outputEncoding"] = strings.ToLower(encoding)
	}
	// The server stores durations in its own form, such as P1D as PT24H.
	if window, ok := stored["dashboardFreshnessWindow"].(string); ok {
		if days, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(window, "P"), "D")); err == nil {
			stored["dashboardFreshnessWindow"] = fmt.Sprintf("PT%dH", days*24)
		}
	}
	// Simulate JSON round-tripping so values have the types the client decodes:
	// numbers stay exact, as the server and client keep them.
	raw, _ := json.Marshal(stored)
	var decoded map[string]any
	_ = decodeJSON(raw, &decoded)
	return decoded, nil
}

func (a *fakeAPI) CreateIntegration(_ context.Context, route string, body client.IntegrationWrite) (*client.Integration, error) {
	a.calls = append(a.calls, "create")
	payload, err := a.store(route, body.Payload)
	if err != nil {
		return nil, err
	}
	a.nextID++
	id := fmt.Sprintf("id-%d", a.nextID)
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.integrations[id] = &client.Integration{Id: id, Name: body.Name, Type: generated.ReadDataWarehouseType(body.Type), Version: 1, Payload: payload, TeamIds: body.TeamIds, OrganizationId: "org", CreatedAt: &createdAt, UpdatedAt: &createdAt}
	created := *a.integrations[id]
	created.Payload = maps.Clone(payload)
	if route == "otlp" {
		a.secrets[id] = map[string]string{"token": "generated-token"}
		created.Payload["plainTextToken"] = "generated-token"
	}
	return &created, nil
}

func (a *fakeAPI) GetIntegration(_ context.Context, _ string, id string) (*client.Integration, error) {
	a.calls = append(a.calls, "read")
	stored, ok := a.integrations[id]
	if !ok {
		return nil, &client.APIError{StatusCode: http.StatusNotFound, Message: "Not Found"}
	}
	read := *stored
	read.Payload = maps.Clone(stored.Payload)
	for field, value := range a.secrets[id] {
		if a.staleSecretReads {
			value = "stale-value"
		}
		if field != "token" {
			read.Payload[field] = mask(value)
		}
	}
	if headers := a.headers[id]; len(headers) > 0 {
		masked := map[string]any{}
		for name, value := range headers {
			masked[name] = map[string]any{"secret": mask(value)}
		}
		read.Payload["secretHeaders"] = masked
	}
	if keys := a.keys[id]; len(keys) > 0 {
		views := []any{}
		for _, key := range keys {
			send := key.id == a.sendKeys[id]
			view := map[string]any{"keyId": key.id, "maskedValue": mask(key.value), "sendKey": send}
			if key.label != "" {
				view["label"] = key.label
			}
			if send {
				read.Payload["apiKey"] = mask(key.value)
			}
			views = append(views, view)
		}
		read.Payload["apiKeys"] = views
	}
	return &read, nil
}

func (a *fakeAPI) UpdateIntegration(_ context.Context, route, id string, body client.IntegrationWrite) (*client.Integration, error) {
	a.calls = append(a.calls, "update")
	stored := a.integrations[id]
	if body.Version != stored.Version+1 {
		return nil, &client.APIError{StatusCode: http.StatusConflict, Message: "Integration was modified or deleted concurrently."}
	}
	if route == "s3-data-warehouse" && body.Payload["bucketName"] != stored.Payload["bucketName"] {
		return nil, badRequest("payload.bucketName cannot be updated")
	}
	payload, err := a.store(route, body.Payload)
	if err != nil {
		return nil, err
	}
	stored.Name, stored.Payload, stored.TeamIds, stored.Version = body.Name, payload, body.TeamIds, body.Version
	updated := *stored
	return &updated, nil
}

func (a *fakeAPI) DeleteIntegration(_ context.Context, _ string, id string) error {
	a.calls = append(a.calls, "delete")
	delete(a.integrations, id)
	return nil
}

func (a *fakeAPI) SetIntegrationSecret(_ context.Context, _ string, id, endpoint, value string) error {
	a.calls = append(a.calls, endpoint)
	if endpoint == a.failSecret {
		return badRequest("rejected")
	}
	if a.secrets[id] == nil {
		a.secrets[id] = map[string]string{}
	}
	a.secrets[id][fakeSecretFields[endpoint]] = value
	a.integrations[id].Version++
	return nil
}

func (a *fakeAPI) SetSecretHeader(_ context.Context, id, name, value string) error {
	a.calls = append(a.calls, "set-header:"+name)
	if a.headers[id] == nil {
		a.headers[id] = map[string]string{}
	}
	a.headers[id][name] = value
	return nil
}

func (a *fakeAPI) DeleteSecretHeader(_ context.Context, id, name string) error {
	a.calls = append(a.calls, "delete-header:"+name)
	delete(a.headers[id], name)
	return nil
}

func (a *fakeAPI) GetIngestionToken(_ context.Context, id string) (string, error) {
	a.calls = append(a.calls, "token")
	if a.tokenStatus != 0 {
		return "", &client.APIError{StatusCode: a.tokenStatus, Message: "Forbidden"}
	}
	return a.secrets[id]["token"], nil
}

// SetDatadogAPIKeys replaces the key set, keeping request order.
func (a *fakeAPI) SetDatadogAPIKeys(ctx context.Context, id string, request generated.SetApiKeysRequest) (*client.Integration, error) {
	a.calls = append(a.calls, "api-keys")
	existing := map[string]fakeKey{}
	for _, key := range a.keys[id] {
		existing[key.id] = key
	}
	var keys []fakeKey
	sendKey := ""
	for _, request := range request.Keys {
		key := fakeKey{}
		if request.KeyId != nil {
			key.id = *request.KeyId
		}
		if key.id == "" {
			a.nextID++
			key.id = fmt.Sprintf("key-%d", a.nextID)
		} else if _, ok := existing[key.id]; !ok {
			return nil, &client.APIError{StatusCode: http.StatusNotFound, Message: "API keys not found: [" + key.id + "]"}
		} else {
			key.value = existing[key.id].value
		}
		if request.Value != nil {
			key.value = *request.Value
		}
		if request.Label != nil {
			key.label = *request.Label
		}
		if request.SendKey != nil && *request.SendKey && sendKey == "" {
			sendKey = key.id
		}
		keys = append(keys, key)
	}
	if sendKey == "" {
		sendKey = keys[0].id
	}
	a.keys[id], a.sendKeys[id] = keys, sendKey
	a.integrations[id].Version++
	return a.GetIntegration(ctx, "datadog", id)
}

func (a *fakeAPI) called(name string) int {
	count := 0
	for _, call := range a.calls {
		if call == name || strings.HasPrefix(call, name+":") {
			count++
		}
	}
	return count
}
