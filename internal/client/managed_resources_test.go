package client

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/grepr/terraform-provider-grepr/internal/client/generated"
	"net/http"
	"strings"
	"testing"
)

func TestIntegrationLifecycleRoutes(t *testing.T) {
	for _, route := range []string{"datadog", "splunk", "newrelic", "sumo", "otlp", "data-warehouse", "s3-data-warehouse"} {
		t.Run(route, func(t *testing.T) {
			var methods []string
			server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				want := "/api/v1/integrations/" + route
				if r.Method != http.MethodPost {
					want += "/test-id"
				}
				if r.URL.Path != want {
					t.Errorf("path = %s, want %s", r.URL.Path, want)
				}
				if r.Method == http.MethodPost || r.Method == http.MethodPut {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["type"] != route || body["name"] != "test" || body["payload"] == nil || body["teamIds"] == nil {
						t.Errorf("wrong envelope: %v", body)
					}
				}
				if r.Method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "test-id", "name": "test", "type": route, "version": 1, "organizationId": "org", "payload": map[string]any{}, "teamIds": []string{}})
			})
			defer server.Close()
			ctx := context.Background()
			write := IntegrationWrite{Name: "test", Type: generated.WriteDataWarehouseType(route), Payload: map[string]any{}}
			created, err := c.CreateIntegration(ctx, route, write)
			if err != nil || created.Id != "test-id" {
				t.Fatalf("create = %v, %v", created, err)
			}
			if _, err := c.GetIntegration(ctx, route, created.Id); err != nil {
				t.Fatal(err)
			}
			write.Version = 2
			if _, err := c.UpdateIntegration(ctx, route, created.Id, write); err != nil {
				t.Fatal(err)
			}
			if err := c.DeleteIntegration(ctx, route, created.Id); err != nil {
				t.Fatal(err)
			}
			if strings.Join(methods, ",") != "POST,GET,PUT,DELETE" {
				t.Fatal(methods)
			}
		})
	}
}

func TestWritesAreSentOnceAndErrorsKeepServerMessageWithoutSecrets(t *testing.T) {
	const secret = `sensitive"value`
	calls := 0
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Datadog did not accept key sensitive\"value or sensitive"value"}`))
	})
	defer server.Close()
	err := c.SetIntegrationSecret(context.Background(), "datadog", "id", "api-key", secret)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("lost HTTP status: %v", err)
	}
	if strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), "Datadog did not accept key [redacted]") {
		t.Fatalf("error must keep the server message and hide the secret: %v", err)
	}
	if calls != 1 {
		t.Fatalf("write performed %d times", calls)
	}
}

func TestServerErrorsOnWritesAreNotRetried(t *testing.T) {
	calls := 0
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer server.Close()
	_, err := c.UpdateIntegration(context.Background(), "datadog", "id", IntegrationWrite{Name: "test"})
	if err == nil || calls != 1 {
		t.Fatalf("write performed %d times: %v", calls, err)
	}
	if !strings.Contains(err.Error(), "Internal Server Error") || !strings.Contains(err.Error(), "may have been applied") {
		t.Fatalf("a failed write should show the status and that its outcome is unknown: %v", err)
	}
}

func TestUnreadableWriteResponseMayHaveApplied(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>proxy page</html>"))
	})
	defer server.Close()
	_, err := c.CreateIntegration(context.Background(), "otlp", IntegrationWrite{Name: "n", Type: "otlp"})
	if err == nil || !strings.Contains(err.Error(), "invalid character") || !strings.Contains(err.Error(), "may have been applied") {
		t.Fatalf("an unreadable write response should keep its cause and say the outcome is unknown: %v", err)
	}
}

func TestWriteThatWasNeverSentDoesNotSayItMayHaveApplied(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("nothing should be sent")
	})
	defer server.Close()
	// No token, and none can be fetched, so the request is never sent.
	c.accessToken, c.clientID, c.auth0Domain = "", "never-sent-test", "127.0.0.1:1"
	_, err := c.CreateIntegration(context.Background(), "otlp", IntegrationWrite{Name: "n", Type: "otlp"})
	if err == nil || strings.Contains(err.Error(), "may have been applied") {
		t.Fatalf("a write that failed before sending should not suggest it may exist: %v", err)
	}
}

func TestWriteLostInTransitMayHaveApplied(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	})
	defer server.Close()
	_, err := c.CreateIntegration(context.Background(), "otlp", IntegrationWrite{Name: "n", Type: "otlp"})
	if err == nil || !strings.Contains(err.Error(), "may have been applied") {
		t.Fatalf("a write that failed after sending should say its outcome is unknown: %v", err)
	}
}

func TestPayloadNumbersAreNotRounded(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"id","type":"otlp","payload":{"big":9007199254740993}}`))
	})
	defer server.Close()
	got, err := c.GetIntegration(context.Background(), "otlp", "id")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(got.Payload)
	if string(body) != `{"big":9007199254740993}` {
		t.Fatalf("payload rounded: %s", body)
	}
}

func TestSecretAndTokenWireFormats(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/integrations/datadog/id/api-key":
			var got map[string]string
			_ = json.NewDecoder(r.Body).Decode(&got)
			if r.Method != http.MethodPut || got["secret"] != "key" {
				t.Errorf("wrong secret request: %s %v", r.Method, got)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/integrations/otlp/id/token":
			_, _ = w.Write([]byte(`"generated-token"`))
		case "/api/v1/integrations/otlp/id/secret-headers/X-Api-Key":
			if r.Method != http.MethodPut && r.Method != http.MethodDelete {
				t.Errorf("method %s", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		case "/api/v1/integrations/datadog/id/api-keys":
			var got map[string][]map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			if len(got["keys"]) != 2 || got["keys"][0]["keyId"] != "k1" || got["keys"][0]["value"] != nil || got["keys"][1]["value"] != "new" {
				t.Errorf("wrong key set: %v", got)
			}
			_, _ = w.Write([]byte(`{"id":"id","type":"datadog","payload":{"apiKeys":[{"keyId":"k1"},{"keyId":"k2"}]}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()
	ctx := context.Background()
	if err := c.SetIntegrationSecret(ctx, "datadog", "id", "api-key", "key"); err != nil {
		t.Fatal(err)
	}
	token, err := c.GetIngestionToken(ctx, "id")
	if err != nil || token != "generated-token" {
		t.Fatalf("token = %q, %v", token, err)
	}
	if err := c.SetSecretHeader(ctx, "id", "X-Api-Key", "value"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteSecretHeader(ctx, "id", "X-Api-Key"); err != nil {
		t.Fatal(err)
	}
	keyID, value, send, other := "k1", "new", true, false
	keys := generated.SetApiKeysRequest{Keys: []generated.ApiKey{{KeyId: &keyID, SendKey: &send}, {Value: &value, SendKey: &other}}}
	if _, err := c.SetDatadogAPIKeys(ctx, "id", keys); err != nil {
		t.Fatal(err)
	}
}

func TestDatasetLifecycleWireFormat(t *testing.T) {
	server, c := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "logs" || body["teamIds"] == nil {
				t.Errorf("wrong request %v", body)
			}
			if r.Method == http.MethodPost && body["integrationId"] != "warehouse" {
				t.Errorf("missing warehouse: %v", body)
			}
			if r.Method == http.MethodPut && body["integrationId"] != nil {
				t.Errorf("sent immutable warehouse: %v", body)
			}
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"id":"dataset","name":"logs","integrationId":"warehouse","organizationId":"org","teamIds":[]}`))
	})
	defer server.Close()
	ctx := context.Background()
	teams := []string{}
	d, err := c.CreateDataset(ctx, generated.DatasetCreate{Name: "logs", IntegrationId: "warehouse", TeamIds: &teams})
	if err != nil || d.Id != "dataset" {
		t.Fatalf("create = %v, %v", d, err)
	}
	if _, err := c.GetDataset(ctx, d.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateDataset(ctx, d.Id, generated.DatasetUpdate{Name: "logs", TeamIds: &teams}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDataset(ctx, d.Id); err != nil {
		t.Fatal(err)
	}
}
