package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

func hookPlugin(t *testing.T, settings Settings) *Plugin {
	t.Helper()
	plugin := &Plugin{}
	plugin.setSettings(settings)
	return plugin
}

func decodeHookResultValue[T any](t *testing.T, result *peggplugin.HookResult) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(result.Value, &value); err != nil {
		t.Fatalf("decode hook value %s: %v", string(result.Value), err)
	}
	return value
}

func TestHooksSpecs(t *testing.T) {
	plugin := &Plugin{}
	specs, err := plugin.Hooks()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("expected 3 hook specs, got %d", len(specs))
	}
	want := map[string]peggplugin.HookKind{
		hookProviders:     peggplugin.HookFilter,
		hookIssueFetch:    peggplugin.HookFilter,
		hookCardSucceeded: peggplugin.HookAction,
	}
	for _, spec := range specs {
		kind, ok := want[spec.Name]
		if !ok {
			t.Errorf("unexpected hook %q", spec.Name)
			continue
		}
		if spec.Kind != kind {
			t.Errorf("%s kind = %q, want %q", spec.Name, spec.Kind, kind)
		}
		if spec.Priority != hookPriority {
			t.Errorf("%s priority = %d", spec.Name, spec.Priority)
		}
		if spec.Description == "" {
			t.Errorf("%s missing description", spec.Name)
		}
	}
}

func TestCapabilities(t *testing.T) {
	capabilities := (&Plugin{}).Capabilities()
	if len(capabilities) != 1 || capabilities[0] != string(peggplugin.CapabilityHooks) {
		t.Fatalf("capabilities = %v, want [%q]", capabilities, peggplugin.CapabilityHooks)
	}
}

func TestExecuteHookProviderInfo(t *testing.T) {
	settings := defaultSettings()
	settings.Site = "acme"
	settings.Email = "user@example.com"
	settings.Token = "secret-token"
	settings.Project = "ABC"
	settings.normalize()

	result, err := hookPlugin(t, settings).ExecuteHook(peggplugin.HookRequest{Name: hookProviders, Value: json.RawMessage(`[{"id":"other","label":"Other"}]`)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled {
		t.Fatal("providers hook not handled")
	}
	infos := decodeHookResultValue[[]ProviderInfo](t, result)
	if len(infos) != 2 || infos[0].ID != "other" {
		t.Fatalf("unexpected providers: %+v", infos)
	}
	info := infos[1]
	if info.ID != pluginName || info.Label != pluginDisplay || !info.Enabled || !info.Available ||
		!info.Authenticated || info.Target != "ABC" || info.Detail == "" || info.SyncMinutes != 0 {
		t.Fatalf("unexpected provider info: %+v", info)
	}
}

func TestExecuteHookDisabledAndOtherProviders(t *testing.T) {
	disabled := hookPlugin(t, Settings{Enabled: false})
	for _, name := range []string{hookProviders, hookIssueFetch, hookCardSucceeded} {
		result, err := disabled.ExecuteHook(peggplugin.HookRequest{Name: name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if result.Handled {
			t.Errorf("%s handled while disabled", name)
		}
	}

	enabled := hookPlugin(t, defaultSettings())
	payload, err := json.Marshal(IssueFetch{Provider: "github"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := enabled.ExecuteHook(peggplugin.HookRequest{Name: hookIssueFetch, Kind: peggplugin.HookFilter, Value: payload})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled {
		t.Fatal("other provider should not be handled")
	}

	result, err = enabled.ExecuteHook(peggplugin.HookRequest{Name: "kanban.unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled {
		t.Fatal("unknown hook should not be handled")
	}
}

func TestExecuteHookIssuesFetch(t *testing.T) {
	server, settings := newFakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(readTestBody(t, r), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["jql"] != "project = ABC ORDER BY updated DESC" {
			t.Errorf("jql = %v", payload["jql"])
		}
		if payload["maxResults"].(float64) != 4 {
			t.Errorf("maxResults = %v", payload["maxResults"])
		}
		writeTestJSON(t, w, map[string]any{"issues": []any{
			map[string]any{"id": "10001", "key": "ABC-7", "fields": map[string]any{
				"summary":   "Fix login",
				"status":    map[string]any{"name": "In Progress", "statusCategory": map[string]any{"key": "indeterminate", "name": "In Progress"}},
				"assignee":  map[string]any{"displayName": "Ada"},
				"priority":  map[string]any{"name": "High"},
				"issuetype": map[string]any{"name": "Task"},
				"labels":    []string{"api", "auth"},
				"created":   "2024-01-01T00:00:00.000+0000",
				"updated":   "2024-01-02T03:04:05.000+0000",
				"description": map[string]any{"type": "doc", "version": 1, "content": []any{
					map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Details"}}},
				}},
			}},
		}})
	})
	defer server.Close()

	value, err := json.Marshal(IssueFetch{Provider: pluginName, Limit: 4, Issues: []HookIssue{{Title: "existing"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := hookPlugin(t, settings).ExecuteHook(peggplugin.HookRequest{
		Name:  hookIssueFetch,
		Kind:  peggplugin.HookFilter,
		Value: value,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled {
		t.Fatal("issues fetch not handled")
	}
	payload := decodeHookResultValue[IssueFetch](t, result)
	if len(payload.Issues) != 2 || payload.Issues[0].Title != "existing" {
		t.Fatalf("issues = %+v", payload.Issues)
	}
	issue := payload.Issues[1]
	if issue.ExternalID != "10001" || issue.Key != "ABC-7" || issue.Title != "Fix login" ||
		issue.Body != "Details" || !strings.HasSuffix(issue.URL, "/browse/ABC-7") ||
		issue.State != "In Progress" || issue.Priority != "High" ||
		issue.CreatedAt != "2024-01-01T00:00:00.000+0000" ||
		issue.UpdatedAt != "2024-01-02T03:04:05.000+0000" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if len(issue.Labels) != 2 || issue.Labels[0] != "api" || len(issue.Assignees) != 1 || issue.Assignees[0] != "Ada" {
		t.Fatalf("unexpected labels/assignees: %+v", issue)
	}
}

func TestExecuteHookCardSucceeded(t *testing.T) {
	var commented bool
	var transitioned bool
	server, settings := newFakeJira(t, func(w http.ResponseWriter, r *http.Request) {
		requireBasicAuth(t, r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/ABC-9/comment":
			var payload struct {
				Body map[string]any `json:"body"`
			}
			if err := json.Unmarshal(readTestBody(t, r), &payload); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(mustJSON(t, payload.Body)), "Run finished") {
				t.Errorf("comment body = %v", payload.Body)
			}
			commented = true
			writeTestJSON(t, w, map[string]any{"id": "1"})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/ABC-9/transitions":
			writeTestJSON(t, w, map[string]any{"transitions": []any{
				map[string]any{"id": "31", "name": "Done", "to": map[string]any{"name": "Done"}},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/ABC-9/transitions":
			var payload struct {
				Transition struct {
					ID string `json:"id"`
				} `json:"transition"`
			}
			if err := json.Unmarshal(readTestBody(t, r), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Transition.ID != "31" {
				t.Errorf("transition id = %q", payload.Transition.ID)
			}
			transitioned = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	value, err := json.Marshal(CardSucceeded{
		Card: Card{
			ID:    "card-1",
			Title: "Fix login",
			Issue: &IssueRef{Provider: pluginName, Key: "ABC-9", URL: settings.BaseURL() + "/browse/ABC-9"},
		},
		Summary:             "Run finished",
		Feedback:            true,
		TransitionOnSuccess: "Done",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := hookPlugin(t, settings).ExecuteHook(peggplugin.HookRequest{
		Name:  hookCardSucceeded,
		Kind:  peggplugin.HookAction,
		Value: value,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Handled {
		t.Fatal("card succeeded not handled")
	}
	if !commented || !transitioned {
		t.Fatalf("commented=%t transitioned=%t", commented, transitioned)
	}
	outcome := decodeHookResultValue[cardSucceededResult](t, result)
	if outcome.Key != "ABC-9" || !outcome.Commented || !outcome.Transitioned {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
}

func TestExecuteHookCardSucceededOtherProvider(t *testing.T) {
	value, err := json.Marshal(CardSucceeded{Card: Card{Issue: &IssueRef{Provider: "github", Key: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := hookPlugin(t, defaultSettings()).ExecuteHook(peggplugin.HookRequest{Name: hookCardSucceeded, Value: value})
	if err != nil {
		t.Fatal(err)
	}
	if result.Handled {
		t.Fatal("other provider should not be handled")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
