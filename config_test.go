package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPrecedence(t *testing.T) {
	t.Setenv("PEGG_JIRA_SITE", "envsite")
	t.Setenv("PEGG_JIRA_EMAIL", "env@example.com")
	t.Setenv("PEGG_JIRA_TOKEN", "env-token")
	t.Setenv("PEGG_JIRA_PROJECT", "ENV")
	t.Setenv("PEGG_JIRA_JQL", "project = ENV")
	t.Setenv("PEGG_JIRA_SYNC_STATE", "Open")
	t.Setenv("PEGG_JIRA_SYNC_LABELS", `["bug","api"]`)
	t.Setenv("PEGG_JIRA_SYNC_LIMIT", "25")
	t.Setenv("PEGG_JIRA_DEFAULT_TRANSITION", "Done")
	t.Setenv("PEGG_JIRA_TIMEOUT_SECONDS", "45")

	settings := defaultSettings()
	settings.applyHost([]byte(`{"plugins":{"options":{"jira":{"site":"host.example","email":"host@example.com","token":"host-token","project":"HOST","default_jql":"host jql","request_timeout_seconds":99}}}}`))
	if err := settings.applyOptions(json.RawMessage(`{"request_timeout_seconds":12}`)); err != nil {
		t.Fatal(err)
	}
	settings.applyEnv()
	settings.normalize()

	if settings.Site != "envsite" || settings.Token != "env-token" || settings.Email != "env@example.com" {
		t.Fatalf("env should win: %+v", settings)
	}
	if settings.Project != "ENV" {
		t.Fatalf("project env not applied: %+v", settings)
	}
	if settings.DefaultJQL != "project = ENV" || settings.RequestTimeoutSeconds != 45 {
		t.Fatalf("jql/timeout env not applied: %+v", settings)
	}
}

func TestConfigHostAndOptions(t *testing.T) {
	settings := defaultSettings()
	settings.applyHost([]byte(`{"plugins":{"options":{"kanban-jira":{"site":"hostsite","token":"host-token","project":"HOST"}}}}`))
	if err := settings.applyOptions(json.RawMessage(`{"request_timeout_seconds":12}`)); err != nil {
		t.Fatal(err)
	}
	settings.normalize()

	if settings.Site != "hostsite" || settings.Token != "host-token" || settings.Project != "HOST" {
		t.Fatalf("host settings not applied: %+v", settings)
	}
	if settings.RequestTimeoutSeconds != 12 {
		t.Fatalf("options not applied: %+v", settings)
	}
}

func TestConfigOptionKeys(t *testing.T) {
	cases := map[string]string{
		"kanban-jira":      "primary",
		"pegg-kanban-jira": "alt",
		"jira":             "legacy",
		"pegg-jira":        "legacy-alt",
	}
	for key, site := range cases {
		settings := defaultSettings()
		settings.applyHost([]byte(`{"plugins":{"options":{"` + key + `":{"site":"` + site + `"}}}}`))
		if settings.Site != site {
			t.Errorf("%s: site = %q, want %q", key, settings.Site, site)
		}
	}

	settings := defaultSettings()
	settings.applyHost([]byte(`{"plugins":{"options":{"jira":{"site":"legacy"},"kanban-jira":{"site":"primary"}}}}`))
	if settings.Site != "primary" {
		t.Fatalf("preferred key should win over legacy: %q", settings.Site)
	}
}

func TestBaseURL(t *testing.T) {
	cases := map[string]string{
		"acme":                        "https://acme.atlassian.net",
		"acme.atlassian.net":          "https://acme.atlassian.net",
		"https://acme.atlassian.net/": "https://acme.atlassian.net",
		"http://localhost:8080/":      "http://localhost:8080",
		"":                            "",
	}
	for site, want := range cases {
		settings := Settings{Site: site}
		if got := settings.BaseURL(); got != want {
			t.Errorf("BaseURL(%q) = %q, want %q", site, got, want)
		}
	}
}

func TestPersistSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".pegg"), 0o755); err != nil {
		t.Fatal(err)
	}

	settings := defaultSettings()
	settings.Site = "acme"
	settings.Email = "user@example.com"
	settings.Token = "secret-token"
	settings.Project = "ABC"
	settings.DefaultJQL = "project = ABC"
	settings.RequestTimeoutSeconds = 15

	path, err := persistSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	options := root["plugins"].(map[string]any)["options"].(map[string]any)[configKey].(map[string]any)
	if options["token"] != "secret-token" || options["site"] != "acme" {
		t.Fatalf("settings not persisted: %v", options)
	}
	if options["default_jql"] != "project = ABC" {
		t.Fatalf("jql not persisted: %v", options)
	}
	if _, ok := options["board_id"]; ok {
		t.Fatalf("board setting should not be persisted: %v", options)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRedactSettings(t *testing.T) {
	settings := Settings{Email: "user@example.com", Token: "abc123"}
	encoded := basicAuth(settings.Email, settings.Token)
	got := redactSettings("token abc123 auth "+encoded, settings)
	if got != "token [redacted] auth [redacted]" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestDisabledEnv(t *testing.T) {
	t.Setenv("PEGG_JIRA_DISABLED", "true")
	settings := defaultSettings()
	settings.applyEnv()
	if settings.Enabled {
		t.Fatal("PEGG_JIRA_DISABLED should disable the plugin")
	}
}

func TestDefaultJQL(t *testing.T) {
	settings := defaultSettings()
	settings.Project = "ABC"
	if got := defaultJQL(settings); got != "project = ABC ORDER BY updated DESC" {
		t.Fatalf("default JQL = %q", got)
	}
	settings.DefaultJQL = "project = XYZ"
	if got := defaultJQL(settings); got != "project = XYZ" {
		t.Fatalf("explicit JQL = %q", got)
	}

	settings.DefaultJQL = ""
	settings.SyncState = "Open"
	settings.SyncLabels = []string{"bug"}
	want := `project = ABC AND status = "Open" AND labels = "bug" ORDER BY updated DESC`
	if got := defaultJQL(settings); got != want {
		t.Fatalf("configured JQL = %q, want %q", got, want)
	}
}

func TestDataCenterBaseURL(t *testing.T) {
	settings := Settings{Kind: "datacenter", BaseURLRaw: "https://jira.example.com/"}
	if got := settings.BaseURL(); got != "https://jira.example.com" {
		t.Fatalf("BaseURL = %q", got)
	}
	settings = Settings{Kind: "dc", Site: "jira.example.com"}
	if got := settings.BaseURL(); got != "https://jira.example.com" {
		t.Fatalf("BaseURL from site = %q", got)
	}
}

func TestDataCenterAuthAndAPIRoot(t *testing.T) {
	settings := Settings{Kind: "datacenter", BaseURLRaw: "https://jira.example.com", Token: "pat-token"}
	client := newClient(settings)
	if got := client.authHeader(); got != "Bearer pat-token" {
		t.Fatalf("auth = %q", got)
	}
	if got := client.apiRoot(); got != "/rest/api/2" {
		t.Fatalf("api root = %q", got)
	}

	settings = Settings{Kind: "datacenter", BaseURLRaw: "https://jira.example.com", Username: "user", Password: "pass"}
	client = newClient(settings)
	if got := client.authHeader(); got != "Basic "+basicAuth("user", "pass") {
		t.Fatalf("basic auth = %q", got)
	}
	if !settings.authenticated() {
		t.Fatal("username/password should authenticate")
	}
}

func TestCloudAuthAndAPIRoot(t *testing.T) {
	settings := Settings{Kind: "cloud", Site: "acme", Email: "user@example.com", Token: "api-token"}
	client := newClient(settings)
	if got := client.authHeader(); got != "Basic "+basicAuth("user@example.com", "api-token") {
		t.Fatalf("auth = %q", got)
	}
	if got := client.apiRoot(); got != "/rest/api/3" {
		t.Fatalf("api root = %q", got)
	}
}
