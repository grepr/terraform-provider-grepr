package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6/tf6server"
)

// These tests run the real Terraform CLI against the real framework, so
// Terraform's own consistency checks apply; only the Grepr API is faked.

type fixtureProvider struct{ api *fakeAPI }

func (p *fixtureProvider) Metadata(_ context.Context, _ provider.MetadataRequest, r *provider.MetadataResponse) {
	r.TypeName = "grepr"
	r.Version = "test"
}
func (p *fixtureProvider) Schema(_ context.Context, _ provider.SchemaRequest, r *provider.SchemaResponse) {
	r.Schema = providerschema.Schema{}
}
func (p *fixtureProvider) Configure(_ context.Context, _ provider.ConfigureRequest, r *provider.ConfigureResponse) {
	r.ResourceData = p.api
}
func (p *fixtureProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewDatadogResource, NewSplunkResource, NewNewRelicResource, NewSumoResource, NewOTLPResource, NewDataWarehouseResource, NewS3DataWarehouseResource, NewPipeResource}
}
func (p *fixtureProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

// harness drives one Terraform working directory against a fake API.
type harness struct {
	t          *testing.T
	api        *fakeAPI
	dir        string
	env        []string
	terraform  string
	resourceID string
}

func newHarness(t *testing.T, address string) *harness {
	t.Helper()
	terraform, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("Terraform CLI is not installed")
	}
	// A short relative directory keeps Unix socket paths under the macOS
	// length limit, inside the module's ignored build directory.
	scratch := "../../../build/terraform-sockets"
	if err := os.MkdirAll(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", scratch)
	api := newFakeAPI()
	ctx, cancel := context.WithCancel(context.Background())
	reattach := make(chan *plugin.ReattachConfig, 1)
	closed := make(chan struct{})
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- tf6server.Serve("registry.terraform.io/grepr/grepr", providerserver.NewProtocol6(&fixtureProvider{api: api}), tf6server.WithDebug(ctx, reattach, closed), tf6server.WithoutLogStderrOverride(), tf6server.WithLoggingSink(t))
	}()
	var conf *plugin.ReattachConfig
	select {
	case conf = <-reattach:
	case err := <-serveErr:
		t.Fatalf("provider failed: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("provider startup timed out")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("provider did not stop")
		}
	})
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dir, "terraform.tfrc")
	if err := os.WriteFile(rc, []byte("provider_installation {\n dev_overrides {\n \"grepr/grepr\" = \""+dir+"\"\n }\n direct {}\n}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The socket path is relative to this package; Terraform runs elsewhere.
	addr := conf.Addr.String()
	if conf.Addr.Network() == "unix" {
		absolute, _ := filepath.Abs(addr)
		addr, _ = filepath.Rel(dir, absolute)
	}
	attachment, _ := json.Marshal(map[string]any{"registry.terraform.io/grepr/grepr": map[string]any{"Protocol": conf.Protocol, "ProtocolVersion": conf.ProtocolVersion, "Pid": conf.Pid, "Test": conf.Test, "Addr": map[string]string{"Network": conf.Addr.Network(), "String": addr}}})
	env := append(os.Environ(), "TF_REATTACH_PROVIDERS="+string(attachment), "TF_CLI_CONFIG_FILE="+rc, "TF_IN_AUTOMATION=1")
	return &harness{t: t, api: api, dir: dir, env: env, terraform: terraform, resourceID: address}
}

func (h *harness) config(body string) {
	h.t.Helper()
	parts := strings.SplitN(h.resourceID, ".", 2)
	main := "terraform {\n required_providers {\n grepr = { source = \"grepr/grepr\" }\n }\n}\nresource \"" + parts[0] + "\" \"" + parts[1] + "\" {\n" + body + "\n}\n"
	if err := os.WriteFile(filepath.Join(h.dir, "main.tf"), []byte(main), 0600); err != nil {
		h.t.Fatal(err)
	}
}

// exec runs a Terraform command and returns its output and exit code.
func (h *harness) exec(args ...string) (string, int) {
	h.t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	cmd := exec.CommandContext(ctx, h.terraform, append(args, "-no-color")...)
	cmd.Dir, cmd.Env = h.dir, h.env
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(out), exitErr.ExitCode()
	}
	if err != nil {
		h.t.Fatalf("terraform %v: %v", args, err)
	}
	return string(out), 0
}

func (h *harness) apply() {
	h.t.Helper()
	if out, code := h.exec("apply", "-auto-approve", "-input=false"); code != 0 {
		h.t.Fatalf("apply failed:\n%s", out)
	}
}

func (h *harness) applyFails(message string) {
	h.t.Helper()
	out, code := h.exec("apply", "-auto-approve", "-input=false")
	if code == 0 || !strings.Contains(out, message) {
		h.t.Fatalf("apply should fail with %q:\n%s", message, out)
	}
}

// plan returns the plan output and whether it has changes.
func (h *harness) plan() (string, bool) {
	h.t.Helper()
	out, code := h.exec("plan", "-detailed-exitcode", "-input=false")
	if code == 1 {
		h.t.Fatalf("plan failed:\n%s", out)
	}
	return out, code == 2
}

func (h *harness) noChanges() {
	h.t.Helper()
	if out, changed := h.plan(); changed {
		h.t.Fatalf("expected an empty plan:\n%s", out)
	}
}

func (h *harness) reimport() {
	h.t.Helper()
	id := ""
	for k := range h.api.integrations {
		id = k
	}
	if out, code := h.exec("state", "rm", h.resourceID); code != 0 {
		h.t.Fatal(out)
	}
	if out, code := h.exec("import", "-input=false", h.resourceID, id); code != 0 {
		h.t.Fatal(out)
	}
}

func (h *harness) destroy() {
	h.t.Helper()
	if out, code := h.exec("destroy", "-auto-approve", "-input=false"); code != 0 {
		h.t.Fatal(out)
	}
	if len(h.api.integrations) != 0 {
		h.t.Fatal("integration not deleted")
	}
}

func (h *harness) only() string {
	h.t.Helper()
	if len(h.api.integrations) != 1 {
		h.t.Fatalf("expected one integration, have %d", len(h.api.integrations))
	}
	for id := range h.api.integrations {
		return id
	}
	return ""
}

func TestDatadogLifecycle(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "first"
site = "datadoghq.com"
api_key = "api-secret"
app_key = "app-secret"`)
	h.apply()
	if got := strings.Join(h.api.calls[:3], ","); got != "create,api-key,app-key" {
		t.Fatalf("API key must be saved before the application key: %s", got)
	}
	h.noChanges()
	id := h.only()
	// Fields this provider does not model, whether the spec knows them or they
	// are newer than it, keep their values through updates.
	h.api.integrations[id].Payload["filterQuery"] = "service:web"
	h.api.integrations[id].Payload["newerSetting"] = "kept"
	h.api.integrations[id].Version++

	h.config(`name = "renamed"
site = "datadoghq.com"
dashboard_freshness_window = "PT24H"
api_key = "api-secret"
app_key = "app-secret"`)
	h.apply()
	h.noChanges()
	payload := h.api.integrations[id].Payload
	if h.api.integrations[id].Name != "renamed" || payload["dashboardFreshnessWindow"] != "PT24H" || payload["filterQuery"] != "service:web" || payload["newerSetting"] != "kept" {
		t.Fatalf("update not applied, or an unmodeled field was lost: %v", h.api.integrations[id])
	}

	// Secrets are write-only: a rotation elsewhere is not read back, and a
	// configured change is written.
	h.api.secrets[id]["appKey"] = "rotated-elsewhere"
	h.noChanges()
	h.config(`name = "renamed"
site = "datadoghq.com"
dashboard_freshness_window = "PT24H"
api_key = "api-secret"
app_key = "app-secret-2"`)
	h.apply()
	if h.api.secrets[id]["appKey"] != "app-secret-2" {
		t.Fatal("changed secret not written")
	}

	// Import cannot recover plaintext, so the next apply writes the secrets.
	h.reimport()
	h.apply()
	h.noChanges()

	h.destroy()
}

func TestDatadogFailedSecretTaintsAndRecovers(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_key = "api-secret"
app_key = "app-secret"`)
	h.api.failSecret = "app-key"
	h.applyFails("Failed to save app-key")
	out, _ := h.exec("state", "show", "grepr_datadog_integration.test")
	if !strings.Contains(out, "tainted") {
		t.Fatalf("failed create must leave a tainted resource:\n%s", out)
	}
	h.api.failSecret = ""
	h.apply()
	h.noChanges()
	h.only()
	h.destroy()
}

func TestDatadogAPIKeySet(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { value = "primary-key", send_key = true }
  backup  = { value = "backup-key", label = "backup" }
}`)
	h.apply()
	h.noChanges()
	id := h.only()
	before := map[string]string{}
	for _, key := range h.api.keys[id] {
		before[key.value] = key.id
	}

	// Adding a key keeps the others' IDs.
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { value = "primary-key", send_key = true }
  backup  = { value = "backup-key", label = "backup" }
  extra   = { value = "extra-key" }
}`)
	h.apply()
	h.noChanges()
	for _, key := range h.api.keys[id] {
		if old, ok := before[key.value]; ok && old != key.id {
			t.Fatalf("key %s was rotated", key.value)
		}
	}
	if len(h.api.keys[id]) != 3 || before["primary-key"] != h.api.sendKeys[id] {
		t.Fatalf("unexpected key set: %v", h.api.keys[id])
	}

	// A key added outside Terraform is not read back; the next change to the
	// set replaces the whole set, which removes it.
	h.api.keys[id] = append(h.api.keys[id], fakeKey{id: "outside", value: "outside-key"})
	h.noChanges()
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { value = "primary-key", send_key = true }
  backup  = { value = "backup-key", label = "backup" }
  extra   = { value = "extra-key", label = "extra" }
}`)
	h.apply()
	h.noChanges()
	if len(h.api.keys[id]) != 3 {
		t.Fatalf("set write did not replace the set: %v", h.api.keys[id])
	}

	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { value = "primary-key", send_key = true }
  other   = { value = "other-key", send_key = true }
}`)
	if out, code := h.exec("plan", "-input=false"); code == 0 || !strings.Contains(out, "Exactly one API key must set send_key") {
		t.Fatalf("two send keys must be rejected:\n%s", out)
	}
	h.config(`name = "test"
site = "datadoghq.com"`)
	h.destroy()
}

func TestOTLPLifecycle(t *testing.T) {
	h := newHarness(t, "grepr_otlp_integration.test")
	h.config(`name = "test"
endpoint = "https://collector.example.com"
headers = [{ name = "Accept-Language", value = "en-US" }, { name = "Accept-Language", value = "fr" }]
attribute_headers = [{ header_name = "X-Service", attribute_path = ["service", "name"] }]
secret_headers = { Authorization = "Bearer one", "X-Api-Key" = "two" }`)
	h.apply()
	h.noChanges()
	if h.api.called("token") != 0 {
		t.Fatal("token must come from the create response, not the audited endpoint")
	}
	id := h.only()
	if h.api.integrations[id].Payload["outputEncoding"] != "proto" {
		t.Fatal("server default was overwritten")
	}

	// Removing a managed header deletes only that header.
	h.api.headers[id]["X-Unmanaged"] = "kept"
	h.config(`name = "test"
endpoint = "https://collector.example.com"
headers = [{ name = "Accept-Language", value = "en-US" }, { name = "Accept-Language", value = "fr" }]
attribute_headers = [{ header_name = "X-Service", attribute_path = ["service", "name"] }]
secret_headers = { Authorization = "Bearer one" }`)
	h.apply()
	h.noChanges()
	if _, ok := h.api.headers[id]["X-Api-Key"]; ok {
		t.Fatal("removed header not deleted")
	}
	if _, ok := h.api.headers[id]["X-Unmanaged"]; !ok {
		t.Fatal("unmanaged header deleted")
	}

	// An imported integration reads its token once.
	h.reimport()
	h.apply()
	h.noChanges()
	if h.api.called("token") != 1 {
		t.Fatalf("token read %d times after import", h.api.called("token"))
	}
	h.destroy()
}

func TestOTLPAttributeHeadersRemovalConverges(t *testing.T) {
	h := newHarness(t, "grepr_otlp_integration.test")
	h.config(`name = "test"
endpoint = "https://collector.example.com"
attribute_headers = [{ header_name = "X-Service", attribute_path = ["service", "name"] }]`)
	h.apply()
	h.noChanges()
	id := h.only()

	// Removing the attribute clears the headers instead of planning an update forever.
	h.config(`name = "test"
endpoint = "https://collector.example.com"`)
	h.apply()
	h.noChanges()
	if headers := h.api.integrations[id].Payload["attributeHeaders"]; len(headers.([]any)) != 0 {
		t.Fatalf("attribute headers not cleared: %v", headers)
	}

	h.config(`name = "test"
endpoint = "https://collector.example.com"
attribute_headers = []`)
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestOTLPHeadersAreReadAsStored(t *testing.T) {
	h := newHarness(t, "grepr_otlp_integration.test")
	h.config(`name = "test"
endpoint = "https://collector.example.com"`)
	h.apply()
	// The UI saves an integration without plain headers as an empty list.
	id := h.only()
	h.api.integrations[id].Payload["headers"] = []any{}
	if out, changed := h.plan(); !changed || !strings.Contains(out, "headers") {
		t.Fatalf("an empty header list differs from no headers:\n%s", out)
	}
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestSplunkSwitchesHostMode(t *testing.T) {
	h := newHarness(t, "grepr_splunk_integration.test")
	h.config(`name = "test"
splunk_host = "splunk.example.com"
auth_token = "hec-token"`)
	h.apply()
	h.noChanges()
	id := h.only()
	h.config(`name = "test"
ingest_host = "hec.example.com"
web_host = "web.example.com"
auth_token = "hec-token"`)
	h.apply()
	h.noChanges()
	if h.api.integrations[id].Payload["splunkHost"] != nil || h.api.integrations[id].Payload["ingestPort"] != "8088" {
		t.Fatalf("mode switch must clear splunk_host and keep server defaults: %v", h.api.integrations[id].Payload)
	}
	h.destroy()
}

func TestDurationsAreStoredAsTheServerWritesThem(t *testing.T) {
	h := newHarness(t, "grepr_newrelic_integration.test")
	h.config(`name = "test"
site = "US"
dashboard_freshness_window = "PT672H"
license_key = "license"`)
	h.apply()
	h.noChanges()
	h.reimport()
	h.apply()
	h.noChanges()
	// Another spelling of the same duration differs from what the server stores.
	h.config(`name = "test"
site = "US"
dashboard_freshness_window = "P28D"
license_key = "license"`)
	h.apply()
	if _, changed := h.plan(); !changed {
		t.Fatal("the server's spelling should show against P28D")
	}
	h.destroy()
}

func TestSumoAndManagedWarehouse(t *testing.T) {
	sumo := newHarness(t, "grepr_sumo_integration.test")
	sumo.config(`name = "test"
endpoint = "https://collectors.sumologic.com/receiver/v1/http/code"`)
	sumo.apply()
	sumo.noChanges()
	sumo.destroy()

	warehouse := newHarness(t, "grepr_data_warehouse_integration.test")
	warehouse.config(`name = "test"`)
	warehouse.apply()
	warehouse.noChanges()
	warehouse.reimport()
	warehouse.noChanges()
	warehouse.destroy()
}

func TestPipeLifecycle(t *testing.T) {
	h := newHarness(t, "grepr_pipe_integration.test")
	h.config(`name = "routed"`)
	h.apply()
	h.noChanges()
	id := h.only()
	if got := h.api.integrations[id]; got.Type != "grepr-pipe" || len(got.Payload) != 0 {
		t.Fatalf("pipe must be created with its type and an empty payload: %v", got)
	}

	h.config(`name = "renamed"`)
	h.apply()
	h.noChanges()
	if h.api.integrations[id].Name != "renamed" || h.api.integrations[id].Version != 2 {
		t.Fatalf("rename not applied: %v", h.api.integrations[id])
	}

	h.reimport()
	h.noChanges()
	h.destroy()
}

func TestS3WarehouseBucketChangeReplaces(t *testing.T) {
	h := newHarness(t, "grepr_s3_data_warehouse_integration.test")
	h.config(`name = "test"
bucket_name = "first-bucket"`)
	h.apply()
	h.noChanges()
	id := h.only()
	if h.api.integrations[id].Payload["assumeRoleArn"] == nil {
		t.Fatal("derived role not recorded")
	}
	h.config(`name = "test"
bucket_name = "second-bucket"`)
	out, changed := h.plan()
	if !changed || !strings.Contains(out, "must be replaced") {
		t.Fatalf("bucket change must plan a replacement:\n%s", out)
	}
	h.apply()
	h.noChanges()
	if h.only() == id {
		t.Fatal("integration not replaced")
	}
	h.destroy()
}

func TestDatadogKeyWithEmptyLabel(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = { "" = { value = "primary-key", send_key = true } }`)
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestOTLPSecretHeaderOwnership(t *testing.T) {
	h := newHarness(t, "grepr_otlp_integration.test")
	h.config(`name = "test"
endpoint = "https://collector.example.com"
secret_headers = { Authorization = "Bearer one", "X-Api-Key" = "two" }`)
	h.apply()
	id := h.only()

	// A header rotated elsewhere is still deleted when removed from the map.
	h.api.headers[id]["Authorization"] = "Bearer rotated"
	h.config(`name = "test"
endpoint = "https://collector.example.com"
secret_headers = { "X-Api-Key" = "two" }`)
	h.apply()
	h.noChanges()
	if _, ok := h.api.headers[id]["Authorization"]; ok {
		t.Fatal("a rotated header removed from the map was not deleted")
	}

	// Removing the whole attribute stops managing the headers without deleting them.
	h.config(`name = "test"
endpoint = "https://collector.example.com"`)
	h.apply()
	h.noChanges()
	if h.api.headers[id]["X-Api-Key"] != "two" {
		t.Fatal("removing secret_headers deleted the remote headers")
	}
	h.destroy()
}

func TestDatadogKeyLabelMovedToAnotherKey(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { value = "secret-aa", send_key = true }
  backup  = { value = "secret-bb" }
}`)
	h.apply()
	id := h.only()
	var backupID string
	for _, key := range h.api.keys[id] {
		if key.value == "secret-bb" {
			backupID = key.id
		}
	}
	// Point the primary label at the backup key, keeping the primary's value: the
	// key must be rotated to that value, not kept with its own.
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = {
  primary = { key_id = "` + backupID + `", value = "secret-aa", send_key = true }
}`)
	h.apply()
	h.noChanges()
	if len(h.api.keys[id]) != 1 || h.api.keys[id][0].id != backupID || h.api.keys[id][0].value != "secret-aa" {
		t.Fatalf("label move did not rotate the key: %v", h.api.keys[id])
	}
	h.destroy()
}

func TestOTLPImportWithoutTokenAccessFails(t *testing.T) {
	h := newHarness(t, "grepr_otlp_integration.test")
	h.config(`name = "test"
endpoint = "https://collector.example.com"`)
	h.apply()
	h.api.tokenStatus = http.StatusForbidden
	if out, code := h.exec("state", "rm", h.resourceID); code != 0 {
		t.Fatal(out)
	}
	out, code := h.exec("import", "-input=false", h.resourceID, h.only())
	if code == 0 || !strings.Contains(out, "Reading the token requires edit access") {
		t.Fatalf("an import that cannot read the token should fail with the reason:\n%s", out)
	}
	h.api.tokenStatus = 0
	if out, code := h.exec("import", "-input=false", h.resourceID, h.only()); code != 0 {
		t.Fatalf("import should succeed once the token is readable:\n%s", out)
	}
	h.noChanges()
	h.destroy()
}

func TestSecretsIgnoreStaleReads(t *testing.T) {
	h := newHarness(t, "grepr_sumo_integration.test")
	h.config(`name = "test"
endpoint = "https://collectors.sumologic.com/receiver/v1/http/one"`)
	h.apply()
	// A server replica can serve a secret from a cache older than the write.
	h.api.staleSecretReads = true
	h.config(`name = "test"
endpoint = "https://collectors.sumologic.com/receiver/v1/http/two"`)
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestUpdateKeepsLargeNumbersExact(t *testing.T) {
	// A typed payload, and one the spec models as a plain map.
	for resource, settings := range map[string]string{
		"grepr_datadog_integration.test":        "\nsite = \"datadoghq.com\"",
		"grepr_data_warehouse_integration.test": "",
	} {
		t.Run(resource, func(t *testing.T) {
			h := newHarness(t, resource)
			h.config(`name = "test"` + settings)
			h.apply()
			id := h.only()
			h.api.integrations[id].Payload["newerLimit"] = json.Number("9007199254740993")
			h.config(`name = "renamed"` + settings)
			h.apply()
			if got := h.api.integrations[id].Payload["newerLimit"]; got != json.Number("9007199254740993") {
				t.Fatalf("an unmodeled number changed: %v", got)
			}
			h.destroy()
		})
	}
}

func TestUpdateWithoutStoredSettingsIsRefused(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"`)
	h.apply()
	id := h.only()
	h.api.integrations[id].Payload["validateKeysOnSave"] = false
	// State written before settings were stored, such as by an earlier build.
	pulled, code := h.exec("state", "pull")
	if code != 0 {
		t.Fatal(pulled)
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(pulled), &state); err != nil {
		t.Fatal(err)
	}
	instance := state["resources"].([]any)[0].(map[string]any)["instances"].([]any)[0].(map[string]any)
	delete(instance, "private")
	state["serial"] = state["serial"].(float64) + 1
	edited, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(h.dir, "edited.tfstate"), edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := h.exec("state", "push", "edited.tfstate"); code != 0 {
		t.Fatal(out)
	}
	h.config(`name = "renamed"
site = "datadoghq.com"`)
	if out, code := h.exec("apply", "-auto-approve", "-input=false", "-refresh=false"); code == 0 || !strings.Contains(out, "Refresh the integration") {
		t.Fatalf("an update without stored settings should be refused:\n%s", out)
	}
	if h.api.integrations[id].Payload["validateKeysOnSave"] != false {
		t.Fatal("an unmodeled setting was reset")
	}
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestSecretWritesKeepTheVersionCurrent(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_key = "api-secret"`)
	h.apply()
	// The key write bumped the version; without refreshing, the next update must
	// still carry the current one.
	h.config(`name = "renamed"
site = "datadoghq.com"
api_key = "api-secret"`)
	if out, code := h.exec("apply", "-auto-approve", "-input=false", "-refresh=false"); code != 0 {
		t.Fatalf("an update right after secret writes should not conflict:\n%s", out)
	}
	h.noChanges()
	h.destroy()
}

func TestSavedPlanFailsAfterAChangeElsewhere(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"`)
	h.apply()
	id := h.only()
	h.config(`name = "renamed"
site = "datadoghq.com"`)
	if out, code := h.exec("plan", "-out=tfplan", "-input=false"); code != 0 {
		t.Fatal(out)
	}
	// The integration changes after the plan was saved.
	h.api.integrations[id].Version++
	if out, code := h.exec("apply", "-input=false", "tfplan"); code == 0 || !strings.Contains(out, "modified or deleted concurrently") {
		t.Fatalf("the saved plan should be refused by the version check:\n%s", out)
	}
	h.apply()
	h.noChanges()
	h.destroy()
}

func TestEmptyKeyIDIsRejectedBeforeWriting(t *testing.T) {
	h := newHarness(t, "grepr_datadog_integration.test")
	h.config(`name = "test"
site = "datadoghq.com"
api_keys = { primary = { key_id = "", value = "primary-key-aa", send_key = true } }`)
	if out, code := h.exec("plan", "-input=false"); code == 0 || !strings.Contains(out, "key_id") {
		t.Fatalf("an empty key_id should be rejected at plan time:\n%s", out)
	}
	if len(h.api.integrations) != 0 {
		t.Fatal("nothing should be created")
	}
}

func TestNullMapEntriesAreRejectedBeforeWriting(t *testing.T) {
	for name, config := range map[string]string{
		"secret_headers": `name = "test"
endpoint = "https://otlp.example.com"
secret_headers = { Authorization = null }`,
		"api_keys": `name = "test"
site = "datadoghq.com"
api_keys = { primary = { value = "primary-key-aa", send_key = true }, empty = null }`,
	} {
		t.Run(name, func(t *testing.T) {
			resource := "grepr_otlp_integration.test"
			if name == "api_keys" {
				resource = "grepr_datadog_integration.test"
			}
			h := newHarness(t, resource)
			h.config(config)
			if out, code := h.exec("plan", "-input=false"); code == 0 || !strings.Contains(out, "null") {
				t.Fatalf("a null %s entry should be rejected at plan time:\n%s", name, out)
			}
			if len(h.api.integrations) != 0 {
				t.Fatal("nothing should be created")
			}
		})
	}
}
