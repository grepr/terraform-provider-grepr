package pipeline

import (
	"context"
	"encoding/json"
	"maps"
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

// A pipeline without tags must read as an empty map. A null value does not match
// a configured tags = {}, so every plan would show a change.
func TestUpdateModelFromJobReadsNoTagsAsEmptyMap(t *testing.T) {
	r := &PipelineResource{}
	model := &PipelineResourceModel{}
	job := testJob(t, testGraphJSON)
	job.Tags = nil

	r.updateModelFromJob(context.Background(), model, job, nil)

	if model.Tags.IsNull() || len(model.Tags.Elements()) != 0 {
		t.Errorf("expected an empty tags map, got %s", model.Tags)
	}
}

// On apply the model is the plan. Terraform rejects state that differs from a
// known planned value, so a null plan from old state must stay null.
func TestUpdateModelFromJobKeepsPlannedTags(t *testing.T) {
	r := &PipelineResource{}
	model := &PipelineResourceModel{Tags: types.MapNull(types.StringType)}
	job := testJob(t, testGraphJSON)
	job.Tags = nil

	r.updateModelFromJob(context.Background(), model, job, &originalJobData{DesiredState: "RUNNING"})

	if !model.Tags.IsNull() {
		t.Errorf("expected the planned null tags, got %s", model.Tags)
	}
}

// Unset tags on create plan an unknown value, so state takes the API tags.
func TestUpdateModelFromJobResolvesUnknownTags(t *testing.T) {
	r := &PipelineResource{}
	model := &PipelineResourceModel{Tags: types.MapUnknown(types.StringType)}
	job := testJob(t, testGraphJSON)
	job.Tags = map[string]string{"grepr-ui-managed": "true"}

	r.updateModelFromJob(context.Background(), model, job, &originalJobData{DesiredState: "RUNNING"})

	if model.Tags.IsUnknown() || len(model.Tags.Elements()) != 1 {
		t.Errorf("expected the API tags, got %s", model.Tags)
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

func stringMap(t *testing.T, m map[string]string) types.Map {
	t.Helper()
	value, diags := types.MapValueFrom(context.Background(), types.StringType, m)
	if diags.HasError() {
		t.Fatalf("failed to build map: %v", diags)
	}
	return value
}

// Tags are sent only when the planned set is known and differs from the tags
// compared against; unknown planned tags (left unset on adoption) keep the
// pipeline's.
func TestTagsToApply(t *testing.T) {
	current := map[string]string{"grepr-ui-managed": "true", "env": "prod"}
	staging := map[string]string{"env": "staging"}
	tests := []struct {
		name        string
		planned     types.Map
		current     map[string]string
		wantChanged bool
		wantTags    map[string]string
	}{
		{name: "unknown tags keep the pipeline's", planned: types.MapUnknown(types.StringType), current: current},
		{name: "unset tags keep the pipeline's", planned: types.MapNull(types.StringType), current: current},
		{name: "same tags send nothing", planned: stringMap(t, current), current: current},
		{name: "empty matches no tags", planned: stringMap(t, map[string]string{}), current: nil},
		{
			name:        "changed tags are sent in full",
			planned:     stringMap(t, staging),
			current:     current,
			wantChanged: true,
			wantTags:    staging,
		},
		{
			name:        "empty clears existing tags",
			planned:     stringMap(t, map[string]string{}),
			current:     current,
			wantChanged: true,
			wantTags:    map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, changed, diags := tagsToApply(context.Background(), tt.planned, tt.current)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if changed != tt.wantChanged {
				t.Fatalf("expected changed=%t, got %t", tt.wantChanged, changed)
			}
			if changed && !maps.Equal(tags, tt.wantTags) {
				t.Errorf("expected tags %v, got %v", tt.wantTags, tags)
			}
		})
	}
}

// Update compares the planned tags with state, never with the live pipeline: with
// tags unset or under ignore_changes the plan carries the state's tags, even from
// a saved plan made before someone edited the tags elsewhere, so those edits stay.
func TestTagsToUpdate(t *testing.T) {
	ctx := context.Background()
	prod := map[string]string{"env": "prod"}
	staging := map[string]string{"env": "staging"}

	_, changed, diags := tagsToUpdate(ctx,
		PipelineResourceModel{Tags: stringMap(t, prod)},
		PipelineResourceModel{Tags: stringMap(t, prod)})
	if diags.HasError() || changed {
		t.Errorf("a plan that keeps the recorded tags must send nothing, got changed=%t diags=%v", changed, diags)
	}

	tags, changed, diags := tagsToUpdate(ctx,
		PipelineResourceModel{Tags: stringMap(t, staging)},
		PipelineResourceModel{Tags: stringMap(t, prod)})
	if diags.HasError() || !changed || !maps.Equal(tags, staging) {
		t.Errorf("a plan that changes the tags must send them, got changed=%t tags=%v diags=%v", changed, tags, diags)
	}
}

// The job update creates a version and redeploys, so it runs only when the
// graph, desired state, or teams change, never for tags or provider settings.
func TestNeedsJobUpdate(t *testing.T) {
	ctx := context.Background()
	base := PipelineResourceModel{
		JobGraphJSON: jsontypes.NewNormalizedValue(testGraphJSON),
		DesiredState: types.StringValue("RUNNING"),
		TeamIDs:      types.SetNull(types.StringType),
		Tags:         stringMap(t, map[string]string{"env": "prod"}),
		WaitForState: types.BoolValue(true),
	}
	teams, _ := types.SetValueFrom(ctx, types.StringType, []string{"team-1"})
	tests := []struct {
		name   string
		change func(m *PipelineResourceModel)
		want   bool
	}{
		{name: "nothing changed", change: func(m *PipelineResourceModel) {}},
		{name: "only tags changed", change: func(m *PipelineResourceModel) {
			m.Tags = stringMap(t, map[string]string{"env": "staging"})
		}},
		{name: "only a provider setting changed", change: func(m *PipelineResourceModel) {
			m.WaitForState = types.BoolValue(false)
		}},
		{name: "graph reformatted only", change: func(m *PipelineResourceModel) {
			m.JobGraphJSON = jsontypes.NewNormalizedValue(
				`{ "edges": [], "vertices": [{"inverted": false, "name": "f", "type": "logs-filter"}] }`)
		}},
		{name: "graph changed", want: true, change: func(m *PipelineResourceModel) {
			m.JobGraphJSON = jsontypes.NewNormalizedValue(`{"vertices":[],"edges":[]}`)
		}},
		{name: "desired state changed", want: true, change: func(m *PipelineResourceModel) {
			m.DesiredState = types.StringValue("STOPPED")
		}},
		{name: "teams changed", want: true, change: func(m *PipelineResourceModel) {
			m.TeamIDs = teams
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := base
			tt.change(&plan)
			got, diags := needsJobUpdate(ctx, plan, base)
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if got != tt.want {
				t.Errorf("expected needsJobUpdate=%t, got %t", tt.want, got)
			}
		})
	}
}

// The server takes tag edits only while the pipeline is running or stopped. From
// any other state a job update, such as a graph fix for a failed pipeline, must
// go first, so a tag change in the same apply cannot block it.
func TestTagsBeforeJobUpdate(t *testing.T) {
	tests := []struct {
		name       string
		state      client.JobState
		jobChanged bool
		want       bool
	}{
		{name: "running with a job update", state: client.JobStateRunning, jobChanged: true, want: true},
		{name: "stopped with a job update", state: client.JobStateStopped, jobChanged: true, want: true},
		{name: "failed with a graph fix", state: client.JobStateFailed, jobChanged: true, want: false},
		{name: "updating with a job update", state: client.JobStateUpdating, jobChanged: true, want: false},
		{name: "failed with only a tag change", state: client.JobStateFailed, jobChanged: false, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagsBeforeJobUpdate(tt.state, tt.jobChanged); got != tt.want {
				t.Errorf("expected tagsBeforeJobUpdate(%s, %t)=%t, got %t", tt.state, tt.jobChanged, tt.want, got)
			}
		})
	}
}
