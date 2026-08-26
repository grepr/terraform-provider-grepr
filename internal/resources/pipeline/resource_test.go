package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grepr/terraform-provider-grepr/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const testGraphJSON = `{"vertices":[{"type":"logs-filter","name":"f","inverted":false}],"edges":[]}`

func testJob(t *testing.T, graphJSON string) *client.Job {
	t.Helper()

	var graph client.JobGraph
	if err := json.Unmarshal([]byte(graphJSON), &graph); err != nil {
		t.Fatalf("failed to build test graph: %s", err)
	}

	return &client.Job{
		Id:             "0test123",
		Name:           "test_pipeline",
		Version:        3,
		State:          client.JobStateRunning,
		OrganizationId: "greprtest",
		JobGraph:       graph,
		Tags:           map[string]string{"grepr-ui-managed": "true"},
	}
}

// A pipeline edited outside Terraform must show up as drift, so Read has to take
// the graph from the API rather than leave the value already in state.
func TestUpdateModelFromJobRefreshesGraphFromAPI(t *testing.T) {
	r := &PipelineResource{}
	model := &PipelineResourceModel{
		JobGraphJSON: jsontypes.NewNormalizedValue(`{"vertices":[],"edges":[]}`),
	}

	r.updateModelFromJob(context.Background(), model, testJob(t, testGraphJSON), nil)

	var got client.JobGraph
	if err := json.Unmarshal([]byte(model.JobGraphJSON.ValueString()), &got); err != nil {
		t.Fatalf("state graph is not valid JSON: %s", err)
	}
	if len(got.Vertices) != 1 {
		t.Errorf("expected the API graph in state, got %s", model.JobGraphJSON.ValueString())
	}
}

// Create and Update pass the configured string so state matches the config that
// produced it, which keeps apply results consistent.
func TestUpdateModelFromJobPreservesConfiguredGraph(t *testing.T) {
	r := &PipelineResource{}
	model := &PipelineResourceModel{}
	configured := `{"edges": [], "vertices": []}`

	r.updateModelFromJob(context.Background(), model, testJob(t, testGraphJSON), &originalJobData{
		JobGraphJSON: configured,
		DesiredState: "RUNNING",
	})

	if model.JobGraphJSON.ValueString() != configured {
		t.Errorf("expected the configured string, got %s", model.JobGraphJSON.ValueString())
	}
}

// Import returns none of the provider-side defaults. Without them an unchanged
// pipeline plans a change on the first apply after an import.
func TestApplyProviderDefaultsFillsNullValues(t *testing.T) {
	model := &PipelineResourceModel{
		WaitForState:    types.BoolNull(),
		StateTimeout:    types.Int64Null(),
		RollbackEnabled: types.BoolNull(),
	}

	applyProviderDefaults(model)

	if model.WaitForState.ValueBool() != defaultWaitForState {
		t.Errorf("wait_for_state = %v, want %v", model.WaitForState.ValueBool(), defaultWaitForState)
	}
	if model.StateTimeout.ValueInt64() != defaultStateTimeoutSeconds {
		t.Errorf("state_timeout = %d, want %d", model.StateTimeout.ValueInt64(), defaultStateTimeoutSeconds)
	}
	if model.RollbackEnabled.ValueBool() != defaultRollbackEnabled {
		t.Errorf("rollback_enabled = %v, want %v", model.RollbackEnabled.ValueBool(), defaultRollbackEnabled)
	}
}

func TestApplyProviderDefaultsKeepsConfiguredValues(t *testing.T) {
	model := &PipelineResourceModel{
		WaitForState:    types.BoolValue(false),
		StateTimeout:    types.Int64Value(60),
		RollbackEnabled: types.BoolValue(false),
	}

	applyProviderDefaults(model)

	if model.WaitForState.ValueBool() {
		t.Error("wait_for_state was overwritten")
	}
	if model.StateTimeout.ValueInt64() != 60 {
		t.Errorf("state_timeout = %d, want 60", model.StateTimeout.ValueInt64())
	}
	if model.RollbackEnabled.ValueBool() {
		t.Error("rollback_enabled was overwritten")
	}
}

// Indentation and key order differ between the API, the CLI, and a hand-edited
// file. Neither is a change to the pipeline.
func TestJobGraphJSONComparesSemantically(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "indentation",
			a:    `{"vertices":[],"edges":[]}`,
			b:    "{\n  \"vertices\": [],\n  \"edges\": []\n}",
			want: true,
		},
		{
			name: "key order",
			a:    `{"vertices":[],"edges":[]}`,
			b:    `{"edges":[],"vertices":[]}`,
			want: true,
		},
		{
			name: "real change",
			a:    `{"reductionTimeWindow":"PT2M"}`,
			b:    `{"reductionTimeWindow":"PT3M"}`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := jsontypes.NewNormalizedValue(tc.a).StringSemanticEquals(
				context.Background(),
				jsontypes.NewNormalizedValue(tc.b),
			)
			if diags.HasError() {
				t.Fatalf("comparison failed: %v", diags)
			}
			if got != tc.want {
				t.Errorf("semantic equality = %v, want %v", got, tc.want)
			}
		})
	}
}

// The API and the CLI disagree on how they render the same number, so comparing
// the raw strings would report drift forever on a pipeline nobody touched.
func TestSameJobGraphIgnoresRepresentation(t *testing.T) {
	apiJob := testJob(t, `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":70.0}],"edges":[]}`)

	tests := []struct {
		name      string
		stateJSON string
		want      bool
	}{
		{
			name:      "number formatting",
			stateJSON: `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":70}],"edges":[]}`,
			want:      true,
		},
		{
			name:      "key order and whitespace",
			stateJSON: "{\n \"edges\": [],\n \"vertices\": [\n  {\"name\": \"r\", \"similarityThreshold\": 70.0, \"type\": \"log-reducer\"}\n ]\n}",
			want:      true,
		},
		{
			name:      "server materialized an optional field the config omits",
			stateJSON: `{"vertices":[{"type":"log-reducer","name":"r"}],"edges":[]}`,
			want:      true,
		},
		{
			name:      "config sets a field the api does not report",
			stateJSON: `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":70,"unknownField":1}],"edges":[]}`,
			want:      false,
		},
		{
			name:      "edited outside terraform",
			stateJSON: `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":85.0}],"edges":[]}`,
			want:      false,
		},
		{
			name:      "empty state",
			stateJSON: "",
			want:      false,
		},
		{
			name:      "unparseable state",
			stateJSON: "{not json",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameJobGraph(tc.stateJSON, apiJob.JobGraph); got != tc.want {
				t.Errorf("sameJobGraph = %v, want %v", got, tc.want)
			}
		})
	}
}

// Drift must survive the refresh, otherwise plan reports no changes on a
// pipeline that no longer matches the configuration.
func TestUpdateModelFromJobKeepsStateStringWhenGraphMatches(t *testing.T) {
	r := &PipelineResource{}
	original := `{"edges": [], "vertices": [{"inverted": false, "name": "f", "type": "logs-filter"}]}`
	model := &PipelineResourceModel{JobGraphJSON: jsontypes.NewNormalizedValue(original)}

	r.updateModelFromJob(context.Background(), model, testJob(t, testGraphJSON), nil)

	if model.JobGraphJSON.ValueString() != original {
		t.Errorf("state string was rewritten for an unchanged graph: %s", model.JobGraphJSON.ValueString())
	}
}

// The API fills in optional fields the configuration omits. Those must not read
// as drift, while a real edit still has to.
func TestAPIMatchesState(t *testing.T) {
	tests := []struct {
		name  string
		api   string
		state string
		want  bool
	}{
		{
			name:  "extra key on the api side is a server default",
			api:   `{"name":"s","hostEnrichmentEnabled":false}`,
			state: `{"name":"s"}`,
			want:  true,
		},
		{
			name:  "extra key on the state side is a real difference",
			api:   `{"name":"s"}`,
			state: `{"name":"s","hostEnrichmentEnabled":false}`,
			want:  false,
		},
		{
			name:  "changed value on a specified field",
			api:   `{"name":"s","hostEnrichmentEnabled":true}`,
			state: `{"name":"s","hostEnrichmentEnabled":false}`,
			want:  false,
		},
		{
			name:  "array gains an element",
			api:   `{"partitionByTags":["service","test"]}`,
			state: `{"partitionByTags":["service"]}`,
			want:  false,
		},
		{
			name:  "nested defaults",
			api:   `{"shardingConfig":{"shardThreshold":40000,"shardInMultiplier":4}}`,
			state: `{"shardingConfig":{"shardThreshold":40000}}`,
			want:  true,
		},
		{
			name:  "nested change",
			api:   `{"shardingConfig":{"shardThreshold":50000}}`,
			state: `{"shardingConfig":{"shardThreshold":40000}}`,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apiValue, err := canonicalJSONValue(tc.api)
			if err != nil {
				t.Fatalf("bad api fixture: %s", err)
			}
			stateValue, err := canonicalJSONValue(tc.state)
			if err != nil {
				t.Fatalf("bad state fixture: %s", err)
			}
			if got := apiMatchesState(apiValue, stateValue); got != tc.want {
				t.Errorf("apiMatchesState = %v, want %v", got, tc.want)
			}
		})
	}
}

// A drift plan should show only the fields that changed, not every default the
// server fills in. The projected graph must also survive a round trip as JSON.
func TestRefreshedJobGraphKeepsTrackedFieldsOnly(t *testing.T) {
	apiJob := testJob(t, `{"vertices":[{"type":"logs-filter","name":"f","inverted":true,"predicate":{"query":"x","type":"datadog-query"},"maxLateEventTimestampDelta":900}],"edges":[]}`)
	stateJSON := `{"vertices":[{"type":"logs-filter","name":"f","inverted":false}],"edges":[]}`

	refreshed, err := refreshedJobGraph(stateJSON, apiJob.JobGraph)
	if err != nil {
		t.Fatalf("refreshedJobGraph failed: %s", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(refreshed), &got); err != nil {
		t.Fatalf("refreshed graph is not valid JSON: %s", err)
	}

	vertex := got["vertices"].([]interface{})[0].(map[string]interface{})
	if _, present := vertex["predicate"]; present {
		t.Error("server default predicate leaked into the refreshed graph")
	}
	if _, present := vertex["maxLateEventTimestampDelta"]; present {
		t.Error("server default maxLateEventTimestampDelta leaked into the refreshed graph")
	}
	if vertex["inverted"] != true {
		t.Errorf("tracked field was not refreshed: inverted = %v, want true", vertex["inverted"])
	}
}

// Numbers must come back as JSON numbers, not strings.
func TestRefreshedJobGraphPreservesNumberTypes(t *testing.T) {
	apiJob := testJob(t, `{"vertices":[{"type":"log-reducer","name":"r","dedupThreshold":9,"similarityThreshold":70.0}],"edges":[]}`)
	stateJSON := `{"vertices":[{"type":"log-reducer","name":"r","dedupThreshold":4,"similarityThreshold":70}],"edges":[]}`

	refreshed, err := refreshedJobGraph(stateJSON, apiJob.JobGraph)
	if err != nil {
		t.Fatalf("refreshedJobGraph failed: %s", err)
	}

	if strings.Contains(refreshed, `"dedupThreshold":"9"`) {
		t.Errorf("number was serialised as a string: %s", refreshed)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(refreshed), &got); err != nil {
		t.Fatalf("refreshed graph is not valid JSON: %s", err)
	}
	vertex := got["vertices"].([]interface{})[0].(map[string]interface{})
	if vertex["dedupThreshold"] != float64(9) {
		t.Errorf("dedupThreshold = %v, want 9", vertex["dedupThreshold"])
	}
}

// A field the API stops reporting has to disappear from state, so its loss
// still shows up as a change.
func TestRefreshedJobGraphDropsFieldsTheAPINoLongerReports(t *testing.T) {
	apiJob := testJob(t, `{"vertices":[{"type":"logs-filter","name":"f"}],"edges":[]}`)
	stateJSON := `{"vertices":[{"type":"logs-filter","name":"f","inverted":false}],"edges":[]}`

	refreshed, err := refreshedJobGraph(stateJSON, apiJob.JobGraph)
	if err != nil {
		t.Fatalf("refreshedJobGraph failed: %s", err)
	}

	if strings.Contains(refreshed, "inverted") {
		t.Errorf("field the API dropped is still in state: %s", refreshed)
	}
}

// A refresh must not rewrite a number just because the API spells it
// differently, or every drift plan carries an unrelated change.
func TestRefreshedJobGraphKeepsNumberSpelling(t *testing.T) {
	apiJob := testJob(t, `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":70.0,"dedupThreshold":9}],"edges":[]}`)
	stateJSON := `{"vertices":[{"type":"log-reducer","name":"r","similarityThreshold":70,"dedupThreshold":4}],"edges":[]}`

	refreshed, err := refreshedJobGraph(stateJSON, apiJob.JobGraph)
	if err != nil {
		t.Fatalf("refreshedJobGraph failed: %s", err)
	}

	if !strings.Contains(refreshed, `"similarityThreshold":70,`) {
		t.Errorf("unchanged number was respelled: %s", refreshed)
	}
	if !strings.Contains(refreshed, `"dedupThreshold":9`) {
		t.Errorf("changed number was not refreshed: %s", refreshed)
	}
}
